package main

func generateServiceToolSchema() map[string]any {
	root := stepEnvelopeSchema("Service", "ServiceStep", "Starts, stops, enables, or disables local services.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"oneOf": []any{
			map[string]any{"required": []any{"name"}, "not": map[string]any{"required": []any{"names"}}},
			map[string]any{"required": []any{"names"}, "not": map[string]any{"required": []any{"name"}}},
		},
		"properties": map[string]any{
			"name":          minLenStringSchema(),
			"names":         stringArraySchema(1, true),
			"daemonReload":  map[string]any{"type": "boolean"},
			"ifExists":      map[string]any{"type": "boolean"},
			"ignoreMissing": map[string]any{"type": "boolean"},
			"enabled":       map[string]any{"type": "boolean"},
			"state":         enumStringSchema("unchanged", "started", "stopped", "restarted", "reloaded"),
		},
	})
	return root
}

func generateSwapToolSchema() map[string]any {
	root := stepEnvelopeSchema("Swap", "SwapStep", "Enables or disables swap and its persistence settings.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"disable":   map[string]any{"type": "boolean", "default": true},
			"persist":   map[string]any{"type": "boolean", "default": true},
			"fstabPath": map[string]any{"type": "string"},
		},
	})
	return root
}

func generateSymlinkToolSchema() map[string]any {
	root := stepEnvelopeSchema("Symlink", "SymlinkStep", "Creates or replaces a symbolic link on the local node.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"path", "target"},
		"properties": map[string]any{
			"path":          minLenStringSchema(),
			"target":        minLenStringSchema(),
			"force":         map[string]any{"type": "boolean", "default": false},
			"createParent":  map[string]any{"type": "boolean", "default": false},
			"requireTarget": map[string]any{"type": "boolean", "default": false},
		},
	})
	return root
}

func generateSysctlToolSchema() map[string]any {
	root := stepEnvelopeSchema("Sysctl", "SysctlStep", "Writes and optionally applies sysctl values on the local node.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"values", "writeFile"},
		"properties": map[string]any{
			"values": map[string]any{
				"type":          "object",
				"minProperties": 1,
				"additionalProperties": map[string]any{"anyOf": []any{
					map[string]any{"type": "string"},
					map[string]any{"type": "number"},
					map[string]any{"type": "integer"},
					map[string]any{"type": "boolean"},
				}},
			},
			"writeFile": map[string]any{"type": "string"},
			"apply":     map[string]any{"type": "boolean", "default": false},
		},
	})
	return root
}

func generateSystemdUnitToolSchema() map[string]any {
	root := stepEnvelopeSchema("SystemdUnit", "SystemdUnitStep", "Writes a systemd unit file and optionally manages the related service.", "public")
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
			"daemonReload":        map[string]any{"type": "boolean"},
			"service": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"name":    minLenStringSchema(),
					"enabled": map[string]any{"type": "boolean"},
					"state":   enumStringSchema("unchanged", "started", "stopped", "restarted", "reloaded"),
				},
			},
		},
		"oneOf": []any{
			map[string]any{"required": []any{"content"}},
			map[string]any{"required": []any{"contentFromTemplate"}},
		},
	})
	return root
}
