package askpolicy

import (
	"testing"

	"github.com/Airgap-Castaways/deck/internal/askcontract"
	"github.com/Airgap-Castaways/deck/internal/askintent"
	"github.com/Airgap-Castaways/deck/internal/askretrieve"
)

func TestNormalizePlannedActionHandlesAddAlias(t *testing.T) {
	if got := normalizePlannedAction("add", "workflows/vars.yaml"); got != "create" {
		t.Fatalf("expected add to normalize to create, got %q", got)
	}
	if got := normalizePlannedAction("create_or_modify", "workflows/scenarios/apply.yaml"); got != "update" {
		t.Fatalf("expected create_or_modify to normalize to update, got %q", got)
	}
}

func TestMergeRequirementsWithPlanPromotesPrepareAndPlannedFiles(t *testing.T) {
	req := ScenarioRequirements{RequiredFiles: []string{"workflows/scenarios/apply.yaml"}, Connectivity: "offline"}
	merged := MergeRequirementsWithPlan(req, askcontract.PlanResponse{
		NeedsPrepare:      true,
		ArtifactKinds:     []string{"package"},
		EntryScenario:     "workflows/scenarios/apply.yaml",
		OfflineAssumption: "offline",
		Files:             []askcontract.PlanFile{{Path: "workflows/prepare.yaml"}, {Path: "workflows/scenarios/apply.yaml"}},
	})
	if !merged.NeedsPrepare || len(merged.ArtifactKinds) == 0 {
		t.Fatalf("expected prepare requirements, got %#v", merged)
	}
	if len(merged.RequiredFiles) != 2 {
		t.Fatalf("expected planned files merged into requirements, got %#v", merged.RequiredFiles)
	}
}

func TestBuildScenarioRequirementsPromotesComplexAskToComplete(t *testing.T) {
	req := BuildScenarioRequirements("create an air-gapped rhel9 3-node kubeadm cluster workflow with prepare and apply workflows", askretrieve.RetrievalResult{}, askretrieve.WorkspaceSummary{}, askintent.Decision{Route: askintent.RouteDraft})
	if req.AcceptanceLevel != "complete" {
		t.Fatalf("expected complete acceptance for complex ask, got %#v", req)
	}
}

func TestBuildScenarioRequirementsSupportsExplicitPrepareOnlyRequest(t *testing.T) {
	req := BuildScenarioRequirements("create a prepare workflow that downloads the runc binary into bundle storage", askretrieve.RetrievalResult{}, askretrieve.WorkspaceSummary{}, askintent.Decision{Route: askintent.RouteDraft, Target: askintent.Target{Kind: "scenario", Path: "workflows/prepare.yaml"}})
	if req.EntryScenario != "" {
		t.Fatalf("expected no apply entry scenario for prepare-only request, got %#v", req)
	}
	if containsString(req.RequiredFiles, "workflows/scenarios/apply.yaml") {
		t.Fatalf("did not expect apply scenario requirement for prepare-only request, got %#v", req.RequiredFiles)
	}
	if !containsString(req.RequiredFiles, "workflows/prepare.yaml") {
		t.Fatalf("expected prepare workflow requirement for prepare-only request, got %#v", req.RequiredFiles)
	}
}

func TestBuildPlanDefaultsPreservesComplexityForComplexAsk(t *testing.T) {
	req := ScenarioRequirements{NeedsPrepare: true, ArtifactKinds: []string{"package", "image"}, ScenarioIntent: []string{"kubeadm", "multi-node", "join", "node-count:3"}, Connectivity: "offline", RequiredFiles: []string{"workflows/prepare.yaml", "workflows/scenarios/apply.yaml", "workflows/vars.yaml"}}
	plan := BuildPlanDefaults(req, "create an air-gapped rhel9 3-node kubeadm workflow with prepare and apply", askintent.Decision{Route: askintent.RouteDraft}, askretrieve.WorkspaceSummary{})
	if plan.Complexity != "complex" {
		t.Fatalf("expected complex plan defaults, got %#v", plan)
	}
	if plan.AuthoringBrief.ModeIntent != "prepare+apply" {
		t.Fatalf("expected prepare+apply brief, got %#v", plan.AuthoringBrief)
	}
	if len(plan.ExecutionModel.ArtifactContracts) == 0 || plan.ExecutionModel.RoleExecution.RoleSelector != "vars.role" || plan.ExecutionModel.Verification.ExpectedNodeCount != 3 || plan.ExecutionModel.Verification.FinalVerificationRole != "control-plane" {
		t.Fatalf("expected execution model defaults for complex ask, got %#v", plan.ExecutionModel)
	}
}

