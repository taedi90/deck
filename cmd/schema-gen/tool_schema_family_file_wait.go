package main

import (
	jsonschema "github.com/invopop/jsonschema"

	"github.com/taedi90/deck/internal/schemamodel"
)

func generateFileToolSchema() map[string]any {
	reflector := jsonschema.Reflector{DoNotReference: true, ExpandedStruct: true}
	root := schemaToMap(reflector.Reflect(&schemamodel.FileStepDocument{}))
	root["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	root["$id"] = "https://deck.local/schemas/tools/file.schema.json"
	root["title"] = "FileStep"
	root["description"] = "Manages file operations through action-specific modes."
	root["x-deck-visibility"] = "public"
	root["type"] = "object"
	root["additionalProperties"] = false
	root["required"] = []any{"id", "apiVersion", "kind", "spec"}

	props := propertyMap(root)
	setMap(props, "id", map[string]any{"type": "string"})
	setMap(props, "apiVersion", map[string]any{"const": "deck/v1alpha1"})
	setMap(props, "kind", map[string]any{"const": "File"})
	mergeMap(props, "metadata", map[string]any{"type": "object", "additionalProperties": true})
	mergeMap(props, "when", map[string]any{"type": "string"})
	mergeMap(props, "retry", map[string]any{"type": "integer", "minimum": 0})
	mergeMap(props, "timeout", map[string]any{"type": "string", "pattern": "^[0-9]+(ms|s|m|h)$"})
	setMap(props, "register", map[string]any{
		"type":                 "object",
		"propertyNames":        map[string]any{"pattern": "^[A-Za-z_][A-Za-z0-9_]*$"},
		"additionalProperties": map[string]any{"type": "string"},
	})
	patchFileSpec(props["spec"])

	return root
}

func generateWaitToolSchema() map[string]any {
	reflector := jsonschema.Reflector{DoNotReference: true, ExpandedStruct: true}
	root := schemaToMap(reflector.Reflect(&schemamodel.WaitStepDocument{}))
	root["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	root["$id"] = "https://deck.local/schemas/tools/wait.schema.json"
	root["title"] = "WaitStep"
	root["description"] = "Waits for healthcheck-style conditions through action-specific polling."
	root["x-deck-visibility"] = "public"
	root["type"] = "object"
	root["additionalProperties"] = false
	root["required"] = []any{"id", "apiVersion", "kind", "spec"}

	props := propertyMap(root)
	setMap(props, "id", map[string]any{"type": "string"})
	setMap(props, "apiVersion", map[string]any{"const": "deck/v1alpha1"})
	setMap(props, "kind", map[string]any{"const": "Wait"})
	mergeMap(props, "metadata", map[string]any{"type": "object", "additionalProperties": true})
	mergeMap(props, "when", map[string]any{"type": "string"})
	mergeMap(props, "retry", map[string]any{"type": "integer", "minimum": 0})
	mergeMap(props, "timeout", map[string]any{"type": "string", "pattern": "^[0-9]+(ms|s|m|h)$"})
	setMap(props, "register", map[string]any{
		"type":                 "object",
		"propertyNames":        map[string]any{"pattern": "^[A-Za-z_][A-Za-z0-9_]*$"},
		"additionalProperties": map[string]any{"type": "string"},
	})
	patchWaitSpec(props["spec"])

	return root
}

func patchFileSpec(node any) {
	spec, ok := node.(map[string]any)
	if !ok {
		return
	}
	spec["type"] = "object"
	spec["additionalProperties"] = false
	spec["required"] = []any{}
	props := propertyMap(spec)
	setMap(props, "action", map[string]any{"type": "string", "enum": []any{"download", "install", "copy", "edit"}})
	mergeMap(props, "mode", map[string]any{"type": "string", "pattern": "^[0-7]{4}$"})
	mergeMap(props, "owner", map[string]any{"type": "string", "minLength": 1})
	mergeMap(props, "group", map[string]any{"type": "string", "minLength": 1})
	patchFileEditRules(props["edits"])
	patchFileSource(props["source"])
	patchFileOutput(props["output"])
	spec["allOf"] = []any{
		conditionalRequired("download", []string{"source", "output"}, nil),
		conditionalRequired("install", []string{"path"}, []any{
			map[string]any{"required": []any{"content"}},
			map[string]any{"required": []any{"contentFromTemplate"}},
		}),
		conditionalRequired("copy", []string{"src", "dest"}, nil),
		conditionalRequired("edit", []string{"path", "edits"}, nil),
	}
}

func patchWaitSpec(node any) {
	spec, ok := node.(map[string]any)
	if !ok {
		return
	}
	spec["type"] = "object"
	spec["additionalProperties"] = false
	spec["required"] = []any{"action"}
	props := propertyMap(spec)
	setMap(props, "action", map[string]any{"type": "string", "enum": []any{"serviceActive", "commandSuccess", "fileExists", "fileAbsent", "tcpPortClosed", "tcpPortOpen"}})
	mergeMap(props, "interval", map[string]any{"type": "string", "pattern": "^[0-9]+(ms|s|m|h)$"})
	mergeMap(props, "initialDelay", map[string]any{"type": "string", "pattern": "^[0-9]+(ms|s|m|h)$"})
	mergeMap(props, "timeout", map[string]any{"type": "string", "pattern": "^[0-9]+(ms|s|m|h)$"})
	mergeMap(props, "pollInterval", map[string]any{"type": "string", "pattern": "^[0-9]+(ms|s|m|h)$"})
	setMap(props, "type", map[string]any{"type": "string", "enum": []any{"any", "file", "dir"}})
	setMap(props, "command", map[string]any{"type": "array", "minItems": 1, "items": map[string]any{"type": "string"}})
	spec["allOf"] = []any{
		conditionalEnumRequired([]string{"serviceActive"}, []string{"name"}),
		conditionalEnumRequired([]string{"commandSuccess"}, []string{"command"}),
		conditionalEnumRequired([]string{"fileExists", "fileAbsent"}, []string{"path"}),
		conditionalEnumRequired([]string{"tcpPortClosed", "tcpPortOpen"}, []string{"port"}),
	}
}

func patchFileEditRules(node any) {
	edits, ok := node.(map[string]any)
	if !ok {
		return
	}
	edits["type"] = "array"
	edits["minItems"] = 1
	edits["items"] = map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"match"},
		"properties": map[string]any{
			"match": map[string]any{"type": "string"},
			"with":  map[string]any{"type": "string"},
			"op":    map[string]any{"type": "string"},
		},
	}
}

