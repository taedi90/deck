package main

import (
	"fmt"

	"github.com/taedi90/deck/internal/stepspec"
	"github.com/taedi90/deck/internal/workflowexec"
)

type builtInSchemaEntry struct {
	Kind          string
	GeneratorName string
	SpecType      any
	Patch         func(root map[string]any)
}

var builtInSchemaEntries = []builtInSchemaEntry{
	{Kind: "CheckHost", GeneratorName: "host-check", SpecType: &stepspec.CheckHost{}, Patch: patchCheckHostToolSchema},
	{Kind: "Command", GeneratorName: "command", SpecType: &stepspec.Command{}, Patch: patchCommandToolSchema},
	{Kind: "WriteContainerdConfig", GeneratorName: "containerd.config", SpecType: &stepspec.WriteContainerdConfig{}, Patch: patchWriteContainerdConfigToolSchema},
	{Kind: "WriteContainerdRegistryHosts", GeneratorName: "containerd.registry-hosts", SpecType: &stepspec.WriteContainerdRegistryHosts{}, Patch: patchWriteContainerdRegistryHostsToolSchema},
	{Kind: "EnsureDirectory", GeneratorName: "directory", SpecType: &stepspec.EnsureDirectory{}, Patch: patchEnsureDirectoryToolSchema},
	{Kind: "DownloadFile", GeneratorName: "file.download", SpecType: &stepspec.DownloadFile{}, Patch: patchDownloadFileToolSchema},
	{Kind: "WriteFile", GeneratorName: "file.write", SpecType: &stepspec.WriteFile{}, Patch: patchWriteFileToolSchema},
	{Kind: "CopyFile", GeneratorName: "file.copy", SpecType: &stepspec.CopyFile{}, Patch: patchCopyFileToolSchema},
	{Kind: "EditFile", GeneratorName: "file.edit", SpecType: &stepspec.EditFile{}, Patch: patchEditFileToolSchema},
	{Kind: "ExtractArchive", GeneratorName: "file.extract-archive", SpecType: &stepspec.ExtractArchive{}, Patch: patchExtractArchiveToolSchema},
	{Kind: "DownloadImage", GeneratorName: "image.download", SpecType: &stepspec.DownloadImage{}, Patch: patchDownloadImageToolSchema},
	{Kind: "LoadImage", GeneratorName: "image.load", SpecType: &stepspec.LoadImage{}, Patch: patchImageLoadToolSchema},
	{Kind: "VerifyImage", GeneratorName: "image.verify", SpecType: &stepspec.VerifyImage{}, Patch: patchVerifyImageToolSchema},
	{Kind: "KernelModule", GeneratorName: "kernel-module", SpecType: &stepspec.KernelModule{}, Patch: patchKernelModuleToolSchema},
	{Kind: "CheckCluster", GeneratorName: "cluster-check", SpecType: &stepspec.ClusterCheck{}, Patch: patchCheckClusterToolSchema},
	{Kind: "InitKubeadm", GeneratorName: "kubeadm.init", SpecType: &stepspec.KubeadmInit{}, Patch: patchInitKubeadmToolSchema},
	{Kind: "JoinKubeadm", GeneratorName: "kubeadm.join", SpecType: &stepspec.KubeadmJoin{}, Patch: patchJoinKubeadmToolSchema},
	{Kind: "ResetKubeadm", GeneratorName: "kubeadm.reset", SpecType: &stepspec.KubeadmReset{}, Patch: patchResetKubeadmToolSchema},
	{Kind: "UpgradeKubeadm", GeneratorName: "kubeadm.upgrade", SpecType: &stepspec.KubeadmUpgrade{}, Patch: patchUpgradeKubeadmToolSchema},
	{Kind: "DownloadPackage", GeneratorName: "package.download", SpecType: &stepspec.DownloadPackage{}, Patch: patchDownloadPackageToolSchema},
	{Kind: "InstallPackage", GeneratorName: "package.install", SpecType: &stepspec.InstallPackage{}, Patch: patchInstallPackageToolSchema},
	{Kind: "ConfigureRepository", GeneratorName: "repository.configure", SpecType: &stepspec.ConfigureRepository{}, Patch: patchConfigureRepositoryToolSchema},
	{Kind: "RefreshRepository", GeneratorName: "repository.refresh", SpecType: &stepspec.RefreshRepository{}, Patch: patchRefreshRepositoryToolSchema},
	{Kind: "ManageService", GeneratorName: "service", SpecType: &stepspec.ManageService{}, Patch: patchManageServiceToolSchema},
	{Kind: "Swap", GeneratorName: "swap", SpecType: &stepspec.Swap{}, Patch: patchSwapToolSchema},
	{Kind: "CreateSymlink", GeneratorName: "symlink", SpecType: &stepspec.CreateSymlink{}, Patch: patchCreateSymlinkToolSchema},
	{Kind: "Sysctl", GeneratorName: "sysctl", SpecType: &stepspec.Sysctl{}, Patch: patchSysctlToolSchema},
	{Kind: "WriteSystemdUnit", GeneratorName: "systemd-unit", SpecType: &stepspec.WriteSystemdUnit{}, Patch: patchWriteSystemdUnitToolSchema},
	{Kind: "WaitForService", GeneratorName: "wait.service-active", SpecType: &stepspec.Wait{}, Patch: patchWaitForServiceToolSchema},
	{Kind: "WaitForCommand", GeneratorName: "wait.command", SpecType: &stepspec.Wait{}, Patch: patchWaitForCommandToolSchema},
	{Kind: "WaitForFile", GeneratorName: "wait.file-exists", SpecType: &stepspec.Wait{}, Patch: patchWaitForFileToolSchema},
	{Kind: "WaitForMissingFile", GeneratorName: "wait.file-absent", SpecType: &stepspec.Wait{}, Patch: patchWaitForMissingFileToolSchema},
	{Kind: "WaitForTCPPort", GeneratorName: "wait.tcp-port-open", SpecType: &stepspec.Wait{}, Patch: patchWaitForTCPPortToolSchema},
	{Kind: "WaitForMissingTCPPort", GeneratorName: "wait.tcp-port-closed", SpecType: &stepspec.Wait{}, Patch: patchWaitForMissingTCPPortToolSchema},
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
		GeneratorName: entry.GeneratorName,
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
