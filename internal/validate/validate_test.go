package validate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFile(t *testing.T) {
	t.Run("valid yaml", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "cluster.yaml")
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: prepare
    steps:
      - id: prepare-images
        apiVersion: deck/v1alpha1
        kind: DownloadImages
        spec:
          images: [registry.k8s.io/kube-apiserver:v1.30.1]
          backend:
            engine: go-containerregistry
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

	t.Run("tool schema valid InstallPackages without source", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: install-packages
        apiVersion: deck/v1alpha1
        kind: InstallPackages
        spec:
          packages: [containerd]
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("install phase accepts InstallPackages with only spec.packages", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: install-packages-curl
        apiVersion: deck/v1alpha1
        kind: InstallPackages
        spec:
          packages: [curl]
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
		if err := os.WriteFile(path, []byte("role: apply\nversion: ["), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err == nil {
			t.Fatalf("expected error for invalid yaml")
		}
	})

	t.Run("unsupported version", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		if err := os.WriteFile(path, []byte("role: apply\nversion: v2\nphases: []\n"), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err == nil {
			t.Fatalf("expected error for unsupported version")
		}
	})

	t.Run("schema invalid kind", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: prepare
    steps:
      - id: invalid-kind
        apiVersion: deck/v1alpha1
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
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: prepare
    steps:
      - id: dup-id
        apiVersion: deck/v1alpha1
        kind: DownloadFile
        spec:
          source:
            url: https://example.local/a
          output:
            path: files/a
  - name: install
    steps:
      - id: dup-id
        apiVersion: deck/v1alpha1
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
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: prepare
    steps:
      - id: s1
        apiVersion: deck/v1alpha1
        kind: DownloadFile
        register:
          token: outputA
        spec:
          source:
            url: https://example.local/a
          output:
            path: files/a
      - id: s2
        apiVersion: deck/v1alpha1
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

	t.Run("tool schema invalid step spec", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: bad-run-command
        apiVersion: deck/v1alpha1
        kind: RunCommand
        spec:
          command: []
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected tool schema validation error")
		}
		if got := err.Error(); !strings.HasPrefix(got, "E_SCHEMA_INVALID") {
			t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
		}
	})

	t.Run("register output key invalid for kind", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: w1
        apiVersion: deck/v1alpha1
        kind: WriteFile
        register:
          x: notARealOutput
        spec:
          path: /tmp/a.txt
          content: hello
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected register output error")
		}
		if got := err.Error(); !strings.HasPrefix(got, "E_REGISTER_OUTPUT_NOT_FOUND") {
			t.Fatalf("expected E_REGISTER_OUTPUT_NOT_FOUND, got %v", err)
		}
	})

	t.Run("register output key valid for kind", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: prepare
    steps:
      - id: d1
        apiVersion: deck/v1alpha1
        kind: DownloadFile
        register:
          fetched: path
        spec:
          source:
            url: https://example.local/a
          output:
            path: files/a
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err != nil {
			t.Fatalf("expected valid register output, got %v", err)
		}
	})

	t.Run("register output key valid for checkhost", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: prepare
    steps:
      - id: c1
        apiVersion: deck/v1alpha1
        kind: CheckHost
        register:
          hostOk: passed
        spec:
          checks: [os, arch]
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err != nil {
			t.Fatalf("expected valid checkhost register output, got %v", err)
		}
	})
}

func TestSchema_ApiVersionOptional(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.yaml")
	content := []byte(`role: apply
version: v1alpha1
steps:
  - id: run-without-api-version
    kind: RunCommand
    spec:
      command: ["true"]
`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := File(path); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidate_SingleBraceTemplateShowsLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.yaml")
	content := []byte(`role: apply
version: v1alpha1
steps:
  - id: bad-template
    kind: RunCommand
    spec:
      command:
        - "echo"
        - "{ .vars.msg }"
`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	err := File(path)
	if err == nil {
		t.Fatalf("expected single-brace template validation error")
	}

	if got := err.Error(); !strings.Contains(got, "E_TEMPLATE_SINGLE_BRACE") {
		t.Fatalf("expected E_TEMPLATE_SINGLE_BRACE, got %v", err)
	}
	if got := err.Error(); !strings.Contains(got, fmt.Sprintf("%s:%d", path, 9)) {
		t.Fatalf("expected file and line in error, got %v", err)
	}
}

func TestSchema_RejectsContextAndImports(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.yaml")
	content := []byte(`role: apply
version: v1alpha1
imports:
  - ./legacy.yaml
context:
  bundleRoot: /tmp/bundle
steps:
  - id: ok-step
    kind: RunCommand
    spec:
      command: ["true"]
`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	err := File(path)
	if err == nil {
		t.Fatalf("expected schema validation error")
	}
	if got := err.Error(); !strings.HasPrefix(got, "E_SCHEMA_INVALID") {
		t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
	}
}
