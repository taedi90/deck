package prepare

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"github.com/taedi90/deck/internal/config"
	"github.com/taedi90/deck/internal/errcode"
	"github.com/taedi90/deck/internal/workflowcontract"
	"github.com/taedi90/deck/internal/workflowexec"
)

func nilContextForPrepareTest() context.Context { return nil }

func TestRun_PrepareArtifactAndManifest(t *testing.T) {
	imageOps := stubDownloadImageOps()

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
							"source":     map[string]any{"url": server.URL + "/artifact"},
							"outputPath": "files/artifact.bin",
						},
					},
					{
						ID:   "download-os-packages",
						Kind: "DownloadPackage",
						Spec: map[string]any{
							"packages": []any{"containerd", "iptables"},
						},
					},
					{
						ID:   "download-k8s-packages",
						Kind: "DownloadPackage",
						Spec: map[string]any{
							"packages": []any{"kubelet"},
						},
					},
					{
						ID:   "download-images",
						Kind: "DownloadImage",
						Spec: map[string]any{
							"images": []any{"registry.k8s.io/kube-apiserver:{{ .vars.kubernetesVersion }}"},
						},
					},
				},
			},
		},
	}

	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, imageDownloadOps: imageOps}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	expectFiles := []string{
		"files/artifact.bin",
		"packages/containerd.txt",
		"packages/iptables.txt",
		"packages/kubelet.txt",
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
		if !strings.HasPrefix(e.Path, "packages/") && !strings.HasPrefix(e.Path, "images/") && !strings.HasPrefix(e.Path, "files/") {
			t.Fatalf("manifest entry outside allowed prefixes: %+v", e)
		}
	}
}

func TestRun_NoPrepareSteps(t *testing.T) {
	wf := &config.Workflow{Version: "v1", Phases: []config.Phase{{Name: "install"}}}
	if err := Run(context.Background(), wf, RunOptions{BundleRoot: t.TempDir()}); err == nil {
		t.Fatalf("expected error when prepare workflow has no steps")
	}
}

func TestRun_PrepareParallelGroupRunsContainerDownloadsConcurrently(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	bundle := t.TempDir()
	r := &concurrencyRunner{delegate: &fakeRunner{}}
	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name:           "prepare",
			MaxParallelism: 2,
			Steps: []config.Step{
				{ID: "ubuntu", Kind: "DownloadPackage", ParallelGroup: "distros", Spec: map[string]any{"packages": []any{"containerd"}, "distro": map[string]any{"family": "debian", "release": "ubuntu2204"}, "repo": map[string]any{"type": "deb-flat"}, "backend": map[string]any{"mode": "container", "runtime": "docker", "image": "ubuntu:22.04"}}},
				{ID: "rhel", Kind: "DownloadPackage", ParallelGroup: "distros", Spec: map[string]any{"packages": []any{"containerd"}, "distro": map[string]any{"family": "rhel", "release": "rhel9"}, "repo": map[string]any{"type": "rpm"}, "backend": map[string]any{"mode": "container", "runtime": "docker", "image": "rockylinux:9"}}},
			},
		}},
	}

	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, CommandRunner: r}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if r.maxActive < 2 {
		t.Fatalf("expected concurrent container downloads, maxActive=%d", r.maxActive)
	}
}

func TestRun_ContainerBackendsWithFakeRunner(t *testing.T) {
	imageOps := stubDownloadImageOps()

	bundle := t.TempDir()
	r := &fakeRunner{}

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "prepare",
			Steps: []config.Step{
				{
					ID:   "pkg",
					Kind: "DownloadPackage",
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
					Kind: "DownloadImage",
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

	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, CommandRunner: r, imageDownloadOps: imageOps}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(bundle, "packages", "mock-package.deb")); err != nil {
		t.Fatalf("expected mock package artifact: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bundle, "images", "registry.k8s.io_kube-apiserver_v1.30.1.tar")); err != nil {
		t.Fatalf("expected mock image artifact: %v", err)
	}
}

func TestRun_DownloadImageUsesOutputDir(t *testing.T) {
	bundle := t.TempDir()
	imageOps := stubDownloadImageOps()
	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "prepare",
			Steps: []config.Step{{
				ID:   "img",
				Kind: "DownloadImage",
				Spec: map[string]any{
					"images":    []any{"registry.k8s.io/kube-apiserver:v1.30.1"},
					"outputDir": "images/control-plane",
				},
			}},
		}},
	}

	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, imageDownloadOps: imageOps}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bundle, "images", "control-plane", "registry.k8s.io_kube-apiserver_v1.30.1.tar")); err != nil {
		t.Fatalf("expected image artifact in custom dir: %v", err)
	}
}

