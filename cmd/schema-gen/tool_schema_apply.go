package main

func generateArtifactsToolSchema() map[string]any {
	root := stepEnvelopeSchema("Artifacts", "ArtifactsStep", "Installs or extracts per-architecture artifacts during apply.", "public")
	props := propertyMap(root)
	setMap(props, "spec", artifactsToolSpecSchema())
	root["$defs"] = map[string]any{"artifactSource": artifactSourceSchema()}
	return root
}

func generateCommandToolSchema() map[string]any {
	root := stepEnvelopeSchema("Command", "CommandStep", "Escape hatch for commands that are not yet covered by typed steps.", "advanced")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"command"},
		"properties": map[string]any{
			"command": stringArraySchema(1, false),
			"env":     map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
			"sudo":    map[string]any{"type": "boolean", "default": false},
			"timeout": durationStringSchema(),
		},
	})
	return root
}

func generateContainerdToolSchema() map[string]any {
	root := stepEnvelopeSchema("Containerd", "ContainerdStep", "Configures containerd defaults and registry host settings during apply.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"path":          minLenStringSchema(),
			"configPath":    minLenStringSchema(),
			"systemdCgroup": map[string]any{"type": "boolean"},
			"createDefault": map[string]any{"type": "boolean", "default": true},
			"registryHosts": map[string]any{"type": "array", "items": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []any{"registry", "server", "host", "capabilities", "skipVerify"},
				"properties": map[string]any{
					"registry":     minLenStringSchema(),
					"server":       minLenStringSchema(),
					"host":         minLenStringSchema(),
					"capabilities": stringArraySchema(1, true),
					"skipVerify":   map[string]any{"type": "boolean"},
				},
			}},
		},
	})
	return root
}

func generateDirectoryToolSchema() map[string]any {
	root := stepEnvelopeSchema("Directory", "DirectoryStep", "Ensures a directory exists on the local node.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"path"},
		"properties":           map[string]any{"path": minLenStringSchema(), "mode": modeSchema()},
	})
	return root
}

func generateImageToolSchema() map[string]any {
	root := stepEnvelopeSchema("Image", "ImageStep", "Checks image-related state through action-specific modes.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"images"},
		"properties": map[string]any{
			"action":  enumStringSchema("download", "verify"),
			"runtime": map[string]any{"type": "object", "additionalProperties": true},
			"command": stringArraySchema(1, false),
			"images":  stringArraySchema(1, false),
			"auth": map[string]any{
				"type":     "array",
				"minItems": 1,
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"required":             []any{"registry", "basic"},
					"properties": map[string]any{
						"registry": minLenStringSchema(),
						"basic": map[string]any{
							"type":                 "object",
							"additionalProperties": false,
							"required":             []any{"username", "password"},
							"properties": map[string]any{
								"username": map[string]any{"type": "string"},
								"password": map[string]any{"type": "string"},
							},
						},
					},
				},
			},
			"backend": map[string]any{"type": "object", "additionalProperties": true},
			"output":  map[string]any{"type": "object", "additionalProperties": true},
		},
		"allOf": []any{
			conditionalRequired("download", []string{"images"}, nil),
			conditionalRequired("verify", []string{"images"}, nil),
			map[string]any{
				"if": map[string]any{
					"properties": map[string]any{
						"action": map[string]any{"const": "verify"},
					},
					"required": []any{"action"},
				},
				"then": map[string]any{
					"not": map[string]any{
						"required": []any{"auth"},
					},
				},
			},
		},
	})
	return root
}

func generateChecksToolSchema() map[string]any {
	root := stepEnvelopeSchema("Checks", "ChecksStep", "Runs host checks before prepare execution.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"checks"},
		"properties": map[string]any{
			"checks":   map[string]any{"type": "array", "minItems": 1, "items": map[string]any{"type": "string", "enum": []any{"os", "arch", "kernelModules", "swap", "binaries"}}},
			"binaries": stringArraySchema(0, false),
			"failFast": map[string]any{"type": "boolean", "default": true},
		},
	})
	return root
}

func generateKernelModuleToolSchema() map[string]any {
	root := stepEnvelopeSchema("KernelModule", "KernelModuleStep", "Loads and persists required kernel modules on the local node.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"oneOf": []any{
			map[string]any{"required": []any{"name"}, "not": map[string]any{"required": []any{"names"}}},
			map[string]any{"required": []any{"names"}, "not": map[string]any{"required": []any{"name"}}},
		},
		"properties": map[string]any{
			"name":        minLenStringSchema(),
			"names":       stringArraySchema(1, true),
			"load":        map[string]any{"type": "boolean", "default": true},
			"persist":     map[string]any{"type": "boolean", "default": true},
			"persistFile": map[string]any{"type": "string"},
		},
	})
	return root
}

func generateKubeadmToolSchema() map[string]any {
	root := stepEnvelopeSchema("Kubeadm", "KubeadmStep", "Runs kubeadm operations through action-specific modes.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"action"},
		"properties": map[string]any{
			"action":                enumStringSchema("init", "join", "reset"),
			"mode":                  enumStringSchema("stub", "real"),
			"configFile":            map[string]any{"type": "string"},
			"configTemplate":        map[string]any{"type": "string"},
			"pullImages":            map[string]any{"type": "boolean"},
			"outputJoinFile":        map[string]any{"type": "string"},
			"kubernetesVersion":     map[string]any{"type": "string"},
			"advertiseAddress":      map[string]any{"type": "string"},
			"podNetworkCIDR":        map[string]any{"type": "string"},
			"criSocket":             map[string]any{"type": "string"},
			"ignorePreflightErrors": stringArraySchema(0, false),
			"extraArgs":             stringArraySchema(0, false),
			"skipIfAdminConfExists": map[string]any{"type": "boolean", "default": true},
			"joinFile":              map[string]any{"type": "string"},
			"asControlPlane":        map[string]any{"type": "boolean", "default": false},
			"force":                 map[string]any{"type": "boolean", "default": false},
			"ignoreErrors":          map[string]any{"type": "boolean", "default": false},
			"stopKubelet":           map[string]any{"type": "boolean", "default": true},
			"removePaths":           stringArraySchema(0, false),
			"removeFiles":           stringArraySchema(0, false),
			"cleanupContainers":     stringArraySchema(0, false),
			"restartRuntimeService": map[string]any{"type": "string"},
		},
		"allOf": []any{
			conditionalRequired("init", []string{"outputJoinFile"}, nil),
			conditionalRequired("join", []string{"joinFile"}, nil),
			conditionalRequired("reset", nil, nil),
		},
	})
	return root
}
