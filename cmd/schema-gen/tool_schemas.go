package main

import (
	"fmt"

	"github.com/taedi90/deck/internal/workflowexec"
)

type toolSchemaGenerator func() (map[string]any, error)

func toolSchemaDefinitions() (map[string]map[string]any, error) {
	defs := workflowexec.StepDefinitions()
	generated := make(map[string]map[string]any, len(defs))
	generators := toolSchemaGenerators()
	for _, def := range defs {
		generatorName := def.ToolSchemaGenerator
		if generatorName == "" {
			generatorName = def.Kind
		}
		generator, ok := generators[generatorName]
		if !ok {
			return nil, fmt.Errorf("missing tool schema generator %q for %s", generatorName, def.Kind)
		}
		schema, err := generator()
		if err != nil {
			return nil, err
		}
		generated[def.SchemaFile] = schema
	}
	usedGenerators := map[string]bool{}
	for _, def := range defs {
		name := def.ToolSchemaGenerator
		if name == "" {
			name = def.Kind
		}
		usedGenerators[name] = true
	}
	for name := range generators {
		if !usedGenerators[name] {
			return nil, fmt.Errorf("tool schema generator registered but unused: %s", name)
		}
	}
	return generated, nil
}

func toolSchemaGenerators() map[string]toolSchemaGenerator {
	return map[string]toolSchemaGenerator{
		"artifacts":            wrapToolSchema(generateArtifactsToolSchema),
		"checks":               wrapToolSchema(generateChecksToolSchema),
		"command":              wrapToolSchema(generateCommandToolSchema),
		"containerd":           wrapToolSchema(generateContainerdToolSchema),
		"directory":            wrapToolSchema(generateDirectoryToolSchema),
		"file.copy":            wrapToolSchema(generateFileCopyToolSchema),
		"file.download":        wrapToolSchema(generateFileDownloadToolSchema),
		"file.edit":            wrapToolSchema(generateFileEditToolSchema),
		"file.write":           wrapToolSchema(generateFileWriteToolSchema),
		"image.download":       wrapToolSchema(generateImageDownloadToolSchema),
		"image.verify":         wrapToolSchema(generateImageVerifyToolSchema),
		"kernel-module":        wrapToolSchema(generateKernelModuleToolSchema),
		"kubeadm.init":         wrapToolSchema(generateKubeadmInitToolSchema),
		"kubeadm.join":         wrapToolSchema(generateKubeadmJoinToolSchema),
		"kubeadm.reset":        wrapToolSchema(generateKubeadmResetToolSchema),
		"package-cache":        wrapToolSchema(generatePackageCacheToolSchema),
		"packages.download":    wrapToolSchema(generatePackagesDownloadToolSchema),
		"packages.install":     wrapToolSchema(generatePackagesInstallToolSchema),
		"repository":           wrapToolSchema(generateRepositoryToolSchema),
		"service":              wrapToolSchema(generateServiceToolSchema),
		"swap":                 wrapToolSchema(generateSwapToolSchema),
		"symlink":              wrapToolSchema(generateSymlinkToolSchema),
		"sysctl":               wrapToolSchema(generateSysctlToolSchema),
		"systemd-unit":         wrapToolSchema(generateSystemdUnitToolSchema),
		"wait.command-success": wrapToolSchema(generateWaitCommandSuccessToolSchema),
		"wait.file-absent":     wrapToolSchema(generateWaitFileAbsentToolSchema),
		"wait.file-exists":     wrapToolSchema(generateWaitFileExistsToolSchema),
		"wait.service-active":  wrapToolSchema(generateWaitServiceActiveToolSchema),
		"wait.tcp-port-closed": wrapToolSchema(generateWaitTCPPortClosedToolSchema),
		"wait.tcp-port-open":   wrapToolSchema(generateWaitTCPPortOpenToolSchema),
	}
}

func wrapToolSchema(generator func() map[string]any) toolSchemaGenerator {
	return func() (map[string]any, error) {
		return generator(), nil
	}
}
