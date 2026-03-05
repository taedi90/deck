package diagnose

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/taedi90/deck/internal/config"
)

func TestPreflight(t *testing.T) {
	t.Run("pass with manifest", func(t *testing.T) {
		dir := t.TempDir()
		bundle := filepath.Join(dir, "bundle")
		report := filepath.Join(dir, "reports", "diagnose.json")
		if err := os.MkdirAll(bundle, 0o755); err != nil {
			t.Fatalf("mkdir bundle: %v", err)
		}
		if err := os.WriteFile(filepath.Join(bundle, "manifest.json"), []byte(`{"entries":[{"path":"x","sha256":"a","size":1}]}`), 0o644); err != nil {
			t.Fatalf("write manifest: %v", err)
		}

		wf := &config.Workflow{Version: "v1", Context: config.Context{BundleRoot: bundle, StateFile: filepath.Join(dir, "state.json")}, Phases: []config.Phase{{Name: "prepare"}, {Name: "install"}}}
		out, err := Preflight(wf, RunOptions{OutputPath: report})
		if err != nil {
			t.Fatalf("expected pass, got %v", err)
		}
		if out.Summary.Failed != 0 {
			t.Fatalf("expected zero failures, got %d", out.Summary.Failed)
		}
		if _, err := os.Stat(report); err != nil {
			t.Fatalf("expected report file: %v", err)
		}
	})

	t.Run("fail without install phase", func(t *testing.T) {
		dir := t.TempDir()
		wf := &config.Workflow{Version: "v1", Context: config.Context{BundleRoot: dir, StateFile: filepath.Join(dir, "state.json")}, Phases: []config.Phase{{Name: "prepare"}}}
		if _, err := Preflight(wf, RunOptions{}); err == nil {
			t.Fatalf("expected preflight failure")
		}
	})
}

