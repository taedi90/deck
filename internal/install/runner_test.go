package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/taedi90/deck/internal/config"
)

func TestRun_InstallTools(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state", "state.json")
	bundle := filepath.Join(dir, "bundle")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	artifact := filepath.Join(bundle, "files", "a.txt")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatalf("mkdir files: %v", err)
	}
	if err := os.WriteFile(artifact, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := writeManifestForTest(bundle, "files/a.txt", []byte("ok")); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	fileA := filepath.Join(dir, "a.txt")
	fileB := filepath.Join(dir, "b.txt")
	sysctlPath := filepath.Join(dir, "sysctl.conf")
	modprobePath := filepath.Join(dir, "modules.conf")
	joinPath := filepath.Join(dir, "join.txt")
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	fakeApt := filepath.Join(binDir, "apt-get")
	if err := os.WriteFile(fakeApt, []byte("#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake apt-get: %v", err)
	}
	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", fmt.Sprintf("%s:%s", binDir, originalPath))

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "install",
			Steps: []config.Step{
				{ID: "install-packages", Kind: "InstallPackages", Spec: map[string]any{"packages": []any{"containerd"}}},
				{ID: "write-file", Kind: "WriteFile", Spec: map[string]any{"path": fileA, "content": "hello world"}},
				{ID: "edit-file", Kind: "EditFile", Spec: map[string]any{"path": fileA, "edits": []any{map[string]any{"op": "replace", "match": "world", "with": "deck"}}}},
				{ID: "copy-file", Kind: "CopyFile", Spec: map[string]any{"src": fileA, "dest": fileB}},
				{ID: "sysctl", Kind: "Sysctl", Spec: map[string]any{"writeFile": sysctlPath, "values": map[string]any{"net.ipv4.ip_forward": "1"}}},
				{ID: "modprobe", Kind: "Modprobe", Spec: map[string]any{"modules": []any{"overlay", "br_netfilter"}, "persistFile": modprobePath}},
				{ID: "run-cmd", Kind: "RunCommand", Spec: map[string]any{"command": []any{"true"}}},
				{ID: "kubeadm-init", Kind: "KubeadmInit", Spec: map[string]any{"outputJoinFile": joinPath}},
				{ID: "kubeadm-join", Kind: "KubeadmJoin", Spec: map[string]any{"joinFile": joinPath}},
			},
		}},
	}

	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	contentA, err := os.ReadFile(fileA)
	if err != nil {
		t.Fatalf("read fileA: %v", err)
	}
	if string(contentA) != "hello deck" {
		t.Fatalf("unexpected edited content: %q", string(contentA))
	}

	if _, err := os.Stat(fileB); err != nil {
		t.Fatalf("copied file missing: %v", err)
	}
	if _, err := os.Stat(sysctlPath); err != nil {
		t.Fatalf("sysctl file missing: %v", err)
	}
	if _, err := os.Stat(modprobePath); err != nil {
		t.Fatalf("modprobe persist file missing: %v", err)
	}
	if _, err := os.Stat(joinPath); err != nil {
		t.Fatalf("join file missing: %v", err)
	}

	rawState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var st State
	if err := json.Unmarshal(rawState, &st); err != nil {
		t.Fatalf("parse state: %v", err)
	}
	if len(st.CompletedSteps) != 9 {
		t.Fatalf("expected 9 completed steps, got %d", len(st.CompletedSteps))
	}
}

func TestRun_DefaultStatePathUsesHomeStateKey(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	bundle := filepath.Join(dir, "bundle")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	artifact := filepath.Join(bundle, "files", "a.txt")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatalf("mkdir files: %v", err)
	}
	if err := os.WriteFile(artifact, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := writeManifestForTest(bundle, "files/a.txt", []byte("ok")); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	t.Setenv("HOME", home)

	wf := &config.Workflow{
		StateKey: "state-key-default-path-test",
		Phases: []config.Phase{{
			Name:  "install",
			Steps: []config.Step{{ID: "s1", Kind: "RunCommand", Spec: map[string]any{"command": []any{"true"}}}},
		}},
	}

	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	statePath := filepath.Join(home, ".deck", "state", wf.StateKey+".json")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("state file missing at expected home path: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bundle, ".deck", "state.json")); !os.IsNotExist(err) {
		t.Fatalf("unexpected bundle state file, err=%v", err)
	}
}

func TestRun_ManifestIntegrityVerified(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "bundle")
	statePath := filepath.Join(dir, "state", "state.json")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	artifact := filepath.Join(bundle, "files", "a.txt")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatalf("mkdir files: %v", err)
	}
	if err := os.WriteFile(artifact, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := writeManifestForTest(bundle, "files/a.txt", []byte("hello")); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name:  "install",
			Steps: []config.Step{{ID: "s1", Kind: "RunCommand", Spec: map[string]any{"command": []any{"true"}}}},
		}},
	}

	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath}); err != nil {
		t.Fatalf("expected install success, got %v", err)
	}
}

func TestRun_ManifestIntegrityMismatch(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "bundle")
	statePath := filepath.Join(dir, "state", "state.json")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	artifact := filepath.Join(bundle, "files", "a.txt")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatalf("mkdir files: %v", err)
	}
	if err := os.WriteFile(artifact, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := writeManifestForTest(bundle, "files/a.txt", []byte("different")); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name:  "install",
			Steps: []config.Step{{ID: "s1", Kind: "RunCommand", Spec: map[string]any{"command": []any{"true"}}}},
		}},
	}

	err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath})
	if err == nil {
		t.Fatalf("expected manifest integrity error")
	}
	if !strings.Contains(err.Error(), "E_BUNDLE_INTEGRITY") {
		t.Fatalf("expected E_BUNDLE_INTEGRITY error, got %v", err)
	}
}

func TestRun_ResumeFromFailedStep(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state", "state.json")
	bundle := filepath.Join(dir, "bundle")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	artifact := filepath.Join(bundle, "files", "a.txt")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatalf("mkdir files: %v", err)
	}
	if err := os.WriteFile(artifact, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := writeManifestForTest(bundle, "files/a.txt", []byte("ok")); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	first := filepath.Join(dir, "first.txt")
	second := filepath.Join(dir, "second.txt")

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "install",
			Steps: []config.Step{
				{ID: "s1", Kind: "WriteFile", Spec: map[string]any{"path": first, "content": "ok"}},
				{ID: "s2", Kind: "RunCommand", Spec: map[string]any{"command": []any{"false"}}},
				{ID: "s3", Kind: "WriteFile", Spec: map[string]any{"path": second, "content": "done"}},
			},
		}},
	}

	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath}); err == nil {
		t.Fatalf("expected failure on s2")
	}

	rawState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var st State
	if err := json.Unmarshal(rawState, &st); err != nil {
		t.Fatalf("parse state: %v", err)
	}
	if len(st.CompletedSteps) != 1 || st.CompletedSteps[0] != "s1" {
		t.Fatalf("unexpected completed steps after failure: %#v", st.CompletedSteps)
	}
	if st.FailedStep != "s2" {
		t.Fatalf("expected failed step s2, got %q", st.FailedStep)
	}

	wf.Phases[0].Steps[1].Spec = map[string]any{"command": []any{"true"}}
	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath}); err != nil {
		t.Fatalf("resume run failed: %v", err)
	}

	if _, err := os.Stat(second); err != nil {
		t.Fatalf("expected second file after resume: %v", err)
	}

	rawState, err = os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read final state: %v", err)
	}
	var final State
	if err := json.Unmarshal(rawState, &final); err != nil {
		t.Fatalf("parse final state: %v", err)
	}
	if len(final.CompletedSteps) != 3 {
		t.Fatalf("expected all steps completed, got %d", len(final.CompletedSteps))
	}
	if final.FailedStep != "" {
		t.Fatalf("expected empty failed step, got %q", final.FailedStep)
	}
}

