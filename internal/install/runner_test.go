package install

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

func useStubInitJoinKubeadm(t *testing.T) {
	t.Helper()
	origInit := kubeadmInitExecutor
	origJoin := kubeadmJoinExecutor
	t.Cleanup(func() {
		kubeadmInitExecutor = origInit
		kubeadmJoinExecutor = origJoin
	})
	kubeadmInitExecutor = func(_ context.Context, spec kubeadmInitSpec) error {
		return runInitKubeadmStub(spec)
	}
	kubeadmJoinExecutor = func(_ context.Context, spec kubeadmJoinSpec) error {
		return runJoinKubeadmStub(spec)
	}
}

func useStubResetKubeadm(t *testing.T) {
	t.Helper()
	origReset := kubeadmResetExecutor
	t.Cleanup(func() {
		kubeadmResetExecutor = origReset
	})
	kubeadmResetExecutor = func(_ context.Context, spec kubeadmResetSpec) error {
		return runResetKubeadmStub(spec)
	}
}

func nilContextForInstallTest() context.Context { return nil }

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
	useStubInitJoinKubeadm(t)

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "install",
			Steps: []config.Step{
				{ID: "install-packages", Kind: "InstallPackage", Spec: map[string]any{"packages": []any{"containerd"}}},
				{ID: "write-file", Kind: "WriteFile", Spec: map[string]any{"path": fileA, "content": "hello world"}},
				{ID: "edit-file", Kind: "EditFile", Spec: map[string]any{"path": fileA, "edits": []any{map[string]any{"op": "replace", "match": "world", "replaceWith": "deck"}}}},
				{ID: "copy-file", Kind: "CopyFile", Spec: map[string]any{"source": map[string]any{"path": fileA}, "path": fileB}},
				{ID: "Sysctl", Kind: "Sysctl", Spec: map[string]any{"writeFile": sysctlPath, "values": map[string]any{"net.ipv4.ip_forward": "1"}}},
				{ID: "modprobe", Kind: "KernelModule", Spec: map[string]any{"name": "overlay", "persistFile": modprobePath}},
				{ID: "run-cmd", Kind: "Command", Spec: map[string]any{"command": []any{"true"}}},
				{ID: "kubeadm-init", Kind: "InitKubeadm", Spec: map[string]any{"outputJoinFile": joinPath}},
				{ID: "kubeadm-join", Kind: "JoinKubeadm", Spec: map[string]any{"joinFile": joinPath}},
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
	if string(contentA) != "hello deck\n" {
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
	if st.Phase != "completed" {
		t.Fatalf("expected final phase state completed, got %q", st.Phase)
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
			Steps: []config.Step{{ID: "s1", Kind: "Command", Spec: map[string]any{"command": []any{"true"}}}},
		}},
	}

	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	statePath := filepath.Join(home, ".local", "state", "deck", "state", wf.StateKey+".json")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("state file missing at expected home path: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bundle, ".deck", "state.json")); !os.IsNotExist(err) {
		t.Fatalf("unexpected bundle state file, err=%v", err)
	}
}

func TestResolveStateReadPathUsesLegacyHomeStateFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))

	wf := &config.Workflow{StateKey: "legacy-state-key"}
	preferred, err := DefaultStatePath(wf)
	if err != nil {
		t.Fatalf("DefaultStatePath failed: %v", err)
	}
	legacyPath, err := LegacyStatePath(wf)
	if err != nil {
		t.Fatalf("LegacyStatePath failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatalf("mkdir legacy state dir: %v", err)
	}
	if err := os.WriteFile(legacyPath, []byte("{\n  \"completedSteps\": [\"s1\"]\n}\n"), 0o600); err != nil {
		t.Fatalf("write legacy state: %v", err)
	}

	resolved, err := ResolveStateReadPathForWorkflow(wf, preferred)
	if err != nil {
		t.Fatalf("ResolveStateReadPathForWorkflow failed: %v", err)
	}
	if resolved != legacyPath {
		t.Fatalf("expected legacy state path, got %q want %q", resolved, legacyPath)
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
			Steps: []config.Step{{ID: "s1", Kind: "Command", Spec: map[string]any{"command": []any{"true"}}}},
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
			Steps: []config.Step{{ID: "s1", Kind: "Command", Spec: map[string]any{"command": []any{"true"}}}},
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

func TestRun_NoPhasesFails(t *testing.T) {
	err := Run(context.Background(), &config.Workflow{Version: "v1"}, RunOptions{})
	if err == nil {
		t.Fatalf("expected no phases error")
	}
	if !strings.Contains(err.Error(), "no phases found") {
		t.Fatalf("unexpected error: %v", err)
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
				{ID: "s2", Kind: "Command", Spec: map[string]any{"command": []any{"false"}}},
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

func TestRun_CommandErrorCodes(t *testing.T) {
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
				Steps: []config.Step{{ID: "cmd", Kind: "Command", Spec: map[string]any{"command": []any{"false"}}}},
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
					Kind: "Command",
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
					Kind: "VerifyImage",
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

func TestRun_Wait(t *testing.T) {
	t.Run("waits for file to appear", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state", "state.json")
		target := filepath.Join(dir, "appears.txt")

		wf := &config.Workflow{
			Version: "v1",
			Phases: []config.Phase{{
				Name: "install",
				Steps: []config.Step{{
					ID:   "wait-file",
					Kind: "WaitForFile",
					Spec: map[string]any{"path": target, "type": "file", "pollInterval": "10ms", "timeout": "1s"},
				}},
			}},
		}

		go func() {
			time.Sleep(40 * time.Millisecond)
			_ = os.WriteFile(target, []byte("ok"), 0o644)
		}()

		if err := Run(context.Background(), wf, RunOptions{StatePath: statePath}); err != nil {
			t.Fatalf("expected Wait success, got %v", err)
		}
	})

	t.Run("waits for path to disappear", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state", "state.json")
		target := filepath.Join(dir, "gone.txt")
		if err := os.WriteFile(target, []byte("still here"), 0o644); err != nil {
			t.Fatalf("write initial target: %v", err)
		}

		wf := &config.Workflow{
			Version: "v1",
			Phases: []config.Phase{{
				Name: "install",
				Steps: []config.Step{{
					ID:   "wait-absent",
					Kind: "WaitForMissingFile",
					Spec: map[string]any{"path": target, "pollInterval": "10ms", "timeout": "1s"},
				}},
			}},
		}

		go func() {
			time.Sleep(40 * time.Millisecond)
			_ = os.Remove(target)
		}()

		if err := Run(context.Background(), wf, RunOptions{StatePath: statePath}); err != nil {
			t.Fatalf("expected Wait absent success, got %v", err)
		}
	})

	t.Run("waits for non-empty file", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state", "state.json")
		target := filepath.Join(dir, "non-empty.txt")
		if err := os.WriteFile(target, []byte{}, 0o644); err != nil {
			t.Fatalf("write empty file: %v", err)
		}

		wf := &config.Workflow{
			Version: "v1",
			Phases: []config.Phase{{
				Name: "install",
				Steps: []config.Step{{
					ID:   "wait-non-empty",
					Kind: "WaitForFile",
					Spec: map[string]any{"path": target, "type": "file", "nonEmpty": true, "pollInterval": "10ms", "timeout": "1s"},
				}},
			}},
		}

		go func() {
			time.Sleep(40 * time.Millisecond)
			_ = os.WriteFile(target, []byte("ready"), 0o644)
		}()

		if err := Run(context.Background(), wf, RunOptions{StatePath: statePath}); err != nil {
			t.Fatalf("expected Wait non-empty success, got %v", err)
		}
	})

	t.Run("type mismatch times out with clear error", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state", "state.json")
		target := filepath.Join(dir, "typed")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatalf("mkdir target: %v", err)
		}

		wf := &config.Workflow{
			Version: "v1",
			Phases: []config.Phase{{
				Name: "install",
				Steps: []config.Step{{
					ID:      "wait-type",
					Kind:    "WaitForFile",
					Timeout: "80ms",
					Spec:    map[string]any{"path": target, "type": "file", "pollInterval": "10ms"},
				}},
			}},
		}

		err := Run(context.Background(), wf, RunOptions{StatePath: statePath})
		if err == nil {
			t.Fatalf("expected Wait timeout error")
		}
		if !strings.Contains(err.Error(), errCodeInstallWaitTimeout) {
			t.Fatalf("expected %s, got %v", errCodeInstallWaitTimeout, err)
		}
		if !strings.Contains(err.Error(), target) {
			t.Fatalf("expected timeout error to include path, got %v", err)
		}
		if !strings.Contains(err.Error(), "exist as a file") {
			t.Fatalf("expected timeout error to include expected condition, got %v", err)
		}
	})
}

