package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/taedi90/deck/internal/config"
	"github.com/taedi90/deck/internal/install"
	sitestore "github.com/taedi90/deck/internal/site/store"
)

func TestRunUsageShowsTopLevelAxes(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "no args", args: []string{}},
		{name: "help flag", args: []string{"--help"}},
		{name: "help command", args: []string{"help"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := execute(tc.args)
			if res.err != nil {
				t.Fatalf("expected help result, got %v", res.err)
			}
			if res.exitCode != 0 {
				t.Fatalf("expected exit code 0, got %d", res.exitCode)
			}

			msg := res.stdout
			for _, cmd := range []string{"init", "validate", "pack", "plan", "apply", "doctor", "completion", "serve", "bundle", "list", "health", "logs", "cache", "node", "site"} {
				if !strings.Contains(msg, cmd) {
					t.Fatalf("usage must include %q, got %q", cmd, msg)
				}
			}
			for _, section := range []string{"Core Commands\n", "Additional Commands\n"} {
				if !strings.Contains(msg, section) {
					t.Fatalf("usage must include %q, got %q", section, msg)
				}
			}
			if strings.Index(msg, "Core Commands\n") > strings.Index(msg, "Additional Commands\n") {
				t.Fatalf("core commands section must appear before additional commands: %q", msg)
			}
			coreCommands := []string{"init", "validate", "pack", "plan", "apply"}
			for i := 0; i < len(coreCommands)-1; i++ {
				if strings.Index(msg, coreCommands[i]) > strings.Index(msg, coreCommands[i+1]) {
					t.Fatalf("core commands must keep registration order: %q appeared after %q in %q", coreCommands[i], coreCommands[i+1], msg)
				}
			}
			if strings.Index(msg, "doctor") > strings.Index(msg, "site") {
				t.Fatalf("additional commands must keep registration order: %q", msg)
			}
			for _, legacy := range []string{"strategy", "control"} {
				if strings.Contains(msg, legacy) {
					t.Fatalf("usage must not include legacy namespace %q, got %q", legacy, msg)
				}
			}
		})
	}
}

func TestCompletionHelp(t *testing.T) {
	out, err := runWithCapturedStdout([]string{"help", "completion"})
	if err != nil {
		t.Fatalf("expected help success, got %v", err)
	}
	if !strings.Contains(out, "deck completion <bash|zsh|fish|powershell>") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestCompletionCommands(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "bash", args: []string{"completion", "bash"}, want: "__start_deck"},
		{name: "zsh", args: []string{"completion", "zsh"}, want: "#compdef deck"},
		{name: "fish", args: []string{"completion", "fish"}, want: "complete -c deck"},
		{name: "powershell", args: []string{"completion", "powershell"}, want: "Register-ArgumentCompleter -CommandName 'deck'"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runWithCapturedStdout(tc.args)
			if err != nil {
				t.Fatalf("expected success, got %v", err)
			}
			if !strings.Contains(out, tc.want) {
				t.Fatalf("expected %q in output, got %q", tc.want, out)
			}
			if strings.Contains(out, "unknown command") || strings.Contains(out, "Error:") {
				t.Fatalf("unexpected non-completion output: %q", out)
			}
		})
	}
}

