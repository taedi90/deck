package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runWithCapturedStdout(args []string) (string, error) {
	for _, name := range []string{"DECK_SERVER", "DECK_API_TOKEN"} {
		oldValue, hadOldValue := os.LookupEnv(name)
		if err := os.Setenv(name, ""); err != nil {
			return "", err
		}
		defer func(name, value string, hadValue bool) {
			if hadValue {
				_ = os.Setenv(name, value)
			} else {
				_ = os.Unsetenv(name)
			}
		}(name, oldValue, hadOldValue)
	}

	configPath := ""
	if os.Getenv("DECK_SERVER_CONFIG_PATH") == "" {
		configPath = filepath.Join(os.TempDir(), "deck-test-server-config.json")
		oldValue, hadOldValue := os.LookupEnv("DECK_SERVER_CONFIG_PATH")
		if err := os.Setenv("DECK_SERVER_CONFIG_PATH", configPath); err != nil {
			return "", err
		}
		defer func() {
			if hadOldValue {
				_ = os.Setenv("DECK_SERVER_CONFIG_PATH", oldValue)
			} else {
				_ = os.Unsetenv("DECK_SERVER_CONFIG_PATH")
			}
		}()
		_ = os.Remove(configPath)
	}

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stdout = w

	runErr := run(args)
	_ = w.Close()
	os.Stdout = oldStdout

	raw, readErr := io.ReadAll(r)
	_ = r.Close()
	if readErr != nil {
		return "", readErr
	}

	return string(raw), runErr
}

type deckBinaryResult struct {
	stdout   string
	stderr   string
	exitCode int
}

func buildDeckBinary(t *testing.T, name string) string {
	t.Helper()
	binaryPath := filepath.Join(t.TempDir(), name)
	buildCmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/deck")
	buildCmd.Dir = filepath.Join("..", "..")
	if raw, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("build deck binary: %v, output=%s", err, string(raw))
	}
	return binaryPath
}

func runDeckBinary(t *testing.T, binaryPath string, args ...string) deckBinaryResult {
	t.Helper()
	cmd := exec.Command(binaryPath, args...)
	cmd.Dir = filepath.Join("..", "..")
	cmd.Env = os.Environ()
	if os.Getenv("XDG_CONFIG_HOME") == "" {
		cmd.Env = append(cmd.Env, "XDG_CONFIG_HOME="+filepath.Join(t.TempDir(), "config"))
	}
	if os.Getenv("XDG_STATE_HOME") == "" {
		cmd.Env = append(cmd.Env, "XDG_STATE_HOME="+filepath.Join(t.TempDir(), "state"))
	}
	if os.Getenv("XDG_CACHE_HOME") == "" {
		cmd.Env = append(cmd.Env, "XDG_CACHE_HOME="+filepath.Join(t.TempDir(), "cache"))
	}
	if os.Getenv("DECK_SERVER_CONFIG_PATH") == "" {
		cmd.Env = append(cmd.Env, "DECK_SERVER_CONFIG_PATH="+filepath.Join(t.TempDir(), "server.json"))
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := deckBinaryResult{stdout: stdout.String(), stderr: stderr.String()}
	if err == nil {
		return result
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("run deck binary: %v", err)
	}
	result.exitCode = exitErr.ExitCode()
	return result
}

func writeWorkflowYAML(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write workflow yaml: %v", err)
	}
}

func writeInstallTrueWorkflowFixture(t *testing.T) string {
	t.Helper()
	return writeApplyTrueWorkflowFixture(t, "install")
}

func writeApplyTrueWorkflowFixture(t *testing.T, phaseName string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "install-true.yaml")
	content := fmt.Sprintf("role: apply\nversion: v1alpha1\nphases:\n  - name: %s\n    steps:\n      - id: run-true\n        kind: Command\n        spec:\n          command: [\"true\"]\n", phaseName)
	writeWorkflowYAML(t, path, content)
	return path
}

func writeValidateWorkflowFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "validate-workflow.yaml")
	content := "role: apply\nversion: v1alpha1\nphases:\n  - name: install\n    steps:\n      - id: validate-run\n        apiVersion: deck/v1alpha1\n        kind: Command\n        spec:\n          command: [\"true\"]\n"
	writeWorkflowYAML(t, path, content)
	return path
}

func createValidBundleManifest(t *testing.T, bundleRoot string) {
	t.Helper()
	artifactRel := "outputs/files/dummy.txt"
	artifactAbs := filepath.Join(bundleRoot, artifactRel)
	if err := os.MkdirAll(filepath.Dir(artifactAbs), 0o755); err != nil {
		t.Fatalf("mkdir artifact dir: %v", err)
	}
	content := []byte("ok\n")
	if err := os.WriteFile(artifactAbs, content, 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	sum := sha256.Sum256(content)
	manifest := fmt.Sprintf("{\n  \"entries\": [\n    {\"path\": %q, \"sha256\": %q, \"size\": %d}\n  ]\n}\n", artifactRel, hex.EncodeToString(sum[:]), len(content))
	manifestPath := filepath.Join(bundleRoot, ".deck", "manifest.json")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatalf("mkdir manifest dir: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func sliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func writeApplyBundleTarFixture(t *testing.T, archivePath string) {
	t.Helper()
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer closeSilently(f)

	tw := tar.NewWriter(f)
	defer closeSilently(tw)

	entries := []struct {
		name string
		body []byte
		mode int64
	}{
		{name: "bundle/workflows/", mode: 0o755},
		{name: "bundle/workflows/scenarios/", mode: 0o755},
		{name: "bundle/workflows/scenarios/apply.yaml", body: []byte("role: apply\nversion: v1alpha1\nsteps: []\n"), mode: 0o644},
		{name: "bundle/workflows/vars.yaml", body: []byte("{}\n"), mode: 0o644},
	}

	for _, entry := range entries {
		h := &tar.Header{Name: entry.name, Mode: entry.mode}
		if strings.HasSuffix(entry.name, "/") {
			h.Typeflag = tar.TypeDir
			h.Size = 0
		} else {
			h.Typeflag = tar.TypeReg
			h.Size = int64(len(entry.body))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatalf("write tar header %s: %v", entry.name, err)
		}
		if h.Typeflag == tar.TypeReg {
			if _, err := tw.Write(entry.body); err != nil {
				t.Fatalf("write tar body %s: %v", entry.name, err)
			}
		}
	}
}
