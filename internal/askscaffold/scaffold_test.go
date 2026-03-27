package askscaffold

import (
	"strings"
	"testing"

	"github.com/Airgap-Castaways/deck/internal/askcontract"
	"github.com/Airgap-Castaways/deck/internal/askintent"
	"github.com/Airgap-Castaways/deck/internal/askknowledge"
	"github.com/Airgap-Castaways/deck/internal/askpolicy"
	"github.com/Airgap-Castaways/deck/internal/askretrieve"
)

func TestBuildSelectsKubeadmStarterScaffold(t *testing.T) {
	scaffold := Build(
		askpolicy.ScenarioRequirements{ScenarioIntent: []string{"kubeadm"}, NeedsPrepare: true, ArtifactKinds: []string{"package", "image"}},
		askretrieve.WorkspaceSummary{},
		askintent.Decision{Route: askintent.RouteDraft},
		askcontract.PlanResponse{Request: "create kubeadm workflow"},
		askknowledge.Current(),
	)
	if scaffold.Family != FamilyKubeadm {
		t.Fatalf("expected kubeadm scaffold, got %#v", scaffold)
	}
	if !strings.Contains(PromptBlock(scaffold), "DownloadPackage") || !strings.Contains(PromptBlock(scaffold), "InitKubeadm") {
		t.Fatalf("expected kubeadm scaffold prompt to include typed starter steps, got %q", PromptBlock(scaffold))
	}
}

func TestBuildSelectsRefineScaffoldForRefineRoute(t *testing.T) {
	scaffold := Build(
		askpolicy.ScenarioRequirements{AcceptanceLevel: "refine"},
		askretrieve.WorkspaceSummary{Files: []askretrieve.WorkspaceFile{{Path: "workflows/scenarios/apply.yaml", Content: "version: v1alpha1\nsteps: []\n"}}},
		askintent.Decision{Route: askintent.RouteRefine},
		askcontract.PlanResponse{Files: []askcontract.PlanFile{{Path: "workflows/scenarios/apply.yaml", Action: "update", Purpose: "entry"}}},
		askknowledge.Current(),
	)
	if scaffold.Family != FamilyRefine {
		t.Fatalf("expected refine scaffold, got %#v", scaffold)
	}
	if !strings.Contains(PromptBlock(scaffold), "preserve=true") {
		t.Fatalf("expected refine scaffold to preserve planned file, got %q", PromptBlock(scaffold))
	}
}

func TestBuildSelectsMultiNodeKubeadmScaffold(t *testing.T) {
	scaffold := Build(
		askpolicy.ScenarioRequirements{ScenarioIntent: []string{"kubeadm", "multi-node", "join"}, NeedsPrepare: true, ArtifactKinds: []string{"package", "image"}},
		askretrieve.WorkspaceSummary{},
		askintent.Decision{Route: askintent.RouteDraft},
		askcontract.PlanResponse{Request: "create 3-node kubeadm workflow", AuthoringBrief: askcontract.AuthoringBrief{Topology: "multi-node", NodeCount: 3}},
		askknowledge.Current(),
	)
	if scaffold.Family != FamilyKubeadmMulti {
		t.Fatalf("expected multi-node kubeadm scaffold, got %#v", scaffold)
	}
	prompt := PromptBlock(scaffold)
	for _, want := range []string{"JoinKubeadm", "verify-cluster", "total: 3"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected %q in scaffold prompt, got %q", want, prompt)
		}
	}
}