func TestRun_CreateSymlink(t *testing.T) {
	t.Run("creates a new symlink", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state", "state.json")
		target := filepath.Join(dir, "target.txt")
		linkPath := filepath.Join(dir, "link.txt")
		if err := os.WriteFile(target, []byte("ok"), 0o644); err != nil {
			t.Fatalf("write target: %v", err)
		}

		wf := &config.Workflow{
			Version: "v1",
			Phases: []config.Phase{{
				Name: "install",
				Steps: []config.Step{{
					ID:   "CreateSymlink",
					Kind: "CreateSymlink",
					Spec: map[string]any{"path": linkPath, "target": target},
				}},
			}},
		}

		if err := Run(context.Background(), wf, RunOptions{StatePath: statePath}); err != nil {
			t.Fatalf("expected symlink success, got %v", err)
		}

		info, err := os.Lstat(linkPath)
		if err != nil {
			t.Fatalf("lstat symlink path: %v", err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("expected symlink mode, got %v", info.Mode())
		}
		actualTarget, err := os.Readlink(linkPath)
		if err != nil {
			t.Fatalf("readlink symlink path: %v", err)
		}
		if actualTarget != target {
			t.Fatalf("expected symlink target %q, got %q", target, actualTarget)
		}
	})

	t.Run("createParent creates destination parent", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state", "state.json")
		target := filepath.Join(dir, "target.txt")
		linkPath := filepath.Join(dir, "nested", "path", "link.txt")
		if err := os.WriteFile(target, []byte("ok"), 0o644); err != nil {
			t.Fatalf("write target: %v", err)
		}

		wf := &config.Workflow{
			Version: "v1",
			Phases: []config.Phase{{
				Name: "install",
				Steps: []config.Step{{
					ID:   "CreateSymlink",
					Kind: "CreateSymlink",
					Spec: map[string]any{"path": linkPath, "target": target, "createParent": true},
				}},
			}},
		}

		if err := Run(context.Background(), wf, RunOptions{StatePath: statePath}); err != nil {
			t.Fatalf("expected symlink success, got %v", err)
		}
		if _, err := os.Stat(filepath.Dir(linkPath)); err != nil {
			t.Fatalf("expected created parent dir, got %v", err)
		}
	})

	t.Run("requireTarget rejects missing target", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state", "state.json")
		missingTarget := filepath.Join(dir, "missing.txt")
		linkPath := filepath.Join(dir, "link.txt")

		wf := &config.Workflow{
			Version: "v1",
			Phases: []config.Phase{{
				Name: "install",
				Steps: []config.Step{{
					ID:   "CreateSymlink",
					Kind: "CreateSymlink",
					Spec: map[string]any{"path": linkPath, "target": missingTarget, "requireTarget": true},
				}},
			}},
		}

		err := Run(context.Background(), wf, RunOptions{StatePath: statePath})
		if err == nil {
			t.Fatalf("expected requireTarget failure")
		}
		if !strings.Contains(err.Error(), "symlink target does not exist") {
			t.Fatalf("expected missing target error, got %v", err)
		}
	})

	t.Run("force replaces existing destination path", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state", "state.json")
		target := filepath.Join(dir, "target.txt")
		linkPath := filepath.Join(dir, "link.txt")
		if err := os.WriteFile(target, []byte("ok"), 0o644); err != nil {
			t.Fatalf("write target: %v", err)
		}
		if err := os.WriteFile(linkPath, []byte("existing"), 0o644); err != nil {
			t.Fatalf("write existing path: %v", err)
		}

		wf := &config.Workflow{
			Version: "v1",
			Phases: []config.Phase{{
				Name: "install",
				Steps: []config.Step{{
					ID:   "CreateSymlink",
					Kind: "CreateSymlink",
					Spec: map[string]any{"path": linkPath, "target": target, "force": true},
				}},
			}},
		}

		if err := Run(context.Background(), wf, RunOptions{StatePath: statePath}); err != nil {
			t.Fatalf("expected symlink success, got %v", err)
		}
		actualTarget, err := os.Readlink(linkPath)
		if err != nil {
			t.Fatalf("expected destination replaced with symlink, got %v", err)
		}
		if actualTarget != target {
			t.Fatalf("expected symlink target %q, got %q", target, actualTarget)
		}
	})

	t.Run("force does not replace existing directory", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state", "state.json")
		target := filepath.Join(dir, "target.txt")
		linkPath := filepath.Join(dir, "existing-dir")
		nested := filepath.Join(linkPath, "keep.txt")
		if err := os.WriteFile(target, []byte("ok"), 0o644); err != nil {
			t.Fatalf("write target: %v", err)
		}
		if err := os.MkdirAll(linkPath, 0o755); err != nil {
			t.Fatalf("mkdir existing directory: %v", err)
		}
		if err := os.WriteFile(nested, []byte("keep"), 0o644); err != nil {
			t.Fatalf("write nested file: %v", err)
		}

		wf := &config.Workflow{
			Version: "v1",
			Phases: []config.Phase{{
				Name: "install",
				Steps: []config.Step{{
					ID:   "CreateSymlink",
					Kind: "CreateSymlink",
					Spec: map[string]any{"path": linkPath, "target": target, "force": true},
				}},
			}},
		}

		err := Run(context.Background(), wf, RunOptions{StatePath: statePath})
		if err == nil {
			t.Fatalf("expected failure when destination is directory")
		}
		if !strings.Contains(err.Error(), "destination is a directory and cannot be replaced") {
			t.Fatalf("expected safe directory replacement error, got %v", err)
		}
		if _, statErr := os.Stat(nested); statErr != nil {
			t.Fatalf("expected directory contents preserved, got %v", statErr)
		}
	})

	t.Run("existing correct symlink is idempotent", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state", "state.json")
		target := filepath.Join(dir, "target.txt")
		linkPath := filepath.Join(dir, "link.txt")
		if err := os.WriteFile(target, []byte("ok"), 0o644); err != nil {
			t.Fatalf("write target: %v", err)
		}
		if err := os.Symlink(target, linkPath); err != nil {
			t.Fatalf("create initial symlink: %v", err)
		}

		before, err := os.Lstat(linkPath)
		if err != nil {
			t.Fatalf("lstat initial symlink: %v", err)
		}

		wf := &config.Workflow{
			Version: "v1",
			Phases: []config.Phase{{
				Name: "install",
				Steps: []config.Step{{
					ID:   "CreateSymlink",
					Kind: "CreateSymlink",
					Spec: map[string]any{"path": linkPath, "target": target},
				}},
			}},
		}

		if err := Run(context.Background(), wf, RunOptions{StatePath: statePath}); err != nil {
			t.Fatalf("expected idempotent symlink success, got %v", err)
		}

		after, err := os.Lstat(linkPath)
		if err != nil {
			t.Fatalf("lstat symlink after run: %v", err)
		}
		if !before.ModTime().Equal(after.ModTime()) {
			t.Fatalf("expected symlink to be unchanged")
		}
		actualTarget, err := os.Readlink(linkPath)
		if err != nil {
			t.Fatalf("readlink symlink after run: %v", err)
		}
		if actualTarget != target {
			t.Fatalf("expected symlink target %q, got %q", target, actualTarget)
		}
	})
}

func TestRun_KubeadmMissingFileErrorCode(t *testing.T) {
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
				Kind: "JoinKubeadm",
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

func TestRun_JoinKubeadmRequiresExistingJoinFile(t *testing.T) {
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
				Kind: "JoinKubeadm",
				Spec: map[string]any{"joinFile": filepath.Join(dir, "join.txt")},
			}},
		}},
	}

	err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath})
	if err == nil {
		t.Fatalf("expected join file error")
	}
	if !strings.Contains(err.Error(), errCodeInstallJoinFileMissing) {
		t.Fatalf("expected kubeadm join file missing error, got %v", err)
	}
}

func TestRun_Image(t *testing.T) {
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
					Kind: "VerifyImage",
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
					Kind: "VerifyImage",
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
				{ID: "kubeadm-init", Kind: "InitKubeadm", Spec: map[string]any{"outputJoinFile": joinPath}},
				{ID: "kubeadm-join", Kind: "JoinKubeadm", Spec: map[string]any{"joinFile": joinPath}},
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

func TestRun_KubeadmRealModeRejectsInvalidCommand(t *testing.T) {
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
				Kind: "JoinKubeadm",
				Spec: map[string]any{"joinFile": joinPath},
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

func TestRun_KubeadmRealModeSupportsJoinConfigFile(t *testing.T) {
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
	kubeadmLog := filepath.Join(dir, "kubeadm.log")
	fakeKubeadmScript := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n' \"$*\" >> \"" + kubeadmLog + "\"\nif [[ \"${1:-}\" == \"join\" ]]; then\n  exit 0\nfi\necho \"unsupported kubeadm invocation\" >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "kubeadm"), []byte(fakeKubeadmScript), 0o755); err != nil {
		t.Fatalf("write fake kubeadm: %v", err)
	}
	configPath := filepath.Join(dir, "kubeadm-join.yaml")
	if err := os.WriteFile(configPath, []byte("apiVersion: kubeadm.k8s.io/v1beta3\nkind: JoinConfiguration\n"), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	t.Setenv("PATH", fmt.Sprintf("%s:%s", binDir, os.Getenv("PATH")))

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "install",
			Steps: []config.Step{{
				ID:   "kubeadm-join",
				Kind: "JoinKubeadm",
				Spec: map[string]any{
					"mode":           "real",
					"configFile":     configPath,
					"asControlPlane": true,
					"extraArgs":      []any{"--skip-phases=preflight"},
				},
			}},
		}},
	}

	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath}); err != nil {
		t.Fatalf("real kubeadm join config run failed: %v", err)
	}

	logRaw, err := os.ReadFile(kubeadmLog)
	if err != nil {
		t.Fatalf("read kubeadm log: %v", err)
	}
	if got := strings.TrimSpace(string(logRaw)); got != "join --config "+configPath+" --control-plane --skip-phases=preflight" {
		t.Fatalf("unexpected kubeadm join args: %q", got)
	}
}

func TestRun_KubeadmRealModeSupportsAsControlPlaneJoinFile(t *testing.T) {
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
	kubeadmLog := filepath.Join(dir, "kubeadm.log")
	fakeKubeadmScript := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n' \"$*\" >> \"" + kubeadmLog + "\"\nif [[ \"${1:-}\" == \"join\" ]]; then\n  exit 0\nfi\necho \"unsupported kubeadm invocation\" >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "kubeadm"), []byte(fakeKubeadmScript), 0o755); err != nil {
		t.Fatalf("write fake kubeadm: %v", err)
	}
	t.Setenv("PATH", fmt.Sprintf("%s:%s", binDir, os.Getenv("PATH")))

	joinPath := filepath.Join(dir, "join.txt")
	if err := os.WriteFile(joinPath, []byte("kubeadm join 10.1.0.10:6443 --token fake.token --discovery-token-ca-cert-hash sha256:fake\n"), 0o644); err != nil {
		t.Fatalf("write join file: %v", err)
	}

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "install",
			Steps: []config.Step{{
				ID:   "kubeadm-join",
				Kind: "JoinKubeadm",
				Spec: map[string]any{
					"mode":           "real",
					"joinFile":       joinPath,
					"asControlPlane": true,
				},
			}},
		}},
	}

	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath}); err != nil {
		t.Fatalf("real kubeadm join control-plane run failed: %v", err)
	}

	logRaw, err := os.ReadFile(kubeadmLog)
	if err != nil {
		t.Fatalf("read kubeadm log: %v", err)
	}
	if got := strings.TrimSpace(string(logRaw)); got != "join 10.1.0.10:6443 --token fake.token --discovery-token-ca-cert-hash sha256:fake --control-plane" {
		t.Fatalf("unexpected kubeadm join args: %q", got)
	}
}

func TestRun_JoinKubeadmRejectsConflictingInputs(t *testing.T) {
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
	if err := os.WriteFile(joinPath, []byte("kubeadm join 10.1.0.10:6443 --token fake.token --discovery-token-ca-cert-hash sha256:fake\n"), 0o644); err != nil {
		t.Fatalf("write join file: %v", err)
	}
	configPath := filepath.Join(dir, "kubeadm-join.yaml")
	if err := os.WriteFile(configPath, []byte("apiVersion: kubeadm.k8s.io/v1beta3\nkind: JoinConfiguration\n"), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "install",
			Steps: []config.Step{{
				ID:   "kubeadm-join",
				Kind: "JoinKubeadm",
				Spec: map[string]any{
					"mode":       "real",
					"joinFile":   joinPath,
					"configFile": configPath,
				},
			}},
		}},
	}

	err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath})
	if err == nil {
		t.Fatalf("expected conflicting join inputs error")
	}
	if !strings.Contains(err.Error(), "E_INSTALL_KUBEADM_JOIN_INPUT_CONFLICT") {
		t.Fatalf("expected E_INSTALL_KUBEADM_JOIN_INPUT_CONFLICT, got %v", err)
	}
}