func stubDownloadImageOps() imageDownloadOps {
	return imageDownloadOps{
		parseReference: func(v string) (name.Reference, error) {
			return name.ParseReference(v, name.WeakValidation)
		},
		fetchImage: func(_ name.Reference, _ ...remote.Option) (v1.Image, error) {
			return empty.Image, nil
		},
		writeArchive: func(path string, _ name.Reference, _ v1.Image, _ ...tarball.WriteOption) error {
			return os.WriteFile(path, []byte("image"), 0o644)
		},
	}
}

func TestRun_PackagesContainerBackend(t *testing.T) {
	bundle := t.TempDir()
	r := &fakeRunner{}

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "prepare",
			Steps: []config.Step{
				{
					ID:   "k8s-pkgs",
					Kind: "DownloadPackage",
					Spec: map[string]any{
						"packages": []any{"kubelet", "kubeadm"},
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

	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, CommandRunner: r}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(bundle, "packages", "mock-package.deb")); err != nil {
		t.Fatalf("expected mock package artifact: %v", err)
	}
}

func TestRun_PackagesContainerRuntimeMissing(t *testing.T) {
	bundle := t.TempDir()

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "prepare",
			Steps: []config.Step{
				{
					ID:   "pkg",
					Kind: "DownloadPackage",
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

	err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, CommandRunner: &noRuntimeRunner{}})
	if err == nil {
		t.Fatalf("expected runtime detection error")
	}
	if !strings.Contains(err.Error(), "E_PREPARE_RUNTIME_NOT_FOUND") {
		t.Fatalf("expected runtime error code, got: %v", err)
	}
}

func TestRun_PackagesContainerNoArtifact(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	bundle := t.TempDir()

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "prepare",
			Steps: []config.Step{
				{
					ID:   "pkg",
					Kind: "DownloadPackage",
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

	err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, CommandRunner: &noArtifactRunner{}})
	if err == nil {
		t.Fatalf("expected no artifacts error")
	}
	if !strings.Contains(err.Error(), "E_PREPARE_NO_ARTIFACTS") {
		t.Fatalf("expected no-artifacts error code, got: %v", err)
	}
}

func TestRun_DownloadPackageUsesOutputDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	bundle := t.TempDir()
	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "prepare",
			Steps: []config.Step{{
				ID:   "pkg",
				Kind: "DownloadPackage",
				Spec: map[string]any{
					"packages":  []any{"containerd"},
					"outputDir": "packages/custom",
				},
			}},
		}},
	}

	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bundle, "packages", "custom", "containerd.txt")); err != nil {
		t.Fatalf("expected package artifact in custom dir: %v", err)
	}
}

func TestRun_ExposesTypedRuntimeErrorCode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	bundle := t.TempDir()

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "prepare",
			Steps: []config.Step{{
				ID:   "pkg",
				Kind: "DownloadPackage",
				Spec: map[string]any{
					"packages": []any{"containerd"},
					"backend": map[string]any{
						"mode":    "container",
						"runtime": "auto",
						"image":   "ubuntu:22.04",
					},
				},
			}},
		}},
	}

	err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, CommandRunner: &noRuntimeRunner{}})
	if !errcode.Is(err, errCodePrepareRuntimeMissing) {
		t.Fatalf("expected typed code %s, got %v", errCodePrepareRuntimeMissing, err)
	}
}

