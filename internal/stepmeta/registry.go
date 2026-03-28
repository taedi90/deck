package stepmeta

import (
	"fmt"
	"io/fs"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
)

type Definition struct {
	Kind        string
	Family      string
	FamilyTitle string
	DocsPage    string
	DocsOrder   int
	Visibility  string
	Roles       []string
	Outputs     []string
	SchemaFile  string
	Summary     string
	WhenToUse   string
	Example     string
	Notes       []string
	Ask         AskMetadata
}

type AskMetadata struct {
	MatchSignals             []string
	KeyFields                []string
	CommonMistakes           []string
	RepairHints              []string
	ValidationHints          []ValidationHint
	ConstrainedLiteralFields []ConstrainedLiteralField
	QualityRules             []QualityRule
	AntiSignals              []string
}

type ValidationHint struct {
	ErrorContains string
	Fix           string
}

type ConstrainedLiteralField struct {
	Path          string
	AllowedValues []string
	Guidance      string
}

type QualityRule struct {
	Trigger string
	Message string
	Level   string
}

type FieldDoc struct {
	Path        string
	Description string
	Example     string
	Required    bool
	Hidden      bool
	Source      SourceRef
}

type Docs struct {
	Summary   string
	WhenToUse string
	Example   string
	Notes     []string
	Fields    []FieldDoc
	Source    SourceRef
}

type Schema struct {
	SpecType any
	Patch    func(root map[string]any)
	Source   SourceRef
}

type SourceRef struct {
	File string
	Line int
}

type Entry struct {
	Definition Definition
	TypeName   string
	Docs       Docs
	Schema     Schema
}

var (
	mu           sync.RWMutex
	entries      = map[string]registeredDef{}
	schemaByKind = map[string]registeredSchema{}
	stepspecFS   fs.FS
)

type registeredDef struct {
	Definition Definition
	TypeName   string
	Type       reflect.Type
}

type registeredSchema struct {
	Type   reflect.Type
	Patch  func(root map[string]any)
	Source SourceRef
}

func MustRegister[T any](def Definition) struct{} {
	kind := strings.TrimSpace(def.Kind)
	if kind == "" {
		panic("stepmeta: kind is required")
	}
	typeName := typeNameFor[T]()
	if typeName == "" {
		panic(fmt.Sprintf("stepmeta: could not resolve type name for %s", kind))
	}
	var zero T
	t := reflect.TypeOf(zero)
	if t == nil {
		panic(fmt.Sprintf("stepmeta: could not resolve reflect type for %s", kind))
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		panic(fmt.Sprintf("stepmeta: registered type for %s must be a struct", kind))
	}
	mu.Lock()
	defer mu.Unlock()
	if _, exists := entries[kind]; exists {
		panic(fmt.Sprintf("stepmeta: duplicate registration for %s", kind))
	}
	entries[kind] = registeredDef{Definition: def, TypeName: typeName, Type: t}
	return struct{}{}
}

func RegisteredKinds() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(entries))
	for kind := range entries {
		out = append(out, kind)
	}
	sort.Strings(out)
	return out
}

func Lookup(kind string) (Entry, bool, error) {
	mu.RLock()
	registered, ok := entries[strings.TrimSpace(kind)]
	schemaRegistered, schemaOK := schemaByKind[strings.TrimSpace(kind)]
	mu.RUnlock()
	if !ok {
		return Entry{}, false, nil
	}
	typeName := registered.TypeName
	docs, err := buildDocs(registered.Type)
	if err != nil {
		return Entry{}, true, err
	}
	entry := Entry{Definition: cloneDefinition(registered.Definition), TypeName: typeName, Docs: docs}
	if schemaOK {
		entry.Schema = Schema{SpecType: reflect.New(schemaRegistered.Type).Interface(), Patch: schemaRegistered.Patch, Source: schemaRegistered.Source}
	}
	entry.Docs = mergeDefinitionDocs(entry.Definition, entry.Docs)
	if err := validateEntry(entry); err != nil {
		return Entry{}, true, err
	}
	return entry, true, nil
}

func MustRegisterSchema[T any](kind string, patch func(root map[string]any)) struct{} {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		panic("stepmeta: schema kind is required")
	}
	if patch == nil {
		panic(fmt.Sprintf("stepmeta: schema patch is required for %s", kind))
	}
	var zero T
	t := reflect.TypeOf(zero)
	if t == nil {
		panic(fmt.Sprintf("stepmeta: could not resolve schema reflect type for %s", kind))
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		panic(fmt.Sprintf("stepmeta: schema type for %s must be a struct", kind))
	}
	mu.Lock()
	defer mu.Unlock()
	if _, exists := schemaByKind[kind]; exists {
		panic(fmt.Sprintf("stepmeta: duplicate schema registration for %s", kind))
	}
	file, line := callerSource()
	schemaByKind[kind] = registeredSchema{Type: t, Patch: patch, Source: SourceRef{File: file, Line: line}}
	return struct{}{}
}

func typeNameFor[T any]() string {
	var zero T
	t := reflect.TypeOf(zero)
	if t == nil {
		return ""
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Name()
}

func cloneDefinition(def Definition) Definition {
	cloned := def
	cloned.Roles = append([]string(nil), def.Roles...)
	cloned.Outputs = append([]string(nil), def.Outputs...)
	cloned.Notes = append([]string(nil), def.Notes...)
	cloned.Ask.MatchSignals = append([]string(nil), def.Ask.MatchSignals...)
	cloned.Ask.KeyFields = append([]string(nil), def.Ask.KeyFields...)
	cloned.Ask.CommonMistakes = append([]string(nil), def.Ask.CommonMistakes...)
	cloned.Ask.RepairHints = append([]string(nil), def.Ask.RepairHints...)
	cloned.Ask.AntiSignals = append([]string(nil), def.Ask.AntiSignals...)
	cloned.Ask.ValidationHints = append([]ValidationHint(nil), def.Ask.ValidationHints...)
	cloned.Ask.QualityRules = append([]QualityRule(nil), def.Ask.QualityRules...)
	if len(def.Ask.ConstrainedLiteralFields) > 0 {
		cloned.Ask.ConstrainedLiteralFields = make([]ConstrainedLiteralField, len(def.Ask.ConstrainedLiteralFields))
		copy(cloned.Ask.ConstrainedLiteralFields, def.Ask.ConstrainedLiteralFields)
		for i := range cloned.Ask.ConstrainedLiteralFields {
			cloned.Ask.ConstrainedLiteralFields[i].AllowedValues = append([]string(nil), def.Ask.ConstrainedLiteralFields[i].AllowedValues...)
		}
	}
	return cloned
}

func mergeDefinitionDocs(def Definition, docs Docs) Docs {
	merged := docs
	if strings.TrimSpace(def.Summary) != "" {
		merged.Summary = strings.TrimSpace(def.Summary)
	}
	if strings.TrimSpace(def.WhenToUse) != "" {
		merged.WhenToUse = strings.TrimSpace(def.WhenToUse)
	}
	if strings.TrimSpace(def.Example) != "" {
		merged.Example = strings.TrimSpace(def.Example)
	}
	if len(def.Notes) > 0 {
		merged.Notes = append([]string(nil), def.Notes...)
	}
	return merged
}

func RegisterSourceFS(source fs.FS) struct{} {
	if source == nil {
		panic("stepmeta: source fs is nil")
	}
	mu.Lock()
	defer mu.Unlock()
	stepspecFS = source
	return struct{}{}
}

func callerSource() (string, int) {
	_, file, line, ok := runtime.Caller(2)
	if !ok {
		return "", 0
	}
	return file, line
}
