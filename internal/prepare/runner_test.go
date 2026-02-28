package prepare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/taedi90/deck/internal/config"
)

func TestRun_PrepareArtifactsAndManifest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello-download-file"))
	}))
	defer server.Close()

	bundle := t.TempDir()

	wf := &config.Workflow{
		Version: "v1",
		Vars: map[string]any{
			"kubernetesVersion": "v1.30.1",
		},
		Phases: []config.Phase{
			{
				Name: "prepare",
				Steps: []config.Step{
					{
						ID:   "download-file",
						Kind: "DownloadFile",
						Spec: map[string]any{
							"source": map[string]any{"url": server.URL + "/artifact"},
							"output": map[string]any{"path": "files/artifact.bin"},
						},
					},
					{
						ID:   "download-os-packages",
						Kind: "DownloadPackages",
						Spec: map[string]any{
							"packages": []any{"containerd", "iptables"},
						},
					},
					{
						ID:   "download-k8s-packages",
						Kind: "DownloadK8sPackages",
						Spec: map[string]any{
							"kubernetesVersion": "{ .vars.kubernetesVersion }",
							"components":        []any{"kubelet"},
						},
					},
					{
						ID:   "download-images",
						Kind: "DownloadImages",
						Spec: map[string]any{
							"images": []any{"registry.k8s.io/kube-apiserver:{ .vars.kubernetesVersion }"},
						},
					},
				},
			},
		},
	}

	if err := Run(wf, RunOptions{BundleRoot: bundle}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	expectFiles := []string{
		"files/artifact.bin",
		"packages/os/containerd.txt",
		"packages/os/iptables.txt",
		"packages/k8s/kubelet-v1.30.1.txt",
		"images/registry.k8s.io_kube-apiserver_v1.30.1.tar",
		"manifest.json",
	}

	for _, rel := range expectFiles {
		abs := filepath.Join(bundle, rel)
		if _, err := os.Stat(abs); err != nil {
			t.Fatalf("expected artifact %s: %v", rel, err)
		}
	}

	manifestRaw, err := os.ReadFile(filepath.Join(bundle, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}

	var mf struct {
		Entries []struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
			Size   int64  `json:"size"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(manifestRaw, &mf); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}

	if len(mf.Entries) < 5 {
		t.Fatalf("expected >= 5 entries, got %d", len(mf.Entries))
	}
	for _, e := range mf.Entries {
		if e.Path == "" || e.SHA256 == "" || e.Size <= 0 {
			t.Fatalf("invalid manifest entry: %+v", e)
		}
	}
}

func TestRun_NoPreparePhase(t *testing.T) {
	wf := &config.Workflow{Version: "v1", Phases: []config.Phase{{Name: "install"}}}
	if err := Run(wf, RunOptions{BundleRoot: t.TempDir()}); err == nil {
		t.Fatalf("expected error when prepare phase is missing")
	}
}

func TestRun_ContainerBackendsWithFakeRunner(t *testing.T) {
	bundle := t.TempDir()
	r := &fakeRunner{}

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "prepare",
			Steps: []config.Step{
				{
					ID:   "pkg",
					Kind: "DownloadPackages",
					Spec: map[string]any{
						"packages": []any{"containerd"},
						"backend": map[string]any{
							"mode":    "container",
							"runtime": "docker",
							"image":   "ubuntu:22.04",
						},
					},
				},
				{
					ID:   "img",
					Kind: "DownloadImages",
					Spec: map[string]any{
						"images": []any{"registry.k8s.io/kube-apiserver:v1.30.1"},
						"backend": map[string]any{
							"engine": "skopeo",
							"sandbox": map[string]any{
								"mode":    "container",
								"runtime": "docker",
								"image":   "quay.io/skopeo/stable:latest",
							},
						},
					},
				},
			},
		}},
	}

	if err := Run(wf, RunOptions{BundleRoot: bundle, CommandRunner: r}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(bundle, "packages", "os", "mock-package.deb")); err != nil {
		t.Fatalf("expected mock package artifact: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bundle, "images", "registry.k8s.io_kube-apiserver_v1.30.1.tar")); err != nil {
		t.Fatalf("expected mock image artifact: %v", err)
	}
}

func TestRun_DownloadK8sPackagesContainerBackend(t *testing.T) {
	bundle := t.TempDir()
	r := &fakeRunner{}

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "prepare",
			Steps: []config.Step{
				{
					ID:   "k8s-pkgs",
					Kind: "DownloadK8sPackages",
					Spec: map[string]any{
						"kubernetesVersion": "v1.30.1",
						"components":        []any{"kubelet", "kubeadm"},
						"backend": map[string]any{
							"mode":    "container",
							"runtime": "docker",
							"image":   "ubuntu:22.04",
						},
					},
				},
			},
		}},
	}

	if err := Run(wf, RunOptions{BundleRoot: bundle, CommandRunner: r}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(bundle, "packages", "k8s", "mock-package.deb")); err != nil {
		t.Fatalf("expected mock package artifact: %v", err)
	}

	versionPath := filepath.Join(bundle, "packages", "k8s", "kubernetes-version.txt")
	raw, err := os.ReadFile(versionPath)
	if err != nil {
		t.Fatalf("expected kubernetes version metadata: %v", err)
	}
	if strings.TrimSpace(string(raw)) != "1.30.1" {
		t.Fatalf("unexpected kubernetes version metadata: %q", strings.TrimSpace(string(raw)))
	}
}

type fakeRunner struct{}

func (f *fakeRunner) LookPath(file string) (string, error) {
	if file == "docker" || file == "podman" || file == "skopeo" {
		return "/usr/bin/" + file, nil
	}
	return "", fmt.Errorf("not found")
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) error {
	if name != "docker" && name != "podman" && name != "skopeo" {
		return nil
	}

	for i := 0; i < len(args); i++ {
		if args[i] == "-v" && i+1 < len(args) {
			mount := args[i+1]
			parts := strings.SplitN(mount, ":", 2)
			if len(parts) != 2 {
				continue
			}
			host := parts[0]
			container := parts[1]
			if container == "/out" {
				if err := os.MkdirAll(host, 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(filepath.Join(host, "mock-package.deb"), []byte("pkg"), 0o644); err != nil {
					return err
				}
			}
		}
	}

	for _, a := range args {
		if strings.Contains(a, "docker-archive:/bundle/") {
			prefix := "docker-archive:/bundle/"
			s := strings.TrimPrefix(a, prefix)
			rel := strings.SplitN(s, ":", 2)[0]
			for i := 0; i < len(args); i++ {
				if args[i] == "-v" && i+1 < len(args) {
					parts := strings.SplitN(args[i+1], ":", 2)
					if len(parts) == 2 && parts[1] == "/bundle" {
						abs := filepath.Join(parts[0], filepath.FromSlash(rel))
						if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
							return err
						}
						return os.WriteFile(abs, []byte("image"), 0o644)
					}
				}
			}
		}
	}

	if name == "skopeo" {
		for _, a := range args {
			if strings.HasPrefix(a, "docker-archive:") {
				target := strings.TrimPrefix(a, "docker-archive:")
				path := strings.SplitN(target, ":", 2)[0]
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					return err
				}
				return os.WriteFile(path, []byte("image"), 0o644)
			}
		}
	}

	return nil
}