func TestRun_DownloadPackageReusesExportedArtifactCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	bundle := t.TempDir()
	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "prepare",
			Steps: []config.Step{{
				ID:   "pkg-cache",
				Kind: "DownloadPackage",
				Spec: map[string]any{
					"packages": []any{"containerd"},
					"backend":  map[string]any{"mode": "container", "runtime": "docker", "image": "ubuntu:22.04"},
				},
			}},
		}},
	}
	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, CommandRunner: &fakeRunner{}}); err != nil {
		t.Fatalf("initial run failed: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(bundle, "packages")); err != nil {
		t.Fatalf("remove bundle packages: %v", err)
	}
	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, CommandRunner: &noArtifactRunner{}}); err != nil {
		t.Fatalf("expected exported cache reuse, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(bundle, "packages", "mock-package.deb")); err != nil {
		t.Fatalf("expected package restored from exported cache: %v", err)
	}
	cacheRoot := filepath.Join(home, ".cache", "deck", "artifacts", "package")
	if _, err := os.Stat(cacheRoot); err != nil {
		t.Fatalf("expected exported package cache root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".cache", "deck", "packages")); !os.IsNotExist(err) {
		t.Fatalf("expected old package-manager cache path to stay unused, got %v", err)
	}
}

func TestRun_DownloadPackageExportFailureLeavesNoBundleOutput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	bundle := t.TempDir()
	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "prepare",
			Steps: []config.Step{{
				ID:   "pkg-export-fail",
				Kind: "DownloadPackage",
				Spec: map[string]any{
					"packages": []any{"containerd"},
					"backend":  map[string]any{"mode": "container", "runtime": "docker", "image": "ubuntu:22.04"},
				},
			}},
		}},
	}
	err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, CommandRunner: &fakeRunner{failExport: true}})
	if err == nil {
		t.Fatalf("expected export failure")
	}
	if _, statErr := os.Stat(filepath.Join(bundle, "packages")); !os.IsNotExist(statErr) {
		t.Fatalf("expected no published bundle output after export failure, got %v", statErr)
	}
	cacheRoot := filepath.Join(home, ".cache", "deck", "artifacts", "package")
	entries, readErr := os.ReadDir(cacheRoot)
	if readErr == nil && len(entries) > 0 {
		t.Fatalf("expected no published cache entries after export failure")
	}
}

func TestRun_FileFallbackLocalThenBundle(t *testing.T) {
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
					"outputPath": "files/fetched.bin",
				},
			}},
		}},
	}

	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundleOut}); err != nil {
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

func TestRun_FileFallbackSourceMissing(t *testing.T) {
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
					"outputPath": "files/out.bin",
				},
			}},
		}},
	}

	err := Run(context.Background(), wf, RunOptions{BundleRoot: bundleOut})
	if err == nil {
		t.Fatalf("expected source not found error")
	}
	if !errcode.Is(err, errCodeArtifactSourceNotFound) {
		t.Fatalf("expected typed code %s, got %v", errCodeArtifactSourceNotFound, err)
	}
	if !strings.Contains(err.Error(), "E_PREPARE_SOURCE_NOT_FOUND") {
		t.Fatalf("expected E_PREPARE_SOURCE_NOT_FOUND, got %v", err)
	}
}

func TestResolveSourceBytes_PreservesContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(250 * time.Millisecond)
		_, _ = w.Write([]byte("should-not-complete"))
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := resolveSourceBytes(ctx, map[string]any{
		"fetch": map[string]any{
			"sources": []any{map[string]any{"type": "online", "url": server.URL}},
		},
	}, "files/remote.bin")
	if err == nil {
		t.Fatalf("expected context cancellation error")
	}
	if strings.Contains(err.Error(), "E_PREPARE_SOURCE_NOT_FOUND") {
		t.Fatalf("expected cancellation to not be mapped to source-not-found, got %v", err)
	}
	if !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("expected canceled context in error, got %v", err)
	}
}

