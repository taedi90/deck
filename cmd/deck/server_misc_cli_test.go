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
	"testing"
	"time"
)

func TestHealth(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Fatalf("unexpected method: %s", r.Method)
			}
			if r.URL.Path != "/healthz" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		out, err := runWithCapturedStdout([]string{"server", "health", "--server", srv.URL})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		expected := fmt.Sprintf("health: ok (%s)\n", srv.URL)
		if out != expected {
			t.Fatalf("unexpected output\nwant: %q\ngot : %q", expected, out)
		}
	})

	t.Run("json output", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/healthz" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		res := execute([]string{"server", "health", "--server", srv.URL, "-o", "json", "--v=1"})
		if res.err != nil {
			t.Fatalf("expected success, got %v", res.err)
		}
		if !strings.Contains(res.stderr, "deck: server health server=") {
			t.Fatalf("expected diagnostics on stderr, got %q", res.stderr)
		}
		var payload struct {
			Status     string `json:"status"`
			Server     string `json:"server"`
			HealthURL  string `json:"healthUrl"`
			HTTPStatus int    `json:"httpStatus"`
		}
		if err := json.Unmarshal([]byte(res.stdout), &payload); err != nil {
			t.Fatalf("parse health json: %v stdout=%q", err, res.stdout)
		}
		if payload.Status != "ok" || payload.Server != srv.URL || payload.HealthURL != srv.URL+"/healthz" || payload.HTTPStatus != http.StatusOK {
			t.Fatalf("unexpected payload: %+v", payload)
		}
		res = execute([]string{"server", "health", "--server", srv.URL, "-o", "json", "--v=2"})
		if res.err != nil {
			t.Fatalf("expected success, got %v", res.err)
		}
		if !strings.Contains(res.stderr, "deck: server health url=") || !strings.Contains(res.stderr, "deck: server health httpStatus=200") {
			t.Fatalf("expected v2 diagnostics on stderr, got %q", res.stderr)
		}
	})

	t.Run("non-200 fails", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/healthz" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer srv.Close()

		_, err := runWithCapturedStdout([]string{"server", "health", "--server", srv.URL})
		if err == nil {
			t.Fatalf("expected error")
		}
		if !strings.Contains(err.Error(), "unexpected status") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("requires explicit --server when omitted", func(t *testing.T) {
		_, err := runWithCapturedStdout([]string{"server", "health"})
		if err == nil {
			t.Fatalf("expected error when --server omitted")
		}
		if !strings.Contains(err.Error(), "--server is required") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects positional args", func(t *testing.T) {
		_, err := runWithCapturedStdout([]string{"server", "health", "extra", "--server", "http://127.0.0.1:8080"})
		if err == nil {
			t.Fatalf("expected arg validation error")
		}
		if !strings.Contains(err.Error(), `unknown command "extra" for "deck server health"`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestServerRemoteCommands(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "server.json")
	t.Setenv("DECK_SERVER_CONFIG_PATH", configPath)

	out, err := runWithCapturedStdout([]string{"server", "remote", "show"})
	if err != nil {
		t.Fatalf("server remote show failed: %v", err)
	}
	if out != "remote=\norigin=none\n" {
		t.Fatalf("unexpected empty remote show output: %q", out)
	}

	out, err = runWithCapturedStdout([]string{"server", "remote", "set", "http://127.0.0.1:8080/"})
	if err != nil {
		t.Fatalf("server remote set failed: %v", err)
	}
	if out != "server remote set: http://127.0.0.1:8080\n" {
		t.Fatalf("unexpected server remote set output: %q", out)
	}

	out, err = runWithCapturedStdout([]string{"server", "remote", "show"})
	if err != nil {
		t.Fatalf("server remote show after set failed: %v", err)
	}
	if out != "remote=http://127.0.0.1:8080\norigin=config\n" {
		t.Fatalf("unexpected saved remote show output: %q", out)
	}

	out, err = runWithCapturedStdout([]string{"server", "remote", "unset"})
	if err != nil {
		t.Fatalf("server remote unset failed: %v", err)
	}
	if out != "server remote cleared\n" {
		t.Fatalf("unexpected server remote unset output: %q", out)
	}
	if _, statErr := os.Stat(configPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected config file removal, got %v", statErr)
	}

	res := execute([]string{"server", "remote", "set", "http://127.0.0.1:9090", "--v=1"})
	if res.err != nil {
		t.Fatalf("server remote verbose set failed: %v", res.err)
	}
	if !strings.Contains(res.stderr, "deck: server remote set url=http://127.0.0.1:9090 config=") {
		t.Fatalf("unexpected verbose set stderr: %q", res.stderr)
	}
	res = execute([]string{"server", "remote", "show", "--v=1"})
	if res.err != nil {
		t.Fatalf("server remote verbose show failed: %v", res.err)
	}
	if !strings.Contains(res.stderr, "deck: server remote show config=") || !strings.Contains(res.stderr, "origin=config") {
		t.Fatalf("unexpected verbose show stderr: %q", res.stderr)
	}
	res = execute([]string{"server", "remote", "unset", "--v=1"})
	if res.err != nil {
		t.Fatalf("server remote verbose unset failed: %v", res.err)
	}
	if !strings.Contains(res.stderr, "deck: server remote unset config=") {
		t.Fatalf("unexpected verbose unset stderr: %q", res.stderr)
	}
}

func TestHealthUsesSavedDefaultServer(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "server.json")
	t.Setenv("DECK_SERVER_CONFIG_PATH", configPath)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if _, err := runWithCapturedStdout([]string{"server", "remote", "set", srv.URL}); err != nil {
		t.Fatalf("server remote set failed: %v", err)
	}

	out, err := runWithCapturedStdout([]string{"server", "health"})
	if err != nil {
		t.Fatalf("server health with saved default failed: %v", err)
	}
	expected := fmt.Sprintf("health: ok (%s)\n", srv.URL)
	if out != expected {
		t.Fatalf("unexpected output\nwant: %q\ngot : %q", expected, out)
	}
}

func TestMigratedLeafHelpContracts(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"help", "list"}, want: "deck list [flags]"},
		{args: []string{"help", "lint"}, want: "deck lint [scenario] [flags]"},
		{args: []string{"help", "server", "health"}, want: "deck server health [flags]"},
	}

	for _, tc := range tests {
		out, err := runWithCapturedStdout(tc.args)
		if err != nil {
			t.Fatalf("expected help success for %v, got %v", tc.args, err)
		}
		if !strings.Contains(out, tc.want) {
			t.Fatalf("expected %q in output for %v, got %q", tc.want, tc.args, out)
		}
	}
}

