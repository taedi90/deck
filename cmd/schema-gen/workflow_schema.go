package main

import (
	"strings"

	jsonschema "github.com/invopop/jsonschema"

	"github.com/taedi90/deck/internal/schemamodel"
	"github.com/taedi90/deck/internal/workflowexec"
)

func generateWorkflowSchema() (map[string]any, error) {
	reflector := jsonschema.Reflector{DoNotReference: true, ExpandedStruct: true}
	root, err := schemaToMap(reflector.Reflect(&schemamodel.WorkflowDocument{}))
	if err != nil {
		return nil, err
	}
	root["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	root["$id"] = "https://deck.local/schemas/deck-workflow.schema.json"
	root["title"] = "DeckWorkflow"
	root["description"] = "Top-level workflow schema for deck workflows."
	root["type"] = "object"
	root["additionalProperties"] = false
	root["required"] = []any{"version"}
	root["not"] = map[string]any{"allOf": []any{
		map[string]any{"required": []any{"phases"}},
		map[string]any{"required": []any{"steps"}},
	}}
	root["anyOf"] = []any{
		map[string]any{"required": []any{"phases"}},
		map[string]any{"required": []any{"steps"}},
	}

	phases, err := phaseSchema()
	if err != nil {
		return nil, err
	}

	props := propertyMap(root)
	setMap(props, "version", map[string]any{"type": "string", "const": "v1alpha1"})
	mergeMap(props, "vars", map[string]any{"type": "object", "additionalProperties": true, "default": map[string]any{}})
	setMap(props, "steps", map[string]any{"type": "array", "minItems": 1, "items": stepBaseSchema()})
	setMap(props, "phases", map[string]any{"type": "array", "minItems": 1, "items": phases})

	return root, nil
}

func generateComponentFragmentSchema() map[string]any {
	root := map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"$id":                  "https://deck.local/schemas/deck-component-fragment.schema.json",
		"title":                "DeckComponentFragment",
		"description":          "Workflow component fragment schema for reusable apply step bundles.",
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"steps"},
		"properties": map[string]any{
			"steps": map[string]any{"type": "array", "minItems": 1, "items": stepBaseSchema()},
		},
	}
	return root
}

func generateToolDefinitionSchema() (map[string]any, error) {
	reflector := jsonschema.Reflector{DoNotReference: true, ExpandedStruct: true}
	root, err := schemaToMap(reflector.Reflect(&schemamodel.ToolDefinitionDocument{}))
	if err != nil {
		return nil, err
	}
	root["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	root["$id"] = "https://deck.local/schemas/deck-tooldefinition.schema.json"
	root["title"] = "DeckToolDefinition"
	root["description"] = "Tool definition manifest schema for describing typed workflow tools."
	root["type"] = "object"
	root["additionalProperties"] = false
	root["required"] = []any{"apiVersion", "kind", "metadata", "spec"}

	props := propertyMap(root)
	setMap(props, "apiVersion", map[string]any{"type": "string", "const": "deck/v1"})
	setMap(props, "kind", map[string]any{"type": "string", "const": "ToolDefinition"})
	patchToolDefinitionMetadata(props["metadata"])
	patchToolDefinitionSpec(props["spec"])

	return root, nil
}

func phaseSchema() (map[string]any, error) {
	reflector := jsonschema.Reflector{DoNotReference: true, ExpandedStruct: true}
	phase, err := schemaToMap(reflector.Reflect(&schemamodel.WorkflowPhase{}))
	if err != nil {
		return nil, err
	}
	phase["type"] = "object"
	phase["additionalProperties"] = false
	phase["required"] = []any{"name"}
	phase["anyOf"] = []any{
		map[string]any{"required": []any{"steps"}},
		map[string]any{"required": []any{"imports"}},
	}
	props := propertyMap(phase)
	setMap(props, "name", map[string]any{"type": "string", "minLength": 1})
	setMap(props, "imports", map[string]any{"type": "array", "minItems": 1, "items": workflowImportSchema()})
	setMap(props, "steps", map[string]any{"type": "array", "items": stepBaseSchema()})
	return phase, nil
}

func workflowImportSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"path"},
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "minLength": 1},
			"when": map[string]any{"type": "string", "minLength": 1},
		},
	}
}