func TestRun_KubeadmRealModeSupportsImagePullAndConfigWrite(t *testing.T) {
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
	kubeadmLog := filepath.Join(dir, "kubeadm.log")
	fakeKubeadmPath := filepath.Join(binDir, "kubeadm")
	fakeKubeadmScript := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n' \"$*\" >> \"" + kubeadmLog + "\"\nif [[ \"${1:-}\" == \"config\" && \"${2:-}\" == \"images\" && \"${3:-}\" == \"pull\" ]]; then\n  exit 0\nfi\nif [[ \"${1:-}\" == \"init\" ]]; then\n  exit 0\nfi\nif [[ \"${1:-}\" == \"token\" && \"${2:-}\" == \"create\" ]]; then\n  echo \"kubeadm join 10.1.0.10:6443 --token fake.token --discovery-token-ca-cert-hash sha256:fake\"\n  exit 0\nfi\necho \"unsupported kubeadm invocation\" >&2\nexit 1\n"
	if err := os.WriteFile(fakeKubeadmPath, []byte(fakeKubeadmScript), 0o755); err != nil {
		t.Fatalf("write fake kubeadm: %v", err)
	}
	fakeIPPath := filepath.Join(binDir, "ip")
	fakeIPScript := "#!/usr/bin/env bash\nset -euo pipefail\nif [[ \"$*\" == \"-4 route get 1.1.1.1\" ]]; then\n  echo \"1.1.1.1 via 10.0.2.2 dev eth0 src 10.20.30.40 uid 0\"\n  exit 0\nfi\nif [[ \"$*\" == \"-4 -o addr show scope global\" ]]; then\n  echo \"2: eth0    inet 10.20.30.40/24 brd 10.20.30.255 scope global dynamic eth0\"\n  exit 0\nfi\nexit 1\n"
	if err := os.WriteFile(fakeIPPath, []byte(fakeIPScript), 0o755); err != nil {
		t.Fatalf("write fake ip: %v", err)
	}

	t.Setenv("PATH", fmt.Sprintf("%s:%s", binDir, os.Getenv("PATH")))

	joinPath := filepath.Join(dir, "join.txt")
	configPath := filepath.Join(dir, "kubeadm-init.yaml")
	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "install",
			Steps: []config.Step{{
				ID:   "kubeadm-init",
				Kind: "InitKubeadm",
				Spec: map[string]any{
					"mode":                  "real",
					"outputJoinFile":        joinPath,
					"configFile":            configPath,
					"configTemplate":        "default",
					"pullImages":            true,
					"kubernetesVersion":     "v1.30.14",
					"podNetworkCIDR":        "10.244.0.0/16",
					"criSocket":             "unix:///run/containerd/containerd.sock",
					"ignorePreflightErrors": []any{"swap"},
					"extraArgs":             []any{"--skip-phases=addon/kube-proxy"},
				},
			}},
		}},
	}

	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath}); err != nil {
		t.Fatalf("real kubeadm mode run failed: %v", err)
	}

	configRaw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	configText := string(configRaw)
	if !strings.Contains(configText, "advertiseAddress: 10.20.30.40") {
		t.Fatalf("expected detected advertise address in config, got %q", configText)
	}
	if !strings.Contains(configText, "kubernetesVersion: v1.30.14") {
		t.Fatalf("expected kubernetes version in config, got %q", configText)
	}
	if !strings.Contains(configText, "podSubnet: 10.244.0.0/16") {
		t.Fatalf("expected pod subnet in config, got %q", configText)
	}
	if !strings.Contains(configText, "criSocket: unix:///run/containerd/containerd.sock") {
		t.Fatalf("expected cri socket in config, got %q", configText)
	}

	logRaw, err := os.ReadFile(kubeadmLog)
	if err != nil {
		t.Fatalf("read kubeadm log: %v", err)
	}
	logText := string(logRaw)
	if !strings.Contains(logText, "config images pull --kubernetes-version v1.30.14 --cri-socket unix:///run/containerd/containerd.sock") {
		t.Fatalf("expected kubeadm image pull invocation, got %q", logText)
	}
	if !strings.Contains(logText, "init --config "+configPath+" --ignore-preflight-errors swap --skip-phases=addon/kube-proxy") {
		t.Fatalf("expected kubeadm init args with config file only, got %q", logText)
	}
}

func TestRun_InitKubeadmSkipsWhenAdminConfExists(t *testing.T) {
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
	kubeadmLog := filepath.Join(dir, "kubeadm.log")
	fakeKubeadmScript := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n' \"$*\" >> \"" + kubeadmLog + "\"\nexit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "kubeadm"), []byte(fakeKubeadmScript), 0o755); err != nil {
		t.Fatalf("write fake kubeadm: %v", err)
	}
	t.Setenv("PATH", fmt.Sprintf("%s:%s", binDir, os.Getenv("PATH")))

	adminConfDir := filepath.Join(dir, "etc", "kubernetes")
	if err := os.MkdirAll(adminConfDir, 0o755); err != nil {
		t.Fatalf("mkdir admin conf dir: %v", err)
	}
	adminConfPath := filepath.Join(adminConfDir, "admin.conf")
	if err := os.WriteFile(adminConfPath, []byte("apiVersion: v1\n"), 0o644); err != nil {
		t.Fatalf("write admin conf: %v", err)
	}
	prevAdminConfPath := kubeadmAdminConfPath
	kubeadmAdminConfPath = adminConfPath
	t.Cleanup(func() { kubeadmAdminConfPath = prevAdminConfPath })

	joinPath := filepath.Join(dir, "join.txt")
	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "install",
			Steps: []config.Step{{
				ID:   "kubeadm-init",
				Kind: "InitKubeadm",
				Spec: map[string]any{
					"mode":           "real",
					"outputJoinFile": joinPath,
				},
			}},
		}},
	}

	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath}); err != nil {
		t.Fatalf("expected init skip success, got %v", err)
	}
	if _, err := os.Stat(joinPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no join file to be written on skip, got err=%v", err)
	}
	if _, err := os.Stat(kubeadmLog); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected kubeadm not to run on skip, got err=%v", err)
	}
}

func TestRun_InitKubeadmRunsWhenSkipDisabledAndAdminConfExists(t *testing.T) {
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
	kubeadmLog := filepath.Join(dir, "kubeadm.log")
	fakeKubeadmScript := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n' \"$*\" >> \"" + kubeadmLog + "\"\nif [[ \"${1:-}\" == \"init\" ]]; then\n  exit 0\nfi\nif [[ \"${1:-}\" == \"token\" && \"${2:-}\" == \"create\" ]]; then\n  echo \"kubeadm join 10.1.0.10:6443 --token fake.token --discovery-token-ca-cert-hash sha256:fake\"\n  exit 0\nfi\necho \"unsupported kubeadm invocation\" >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "kubeadm"), []byte(fakeKubeadmScript), 0o755); err != nil {
		t.Fatalf("write fake kubeadm: %v", err)
	}
	t.Setenv("PATH", fmt.Sprintf("%s:%s", binDir, os.Getenv("PATH")))

	adminConfDir := filepath.Join(dir, "etc", "kubernetes")
	if err := os.MkdirAll(adminConfDir, 0o755); err != nil {
		t.Fatalf("mkdir admin conf dir: %v", err)
	}
	adminConfPath := filepath.Join(adminConfDir, "admin.conf")
	if err := os.WriteFile(adminConfPath, []byte("apiVersion: v1\n"), 0o644); err != nil {
		t.Fatalf("write admin conf: %v", err)
	}
	prevAdminConfPath := kubeadmAdminConfPath
	kubeadmAdminConfPath = adminConfPath
	t.Cleanup(func() { kubeadmAdminConfPath = prevAdminConfPath })

	joinPath := filepath.Join(dir, "join.txt")
	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "install",
			Steps: []config.Step{{
				ID:   "kubeadm-init",
				Kind: "InitKubeadm",
				Spec: map[string]any{
					"mode":                  "real",
					"outputJoinFile":        joinPath,
					"skipIfAdminConfExists": false,
				},
			}},
		}},
	}

	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath}); err != nil {
		t.Fatalf("expected init to run when skip disabled, got %v", err)
	}
	joinRaw, err := os.ReadFile(joinPath)
	if err != nil {
		t.Fatalf("read join file: %v", err)
	}
	if !strings.Contains(string(joinRaw), "kubeadm join") {
		t.Fatalf("expected join file output, got %q", string(joinRaw))
	}
	logRaw, err := os.ReadFile(kubeadmLog)
	if err != nil {
		t.Fatalf("read kubeadm log: %v", err)
	}
	if !strings.Contains(string(logRaw), "init") {
		t.Fatalf("expected kubeadm init to run, got %q", string(logRaw))
	}
}

func TestRun_KubeadmAdvertiseAddressDetectionFallback(t *testing.T) {
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
	fakeKubeadmScript := "#!/usr/bin/env bash\nset -euo pipefail\nif [[ \"${1:-}\" == \"init\" ]]; then\n  exit 0\nfi\nif [[ \"${1:-}\" == \"token\" && \"${2:-}\" == \"create\" ]]; then\n  echo \"kubeadm join 10.1.0.10:6443 --token fake.token --discovery-token-ca-cert-hash sha256:fake\"\n  exit 0\nfi\nexit 1\n"
	if err := os.WriteFile(fakeKubeadmPath, []byte(fakeKubeadmScript), 0o755); err != nil {
		t.Fatalf("write fake kubeadm: %v", err)
	}
	fakeIPPath := filepath.Join(binDir, "ip")
	fakeIPScript := "#!/usr/bin/env bash\nset -euo pipefail\nif [[ \"$*\" == \"-4 route get 1.1.1.1\" ]]; then\n  exit 1\nfi\nif [[ \"$*\" == \"-4 -o addr show scope global\" ]]; then\n  echo \"2: eth0    inet 172.16.0.25/24 brd 172.16.0.255 scope global dynamic eth0\"\n  exit 0\nfi\nexit 1\n"
	if err := os.WriteFile(fakeIPPath, []byte(fakeIPScript), 0o755); err != nil {
		t.Fatalf("write fake ip: %v", err)
	}

	t.Setenv("PATH", fmt.Sprintf("%s:%s", binDir, os.Getenv("PATH")))

	joinPath := filepath.Join(dir, "join.txt")
	configPath := filepath.Join(dir, "kubeadm-init.yaml")
	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "install",
			Steps: []config.Step{{
				ID:   "kubeadm-init",
				Kind: "InitKubeadm",
				Spec: map[string]any{
					"mode":              "real",
					"outputJoinFile":    joinPath,
					"configFile":        configPath,
					"configTemplate":    "default",
					"kubernetesVersion": "v1.30.14",
				},
			}},
		}},
	}

	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath}); err != nil {
		t.Fatalf("real kubeadm mode run failed: %v", err)
	}

	configRaw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	if !strings.Contains(string(configRaw), "advertiseAddress: 172.16.0.25") {
		t.Fatalf("expected fallback global IPv4 advertise address, got %q", string(configRaw))
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
				{ID: "init", Kind: "InitKubeadm", Spec: map[string]any{"outputJoinFile": joinPath}, Register: map[string]string{"workerJoinFile": "joinFile"}},
				{ID: "use-register", Kind: "WriteFile", When: "vars.role == \"control-plane\"", Spec: map[string]any{"path": registeredOutputPath, "content": "{{ .runtime.workerJoinFile }}"}},
				{ID: "skip-worker", Kind: "WriteFile", When: "vars.role == \"worker\"", Spec: map[string]any{"path": skippedOutputPath, "content": "worker"}},
			},
		}},
	}
	useStubInitJoinKubeadm(t)

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

