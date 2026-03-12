package install

import (
	"github.com/taedi90/deck/internal/config"
	"github.com/taedi90/deck/internal/workflowexec"
)

func stepOutputs(kind string, rendered map[string]any) map[string]any {
	outputs := map[string]any{}
	switch kind {
	case "DownloadFile":
		if path := stringValue(mapValue(rendered, "output"), "path"); path != "" {
			outputs["path"] = path
		}
	case "WriteFile":
		if path := stringValue(rendered, "path"); path != "" {
			outputs["path"] = path
		}
	case "CopyFile":
		if dest := stringValue(rendered, "dest"); dest != "" {
			outputs["dest"] = dest
		}
	case "EnsureDir", "Symlink", "InstallFile", "TemplateFile", "SystemdUnit", "RepoConfig", "ContainerdConfig":
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
	case "KubeadmInit":
		if joinFile := stringValue(rendered, "outputJoinFile"); joinFile != "" {
			outputs["joinFile"] = joinFile
		}
	}
	return outputs
}

func applyRegister(step config.Step, rendered map[string]any, runtimeVars map[string]any) error {
	return workflowexec.ApplyRegister(step, stepOutputs(step.Kind, rendered), runtimeVars, errCodeRegisterOutputMissing)
}
