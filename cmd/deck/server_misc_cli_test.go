package main

import (
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

func TestServerDefaultCommands(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "server.json")
	t.Setenv("DECK_SERVER_CONFIG_PATH", configPath)

	out, err := runWithCapturedStdout([]string{"server", "show"})
	if err != nil {
		t.Fatalf("server show failed: %v", err)
	}
	if out != "server=\nsource=none\n" {
		t.Fatalf("unexpected empty server show output: %q", out)
	}

	out, err = runWithCapturedStdout([]string{"server", "set", "http://127.0.0.1:8080/"})
	if err != nil {
		t.Fatalf("server set failed: %v", err)
	}
	if out != "server default set: http://127.0.0.1:8080\n" {
		t.Fatalf("unexpected server set output: %q", out)
	}

	out, err = runWithCapturedStdout([]string{"server", "show"})
	if err != nil {
		t.Fatalf("server show after set failed: %v", err)
	}
	if out != "server=http://127.0.0.1:8080\nsource=config\n" {
		t.Fatalf("unexpected saved server show output: %q", out)
	}

	out, err = runWithCapturedStdout([]string{"server", "unset"})
	if err != nil {
		t.Fatalf("server unset failed: %v", err)
	}
	if out != "server default cleared\n" {
		t.Fatalf("unexpected server unset output: %q", out)
	}
	if _, statErr := os.Stat(configPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected config file removal, got %v", statErr)
	}
}

func TestServerDefaultsReadLegacyHomePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	legacyPath := filepath.Join(home, ".deck", "server.json")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatalf("mkdir legacy config dir: %v", err)
	}
	if err := os.WriteFile(legacyPath, []byte("{\n  \"url\": \"http://127.0.0.1:9090\"\n}\n"), 0o600); err != nil {
		t.Fatalf("write legacy server defaults: %v", err)
	}

	resolved, source, err := resolveServerURL("")
	if err != nil {
		t.Fatalf("resolveServerURL failed: %v", err)
	}
	if resolved != "http://127.0.0.1:9090" || source != "config" {
		t.Fatalf("unexpected legacy server defaults resolution: resolved=%q source=%q", resolved, source)
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

	if _, err := runWithCapturedStdout([]string{"server", "set", srv.URL}); err != nil {
		t.Fatalf("server set failed: %v", err)
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
	for _, cmd := range []string{"run", "resume", "diagnose", "agent", "workflow", "control", "strategy", "source", "service", "serve", "health", "logs"} {
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

func TestRunWorkflowLintAndLegacyValidateMigration(t *testing.T) {
	wf := writeValidateWorkflowFixture(t)

	t.Run("lint with -f", func(t *testing.T) {
		out, err := runWithCapturedStdout([]string{"lint", "-f", wf})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if out != fmt.Sprintf("lint: ok (%s)\n", wf) {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("lint with --file", func(t *testing.T) {
		out, err := runWithCapturedStdout([]string{"lint", "--file", wf})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if out != fmt.Sprintf("lint: ok (%s)\n", wf) {
			t.Fatalf("unexpected output: %q", out)
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
		if err := os.WriteFile(filepath.Join(root, "workflows", "scenarios", "prepare.yaml"), []byte("role: prepare\nversion: v1alpha1\nphases:\n  - name: prepare\n    steps: []\n"), 0o644); err != nil {
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
		if out != "lint: ok (1 workflows)\n" {
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
		preparePath := filepath.Join(root, "workflows", "scenarios", "prepare.yaml")
		if err := os.WriteFile(preparePath, []byte("role: prepare\nversion: v1alpha1\nphases:\n  - name: prepare\n    steps: []\n"), 0o644); err != nil {
			t.Fatalf("write prepare: %v", err)
		}

		out, err := runWithCapturedStdout([]string{"lint", "prepare", "--root", root})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if out != "lint: ok (1 workflows)\n" {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("lint rejects component entrypoints", func(t *testing.T) {
		root := t.TempDir()
		componentPath := filepath.Join(root, "workflows", "components", "shared.yaml")
		if err := os.MkdirAll(filepath.Dir(componentPath), 0o755); err != nil {
			t.Fatalf("mkdir component dir: %v", err)
		}
		if err := os.WriteFile(componentPath, []byte("role: apply\nversion: v1alpha1\nsteps: []\n"), 0o644); err != nil {
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
