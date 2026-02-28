package validate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFile(t *testing.T) {
	t.Run("valid yaml", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "cluster.yaml")
		content := []byte("version: v1\nphases: []\n")
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		if err := File("does-not-exist.yaml"); err == nil {
			t.Fatalf("expected error for missing file")
		}
	})

	t.Run("invalid yaml", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "invalid.yaml")
		if err := os.WriteFile(path, []byte("version: ["), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err == nil {
			t.Fatalf("expected error for invalid yaml")
		}
	})
}