func TestPreflight_PrepareBackendChecks(t *testing.T) {
	t.Run("passes when runtime and go-containerregistry are available", func(t *testing.T) {
		dir := t.TempDir()
		bundle := filepath.Join(dir, "bundle")
		if err := os.MkdirAll(bundle, 0o755); err != nil {
			t.Fatalf("mkdir bundle: %v", err)
		}
		if err := os.WriteFile(filepath.Join(bundle, "manifest.json"), []byte(`{"entries":[{"path":"x","sha256":"a","size":1}]}`), 0o644); err != nil {
			t.Fatalf("write manifest: %v", err)
		}

		wf := &config.Workflow{
			Version: "v1",
			Context: config.Context{BundleRoot: bundle, StateFile: filepath.Join(dir, "state.json")},
			Phases: []config.Phase{
				{
					Name: "prepare",
					Steps: []config.Step{
						{
							ID:   "pkg",
							Kind: "DownloadPackages",
							Spec: map[string]any{"backend": map[string]any{"mode": "container", "runtime": "auto", "image": "ubuntu:22.04"}},
						},
						{
							ID:   "img",
							Kind: "DownloadImages",
							Spec: map[string]any{"backend": map[string]any{"engine": "go-containerregistry"}},
						},
					},
				},
				{Name: "install"},
			},
		}

		out, err := Preflight(wf, RunOptions{LookPath: availableLookPath("docker")})
		if err != nil {
			t.Fatalf("expected pass, got %v", err)
		}
		if out.Summary.Failed != 0 {
			t.Fatalf("expected zero failures, got %d", out.Summary.Failed)
		}
	})

	t.Run("fails when auto runtime is unavailable", func(t *testing.T) {
		dir := t.TempDir()
		bundle := filepath.Join(dir, "bundle")
		if err := os.MkdirAll(bundle, 0o755); err != nil {
			t.Fatalf("mkdir bundle: %v", err)
		}
		if err := os.WriteFile(filepath.Join(bundle, "manifest.json"), []byte(`{"entries":[{"path":"x","sha256":"a","size":1}]}`), 0o644); err != nil {
			t.Fatalf("write manifest: %v", err)
		}

		wf := &config.Workflow{
			Version: "v1",
			Context: config.Context{BundleRoot: bundle, StateFile: filepath.Join(dir, "state.json")},
			Phases: []config.Phase{
				{
					Name: "prepare",
					Steps: []config.Step{{
						ID:   "pkg",
						Kind: "DownloadPackages",
						Spec: map[string]any{"backend": map[string]any{"mode": "container", "runtime": "auto", "image": "ubuntu:22.04"}},
					}},
				},
				{Name: "install"},
			},
		}

		_, err := Preflight(wf, RunOptions{LookPath: availableLookPath("kubectl")})
		if err == nil {
			t.Fatalf("expected preflight failure")
		}
		if !strings.Contains(err.Error(), "preflight failed") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("fails when unsupported image engine is configured", func(t *testing.T) {
		dir := t.TempDir()
		bundle := filepath.Join(dir, "bundle")
		if err := os.MkdirAll(bundle, 0o755); err != nil {
			t.Fatalf("mkdir bundle: %v", err)
		}
		if err := os.WriteFile(filepath.Join(bundle, "manifest.json"), []byte(`{"entries":[{"path":"x","sha256":"a","size":1}]}`), 0o644); err != nil {
			t.Fatalf("write manifest: %v", err)
		}

		wf := &config.Workflow{
			Version: "v1",
			Context: config.Context{BundleRoot: bundle, StateFile: filepath.Join(dir, "state.json")},
			Phases: []config.Phase{
				{
					Name: "prepare",
					Steps: []config.Step{{
						ID:   "img",
						Kind: "DownloadImages",
						Spec: map[string]any{"backend": map[string]any{"engine": "skopeo"}},
					}},
				},
				{Name: "install"},
			},
		}

		_, err := Preflight(wf, RunOptions{LookPath: availableLookPath("docker")})
		if err == nil {
			t.Fatalf("expected preflight failure")
		}
		if !strings.Contains(err.Error(), "preflight failed") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestPreflight_HostChecks(t *testing.T) {
	t.Run("passes with host checks when prerequisites are satisfied", func(t *testing.T) {
		dir := t.TempDir()
		bundle := filepath.Join(dir, "bundle")
		if err := os.MkdirAll(bundle, 0o755); err != nil {
			t.Fatalf("mkdir bundle: %v", err)
		}
		if err := os.WriteFile(filepath.Join(bundle, "manifest.json"), []byte(`{"entries":[{"path":"x","sha256":"a","size":1}]}`), 0o644); err != nil {
			t.Fatalf("write manifest: %v", err)
		}

		wf := &config.Workflow{Version: "v1", Context: config.Context{BundleRoot: bundle, StateFile: filepath.Join(dir, "state.json")}, Phases: []config.Phase{{Name: "prepare"}, {Name: "install"}}}
		_, err := Preflight(wf, RunOptions{
			EnforceHostChecks: true,
			LookPath:          availableLookPath("timedatectl"),
			ReadFile: func(path string) ([]byte, error) {
				switch path {
				case "/etc/os-release":
					return []byte("ID=ubuntu\n"), nil
				case "/proc/swaps":
					return []byte("Filename\tType\tSize\tUsed\tPriority\n"), nil
				case "/proc/modules":
					return []byte("overlay 1 0 - Live 0x0\nbr_netfilter 1 0 - Live 0x0\n"), nil
				default:
					return nil, fmt.Errorf("unknown file")
				}
			},
			RunCommandOutput: func(name string, args ...string) ([]byte, error) {
				if name == "timedatectl" {
					return []byte("yes\n"), nil
				}
				return nil, fmt.Errorf("unsupported command")
			},
			DiskAvailableFunc: func(path string) (uint64, error) {
				return uint64(20 * 1024 * 1024 * 1024), nil
			},
		})
		if err != nil {
			t.Fatalf("expected host checks pass, got %v", err)
		}
	})

	t.Run("fails with host checks when prerequisites are missing", func(t *testing.T) {
		dir := t.TempDir()
		bundle := filepath.Join(dir, "bundle")
		if err := os.MkdirAll(bundle, 0o755); err != nil {
			t.Fatalf("mkdir bundle: %v", err)
		}
		if err := os.WriteFile(filepath.Join(bundle, "manifest.json"), []byte(`{"entries":[{"path":"x","sha256":"a","size":1}]}`), 0o644); err != nil {
			t.Fatalf("write manifest: %v", err)
		}

		wf := &config.Workflow{Version: "v1", Context: config.Context{BundleRoot: bundle, StateFile: filepath.Join(dir, "state.json")}, Phases: []config.Phase{{Name: "prepare"}, {Name: "install"}}}
		_, err := Preflight(wf, RunOptions{
			EnforceHostChecks: true,
			LookPath:          availableLookPath(),
			ReadFile: func(path string) ([]byte, error) {
				switch path {
				case "/etc/os-release":
					return []byte(""), nil
				case "/proc/swaps":
					return []byte("Filename\tType\tSize\tUsed\tPriority\n/dev/sda file 1 0 -2\n"), nil
				case "/proc/modules":
					return []byte("overlay 1 0 - Live 0x0\n"), nil
				default:
					return nil, fmt.Errorf("unknown file")
				}
			},
			RunCommandOutput: func(name string, args ...string) ([]byte, error) {
				return []byte("no\n"), nil
			},
			DiskAvailableFunc: func(path string) (uint64, error) {
				return uint64(1024), nil
			},
		})
		if err == nil {
			t.Fatalf("expected host checks failure")
		}
		if !strings.Contains(err.Error(), "preflight failed") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func availableLookPath(bins ...string) func(file string) (string, error) {
	set := map[string]bool{}
	for _, b := range bins {
		set[b] = true
	}
	return func(file string) (string, error) {
		if set[file] {
			return "/usr/bin/" + file, nil
		}
		return "", fmt.Errorf("not found")
	}
}
