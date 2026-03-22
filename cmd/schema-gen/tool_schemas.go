package main

import (
	"fmt"

	"github.com/taedi90/deck/internal/stepspec"
	"github.com/taedi90/deck/internal/workflowexec"
)

var _ = workflowexec.RegisterSchemaMetadataBuilder(func(def workflowexec.StepDefinition) workflowexec.SchemaMetadata {
	generatorName := def.ToolSchemaGenerator
	if generatorName == "" {
		generatorName = def.Kind
	}
	meta := workflowexec.SchemaMetadata{GeneratorName: generatorName}
	switch def.Kind {
	case "CheckHost":
		meta.SpecType = &stepspec.CheckHost{}
		meta.Patch = patchCheckHostToolSchema
	case "Command":
		meta.SpecType = &stepspec.Command{}
		meta.Patch = patchCommandToolSchema
	case "WriteContainerdConfig":
		meta.SpecType = &stepspec.WriteContainerdConfig{}
		meta.Patch = patchWriteContainerdConfigToolSchema
	case "WriteContainerdRegistryHosts":
		meta.SpecType = &stepspec.WriteContainerdRegistryHosts{}
		meta.Patch = patchWriteContainerdRegistryHostsToolSchema
	case "EnsureDirectory":
		meta.SpecType = &stepspec.EnsureDirectory{}
		meta.Patch = patchEnsureDirectoryToolSchema
	case "DownloadFile":
		meta.SpecType = &stepspec.DownloadFile{}
		meta.Patch = patchDownloadFileToolSchema
	case "WriteFile":
		meta.SpecType = &stepspec.WriteFile{}
		meta.Patch = patchWriteFileToolSchema
	case "CopyFile":
		meta.SpecType = &stepspec.CopyFile{}
		meta.Patch = patchCopyFileToolSchema
	case "EditFile":
		meta.SpecType = &stepspec.EditFile{}
		meta.Patch = patchEditFileToolSchema
	case "ExtractArchive":
		meta.SpecType = &stepspec.ExtractArchive{}
		meta.Patch = patchExtractArchiveToolSchema
	case "DownloadImage":
		meta.SpecType = &stepspec.DownloadImage{}
		meta.Patch = patchDownloadImageToolSchema
	case "LoadImage":
		meta.SpecType = &stepspec.LoadImage{}
		meta.Patch = patchImageLoadToolSchema
	case "VerifyImage":
		meta.SpecType = &stepspec.VerifyImage{}
		meta.Patch = patchVerifyImageToolSchema
	case "KernelModule":
		meta.SpecType = &stepspec.KernelModule{}
		meta.Patch = patchKernelModuleToolSchema
	case "CheckCluster":
		meta.SpecType = &stepspec.ClusterCheck{}
		meta.Patch = patchCheckClusterToolSchema
	case "InitKubeadm":
		meta.SpecType = &stepspec.KubeadmInit{}
		meta.Patch = patchInitKubeadmToolSchema
	case "JoinKubeadm":
		meta.SpecType = &stepspec.KubeadmJoin{}
		meta.Patch = patchJoinKubeadmToolSchema
	case "ResetKubeadm":
		meta.SpecType = &stepspec.KubeadmReset{}
		meta.Patch = patchResetKubeadmToolSchema
	case "UpgradeKubeadm":
		meta.SpecType = &stepspec.KubeadmUpgrade{}
		meta.Patch = patchUpgradeKubeadmToolSchema
	case "DownloadPackage":
		meta.SpecType = &stepspec.DownloadPackage{}
		meta.Patch = patchDownloadPackageToolSchema
	case "InstallPackage":
		meta.SpecType = &stepspec.InstallPackage{}
		meta.Patch = patchInstallPackageToolSchema
	case "ConfigureRepository":
		meta.SpecType = &stepspec.ConfigureRepository{}
		meta.Patch = patchConfigureRepositoryToolSchema
	case "RefreshRepository":
		meta.SpecType = &stepspec.RefreshRepository{}
		meta.Patch = patchRefreshRepositoryToolSchema
	case "ManageService":
		meta.SpecType = &stepspec.ManageService{}
		meta.Patch = patchManageServiceToolSchema
	case "Swap":
		meta.SpecType = &stepspec.Swap{}
		meta.Patch = patchSwapToolSchema
	case "CreateSymlink":
		meta.SpecType = &stepspec.CreateSymlink{}
		meta.Patch = patchCreateSymlinkToolSchema
	case "Sysctl":
		meta.SpecType = &stepspec.Sysctl{}
		meta.Patch = patchSysctlToolSchema
	case "WriteSystemdUnit":
		meta.SpecType = &stepspec.WriteSystemdUnit{}
		meta.Patch = patchWriteSystemdUnitToolSchema
	case "WaitForService", "WaitForCommand", "WaitForFile", "WaitForMissingFile", "WaitForTCPPort", "WaitForMissingTCPPort":
		meta.SpecType = &stepspec.Wait{}
		switch def.Kind {
		case "WaitForService":
			meta.Patch = patchWaitForServiceToolSchema
		case "WaitForCommand":
			meta.Patch = patchWaitForCommandToolSchema
		case "WaitForFile":
			meta.Patch = patchWaitForFileToolSchema
		case "WaitForMissingFile":
			meta.Patch = patchWaitForMissingFileToolSchema
		case "WaitForTCPPort":
			meta.Patch = patchWaitForTCPPortToolSchema
		case "WaitForMissingTCPPort":
			meta.Patch = patchWaitForMissingTCPPortToolSchema
		}
	}
	if meta.Patch == nil {
		panic(fmt.Sprintf("missing direct schema patch hook for %s", def.Kind))
	}
	return meta
})

func toolSchemaDefinitions() (map[string]map[string]any, error) {
	defs := workflowexec.BuiltInTypeDefinitions()
	generated := make(map[string]map[string]any, len(defs))
	for _, def := range defs {
		if def.Schema.SpecType == nil || def.Schema.Patch == nil {
			return nil, fmt.Errorf("missing direct schema metadata for %s", def.Step.Kind)
		}
		schema, err := generateToolSchemaFromRegistry(def)
		if err != nil {
			return nil, err
		}
		generated[def.Step.SchemaFile] = schema
	}
	usedGenerators := map[string]bool{}
	for _, def := range defs {
		name := def.Schema.GeneratorName
		if name == "" {
			name = def.Step.Kind
		}
		usedGenerators[name] = true
	}
	return generated, nil
}

func generateToolSchemaFromRegistry(def workflowexec.BuiltInTypeDefinition) (map[string]any, error) {
	root := stepEnvelopeSchema(def.Step.Kind, def.Step.Kind+"Step", def.Step.Summary, def.Step.Visibility)
	spec, err := reflectedSpecSchema(def.Schema.SpecType)
	if err != nil {
		return nil, err
	}
	setMap(propertyMap(root), "spec", spec)
	def.Schema.Patch(root)
	return root, nil
}
