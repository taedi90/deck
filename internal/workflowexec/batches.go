package workflowexec

import (
	"strings"

	"github.com/Airgap-Castaways/deck/internal/config"
)

type StepBatch struct {
	PhaseName      string
	ParallelGroup  string
	MaxParallelism int
	Steps          []config.Step
}

func (b StepBatch) Parallel() bool {
	return strings.TrimSpace(b.ParallelGroup) != "" && len(b.Steps) > 1
}

func BuildPhaseBatches(phase config.Phase) []StepBatch {
	if len(phase.Steps) == 0 {
		return nil
	}
	batches := make([]StepBatch, 0, len(phase.Steps))
	for _, step := range phase.Steps {
		group := strings.TrimSpace(step.ParallelGroup)
		if group == "" {
			batches = append(batches, StepBatch{PhaseName: phase.Name, MaxParallelism: phase.MaxParallelism, Steps: []config.Step{step}})
			continue
		}
		if len(batches) > 0 && batches[len(batches)-1].ParallelGroup == group {
			batches[len(batches)-1].Steps = append(batches[len(batches)-1].Steps, step)
			continue
		}
		batches = append(batches, StepBatch{PhaseName: phase.Name, ParallelGroup: group, MaxParallelism: phase.MaxParallelism, Steps: []config.Step{step}})
	}
	return batches
}
