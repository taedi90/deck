package main

import (
	"fmt"

	"github.com/taedi90/deck/internal/stepspec"
	"github.com/taedi90/deck/internal/workflowexec"
)

type builtInSchemaEntry struct {
	Kind     string
	SpecType any
	Patch    func(root map[string]any)
}

var builtInSchemaEntries = []builtInSchemaEntry{
	{Kind: "CheckHost", SpecType: &stepspec.CheckHost{}, Patch: patchCheckHostToolSchema},
	{Kind: "Command", SpecType: &stepspec.Command{}, Patch: patchCommandToolSchema},
	{Kind: "WriteContainerdConfig", SpecType: &stepspec.WriteContainerdConfig{}, Patch: patchWriteContainerdConfigToolSchema},
	{Kind: "WriteContainerdRegistryHosts", SpecType: &stepspec.WriteContainerdRegistryHosts{}, Patch: patchWriteContainerdRegistryHostsToolSchema},
	{Kind: "EnsureDirectory", SpecType: &stepspec.EnsureDirectory{}, Patch: patchEnsureDirectoryToolSchema},
	{Kind: "DownloadFile", SpecType: &stepspec.DownloadFile{}, Patch: patchDownloadFileToolSchema},
	{Kind: "WriteFile", SpecType: &stepspec.WriteFile{}, Patch: patchWriteFileToolSchema},
	{Kind: "CopyFile", SpecType: &stepspec.CopyFile{}, Patch: patchCopyFileToolSchema},
	{Kind: "EditFile", SpecType: &stepspec.EditFile{}, Patch: patchEditFileToolSchema},
	{Kind: "EditTOML", SpecType: &stepspec.EditTOML{}, Patch: patchEditTOMLToolSchema},
	{Kind: "EditYAML", SpecType: &stepspec.EditYAML{}, Patch: patchEditYAMLToolSchema},
	{Kind: "EditJSON", SpecType: &stepspec.EditJSON{}, Patch: patchEditJSONToolSchema},
	{Kind: "ExtractArchive", SpecType: &stepspec.ExtractArchive{}, Patch: patchExtractArchiveToolSchema},
	{Kind: "DownloadImage", SpecType: &stepspec.DownloadImage{}, Patch: patchDownloadImageToolSchema},
	{Kind: "LoadImage", SpecType: &stepspec.LoadImage{}, Patch: patchImageLoadToolSchema},
	{Kind: "VerifyImage", SpecType: &stepspec.VerifyImage{}, Patch: patchVerifyImageToolSchema},
	{Kind: "KernelModule", SpecType: &stepspec.KernelModule{}, Patch: patchKernelModuleToolSchema},
	{Kind: "CheckCluster", SpecType: &stepspec.ClusterCheck{}, Patch: patchCheckClusterToolSchema},
	{Kind: "InitKubeadm", SpecType: &stepspec.KubeadmInit{}, Patch: patchInitKubeadmToolSchema},
	{Kind: "JoinKubeadm", SpecType: &stepspec.KubeadmJoin{}, Patch: patchJoinKubeadmToolSchema},
	{Kind: "ResetKubeadm", SpecType: &stepspec.KubeadmReset{}, Patch: patchResetKubeadmToolSchema},
	{Kind: "UpgradeKubeadm", SpecType: &stepspec.KubeadmUpgrade{}, Patch: patchUpgradeKubeadmToolSchema},
	{Kind: "DownloadPackage", SpecType: &stepspec.DownloadPackage{}, Patch: patchDownloadPackageToolSchema},
	{Kind: "InstallPackage", SpecType: &stepspec.InstallPackage{}, Patch: patchInstallPackageToolSchema},
	{Kind: "ConfigureRepository", SpecType: &stepspec.ConfigureRepository{}, Patch: patchConfigureRepositoryToolSchema},
	{Kind: "RefreshRepository", SpecType: &stepspec.RefreshRepository{}, Patch: patchRefreshRepositoryToolSchema},
	{Kind: "ManageService", SpecType: &stepspec.ManageService{}, Patch: patchManageServiceToolSchema},
	{Kind: "Swap", SpecType: &stepspec.Swap{}, Patch: patchSwapToolSchema},
	{Kind: "CreateSymlink", SpecType: &stepspec.CreateSymlink{}, Patch: patchCreateSymlinkToolSchema},
	{Kind: "Sysctl", SpecType: &stepspec.Sysctl{}, Patch: patchSysctlToolSchema},
	{Kind: "WriteSystemdUnit", SpecType: &stepspec.WriteSystemdUnit{}, Patch: patchWriteSystemdUnitToolSchema},
	{Kind: "WaitForService", SpecType: &stepspec.Wait{}, Patch: patchWaitForServiceToolSchema},
	{Kind: "WaitForCommand", SpecType: &stepspec.Wait{}, Patch: patchWaitForCommandToolSchema},
	{Kind: "WaitForFile", SpecType: &stepspec.Wait{}, Patch: patchWaitForFileToolSchema},
	{Kind: "WaitForMissingFile", SpecType: &stepspec.Wait{}, Patch: patchWaitForMissingFileToolSchema},
	{Kind: "WaitForTCPPort", SpecType: &stepspec.Wait{}, Patch: patchWaitForTCPPortToolSchema},
	{Kind: "WaitForMissingTCPPort", SpecType: &stepspec.Wait{}, Patch: patchWaitForMissingTCPPortToolSchema},
}

var builtInSchemaIndex = func() map[string]builtInSchemaEntry {
	index := make(map[string]builtInSchemaEntry, len(builtInSchemaEntries))
	for _, entry := range builtInSchemaEntries {
		if _, exists := index[entry.Kind]; exists {
			panic(fmt.Sprintf("duplicate built-in schema entry for %s", entry.Kind))
		}
		index[entry.Kind] = entry
	}
	return index
}()

var _ = workflowexec.RegisterSchemaMetadataBuilder(func(def workflowexec.StepDefinition) workflowexec.SchemaMetadata {
	entry, ok := builtInSchemaIndex[def.Kind]
	if !ok {
		panic(fmt.Sprintf("missing declarative schema entry for %s", def.Kind))
	}
	meta := workflowexec.SchemaMetadata{
		GeneratorName: def.ToolSchemaGenerator,
		SpecType:      entry.SpecType,
		Patch:         entry.Patch,
	}
	if meta.GeneratorName == "" {
		meta.GeneratorName = def.ToolSchemaGenerator
	}
	if meta.GeneratorName == "" {
		meta.GeneratorName = def.Kind
	}
	if meta.Patch == nil || meta.SpecType == nil {
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
