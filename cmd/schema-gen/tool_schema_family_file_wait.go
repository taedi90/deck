package main

func generateDownloadFileToolSchema() map[string]any {
	root := stepEnvelopeSchema("DownloadFile", "DownloadFileStep", "Downloads or stages a file into bundle output storage.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"source"},
		"properties": map[string]any{
			"source":     fileSourceSchema(),
			"fetch":      fileFetchSchema(),
			"outputPath": minLenStringSchema(),
			"mode":       modeSchema(),
		},
	})
	return root
}

func generateWriteFileToolSchema() map[string]any {
	root := stepEnvelopeSchema("WriteFile", "WriteFileStep", "Writes inline or templated content to a node path.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"path"},
		"properties": map[string]any{
			"path":     minLenStringSchema(),
			"content":  map[string]any{"type": "string"},
			"template": map[string]any{"type": "string"},
			"mode":     modeSchema(),
		},
		"oneOf": []any{
			map[string]any{"required": []any{"content"}, "not": map[string]any{"required": []any{"template"}}},
			map[string]any{"required": []any{"template"}, "not": map[string]any{"required": []any{"content"}}},
		},
	})
	return root
}

func generateCopyFileToolSchema() map[string]any {
	root := stepEnvelopeSchema("CopyFile", "CopyFileStep", "Copies a file already present on the node.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"source", "path"},
		"properties": map[string]any{
			"source": fileSourceSchema(),
			"path":   minLenStringSchema(),
			"mode":   modeSchema(),
		},
	})
	return root
}

func generateExtractArchiveToolSchema() map[string]any {
	root := stepEnvelopeSchema("ExtractArchive", "ExtractArchiveStep", "Extracts an archive from a declared source into a destination directory.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"source", "path"},
		"properties": map[string]any{
			"source":  fileSourceSchema(),
			"path":    minLenStringSchema(),
			"include": stringArraySchema(0, false),
			"mode":    modeSchema(),
		},
	})
	return root
}

func generateEditFileToolSchema() map[string]any {
	root := stepEnvelopeSchema("EditFile", "EditFileStep", "Edits an existing file in place.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"path", "edits"},
		"properties": map[string]any{
			"path":   minLenStringSchema(),
			"backup": map[string]any{"type": "boolean"},
			"edits":  fileEditsSchema(),
			"mode":   modeSchema(),
		},
	})
	return root
}

func fileSourceSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"url":    minLenStringSchema(),
			"path":   minLenStringSchema(),
			"sha256": sha256Schema(),
			"bundle": bundleRefSchema(),
		},
		"anyOf": []any{
			map[string]any{"required": []any{"url"}},
			map[string]any{"required": []any{"path"}},
			map[string]any{"required": []any{"bundle"}},
		},
	}
}

func fileFetchSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"offlineOnly": map[string]any{"type": "boolean"},
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
	}
}

func fileEditsSchema() map[string]any {
	return map[string]any{
		"type":     "array",
		"minItems": 1,
		"items": map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []any{"match"},
			"properties": map[string]any{
				"match":       map[string]any{"type": "string"},
				"replaceWith": map[string]any{"type": "string"},
				"op":          enumStringSchema("replace", "append"),
			},
		},
	}
}

func generateWaitForServiceToolSchema() map[string]any {
	return generateWaitToolSchema("WaitForService", "WaitForServiceStep", "Waits until a systemd service reports active.", []string{"name"}, map[string]any{
		"interval":     durationStringSchema(),
		"initialDelay": durationStringSchema(),
		"name":         minLenStringSchema(),
		"timeout":      durationStringSchema(),
		"pollInterval": durationStringSchema(),
	})
}

func generateWaitForCommandToolSchema() map[string]any {
	return generateWaitToolSchema("WaitForCommand", "WaitForCommandStep", "Waits until a command exits successfully.", []string{"command"}, map[string]any{
		"interval":     durationStringSchema(),
		"initialDelay": durationStringSchema(),
		"command":      stringArraySchema(1, false),
		"timeout":      durationStringSchema(),
		"pollInterval": durationStringSchema(),
	})
}

func generateWaitForFileToolSchema() map[string]any {
	return generateWaitToolSchema("WaitForFile", "WaitForFileStep", "Waits until a file or directory exists.", []string{"path"}, map[string]any{
		"interval":     durationStringSchema(),
		"initialDelay": durationStringSchema(),
		"path":         minLenStringSchema(),
		"type":         enumStringSchema("any", "file", "dir"),
		"nonEmpty":     map[string]any{"type": "boolean"},
		"timeout":      durationStringSchema(),
		"pollInterval": durationStringSchema(),
	})
}

func generateWaitForMissingFileToolSchema() map[string]any {
	return generateWaitToolSchema("WaitForMissingFile", "WaitForMissingFileStep", "Waits until a file or directory is absent.", []string{"path"}, map[string]any{
		"interval":     durationStringSchema(),
		"initialDelay": durationStringSchema(),
		"path":         minLenStringSchema(),
		"type":         enumStringSchema("any", "file", "dir"),
		"timeout":      durationStringSchema(),
		"pollInterval": durationStringSchema(),
	})
}

func generateWaitForTCPPortToolSchema() map[string]any {
	return generateWaitToolSchema("WaitForTCPPort", "WaitForTCPPortStep", "Waits until a TCP port becomes reachable.", []string{"port"}, map[string]any{
		"interval":     durationStringSchema(),
		"initialDelay": durationStringSchema(),
		"address":      minLenStringSchema(),
		"port":         minLenStringSchema(),
		"timeout":      durationStringSchema(),
		"pollInterval": durationStringSchema(),
	})
}

func generateWaitForMissingTCPPortToolSchema() map[string]any {
	return generateWaitToolSchema("WaitForMissingTCPPort", "WaitForMissingTCPPortStep", "Waits until a TCP port becomes unreachable.", []string{"port"}, map[string]any{
		"interval":     durationStringSchema(),
		"initialDelay": durationStringSchema(),
		"address":      minLenStringSchema(),
		"port":         minLenStringSchema(),
		"timeout":      durationStringSchema(),
		"pollInterval": durationStringSchema(),
	})
}

func generateWaitToolSchema(kind, title, description string, required []string, properties map[string]any) map[string]any {
	root := stepEnvelopeSchema(kind, title, description, "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             toAnySlice(required),
		"properties":           properties,
	})
	return root
}
