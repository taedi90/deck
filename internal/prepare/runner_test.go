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

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/taedi90/deck/internal/config"
)

func TestRun_PrepareArtifactsAndManifest(t *testing.T) {
	stubImageDownload(t)

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
							"kubernetesVersion": "{{ .vars.kubernetesVersion }}",
							"components":        []any{"kubelet"},
						},
					},
					{
						ID:   "download-images",
						Kind: "DownloadImages",
						Spec: map[string]any{
							"images": []any{"registry.k8s.io/kube-apiserver:{{ .vars.kubernetesVersion }}"},
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
		"packages/containerd.txt",
		"packages/iptables.txt",
		"packages/kubelet-v1.30.1.txt",
		"images/registry.k8s.io_kube-apiserver_v1.30.1.tar",
		".deck/manifest.json",
	}

	for _, rel := range expectFiles {
		abs := filepath.Join(bundle, rel)
		if _, err := os.Stat(abs); err != nil {
			t.Fatalf("expected artifact %s: %v", rel, err)
		}
	}

	manifestRaw, err := os.ReadFile(filepath.Join(bundle, ".deck", "manifest.json"))
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
		if strings.HasPrefix(e.Path, "workflows/") || e.Path == "deck" {
			t.Fatalf("manifest must exclude workflow and root deck entries: %+v", e)
		}
		if !(strings.HasPrefix(e.Path, "packages/") || strings.HasPrefix(e.Path, "images/") || strings.HasPrefix(e.Path, "files/")) {
			t.Fatalf("manifest entry outside allowed prefixes: %+v", e)
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
	stubImageDownload(t)

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
							"engine": "go-containerregistry",
						},
					},
				},
			},
		}},
	}

	if err := Run(wf, RunOptions{BundleRoot: bundle, CommandRunner: r}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(bundle, "packages", "mock-package.deb")); err != nil {
		t.Fatalf("expected mock package artifact: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bundle, "images", "registry.k8s.io_kube-apiserver_v1.30.1.tar")); err != nil {
		t.Fatalf("expected mock image artifact: %v", err)
	}
}

