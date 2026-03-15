package test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/taedi90/deck/internal/config"
)

func TestWorkflowIntegrationBootstrap(t *testing.T) {
	root := projectRoot(t)
	workflowPath := filepath.Join(root, "test", "workflows", "scenarios", "control-plane-bootstrap.yaml")

	wf, err := config.LoadWithOptions(context.Background(), workflowPath, config.LoadOptions{VarOverrides: map[string]any{"clusterName": "bootstrap-cli"}})
	if err != nil {
		t.Fatalf("load bootstrap workflow: %v", err)
	}

	if got := wf.Vars["sharedFlavor"]; got != "k8s-shared" {
		t.Fatalf("expected shared var from import, got %v", got)
	}
	if got := wf.Vars["clusterName"]; got != "bootstrap-cli" {
		t.Fatalf("expected CLI var precedence, got %v", got)
	}
	if got := len(wf.Phases); got != 1 {
		t.Fatalf("expected 1 phase, got %d", got)
	}
	steps := wf.Phases[0].Steps
	prereq := stepIndexByID(steps, "prep-disable-swap")
	reset := stepIndexByID(steps, "bootstrap-reset-preflight")
	init := stepIndexByID(steps, "bootstrap-init")
	publish := stepIndexByID(steps, "bootstrap-publish-join")
	report := stepIndexByID(steps, "bootstrap-report")
	if prereq == -1 || reset == -1 || init == -1 || publish == -1 || report == -1 {
		t.Fatalf("missing expected merged steps: prereq=%d reset=%d init=%d publish=%d report=%d", prereq, reset, init, publish, report)
	}
	if prereq >= reset || reset >= init || init >= publish || publish >= report {
		t.Fatalf("unexpected merged step order: prereq=%d reset=%d init=%d publish=%d report=%d", prereq, reset, init, publish, report)
	}

	out := runWorkflowApplyDryRun(t, root, workflowPath)
	requireDryRunOutput(t, out,
		"PHASE=install",
		"prep-disable-swap Swap PLAN",
		"bootstrap-reset-preflight Kubeadm PLAN",
		"bootstrap-init Kubeadm PLAN",
		"bootstrap-report Command PLAN",
	)
}

func TestWorkflowIntegrationWorkerJoin(t *testing.T) {
	root := projectRoot(t)
	workflowPath := filepath.Join(root, "test", "workflows", "scenarios", "worker-join.yaml")

	wf, err := config.LoadWithOptions(context.Background(), workflowPath, config.LoadOptions{VarOverrides: map[string]any{"joinFile": "/tmp/join-cli.txt"}})
	if err != nil {
		t.Fatalf("load worker join workflow: %v", err)
	}

	if got := wf.Vars["joinFile"]; got != "/tmp/join-cli.txt" {
		t.Fatalf("expected CLI joinFile override, got %v", got)
	}
	if got := wf.Vars["joinSource"]; got != "cluster-file" {
		t.Fatalf("expected scenario var from varImports, got %v", got)
	}

	out := runWorkflowApplyDryRun(t, root, workflowPath)
	requireDryRunOutput(t, out,
		"PHASE=install",
		"prep-disable-swap Swap PLAN",
		"fetch-join-file File PLAN",
		"join-worker Kubeadm PLAN",
	)
}

func TestWorkflowIntegrationNodeReset(t *testing.T) {
	root := projectRoot(t)
	workflowPath := filepath.Join(root, "test", "workflows", "scenarios", "node-reset.yaml")

	wf, err := config.Load(context.Background(), workflowPath)
	if err != nil {
		t.Fatalf("load node-reset workflow: %v", err)
	}

	if got := wf.Vars["allowDestructive"]; got != "false" {
		t.Fatalf("expected non-destructive default from varImports, got %v", got)
	}

	out := runWorkflowApplyDryRun(t, root, workflowPath)
	requireDryRunOutput(t, out,
		"PHASE=install",
		"prep-disable-swap Swap PLAN",
		"reset-node Kubeadm SKIP",
		"reset-runtime-ready Command PLAN",
		"reset-state-report Command PLAN",
		"reset-summary Command PLAN",
	)
}

func TestWorkflowIntegrationRejectsBrokenImports(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "broken.yaml")
	content := "role: apply\nversion: v1alpha1\nimports:\n  - missing/import.yaml\nphases:\n  - name: install\n    steps: []\n"
	if err := os.WriteFile(workflowPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write broken workflow: %v", err)
	}
	_, err := config.Load(context.Background(), workflowPath)
	if err == nil {
		t.Fatal("expected broken import to fail load")
	}
	if !strings.Contains(err.Error(), "missing") && !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected missing import error, got %v", err)
	}
}

