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

func TestClassifyPrefersAuthoringOverGenericReviewTokens(t *testing.T) {
	decision := Classify(Input{Prompt: "check and create prepare and apply workflows for an air-gapped kubeadm cluster"})
	if decision.Route != RouteDraft {
		t.Fatalf("expected draft route, got %#v", decision)
	}
	if decision.Target.Kind != "workspace" {
		t.Fatalf("expected workspace target for prepare+apply authoring request, got %#v", decision.Target)
	}
}

func TestInferTargetKeepsPrepareAndApplyAsWorkspaceScope(t *testing.T) {
	target := inferTarget("create prepare and apply workflows for kubeadm")
	if target.Kind != "workspace" {
		t.Fatalf("expected workspace target, got %#v", target)
	}
}
