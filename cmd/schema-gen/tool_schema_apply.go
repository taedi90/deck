package main

import "github.com/taedi90/deck/internal/stepspec"

func patchCommandToolSchema(root map[string]any) {
	props := propertyMap(root)
	spec, err := reflectedSpecSchema(&stepspec.Command{})
	if err != nil {
		panic(err)
	}
	properties := propertyMap(spec)
	setMap(properties, "command", stringArraySchema(1, false))
	setMap(properties, "env", map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}})
	setMap(properties, "sudo", map[string]any{"type": "boolean", "default": false})
	setMap(properties, "timeout", durationStringSchema())
	spec["required"] = []any{"command"}
	setMap(props, "spec", spec)
}

func patchWriteContainerdConfigToolSchema(root map[string]any) {
	props := propertyMap(root)
	spec, err := reflectedSpecSchema(&stepspec.WriteContainerdConfig{})
	if err != nil {
		panic(err)
	}
	delete(propertyMap(spec), "timeout")
	properties := propertyMap(spec)
	setMap(properties, "path", minLenStringSchema())
	setMap(properties, "configPath", minLenStringSchema())
	setMap(properties, "systemdCgroup", map[string]any{"type": "boolean"})
	setMap(properties, "createDefault", map[string]any{"type": "boolean", "default": true})
	setMap(props, "spec", spec)
}

func patchWriteContainerdRegistryHostsToolSchema(root map[string]any) {
	props := propertyMap(root)
	spec, err := reflectedSpecSchema(&stepspec.WriteContainerdRegistryHosts{})
	if err != nil {
		panic(err)
	}
	properties := propertyMap(spec)
	setMap(properties, "path", minLenStringSchema())
	if registryHosts, ok := properties["registryHosts"].(map[string]any); ok {
		registryHosts["minItems"] = 1
		if items, ok := registryHosts["items"].(map[string]any); ok {
			items["required"] = []any{"registry", "server", "host", "capabilities", "skipVerify"}
			itemProps := propertyMap(items)
			setMap(itemProps, "registry", minLenStringSchema())
			setMap(itemProps, "server", minLenStringSchema())
			setMap(itemProps, "host", minLenStringSchema())
			setMap(itemProps, "capabilities", stringArraySchema(1, true))
			setMap(itemProps, "skipVerify", map[string]any{"type": "boolean"})
		}
	}
	spec["required"] = []any{"path", "registryHosts"}
	setMap(props, "spec", spec)
}

func patchEnsureDirectoryToolSchema(root map[string]any) {
	props := propertyMap(root)
	spec, err := reflectedSpecSchema(&stepspec.EnsureDirectory{})
	if err != nil {
		panic(err)
	}
	properties := propertyMap(spec)
	setMap(properties, "path", minLenStringSchema())
	setMap(properties, "mode", modeSchema())
	spec["required"] = []any{"path"}
	setMap(props, "spec", spec)
}

func patchDownloadImageToolSchema(root map[string]any) {
	props := propertyMap(root)
	spec, err := reflectedSpecSchema(&stepspec.DownloadImage{})
	if err != nil {
		panic(err)
	}
	delete(propertyMap(spec), "timeout")
	spec["required"] = []any{"images"}
	properties := propertyMap(spec)
	setMap(properties, "images", stringArraySchema(1, false))
	setMap(properties, "auth", imageAuthSchema())
	setMap(properties, "backend", imageBackendSchema())
	setMap(properties, "outputDir", minLenStringSchema())
	setMap(props, "spec", spec)
}

func patchImageLoadToolSchema(root map[string]any) {
	props := propertyMap(root)
	spec, err := reflectedSpecSchema(&stepspec.LoadImage{})
	if err != nil {
		panic(err)
	}
	delete(propertyMap(spec), "timeout")
	spec["required"] = []any{"images"}
	properties := propertyMap(spec)
	setMap(properties, "images", stringArraySchema(1, false))
	setMap(properties, "sourceDir", minLenStringSchema())
	setMap(properties, "runtime", enumStringSchema("auto", "ctr", "docker", "podman"))
	setMap(properties, "command", stringArraySchema(1, false))
	setMap(props, "spec", spec)
}

