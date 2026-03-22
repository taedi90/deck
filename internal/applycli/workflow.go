package applycli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/taedi90/deck/internal/config"
)

func BuildPrefetchWorkflow(wf *config.Workflow) *config.Workflow {
	if wf == nil {
		return &config.Workflow{}
	}
	prefetchSteps := make([]config.Step, 0)
	for _, phase := range config.NormalizedPhases(wf) {
		for _, step := range phase.Steps {
			if step.Kind == "DownloadFile" {
				prefetchSteps = append(prefetchSteps, step)
			}
		}
	}
	if len(prefetchSteps) == 0 {
		return &config.Workflow{}
	}
	return &config.Workflow{
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
	phases := config.NormalizedPhases(wf)
	if strings.TrimSpace(phaseName) == "" {
		selectedPhases := make([]config.Phase, len(phases))
		copy(selectedPhases, phases)
		return &config.Workflow{
			Version:        wf.Version,
			Vars:           wf.Vars,
			Phases:         selectedPhases,
			StateKey:       wf.StateKey,
			WorkflowSHA256: wf.WorkflowSHA256,
		}, nil
	}
	selectedPhase, found := findWorkflowPhaseByName(phases, phaseName)
	if !found {
		return nil, fmt.Errorf("%s phase not found", phaseName)
	}
	return &config.Workflow{
		Version:        wf.Version,
		Vars:           wf.Vars,
		Phases:         []config.Phase{{Name: selectedPhase.Name, MaxParallelism: selectedPhase.MaxParallelism, Steps: selectedPhase.Steps}},
		StateKey:       wf.StateKey,
		WorkflowSHA256: wf.WorkflowSHA256,
	}, nil
}

func findWorkflowPhaseByName(phases []config.Phase, name string) (config.Phase, bool) {
	for _, phase := range phases {
		if phase.Name == name {
			return phase, true
		}
	}
	return config.Phase{}, false
}
