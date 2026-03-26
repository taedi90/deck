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
		content := []byte(`version: v1alpha1
phases:
  - name: prepare
    steps:
      - id: prepare-images
        apiVersion: deck/v1alpha1
        kind: DownloadImage
        spec:
          images: [registry.k8s.io/kube-apiserver:v1.30.1]
          backend:
            engine: go-containerregistry
          outputDir: images
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("download file accepts structured vars items template", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
vars:
  downloads:
    binaries:
      - source:
          url: https://example.local/a
        outputPath: files/a
      - source:
          url: https://example.local/b
        outputPath: files/b
phases:
  - name: prepare
    steps:
      - id: download-many
        apiVersion: deck/v1alpha1
        kind: DownloadFile
        register:
          allFiles: outputPaths
        spec:
          items: "{{ .vars.downloads.binaries }}"
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("conditional step keeps empty string template for schema validation", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
vars:
  upgradeKubernetesVersion: ""
phases:
  - name: apply
    steps:
      - id: upgrade-control-plane
        apiVersion: deck/v1alpha1
        kind: UpgradeKubeadm
        when: vars.upgradeKubernetesVersion != ""
        spec:
          kubernetesVersion: "{{ .vars.upgradeKubernetesVersion }}"
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("tool schema valid Image download with auth", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: prepare
    steps:
      - id: pull-private-images
        apiVersion: deck/v1alpha1
        kind: DownloadImage
        spec:
          images:
            - registry.example.com/team/app:1.0.0
          auth:
            - registry: registry.example.com
              basic:
                username: robot
                password: secret
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("tool schema rejects Image without explicit action", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: prepare
    steps:
      - id: pull-images
        apiVersion: deck/v1alpha1
        kind: Image
        spec:
          images:
            - registry.k8s.io/pause:3.9
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected tool schema validation error")
		}
		if got := err.Error(); !strings.Contains(got, "E_SCHEMA_INVALID") {
			t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
		}
	})

	t.Run("tool schema valid WaitForMissingFile glob", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: wait-missing-glob
        apiVersion: deck/v1alpha1
        kind: WaitForMissingFile
        spec:
          glob: /etc/kubernetes/manifests/*.yaml
          interval: 2s
          timeout: 2m
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("tool schema valid WaitForMissingFile paths", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: wait-missing-paths
        apiVersion: deck/v1alpha1
        kind: WaitForMissingFile
        spec:
          paths:
            - /etc/kubernetes/manifests/kube-apiserver.yaml
            - /etc/kubernetes/manifests/kube-scheduler.yaml
          interval: 2s
          timeout: 2m
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("tool schema rejects Image verify with auth", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: apply
    steps:
      - id: verify-private-images
        apiVersion: deck/v1alpha1
        kind: VerifyImage
        spec:
          images:
            - registry.example.com/team/app:1.0.0
          auth:
            - registry: registry.example.com
              basic:
                username: robot
                password: secret
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected tool schema validation error")
		}
		if got := err.Error(); !strings.Contains(got, "E_SCHEMA_INVALID") {
			t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
		}
	})

	t.Run("tool schema rejects Image verify with backend", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: apply
    steps:
      - id: verify-images
        apiVersion: deck/v1alpha1
        kind: VerifyImage
        spec:
          images:
            - registry.k8s.io/pause:3.9
          backend:
            engine: go-containerregistry
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected tool schema validation error")
		}
		if got := err.Error(); !strings.Contains(got, "E_SCHEMA_INVALID") {
			t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
		}
	})

	t.Run("tool schema valid InstallPackages without source", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: install-packages
        apiVersion: deck/v1alpha1
        kind: InstallPackage
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

	t.Run("tool schema rejects removed Package family kind", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: install-packages
        apiVersion: deck/v1alpha1
        kind: Packages
        spec:
          packages: [containerd]
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected tool schema validation error")
		}
		if got := err.Error(); !strings.Contains(got, "E_SCHEMA_INVALID") {
			t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
		}
	})

	t.Run("tool schema rejects Package download with install-only source", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: prepare
    steps:
      - id: download-packages
        apiVersion: deck/v1alpha1
        kind: DownloadPackage
        spec:
          packages: [containerd]
          source:
            type: local-repo
            path: /opt/deck/repos/custom
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected tool schema validation error")
		}
		if got := err.Error(); !strings.Contains(got, "E_SCHEMA_INVALID") {
			t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
		}
	})

	t.Run("tool schema rejects Package install with download-only backend", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: install-packages
        apiVersion: deck/v1alpha1
        kind: InstallPackage
        spec:
          packages: [containerd]
          backend:
            mode: container
            runtime: docker
            image: ubuntu:22.04
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected tool schema validation error")
		}
		if got := err.Error(); !strings.Contains(got, "E_SCHEMA_INVALID") {
			t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
		}
	})

	t.Run("tool schema valid RefreshRepository", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: refresh-cache
        apiVersion: deck/v1alpha1
        kind: RefreshRepository
        spec:
          manager: auto
          clean: true
          update: true
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("tool schema rejects invalid RefreshRepository", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: bad-cache
        apiVersion: deck/v1alpha1
        kind: RefreshRepository
        spec:
          manager: yum
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected tool schema validation error")
		}
		if got := err.Error(); !strings.Contains(got, "E_SCHEMA_INVALID") {
			t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
		}
	})

	t.Run("install phase accepts InstallPackages with only spec.packages", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: install-packages-curl
        apiVersion: deck/v1alpha1
        kind: InstallPackage
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

	t.Run("tool schema rejects ConfigureRepository refreshCache block", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: repo-apt
        apiVersion: deck/v1alpha1
        kind: ConfigureRepository
        spec:
          format: deb
          replaceExisting: true
          refreshCache:
            enabled: true
            clean: true
          repositories:
            - baseurl: http://repo.local/apt/bookworm
              trusted: true
              suite: ./
              component: main
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected schema error")
		}
		if !strings.Contains(err.Error(), "refreshCache") {
			t.Fatalf("unexpected error: %v", err)
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

	t.Run("omitted step apiVersion inherits workflow version default", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: write-config
        kind: WriteFile
        spec:
          path: /tmp/example.conf
          content: hello
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err != nil {
			t.Fatalf("expected omitted apiVersion to inherit workflow version default, got %v", err)
		}
	})

	t.Run("unsupported explicit step apiVersion", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: write-config
        apiVersion: deck/v9
        kind: WriteFile
        spec:
          path: /tmp/example.conf
          content: hello
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected unsupported step apiVersion error")
		}
		if got := err.Error(); !strings.Contains(got, "apiVersion") {
			t.Fatalf("expected apiVersion validation error, got %q", got)
		}
	})

	t.Run("schema invalid kind", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
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
		content := []byte(`version: v1alpha1
phases:
  - name: prepare
    steps:
      - id: dup-id
        apiVersion: deck/v1alpha1
        kind: DownloadFile
        spec:
          source:
            url: https://example.local/a
          outputPath: files/a
  - name: install
    steps:
      - id: dup-id
        apiVersion: deck/v1alpha1
        kind: Command
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
		content := []byte(`version: v1alpha1
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
          outputPath: files/a
      - id: s2
        apiVersion: deck/v1alpha1
        kind: DownloadFile
        register:
          token: outputB
        spec:
          source:
            url: https://example.local/b
          outputPath: files/b
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err == nil {
			t.Fatalf("expected runtime var redefinition error")
		}
	})

	t.Run("duplicate phase name", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: first
        apiVersion: deck/v1alpha1
        kind: Command
        spec:
          command: ["true"]
  - name: install
    steps:
      - id: second
        apiVersion: deck/v1alpha1
        kind: Command
        spec:
          command: ["true"]
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		err := File(path)
		if err == nil || !strings.Contains(err.Error(), "E_DUPLICATE_PHASE_NAME") {
			t.Fatalf("expected duplicate phase name error, got %v", err)
		}
	})

	t.Run("parallel group rejects same batch runtime dependency", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: prepare
    steps:
      - id: download-a
        apiVersion: deck/v1alpha1
        kind: DownloadFile
        parallelGroup: pair
        register:
          downloaded: outputPath
        spec:
          source:
            url: https://example.local/a
          outputPath: files/a.bin
      - id: download-b
        apiVersion: deck/v1alpha1
        kind: DownloadFile
        parallelGroup: pair
        spec:
          source:
            path: "{{ .runtime.downloaded }}"
          outputPath: files/b.bin
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		err := File(path)
		if err == nil || !strings.Contains(err.Error(), "E_PARALLEL_RUNTIME_DEPENDENCY") {
			t.Fatalf("expected parallel runtime dependency error, got %v", err)
		}
	})

	t.Run("parallel group rejects unsafe apply kind", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: install-a
        apiVersion: deck/v1alpha1
        kind: InstallPackage
        parallelGroup: packages
        spec:
          packages: [containerd]
      - id: install-b
        apiVersion: deck/v1alpha1
        kind: InstallPackage
        parallelGroup: packages
        spec:
          packages: [iptables]
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		err := File(path)
		if err == nil || !strings.Contains(err.Error(), "E_PARALLEL_KIND_UNSAFE") {
			t.Fatalf("expected unsafe parallel kind error, got %v", err)
		}
	})

	t.Run("tool schema rejects invalid register output key for action", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: copy-file
        apiVersion: deck/v1alpha1
        kind: CopyFile
        register:
          copiedPath: dest
        spec:
          source:
            path: /tmp/source.txt
          path: /tmp/dest.txt
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected schema/register validation error")
		}
		if got := err.Error(); !strings.Contains(got, "E_SCHEMA_INVALID") {
			t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
		}
	})

	t.Run("tool schema invalid step spec", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: bad-run-command
        apiVersion: deck/v1alpha1
        kind: Command
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
		if got := err.Error(); !strings.Contains(got, "E_SCHEMA_INVALID") {
			t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
		}
	})

	t.Run("file error includes workflow path", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: bad-run-command
        apiVersion: deck/v1alpha1
        kind: Command
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
		if !strings.Contains(err.Error(), path+": E_SCHEMA_INVALID") {
			t.Fatalf("expected path-prefixed error, got %v", err)
		}
	})

	t.Run("tool schema valid WaitPath", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: wait-admin-conf
        apiVersion: deck/v1alpha1
        kind: WaitForFile
        spec:
          path: /etc/kubernetes/admin.conf
          type: file
          nonEmpty: true
          interval: 2s
          timeout: 5m
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("tool schema rejects Wait without explicit action", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: wait-admin-conf
        apiVersion: deck/v1alpha1
        kind: Wait
        spec:
          path: /etc/kubernetes/admin.conf
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected tool schema validation error")
		}
		if got := err.Error(); !strings.Contains(got, "E_SCHEMA_INVALID") {
			t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
		}
	})

	t.Run("tool schema rejects WaitPath nonEmpty with absent state", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: bad-wait
        apiVersion: deck/v1alpha1
        kind: WaitForMissingFile
        spec:
          path: /tmp/old-file
          nonEmpty: true
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected tool schema validation error")
		}
		if got := err.Error(); !strings.Contains(got, "E_SCHEMA_INVALID") {
			t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
		}
	})

	t.Run("tool schema rejects WaitPath invalid type", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: bad-wait-type
        apiVersion: deck/v1alpha1
        kind: WaitForFile
        spec:
          path: /tmp/target
          type: socket
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected tool schema validation error")
		}
		if got := err.Error(); !strings.Contains(got, "E_SCHEMA_INVALID") {
			t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
		}
	})

	t.Run("tool schema rejects Wait fileExists with tcp fields", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: bad-wait-mix
        apiVersion: deck/v1alpha1
        kind: WaitForFile
        spec:
          path: /tmp/target
          port: "6443"
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected tool schema validation error")
		}
		if got := err.Error(); !strings.Contains(got, "E_SCHEMA_INVALID") {
			t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
		}
	})

	t.Run("tool schema valid CreateSymlink", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: symlink-runc
        apiVersion: deck/v1alpha1
        kind: CreateSymlink
        spec:
          path: /usr/bin/runc
          target: /usr/local/sbin/runc
          force: true
          createParent: true
          requireTarget: true
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("tool schema valid CreateSymlink ignoreMissingTarget", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: symlink-runc
        apiVersion: deck/v1alpha1
        kind: CreateSymlink
        spec:
          path: /opt/cni/bin
          target: /usr/lib/cni
          createParent: true
          ignoreMissingTarget: true
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("tool schema valid repository without legacy action", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: repo-config
        apiVersion: deck/v1alpha1
        kind: ConfigureRepository
        spec:
          format: deb
          repositories:
            - id: offline
              baseurl: http://repo.local/debian
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("tool schema valid KernelModule names", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: load-modules
        apiVersion: deck/v1alpha1
        kind: KernelModule
        spec:
          names: [overlay, br_netfilter]
          load: true
          persist: true
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("tool schema rejects CreateSymlink missing target", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: symlink-runc
        apiVersion: deck/v1alpha1
        kind: CreateSymlink
        spec:
          path: /usr/bin/runc
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected tool schema validation error")
		}
		if got := err.Error(); !strings.Contains(got, "E_SCHEMA_INVALID") {
			t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
		}
	})

	t.Run("tool schema rejects CreateSymlink conflicting target flags", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: symlink-runc
        apiVersion: deck/v1alpha1
        kind: CreateSymlink
        spec:
          path: /usr/bin/runc
          target: /usr/local/sbin/runc
          requireTarget: true
          ignoreMissingTarget: true
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected validation error")
		}
		if !strings.Contains(err.Error(), "E_SCHEMA_INVALID") {
			t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
		}
	})

	t.Run("tool schema valid WriteSystemdUnit", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: install-kubelet-unit
        apiVersion: deck/v1alpha1
        kind: WriteSystemdUnit
        spec:
          path: /etc/systemd/system/kubelet.service
          mode: "0644"
          template: |
            [Unit]
            Description=kubelet
          daemonReload: true
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("tool schema valid WriteSystemdUnit without explicit service name", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: install-kubelet-unit
        apiVersion: deck/v1alpha1
        kind: WriteSystemdUnit
        spec:
          path: /etc/systemd/system/kubelet.service
          content: |
            [Unit]
            Description=kubelet
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("tool schema valid File download without output path", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: prepare
    steps:
      - id: download-runc
        apiVersion: deck/v1alpha1
        kind: DownloadFile
        spec:
          source:
            url: https://example.invalid/runc
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("tool schema rejects File without explicit action", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: prepare
    steps:
      - id: download-runc
        apiVersion: deck/v1alpha1
        kind: File
        spec:
          source:
            url: https://example.invalid/runc
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected tool schema validation error")
		}
		if got := err.Error(); !strings.Contains(got, "E_SCHEMA_INVALID") {
			t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
		}
	})

	t.Run("tool schema rejects File write with both content fields", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: write-file
        apiVersion: deck/v1alpha1
        kind: WriteFile
        spec:
          path: /etc/example.conf
          content: plain text
          template: |
            templated {{ .vars.role }}
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected tool schema validation error")
		}
		if got := err.Error(); !strings.Contains(got, "E_SCHEMA_INVALID") {
			t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
		}
	})

	t.Run("tool schema rejects File write with copy-only fields", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: write-file
        apiVersion: deck/v1alpha1
        kind: WriteFile
        spec:
          path: /etc/example.conf
          content: plain text
          src: /tmp/source.txt
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected tool schema validation error")
		}
		if got := err.Error(); !strings.Contains(got, "E_SCHEMA_INVALID") {
			t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
		}
	})

	t.Run("tool schema valid Image download output dir", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: prepare
    steps:
      - id: download-images
        apiVersion: deck/v1alpha1
        kind: DownloadImage
        spec:
          images: [registry.k8s.io/pause:3.9]
          outputDir: images/core
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("tool schema valid Package download output dir", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: prepare
    steps:
      - id: download-packages
        apiVersion: deck/v1alpha1
        kind: DownloadPackage
        spec:
          packages: [containerd]
          outputDir: packages/custom
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("tool schema rejects File download outside canonical root", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: prepare
    steps:
      - id: download-file
        apiVersion: deck/v1alpha1
        kind: DownloadFile
        spec:
          source:
            url: https://example.invalid/payload.bin
          outputPath: artifacts/payload.bin
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil || !strings.Contains(err.Error(), "E_PREPARE_OUTPUT_ROOT_INVALID") {
			t.Fatalf("expected canonical root error, got %v", err)
		}
	})

	t.Run("tool schema rejects Image download outside canonical root", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: prepare
    steps:
      - id: download-images
        apiVersion: deck/v1alpha1
        kind: DownloadImage
        spec:
          images: [registry.k8s.io/pause:3.9]
          outputDir: artifacts/images
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil || !strings.Contains(err.Error(), "E_PREPARE_OUTPUT_ROOT_INVALID") {
			t.Fatalf("expected canonical root error, got %v", err)
		}
	})

	t.Run("tool schema rejects Package download outside canonical root", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: prepare
    steps:
      - id: download-packages
        apiVersion: deck/v1alpha1
        kind: DownloadPackage
        spec:
          packages: [containerd]
          outputDir: artifacts/packages
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil || !strings.Contains(err.Error(), "E_PREPARE_OUTPUT_ROOT_INVALID") {
			t.Fatalf("expected canonical root error, got %v", err)
		}
	})

	t.Run("tool schema rejects invalid WriteSystemdUnit", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: bad-systemd-unit
        apiVersion: deck/v1alpha1
        kind: WriteSystemdUnit
        spec:
          path: /etc/systemd/system/kubelet.service
          content: |
            [Unit]
            Description=kubelet
          template: |
            [Unit]
            Description=kubelet-alt
          service:
            enabled: true
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected tool schema validation error")
		}
		if got := err.Error(); !strings.Contains(got, "E_SCHEMA_INVALID") {
			t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
		}
	})

	t.Run("tool schema valid ResetKubeadm", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: reset-node
        apiVersion: deck/v1alpha1
        kind: ResetKubeadm
        spec:
          force: true
          ignoreErrors: true
          stopKubelet: true
          criSocket: unix:///run/containerd/containerd.sock
          removePaths: [/etc/cni/net.d, /var/lib/kubelet/pki]
          removeFiles: [/etc/kubernetes/admin.conf]
          cleanupContainers: [kube-apiserver, etcd]
          restartRuntimeService: containerd
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("tool schema valid expanded ResetKubeadm", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: reset-node
        apiVersion: deck/v1alpha1
        kind: ResetKubeadm
        spec:
          force: true
          criSocket: unix:///run/containerd/containerd.sock
          restartRuntimeService: containerd
          waitForRuntimeService: true
          waitForRuntimeReady: true
          waitForMissingManifestsGlob: /etc/kubernetes/manifests/*.yaml
          stopKubeletAfterReset: true
          verifyContainersAbsent: [kube-apiserver, etcd]
          reportFile: /tmp/deck/reports/reset-state.txt
          reportResetReason: acceptance
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("tool schema valid UpgradeKubeadm", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: upgrade-control-plane
        apiVersion: deck/v1alpha1
        kind: UpgradeKubeadm
        spec:
          kubernetesVersion: v1.31.0
          ignorePreflightErrors: [Swap]
          restartKubelet: true
          kubeletService: kubelet
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("tool schema valid CheckCluster", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: verify
    steps:
      - id: check-cluster
        apiVersion: deck/v1alpha1
        kind: CheckCluster
        spec:
          nodes:
            total: 1
            ready: 1
            controlPlaneReady: 1
          versions:
            targetVersion: v1.31.0
            server: v1.31.0
            kubelet: v1.31.0
            reportPath: /tmp/deck/reports/version.txt
          kubeSystem:
            readyNames: [etcd-control-plane]
            readyPrefixMinimums:
              - prefix: coredns-
                minReady: 2
          fileAssertions:
            - path: /etc/containerd/config.toml
              contains: [registry.k8s.io/pause:3.10]
          reports:
            nodesPath: /tmp/deck/reports/nodes.txt
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("tool schema valid expanded InitKubeadm", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: kubeadm-init
        apiVersion: deck/v1alpha1
        kind: InitKubeadm
        spec:
          outputJoinFile: /tmp/deck/join.txt
          configFile: /tmp/deck/kubeadm-init.yaml
          configTemplate: default
          kubernetesVersion: v1.30.14
          advertiseAddress: auto
          podNetworkCIDR: 10.244.0.0/16
          criSocket: unix:///run/containerd/containerd.sock
          ignorePreflightErrors: [swap]
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("tool schema rejects invalid expanded InitKubeadm shape", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: kubeadm-init
        apiVersion: deck/v1alpha1
        kind: InitKubeadm
        spec:
          outputJoinFile: /tmp/deck/join.txt
          pullImages: "yes"
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected tool schema validation error")
		}
		if got := err.Error(); !strings.Contains(got, "E_SCHEMA_INVALID") {
			t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
		}
	})

	t.Run("tool schema rejects Kubeadm stub mode", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: kubeadm-init
        apiVersion: deck/v1alpha1
        kind: InitKubeadm
        spec:
          mode: stub
          outputJoinFile: /tmp/deck/join.txt
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected tool schema validation error")
		}
		if got := err.Error(); !strings.Contains(got, "E_SCHEMA_INVALID") {
			t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
		}
	})

	t.Run("tool schema valid expanded JoinKubeadm with configFile", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: kubeadm-join
        apiVersion: deck/v1alpha1
        kind: JoinKubeadm
        spec:
          configFile: /tmp/deck/kubeadm-join.yaml
          asControlPlane: true
          extraArgs: [--skip-phases=preflight]
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("tool schema rejects JoinKubeadm with both joinFile and configFile", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: kubeadm-join
        apiVersion: deck/v1alpha1
        kind: JoinKubeadm
        spec:
          joinFile: /tmp/deck/join.txt
          configFile: /tmp/deck/kubeadm-join.yaml
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected tool schema validation error")
		}
		if got := err.Error(); !strings.Contains(got, "E_SCHEMA_INVALID") {
			t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
		}
	})

	t.Run("tool schema rejects Kubeadm without explicit action", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: kubeadm-join
        apiVersion: deck/v1alpha1
        kind: Kubeadm
        spec:
          joinFile: /tmp/deck/join.txt
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected tool schema validation error")
		}
		if got := err.Error(); !strings.Contains(got, "E_SCHEMA_INVALID") {
			t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
		}
	})

	t.Run("tool schema rejects reset-only fields on JoinKubeadm", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: kubeadm-join
        apiVersion: deck/v1alpha1
        kind: JoinKubeadm
        spec:
          joinFile: /tmp/deck/join.txt
          force: true
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected tool schema validation error")
		}
		if got := err.Error(); !strings.Contains(got, "E_SCHEMA_INVALID") {
			t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
		}
	})

	t.Run("tool schema rejects join-only fields on InitKubeadm", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: kubeadm-init
        apiVersion: deck/v1alpha1
        kind: InitKubeadm
        spec:
          outputJoinFile: /tmp/deck/join.txt
          asControlPlane: true
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected tool schema validation error")
		}
		if got := err.Error(); !strings.Contains(got, "E_SCHEMA_INVALID") {
			t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
		}
	})

	t.Run("tool schema rejects init-only fields on ResetKubeadm", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: reset-node
        apiVersion: deck/v1alpha1
        kind: ResetKubeadm
        spec:
          outputJoinFile: /tmp/deck/join.txt
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected tool schema validation error")
		}
		if got := err.Error(); !strings.Contains(got, "E_SCHEMA_INVALID") {
			t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
		}
	})

	t.Run("tool schema rejects invalid ResetKubeadm", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: reset-node
        apiVersion: deck/v1alpha1
        kind: ResetKubeadm
        spec:
          cleanupContainers: kube-apiserver
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected tool schema validation error")
		}
		if got := err.Error(); !strings.Contains(got, "E_SCHEMA_INVALID") {
			t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
		}
	})

	t.Run("register output key invalid for kind", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
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
		if got := err.Error(); !strings.Contains(got, "E_SCHEMA_INVALID") {
			t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
		}
	})

	t.Run("register output key valid for kind", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: prepare
    steps:
      - id: d1
        apiVersion: deck/v1alpha1
        kind: DownloadFile
        register:
          fetched: outputPath
        spec:
          source:
            url: https://example.local/a
          outputPath: files/a
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
		content := []byte(`version: v1alpha1
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

	t.Run("checkhost allowed in apply scenario", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflows", "scenarios", "apply.yaml")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir workflows: %v", err)
		}
		content := []byte(`version: v1alpha1
phases:
  - name: preflight
    steps:
      - id: check-host
        apiVersion: deck/v1alpha1
        kind: CheckHost
        register:
          hostOk: passed
        spec:
          checks: [os, arch]
  - name: install
    steps:
      - id: install-rhel-packages
        apiVersion: deck/v1alpha1
        kind: InstallPackage
        when: runtime.hostOk == true && runtime.host.os.family == "rhel"
        spec:
          packages: [kubeadm]
          source:
            type: local-repo
            path: /tmp/deck/offline/packages
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err != nil {
			t.Fatalf("expected valid apply CheckHost workflow, got %v", err)
		}
	})

	t.Run("register output key valid for symlink", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: s1
        apiVersion: deck/v1alpha1
        kind: CreateSymlink
        register:
          linkPath: path
        spec:
          path: /usr/bin/runc
          target: /usr/local/sbin/runc
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err != nil {
			t.Fatalf("expected valid symlink register output, got %v", err)
		}
	})

	t.Run("reserved runtime host key is rejected", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`version: v1alpha1
phases:
  - name: prepare
    steps:
      - id: c1
        apiVersion: deck/v1alpha1
        kind: CheckHost
        register:
          host: passed
        spec:
          checks: [os]
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected reserved runtime host key error")
		}
		if got := err.Error(); !strings.Contains(got, "E_RUNTIME_VAR_RESERVED") {
			t.Fatalf("expected E_RUNTIME_VAR_RESERVED, got %v", err)
		}
	})
}