func patchVerifyImageToolSchema(root map[string]any) {
	props := propertyMap(root)
	spec, err := reflectedSpecSchema(&stepspec.VerifyImage{})
	if err != nil {
		panic(err)
	}
	delete(propertyMap(spec), "timeout")
	spec["required"] = []any{"images"}
	properties := propertyMap(spec)
	setMap(properties, "images", stringArraySchema(1, false))
	setMap(properties, "command", stringArraySchema(1, false))
	setMap(props, "spec", spec)
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

func patchCheckHostToolSchema(root map[string]any) {
	props := propertyMap(root)
	spec, err := reflectedSpecSchema(&stepspec.CheckHost{})
	if err != nil {
		panic(err)
	}
	properties := propertyMap(spec)
	setMap(properties, "checks", map[string]any{"type": "array", "minItems": 1, "items": map[string]any{"type": "string", "enum": []any{"os", "arch", "kernelModules", "swap", "binaries"}}})
	setMap(properties, "binaries", stringArraySchema(0, false))
	setMap(properties, "failFast", map[string]any{"type": "boolean", "default": true})
	spec["required"] = []any{"checks"}
	setMap(props, "spec", spec)
}

func patchKernelModuleToolSchema(root map[string]any) {
	props := propertyMap(root)
	spec, err := reflectedSpecSchema(&stepspec.KernelModule{})
	if err != nil {
		panic(err)
	}
	delete(propertyMap(spec), "timeout")
	properties := propertyMap(spec)
	setMap(properties, "name", minLenStringSchema())
	setMap(properties, "names", stringArraySchema(1, true))
	setMap(properties, "load", map[string]any{"type": "boolean", "default": true})
	setMap(properties, "persist", map[string]any{"type": "boolean", "default": true})
	setMap(properties, "persistFile", map[string]any{"type": "string"})
	spec["oneOf"] = []any{
		map[string]any{"required": []any{"name"}, "not": map[string]any{"required": []any{"names"}}},
		map[string]any{"required": []any{"names"}, "not": map[string]any{"required": []any{"name"}}},
	}
	setMap(props, "spec", spec)
}

func patchInitKubeadmToolSchema(root map[string]any) {
	props := propertyMap(root)
	spec, err := reflectedSpecSchema(&stepspec.KubeadmInit{})
	if err != nil {
		panic(err)
	}
	delete(propertyMap(spec), "timeout")
	spec["required"] = []any{"outputJoinFile"}
	properties := propertyMap(spec)
	setMap(properties, "outputJoinFile", minLenStringSchema())
	setMap(properties, "skipIfAdminConfExists", map[string]any{"type": "boolean", "default": true})
	setMap(props, "spec", spec)
}

func patchJoinKubeadmToolSchema(root map[string]any) {
	props := propertyMap(root)
	spec, err := reflectedSpecSchema(&stepspec.KubeadmJoin{})
	if err != nil {
		panic(err)
	}
	delete(propertyMap(spec), "timeout")
	spec["oneOf"] = []any{
		map[string]any{"required": []any{"joinFile"}},
		map[string]any{"required": []any{"configFile"}},
	}
	properties := propertyMap(spec)
	setMap(properties, "joinFile", minLenStringSchema())
	setMap(properties, "configFile", minLenStringSchema())
	setMap(properties, "asControlPlane", map[string]any{"type": "boolean", "default": false})
	setMap(props, "spec", spec)
}

func patchResetKubeadmToolSchema(root map[string]any) {
	props := propertyMap(root)
	spec, err := reflectedSpecSchema(&stepspec.KubeadmReset{})
	if err != nil {
		panic(err)
	}
	delete(propertyMap(spec), "timeout")
	properties := propertyMap(spec)
	setMap(properties, "force", map[string]any{"type": "boolean", "default": false})
	setMap(properties, "ignoreErrors", map[string]any{"type": "boolean", "default": false})
	setMap(properties, "stopKubelet", map[string]any{"type": "boolean", "default": true})
	setMap(props, "spec", spec)
}

func patchUpgradeKubeadmToolSchema(root map[string]any) {
	props := propertyMap(root)
	spec, err := reflectedSpecSchema(&stepspec.KubeadmUpgrade{})
	if err != nil {
		panic(err)
	}
	delete(propertyMap(spec), "timeout")
	spec["required"] = []any{"kubernetesVersion"}
	properties := propertyMap(spec)
	setMap(properties, "kubernetesVersion", minLenStringSchema())
	setMap(properties, "restartKubelet", map[string]any{"type": "boolean", "default": true})
	setMap(properties, "kubeletService", minLenStringSchema())
	setMap(props, "spec", spec)
}

func patchCheckClusterToolSchema(root map[string]any) {
	props := propertyMap(root)
	spec, err := reflectedSpecSchema(&stepspec.ClusterCheck{})
	if err != nil {
		panic(err)
	}
	properties := propertyMap(spec)
	setMap(properties, "kubeconfig", minLenStringSchema())
	setMap(properties, "interval", durationStringSchema())
	setMap(properties, "initialDelay", durationStringSchema())
	setMap(properties, "timeout", durationStringSchema())

	nodes := propertyMap(spec)["nodes"].(map[string]any)
	nodeProps := propertyMap(nodes)
	setMap(nodeProps, "total", map[string]any{"type": "integer", "minimum": 0})
	setMap(nodeProps, "ready", map[string]any{"type": "integer", "minimum": 0})
	setMap(nodeProps, "controlPlaneReady", map[string]any{"type": "integer", "minimum": 0})

	versions := propertyMap(spec)["versions"].(map[string]any)
	versionProps := propertyMap(versions)
	setMap(versionProps, "targetVersion", minLenStringSchema())
	setMap(versionProps, "server", minLenStringSchema())
	setMap(versionProps, "kubelet", minLenStringSchema())
	setMap(versionProps, "kubeadm", minLenStringSchema())
	setMap(versionProps, "nodeName", minLenStringSchema())
	setMap(versionProps, "reportPath", minLenStringSchema())

	kubeSystem := propertyMap(spec)["kubeSystem"].(map[string]any)
	kubeSystemProps := propertyMap(kubeSystem)
	setMap(kubeSystemProps, "readyNames", stringArraySchema(0, false))
	setMap(kubeSystemProps, "readyPrefixes", stringArraySchema(0, false))
	setMap(kubeSystemProps, "reportPath", minLenStringSchema())
	setMap(kubeSystemProps, "jsonReportPath", minLenStringSchema())
	readyPrefixMinimums, _ := kubeSystemProps["readyPrefixMinimums"].(map[string]any)
	if items, ok := readyPrefixMinimums["items"].(map[string]any); ok {
		items["required"] = []any{"prefix", "minReady"}
		itemProps := propertyMap(items)
		setMap(itemProps, "prefix", minLenStringSchema())
		setMap(itemProps, "minReady", map[string]any{"type": "integer", "minimum": 1})
	}

	fileAssertions, _ := propertyMap(spec)["fileAssertions"].(map[string]any)
	if items, ok := fileAssertions["items"].(map[string]any); ok {
		items["required"] = []any{"path", "contains"}
		itemProps := propertyMap(items)
		setMap(itemProps, "path", minLenStringSchema())
		setMap(itemProps, "contains", stringArraySchema(1, false))
	}

	reports := propertyMap(spec)["reports"].(map[string]any)
	reportProps := propertyMap(reports)
	setMap(reportProps, "nodesPath", minLenStringSchema())
	setMap(reportProps, "clusterNodesPath", minLenStringSchema())

	setMap(props, "spec", spec)
}