func TestRun_InitKubeadmSkipDoesNotRegisterMissingJoinFile(t *testing.T) {
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

	adminConfDir := filepath.Join(dir, "etc", "kubernetes")
	if err := os.MkdirAll(adminConfDir, 0o755); err != nil {
		t.Fatalf("mkdir admin conf dir: %v", err)
	}
	adminConfPath := filepath.Join(adminConfDir, "admin.conf")
	if err := os.WriteFile(adminConfPath, []byte("apiVersion: v1\n"), 0o644); err != nil {
		t.Fatalf("write admin conf: %v", err)
	}
	prevAdminConfPath := kubeadmAdminConfPath
	kubeadmAdminConfPath = adminConfPath
	t.Cleanup(func() { kubeadmAdminConfPath = prevAdminConfPath })

	joinPath := filepath.Join(dir, "join.txt")
	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "install",
			Steps: []config.Step{{
				ID:       "init",
				Kind:     "InitKubeadm",
				Spec:     map[string]any{"outputJoinFile": joinPath},
				Register: map[string]string{"workerJoinFile": "joinFile"},
			}},
		}},
	}

	err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath})
	if err == nil {
		t.Fatalf("expected register failure for skipped init without join file")
	}
	if !strings.Contains(err.Error(), "E_REGISTER_OUTPUT_NOT_FOUND") {
		t.Fatalf("expected E_REGISTER_OUTPUT_NOT_FOUND, got %v", err)
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
				Steps: []config.Step{{ID: "retry-cmd", Kind: "Command", Retry: 1, Spec: map[string]any{"command": []any{scriptPath}}}},
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
				Steps: []config.Step{{ID: "retry-cmd", Kind: "Command", Retry: 1, Spec: map[string]any{"command": []any{scriptPath}}}},
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
	script := "#!/usr/bin/env bash\nset -euo pipefail\ncount=0\nif [[ -f \"" + counterPath + "\" ]]; then\n  count=$(cat \"" + counterPath + "\")\nfi\ncount=$((count+1))\necho \"${count}\" > \"" + counterPath + "\"\nsleep 30\nexit 1\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name:  "install",
			Steps: []config.Step{{ID: "retry-cmd", Kind: "Command", Retry: 4, Spec: map[string]any{"command": []any{scriptPath}, "timeout": "5s"}}},
		}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
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