func TestRun_FileFallbackRepoThenOnline(t *testing.T) {
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
					"outputPath": "files/fetched-online.bin",
				},
			}},
		}},
	}

	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundleOut}); err != nil {
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

func TestRun_FileOfflinePolicyBlocksOnlineFallback(t *testing.T) {
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
					"outputPath": "files/out.bin",
				},
			}},
		}},
	}

	err := Run(context.Background(), wf, RunOptions{BundleRoot: bundleOut})
	if err == nil {
		t.Fatalf("expected offline policy block error")
	}
	if !errcode.Is(err, errCodePrepareOfflinePolicyBlock) {
		t.Fatalf("expected typed code %s, got %v", errCodePrepareOfflinePolicyBlock, err)
	}
	if !strings.Contains(err.Error(), "E_PREPARE_OFFLINE_POLICY_BLOCK") {
		t.Fatalf("expected E_PREPARE_OFFLINE_POLICY_BLOCK, got %v", err)
	}
}

func TestRun_FileOfflinePolicyBlocksDirectURL(t *testing.T) {
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
					"source":     map[string]any{"url": server.URL + "/files/a.bin"},
					"fetch":      map[string]any{"offlineOnly": true},
					"outputPath": "files/out.bin",
				},
			}},
		}},
	}

	err := Run(context.Background(), wf, RunOptions{BundleRoot: bundleOut})
	if err == nil {
		t.Fatalf("expected offline policy block error")
	}
	if !errcode.Is(err, errCodePrepareOfflinePolicyBlock) {
		t.Fatalf("expected typed code %s, got %v", errCodePrepareOfflinePolicyBlock, err)
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
						"source":     map[string]any{"path": sourceRel},
						"fetch":      map[string]any{"sources": []any{map[string]any{"type": "local", "path": localCache}}},
						"outputPath": "files/a-out.bin",
					},
					Register: map[string]string{"downloaded": "outputPath"},
				},
				{
					ID:   "download-b",
					Kind: "DownloadFile",
					When: "vars.role == \"control-plane\"",
					Spec: map[string]any{
						"source":     map[string]any{"path": "{{ .runtime.downloaded }}"},
						"fetch":      map[string]any{"sources": []any{map[string]any{"type": "bundle", "path": bundle}}},
						"outputPath": "files/b-out.bin",
					},
				},
				{
					ID:   "skip-worker-only",
					Kind: "DownloadFile",
					When: "vars.role == \"worker\"",
					Spec: map[string]any{
						"source":     map[string]any{"path": sourceRel},
						"fetch":      map[string]any{"sources": []any{map[string]any{"type": "local", "path": localCache}}},
						"outputPath": "files/skip.bin",
					},
				},
			},
		}},
	}

	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle}); err != nil {
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
		t.Setenv("HOME", t.TempDir())
		bundle := t.TempDir()
		runner := &failOnceRunner{}
		wf := &config.Workflow{
			Version: "v1",
			Phases: []config.Phase{{
				Name: "prepare",
				Steps: []config.Step{{
					ID:    "retry-packages",
					Kind:  "DownloadPackage",
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

		if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, CommandRunner: runner}); err != nil {
			t.Fatalf("expected retry success, got %v", err)
		}
		if runner.attempts != 2 {
			t.Fatalf("expected 2 attempts, got %d", runner.attempts)
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
						"source":     map[string]any{"path": "files/missing.bin"},
						"fetch":      map[string]any{"sources": []any{map[string]any{"type": "local", "path": t.TempDir()}}},
						"outputPath": "files/retry-fail.bin",
					},
				}},
			}},
		}

		err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle})
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
				Kind: "DownloadPackage",
				When: "vars.role = \"worker\"",
				Spec: map[string]any{"packages": []any{"containerd"}},
			}},
		}},
	}

	err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle})
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
	ok, err := EvaluateWhen("vars.nodeRole == \"worker\"", vars, runtimeVars)
	if err != nil {
		t.Fatalf("expected vars namespace expression to pass, got %v", err)
	}
	if !ok {
		t.Fatalf("expected vars namespace expression to be true")
	}

	_, err = EvaluateWhen("nodeRole == \"worker\"", vars, runtimeVars)
	if err == nil {
		t.Fatalf("expected bare identifier to fail")
	}
	if !strings.Contains(err.Error(), "unknown identifier \"nodeRole\"; use vars.nodeRole") {
		t.Fatalf("expected bare identifier guidance, got %v", err)
	}

	_, err = EvaluateWhen("context.nodeRole == \"worker\"", vars, runtimeVars)
	if err == nil {
		t.Fatalf("expected context namespace to fail")
	}
	if !strings.Contains(err.Error(), "unknown identifier \"context.nodeRole\"; supported prefixes are vars. and runtime") {
		t.Fatalf("expected namespace restriction message, got %v", err)
	}

	_, err = EvaluateWhen("other.nodeRole == \"worker\"", vars, runtimeVars)
	if err == nil {
		t.Fatalf("expected unknown dotted namespace to fail")
	}
	if !strings.Contains(err.Error(), "unknown identifier \"other.nodeRole\"; supported prefixes are vars. and runtime") {
		t.Fatalf("expected namespace restriction message, got %v", err)
	}
}

