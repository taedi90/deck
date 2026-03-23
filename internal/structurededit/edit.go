package structurededit

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"

	"github.com/taedi90/deck/internal/stepspec"
)

type Format string

const (
	FormatTOML Format = "toml"
	FormatYAML Format = "yaml"
	FormatJSON Format = "json"
)

func Apply(format Format, raw []byte, edits []stepspec.StructuredEdit) ([]byte, error) {
	doc, err := parseDocument(format, raw)
	if err != nil {
		return nil, err
	}
	for idx, edit := range edits {
		path, err := ParsePath(strings.TrimSpace(edit.RawPath))
		if err != nil {
			return nil, fmt.Errorf("edits[%d]: %w", idx, err)
		}
		doc, err = applyEdit(doc, path, edit)
		if err != nil {
			return nil, fmt.Errorf("edits[%d]: %w", idx, err)
		}
	}
	return encodeDocument(format, doc)
}

func parseDocument(format Format, raw []byte) (any, error) {
	if strings.TrimSpace(string(raw)) == "" {
		return map[string]any{}, nil
	}
	switch format {
	case FormatTOML:
		doc := map[string]any{}
		if err := toml.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("parse TOML: %w", err)
		}
		return doc, nil
	case FormatYAML:
		var doc any
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("parse YAML: %w", err)
		}
		return normalizeYAML(doc), nil
	case FormatJSON:
		var doc any
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.UseNumber()
		if err := dec.Decode(&doc); err != nil {
			return nil, fmt.Errorf("parse JSON: %w", err)
		}
		return normalizeJSON(doc), nil
	default:
		return nil, fmt.Errorf("unsupported structured edit format %q", format)
	}
}

func encodeDocument(format Format, doc any) ([]byte, error) {
	switch format {
	case FormatTOML:
		root, ok := doc.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("TOML root must be an object")
		}
		raw, err := toml.Marshal(root)
		if err != nil {
			return nil, fmt.Errorf("marshal TOML: %w", err)
		}
		return raw, nil
	case FormatYAML:
		raw, err := yaml.Marshal(doc)
		if err != nil {
			return nil, fmt.Errorf("marshal YAML: %w", err)
		}
		return raw, nil
	case FormatJSON:
		raw, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal JSON: %w", err)
		}
		return append(raw, '\n'), nil
	default:
		return nil, fmt.Errorf("unsupported structured edit format %q", format)
	}
}

type pathSegment struct {
	key     string
	index   int
	isIndex bool
}

func ParsePath(rawPath string) ([]pathSegment, error) {
	if rawPath == "" {
		return nil, fmt.Errorf("rawPath is required")
	}
	parts := make([]string, 0)
	var current strings.Builder
	inQuotes := false
	escaped := false
	for i := 0; i < len(rawPath); i++ {
		ch := rawPath[i]
		switch {
		case escaped:
			current.WriteByte(ch)
			escaped = false
		case ch == '\\' && inQuotes:
			escaped = true
		case ch == '"':
			inQuotes = !inQuotes
		case ch == '.' && !inQuotes:
			segment := strings.TrimSpace(current.String())
			if segment == "" {
				return nil, fmt.Errorf("invalid rawPath %q", rawPath)
			}
			parts = append(parts, segment)
			current.Reset()
		default:
			current.WriteByte(ch)
		}
	}
	if inQuotes || escaped {
		return nil, fmt.Errorf("invalid rawPath %q", rawPath)
	}
	last := strings.TrimSpace(current.String())
	if last == "" {
		return nil, fmt.Errorf("invalid rawPath %q", rawPath)
	}
	parts = append(parts, last)
	segments := make([]pathSegment, 0, len(parts))
	for _, part := range parts {
		if idx, err := strconv.Atoi(part); err == nil {
			if idx < 0 {
				return nil, fmt.Errorf("invalid negative index in rawPath %q", rawPath)
			}
			segments = append(segments, pathSegment{index: idx, isIndex: true})
			continue
		}
		segments = append(segments, pathSegment{key: part})
	}
	return segments, nil
}