func TestRun_CommandParentCancelNotRelabeledAsTimeout(t *testing.T) {
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
			Steps: []config.Step{{ID: "cmd", Kind: "Command", Spec: map[string]any{"command": []any{"true"}, "timeout": "3s"}}},
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

func TestRun_FileRespectsParentContext(t *testing.T) {
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
				Kind: "CopyFile",
				Spec: map[string]any{"source": map[string]any{"url": srv.URL + "/files/payload.txt"}, "path": filepath.Join(dir, "payload.txt")},
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

func TestRun_DownloadFileRegistersOutputs(t *testing.T) {
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
		_, _ = w.Write([]byte("payload"))
	}))
	defer srv.Close()

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "install",
			Steps: []config.Step{{
				ID:       "download",
				Kind:     "CopyFile",
				Spec:     map[string]any{"source": map[string]any{"url": srv.URL + "/files/payload.txt"}, "path": filepath.Join(dir, "payload.txt")},
				Register: map[string]string{"downloadPath": "path"},
			}},
		}},
	}

	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	stateRaw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var st State
	if err := json.Unmarshal(stateRaw, &st); err != nil {
		t.Fatalf("parse state: %v", err)
	}
	if st.RuntimeVars["downloadPath"] != filepath.Join(dir, "payload.txt") {
		t.Fatalf("expected registered path, got %#v", st.RuntimeVars["downloadPath"])
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

func TestCommandOutputWithContext_TimeoutReturnsSentinel(t *testing.T) {
	_, err := runCommandOutputWithContext(context.Background(), []string{"sleep", "1"}, 10*time.Millisecond)
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if !errors.Is(err, ErrStepCommandTimeout) {
		t.Fatalf("expected step timeout sentinel, got %v", err)
	}
}

func TestCommandOutputWithContext_RejectsNilContext(t *testing.T) {
	_, err := runCommandOutputWithContext(nilContextForInstallTest(), []string{"true"}, time.Second)
	if err == nil {
		t.Fatalf("expected nil context error")
	}
	if !strings.Contains(err.Error(), "context is nil") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteStep_CopyFileDecodeError(t *testing.T) {
	err := executeStep(context.Background(), "CopyFile", map[string]any{"source": 42, "path": "/tmp/out"}, ExecutionContext{})
	if err == nil {
		t.Fatalf("expected decode error")
	}
	if !strings.Contains(err.Error(), "decode CopyFile spec") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRun_WriteContainerdConfigDefaultGenerationRespectsParentContext(t *testing.T) {
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
	fakeWriteContainerdConfig := filepath.Join(binDir, "containerd")
	script := "#!/usr/bin/env bash\nset -euo pipefail\nif [[ \"${1:-}\" == \"config\" && \"${2:-}\" == \"default\" ]]; then\n  sleep 1\n  echo 'version = 2'\n  exit 0\nfi\nexit 1\n"
	if err := os.WriteFile(fakeWriteContainerdConfig, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake containerd: %v", err)
	}
	t.Setenv("PATH", fmt.Sprintf("%s:%s", binDir, os.Getenv("PATH")))

	target := filepath.Join(dir, "containerd", "config.toml")
	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name:  "install",
			Steps: []config.Step{{ID: "containerd-config", Kind: "WriteContainerdConfig", Spec: map[string]any{"path": target, "timeout": "5s"}}},
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

func TestRun_WriteContainerdConfigDefaultGenerationTimeoutUsesTimeoutClassification(t *testing.T) {
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
	fakeWriteContainerdConfig := filepath.Join(binDir, "containerd")
	script := "#!/usr/bin/env bash\nset -euo pipefail\nif [[ \"${1:-}\" == \"config\" && \"${2:-}\" == \"default\" ]]; then\n  sleep 1\n  echo 'version = 2'\n  exit 0\nfi\nexit 1\n"
	if err := os.WriteFile(fakeWriteContainerdConfig, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake containerd: %v", err)
	}
	t.Setenv("PATH", fmt.Sprintf("%s:%s", binDir, os.Getenv("PATH")))

	target := filepath.Join(dir, "containerd", "config.toml")
	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name:  "install",
			Steps: []config.Step{{ID: "containerd-config", Kind: "WriteContainerdConfig", Spec: map[string]any{"path": target, "timeout": "20ms"}}},
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
				Kind: "Command",
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

func TestRun_PackagesExecutesPackageManager(t *testing.T) {
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
				Kind: "InstallPackage",
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

func TestRun_PackagesSourcePathValidation(t *testing.T) {
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
				Kind: "InstallPackage",
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

func TestRun_InstallPackagesFromLocalRepo(t *testing.T) {
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
				Kind: "InstallPackage",
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

func TestRun_PackagesTimeoutUsesTimeoutClassification(t *testing.T) {
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
				Kind:    "InstallPackage",
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

func TestEditFileSupportsAppendOperation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target.conf")
	if err := os.WriteFile(path, []byte("alpha\nalpha\n"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	err := runEditFile(map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{"match": "alpha", "replaceWith": "-beta", "op": "append"},
		},
	})
	if err != nil {
		t.Fatalf("runEditFile failed: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(raw) != "alpha-beta\nalpha-beta\n" {
		t.Fatalf("unexpected edited content: %q", string(raw))
	}
}

func TestFileModeAppliesToCopyAndEdit(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dest := filepath.Join(dir, "dest.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if err := runCopyFile(context.Background(), "", map[string]any{"source": map[string]any{"path": src}, "path": dest, "mode": "0600"}); err != nil {
		t.Fatalf("runCopyFile failed: %v", err)
	}
	if info, err := os.Stat(dest); err != nil {
		t.Fatalf("stat dest: %v", err)
	} else if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected dest mode 0600, got %o", info.Mode().Perm())
	}

	if err := runEditFile(map[string]any{
		"path":  dest,
		"mode":  "0640",
		"edits": []any{map[string]any{"match": "hello", "with": "deck", "op": "replace"}},
	}); err != nil {
		t.Fatalf("runEditFile failed: %v", err)
	}
	if info, err := os.Stat(dest); err != nil {
		t.Fatalf("stat edited dest: %v", err)
	} else if info.Mode().Perm() != 0o640 {
		t.Fatalf("expected edited dest mode 0640, got %o", info.Mode().Perm())
	}
}

func TestCopyFileReadsBundleSourceFromBundleRoot(t *testing.T) {
	dir := t.TempDir()
	bundleRoot := filepath.Join(dir, "bundle")
	dest := filepath.Join(dir, "dest.txt")
	sourcePath := filepath.Join(bundleRoot, "files", "bin", "tool")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatalf("mkdir bundle source: %v", err)
	}
	if err := os.WriteFile(sourcePath, []byte("bundle-copy"), 0o644); err != nil {
		t.Fatalf("write bundle source: %v", err)
	}

	if err := runCopyFile(context.Background(), bundleRoot, map[string]any{
		"source": map[string]any{"bundle": map[string]any{"root": "files", "path": "bin/tool"}},
		"path":   dest,
	}); err != nil {
		t.Fatalf("runCopyFile failed: %v", err)
	}

	raw, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(raw) != "bundle-copy" {
		t.Fatalf("unexpected copied content: %q", string(raw))
	}
}

func TestExtractArchiveReadsBundleSourceFromBundleRoot(t *testing.T) {
	dir := t.TempDir()
	bundleRoot := filepath.Join(dir, "bundle")
	destDir := filepath.Join(dir, "out")
	archivePath := filepath.Join(bundleRoot, "files", "archives", "tool.tar.gz")
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
		t.Fatalf("mkdir archive dir: %v", err)
	}
	if err := writeTestTarGz(archivePath, map[string]string{"bin/tool": "extracted"}); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	if err := runExtractArchive(context.Background(), bundleRoot, map[string]any{
		"source": map[string]any{"bundle": map[string]any{"root": "files", "path": "archives/tool.tar.gz"}},
		"path":   destDir,
	}); err != nil {
		t.Fatalf("runExtractArchive failed: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(destDir, "bin", "tool"))
	if err != nil {
		t.Fatalf("read extracted file: %v", err)
	}
	if string(raw) != "extracted" {
		t.Fatalf("unexpected extracted content: %q", string(raw))
	}
}

func TestLoadImageReadsArchivesFromBundleRoot(t *testing.T) {
	dir := t.TempDir()
	bundleRoot := filepath.Join(dir, "bundle")
	archivePath := filepath.Join(bundleRoot, "images", "control-plane", sanitizeImageArchiveName("registry.k8s.io/kube-apiserver:v1.30.1")+".tar")
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
		t.Fatalf("mkdir image dir: %v", err)
	}
	if err := os.WriteFile(archivePath, []byte("image"), 0o644); err != nil {
		t.Fatalf("write image archive: %v", err)
	}

	if err := runLoadImage(context.Background(), bundleRoot, map[string]any{
		"images":    []string{"registry.k8s.io/kube-apiserver:v1.30.1"},
		"sourceDir": "images/control-plane",
		"command":   []string{"/bin/sh", "-c", "test -f {archive}"},
	}); err != nil {
		t.Fatalf("runLoadImage failed: %v", err)
	}
}

func writeTestTarGz(path string, files map[string]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz := gzip.NewWriter(f)
	defer func() { _ = gz.Close() }()
	tw := tar.NewWriter(gz)
	defer func() { _ = tw.Close() }()
	for name, content := range files {
		raw := []byte(content)
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(raw))}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := io.Copy(tw, strings.NewReader(content)); err != nil {
			return err
		}
	}
	return nil
}

func TestManageServiceStep(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	logPath := filepath.Join(dir, "systemctl.log")
	scriptPath := filepath.Join(binDir, "systemctl")
	script := "#!/usr/bin/env bash\nset -euo pipefail\ncontains_unit() {\n  local list=\"${1:-}\"\n  local unit=\"${2:-}\"\n  [[ -n \"${unit}\" ]] || return 1\n  if [[ \",${list},\" == *\",${unit},\"* ]]; then\n    return 0\n  fi\n  if [[ \"${unit}\" == *.service ]]; then\n    local base=\"${unit%.service}\"\n    [[ \",${list},\" == *\",${base},\"* ]]\n    return\n  fi\n  [[ \",${list},\" == *\",${unit}.service,\"* ]]\n}\ncmd=\"${1:-}\"\ncase \"${cmd}\" in\n  is-enabled)\n    if contains_unit \"${SYSTEMCTL_ENABLED_UNITS:-}\" \"${2:-}\"; then\n      exit 0\n    fi\n    exit 1\n    ;;\n  is-active)\n    unit=\"${2:-}\"\n    if [[ \"${unit}\" == \"--quiet\" ]]; then\n      unit=\"${3:-}\"\n    fi\n    if contains_unit \"${SYSTEMCTL_ACTIVE_UNITS:-}\" \"${unit}\"; then\n      exit 0\n    fi\n    exit 1\n    ;;\n  list-unit-files)\n    if contains_unit \"${SYSTEMCTL_EXISTING_UNITS:-}\" \"${2:-}\"; then\n      printf '%s enabled\\n' \"${2:-}\"\n      exit 0\n    fi\n    exit 1\n    ;;\n  daemon-reload)\n    printf '%s\\n' \"$*\" >> \"" + logPath + "\"\n    exit 0\n    ;;\n  enable|disable|start|stop|restart|reload)\n    if contains_unit \"${SYSTEMCTL_MISSING_UNITS:-}\" \"${2:-}\"; then\n      printf 'Unit %s not found.\\n' \"${2:-}\" >&2\n      exit 1\n    fi\n    ;;\nesac\nprintf '%s\\n' \"$*\" >> \"" + logPath + "\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write systemctl script: %v", err)
	}
	t.Setenv("PATH", fmt.Sprintf("%s:%s", binDir, os.Getenv("PATH")))
	readLog := func() string {
		raw, err := os.ReadFile(logPath)
		if err != nil {
			if os.IsNotExist(err) {
				return ""
			}
			t.Fatalf("read log: %v", err)
		}
		return string(raw)
	}
	resetLog := func() {
		_ = os.Remove(logPath)
	}

	t.Run("single-name preserves enable and start behavior", func(t *testing.T) {
		resetLog()
		t.Setenv("SYSTEMCTL_ENABLED_UNITS", "")
		t.Setenv("SYSTEMCTL_ACTIVE_UNITS", "")
		t.Setenv("SYSTEMCTL_EXISTING_UNITS", "")
		t.Setenv("SYSTEMCTL_MISSING_UNITS", "")

		if err := runManageService(context.Background(), map[string]any{"name": "containerd", "enabled": true, "state": "started"}); err != nil {
			t.Fatalf("runManageService failed: %v", err)
		}

		got := readLog()
		if !strings.Contains(got, "enable containerd") || !strings.Contains(got, "start containerd") {
			t.Fatalf("expected enable/start invocations, got %q", got)
		}
	})

	t.Run("multi-name disable and stop applies per service", func(t *testing.T) {
		resetLog()
		t.Setenv("SYSTEMCTL_ENABLED_UNITS", "firewalld,ufw")
		t.Setenv("SYSTEMCTL_ACTIVE_UNITS", "firewalld,ufw")
		t.Setenv("SYSTEMCTL_EXISTING_UNITS", "")
		t.Setenv("SYSTEMCTL_MISSING_UNITS", "")

		if err := runManageService(context.Background(), map[string]any{"names": []any{"firewalld", "ufw"}, "enabled": false, "state": "stopped"}); err != nil {
			t.Fatalf("runManageService failed: %v", err)
		}

		got := readLog()
		if !strings.Contains(got, "disable firewalld") || !strings.Contains(got, "disable ufw") {
			t.Fatalf("expected disable for each service, got %q", got)
		}
		if !strings.Contains(got, "stop firewalld") || !strings.Contains(got, "stop ufw") {
			t.Fatalf("expected stop for each service, got %q", got)
		}
	})

	t.Run("daemon reload runs before service operation", func(t *testing.T) {
		resetLog()
		t.Setenv("SYSTEMCTL_ENABLED_UNITS", "")
		t.Setenv("SYSTEMCTL_ACTIVE_UNITS", "")
		t.Setenv("SYSTEMCTL_EXISTING_UNITS", "")
		t.Setenv("SYSTEMCTL_MISSING_UNITS", "")

		if err := runManageService(context.Background(), map[string]any{"name": "containerd", "daemonReload": true, "state": "restarted"}); err != nil {
			t.Fatalf("runManageService failed: %v", err)
		}

		lines := strings.Split(strings.TrimSpace(readLog()), "\n")
		if len(lines) < 2 {
			t.Fatalf("expected daemon-reload and restart commands, got %q", readLog())
		}
		if lines[0] != "daemon-reload" {
			t.Fatalf("expected daemon-reload first, got %q", lines[0])
		}
		if lines[1] != "restart containerd" {
			t.Fatalf("expected restart after daemon-reload, got %q", lines[1])
		}
	})

	t.Run("ifExists skips missing units", func(t *testing.T) {
		resetLog()
		t.Setenv("SYSTEMCTL_ENABLED_UNITS", "")
		t.Setenv("SYSTEMCTL_ACTIVE_UNITS", "firewalld")
		t.Setenv("SYSTEMCTL_EXISTING_UNITS", "firewalld.service")
		t.Setenv("SYSTEMCTL_MISSING_UNITS", "")

		if err := runManageService(context.Background(), map[string]any{"names": []any{"firewalld", "ufw"}, "state": "stopped", "ifExists": true}); err != nil {
			t.Fatalf("runManageService failed: %v", err)
		}

		got := readLog()
		if !strings.Contains(got, "stop firewalld") {
			t.Fatalf("expected firewalld stop call, got %q", got)
		}
		if strings.Contains(got, "ufw") {
			t.Fatalf("expected missing ufw service to be skipped, got %q", got)
		}
	})

	t.Run("ignoreMissing suppresses missing unit operation failures", func(t *testing.T) {
		resetLog()
		t.Setenv("SYSTEMCTL_ENABLED_UNITS", "")
		t.Setenv("SYSTEMCTL_ACTIVE_UNITS", "")
		t.Setenv("SYSTEMCTL_EXISTING_UNITS", "firewalld.service")
		t.Setenv("SYSTEMCTL_MISSING_UNITS", "ufw")

		if err := runManageService(context.Background(), map[string]any{"names": []any{"firewalld", "ufw"}, "state": "started", "ignoreMissing": true}); err != nil {
			t.Fatalf("runManageService failed: %v", err)
		}

		got := readLog()
		if !strings.Contains(got, "start firewalld") {
			t.Fatalf("expected start call for existing service, got %q", got)
		}
		if strings.Contains(got, "start ufw") {
			t.Fatalf("expected missing ufw start failure to be suppressed, got %q", got)
		}
	})
}

func TestRun_ManageServiceRegistersNamesOutput(t *testing.T) {
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
	logPath := filepath.Join(dir, "systemctl.log")
	script := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n' \"$*\" >> \"" + logPath + "\"\nexit 0\n"
	if err := os.WriteFile(filepath.Join(binDir, "systemctl"), []byte(script), 0o755); err != nil {
		t.Fatalf("write systemctl script: %v", err)
	}
	t.Setenv("PATH", fmt.Sprintf("%s:%s", binDir, os.Getenv("PATH")))

	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "install",
			Steps: []config.Step{{
				ID:       "ManageService",
				Kind:     "ManageService",
				Spec:     map[string]any{"names": []any{"firewalld", "ufw"}, "state": "restarted", "ignoreMissing": true},
				Register: map[string]string{"managedManageServices": "names"},
			}},
		}},
	}

	if err := Run(context.Background(), wf, RunOptions{BundleRoot: bundle, StatePath: statePath}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	stateRaw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var st State
	if err := json.Unmarshal(stateRaw, &st); err != nil {
		t.Fatalf("parse state: %v", err)
	}
	services, ok := st.RuntimeVars["managedManageServices"].([]any)
	if !ok || len(services) != 2 || services[0] != "firewalld" || services[1] != "ufw" {
		t.Fatalf("expected registered service names, got %#v", st.RuntimeVars["managedManageServices"])
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
	if err := runWriteFile(spec); err != nil {
		t.Fatalf("runWriteFile failed: %v", err)
	}
	before, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat before: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if err := runWriteFile(spec); err != nil {
		t.Fatalf("runWriteFile second pass failed: %v", err)
	}
	after, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("expected idempotent write to keep mtime")
	}
}

func TestCommandSupportsEnvAndSudo(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	logPath := filepath.Join(dir, "command.log")
	sudoScript := "#!/usr/bin/env bash\nset -euo pipefail\nprintf 'sudo:%s\\n' \"$*\" >> \"" + logPath + "\"\nexec \"$@\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "sudo"), []byte(sudoScript), 0o755); err != nil {
		t.Fatalf("write sudo script: %v", err)
	}
	commandPath := filepath.Join(binDir, "print-env")
	commandScript := "#!/usr/bin/env bash\nset -euo pipefail\nprintf 'env:%s\\n' \"${DECK_TEST_ENV:-missing}\" >> \"" + logPath + "\"\n"
	if err := os.WriteFile(commandPath, []byte(commandScript), 0o755); err != nil {
		t.Fatalf("write command script: %v", err)
	}
	t.Setenv("PATH", fmt.Sprintf("%s:%s", binDir, os.Getenv("PATH")))

	if err := runCommand(context.Background(), map[string]any{
		"command": []any{"print-env"},
		"env":     map[string]any{"DECK_TEST_ENV": "present"},
		"sudo":    true,
	}); err != nil {
		t.Fatalf("runCommand failed: %v", err)
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	logText := string(raw)
	if !strings.Contains(logText, "sudo:print-env") {
		t.Fatalf("expected sudo invocation, got %q", logText)
	}
	if !strings.Contains(logText, "env:present") {
		t.Fatalf("expected env to be passed, got %q", logText)
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

func TestWriteSystemdUnitStep(t *testing.T) {
	t.Run("writes unit file with content", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "systemd", "demo.service")
		if err := runWriteSystemdUnit(context.Background(), map[string]any{"path": target, "content": "[Unit]\nDescription=demo"}); err != nil {
			t.Fatalf("runWriteSystemdUnit failed: %v", err)
		}
		raw, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("read unit file: %v", err)
		}
		if string(raw) != "[Unit]\nDescription=demo\n" {
			t.Fatalf("unexpected unit content: %q", string(raw))
		}
	})

	t.Run("writes unit file from template content", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "systemd", "templated.service")
		if err := runWriteSystemdUnit(context.Background(), map[string]any{"path": target, "template": "[ManageService]\nExecStart=/usr/bin/true"}); err != nil {
			t.Fatalf("runWriteSystemdUnit failed: %v", err)
		}
		raw, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("read unit file: %v", err)
		}
		if string(raw) != "[ManageService]\nExecStart=/usr/bin/true\n" {
			t.Fatalf("unexpected unit template content: %q", string(raw))
		}
	})

	t.Run("creates parent directories", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "etc", "systemd", "system", "kubelet.service")
		if err := runWriteSystemdUnit(context.Background(), map[string]any{"path": target, "content": "[Install]"}); err != nil {
			t.Fatalf("runWriteSystemdUnit failed: %v", err)
		}
		if _, err := os.Stat(filepath.Dir(target)); err != nil {
			t.Fatalf("expected parent directory to exist: %v", err)
		}
	})
}

