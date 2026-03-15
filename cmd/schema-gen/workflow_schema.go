package main

import (
	"strings"

	jsonschema "github.com/invopop/jsonschema"

	"github.com/taedi90/deck/internal/schemamodel"
	"github.com/taedi90/deck/internal/workflowexec"
)

func generateWorkflowSchema() map[string]any {
	reflector := jsonschema.Reflector{DoNotReference: true, ExpandedStruct: true}
	root := schemaToMap(reflector.Reflect(&schemamodel.WorkflowDocument{}))
	root["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	root["$id"] = "https://deck.local/schemas/deck-workflow.schema.json"
	root["title"] = "DeckWorkflow"
	root["description"] = "Top-level workflow schema for deck workflows."
	root["type"] = "object"
	root["additionalProperties"] = false
	root["required"] = []any{"role", "version"}
	root["not"] = map[string]any{"allOf": []any{
		map[string]any{"required": []any{"phases"}},
		map[string]any{"required": []any{"steps"}},
	}}
	root["anyOf"] = []any{
		map[string]any{"required": []any{"artifacts"}},
		map[string]any{"required": []any{"phases"}},
		map[string]any{"required": []any{"steps"}},
		map[string]any{"required": []any{"imports"}},
	}

	props := propertyMap(root)
	setMap(props, "role", map[string]any{"type": "string", "enum": []any{"prepare", "apply"}})
	setMap(props, "version", map[string]any{"type": "string", "const": "v1alpha1"})
	mergeMap(props, "vars", map[string]any{"type": "object", "additionalProperties": true, "default": map[string]any{}})
	mergeMap(props, "varImports", map[string]any{"type": "array", "minItems": 1, "uniqueItems": true, "items": map[string]any{"type": "string", "minLength": 1}})
	mergeMap(props, "imports", map[string]any{"type": "array", "minItems": 1, "uniqueItems": true, "items": map[string]any{"type": "string", "minLength": 1}})
	mergeMap(props, "artifacts", map[string]any{"description": "Declarative prepare artifact inventory. Prefer this over legacy prepare download steps in new role: prepare workflows."})
	setMap(props, "steps", map[string]any{"type": "array", "minItems": 1, "items": stepBaseSchema()})
	setMap(props, "phases", map[string]any{"type": "array", "minItems": 1, "items": phaseSchema()})
	patchArtifactsSchema(props["artifacts"])

	return root
}

func generateToolDefinitionSchema() map[string]any {
	reflector := jsonschema.Reflector{DoNotReference: true, ExpandedStruct: true}
	root := schemaToMap(reflector.Reflect(&schemamodel.ToolDefinitionDocument{}))
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

	return root
}

func phaseSchema() map[string]any {
	reflector := jsonschema.Reflector{DoNotReference: true, ExpandedStruct: true}
	phase := schemaToMap(reflector.Reflect(&schemamodel.WorkflowPhase{}))
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
	return phase
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
	return map[string]any{
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
}

func patchArtifactsSchema(node any) {
	artifacts, ok := node.(map[string]any)
	if !ok {
		return
	}
	artifacts["type"] = "object"
	artifacts["additionalProperties"] = false
	artifacts["properties"] = map[string]any{
		"files":    map[string]any{"type": "array", "items": artifactFileGroupSchema()},
		"images":   map[string]any{"type": "array", "items": artifactImageGroupSchema()},
		"packages": map[string]any{"type": "array", "items": artifactPackageGroupSchema()},
	}
}

func artifactTargetSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"os":       map[string]any{"type": "string", "minLength": 1},
			"osFamily": map[string]any{"type": "string", "minLength": 1},
			"release":  map[string]any{"type": "string", "minLength": 1},
			"arch":     map[string]any{"type": "string", "minLength": 1},
		},
	}
}

func artifactFileGroupSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"group", "items"},
		"properties": map[string]any{
			"group":   map[string]any{"type": "string", "minLength": 1},
			"targets": map[string]any{"type": "array", "items": artifactTargetSchema()},
			"items":   map[string]any{"type": "array", "minItems": 1, "items": artifactFileItemSchema()},
		},
	}
}

func artifactFileItemSchema() map[string]any {
	return map[string]any{
		"description":          "One file artifact to place under the bundle files/ root.",
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"id", "source", "output"},
		"properties": map[string]any{
			"id": map[string]any{"type": "string", "minLength": 1},
			"source": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"url":    map[string]any{"type": "string", "minLength": 1},
					"path":   map[string]any{"type": "string", "minLength": 1},
					"sha256": map[string]any{"type": "string", "minLength": 1},
				},
				"oneOf": []any{
					map[string]any{"required": []any{"url"}},
					map[string]any{"required": []any{"path"}},
				},
			},
			"output": map[string]any{
				"description":          "Bundle file output. path is relative to the files/ root and must not start with files/.",
				"type":                 "object",
				"additionalProperties": false,
				"required":             []any{"path"},
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "minLength": 1},
					"mode": map[string]any{"type": "string", "pattern": "^[0-7]{4}$"},
				},
			},
			"checksum": map[string]any{"type": "string", "minLength": 1},
			"mode":     map[string]any{"type": "string", "pattern": "^[0-7]{4}$"},
		},
	}
}

