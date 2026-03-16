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

	workflowBytes := []byte("role: prepare\r\nversion: v1alpha1\r\nphases: []\r\n")
	workflowSHA := computeWorkflowSHA256(workflowBytes)

	wf := &config.Workflow{
		Role:           "prepare",
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
					Kind: "Packages",
					Spec: map[string]any{
						"action":   "download",
						"packages": []any{"containerd-{{ .vars.pkgA }}"},
					},
				},
				{
					ID:   "artifact-b",
					Kind: "Packages",
					Spec: map[string]any{
						"action":   "download",
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

	if len(prevState.Artifacts) != 2 {
		t.Fatalf("expected two artifact states, got %d", len(prevState.Artifacts))
	}

	wf.Vars["pkgB"] = "gamma"
	plan := ComputePackCachePlan(prevState, workflowBytes, wf.Vars, wf.Phases[0].Steps)

	if len(plan.Artifacts) != 2 {
		t.Fatalf("expected two plan artifacts, got %d", len(plan.Artifacts))
	}

	actions := map[string]string{}
	for _, artifact := range plan.Artifacts {
		actions[artifact.StepID] = artifact.Action
	}

	if actions["artifact-a"] != packCacheActionReuse {
		t.Fatalf("artifact-a action = %s, want %s", actions["artifact-a"], packCacheActionReuse)
	}
	if actions["artifact-b"] != packCacheActionFetch {
		t.Fatalf("artifact-b action = %s, want %s", actions["artifact-b"], packCacheActionFetch)
	}
}

func TestRun_PackCacheRoleGate(t *testing.T) {
	t.Run("apply role does not write pack cache state", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		workflowBytes := []byte("role: apply\r\nversion: v1alpha1\r\nphases: []\r\n")
		workflowSHA := computeWorkflowSHA256(workflowBytes)
		wf := &config.Workflow{
			Role:           "apply",
			Version:        "v1alpha1",
			WorkflowSHA256: workflowSHA,
			Phases: []config.Phase{{
				Name: "prepare",
				Steps: []config.Step{{
					ID:   "artifact-a",
					Kind: "Packages",
					Spec: map[string]any{
						"action":   "download",
						"packages": []any{"containerd"},
					},
				}},
			}},
		}

		if err := Run(context.Background(), wf, RunOptions{BundleRoot: t.TempDir()}); err != nil {
			t.Fatalf("Run failed: %v", err)
		}

		statePath := filepath.Join(home, ".cache", "deck", "state", workflowSHA+".json")
		if _, err := os.Stat(statePath); !os.IsNotExist(err) {
			t.Fatalf("pack cache state must not be written for apply role, err=%v", err)
		}
	})

	t.Run("empty role does not touch pack cache state", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		workflowBytes := []byte("version: v1alpha1\r\nphases: []\r\n")
		workflowSHA := computeWorkflowSHA256(workflowBytes)
		wf := &config.Workflow{
			Role:           "",
			Version:        "v1alpha1",
			WorkflowSHA256: workflowSHA,
			Phases: []config.Phase{{
				Name: "prepare",
				Steps: []config.Step{{
					ID:   "artifact-a",
					Kind: "Packages",
					Spec: map[string]any{
						"action":   "download",
						"packages": []any{"containerd"},
					},
				}},
			}},
		}

		if err := Run(context.Background(), wf, RunOptions{BundleRoot: t.TempDir()}); err != nil {
			t.Fatalf("Run failed: %v", err)
		}

		statePath := filepath.Join(home, ".cache", "deck", "state", workflowSHA+".json")
		if _, err := os.Stat(statePath); !os.IsNotExist(err) {
			t.Fatalf("pack cache state must not be written for empty role, err=%v", err)
		}
	})
}