func TestRepoConfigStep(t *testing.T) {
	t.Run("yum with explicit path", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "repo", "offline.repo")
		spec := map[string]any{
			"format": "yum",
			"path":   target,
			"repositories": []any{map[string]any{
				"id":       "offline-base",
				"name":     "offline-base",
				"baseurl":  "file:///srv/repo",
				"enabled":  true,
				"gpgcheck": false,
			}},
		}
		if err := runRepoConfig(context.Background(), spec); err != nil {
			t.Fatalf("runRepoConfig failed: %v", err)
		}
		raw, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("read config: %v", err)
		}
		got := string(raw)
		if !strings.Contains(got, "[offline-base]") || !strings.Contains(got, "baseurl=file:///srv/repo") {
			t.Fatalf("unexpected repo config: %q", got)
		}
	})

	t.Run("apt list rendering", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "repo", "offline.list")
		spec := map[string]any{
			"format": "apt",
			"path":   target,
			"repositories": []any{map[string]any{
				"baseurl":   "http://repo.local/apt/bookworm",
				"trusted":   true,
				"suite":     "./",
				"component": "main",
			}},
		}
		if err := runRepoConfig(context.Background(), spec); err != nil {
			t.Fatalf("runRepoConfig failed: %v", err)
		}
		raw, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("read config: %v", err)
		}
		if strings.TrimSpace(string(raw)) != "deb [trusted=yes] http://repo.local/apt/bookworm ./ main" {
			t.Fatalf("unexpected apt repo config: %q", string(raw))
		}
	})

	t.Run("auto format uses host family and default path", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "repo", "default.list")
		origDetect := repoConfigDetectHostFacts
		origDefaultPath := repoConfigDefaultPathFunc
		t.Cleanup(func() {
			repoConfigDetectHostFacts = origDetect
			repoConfigDefaultPathFunc = origDefaultPath
		})
		repoConfigDetectHostFacts = func() map[string]any {
			return map[string]any{"os": map[string]any{"family": "debian"}}
		}
		repoConfigDefaultPathFunc = func(format string) string {
			if format != "apt" {
				t.Fatalf("expected apt format, got %s", format)
			}
			return target
		}

		spec := map[string]any{
			"format": "auto",
			"repositories": []any{map[string]any{
				"baseurl": "http://repo.local/apt/bookworm",
			}},
		}
		if err := runRepoConfig(context.Background(), spec); err != nil {
			t.Fatalf("runRepoConfig failed: %v", err)
		}
		raw, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("read config: %v", err)
		}
		if strings.TrimSpace(string(raw)) != "deb http://repo.local/apt/bookworm ./" {
			t.Fatalf("unexpected apt auto-rendered config: %q", string(raw))
		}
	})

	t.Run("cleanup and backup paths", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "repo", "offline.repo")
		legacyA := filepath.Join(dir, "legacy-a.repo")
		legacyB := filepath.Join(dir, "legacy-b.repo")
		if err := os.WriteFile(legacyA, []byte("[a]\nenabled=1\n"), 0o644); err != nil {
			t.Fatalf("write legacyA: %v", err)
		}
		if err := os.WriteFile(legacyB, []byte("[b]\nenabled=1\n"), 0o644); err != nil {
			t.Fatalf("write legacyB: %v", err)
		}

		spec := map[string]any{
			"format":       "yum",
			"path":         target,
			"cleanupPaths": []any{filepath.Join(dir, "legacy-*.repo")},
			"backupPaths":  []any{filepath.Join(dir, "legacy-*.repo")},
			"repositories": []any{map[string]any{
				"id":      "offline",
				"baseurl": "file:///srv/repo",
			}},
		}
		if err := runRepoConfig(context.Background(), spec); err != nil {
			t.Fatalf("runRepoConfig failed: %v", err)
		}
		if _, err := os.Stat(legacyA + ".deck.bak"); err != nil {
			t.Fatalf("missing backup for legacyA: %v", err)
		}
		if _, err := os.Stat(legacyB + ".deck.bak"); err != nil {
			t.Fatalf("missing backup for legacyB: %v", err)
		}
		if _, err := os.Stat(legacyA); !os.IsNotExist(err) {
			t.Fatalf("expected legacyA removed, err=%v", err)
		}
		if _, err := os.Stat(legacyB); !os.IsNotExist(err) {
			t.Fatalf("expected legacyB removed, err=%v", err)
		}
	})

	t.Run("disable existing yum repositories", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "offline.repo")
		existing := filepath.Join(dir, "legacy.repo")
		if err := os.WriteFile(existing, []byte("[legacy]\nname=legacy\nenabled=1\n"), 0o644); err != nil {
			t.Fatalf("write legacy repo: %v", err)
		}

		spec := map[string]any{
			"format":          "yum",
			"path":            target,
			"disableExisting": true,
			"backupPaths":     []any{existing},
			"repositories": []any{map[string]any{
				"id":      "offline",
				"baseurl": "file:///srv/repo",
			}},
		}
		if err := runRepoConfig(context.Background(), spec); err != nil {
			t.Fatalf("runRepoConfig failed: %v", err)
		}

		raw, err := os.ReadFile(existing)
		if err != nil {
			t.Fatalf("read legacy repo: %v", err)
		}
		if !strings.Contains(string(raw), "enabled=0") {
			t.Fatalf("expected existing repo to be disabled, got %q", string(raw))
		}
	})

	t.Run("disable existing apt source paths", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "offline.list")
		existing := filepath.Join(dir, "legacy.list")
		if err := os.WriteFile(existing, []byte("deb http://legacy.local stable main\n"), 0o644); err != nil {
			t.Fatalf("write legacy apt source: %v", err)
		}

		spec := map[string]any{
			"format":          "apt",
			"path":            target,
			"disableExisting": true,
			"backupPaths":     []any{existing},
			"repositories": []any{map[string]any{
				"baseurl": "http://repo.local/apt/bookworm",
			}},
		}
		if err := runRepoConfig(context.Background(), spec); err != nil {
			t.Fatalf("runRepoConfig failed: %v", err)
		}

		if _, err := os.Stat(existing + ".deck.bak"); err != nil {
			t.Fatalf("missing backup for existing apt source: %v", err)
		}
		if _, err := os.Stat(existing); !os.IsNotExist(err) {
			t.Fatalf("expected existing apt source removed, err=%v", err)
		}

		raw, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("read new apt source: %v", err)
		}
		if strings.TrimSpace(string(raw)) != "deb http://repo.local/apt/bookworm ./" {
			t.Fatalf("unexpected apt repo config: %q", string(raw))
		}
	})

	t.Run("repository configure does not refresh cache inline", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "repo", "offline.list")
		origRun := repoConfigRunTimedCommand
		t.Cleanup(func() { repoConfigRunTimedCommand = origRun })
		calls := make([]string, 0)
		repoConfigRunTimedCommand = func(_ context.Context, name string, args []string, timeout time.Duration) error {
			calls = append(calls, name+" "+strings.Join(args, " "))
			if timeout < time.Second {
				t.Fatalf("unexpected timeout: %s", timeout)
			}
			return nil
		}

		spec := map[string]any{
			"format": "apt",
			"path":   target,
			"repositories": []any{map[string]any{
				"baseurl": "http://repo.local/apt/bookworm",
			}},
		}
		if err := runRepoConfig(context.Background(), spec); err != nil {
			t.Fatalf("runRepoConfig failed: %v", err)
		}
		if len(calls) != 0 {
			t.Fatalf("expected no refresh commands during configure: %#v", calls)
		}
	})

	t.Run("repository configure only writes repo files", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "repo", "offline.list")
		origRun := repoConfigRunTimedCommand
		t.Cleanup(func() { repoConfigRunTimedCommand = origRun })
		calls := make([]string, 0)
		repoConfigRunTimedCommand = func(_ context.Context, name string, args []string, timeout time.Duration) error {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return nil
		}

		spec := map[string]any{
			"format": "apt",
			"path":   target,
			"repositories": []any{map[string]any{
				"baseurl": "http://repo.local/apt/bookworm",
			}},
		}
		if err := runRepoConfig(context.Background(), spec); err != nil {
			t.Fatalf("runRepoConfig failed: %v", err)
		}
		if len(calls) != 0 {
			t.Fatalf("expected no refresh command sequence: %#v", calls)
		}
	})
}

func TestRefreshRepositoryStep(t *testing.T) {
	t.Run("apt clean only", func(t *testing.T) {
		calls := make([]string, 0)
		runner := func(name string, args []string, timeout time.Duration) error {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return nil
		}

		spec := map[string]any{"manager": "apt", "clean": true}
		if err := runRefreshRepositoryWithRunner(spec, runner); err != nil {
			t.Fatalf("runRefreshRepository failed: %v", err)
		}

		if len(calls) != 1 || calls[0] != "apt-get clean" {
			t.Fatalf("unexpected apt clean commands: %#v", calls)
		}
	})

	t.Run("apt clean plus update ordering", func(t *testing.T) {
		calls := make([]string, 0)
		runner := func(name string, args []string, timeout time.Duration) error {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return nil
		}

		spec := map[string]any{"manager": "apt", "clean": true, "update": true}
		if err := runRefreshRepositoryWithRunner(spec, runner); err != nil {
			t.Fatalf("runRefreshRepository failed: %v", err)
		}

		if len(calls) != 2 || calls[0] != "apt-get clean" || calls[1] != "apt-get update" {
			t.Fatalf("unexpected apt clean/update sequence: %#v", calls)
		}
	})

	t.Run("dnf clean only", func(t *testing.T) {
		calls := make([]string, 0)
		runner := func(name string, args []string, timeout time.Duration) error {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return nil
		}

		spec := map[string]any{"manager": "dnf", "clean": true}
		if err := runRefreshRepositoryWithRunner(spec, runner); err != nil {
			t.Fatalf("runRefreshRepository failed: %v", err)
		}

		if len(calls) != 1 || calls[0] != "dnf clean all" {
			t.Fatalf("unexpected dnf clean commands: %#v", calls)
		}
	})

	t.Run("dnf update uses makecache behavior", func(t *testing.T) {
		calls := make([]string, 0)
		runner := func(name string, args []string, timeout time.Duration) error {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return nil
		}

		spec := map[string]any{"manager": "dnf", "update": true}
		if err := runRefreshRepositoryWithRunner(spec, runner); err != nil {
			t.Fatalf("runRefreshRepository failed: %v", err)
		}

		if len(calls) != 1 || calls[0] != "dnf makecache -y" {
			t.Fatalf("expected makecache call, got %#v", calls)
		}
		for _, call := range calls {
			if strings.Contains(call, "dnf update") || strings.Contains(call, "dnf install") {
				t.Fatalf("dnf update must not perform package upgrade/install, got %q", call)
			}
		}
	})

	t.Run("auto manager resolves using host facts", func(t *testing.T) {
		origDetect := repoConfigDetectHostFacts
		t.Cleanup(func() { repoConfigDetectHostFacts = origDetect })

		calls := make([]string, 0)
		runner := func(name string, args []string, timeout time.Duration) error {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return nil
		}
		repoConfigDetectHostFacts = func() map[string]any {
			return map[string]any{"os": map[string]any{"family": "rhel"}}
		}

		spec := map[string]any{"manager": "auto", "update": true}
		if err := runRefreshRepositoryWithRunner(spec, runner); err != nil {
			t.Fatalf("runRefreshRepository failed: %v", err)
		}

		if len(calls) != 1 || calls[0] != "dnf makecache -y" {
			t.Fatalf("expected auto/rhel to resolve to dnf makecache, got %#v", calls)
		}
	})
}

