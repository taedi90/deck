package prepare

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/taedi90/deck/internal/config"
)

func TestPackCacheInvalidation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	workflowBytes := []byte("version: v1alpha1\r\nphases: []\r\n")
	workflowSHA := computeWorkflowSHA256(workflowBytes)

	wf := &config.Workflow{
		Version:        "v1alpha1",
		WorkflowSHA256: workflowSHA,
		Vars: map[string]any{
			"pkgA": "alpha",
			"pkgB": "beta",
		},
		Phases: []config.Phase{{
			Name: "prepare",
			Steps: []config.Step{
				{
					ID:   "artifact-a",
					Kind: "DownloadPackage",
					Spec: map[string]any{
						"packages": []any{"containerd-{{ .vars.pkgA }}"},
					},
				},
				{
					ID:   "artifact-b",
					Kind: "DownloadPackage",
					Spec: map[string]any{
						"packages": []any{"iptables-{{ .vars.pkgB }}"},
					},
				},
			},
		}},
	}

	bundleRoot := t.TempDir()
	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundleRoot}); err != nil {
		t.Fatalf("first Run failed: %v", err)
	}

	statePath := filepath.Join(home, ".cache", "deck", "state", workflowSHA+".json")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("expected pack cache state file: %v", err)
	}
	prevState, err := loadPackCacheState(statePath)
	if err != nil {
		t.Fatalf("loadPackCacheState failed: %v", err)
	}

	if len(prevState.Artifact) != 2 {
		t.Fatalf("expected two artifact states, got %d", len(prevState.Artifact))
	}

	wf.Vars["pkgB"] = "gamma"
	plan := ComputePackCachePlan(prevState, workflowBytes, wf.Vars, wf.Phases[0].Steps)

	if len(plan.Artifact) != 2 {
		t.Fatalf("expected two plan artifacts, got %d", len(plan.Artifact))
	}

	actions := map[string]string{}
	for _, artifact := range plan.Artifact {
		actions[artifact.StepID] = artifact.Action
	}

	if actions["artifact-a"] != packCacheActionReuse {
		t.Fatalf("artifact-a action = %s, want %s", actions["artifact-a"], packCacheActionReuse)
	}
	if actions["artifact-b"] != packCacheActionFetch {
		t.Fatalf("artifact-b action = %s, want %s", actions["artifact-b"], packCacheActionFetch)
	}
}