func applyEdit(root any, path []pathSegment, edit stepspec.StructuredEdit) (any, error) {
	if len(path) == 0 {
		return nil, fmt.Errorf("rawPath is required")
	}
	op := strings.TrimSpace(edit.Op)
	if op == "" {
		return nil, fmt.Errorf("op is required")
	}
	switch op {
	case "set":
		value, err := normalizeValue(edit.Value)
		if err != nil {
			return nil, err
		}
		return setAtPath(root, path, value)
	case "delete":
		return deleteAtPath(root, path)
	case "appendUnique":
		items, err := normalizeStringList(edit.Value, true)
		if err != nil {
			return nil, err
		}
		return appendUniqueAtPath(root, path, items)
	case "replaceList":
		items, err := normalizeStringList(edit.Value, false)
		if err != nil {
			return nil, err
		}
		return setAtPath(root, path, toAnySlice(items))
	default:
		return nil, fmt.Errorf("unsupported op %q", op)
	}
}

func normalizeValue(raw any) (any, error) {
	switch value := raw.(type) {
	case nil:
		return nil, nil
	case string:
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("set requires a non-empty string, number, boolean, object, list, or null value")
		}
		return value, nil
	case bool:
		return value, nil
	case int:
		return int64(value), nil
	case int32:
		return int64(value), nil
	case int64:
		return value, nil
	case float64:
		if math.Trunc(value) == value {
			return int64(value), nil
		}
		return value, nil
	case json.Number:
		if i, err := value.Int64(); err == nil {
			return i, nil
		}
		f, err := value.Float64()
		if err != nil {
			return nil, fmt.Errorf("invalid number value: %w", err)
		}
		return f, nil
	case []any:
		out := make([]any, 0, len(value))
		for _, item := range value {
			norm, err := normalizeValue(item)
			if err != nil {
				return nil, err
			}
			out = append(out, norm)
		}
		return out, nil
	case []string:
		return toAnySlice(value), nil
	case map[string]any:
		out := map[string]any{}
		for k, item := range value {
			norm, err := normalizeValue(item)
			if err != nil {
				return nil, err
			}
			out[k] = norm
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported value type %T", raw)
	}
}

func normalizeStringList(raw any, singleAllowed bool) ([]string, error) {
	switch value := raw.(type) {
	case string:
		if !singleAllowed || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("operation requires a non-empty string list value")
		}
		return []string{value}, nil
	case []string:
		return cleanStringList(value)
	case []any:
		items := make([]string, 0, len(value))
		for _, item := range value {
			str, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("operation requires a string list value")
			}
			items = append(items, str)
		}
		return cleanStringList(items)
	default:
		return nil, fmt.Errorf("operation requires a string list value")
	}
}

func cleanStringList(items []string) ([]string, error) {
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			return nil, fmt.Errorf("string list values must not be empty")
		}
		result = append(result, item)
	}
	return result, nil
}

func setAtPath(root any, path []pathSegment, value any) (any, error) {
	updated, err := setNode(root, path, value)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func deleteAtPath(root any, path []pathSegment) (any, error) {
	updated, found, err := deleteNode(root, path)
	if err != nil {
		return nil, err
	}
	if !found {
		return root, nil
	}
	return updated, nil
}

func appendUniqueAtPath(root any, path []pathSegment, items []string) (any, error) {
	existing, found, err := getAtPath(root, path)
	if err != nil {
		return nil, err
	}
	if !found || existing == nil {
		return setAtPath(root, path, toAnySlice(items))
	}
	current, err := readStringList(existing)
	if err != nil {
		return nil, fmt.Errorf("existing value at rawPath is not a string list")
	}
	seen := map[string]struct{}{}
	merged := make([]string, 0, len(current)+len(items))
	for _, item := range current {
		merged = append(merged, item)
		seen[item] = struct{}{}
	}
	for _, item := range items {
		if _, ok := seen[item]; ok {
			continue
		}
		merged = append(merged, item)
		seen[item] = struct{}{}
	}
	return setAtPath(root, path, toAnySlice(merged))
}

func setNode(current any, path []pathSegment, value any) (any, error) {
	segment := path[0]
	if segment.isIndex {
		holder, ok := current.([]any)
		if !ok {
			return nil, fmt.Errorf("path segment must be an array index")
		}
		if segment.index < 0 || segment.index > len(holder) {
			return nil, fmt.Errorf("array index %d out of range", segment.index)
		}
		if segment.index == len(holder) {
			seed := any(nil)
			if len(path) > 1 {
				seed = newContainer(path[1])
			}
			holder = append(holder, seed)
		}
		if len(path) == 1 {
			holder[segment.index] = value
			return holder, nil
		}
		child := holder[segment.index]
		if child == nil {
			child = newContainer(path[1])
		}
		updatedChild, err := setNode(child, path[1:], value)
		if err != nil {
			return nil, err
		}
		holder[segment.index] = updatedChild
		return holder, nil
	}

	holder, ok := current.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("path conflict at segment %q", segment.key)
	}
	if len(path) == 1 {
		holder[segment.key] = value
		return holder, nil
	}
	child, ok := holder[segment.key]
	if !ok || child == nil {
		child = newContainer(path[1])
	}
	updatedChild, err := setNode(child, path[1:], value)
	if err != nil {
		return nil, err
	}
	holder[segment.key] = updatedChild
	return holder, nil
}

