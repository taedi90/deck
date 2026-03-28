package askcontext

import "testing"

func TestDiscoverCandidateStepsWithOptionsKeepsBootstrapKindsVisible(t *testing.T) {
	selected := DiscoverCandidateStepsWithOptions("create an air-gapped rhel9 single-node kubeadm workflow", StepGuidanceOptions{ModeIntent: "apply-only", Topology: "single-node", RequiredCapabilities: []string{"kubeadm-bootstrap", "cluster-verification"}})
	seen := map[string]bool{}
	for _, item := range selected {
		seen[item.Step.Kind] = true
	}
	for _, want := range []string{"InitKubeadm", "CheckHost", "CheckCluster"} {
		if !seen[want] {
			t.Fatalf("expected candidate %s, got %#v", want, selected)
		}
	}
}
