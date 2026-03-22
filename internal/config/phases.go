package config

const ImplicitPhaseName = "default"

func NormalizedPhases(wf *Workflow) []Phase {
	if wf == nil {
		return nil
	}
	if len(wf.Phases) > 0 {
		phases := make([]Phase, len(wf.Phases))
		copy(phases, wf.Phases)
		return phases
	}
	if len(wf.Steps) == 0 {
		return nil
	}
	steps := make([]Step, len(wf.Steps))
	copy(steps, wf.Steps)
	return []Phase{{Name: ImplicitPhaseName, Steps: steps}}
}