func stepBaseSchema() map[string]any {
	root := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"id", "kind", "spec"},
		"properties": map[string]any{
			"id":         map[string]any{"type": "string", "pattern": "^[a-z0-9][a-z0-9-]{1,127}$"},
			"apiVersion": map[string]any{"type": "string", "const": "deck/v1alpha1"},
			"kind":       map[string]any{"type": "string", "enum": toAnySlice(workflowexec.StepKinds())},
			"metadata":   map[string]any{"type": "object", "additionalProperties": true},
			"when":       map[string]any{"type": "string", "minLength": 1},
			"register": map[string]any{
				"type":                 "object",
				"propertyNames":        map[string]any{"pattern": "^[A-Za-z_][A-Za-z0-9_]*$"},
				"additionalProperties": map[string]any{"type": "string"},
			},
			"retry":   map[string]any{"type": "integer", "minimum": 0},
			"timeout": map[string]any{"type": "string", "pattern": "^[0-9]+(ms|s|m|h)$"},
			"spec":    map[string]any{"type": "object"},
		},
	}
	root["allOf"] = registerContractClauses()
	return root
}

func registerContractClauses() []any {
	defs := workflowexec.StepDefinitions()
	clauses := make([]any, 0, len(defs))
	for _, def := range defs {
		clauses = append(clauses, map[string]any{
			"if": map[string]any{
				"properties": map[string]any{"kind": map[string]any{"const": def.Kind}},
				"required":   []any{"kind"},
			},
			"then": map[string]any{
				"properties": map[string]any{"register": registerValueSchema(def.Outputs)},
			},
		})
	}
	return clauses
}

func registerValueSchema(outputs []string) map[string]any {
	root := map[string]any{
		"type":          "object",
		"propertyNames": map[string]any{"pattern": "^[A-Za-z_][A-Za-z0-9_]*$"},
	}
	if len(outputs) == 0 {
		root["maxProperties"] = 0
		root["additionalProperties"] = false
		return root
	}
	root["additionalProperties"] = map[string]any{"enum": toAnySlice(outputs)}
	return root
}

func stepEnvelopeSchema(kind, title, description, visibility string) map[string]any {
	root := map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"$id":                  "https://deck.local/schemas/tools/" + schemaFileName(kind),
		"title":                title,
		"description":          description,
		"x-deck-visibility":    visibility,
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"id", "kind", "spec"},
		"properties": map[string]any{
			"id":         map[string]any{"type": "string"},
			"apiVersion": map[string]any{"const": "deck/v1alpha1"},
			"kind":       map[string]any{"const": kind},
			"metadata":   map[string]any{"type": "object", "additionalProperties": true},
			"when":       map[string]any{"type": "string"},
			"retry":      map[string]any{"type": "integer", "minimum": 0},
			"timeout":    durationStringSchema(),
			"register": map[string]any{
				"type":                 "object",
				"propertyNames":        map[string]any{"pattern": "^[A-Za-z_][A-Za-z0-9_]*$"},
				"additionalProperties": map[string]any{"type": "string"},
			},
		},
	}
	return root
}

func bundleRefSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"root", "path"},
		"properties": map[string]any{
			"root": enumStringSchema("files", "images", "packages"),
			"path": minLenStringSchema(),
		},
	}
}

func enumStringSchema(values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": toAnySlice(values)}
}

func minLenStringSchema() map[string]any {
	return map[string]any{"type": "string", "minLength": 1}
}

func durationStringSchema() map[string]any {
	return map[string]any{"type": "string", "pattern": "^[0-9]+(ms|s|m|h)$"}
}

func modeSchema() map[string]any {
	return map[string]any{"type": "string", "pattern": "^[0-7]{4}$"}
}

func sha256Schema() map[string]any {
	return map[string]any{"type": "string", "pattern": "^[a-fA-F0-9]{64}$"}
}

func stringArraySchema(minItems int, minLen bool) map[string]any {
	item := map[string]any{"type": "string"}
	if minLen {
		item["minLength"] = 1
	}
	field := map[string]any{"type": "array", "items": item}
	if minItems > 0 {
		field["minItems"] = minItems
	}
	return field
}

func schemaFileName(kind string) string {
	if def, ok := workflowexec.StepDefinitionForKind(kind); ok {
		return def.SchemaFile
	}
	return strings.ToLower(kind) + ".schema.json"
}