func TestLogs(t *testing.T) {
	t.Run("file json output", func(t *testing.T) {
		root := t.TempDir()
		logDir := filepath.Join(root, ".deck", "logs")
		if err := os.MkdirAll(logDir, 0o755); err != nil {
			t.Fatalf("mkdir log dir: %v", err)
		}
		logPath := filepath.Join(logDir, "server-audit.log")
		line := `{"ts":"2026-03-05T12:01:00Z","schema_version":1,"source":"server","event_type":"http_request","level":"info","message":"current","job_id":"current"}` + "\n"
		if err := os.WriteFile(logPath, []byte(line), 0o644); err != nil {
			t.Fatalf("write log: %v", err)
		}

		out, err := runWithCapturedStdout([]string{"server", "logs", "--root", root, "--source", "file", "-o", "json"})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if !strings.Contains(out, `"job_id":"current"`) {
			t.Fatalf("expected log entry in output, got %q", out)
		}
	})

	t.Run("file json keeps diagnostics on stderr", func(t *testing.T) {
		root := t.TempDir()
		logDir := filepath.Join(root, ".deck", "logs")
		if err := os.MkdirAll(logDir, 0o755); err != nil {
			t.Fatalf("mkdir log dir: %v", err)
		}
		logPath := filepath.Join(logDir, "server-audit.log")
		line := `{"ts":"2026-03-05T12:01:00Z","schema_version":1,"source":"server","event_type":"http_request","level":"info","message":"current","job_id":"current"}` + "\n"
		if err := os.WriteFile(logPath, []byte(line), 0o644); err != nil {
			t.Fatalf("write log: %v", err)
		}

		res := execute([]string{"server", "logs", "--root", root, "--source", "file", "-o", "json", "--v=1"})
		if res.err != nil {
			t.Fatalf("expected success, got %v", res.err)
		}
		if !strings.Contains(res.stderr, "deck: server logs file=") || !strings.Contains(res.stderr, "deck: server logs records=1") {
			t.Fatalf("expected diagnostics on stderr, got %q", res.stderr)
		}
		var records []map[string]any
		if err := json.Unmarshal([]byte(res.stdout), &records); err != nil {
			t.Fatalf("parse logs json: %v stdout=%q", err, res.stdout)
		}
		if len(records) != 1 || records[0]["job_id"] != "current" {
			t.Fatalf("unexpected records: %+v", records)
		}
	})

	t.Run("source all reports skipped server at v2", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("HOME", t.TempDir())
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
		t.Setenv("DECK_SERVER_CONFIG_PATH", filepath.Join(t.TempDir(), "server.json"))
		t.Setenv("DECK_SERVER_URL", "")
		if err := os.MkdirAll(filepath.Join(root, "workflows", "scenarios"), 0o755); err != nil {
			t.Fatalf("mkdir scenarios: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "workflows", "scenarios", "apply.yaml"), []byte("version: v1alpha1\nsteps: []\n"), 0o644); err != nil {
			t.Fatalf("write scenario: %v", err)
		}
		oldWD, err := os.Getwd()
		if err != nil {
			t.Fatalf("getwd: %v", err)
		}
		if err := os.Chdir(root); err != nil {
			t.Fatalf("chdir: %v", err)
		}
		defer func() { _ = os.Chdir(oldWD) }()

		res := execute([]string{"list", "--source", "all", "-o", "json", "--v=2"})
		if res.err != nil {
			t.Fatalf("expected success, got %v", res.err)
		}
		if !strings.Contains(res.stderr, "deck: list server skipped reason=no-remote") {
			t.Fatalf("expected skipped server diagnostic, got %q", res.stderr)
		}
	})

	t.Run("journal missing suggests one command", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		_, err := runWithCapturedStdout([]string{"server", "logs", "--source", "journal", "--unit", "deck-server.service"})
		if err == nil {
			t.Fatalf("expected error")
		}
		msg := err.Error()
		if strings.Count(msg, "suggestion:") != 1 {
			t.Fatalf("expected exactly one suggestion, got %q", msg)
		}
		if !strings.Contains(msg, "suggestion: sudo journalctl -u deck-server.service --no-pager -n 50") {
			t.Fatalf("unexpected suggestion: %q", msg)
		}
	})
}

func TestCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cacheRoot := filepath.Join(home, ".cache", "deck")
	packagesDir := filepath.Join(cacheRoot, "packages")
	stateDir := filepath.Join(cacheRoot, "state")
	if err := os.MkdirAll(packagesDir, 0o755); err != nil {
		t.Fatalf("mkdir packages dir: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packagesDir, "p.deb"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write package file: %v", err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(packagesDir, old, old); err != nil {
		t.Fatalf("chtimes packages dir: %v", err)
	}

	t.Run("list json", func(t *testing.T) {
		out, err := runWithCapturedStdout([]string{"cache", "list", "-o", "json"})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		var entries []struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal([]byte(out), &entries); err != nil {
			t.Fatalf("decode json: %v", err)
		}
		found := false
		for _, e := range entries {
			if e.Path == "packages/p.deb" {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected packages/p.deb in output, got %q", out)
		}
	})

	t.Run("list json keeps diagnostics on stderr", func(t *testing.T) {
		res := execute([]string{"cache", "list", "-o", "json", "--v=1"})
		if res.err != nil {
			t.Fatalf("expected success, got %v", res.err)
		}
		if !strings.Contains(res.stderr, "deck: cache list root=") || !strings.Contains(res.stderr, "deck: cache list entries=") {
			t.Fatalf("expected diagnostics on stderr, got %q", res.stderr)
		}
		var entries []struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal([]byte(res.stdout), &entries); err != nil {
			t.Fatalf("decode json: %v stdout=%q", err, res.stdout)
		}
		found := false
		for _, e := range entries {
			if e.Path == "packages/p.deb" {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected packages/p.deb in output, got %q", res.stdout)
		}
	})

	t.Run("clean dry-run older-than", func(t *testing.T) {
		out, err := runWithCapturedStdout([]string{"cache", "clean", "--older-than", "1h", "--dry-run"})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if !strings.Contains(out, packagesDir) {
			t.Fatalf("expected packages dir in plan, got %q", out)
		}
		if strings.Contains(out, stateDir) {
			t.Fatalf("expected state dir excluded from plan, got %q", out)
		}
	})

	t.Run("clean dry-run diagnostics", func(t *testing.T) {
		res := execute([]string{"cache", "clean", "--older-than", "1h", "--dry-run", "--v=2"})
		if res.err != nil {
			t.Fatalf("expected success, got %v", res.err)
		}
		if !strings.Contains(res.stdout, packagesDir) {
			t.Fatalf("expected packages dir in plan, got %q", res.stdout)
		}
		for _, want := range []string{"deck: cache clean root=", "deck: cache clean matches=1", "deck: cache clean path="} {
			if !strings.Contains(res.stderr, want) {
				t.Fatalf("expected %q in stderr, got %q", want, res.stderr)
			}
		}
	})
}

func TestRunServerAuditRotationFlagValidation(t *testing.T) {
	err := executeServe(context.Background(), "./bundle", ":8080", 0, 10, "", "", false)
	if err == nil || !strings.Contains(err.Error(), "--audit-max-size-mb must be > 0") {
		t.Fatalf("expected audit max size validation error, got %v", err)
	}

	err = executeServe(context.Background(), "./bundle", ":8080", 50, 0, "", "", false)
	if err == nil || !strings.Contains(err.Error(), "--audit-max-files must be > 0") {
		t.Fatalf("expected audit max files validation error, got %v", err)
	}
}

func TestRunLegacyTopLevelCommandsAreRemoved(t *testing.T) {
	for _, cmd := range []string{"run", "resume", "diagnose", "agent", "workflow", "control", "strategy", "ManageService", "serve", "health", "logs"} {
		t.Run(cmd, func(t *testing.T) {
			err := run([]string{cmd})
			if err == nil {
				t.Fatalf("expected unknown command error")
			}
			msg := err.Error()
			want := fmt.Sprintf("unknown command %q for %q", cmd, "deck")
			if !strings.Contains(msg, want) {
				t.Fatalf("unexpected error\nwant: %q\ngot : %q", want, msg)
			}
		})
	}
}

func TestLegacyManageServiceSurfaceRemoved(t *testing.T) {
	err := run([]string{"ManageService"})
	if err == nil {
		t.Fatalf("expected unknown command error")
	}
	if !strings.Contains(err.Error(), `unknown command "ManageService" for "deck"`) {
		t.Fatalf("unexpected error: %q", err.Error())
	}
}

func TestServerRemoteUsage(t *testing.T) {
	out, err := runWithCapturedStdout([]string{"server", "remote"})
	if err != nil {
		t.Fatalf("expected help output, got %v", err)
	}
	if !strings.Contains(out, "deck server remote [command]") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestSourceCommandRemoved(t *testing.T) {
	err := run([]string{"source"})
	if err == nil {
		t.Fatalf("expected unknown command error")
	}
	if !strings.Contains(err.Error(), `unknown command "source" for "deck"`) {
		t.Fatalf("unexpected error: %q", err.Error())
	}
}

func TestRunWorkflowLintAndLegacyValidateMigration(t *testing.T) {
	wf := writeValidateWorkflowFixture(t)

	t.Run("lint with -f", func(t *testing.T) {
		out, err := runWithCapturedStdout([]string{"lint", "-f", wf})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if out != fmt.Sprintf("lint: ok (%s)\nSUMMARY mode=file workflows=1 warnings=1 errors=0 supportedVersion=v1alpha1 modes=prepare,apply topLevelModes=phases,steps\n", wf) {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("lint with --file", func(t *testing.T) {
		out, err := runWithCapturedStdout([]string{"lint", "--file", wf})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if out != fmt.Sprintf("lint: ok (%s)\nSUMMARY mode=file workflows=1 warnings=1 errors=0 supportedVersion=v1alpha1 modes=prepare,apply topLevelModes=phases,steps\n", wf) {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("lint json output", func(t *testing.T) {
		res := execute([]string{"lint", "--file", wf, "-o", "json"})
		if res.err != nil {
			t.Fatalf("expected success, got %v", res.err)
		}
		var payload struct {
			Status     string   `json:"status"`
			Mode       string   `json:"mode"`
			Entrypoint string   `json:"entrypoint"`
			Workflows  []string `json:"workflows"`
			Summary    struct {
				WorkflowCount int `json:"workflowCount"`
				WarningCount  int `json:"warningCount"`
				ErrorCount    int `json:"errorCount"`
			} `json:"summary"`
			Contracts struct {
				SupportedVersion string   `json:"supportedVersion"`
				SupportedModes   []string `json:"supportedModes"`
				TopLevelModes    []string `json:"topLevelModes"`
				ImportRule       string   `json:"importRule"`
				InvariantNotes   []string `json:"invariantNotes"`
			} `json:"contracts"`
			Findings []any `json:"findings"`
		}
		if err := json.Unmarshal([]byte(res.stdout), &payload); err != nil {
			t.Fatalf("parse lint json: %v stdout=%q", err, res.stdout)
		}
		if payload.Status != "ok" || payload.Mode != "file" || payload.Entrypoint != wf || payload.Summary.WorkflowCount != 1 {
			t.Fatalf("unexpected payload: %+v", payload)
		}
		if payload.Summary.WarningCount != 1 || payload.Summary.ErrorCount != 0 {
			t.Fatalf("unexpected summary counts: %+v", payload.Summary)
		}
		if len(payload.Workflows) != 1 || payload.Workflows[0] != wf {
			t.Fatalf("unexpected workflows: %+v", payload.Workflows)
		}
		if payload.Contracts.SupportedVersion != "v1alpha1" || len(payload.Contracts.SupportedModes) != 2 || len(payload.Contracts.TopLevelModes) != 2 || len(payload.Contracts.InvariantNotes) == 0 {
			t.Fatalf("unexpected contracts: %+v", payload.Contracts)
		}
		if len(payload.Findings) != 1 {
			t.Fatalf("unexpected findings: %+v", payload.Findings)
		}
		finding, ok := payload.Findings[0].(map[string]any)
		if !ok {
			t.Fatalf("unexpected finding payload type: %#v", payload.Findings[0])
		}
		if finding["code"] != "W_COMMAND_OPAQUE" || finding["severity"] != "warning" || finding["stepId"] != "validate-run" || finding["kind"] != "Command" {
			t.Fatalf("unexpected finding payload: %+v", finding)
		}
		if res.stderr != "" {
			t.Fatalf("expected empty stderr, got %q", res.stderr)
		}
	})

	t.Run("lint verbose diagnostics stay on stderr", func(t *testing.T) {
		res := execute([]string{"lint", "--file", wf, "--v=2"})
		if res.err != nil {
			t.Fatalf("expected success, got %v", res.err)
		}
		if res.stdout != fmt.Sprintf("lint: ok (%s)\nSUMMARY mode=file workflows=1 warnings=1 errors=0 supportedVersion=v1alpha1 modes=prepare,apply topLevelModes=phases,steps\n", wf) {
			t.Fatalf("unexpected stdout: %q", res.stdout)
		}
		for _, want := range []string{"deck: lint root=", "scenario=", fmt.Sprintf("deck: lint workflow=%s", wf), "deck: lint finding code=W_COMMAND_OPAQUE severity=warning"} {
			if !strings.Contains(res.stderr, want) {
				t.Fatalf("expected %q in stderr, got %q", want, res.stderr)
			}
		}
	})

	t.Run("lint workspace verbose traces imported workflows", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "workflows", "scenarios"), 0o755); err != nil {
			t.Fatalf("mkdir scenarios: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(root, "workflows", "components", "shared"), 0o755); err != nil {
			t.Fatalf("mkdir components: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "workflows", "vars.yaml"), []byte("{}\n"), 0o644); err != nil {
			t.Fatalf("write vars: %v", err)
		}
		applyPath := filepath.Join(root, "workflows", "scenarios", "apply.yaml")
		if err := os.WriteFile(applyPath, []byte("version: v1alpha1\nphases:\n  - name: install\n    imports:\n      - path: shared/checks.yaml\n"), 0o644); err != nil {
			t.Fatalf("write apply: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "workflows", "prepare.yaml"), []byte("version: v1alpha1\nphases:\n  - name: prepare\n    imports:\n      - path: shared/checks.yaml\n"), 0o644); err != nil {
			t.Fatalf("write prepare: %v", err)
		}
		componentPath := filepath.Join(root, "workflows", "components", "shared", "checks.yaml")
		if err := os.WriteFile(componentPath, []byte("steps:\n  - id: imported-check\n    kind: Command\n    spec:\n      command: [\"true\"]\n"), 0o644); err != nil {
			t.Fatalf("write component: %v", err)
		}

		oldWD, err := os.Getwd()
		if err != nil {
			t.Fatalf("getwd: %v", err)
		}
		if err := os.Chdir(root); err != nil {
			t.Fatalf("chdir: %v", err)
		}
		defer func() { _ = os.Chdir(oldWD) }()

		res := execute([]string{"lint", "--file", applyPath, "--root", root, "--v=2"})
		if res.err != nil {
			t.Fatalf("expected success, got %v", res.err)
		}
		for _, want := range []string{"deck: lint workflow=", filepath.ToSlash(applyPath), filepath.ToSlash(componentPath)} {
			if !strings.Contains(res.stderr, want) {
				t.Fatalf("expected %q in stderr, got %q", want, res.stderr)
			}
		}
	})

	t.Run("lint current workspace by default", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "workflows", "scenarios"), 0o755); err != nil {
			t.Fatalf("mkdir scenarios: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "workflows", "vars.yaml"), []byte("{}\n"), 0o644); err != nil {
			t.Fatalf("write vars: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "workflows", "scenarios", "apply.yaml"), []byte("version: v1alpha1\nphases:\n  - name: install\n    steps: []\n"), 0o644); err != nil {
			t.Fatalf("write apply: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "workflows", "prepare.yaml"), []byte("version: v1alpha1\nphases:\n  - name: prepare\n    steps: []\n"), 0o644); err != nil {
			t.Fatalf("write prepare: %v", err)
		}

		originalCWD, err := os.Getwd()
		if err != nil {
			t.Fatalf("getwd: %v", err)
		}
		if err := os.Chdir(root); err != nil {
			t.Fatalf("chdir: %v", err)
		}
		defer func() { _ = os.Chdir(originalCWD) }()

		out, err := runWithCapturedStdout([]string{"lint"})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if out != "lint: ok (1 workflows)\nSUMMARY mode=workspace workflows=1 warnings=0 errors=0 supportedVersion=v1alpha1 modes=prepare,apply topLevelModes=phases,steps\n" {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("lint resolves scenario shorthand", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "workflows", "scenarios"), 0o755); err != nil {
			t.Fatalf("mkdir scenarios: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "workflows", "vars.yaml"), []byte("{}\n"), 0o644); err != nil {
			t.Fatalf("write vars: %v", err)
		}
		applyPath := filepath.Join(root, "workflows", "scenarios", "apply.yaml")
		if err := os.WriteFile(applyPath, []byte("version: v1alpha1\nphases:\n  - name: install\n    steps: []\n"), 0o644); err != nil {
			t.Fatalf("write apply: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "workflows", "prepare.yaml"), []byte("version: v1alpha1\nphases:\n  - name: prepare\n    steps: []\n"), 0o644); err != nil {
			t.Fatalf("write prepare: %v", err)
		}

		out, err := runWithCapturedStdout([]string{"lint", "apply", "--root", root})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if out != "lint: ok (1 workflows)\nSUMMARY mode=scenario workflows=1 warnings=0 errors=0 supportedVersion=v1alpha1 modes=prepare,apply topLevelModes=phases,steps\n" {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("lint does not emit legacy artifact integrity warnings for DownloadFile steps", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "prepare.yaml")
		writeWorkflowYAML(t, path, "version: v1alpha1\nphases:\n  - name: prepare\n    steps:\n      - id: rpm\n        kind: DownloadFile\n        spec:\n          source:\n            url: https://example.com/pkg.rpm\n          outputPath: files/pkg.rpm\n")

		res := execute([]string{"lint", "--file", path, "-o", "json"})
		if res.err != nil {
			t.Fatalf("expected success, got %v", res.err)
		}
		var payload struct {
			Summary struct {
				WarningCount int `json:"warningCount"`
			} `json:"summary"`
			Findings []map[string]any `json:"findings"`
		}
		if err := json.Unmarshal([]byte(res.stdout), &payload); err != nil {
			t.Fatalf("parse lint json: %v stdout=%q", err, res.stdout)
		}
		if payload.Summary.WarningCount != 0 {
			t.Fatalf("unexpected warning count: %+v", payload.Summary)
		}
		if len(payload.Findings) != 0 {
			t.Fatalf("unexpected findings: %+v", payload.Findings)
		}
	})

	t.Run("lint rejects component entrypoints", func(t *testing.T) {
		root := t.TempDir()
		componentPath := filepath.Join(root, "workflows", "components", "shared.yaml")
		if err := os.MkdirAll(filepath.Dir(componentPath), 0o755); err != nil {
			t.Fatalf("mkdir component dir: %v", err)
		}
		if err := os.WriteFile(componentPath, []byte("version: v1alpha1\nsteps: []\n"), 0o644); err != nil {
			t.Fatalf("write component: %v", err)
		}
		_, err := runWithCapturedStdout([]string{"lint", "--file", componentPath})
		if err == nil {
			t.Fatalf("expected component entrypoint error")
		}
		if !strings.Contains(err.Error(), "workflows/scenarios/") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("legacy workflow namespace is removed", func(t *testing.T) {
		err := run([]string{"workflow", "lint", "-f", wf})
		if err == nil {
			t.Fatalf("expected unknown command error")
		}
		if !strings.Contains(err.Error(), `unknown command "workflow" for "deck"`) {
			t.Fatalf("unexpected error: %q", err.Error())
		}
	})
}

func TestRunWorkflowBundleVerifySuccess(t *testing.T) {
	bundleDir := t.TempDir()
	createValidBundleManifest(t, bundleDir)

	out, err := runWithCapturedStdout([]string{"bundle", "verify", "--file", bundleDir})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	expected := fmt.Sprintf("bundle verify: ok (%s)\n", bundleDir)
	if out != expected {
		t.Fatalf("unexpected output\nwant: %q\ngot : %q", expected, out)
	}
}

func TestRunWorkflowBundleVerifyJSON(t *testing.T) {
	bundleDir := t.TempDir()
	createValidBundleManifest(t, bundleDir)

	res := execute([]string{"bundle", "verify", "--file", bundleDir, "-o", "json", "--v=1"})
	if res.err != nil {
		t.Fatalf("expected success, got %v", res.err)
	}
	if !strings.Contains(res.stderr, "deck: bundle verify path=") {
		t.Fatalf("expected diagnostics on stderr, got %q", res.stderr)
	}
	var payload struct {
		Status string `json:"status"`
		Path   string `json:"path"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &payload); err != nil {
		t.Fatalf("parse bundle verify json: %v stdout=%q", err, res.stdout)
	}
	if payload.Status != "ok" || payload.Path != bundleDir {
		t.Fatalf("unexpected payload: %+v", payload)
	}

	res = execute([]string{"bundle", "verify", "--file", bundleDir, "-o", "json", "--v=2"})
	if res.err != nil {
		t.Fatalf("expected success, got %v", res.err)
	}
	if !strings.Contains(res.stderr, "deck: bundle verify manifestEntries=1 files=1 images=0 packages=0 other=0") {
		t.Fatalf("expected manifest count diagnostic, got %q", res.stderr)
	}
}

func TestRunWorkflowBundleBuildSuccess(t *testing.T) {
	bundleDir := t.TempDir()
	createValidBundleManifest(t, bundleDir)
	archivePath := filepath.Join(t.TempDir(), "bundle.tar")

	collectOut, err := runWithCapturedStdout([]string{"bundle", "build", "--root", bundleDir, "--out", archivePath})
	if err != nil {
		t.Fatalf("expected build success, got %v", err)
	}
	expectedCollect := fmt.Sprintf("bundle build: ok (%s -> %s)\n", bundleDir, archivePath)
	if collectOut != expectedCollect {
		t.Fatalf("unexpected build output\nwant: %q\ngot : %q", expectedCollect, collectOut)
	}
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("expected archive file, got %v", err)
	}

	res := execute([]string{"bundle", "build", "--root", bundleDir, "--out", archivePath, "--v=2"})
	if res.err != nil {
		t.Fatalf("expected build success, got %v", res.err)
	}
	for _, want := range []string{"deck: bundle build manifest=", "entries=1", "deck: bundle build manifest files=1 images=0 packages=0 other=0", "deck: bundle build archiveSize="} {
		if !strings.Contains(res.stderr, want) {
			t.Fatalf("expected %q in stderr, got %q", want, res.stderr)
		}
	}
}

func TestRunWorkflowBundleVerifyRejectsExtraPositionalArgs(t *testing.T) {
	_, err := runWithCapturedStdout([]string{"bundle", "verify", "./one", "./two"})
	if err == nil {
		t.Fatalf("expected positional argument validation error")
	}
	if err.Error() != "bundle verify accepts a single <path>" {
		t.Fatalf("unexpected error: %q", err.Error())
	}
}

func TestRunWorkflowBundleMergeIsRemoved(t *testing.T) {
	_, err := runWithCapturedStdout([]string{"bundle", "merge"})
	if err == nil {
		t.Fatalf("expected unknown command error")
	}
	if !strings.Contains(err.Error(), `unknown command "merge" for "deck bundle"`) {
		t.Fatalf("unexpected error: %q", err.Error())
	}
}

func TestRunWorkflowBundleExtractIsRemoved(t *testing.T) {
	_, err := runWithCapturedStdout([]string{"bundle", "extract"})
	if err == nil {
		t.Fatalf("expected unknown command error")
	}
	if !strings.Contains(err.Error(), `unknown command "extract" for "deck bundle"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunWorkflowBundleInspectIsRemoved(t *testing.T) {
	_, err := runWithCapturedStdout([]string{"bundle", "inspect"})
	if err == nil {
		t.Fatalf("expected unknown command error")
	}
	if !strings.Contains(err.Error(), `unknown command "inspect" for "deck bundle"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCacheCleanIgnoresLegacyPositionalArgShape(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cacheRoot := filepath.Join(home, ".cache", "deck")
	if err := os.MkdirAll(filepath.Join(cacheRoot, "packages"), 0o755); err != nil {
		t.Fatalf("mkdir packages dir: %v", err)
	}

	_, err := runWithCapturedStdout([]string{"cache", "clean", "extra", "--dry-run"})
	if err != nil {
		t.Fatalf("expected positional arg to be ignored, got %v", err)
	}
}

func TestCacheCleanExtraPositionalStopsFlagParsingLikeLegacy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := runWithCapturedStdout([]string{"cache", "clean", "extra", "--older-than", "invalid", "--dry-run"})
	if err != nil {
		t.Fatalf("expected trailing flags after extra positional to be ignored, got %v", err)
	}
}

func TestServerUpRejectsUnexpectedPositionalArg(t *testing.T) {
	_, err := runWithCapturedStdout([]string{"server", "up", "extra"})
	if err == nil {
		t.Fatalf("expected arg validation error")
	}
	if !strings.Contains(err.Error(), `unknown command "extra" for "deck server up"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPackExtraPositionalStopsFlagParsingLikeLegacy(t *testing.T) {
	_, err := runWithCapturedStdout([]string{"prepare", "extra"})
	if err == nil {
		t.Fatalf("expected arg validation error")
	}
	if !strings.Contains(err.Error(), `unknown command "extra" for "deck prepare"`) {
		t.Fatalf("unexpected error: %q", err.Error())
	}
}

func TestPlanExtraPositionalStopsFlagParsingLikeLegacy(t *testing.T) {
	_, err := runWithCapturedStdout([]string{"plan", "extra", "--workflow", "/no/such.yaml"})
	if err == nil {
		t.Fatalf("expected missing file error")
	}
	if !strings.Contains(err.Error(), "workflow directory not found") {
		t.Fatalf("unexpected error: %q", err.Error())
	}
}
