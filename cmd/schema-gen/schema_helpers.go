package main

import (
	"encoding/json"
	"fmt"
	"os"

	jsonschema "github.com/invopop/jsonschema"
)

func writeFile(path string, content []byte) {
	if err := os.WriteFile(path, content, 0o644); err != nil {
		fail(err)
	}
}

func writeJSONFile(path string, value map[string]any) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fail(err)
	}
	raw = append(raw, '\n')
	writeFile(path, raw)
}

func schemaToMap(schema *jsonschema.Schema) map[string]any {
	raw, err := json.Marshal(schema)
	if err != nil {
		fail(err)
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		fail(err)
	}
	return out
}

func propertyMap(node map[string]any) map[string]any {
	props, _ := node["properties"].(map[string]any)
	if props == nil {
		props = map[string]any{}
		node["properties"] = props
	}
	return props
}

func setMap(root map[string]any, key string, value map[string]any) {
	root[key] = value
}

func mergeMap(root map[string]any, key string, extra map[string]any) {
	current, _ := root[key].(map[string]any)
	if current == nil {
		current = map[string]any{}
	}
	for k, v := range extra {
		current[k] = v
	}
	root[key] = current
}

func toAnySlice(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