func TestRun_UnsupportedInstallKindFails(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "bundle")
	statePath := filepath.Join(dir, "state", "state.json")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	artifact := filepath.Join(bundle, "files", "a.txt")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatalf("mkdir files: %v", err)
	}
	if err := os.WriteFile(artifact, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := writeManifestForTest(bundle, "files/a.txt", []byte("ok")); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name:  "install",
			Steps: []config.Step{{ID: "x", Kind: "UnknownKind", Spec: map[string]any{}}},
		}},
	}

	err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath})
	if err == nil {
		t.Fatalf("expected unsupported kind error")
	}
	if !strings.Contains(err.Error(), "E_INSTALL_KIND_UNSUPPORTED") {
		t.Fatalf("expected E_INSTALL_KIND_UNSUPPORTED, got %v", err)
	}
}

func TestRun_RunCommandErrorCodes(t *testing.T) {
	t.Run("non-zero exit", func(t *testing.T) {
		dir := t.TempDir()
		bundle := filepath.Join(dir, "bundle")
		statePath := filepath.Join(dir, "state", "state.json")
		if err := os.MkdirAll(bundle, 0o755); err != nil {
			t.Fatalf("mkdir bundle: %v", err)
		}
		artifact := filepath.Join(bundle, "files", "a.txt")
		if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
			t.Fatalf("mkdir files: %v", err)
		}
		if err := os.WriteFile(artifact, []byte("ok"), 0o644); err != nil {
			t.Fatalf("write artifact: %v", err)
		}
		if err := writeManifestForTest(bundle, "files/a.txt", []byte("ok")); err != nil {
			t.Fatalf("write manifest: %v", err)
		}

		wf := &config.Workflow{
			Version: "v1",
			Phases: []config.Phase{{
				Name:  "install",
				Steps: []config.Step{{ID: "cmd", Kind: "RunCommand", Spec: map[string]any{"command": []any{"false"}}}},
			}},
		}

		err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath})
		if err == nil {
			t.Fatalf("expected run command failure")
		}
		if !strings.Contains(err.Error(), "E_INSTALL_RUNCOMMAND_FAILED") {
			t.Fatalf("expected E_INSTALL_RUNCOMMAND_FAILED, got %v", err)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		dir := t.TempDir()
		bundle := filepath.Join(dir, "bundle")
		statePath := filepath.Join(dir, "state", "state.json")
		if err := os.MkdirAll(bundle, 0o755); err != nil {
			t.Fatalf("mkdir bundle: %v", err)
		}
		artifact := filepath.Join(bundle, "files", "a.txt")
		if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
			t.Fatalf("mkdir files: %v", err)
		}
		if err := os.WriteFile(artifact, []byte("ok"), 0o644); err != nil {
			t.Fatalf("write artifact: %v", err)
		}
		if err := writeManifestForTest(bundle, "files/a.txt", []byte("ok")); err != nil {
			t.Fatalf("write manifest: %v", err)
		}

		wf := &config.Workflow{
			Version: "v1",
			Phases: []config.Phase{{
				Name: "install",
				Steps: []config.Step{{
					ID:   "cmd",
					Kind: "RunCommand",
					Spec: map[string]any{"command": []any{"sleep", "1"}, "timeout": "10ms"},
				}},
			}},
		}

		err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath})
		if err == nil {
			t.Fatalf("expected run command timeout")
		}
		if !strings.Contains(err.Error(), "E_INSTALL_RUNCOMMAND_TIMEOUT") {
			t.Fatalf("expected E_INSTALL_RUNCOMMAND_TIMEOUT, got %v", err)
		}
	})

	t.Run("timeout classification", func(t *testing.T) {
		dir := t.TempDir()
		bundle := filepath.Join(dir, "bundle")
		statePath := filepath.Join(dir, "state", "state.json")
		if err := os.MkdirAll(bundle, 0o755); err != nil {
			t.Fatalf("mkdir bundle: %v", err)
		}
		artifact := filepath.Join(bundle, "files", "a.txt")
		if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
			t.Fatalf("mkdir files: %v", err)
		}
		if err := os.WriteFile(artifact, []byte("ok"), 0o644); err != nil {
			t.Fatalf("write artifact: %v", err)
		}
		if err := writeManifestForTest(bundle, "files/a.txt", []byte("ok")); err != nil {
			t.Fatalf("write manifest: %v", err)
		}

		fakeList := filepath.Join(dir, "fake-images-timeout.sh")
		script := "#!/usr/bin/env bash\nset -euo pipefail\nsleep 1\n"
		if err := os.WriteFile(fakeList, []byte(script), 0o755); err != nil {
			t.Fatalf("write fake list script: %v", err)
		}

		wf := &config.Workflow{
			Version: "v1",
			Phases: []config.Phase{{
				Name: "install",
				Steps: []config.Step{{
					ID:   "verify-images",
					Kind: "VerifyImages",
					Spec: map[string]any{
						"images":  []any{"registry.k8s.io/pause:3.10.1"},
						"command": []any{fakeList},
						"timeout": "20ms",
					},
				}},
			}},
		}

		err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath})
		if err == nil {
			t.Fatalf("expected verify images timeout")
		}
		if !strings.Contains(err.Error(), errCodeInstallImagesCmdFailed) {
			t.Fatalf("expected verify images error code, got %v", err)
		}
		if !strings.Contains(err.Error(), "image verification timed out") {
			t.Fatalf("expected timeout classification, got %v", err)
		}
	})
}

func TestRun_KubeadmJoinMissingFileErrorCode(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "bundle")
	statePath := filepath.Join(dir, "state", "state.json")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	artifact := filepath.Join(bundle, "files", "a.txt")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatalf("mkdir files: %v", err)
	}
	if err := os.WriteFile(artifact, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := writeManifestForTest(bundle, "files/a.txt", []byte("ok")); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "install",
			Steps: []config.Step{{
				ID:   "join",
				Kind: "KubeadmJoin",
				Spec: map[string]any{"joinFile": filepath.Join(dir, "missing-join.txt")},
			}},
		}},
	}

	err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath})
	if err == nil {
		t.Fatalf("expected kubeadm join missing file error")
	}
	if !strings.Contains(err.Error(), "E_INSTALL_KUBEADM_JOIN_FILE_NOT_FOUND") {
		t.Fatalf("expected E_INSTALL_KUBEADM_JOIN_FILE_NOT_FOUND, got %v", err)
	}
}

