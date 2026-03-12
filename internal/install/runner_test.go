package install

import (
	"archive/tar"
	"bytes"
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

func TestRun_WaitPath(t *testing.T) {
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
					Kind: "WaitPath",
					Spec: map[string]any{"path": target, "state": "exists", "type": "file", "pollInterval": "10ms", "timeout": "1s"},
				}},
			}},
		}

		go func() {
			time.Sleep(40 * time.Millisecond)
			_ = os.WriteFile(target, []byte("ok"), 0o644)
		}()

		if err := Run(context.Background(), wf, RunOptions{StatePath: statePath}); err != nil {
			t.Fatalf("expected WaitPath success, got %v", err)
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
					Kind: "WaitPath",
					Spec: map[string]any{"path": target, "state": "absent", "pollInterval": "10ms", "timeout": "1s"},
				}},
			}},
		}

		go func() {
			time.Sleep(40 * time.Millisecond)
			_ = os.Remove(target)
		}()

		if err := Run(context.Background(), wf, RunOptions{StatePath: statePath}); err != nil {
			t.Fatalf("expected WaitPath absent success, got %v", err)
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
					Kind: "WaitPath",
					Spec: map[string]any{"path": target, "state": "exists", "type": "file", "nonEmpty": true, "pollInterval": "10ms", "timeout": "1s"},
				}},
			}},
		}

		go func() {
			time.Sleep(40 * time.Millisecond)
			_ = os.WriteFile(target, []byte("ready"), 0o644)
		}()

		if err := Run(context.Background(), wf, RunOptions{StatePath: statePath}); err != nil {
			t.Fatalf("expected WaitPath non-empty success, got %v", err)
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
					Kind:    "WaitPath",
					Timeout: "80ms",
					Spec:    map[string]any{"path": target, "state": "exists", "type": "file", "pollInterval": "10ms"},
				}},
			}},
		}

		err := Run(context.Background(), wf, RunOptions{StatePath: statePath})
		if err == nil {
			t.Fatalf("expected WaitPath timeout error")
		}
		if !strings.Contains(err.Error(), errCodeInstallWaitPathTimeout) {
			t.Fatalf("expected %s, got %v", errCodeInstallWaitPathTimeout, err)
		}
		if !strings.Contains(err.Error(), target) {
			t.Fatalf("expected timeout error to include path, got %v", err)
		}
		if !strings.Contains(err.Error(), "exist as a file") {
			t.Fatalf("expected timeout error to include expected condition, got %v", err)
		}
	})
}

