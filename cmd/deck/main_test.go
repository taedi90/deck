package main

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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
			err := run(tc.args)
			if err == nil {
				t.Fatalf("expected usage error")
			}

			msg := err.Error()
			for _, cmd := range []string{"pack", "apply", "serve", "bundle", "list", "validate", "diff", "init", "doctor", "health", "logs", "cache", "service"} {
				if !strings.Contains(msg, cmd) {
					t.Fatalf("usage must include %q, got %q", cmd, msg)
				}
			}
			for _, legacy := range []string{"strategy", "control", "workflow"} {
				if strings.Contains(msg, legacy) {
					t.Fatalf("usage must not include legacy namespace %q, got %q", legacy, msg)
				}
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
		err := run([]string{"cache"})
		if err == nil {
			t.Fatalf("expected usage error")
		}
		if !strings.Contains(err.Error(), "usage: deck cache list|clean") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
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
}

func TestService(t *testing.T) {
	t.Run("usage lists install status stop only", func(t *testing.T) {
		err := run([]string{"service"})
		if err == nil {
			t.Fatalf("expected usage error")
		}
		if !strings.Contains(err.Error(), "usage: deck service <install|status|stop>") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("install writes deck-server.service with serve execstart", func(t *testing.T) {
		outDir := t.TempDir()
		stdout, err := runWithCapturedStdout([]string{"service", "install", "--out", outDir, "--root", "/var/lib/deck/bundle", "--addr", ":18080"})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		unitPath := filepath.Join(outDir, "deck-server.service")
		if !strings.Contains(stdout, unitPath) {
			t.Fatalf("expected output path in stdout, got %q", stdout)
		}
		raw, readErr := os.ReadFile(unitPath)
		if readErr != nil {
			t.Fatalf("read service unit: %v", readErr)
		}
		if !strings.Contains(string(raw), "ExecStart=deck serve --root /var/lib/deck/bundle --addr :18080") {
			t.Fatalf("unexpected unit content: %q", string(raw))
		}
	})

	t.Run("status and stop are server-only", func(t *testing.T) {
		installFakeSystemctl(t, `#!/bin/sh
if [ "$1" = "is-active" ] && [ "$2" = "deck-server.service" ]; then
  echo active
  exit 0
fi
if [ "$1" = "stop" ] && [ "$2" = "deck-server.service" ]; then
  exit 0
fi
exit 1
`)

		statusOut, err := runWithCapturedStdout([]string{"service", "status"})
		if err != nil {
			t.Fatalf("status should succeed: %v", err)
		}
		if statusOut != "service status: active (deck-server.service)\n" {
			t.Fatalf("unexpected status output: %q", statusOut)
		}

		stopOut, err := runWithCapturedStdout([]string{"service", "stop"})
		if err != nil {
			t.Fatalf("stop should succeed: %v", err)
		}
		if stopOut != "service stop: ok (deck-server.service)\n" {
			t.Fatalf("unexpected stop output: %q", stopOut)
		}

		_, err = runWithCapturedStdout([]string{"service", "status", "server"})
		if err == nil || !strings.Contains(err.Error(), "usage: deck service status") {
			t.Fatalf("expected usage error for positional arg, got %v", err)
		}

		_, err = runWithCapturedStdout([]string{"service", "status", "--unknown", "server"})
		if err == nil || !strings.Contains(err.Error(), "flag provided but not defined: -unknown") {
			t.Fatalf("expected undefined flag error, got %v", err)
		}
	})
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

func TestRunControlStatusAndStopWithFakeSystemctl(t *testing.T) {
	t.Run("status maps inactive output", func(t *testing.T) {
		installFakeSystemctl(t, `#!/bin/sh
if [ "$1" = "is-active" ]; then
  echo inactive
  exit 3
fi
exit 1
`)

		out, err := runWithCapturedStdout([]string{"service", "status"})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if out != "service status: inactive (deck-server.service)\n" {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("status missing systemctl suggests one command", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())

		_, err := runWithCapturedStdout([]string{"service", "status"})
		if err == nil {
			t.Fatalf("expected error")
		}
		msg := err.Error()
		if strings.Count(msg, "suggestion:") != 1 {
			t.Fatalf("expected exactly one suggestion, got %q", msg)
		}
		if !strings.Contains(msg, "suggestion: sudo systemctl status deck-server.service") {
			t.Fatalf("unexpected suggestion: %q", msg)
		}
	})

	t.Run("stop failure suggests one command", func(t *testing.T) {
		installFakeSystemctl(t, `#!/bin/sh
if [ "$1" = "stop" ]; then
  echo permission denied
  exit 1
fi
exit 1
`)

		_, err := runWithCapturedStdout([]string{"service", "stop"})
		if err == nil {
			t.Fatalf("expected error")
		}
		msg := err.Error()
		if strings.Count(msg, "suggestion:") != 1 {
			t.Fatalf("expected exactly one suggestion, got %q", msg)
		}
		if !strings.Contains(msg, "suggestion: sudo systemctl status deck-server.service") {
			t.Fatalf("unexpected suggestion: %q", msg)
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
	err := runServer([]string{"start", "--audit-max-size-mb", "0"})
	if err == nil || !strings.Contains(err.Error(), "--audit-max-size-mb must be > 0") {
		t.Fatalf("expected audit max size validation error, got %v", err)
	}

	err = runServer([]string{"start", "--audit-max-files", "0"})
	if err == nil || !strings.Contains(err.Error(), "--audit-max-files must be > 0") {
		t.Fatalf("expected audit max files validation error, got %v", err)
	}
}

func TestRunLegacyTopLevelCommandsAreRemoved(t *testing.T) {
	for _, cmd := range []string{"run", "resume", "diagnose", "server", "agent", "workflow", "control", "strategy"} {
		t.Run(cmd, func(t *testing.T) {
			err := run([]string{cmd})
			if err == nil {
				t.Fatalf("expected unknown command error")
			}
			msg := err.Error()
			want := fmt.Sprintf("unknown command %q", cmd)
			if msg != want {
				t.Fatalf("unexpected error\nwant: %q\ngot : %q", want, msg)
			}
		})
	}
}

func TestRunWorkflowValidateAndLegacyValidateMigration(t *testing.T) {
	wf := filepath.Join("..", "..", "cluster.yaml")

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

	t.Run("legacy workflow namespace is removed", func(t *testing.T) {
		err := run([]string{"workflow", "validate", "-f", wf})
		if err == nil {
			t.Fatalf("expected unknown command error")
		}
		if err.Error() != `unknown command "workflow"` {
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

func TestRunWorkflowRunInstallLocalSuccess(t *testing.T) {
	wf := filepath.Join("..", "..", "testdata", "workflows", "install-true.yaml")
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

	t.Run("template is required", func(t *testing.T) {
		_, err := runWithCapturedStdout([]string{"init"})
		if err == nil || !strings.Contains(err.Error(), "--template is required") {
			t.Fatalf("expected template-required error, got %v", err)
		}
	})

	t.Run("template accepts only single or multi", func(t *testing.T) {
		_, err := runWithCapturedStdout([]string{"init", "--template", "multinode"})
		if err == nil || !strings.Contains(err.Error(), "single or multi") {
			t.Fatalf("expected template validation error, got %v", err)
		}
	})

	t.Run("single template creates starter set under --out workflows", func(t *testing.T) {
		outputDir := t.TempDir()
		if _, err := runWithCapturedStdout([]string{"init", "--template", "single", "--out", outputDir}); err != nil {
			t.Fatalf("init single failed: %v", err)
		}
		assertWorkflowSet(t, outputDir, "{}\n")
	})

	t.Run("multi template creates starter set under default --out .", func(t *testing.T) {
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

		if _, err := runWithCapturedStdout([]string{"init", "--template", "multi"}); err != nil {
			t.Fatalf("init multi failed: %v", err)
		}
		assertWorkflowSet(t, root, "nodes: []\n")
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

		_, err := runWithCapturedStdout([]string{"init", "--template", "single", "--out", outputDir})
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

		_, err := runWithCapturedStdout([]string{"init", "--template", "single", "--out", outputDir})
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

	wf := filepath.Join("..", "..", "testdata", "workflows", "install-true.yaml")

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

	wf, err := config.Load(workflowPath)
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

		wf1, err := config.Load(workflowURL)
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

		wf2, err := config.Load(workflowURL)
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

func TestDiff(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	wfPath := filepath.Join(t.TempDir(), "apply.yaml")
	writeWorkflowYAML(t, wfPath, "role: apply\nversion: v1alpha1\nphases:\n  - name: install\n    steps:\n      - id: step-1\n        apiVersion: deck/v1alpha1\n        kind: RunCommand\n        spec:\n          command: [\"true\"]\n")

	before, err := runWithCapturedStdout([]string{"diff", "--file", wfPath})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !strings.Contains(before, "RUN") {
		t.Fatalf("expected RUN in diff output, got %q", before)
	}

	wf, err := config.Load(wfPath)
	if err != nil {
		t.Fatalf("load workflow: %v", err)
	}
	execWf, err := buildApplyExecutionWorkflow(wf, "install")
	if err != nil {
		t.Fatalf("build execution workflow: %v", err)
	}
	if err := install.Run(execWf, install.RunOptions{BundleRoot: ""}); err != nil {
		t.Fatalf("install run: %v", err)
	}

	after, err := runWithCapturedStdout([]string{"diff", "--file", wfPath})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if before == after {
		t.Fatalf("expected diff output to change after apply run")
	}
	if !strings.Contains(after, "SKIP") {
		t.Fatalf("expected SKIP in diff output after apply run, got %q", after)
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

func runWithCapturedOutput(args []string) (string, string, error) {
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return "", "", err
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		return "", "", err
	}

	os.Stdout = stdoutW
	os.Stderr = stderrW

	runErr := run(args)

	_ = stdoutW.Close()
	_ = stderrW.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	stdoutRaw, stdoutReadErr := io.ReadAll(stdoutR)
	_ = stdoutR.Close()
	if stdoutReadErr != nil {
		_ = stderrR.Close()
		return "", "", stdoutReadErr
	}
	stderrRaw, stderrReadErr := io.ReadAll(stderrR)
	_ = stderrR.Close()
	if stderrReadErr != nil {
		return "", "", stderrReadErr
	}

	return string(stdoutRaw), string(stderrRaw), runErr
}

func writeWorkflowYAML(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write workflow yaml: %v", err)
	}
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
	defer f.Close()
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
	content := []byte("ok\n")
	sum := sha256.Sum256(content)
	manifest := fmt.Sprintf("{\n  \"entries\": [\n    {\"path\": %q, \"sha256\": %q, \"size\": %d}\n  ]\n}\n", "files/dummy.txt", hex.EncodeToString(sum[:]), len(content))

	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer f.Close()

	tw := tar.NewWriter(f)
	defer tw.Close()

	for _, entry := range []struct {
		name string
		body []byte
	}{
		{name: "bundle/.deck/manifest.json", body: []byte(manifest)},
		{name: "bundle/files/dummy.txt", body: content},
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
	defer f.Close()

	tw := tar.NewWriter(f)
	defer tw.Close()

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

func writeDoctorWorkflowFile(t *testing.T, includePrepare bool) string {
	t.Helper()
	prepareSection := ""
	if includePrepare {
		prepareSection = `  - name: prepare
    steps:
      - id: prepare-sample
        apiVersion: deck/v1alpha1
        kind: DownloadPackages
        spec:
          packages: [containerd]
`
	}
	content := fmt.Sprintf("role: apply\nversion: v1alpha1\nphases:\n%s  - name: install\n    steps:\n      - id: install-sample\n        apiVersion: deck/v1alpha1\n        kind: RunCommand\n        spec:\n          command: [\"true\"]\n", prepareSection)

	wfPath := filepath.Join(t.TempDir(), "workflow.yaml")
	if err := os.WriteFile(wfPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}
	return wfPath
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
}

func installFakeSystemctl(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	systemctlPath := filepath.Join(dir, "systemctl")
	if err := os.WriteFile(systemctlPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake systemctl: %v", err)
	}
	t.Setenv("PATH", dir)
}
