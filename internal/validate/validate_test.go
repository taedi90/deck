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
		content := []byte(`role: prepare
version: v1alpha1
phases:
  - name: prepare
    steps:
      - id: prepare-images
        apiVersion: deck/v1alpha1
        kind: Image
        spec:
          action: download
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

	t.Run("tool schema valid Image download with auth", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: prepare
version: v1alpha1
phases:
  - name: prepare
    steps:
      - id: pull-private-images
        apiVersion: deck/v1alpha1
        kind: Image
        spec:
          action: download
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

	t.Run("tool schema rejects Image verify with auth", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: apply
    steps:
      - id: verify-private-images
        apiVersion: deck/v1alpha1
        kind: Image
        spec:
          action: verify
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
        kind: Packages
        spec:
          action: install
          packages: [containerd]
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("tool schema valid PackageCache", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: refresh-cache
        apiVersion: deck/v1alpha1
        kind: PackageCache
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

	t.Run("tool schema valid InstallArtifacts", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: install-artifacts
        apiVersion: deck/v1alpha1
        kind: Artifacts
        spec:
          artifacts:
            - source:
                amd64:
                  url: http://repo.local/bin/linux/amd64/kubelet
                arm64:
                  url: http://repo.local/bin/linux/arm64/kubelet
              skipIfPresent:
                path: /usr/bin/kubelet
                executable: true
              install:
                path: /usr/bin/kubelet
                mode: "0755"
            - source:
                amd64:
                  path: files/bin/linux/amd64/crictl.tar.gz
                arm64:
                  path: files/bin/linux/arm64/crictl.tar.gz
              extract:
                destination: /usr/bin
                include: [crictl]
                mode: "0755"
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("tool schema rejects invalid InstallArtifacts", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: bad-install-artifacts
        apiVersion: deck/v1alpha1
        kind: Artifacts
        spec:
          artifacts:
            - source:
                amd64:
                  url: http://repo.local/bin/linux/amd64/kubelet
              install:
                path: /usr/bin/kubelet
              extract:
                destination: /usr/bin
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

	t.Run("tool schema rejects invalid PackageCache", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: bad-cache
        apiVersion: deck/v1alpha1
        kind: PackageCache
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
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: install-packages-curl
        apiVersion: deck/v1alpha1
        kind: Packages
        spec:
          action: install
          packages: [curl]
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("tool schema valid RepoConfig apt without path", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: repo-apt
        apiVersion: deck/v1alpha1
        kind: Repository
        spec:
          action: configure
          format: apt
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

		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("prepare artifacts allow execution settings", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: prepare
version: v1alpha1
artifacts:
  files:
    - group: binaries
      execution:
        parallelism: 2
        retry: 1
      items:
        - id: crictl
          source:
            url: https://example.invalid/crictl
          output:
            path: bin/crictl
  images:
    - group: control-plane
      execution:
        parallelism: 3
        retry: 2
      items:
        - image: registry.k8s.io/pause:3.9
  packages:
    - group: runtimes
      execution:
        parallelism: 2
        retry: 1
      targets:
        - osFamily: rhel
          release: "9"
      items:
        - name: containerd
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("prepare artifacts reject duplicate package roots", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: prepare
version: v1alpha1
artifacts:
  packages:
    - group: rhel-a
      targets:
        - osFamily: rhel
          release: "9"
      items:
        - name: containerd
      repo:
        type: yum
      backend:
        mode: container
        image: rockylinux:9
    - group: rhel-b
      targets:
        - osFamily: rhel
          release: "9"
      items:
        - name: cri-tools
      repo:
        type: yum
      backend:
        mode: container
        image: rockylinux:9
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		err := File(path)
		if err == nil {
			t.Fatalf("expected duplicate package root error")
		}
		if !strings.Contains(err.Error(), "duplicate package artifact root") {
			t.Fatalf("expected duplicate package artifact root, got %v", err)
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
		content := []byte(`role: prepare
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
		content := []byte(`role: prepare
version: v1alpha1
phases:
  - name: prepare
    steps:
      - id: dup-id
        apiVersion: deck/v1alpha1
        kind: File
        spec:
          action: download
          source:
            url: https://example.local/a
          output:
            path: files/a
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
		content := []byte(`role: prepare
version: v1alpha1
phases:
  - name: prepare
    steps:
      - id: s1
        apiVersion: deck/v1alpha1
        kind: File
        register:
          token: outputA
        spec:
          action: download
          source:
            url: https://example.local/a
          output:
            path: files/a
      - id: s2
        apiVersion: deck/v1alpha1
        kind: File
        register:
          token: outputB
        spec:
          action: download
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
		content := []byte(`role: apply
version: v1alpha1
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
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: wait-admin-conf
        apiVersion: deck/v1alpha1
        kind: Wait
        spec:
          action: fileExists
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

	t.Run("tool schema rejects WaitPath nonEmpty with absent state", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: bad-wait
        apiVersion: deck/v1alpha1
        kind: Wait
        spec:
          action: fileAbsent
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
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: bad-wait-type
        apiVersion: deck/v1alpha1
        kind: Wait
        spec:
          action: fileExists
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

	t.Run("tool schema valid Symlink", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: symlink-runc
        apiVersion: deck/v1alpha1
        kind: Symlink
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

	t.Run("tool schema rejects Symlink missing target", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: symlink-runc
        apiVersion: deck/v1alpha1
        kind: Symlink
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

	t.Run("tool schema valid SystemdUnit", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: install-kubelet-unit
        apiVersion: deck/v1alpha1
        kind: SystemdUnit
        spec:
          path: /etc/systemd/system/kubelet.service
          mode: "0644"
          contentFromTemplate: |
            [Unit]
            Description=kubelet
          daemonReload: true
          service:
            name: kubelet
            enabled: true
            state: started
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("tool schema rejects invalid SystemdUnit", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: bad-systemd-unit
        apiVersion: deck/v1alpha1
        kind: SystemdUnit
        spec:
          path: /etc/systemd/system/kubelet.service
          content: |
            [Unit]
            Description=kubelet
          contentFromTemplate: |
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

	t.Run("tool schema valid KubeadmReset", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: reset-node
        apiVersion: deck/v1alpha1
        kind: Kubeadm
        spec:
          action: reset
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

	t.Run("tool schema valid expanded KubeadmInit", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: kubeadm-init
        apiVersion: deck/v1alpha1
        kind: Kubeadm
        spec:
          action: init
          mode: real
          outputJoinFile: /tmp/deck/join.txt
          configFile: /tmp/deck/kubeadm-init.yaml
          configTemplate: default
          pullImages: true
          kubernetesVersion: v1.30.14
          advertiseAddress: auto
          podNetworkCIDR: 10.244.0.0/16
          criSocket: unix:///run/containerd/containerd.sock
          ignorePreflightErrors: [Swap]
`)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		if err := File(path); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("tool schema rejects invalid expanded KubeadmInit shape", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: kubeadm-init
        apiVersion: deck/v1alpha1
        kind: Kubeadm
        spec:
          action: init
          mode: real
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

	t.Run("tool schema valid expanded KubeadmJoin with configFile", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: kubeadm-join
        apiVersion: deck/v1alpha1
        kind: Kubeadm
        spec:
          action: join
          mode: real
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

	t.Run("tool schema rejects KubeadmJoin with both joinFile and configFile", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: kubeadm-join
        apiVersion: deck/v1alpha1
        kind: Kubeadm
        spec:
          action: join
          mode: real
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
		content := []byte(`role: apply
version: v1alpha1
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

	t.Run("tool schema rejects reset-only fields on KubeadmJoin", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: kubeadm-join
        apiVersion: deck/v1alpha1
        kind: Kubeadm
        spec:
          action: join
          mode: real
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

	t.Run("tool schema rejects join-only fields on KubeadmInit", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: kubeadm-init
        apiVersion: deck/v1alpha1
        kind: Kubeadm
        spec:
          action: init
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

	t.Run("tool schema rejects init-only fields on KubeadmReset", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: reset-node
        apiVersion: deck/v1alpha1
        kind: Kubeadm
        spec:
          action: reset
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

	t.Run("tool schema rejects invalid KubeadmReset", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: reset-node
        apiVersion: deck/v1alpha1
        kind: Kubeadm
        spec:
          action: reset
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
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: w1
        apiVersion: deck/v1alpha1
        kind: File
        register:
          x: notARealOutput
        spec:
          action: write
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
		if got := err.Error(); !strings.Contains(got, "E_REGISTER_OUTPUT_NOT_FOUND") {
			t.Fatalf("expected E_REGISTER_OUTPUT_NOT_FOUND, got %v", err)
		}
	})

	t.Run("register output key valid for kind", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: prepare
version: v1alpha1
phases:
  - name: prepare
    steps:
      - id: d1
        apiVersion: deck/v1alpha1
        kind: File
        register:
          fetched: path
        spec:
          action: download
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
		content := []byte(`role: prepare
version: v1alpha1
phases:
  - name: prepare
    steps:
      - id: c1
        apiVersion: deck/v1alpha1
        kind: Checks
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

	t.Run("register output key valid for symlink", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "workflow.yaml")
		content := []byte(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: s1
        apiVersion: deck/v1alpha1
        kind: Symlink
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
		content := []byte(`role: prepare
version: v1alpha1
phases:
  - name: prepare
    steps:
      - id: c1
        apiVersion: deck/v1alpha1
        kind: Checks
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
	content := []byte(`role: apply
version: v1alpha1
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
	content := []byte(`role: apply
version: v1alpha1
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
	if got := err.Error(); !strings.Contains(got, fmt.Sprintf("%s:%d", path, 9)) {
		t.Fatalf("expected file and line in error, got %v", err)
	}
}

func TestSchema_RejectsTopLevelImports(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.yaml")
	content := []byte(`role: apply
version: v1alpha1
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
	content := []byte(`role: apply
version: v1alpha1
phases:
  - name: install
    imports:
      - path: ./fragments/install-common.yaml
        when: vars.osFamily == "rhel"
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
  osFamily: debian
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
	content := []byte(`role: apply
version: v1alpha1
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
	content := []byte(`role: apply
version: v1alpha1
steps:
  - id: svc
    kind: Service
    spec:
      name: containerd
      enabled: true
      state: started
  - id: svc-many
    kind: Service
    spec:
      names: [firewalld, ufw]
      daemonReload: true
      ifExists: true
      ignoreMissing: true
      state: stopped
  - id: ensure-dir
    kind: Directory
    spec:
      path: /etc/containerd/certs.d
      mode: "0755"
  - id: install-file
    kind: File
    spec:
      action: write
      path: /etc/modules-load.d/k8s.conf
      content: |
        overlay
  - id: template-file
    kind: File
    spec:
      action: write
      path: /etc/containerd/certs.d/registry.k8s.io/hosts.toml
      contentFromTemplate: |
        server = "http://registry.local"
  - id: repo-config
    kind: Repository
    spec:
      action: configure
      path: /etc/yum.repos.d/offline.repo
      repositories:
        - id: offline-base
          baseurl: file:///srv/offline-repo
          enabled: true
          gpgcheck: false
  - id: containerd-config
    kind: Containerd
    spec:
      path: /etc/containerd/config.toml
      configPath: /etc/containerd/certs.d
      systemdCgroup: true
      registryHosts:
        - registry: registry.k8s.io
          server: https://registry.k8s.io
          host: http://127.0.0.1:5000
          capabilities: [pull, resolve]
          skipVerify: true
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
	invalid := []byte(`role: apply
version: v1alpha1
steps:
  - id: bad-service
    kind: Service
    spec: {}
  - id: bad-service-both
    kind: Service
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