func TestRunPrepareStep_DownloadFileDecodeError(t *testing.T) {
	step := config.Step{Kind: "DownloadFile", Spec: map[string]any{"source": 42}}
	key := workflowexec.StepTypeKey{APIVersion: workflowcontract.BuiltInStepAPIVersion, Kind: "DownloadFile"}
	_, _, err := runPrepareRenderedStepWithKey(context.Background(), &fakeRunner{}, t.TempDir(), step, step.Spec, key, nil, RunOptions{})
	if err == nil {
		t.Fatalf("expected decode error")
	}
	if !strings.Contains(err.Error(), "decode prepare File spec") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDownloadURLToFile_RejectsNilContext(t *testing.T) {
	target, err := os.CreateTemp(t.TempDir(), "download-*.txt")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer func() { _ = target.Close() }()
	if err := downloadURLToFile(nilContextForPrepareTest(), target, "https://example.invalid/file"); err == nil {
		t.Fatalf("expected nil context error")
	} else if !strings.Contains(err.Error(), "context is nil") {
		t.Fatalf("unexpected error: %v", err)
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
						Kind: "DownloadPackage",
						When: "runtime.hostPassed == true && vars.want == \"ok\" && runtime.host.os.family == \"debian\" && runtime.host.arch == \"arm64\"",
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

		checkRuntime := checksRuntime{
			readHostFile: func(path string) ([]byte, error) {
				switch path {
				case "/etc/os-release":
					return []byte("ID=ubuntu\nID_LIKE=debian\nVERSION=\"24.04 LTS\"\nVERSION_ID=\"24.04\"\n"), nil
				case "/proc/sys/kernel/osrelease":
					return []byte("6.8.0-test\n"), nil
				default:
					return os.ReadFile(path)
				}
			},
			currentGOOS:   func() string { return "linux" },
			currentGOARCH: func() string { return "arm64" },
		}

		if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, CommandRunner: &fakeRunner{}, checksRuntime: checkRuntime}); err != nil {
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

		checkRuntime := checksRuntime{
			readHostFile: func(path string) ([]byte, error) {
				switch path {
				case "/proc/swaps":
					return []byte("Filename\tType\tSize\tUsed\tPriority\n/dev/sda file 1 0 -2\n"), nil
				case "/proc/modules":
					return []byte("overlay 1 0 - Live 0x0\n"), nil
				default:
					return os.ReadFile(path)
				}
			},
			currentGOOS:   func() string { return "darwin" },
			currentGOARCH: func() string { return "386" },
		}

		err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, CommandRunner: &noRuntimeRunner{}, checksRuntime: checkRuntime})
		if err == nil {
			t.Fatalf("expected checkhost failure")
		}
		if !errcode.Is(err, errCodePrepareCheckHostFailed) {
			t.Fatalf("expected typed code %s, got %v", errCodePrepareCheckHostFailed, err)
		}
		if !strings.Contains(err.Error(), "E_PREPARE_CHECKHOST_FAILED") {
			t.Fatalf("expected E_PREPARE_CHECKHOST_FAILED, got %v", err)
		}
		if !strings.Contains(err.Error(), "os:") || !strings.Contains(err.Error(), "arch:") || !strings.Contains(err.Error(), "binaries:") {
			t.Fatalf("expected aggregated failures, got %v", err)
		}
	})
}

