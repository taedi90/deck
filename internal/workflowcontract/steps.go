package workflowcontract

import (
	"sort"
	"strings"
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
		stepDef("CheckHost", "host-check", "HostCheck", "host-check", 10, "host-check.schema.json", "host-check", "public", "prepare", "Run host checks before prepare execution.", "Use this at the start of prepare workflows to fail early on unsupported hosts.", []string{"prepare"}, []string{"passed", "failedChecks"}),
		stepDef("Command", "command", "Command", "command", 10, "command.schema.json", "command", "public", "advanced", "Run an explicit command as an escape hatch.", "Use this only when no typed step expresses the change clearly enough.", []string{"apply"}, nil),
		stepDef("WriteContainerdConfig", "containerd", "Containerd", "containerd", 10, "containerd.config.schema.json", "containerd.config", "public", "runtime", "Write the containerd config file on the node.", "Use this when the node runtime needs a managed containerd config.toml.", []string{"apply"}, []string{"path"}),
		stepDef("WriteContainerdRegistryHosts", "containerd", "Containerd", "containerd", 20, "containerd.registry-hosts.schema.json", "containerd.registry-hosts", "public", "runtime", "Write containerd registry host configuration for mirrors and trust policy.", "Use this when containerd should resolve pulls through explicit registry host configuration.", []string{"apply"}, []string{"path"}),
		stepDef("EnsureDirectory", "directory", "Directory", "directory", 10, "directory.schema.json", "directory", "public", "filesystem", "Ensure a directory exists with an optional mode.", "Use this before writing files or placing extracted content.", []string{"apply"}, []string{"path"}),
		stepDef("DownloadFile", "file", "File", "file", 10, "file.download.schema.json", "file.download", "public", "filesystem", "Download a file into prepared bundle storage.", "Use this during prepare to stage files into the bundle.", []string{"prepare"}, []string{"outputPath", "artifacts"}),
		stepDef("WriteFile", "file", "File", "file", 20, "file.write.schema.json", "file.write", "public", "filesystem", "Write inline or templated file content to a destination path.", "Use this to create or fully replace a managed file on the node.", []string{"apply"}, []string{"path"}),
		stepDef("CopyFile", "file", "File", "file", 30, "file.copy.schema.json", "file.copy", "public", "filesystem", "Copy a file from a declared source to a destination path.", "Use this to place a prepared or local file at its final location on the node.", []string{"apply"}, []string{"path"}),
		stepDef("EditFile", "file", "File", "file", 40, "file.edit.schema.json", "file.edit", "public", "filesystem", "Edit an existing file in place using ordered match rules.", "Use this for small in-place configuration edits when full file ownership is unnecessary.", []string{"apply"}, []string{"path"}),
		stepDef("ExtractArchive", "file", "File", "file", 50, "file.extract-archive.schema.json", "file.extract-archive", "public", "filesystem", "Extract an archive from a declared source into a destination directory.", "Use this when prepared tarballs or local archives should be expanded onto the node.", []string{"apply"}, []string{"path"}),
		stepDef("DownloadImage", "image", "Image", "image", 10, "image.download.schema.json", "image.download", "public", "containers", "Download container images into prepared bundle storage.", "Use this during prepare to collect required images for offline use.", []string{"prepare"}, []string{"artifacts"}),
		stepDef("LoadImage", "image", "Image", "image", 20, "image.load.schema.json", "image.load", "public", "containers", "Load prepared image archives into the local container runtime.", "Use this during apply before verifying or using images from an offline bundle.", []string{"apply"}, nil),
		stepDef("VerifyImage", "image", "Image", "image", 30, "image.verify.schema.json", "image.verify", "public", "containers", "Verify that required container images already exist on the node.", "Use this during apply when images should already be present and only need verification.", []string{"apply"}, nil),
		stepDef("KernelModule", "kernel-module", "KernelModule", "kernel-module", 10, "kernel-module.schema.json", "kernel-module", "public", "system", "Load and persist kernel modules.", "Use this for modules that must be present before networking or container runtime setup.", []string{"apply"}, []string{"name", "names"}),
		stepDef("CheckCluster", "cluster-check", "ClusterCheck", "cluster-check", 10, "cluster-check.schema.json", "cluster-check", "public", "kubernetes", "Poll and verify Kubernetes cluster health on the local node.", "Use this for typed bootstrap and upgrade verification instead of ad-hoc kubectl shell loops.", []string{"apply"}, nil),
		stepDef("InitKubeadm", "kubeadm", "Kubeadm", "kubeadm", 10, "kubeadm.init.schema.json", "kubeadm.init", "public", "kubernetes", "Run kubeadm init and write the join command to a file.", "Use this to bootstrap a control-plane node after host prerequisites are ready.", []string{"apply"}, []string{"joinFile"}),
		stepDef("JoinKubeadm", "kubeadm", "Kubeadm", "kubeadm", 20, "kubeadm.join.schema.json", "kubeadm.join", "public", "kubernetes", "Run kubeadm join for a worker or additional control-plane node.", "Use this after a bootstrap node has produced a valid join file or config.", []string{"apply"}, nil),
		stepDef("ResetKubeadm", "kubeadm", "Kubeadm", "kubeadm", 30, "kubeadm.reset.schema.json", "kubeadm.reset", "public", "kubernetes", "Run kubeadm reset and optional cleanup steps.", "Use this to tear down an existing kubeadm-managed node safely.", []string{"apply"}, nil),
		stepDef("UpgradeKubeadm", "kubeadm", "Kubeadm", "kubeadm", 40, "kubeadm.upgrade.schema.json", "kubeadm.upgrade", "public", "kubernetes", "Run kubeadm upgrade apply and optional kubelet restart.", "Use this to upgrade a local kubeadm-managed control-plane node with a typed workflow step.", []string{"apply"}, nil),
		stepDef("DownloadPackage", "package", "Package", "package", 10, "package.download.schema.json", "package.download", "public", "packages", "Download packages into prepared bundle storage.", "Use this during prepare to collect package-manager content for offline installation.", []string{"prepare"}, []string{"artifacts"}),
		stepDef("InstallPackage", "package", "Package", "package", 20, "package.install.schema.json", "package.install", "public", "packages", "Install packages on the local node.", "Use this during apply to install packages from configured local or mirrored repositories.", []string{"apply"}, nil),
		stepDef("ConfigureRepository", "repository", "Repository", "repository", 10, "repository.configure.schema.json", "repository.configure", "public", "packages", "Write deb or rpm repository definitions.", "Use this before refreshing caches or installing packages from a local mirror.", []string{"apply"}, []string{"path"}),
		stepDef("RefreshRepository", "repository", "Repository", "repository", 20, "repository.refresh.schema.json", "repository.refresh", "public", "packages", "Refresh package metadata with repo filtering.", "Use this after writing repo definitions and before package install steps that depend on fresh metadata.", []string{"apply"}, nil),
		stepDef("ManageService", "service", "Service", "service", 10, "service.schema.json", "service", "public", "system", "Start, stop, enable, disable, restart, or reload local services.", "Use this after config changes that need a service lifecycle action.", []string{"apply"}, []string{"name", "names"}),
		stepDef("Swap", "swap", "Swap", "swap", 10, "swap.schema.json", "swap", "public", "system", "Enable or disable swap and its persistence.", "Use this for Kubernetes-oriented host prep where swap policy matters.", []string{"apply"}, nil),
		stepDef("CreateSymlink", "symlink", "Symlink", "symlink", 10, "symlink.schema.json", "symlink", "public", "filesystem", "Create or replace a symbolic link.", "Use this when tools or runtimes expect a stable path alias.", []string{"apply"}, []string{"path"}),
		stepDef("Sysctl", "sysctl", "Sysctl", "sysctl", 10, "sysctl.schema.json", "sysctl", "public", "system", "Write and optionally apply sysctl values.", "Use this for kernel tunables that must survive reboot and may need immediate application.", []string{"apply"}, nil),
		stepDef("WriteSystemdUnit", "systemd-unit", "SystemdUnit", "systemd-unit", 10, "systemd-unit.schema.json", "systemd-unit", "public", "system", "Write a systemd unit file on the node.", "Use this when workflows need to install or override a custom unit definition.", []string{"apply"}, []string{"path"}),
		stepDef("WaitForService", "wait", "Wait", "wait", 10, "wait.service-active.schema.json", "wait.service-active", "public", "control-flow", "Wait until a systemd service reports active.", "Use this after service restarts or runtime configuration changes that take time to settle.", []string{"apply"}, nil),
		stepDef("WaitForCommand", "wait", "Wait", "wait", 20, "wait.command.schema.json", "wait.command", "public", "control-flow", "Wait until a command exits successfully.", "Use this when a dependent step should wait for a local command-based condition to succeed.", []string{"apply"}, nil),
		stepDef("WaitForFile", "wait", "Wait", "wait", 30, "wait.file-exists.schema.json", "wait.file-exists", "public", "control-flow", "Wait until a file or directory exists.", "Use this when a prior step produces a file that later steps depend on.", []string{"apply"}, nil),
		stepDef("WaitForMissingFile", "wait", "Wait", "wait", 40, "wait.file-absent.schema.json", "wait.file-absent", "public", "control-flow", "Wait until a file or directory is absent.", "Use this when a cleanup step needs to finish before later steps continue.", []string{"apply"}, nil),
		stepDef("WaitForTCPPort", "wait", "Wait", "wait", 50, "wait.tcp-port-open.schema.json", "wait.tcp-port-open", "public", "control-flow", "Wait until a TCP port opens.", "Use this when a service must become reachable before later steps continue.", []string{"apply"}, nil),
		stepDef("WaitForMissingTCPPort", "wait", "Wait", "wait", 60, "wait.tcp-port-closed.schema.json", "wait.tcp-port-closed", "public", "control-flow", "Wait until a TCP port closes.", "Use this when a process must fully stop before a later step continues.", []string{"apply"}, nil),
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
