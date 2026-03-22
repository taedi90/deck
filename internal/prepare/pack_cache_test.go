package prepare

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/taedi90/deck/internal/config"
	"github.com/taedi90/deck/internal/filemode"
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
						"backend":  map[string]any{"mode": "container", "runtime": "docker", "image": "ubuntu:22.04"},
					},
				},
				{
					ID:   "artifact-b",
					Kind: "DownloadPackage",
					Spec: map[string]any{
						"packages": []any{"iptables-{{ .vars.pkgB }}"},
						"backend":  map[string]any{"mode": "container", "runtime": "docker", "image": "ubuntu:22.04"},
					},
				},
			},
		}},
	}

	bundleRoot := t.TempDir()
	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundleRoot, CommandRunner: &fakeRunner{}}); err != nil {
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

	prevPaths := map[string]string{}
	for _, artifact := range prevState.Artifact {
		path, err := exportedPackageCachePath(artifact.CacheKey, artifact.InputVars)
		if err != nil {
			t.Fatalf("exportedPackageCachePath failed: %v", err)
		}
		prevPaths[artifact.StepID] = path
	}
	for _, artifact := range plan.Artifact {
		path, err := exportedPackageCachePath(artifact.CacheKey, artifact.InputVars)
		if err != nil {
			t.Fatalf("exportedPackageCachePath failed: %v", err)
		}
		switch artifact.StepID {
		case "artifact-a":
			if path != prevPaths[artifact.StepID] {
				t.Fatalf("artifact-a cache path changed unexpectedly: %q != %q", path, prevPaths[artifact.StepID])
			}
		case "artifact-b":
			if path == prevPaths[artifact.StepID] {
				t.Fatalf("artifact-b cache path did not change after input var update")
			}
		}
	}
}

func TestPackCachePlanFallsBackToFetchWhenExportedCacheMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workflowBytes := []byte("version: v1alpha1\nphases: []\n")
	step := config.Step{
		ID:   "artifact-a",
		Kind: "DownloadPackage",
		Spec: map[string]any{"packages": []any{"containerd-{{ .vars.pkgA }}"}},
	}
	effectiveVars := map[string]any{"pkgA": "alpha"}
	prevState := packCacheState{Artifact: collectPackCacheArtifact([]config.Step{step}, effectiveVars)}
	plan := ComputePackCachePlan(prevState, workflowBytes, effectiveVars, []config.Step{step})
	if got := plan.Artifact[0].Action; got != packCacheActionFetch {
		t.Fatalf("expected FETCH when exported cache is missing, got %s", got)
	}
	cachePath, err := exportedPackageCachePath(plan.Artifact[0].CacheKey, plan.Artifact[0].InputVars)
	if err != nil {
		t.Fatalf("exportedPackageCachePath failed: %v", err)
	}
	stage := buildExportedPackageCacheStage(cachePath)
	if err := filemode.EnsureDir(exportedPackageCachePayloadPath(stage), filemode.PublishedArtifact); err != nil {
		t.Fatalf("EnsureDir failed: %v", err)
	}
	if err := filemode.WriteArtifactFile(filepath.Join(exportedPackageCachePayloadPath(stage), "mock-package.deb"), []byte("pkg")); err != nil {
		t.Fatalf("WriteArtifactFile failed: %v", err)
	}
	if err := saveExportedPackageCacheMeta(stage, exportedPackageCacheMeta{RootRel: "packages", Packages: []string{"containerd-alpha"}, Files: []string{"mock-package.deb"}}); err != nil {
		t.Fatalf("saveExportedPackageCacheMeta failed: %v", err)
	}
	if err := replacePublishedArtifactDir(stage, cachePath); err != nil {
		t.Fatalf("replacePublishedArtifactDir failed: %v", err)
	}
	plan = ComputePackCachePlan(prevState, workflowBytes, effectiveVars, []config.Step{step})
	if got := plan.Artifact[0].Action; got != packCacheActionReuse {
		t.Fatalf("expected REUSE when exported cache exists, got %s", got)
	}
}