func TestRun_ExposesTypedPrepareValidationCodes(t *testing.T) {
	t.Run("unsupported image engine", func(t *testing.T) {
		bundle := t.TempDir()
		wf := &config.Workflow{
			Version: "v1",
			Phases: []config.Phase{{
				Name: "prepare",
				Steps: []config.Step{{
					ID:   "img",
					Kind: "DownloadImage",
					Spec: map[string]any{"images": []any{"registry.k8s.io/pause:3.10.1"}, "backend": map[string]any{"engine": "other"}},
				}},
			}},
		}
		err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle})
		if !errcode.Is(err, errCodePrepareEngineUnsupported) {
			t.Fatalf("expected typed code %s, got %v", errCodePrepareEngineUnsupported, err)
		}
	})

	t.Run("no package artifacts", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		bundle := t.TempDir()
		wf := &config.Workflow{
			Version: "v1",
			Phases: []config.Phase{{
				Name: "prepare",
				Steps: []config.Step{{
					ID:   "pkg",
					Kind: "DownloadPackage",
					Spec: map[string]any{"packages": []any{"containerd"}, "backend": map[string]any{"mode": "container", "runtime": "docker", "image": "ubuntu:22.04"}},
				}},
			}},
		}
		err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, CommandRunner: &noArtifactRunner{}})
		if !errcode.Is(err, errCodePrepareArtifactEmpty) {
			t.Fatalf("expected typed code %s, got %v", errCodePrepareArtifactEmpty, err)
		}
	})
}