func TestRun_VerifyImages(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		dir := t.TempDir()
		bundle := filepath.Join(dir, "bundle")
		statePath := filepath.Join(dir, "state", "state.json")
		if err := os.MkdirAll(bundle, 0o755); err != nil {
			t.Fatalf("mkdir bundle: %v", err)
		}
		artifact := filepath.Join(bundle, "files", "a.txt")
		if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
			t.Fatalf("mkdir files: %v", err)
		}
		if err := os.WriteFile(artifact, []byte("ok"), 0o644); err != nil {
			t.Fatalf("write artifact: %v", err)
		}
		if err := writeManifestForTest(bundle, "files/a.txt", []byte("ok")); err != nil {
			t.Fatalf("write manifest: %v", err)
		}

		fakeList := filepath.Join(dir, "fake-images-list.sh")
		script := "#!/usr/bin/env bash\nset -euo pipefail\nprintf 'registry.k8s.io/pause:3.10.1\\nregistry.k8s.io/kube-apiserver:v1.30.14\\n'\n"
		if err := os.WriteFile(fakeList, []byte(script), 0o755); err != nil {
			t.Fatalf("write fake list script: %v", err)
		}

		wf := &config.Workflow{
			Version: "v1",
			Phases: []config.Phase{{
				Name: "install",
				Steps: []config.Step{{
					ID:   "verify-images",
					Kind: "VerifyImages",
					Spec: map[string]any{
						"images":  []any{"registry.k8s.io/pause:3.10.1"},
						"command": []any{fakeList},
					},
				}},
			}},
		}

		if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath}); err != nil {
			t.Fatalf("verify images should succeed: %v", err)
		}
	})

	t.Run("missing image", func(t *testing.T) {
		dir := t.TempDir()
		bundle := filepath.Join(dir, "bundle")
		statePath := filepath.Join(dir, "state", "state.json")
		if err := os.MkdirAll(bundle, 0o755); err != nil {
			t.Fatalf("mkdir bundle: %v", err)
		}
		artifact := filepath.Join(bundle, "files", "a.txt")
		if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
			t.Fatalf("mkdir files: %v", err)
		}
		if err := os.WriteFile(artifact, []byte("ok"), 0o644); err != nil {
			t.Fatalf("write artifact: %v", err)
		}
		if err := writeManifestForTest(bundle, "files/a.txt", []byte("ok")); err != nil {
			t.Fatalf("write manifest: %v", err)
		}

		fakeList := filepath.Join(dir, "fake-images-list.sh")
		script := "#!/usr/bin/env bash\nset -euo pipefail\nprintf 'registry.k8s.io/kube-apiserver:v1.30.14\\n'\n"
		if err := os.WriteFile(fakeList, []byte(script), 0o755); err != nil {
			t.Fatalf("write fake list script: %v", err)
		}

		wf := &config.Workflow{
			Version: "v1",
			Phases: []config.Phase{{
				Name: "install",
				Steps: []config.Step{{
					ID:   "verify-images",
					Kind: "VerifyImages",
					Spec: map[string]any{
						"images":  []any{"registry.k8s.io/pause:3.10.1"},
						"command": []any{fakeList},
					},
				}},
			}},
		}

		err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath})
		if err == nil {
			t.Fatalf("expected missing image error")
		}
		if !strings.Contains(err.Error(), "E_INSTALL_VERIFY_IMAGES_NOT_FOUND") {
			t.Fatalf("expected E_INSTALL_VERIFY_IMAGES_NOT_FOUND, got %v", err)
		}
	})
}

func TestRun_KubeadmRealMode(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "bundle")
	statePath := filepath.Join(dir, "state", "state.json")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	artifact := filepath.Join(bundle, "files", "a.txt")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatalf("mkdir files: %v", err)
	}
	if err := os.WriteFile(artifact, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := writeManifestForTest(bundle, "files/a.txt", []byte("ok")); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	fakeKubeadmPath := filepath.Join(binDir, "kubeadm")
	fakeScript := "#!/usr/bin/env bash\nset -euo pipefail\nif [[ \"${1:-}\" == \"init\" ]]; then\n  exit 0\nfi\nif [[ \"${1:-}\" == \"token\" && \"${2:-}\" == \"create\" ]]; then\n  echo \"kubeadm join 10.1.0.10:6443 --token fake.token --discovery-token-ca-cert-hash sha256:fake\"\n  exit 0\nfi\nif [[ \"${1:-}\" == \"join\" ]]; then\n  exit 0\nfi\necho \"unsupported kubeadm invocation\" >&2\nexit 1\n"
	if err := os.WriteFile(fakeKubeadmPath, []byte(fakeScript), 0o755); err != nil {
		t.Fatalf("write fake kubeadm: %v", err)
	}

	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", fmt.Sprintf("%s:%s", binDir, originalPath))

	joinPath := filepath.Join(dir, "join.txt")
	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "install",
			Steps: []config.Step{
				{ID: "kubeadm-init", Kind: "KubeadmInit", Spec: map[string]any{"mode": "real", "outputJoinFile": joinPath}},
				{ID: "kubeadm-join", Kind: "KubeadmJoin", Spec: map[string]any{"mode": "real", "joinFile": joinPath}},
			},
		}},
	}

	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath}); err != nil {
		t.Fatalf("real kubeadm mode run failed: %v", err)
	}

	joinRaw, err := os.ReadFile(joinPath)
	if err != nil {
		t.Fatalf("read join file: %v", err)
	}
	if !strings.Contains(string(joinRaw), "kubeadm join") {
		t.Fatalf("expected real join command in join file, got %q", string(joinRaw))
	}
}

func TestRun_KubeadmJoinRealModeRejectsInvalidCommand(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "bundle")
	statePath := filepath.Join(dir, "state", "state.json")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	artifact := filepath.Join(bundle, "files", "a.txt")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatalf("mkdir files: %v", err)
	}
	if err := os.WriteFile(artifact, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := writeManifestForTest(bundle, "files/a.txt", []byte("ok")); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	joinPath := filepath.Join(dir, "join.txt")
	if err := os.WriteFile(joinPath, []byte("echo not-kubeadm\n"), 0o644); err != nil {
		t.Fatalf("write join file: %v", err)
	}

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "install",
			Steps: []config.Step{{
				ID:   "kubeadm-join",
				Kind: "KubeadmJoin",
				Spec: map[string]any{"mode": "real", "joinFile": joinPath},
			}},
		}},
	}

	err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath})
	if err == nil {
		t.Fatalf("expected invalid join command error")
	}
	if !strings.Contains(err.Error(), "E_INSTALL_KUBEADM_JOIN_COMMAND_INVALID") {
		t.Fatalf("expected E_INSTALL_KUBEADM_JOIN_COMMAND_INVALID, got %v", err)
	}
}