func TestRun_Symlink(t *testing.T) {
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
					ID:   "symlink",
					Kind: "Symlink",
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
					ID:   "symlink",
					Kind: "Symlink",
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
					ID:   "symlink",
					Kind: "Symlink",
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
					ID:   "symlink",
					Kind: "Symlink",
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
					ID:   "symlink",
					Kind: "Symlink",
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
					ID:   "symlink",
					Kind: "Symlink",
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

func TestRun_KubeadmInitRealModeSupportsImagePullAndConfigWrite(t *testing.T) {
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
				Kind: "KubeadmInit",
				Spec: map[string]any{
					"mode":                  "real",
					"outputJoinFile":        joinPath,
					"configFile":            configPath,
					"configTemplate":        "default",
					"pullImages":            true,
					"kubernetesVersion":     "v1.30.14",
					"podNetworkCIDR":        "10.244.0.0/16",
					"criSocket":             "unix:///run/containerd/containerd.sock",
					"ignorePreflightErrors": []any{"Swap"},
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
	if !strings.Contains(logText, "init --config "+configPath+" --apiserver-advertise-address 10.20.30.40 --pod-network-cidr 10.244.0.0/16 --cri-socket unix:///run/containerd/containerd.sock --kubernetes-version v1.30.14 --ignore-preflight-errors Swap --skip-phases=addon/kube-proxy") {
		t.Fatalf("expected kubeadm init args with first-class fields, got %q", logText)
	}
}

func TestRun_KubeadmInitAdvertiseAddressDetectionFallback(t *testing.T) {
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
				Kind: "KubeadmInit",
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

		if err := runService(map[string]any{"name": "containerd", "enabled": true, "state": "started"}); err != nil {
			t.Fatalf("runService failed: %v", err)
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

		if err := runService(map[string]any{"names": []any{"firewalld", "ufw"}, "enabled": false, "state": "stopped"}); err != nil {
			t.Fatalf("runService failed: %v", err)
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

		if err := runService(map[string]any{"name": "containerd", "daemonReload": true, "state": "restarted"}); err != nil {
			t.Fatalf("runService failed: %v", err)
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

		if err := runService(map[string]any{"names": []any{"firewalld", "ufw"}, "state": "stopped", "ifExists": true}); err != nil {
			t.Fatalf("runService failed: %v", err)
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

		if err := runService(map[string]any{"names": []any{"firewalld", "ufw"}, "state": "started", "ignoreMissing": true}); err != nil {
			t.Fatalf("runService failed: %v", err)
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

func TestInstallArtifactsStep_InstallsSingleFileWithMode(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "kubelet")
	if err := os.WriteFile(source, []byte("kubelet-binary"), 0o644); err != nil {
		t.Fatalf("write source artifact: %v", err)
	}
	target := filepath.Join(dir, "usr", "bin", "kubelet")

	origDetect := installArtifactsDetectHostFacts
	t.Cleanup(func() { installArtifactsDetectHostFacts = origDetect })
	installArtifactsDetectHostFacts = func() map[string]any {
		return map[string]any{"arch": "amd64"}
	}

	spec := map[string]any{
		"artifacts": []any{map[string]any{
			"source": map[string]any{
				"amd64": map[string]any{"path": source},
				"arm64": map[string]any{"path": source},
			},
			"install": map[string]any{"path": target, "mode": "0755"},
		}},
	}

	if err := runInstallArtifacts(context.Background(), spec); err != nil {
		t.Fatalf("runInstallArtifacts failed: %v", err)
	}

	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read installed artifact: %v", err)
	}
	if string(raw) != "kubelet-binary" {
		t.Fatalf("unexpected installed artifact content: %q", string(raw))
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat installed artifact: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("expected mode 0755, got %o", info.Mode().Perm())
	}
}

func TestInstallArtifactsStep_ExtractsTarGzArtifact(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "crictl.tar.gz")
	if err := writeTarGzArchiveForTest(archivePath, map[string]string{"crictl": "binary"}); err != nil {
		t.Fatalf("write test archive: %v", err)
	}
	destination := filepath.Join(dir, "usr", "bin")

	origDetect := installArtifactsDetectHostFacts
	t.Cleanup(func() { installArtifactsDetectHostFacts = origDetect })
	installArtifactsDetectHostFacts = func() map[string]any {
		return map[string]any{"arch": "amd64"}
	}

	spec := map[string]any{
		"artifacts": []any{map[string]any{
			"source": map[string]any{
				"amd64": map[string]any{"path": archivePath},
				"arm64": map[string]any{"path": archivePath},
			},
			"extract": map[string]any{"destination": destination, "include": []any{"crictl"}, "mode": "0755"},
		}},
	}

	if err := runInstallArtifacts(context.Background(), spec); err != nil {
		t.Fatalf("runInstallArtifacts failed: %v", err)
	}

	target := filepath.Join(destination, "crictl")
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read extracted file: %v", err)
	}
	if string(raw) != "binary" {
		t.Fatalf("unexpected extracted content: %q", string(raw))
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat extracted file: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("expected extracted mode 0755, got %o", info.Mode().Perm())
	}
}

func TestInstallArtifactsStep_SelectsSourceByArchitecture(t *testing.T) {
	dir := t.TempDir()
	amd64Source := filepath.Join(dir, "kubeadm-amd64")
	arm64Source := filepath.Join(dir, "kubeadm-arm64")
	if err := os.WriteFile(amd64Source, []byte("amd64-binary"), 0o644); err != nil {
		t.Fatalf("write amd64 source: %v", err)
	}
	if err := os.WriteFile(arm64Source, []byte("arm64-binary"), 0o644); err != nil {
		t.Fatalf("write arm64 source: %v", err)
	}
	target := filepath.Join(dir, "usr", "bin", "kubeadm")

	origDetect := installArtifactsDetectHostFacts
	t.Cleanup(func() { installArtifactsDetectHostFacts = origDetect })

	installArtifactsDetectHostFacts = func() map[string]any {
		return map[string]any{"arch": "amd64"}
	}
	spec := map[string]any{
		"artifacts": []any{map[string]any{
			"source": map[string]any{
				"amd64": map[string]any{"path": amd64Source},
				"arm64": map[string]any{"path": arm64Source},
			},
			"install": map[string]any{"path": target, "mode": "0755"},
		}},
	}
	if err := runInstallArtifacts(context.Background(), spec); err != nil {
		t.Fatalf("runInstallArtifacts amd64 failed: %v", err)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read installed amd64 artifact: %v", err)
	}
	if string(raw) != "amd64-binary" {
		t.Fatalf("expected amd64 artifact, got %q", string(raw))
	}

	installArtifactsDetectHostFacts = func() map[string]any {
		return map[string]any{"arch": "arm64"}
	}
	if err := runInstallArtifacts(context.Background(), spec); err != nil {
		t.Fatalf("runInstallArtifacts arm64 failed: %v", err)
	}
	raw, err = os.ReadFile(target)
	if err != nil {
		t.Fatalf("read installed arm64 artifact: %v", err)
	}
	if string(raw) != "arm64-binary" {
		t.Fatalf("expected arm64 artifact, got %q", string(raw))
	}
}

func TestInstallArtifactsStep_SkipIfPresentExecutable(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "kubectl-source")
	if err := os.WriteFile(source, []byte("new-content"), 0o644); err != nil {
		t.Fatalf("write source artifact: %v", err)
	}
	target := filepath.Join(dir, "usr", "bin", "kubectl")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir target parent: %v", err)
	}
	if err := os.WriteFile(target, []byte("existing-content"), 0o755); err != nil {
		t.Fatalf("write existing executable: %v", err)
	}

	origDetect := installArtifactsDetectHostFacts
	t.Cleanup(func() { installArtifactsDetectHostFacts = origDetect })
	installArtifactsDetectHostFacts = func() map[string]any {
		return map[string]any{"arch": "amd64"}
	}

	spec := map[string]any{
		"artifacts": []any{map[string]any{
			"source": map[string]any{
				"amd64": map[string]any{"path": source},
				"arm64": map[string]any{"path": source},
			},
			"skipIfPresent": map[string]any{"path": target, "executable": true},
			"install":       map[string]any{"path": target, "mode": "0755"},
		}},
	}

	if err := runInstallArtifacts(context.Background(), spec); err != nil {
		t.Fatalf("runInstallArtifacts failed: %v", err)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target artifact: %v", err)
	}
	if string(raw) != "existing-content" {
		t.Fatalf("expected skip to preserve existing target content, got %q", string(raw))
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

func TestSystemdUnitStep(t *testing.T) {
	t.Run("writes unit file with content", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "systemd", "demo.service")
		if err := runSystemdUnit(map[string]any{"path": target, "content": "[Unit]\nDescription=demo"}); err != nil {
			t.Fatalf("runSystemdUnit failed: %v", err)
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
		if err := runSystemdUnit(map[string]any{"path": target, "contentFromTemplate": "[Service]\nExecStart=/usr/bin/true"}); err != nil {
			t.Fatalf("runSystemdUnit failed: %v", err)
		}
		raw, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("read unit file: %v", err)
		}
		if string(raw) != "[Service]\nExecStart=/usr/bin/true\n" {
			t.Fatalf("unexpected unit template content: %q", string(raw))
		}
	})

	t.Run("creates parent directories", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "etc", "systemd", "system", "kubelet.service")
		if err := runSystemdUnit(map[string]any{"path": target, "content": "[Install]"}); err != nil {
			t.Fatalf("runSystemdUnit failed: %v", err)
		}
		if _, err := os.Stat(filepath.Dir(target)); err != nil {
			t.Fatalf("expected parent directory to exist: %v", err)
		}
	})

	t.Run("daemon-reload runs before service operations", func(t *testing.T) {
		dir := t.TempDir()
		binDir := filepath.Join(dir, "bin")
		if err := os.MkdirAll(binDir, 0o755); err != nil {
			t.Fatalf("mkdir bin: %v", err)
		}
		logPath := filepath.Join(dir, "systemctl.log")
		script := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n' \"$*\" >> \"" + logPath + "\"\nif [[ \"${1:-}\" == \"is-active\" ]]; then\n  exit 1\nfi\nif [[ \"${1:-}\" == \"is-enabled\" ]]; then\n  exit 1\nfi\nexit 0\n"
		if err := os.WriteFile(filepath.Join(binDir, "systemctl"), []byte(script), 0o755); err != nil {
			t.Fatalf("write systemctl script: %v", err)
		}
		t.Setenv("PATH", fmt.Sprintf("%s:%s", binDir, os.Getenv("PATH")))

		target := filepath.Join(dir, "kubelet.service")
		spec := map[string]any{
			"path":         target,
			"content":      "[Unit]",
			"daemonReload": true,
			"service": map[string]any{
				"name":  "kubelet",
				"state": "restarted",
			},
		}
		if err := runSystemdUnit(spec); err != nil {
			t.Fatalf("runSystemdUnit failed: %v", err)
		}

		raw, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatalf("read systemctl log: %v", err)
		}
		lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
		if len(lines) < 2 {
			t.Fatalf("expected daemon-reload and restart, got %q", string(raw))
		}
		if lines[0] != "daemon-reload" {
			t.Fatalf("expected daemon-reload first, got %q", lines[0])
		}
		if lines[1] != "restart kubelet" {
			t.Fatalf("expected restart after daemon-reload, got %q", lines[1])
		}
	})

	t.Run("nested service can enable and restart unit", func(t *testing.T) {
		dir := t.TempDir()
		binDir := filepath.Join(dir, "bin")
		if err := os.MkdirAll(binDir, 0o755); err != nil {
			t.Fatalf("mkdir bin: %v", err)
		}
		logPath := filepath.Join(dir, "systemctl.log")
		script := "#!/usr/bin/env bash\nset -euo pipefail\ncmd=\"${1:-}\"\nif [[ \"${cmd}\" == \"is-enabled\" ]]; then\n  exit 1\nfi\nprintf '%s\\n' \"$*\" >> \"" + logPath + "\"\nexit 0\n"
		if err := os.WriteFile(filepath.Join(binDir, "systemctl"), []byte(script), 0o755); err != nil {
			t.Fatalf("write systemctl script: %v", err)
		}
		t.Setenv("PATH", fmt.Sprintf("%s:%s", binDir, os.Getenv("PATH")))

		target := filepath.Join(dir, "containerd.service")
		spec := map[string]any{
			"path":    target,
			"content": "[Unit]",
			"service": map[string]any{
				"name":    "containerd",
				"enabled": true,
				"state":   "restarted",
			},
		}
		if err := runSystemdUnit(spec); err != nil {
			t.Fatalf("runSystemdUnit failed: %v", err)
		}

		raw, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatalf("read systemctl log: %v", err)
		}
		logText := string(raw)
		if !strings.Contains(logText, "enable containerd") {
			t.Fatalf("expected enable call, got %q", logText)
		}
		if !strings.Contains(logText, "restart containerd") {
			t.Fatalf("expected restart call, got %q", logText)
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
		if err := runRepoConfig(spec); err != nil {
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
		if err := runRepoConfig(spec); err != nil {
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
		if err := runRepoConfig(spec); err != nil {
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
		if err := runRepoConfig(spec); err != nil {
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
		if err := runRepoConfig(spec); err != nil {
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
		if err := runRepoConfig(spec); err != nil {
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

	t.Run("refresh cache invokes apt commands", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "repo", "offline.list")
		origRun := repoConfigRunTimedCommand
		t.Cleanup(func() { repoConfigRunTimedCommand = origRun })
		calls := make([]string, 0)
		repoConfigRunTimedCommand = func(name string, args []string, timeout time.Duration) error {
			calls = append(calls, name+" "+strings.Join(args, " "))
			if timeout < time.Second {
				t.Fatalf("unexpected timeout: %s", timeout)
			}
			return nil
		}

		spec := map[string]any{
			"format": "apt",
			"path":   target,
			"refreshCache": map[string]any{
				"enabled": true,
				"clean":   true,
			},
			"repositories": []any{map[string]any{
				"baseurl": "http://repo.local/apt/bookworm",
			}},
		}
		if err := runRepoConfig(spec); err != nil {
			t.Fatalf("runRepoConfig failed: %v", err)
		}
		if len(calls) != 2 || calls[0] != "apt-get clean" || calls[1] != "apt-get update" {
			t.Fatalf("unexpected refresh command sequence: %#v", calls)
		}
	})
}

func TestPackageCacheStep(t *testing.T) {
	t.Run("apt clean only", func(t *testing.T) {
		origRun := packageCacheRunTimedCommand
		t.Cleanup(func() { packageCacheRunTimedCommand = origRun })

		calls := make([]string, 0)
		packageCacheRunTimedCommand = func(name string, args []string, timeout time.Duration) error {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return nil
		}

		spec := map[string]any{"manager": "apt", "clean": true}
		if err := runPackageCache(spec); err != nil {
			t.Fatalf("runPackageCache failed: %v", err)
		}

		if len(calls) != 1 || calls[0] != "apt-get clean" {
			t.Fatalf("unexpected apt clean commands: %#v", calls)
		}
	})

	t.Run("apt clean plus update ordering", func(t *testing.T) {
		origRun := packageCacheRunTimedCommand
		t.Cleanup(func() { packageCacheRunTimedCommand = origRun })

		calls := make([]string, 0)
		packageCacheRunTimedCommand = func(name string, args []string, timeout time.Duration) error {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return nil
		}

		spec := map[string]any{"manager": "apt", "clean": true, "update": true}
		if err := runPackageCache(spec); err != nil {
			t.Fatalf("runPackageCache failed: %v", err)
		}

		if len(calls) != 2 || calls[0] != "apt-get clean" || calls[1] != "apt-get update" {
			t.Fatalf("unexpected apt clean/update sequence: %#v", calls)
		}
	})

	t.Run("dnf clean only", func(t *testing.T) {
		origRun := packageCacheRunTimedCommand
		t.Cleanup(func() { packageCacheRunTimedCommand = origRun })

		calls := make([]string, 0)
		packageCacheRunTimedCommand = func(name string, args []string, timeout time.Duration) error {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return nil
		}

		spec := map[string]any{"manager": "dnf", "clean": true}
		if err := runPackageCache(spec); err != nil {
			t.Fatalf("runPackageCache failed: %v", err)
		}

		if len(calls) != 1 || calls[0] != "dnf clean all" {
			t.Fatalf("unexpected dnf clean commands: %#v", calls)
		}
	})

	t.Run("dnf update uses makecache behavior", func(t *testing.T) {
		origRun := packageCacheRunTimedCommand
		t.Cleanup(func() { packageCacheRunTimedCommand = origRun })

		calls := make([]string, 0)
		packageCacheRunTimedCommand = func(name string, args []string, timeout time.Duration) error {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return nil
		}

		spec := map[string]any{"manager": "dnf", "update": true}
		if err := runPackageCache(spec); err != nil {
			t.Fatalf("runPackageCache failed: %v", err)
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
		origRun := packageCacheRunTimedCommand
		origDetect := repoConfigDetectHostFacts
		t.Cleanup(func() {
			packageCacheRunTimedCommand = origRun
			repoConfigDetectHostFacts = origDetect
		})

		calls := make([]string, 0)
		packageCacheRunTimedCommand = func(name string, args []string, timeout time.Duration) error {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return nil
		}
		repoConfigDetectHostFacts = func() map[string]any {
			return map[string]any{"os": map[string]any{"family": "rhel"}}
		}

		spec := map[string]any{"manager": "auto", "update": true}
		if err := runPackageCache(spec); err != nil {
			t.Fatalf("runPackageCache failed: %v", err)
		}

		if len(calls) != 1 || calls[0] != "dnf makecache -y" {
			t.Fatalf("expected auto/rhel to resolve to dnf makecache, got %#v", calls)
		}
	})
}

func TestContainerdConfigStep(t *testing.T) {
	t.Run("updates existing config.toml fields", func(t *testing.T) {
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
	})

	t.Run("writes single registry hosts file under explicit configPath", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "config.toml")
		configPath := filepath.Join(dir, "certs.d")
		if err := os.WriteFile(target, []byte("version = 2\n"), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		spec := map[string]any{
			"path":       target,
			"configPath": configPath,
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

		if err := runContainerdConfig(context.Background(), spec); err != nil {
			t.Fatalf("runContainerdConfig failed: %v", err)
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

		if err := runContainerdConfig(context.Background(), spec); err != nil {
			t.Fatalf("runContainerdConfig second pass failed: %v", err)
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

		if err := runContainerdConfig(context.Background(), spec); err != nil {
			t.Fatalf("runContainerdConfig failed: %v", err)
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

func TestRun_KubeadmReset(t *testing.T) {
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
					Kind: "KubeadmReset",
					Spec: map[string]any{
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
					Kind: "KubeadmReset",
					Spec: map[string]any{
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
}

func writeTarGzArchiveForTest(path string, files map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	gzWriter := gzip.NewWriter(f)
	defer func() { _ = gzWriter.Close() }()
	tarWriter := tar.NewWriter(gzWriter)
	defer func() { _ = tarWriter.Close() }()

	for name, content := range files {
		normalized := strings.TrimSpace(name)
		if normalized == "" {
			continue
		}
		raw := []byte(content)
		header := &tar.Header{
			Name: normalized,
			Mode: 0o755,
			Size: int64(len(raw)),
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if _, err := io.Copy(tarWriter, bytes.NewReader(raw)); err != nil {
			return err
		}
	}

	return nil
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
