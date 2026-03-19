package workflowexec

import "github.com/taedi90/deck/internal/workflowcontract"

type (
	StepDefinition       = workflowcontract.StepDefinition
	StepActionDefinition = workflowcontract.StepActionDefinition
)

func StepDefinitions() []StepDefinition {
	return workflowcontract.StepDefinitions()
}

func StepDefinitionForKind(kind string) (StepDefinition, bool) {
	return workflowcontract.StepDefinitionForKind(kind)
}
