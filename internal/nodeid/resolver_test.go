package nodeid

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveNodeIDPrecedence(t *testing.T) {
	root := t.TempDir()
	paths := Paths{
		OperatorPath:  filepath.Join(root, "etc", "deck", "node-id"),
		GeneratedPath: filepath.Join(root, "var", "lib", "deck", "node-id"),
	}
	if err := os.MkdirAll(filepath.Dir(paths.OperatorPath), 0o755); err != nil {
		t.Fatalf("mkdir operator: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.GeneratedPath), 0o755); err != nil {
		t.Fatalf("mkdir generated: %v", err)
	}
	if err := os.WriteFile(paths.OperatorPath, []byte("operator-id\n"), 0o644); err != nil {
		t.Fatalf("write operator id: %v", err)
	}
	if err := os.WriteFile(paths.GeneratedPath, []byte("generated-id\n"), 0o644); err != nil {
		t.Fatalf("write generated id: %v", err)
	}

	resolved, err := Resolve(paths)
	if err != nil {
		t.Fatalf("resolve node id: %v", err)
	}
	if resolved.ID != "operator-id" {
		t.Fatalf("expected operator id, got %q", resolved.ID)
	}
	if resolved.Source != SourceOperator {
		t.Fatalf("expected source %q, got %q", SourceOperator, resolved.Source)
	}
}

func TestResolveNodeIDMismatch(t *testing.T) {
	root := t.TempDir()
	paths := Paths{
		OperatorPath:  filepath.Join(root, "etc", "deck", "node-id"),
		GeneratedPath: filepath.Join(root, "var", "lib", "deck", "node-id"),
	}
	if err := os.MkdirAll(filepath.Dir(paths.OperatorPath), 0o755); err != nil {
		t.Fatalf("mkdir operator: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.GeneratedPath), 0o755); err != nil {
		t.Fatalf("mkdir generated: %v", err)
	}
	if err := os.WriteFile(paths.OperatorPath, []byte("operator-id\n"), 0o644); err != nil {
		t.Fatalf("write operator id: %v", err)
	}
	if err := os.WriteFile(paths.GeneratedPath, []byte("generated-id\n"), 0o644); err != nil {
		t.Fatalf("write generated id: %v", err)
	}

	resolved, err := Resolve(paths)
	if err != nil {
		t.Fatalf("resolve node id: %v", err)
	}
	if !resolved.Mismatch {
		t.Fatalf("expected mismatch=true")
	}
	if resolved.OperatorID != "operator-id" {
		t.Fatalf("unexpected operator id: %q", resolved.OperatorID)
	}
	if resolved.GeneratedID != "generated-id" {
		t.Fatalf("unexpected generated id: %q", resolved.GeneratedID)
	}
}

func TestResolveNodeIDGeneratesFallback(t *testing.T) {
	root := t.TempDir()
	paths := Paths{
		OperatorPath:  filepath.Join(root, "etc", "deck", "node-id"),
		GeneratedPath: filepath.Join(root, "var", "lib", "deck", "node-id"),
	}

	resolved, err := Resolve(paths)
	if err != nil {
		t.Fatalf("resolve node id: %v", err)
	}
	if resolved.Source != SourceGeneratedNew {
		t.Fatalf("expected source %q, got %q", SourceGeneratedNew, resolved.Source)
	}
	if !resolved.GeneratedIDNew {
		t.Fatalf("expected GeneratedIDNew=true")
	}
	if !strings.HasPrefix(resolved.ID, "node-") {
		t.Fatalf("expected generated node-* id, got %q", resolved.ID)
	}

	raw, err := os.ReadFile(paths.GeneratedPath)
	if err != nil {
		t.Fatalf("read generated path: %v", err)
	}
	if strings.TrimSpace(string(raw)) != resolved.ID {
		t.Fatalf("generated file mismatch: got %q want %q", strings.TrimSpace(string(raw)), resolved.ID)
	}
}

func TestResolveNodeIDRejectsInvalidStoredValue(t *testing.T) {
	root := t.TempDir()
	paths := Paths{
		OperatorPath:  filepath.Join(root, "etc", "deck", "node-id"),
		GeneratedPath: filepath.Join(root, "var", "lib", "deck", "node-id"),
	}
	if err := os.MkdirAll(filepath.Dir(paths.GeneratedPath), 0o755); err != nil {
		t.Fatalf("mkdir generated: %v", err)
	}
	if err := os.WriteFile(paths.GeneratedPath, []byte("INVALID_UPPER\n"), 0o644); err != nil {
		t.Fatalf("write generated id: %v", err)
	}

	_, err := Resolve(paths)
	if err == nil || !strings.Contains(err.Error(), "invalid node-id") {
		t.Fatalf("expected invalid node-id error, got %v", err)
	}
}
