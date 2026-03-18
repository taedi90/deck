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
		generator, ok := generators[def.Kind]
		if !ok {
			return nil, fmt.Errorf("missing tool schema generator for %s", def.Kind)
		}
		schema, err := generator()
		if err != nil {
			return nil, err
		}
		generated[def.SchemaFile] = schema
	}
	for kind := range generators {
		if _, ok := workflowexec.StepContractForKind(kind); !ok {
			return nil, fmt.Errorf("tool schema generator registered for unknown kind %s", kind)
		}
	}
	return generated, nil
}

func toolSchemaGenerators() map[string]toolSchemaGenerator {
	return map[string]toolSchemaGenerator{
		"Artifacts":    wrapToolSchema(generateArtifactsToolSchema),
		"Checks":       wrapToolSchema(generateChecksToolSchema),
		"Command":      wrapToolSchema(generateCommandToolSchema),
		"Containerd":   wrapToolSchema(generateContainerdToolSchema),
		"Directory":    wrapToolSchema(generateDirectoryToolSchema),
		"File":         generateFileToolSchema,
		"Image":        wrapToolSchema(generateImageToolSchema),
		"KernelModule": wrapToolSchema(generateKernelModuleToolSchema),
		"Kubeadm":      wrapToolSchema(generateKubeadmToolSchema),
		"PackageCache": wrapToolSchema(generatePackageCacheToolSchema),
		"Packages":     wrapToolSchema(generatePackagesToolSchema),
		"Repository":   wrapToolSchema(generateRepositoryToolSchema),
		"Service":      wrapToolSchema(generateServiceToolSchema),
		"Swap":         wrapToolSchema(generateSwapToolSchema),
		"Symlink":      wrapToolSchema(generateSymlinkToolSchema),
		"Sysctl":       wrapToolSchema(generateSysctlToolSchema),
		"SystemdUnit":  wrapToolSchema(generateSystemdUnitToolSchema),
		"Wait":         generateWaitToolSchema,
	}
}

func wrapToolSchema(generator func() map[string]any) toolSchemaGenerator {
	return func() (map[string]any, error) {
		return generator(), nil
	}
}
