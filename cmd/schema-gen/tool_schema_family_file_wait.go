package main

func generateFileDownloadToolSchema() map[string]any {
	root := stepEnvelopeSchema("FileDownload", "FileDownloadStep", "Downloads or stages a file into bundle output storage.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"source"},
		"properties": map[string]any{
			"source": fileSourceSchema(),
			"fetch":  fileFetchSchema(),
			"output": fileOutputSchema(),
		},
	})
	return root
}

func generateFileWriteToolSchema() map[string]any {
	root := stepEnvelopeSchema("FileWrite", "FileWriteStep", "Writes inline or templated content to a node path.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"path"},
		"properties": map[string]any{
			"path":                minLenStringSchema(),
			"content":             map[string]any{"type": "string"},
			"contentFromTemplate": map[string]any{"type": "string"},
			"mode":                modeSchema(),
		},
		"oneOf": []any{
			map[string]any{"required": []any{"content"}, "not": map[string]any{"required": []any{"contentFromTemplate"}}},
			map[string]any{"required": []any{"contentFromTemplate"}, "not": map[string]any{"required": []any{"content"}}},
		},
	})
	return root
}

func generateFileCopyToolSchema() map[string]any {
	root := stepEnvelopeSchema("FileCopy", "FileCopyStep", "Copies a file already present on the node.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"src", "dest"},
		"properties": map[string]any{
			"src":  minLenStringSchema(),
			"dest": minLenStringSchema(),
			"mode": modeSchema(),
		},
	})
	return root
}

func generateFileEditToolSchema() map[string]any {
	root := stepEnvelopeSchema("FileEdit", "FileEditStep", "Edits an existing file in place.", "public")
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

func fileOutputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"path":  minLenStringSchema(),
			"chmod": modeSchema(),
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
				"match": map[string]any{"type": "string"},
				"with":  map[string]any{"type": "string"},
				"op":    enumStringSchema("replace", "append"),
			},
		},
	}
}

func generateWaitServiceActiveToolSchema() map[string]any {
	return generateWaitToolSchema("WaitServiceActive", "WaitServiceActiveStep", "Waits until a systemd service reports active.", []string{"name"}, map[string]any{
		"interval":     durationStringSchema(),
		"initialDelay": durationStringSchema(),
		"name":         minLenStringSchema(),
		"timeout":      durationStringSchema(),
		"pollInterval": durationStringSchema(),
	})
}

func generateWaitCommandSuccessToolSchema() map[string]any {
	return generateWaitToolSchema("WaitCommandSuccess", "WaitCommandSuccessStep", "Waits until a command exits successfully.", []string{"command"}, map[string]any{
		"interval":     durationStringSchema(),
		"initialDelay": durationStringSchema(),
		"command":      stringArraySchema(1, false),
		"timeout":      durationStringSchema(),
		"pollInterval": durationStringSchema(),
	})
}

func generateWaitFileExistsToolSchema() map[string]any {
	return generateWaitToolSchema("WaitFileExists", "WaitFileExistsStep", "Waits until a file or directory exists.", []string{"path"}, map[string]any{
		"interval":     durationStringSchema(),
		"initialDelay": durationStringSchema(),
		"path":         minLenStringSchema(),
		"type":         enumStringSchema("any", "file", "dir"),
		"nonEmpty":     map[string]any{"type": "boolean"},
		"timeout":      durationStringSchema(),
		"pollInterval": durationStringSchema(),
	})
}

func generateWaitFileAbsentToolSchema() map[string]any {
	return generateWaitToolSchema("WaitFileAbsent", "WaitFileAbsentStep", "Waits until a file or directory is absent.", []string{"path"}, map[string]any{
		"interval":     durationStringSchema(),
		"initialDelay": durationStringSchema(),
		"path":         minLenStringSchema(),
		"type":         enumStringSchema("any", "file", "dir"),
		"timeout":      durationStringSchema(),
		"pollInterval": durationStringSchema(),
	})
}

func generateWaitTCPPortOpenToolSchema() map[string]any {
	return generateWaitToolSchema("WaitTCPPortOpen", "WaitTCPPortOpenStep", "Waits until a TCP port becomes reachable.", []string{"port"}, map[string]any{
		"interval":     durationStringSchema(),
		"initialDelay": durationStringSchema(),
		"address":      minLenStringSchema(),
		"port":         minLenStringSchema(),
		"timeout":      durationStringSchema(),
		"pollInterval": durationStringSchema(),
	})
}

func generateWaitTCPPortClosedToolSchema() map[string]any {
	return generateWaitToolSchema("WaitTCPPortClosed", "WaitTCPPortClosedStep", "Waits until a TCP port becomes unreachable.", []string{"port"}, map[string]any{
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
