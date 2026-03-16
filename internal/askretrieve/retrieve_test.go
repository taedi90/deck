package askretrieve

import (
	"path/filepath"
	"testing"

	"github.com/taedi90/deck/internal/askintent"
	"github.com/taedi90/deck/internal/askstate"
)

func TestWorkspaceFileAllowed(t *testing.T) {
	for _, path := range []string{"workflows/scenarios/apply.yaml", "workflows/components/foo.yaml", "workflows/vars.yaml"} {
		if !workspaceFileAllowed(path) {
			t.Fatalf("expected allowed path: %s", path)
		}
	}
	for _, path := range []string{".env", "outputs/report.txt", ".git/config", "test/artifacts/foo", "bin/tool"} {
		if workspaceFileAllowed(path) {
			t.Fatalf("expected blocked path: %s", path)
		}
	}
}

func TestRetrieveHasBudgetAndDropTracking(t *testing.T) {
	workspace := WorkspaceSummary{
		Root: filepath.ToSlash(t.TempDir()),
		Files: []WorkspaceFile{
			{Path: "workflows/scenarios/apply.yaml", Content: longText(7000)},
			{Path: "workflows/scenarios/prepare.yaml", Content: longText(7000)},
		},
	}
	result := Retrieve(askintent.RouteQuestion, "what is this workspace", askintent.Target{}, workspace, askstate.Context{}, nil)
	if result.MaxBytes <= 0 {
		t.Fatalf("expected non-zero budget")
	}
	if len(result.Chunks) == 0 {
		t.Fatalf("expected at least one retrieval chunk")
	}
	if len(result.Dropped) == 0 {
		t.Fatalf("expected dropped chunks for oversized context")
	}
}

func TestRetrieveIncludesRelatedImportsForTargetScenario(t *testing.T) {
	workspace := WorkspaceSummary{
		Root: filepath.ToSlash(t.TempDir()),
		Files: []WorkspaceFile{
			{Path: "workflows/scenarios/apply.yaml", Content: "role: apply\nversion: v1alpha1\nphases:\n  - name: bootstrap\n    imports:\n      - path: bootstrap.yaml\n"},
			{Path: "workflows/components/bootstrap.yaml", Content: "steps:\n  - id: init\n    kind: Kubeadm\n"},
			{Path: "workflows/components/unrelated.yaml", Content: "steps:\n  - id: noop\n    kind: Command\n"},
		},
	}
	result := Retrieve(askintent.RouteExplain, "explain apply workflow", askintent.Target{Kind: "scenario", Path: "workflows/scenarios/apply.yaml", Name: "apply"}, workspace, askstate.Context{}, nil)
	ids := chunkIDs(result.Chunks)
	if !contains(ids, "workspace-workflows_components_bootstrap.yaml") {
		t.Fatalf("expected related imported component chunk, got %v", ids)
	}
}

func chunkIDs(chunks []Chunk) []string {
	ids := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		ids = append(ids, chunk.ID)
	}
	return ids
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func longText(size int) string {
	b := make([]byte, size)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}
