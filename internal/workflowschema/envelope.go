package workflowschema

import (
	"strings"

	"github.com/Airgap-Castaways/deck/internal/workflowcontract"
	"github.com/Airgap-Castaways/deck/internal/workflowexec"
)

func stepEnvelopeSchema(kind, title, description, visibility string) map[string]any {
	apiVersion := workflowcontract.BuiltInStepAPIVersion
	if def, ok := workflowexec.BuiltInTypeDefinitionForKey(workflowexec.StepTypeKey{APIVersion: workflowcontract.BuiltInStepAPIVersion, Kind: kind}); ok && def.Step.APIVersion != "" {
		apiVersion = def.Step.APIVersion
	}
	return map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"$id":                  "https://deck.local/schemas/tools/" + schemaFileName(kind),
		"title":                title,
		"description":          description,
		"x-deck-visibility":    visibility,
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"id", "kind", "spec"},
		"properties": map[string]any{
			"id":            map[string]any{"type": "string"},
			"apiVersion":    map[string]any{"type": "string", "const": apiVersion},
			"kind":          map[string]any{"const": kind},
			"metadata":      map[string]any{"type": "object", "additionalProperties": true},
			"when":          map[string]any{"type": "string"},
			"parallelGroup": map[string]any{"type": "string", "minLength": 1},
			"retry":         map[string]any{"type": "integer", "minimum": 0},
			"timeout":       durationStringSchema(),
			"register": map[string]any{
				"type":                 "object",
				"propertyNames":        map[string]any{"pattern": "^[A-Za-z_][A-Za-z0-9_]*$"},
				"additionalProperties": map[string]any{"type": "string"},
			},
		},
	}
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
	if def, ok := workflowexec.BuiltInTypeDefinitionForKey(workflowexec.StepTypeKey{APIVersion: workflowcontract.BuiltInStepAPIVersion, Kind: kind}); ok {
		return def.Step.SchemaFile
	}
	return strings.ToLower(kind) + ".schema.json"
}