func stubImageDownload(t *testing.T) {
	t.Helper()

	oldParse := parseImageReferenceFn
	oldFetch := remoteImageFetchFn
	oldWrite := tarballWriteToFileFn

	parseImageReferenceFn = func(v string) (name.Reference, error) {
		return name.ParseReference(v, name.WeakValidation)
	}
	remoteImageFetchFn = func(_ name.Reference, _ ...remote.Option) (v1.Image, error) {
		return empty.Image, nil
	}
	tarballWriteToFileFn = func(path string, _ name.Reference, _ v1.Image, _ ...tarball.WriteOption) error {
		return os.WriteFile(path, []byte("image"), 0o644)
	}

	t.Cleanup(func() {
		parseImageReferenceFn = oldParse
		remoteImageFetchFn = oldFetch
		tarballWriteToFileFn = oldWrite
	})
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

	if _, err := os.Stat(filepath.Join(bundle, "packages", "mock-package.deb")); err != nil {
		t.Fatalf("expected mock package artifact: %v", err)
	}

	versionPath := filepath.Join(bundle, "packages", "kubernetes-version.txt")
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
					When: "vars.role == \"control-plane\"",
					Spec: map[string]any{
						"source": map[string]any{"path": "{{ .runtime.downloaded }}"},
						"fetch":  map[string]any{"sources": []any{map[string]any{"type": "bundle", "path": bundle}}},
						"output": map[string]any{"path": "files/b-out.bin"},
					},
				},
				{
					ID:   "skip-worker-only",
					Kind: "DownloadFile",
					When: "vars.role == \"worker\"",
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
				When: "vars.role = \"worker\"",
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

func TestWhen_NamespaceEnforced(t *testing.T) {
	vars := map[string]any{"nodeRole": "worker"}
	runtimeVars := map[string]any{"hostPassed": true}
	ctx := map[string]any{"nodeRole": "worker"}

	ok, err := EvaluateWhen("vars.nodeRole == \"worker\"", vars, runtimeVars, ctx)
	if err != nil {
		t.Fatalf("expected vars namespace expression to pass, got %v", err)
	}
	if !ok {
		t.Fatalf("expected vars namespace expression to be true")
	}

	_, err = EvaluateWhen("nodeRole == \"worker\"", vars, runtimeVars, ctx)
	if err == nil {
		t.Fatalf("expected bare identifier to fail")
	}
	if !strings.Contains(err.Error(), "unknown identifier \"nodeRole\"; use vars.nodeRole") {
		t.Fatalf("expected bare identifier guidance, got %v", err)
	}

	_, err = EvaluateWhen("context.nodeRole == \"worker\"", vars, runtimeVars, ctx)
	if err == nil {
		t.Fatalf("expected context namespace to fail")
	}
	if !strings.Contains(err.Error(), "unknown identifier \"context.nodeRole\"; supported prefixes are vars. and runtime.") {
		t.Fatalf("expected namespace restriction message, got %v", err)
	}

	_, err = EvaluateWhen("other.nodeRole == \"worker\"", vars, runtimeVars, ctx)
	if err == nil {
		t.Fatalf("expected unknown dotted namespace to fail")
	}
	if !strings.Contains(err.Error(), "unknown identifier \"other.nodeRole\"; supported prefixes are vars. and runtime.") {
		t.Fatalf("expected namespace restriction message, got %v", err)
	}
}

func TestRun_CheckHostStep(t *testing.T) {
	t.Run("pass and register", func(t *testing.T) {
		bundle := t.TempDir()
		wf := &config.Workflow{
			Version: "v1",
			Vars:    map[string]any{"want": "ok"},
			Phases: []config.Phase{{
				Name: "prepare",
				Steps: []config.Step{
					{
						ID:       "host-check",
						Kind:     "CheckHost",
						Register: map[string]string{"hostPassed": "passed"},
						Spec: map[string]any{
							"checks":   []any{"os", "arch", "binaries"},
							"binaries": []any{"docker"},
						},
					},
					{
						ID:   "runtime-branch",
						Kind: "DownloadPackages",
						When: "runtime.hostPassed == true and vars.want == \"ok\"",
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

		oldRead := readFileFn
		oldGOOS := goosFn
		oldGOARCH := goarchFn
		readFileFn = func(path string) ([]byte, error) {
			return os.ReadFile(path)
		}
		goosFn = func() string { return "linux" }
		goarchFn = func() string { return "arm64" }
		defer func() {
			readFileFn = oldRead
			goosFn = oldGOOS
			goarchFn = oldGOARCH
		}()

		if err := Run(wf, RunOptions{BundleRoot: bundle, CommandRunner: &fakeRunner{}}); err != nil {
			t.Fatalf("expected checkhost pass, got %v", err)
		}
	})

	t.Run("failfast false aggregates errors", func(t *testing.T) {
		bundle := t.TempDir()
		wf := &config.Workflow{
			Version: "v1",
			Phases: []config.Phase{{
				Name: "prepare",
				Steps: []config.Step{{
					ID:   "host-check",
					Kind: "CheckHost",
					Spec: map[string]any{
						"checks":   []any{"os", "arch", "binaries", "swap", "kernelModules"},
						"binaries": []any{"missing-bin"},
						"failFast": false,
					},
				}},
			}},
		}

		oldRead := readFileFn
		oldGOOS := goosFn
		oldGOARCH := goarchFn
		readFileFn = func(path string) ([]byte, error) {
			switch path {
			case "/proc/swaps":
				return []byte("Filename\tType\tSize\tUsed\tPriority\n/dev/sda file 1 0 -2\n"), nil
			case "/proc/modules":
				return []byte("overlay 1 0 - Live 0x0\n"), nil
			default:
				return os.ReadFile(path)
			}
		}
		goosFn = func() string { return "darwin" }
		goarchFn = func() string { return "386" }
		defer func() {
			readFileFn = oldRead
			goosFn = oldGOOS
			goarchFn = oldGOARCH
		}()

		err := Run(wf, RunOptions{BundleRoot: bundle, CommandRunner: &noRuntimeRunner{}})
		if err == nil {
			t.Fatalf("expected checkhost failure")
		}
		if !strings.Contains(err.Error(), "E_PREPARE_CHECKHOST_FAILED") {
			t.Fatalf("expected E_PREPARE_CHECKHOST_FAILED, got %v", err)
		}
		if !strings.Contains(err.Error(), "os:") || !strings.Contains(err.Error(), "arch:") || !strings.Contains(err.Error(), "binaries:") {
			t.Fatalf("expected aggregated failures, got %v", err)
		}
	})
}

func TestRun_DownloadPackagesRepoModeAptFlatGeneratesMetadata(t *testing.T) {
	bundle := t.TempDir()
	r := &fakeRunner{}

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "prepare",
			Steps: []config.Step{{
				ID:   "pkgs",
				Kind: "DownloadPackages",
				Spec: map[string]any{
					"packages": []any{"containerd"},
					"distro": map[string]any{
						"family":  "debian",
						"release": "ubuntu2204",
					},
					"repo": map[string]any{
						"type": "apt-flat",
					},
					"backend": map[string]any{
						"mode":    "container",
						"runtime": "docker",
						"image":   "ubuntu:22.04",
					},
				},
			}},
		}},
	}

	if err := Run(wf, RunOptions{BundleRoot: bundle, CommandRunner: r}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(bundle, "packages", "apt", "ubuntu2204", "pkgs", "mock-package.deb")); err != nil {
		t.Fatalf("expected mock deb artifact: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bundle, "packages", "apt", "ubuntu2204", "Packages.gz")); err != nil {
		t.Fatalf("expected Packages.gz: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bundle, "packages", "apt", "ubuntu2204", "Release")); err != nil {
		t.Fatalf("expected Release: %v", err)
	}
}

func TestRun_DownloadPackagesRepoModeYumGeneratesRepodata(t *testing.T) {
	bundle := t.TempDir()
	r := &fakeRunner{}

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "prepare",
			Steps: []config.Step{{
				ID:   "pkgs",
				Kind: "DownloadPackages",
				Spec: map[string]any{
					"packages": []any{"containerd"},
					"distro": map[string]any{
						"family":  "rhel",
						"release": "rhel9",
					},
					"repo": map[string]any{
						"type": "yum",
					},
					"backend": map[string]any{
						"mode":    "container",
						"runtime": "docker",
						"image":   "rockylinux:9",
					},
				},
			}},
		}},
	}

	if err := Run(wf, RunOptions{BundleRoot: bundle, CommandRunner: r}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(bundle, "packages", "yum", "rhel9", "repodata", "repomd.xml")); err != nil {
		t.Fatalf("expected repodata/repomd.xml: %v", err)
	}
}

type fakeRunner struct{}

func TestRun_DownloadK8sPackagesRepoModeAptFlatGeneratesMetadata(t *testing.T) {
	bundle := t.TempDir()
	r := &fakeRunner{}

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "prepare",
			Steps: []config.Step{{
				ID:   "k8s-pkgs",
				Kind: "DownloadK8sPackages",
				Spec: map[string]any{
					"kubernetesVersion": "v1.30.1",
					"components":        []any{"kubelet", "kubeadm", "kubectl"},
					"distro": map[string]any{
						"family":  "debian",
						"release": "ubuntu2204",
					},
					"repo": map[string]any{
						"type": "apt-flat",
					},
					"backend": map[string]any{
						"mode":    "container",
						"runtime": "docker",
						"image":   "ubuntu:22.04",
					},
				},
			}},
		}},
	}

	if err := Run(wf, RunOptions{BundleRoot: bundle, CommandRunner: r}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	base := filepath.Join(bundle, "packages", "apt-k8s", "ubuntu2204")
	for _, name := range []string{"kubelet.deb", "kubeadm.deb", "kubectl.deb"} {
		if _, err := os.Stat(filepath.Join(base, "pkgs", name)); err != nil {
			t.Fatalf("expected %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(base, "Packages.gz")); err != nil {
		t.Fatalf("expected Packages.gz: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "Release")); err != nil {
		t.Fatalf("expected Release: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "kubernetes-version.txt")); err != nil {
		t.Fatalf("expected kubernetes-version.txt: %v", err)
	}
}

func TestRun_DownloadK8sPackagesRepoModeYumGeneratesRepodata(t *testing.T) {
	bundle := t.TempDir()
	r := &fakeRunner{}

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "prepare",
			Steps: []config.Step{{
				ID:   "k8s-pkgs",
				Kind: "DownloadK8sPackages",
				Spec: map[string]any{
					"kubernetesVersion": "v1.30.1",
					"components":        []any{"kubelet", "kubeadm"},
					"distro": map[string]any{
						"family":  "rhel",
						"release": "rhel9",
					},
					"repo": map[string]any{
						"type": "yum",
					},
					"backend": map[string]any{
						"mode":    "container",
						"runtime": "docker",
						"image":   "rockylinux:9",
					},
				},
			}},
		}},
	}

	if err := Run(wf, RunOptions{BundleRoot: bundle, CommandRunner: r}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	base := filepath.Join(bundle, "packages", "yum-k8s", "rhel9")
	if _, err := os.Stat(filepath.Join(base, "repodata", "repomd.xml")); err != nil {
		t.Fatalf("expected repodata/repomd.xml: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "kubernetes-version.txt")); err != nil {
		t.Fatalf("expected kubernetes-version.txt: %v", err)
	}
}

type failOnceRunner struct {
	calls int
}

type noRuntimeRunner struct{}

func (f *failOnceRunner) LookPath(file string) (string, error) {
	if file == "docker" || file == "podman" {
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
	if file == "docker" || file == "podman" {
		return "/usr/bin/" + file, nil
	}
	return "", fmt.Errorf("not found")
}

func (n *noArtifactsRunner) Run(_ context.Context, _ string, _ ...string) error {
	return nil
}

func (f *fakeRunner) LookPath(file string) (string, error) {
	if file == "docker" || file == "podman" {
		return "/usr/bin/" + file, nil
	}
	return "", fmt.Errorf("not found")
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) error {
	if name != "docker" && name != "podman" {
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
				// repo-mode simulation: create minimal artifacts + metadata
				if strings.Contains(host, string(filepath.Separator)+"packages"+string(filepath.Separator)+"apt"+string(filepath.Separator)) ||
					strings.Contains(host, string(filepath.Separator)+"packages"+string(filepath.Separator)+"apt-k8s"+string(filepath.Separator)) {
					pkgs := filepath.Join(host, "pkgs")
					if err := os.MkdirAll(pkgs, 0o755); err != nil {
						return err
					}
					if strings.Contains(host, string(filepath.Separator)+"packages"+string(filepath.Separator)+"apt-k8s"+string(filepath.Separator)) {
						for _, name := range []string{"kubelet.deb", "kubeadm.deb", "kubectl.deb"} {
							if err := os.WriteFile(filepath.Join(pkgs, name), []byte("pkg"), 0o644); err != nil {
								return err
							}
						}
					} else {
						if err := os.WriteFile(filepath.Join(pkgs, "mock-package.deb"), []byte("pkg"), 0o644); err != nil {
							return err
						}
					}
					if err := os.WriteFile(filepath.Join(host, "Packages"), []byte("Packages"), 0o644); err != nil {
						return err
					}
					if err := os.WriteFile(filepath.Join(host, "Packages.gz"), []byte("Packages.gz"), 0o644); err != nil {
						return err
					}
					if err := os.WriteFile(filepath.Join(host, "Release"), []byte("Release"), 0o644); err != nil {
						return err
					}
					continue
				}
				if strings.Contains(host, string(filepath.Separator)+"packages"+string(filepath.Separator)+"yum"+string(filepath.Separator)) ||
					strings.Contains(host, string(filepath.Separator)+"packages"+string(filepath.Separator)+"yum-k8s"+string(filepath.Separator)) {
					repodata := filepath.Join(host, "repodata")
					if err := os.MkdirAll(repodata, 0o755); err != nil {
						return err
					}
					if err := os.WriteFile(filepath.Join(host, "mock-package.rpm"), []byte("pkg"), 0o644); err != nil {
						return err
					}
					if err := os.WriteFile(filepath.Join(repodata, "repomd.xml"), []byte("repomd"), 0o644); err != nil {
						return err
					}
					continue
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

	return nil
}

func TestTemplate_RenderVarsAndRuntime(t *testing.T) {
	wf := &config.Workflow{Vars: map[string]any{"kubernetesVersion": "v1.30.1", "registry": map[string]any{"host": "registry.k8s.io"}}}
	runtimeVars := map[string]any{"downloaded": "files/a.bin"}

	rendered, err := renderSpec(map[string]any{
		"source": map[string]any{"path": "{{ .runtime.downloaded }}"},
		"output": map[string]any{"path": "files/{{ .vars.kubernetesVersion }}.bin"},
		"images": []any{
			"{{ .vars.registry.host }}/kube-apiserver:{{ .vars.kubernetesVersion }}",
			map[string]any{"tag": "{{ .runtime.downloaded }}"},
			7,
		},
	}, wf, runtimeVars)
	if err != nil {
		t.Fatalf("renderSpec failed: %v", err)
	}

	source, ok := rendered["source"].(map[string]any)
	if !ok || source["path"] != "files/a.bin" {
		t.Fatalf("unexpected rendered source: %#v", rendered["source"])
	}
	output, ok := rendered["output"].(map[string]any)
	if !ok || output["path"] != "files/v1.30.1.bin" {
		t.Fatalf("unexpected rendered output: %#v", rendered["output"])
	}
	images, ok := rendered["images"].([]any)
	if !ok {
		t.Fatalf("images should be slice, got %#v", rendered["images"])
	}
	if got := images[0]; got != "registry.k8s.io/kube-apiserver:v1.30.1" {
		t.Fatalf("unexpected rendered images[0]: %#v", got)
	}
	imageMap, ok := images[1].(map[string]any)
	if !ok || imageMap["tag"] != "files/a.bin" {
		t.Fatalf("unexpected rendered images[1]: %#v", images[1])
	}
	if got := images[2]; got != 7 {
		t.Fatalf("unexpected rendered images[2]: %#v", got)
	}

	_, err = renderSpec(map[string]any{"content": "{{ .vars.missing }}"}, wf, runtimeVars)
	if err == nil {
		t.Fatalf("expected unresolved template reference error")
	}
	if !strings.Contains(err.Error(), "spec.content") {
		t.Fatalf("expected error to include spec path, got %v", err)
	}
}
