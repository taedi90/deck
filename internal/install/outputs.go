package install

import (
	"github.com/taedi90/deck/internal/config"
	"github.com/taedi90/deck/internal/workflowexec"
)

func stepOutputs(kind string, rendered map[string]any) map[string]any {
	outputs := map[string]any{}
	switch kind {
	case "FileFetch":
		if path := stringValue(mapValue(rendered, "output"), "path"); path != "" {
			outputs["path"] = path
			outputs["artifacts"] = []string{path}
		}
	case "Directory", "Symlink", "SystemdUnit", "Containerd":
		if path := stringValue(rendered, "path"); path != "" {
			outputs["path"] = path
		}
	case "Service":
		if name := stringValue(rendered, "name"); name != "" {
			outputs["name"] = name
		}
	case "KernelModule":
		if name := stringValue(rendered, "name"); name != "" {
			outputs["name"] = name
		}
		if names := stringSlice(rendered["names"]); len(names) > 0 {
			outputs["names"] = names
		}
	case "File":
		switch fileAction(rendered) {
		case "install", "edit":
			if path := stringValue(rendered, "path"); path != "" {
				outputs["path"] = path
			}
		case "copy":
			if dest := stringValue(rendered, "dest"); dest != "" {
				outputs["dest"] = dest
			}
		}
	case "Repository":
		if repositoryAction(rendered) == "configure" {
			if path := stringValue(rendered, "path"); path != "" {
				outputs["path"] = path
			}
		}
	case "Kubeadm":
		if kubeadmAction(rendered) == "init" {
			if joinFile := stringValue(rendered, "outputJoinFile"); joinFile != "" {
				outputs["joinFile"] = joinFile
			}
		}
	case "Artifacts", "Packages", "Image", "Wait", "PackageCache", "Sysctl", "Swap", "Command", "Inspection":
		// no register outputs
	default:
		if joinFile := stringValue(rendered, "outputJoinFile"); joinFile != "" {
			outputs["joinFile"] = joinFile
		}
	}
	return outputs
}

func applyRegister(step config.Step, rendered map[string]any, runtimeVars map[string]any) error {
	return workflowexec.ApplyRegister(step, stepOutputs(step.Kind, rendered), runtimeVars, errCodeRegisterOutputMissing)
}
