package install

import (
	"fmt"
	"strings"

	"github.com/taedi90/deck/internal/config"
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
	case "EnsureDir", "InstallFile", "TemplateFile", "RepoConfig", "ContainerdConfig":
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
	if len(step.Register) == 0 {
		return nil
	}
	outputs := stepOutputs(step.Kind, rendered)
	for runtimeKey, outputKey := range step.Register {
		if isReservedRuntimeVar(runtimeKey) {
			return fmt.Errorf("E_RUNTIME_VAR_RESERVED: %s", runtimeKey)
		}
		v, ok := outputs[outputKey]
		if !ok {
			return fmt.Errorf("%s: step %s kind %s has no output key %s", errCodeRegisterOutputMissing, step.ID, step.Kind, outputKey)
		}
		runtimeVars[runtimeKey] = v
	}
	return nil
}

func isReservedRuntimeVar(runtimeKey string) bool {
	trimmed := strings.TrimSpace(runtimeKey)
	return trimmed == "host" || strings.HasPrefix(trimmed, "host.")
}