func TestWriteContainerdConfigStep(t *testing.T) {
	t.Run("updates existing config.toml fields", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "config.toml")
		initial := "config_path = \"\"\n            SystemdCgroup = false\n"
		if err := os.WriteFile(target, []byte(initial), 0o644); err != nil {
			t.Fatalf("write initial config: %v", err)
		}

		spec := map[string]any{"path": target, "configPath": "/etc/containerd/certs.d", "systemdCgroup": true}
		if err := runWriteContainerdConfig(context.Background(), spec); err != nil {
			t.Fatalf("runWriteContainerdConfig failed: %v", err)
		}

		raw, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("read config: %v", err)
		}
		got := string(raw)
		if !strings.Contains(got, "config_path = \"/etc/containerd/certs.d\"") || !strings.Contains(got, "SystemdCgroup = true") {
			t.Fatalf("unexpected config content: %q", got)
		}
	})

	t.Run("writes single registry hosts file under explicit configPath", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "config.toml")
		configPath := filepath.Join(dir, "certs.d")
		if err := os.WriteFile(target, []byte("version = 2\n"), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		spec := map[string]any{
			"path": configPath,
			"registryHosts": []any{
				map[string]any{
					"registry":     "registry.k8s.io",
					"server":       "https://registry.k8s.io",
					"host":         "http://127.0.0.1:5000",
					"capabilities": []any{"pull", "resolve"},
					"skipVerify":   true,
				},
			},
		}

		if err := runWriteContainerdRegistryHosts(spec); err != nil {
			t.Fatalf("runWriteContainerdRegistryHosts failed: %v", err)
		}

		hostsPath := filepath.Join(configPath, "registry.k8s.io", "hosts.toml")
		hostsRaw, err := os.ReadFile(hostsPath)
		if err != nil {
			t.Fatalf("read hosts.toml: %v", err)
		}
		expected := "server = \"https://registry.k8s.io\"\n\n[host.\"http://127.0.0.1:5000\"]\n  capabilities = [\"pull\", \"resolve\"]\n  skip_verify = true\n"
		if string(hostsRaw) != expected {
			t.Fatalf("unexpected hosts.toml content: %q", string(hostsRaw))
		}

		if err := runWriteContainerdRegistryHosts(spec); err != nil {
			t.Fatalf("runWriteContainerdRegistryHosts second pass failed: %v", err)
		}
		hostsRawAgain, err := os.ReadFile(hostsPath)
		if err != nil {
			t.Fatalf("read hosts.toml second pass: %v", err)
		}
		if string(hostsRawAgain) != expected {
			t.Fatalf("hosts.toml changed unexpectedly on second pass: %q", string(hostsRawAgain))
		}
	})

	t.Run("writes multiple registry hosts files with default configPath", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "containerd", "config.toml")
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatalf("mkdir config parent: %v", err)
		}
		if err := os.WriteFile(target, []byte("version = 2\n"), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		defaultConfigPath := filepath.Join(filepath.Dir(target), "certs.d")
		firstHostsPath := filepath.Join(defaultConfigPath, "registry.k8s.io", "hosts.toml")
		secondHostsPath := filepath.Join(defaultConfigPath, "docker.io", "hosts.toml")

		spec := map[string]any{
			"path": target,
			"registryHosts": []any{
				map[string]any{
					"registry":     "registry.k8s.io",
					"server":       "https://registry.k8s.io",
					"host":         "http://127.0.0.1:5000",
					"capabilities": []any{"pull", "resolve"},
					"skipVerify":   true,
				},
				map[string]any{
					"registry":     "docker.io",
					"server":       "https://registry-1.docker.io",
					"host":         "http://127.0.0.1:5001",
					"capabilities": []any{"pull"},
					"skipVerify":   false,
				},
			},
		}

		if err := runWriteContainerdRegistryHosts(map[string]any{"path": defaultConfigPath, "registryHosts": spec["registryHosts"]}); err != nil {
			t.Fatalf("runWriteContainerdRegistryHosts failed: %v", err)
		}

		firstRaw, err := os.ReadFile(firstHostsPath)
		if err != nil {
			t.Fatalf("read first hosts.toml: %v", err)
		}
		if !strings.Contains(string(firstRaw), "server = \"https://registry.k8s.io\"") {
			t.Fatalf("unexpected first hosts.toml content: %q", string(firstRaw))
		}

		secondRaw, err := os.ReadFile(secondHostsPath)
		if err != nil {
			t.Fatalf("read second hosts.toml: %v", err)
		}
		if !strings.Contains(string(secondRaw), "server = \"https://registry-1.docker.io\"") || !strings.Contains(string(secondRaw), "skip_verify = false") {
			t.Fatalf("unexpected second hosts.toml content: %q", string(secondRaw))
		}

		configRaw, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("read config.toml: %v", err)
		}
		if strings.Contains(string(configRaw), "config_path") {
			t.Fatalf("did not expect config_path to be injected when configPath is omitted: %q", string(configRaw))
		}
	})
}

func TestSwapStep(t *testing.T) {
	dir := t.TempDir()
	fstab := filepath.Join(dir, "fstab")
	content := "UUID=abc / ext4 defaults 0 1\n/swapfile none swap sw 0 0\n"
	if err := os.WriteFile(fstab, []byte(content), 0o644); err != nil {
		t.Fatalf("write fstab: %v", err)
	}
	if err := runSwap(context.Background(), map[string]any{"disable": false, "persist": true, "fstabPath": fstab}); err != nil {
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
	if err := runKernelModule(context.Background(), spec); err != nil {
		t.Fatalf("runKernelModule failed: %v", err)
	}
	if err := runKernelModule(context.Background(), spec); err != nil {
		t.Fatalf("runKernelModule second pass failed: %v", err)
	}
	raw, err := os.ReadFile(persistPath)
	if err != nil {
		t.Fatalf("read persist file: %v", err)
	}
	if strings.Count(string(raw), "overlay") != 1 {
		t.Fatalf("expected single module line, got %q", string(raw))
	}

	multiPersistPath := filepath.Join(dir, "modules-load.d", "multi.conf")
	multiSpec := map[string]any{"names": []any{"overlay", "br_netfilter", "overlay"}, "load": false, "persist": true, "persistFile": multiPersistPath}
	if err := runKernelModule(context.Background(), multiSpec); err != nil {
		t.Fatalf("runKernelModule multi failed: %v", err)
	}
	multiRaw, err := os.ReadFile(multiPersistPath)
	if err != nil {
		t.Fatalf("read multi persist file: %v", err)
	}
	if strings.Count(string(multiRaw), "overlay") != 1 || strings.Count(string(multiRaw), "br_netfilter") != 1 {
		t.Fatalf("expected deduplicated module lines, got %q", string(multiRaw))
	}
}

func TestRun_WaitRequiresRequiredFields(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state", "state.json")
	target := filepath.Join(dir, "appears.txt")
	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "install",
			Steps: []config.Step{{
				ID:   "wait-file",
				Kind: "WaitForFile",
				Spec: map[string]any{"path": target, "timeout": "10ms"},
			}},
		}},
	}
	err := Run(context.Background(), wf, RunOptions{StatePath: statePath})
	if err == nil {
		t.Fatalf("expected wait timeout error")
	}
	if !strings.Contains(err.Error(), errCodeInstallWaitTimeout) {
		t.Fatalf("expected wait timeout error, got %v", err)
	}
}

