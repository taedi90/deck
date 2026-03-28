package validate

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/config"
	"github.com/Airgap-Castaways/deck/internal/workflowcontract"
	"github.com/Airgap-Castaways/deck/internal/workflowexec"
)

func stableLiteralPath(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.Contains(trimmed, "{{") {
		return ""
	}
	return filepath.ToSlash(trimmed)
}

func validateRoleKinds(name string, wf *config.Workflow) error {
	role := inferWorkflowMode(name, wf)
	if role == "" {
		return nil
	}
	for _, step := range workflowSteps(wf) {
		key, err := effectiveStepTypeKey(wf.Version, step)
		if err != nil {
			return err
		}
		if workflowexec.StepAllowedForRoleForKey(role, key) {
			continue
		}
		return fmt.Errorf("E_KIND_ROLE_MISMATCH: step %s (%s) is not supported for role %s", step.ID, step.Kind, role)
	}
	return nil
}

func inferWorkflowMode(name string, wf *config.Workflow) string {
	trimmed := filepath.ToSlash(strings.TrimSpace(name))
	if filepath.Base(trimmed) == "prepare.yaml" {
		return "prepare"
	}
	if strings.Contains(trimmed, "/workflows/scenarios/") || strings.HasPrefix(trimmed, "workflows/scenarios/") {
		return "apply"
	}
	seenPrepare := false
	seenApply := false
	for _, step := range workflowSteps(wf) {
		key, err := effectiveStepTypeKey(wf.Version, step)
		if err != nil {
			continue
		}
		if workflowexec.StepAllowedForRoleForKey("prepare", key) {
			seenPrepare = true
		}
		if workflowexec.StepAllowedForRoleForKey("apply", key) {
			seenApply = true
		}
	}
	if seenPrepare && !seenApply {
		return "prepare"
	}
	if seenApply && !seenPrepare {
		return "apply"
	}
	return ""
}

func isReservedRuntimeVar(runtimeVar string) bool {
	trimmed := strings.TrimSpace(runtimeVar)
	return trimmed == "host" || strings.HasPrefix(trimmed, "host.")
}

func isValidOutputKey(workflowVersion string, step config.Step, outputKey string) bool {
	key, err := effectiveStepTypeKey(workflowVersion, step)
	if err != nil {
		return false
	}
	return workflowexec.StepHasOutputForKey(key, outputKey)
}

func effectiveStepTypeKey(workflowVersion string, step config.Step) (workflowexec.StepTypeKey, error) {
	apiVersion, err := workflowcontract.ResolveStepAPIVersion(workflowVersion, step.APIVersion)
	if err != nil {
		return workflowexec.StepTypeKey{}, fmt.Errorf("E_SCHEMA_INVALID: step %s (%s): %w", step.ID, step.Kind, err)
	}
	return workflowexec.StepTypeKey{APIVersion: strings.TrimSpace(apiVersion), Kind: strings.TrimSpace(step.Kind)}, nil
}

func workflowSteps(wf *config.Workflow) []config.Step {
	steps := make([]config.Step, 0)
	for _, phase := range config.NormalizedPhases(wf) {
		steps = append(steps, phase.Steps...)
	}
	return steps
}

func stringValue(v map[string]any, key string) string {
	if v == nil {
		return ""
	}
	raw, ok := v[key]
	if !ok {
		return ""
	}
	s, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func mapValue(v map[string]any, key string) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	raw, ok := v[key]
	if !ok {
		return map[string]any{}
	}
	m, ok := raw.(map[string]any)
	if !ok || m == nil {
		return map[string]any{}
	}
	return m
}

func validateSingleBraceTemplates(name string, content []byte) error {
	lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	for idx, line := range lines {
		matches := singleBraceTemplatePattern.FindStringSubmatch(line)
		if len(matches) < 3 {
			continue
		}
		return fmt.Errorf("E_TEMPLATE_SINGLE_BRACE: %s:%d: unsupported single-brace template %s", name, idx+1, strings.TrimSpace(matches[2]))
	}

	return nil
}

func normalizeYAMLForJSON(value any) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for k, v := range typed {
			normalized, err := normalizeYAMLForJSON(v)
			if err != nil {
				return nil, err
			}
			out[k] = normalized
		}
		return out, nil
	case map[any]any:
		out := make(map[string]any, len(typed))
		for k, v := range typed {
			key, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("non-string map key type %T", k)
			}
			normalized, err := normalizeYAMLForJSON(v)
			if err != nil {
				return nil, err
			}
			out[key] = normalized
		}
		return out, nil
	case []any:
		out := make([]any, len(typed))
		for i := range typed {
			normalized, err := normalizeYAMLForJSON(typed[i])
			if err != nil {
				return nil, err
			}
			out[i] = normalized
		}
		return out, nil
	default:
		return typed, nil
	}
}
