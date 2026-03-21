package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyDryRunExitCodeViaBinary(t *testing.T) {
	binaryPath := filepath.Join(t.TempDir(), "deck-test-bin")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/deck")
	buildCmd.Dir = filepath.Join("..", "..")
	buildOut, buildErr := buildCmd.CombinedOutput()
	if buildErr != nil {
		t.Fatalf("build deck binary: %v, output=%s", buildErr, string(buildOut))
	}

	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}

	root := t.TempDir()
	bundle := root
	if err := os.MkdirAll(filepath.Join(bundle, "workflows"), 0o755); err != nil {
		t.Fatalf("mkdir workflows: %v", err)
	}
	workflowPath := filepath.Join(root, "apply.yaml")
	workflowBody := "version: v1alpha1\nphases:\n  - name: install\n    steps:\n      - id: dry-run-step\n        kind: Command\n        spec:\n          command: [\"true\"]\n"
	if err := os.WriteFile(workflowPath, []byte(workflowBody), 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	cmd := exec.Command(binaryPath,
		"apply",
		"--workflow", workflowPath,
		"--dry-run",
		bundle,
	)
	cmd.Dir = filepath.Join("..", "..")
	cmd.Env = append(os.Environ(), "HOME="+home)
	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected zero exit, err=%v output=%s", err, string(raw))
	}
}

func TestCLIContractRoutesErrorsToStderrWithNonZeroExit(t *testing.T) {
	binaryPath := buildDeckBinary(t, "deck-contract-bin")

	tests := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{
			name:       "unknown flag",
			args:       []string{"init", "--template", "multinode"},
			wantStderr: "Error: unknown flag: --template",
		},
		{
			name:       "unknown top level command",
			args:       []string{"unknown"},
			wantStderr: "Error: unknown command \"unknown\" for \"deck\"",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := runDeckBinary(t, binaryPath, tc.args...)
			if res.exitCode == 0 {
				t.Fatalf("expected non-zero exit for %v", tc.args)
			}
			if !strings.Contains(res.stderr, tc.wantStderr) {
				t.Fatalf("expected stderr to include %q, got %q", tc.wantStderr, res.stderr)
			}
		})
	}
}

func TestCLIContractUsesSingleErrorLineWithoutAutoUsage(t *testing.T) {
	binaryPath := buildDeckBinary(t, "deck-contract-cobra-errors-bin")

	tests := []struct {
		name        string
		args        []string
		wantStderr  string
		forbidden   []string
		exactlyOnce []string
	}{
		{
			name:        "unknown command",
			args:        []string{"unknown"},
			wantStderr:  "Error: unknown command \"unknown\" for \"deck\"",
			forbidden:   []string{},
			exactlyOnce: []string{"unknown command \"unknown\" for \"deck\""},
		},
		{
			name:        "unknown flag",
			args:        []string{"init", "--template", "multinode"},
			wantStderr:  "Error: unknown flag: --template",
			forbidden:   []string{},
			exactlyOnce: []string{"unknown flag: --template"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := runDeckBinary(t, binaryPath, tc.args...)
			if res.exitCode == 0 {
				t.Fatalf("expected non-zero exit for %v", tc.args)
			}
			if res.stdout != "" {
				t.Fatalf("expected empty stdout for %v, got %q", tc.args, res.stdout)
			}
			if !strings.Contains(res.stderr, tc.wantStderr) {
				t.Fatalf("expected stderr to include %q, got %q", tc.wantStderr, res.stderr)
			}
			if strings.Contains(res.stderr, "Usage:") {
				t.Fatalf("stderr must not include automatic usage for %v, got %q", tc.args, res.stderr)
			}
			for _, token := range tc.forbidden {
				if strings.Contains(res.stderr, token) {
					t.Fatalf("stderr must not include %q, got %q", token, res.stderr)
				}
			}
			for _, token := range tc.exactlyOnce {
				if strings.Count(res.stderr, token) != 1 {
					t.Fatalf("stderr must include %q exactly once, got %q", token, res.stderr)
				}
			}
		})
	}
}

