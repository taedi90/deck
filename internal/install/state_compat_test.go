package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/taedi90/deck/internal/config"
)

func TestResolveStateReadPathUsesLegacyHomeStateFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))

	wf := &config.Workflow{StateKey: "legacy-state-key"}
	preferred, err := DefaultStatePath(wf)
	if err != nil {
		t.Fatalf("DefaultStatePath failed: %v", err)
	}
	legacyPath, err := LegacyStatePath(wf)
	if err != nil {
		t.Fatalf("LegacyStatePath failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatalf("mkdir legacy state dir: %v", err)
	}
	if err := os.WriteFile(legacyPath, []byte("{\n  \"completedSteps\": [\"s1\"]\n}\n"), 0o600); err != nil {
		t.Fatalf("write legacy state: %v", err)
	}

	resolved, err := ResolveStateReadPathForWorkflow(wf, preferred)
	if err != nil {
		t.Fatalf("ResolveStateReadPathForWorkflow failed: %v", err)
	}
	if resolved != legacyPath {
		t.Fatalf("expected legacy state path, got %q want %q", resolved, legacyPath)
	}
}