func TestRun_WhenAndRegisterSemantics(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "bundle")
	statePath := filepath.Join(dir, "state", "state.json")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	artifact := filepath.Join(bundle, "files", "a.txt")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatalf("mkdir files: %v", err)
	}
	if err := os.WriteFile(artifact, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := writeManifestForTest(bundle, "files/a.txt", []byte("ok")); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	joinPath := filepath.Join(dir, "join.txt")
	registeredOutputPath := filepath.Join(dir, "registered.txt")
	skippedOutputPath := filepath.Join(dir, "skipped.txt")

	wf := &config.Workflow{
		Version: "v1",
		Vars:    map[string]any{"role": "control-plane"},
		Phases: []config.Phase{{
			Name: "install",
			Steps: []config.Step{
				{ID: "init", Kind: "KubeadmInit", Spec: map[string]any{"outputJoinFile": joinPath}, Register: map[string]string{"workerJoinFile": "joinFile"}},
				{ID: "use-register", Kind: "WriteFile", When: "vars.role == \"control-plane\"", Spec: map[string]any{"path": registeredOutputPath, "content": "{{ .runtime.workerJoinFile }}"}},
				{ID: "skip-worker", Kind: "WriteFile", When: "vars.role == \"worker\"", Spec: map[string]any{"path": skippedOutputPath, "content": "worker"}},
			},
		}},
	}

	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	raw, err := os.ReadFile(registeredOutputPath)
	if err != nil {
		t.Fatalf("read registered output: %v", err)
	}
	if strings.TrimSpace(string(raw)) != joinPath {
		t.Fatalf("expected registered content to be %q, got %q", joinPath, strings.TrimSpace(string(raw)))
	}

	if _, err := os.Stat(skippedOutputPath); err == nil {
		t.Fatalf("expected skipped step output to not exist")
	}

	stateRaw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var st State
	if err := json.Unmarshal(stateRaw, &st); err != nil {
		t.Fatalf("parse state: %v", err)
	}
	if st.RuntimeVars["workerJoinFile"] != joinPath {
		t.Fatalf("expected runtime var workerJoinFile=%q, got %#v", joinPath, st.RuntimeVars["workerJoinFile"])
	}
	if len(st.SkippedSteps) != 1 || st.SkippedSteps[0] != "skip-worker" {
		t.Fatalf("unexpected skipped steps: %#v", st.SkippedSteps)
	}
}

func TestRun_RetrySemantics(t *testing.T) {
	t.Run("retry succeeds on second attempt", func(t *testing.T) {
		dir := t.TempDir()
		bundle := filepath.Join(dir, "bundle")
		statePath := filepath.Join(dir, "state", "state.json")
		if err := os.MkdirAll(bundle, 0o755); err != nil {
			t.Fatalf("mkdir bundle: %v", err)
		}
		artifact := filepath.Join(bundle, "files", "a.txt")
		if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
			t.Fatalf("mkdir files: %v", err)
		}
		if err := os.WriteFile(artifact, []byte("ok"), 0o644); err != nil {
			t.Fatalf("write artifact: %v", err)
		}
		if err := writeManifestForTest(bundle, "files/a.txt", []byte("ok")); err != nil {
			t.Fatalf("write manifest: %v", err)
		}

		marker := filepath.Join(dir, "marker")
		scriptPath := filepath.Join(dir, "fail-once.sh")
		script := "#!/usr/bin/env bash\nset -euo pipefail\nif [[ ! -f \"" + marker + "\" ]]; then\n  touch \"" + marker + "\"\n  exit 1\nfi\nexit 0\n"
		if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
			t.Fatalf("write script: %v", err)
		}

		wf := &config.Workflow{
			Version: "v1",
			Phases: []config.Phase{{
				Name:  "install",
				Steps: []config.Step{{ID: "retry-cmd", Kind: "RunCommand", Retry: 1, Spec: map[string]any{"command": []any{scriptPath}}}},
			}},
		}

		if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath}); err != nil {
			t.Fatalf("expected retry success, got %v", err)
		}
	})

	t.Run("retry exhausted keeps failure", func(t *testing.T) {
		dir := t.TempDir()
		bundle := filepath.Join(dir, "bundle")
		statePath := filepath.Join(dir, "state", "state.json")
		if err := os.MkdirAll(bundle, 0o755); err != nil {
			t.Fatalf("mkdir bundle: %v", err)
		}
		artifact := filepath.Join(bundle, "files", "a.txt")
		if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
			t.Fatalf("mkdir files: %v", err)
		}
		if err := os.WriteFile(artifact, []byte("ok"), 0o644); err != nil {
			t.Fatalf("write artifact: %v", err)
		}
		if err := writeManifestForTest(bundle, "files/a.txt", []byte("ok")); err != nil {
			t.Fatalf("write manifest: %v", err)
		}

		counterPath := filepath.Join(dir, "counter")
		scriptPath := filepath.Join(dir, "always-fail.sh")
		script := "#!/usr/bin/env bash\nset -euo pipefail\ncount=0\nif [[ -f \"" + counterPath + "\" ]]; then\n  count=$(cat \"" + counterPath + "\")\nfi\ncount=$((count+1))\necho \"${count}\" > \"" + counterPath + "\"\nexit 1\n"
		if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
			t.Fatalf("write script: %v", err)
		}

		wf := &config.Workflow{
			Version: "v1",
			Phases: []config.Phase{{
				Name:  "install",
				Steps: []config.Step{{ID: "retry-cmd", Kind: "RunCommand", Retry: 1, Spec: map[string]any{"command": []any{scriptPath}}}},
			}},
		}

		err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath})
		if err == nil {
			t.Fatalf("expected failure after retry exhaustion")
		}

		counterRaw, err := os.ReadFile(counterPath)
		if err != nil {
			t.Fatalf("read counter: %v", err)
		}
		if strings.TrimSpace(string(counterRaw)) != "2" {
			t.Fatalf("expected 2 attempts with retry=1, got %q", strings.TrimSpace(string(counterRaw)))
		}
	})
}

func TestRun_RetryStopsWhenParentContextDone(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "bundle")
	statePath := filepath.Join(dir, "state", "state.json")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	artifact := filepath.Join(bundle, "files", "a.txt")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatalf("mkdir files: %v", err)
	}
	if err := os.WriteFile(artifact, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := writeManifestForTest(bundle, "files/a.txt", []byte("ok")); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	counterPath := filepath.Join(dir, "counter")
	scriptPath := filepath.Join(dir, "slow-fail.sh")
	script := "#!/usr/bin/env bash\nset -euo pipefail\ncount=0\nif [[ -f \"" + counterPath + "\" ]]; then\n  count=$(cat \"" + counterPath + "\")\nfi\ncount=$((count+1))\necho \"${count}\" > \"" + counterPath + "\"\nsleep 1\nexit 1\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name:  "install",
			Steps: []config.Step{{ID: "retry-cmd", Kind: "RunCommand", Retry: 4, Spec: map[string]any{"command": []any{scriptPath}, "timeout": "5s"}}},
		}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := Run(ctx, wf, RunOptions{BundleRoot: bundle, StatePath: statePath})
	if err == nil {
		t.Fatalf("expected parent context cancellation")
	}

	counterRaw, readErr := os.ReadFile(counterPath)
	if readErr != nil {
		t.Fatalf("read counter: %v", readErr)
	}
	if strings.TrimSpace(string(counterRaw)) != "1" {
		t.Fatalf("expected exactly one attempt when parent context ends, got %q", strings.TrimSpace(string(counterRaw)))
	}
}

