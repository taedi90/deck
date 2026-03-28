package stepmeta

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
)

var (
	parseOnce sync.Once
	parsed    map[string]Docs
	errParsed error
)

func buildDocs(rootType reflect.Type) (Docs, error) {
	parseOnce.Do(func() {
		parsed, errParsed = parseStepspecDocs()
	})
	if errParsed != nil {
		return Docs{}, errParsed
	}
	return buildDocsForType(rootType, map[string]bool{})
}

func buildDocsForType(rootType reflect.Type, seen map[string]bool) (Docs, error) {
	t := rootType
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	typeName := t.Name()
	docs, ok := parsed[typeName]
	if !ok {
		return Docs{}, fmt.Errorf("stepmeta: missing parsed docs for type %s", typeName)
	}
	if seen[typeName] {
		return docs, nil
	}
	seen[typeName] = true
	out := Docs{
		Summary:   docs.Summary,
		WhenToUse: docs.WhenToUse,
		Example:   docs.Example,
		Notes:     append([]string(nil), docs.Notes...),
		Fields:    make([]FieldDoc, 0, len(docs.Fields)),
		Source:    docs.Source,
	}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		jsonTag := field.Tag.Get("json")
		name, _, _ := strings.Cut(jsonTag, ",")
		name = strings.TrimSpace(name)
		if name == "" || name == "-" {
			continue
		}
		fieldDoc := findFieldDoc(docs.Fields, "spec."+name)
		fieldType := field.Type
		for fieldType.Kind() == reflect.Pointer {
			fieldType = fieldType.Elem()
		}
		arrayPrefix := ""
		if fieldType.Kind() == reflect.Slice || fieldType.Kind() == reflect.Array {
			arrayPrefix = "[]"
			fieldType = fieldType.Elem()
			for fieldType.Kind() == reflect.Pointer {
				fieldType = fieldType.Elem()
			}
		}
		if fieldType.Kind() == reflect.Struct && fieldType.PkgPath() == t.PkgPath() {
			nested, err := buildDocsForType(fieldType, cloneSeen(seen))
			if err != nil {
				return Docs{}, err
			}
			out.Fields = append(out.Fields, fieldDoc)
			for _, nestedField := range nested.Fields {
				path := strings.TrimPrefix(nestedField.Path, "spec.")
				out.Fields = append(out.Fields, FieldDoc{
					Path:        "spec." + name + arrayPrefix + "." + path,
					Description: nestedField.Description,
					Example:     nestedField.Example,
					Required:    nestedField.Required,
					Hidden:      nestedField.Hidden,
					Source:      nestedField.Source,
				})
			}
			continue
		}
		out.Fields = append(out.Fields, fieldDoc)
	}
	return out, nil
}

func findFieldDoc(fields []FieldDoc, path string) FieldDoc {
	for _, field := range fields {
		if field.Path == path {
			return field
		}
	}
	return FieldDoc{Path: path}
}

func cloneSeen(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func parseStepspecDocs() (map[string]Docs, error) {
	fset := token.NewFileSet()
	dir := stepspecDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("stepmeta: read stepspec package: %w", err)
	}
	files := make(map[string]*ast.File, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		fullPath := dir + "/" + entry.Name()
		file, err := parser.ParseFile(fset, fullPath, nil, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("stepmeta: parse %s: %w", entry.Name(), err)
		}
		files[fullPath] = file
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("stepmeta: no stepspec files found")
	}
	out := map[string]Docs{}
	for filePath, file := range files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				structType, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					continue
				}
				group := typeSpec.Doc
				if group == nil {
					group = gen.Doc
				}
				docs, err := parseTypeDocs(fset, filePath, typeSpec, group, structType)
				if err != nil {
					return nil, err
				}
				out[typeSpec.Name.Name] = docs
			}
		}
	}
	return out, nil
}