func TestRun_RepositoryRequiresExplicitAction(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state", "state.json")
	target := filepath.Join(dir, "offline.list")
	wf := &config.Workflow{
		Version: "v1",
		Phases: []config.Phase{{
			Name: "install",
			Steps: []config.Step{{
				ID:   "repo-config",
				Kind: "ConfigureRepository",
				Spec: map[string]any{
					"format":       "apt",
					"path":         target,
					"repositories": []any{map[string]any{"id": "offline", "baseurl": "http://repo.local/debian"}},
				},
			}},
		}},
	}
	if err := Run(context.Background(), wf, RunOptions{StatePath: statePath}); err != nil {
		t.Fatalf("expected repository step to run, got %v", err)
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

	if err := runSysctlApply(context.Background(), map[string]any{"file": "/etc/sysctl.d/99-kubernetes-cri.conf"}); err != nil {
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

func TestRun_Kubeadm(t *testing.T) {
	t.Run("runs reset command and cleanup actions", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state", "state.json")
		binDir := filepath.Join(dir, "bin")
		if err := os.MkdirAll(binDir, 0o755); err != nil {
			t.Fatalf("mkdir bin: %v", err)
		}

		kubeadmLog := filepath.Join(dir, "kubeadm.log")
		systemctlLog := filepath.Join(dir, "systemctl.log")
		crictlLog := filepath.Join(dir, "crictl.log")

		kubeadmScript := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n' \"$*\" >> \"" + kubeadmLog + "\"\nexit 0\n"
		if err := os.WriteFile(filepath.Join(binDir, "kubeadm"), []byte(kubeadmScript), 0o755); err != nil {
			t.Fatalf("write kubeadm script: %v", err)
		}
		systemctlScript := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n' \"$*\" >> \"" + systemctlLog + "\"\nexit 0\n"
		if err := os.WriteFile(filepath.Join(binDir, "systemctl"), []byte(systemctlScript), 0o755); err != nil {
			t.Fatalf("write systemctl script: %v", err)
		}
		crictlScript := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n' \"$*\" >> \"" + crictlLog + "\"\nif [[ \"$*\" == *\"ps -a --name kube-apiserver -q\"* ]]; then\n  echo cid-apiserver\n  exit 0\nfi\nif [[ \"$*\" == *\"ps -a --name etcd -q\"* ]]; then\n  echo cid-etcd\n  exit 0\nfi\nexit 0\n"
		if err := os.WriteFile(filepath.Join(binDir, "crictl"), []byte(crictlScript), 0o755); err != nil {
			t.Fatalf("write crictl script: %v", err)
		}
		t.Setenv("PATH", fmt.Sprintf("%s:%s", binDir, os.Getenv("PATH")))

		removeDir := filepath.Join(dir, "remove-dir")
		removeFile := filepath.Join(dir, "remove-file.conf")
		if err := os.MkdirAll(removeDir, 0o755); err != nil {
			t.Fatalf("mkdir remove dir: %v", err)
		}
		if err := os.WriteFile(removeFile, []byte("stale"), 0o644); err != nil {
			t.Fatalf("write remove file: %v", err)
		}

		wf := &config.Workflow{
			Version: "v1",
			Phases: []config.Phase{{
				Name: "install",
				Steps: []config.Step{{
					ID:   "reset",
					Kind: "ResetKubeadm",
					Spec: map[string]any{
						"mode":                  "real",
						"force":                 true,
						"criSocket":             "unix:///run/containerd/containerd.sock",
						"removePaths":           []any{removeDir},
						"removeFiles":           []any{removeFile},
						"cleanupContainers":     []any{"kube-apiserver", "etcd"},
						"restartRuntimeService": "containerd",
					},
				}},
			}},
		}

		if err := Run(context.Background(), wf, RunOptions{StatePath: statePath}); err != nil {
			t.Fatalf("kubeadm reset run failed: %v", err)
		}

		if _, err := os.Stat(removeDir); !os.IsNotExist(err) {
			t.Fatalf("expected remove dir deleted, err=%v", err)
		}
		if _, err := os.Stat(removeFile); !os.IsNotExist(err) {
			t.Fatalf("expected remove file deleted, err=%v", err)
		}

		kubeadmRaw, err := os.ReadFile(kubeadmLog)
		if err != nil {
			t.Fatalf("read kubeadm log: %v", err)
		}
		kubeadmArgs := strings.TrimSpace(string(kubeadmRaw))
		if !strings.Contains(kubeadmArgs, "reset --force --cri-socket unix:///run/containerd/containerd.sock") {
			t.Fatalf("unexpected kubeadm args: %q", kubeadmArgs)
		}

		systemctlRaw, err := os.ReadFile(systemctlLog)
		if err != nil {
			t.Fatalf("read systemctl log: %v", err)
		}
		systemctlLogText := string(systemctlRaw)
		if !strings.Contains(systemctlLogText, "stop kubelet") {
			t.Fatalf("expected kubelet stop command, got %q", systemctlLogText)
		}
		if !strings.Contains(systemctlLogText, "restart containerd") {
			t.Fatalf("expected runtime restart command, got %q", systemctlLogText)
		}

		crictlRaw, err := os.ReadFile(crictlLog)
		if err != nil {
			t.Fatalf("read crictl log: %v", err)
		}
		crictlLogText := string(crictlRaw)
		if !strings.Contains(crictlLogText, "ps -a --name kube-apiserver -q") || !strings.Contains(crictlLogText, "ps -a --name etcd -q") {
			t.Fatalf("expected crictl ps cleanup lookups, got %q", crictlLogText)
		}
		if !strings.Contains(crictlLogText, "rm -f cid-apiserver") || !strings.Contains(crictlLogText, "rm -f cid-etcd") {
			t.Fatalf("expected crictl rm cleanup calls, got %q", crictlLogText)
		}
	})

	t.Run("ignoreErrors tolerates kubeadm failure and continues cleanup", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state", "state.json")
		binDir := filepath.Join(dir, "bin")
		if err := os.MkdirAll(binDir, 0o755); err != nil {
			t.Fatalf("mkdir bin: %v", err)
		}

		systemctlLog := filepath.Join(dir, "systemctl.log")
		kubeadmScript := "#!/usr/bin/env bash\nset -euo pipefail\nexit 1\n"
		if err := os.WriteFile(filepath.Join(binDir, "kubeadm"), []byte(kubeadmScript), 0o755); err != nil {
			t.Fatalf("write kubeadm script: %v", err)
		}
		systemctlScript := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n' \"$*\" >> \"" + systemctlLog + "\"\nexit 0\n"
		if err := os.WriteFile(filepath.Join(binDir, "systemctl"), []byte(systemctlScript), 0o755); err != nil {
			t.Fatalf("write systemctl script: %v", err)
		}
		crictlScript := "#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n"
		if err := os.WriteFile(filepath.Join(binDir, "crictl"), []byte(crictlScript), 0o755); err != nil {
			t.Fatalf("write crictl script: %v", err)
		}
		t.Setenv("PATH", fmt.Sprintf("%s:%s", binDir, os.Getenv("PATH")))

		removeDir := filepath.Join(dir, "remove-dir")
		removeFile := filepath.Join(dir, "remove-file.conf")
		if err := os.MkdirAll(removeDir, 0o755); err != nil {
			t.Fatalf("mkdir remove dir: %v", err)
		}
		if err := os.WriteFile(removeFile, []byte("stale"), 0o644); err != nil {
			t.Fatalf("write remove file: %v", err)
		}

		wf := &config.Workflow{
			Version: "v1",
			Phases: []config.Phase{{
				Name: "install",
				Steps: []config.Step{{
					ID:   "reset",
					Kind: "ResetKubeadm",
					Spec: map[string]any{
						"mode":                  "real",
						"ignoreErrors":          true,
						"removePaths":           []any{removeDir},
						"removeFiles":           []any{removeFile},
						"restartRuntimeService": "containerd",
					},
				}},
			}},
		}

		if err := Run(context.Background(), wf, RunOptions{StatePath: statePath}); err != nil {
			t.Fatalf("expected ignoreErrors success, got %v", err)
		}

		if _, err := os.Stat(removeDir); !os.IsNotExist(err) {
			t.Fatalf("expected remove dir deleted, err=%v", err)
		}
		if _, err := os.Stat(removeFile); !os.IsNotExist(err) {
			t.Fatalf("expected remove file deleted, err=%v", err)
		}

		systemctlRaw, err := os.ReadFile(systemctlLog)
		if err != nil {
			t.Fatalf("read systemctl log: %v", err)
		}
		systemctlLogText := string(systemctlRaw)
		if !strings.Contains(systemctlLogText, "restart containerd") {
			t.Fatalf("expected runtime restart command, got %q", systemctlLogText)
		}
	})

	t.Run("real mode passes extra args to kubeadm reset", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state", "state.json")
		binDir := filepath.Join(dir, "bin")
		if err := os.MkdirAll(binDir, 0o755); err != nil {
			t.Fatalf("mkdir bin: %v", err)
		}

		kubeadmLog := filepath.Join(dir, "kubeadm.log")
		kubeadmScript := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n' \"$*\" >> \"" + kubeadmLog + "\"\nexit 0\n"
		if err := os.WriteFile(filepath.Join(binDir, "kubeadm"), []byte(kubeadmScript), 0o755); err != nil {
			t.Fatalf("write kubeadm script: %v", err)
		}
		systemctlScript := "#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n"
		if err := os.WriteFile(filepath.Join(binDir, "systemctl"), []byte(systemctlScript), 0o755); err != nil {
			t.Fatalf("write systemctl script: %v", err)
		}
		crictlScript := "#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n"
		if err := os.WriteFile(filepath.Join(binDir, "crictl"), []byte(crictlScript), 0o755); err != nil {
			t.Fatalf("write crictl script: %v", err)
		}
		t.Setenv("PATH", fmt.Sprintf("%s:%s", binDir, os.Getenv("PATH")))

		wf := &config.Workflow{
			Version: "v1",
			Phases: []config.Phase{{
				Name: "install",
				Steps: []config.Step{{
					ID:   "reset",
					Kind: "ResetKubeadm",
					Spec: map[string]any{
						"mode":      "real",
						"force":     true,
						"extraArgs": []any{"--cleanup-tmp-dir"},
					},
				}},
			}},
		}

		if err := Run(context.Background(), wf, RunOptions{StatePath: statePath}); err != nil {
			t.Fatalf("reset run failed: %v", err)
		}

		raw, err := os.ReadFile(kubeadmLog)
		if err != nil {
			t.Fatalf("read kubeadm log: %v", err)
		}
		if got := strings.TrimSpace(string(raw)); got != "reset --force --cleanup-tmp-dir" {
			t.Fatalf("unexpected kubeadm args: %q", got)
		}
	})

	t.Run("stub mode skips reset side effects", func(t *testing.T) {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state", "state.json")
		binDir := filepath.Join(dir, "bin")
		if err := os.MkdirAll(binDir, 0o755); err != nil {
			t.Fatalf("mkdir bin: %v", err)
		}

		kubeadmLog := filepath.Join(dir, "kubeadm.log")
		systemctlLog := filepath.Join(dir, "systemctl.log")
		crictlLog := filepath.Join(dir, "crictl.log")
		kubeadmScript := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n' \"$*\" >> \"" + kubeadmLog + "\"\nexit 1\n"
		if err := os.WriteFile(filepath.Join(binDir, "kubeadm"), []byte(kubeadmScript), 0o755); err != nil {
			t.Fatalf("write kubeadm script: %v", err)
		}
		systemctlScript := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n' \"$*\" >> \"" + systemctlLog + "\"\nexit 1\n"
		if err := os.WriteFile(filepath.Join(binDir, "systemctl"), []byte(systemctlScript), 0o755); err != nil {
			t.Fatalf("write systemctl script: %v", err)
		}
		crictlScript := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n' \"$*\" >> \"" + crictlLog + "\"\nexit 1\n"
		if err := os.WriteFile(filepath.Join(binDir, "crictl"), []byte(crictlScript), 0o755); err != nil {
			t.Fatalf("write crictl script: %v", err)
		}
		t.Setenv("PATH", fmt.Sprintf("%s:%s", binDir, os.Getenv("PATH")))

		removeDir := filepath.Join(dir, "remove-dir")
		removeFile := filepath.Join(dir, "remove-file.conf")
		if err := os.MkdirAll(removeDir, 0o755); err != nil {
			t.Fatalf("mkdir remove dir: %v", err)
		}
		if err := os.WriteFile(removeFile, []byte("stale"), 0o644); err != nil {
			t.Fatalf("write remove file: %v", err)
		}

		wf := &config.Workflow{
			Version: "v1",
			Phases: []config.Phase{{
				Name: "install",
				Steps: []config.Step{{
					ID:   "reset",
					Kind: "ResetKubeadm",
					Spec: map[string]any{
						"force":                 true,
						"criSocket":             "unix:///run/containerd/containerd.sock",
						"extraArgs":             []any{"--cleanup-tmp-dir"},
						"removePaths":           []any{removeDir},
						"removeFiles":           []any{removeFile},
						"cleanupContainers":     []any{"kube-apiserver"},
						"restartRuntimeService": "containerd",
					},
				}},
			}},
		}
		useStubResetKubeadm(t)

		if err := Run(context.Background(), wf, RunOptions{StatePath: statePath}); err != nil {
			t.Fatalf("expected stub reset success, got %v", err)
		}

		if _, err := os.Stat(kubeadmLog); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected kubeadm not to run in stub mode, err=%v", err)
		}
		if _, err := os.Stat(systemctlLog); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected systemctl not to run in stub mode, err=%v", err)
		}
		if _, err := os.Stat(crictlLog); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected crictl not to run in stub mode, err=%v", err)
		}
		if _, err := os.Stat(removeDir); err != nil {
			t.Fatalf("expected remove dir to remain in stub mode, err=%v", err)
		}
		if _, err := os.Stat(removeFile); err != nil {
			t.Fatalf("expected remove file to remain in stub mode, err=%v", err)
		}
	})
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
