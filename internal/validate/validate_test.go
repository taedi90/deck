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
		content := []byte(`version: v1
phases:
  - name: prepare
    steps:
      - id: prepare-images
        apiVersion: deck/v1
        kind: DownloadImages
        spec:
          images: [registry.k8s.io/kube-apiserver:v1.30.1]
          backend:
            engine: skopeo
          output:
            dir: images
            format: docker-archive
            manifest: images/manifest.json
`)
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

	t.Run("unsupported version", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		if err := os.WriteFile(path, []byte("version: v2\nphases: []\n"), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err == nil {
			t.Fatalf("expected error for unsupported version")
		}
	})

	t.Run("schema invalid kind", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1
phases:
  - name: prepare
    steps:
      - id: invalid-kind
        apiVersion: deck/v1
        kind: NotARealKind
        spec: {}
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err == nil {
			t.Fatalf("expected schema error")
		}
	})

	t.Run("duplicate step id", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1
phases:
  - name: prepare
    steps:
      - id: dup-id
        apiVersion: deck/v1
        kind: DownloadFile
        spec:
          source:
            url: https://example.local/a
          output:
            path: files/a
  - name: install
    steps:
      - id: dup-id
        apiVersion: deck/v1
        kind: RunCommand
        spec:
          command: ["true"]
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err == nil {
			t.Fatalf("expected duplicate step id error")
		}
	})

	t.Run("runtime register redefinition", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1
phases:
  - name: prepare
    steps:
      - id: s1
        apiVersion: deck/v1
        kind: DownloadFile
        register:
          token: outputA
        spec:
          source:
            url: https://example.local/a
          output:
            path: files/a
      - id: s2
        apiVersion: deck/v1
        kind: DownloadFile
        register:
          token: outputB
        spec:
          source:
            url: https://example.local/b
          output:
            path: files/b
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err == nil {
			t.Fatalf("expected runtime var redefinition error")
		}
	})
}