func TestRun_RunCommandParentCancelNotRelabeledAsTimeout(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "bundle")
	statePath := filepath.Join(dir, "state", "state.json")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	artifact := filepath.Join(bundle, "files", "a.txt")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatalf("mkdir files: %v", err)
	}
	if err := os.WriteFile(artifact, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := writeManifestForTest(bundle, "files/a.txt", []byte("ok")); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name:  "install",
			Steps: []config.Step{{ID: "cmd", Kind: "RunCommand", Spec: map[string]any{"command": []any{"true"}, "timeout": "3s"}}},
		}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Run(ctx, wf, RunOptions{BundleRoot: bundle, StatePath: statePath})
	if err == nil {
		t.Fatalf("expected canceled context error")
	}
	if strings.Contains(err.Error(), errCodeInstallCommandTimeout) {
		t.Fatalf("expected parent cancellation to not be mapped to timeout, got %v", err)
	}
	if !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("expected canceled context in error, got %v", err)
	}
}

func TestRun_DownloadFileRespectsParentContext(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "bundle")
	statePath := filepath.Join(dir, "state", "state.json")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	artifact := filepath.Join(bundle, "files", "a.txt")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatalf("mkdir files: %v", err)
	}
	if err := os.WriteFile(artifact, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := writeManifestForTest(bundle, "files/a.txt", []byte("ok")); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(250 * time.Millisecond)
		_, _ = w.Write([]byte("payload"))
	}))
	defer srv.Close()

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "install",
			Steps: []config.Step{{
				ID:   "download",
				Kind: "DownloadFile",
				Spec: map[string]any{"source": map[string]any{"url": srv.URL + "/files/payload.txt"}, "output": map[string]any{"path": "files/payload.txt"}},
			}},
		}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := Run(ctx, wf, RunOptions{BundleRoot: bundle, StatePath: statePath})
	if err == nil {
		t.Fatalf("expected download cancellation")
	}
	if !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("expected deadline exceeded in error, got %v", err)
	}
}

func TestResolveSourceBytes_PreservesContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(250 * time.Millisecond)
		_, _ = w.Write([]byte("payload"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := resolveSourceBytes(ctx, map[string]any{
		"fetch": map[string]any{
			"sources": []any{map[string]any{"type": "online", "url": srv.URL}},
		},
	}, "files/payload.txt")
	if err == nil {
		t.Fatalf("expected context cancellation error")
	}
	if strings.Contains(err.Error(), "E_INSTALL_SOURCE_NOT_FOUND") {
		t.Fatalf("expected cancellation to not be mapped to source-not-found, got %v", err)
	}
	if !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("expected canceled context in error, got %v", err)
	}
}

func TestRunCommandOutputWithContext_TimeoutReturnsSentinel(t *testing.T) {
	_, err := runCommandOutputWithContext(context.Background(), []string{"sleep", "1"}, 10*time.Millisecond)
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if !errors.Is(err, errStepCommandTimeout) {
		t.Fatalf("expected step timeout sentinel, got %v", err)
	}
}

func TestRun_ContainerdConfigDefaultGenerationRespectsParentContext(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "bundle")
	statePath := filepath.Join(dir, "state", "state.json")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	artifact := filepath.Join(bundle, "files", "a.txt")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatalf("mkdir files: %v", err)
	}
	if err := os.WriteFile(artifact, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := writeManifestForTest(bundle, "files/a.txt", []byte("ok")); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	fakeContainerd := filepath.Join(binDir, "containerd")
	script := "#!/usr/bin/env bash\nset -euo pipefail\nif [[ \"${1:-}\" == \"config\" && \"${2:-}\" == \"default\" ]]; then\n  sleep 1\n  echo 'version = 2'\n  exit 0\nfi\nexit 1\n"
	if err := os.WriteFile(fakeContainerd, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake containerd: %v", err)
	}
	t.Setenv("PATH", fmt.Sprintf("%s:%s", binDir, os.Getenv("PATH")))

	target := filepath.Join(dir, "containerd", "config.toml")
	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name:  "install",
			Steps: []config.Step{{ID: "containerd-config", Kind: "ContainerdConfig", Spec: map[string]any{"path": target, "timeout": "5s"}}},
		}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := Run(ctx, wf, RunOptions{BundleRoot: bundle, StatePath: statePath})
	if err == nil {
		t.Fatalf("expected canceled containerd config generation")
	}
	if !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("expected deadline exceeded in error, got %v", err)
	}
}

func TestRun_ContainerdConfigDefaultGenerationTimeoutUsesTimeoutClassification(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "bundle")
	statePath := filepath.Join(dir, "state", "state.json")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	artifact := filepath.Join(bundle, "files", "a.txt")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatalf("mkdir files: %v", err)
	}
	if err := os.WriteFile(artifact, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := writeManifestForTest(bundle, "files/a.txt", []byte("ok")); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	fakeContainerd := filepath.Join(binDir, "containerd")
	script := "#!/usr/bin/env bash\nset -euo pipefail\nif [[ \"${1:-}\" == \"config\" && \"${2:-}\" == \"default\" ]]; then\n  sleep 1\n  echo 'version = 2'\n  exit 0\nfi\nexit 1\n"
	if err := os.WriteFile(fakeContainerd, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake containerd: %v", err)
	}
	t.Setenv("PATH", fmt.Sprintf("%s:%s", binDir, os.Getenv("PATH")))

	target := filepath.Join(dir, "containerd", "config.toml")
	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name:  "install",
			Steps: []config.Step{{ID: "containerd-config", Kind: "ContainerdConfig", Spec: map[string]any{"path": target, "timeout": "20ms"}}},
		}},
	}

	err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath})
	if err == nil {
		t.Fatalf("expected containerd config timeout")
	}
	if !strings.Contains(err.Error(), "containerd config default generation timed out") {
		t.Fatalf("expected timeout classification, got %v", err)
	}
}

