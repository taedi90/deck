package main

func toolSchemaDefinitions() map[string]map[string]any {
	return map[string]map[string]any{
		"artifacts.schema.json":     generateArtifactsToolSchema(),
		"command.schema.json":       generateCommandToolSchema(),
		"containerd.schema.json":    generateContainerdToolSchema(),
		"directory.schema.json":     generateDirectoryToolSchema(),
		"file.schema.json":          generateFileToolSchema(),
		"image.schema.json":         generateImageToolSchema(),
		"inspection.schema.json":    generateInspectionToolSchema(),
		"kernel-module.schema.json": generateKernelModuleToolSchema(),
		"kubeadm.schema.json":       generateKubeadmToolSchema(),
		"package-cache.schema.json": generatePackageCacheToolSchema(),
		"packages.schema.json":      generatePackagesToolSchema(),
		"repository.schema.json":    generateRepositoryToolSchema(),
		"service.schema.json":       generateServiceToolSchema(),
		"swap.schema.json":          generateSwapToolSchema(),
		"symlink.schema.json":       generateSymlinkToolSchema(),
		"sysctl.schema.json":        generateSysctlToolSchema(),
		"systemd-unit.schema.json":  generateSystemdUnitToolSchema(),
		"wait.schema.json":          generateWaitToolSchema(),
	}
}
