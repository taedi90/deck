package install

import (
	"path/filepath"
	"testing"

	"github.com/taedi90/deck/internal/nodeid"
)

func TestResolveNodeIDPrecedence(t *testing.T) {
	root := t.TempDir()
	paths := nodeid.Paths{
		OperatorPath:  filepath.Join(root, "etc", "deck", "node-id"),
		GeneratedPath: filepath.Join(root, "var", "lib", "deck", "node-id"),
	}
	resolved, err := nodeid.SetOperator(paths, "operator-id")
	if err != nil {
		t.Fatalf("set operator node-id: %v", err)
	}
	if resolved.ID != "operator-id" || resolved.Source != nodeid.SourceOperator {
		t.Fatalf("unexpected node-id result: %+v", resolved)
	}
}

func TestResolveNodeIDMismatch(t *testing.T) {
	root := t.TempDir()
	paths := nodeid.Paths{
		OperatorPath:  filepath.Join(root, "etc", "deck", "node-id"),
		GeneratedPath: filepath.Join(root, "var", "lib", "deck", "node-id"),
	}
	if _, err := nodeid.SetOperator(paths, "operator-id"); err != nil {
		t.Fatalf("set operator node-id: %v", err)
	}
	if _, err := nodeid.Init(paths); err != nil {
		t.Fatalf("init generated node-id: %v", err)
	}
	resolved, err := nodeid.Resolve(paths)
	if err != nil {
		t.Fatalf("resolve node-id: %v", err)
	}
	if !resolved.Mismatch {
		t.Fatalf("expected mismatch=true, got %+v", resolved)
	}
}

func TestResolveNodeIDGeneratesFallback(t *testing.T) {
	root := t.TempDir()
	paths := nodeid.Paths{
		OperatorPath:  filepath.Join(root, "etc", "deck", "node-id"),
		GeneratedPath: filepath.Join(root, "var", "lib", "deck", "node-id"),
	}
	resolved, err := nodeid.Resolve(paths)
	if err != nil {
		t.Fatalf("resolve node-id: %v", err)
	}
	if resolved.ID == "" || !resolved.GeneratedIDNew {
		t.Fatalf("expected generated fallback, got %+v", resolved)
	}
}
