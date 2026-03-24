package main

import "github.com/Airgap-Castaways/deck/internal/stepspec"

func patchRefreshRepositoryToolSchema(root map[string]any) {
	props := propertyMap(root)
	spec, err := reflectedSpecSchema(&stepspec.RefreshRepository{})
	if err != nil {
		panic(err)
	}
	delete(propertyMap(spec), "timeout")
	properties := propertyMap(spec)
	setMap(properties, "manager", enumStringSchema("auto", "apt", "dnf"))
	setMap(properties, "clean", map[string]any{"type": "boolean"})
	setMap(properties, "update", map[string]any{"type": "boolean"})
	setMap(properties, "restrictToRepos", stringArraySchema(0, true))
	setMap(properties, "excludeRepos", stringArraySchema(0, true))
	spec["anyOf"] = []any{
		map[string]any{"properties": map[string]any{"clean": map[string]any{"const": true}}, "required": []any{"clean"}},
		map[string]any{"properties": map[string]any{"update": map[string]any{"const": true}}, "required": []any{"update"}},
	}
	setMap(props, "spec", spec)
}

func patchDownloadPackageToolSchema(root map[string]any) {
	props := propertyMap(root)
	spec, err := reflectedSpecSchema(&stepspec.DownloadPackage{})
	if err != nil {
		panic(err)
	}
	delete(propertyMap(spec), "timeout")
	properties := propertyMap(spec)
	setMap(properties, "packages", stringArraySchema(1, false))
	distro := propertyMap(spec)["distro"].(map[string]any)
	setMap(propertyMap(distro), "family", minLenStringSchema())
	setMap(propertyMap(distro), "release", minLenStringSchema())
	repo := propertyMap(spec)["repo"].(map[string]any)
	repoProps := propertyMap(repo)
	setMap(repoProps, "type", enumStringSchema("deb-flat", "rpm"))
	setMap(repoProps, "generate", map[string]any{"type": "boolean"})
	setMap(repoProps, "pkgsDir", minLenStringSchema())
	if items, ok := repoProps["modules"].(map[string]any); ok {
		if itemMap, ok := items["items"].(map[string]any); ok {
			itemMap["required"] = []any{"name", "stream"}
			itemProps := propertyMap(itemMap)
			setMap(itemProps, "name", minLenStringSchema())
			setMap(itemProps, "stream", minLenStringSchema())
		}
	}
	backend := propertyMap(spec)["backend"].(map[string]any)
	backendProps := propertyMap(backend)
	setMap(backendProps, "mode", enumStringSchema("container"))
	setMap(backendProps, "runtime", enumStringSchema("auto", "docker", "podman"))
	setMap(backendProps, "image", minLenStringSchema())
	backend["required"] = []any{"mode", "image"}
	setMap(properties, "outputDir", minLenStringSchema())
	spec["required"] = []any{"packages"}
	setMap(props, "spec", spec)
}

func patchInstallPackageToolSchema(root map[string]any) {
	props := propertyMap(root)
	spec, err := reflectedSpecSchema(&stepspec.InstallPackage{})
	if err != nil {
		panic(err)
	}
	delete(propertyMap(spec), "timeout")
	properties := propertyMap(spec)
	if source, ok := properties["source"].(map[string]any); ok {
		source["required"] = []any{"type", "path"}
		sourceProps := propertyMap(source)
		setMap(sourceProps, "type", map[string]any{"const": "local-repo"})
		setMap(sourceProps, "path", minLenStringSchema())
	}
	setMap(properties, "packages", stringArraySchema(1, false))
	setMap(properties, "restrictToRepos", stringArraySchema(0, true))
	setMap(properties, "excludeRepos", stringArraySchema(0, true))
	spec["required"] = []any{"packages"}
	setMap(props, "spec", spec)
}

func patchConfigureRepositoryToolSchema(root map[string]any) {
	props := propertyMap(root)
	spec, err := reflectedSpecSchema(&stepspec.ConfigureRepository{})
	if err != nil {
		panic(err)
	}
	properties := propertyMap(spec)
	setMap(properties, "format", enumStringSchema("auto", "deb", "rpm"))
	setMap(properties, "path", map[string]any{"type": "string"})
	setMap(properties, "mode", modeSchema())
	setMap(properties, "replaceExisting", map[string]any{"type": "boolean"})
	setMap(properties, "disableExisting", map[string]any{"type": "boolean"})
	setMap(properties, "backupPaths", stringArraySchema(0, false))
	setMap(properties, "cleanupPaths", stringArraySchema(0, false))
	if repos, ok := properties["repositories"].(map[string]any); ok {
		repos["type"] = "array"
		repos["minItems"] = 1
		if items, ok := repos["items"].(map[string]any); ok {
			itemProps := propertyMap(items)
			setMap(itemProps, "id", minLenStringSchema())
			setMap(itemProps, "name", minLenStringSchema())
			setMap(itemProps, "baseurl", minLenStringSchema())
			setMap(itemProps, "enabled", map[string]any{"type": "boolean"})
			setMap(itemProps, "gpgcheck", map[string]any{"type": "boolean"})
			setMap(itemProps, "gpgkey", minLenStringSchema())
			setMap(itemProps, "trusted", map[string]any{"type": "boolean"})
			setMap(itemProps, "suite", minLenStringSchema())
			setMap(itemProps, "component", minLenStringSchema())
			setMap(itemProps, "type", enumStringSchema("deb", "deb-src"))
			setMap(itemProps, "extra", map[string]any{"type": "object", "additionalProperties": map[string]any{"anyOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "boolean"}, map[string]any{"type": "integer"}, map[string]any{"type": "number"}}}})
		}
	}
	spec["required"] = []any{"repositories"}
	setMap(props, "spec", spec)
}
