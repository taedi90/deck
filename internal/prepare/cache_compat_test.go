package prepare

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/taedi90/deck/internal/userdirs"
)

func TestLoadLegacyPackCacheState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))

	workflowSHA := "abc123"
	legacyRoot, err := userdirs.LegacyCacheRoot()
	if err != nil {
		t.Fatalf("LegacyCacheRoot failed: %v", err)
	}
	legacyPath := filepath.Join(legacyRoot, "state", workflowSHA+".json")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatalf("mkdir legacy cache state dir: %v", err)
	}
	want := []byte("{\n  \"artifact\": []\n}\n")
	if err := os.WriteFile(legacyPath, want, 0o600); err != nil {
		t.Fatalf("write legacy cache state: %v", err)
	}

	canonicalPath := filepath.Join(home, ".cache", "deck", "state", workflowSHA+".json")
	raw, found, err := loadLegacyPackCacheState(canonicalPath)
	if err != nil {
		t.Fatalf("loadLegacyPackCacheState failed: %v", err)
	}
	if !found {
		t.Fatal("expected legacy cache state to be found")
	}
	if string(raw) != string(want) {
		t.Fatalf("unexpected legacy cache state\nwant: %q\ngot : %q", string(want), string(raw))
	}
}