func artifactImageGroupSchema() map[string]any {
	return map[string]any{
		"description":          "A grouped set of images to collect during prepare.",
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"group", "items"},
		"properties": map[string]any{
			"group":   map[string]any{"type": "string", "minLength": 1},
			"targets": map[string]any{"type": "array", "items": artifactTargetSchema()},
			"backend": map[string]any{"type": "object", "additionalProperties": true},
			"output":  map[string]any{"type": "object", "additionalProperties": true},
			"items": map[string]any{"type": "array", "minItems": 1, "items": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []any{"image"},
				"properties":           map[string]any{"image": map[string]any{"type": "string", "minLength": 1}},
			}},
		},
	}
}

func artifactPackageGroupSchema() map[string]any {
	return map[string]any{
		"description":          "A grouped set of packages for one or more OS targets to collect during prepare.",
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"group", "targets", "items"},
		"properties": map[string]any{
			"group":   map[string]any{"type": "string", "minLength": 1},
			"targets": map[string]any{"type": "array", "minItems": 1, "items": artifactTargetSchema()},
			"repo":    map[string]any{"type": "object", "additionalProperties": true},
			"backend": map[string]any{"type": "object", "additionalProperties": true},
			"items": map[string]any{"type": "array", "minItems": 1, "items": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []any{"name"},
				"properties":           map[string]any{"name": map[string]any{"type": "string", "minLength": 1}},
			}},
		},
	}
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
		"required":             []any{"id", "apiVersion", "kind", "spec"},
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

func artifactsToolSpecSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"artifacts"},
		"properties": map[string]any{
			"fetch": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"offlineOnly": map[string]any{"type": "boolean", "default": false},
					"strategy":    enumStringSchema("fallback"),
					"sources": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type":                 "object",
							"additionalProperties": false,
							"required":             []any{"type"},
							"anyOf": []any{
								map[string]any{"required": []any{"path"}},
								map[string]any{"required": []any{"url"}},
							},
							"properties": map[string]any{
								"type": enumStringSchema("local", "bundle", "repo", "online"),
								"path": minLenStringSchema(),
								"url":  minLenStringSchema(),
							},
						},
					},
				},
			},
			"artifacts": map[string]any{
				"type":     "array",
				"minItems": 1,
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"required":             []any{"source"},
					"properties": map[string]any{
						"source": map[string]any{
							"type":                 "object",
							"additionalProperties": false,
							"required":             []any{"amd64", "arm64"},
							"properties":           map[string]any{"amd64": map[string]any{"$ref": "#/$defs/artifactSource"}, "arm64": map[string]any{"$ref": "#/$defs/artifactSource"}},
						},
						"skipIfPresent": map[string]any{
							"type":                 "object",
							"additionalProperties": false,
							"required":             []any{"path"},
							"properties":           map[string]any{"path": minLenStringSchema(), "executable": map[string]any{"type": "boolean", "default": false}},
						},
						"install": map[string]any{
							"type":                 "object",
							"additionalProperties": false,
							"required":             []any{"path"},
							"properties":           map[string]any{"path": minLenStringSchema(), "mode": modeSchema()},
						},
						"extract": map[string]any{
							"type":                 "object",
							"additionalProperties": false,
							"required":             []any{"destination"},
							"properties":           map[string]any{"destination": minLenStringSchema(), "include": stringArraySchema(1, true), "mode": modeSchema()},
						},
					},
					"oneOf": []any{
						map[string]any{"required": []any{"install"}},
						map[string]any{"required": []any{"extract"}},
					},
				},
			},
		},
	}
}

func artifactSourceSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"anyOf": []any{
			map[string]any{"required": []any{"path"}},
			map[string]any{"required": []any{"url"}},
			map[string]any{"required": []any{"bundle"}},
		},
		"properties": map[string]any{
			"path":   minLenStringSchema(),
			"url":    minLenStringSchema(),
			"sha256": sha256Schema(),
			"bundle": bundleRefSchema(),
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
	for _, pair := range []struct{ kind, file string }{
		{"Inspection", "inspection.schema.json"},
		{"Artifacts", "artifacts.schema.json"},
		{"Packages", "packages.schema.json"},
		{"Directory", "directory.schema.json"},
		{"Symlink", "symlink.schema.json"},
		{"SystemdUnit", "systemd-unit.schema.json"},
		{"Containerd", "containerd.schema.json"},
		{"Repository", "repository.schema.json"},
		{"PackageCache", "package-cache.schema.json"},
		{"Swap", "swap.schema.json"},
		{"KernelModule", "kernel-module.schema.json"},
		{"Service", "service.schema.json"},
		{"Sysctl", "sysctl.schema.json"},
		{"File", "file.schema.json"},
		{"Image", "image.schema.json"},
		{"Wait", "wait.schema.json"},
		{"Kubeadm", "kubeadm.schema.json"},
		{"Command", "command.schema.json"},
	} {
		if pair.kind == kind {
			return pair.file
		}
	}
	return strings.ToLower(kind) + ".schema.json"
}
