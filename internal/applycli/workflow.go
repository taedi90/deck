package applycli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/taedi90/deck/internal/config"
	"github.com/taedi90/deck/internal/workflowexec"
)

func BuildPrefetchWorkflow(wf *config.Workflow) *config.Workflow {
	if wf == nil {
		return &config.Workflow{}
	}
	prefetchSteps := make([]config.Step, 0)
	for _, phase := range wf.Phases {
		for _, step := range phase.Steps {
			if step.Kind == "File" && workflowexec.InferStepAction(step.Kind, step.Spec) == "download" {
				prefetchSteps = append(prefetchSteps, step)
			}
		}
	}
	if len(prefetchSteps) == 0 {
		return &config.Workflow{}
	}
	return &config.Workflow{
		Role:           "prepare",
		Version:        wf.Version,
		Vars:           wf.Vars,
		Phases:         []config.Phase{{Name: "prefetch", Steps: prefetchSteps}},
		StateKey:       wf.StateKey,
		WorkflowSHA256: wf.WorkflowSHA256,
	}
}

func BuildExecutionWorkflow(wf *config.Workflow, phaseName string) (*config.Workflow, error) {
	if wf == nil {
		return nil, errors.New("workflow is nil")
	}
	if strings.TrimSpace(phaseName) == "" {
		phases := make([]config.Phase, len(wf.Phases))
		copy(phases, wf.Phases)
		return &config.Workflow{
			Role:           wf.Role,
			Version:        wf.Version,
			Vars:           wf.Vars,
			Phases:         phases,
			StateKey:       wf.StateKey,
			WorkflowSHA256: wf.WorkflowSHA256,
		}, nil
	}
	selectedPhase, found := findWorkflowPhaseByName(wf, phaseName)
	if !found {
		return nil, fmt.Errorf("%s phase not found", phaseName)
	}
	return &config.Workflow{
		Role:           wf.Role,
		Version:        wf.Version,
		Vars:           wf.Vars,
		Phases:         []config.Phase{{Name: selectedPhase.Name, Steps: selectedPhase.Steps}},
		StateKey:       wf.StateKey,
		WorkflowSHA256: wf.WorkflowSHA256,
	}, nil
}

func findWorkflowPhaseByName(wf *config.Workflow, name string) (config.Phase, bool) {
	for _, phase := range wf.Phases {
		if phase.Name == name {
			return phase, true
		}
	}
	return config.Phase{}, false
}