func TestNormalizePlanCanonicalizesPlannerAuthoringBrief(t *testing.T) {
	plan := NormalizePlan(askcontract.PlanResponse{
		Request: "create 3-node kubeadm workflow",
		Intent:  "draft",
		AuthoringBrief: askcontract.AuthoringBrief{
			RouteIntent:          "Create staged offline workflows for kubeadm cluster bootstrap in this workspace",
			TargetScope:          "workspace-level",
			TargetPaths:          []string{"the apply scenario for this workspace"},
			ModeIntent:           "prepare and apply",
			Connectivity:         "apply runs air-gapped after prepare",
			CompletenessTarget:   "full",
			Topology:             "3-node kubeadm cluster",
			NodeCount:            3,
			RequiredCapabilities: []string{"kubeadm init/join", "offline package cache"},
		},
		Files: []askcontract.PlanFile{{Path: "workflows/prepare.yaml"}, {Path: "workflows/scenarios/apply.yaml"}, {Path: "workflows/vars.yaml"}},
	}, "create an air-gapped rhel9 3-node kubeadm workflow with prepare and apply package and image staging", askretrieve.RetrievalResult{}, askretrieve.WorkspaceSummary{}, askintent.Decision{Route: askintent.RouteDraft, Target: askintent.Target{Kind: "workspace"}})
	brief := plan.AuthoringBrief
	if brief.TargetScope != "workspace" || brief.ModeIntent != "prepare+apply" || brief.Topology != "multi-node" || brief.CompletenessTarget != "complete" {
		t.Fatalf("expected canonical brief fields, got %#v", brief)
	}
	if len(brief.TargetPaths) != 2 {
		t.Fatalf("expected fallback target paths, got %#v", brief)
	}
	for _, want := range []string{"kubeadm-bootstrap", "kubeadm-join", "prepare-artifacts"} {
		found := false
		for _, capability := range brief.RequiredCapabilities {
			if capability == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected %q in canonical capabilities, got %#v", want, brief.RequiredCapabilities)
		}
	}
	if len(plan.ExecutionModel.ArtifactContracts) == 0 {
		t.Fatalf("expected normalized execution model artifact contracts, got %#v", plan.ExecutionModel)
	}
	if plan.ExecutionModel.RoleExecution.RoleSelector != "vars.role" {
		t.Fatalf("expected fallback role selector, got %#v", plan.ExecutionModel)
	}
	if plan.ExecutionModel.Verification.ExpectedNodeCount != 3 {
		t.Fatalf("expected fallback expected node count, got %#v", plan.ExecutionModel)
	}
}

func TestNormalizePlanCanonicalizesExecutionModel(t *testing.T) {
	plan := NormalizePlan(askcontract.PlanResponse{
		Request:        "create 3-node kubeadm workflow",
		Intent:         "draft",
		AuthoringBrief: askcontract.AuthoringBrief{Topology: "multi-node", ModeIntent: "prepare+apply"},
		ExecutionModel: askcontract.ExecutionModel{
			ArtifactContracts:    []askcontract.ArtifactContract{{Kind: "Package", ProducerPath: "workflows/prepare.yaml", ConsumerPath: "workflows/scenarios/apply.yaml", Description: "packages"}, {Kind: "unknown", ProducerPath: "bad", ConsumerPath: "bad"}},
			SharedStateContracts: []askcontract.SharedStateContract{{Name: "join-file", ProducerPath: "/tmp/deck/join.txt", ConsumerPaths: []string{" /tmp/deck/join.txt "}, AvailabilityModel: "published-for-worker-consumption"}, {Name: "", ProducerPath: "", AvailabilityModel: "weird"}},
			RoleExecution:        askcontract.RoleExecutionModel{RoleSelector: "", ControlPlaneFlow: "", WorkerFlow: "", PerNodeInvocation: false},
			Verification:         askcontract.VerificationStrategy{ExpectedNodeCount: 0},
		},
		Files: []askcontract.PlanFile{{Path: "workflows/prepare.yaml"}, {Path: "workflows/scenarios/apply.yaml"}},
	}, "create an air-gapped rhel9 3-node kubeadm workflow with prepare and apply", askretrieve.RetrievalResult{}, askretrieve.WorkspaceSummary{}, askintent.Decision{Route: askintent.RouteDraft, Target: askintent.Target{Kind: "workspace"}})
	if len(plan.ExecutionModel.ArtifactContracts) == 0 || plan.ExecutionModel.ArtifactContracts[0].Kind != "package" {
		t.Fatalf("expected canonical artifact contract, got %#v", plan.ExecutionModel.ArtifactContracts)
	}
	if len(plan.ExecutionModel.SharedStateContracts) == 0 || len(plan.ExecutionModel.SharedStateContracts[0].ConsumerPaths) != 1 {
		t.Fatalf("expected canonical shared-state contract, got %#v", plan.ExecutionModel.SharedStateContracts)
	}
	if plan.ExecutionModel.RoleExecution.RoleSelector != "vars.role" || plan.ExecutionModel.Verification.ExpectedNodeCount != 3 || plan.ExecutionModel.Verification.FinalVerificationRole != "control-plane" {
		t.Fatalf("expected fallback execution details, got %#v", plan.ExecutionModel)
	}
}

func TestValidatePlanStructureAllowsRecoverableExecutionDetailGaps(t *testing.T) {
	plan := askcontract.PlanResponse{
		AuthoringBrief: askcontract.AuthoringBrief{ModeIntent: "prepare+apply", Topology: "multi-node", NodeCount: 3},
		EntryScenario:  "workflows/scenarios/apply.yaml",
		Files:          []askcontract.PlanFile{{Path: "workflows/prepare.yaml"}, {Path: "workflows/scenarios/apply.yaml"}},
	}
	if err := ValidatePlanStructure(plan); err != nil {
		t.Fatalf("expected recoverable execution detail gaps to pass viability check, got %v", err)
	}
}

func TestValidatePlanStructureRejectsMissingViableEntryScenario(t *testing.T) {
	plan := askcontract.PlanResponse{
		AuthoringBrief: askcontract.AuthoringBrief{ModeIntent: "prepare+apply", Topology: "multi-node", NodeCount: 3},
		Files:          []askcontract.PlanFile{{Path: "workflows/prepare.yaml"}},
	}
	if err := ValidatePlanStructure(plan); err == nil {
		t.Fatalf("expected missing entry scenario to fail viability check")
	}
}
