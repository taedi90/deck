package main

func patchToolDefinitionMetadata(node any) {
	metadata, ok := node.(map[string]any)
	if !ok {
		return
	}
	metadata["type"] = "object"
	metadata["additionalProperties"] = false
	metadata["required"] = []any{"name"}
	metadata["properties"] = map[string]any{
		"name":        map[string]any{"type": "string", "pattern": "^[A-Z][A-Za-z0-9]+$"},
		"labels":      map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
		"description": map[string]any{"type": "string"},
	}
}

func patchToolDefinitionSpec(node any) {
	spec, ok := node.(map[string]any)
	if !ok {
		return
	}
	spec["type"] = "object"
	spec["additionalProperties"] = false
	spec["required"] = []any{"version", "summary", "category", "inputSchema"}
	spec["properties"] = map[string]any{
		"version":        map[string]any{"type": "string", "pattern": "^v[0-9]+$"},
		"summary":        map[string]any{"type": "string", "minLength": 1},
		"category":       map[string]any{"type": "string", "enum": []any{"prepare", "install", "shared"}},
		"offlineAllowed": map[string]any{"type": "boolean", "default": false},
		"requires": map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"network":          map[string]any{"type": "boolean"},
				"root":             map[string]any{"type": "boolean"},
				"containerRuntime": map[string]any{"type": "string", "enum": []any{"required", "optional", "none"}},
			},
		},
		"inputSchema": map[string]any{"type": "object"},
		"outputContract": map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"artifacts": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"stateKeys": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
		},
		"idempotency": map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []any{"mode", "keys"},
			"properties": map[string]any{
				"mode": map[string]any{"type": "string", "enum": []any{"by-output", "by-state", "always-run"}},
				"keys": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
		},
		"failurePolicy": map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"retryable":    map[string]any{"type": "boolean"},
				"defaultRetry": map[string]any{"type": "integer", "minimum": 0},
			},
		},
	}
}
