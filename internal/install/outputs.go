package install

import (
	"os"
	"path/filepath"

	"github.com/Airgap-Castaways/deck/internal/config"
	"github.com/Airgap-Castaways/deck/internal/workflowexec"
)

func stepOutputs(kind string, rendered map[string]any) map[string]any {
	outputs := map[string]any{}
	switch kind {
	case "CopyFile":
		if path := stringValue(rendered, "path"); path != "" {
			outputs["path"] = path
		}
	case "DownloadFile":
		if items := mapItems(rendered["items"]); len(items) > 0 {
			paths := make([]string, 0, len(items))
			for _, item := range items {
				path := stringValue(item, "outputPath")
				if path == "" {
					path = filepath.ToSlash(filepath.Join("files", inferDownloadFileName(stringValue(mapValue(item, "source"), "path"), stringValue(mapValue(item, "source"), "url"))))
				}
				if path != "" {
					paths = append(paths, path)
				}
			}
			if len(paths) > 0 {
				outputs["artifacts"] = paths
				outputs["outputPaths"] = paths
				if len(paths) == 1 {
					outputs["outputPath"] = paths[0]
				}
			}
			break
		}
		path := stringValue(rendered, "outputPath")
		if path == "" {
			path = filepath.ToSlash(filepath.Join("files", inferDownloadFileName(stringValue(mapValue(rendered, "source"), "path"), stringValue(mapValue(rendered, "source"), "url"))))
		}
		if path != "" {
			outputs["outputPath"] = path
			outputs["artifacts"] = []string{path}
			outputs["outputPaths"] = []string{path}
		}
	case "WriteFile", "EditFile", "EditTOML", "EditYAML", "EditJSON", "ExtractArchive":
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

func mapItems(v any) []map[string]any {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		mapped, ok := item.(map[string]any)
		if !ok || mapped == nil {
			continue
		}
		out = append(out, mapped)
	}
	return out
}

func applyRegister(step config.Step, rendered map[string]any, outputs map[string]any, runtimeVars map[string]any) error {
	merged := stepOutputs(step.Kind, rendered)
	for key, value := range outputs {
		merged[key] = value
	}
	return workflowexec.ApplyRegister(step, merged, runtimeVars, errCodeRegisterOutputMissing)
}
