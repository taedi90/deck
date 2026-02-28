package prepare

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

func TestRun_DownloadPackagesContainerRuntimeMissing(t *testing.T) {
	bundle := t.TempDir()

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
							"runtime": "auto",
							"image":   "ubuntu:22.04",
						},
					},
				},
			},
		}},
	}

	err := Run(wf, RunOptions{BundleRoot: bundle, CommandRunner: &noRuntimeRunner{}})
	if err == nil {
		t.Fatalf("expected runtime detection error")
	}
	if !strings.Contains(err.Error(), "E_PREPARE_RUNTIME_NOT_FOUND") {
		t.Fatalf("expected runtime error code, got: %v", err)
	}
}

func TestRun_DownloadPackagesContainerNoArtifacts(t *testing.T) {
	bundle := t.TempDir()

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
			},
		}},
	}

	err := Run(wf, RunOptions{BundleRoot: bundle, CommandRunner: &noArtifactsRunner{}})
	if err == nil {
		t.Fatalf("expected no artifacts error")
	}
	if !strings.Contains(err.Error(), "E_PREPARE_NO_ARTIFACTS") {
		t.Fatalf("expected no-artifacts error code, got: %v", err)
	}
}

func TestRun_DownloadFileFallbackLocalThenBundle(t *testing.T) {
	bundleOut := t.TempDir()
	localCache := t.TempDir()
	bundleCache := t.TempDir()

	relSource := filepath.ToSlash(filepath.Join("files", "artifact.bin"))
	bundleOnlyPath := filepath.Join(bundleCache, filepath.FromSlash(relSource))
	if err := os.MkdirAll(filepath.Dir(bundleOnlyPath), 0o755); err != nil {
		t.Fatalf("mkdir bundle cache path: %v", err)
	}
	if err := os.WriteFile(bundleOnlyPath, []byte("from-bundle-source"), 0o644); err != nil {
		t.Fatalf("write bundle cache source: %v", err)
	}
	sum := sha256.Sum256([]byte("from-bundle-source"))

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "prepare",
			Steps: []config.Step{{
				ID:   "download-file",
				Kind: "DownloadFile",
				Spec: map[string]any{
					"source": map[string]any{
						"path":   relSource,
						"sha256": hex.EncodeToString(sum[:]),
					},
					"fetch": map[string]any{
						"strategy": "fallback",
						"sources": []any{
							map[string]any{"type": "local", "path": localCache},
							map[string]any{"type": "bundle", "path": bundleCache},
						},
					},
					"output": map[string]any{"path": "files/fetched.bin"},
				},
			}},
		}},
	}

	if err := Run(wf, RunOptions{BundleRoot: bundleOut}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(bundleOut, "files", "fetched.bin"))
	if err != nil {
		t.Fatalf("read fetched output: %v", err)
	}
	if string(raw) != "from-bundle-source" {
		t.Fatalf("unexpected fetched content: %q", string(raw))
	}
}

func TestRun_DownloadFileFallbackSourceMissing(t *testing.T) {
	bundleOut := t.TempDir()

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "prepare",
			Steps: []config.Step{{
				ID:   "download-file",
				Kind: "DownloadFile",
				Spec: map[string]any{
					"source": map[string]any{
						"path": "files/missing.bin",
					},
					"fetch": map[string]any{
						"strategy": "fallback",
						"sources":  []any{map[string]any{"type": "local", "path": t.TempDir()}},
					},
					"output": map[string]any{"path": "files/out.bin"},
				},
			}},
		}},
	}

	err := Run(wf, RunOptions{BundleRoot: bundleOut})
	if err == nil {
		t.Fatalf("expected source not found error")
	}
	if !strings.Contains(err.Error(), "E_PREPARE_SOURCE_NOT_FOUND") {
		t.Fatalf("expected E_PREPARE_SOURCE_NOT_FOUND, got %v", err)
	}
}

func TestRun_DownloadFileFallbackRepoThenOnline(t *testing.T) {
	bundleOut := t.TempDir()

	repo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer repo.Close()

	online := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/files/remote.bin" {
			_, _ = w.Write([]byte("from-online-source"))
			return
		}
		http.NotFound(w, r)
	}))
	defer online.Close()

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "prepare",
			Steps: []config.Step{{
				ID:   "download-file",
				Kind: "DownloadFile",
				Spec: map[string]any{
					"source": map[string]any{
						"path": "files/remote.bin",
					},
					"fetch": map[string]any{
						"strategy": "fallback",
						"sources": []any{
							map[string]any{"type": "repo", "url": repo.URL},
							map[string]any{"type": "online", "url": online.URL},
						},
					},
					"output": map[string]any{"path": "files/fetched-online.bin"},
				},
			}},
		}},
	}

	if err := Run(wf, RunOptions{BundleRoot: bundleOut}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(bundleOut, "files", "fetched-online.bin"))
	if err != nil {
		t.Fatalf("read fetched output: %v", err)
	}
	if string(raw) != "from-online-source" {
		t.Fatalf("unexpected fetched content: %q", string(raw))
	}
}

