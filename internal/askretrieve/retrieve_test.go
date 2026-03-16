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
	result := Retrieve(askintent.RouteQuestion, "what is this workspace", workspace, askstate.Context{})
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

func longText(size int) string {
	b := make([]byte, size)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}
