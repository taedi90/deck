package askintent

import "testing"

func TestClassifyClarifyForLowInfoPrompt(t *testing.T) {
	decision := Classify(Input{Prompt: "test"})
	if decision.Route != RouteClarify {
		t.Fatalf("expected clarify route, got %s", decision.Route)
	}
	if decision.AllowGeneration {
		t.Fatalf("clarify route must not allow generation")
	}
}

func TestClassifyDraftAndRefine(t *testing.T) {
	draft := Classify(Input{Prompt: "create rhel9 kubeadm scenario"})
	if draft.Route != RouteDraft {
		t.Fatalf("expected draft route, got %s", draft.Route)
	}
	refine := Classify(Input{Prompt: "improve apply workflow", HasWorkflowTree: true})
	if refine.Route != RouteRefine {
		t.Fatalf("expected refine route, got %s", refine.Route)
	}
}
