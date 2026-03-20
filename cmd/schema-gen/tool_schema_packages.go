package main

func generateRefreshRepositoryToolSchema() map[string]any {
	root := stepEnvelopeSchema("RefreshRepository", "RefreshRepositoryStep", "Refreshes package metadata and can restrict or exclude repos during refresh.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"manager":         enumStringSchema("auto", "apt", "dnf"),
			"clean":           map[string]any{"type": "boolean"},
			"update":          map[string]any{"type": "boolean"},
			"restrictToRepos": stringArraySchema(0, true),
			"excludeRepos":    stringArraySchema(0, true),
		},
		"anyOf": []any{
			map[string]any{"properties": map[string]any{"clean": map[string]any{"const": true}}, "required": []any{"clean"}},
			map[string]any{"properties": map[string]any{"update": map[string]any{"const": true}}, "required": []any{"update"}},
		},
	})
	return root
}

func generateDownloadPackageToolSchema() map[string]any {
	root := stepEnvelopeSchema("DownloadPackage", "DownloadPackageStep", "Downloads packages into bundle output storage.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"packages"},
		"properties": map[string]any{
			"packages": stringArraySchema(1, false),
			"distro":   map[string]any{"type": "object", "additionalProperties": true},
			"repo": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []any{"type"},
				"properties": map[string]any{
					"type":     enumStringSchema("apt-flat", "yum"),
					"generate": map[string]any{"type": "boolean"},
					"pkgsDir":  minLenStringSchema(),
					"modules": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type":                 "object",
							"additionalProperties": false,
							"required":             []any{"name", "stream"},
							"properties": map[string]any{
								"name":   minLenStringSchema(),
								"stream": minLenStringSchema(),
							},
						},
					},
				},
			},
			"backend": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []any{"mode", "image"},
				"properties": map[string]any{
					"mode":    enumStringSchema("container"),
					"runtime": enumStringSchema("auto", "docker", "podman"),
					"image":   minLenStringSchema(),
				},
			},
			"outputDir": minLenStringSchema(),
		},
	})
	return root
}

func generateInstallPackageToolSchema() map[string]any {
	root := stepEnvelopeSchema("InstallPackage", "InstallPackageStep", "Installs packages on the local node.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"packages"},
		"properties": map[string]any{
			"source": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []any{"type", "path"},
				"properties":           map[string]any{"type": map[string]any{"const": "local-repo"}, "path": map[string]any{"type": "string"}},
			},
			"packages":        stringArraySchema(1, false),
			"restrictToRepos": stringArraySchema(0, true),
			"excludeRepos":    stringArraySchema(0, true),
		},
	})
	return root
}

func generateConfigureRepositoryToolSchema() map[string]any {
	root := stepEnvelopeSchema("ConfigureRepository", "ConfigureRepositoryStep", "Configures repository definitions on the local node.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"repositories"},
		"properties": map[string]any{
			"format":          enumStringSchema("auto", "apt", "yum"),
			"path":            map[string]any{"type": "string"},
			"mode":            modeSchema(),
			"replaceExisting": map[string]any{"type": "boolean"},
			"disableExisting": map[string]any{"type": "boolean"},
			"backupPaths":     stringArraySchema(0, false),
			"cleanupPaths":    stringArraySchema(0, false),
			"repositories": map[string]any{
				"type":     "array",
				"minItems": 1,
				"items":    map[string]any{"type": "object", "additionalProperties": true},
			},
		},
	})
	return root
}
