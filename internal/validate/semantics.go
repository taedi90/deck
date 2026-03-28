package validate

import (
	"fmt"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/config"
	"github.com/Airgap-Castaways/deck/internal/workflowexpr"
)

func validateSemantics(name string, wf *config.Workflow) error {
	if err := validateRoleKinds(name, wf); err != nil {
		return err
	}
	if err := validatePhaseSemantics(wf, inferWorkflowMode(name, wf)); err != nil {
		return err
	}

	seenStepID := map[string]bool{}
	assignedRuntime := map[string]string{}

	for _, step := range workflowSteps(wf) {
		if _, hasLegacyAction := step.Spec["action"]; hasLegacyAction {
			return fmt.Errorf("E_SCHEMA_INVALID: step %s (%s): spec.action is no longer supported; move the operation into kind (for example `DownloadFile`)", step.ID, step.Kind)
		}
		if step.Kind == "ConfigureRepository" {
			if _, hasRefreshCache := step.Spec["refreshCache"]; hasRefreshCache {
				return fmt.Errorf("E_SCHEMA_INVALID: step %s (%s): spec.refreshCache is no longer supported; use a separate `RefreshRepository` step", step.ID, step.Kind)
			}
		}
		if output := literalPrepareOutputRoot(step); output != "" {
			if err := validatePrepareOutputRoot(step, output); err != nil {
				return err
			}
		}

		if strings.TrimSpace(step.When) != "" {
			if _, err := workflowexpr.CompileWhen(step.When); err != nil {
				return fmt.Errorf("E_SCHEMA_INVALID: step %s (%s): invalid when expression: %w", step.ID, step.Kind, err)
			}
		}
		if step.ID == "" {
			continue
		}
		if seenStepID[step.ID] {
			return fmt.Errorf("E_DUPLICATE_STEP_ID: %s", step.ID)
		}
		seenStepID[step.ID] = true

		for runtimeVar, outputKey := range step.Register {
			if !runtimeVarNamePattern.MatchString(runtimeVar) {
				return fmt.Errorf("E_REGISTER_VAR_INVALID: %s", runtimeVar)
			}
			if isReservedRuntimeVar(runtimeVar) {
				return fmt.Errorf("E_RUNTIME_VAR_RESERVED: %s", runtimeVar)
			}
			if strings.TrimSpace(outputKey) == "" {
				return fmt.Errorf("E_REGISTER_OUTPUT_NOT_FOUND: empty output key in step %s", step.ID)
			}
			if !isValidOutputKey(wf.Version, step, outputKey) {
				return fmt.Errorf("E_REGISTER_OUTPUT_NOT_FOUND: step %s (%s) has no output key %s", step.ID, step.Kind, outputKey)
			}
			if previous, exists := assignedRuntime[runtimeVar]; exists {
				return fmt.Errorf("E_RUNTIME_VAR_REDEFINED: %s (previous step: %s)", runtimeVar, previous)
			}
			assignedRuntime[runtimeVar] = step.ID
		}

		if step.Kind == "WaitForMissingFile" {
			nonEmpty, _ := step.Spec["nonEmpty"].(bool)
			if nonEmpty {
				return fmt.Errorf("E_SCHEMA_INVALID: step %s (wait.file-absent): nonEmpty is only valid for wait.file-exists", step.ID)
			}
		}
		if step.Kind == "CreateSymlink" {
			requireTarget, _ := step.Spec["requireTarget"].(bool)
			ignoreMissingTarget, _ := step.Spec["ignoreMissingTarget"].(bool)
			if requireTarget && ignoreMissingTarget {
				return fmt.Errorf("E_SCHEMA_INVALID: step %s (%s): requireTarget and ignoreMissingTarget cannot both be true", step.ID, step.Kind)
			}
		}
	}

	return nil
}
