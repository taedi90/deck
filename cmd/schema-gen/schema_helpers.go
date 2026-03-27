package main

import (
	"encoding/json"

	"github.com/invopop/jsonschema"

	"github.com/Airgap-Castaways/deck/internal/filemode"
)

func writeFile(path string, content []byte) error {
	if err := filemode.WriteArtifactFile(path, content); err != nil {
		return err
	}
	return nil
}

func writeJSONFile(path string, value map[string]any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return writeFile(path, raw)
}

func schemaToMap(schema *jsonschema.Schema) (map[string]any, error) {
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
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