func TestWorkflowIntegrationRejectsMissingJoinPublish(t *testing.T) {
	root := projectRoot(t)
	workflowPath := writeExecutableNegativeWorkflow(t, root, "join-publish-fail.yaml", `role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: bootstrap-init
        apiVersion: deck/v1alpha1
        kind: Command
        spec:
          command: ["bash", "-lc", "true"]
      - id: bootstrap-publish-join
        apiVersion: deck/v1alpha1
        kind: File
        spec:
          sourcePath: /tmp/nonexistent-join.txt
          destinationPath: /tmp/published-join.txt
      - id: bootstrap-report
        apiVersion: deck/v1alpha1
        kind: Command
        spec:
          command: ["bash", "-lc", "test -f /tmp/published-join.txt"]
`)
	err := runWorkflowApplyExpectError(t, root, workflowPath)
	if !strings.Contains(err, "bootstrap-publish-join") && !strings.Contains(err, "File") {
		t.Fatalf("expected join publish failure, got %s", err)
	}
}

func TestWorkflowIntegrationRejectsMissingJoinFetch(t *testing.T) {
	root := projectRoot(t)
	workflowPath := writeExecutableNegativeWorkflow(t, root, "join-fetch-fail.yaml", `role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: fetch-join-file
        apiVersion: deck/v1alpha1
        kind: File
        spec:
          source:
            url: http://127.0.0.1:9/join.txt
          output:
            path: /tmp/deck/join.txt
      - id: join-worker
        apiVersion: deck/v1alpha1
        kind: Command
        spec:
          command: ["bash", "-lc", "test -s /tmp/deck/join.txt"]
`)
	err := runWorkflowApplyExpectError(t, root, workflowPath)
	if !strings.Contains(err, "fetch-join-file") && !strings.Contains(err, "DownloadFile") {
		t.Fatalf("expected join fetch failure, got %s", err)
	}
}

func TestWorkflowIntegrationRejectsUnhealthyResetRuntime(t *testing.T) {
	root := projectRoot(t)
	workflowPath := writeExecutableNegativeWorkflow(t, root, "reset-runtime-fail.yaml", `role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: reset-runtime-ready
        apiVersion: deck/v1alpha1
        kind: Command
        spec:
          command: ["bash", "-lc", "echo runtime unhealthy >&2; exit 1"]
      - id: reset-state-report
        apiVersion: deck/v1alpha1
        kind: Command
        spec:
          command: ["bash", "-lc", "echo should-not-run"]
`)
	err := runWorkflowApplyExpectError(t, root, workflowPath)
	if !strings.Contains(err, "reset-runtime-ready") && !strings.Contains(err, "runtime unhealthy") {
		t.Fatalf("expected reset runtime failure, got %s", err)
	}
}

func writeExecutableNegativeWorkflow(t *testing.T, root, name, content string) string {
	t.Helper()
	workflowPath := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(workflowPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write executable negative workflow: %v", err)
	}
	return workflowPath
}

func runWorkflowApplyExpectError(t *testing.T, repoRoot, workflowPath string, vars ...string) string {
	t.Helper()
	bundleRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(bundleRoot, ".deck"), 0o755); err != nil {
		t.Fatalf("mkdir bundle manifest dir: %v", err)
	}
	manifest := `{"entries":[{"path":"files/dummy","sha256":"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855","size":0}]}` + "\n"
	if err := os.WriteFile(filepath.Join(bundleRoot, ".deck", "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write bundle manifest: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(bundleRoot, "files"), 0o755); err != nil {
		t.Fatalf("mkdir bundle files dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleRoot, "files", "dummy"), []byte{}, 0o644); err != nil {
		t.Fatalf("write dummy bundle file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(bundleRoot, "workflows"), 0o755); err != nil {
		t.Fatalf("mkdir bundle workflows: %v", err)
	}
	args := []string{"run", "./cmd/deck", "apply", "--file", workflowPath, "--phase", "install"}
	for _, item := range vars {
		args = append(args, "--var", item)
	}
	args = append(args, bundleRoot)
	cmd := exec.Command("go", args...)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected workflow apply to fail\noutput:\n%s", string(out))
	}
	return string(out)
}

func requireDryRunOutput(t *testing.T, output string, expected ...string) {
	t.Helper()
	for _, item := range expected {
		if !strings.Contains(output, item) {
			t.Fatalf("expected dry-run output to include %q", item)
		}
	}
}

func stepIndexByID(steps []config.Step, id string) int {
	for i := range steps {
		if steps[i].ID == id {
			return i
		}
	}
	return -1
}

func runWorkflowApplyDryRun(t *testing.T, repoRoot, workflowPath string, vars ...string) string {
	t.Helper()

	bundleRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(bundleRoot, "workflows"), 0o755); err != nil {
		t.Fatalf("mkdir bundle workflows: %v", err)
	}

	args := []string{"run", "./cmd/deck", "apply", "--file", workflowPath, "--dry-run"}
	for _, item := range vars {
		args = append(args, "--var", item)
	}
	args = append(args, bundleRoot)

	cmd := exec.Command("go", args...)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run dry-run failed: %v\noutput:\n%s", err, string(out))
	}
	return string(out)
}
