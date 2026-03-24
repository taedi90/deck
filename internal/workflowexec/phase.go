package workflowexec

import "github.com/Airgap-Castaways/deck/internal/config"

func FindPhase(wf *config.Workflow, name string) (config.Phase, bool) {
	if wf == nil {
		return config.Phase{}, false
	}
	for _, phase := range config.NormalizedPhases(wf) {
		if phase.Name == name {
			return phase, true
		}
	}
	return config.Phase{}, false
}