func TestRun_WhenInvalidExpression(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "bundle")
	statePath := filepath.Join(dir, "state", "state.json")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	artifact := filepath.Join(bundle, "files", "a.txt")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatalf("mkdir files: %v", err)
	}
	if err := os.WriteFile(artifact, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := writeManifestForTest(bundle, "files/a.txt", []byte("ok")); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	wf := &config.Workflow{
		Version: "v1",
		Vars:    map[string]any{"role": "worker"},
		Phases: []config.Phase{{
			Name: "install",
			Steps: []config.Step{{
				ID:   "bad-when",
				Kind: "RunCommand",
				When: "vars.role = \"worker\"",
				Spec: map[string]any{"command": []any{"true"}},
			}},
		}},
	}

	err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath})
	if err == nil {
		t.Fatalf("expected condition evaluation failure")
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
	if !strings.Contains(err.Error(), "unknown identifier \"context.nodeRole\"; supported prefixes are vars. and runtime") {
		t.Fatalf("expected namespace restriction message, got %v", err)
	}

	_, err = EvaluateWhen("other.nodeRole == \"worker\"", vars, runtimeVars, ctx)
	if err == nil {
		t.Fatalf("expected unknown dotted namespace to fail")
	}
	if !strings.Contains(err.Error(), "unknown identifier \"other.nodeRole\"; supported prefixes are vars. and runtime") {
		t.Fatalf("expected namespace restriction message, got %v", err)
	}
}

func TestRun_InstallPackagesExecutesPackageManager(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "bundle")
	statePath := filepath.Join(dir, "state", "state.json")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	artifact := filepath.Join(bundle, "files", "a.txt")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatalf("mkdir files: %v", err)
	}
	if err := os.WriteFile(artifact, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := writeManifestForTest(bundle, "files/a.txt", []byte("ok")); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	marker := filepath.Join(dir, "apt-invoked.txt")
	fakeApt := filepath.Join(binDir, "apt-get")
	script := "#!/usr/bin/env bash\nset -euo pipefail\necho \"$*\" > \"" + marker + "\"\nexit 0\n"
	if err := os.WriteFile(fakeApt, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake apt-get: %v", err)
	}

	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", fmt.Sprintf("%s:%s", binDir, originalPath))

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "install",
			Steps: []config.Step{{
				ID:   "install-pkgs",
				Kind: "InstallPackages",
				Spec: map[string]any{"packages": []any{"containerd", "kubelet"}},
			}},
		}},
	}

	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath}); err != nil {
		t.Fatalf("expected install packages success, got %v", err)
	}

	raw, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	args := strings.TrimSpace(string(raw))
	if !strings.Contains(args, "install -y containerd kubelet") {
		t.Fatalf("unexpected apt-get args: %q", args)
	}
}

func TestRun_InstallPackagesSourcePathValidation(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "bundle")
	statePath := filepath.Join(dir, "state", "state.json")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	artifact := filepath.Join(bundle, "files", "a.txt")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatalf("mkdir files: %v", err)
	}
	if err := os.WriteFile(artifact, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := writeManifestForTest(bundle, "files/a.txt", []byte("ok")); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "install",
			Steps: []config.Step{{
				ID:   "install-pkgs",
				Kind: "InstallPackages",
				Spec: map[string]any{
					"packages": []any{"containerd"},
					"source":   map[string]any{"type": "local-repo", "path": filepath.Join(dir, "missing")},
				},
			}},
		}},
	}

	err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath})
	if err == nil {
		t.Fatalf("expected source validation error")
	}
	if !strings.Contains(err.Error(), "E_INSTALL_PACKAGES_SOURCE_INVALID") {
		t.Fatalf("expected E_INSTALL_PACKAGES_SOURCE_INVALID, got %v", err)
	}
}

func TestRun_InstallPackagesInstallsFromLocalRepo(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "bundle")
	statePath := filepath.Join(dir, "state", "state.json")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	artifact := filepath.Join(bundle, "files", "a.txt")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatalf("mkdir files: %v", err)
	}
	if err := os.WriteFile(artifact, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := writeManifestForTest(bundle, "files/a.txt", []byte("ok")); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	repoDir := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	debA := filepath.Join(repoDir, "containerd_1.0.0_amd64.deb")
	debB := filepath.Join(repoDir, "kubelet_1.30.1_amd64.deb")
	if err := os.WriteFile(debA, []byte("deb-a"), 0o644); err != nil {
		t.Fatalf("write debA: %v", err)
	}
	if err := os.WriteFile(debB, []byte("deb-b"), 0o644); err != nil {
		t.Fatalf("write debB: %v", err)
	}

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	marker := filepath.Join(dir, "apt-local-invoked.txt")
	fakeApt := filepath.Join(binDir, "apt-get")
	script := "#!/usr/bin/env bash\nset -euo pipefail\necho \"$*\" > \"" + marker + "\"\nexit 0\n"
	if err := os.WriteFile(fakeApt, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake apt-get: %v", err)
	}

	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", fmt.Sprintf("%s:%s", binDir, originalPath))

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "install",
			Steps: []config.Step{{
				ID:   "install-pkgs",
				Kind: "InstallPackages",
				Spec: map[string]any{
					"packages": []any{"containerd", "kubelet"},
					"source":   map[string]any{"type": "local-repo", "path": repoDir},
				},
			}},
		}},
	}

	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath}); err != nil {
		t.Fatalf("expected local repo install success, got %v", err)
	}

	raw, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	args := strings.TrimSpace(string(raw))
	if !strings.Contains(args, "install -y") {
		t.Fatalf("unexpected apt-get args: %q", args)
	}
	if !strings.Contains(args, debA) || !strings.Contains(args, debB) {
		t.Fatalf("local deb artifacts were not passed to apt-get: %q", args)
	}
}

func TestRun_InstallPackagesTimeoutUsesTimeoutClassification(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "bundle")
	statePath := filepath.Join(dir, "state", "state.json")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	artifact := filepath.Join(bundle, "files", "a.txt")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatalf("mkdir files: %v", err)
	}
	if err := os.WriteFile(artifact, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := writeManifestForTest(bundle, "files/a.txt", []byte("ok")); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	fakeApt := filepath.Join(binDir, "apt-get")
	script := "#!/usr/bin/env bash\nset -euo pipefail\nsleep 1\n"
	if err := os.WriteFile(fakeApt, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake apt-get: %v", err)
	}
	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", fmt.Sprintf("%s:%s", binDir, originalPath))

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "install",
			Steps: []config.Step{{
				ID:      "install-pkgs",
				Kind:    "InstallPackages",
				Timeout: "20ms",
				Spec:    map[string]any{"packages": []any{"containerd"}},
			}},
		}},
	}

	err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath})
	if err == nil {
		t.Fatalf("expected install packages timeout")
	}
	if !strings.Contains(err.Error(), errCodeInstallPkgFailed) {
		t.Fatalf("expected package install error code, got %v", err)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout classification, got %v", err)
	}
	if strings.Contains(err.Error(), "package installation failed") {
		t.Fatalf("expected timeout path instead of generic failure, got %v", err)
	}
}