func TestCLIContractHelpTokenIsNotHijackedFromFlagValues(t *testing.T) {
	t.Setenv("DECK_SERVER_CONFIG_PATH", filepath.Join(t.TempDir(), "server.json"))

	out, err := runWithCapturedStdout([]string{"server", "remote", "set", "http://127.0.0.1:8080/help"})
	if err != nil {
		t.Fatalf("expected server remote set success, got %v", err)
	}
	if !strings.Contains(out, "server remote set: http://127.0.0.1:8080/help") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestCLIContractAllowsCobraCompleteInternalPath(t *testing.T) {
	binaryPath := buildDeckBinary(t, "deck-complete-bin")
	res := runDeckBinary(t, binaryPath, "__complete", "completion", "b")
	if res.exitCode != 0 {
		t.Fatalf("expected zero exit, got %d stderr=%q", res.exitCode, res.stderr)
	}
	if !strings.Contains(res.stdout, "bash") {
		t.Fatalf("expected completion candidate output, got %q", res.stdout)
	}
	if !strings.Contains(res.stdout, ":") {
		t.Fatalf("expected completion directive output, got %q", res.stdout)
	}
	if strings.Contains(res.stderr, "unknown command") {
		t.Fatalf("completion path must not be rejected, stderr=%q", res.stderr)
	}
}

func TestCLIContractUnknownFlagUsesCobraWording(t *testing.T) {
	binaryPath := buildDeckBinary(t, "deck-cobra-flag-bin")
	res := runDeckBinary(t, binaryPath, "server", "up", "--bogus")
	if res.exitCode == 0 {
		t.Fatalf("expected non-zero exit")
	}
	if !strings.Contains(res.stderr, "Error: unknown flag: --bogus") {
		t.Fatalf("unexpected stderr: %q", res.stderr)
	}
}

func TestCLIContractHelpRoutesViaBinary(t *testing.T) {
	binaryPath := buildDeckBinary(t, "deck-help-contract-bin")

	tests := []struct {
		name       string
		args       []string
		wantStdout string
	}{
		{name: "root help flag", args: []string{"--help"}, wantStdout: "deck [command]"},
		{name: "root help includes version", args: []string{"--help"}, wantStdout: "version"},
		{name: "help bundle", args: []string{"help", "bundle"}, wantStdout: "deck bundle [command]"},
		{name: "nested server help", args: []string{"server", "health", "--help"}, wantStdout: "deck server health [flags]"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := runDeckBinary(t, binaryPath, tc.args...)
			if res.exitCode != 0 {
				t.Fatalf("expected zero exit for %v, got %d stderr=%q", tc.args, res.exitCode, res.stderr)
			}
			if !strings.Contains(res.stdout, tc.wantStdout) {
				t.Fatalf("expected stdout to include %q, got %q", tc.wantStdout, res.stdout)
			}
			if res.stderr != "" {
				t.Fatalf("expected empty stderr for %v, got %q", tc.args, res.stderr)
			}
		})
	}
}

func TestCLIContractVersionCommandViaBinary(t *testing.T) {
	binaryPath := buildDeckBinary(t, "deck-version-bin")
	res := runDeckBinary(t, binaryPath, "version")
	if res.exitCode != 0 {
		t.Fatalf("expected zero exit, got %d stderr=%q", res.exitCode, res.stderr)
	}
	if res.stdout != "deck dev\n" {
		t.Fatalf("unexpected stdout: %q", res.stdout)
	}
	if res.stderr != "" {
		t.Fatalf("expected empty stderr, got %q", res.stderr)
	}
}

func TestCLIContractGroupedParentsRejectUnknownSubcommandsViaBinary(t *testing.T) {
	binaryPath := buildDeckBinary(t, "deck-subcmd-bin")

	tests := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{name: "bundle", args: []string{"bundle", "wat"}, wantStderr: "Error: unknown command \"wat\" for \"deck bundle\""},
		{name: "cache", args: []string{"cache", "wat"}, wantStderr: "Error: unknown command \"wat\" for \"deck cache\""},
		{name: "server", args: []string{"server", "wat"}, wantStderr: "Error: unknown command \"wat\" for \"deck server\""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := runDeckBinary(t, binaryPath, tc.args...)
			if res.exitCode == 0 {
				t.Fatalf("expected non-zero exit for %v", tc.args)
			}
			if res.stdout != "" {
				t.Fatalf("expected empty stdout for %v, got %q", tc.args, res.stdout)
			}
			if !strings.Contains(res.stderr, tc.wantStderr) {
				t.Fatalf("expected stderr to include %q, got %q", tc.wantStderr, res.stderr)
			}
			if strings.Contains(res.stderr, "Usage:") {
				t.Fatalf("stderr must not include automatic usage for %v, got %q", tc.args, res.stderr)
			}
		})
	}
}

func TestCLIContractBundleInspectRemovedViaBinary(t *testing.T) {
	binaryPath := buildDeckBinary(t, "deck-positional-bin")
	res := runDeckBinary(t, binaryPath, "bundle", "inspect")
	if res.exitCode == 0 {
		t.Fatalf("expected non-zero exit")
	}
	if !strings.Contains(res.stderr, `unknown command "inspect" for "deck bundle"`) {
		t.Fatalf("unexpected stderr: %q", res.stderr)
	}
}

