package askpolicy

import (
	"testing"

	"github.com/Airgap-Castaways/deck/internal/askcontract"
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