func TestTemplate_RenderVarsAndRuntime(t *testing.T) {
	wf := &config.Workflow{Vars: map[string]any{"kubernetesVersion": "v1.30.1", "registry": map[string]any{"host": "registry.k8s.io"}}}
	runtimeVars := map[string]any{"joinFile": "/tmp/join.txt"}

	rendered, err := renderSpec(map[string]any{
		"path": "{{ .runtime.joinFile }}",
		"nested": map[string]any{
			"image": "{{ .vars.registry.host }}/kube-apiserver:{{ .vars.kubernetesVersion }}",
		},
		"items": []any{
			"{{ .vars.kubernetesVersion }}",
			map[string]any{"join": "{{ .runtime.joinFile }}"},
			123,
		},
	}, wf, runtimeVars)
	if err != nil {
		t.Fatalf("renderSpec failed: %v", err)
	}

	if got := rendered["path"]; got != "/tmp/join.txt" {
		t.Fatalf("unexpected rendered path: %#v", got)
	}
	nested, ok := rendered["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested should be map, got %#v", rendered["nested"])
	}
	if got := nested["image"]; got != "registry.k8s.io/kube-apiserver:v1.30.1" {
		t.Fatalf("unexpected rendered image: %#v", got)
	}
	items, ok := rendered["items"].([]any)
	if !ok {
		t.Fatalf("items should be slice, got %#v", rendered["items"])
	}
	if got := items[0]; got != "v1.30.1" {
		t.Fatalf("unexpected rendered items[0]: %#v", got)
	}
	itemMap, ok := items[1].(map[string]any)
	if !ok || itemMap["join"] != "/tmp/join.txt" {
		t.Fatalf("unexpected rendered items[1]: %#v", items[1])
	}
	if got := items[2]; got != 123 {
		t.Fatalf("unexpected rendered items[2]: %#v", got)
	}

	_, err = renderSpec(map[string]any{"content": "{{ .runtime.missing }}"}, wf, runtimeVars)
	if err == nil {
		t.Fatalf("expected unresolved template reference error")
	}
	if !strings.Contains(err.Error(), "spec.content") {
		t.Fatalf("expected error to include spec path, got %v", err)
	}
}