func TestRun_PackagesKubernetesSetRepoModeDebFlatGeneratesMetadata(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	bundle := t.TempDir()
	r := &fakeRunner{}

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "prepare",
			Steps: []config.Step{{
				ID:   "pkgs",
				Kind: "DownloadPackage",
				Spec: map[string]any{
					"packages": []any{"containerd"},
					"distro": map[string]any{
						"family":  "debian",
						"release": "ubuntu2204",
					},
					"repo": map[string]any{
						"type": "deb-flat",
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

	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, CommandRunner: r}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(bundle, "packages", "deb", "ubuntu2204", "pkgs", "mock-package.deb")); err != nil {
		t.Fatalf("expected mock deb artifact: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bundle, "packages", "deb", "ubuntu2204", "Packages.gz")); err != nil {
		t.Fatalf("expected Packages.gz: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bundle, "packages", "deb", "ubuntu2204", "Release")); err != nil {
		t.Fatalf("expected Release: %v", err)
	}
}

func TestRun_PackagesKubernetesSetRepoModeRpmGeneratesRepodata(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	bundle := t.TempDir()
	r := &fakeRunner{}

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "prepare",
			Steps: []config.Step{{
				ID:   "pkgs",
				Kind: "DownloadPackage",
				Spec: map[string]any{
					"packages": []any{"containerd"},
					"distro": map[string]any{
						"family":  "rhel",
						"release": "rhel9",
					},
					"repo": map[string]any{
						"type": "rpm",
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

	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, CommandRunner: r}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(bundle, "packages", "rpm", "rhel9", "repodata", "repomd.xml")); err != nil {
		t.Fatalf("expected repodata/repomd.xml: %v", err)
	}
}

type fakeRunner struct {
	mu                sync.Mutex
	containerPayloads map[string]map[string][]byte
	failExport        bool
}

type concurrencyRunner struct {
	delegate  CommandRunner
	mu        sync.Mutex
	active    int
	maxActive int
}

type failOnceRunner struct {
	attempts int
	delegate *fakeRunner
}

type noRuntimeRunner struct{}

func (f *failOnceRunner) LookPath(file string) (string, error) {
	if file == "docker" || file == "podman" {
		return "/usr/bin/" + file, nil
	}
	return "", fmt.Errorf("not found")
}

func (f *failOnceRunner) Run(ctx context.Context, name string, args ...string) error {
	fr := f.delegate
	if fr == nil {
		fr = &fakeRunner{}
		f.delegate = fr
	}
	return fr.Run(ctx, name, args...)
}

func (f *failOnceRunner) RunWithIO(ctx context.Context, stdout io.Writer, stderr io.Writer, name string, args ...string) error {
	if (name == "docker" || name == "podman") && len(args) > 0 && args[0] == "start" {
		f.attempts++
		if f.attempts == 1 {
			return fmt.Errorf("intentional first failure")
		}
	}
	fr := f.delegate
	if fr == nil {
		fr = &fakeRunner{}
		f.delegate = fr
	}
	return fr.RunWithIO(ctx, stdout, stderr, name, args...)
}

func (n *noRuntimeRunner) LookPath(_ string) (string, error) {
	return "", fmt.Errorf("not found")
}

func (n *noRuntimeRunner) Run(_ context.Context, _ string, _ ...string) error {
	return nil
}

func (n *noRuntimeRunner) RunWithIO(_ context.Context, _ io.Writer, _ io.Writer, _ string, _ ...string) error {
	return nil
}

type noArtifactRunner struct{}

func (n *noArtifactRunner) LookPath(file string) (string, error) {
	if file == "docker" || file == "podman" {
		return "/usr/bin/" + file, nil
	}
	return "", fmt.Errorf("not found")
}

func (n *noArtifactRunner) Run(_ context.Context, _ string, _ ...string) error {
	return nil
}

func (n *noArtifactRunner) RunWithIO(_ context.Context, stdout io.Writer, _ io.Writer, name string, args ...string) error {
	if (name == "docker" || name == "podman") && len(args) > 0 && args[0] == "create" {
		_, _ = io.WriteString(stdout, "empty-container\n")
	}
	return nil
}

func (f *fakeRunner) LookPath(file string) (string, error) {
	if file == "docker" || file == "podman" {
		return "/usr/bin/" + file, nil
	}
	return "", fmt.Errorf("not found")
}

func (c *concurrencyRunner) LookPath(file string) (string, error) {
	return c.delegate.LookPath(file)
}

func (c *concurrencyRunner) Run(ctx context.Context, name string, args ...string) error {
	return c.delegate.Run(ctx, name, args...)
}

func (c *concurrencyRunner) RunWithIO(ctx context.Context, stdout io.Writer, stderr io.Writer, name string, args ...string) error {
	if name == "docker" || name == "podman" {
		c.mu.Lock()
		c.active++
		if c.active > c.maxActive {
			c.maxActive = c.active
		}
		c.mu.Unlock()
		defer func() {
			c.mu.Lock()
			c.active--
			c.mu.Unlock()
		}()
		time.Sleep(100 * time.Millisecond)
	}
	return c.delegate.RunWithIO(ctx, stdout, stderr, name, args...)
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) error {
	return f.RunWithIO(context.Background(), nil, nil, name, args...)
}

func (f *fakeRunner) RunWithIO(_ context.Context, stdout io.Writer, _ io.Writer, name string, args ...string) error {
	if name != "docker" && name != "podman" {
		return nil
	}
	if f.containerPayloads == nil {
		f.mu.Lock()
		if f.containerPayloads == nil {
			f.containerPayloads = map[string]map[string][]byte{}
		}
		f.mu.Unlock()
	}
	if len(args) == 0 {
		return nil
	}
	switch args[0] {
	case "create":
		f.mu.Lock()
		containerID := fmt.Sprintf("container-%d", len(f.containerPayloads)+1)
		f.containerPayloads[containerID] = fakePackageContainerPayload(args)
		f.mu.Unlock()
		if stdout != nil {
			_, _ = io.WriteString(stdout, containerID+"\n")
		}
		return nil
	case "start":
		return nil
	case "cp":
		if f.failExport {
			return fmt.Errorf("intentional export failure")
		}
		if len(args) < 3 {
			return fmt.Errorf("cp requires src and dst")
		}
		containerID := strings.SplitN(args[1], ":", 2)[0]
		f.mu.Lock()
		payload := f.containerPayloads[containerID]
		f.mu.Unlock()
		if stdout == nil {
			return nil
		}
		return writeFakeTar(stdout, payload)
	case "rm":
		if len(args) > 1 {
			f.mu.Lock()
			delete(f.containerPayloads, args[len(args)-1])
			f.mu.Unlock()
		}
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
				if strings.Contains(host, string(filepath.Separator)+"packages"+string(filepath.Separator)+"deb"+string(filepath.Separator)) ||
					strings.Contains(host, string(filepath.Separator)+"packages"+string(filepath.Separator)+"deb-k8s"+string(filepath.Separator)) {
					pkgs := filepath.Join(host, "pkgs")
					if err := os.MkdirAll(pkgs, 0o755); err != nil {
						return err
					}
					if strings.Contains(host, string(filepath.Separator)+"packages"+string(filepath.Separator)+"deb-k8s"+string(filepath.Separator)) {
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
				if strings.Contains(host, string(filepath.Separator)+"packages"+string(filepath.Separator)+"rpm"+string(filepath.Separator)) ||
					strings.Contains(host, string(filepath.Separator)+"packages"+string(filepath.Separator)+"rpm-k8s"+string(filepath.Separator)) {
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

func fakePackageContainerPayload(args []string) map[string][]byte {
	payload := map[string][]byte{}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "apt-ftparchive") {
		payload["pkgs/mock-package.deb"] = []byte("pkg")
		payload["Packages"] = []byte("Packages")
		payload["Packages.gz"] = []byte("Packages.gz")
		payload["Release"] = []byte("Release")
		if strings.Contains(joined, "apt-k8s") {
			payload["pkgs/kubelet.deb"] = []byte("pkg")
			payload["pkgs/kubeadm.deb"] = []byte("pkg")
			payload["pkgs/kubectl.deb"] = []byte("pkg")
			delete(payload, "pkgs/mock-package.deb")
		}
		return payload
	}
	if strings.Contains(joined, "createrepo_c") {
		payload["mock-package.rpm"] = []byte("pkg")
		payload["repodata/repomd.xml"] = []byte("repomd")
		return payload
	}
	payload["mock-package.deb"] = []byte("pkg")
	return payload
}

func writeFakeTar(w io.Writer, payload map[string][]byte) error {
	tw := tar.NewWriter(w)
	defer func() { _ = tw.Close() }()
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	seenDirs := map[string]bool{}
	for _, key := range keys {
		dir := filepath.Dir(key)
		for dir != "." && dir != "" {
			if seenDirs[dir] {
				break
			}
			seenDirs[dir] = true
			if err := tw.WriteHeader(&tar.Header{Name: filepath.ToSlash(dir), Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
				return err
			}
			dir = filepath.Dir(dir)
		}
		content := payload[key]
		if err := tw.WriteHeader(&tar.Header{Name: filepath.ToSlash(key), Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(content))}); err != nil {
			return err
		}
		if _, err := tw.Write(content); err != nil {
			return err
		}
	}
	return nil
}

func TestTemplate_RenderVarsAndRuntime(t *testing.T) {
	wf := &config.Workflow{Vars: map[string]any{"kubernetesVersion": "v1.30.1", "registry": map[string]any{"host": "registry.k8s.io"}}}
	runtimeVars := map[string]any{"downloaded": "files/a.bin"}

	rendered, err := renderSpec(map[string]any{
		"source":     map[string]any{"path": "{{ .runtime.downloaded }}"},
		"outputPath": "files/{{ .vars.kubernetesVersion }}.bin",
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
	outputPath, ok := rendered["outputPath"].(string)
	if !ok || outputPath != "files/v1.30.1.bin" {
		t.Fatalf("unexpected rendered output: %#v", rendered["outputPath"])
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