func patchFileSource(node any) {
	source, ok := node.(map[string]any)
	if !ok {
		return
	}
	source["type"] = "object"
	source["additionalProperties"] = false
	mergeMap(source, "properties", map[string]any{})
	props := propertyMap(source)
	mergeMap(props, "url", map[string]any{"type": "string", "minLength": 1})
	mergeMap(props, "path", map[string]any{"type": "string", "minLength": 1})
	setMap(props, "sha256", map[string]any{"type": "string", "pattern": "^[a-fA-F0-9]{64}$"})
	setMap(props, "bundle", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"root", "path"},
		"properties": map[string]any{
			"root": map[string]any{"type": "string", "enum": []any{"files", "images", "packages"}},
			"path": map[string]any{"type": "string", "minLength": 1},
		},
	})
	source["anyOf"] = []any{
		map[string]any{"required": []any{"url"}},
		map[string]any{"required": []any{"path"}},
		map[string]any{"required": []any{"bundle"}},
	}
}

func patchFileOutput(node any) {
	output, ok := node.(map[string]any)
	if !ok {
		return
	}
	output["type"] = "object"
	output["additionalProperties"] = false
	output["required"] = []any{"path"}
	output["properties"] = map[string]any{
		"path":  map[string]any{"type": "string", "minLength": 1},
		"chmod": map[string]any{"type": "string", "pattern": "^[0-7]{4}$"},
	}
}

func conditionalRequired(action string, required []string, extra any) map[string]any {
	then := map[string]any{"required": toAnySlice(required)}
	if extra != nil {
		then["anyOf"] = extra
	}
	return map[string]any{
		"if": map[string]any{
			"properties": map[string]any{
				"action": map[string]any{"const": action},
			},
			"required": []any{"action"},
		},
		"then": then,
	}
}

func conditionalEnumRequired(actions []string, required []string) map[string]any {
	return map[string]any{
		"if": map[string]any{
			"properties": map[string]any{
				"action": map[string]any{"enum": toAnySlice(actions)},
			},
			"required": []any{"action"},
		},
		"then": map[string]any{"required": toAnySlice(required)},
	}
}