func TestEditFileBackup_DefaultEnabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target.conf")
	if err := os.WriteFile(path, []byte("mode=old\n"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	spec := map[string]any{
		"path":  path,
		"edits": []any{map[string]any{"match": "mode=old", "with": "mode=new"}},
	}
	if err := runEditFile(spec); err != nil {
		t.Fatalf("runEditFile failed: %v", err)
	}

	backups, err := listEditFileBackups(path)
	if err != nil {
		t.Fatalf("list backups: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected 1 backup, got %d", len(backups))
	}

	raw, err := os.ReadFile(filepath.Join(filepath.Dir(path), backups[0]))
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(raw) != "mode=old\n" {
		t.Fatalf("unexpected backup content: %q", string(raw))
	}
}

func TestEditFileBackup_OptOutDisabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target.conf")
	if err := os.WriteFile(path, []byte("mode=old\n"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	spec := map[string]any{
		"path":   path,
		"backup": false,
		"edits":  []any{map[string]any{"match": "mode=old", "with": "mode=new"}},
	}
	if err := runEditFile(spec); err != nil {
		t.Fatalf("runEditFile failed: %v", err)
	}

	backups, err := listEditFileBackups(path)
	if err != nil {
		t.Fatalf("list backups: %v", err)
	}
	if len(backups) != 0 {
		t.Fatalf("expected no backup files, got %d", len(backups))
	}
}

func TestEditFileBackup_RetentionKeepsLatestTen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target.conf")
	if err := os.WriteFile(path, []byte("mode=old\n"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	oldest := filepath.Base(path) + ".bak-20240101T000000Z"
	oldestPath := filepath.Join(dir, oldest)
	if err := os.WriteFile(oldestPath, []byte("oldest"), 0o644); err != nil {
		t.Fatalf("write oldest backup: %v", err)
	}
	if err := os.Chtimes(oldestPath, time.Unix(1, 0), time.Unix(1, 0)); err != nil {
		t.Fatalf("chtimes oldest: %v", err)
	}

	for i := 1; i < 10; i++ {
		name := fmt.Sprintf("%s.bak-20240101T0000%02dZ", filepath.Base(path), i)
		backupPath := filepath.Join(dir, name)
		if err := os.WriteFile(backupPath, []byte("existing"), 0o644); err != nil {
			t.Fatalf("write existing backup %d: %v", i, err)
		}
		stamp := time.Unix(int64(100+i), 0)
		if err := os.Chtimes(backupPath, stamp, stamp); err != nil {
			t.Fatalf("chtimes existing backup %d: %v", i, err)
		}
	}

	spec := map[string]any{
		"path":  path,
		"edits": []any{map[string]any{"match": "mode=old", "with": "mode=new"}},
	}
	if err := runEditFile(spec); err != nil {
		t.Fatalf("runEditFile failed: %v", err)
	}

	backups, err := listEditFileBackups(path)
	if err != nil {
		t.Fatalf("list backups: %v", err)
	}
	if len(backups) != 10 {
		t.Fatalf("expected 10 backup files, got %d", len(backups))
	}
	if _, err := os.Stat(oldestPath); !os.IsNotExist(err) {
		t.Fatalf("expected oldest backup to be removed, err=%v", err)
	}
}

func TestEditFileBackup_NameFormatAndCollisionSuffix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target.conf")
	if err := os.WriteFile(path, []byte("mode=old\n"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	basePattern := regexp.MustCompile(`^target\.conf\.bak-\d{8}T\d{6}Z$`)
	collisionPattern := regexp.MustCompile(`^target\.conf\.bak-\d{8}T\d{6}Z-[0-9a-f]{8}$`)

	for i := 0; i < 20; i++ {
		first, err := createEditFileBackup(path, []byte("old"))
		if err != nil {
			t.Fatalf("create first backup: %v", err)
		}
		second, err := createEditFileBackup(path, []byte("old"))
		if err != nil {
			t.Fatalf("create second backup: %v", err)
		}

		firstName := filepath.Base(first)
		if !basePattern.MatchString(firstName) {
			t.Fatalf("unexpected first backup name format: %q", firstName)
		}

		secondName := filepath.Base(second)
		if strings.HasPrefix(secondName, firstName+"-") {
			if !collisionPattern.MatchString(secondName) {
				t.Fatalf("unexpected collision backup name format: %q", secondName)
			}
			return
		}

		if err := os.Remove(first); err != nil {
			t.Fatalf("remove first backup retry %d: %v", i, err)
		}
		if err := os.Remove(second); err != nil {
			t.Fatalf("remove second backup retry %d: %v", i, err)
		}
	}

	t.Fatalf("expected at least one same-second collision with suffix")
}

func TestEditFileBackup_CreateFailureIncludesBackupPath(t *testing.T) {
	dir := t.TempDir()
	readOnlyDir := filepath.Join(dir, "readonly")
	if err := os.MkdirAll(readOnlyDir, 0o755); err != nil {
		t.Fatalf("mkdir readonly dir: %v", err)
	}
	path := filepath.Join(readOnlyDir, "target.conf")
	if err := os.WriteFile(path, []byte("mode=old\n"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	if err := os.Chmod(readOnlyDir, 0o555); err != nil {
		t.Fatalf("chmod readonly dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(readOnlyDir, 0o755)
	})

	err := runEditFile(map[string]any{
		"path":  path,
		"edits": []any{map[string]any{"match": "mode=old", "with": "mode=new"}},
	})
	if err == nil {
		t.Fatalf("expected backup creation failure")
	}

	if !strings.Contains(err.Error(), path+".bak-") {
		t.Fatalf("expected error to include backup path prefix %q, got %v", path+".bak-", err)
	}
}

func TestServiceStep(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	logPath := filepath.Join(dir, "systemctl.log")
	scriptPath := filepath.Join(binDir, "systemctl")
	script := "#!/usr/bin/env bash\nset -euo pipefail\nif [[ \"${1:-}\" == \"is-enabled\" ]]; then\n  exit 1\nfi\nif [[ \"${1:-}\" == \"is-active\" ]]; then\n  exit 1\nfi\nprintf '%s\\n' \"$*\" >> \"" + logPath + "\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write systemctl script: %v", err)
	}
	t.Setenv("PATH", fmt.Sprintf("%s:%s", binDir, os.Getenv("PATH")))

	if err := runService(map[string]any{"name": "containerd", "enabled": true, "state": "started"}); err != nil {
		t.Fatalf("runService failed: %v", err)
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	got := string(raw)
	if !strings.Contains(got, "enable containerd") || !strings.Contains(got, "start containerd") {
		t.Fatalf("expected enable/start invocations, got %q", got)
	}
}

func TestEnsureDirStep(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a", "b")
	if err := runEnsureDir(map[string]any{"path": target, "mode": "0750"}); err != nil {
		t.Fatalf("runEnsureDir failed: %v", err)
	}
	if err := runEnsureDir(map[string]any{"path": target, "mode": "0750"}); err != nil {
		t.Fatalf("runEnsureDir second pass failed: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}
	if info.Mode().Perm() != 0o750 {
		t.Fatalf("unexpected mode: %o", info.Mode().Perm())
	}
}

func TestInstallFileStep(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "installed.txt")
	spec := map[string]any{"path": target, "content": "hello", "mode": "0640"}
	if err := runInstallFile(spec); err != nil {
		t.Fatalf("runInstallFile failed: %v", err)
	}
	before, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat before: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if err := runInstallFile(spec); err != nil {
		t.Fatalf("runInstallFile second pass failed: %v", err)
	}
	after, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("expected idempotent write to keep mtime")
	}
}

func TestTemplateFileStep(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "templated.txt")
	if err := runTemplateFile(map[string]any{"path": target, "template": "line", "mode": "0644"}); err != nil {
		t.Fatalf("runTemplateFile failed: %v", err)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(raw) != "line\n" {
		t.Fatalf("unexpected content: %q", string(raw))
	}
}

func TestRepoConfigStep(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "repo", "offline.repo")
	spec := map[string]any{
		"path": target,
		"repositories": []any{map[string]any{
			"id":       "offline-base",
			"name":     "offline-base",
			"baseurl":  "file:///srv/repo",
			"enabled":  true,
			"gpgcheck": false,
		}},
	}
	if err := runRepoConfig(spec); err != nil {
		t.Fatalf("runRepoConfig failed: %v", err)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(raw), "[offline-base]") || !strings.Contains(string(raw), "baseurl=file:///srv/repo") {
		t.Fatalf("unexpected repo config: %q", string(raw))
	}
}

func TestContainerdConfigStep(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.toml")
	initial := "config_path = \"\"\n            SystemdCgroup = false\n"
	if err := os.WriteFile(target, []byte(initial), 0o644); err != nil {
		t.Fatalf("write initial config: %v", err)
	}
	spec := map[string]any{"path": target, "configPath": "/etc/containerd/certs.d", "systemdCgroup": true}
	if err := runContainerdConfig(context.Background(), spec); err != nil {
		t.Fatalf("runContainerdConfig failed: %v", err)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	got := string(raw)
	if !strings.Contains(got, "config_path = \"/etc/containerd/certs.d\"") || !strings.Contains(got, "SystemdCgroup = true") {
		t.Fatalf("unexpected config content: %q", got)
	}
}

func TestSwapStep(t *testing.T) {
	dir := t.TempDir()
	fstab := filepath.Join(dir, "fstab")
	content := "UUID=abc / ext4 defaults 0 1\n/swapfile none swap sw 0 0\n"
	if err := os.WriteFile(fstab, []byte(content), 0o644); err != nil {
		t.Fatalf("write fstab: %v", err)
	}
	if err := runSwap(map[string]any{"disable": false, "persist": true, "fstabPath": fstab}); err != nil {
		t.Fatalf("runSwap failed: %v", err)
	}
	raw, err := os.ReadFile(fstab)
	if err != nil {
		t.Fatalf("read fstab: %v", err)
	}
	if !strings.Contains(string(raw), "# /swapfile none swap sw 0 0") {
		t.Fatalf("expected swap line to be commented: %q", string(raw))
	}
}

func TestKernelModuleStep(t *testing.T) {
	dir := t.TempDir()
	persistPath := filepath.Join(dir, "modules-load.d", "k8s.conf")
	spec := map[string]any{"name": "overlay", "load": false, "persist": true, "persistFile": persistPath}
	if err := runKernelModule(spec); err != nil {
		t.Fatalf("runKernelModule failed: %v", err)
	}
	if err := runKernelModule(spec); err != nil {
		t.Fatalf("runKernelModule second pass failed: %v", err)
	}
	raw, err := os.ReadFile(persistPath)
	if err != nil {
		t.Fatalf("read persist file: %v", err)
	}
	if strings.Count(string(raw), "overlay") != 1 {
		t.Fatalf("expected single module line, got %q", string(raw))
	}
}

func TestSysctlApplyStep(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	logPath := filepath.Join(dir, "sysctl.log")
	scriptPath := filepath.Join(binDir, "sysctl")
	script := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n' \"$*\" >> \"" + logPath + "\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write sysctl script: %v", err)
	}
	t.Setenv("PATH", fmt.Sprintf("%s:%s", binDir, os.Getenv("PATH")))

	if err := runSysctlApply(map[string]any{"file": "/etc/sysctl.d/99-kubernetes-cri.conf"}); err != nil {
		t.Fatalf("runSysctlApply failed: %v", err)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if strings.TrimSpace(string(raw)) != "-p /etc/sysctl.d/99-kubernetes-cri.conf" {
		t.Fatalf("unexpected sysctl args: %q", string(raw))
	}
}

func listEditFileBackups(path string) ([]string, error) {
	dir := filepath.Dir(path)
	prefix := filepath.Base(path) + ".bak-"
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	backups := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), prefix) {
			backups = append(backups, entry.Name())
		}
	}
	return backups, nil
}

func writeManifestForTest(bundleRoot, relPath string, content []byte) error {
	sum := sha256.Sum256(content)
	entry := map[string]any{
		"path":   relPath,
		"sha256": hex.EncodeToString(sum[:]),
		"size":   len(content),
	}
	manifest := map[string]any{"entries": []any{entry}}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	manifestPath := filepath.Join(bundleRoot, ".deck", "manifest.json")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(manifestPath, raw, 0o644)
}
