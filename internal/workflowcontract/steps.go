package workflowcontract

import "sort"

type StepDefinition struct {
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

func StepDefinitions() []StepDefinition {
	defs := []StepDefinition{
		stepDef("Artifacts", "artifacts", "Artifacts", "artifacts", 10, "artifacts.schema.json", "artifacts", "public", "apply", "Install or extract pre-collected artifacts on the node.", "Use this when your apply workflow needs binaries or archives that were prepared into the bundle.", []string{"apply"}, nil),
		stepDef("Checks", "checks", "Checks", "checks", 10, "checks.schema.json", "checks", "public", "prepare", "Run host checks before prepare execution.", "Use this at the start of prepare workflows to fail early on unsupported hosts.", []string{"prepare"}, []string{"passed", "failedChecks"}),
		stepDef("Command", "command", "Command", "command", 10, "command.schema.json", "command", "public", "advanced", "Run an explicit command as an escape hatch.", "Use this only when no typed step expresses the change clearly enough.", []string{"apply"}, nil),
		stepDef("Containerd", "containerd", "Containerd", "containerd", 10, "containerd.schema.json", "containerd", "public", "runtime", "Write containerd config and registry host settings.", "Use this when the node runtime needs a default config or registry mirror layout.", []string{"apply"}, []string{"path"}),
		stepDef("Directory", "directory", "Directory", "directory", 10, "directory.schema.json", "directory", "public", "filesystem", "Ensure a directory exists with an optional mode.", "Use this before writing files or placing extracted content.", []string{"apply"}, []string{"path"}),
		stepDef("FileDownload", "file", "File", "file", 10, "file.download.schema.json", "file.download", "public", "filesystem", "Download or stage a file into bundle output storage.", "Use this during prepare, or when apply needs to stage a bundle file for later steps.", []string{"prepare", "apply"}, []string{"path", "artifacts"}),
		stepDef("FileWrite", "file", "File", "file", 20, "file.write.schema.json", "file.write", "public", "filesystem", "Write inline or templated file content to a destination path.", "Use this to create or fully replace a managed file on the node.", []string{"apply"}, []string{"path"}),
		stepDef("FileCopy", "file", "File", "file", 30, "file.copy.schema.json", "file.copy", "public", "filesystem", "Copy a file already present on the node to another path.", "Use this when a workflow needs to duplicate a local file into its final location.", []string{"apply"}, []string{"dest"}),
		stepDef("FileEdit", "file", "File", "file", 40, "file.edit.schema.json", "file.edit", "public", "filesystem", "Edit an existing file in place using ordered match rules.", "Use this for small in-place configuration edits when full file ownership is unnecessary.", []string{"apply"}, []string{"path"}),
		stepDef("ImageDownload", "image", "Image", "image", 10, "image.download.schema.json", "image.download", "public", "containers", "Download container images into the bundle.", "Use this during prepare to collect required images for offline use.", []string{"prepare"}, []string{"artifacts"}),
		stepDef("ImageVerify", "image", "Image", "image", 20, "image.verify.schema.json", "image.verify", "public", "containers", "Verify that required container images already exist on the node.", "Use this during apply when images should already be present and only need verification.", []string{"apply"}, nil),
		stepDef("KernelModule", "kernel-module", "KernelModule", "kernel-module", 10, "kernel-module.schema.json", "kernel-module", "public", "system", "Load and persist kernel modules.", "Use this for modules that must be present before networking or container runtime setup.", []string{"apply"}, []string{"name", "names"}),
		stepDef("KubeadmInit", "kubeadm", "Kubeadm", "kubeadm", 10, "kubeadm.init.schema.json", "kubeadm.init", "public", "kubernetes", "Run `kubeadm init` and write the join command to a file.", "Use this to bootstrap a control-plane node after host prerequisites are ready.", []string{"apply"}, []string{"joinFile"}),
		stepDef("KubeadmJoin", "kubeadm", "Kubeadm", "kubeadm", 20, "kubeadm.join.schema.json", "kubeadm.join", "public", "kubernetes", "Run `kubeadm join` for a worker or additional control-plane node.", "Use this after a bootstrap node has produced a valid join file or config.", []string{"apply"}, nil),
		stepDef("KubeadmReset", "kubeadm", "Kubeadm", "kubeadm", 30, "kubeadm.reset.schema.json", "kubeadm.reset", "public", "kubernetes", "Run `kubeadm reset` and optional cleanup steps.", "Use this to tear down an existing kubeadm-managed node safely.", []string{"apply"}, nil),
		stepDef("PackageCache", "package-cache", "PackageCache", "package-cache", 10, "package-cache.schema.json", "package-cache", "public", "packages", "Refresh package metadata with repo filtering.", "Use this after writing repo definitions and before package install steps that depend on fresh metadata.", []string{"apply"}, nil),
		stepDef("PackagesDownload", "packages", "Packages", "packages", 10, "packages.download.schema.json", "packages.download", "public", "packages", "Download packages into the bundle.", "Use this during prepare to collect package-manager content for offline installation.", []string{"prepare"}, []string{"artifacts"}),
		stepDef("PackagesInstall", "packages", "Packages", "packages", 20, "packages.install.schema.json", "packages.install", "public", "packages", "Install package sets on the local node.", "Use this during apply to install packages from configured local or mirrored repositories.", []string{"apply"}, nil),
		stepDef("Repository", "repository", "Repository", "repository", 10, "repository.schema.json", "repository", "public", "packages", "Configure apt or yum repository definitions.", "Use this before refreshing caches or installing packages from a local mirror.", []string{"apply"}, []string{"path"}),
		stepDef("Service", "service", "Service", "service", 10, "service.schema.json", "service", "public", "system", "Start, stop, enable, or reload local services.", "Use this after config changes that need a service lifecycle action.", []string{"apply"}, []string{"name", "names"}),
		stepDef("Swap", "swap", "Swap", "swap", 10, "swap.schema.json", "swap", "public", "system", "Enable or disable swap and its persistence.", "Use this for Kubernetes-oriented host prep where swap policy matters.", []string{"apply"}, nil),
		stepDef("Symlink", "symlink", "Symlink", "symlink", 10, "symlink.schema.json", "symlink", "public", "filesystem", "Create or replace a symbolic link.", "Use this when tools or runtimes expect a stable path alias.", []string{"apply"}, []string{"path"}),
		stepDef("Sysctl", "sysctl", "Sysctl", "sysctl", 10, "sysctl.schema.json", "sysctl", "public", "system", "Write and optionally apply sysctl values.", "Use this for kernel tunables that must survive reboot and may need immediate application.", []string{"apply"}, nil),
		stepDef("SystemdUnit", "systemd-unit", "SystemdUnit", "systemd-unit", 10, "systemd-unit.schema.json", "systemd-unit", "public", "system", "Write a systemd unit file and optionally manage the service.", "Use this when workflows need to install or override a custom unit definition.", []string{"apply"}, []string{"path"}),
		stepDef("WaitServiceActive", "wait", "Wait", "wait", 10, "wait.service-active.schema.json", "wait.service-active", "public", "control-flow", "Wait until a systemd service reports active.", "Use this after service restarts or runtime configuration changes that take time to settle.", []string{"apply"}, nil),
		stepDef("WaitCommandSuccess", "wait", "Wait", "wait", 20, "wait.command-success.schema.json", "wait.command-success", "public", "control-flow", "Wait until a command exits successfully.", "Use this when a dependent step should wait for a local command-based condition to succeed.", []string{"apply"}, nil),
		stepDef("WaitFileExists", "wait", "Wait", "wait", 30, "wait.file-exists.schema.json", "wait.file-exists", "public", "control-flow", "Wait until a file or directory exists.", "Use this when a prior step produces a file that later steps depend on.", []string{"apply"}, nil),
		stepDef("WaitFileAbsent", "wait", "Wait", "wait", 40, "wait.file-absent.schema.json", "wait.file-absent", "public", "control-flow", "Wait until a file or directory is absent.", "Use this when a cleanup step needs to finish before later steps continue.", []string{"apply"}, nil),
		stepDef("WaitTCPPortOpen", "wait", "Wait", "wait", 50, "wait.tcp-port-open.schema.json", "wait.tcp-port-open", "public", "control-flow", "Wait until a TCP port opens.", "Use this when a service must become reachable before later steps continue.", []string{"apply"}, nil),
		stepDef("WaitTCPPortClosed", "wait", "Wait", "wait", 60, "wait.tcp-port-closed.schema.json", "wait.tcp-port-closed", "public", "control-flow", "Wait until a TCP port closes.", "Use this when a process must fully stop before a later step continues.", []string{"apply"}, nil),
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Kind < defs[j].Kind })
	return defs
}

func StepDefinitionForKind(kind string) (StepDefinition, bool) {
	for _, def := range StepDefinitions() {
		if def.Kind == kind {
			return def, true
		}
	}
	return StepDefinition{}, false
}

func stepDef(kind, family, familyTitle, docsPage string, docsOrder int, schemaFile, generator, visibility, category, summary, whenToUse string, roles, outputs []string) StepDefinition {
	def := StepDefinition{
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
