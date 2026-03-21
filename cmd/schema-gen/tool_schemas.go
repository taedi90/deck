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
		"host-check":                wrapToolSchema(generateCheckHostToolSchema),
		"command":                   wrapToolSchema(generateCommandToolSchema),
		"containerd.config":         wrapToolSchema(generateWriteContainerdConfigToolSchema),
		"containerd.registry-hosts": wrapToolSchema(generateWriteContainerdRegistryHostsToolSchema),
		"directory":                 wrapToolSchema(generateEnsureDirectoryToolSchema),
		"file.copy":                 wrapToolSchema(generateCopyFileToolSchema),
		"file.download":             wrapToolSchema(generateDownloadFileToolSchema),
		"file.edit":                 wrapToolSchema(generateEditFileToolSchema),
		"file.extract-archive":      wrapToolSchema(generateExtractArchiveToolSchema),
		"file.write":                wrapToolSchema(generateWriteFileToolSchema),
		"image.download":            wrapToolSchema(generateDownloadImageToolSchema),
		"image.load":                wrapToolSchema(generateImageLoadToolSchema),
		"image.verify":              wrapToolSchema(generateVerifyImageToolSchema),
		"kernel-module":             wrapToolSchema(generateKernelModuleToolSchema),
		"kubeadm.init":              wrapToolSchema(generateInitKubeadmToolSchema),
		"kubeadm.join":              wrapToolSchema(generateJoinKubeadmToolSchema),
		"kubeadm.reset":             wrapToolSchema(generateResetKubeadmToolSchema),
		"package.download":          wrapToolSchema(generateDownloadPackageToolSchema),
		"package.install":           wrapToolSchema(generateInstallPackageToolSchema),
		"repository.configure":      wrapToolSchema(generateConfigureRepositoryToolSchema),
		"repository.refresh":        wrapToolSchema(generateRefreshRepositoryToolSchema),
		"service":                   wrapToolSchema(generateManageServiceToolSchema),
		"swap":                      wrapToolSchema(generateSwapToolSchema),
		"symlink":                   wrapToolSchema(generateCreateSymlinkToolSchema),
		"sysctl":                    wrapToolSchema(generateSysctlToolSchema),
		"systemd-unit":              wrapToolSchema(generateWriteSystemdUnitToolSchema),
		"wait.command":              wrapToolSchema(generateWaitForCommandToolSchema),
		"wait.file-absent":          wrapToolSchema(generateWaitForMissingFileToolSchema),
		"wait.file-exists":          wrapToolSchema(generateWaitForFileToolSchema),
		"wait.service-active":       wrapToolSchema(generateWaitForServiceToolSchema),
		"wait.tcp-port-closed":      wrapToolSchema(generateWaitForMissingTCPPortToolSchema),
		"wait.tcp-port-open":        wrapToolSchema(generateWaitForTCPPortToolSchema),
	}
}

func wrapToolSchema(generator func() map[string]any) toolSchemaGenerator {
	return func() (map[string]any, error) {
		return generator(), nil
	}
}