func TestBundledApplyWorksFromBundleDir(t *testing.T) {
	binaryPath := filepath.Join(t.TempDir(), "deck-prepare-bin")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/deck")
	buildCmd.Dir = filepath.Join("..", "..")
	if raw, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("build deck binary: %v, output=%s", err, string(raw))
	}

	tmpRoot := t.TempDir()
	home := filepath.Join(tmpRoot, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	workflowsDir := filepath.Join(tmpRoot, "workflows")
	if err := os.MkdirAll(filepath.Join(workflowsDir, "scenarios"), 0o755); err != nil {
		t.Fatalf("mkdir workflows: %v", err)
	}
	seedDir := filepath.Join(tmpRoot, "seed", "files")
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		t.Fatalf("mkdir seed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(seedDir, "source.bin"), []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("write seed file: %v", err)
	}
	packBody := fmt.Sprintf(`version: v1alpha1
phases:
  - name: prepare
    steps:
      - id: seed-file
        kind: DownloadFile
        spec:
          source:
            path: files/source.bin
          fetch:
            sources:
              - type: local
                path: %q
`, filepath.Join(tmpRoot, "seed"))
	if err := os.WriteFile(filepath.Join(tmpRoot, "workflows", "prepare.yaml"), []byte(packBody), 0o644); err != nil {
		t.Fatalf("write prepare workflow: %v", err)
	}
	applyLogPath := filepath.Join(tmpRoot, "apply.log")
	applyBody := fmt.Sprintf(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: apply-step
        kind: Command
        spec:
          command: ["sh", "-c", "echo hit >> %s"]
`, strings.ReplaceAll(applyLogPath, "\\", "\\\\"))
	if err := os.WriteFile(filepath.Join(workflowsDir, "scenarios", "apply.yaml"), []byte(applyBody), 0o644); err != nil {
		t.Fatalf("write apply workflow: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowsDir, "vars.yaml"), []byte("kubernetesVersion: v1.30.1\n"), 0o644); err != nil {
		t.Fatalf("write vars workflow: %v", err)
	}

	preparedRoot := filepath.Join(tmpRoot, "outputs")
	prepareCmd := exec.Command(binaryPath, "prepare", "--root", preparedRoot)
	prepareCmd.Dir = tmpRoot
	prepareCmd.Env = append(os.Environ(), "HOME="+home)
	if raw, err := prepareCmd.CombinedOutput(); err != nil {
		t.Fatalf("prepare bundle: %v, output=%s", err, string(raw))
	}

	bundleTar := filepath.Join(tmpRoot, "bundle.tar")
	buildBundleCmd := exec.Command(binaryPath, "bundle", "build", "--root", tmpRoot, "--out", bundleTar)
	buildBundleCmd.Dir = tmpRoot
	buildBundleCmd.Env = append(os.Environ(), "HOME="+home)
	if raw, err := buildBundleCmd.CombinedOutput(); err != nil {
		t.Fatalf("build bundle tar: %v, output=%s", err, string(raw))
	}

	extractCmd := exec.Command("tar", "xf", bundleTar, "-C", tmpRoot)
	if raw, err := extractCmd.CombinedOutput(); err != nil {
		t.Fatalf("extract bundle tar: %v, output=%s", err, string(raw))
	}

	applyCmd1 := exec.Command("./deck", "apply")
	applyCmd1.Dir = tmpRoot
	applyCmd1.Env = append(os.Environ(), "HOME="+home)
	if raw, err := applyCmd1.CombinedOutput(); err != nil {
		t.Fatalf("bundled apply first run: %v, output=%s", err, string(raw))
	}

	applyCmd2 := exec.Command("./deck", "apply")
	applyCmd2.Dir = tmpRoot
	applyCmd2.Env = append(os.Environ(), "HOME="+home)
	if raw, err := applyCmd2.CombinedOutput(); err != nil {
		t.Fatalf("bundled apply second run: %v, output=%s", err, string(raw))
	}

	raw, err := os.ReadFile(applyLogPath)
	if err != nil {
		t.Fatalf("read apply log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected bundled apply to execute once due state skip, got %d (%q)", len(lines), string(raw))
	}
}

func TestRunUnknownCommand(t *testing.T) {
	err := run([]string{"unknown"})
	if err == nil {
		t.Fatalf("expected unknown command error")
	}
	if !strings.Contains(err.Error(), `unknown command "unknown"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}
