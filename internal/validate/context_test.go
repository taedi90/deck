package validate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceWithContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := WorkspaceWithContext(ctx, t.TempDir())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestEntrypointWithContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := EntrypointWithContext(ctx, filepath.Join(t.TempDir(), "workflows", "scenarios", "apply.yaml"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestAnalyzeFilesWithContextCanceled(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "workflow.yaml")
	if err := os.WriteFile(path, []byte("version: v1\nsteps: []\n"), 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := AnalyzeFilesWithContext(ctx, []string{path})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}