func TestRun_DownloadFileOfflinePolicyBlocksOnlineFallback(t *testing.T) {
	bundleOut := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("should-not-be-downloaded"))
	}))
	defer server.Close()

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "prepare",
			Steps: []config.Step{{
				ID:   "download-file",
				Kind: "DownloadFile",
				Spec: map[string]any{
					"source": map[string]any{
						"path": "files/not-found.bin",
						"url":  server.URL + "/files/not-found.bin",
					},
					"fetch": map[string]any{
						"offlineOnly": true,
						"strategy":    "fallback",
						"sources":     []any{map[string]any{"type": "online", "url": server.URL}},
					},
					"output": map[string]any{"path": "files/out.bin"},
				},
			}},
		}},
	}

	err := Run(wf, RunOptions{BundleRoot: bundleOut})
	if err == nil {
		t.Fatalf("expected offline policy block error")
	}
	if !strings.Contains(err.Error(), "E_PREPARE_OFFLINE_POLICY_BLOCK") {
		t.Fatalf("expected E_PREPARE_OFFLINE_POLICY_BLOCK, got %v", err)
	}
}

func TestRun_DownloadFileOfflinePolicyBlocksDirectURL(t *testing.T) {
	bundleOut := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("should-not-be-downloaded"))
	}))
	defer server.Close()

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "prepare",
			Steps: []config.Step{{
				ID:   "download-file",
				Kind: "DownloadFile",
				Spec: map[string]any{
					"source": map[string]any{"url": server.URL + "/files/a.bin"},
					"fetch":  map[string]any{"offlineOnly": true},
					"output": map[string]any{"path": "files/out.bin"},
				},
			}},
		}},
	}

	err := Run(wf, RunOptions{BundleRoot: bundleOut})
	if err == nil {
		t.Fatalf("expected offline policy block error")
	}
	if !strings.Contains(err.Error(), "E_PREPARE_OFFLINE_POLICY_BLOCK") {
		t.Fatalf("expected E_PREPARE_OFFLINE_POLICY_BLOCK, got %v", err)
	}
}

