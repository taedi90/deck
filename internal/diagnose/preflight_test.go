package diagnose

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/taedi90/deck/internal/config"
)

func TestPreflight(t *testing.T) {
	t.Run("pass with manifest", func(t *testing.T) {
		dir := t.TempDir()
		bundle := filepath.Join(dir, "bundle")
		report := filepath.Join(dir, "reports", "diagnose.json")
		if err := os.MkdirAll(bundle, 0o755); err != nil {
			t.Fatalf("mkdir bundle: %v", err)
		}
		if err := os.WriteFile(filepath.Join(bundle, "manifest.json"), []byte(`{"entries":[{"path":"x","sha256":"a","size":1}]}`), 0o644); err != nil {
			t.Fatalf("write manifest: %v", err)
		}

		wf := &config.Workflow{Version: "v1", Context: config.Context{BundleRoot: bundle, StateFile: filepath.Join(dir, "state.json")}, Phases: []config.Phase{{Name: "prepare"}, {Name: "install"}}}
		out, err := Preflight(wf, RunOptions{OutputPath: report})
		if err != nil {
			t.Fatalf("expected pass, got %v", err)
		}
		if out.Summary.Failed != 0 {
			t.Fatalf("expected zero failures, got %d", out.Summary.Failed)
		}
		if _, err := os.Stat(report); err != nil {
			t.Fatalf("expected report file: %v", err)
		}
	})

	t.Run("fail without install phase", func(t *testing.T) {
		dir := t.TempDir()
		wf := &config.Workflow{Version: "v1", Context: config.Context{BundleRoot: dir, StateFile: filepath.Join(dir, "state.json")}, Phases: []config.Phase{{Name: "prepare"}}}
		if _, err := Preflight(wf, RunOptions{}); err == nil {
			t.Fatalf("expected preflight failure")
		}
	})
}
