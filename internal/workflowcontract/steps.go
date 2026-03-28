package workflowcontract

import (
	"sort"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/stepmeta"
	_ "github.com/Airgap-Castaways/deck/internal/stepspec"
)

type StepDefinition struct {
	APIVersion          string
	Kind                string
	Family              string
	FamilyTitle         string
	DocsPage            string
	DocsOrder           int
	SchemaFile          string
	ToolSchemaGenerator string
	Visibility          string
	Category            string
	Summary             string
	WhenToUse           string
	Roles               []string
	Outputs             []string
}

type StepTypeKey struct {
	APIVersion string
	Kind       string
}

func StepDefinitions() []StepDefinition {
	defs := []StepDefinition{
		stepDefFromMeta("CheckHost", "host-check", "system"),
		stepDefFromMeta("Command", "command", "advanced"),
		stepDefFromMeta("WriteContainerdConfig", "containerd.config", "runtime"),
		stepDefFromMeta("WriteContainerdRegistryHosts", "containerd.registry-hosts", "runtime"),
		stepDefFromMeta("EnsureDirectory", "directory", "filesystem"),
		stepDefFromMeta("DownloadFile", "file.download", "filesystem"),
		stepDefFromMeta("WriteFile", "file.write", "filesystem"),
		stepDefFromMeta("CopyFile", "file.copy", "filesystem"),
		stepDefFromMeta("EditFile", "file.edit", "filesystem"),
		stepDefFromMeta("EditTOML", "file.edit-toml", "filesystem"),
		stepDefFromMeta("EditYAML", "file.edit-yaml", "filesystem"),
		stepDefFromMeta("EditJSON", "file.edit-json", "filesystem"),
		stepDefFromMeta("ExtractArchive", "file.extract-archive", "filesystem"),
		stepDefFromMeta("DownloadImage", "image.download", "containers"),
		stepDefFromMeta("LoadImage", "image.load", "containers"),
		stepDefFromMeta("VerifyImage", "image.verify", "containers"),
		stepDefFromMeta("KernelModule", "kernel-module", "system"),
		stepDefFromMeta("CheckCluster", "cluster-check", "kubernetes"),
		stepDefFromMeta("InitKubeadm", "kubeadm.init", "kubernetes"),
		stepDefFromMeta("JoinKubeadm", "kubeadm.join", "kubernetes"),
		stepDefFromMeta("ResetKubeadm", "kubeadm.reset", "kubernetes"),
		stepDefFromMeta("UpgradeKubeadm", "kubeadm.upgrade", "kubernetes"),
		stepDefFromMeta("DownloadPackage", "package.download", "packages"),
		stepDefFromMeta("InstallPackage", "package.install", "packages"),
		stepDefFromMeta("ConfigureRepository", "repository.configure", "packages"),
		stepDefFromMeta("RefreshRepository", "repository.refresh", "packages"),
		stepDefFromMeta("ManageService", "service", "system"),
		stepDefFromMeta("Swap", "swap", "system"),
		stepDefFromMeta("CreateSymlink", "symlink", "filesystem"),
		stepDefFromMeta("Sysctl", "sysctl", "system"),
		stepDefFromMeta("WriteSystemdUnit", "systemd-unit", "system"),
		stepDefFromMeta("WaitForService", "wait.service-active", "control-flow"),
		stepDefFromMeta("WaitForCommand", "wait.command", "control-flow"),
		stepDefFromMeta("WaitForFile", "wait.file-exists", "control-flow"),
		stepDefFromMeta("WaitForMissingFile", "wait.file-absent", "control-flow"),
		stepDefFromMeta("WaitForTCPPort", "wait.tcp-port-open", "control-flow"),
		stepDefFromMeta("WaitForMissingTCPPort", "wait.tcp-port-closed", "control-flow"),
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Kind < defs[j].Kind })
	return defs
}

func StepDefinitionForKey(key StepTypeKey) (StepDefinition, bool) {
	for _, def := range StepDefinitions() {
		if def.APIVersion == strings.TrimSpace(key.APIVersion) && def.Kind == strings.TrimSpace(key.Kind) {
			return def, true
		}
	}
	return StepDefinition{}, false
}

func stepDef(kind, family, familyTitle, docsPage string, docsOrder int, schemaFile, generator, visibility, category, summary, whenToUse string, roles, outputs []string) StepDefinition {
	def := StepDefinition{
		APIVersion:          BuiltInStepAPIVersion,
		Kind:                kind,
		Family:              family,
		FamilyTitle:         familyTitle,
		DocsPage:            docsPage,
		DocsOrder:           docsOrder,
		SchemaFile:          schemaFile,
		ToolSchemaGenerator: generator,
		Visibility:          visibility,
		Category:            category,
		Summary:             summary,
		WhenToUse:           whenToUse,
		Roles:               append([]string(nil), roles...),
		Outputs:             append([]string(nil), outputs...),
	}
	sort.Strings(def.Roles)
	sort.Strings(def.Outputs)
	return def
}

func stepDefFromMeta(kind string, generator string, category string) StepDefinition {
	entry, ok, err := stepmeta.LookupCatalogEntry(kind)
	if err != nil {
		panic(err)
	}
	if !ok {
		panic("missing stepmeta registration for " + kind)
	}
	projection := stepmeta.ProjectWorkflow(entry, category, generator)
	return stepDef(
		projection.Kind,
		projection.Family,
		projection.FamilyTitle,
		projection.DocsPage,
		projection.DocsOrder,
		projection.SchemaFile,
		projection.Generator,
		projection.Visibility,
		projection.Category,
		projection.Summary,
		projection.WhenToUse,
		projection.Roles,
		projection.Outputs,
	)
}
