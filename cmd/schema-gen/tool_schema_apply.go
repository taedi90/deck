package main

func generateCommandToolSchema() map[string]any {
	root := stepEnvelopeSchema("Command", "CommandStep", "Escape hatch for commands that are not yet covered by typed steps.", "public")
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

func generateWriteContainerdConfigToolSchema() map[string]any {
	root := stepEnvelopeSchema("WriteContainerdConfig", "WriteContainerdConfigStep", "Writes the containerd config.toml file on the node.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"path":          minLenStringSchema(),
			"configPath":    minLenStringSchema(),
			"systemdCgroup": map[string]any{"type": "boolean"},
			"createDefault": map[string]any{"type": "boolean", "default": true},
		},
	})
	return root
}

func generateWriteContainerdRegistryHostsToolSchema() map[string]any {
	root := stepEnvelopeSchema("WriteContainerdRegistryHosts", "WriteContainerdRegistryHostsStep", "Writes containerd registry host configuration for mirrors and trust policy.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"path", "registryHosts"},
		"properties": map[string]any{
			"path": minLenStringSchema(),
			"registryHosts": map[string]any{"type": "array", "minItems": 1, "items": map[string]any{
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

func generateEnsureDirectoryToolSchema() map[string]any {
	root := stepEnvelopeSchema("EnsureDirectory", "EnsureDirectoryStep", "Ensures a directory exists on the local node.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"path"},
		"properties":           map[string]any{"path": minLenStringSchema(), "mode": modeSchema()},
	})
	return root
}

func generateDownloadImageToolSchema() map[string]any {
	root := stepEnvelopeSchema("DownloadImage", "DownloadImageStep", "Downloads images into bundle output storage.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"images"},
		"properties": map[string]any{
			"images":    stringArraySchema(1, false),
			"auth":      imageAuthSchema(),
			"backend":   imageBackendSchema(),
			"outputDir": minLenStringSchema(),
		},
	})
	return root
}

func generateImageLoadToolSchema() map[string]any {
	root := stepEnvelopeSchema("LoadImage", "LoadImageStep", "Loads prepared image archives into the local container runtime.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"images"},
		"properties": map[string]any{
			"images":    stringArraySchema(1, false),
			"sourceDir": minLenStringSchema(),
			"runtime":   enumStringSchema("auto", "ctr", "docker", "podman"),
			"command":   stringArraySchema(1, false),
		},
	})
	return root
}

func generateVerifyImageToolSchema() map[string]any {
	root := stepEnvelopeSchema("VerifyImage", "VerifyImageStep", "Verifies that required images already exist on the node.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"images"},
		"properties": map[string]any{
			"images":  stringArraySchema(1, false),
			"command": stringArraySchema(1, false),
		},
	})
	return root
}

func imageAuthSchema() map[string]any {
	return map[string]any{
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
	}
}

func imageBackendSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"engine": enumStringSchema("go-containerregistry"),
		},
	}
}

func generateCheckHostToolSchema() map[string]any {
	root := stepEnvelopeSchema("CheckHost", "CheckHostStep", "Runs host checks before prepare execution.", "public")
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

func generateInitKubeadmToolSchema() map[string]any {
	root := stepEnvelopeSchema("InitKubeadm", "InitKubeadmStep", "Runs kubeadm init and writes a join command file.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"outputJoinFile"},
		"properties": map[string]any{
			"configFile":            map[string]any{"type": "string"},
			"configTemplate":        map[string]any{"type": "string"},
			"outputJoinFile":        map[string]any{"type": "string"},
			"kubernetesVersion":     map[string]any{"type": "string"},
			"advertiseAddress":      map[string]any{"type": "string"},
			"podNetworkCIDR":        map[string]any{"type": "string"},
			"criSocket":             map[string]any{"type": "string"},
			"ignorePreflightErrors": stringArraySchema(0, false),
			"extraArgs":             stringArraySchema(0, false),
			"skipIfAdminConfExists": map[string]any{"type": "boolean", "default": true},
		},
	})
	return root
}

func generateJoinKubeadmToolSchema() map[string]any {
	root := stepEnvelopeSchema("JoinKubeadm", "JoinKubeadmStep", "Runs kubeadm join.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"oneOf": []any{
			map[string]any{"required": []any{"joinFile"}},
			map[string]any{"required": []any{"configFile"}},
		},
		"properties": map[string]any{
			"configFile":     map[string]any{"type": "string"},
			"joinFile":       map[string]any{"type": "string"},
			"asControlPlane": map[string]any{"type": "boolean", "default": false},
			"extraArgs":      stringArraySchema(0, false),
		},
	})
	return root
}

func generateResetKubeadmToolSchema() map[string]any {
	root := stepEnvelopeSchema("ResetKubeadm", "ResetKubeadmStep", "Runs kubeadm reset and optional cleanup steps.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"force":                 map[string]any{"type": "boolean", "default": false},
			"ignoreErrors":          map[string]any{"type": "boolean", "default": false},
			"stopKubelet":           map[string]any{"type": "boolean", "default": true},
			"criSocket":             map[string]any{"type": "string"},
			"extraArgs":             stringArraySchema(0, false),
			"removePaths":           stringArraySchema(0, false),
			"removeFiles":           stringArraySchema(0, false),
			"cleanupContainers":     stringArraySchema(0, false),
			"restartRuntimeService": map[string]any{"type": "string"},
		},
	})
	return root
}