func parseTypeDocs(fset *token.FileSet, filePath string, typeSpec *ast.TypeSpec, group *ast.CommentGroup, structType *ast.StructType) (Docs, error) {
	parsed, err := parseCommentGroup(fset, filePath, typeSpec.Name.Name, group)
	if err != nil {
		return Docs{}, err
	}
	docs := Docs{
		Summary:   parsed.summary,
		WhenToUse: parsed.when,
		Example:   parsed.example,
		Notes:     append([]string(nil), parsed.notes...),
		Source:    sourceRef(fset, filePath, typeSpec.Pos()),
	}
	for _, field := range structType.Fields.List {
		if len(field.Names) == 0 {
			continue
		}
		jsonName := jsonFieldName(field.Tag)
		if jsonName == "" || jsonName == "-" {
			continue
		}
		fieldParsed, err := parseCommentGroup(fset, filePath, typeSpec.Name.Name+"."+field.Names[0].Name, field.Doc)
		if err != nil {
			return Docs{}, err
		}
		if fieldParsed.hidden {
			continue
		}
		docs.Fields = append(docs.Fields, FieldDoc{
			Path:        "spec." + jsonName,
			Description: fieldParsed.summary,
			Example:     fieldParsed.example,
			Required:    fieldParsed.required || fieldRequired(field.Tag),
			Hidden:      fieldParsed.hidden,
			Source:      sourceRef(fset, filePath, field.Pos()),
		})
	}
	return docs, nil
}

type parsedComment struct {
	summary  string
	when     string
	example  string
	notes    []string
	required bool
	hidden   bool
}

func parseCommentGroup(fset *token.FileSet, filePath string, name string, group *ast.CommentGroup) (parsedComment, error) {
	if group == nil {
		return parsedComment{}, nil
	}
	lines := commentLines(group)
	var textLines []string
	out := parsedComment{}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			textLines = append(textLines, "")
			continue
		}
		if !strings.HasPrefix(trimmed, "@deck.") {
			textLines = append(textLines, trimmed)
			continue
		}
		key, value, _ := strings.Cut(strings.TrimPrefix(trimmed, "@deck."), " ")
		value = strings.TrimSpace(value)
		switch key {
		case "when":
			out.when = value
		case "note":
			out.notes = append(out.notes, value)
		case "required":
			out.required = true
		case "hidden":
			out.hidden = true
		case "example":
			if value != "" {
				out.example = value
				continue
			}
			block := make([]string, 0)
			for i+1 < len(lines) {
				next := strings.TrimSpace(lines[i+1])
				if strings.HasPrefix(next, "@deck.") {
					break
				}
				i++
				block = append(block, lines[i])
			}
			out.example = strings.TrimSpace(strings.Join(block, "\n"))
		default:
			line := 0
			if group != nil && i < len(group.List) {
				line = fset.Position(group.List[i].Pos()).Line
			}
			return parsedComment{}, fmt.Errorf("stepmeta: %s:%d %s uses unsupported directive @deck.%s", filePath, line, name, key)
		}
	}
	out.summary = strings.TrimSpace(strings.Join(filterEmpty(textLines), " "))
	return out, nil
}

func sourceRef(fset *token.FileSet, filePath string, pos token.Pos) SourceRef {
	position := fset.Position(pos)
	if position.Filename == "" {
		position.Filename = filePath
	}
	return SourceRef{File: position.Filename, Line: position.Line}
}

func commentLines(group *ast.CommentGroup) []string {
	lines := make([]string, 0, len(group.List))
	for _, comment := range group.List {
		text := strings.TrimPrefix(comment.Text, "//")
		text = strings.TrimPrefix(text, " ")
		text = strings.TrimPrefix(text, "/*")
		text = strings.TrimSuffix(text, "*/")
		lines = append(lines, strings.TrimRight(text, "\r\n"))
	}
	return lines
}

func filterEmpty(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, strings.TrimSpace(line))
	}
	return out
}

func jsonFieldName(tag *ast.BasicLit) string {
	if tag == nil {
		return ""
	}
	value, err := strconv.Unquote(tag.Value)
	if err != nil {
		return ""
	}
	jsonTag := reflect.StructTag(value).Get("json")
	if jsonTag == "" {
		return ""
	}
	name, _, _ := strings.Cut(jsonTag, ",")
	return strings.TrimSpace(name)
}

func fieldRequired(tag *ast.BasicLit) bool {
	if tag == nil {
		return true
	}
	value, err := strconv.Unquote(tag.Value)
	if err != nil {
		return true
	}
	jsonTag := reflect.StructTag(value).Get("json")
	if jsonTag == "" {
		return true
	}
	_, opts, _ := strings.Cut(jsonTag, ",")
	for _, opt := range strings.Split(opts, ",") {
		if strings.TrimSpace(opt) == "omitempty" {
			return false
		}
	}
	return true
}