func TestRunTopLevelStubUsage(t *testing.T) {
	t.Run("pack usage", func(t *testing.T) {
		err := run([]string{"pack"})
		if err == nil {
			t.Fatalf("expected usage error")
		}
		if !strings.Contains(err.Error(), "--out is required") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("cache usage", func(t *testing.T) {
		out, err := runWithCapturedStdout([]string{"cache"})
		if err != nil {
			t.Fatalf("expected help output, got %v", err)
		}
		if !strings.Contains(out, "Inspect or clean deck cache data") || !strings.Contains(out, "deck cache [command]") {
			t.Fatalf("unexpected output: %q", out)
		}
	})
}

func TestNestedHelpRoutesToStdout(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"help", "pack"}, want: "deck pack [flags]"},
		{args: []string{"site", "release", "--help"}, want: "deck site release [command]"},
		{args: []string{"node", "id", "--help"}, want: "deck node id [command]"},
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

func TestNodeIDCLIShowSetInit(t *testing.T) {
	root := t.TempDir()
	operatorPath := filepath.Join(root, "etc", "deck", "node-id")
	generatedPath := filepath.Join(root, "var", "lib", "deck", "node-id")
	t.Setenv("DECK_NODE_ID_OPERATOR_PATH", operatorPath)
	t.Setenv("DECK_NODE_ID_GENERATED_PATH", generatedPath)

	initOut, err := runWithCapturedStdout([]string{"node", "id", "init"})
	if err != nil {
		t.Fatalf("node id init failed: %v", err)
	}
	if !strings.Contains(initOut, "node id init: created generated node-id") {
		t.Fatalf("expected init create output, got %q", initOut)
	}
	if !strings.Contains(initOut, "source=generated") && !strings.Contains(initOut, "source=generated-new") {
		t.Fatalf("expected generated source after init, got %q", initOut)
	}

	showOut, err := runWithCapturedStdout([]string{"node", "id", "show"})
	if err != nil {
		t.Fatalf("node id show failed: %v", err)
	}
	if !strings.Contains(showOut, "node-id=node-") {
		t.Fatalf("expected generated node id in show output, got %q", showOut)
	}
	if !strings.Contains(showOut, "source=generated") {
		t.Fatalf("expected source=generated, got %q", showOut)
	}
	if !strings.Contains(showOut, "hostname=") {
		t.Fatalf("expected hostname output, got %q", showOut)
	}

	_, err = runWithCapturedStdout([]string{"node", "id", "set", "INVALID_UPPERCASE"})
	if err == nil || !strings.Contains(err.Error(), "node-id must match") {
		t.Fatalf("expected invalid node-id validation error, got %v", err)
	}

	setOut, err := runWithCapturedStdout([]string{"node", "id", "set", "operator-1"})
	if err != nil {
		t.Fatalf("node id set failed: %v", err)
	}
	if !strings.Contains(setOut, "node id set: operator-1") {
		t.Fatalf("expected set output, got %q", setOut)
	}

	showOut, err = runWithCapturedStdout([]string{"node", "id", "show"})
	if err != nil {
		t.Fatalf("node id show failed after set: %v", err)
	}
	if !strings.Contains(showOut, "node-id=operator-1") {
		t.Fatalf("expected operator node-id after set, got %q", showOut)
	}
	if !strings.Contains(showOut, "source=operator") {
		t.Fatalf("expected operator source after set, got %q", showOut)
	}
	if !strings.Contains(showOut, "mismatch=true") {
		t.Fatalf("expected mismatch output when both files differ, got %q", showOut)
	}
}

func TestNodeAssignmentShow(t *testing.T) {
	root := t.TempDir()
	operatorPath := filepath.Join(t.TempDir(), "etc", "deck", "node-id")
	t.Setenv("DECK_NODE_ID_OPERATOR_PATH", operatorPath)
	t.Setenv("DECK_NODE_ID_GENERATED_PATH", filepath.Join(t.TempDir(), "var", "lib", "deck", "node-id"))
	if err := os.MkdirAll(filepath.Dir(operatorPath), 0o755); err != nil {
		t.Fatalf("mkdir operator path: %v", err)
	}
	if err := os.WriteFile(operatorPath, []byte("node-1\n"), 0o644); err != nil {
		t.Fatalf("write operator node id: %v", err)
	}

	st, err := sitestore.New(root)
	if err != nil {
		t.Fatalf("new site store: %v", err)
	}
	if err := st.CreateSession(sitestore.Session{ID: "session-1", ReleaseID: "rel-1", Status: "open"}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := st.SaveAssignment("session-1", sitestore.Assignment{ID: "assign-1", SessionID: "session-1", NodeID: "node-1", Role: "apply", Workflow: "workflows/apply.yaml"}); err != nil {
		t.Fatalf("save assignment: %v", err)
	}

	out, err := runWithCapturedStdout([]string{"node", "assignment", "show", "--root", root, "--session", "session-1"})
	if err != nil {
		t.Fatalf("node assignment show failed: %v", err)
	}
	for _, want := range []string{"session=session-1", "node-id=node-1", "assignment=assign-1", "role=apply", "workflow=workflows/apply.yaml"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in output, got %q", want, out)
		}
	}
}

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

		out, err := runWithCapturedStdout([]string{"health", "--server", srv.URL})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		expected := fmt.Sprintf("health: ok (%s)\n", srv.URL)
		if out != expected {
			t.Fatalf("unexpected output\nwant: %q\ngot : %q", expected, out)
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

		_, err := runWithCapturedStdout([]string{"health", "--server", srv.URL})
		if err == nil {
			t.Fatalf("expected error")
		}
		if !strings.Contains(err.Error(), "unexpected status") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("requires explicit --server when omitted", func(t *testing.T) {
		_, err := runWithCapturedStdout([]string{"health"})
		if err == nil {
			t.Fatalf("expected error when --server omitted")
		}
		if !strings.Contains(err.Error(), "--server is required") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects positional args", func(t *testing.T) {
		_, err := runWithCapturedStdout([]string{"health", "extra", "--server", "http://127.0.0.1:8080"})
		if err == nil {
			t.Fatalf("expected arg validation error")
		}
		if !strings.Contains(err.Error(), "accepts 0 arg(s), received 1") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestMigratedLeafHelpContracts(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"help", "list"}, want: "deck list [flags]"},
		{args: []string{"help", "validate"}, want: "deck validate [flags]"},
		{args: []string{"help", "health"}, want: "deck health [flags]"},
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

func TestDoctor(t *testing.T) {
	localRepo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(localRepo, 0o755); err != nil {
		t.Fatalf("mkdir local repo: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/packages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	wfPath := filepath.Join(t.TempDir(), "apply.yaml")
	writeWorkflowYAML(t, wfPath, fmt.Sprintf("role: apply\nversion: v1alpha1\nvars:\n  localRepo: %q\n  httpRepo: %q\nphases:\n  - name: install\n    steps:\n      - id: check-sources\n        apiVersion: deck/v1alpha1\n        kind: DownloadFile\n        spec:\n          source:\n            path: dummy.txt\n          fetch:\n            sources:\n              - type: local\n                path: \"{{ .vars.localRepo }}\"\n              - type: repo\n                url: \"{{ .vars.httpRepo }}\"\n          output:\n            path: files/dummy.txt\n", localRepo, srv.URL+"/packages"))

	t.Run("ok", func(t *testing.T) {
		reportPath := filepath.Join(t.TempDir(), "doctor.json")
		_, err := runWithCapturedStdout([]string{"doctor", "--file", wfPath, "--out", reportPath})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		raw, err := os.ReadFile(reportPath)
		if err != nil {
			t.Fatalf("read report: %v", err)
		}
		var report struct {
			Summary struct {
				Passed int `json:"passed"`
				Failed int `json:"failed"`
			} `json:"summary"`
			Checks []struct {
				Name   string `json:"name"`
				Status string `json:"status"`
			} `json:"checks"`
		}
		if err := json.Unmarshal(raw, &report); err != nil {
			t.Fatalf("decode report: %v", err)
		}
		if report.Summary.Failed != 0 {
			t.Fatalf("expected no failures, got %+v", report.Summary)
		}
		got := map[string]string{}
		for _, c := range report.Checks {
			got[c.Name] = c.Status
		}
		if got["vars.localRepo"] != "passed" {
			t.Fatalf("expected vars.localRepo passed, got %q", got["vars.localRepo"])
		}
		if got["vars.httpRepo"] != "passed" {
			t.Fatalf("expected vars.httpRepo passed, got %q", got["vars.httpRepo"])
		}
	})

	t.Run("missing path fails", func(t *testing.T) {
		reportPath := filepath.Join(t.TempDir(), "doctor-failed.json")
		_, err := runWithCapturedStdout([]string{"doctor", "--file", wfPath, "--out", reportPath, "--var", "localRepo=/no-such-path"})
		if err == nil {
			t.Fatalf("expected failure")
		}
		if _, statErr := os.Stat(reportPath); statErr != nil {
			t.Fatalf("expected report file, got stat error: %v", statErr)
		}
	})
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

		out, err := runWithCapturedStdout([]string{"logs", "--root", root, "--source", "file", "-o", "json"})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if !strings.Contains(out, `"job_id":"current"`) {
			t.Fatalf("expected log entry in output, got %q", out)
		}
	})

	t.Run("journal missing suggests one command", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		_, err := runWithCapturedStdout([]string{"logs", "--source", "journal", "--unit", "deck-server.service"})
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
	cacheRoot := filepath.Join(home, ".deck", "cache")
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
}

func TestRunServerAuditRotationFlagValidation(t *testing.T) {
	err := executeServe("./bundle", ":8080", "deck-site-v1", 200, 0, 10, "", "", false)
	if err == nil || !strings.Contains(err.Error(), "--audit-max-size-mb must be > 0") {
		t.Fatalf("expected audit max size validation error, got %v", err)
	}

	err = executeServe("./bundle", ":8080", "deck-site-v1", 200, 50, 0, "", "", false)
	if err == nil || !strings.Contains(err.Error(), "--audit-max-files must be > 0") {
		t.Fatalf("expected audit max files validation error, got %v", err)
	}
}

func TestRunLegacyTopLevelCommandsAreRemoved(t *testing.T) {
	for _, cmd := range []string{"run", "resume", "diagnose", "server", "agent", "workflow", "control", "strategy", "source", "service"} {
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

func TestLegacyServiceSurfaceRemoved(t *testing.T) {
	err := run([]string{"service"})
	if err == nil {
		t.Fatalf("expected unknown command error")
	}
	if !strings.Contains(err.Error(), `unknown command "service" for "deck"`) {
		t.Fatalf("unexpected error: %q", err.Error())
	}
}

func TestLegacySourceSurfaceRemoved(t *testing.T) {
	err := run([]string{"source"})
	if err == nil {
		t.Fatalf("expected unknown command error")
	}
	if !strings.Contains(err.Error(), `unknown command "source" for "deck"`) {
		t.Fatalf("unexpected error: %q", err.Error())
	}
}

func TestRunWorkflowValidateAndLegacyValidateMigration(t *testing.T) {
	wf := writeValidateWorkflowFixture(t)

	t.Run("validate with -f", func(t *testing.T) {
		out, err := runWithCapturedStdout([]string{"validate", "-f", wf})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if out != "validate: ok\n" {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("validate with --file", func(t *testing.T) {
		out, err := runWithCapturedStdout([]string{"validate", "--file", wf})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if out != "validate: ok\n" {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("validate requires file flag", func(t *testing.T) {
		_, err := runWithCapturedStdout([]string{"validate"})
		if err == nil {
			t.Fatalf("expected missing file error")
		}
		if err.Error() != "--file (or -f) is required" {
			t.Fatalf("unexpected error: %q", err.Error())
		}
	})

	t.Run("validate rejects positional args", func(t *testing.T) {
		_, err := runWithCapturedStdout([]string{"validate", wf})
		if err == nil {
			t.Fatalf("expected arg validation error")
		}
		if !strings.Contains(err.Error(), "accepts 0 arg(s), received 1") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("legacy workflow namespace is removed", func(t *testing.T) {
		err := run([]string{"workflow", "validate", "-f", wf})
		if err == nil {
			t.Fatalf("expected unknown command error")
		}
		if !strings.Contains(err.Error(), `unknown command "workflow" for "deck"`) {
			t.Fatalf("unexpected error: %q", err.Error())
		}
	})
}

func TestListIgnoresLegacyPositionalArgShape(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "workflows"), 0o755); err != nil {
		t.Fatalf("mkdir workflows: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "workflows", "apply.yaml"), []byte("role: apply\n"), 0o644); err != nil {
		t.Fatalf("write apply workflow: %v", err)
	}

	originalCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir root: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalCWD)
	})

	out, err := runWithCapturedStdout([]string{"list", "extra"})
	if err != nil {
		t.Fatalf("expected positional arg to be ignored, got %v", err)
	}
	if !strings.Contains(out, "workflows/apply.yaml") {
		t.Fatalf("expected list output, got %q", out)
	}
}

func TestListExtraPositionalStopsFlagParsingLikeLegacy(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "workflows"), 0o755); err != nil {
		t.Fatalf("mkdir workflows: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "workflows", "apply.yaml"), []byte("role: apply\n"), 0o644); err != nil {
		t.Fatalf("write apply workflow: %v", err)
	}

	originalCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir root: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalCWD)
	})

	out, err := runWithCapturedStdout([]string{"list", "extra", "--output", "invalid"})
	if err != nil {
		t.Fatalf("expected trailing flags after extra positional to be ignored, got %v", err)
	}
	if !strings.Contains(out, "workflows/apply.yaml") {
		t.Fatalf("expected list output, got %q", out)
	}
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

func TestRunWorkflowBundleCollectAndImportSuccess(t *testing.T) {
	bundleDir := t.TempDir()
	createValidBundleManifest(t, bundleDir)
	archivePath := filepath.Join(t.TempDir(), "bundle.tar")

	collectOut, err := runWithCapturedStdout([]string{"bundle", "collect", "--root", bundleDir, "--out", archivePath})
	if err != nil {
		t.Fatalf("expected collect success, got %v", err)
	}
	expectedCollect := fmt.Sprintf("bundle collect: ok (%s -> %s)\n", bundleDir, archivePath)
	if collectOut != expectedCollect {
		t.Fatalf("unexpected collect output\nwant: %q\ngot : %q", expectedCollect, collectOut)
	}
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("expected archive file, got %v", err)
	}

	importDest := t.TempDir()
	importOut, err := runWithCapturedStdout([]string{"bundle", "import", "--file", archivePath, "--dest", importDest})
	if err != nil {
		t.Fatalf("expected import success, got %v", err)
	}
	expectedImport := fmt.Sprintf("bundle import: ok (%s -> %s)\n", archivePath, importDest)
	if importOut != expectedImport {
		t.Fatalf("unexpected import output\nwant: %q\ngot : %q", expectedImport, importOut)
	}

	manifestPath := filepath.Join(importDest, "bundle", ".deck", "manifest.json")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("expected imported manifest, got %v", err)
	}

	artifactPath := filepath.Join(importDest, "bundle", "files", "dummy.txt")
	artifact, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("read imported artifact: %v", err)
	}
	if string(artifact) != "ok\n" {
		t.Fatalf("unexpected imported artifact content: %q", string(artifact))
	}
}

func TestRunWorkflowBundleInspectTar(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "bundle.tar")
	writeBundleTarFixture(t, archivePath)

	out, err := runWithCapturedStdout([]string{"bundle", "inspect", archivePath, "-o", "json"})
	if err != nil {
		t.Fatalf("expected inspect success, got %v", err)
	}
	if !strings.Contains(out, `"entries"`) || !strings.Contains(out, `"files/dummy.txt"`) {
		t.Fatalf("unexpected inspect output: %q", out)
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

func TestRunWorkflowBundleInspectRejectsExtraPositionalArgs(t *testing.T) {
	_, err := runWithCapturedStdout([]string{"bundle", "inspect", "./one", "./two"})
	if err == nil {
		t.Fatalf("expected positional argument validation error")
	}
	if err.Error() != "bundle inspect accepts a single <path>" {
		t.Fatalf("unexpected error: %q", err.Error())
	}
}

func TestRunWorkflowBundleMergeRequiresSingleArchiveArgument(t *testing.T) {
	_, err := runWithCapturedStdout([]string{"bundle", "merge", "./one.tar", "./two.tar", "--to", "./dest"})
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if err.Error() != "--to is required" {
		t.Fatalf("unexpected error: %q", err.Error())
	}
}

func TestRunWorkflowBundleMergeRejectsTrailingPositionalAfterFlags(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "bundle.tar")
	writeBundleTarFixture(t, archivePath)
	dest := filepath.Join(root, "dest")

	_, err := runWithCapturedStdout([]string{"bundle", "merge", archivePath, "--dry-run", "--to", dest, "./ignored.tar"})
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if err.Error() != "--to is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNodeIDSetArgShapeRejectsMissingArg(t *testing.T) {
	_, err := runWithCapturedStdout([]string{"node", "id", "set"})
	if err == nil {
		t.Fatalf("expected arg validation error")
	}
	if err.Error() != "accepts 1 arg(s), received 0" {
		t.Fatalf("unexpected error: %q", err.Error())
	}
}

func TestCacheCleanIgnoresLegacyPositionalArgShape(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cacheRoot := filepath.Join(home, ".deck", "cache")
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

func TestServeExtraPositionalStopsFlagParsingLikeLegacy(t *testing.T) {
	listener, err := net.Listen("tcp", ":8080")
	if err == nil {
		t.Cleanup(func() {
			_ = listener.Close()
		})
	}

	_, err = runWithCapturedStdout([]string{"serve", "extra", "--audit-max-size-mb", "0"})
	if err == nil {
		t.Fatalf("expected bind failure")
	}
	if !strings.Contains(err.Error(), "address already in use") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPackExtraPositionalStopsFlagParsingLikeLegacy(t *testing.T) {
	_, err := runWithCapturedStdout([]string{"pack", "extra", "--out", filepath.Join(t.TempDir(), "bundle.tar")})
	if err == nil {
		t.Fatalf("expected missing out error")
	}
	if err.Error() != "--out is required" {
		t.Fatalf("unexpected error: %q", err.Error())
	}
}

func TestPlanExtraPositionalStopsFlagParsingLikeLegacy(t *testing.T) {
	_, err := runWithCapturedStdout([]string{"plan", "extra", "--file", "/no/such.yaml"})
	if err == nil {
		t.Fatalf("expected missing file error")
	}
	if err.Error() != "--file (or -f) is required" {
		t.Fatalf("unexpected error: %q", err.Error())
	}
}

func TestDoctorExtraPositionalStopsFlagParsingLikeLegacy(t *testing.T) {
	_, err := runWithCapturedStdout([]string{"doctor", "extra", "--out", filepath.Join(t.TempDir(), "doctor.json")})
	if err == nil {
		t.Fatalf("expected missing out error")
	}
	if err.Error() != "--out is required" {
		t.Fatalf("unexpected error: %q", err.Error())
	}
}

func TestRunWorkflowRunInstallLocalSuccess(t *testing.T) {
	wf := writeInstallTrueWorkflowFixture(t)
	bundle := t.TempDir()
	createValidBundleManifest(t, bundle)
	if err := os.MkdirAll(filepath.Join(bundle, "workflows"), 0o755); err != nil {
		t.Fatalf("mkdir bundle workflows: %v", err)
	}

	out, err := runWithCapturedStdout([]string{"apply", "--file", wf, bundle})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if out != "apply: ok\n" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestRunApplyPositionalWorkflowPath(t *testing.T) {
	wf := writeInstallTrueWorkflowFixture(t)
	bundle := t.TempDir()
	createValidBundleManifest(t, bundle)
	if err := os.MkdirAll(filepath.Join(bundle, "workflows"), 0o755); err != nil {
		t.Fatalf("mkdir bundle workflows: %v", err)
	}

	out, err := runWithCapturedStdout([]string{"apply", wf, bundle})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if out != "apply: ok\n" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestRunApplyTwoPositionalsRequireWorkflowThenBundle(t *testing.T) {
	_, err := runWithCapturedStdout([]string{"apply", "bundle-a", "bundle-b"})
	if err == nil {
		t.Fatalf("expected positional argument validation error")
	}
	if !strings.Contains(err.Error(), "requires [workflow] [bundle]") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInit(t *testing.T) {
	assertWorkflowSet := func(t *testing.T, outDir string, wantVars string) {
		t.Helper()
		want := map[string]string{
			"vars.yaml":  wantVars,
			"pack.yaml":  "role: pack\nversion: v1alpha1\nsteps: []\n",
			"apply.yaml": "role: apply\nversion: v1alpha1\nsteps: []\n",
		}
		for fileName, expected := range want {
			path := filepath.Join(outDir, "workflows", fileName)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read generated file %s: %v", path, err)
			}
			if string(raw) != expected {
				t.Fatalf("unexpected content for %s\nwant:\n%s\ngot:\n%s", fileName, expected, string(raw))
			}
		}
	}

	t.Run("default template is single when omitted", func(t *testing.T) {
		outputDir := t.TempDir()
		if _, err := runWithCapturedStdout([]string{"init", "--out", outputDir}); err != nil {
			t.Fatalf("init failed: %v", err)
		}
		assertWorkflowSet(t, outputDir, "{}\n")
	})

	t.Run("template flag is no longer supported", func(t *testing.T) {
		_, err := runWithCapturedStdout([]string{"init", "--template", "multinode"})
		if err == nil || !strings.Contains(err.Error(), "unknown flag") {
			t.Fatalf("expected unknown flag error, got %v", err)
		}
	})

	t.Run("creates starter set under --out workflows", func(t *testing.T) {
		outputDir := t.TempDir()
		if _, err := runWithCapturedStdout([]string{"init", "--out", outputDir}); err != nil {
			t.Fatalf("init failed: %v", err)
		}
		assertWorkflowSet(t, outputDir, "{}\n")
	})

	t.Run("creates starter set under default --out .", func(t *testing.T) {
		root := t.TempDir()
		originalCWD, err := os.Getwd()
		if err != nil {
			t.Fatalf("getwd: %v", err)
		}
		if err := os.Chdir(root); err != nil {
			t.Fatalf("chdir root: %v", err)
		}
		t.Cleanup(func() {
			_ = os.Chdir(originalCWD)
		})

		if _, err := runWithCapturedStdout([]string{"init"}); err != nil {
			t.Fatalf("init failed: %v", err)
		}
		assertWorkflowSet(t, root, "{}\n")
	})

	t.Run("fails when any target file already exists and does not overwrite", func(t *testing.T) {
		outputDir := t.TempDir()
		workflowsDir := filepath.Join(outputDir, "workflows")
		if err := os.MkdirAll(workflowsDir, 0o755); err != nil {
			t.Fatalf("mkdir workflows: %v", err)
		}
		existingPath := filepath.Join(workflowsDir, "pack.yaml")
		if err := os.WriteFile(existingPath, []byte("seed\n"), 0o644); err != nil {
			t.Fatalf("seed pack.yaml: %v", err)
		}

		_, err := runWithCapturedStdout([]string{"init", "--out", outputDir})
		if err == nil {
			t.Fatalf("expected overwrite refusal error")
		}
		if !strings.Contains(err.Error(), "refusing to overwrite") || !strings.Contains(err.Error(), existingPath) {
			t.Fatalf("expected actionable overwrite error, got %v", err)
		}

		raw, readErr := os.ReadFile(existingPath)
		if readErr != nil {
			t.Fatalf("read seeded pack.yaml: %v", readErr)
		}
		if string(raw) != "seed\n" {
			t.Fatalf("existing file must remain unchanged, got %q", string(raw))
		}
		if _, statErr := os.Stat(filepath.Join(workflowsDir, "apply.yaml")); !os.IsNotExist(statErr) {
			t.Fatalf("apply.yaml must not be created on failure, stat err=%v", statErr)
		}
	})

	t.Run("fails when target path exists as directory", func(t *testing.T) {
		outputDir := t.TempDir()
		workflowsDir := filepath.Join(outputDir, "workflows")
		targetDir := filepath.Join(workflowsDir, "pack.yaml")
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			t.Fatalf("mkdir conflicting directory: %v", err)
		}

		_, err := runWithCapturedStdout([]string{"init", "--out", outputDir})
		if err == nil {
			t.Fatalf("expected overwrite refusal error")
		}
		if !strings.Contains(err.Error(), "refusing to overwrite") || !strings.Contains(err.Error(), targetDir) {
			t.Fatalf("expected directory conflict path in error, got %v", err)
		}
		if _, statErr := os.Stat(filepath.Join(workflowsDir, "apply.yaml")); !os.IsNotExist(statErr) {
			t.Fatalf("apply.yaml must not be created on failure, stat err=%v", statErr)
		}
		if _, statErr := os.Stat(filepath.Join(workflowsDir, "vars.yaml")); !os.IsNotExist(statErr) {
			t.Fatalf("vars.yaml must not be created on failure, stat err=%v", statErr)
		}
	})
}

func TestRunWorkflowRunDryRunPrintsPlan(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Setenv("HOME", home)
	bundle := t.TempDir()
	if err := os.MkdirAll(filepath.Join(bundle, "workflows"), 0o755); err != nil {
		t.Fatalf("mkdir bundle workflows: %v", err)
	}

	wf := writeInstallTrueWorkflowFixture(t)

	out, err := runWithCapturedStdout([]string{"apply", "--file", wf, "--dry-run", bundle})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !strings.Contains(out, "PHASE=install") {
		t.Fatalf("expected phase line in output, got %q", out)
	}
	if !strings.Contains(out, "run-true RunCommand PLAN") {
		t.Fatalf("expected step plan line in output, got %q", out)
	}
}

func TestRunPackCreatesBundleTar(t *testing.T) {
	root := t.TempDir()
	workflowsDir := filepath.Join(root, "workflows")
	if err := os.MkdirAll(workflowsDir, 0o755); err != nil {
		t.Fatalf("mkdir workflows: %v", err)
	}
	seedDir := filepath.Join(root, "seed", "files")
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		t.Fatalf("mkdir seed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(seedDir, "source.bin"), []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("write seed file: %v", err)
	}
	packPath := filepath.Join(workflowsDir, "pack.yaml")
	packBody := fmt.Sprintf(`role: pack
version: v1alpha1
phases:
  - name: prepare
    steps:
      - id: p1
        kind: DownloadFile
        spec:
          source:
            path: files/source.bin
          fetch:
            sources:
              - type: local
                path: %q
`, filepath.Join(root, "seed"))
	if err := os.WriteFile(packPath, []byte(packBody), 0o644); err != nil {
		t.Fatalf("write pack workflow: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowsDir, "apply.yaml"), []byte("role: apply\nversion: v1alpha1\nsteps: []\n"), 0o644); err != nil {
		t.Fatalf("write apply workflow: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowsDir, "vars.yaml"), []byte("kubernetesVersion: v1.30.1\n"), 0o644); err != nil {
		t.Fatalf("write vars workflow: %v", err)
	}
	fragmentDir := filepath.Join(workflowsDir, "offline-multinode")
	if err := os.MkdirAll(fragmentDir, 0o755); err != nil {
		t.Fatalf("mkdir fragment dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fragmentDir, "apply-common.yaml"), []byte("role: apply\nversion: v1alpha1\nsteps: []\n"), 0o644); err != nil {
		t.Fatalf("write workflow fragment: %v", err)
	}

	originalCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir root: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalCWD)
	})

	outTar := filepath.Join(root, "bundle.tar")
	if _, err := runWithCapturedStdout([]string{"pack", "--out", outTar}); err != nil {
		t.Fatalf("pack failed: %v", err)
	}

	names, err := tarEntryNamesFromFile(outTar)
	if err != nil {
		t.Fatalf("read tar entries: %v", err)
	}
	for _, name := range names {
		if !strings.HasPrefix(name, "bundle/") {
			t.Fatalf("unexpected non-bundle tar path: %s", name)
		}
	}
	for _, required := range []string{
		"bundle/deck",
		"bundle/files/deck",
		"bundle/workflows/pack.yaml",
		"bundle/workflows/apply.yaml",
		"bundle/workflows/vars.yaml",
		"bundle/workflows/offline-multinode/apply-common.yaml",
		"bundle/.deck/manifest.json",
	} {
		if !sliceContains(names, required) {
			t.Fatalf("missing tar entry %s: %#v", required, names)
		}
	}
}

func TestRunPackDryRunDoesNotWrite(t *testing.T) {
	root := t.TempDir()
	workflowsDir := filepath.Join(root, "workflows")
	if err := os.MkdirAll(workflowsDir, 0o755); err != nil {
		t.Fatalf("mkdir workflows: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowsDir, "pack.yaml"), []byte("role: pack\nversion: v1alpha1\nsteps: []\n"), 0o644); err != nil {
		t.Fatalf("write pack workflow: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowsDir, "apply.yaml"), []byte("role: apply\nversion: v1alpha1\nsteps: []\n"), 0o644); err != nil {
		t.Fatalf("write apply workflow: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowsDir, "vars.yaml"), []byte("x: y\n"), 0o644); err != nil {
		t.Fatalf("write vars workflow: %v", err)
	}
	originalCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir root: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalCWD)
	})

	outTar := filepath.Join(root, "bundle.tar")
	planOut, err := runWithCapturedStdout([]string{"pack", "--dry-run"})
	if err != nil {
		t.Fatalf("pack dry-run failed: %v", err)
	}
	if !strings.Contains(planOut, "PACK_WORKFLOW=") {
		t.Fatalf("expected dry-run plan output, got %q", planOut)
	}
	if _, statErr := os.Stat(outTar); !os.IsNotExist(statErr) {
		t.Fatalf("dry-run must not create tar, stat err=%v", statErr)
	}
}

func TestRunPackVarFlagOverridesWorkflowVars(t *testing.T) {
	root := t.TempDir()
	workflowsDir := filepath.Join(root, "workflows")
	if err := os.MkdirAll(workflowsDir, 0o755); err != nil {
		t.Fatalf("mkdir workflows: %v", err)
	}
	seedDir := filepath.Join(root, "seed", "files")
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		t.Fatalf("mkdir seed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(seedDir, "source.bin"), []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("write seed file: %v", err)
	}

	packPath := filepath.Join(workflowsDir, "pack.yaml")
	packBody := fmt.Sprintf(`role: pack
version: v1alpha1
vars:
  relPath: default.bin
phases:
  - name: prepare
    steps:
      - id: p1
        kind: DownloadFile
        spec:
          source:
            path: files/source.bin
          fetch:
            sources:
              - type: local
                path: %q
          output:
            path: files/{{ .vars.relPath  }}
`, filepath.Join(root, "seed"))
	if err := os.WriteFile(packPath, []byte(packBody), 0o644); err != nil {
		t.Fatalf("write pack workflow: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowsDir, "apply.yaml"), []byte("role: apply\nversion: v1alpha1\nsteps: []\n"), 0o644); err != nil {
		t.Fatalf("write apply workflow: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowsDir, "vars.yaml"), []byte("kubernetesVersion: v1.30.1\n"), 0o644); err != nil {
		t.Fatalf("write vars workflow: %v", err)
	}

	originalCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir root: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalCWD)
	})

	outTar := filepath.Join(root, "bundle.tar")
	if _, err := runWithCapturedStdout([]string{"pack", "--out", outTar, "--var", "relPath=override.bin"}); err != nil {
		t.Fatalf("pack failed: %v", err)
	}

	names, err := tarEntryNamesFromFile(outTar)
	if err != nil {
		t.Fatalf("read tar entries: %v", err)
	}
	if !sliceContains(names, "bundle/files/override.bin") {
		t.Fatalf("expected override output in tar entries: %#v", names)
	}
	if sliceContains(names, "bundle/files/default.bin") {
		t.Fatalf("unexpected default output in tar entries: %#v", names)
	}
}

func TestResolveInstallStatePathUsesHomeAndStateKey(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Setenv("HOME", home)

	wf := &config.Workflow{StateKey: "abc123"}
	statePath, err := resolveInstallStatePath(wf)
	if err != nil {
		t.Fatalf("resolveInstallStatePath failed: %v", err)
	}

	expected := filepath.Join(home, ".deck", "state", "abc123.json")
	if statePath != expected {
		t.Fatalf("state path mismatch: got %q want %q", statePath, expected)
	}
}

func TestRunApplyVarFlagLastWins(t *testing.T) {
	wfPath := filepath.Join(t.TempDir(), "apply-vars.yaml")
	content := `role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: run-with-vars
        kind: RunCommand
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

	out, err := runWithCapturedStdout([]string{"apply", "-f", wfPath, "--dry-run", "--var", "run=no", "--var", "run=yes", bundle})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !strings.Contains(out, "run-with-vars RunCommand PLAN") {
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
	bundleRoot := filepath.Join(root, "bundle")
	createValidBundleManifest(t, bundleRoot)
	if err := os.MkdirAll(filepath.Join(bundleRoot, "workflows"), 0o755); err != nil {
		t.Fatalf("mkdir workflows: %v", err)
	}

	installLogPath := filepath.Join(root, "install.log")
	postLogPath := filepath.Join(root, "post.log")
	workflowPath := filepath.Join(root, "apply.yaml")
	workflowBody := fmt.Sprintf(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: install-step
        kind: RunCommand
        spec:
          command: ["sh", "-c", "echo install >> %s"]
  - name: post
    steps:
      - id: post-step
        kind: RunCommand
        spec:
          command: ["sh", "-c", "echo post >> %s"]
`, strings.ReplaceAll(installLogPath, "\\", "\\\\"), strings.ReplaceAll(postLogPath, "\\", "\\\\"))
	if err := os.WriteFile(workflowPath, []byte(workflowBody), 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	if _, err := runWithCapturedStdout([]string{"apply", "--file", workflowPath, "--phase", "post", bundleRoot}); err != nil {
		t.Fatalf("first apply --phase post failed: %v", err)
	}
	if _, err := runWithCapturedStdout([]string{"apply", "--file", workflowPath, "--phase", "post", bundleRoot}); err != nil {
		t.Fatalf("second apply --phase post failed: %v", err)
	}
	dryRunOut, err := runWithCapturedStdout([]string{"apply", "--file", workflowPath, "--phase", "post", "--dry-run", bundleRoot})
	if err != nil {
		t.Fatalf("dry-run apply --phase post failed: %v", err)
	}
	if !strings.Contains(dryRunOut, "PHASE=post") {
		t.Fatalf("expected post phase line in dry-run output, got %q", dryRunOut)
	}
	if !strings.Contains(dryRunOut, "post-step RunCommand SKIP (completed)") {
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

func TestApplyPrefetch(t *testing.T) {
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

	downloadedRelPath := filepath.ToSlash(filepath.Join("files", "prefetched.txt"))
	downloadedPath := filepath.Join(bundle, filepath.FromSlash(downloadedRelPath))
	workflowPath := filepath.Join(t.TempDir(), "apply-prefetch.yaml")

	var mu sync.Mutex
	downloadCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		downloadCount++
		mu.Unlock()
		_, _ = w.Write([]byte("prefetched\n"))
	}))
	defer srv.Close()

	workflowBody := fmt.Sprintf(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: requires-prefetch
        kind: RunCommand
        spec:
          command:
            - sh
            - -c
            - 'test -f %s'
      - id: download-file
        kind: DownloadFile
        spec:
          source:
            url: '%s'
          output:
            path: '%s'
`, downloadedPath, srv.URL+"/payload", downloadedRelPath)
	if err := os.WriteFile(workflowPath, []byte(workflowBody), 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	_, err := runWithCapturedStdout([]string{"apply", "--file", workflowPath, bundle})
	if err == nil {
		t.Fatalf("expected apply without --prefetch to fail")
	}

	if _, err := runWithCapturedStdout([]string{"apply", "--file", workflowPath, "--prefetch", bundle}); err != nil {
		t.Fatalf("apply with --prefetch failed: %v", err)
	}

	raw, err := os.ReadFile(downloadedPath)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(raw) != "prefetched\n" {
		t.Fatalf("unexpected downloaded content: %q", string(raw))
	}

	wf, err := config.Load(context.Background(), workflowPath)
	if err != nil {
		t.Fatalf("load workflow: %v", err)
	}
	statePath, err := resolveInstallStatePath(wf)
	if err != nil {
		t.Fatalf("resolve state path: %v", err)
	}
	stateRaw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var state struct {
		CompletedSteps []string `json:"completedSteps"`
	}
	if err := json.Unmarshal(stateRaw, &state); err != nil {
		t.Fatalf("parse state: %v", err)
	}
	if !sliceContains(state.CompletedSteps, "download-file") {
		t.Fatalf("expected download-file to be completed in state, got %#v", state.CompletedSteps)
	}

	if err := os.Remove(downloadedPath); err != nil {
		t.Fatalf("remove downloaded file before rerun: %v", err)
	}
	if _, err := runWithCapturedStdout([]string{"apply", "--file", workflowPath, "--prefetch", bundle}); err != nil {
		t.Fatalf("second apply with --prefetch failed: %v", err)
	}

	mu.Lock()
	gotDownloads := downloadCount
	mu.Unlock()
	if gotDownloads != 1 {
		t.Fatalf("expected exactly one download across prefetch reruns, got %d", gotDownloads)
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
		workflowBody := fmt.Sprintf(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: remote-step
        kind: RunCommand
        spec:
          command: ["sh", "-c", "echo hit >> %s"]
`, strings.ReplaceAll(logPath, "\\", "\\\\"))

		var mu sync.Mutex
		varsBody := "mode: alpha\n"
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/workflows/apply.yaml":
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

		workflowURL := ts.URL + "/workflows/apply.yaml"
		if _, err := runWithCapturedStdout([]string{"apply", "--file", workflowURL}); err != nil {
			t.Fatalf("remote apply(1) failed: %v", err)
		}

		wf1, err := config.Load(context.Background(), workflowURL)
		if err != nil {
			t.Fatalf("load remote workflow(1): %v", err)
		}
		statePath1, err := resolveInstallStatePath(wf1)
		if err != nil {
			t.Fatalf("resolve state path(1): %v", err)
		}
		if _, err := os.Stat(statePath1); err != nil {
			t.Fatalf("expected first state file: %v", err)
		}

		mu.Lock()
		varsBody = "mode: beta\n"
		mu.Unlock()

		if _, err := runWithCapturedStdout([]string{"apply", "--file", workflowURL}); err != nil {
			t.Fatalf("remote apply(2) failed: %v", err)
		}

		wf2, err := config.Load(context.Background(), workflowURL)
		if err != nil {
			t.Fatalf("load remote workflow(2): %v", err)
		}
		statePath2, err := resolveInstallStatePath(wf2)
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
		workflowBody := fmt.Sprintf(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: remote-step
        kind: RunCommand
        spec:
          command: ["sh", "-c", "echo hit >> %s"]
`, strings.ReplaceAll(logPath, "\\", "\\\\"))

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/workflows/apply.yaml":
				_, _ = w.Write([]byte(workflowBody))
			case "/workflows/vars.yaml":
				http.NotFound(w, r)
			default:
				http.NotFound(w, r)
			}
		}))
		defer ts.Close()

		workflowURL := ts.URL + "/workflows/apply.yaml"
		ignoredBundleArg := filepath.Join(t.TempDir(), "missing-bundle")
		if _, err := runWithCapturedStdout([]string{"apply", "--file", workflowURL, ignoredBundleArg}); err != nil {
			t.Fatalf("remote apply with ignored positional bundle(1) failed: %v", err)
		}
		if _, err := runWithCapturedStdout([]string{"apply", "--file", workflowURL, ignoredBundleArg}); err != nil {
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

	t.Run("role pack is rejected for remote apply", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/workflows/apply.yaml":
				_, _ = w.Write([]byte("role: pack\nversion: v1alpha1\nsteps:\n  - id: pack-step\n    kind: RunCommand\n    spec:\n      command: [\"true\"]\n"))
			case "/workflows/vars.yaml":
				http.NotFound(w, r)
			default:
				http.NotFound(w, r)
			}
		}))
		defer ts.Close()

		_, err := runWithCapturedStdout([]string{"apply", "--file", ts.URL + "/workflows/apply.yaml"})
		if err == nil {
			t.Fatalf("expected role rejection error")
		}
		if !strings.Contains(err.Error(), "apply workflow role must be apply") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestPlan(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	wfPath := filepath.Join(t.TempDir(), "apply.yaml")
	writeWorkflowYAML(t, wfPath, "role: apply\nversion: v1alpha1\nphases:\n  - name: install\n    steps:\n      - id: step-1\n        apiVersion: deck/v1alpha1\n        kind: RunCommand\n        spec:\n          command: [\"true\"]\n")

	before, err := runWithCapturedStdout([]string{"plan", "--file", wfPath})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !strings.Contains(before, "RUN") {
		t.Fatalf("expected RUN in plan output, got %q", before)
	}

	wf, err := config.Load(context.Background(), wfPath)
	if err != nil {
		t.Fatalf("load workflow: %v", err)
	}
	execWf, err := buildApplyExecutionWorkflow(wf, "install")
	if err != nil {
		t.Fatalf("build execution workflow: %v", err)
	}
	if err := install.Run(context.Background(), execWf, install.RunOptions{BundleRoot: ""}); err != nil {
		t.Fatalf("install run: %v", err)
	}

	after, err := runWithCapturedStdout([]string{"plan", "--file", wfPath})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if before == after {
		t.Fatalf("expected plan output to change after apply run")
	}
	if !strings.Contains(after, "SKIP") {
		t.Fatalf("expected SKIP in plan output after apply run, got %q", after)
	}
}

func TestAssistedApplyUsesLocalEngine(t *testing.T) {
	assistedRoot := t.TempDir()
	t.Setenv("DECK_ASSISTED_ROOT", assistedRoot)
	operatorPath := filepath.Join(t.TempDir(), "etc", "deck", "node-id")
	t.Setenv("DECK_NODE_ID_OPERATOR_PATH", operatorPath)
	t.Setenv("DECK_NODE_ID_GENERATED_PATH", filepath.Join(t.TempDir(), "var", "lib", "deck", "node-id"))
	if err := os.MkdirAll(filepath.Dir(operatorPath), 0o755); err != nil {
		t.Fatalf("mkdir operator path: %v", err)
	}
	if err := os.WriteFile(operatorPath, []byte("node-1\n"), 0o644); err != nil {
		t.Fatalf("write node id: %v", err)
	}

	logPath := filepath.Join(t.TempDir(), "assisted-apply.log")
	bundleFilePath := filepath.Join(assistedRoot, "releases", "release-1", "bundle", "files", "seed.txt")
	workflowBody := fmt.Sprintf("role: apply\nversion: v1alpha1\nphases:\n  - name: install\n    steps:\n      - id: assisted-apply\n        kind: RunCommand\n        spec:\n          command: [\"sh\", \"-c\", \"test -f %s && echo assisted >> %s\"]\n", strings.ReplaceAll(bundleFilePath, "\\", "\\\\"), strings.ReplaceAll(logPath, "\\", "\\\\"))
	seedContent := []byte("seed\n")
	seedSum := sha256.Sum256(seedContent)
	manifestBody := fmt.Sprintf("{\n  \"entries\": [\n    {\"path\": %q, \"sha256\": %q, \"size\": %d}\n  ]\n}\n", "files/seed.txt", hex.EncodeToString(seedSum[:]), len(seedContent))

	var uploadedReport struct {
		Action string `json:"action"`
		Status string `json:"status"`
		NodeID string `json:"node_id"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/site/v1/") && r.Header.Get("Authorization") != "Bearer token-1" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/site/v1/sessions/session-1":
			_, _ = w.Write([]byte(`{"id":"session-1","release_id":"release-1","status":"open"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/site/v1/sessions/session-1/assignment":
			_, _ = w.Write([]byte(`{"id":"assign-1","session_id":"session-1","node_id":"node-1","role":"apply","workflow":"workflows/apply.yaml"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/site/v1/sessions/session-1/reports":
			if err := json.NewDecoder(r.Body).Decode(&uploadedReport); err != nil {
				t.Fatalf("decode uploaded report: %v", err)
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"status":"accepted"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/site/releases/release-1/bundle/.deck/manifest.json":
			_, _ = w.Write([]byte(manifestBody))
		case r.Method == http.MethodGet && r.URL.Path == "/site/releases/release-1/bundle/workflows/apply.yaml":
			_, _ = w.Write([]byte(workflowBody))
		case r.Method == http.MethodGet && r.URL.Path == "/site/releases/release-1/bundle/workflows/vars.yaml":
			_, _ = w.Write([]byte("{}\n"))
		case r.Method == http.MethodGet && r.URL.Path == "/site/releases/release-1/bundle/files/seed.txt":
			_, _ = w.Write(seedContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out, err := runWithCapturedStdout([]string{"apply", "--server", srv.URL, "--session", "session-1", "--api-token", "token-1"})
	if err != nil {
		t.Fatalf("assisted apply failed: %v", err)
	}
	if out != "apply: ok\n" {
		t.Fatalf("unexpected apply output: %q", out)
	}
	if uploadedReport.Action != "apply" || uploadedReport.Status != "ok" || uploadedReport.NodeID != "node-1" {
		t.Fatalf("unexpected uploaded report: %#v", uploadedReport)
	}
	if raw, readErr := os.ReadFile(logPath); readErr != nil || !strings.Contains(string(raw), "assisted") {
		t.Fatalf("expected local engine execution log, err=%v raw=%q", readErr, string(raw))
	}
}

func TestAssistedDiffUsesLocalEngine(t *testing.T) {
	assistedRoot := t.TempDir()
	t.Setenv("DECK_ASSISTED_ROOT", assistedRoot)
	operatorPath := filepath.Join(t.TempDir(), "etc", "deck", "node-id")
	t.Setenv("DECK_NODE_ID_OPERATOR_PATH", operatorPath)
	t.Setenv("DECK_NODE_ID_GENERATED_PATH", filepath.Join(t.TempDir(), "var", "lib", "deck", "node-id"))
	_ = os.MkdirAll(filepath.Dir(operatorPath), 0o755)
	_ = os.WriteFile(operatorPath, []byte("node-1\n"), 0o644)

	workflowBody := "role: apply\nversion: v1alpha1\nphases:\n  - name: install\n    steps:\n      - id: assisted-diff\n        kind: RunCommand\n        spec:\n          command: [\"true\"]\n"
	manifestBody := "{\n  \"entries\": []\n}\n"
	uploaded := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/site/v1/sessions/session-1":
			_, _ = w.Write([]byte(`{"id":"session-1","release_id":"release-1","status":"open"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/site/v1/sessions/session-1/assignment":
			_, _ = w.Write([]byte(`{"id":"assign-diff","session_id":"session-1","node_id":"node-1","role":"diff","workflow":"workflows/apply.yaml"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/site/v1/sessions/session-1/reports":
			uploaded = true
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"status":"accepted"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/site/releases/release-1/bundle/.deck/manifest.json":
			_, _ = w.Write([]byte(manifestBody))
		case r.Method == http.MethodGet && r.URL.Path == "/site/releases/release-1/bundle/workflows/apply.yaml":
			_, _ = w.Write([]byte(workflowBody))
		case r.Method == http.MethodGet && r.URL.Path == "/site/releases/release-1/bundle/workflows/vars.yaml":
			_, _ = w.Write([]byte("{}\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out, err := runWithCapturedStdout([]string{"plan", "--server", srv.URL, "--session", "session-1", "--api-token", "deck-site-v1"})
	if err != nil {
		t.Fatalf("assisted plan failed: %v", err)
	}
	if !strings.Contains(out, "RUN") {
		t.Fatalf("expected RUN plan output, got %q", out)
	}
	if !uploaded {
		t.Fatalf("expected assisted plan report upload")
	}
}

func TestAssistedDoctorUsesLocalEngine(t *testing.T) {
	assistedRoot := t.TempDir()
	t.Setenv("DECK_ASSISTED_ROOT", assistedRoot)
	operatorPath := filepath.Join(t.TempDir(), "etc", "deck", "node-id")
	t.Setenv("DECK_NODE_ID_OPERATOR_PATH", operatorPath)
	t.Setenv("DECK_NODE_ID_GENERATED_PATH", filepath.Join(t.TempDir(), "var", "lib", "deck", "node-id"))
	_ = os.MkdirAll(filepath.Dir(operatorPath), 0o755)
	_ = os.WriteFile(operatorPath, []byte("node-1\n"), 0o644)

	localRepo := t.TempDir()
	reportPath := filepath.Join(t.TempDir(), "doctor-assist.json")
	workflowBody := "role: apply\nversion: v1alpha1\nphases:\n  - name: install\n    steps:\n      - id: doctor-check\n        kind: DownloadFile\n        spec:\n          source:\n            path: files/dummy.txt\n          fetch:\n            sources:\n              - type: local\n                path: \"{{ .vars.localRepo }}\"\n          output:\n            path: files/dummy.txt\n"
	varsBody := fmt.Sprintf("localRepo: %q\n", localRepo)
	manifestBody := "{\n  \"entries\": []\n}\n"
	uploaded := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/site/v1/sessions/session-1":
			_, _ = w.Write([]byte(`{"id":"session-1","release_id":"release-1","status":"open"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/site/v1/sessions/session-1/assignment":
			_, _ = w.Write([]byte(`{"id":"assign-doctor","session_id":"session-1","node_id":"node-1","role":"doctor","workflow":"workflows/apply.yaml"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/site/v1/sessions/session-1/reports":
			uploaded = true
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"status":"accepted"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/site/releases/release-1/bundle/.deck/manifest.json":
			_, _ = w.Write([]byte(manifestBody))
		case r.Method == http.MethodGet && r.URL.Path == "/site/releases/release-1/bundle/workflows/apply.yaml":
			_, _ = w.Write([]byte(workflowBody))
		case r.Method == http.MethodGet && r.URL.Path == "/site/releases/release-1/bundle/workflows/vars.yaml":
			_, _ = w.Write([]byte(varsBody))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out, err := runWithCapturedStdout([]string{"doctor", "--server", srv.URL, "--session", "session-1", "--api-token", "deck-site-v1", "--out", reportPath})
	if err != nil {
		t.Fatalf("assisted doctor failed: %v", err)
	}
	if !strings.Contains(out, "doctor: wrote") {
		t.Fatalf("unexpected doctor output: %q", out)
	}
	if !uploaded {
		t.Fatalf("expected assisted doctor report upload")
	}
}

func TestAssistedModeNoAssignmentSkips(t *testing.T) {
	assistedRoot := t.TempDir()
	t.Setenv("DECK_ASSISTED_ROOT", assistedRoot)
	operatorPath := filepath.Join(t.TempDir(), "etc", "deck", "node-id")
	t.Setenv("DECK_NODE_ID_OPERATOR_PATH", operatorPath)
	t.Setenv("DECK_NODE_ID_GENERATED_PATH", filepath.Join(t.TempDir(), "var", "lib", "deck", "node-id"))
	_ = os.MkdirAll(filepath.Dir(operatorPath), 0o755)
	_ = os.WriteFile(operatorPath, []byte("node-1\n"), 0o644)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/site/v1/sessions/session-1":
			_, _ = w.Write([]byte(`{"id":"session-1","release_id":"release-1","status":"open"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/site/v1/sessions/session-1/assignment":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"no assignment matched session \"session-1\" node_id \"node-1\" role \"apply\""}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/site/v1/sessions/session-1/reports":
			t.Fatalf("no-assignment path must not upload report")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out, err := runWithCapturedStdout([]string{"apply", "--server", srv.URL, "--session", "session-1"})
	if err != nil {
		t.Fatalf("assisted apply no-assignment should succeed with skip, got %v", err)
	}
	if !strings.Contains(out, "apply: skipped (no-assignment)") {
		t.Fatalf("unexpected skip output: %q", out)
	}
	reportDir := filepath.Join(assistedRoot, "reports", "session-1", "node-1")
	entries, readErr := os.ReadDir(reportDir)
	if readErr != nil || len(entries) == 0 {
		t.Fatalf("expected local skipped report at %s: err=%v", reportDir, readErr)
	}
}

func TestAssistedApplyServerDownAfterAssignment(t *testing.T) {
	assistedRoot := t.TempDir()
	t.Setenv("DECK_ASSISTED_ROOT", assistedRoot)
	operatorPath := filepath.Join(t.TempDir(), "etc", "deck", "node-id")
	t.Setenv("DECK_NODE_ID_OPERATOR_PATH", operatorPath)
	t.Setenv("DECK_NODE_ID_GENERATED_PATH", filepath.Join(t.TempDir(), "var", "lib", "deck", "node-id"))
	_ = os.MkdirAll(filepath.Dir(operatorPath), 0o755)
	_ = os.WriteFile(operatorPath, []byte("node-1\n"), 0o644)

	logPath := filepath.Join(t.TempDir(), "assisted-upload-fail.log")
	bundleFilePath := filepath.Join(assistedRoot, "releases", "release-1", "bundle", "files", "seed.txt")
	workflowBody := fmt.Sprintf("role: apply\nversion: v1alpha1\nphases:\n  - name: install\n    steps:\n      - id: assisted-apply\n        kind: RunCommand\n        spec:\n          command: [\"sh\", \"-c\", \"test -f %s && echo assisted >> %s\"]\n", strings.ReplaceAll(bundleFilePath, "\\", "\\\\"), strings.ReplaceAll(logPath, "\\", "\\\\"))
	seedContent := []byte("seed\n")
	seedSum := sha256.Sum256(seedContent)
	manifestBody := fmt.Sprintf("{\n  \"entries\": [\n    {\"path\": %q, \"sha256\": %q, \"size\": %d}\n  ]\n}\n", "files/seed.txt", hex.EncodeToString(seedSum[:]), len(seedContent))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/site/v1/sessions/session-1":
			_, _ = w.Write([]byte(`{"id":"session-1","release_id":"release-1","status":"open"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/site/v1/sessions/session-1/assignment":
			_, _ = w.Write([]byte(`{"id":"assign-1","session_id":"session-1","node_id":"node-1","role":"apply","workflow":"workflows/apply.yaml"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/site/v1/sessions/session-1/reports":
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"temporary_unavailable"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/site/releases/release-1/bundle/.deck/manifest.json":
			_, _ = w.Write([]byte(manifestBody))
		case r.Method == http.MethodGet && r.URL.Path == "/site/releases/release-1/bundle/workflows/apply.yaml":
			_, _ = w.Write([]byte(workflowBody))
		case r.Method == http.MethodGet && r.URL.Path == "/site/releases/release-1/bundle/workflows/vars.yaml":
			_, _ = w.Write([]byte("{}\n"))
		case r.Method == http.MethodGet && r.URL.Path == "/site/releases/release-1/bundle/files/seed.txt":
			_, _ = w.Write(seedContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	_, err := runWithCapturedStdout([]string{"apply", "--server", srv.URL, "--session", "session-1"})
	if err == nil {
		t.Fatalf("expected upload transport failure")
	}
	if !strings.Contains(err.Error(), "report upload transport failure") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), filepath.Join(assistedRoot, "reports", "session-1", "node-1")) {
		t.Fatalf("expected local report path in error: %v", err)
	}
	if raw, readErr := os.ReadFile(logPath); readErr != nil || !strings.Contains(string(raw), "assisted") {
		t.Fatalf("expected apply execution to complete before upload failure, err=%v raw=%q", readErr, string(raw))
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
	workflowBody := "role: apply\nversion: v1alpha1\nphases:\n  - name: install\n    steps:\n      - id: step-one\n        kind: RunCommand\n        spec:\n          command: [\"true\"]\n"
	if err := os.WriteFile(workflowPath, []byte(workflowBody), 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	_, err := runWithCapturedStdout([]string{"apply", "--file", workflowPath, "--phase", "post", bundle})
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

	bundleDir := filepath.Join(root, "bundle")
	if err := os.MkdirAll(filepath.Join(bundleDir, "workflows"), 0o755); err != nil {
		t.Fatalf("mkdir bundle workflows: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(root, "workflows"), 0o755); err != nil {
		t.Fatalf("mkdir cwd workflows: %v", err)
	}

	archivePath := filepath.Join(root, "bundle.tar")
	writeApplyBundleTarFixture(t, archivePath)

	resolved, err := resolveApplyBundleRoot(positionalBundle)
	if err != nil {
		t.Fatalf("resolve positional bundle: %v", err)
	}
	if resolved != positionalBundle {
		t.Fatalf("expected positional bundle, got %s", resolved)
	}

	resolved, err = resolveApplyBundleRoot("")
	if err != nil {
		t.Fatalf("resolve default bundle candidate: %v", err)
	}
	resolvedSlash := filepath.ToSlash(resolved)
	if !strings.Contains(resolvedSlash, "/.deck/extract/") || !strings.HasSuffix(resolvedSlash, "/bundle") {
		t.Fatalf("expected extracted bundle root, got %s", resolved)
	}

	if err := os.Remove(archivePath); err != nil {
		t.Fatalf("remove bundle.tar: %v", err)
	}
	resolved, err = resolveApplyBundleRoot("")
	if err != nil {
		t.Fatalf("resolve bundle directory candidate: %v", err)
	}
	if resolved != bundleDir {
		t.Fatalf("expected bundle directory, got %s", resolved)
	}

	if err := os.RemoveAll(bundleDir); err != nil {
		t.Fatalf("remove bundle dir: %v", err)
	}
	resolved, err = resolveApplyBundleRoot("")
	if err != nil {
		t.Fatalf("resolve cwd candidate: %v", err)
	}
	if resolved != root {
		t.Fatalf("expected cwd bundle root, got %s", resolved)
	}
}

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
	bundle := filepath.Join(root, "bundle")
	if err := os.MkdirAll(filepath.Join(bundle, "workflows"), 0o755); err != nil {
		t.Fatalf("mkdir workflows: %v", err)
	}
	workflowPath := filepath.Join(root, "apply.yaml")
	workflowBody := "role: apply\nversion: v1alpha1\nphases:\n  - name: install\n    steps:\n      - id: dry-run-step\n        kind: RunCommand\n        spec:\n          command: [\"true\"]\n"
	if err := os.WriteFile(workflowPath, []byte(workflowBody), 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	cmd := exec.Command(binaryPath,
		"apply",
		"--file", workflowPath,
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
	root := t.TempDir()
	bundlePath := filepath.Join(t.TempDir(), "site-release.tar")
	writeSiteReleaseBundleTarFixture(t, bundlePath)

	if _, err := runWithCapturedStdout([]string{"site", "release", "import", "--root", root, "--id", "release-1", "--bundle", bundlePath}); err != nil {
		t.Fatalf("site release import failed: %v", err)
	}

	out, err := runWithCapturedStdout([]string{"site", "session", "create", "--root", root, "--id", "help", "--release", "release-1"})
	if err != nil {
		t.Fatalf("expected session create success, got %v", err)
	}
	if !strings.Contains(out, "site session create: ok (session=help release=release-1)") {
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
	res := runDeckBinary(t, binaryPath, "serve", "--bogus")
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
		{name: "help bundle", args: []string{"help", "bundle"}, wantStdout: "deck bundle [command]"},
		{name: "nested site help", args: []string{"site", "release", "--help"}, wantStdout: "deck site release [command]"},
		{name: "nested node help", args: []string{"node", "id", "--help"}, wantStdout: "deck node id [command]"},
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

func TestCLIContractGroupedParentsRejectUnknownSubcommandsViaBinary(t *testing.T) {
	binaryPath := buildDeckBinary(t, "deck-subcmd-bin")

	tests := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{name: "bundle", args: []string{"bundle", "wat"}, wantStderr: "Error: unknown command \"wat\" for \"deck bundle\""},
		{name: "cache", args: []string{"cache", "wat"}, wantStderr: "Error: unknown command \"wat\" for \"deck cache\""},
		{name: "node", args: []string{"node", "id", "wat"}, wantStderr: "Error: unknown command \"wat\" for \"deck node id\""},
		{name: "site", args: []string{"site", "release", "wat"}, wantStderr: "Error: unknown command \"wat\" for \"deck site release\""},
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

func TestCLIContractBundleInspectAllowsPositionalBeforeFlagViaBinary(t *testing.T) {
	binaryPath := buildDeckBinary(t, "deck-positional-bin")
	archivePath := filepath.Join(t.TempDir(), "bundle.tar")
	writeBundleTarFixture(t, archivePath)

	res := runDeckBinary(t, binaryPath, "bundle", "inspect", archivePath, "-o", "json")
	if res.exitCode != 0 {
		t.Fatalf("expected zero exit, got %d stderr=%q", res.exitCode, res.stderr)
	}
	if res.stderr != "" {
		t.Fatalf("expected empty stderr, got %q", res.stderr)
	}
	if !strings.Contains(res.stdout, `"entries"`) || !strings.Contains(res.stdout, `"files/dummy.txt"`) {
		t.Fatalf("unexpected stdout: %q", res.stdout)
	}
}

func TestBundledApplyWorksFromBundleDir(t *testing.T) {
	binaryPath := filepath.Join(t.TempDir(), "deck-pack-bin")
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
	if err := os.MkdirAll(workflowsDir, 0o755); err != nil {
		t.Fatalf("mkdir workflows: %v", err)
	}
	seedDir := filepath.Join(tmpRoot, "seed", "files")
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		t.Fatalf("mkdir seed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(seedDir, "source.bin"), []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("write seed file: %v", err)
	}
	packPath := filepath.Join(workflowsDir, "pack.yaml")
	packBody := fmt.Sprintf(`role: pack
version: v1alpha1
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
	if err := os.WriteFile(packPath, []byte(packBody), 0o644); err != nil {
		t.Fatalf("write pack workflow: %v", err)
	}
	applyLogPath := filepath.Join(tmpRoot, "apply.log")
	applyBody := fmt.Sprintf(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: apply-step
        kind: RunCommand
        spec:
          command: ["sh", "-c", "echo hit >> %s"]
`, strings.ReplaceAll(applyLogPath, "\\", "\\\\"))
	if err := os.WriteFile(filepath.Join(workflowsDir, "apply.yaml"), []byte(applyBody), 0o644); err != nil {
		t.Fatalf("write apply workflow: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowsDir, "vars.yaml"), []byte("kubernetesVersion: v1.30.1\n"), 0o644); err != nil {
		t.Fatalf("write vars workflow: %v", err)
	}

	bundleTar := filepath.Join(tmpRoot, "bundle.tar")
	packCmd := exec.Command(binaryPath, "pack", "--out", bundleTar)
	packCmd.Dir = tmpRoot
	packCmd.Env = append(os.Environ(), "HOME="+home)
	if raw, err := packCmd.CombinedOutput(); err != nil {
		t.Fatalf("pack bundle: %v, output=%s", err, string(raw))
	}

	extractCmd := exec.Command("tar", "xf", bundleTar, "-C", tmpRoot)
	if raw, err := extractCmd.CombinedOutput(); err != nil {
		t.Fatalf("extract bundle tar: %v, output=%s", err, string(raw))
	}

	bundleDir := filepath.Join(tmpRoot, "bundle")
	applyCmd1 := exec.Command("./deck", "apply")
	applyCmd1.Dir = bundleDir
	applyCmd1.Env = append(os.Environ(), "HOME="+home)
	if raw, err := applyCmd1.CombinedOutput(); err != nil {
		t.Fatalf("bundled apply first run: %v, output=%s", err, string(raw))
	}

	applyCmd2 := exec.Command("./deck", "apply")
	applyCmd2.Dir = bundleDir
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

func TestSiteReleaseImportList(t *testing.T) {
	root := t.TempDir()
	bundlePath := filepath.Join(t.TempDir(), "site-release.tar")
	writeSiteReleaseBundleTarFixture(t, bundlePath)

	out, err := runWithCapturedStdout([]string{"site", "release", "import", "--root", root, "--id", "release-1", "--bundle", bundlePath})
	if err != nil {
		t.Fatalf("site release import failed: %v", err)
	}
	if !strings.Contains(out, "site release import: ok") {
		t.Fatalf("unexpected import output: %q", out)
	}

	listOut, err := runWithCapturedStdout([]string{"site", "release", "list", "--root", root})
	if err != nil {
		t.Fatalf("site release list failed: %v", err)
	}
	if !strings.Contains(listOut, "release-1") {
		t.Fatalf("expected imported release in list output, got %q", listOut)
	}
}

func TestSiteSessionCreateClose(t *testing.T) {
	root := t.TempDir()
	bundlePath := filepath.Join(t.TempDir(), "site-release.tar")
	writeSiteReleaseBundleTarFixture(t, bundlePath)

	if _, err := runWithCapturedStdout([]string{"site", "release", "import", "--root", root, "--id", "release-1", "--bundle", bundlePath}); err != nil {
		t.Fatalf("site release import failed: %v", err)
	}

	createOut, err := runWithCapturedStdout([]string{"site", "session", "create", "--root", root, "--id", "session-1", "--release", "release-1"})
	if err != nil {
		t.Fatalf("site session create failed: %v", err)
	}
	if !strings.Contains(createOut, "site session create: ok") {
		t.Fatalf("unexpected create output: %q", createOut)
	}

	closeOut, err := runWithCapturedStdout([]string{"site", "session", "close", "--root", root, "--id", "session-1"})
	if err != nil {
		t.Fatalf("site session close failed: %v", err)
	}
	if !strings.Contains(closeOut, "status=closed") {
		t.Fatalf("expected closed status output, got %q", closeOut)
	}
}

func TestSiteAssignRoleNode(t *testing.T) {
	root := t.TempDir()
	bundlePath := filepath.Join(t.TempDir(), "site-release.tar")
	writeSiteReleaseBundleTarFixture(t, bundlePath)

	if _, err := runWithCapturedStdout([]string{"site", "release", "import", "--root", root, "--id", "release-1", "--bundle", bundlePath}); err != nil {
		t.Fatalf("site release import failed: %v", err)
	}
	if _, err := runWithCapturedStdout([]string{"site", "session", "create", "--root", root, "--id", "session-1", "--release", "release-1"}); err != nil {
		t.Fatalf("site session create failed: %v", err)
	}

	roleOut, err := runWithCapturedStdout([]string{"site", "assign", "role", "--root", root, "--session", "session-1", "--assignment", "assign-role", "--role", "apply", "--workflow", "workflows/apply.yaml"})
	if err != nil {
		t.Fatalf("site assign role failed: %v", err)
	}
	if !strings.Contains(roleOut, "site assign role: ok") {
		t.Fatalf("unexpected role assignment output: %q", roleOut)
	}

	nodeOut, err := runWithCapturedStdout([]string{"site", "assign", "node", "--root", root, "--session", "session-1", "--assignment", "assign-node", "--node", "node-1", "--role", "apply", "--workflow", "workflows/apply.yaml"})
	if err != nil {
		t.Fatalf("site assign node failed: %v", err)
	}
	if !strings.Contains(nodeOut, "site assign node: ok") {
		t.Fatalf("unexpected node assignment output: %q", nodeOut)
	}

	statusOut, err := runWithCapturedStdout([]string{"site", "status", "--root", root})
	if err != nil {
		t.Fatalf("site status failed: %v", err)
	}
	if !strings.Contains(statusOut, "node node-1") || !strings.Contains(statusOut, "apply=not-run") {
		t.Fatalf("unexpected site status output: %q", statusOut)
	}
}

func TestSiteAssignRejectsUnknownSession(t *testing.T) {
	root := t.TempDir()
	bundlePath := filepath.Join(t.TempDir(), "site-release.tar")
	writeSiteReleaseBundleTarFixture(t, bundlePath)

	if _, err := runWithCapturedStdout([]string{"site", "release", "import", "--root", root, "--id", "release-1", "--bundle", bundlePath}); err != nil {
		t.Fatalf("site release import failed: %v", err)
	}

	_, err := runWithCapturedStdout([]string{"site", "assign", "role", "--root", root, "--session", "missing-session", "--assignment", "assign-role", "--role", "apply", "--workflow", "workflows/apply.yaml"})
	if err == nil {
		t.Fatalf("expected unknown session error")
	}
	if !strings.Contains(err.Error(), "session \"missing-session\" not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSiteReleaseImportRejectsInvalidBundle(t *testing.T) {
	root := t.TempDir()
	invalidPath := filepath.Join(t.TempDir(), "invalid.tar")
	if err := os.WriteFile(invalidPath, []byte("not-a-tar"), 0o644); err != nil {
		t.Fatalf("write invalid bundle file: %v", err)
	}

	_, err := runWithCapturedStdout([]string{"site", "release", "import", "--root", root, "--id", "release-1", "--bundle", invalidPath})
	if err == nil {
		t.Fatalf("expected invalid bundle rejection")
	}
	if !strings.Contains(err.Error(), "site release import") {
		t.Fatalf("expected site release import error context, got %v", err)
	}
}

func runWithCapturedStdout(args []string) (string, error) {
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
	path := filepath.Join(t.TempDir(), "install-true.yaml")
	content := "role: apply\nversion: v1alpha1\nphases:\n  - name: install\n    steps:\n      - id: run-true\n        kind: RunCommand\n        spec:\n          command: [\"true\"]\n"
	writeWorkflowYAML(t, path, content)
	return path
}

func writeValidateWorkflowFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "validate-workflow.yaml")
	content := "role: apply\nversion: v1alpha1\nphases:\n  - name: install\n    steps:\n      - id: validate-run\n        apiVersion: deck/v1alpha1\n        kind: RunCommand\n        spec:\n          command: [\"true\"]\n"
	writeWorkflowYAML(t, path, content)
	return path
}

func createValidBundleManifest(t *testing.T, bundleRoot string) {
	t.Helper()
	artifactRel := "files/dummy.txt"
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

func tarEntryNamesFromFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer closeSilently(f)
	tr := tar.NewReader(f)
	names := make([]string, 0)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		names = append(names, hdr.Name)
	}
	return names, nil
}

func sliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func writeBundleTarFixture(t *testing.T, archivePath string) {
	t.Helper()
	entries := []struct {
		path string
		body []byte
	}{
		{path: "files/dummy.txt", body: []byte("ok\n")},
		{path: "files/deck", body: []byte("deck-binary\n")},
	}
	manifestEntries := make([]string, 0, len(entries))
	for _, entry := range entries {
		sum := sha256.Sum256(entry.body)
		manifestEntries = append(manifestEntries, fmt.Sprintf("    {\"path\": %q, \"sha256\": %q, \"size\": %d}", entry.path, hex.EncodeToString(sum[:]), len(entry.body)))
	}
	manifest := fmt.Sprintf("{\n  \"entries\": [\n%s\n  ]\n}\n", strings.Join(manifestEntries, ",\n"))

	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer closeSilently(f)

	tw := tar.NewWriter(f)
	defer closeSilently(tw)

	for _, entry := range []struct {
		name string
		body []byte
	}{
		{name: "bundle/.deck/manifest.json", body: []byte(manifest)},
		{name: "bundle/files/dummy.txt", body: entries[0].body},
		{name: "bundle/files/deck", body: entries[1].body},
	} {
		h := &tar.Header{Name: entry.name, Mode: 0o644, Size: int64(len(entry.body)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatalf("write tar header %s: %v", entry.name, err)
		}
		if _, err := tw.Write(entry.body); err != nil {
			t.Fatalf("write tar body %s: %v", entry.name, err)
		}
	}
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
		{name: "bundle/workflows/apply.yaml", body: []byte("role: apply\nversion: v1alpha1\nsteps: []\n"), mode: 0o644},
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

func writeSiteReleaseBundleTarFixture(t *testing.T, archivePath string) {
	t.Helper()
	workflowBody := []byte("role: apply\nversion: v1alpha1\nsteps: []\n")
	workflowSum := sha256.Sum256(workflowBody)
	manifest := fmt.Sprintf("{\n  \"entries\": [\n    {\"path\": %q, \"sha256\": %q, \"size\": %d}\n  ]\n}\n", "workflows/apply.yaml", hex.EncodeToString(workflowSum[:]), len(workflowBody))

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
		{name: "bundle/.deck/", mode: 0o755},
		{name: "bundle/.deck/manifest.json", body: []byte(manifest), mode: 0o644},
		{name: "bundle/workflows/", mode: 0o755},
		{name: "bundle/workflows/apply.yaml", body: workflowBody, mode: 0o644},
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

func TestList(t *testing.T) {
	items := []string{"workflows/pack.yaml", "workflows/apply.yaml"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/workflows/index.json" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(items)
	}))
	defer srv.Close()

	t.Run("text", func(t *testing.T) {
		out, err := runWithCapturedStdout([]string{"list", "--server", srv.URL})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		expected := strings.Join(items, "\n") + "\n"
		if out != expected {
			t.Fatalf("unexpected output\nwant: %q\ngot : %q", expected, out)
		}
	})

	t.Run("json", func(t *testing.T) {
		out, err := runWithCapturedStdout([]string{"list", "--server", srv.URL, "-o", "json"})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		var got []string
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("decode json output: %v\nraw: %q", err, out)
		}
		if !reflect.DeepEqual(got, items) {
			t.Fatalf("unexpected items\nwant: %#v\ngot : %#v", items, got)
		}
	})

	t.Run("without --server lists local workflows", func(t *testing.T) {
		root := t.TempDir()
		workflowsDir := filepath.Join(root, "workflows", "nested")
		if err := os.MkdirAll(workflowsDir, 0o755); err != nil {
			t.Fatalf("mkdir workflows: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "workflows", "apply.yaml"), []byte("role: apply\n"), 0o644); err != nil {
			t.Fatalf("write apply.yaml: %v", err)
		}
		if err := os.WriteFile(filepath.Join(workflowsDir, "post.yml"), []byte("role: apply\n"), 0o644); err != nil {
			t.Fatalf("write nested workflow: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "workflows", "README.md"), []byte("ignore"), 0o644); err != nil {
			t.Fatalf("write README.md: %v", err)
		}

		originalCWD, err := os.Getwd()
		if err != nil {
			t.Fatalf("getwd: %v", err)
		}
		if err := os.Chdir(root); err != nil {
			t.Fatalf("chdir root: %v", err)
		}
		t.Cleanup(func() {
			_ = os.Chdir(originalCWD)
		})

		out, err := runWithCapturedStdout([]string{"list"})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		expected := "workflows/apply.yaml\nworkflows/nested/post.yml\n"
		if out != expected {
			t.Fatalf("unexpected output\nwant: %q\ngot : %q", expected, out)
		}
	})

	t.Run("server 404 index returns empty list", func(t *testing.T) {
		missing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		defer missing.Close()

		textOut, err := runWithCapturedStdout([]string{"list", "--server", missing.URL})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if textOut != "" {
			t.Fatalf("expected empty text output, got %q", textOut)
		}

		jsonOut, err := runWithCapturedStdout([]string{"list", "--server", missing.URL, "-o", "json"})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		var got []string
		if err := json.Unmarshal([]byte(jsonOut), &got); err != nil {
			t.Fatalf("decode json output: %v\nraw: %q", err, jsonOut)
		}
		if len(got) != 0 {
			t.Fatalf("expected empty json list, got %#v", got)
		}
	})
}
