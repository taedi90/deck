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
	case "CopyFile":
		if path := stringValue(rendered, "path"); path != "" {
			outputs["path"] = path
		}
	case "DownloadFile":
		path := stringValue(rendered, "outputPath")
		if path == "" {
			path = filepath.ToSlash(filepath.Join("files", inferDownloadFileName(stringValue(mapValue(rendered, "source"), "path"), stringValue(mapValue(rendered, "source"), "url"))))
		}
		if path != "" {
			outputs["outputPath"] = path
			outputs["artifacts"] = []string{path}
		}
	case "WriteFile", "EditFile", "ExtractArchive":
		if path := stringValue(mapValue(rendered, "output"), "path"); path != "" {
			outputs["path"] = path
		} else if path := stringValue(rendered, "path"); path != "" {
			outputs["path"] = path
		}
	case "EnsureDirectory", "CreateSymlink", "WriteSystemdUnit", "ConfigureRepository", "WriteContainerdConfig", "WriteContainerdRegistryHosts":
		if path := stringValue(rendered, "path"); path != "" {
			outputs["path"] = path
		}
	case "ManageService":
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
	case "InitKubeadm":
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