func getAtPath(current any, path []pathSegment) (any, bool, error) {
	segment := path[0]
	if segment.isIndex {
		holder, ok := current.([]any)
		if !ok {
			return nil, false, fmt.Errorf("path segment must be an array index")
		}
		if segment.index < 0 || segment.index >= len(holder) {
			return nil, false, nil
		}
		if len(path) == 1 {
			return holder[segment.index], true, nil
		}
		if holder[segment.index] == nil {
			return nil, false, nil
		}
		return getAtPath(holder[segment.index], path[1:])
	}
	holder, ok := current.(map[string]any)
	if !ok {
		return nil, false, fmt.Errorf("path conflict at segment %q", segment.key)
	}
	child, ok := holder[segment.key]
	if !ok {
		return nil, false, nil
	}
	if len(path) == 1 {
		return child, true, nil
	}
	if child == nil {
		return nil, false, nil
	}
	return getAtPath(child, path[1:])
}

func deleteNode(current any, path []pathSegment) (any, bool, error) {
	segment := path[0]
	if segment.isIndex {
		holder, ok := current.([]any)
		if !ok {
			return nil, false, fmt.Errorf("path segment must be an array index")
		}
		if segment.index < 0 || segment.index >= len(holder) {
			return current, false, nil
		}
		if len(path) == 1 {
			holder[segment.index] = nil
			return holder, true, nil
		}
		if holder[segment.index] == nil {
			return current, false, nil
		}
		updatedChild, found, err := deleteNode(holder[segment.index], path[1:])
		if err != nil || !found {
			return current, found, err
		}
		holder[segment.index] = updatedChild
		return holder, true, nil
	}
	holder, ok := current.(map[string]any)
	if !ok {
		return nil, false, fmt.Errorf("path conflict at segment %q", segment.key)
	}
	child, ok := holder[segment.key]
	if !ok {
		return current, false, nil
	}
	if len(path) == 1 {
		delete(holder, segment.key)
		return holder, true, nil
	}
	if child == nil {
		return current, false, nil
	}
	updatedChild, found, err := deleteNode(child, path[1:])
	if err != nil || !found {
		return current, found, err
	}
	holder[segment.key] = updatedChild
	return holder, true, nil
}

func newContainer(next pathSegment) any {
	if next.isIndex {
		return []any{}
	}
	return map[string]any{}
}

func readStringList(raw any) ([]string, error) {
	switch value := raw.(type) {
	case []string:
		return value, nil
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			str, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("non-string list item")
			}
			out = append(out, str)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("got %T", raw)
	}
}

func toAnySlice(items []string) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	return out
}

func normalizeYAML(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, v := range typed {
			out[k] = normalizeYAML(v)
		}
		return out
	case map[any]any:
		out := map[string]any{}
		for k, v := range typed {
			out[fmt.Sprint(k)] = normalizeYAML(v)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, normalizeYAML(item))
		}
		return out
	default:
		return typed
	}
}

func normalizeJSON(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, v := range typed {
			out[k] = normalizeJSON(v)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, normalizeJSON(item))
		}
		return out
	case json.Number:
		if i, err := typed.Int64(); err == nil {
			return i
		}
		if f, err := typed.Float64(); err == nil {
			return f
		}
		return typed.String()
	default:
		return typed
	}
}
