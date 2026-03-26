package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Airgap-Castaways/deck/internal/applycli"
	"github.com/Airgap-Castaways/deck/internal/config"
	"github.com/Airgap-Castaways/deck/internal/install"
)

func TestResolveInstallStatePathUsesHomeAndStateKey(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Setenv("HOME", home)

	wf := &config.Workflow{StateKey: "abc123"}
	statePath, err := applycli.ResolveInstallStatePath(wf)
	if err != nil {
		t.Fatalf("resolveInstallStatePath failed: %v", err)
	}

	expected := filepath.Join(home, ".local", "state", "deck", "state", "abc123.json")
	if statePath != expected {
		t.Fatalf("state path mismatch: got %q want %q", statePath, expected)
	}
}

func TestApplyWritesRunRecordUnderXDGStateHome(t *testing.T) {
	stateHome := filepath.Join(t.TempDir(), "state-home")
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("HOME", filepath.Join(t.TempDir(), "home"))

	wfPath := filepath.Join(t.TempDir(), "apply-runlog.yaml")
	content := "version: v1alpha1\nphases:\n  - name: install\n    steps:\n      - id: run-true\n        kind: Command\n        spec:\n          command: [\"true\"]\n"
	if err := os.WriteFile(wfPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}
	bundle := t.TempDir()
	if err := os.MkdirAll(filepath.Join(bundle, "workflows"), 0o755); err != nil {
		t.Fatalf("mkdir bundle workflows: %v", err)
	}
	createValidBundleManifest(t, bundle)

	if _, err := runWithCapturedStdout([]string{"apply", "--workflow", wfPath, bundle}); err != nil {
		t.Fatalf("apply failed: %v", err)
	}

	runsRoot := filepath.Join(stateHome, "deck", "runs")
	entries, err := os.ReadDir(runsRoot)
	if err != nil {
		t.Fatalf("read runs root: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one run directory, got %d", len(entries))
	}
	recordPath := filepath.Join(runsRoot, entries[0].Name(), "record.json")
	raw, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read run record: %v", err)
	}
	var record struct {
		Command string `json:"command"`
		Status  string `json:"status"`
		Steps   []struct {
			StepID string `json:"step_id"`
			Status string `json:"status"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatalf("parse run record: %v", err)
	}
	if record.Command != "apply" || record.Status != "ok" {
		t.Fatalf("unexpected run record: %+v", record)
	}
	if len(record.Steps) == 0 || record.Steps[len(record.Steps)-1].Status != "succeeded" {
		t.Fatalf("expected succeeded step record, got %+v", record.Steps)
	}
}

func TestRunApplyVarFlagLastWins(t *testing.T) {
	wfPath := filepath.Join(t.TempDir(), "apply-vars.yaml")
	content := `version: v1alpha1
phases:
  - name: install
    steps:
      - id: run-with-vars
        kind: Command
        when: vars.run == "yes"
        spec:
          command: ["true"]
`
	if err := os.WriteFile(wfPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}
	bundle := t.TempDir()
	if err := os.MkdirAll(filepath.Join(bundle, "workflows"), 0o755); err != nil {
		t.Fatalf("mkdir bundle workflows: %v", err)
	}

	out, err := runWithCapturedStdout([]string{"apply", "--workflow", wfPath, "--dry-run", "--var", "run=no", "--var", "run=yes", bundle})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !strings.Contains(out, "run-with-vars Command PLAN") {
		t.Fatalf("expected PLAN status, got %q", out)
	}
}

func TestRunApplyPhaseSelectionAndSkip(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Setenv("HOME", home)

	root := t.TempDir()
	bundleRoot := root
	createValidBundleManifest(t, bundleRoot)
	if err := os.MkdirAll(filepath.Join(bundleRoot, "workflows"), 0o755); err != nil {
		t.Fatalf("mkdir workflows: %v", err)
	}

	installLogPath := filepath.Join(root, "install.log")
	postLogPath := filepath.Join(root, "post.log")
	workflowPath := filepath.Join(root, "apply.yaml")
	workflowBody := fmt.Sprintf(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: install-step
        kind: Command
        spec:
          command: ["sh", "-c", "echo install >> %s"]
  - name: post
    steps:
      - id: post-step
        kind: Command
        spec:
          command: ["sh", "-c", "echo post >> %s"]
`, strings.ReplaceAll(installLogPath, "\\", "\\\\"), strings.ReplaceAll(postLogPath, "\\", "\\\\"))
	if err := os.WriteFile(workflowPath, []byte(workflowBody), 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	if _, err := runWithCapturedStdout([]string{"apply", "--workflow", workflowPath, "--phase", "post", bundleRoot}); err != nil {
		t.Fatalf("first apply --phase post failed: %v", err)
	}
	if _, err := runWithCapturedStdout([]string{"apply", "--workflow", workflowPath, "--phase", "post", bundleRoot}); err != nil {
		t.Fatalf("second apply --phase post failed: %v", err)
	}
	dryRunOut, err := runWithCapturedStdout([]string{"apply", "--workflow", workflowPath, "--phase", "post", "--dry-run", bundleRoot})
	if err != nil {
		t.Fatalf("dry-run apply --phase post failed: %v", err)
	}
	if !strings.Contains(dryRunOut, "PHASE=post") {
		t.Fatalf("expected post phase line in dry-run output, got %q", dryRunOut)
	}
	if !strings.Contains(dryRunOut, "SKIP (completed phase)") {
		t.Fatalf("expected completed skip in dry-run output, got %q", dryRunOut)
	}
	if strings.Contains(dryRunOut, "install-step") {
		t.Fatalf("dry-run for phase post must not include install steps, got %q", dryRunOut)
	}

	postRaw, err := os.ReadFile(postLogPath)
	if err != nil {
		t.Fatalf("read post log: %v", err)
	}
	postLines := strings.Split(strings.TrimSpace(string(postRaw)), "\n")
	if len(postLines) != 1 {
		t.Fatalf("expected exactly one post execution, got %d (%q)", len(postLines), string(postRaw))
	}

	installRaw, err := os.ReadFile(installLogPath)
	if err != nil {
		if !os.IsNotExist(err) {
			t.Fatalf("read install log: %v", err)
		}
	} else if strings.TrimSpace(string(installRaw)) != "" {
		t.Fatalf("expected install phase not to execute, got %q", string(installRaw))
	}
}

func TestApplyRemoteWorkflow(t *testing.T) {
	t.Run("vars.yaml 200 changes state key when vars changes", func(t *testing.T) {
		home := filepath.Join(t.TempDir(), "home")
		if err := os.MkdirAll(home, 0o755); err != nil {
			t.Fatalf("mkdir home: %v", err)
		}
		t.Setenv("HOME", home)

		logPath := filepath.Join(t.TempDir(), "remote-vars.log")
		workflowBody := fmt.Sprintf(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: remote-step
        kind: Command
        spec:
          command: ["sh", "-c", "echo hit >> %s"]
`, strings.ReplaceAll(logPath, "\\", "\\\\"))

		var mu sync.Mutex
		varsBody := "mode: alpha\n"
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/workflows/scenarios/apply.yaml":
				_, _ = w.Write([]byte(workflowBody))
			case "/workflows/vars.yaml":
				mu.Lock()
				current := varsBody
				mu.Unlock()
				_, _ = w.Write([]byte(current))
			default:
				http.NotFound(w, r)
			}
		}))
		defer ts.Close()

		workflowURL := ts.URL + "/workflows/scenarios/apply.yaml"
		if _, err := runWithCapturedStdout([]string{"apply", "--workflow", workflowURL}); err != nil {
			t.Fatalf("remote apply(1) failed: %v", err)
		}

		wf1, err := config.Load(context.Background(), workflowURL)
		if err != nil {
			t.Fatalf("load remote workflow(1): %v", err)
		}
		statePath1, err := applycli.ResolveInstallStatePath(wf1)
		if err != nil {
			t.Fatalf("resolve state path(1): %v", err)
		}
		if _, err := os.Stat(statePath1); err != nil {
			t.Fatalf("expected first state file: %v", err)
		}

		mu.Lock()
		varsBody = "mode: beta\n"
		mu.Unlock()

		if _, err := runWithCapturedStdout([]string{"apply", "--workflow", workflowURL}); err != nil {
			t.Fatalf("remote apply(2) failed: %v", err)
		}

		wf2, err := config.Load(context.Background(), workflowURL)
		if err != nil {
			t.Fatalf("load remote workflow(2): %v", err)
		}
		statePath2, err := applycli.ResolveInstallStatePath(wf2)
		if err != nil {
			t.Fatalf("resolve state path(2): %v", err)
		}
		if statePath1 == statePath2 {
			t.Fatalf("expected state path to change when vars.yaml changes")
		}
		if _, err := os.Stat(statePath2); err != nil {
			t.Fatalf("expected second state file: %v", err)
		}

		raw, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatalf("read remote log: %v", err)
		}
		lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
		if len(lines) != 2 {
			t.Fatalf("expected two executions with changed vars, got %d (%q)", len(lines), string(raw))
		}
	})

	t.Run("vars.yaml 404 is non-fatal and rerun skips with same state", func(t *testing.T) {
		home := filepath.Join(t.TempDir(), "home")
		if err := os.MkdirAll(home, 0o755); err != nil {
			t.Fatalf("mkdir home: %v", err)
		}
		t.Setenv("HOME", home)

		logPath := filepath.Join(t.TempDir(), "remote-404.log")
		workflowBody := fmt.Sprintf(`version: v1alpha1
phases:
  - name: install
    steps:
      - id: remote-step
        kind: Command
        spec:
          command: ["sh", "-c", "echo hit >> %s"]
`, strings.ReplaceAll(logPath, "\\", "\\\\"))

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/workflows/scenarios/apply.yaml":
				_, _ = w.Write([]byte(workflowBody))
			default:
				http.NotFound(w, r)
			}
		}))
		defer ts.Close()

		workflowURL := ts.URL + "/workflows/scenarios/apply.yaml"
		ignoredBundleArg := filepath.Join(t.TempDir(), "missing-bundle")
		if _, err := runWithCapturedStdout([]string{"apply", "--workflow", workflowURL, ignoredBundleArg}); err != nil {
			t.Fatalf("remote apply with ignored positional bundle(1) failed: %v", err)
		}
		if _, err := runWithCapturedStdout([]string{"apply", "--workflow", workflowURL, ignoredBundleArg}); err != nil {
			t.Fatalf("remote apply with ignored positional bundle(2) failed: %v", err)
		}

		raw, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatalf("read remote log: %v", err)
		}
		lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
		if len(lines) != 1 {
			t.Fatalf("expected one execution due to state reuse, got %d (%q)", len(lines), string(raw))
		}
	})

	t.Run("remote apply workflow with top-level steps uses implicit default phase", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/workflows/scenarios/apply.yaml":
				_, _ = w.Write([]byte("version: v1alpha1\nsteps:\n  - id: pack-step\n    kind: Command\n    spec:\n      command: [\"true\"]\n"))
			default:
				http.NotFound(w, r)
			}
		}))
		defer ts.Close()

		_, err := runWithCapturedStdout([]string{"apply", "--workflow", ts.URL + "/workflows/scenarios/apply.yaml"})
		if err != nil {
			t.Fatalf("expected success for implicit default phase, got %v", err)
		}
	})
}

func TestPlan(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	wfPath := filepath.Join(t.TempDir(), "apply.yaml")
	writeWorkflowYAML(t, wfPath, "version: v1alpha1\nphases:\n  - name: install\n    steps:\n      - id: step-1\n        apiVersion: deck/v1alpha1\n        kind: Command\n        spec:\n          command: [\"true\"]\n")

	before, err := runWithCapturedStdout([]string{"plan", "--workflow", wfPath})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !strings.Contains(before, "SUMMARY steps=1 run=1 skip=0") {
		t.Fatalf("expected summary in plan output, got %q", before)
	}
	if !strings.Contains(before, "RUN") {
		t.Fatalf("expected RUN in plan output, got %q", before)
	}

	wf, err := config.Load(context.Background(), wfPath)
	if err != nil {
		t.Fatalf("load workflow: %v", err)
	}
	execWf, err := applycli.BuildExecutionWorkflow(wf, "install")
	if err != nil {
		t.Fatalf("build execution workflow: %v", err)
	}
	if err := install.Run(context.Background(), execWf, install.RunOptions{BundleRoot: ""}); err != nil {
		t.Fatalf("install run: %v", err)
	}

	after, err := runWithCapturedStdout([]string{"plan", "--workflow", wfPath})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if before == after {
		t.Fatalf("expected plan output to change after apply run")
	}
	if !strings.Contains(after, "SKIP") {
		t.Fatalf("expected SKIP in plan output after apply run, got %q", after)
	}
	if !strings.Contains(after, "SUMMARY steps=1 run=0 skip=1 skipCompleted=1") {
		t.Fatalf("expected completed summary in plan output, got %q", after)
	}
}

func TestPlanJSONAndVerboseDiagnostics(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	wfPath := filepath.Join(t.TempDir(), "apply-plan.json.yaml")
	writeWorkflowYAML(t, wfPath, "version: v1alpha1\nphases:\n  - name: install\n    steps:\n      - id: guarded\n        apiVersion: deck/v1alpha1\n        kind: Command\n        when: vars.run == \"yes\"\n        retry: 2\n        timeout: 30s\n        spec:\n          command: [\"true\"]\n")

	res := execute([]string{"plan", "--workflow", wfPath, "-o", "json", "--v=2", "--var", "run=yes"})
	if res.err != nil {
		t.Fatalf("expected success, got %v", res.err)
	}
	if !strings.Contains(res.stderr, "deck: plan workflow=") {
		t.Fatalf("expected plan diagnostics on stderr, got %q", res.stderr)
	}
	if !strings.Contains(res.stderr, "deck: plan step=guarded") {
		t.Fatalf("expected verbose step diagnostics on stderr, got %q", res.stderr)
	}
	var payload struct {
		WorkflowPath   string   `json:"workflowPath"`
		SelectedPhase  string   `json:"selectedPhase"`
		StatePath      string   `json:"statePath"`
		RuntimeVarKeys []string `json:"runtimeVarKeys"`
		Summary        struct {
			TotalSteps      int `json:"totalSteps"`
			RunSteps        int `json:"runSteps"`
			SkipSteps       int `json:"skipSteps"`
			CompletedPhases int `json:"completedPhases"`
		} `json:"summary"`
		Steps []struct {
			Phase   string `json:"phase"`
			ID      string `json:"id"`
			Kind    string `json:"kind"`
			Action  string `json:"action"`
			When    string `json:"when"`
			Retry   int    `json:"retry"`
			Timeout string `json:"timeout"`
		} `json:"steps"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &payload); err != nil {
		t.Fatalf("parse plan json: %v stdout=%q", err, res.stdout)
	}
	if payload.WorkflowPath != wfPath {
		t.Fatalf("unexpected workflow path: %q", payload.WorkflowPath)
	}
	if payload.Summary.TotalSteps != 1 || payload.Summary.RunSteps != 1 || payload.Summary.SkipSteps != 0 {
		t.Fatalf("unexpected summary: %+v", payload.Summary)
	}
	if len(payload.Steps) != 1 {
		t.Fatalf("unexpected steps: %+v", payload.Steps)
	}
	step := payload.Steps[0]
	if step.ID != "guarded" || step.Action != "run" || step.When != "vars.run == \"yes\"" || step.Retry != 2 || step.Timeout != "30s" {
		t.Fatalf("unexpected step payload: %+v", step)
	}

	res = execute([]string{"plan", "--workflow", wfPath, "--v=3", "--var", "run=yes"})
	if res.err != nil {
		t.Fatalf("expected success, got %v", res.err)
	}
	for _, want := range []string{"deck: plan workflowVars=run runtimeVars=host completedPhases=0", "deck: plan stepEval step=guarded whenEvaluated=true registerKeys=-"} {
		if !strings.Contains(res.stderr, want) {
			t.Fatalf("expected %q in stderr, got %q", want, res.stderr)
		}
	}
}

func TestApplyAndPlanFresh(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	wfPath := filepath.Join(t.TempDir(), "apply-fresh.yaml")
	logPath := filepath.Join(t.TempDir(), "fresh.log")
	writeWorkflowYAML(t, wfPath, fmt.Sprintf("version: v1alpha1\nphases:\n  - name: install\n    steps:\n      - id: once\n        kind: Command\n        spec:\n          command: [\"sh\", \"-c\", \"echo run >> %s\"]\n", strings.ReplaceAll(logPath, "\\", "\\\\")))

	if _, err := runWithCapturedStdout([]string{"apply", "--workflow", wfPath}); err != nil {
		t.Fatalf("initial apply failed: %v", err)
	}
	planOut, err := runWithCapturedStdout([]string{"plan", "--workflow", wfPath})
	if err != nil {
		t.Fatalf("plan failed: %v", err)
	}
	if !strings.Contains(planOut, "SKIP (completed)") {
		t.Fatalf("expected completed phase skip in plan output, got %q", planOut)
	}
	freshPlan, err := runWithCapturedStdout([]string{"plan", "--workflow", wfPath, "--fresh"})
	if err != nil {
		t.Fatalf("fresh plan failed: %v", err)
	}
	if !strings.Contains(freshPlan, "once Command RUN") {
		t.Fatalf("expected fresh plan to rerun step, got %q", freshPlan)
	}
	if _, err := runWithCapturedStdout([]string{"apply", "--workflow", wfPath, "--fresh"}); err != nil {
		t.Fatalf("fresh apply failed: %v", err)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read fresh log: %v", err)
	}
	if got := strings.Count(strings.TrimSpace(string(raw)), "run"); got != 2 {
		t.Fatalf("expected 2 executions after fresh apply, got %d (%q)", got, string(raw))
	}
}

func TestApplyVerboseDiagnostics(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	wfPath := filepath.Join(t.TempDir(), "apply-verbose.yaml")
	writeWorkflowYAML(t, wfPath, "version: v1alpha1\nphases:\n  - name: install\n    steps:\n      - id: verbose-step\n        kind: Command\n        retry: 1\n        spec:\n          command: [\"true\"]\n")

	res := execute([]string{"apply", "--workflow", wfPath, "--v=2"})
	if res.err != nil {
		t.Fatalf("expected success, got %v", res.err)
	}
	if res.stdout != "apply: ok\n" {
		t.Fatalf("unexpected stdout: %q", res.stdout)
	}
	for _, want := range []string{"deck: apply workflow=", "deck: apply runlog=", "deck: apply step=verbose-step kind=Command phase=install status=started attempt=1", "deck: apply step=verbose-step kind=Command phase=install status=succeeded attempt=1"} {
		if !strings.Contains(res.stderr, want) {
			t.Fatalf("expected %q in stderr, got %q", want, res.stderr)
		}
	}
}

func TestRunApplyPhaseNotFound(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Setenv("HOME", home)

	bundle := t.TempDir()
	createValidBundleManifest(t, bundle)
	if err := os.MkdirAll(filepath.Join(bundle, "workflows"), 0o755); err != nil {
		t.Fatalf("mkdir bundle workflows: %v", err)
	}

	workflowPath := filepath.Join(t.TempDir(), "apply.yaml")
	workflowBody := "version: v1alpha1\nphases:\n  - name: install\n    steps:\n      - id: step-one\n        kind: Command\n        spec:\n          command: [\"true\"]\n"
	if err := os.WriteFile(workflowPath, []byte(workflowBody), 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	_, err := runWithCapturedStdout([]string{"apply", "--workflow", workflowPath, "--phase", "post", bundle})
	if err == nil {
		t.Fatalf("expected phase not found error")
	}
	if !strings.Contains(err.Error(), "post phase not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveApplyBundleRootPrecedence(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Setenv("HOME", home)

	root := t.TempDir()
	originalCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir root: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalCWD) })

	positionalBundle := filepath.Join(root, "positional")
	if err := os.MkdirAll(filepath.Join(positionalBundle, "workflows"), 0o755); err != nil {
		t.Fatalf("mkdir positional workflows: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(root, "workflows"), 0o755); err != nil {
		t.Fatalf("mkdir cwd workflows: %v", err)
	}

	archivePath := filepath.Join(root, "bundle.tar")
	writeApplyBundleTarFixture(t, archivePath)

	resolved, err := applycli.ResolveBundleRoot(positionalBundle)
	if err != nil {
		t.Fatalf("resolve positional bundle: %v", err)
	}
	if resolved != positionalBundle {
		t.Fatalf("expected positional bundle, got %s", resolved)
	}

	resolved, err = applycli.ResolveBundleRoot("")
	if err != nil {
		t.Fatalf("resolve default bundle candidate: %v", err)
	}
	resolvedSlash := filepath.ToSlash(resolved)
	if !strings.Contains(resolvedSlash, "/.cache/deck/extract/") || !strings.HasSuffix(resolvedSlash, "/bundle") {
		t.Fatalf("expected extracted bundle root, got %s", resolved)
	}

	if err := os.Remove(archivePath); err != nil {
		t.Fatalf("remove bundle.tar: %v", err)
	}
	resolved, err = applycli.ResolveBundleRoot("")
	if err != nil {
		t.Fatalf("resolve cwd candidate: %v", err)
	}
	rootResolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("eval symlinks on root: %v", err)
	}
	if resolved != rootResolved {
		t.Fatalf("expected cwd bundle root, got %s", resolved)
	}
}
