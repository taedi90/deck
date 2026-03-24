package workflowexec

import (
	"strings"

	"github.com/Airgap-Castaways/deck/internal/workflowcontract"
)

type (
	StepDefinition = workflowcontract.StepDefinition
	StepTypeKey    = workflowcontract.StepTypeKey
)

func StepDefinitions() []StepDefinition {
	return workflowcontract.StepDefinitions()
}

func StepDefinitionForKey(key StepTypeKey) (StepDefinition, bool) {
	return workflowcontract.StepDefinitionForKey(key)
}

func ResolveStepTypeKey(workflowVersion, stepAPIVersion, kind string) (StepTypeKey, error) {
	workflowVersion = normalizeWorkflowVersionForStepResolution(workflowVersion)
	apiVersion, err := workflowcontract.ResolveStepAPIVersion(workflowVersion, stepAPIVersion)
	if err != nil {
		return StepTypeKey{}, err
	}
	return StepTypeKey{APIVersion: apiVersion, Kind: kind}, nil
}

func normalizeWorkflowVersionForStepResolution(version string) string {
	trimmed := strings.TrimSpace(version)
	if trimmed == "" || trimmed == "v1" {
		return workflowcontract.SupportedWorkflowVersion
	}
	return trimmed
}