func TestSchema_ApiVersionOptional(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.yaml")
	content := []byte(`version: v1alpha1
steps:
  - id: run-without-api-version
    kind: Command
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
	content := []byte(`version: v1alpha1
steps:
  - id: bad-template
    kind: Command
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
	if got := err.Error(); !strings.Contains(got, fmt.Sprintf("%s:%d", path, 8)) {
		t.Fatalf("expected file and line in error, got %v", err)
	}
}

func TestSchema_RejectsTopLevelImports(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.yaml")
	content := []byte(`version: v1alpha1
imports:
  - ./legacy.yaml
steps:
  - id: ok-step
    kind: Command
    spec:
      command: ["true"]
`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	err := File(path)
	if err == nil {
		t.Fatalf("expected top-level imports validation error")
	}
	if got := err.Error(); !strings.Contains(got, "E_SCHEMA_INVALID") {
		t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
	}
}

func TestSchema_AcceptsPhaseImports(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.yaml")
	content := []byte(`version: v1alpha1
phases:
  - name: install
    imports:
      - path: ./fragments/install-common.yaml
        when: runtime.host.os.family == "rhel"
`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := File(path); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestSchema_AcceptsComponentFragment(t *testing.T) {
	dir := t.TempDir()
	componentsDir := filepath.Join(dir, "workflows", "components", "k8s")
	if err := os.MkdirAll(componentsDir, 0o755); err != nil {
		t.Fatalf("mkdir components: %v", err)
	}
	path := filepath.Join(componentsDir, "prereq.yaml")
	content := []byte(`steps:
  - id: prep-disable-swap
    kind: Swap
    when: vars.enableSwap == "false"
    spec:
      disable: true
      persist: false
`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := File(path); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestSchema_RejectsComponentFragmentVars(t *testing.T) {
	dir := t.TempDir()
	componentsDir := filepath.Join(dir, "workflows", "components", "k8s")
	if err := os.MkdirAll(componentsDir, 0o755); err != nil {
		t.Fatalf("mkdir components: %v", err)
	}
	path := filepath.Join(componentsDir, "prereq.yaml")
	content := []byte(`vars:
  clusterName: demo
steps:
  - id: prep-disable-swap
    kind: Swap
    spec:
      disable: true
      persist: false
`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	err := File(path)
	if err == nil {
		t.Fatalf("expected schema validation error")
	}
	if got := err.Error(); !strings.Contains(got, "E_SCHEMA_INVALID") {
		t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
	}
}

func TestSchema_RejectsComponentFragmentPhases(t *testing.T) {
	dir := t.TempDir()
	componentsDir := filepath.Join(dir, "workflows", "components", "k8s")
	if err := os.MkdirAll(componentsDir, 0o755); err != nil {
		t.Fatalf("mkdir components: %v", err)
	}
	path := filepath.Join(componentsDir, "prereq.yaml")
	content := []byte(`phases:
  - name: install
    steps:
      - id: prep-disable-swap
        kind: Swap
        spec:
          disable: true
          persist: false
`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	err := File(path)
	if err == nil {
		t.Fatalf("expected schema validation error")
	}
	if got := err.Error(); !strings.Contains(got, "E_SCHEMA_INVALID") {
		t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
	}
}

func TestSchema_RejectsContext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.yaml")
	content := []byte(`version: v1alpha1
context:
  bundleRoot: /tmp/bundle
steps:
  - id: ok-step
    kind: Command
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
	if got := err.Error(); !strings.Contains(got, "E_SCHEMA_INVALID") {
		t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
	}
}

func TestValidateToolSchemas(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.yaml")
	content := []byte(`version: v1alpha1
steps:
  - id: svc
    kind: ManageService
    spec:
      name: containerd
      enabled: true
      state: started
  - id: svc-many
    kind: ManageService
    spec:
      names: [firewalld, ufw]
      daemonReload: true
      ifExists: true
      ignoreMissing: true
      state: stopped
  - id: ensure-dir
    kind: EnsureDirectory
    spec:
      path: /etc/containerd/certs.d
      mode: "0755"
  - id: install-file
    kind: WriteFile
    spec:
      path: /etc/modules-load.d/k8s.conf
      content: |
        overlay
  - id: template-file
    kind: WriteFile
    spec:
      path: /etc/containerd/certs.d/registry.k8s.io/hosts.toml
      template: |
        server = "http://registry.local"
  - id: repo-config
    kind: ConfigureRepository
    spec:
      path: /etc/yum.repos.d/offline.repo
      repositories:
        - id: offline-base
          baseurl: file:///srv/offline-repo
          enabled: true
          gpgcheck: false
  - id: containerd-config
    kind: WriteContainerdConfig
    spec:
      path: /etc/containerd/config.toml
      rawSettings:
        - op: set
          key: registry.configPath
          value: /etc/containerd/certs.d
        - op: set
          key: runtime.runtimes.runc.options.SystemdCgroup
          value: true
  - id: containerd-registry-hosts
    kind: WriteContainerdRegistryHosts
    spec:
      path: /etc/containerd/certs.d
      registryHosts:
        - registry: registry.k8s.io
          server: https://registry.k8s.io
          host: http://127.0.0.1:5000
          capabilities: [pull, resolve]
          skipVerify: true
  - id: edit-toml
    kind: EditTOML
    spec:
      path: /etc/containerd/config.toml
      edits:
        - op: set
          rawPath: plugins."io.containerd.grpc.v1.cri".registry.config_path
          value: /etc/containerd/certs.d
  - id: edit-yaml
    kind: EditYAML
    spec:
      path: /etc/kubernetes/kubeadm-config.yaml
      createIfMissing: true
      edits:
        - op: set
          rawPath: ClusterConfiguration.imageRepository
          value: registry.local/k8s
  - id: edit-json
    kind: EditJSON
    spec:
      path: /etc/cni/net.d/10-custom.conflist
      edits:
        - op: set
          rawPath: plugins.0.type
          value: bridge
  - id: swap
    kind: Swap
    spec:
      disable: true
      persist: true
  - id: kernel-module
    kind: KernelModule
    spec:
      name: br_netfilter
      load: true
      persist: true
  - id: sysctl-apply
    kind: Sysctl
    spec:
      values:
        net.ipv4.ip_forward: 1
      writeFile: /etc/sysctl.d/99-kubernetes-cri.conf
      apply: true
  - id: run-cmd
    kind: Command
    spec:
      command: ["true"]
`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := File(path); err != nil {
		t.Fatalf("expected no error for new/old kinds, got %v", err)
	}

	invalidPath := filepath.Join(dir, "invalid.yaml")
	invalid := []byte(`version: v1alpha1
steps:
  - id: bad-service
    kind: ManageService
    spec: {}
  - id: bad-service-both
    kind: ManageService
    spec:
      name: containerd
      names: [containerd]
`)
	if err := os.WriteFile(invalidPath, invalid, 0o644); err != nil {
		t.Fatalf("write invalid file: %v", err)
	}
	err := File(invalidPath)
	if err == nil {
		t.Fatalf("expected schema error for missing service.name")
	}
	if !strings.Contains(err.Error(), "E_SCHEMA_INVALID") {
		t.Fatalf("expected E_SCHEMA_INVALID, got %v", err)
	}
}
