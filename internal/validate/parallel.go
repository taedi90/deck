package validate

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/config"
	"github.com/Airgap-Castaways/deck/internal/workspacepaths"
)

func parallelApplyKindAllowed(kind string) bool {
	switch kind {
	case "Command", "CopyFile", "EnsureDirectory", "ExtractArchive", "WaitForCommand", "WaitForFile", "WaitForMissingFile", "WaitForService", "WaitForTCPPort", "WaitForMissingTCPPort", "WriteFile":
		return true
	default:
		return false
	}
}

func referencedRuntimeVars(step config.Step) []string {
	seen := map[string]bool{}
	for _, match := range runtimeWhenRefPattern.FindAllStringSubmatch(strings.TrimSpace(step.When), -1) {
		if len(match) == 2 {
			seen[match[1]] = true
		}
	}
	collectRuntimeTemplateRefs(step.Spec, seen)
	vars := make([]string, 0, len(seen))
	for key := range seen {
		vars = append(vars, key)
	}
	sort.Strings(vars)
	return vars
}

func collectRuntimeTemplateRefs(value any, seen map[string]bool) {
	switch typed := value.(type) {
	case string:
		for _, match := range runtimeTemplateRefPattern.FindAllStringSubmatch(typed, -1) {
			if len(match) == 2 {
				seen[match[1]] = true
			}
		}
	case map[string]any:
		for _, item := range typed {
			collectRuntimeTemplateRefs(item, seen)
		}
	case []any:
		for _, item := range typed {
			collectRuntimeTemplateRefs(item, seen)
		}
	}
}

func literalApplyTargetPath(step config.Step) string {
	if step.Kind == "WriteFile" || step.Kind == "CopyFile" || step.Kind == "EnsureDirectory" || step.Kind == "CreateSymlink" || step.Kind == "WriteContainerdConfig" || step.Kind == "WriteContainerdRegistryHosts" || step.Kind == "ConfigureRepository" || step.Kind == "EditTOML" || step.Kind == "EditYAML" || step.Kind == "EditJSON" {
		return stableLiteralPath(stringValue(step.Spec, "path"))
	}
	if step.Kind == "ExtractArchive" || step.Kind == "EditFile" || step.Kind == "WriteSystemdUnit" {
		if nested := mapValue(step.Spec, "output"); len(nested) > 0 {
			if path := stableLiteralPath(stringValue(nested, "path")); path != "" {
				return path
			}
		}
		return stableLiteralPath(stringValue(step.Spec, "path"))
	}
	return ""
}

func literalPrepareOutputRoot(step config.Step) string {
	switch step.Kind {
	case "DownloadPackage", "DownloadImage":
		return stableLiteralPath(stringValue(step.Spec, "outputDir"))
	case "DownloadFile":
		return stableLiteralPath(stringValue(step.Spec, "outputPath"))
	default:
		return ""
	}
}

func validatePrepareOutputRoot(step config.Step, output string) error {
	trimmed := strings.TrimSpace(output)
	switch step.Kind {
	case "DownloadFile":
		if workspacepaths.IsPreparedPathUnderRoot(trimmed, workspacepaths.PreparedFilesRoot) {
			return nil
		}
		return fmt.Errorf("E_PREPARE_OUTPUT_ROOT_INVALID: step %s (%s) outputPath must stay under %s/", step.ID, step.Kind, workspacepaths.PreparedFilesRoot)
	case "DownloadImage":
		if workspacepaths.IsPreparedPathUnderRoot(trimmed, workspacepaths.PreparedImagesRoot) {
			return nil
		}
		return fmt.Errorf("E_PREPARE_OUTPUT_ROOT_INVALID: step %s (%s) outputDir must stay under %s/", step.ID, step.Kind, workspacepaths.PreparedImagesRoot)
	case "DownloadPackage":
		if workspacepaths.IsPreparedPathUnderRoot(trimmed, workspacepaths.PreparedPackagesRoot) {
			return nil
		}
		return fmt.Errorf("E_PREPARE_OUTPUT_ROOT_INVALID: step %s (%s) outputDir must stay under %s/", step.ID, step.Kind, workspacepaths.PreparedPackagesRoot)
	default:
		return nil
	}
}