func TestRun_WhenAndRegisterSemantics(t *testing.T) {
	bundle := t.TempDir()
	localCache := t.TempDir()

	sourceRel := filepath.ToSlash(filepath.Join("files", "a.bin"))
	sourceAbs := filepath.Join(localCache, filepath.FromSlash(sourceRel))
	if err := os.MkdirAll(filepath.Dir(sourceAbs), 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.WriteFile(sourceAbs, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	wf := &config.Workflow{
		Version: "v1",
		Vars:    map[string]any{"role": "control-plane"},
		Phases: []config.Phase{{
			Name: "prepare",
			Steps: []config.Step{
				{
					ID:   "download-a",
					Kind: "DownloadFile",
					Spec: map[string]any{
						"source": map[string]any{"path": sourceRel},
						"fetch":  map[string]any{"sources": []any{map[string]any{"type": "local", "path": localCache}}},
						"output": map[string]any{"path": "files/a-out.bin"},
					},
					Register: map[string]string{"downloaded": "path"},
				},
				{
					ID:   "download-b",
					Kind: "DownloadFile",
					When: "role == \"control-plane\"",
					Spec: map[string]any{
						"source": map[string]any{"path": "{ .runtime.downloaded }"},
						"fetch":  map[string]any{"sources": []any{map[string]any{"type": "bundle", "path": bundle}}},
						"output": map[string]any{"path": "files/b-out.bin"},
					},
				},
				{
					ID:   "skip-worker-only",
					Kind: "DownloadFile",
					When: "role == \"worker\"",
					Spec: map[string]any{
						"source": map[string]any{"path": sourceRel},
						"fetch":  map[string]any{"sources": []any{map[string]any{"type": "local", "path": localCache}}},
						"output": map[string]any{"path": "files/skip.bin"},
					},
				},
			},
		}},
	}

	if err := Run(wf, RunOptions{BundleRoot: bundle}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(bundle, "files", "a-out.bin")); err != nil {
		t.Fatalf("expected a-out artifact: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bundle, "files", "b-out.bin")); err != nil {
		t.Fatalf("expected b-out artifact: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bundle, "files", "skip.bin")); err == nil {
		t.Fatalf("expected skipped artifact to not exist")
	}
}

func TestRun_RetrySemantics(t *testing.T) {
	t.Run("retry succeeds on second attempt", func(t *testing.T) {
		bundle := t.TempDir()
		runner := &failOnceRunner{}
		wf := &config.Workflow{
			Version: "v1",
			Phases: []config.Phase{{
				Name: "prepare",
				Steps: []config.Step{{
					ID:    "retry-packages",
					Kind:  "DownloadPackages",
					Retry: 1,
					Spec: map[string]any{
						"packages": []any{"containerd"},
						"backend": map[string]any{
							"mode":    "container",
							"runtime": "docker",
							"image":   "ubuntu:22.04",
						},
					},
				}},
			}},
		}

		if err := Run(wf, RunOptions{BundleRoot: bundle, CommandRunner: runner}); err != nil {
			t.Fatalf("expected retry success, got %v", err)
		}
		if runner.calls != 2 {
			t.Fatalf("expected 2 attempts, got %d", runner.calls)
		}
	})

	t.Run("retry exhausted keeps failure", func(t *testing.T) {
		bundle := t.TempDir()

		wf := &config.Workflow{
			Version: "v1",
			Phases: []config.Phase{{
				Name: "prepare",
				Steps: []config.Step{{
					ID:    "retry-fail",
					Kind:  "DownloadFile",
					Retry: 1,
					Spec: map[string]any{
						"source": map[string]any{"path": "files/missing.bin"},
						"fetch":  map[string]any{"sources": []any{map[string]any{"type": "local", "path": t.TempDir()}}},
						"output": map[string]any{"path": "files/retry-fail.bin"},
					},
				}},
			}},
		}

		err := Run(wf, RunOptions{BundleRoot: bundle})
		if err == nil {
			t.Fatalf("expected failure after retry exhaustion")
		}
		if !strings.Contains(err.Error(), "E_PREPARE_SOURCE_NOT_FOUND") {
			t.Fatalf("expected E_PREPARE_SOURCE_NOT_FOUND, got %v", err)
		}
	})
}

func TestRun_WhenInvalidExpression(t *testing.T) {
	bundle := t.TempDir()
	wf := &config.Workflow{
		Version: "v1",
		Vars:    map[string]any{"role": "worker"},
		Phases: []config.Phase{{
			Name: "prepare",
			Steps: []config.Step{{
				ID:   "bad-when",
				Kind: "DownloadPackages",
				When: "role = \"worker\"",
				Spec: map[string]any{"packages": []any{"containerd"}},
			}},
		}},
	}

	err := Run(wf, RunOptions{BundleRoot: bundle})
	if err == nil {
		t.Fatalf("expected condition eval error")
	}
	if !strings.Contains(err.Error(), "E_CONDITION_EVAL") {
		t.Fatalf("expected E_CONDITION_EVAL, got %v", err)
	}
}

type fakeRunner struct{}

type failOnceRunner struct {
	calls int
}

type noRuntimeRunner struct{}

func (f *failOnceRunner) LookPath(file string) (string, error) {
	if file == "docker" || file == "podman" || file == "skopeo" {
		return "/usr/bin/" + file, nil
	}
	return "", fmt.Errorf("not found")
}

func (f *failOnceRunner) Run(ctx context.Context, name string, args ...string) error {
	f.calls++
	if f.calls == 1 {
		return fmt.Errorf("intentional first failure")
	}
	fr := &fakeRunner{}
	return fr.Run(ctx, name, args...)
}

func (n *noRuntimeRunner) LookPath(_ string) (string, error) {
	return "", fmt.Errorf("not found")
}

func (n *noRuntimeRunner) Run(_ context.Context, _ string, _ ...string) error {
	return nil
}

type noArtifactsRunner struct{}

func (n *noArtifactsRunner) LookPath(file string) (string, error) {
	if file == "docker" || file == "podman" || file == "skopeo" {
		return "/usr/bin/" + file, nil
	}
	return "", fmt.Errorf("not found")
}

func (n *noArtifactsRunner) Run(_ context.Context, _ string, _ ...string) error {
	return nil
}

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
