package workflowcontract

import "sort"

type StepDefinition struct {
	Kind                string
	SchemaFile          string
	ToolSchemaGenerator string
	Visibility          string
	Category            string
	Summary             string
	WhenToUse           string
	Roles               []string
	Outputs             []string
	Actions             []StepActionDefinition
}

type StepActionDefinition struct {
	Name    string
	Roles   []string
	Outputs []string
	Fields  []string
}

func StepDefinitions() []StepDefinition {
	defs := []StepDefinition{
		stepDefWithCategory("Checks", "checks.schema.json", "public", "prepare", "Run host checks before prepare execution.", "Use this at the start of prepare workflows to fail early on unsupported hosts.", []string{"prepare"}, []string{"passed", "failedChecks"}),
		stepDefWithCategory("Artifacts", "artifacts.schema.json", "public", "apply", "Install or extract pre-collected artifacts on the node.", "Use this when your apply workflow needs binaries or archives that were prepared into the bundle.", []string{"apply"}, nil),
		stepDefWithCategory("Packages", "packages.schema.json", "public", "packages", "Download or install package sets.", "Use this for package-manager driven dependencies instead of shelling out directly.", []string{"prepare", "apply"}, nil,
			actionDefWithFields("download", []string{"prepare"}, []string{"artifacts"}, []string{"action", "packages", "distro", "repo", "backend", "output"}),
			actionDefWithFields("install", []string{"apply"}, nil, []string{"action", "packages", "source", "restrictToRepos", "excludeRepos"}),
		),
		stepDefWithCategory("Directory", "directory.schema.json", "public", "filesystem", "Ensure a directory exists with an optional mode.", "Use this before writing files or placing extracted content.", []string{"apply"}, []string{"path"}),
		stepDefWithCategory("Symlink", "symlink.schema.json", "public", "filesystem", "Create or replace a symbolic link.", "Use this when tools or runtimes expect a stable path alias.", []string{"apply"}, []string{"path"}),
		stepDefWithCategory("SystemdUnit", "systemd-unit.schema.json", "public", "system", "Write a systemd unit file and optionally manage the service.", "Use this when workflows need to install or override a custom unit definition.", []string{"apply"}, []string{"path"}),
		stepDefWithCategory("Containerd", "containerd.schema.json", "public", "runtime", "Write containerd config and registry host settings.", "Use this when the node runtime needs a default config or registry mirror layout.", []string{"apply"}, []string{"path"}),
		stepDefWithCategory("PackageCache", "package-cache.schema.json", "public", "packages", "Refresh package metadata with repo filtering.", "Use this after writing repo definitions and before package install steps that depend on fresh metadata.", []string{"apply"}, nil),
		stepDefWithCategory("Swap", "swap.schema.json", "public", "system", "Enable or disable swap and its persistence.", "Use this for Kubernetes-oriented host prep where swap policy matters.", []string{"apply"}, nil),
		stepDefWithCategory("KernelModule", "kernel-module.schema.json", "public", "system", "Load and persist kernel modules.", "Use this for modules that must be present before networking or container runtime setup.", []string{"apply"}, []string{"name", "names"}),
		stepDefWithCategory("Command", "command.schema.json", "public", "advanced", "Run an explicit command as an escape hatch.", "Use this only when no typed step expresses the change clearly enough.", []string{"apply"}, nil),
		stepDefWithCategory("Service", "service.schema.json", "public", "system", "Start, stop, enable, or reload local services.", "Use this after config changes that need a service lifecycle action.", []string{"apply"}, []string{"name", "names"}),
		stepDefWithCategory("Sysctl", "sysctl.schema.json", "public", "system", "Write and optionally apply sysctl values.", "Use this for kernel tunables that must survive reboot and may need immediate application.", []string{"apply"}, nil),
		stepDefWithCategory("File", "file.schema.json", "public", "filesystem", "Manage files through write, copy, edit, or download actions.", "Use this for most file-oriented host changes instead of generic commands.", []string{"prepare", "apply"}, nil,
			actionDefWithFields("download", []string{"prepare", "apply"}, []string{"path", "artifacts"}, []string{"action", "source", "fetch", "output"}),
			actionDefWithFields("write", []string{"apply"}, []string{"path"}, []string{"action", "path", "content", "contentFromTemplate", "mode"}),
			actionDefWithFields("copy", []string{"apply"}, []string{"dest"}, []string{"action", "src", "dest", "mode"}),
			actionDefWithFields("edit", []string{"apply"}, []string{"path"}, []string{"action", "path", "backup", "edits", "mode"}),
		),
		stepDefWithCategory("Repository", "repository.schema.json", "public", "packages", "Configure apt or yum repository definitions.", "Use this before refreshing caches or installing packages from a local mirror.", []string{"apply"}, nil,
			actionDefWithFields("configure", []string{"apply"}, []string{"path"}, []string{"action", "format", "path", "mode", "replaceExisting", "disableExisting", "backupPaths", "cleanupPaths", "refreshCache", "repositories", "timeout"}),
		),
		stepDefWithCategory("Image", "image.schema.json", "public", "containers", "Download or verify container images.", "Use this when workflows need image presence checks or bundle-time image collection.", []string{"prepare", "apply"}, nil,
			actionDefWithFields("download", []string{"prepare"}, []string{"artifacts"}, []string{"action", "images", "auth", "backend", "output"}),
			actionDefWithFields("verify", []string{"apply"}, nil, []string{"action", "images", "command"}),
		),
		stepDefWithCategory("Wait", "wait.schema.json", "public", "control-flow", "Wait for command, service, file, or port conditions.", "Use this between dependent steps when the host needs time to converge after a change.", []string{"apply"}, nil,
			actionDefWithFields("serviceActive", []string{"apply"}, nil, []string{"action", "interval", "initialDelay", "name", "timeout", "pollInterval"}),
			actionDefWithFields("commandSuccess", []string{"apply"}, nil, []string{"action", "interval", "initialDelay", "command", "timeout", "pollInterval"}),
			actionDefWithFields("fileExists", []string{"apply"}, nil, []string{"action", "interval", "initialDelay", "path", "type", "nonEmpty", "timeout", "pollInterval"}),
			actionDefWithFields("fileAbsent", []string{"apply"}, nil, []string{"action", "interval", "initialDelay", "path", "type", "timeout", "pollInterval"}),
			actionDefWithFields("tcpPortClosed", []string{"apply"}, nil, []string{"action", "interval", "initialDelay", "address", "port", "timeout", "pollInterval"}),
			actionDefWithFields("tcpPortOpen", []string{"apply"}, nil, []string{"action", "interval", "initialDelay", "address", "port", "timeout", "pollInterval"}),
		),
		stepDefWithCategory("Kubeadm", "kubeadm.schema.json", "public", "kubernetes", "Run kubeadm init, join, or reset actions.", "Use this for bootstrap lifecycle steps after host prerequisites are ready.", []string{"apply"}, nil,
			actionDefWithFields("init", []string{"apply"}, []string{"joinFile"}, []string{"action", "configFile", "configTemplate", "pullImages", "outputJoinFile", "kubernetesVersion", "advertiseAddress", "podNetworkCIDR", "criSocket", "ignorePreflightErrors", "extraArgs", "skipIfAdminConfExists"}),
			actionDefWithFields("join", []string{"apply"}, nil, []string{"action", "configFile", "joinFile", "asControlPlane", "extraArgs"}),
			actionDefWithFields("reset", []string{"apply"}, nil, []string{"action", "force", "ignoreErrors", "stopKubelet", "criSocket", "extraArgs", "removePaths", "removeFiles", "cleanupContainers", "restartRuntimeService"}),
		),
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

func stepDefWithCategory(kind, schemaFile, visibility, category, summary, whenToUse string, roles, outputs []string, actions ...StepActionDefinition) StepDefinition {
	def := StepDefinition{
		Kind:                kind,
		SchemaFile:          schemaFile,
		ToolSchemaGenerator: kind,
		Visibility:          visibility,
		Category:            category,
		Summary:             summary,
		WhenToUse:           whenToUse,
		Roles:               append([]string(nil), roles...),
		Outputs:             append([]string(nil), outputs...),
		Actions:             append([]StepActionDefinition(nil), actions...),
	}
	sort.Strings(def.Roles)
	sort.Strings(def.Outputs)
	sort.Slice(def.Actions, func(i, j int) bool { return def.Actions[i].Name < def.Actions[j].Name })
	return def
}

func actionDefWithFields(name string, roles, outputs, fields []string) StepActionDefinition {
	def := StepActionDefinition{Name: name, Roles: append([]string(nil), roles...), Outputs: append([]string(nil), outputs...), Fields: append([]string(nil), fields...)}
	sort.Strings(def.Roles)
	sort.Strings(def.Outputs)
	sort.Strings(def.Fields)
	return def
}
