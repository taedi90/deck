package install

import (
	"os"
	"path/filepath"

	"github.com/taedi90/deck/internal/config"
	"github.com/taedi90/deck/internal/workflowexec"
)

func stepOutputs(kind string, rendered map[string]any) map[string]any {
	outputs := map[string]any{}
	switch kind {
	case "FileCopy":
		if dest := stringValue(rendered, "dest"); dest != "" {
			outputs["dest"] = dest
		}
	case "FileDownload":
		path := stringValue(mapValue(rendered, "output"), "path")
		if path == "" {
			path = filepath.ToSlash(filepath.Join("files", inferDownloadFileName(stringValue(mapValue(rendered, "source"), "path"), stringValue(mapValue(rendered, "source"), "url"))))
		}
		if path != "" {
			outputs["path"] = path
			outputs["artifacts"] = []string{path}
		}
	case "FileWrite", "FileEdit":
		if path := stringValue(mapValue(rendered, "output"), "path"); path != "" {
			outputs["path"] = path
		} else if path := stringValue(rendered, "path"); path != "" {
			outputs["path"] = path
		}
	case "Directory", "Symlink", "SystemdUnit", "Repository", "Containerd":
		if path := stringValue(rendered, "path"); path != "" {
			outputs["path"] = path
		}
	case "Service":
		if name := stringValue(rendered, "name"); name != "" {
			outputs["name"] = name
		} else if names := stringSlice(rendered["names"]); len(names) > 0 {
			outputs["names"] = names
		}
	case "KernelModule":
		if name := stringValue(rendered, "name"); name != "" {
			outputs["name"] = name
		} else if names := stringSlice(rendered["names"]); len(names) > 0 {
			outputs["names"] = names
		}
	case "KubeadmInit":
		if joinFile := stringValue(rendered, "outputJoinFile"); joinFile != "" {
			if _, err := os.Stat(joinFile); err == nil {
				outputs["joinFile"] = joinFile
			}
		}
	}
	return outputs
}

func applyRegister(step config.Step, rendered map[string]any, runtimeVars map[string]any) error {
	return workflowexec.ApplyRegister(step, stepOutputs(step.Kind, rendered), runtimeVars, errCodeRegisterOutputMissing)
}
