package main

func generatePackageCacheToolSchema() map[string]any {
	root := stepEnvelopeSchema("PackageCache", "PackageCacheStep", "Refreshes package metadata and can restrict or exclude repos during refresh.", "public")
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

func generatePackagesToolSchema() map[string]any {
	root := stepEnvelopeSchema("Packages", "PackagesStep", "Installs packages on the local node.", "public")
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
			"action":          enumStringSchema("download", "install"),
			"distro":          map[string]any{"type": "object", "additionalProperties": true},
			"repo":            map[string]any{"type": "object", "additionalProperties": true},
			"backend":         map[string]any{"type": "object", "additionalProperties": true},
		},
		"allOf": []any{
			conditionalRequired("download", []string{"packages"}, nil),
			conditionalRequired("install", []string{"packages"}, nil),
		},
	})
	return root
}

func generateRepositoryToolSchema() map[string]any {
	root := stepEnvelopeSchema("Repository", "RepositoryStep", "Configures repository definitions on the local node.", "public")
	props := propertyMap(root)
	setMap(props, "spec", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"action"},
		"properties": map[string]any{
			"action":          enumStringSchema("configure"),
			"format":          enumStringSchema("apt", "yum"),
			"path":            map[string]any{"type": "string"},
			"mode":            modeSchema(),
			"replaceExisting": map[string]any{"type": "boolean"},
			"disableExisting": map[string]any{"type": "boolean"},
			"backupPaths":     stringArraySchema(0, false),
			"cleanupPaths":    stringArraySchema(0, false),
			"refreshCache":    map[string]any{"type": "object", "additionalProperties": true},
			"repositories": map[string]any{
				"type":     "array",
				"minItems": 1,
				"items":    map[string]any{"type": "object", "additionalProperties": true},
			},
		},
		"allOf": []any{map[string]any{
			"if":   map[string]any{"properties": map[string]any{"action": map[string]any{"const": "configure"}}},
			"then": map[string]any{"required": []any{"repositories"}},
		}},
	})
	return root
}
