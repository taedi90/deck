package main

import (
	"bytes"
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
	"strings"
	"testing"
	"time"

	"github.com/taedi90/deck/internal/strategycfg"
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
			if !strings.Contains(msg, "deck strategy") {
				t.Fatalf("usage must include strategy axis, got %q", msg)
			}
			if !strings.Contains(msg, "deck control") {
				t.Fatalf("usage must include control axis, got %q", msg)
			}
			if !strings.Contains(msg, "deck workflow") {
				t.Fatalf("usage must include workflow axis, got %q", msg)
			}
			for _, legacy := range []string{"apply", "run", "resume", "validate", "bundle", "diagnose", "server", "agent"} {
				if strings.Contains(msg, "deck "+legacy) {
					t.Fatalf("usage must not include legacy command %q, got %q", legacy, msg)
				}
			}
		})
	}
}

func TestRunControlWorkflowStubUsage(t *testing.T) {
	t.Run("control usage", func(t *testing.T) {
		err := run([]string{"control"})
		if err == nil {
			t.Fatalf("expected usage error")
		}
		if !strings.Contains(err.Error(), "usage: deck control <subcommand>") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("workflow usage", func(t *testing.T) {
		err := run([]string{"workflow"})
		if err == nil {
			t.Fatalf("expected usage error")
		}
		if !strings.Contains(err.Error(), "usage: deck workflow <subcommand>") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestRunControlHealth(t *testing.T) {
	t.Run("uses --server when provided", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/health" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}))
		defer srv.Close()

		out, err := runWithCapturedStdout([]string{"control", "health", "--server", srv.URL})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		expected := fmt.Sprintf("control health: ok (%s)\n", srv.URL)
		if out != expected {
			t.Fatalf("unexpected output\nwant: %q\ngot : %q", expected, out)
		}
	})

	t.Run("falls back to strategy config server url", func(t *testing.T) {
		setupStrategyConfigEnv(t)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/health" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}))
		defer srv.Close()

		if _, err := runWithCapturedStdout([]string{"strategy", "use", "server", "--server", srv.URL}); err != nil {
			t.Fatalf("strategy use server failed: %v", err)
		}

		out, err := runWithCapturedStdout([]string{"control", "health"})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		expected := fmt.Sprintf("control health: ok (%s)\n", srv.URL)
		if out != expected {
			t.Fatalf("unexpected output\nwant: %q\ngot : %q", expected, out)
		}
	})
}

func TestRunControlStartAgentUsesConfigServerWhenOmitted(t *testing.T) {
	setupStrategyConfigEnv(t)
	heartbeatCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/agent/heartbeat":
			heartbeatCalls++
			w.WriteHeader(http.StatusOK)
		case "/api/agent/lease":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/api/agent/report":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	if _, err := runWithCapturedStdout([]string{"strategy", "use", "server", "--server", srv.URL}); err != nil {
		t.Fatalf("strategy use server failed: %v", err)
	}

	if _, err := runWithCapturedStdout([]string{"control", "start", "agent", "--once", "--interval", "1s"}); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if heartbeatCalls != 1 {
		t.Fatalf("expected one heartbeat call, got %d", heartbeatCalls)
	}
}

func TestRunControlDoctorPreflight(t *testing.T) {
	bundleDir := t.TempDir()
	createValidBundleManifest(t, bundleDir)

	t.Run("success output", func(t *testing.T) {
		wfPath := writeDoctorWorkflowFile(t, bundleDir, true)
		reportPath := filepath.Join(t.TempDir(), "doctor.json")

		out, err := runWithCapturedStdout([]string{"control", "doctor", "preflight", "--file", wfPath, "--bundle", bundleDir, "--out", reportPath})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if !strings.Contains(out, "doctor preflight: ok (") {
			t.Fatalf("expected success message, got %q", out)
		}
		expectedReportLine := fmt.Sprintf("doctor report: %s\n", reportPath)
		if !strings.Contains(out, expectedReportLine) {
			t.Fatalf("expected report path line, got %q", out)
		}
	})

	t.Run("failure still prints report path", func(t *testing.T) {
		wfPath := writeDoctorWorkflowFile(t, bundleDir, false)
		reportPath := filepath.Join(t.TempDir(), "doctor-failed.json")

		out, err := runWithCapturedStdout([]string{"control", "doctor", "preflight", "--file", wfPath, "--bundle", bundleDir, "--out", reportPath})
		if err == nil {
			t.Fatalf("expected failure")
		}
		if !strings.Contains(out, "doctor preflight: failed (") {
			t.Fatalf("expected failed message, got %q", out)
		}
		expectedReportLine := fmt.Sprintf("doctor report: %s\n", reportPath)
		if !strings.Contains(out, expectedReportLine) {
			t.Fatalf("expected report path line, got %q", out)
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

		out, err := runWithCapturedStdout([]string{"control", "status", "--type", "server"})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if out != "control status: inactive (deck-server.service)\n" {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("status json maps active output", func(t *testing.T) {
		installFakeSystemctl(t, `#!/bin/sh
if [ "$1" = "is-active" ]; then
  echo active
  exit 0
fi
exit 1
`)

		out, err := runWithCapturedStdout([]string{"control", "status", "--type", "agent", "--output", "json"})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if !strings.Contains(out, `"status":"active"`) || !strings.Contains(out, `"unit":"deck-agent.service"`) {
			t.Fatalf("unexpected json output: %q", out)
		}
	})

	t.Run("status missing systemctl suggests one command", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())

		_, err := runWithCapturedStdout([]string{"control", "status", "--type", "server"})
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

		_, err := runWithCapturedStdout([]string{"control", "stop", "--type", "agent"})
		if err == nil {
			t.Fatalf("expected error")
		}
		msg := err.Error()
		if strings.Count(msg, "suggestion:") != 1 {
			t.Fatalf("expected exactly one suggestion, got %q", msg)
		}
		if !strings.Contains(msg, "suggestion: sudo systemctl status deck-agent.service") {
			t.Fatalf("unexpected suggestion: %q", msg)
		}
	})
}

func TestControlLogsJournalFailure(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := runWithCapturedStdout([]string{"control", "logs", "--source", "journal", "--unit", "deck-server.service"})
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
}

func TestControlLogsReadsCurrentAfterRotation(t *testing.T) {
	logDir := filepath.Join(t.TempDir(), ".deck", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir log dir: %v", err)
	}
	logPath := filepath.Join(logDir, "server-audit.log")

	rotatedLine := `{"ts":"2026-03-05T12:00:00Z","schema_version":1,"source":"server","event_type":"http_request","level":"info","message":"old","job_id":"old"}` + "\n"
	currentLine := `{"ts":"2026-03-05T12:01:00Z","schema_version":1,"source":"server","event_type":"http_request","level":"info","message":"current","job_id":"current"}` + "\n"
	if err := os.WriteFile(logPath+".1", []byte(rotatedLine), 0o644); err != nil {
		t.Fatalf("write rotated log: %v", err)
	}
	if err := os.WriteFile(logPath, []byte(currentLine), 0o644); err != nil {
		t.Fatalf("write current log: %v", err)
	}

	out, err := runWithCapturedStdout([]string{"control", "logs", "--source", "file", "--path", logPath, "--tail", "200", "--output", "json"})
	if err != nil {
		t.Fatalf("run control logs: %v", err)
	}
	if !strings.Contains(out, `"job_id":"current"`) {
		t.Fatalf("expected current log entry in output, got %q", out)
	}
	if strings.Contains(out, `"job_id":"old"`) {
		t.Fatalf("expected rotated log entries excluded from output, got %q", out)
	}
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
	for _, cmd := range []string{"apply", "run", "resume", "validate", "bundle", "diagnose", "server", "agent"} {
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

	t.Run("workflow validate with -f", func(t *testing.T) {
		out, err := runWithCapturedStdout([]string{"workflow", "validate", "-f", wf})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if out != "validate: ok\n" {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("workflow validate with --file", func(t *testing.T) {
		out, err := runWithCapturedStdout([]string{"workflow", "validate", "--file", wf})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if out != "validate: ok\n" {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("legacy validate is removed", func(t *testing.T) {
		err := run([]string{"validate", "-f", wf})
		if err == nil {
			t.Fatalf("expected unknown command error")
		}
		if err.Error() != `unknown command "validate"` {
			t.Fatalf("unexpected error: %q", err.Error())
		}
	})
}

func TestRunWorkflowBundleVerifySuccess(t *testing.T) {
	bundleDir := t.TempDir()
	createValidBundleManifest(t, bundleDir)

	out, err := runWithCapturedStdout([]string{"workflow", "bundle", "verify", "--bundle", bundleDir})
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

	collectOut, err := runWithCapturedStdout([]string{"workflow", "bundle", "collect", "--bundle", bundleDir, "--output", archivePath})
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
	importOut, err := runWithCapturedStdout([]string{"workflow", "bundle", "import", "--file", archivePath, "--dest", importDest})
	if err != nil {
		t.Fatalf("expected import success, got %v", err)
	}
	expectedImport := fmt.Sprintf("bundle import: ok (%s -> %s)\n", archivePath, importDest)
	if importOut != expectedImport {
		t.Fatalf("unexpected import output\nwant: %q\ngot : %q", expectedImport, importOut)
	}

	manifestPath := filepath.Join(importDest, "manifest.json")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("expected imported manifest, got %v", err)
	}

	artifactPath := filepath.Join(importDest, "artifacts", "dummy.txt")
	artifact, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("read imported artifact: %v", err)
	}
	if string(artifact) != "ok\n" {
		t.Fatalf("unexpected imported artifact content: %q", string(artifact))
	}
}

func TestRunWorkflowRunInstallLocalSuccess(t *testing.T) {
	wf := filepath.Join("..", "..", "testdata", "workflows", "install-true.yaml")
	bundle := t.TempDir()
	createValidBundleManifest(t, bundle)

	out, err := runWithCapturedStdout([]string{"workflow", "run", "--file", wf, "--phase", "install", "--bundle", bundle})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if out != "run install: ok\n" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestRunWorkflowInitTemplates(t *testing.T) {
	singleTemplate := filepath.Join("..", "..", "docs", "examples", "vagrant-smoke-install.yaml")
	controlPlaneTemplate := filepath.Join("..", "..", "docs", "examples", "offline-k8s-control-plane.yaml")
	workerTemplate := filepath.Join("..", "..", "docs", "examples", "offline-k8s-worker.yaml")

	t.Run("single template success creates file", func(t *testing.T) {
		outputPath := filepath.Join(t.TempDir(), "single.yaml")
		if _, err := runWithCapturedStdout([]string{"workflow", "init", "--template", "single", "--output", outputPath}); err != nil {
			t.Fatalf("workflow init single failed: %v", err)
		}

		generated, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("read generated single file: %v", err)
		}
		expected, err := os.ReadFile(singleTemplate)
		if err != nil {
			t.Fatalf("read single template: %v", err)
		}
		if !bytes.Equal(generated, expected) {
			t.Fatalf("single template content mismatch")
		}
	})

	t.Run("multinode template success creates two files", func(t *testing.T) {
		outputDir := filepath.Join(t.TempDir(), "mn")
		if _, err := runWithCapturedStdout([]string{"workflow", "init", "--template", "multinode", "--output", outputDir}); err != nil {
			t.Fatalf("workflow init multinode failed: %v", err)
		}

		controlPlanePath := filepath.Join(outputDir, "control-plane.yaml")
		workerPath := filepath.Join(outputDir, "worker.yaml")

		generatedControlPlane, err := os.ReadFile(controlPlanePath)
		if err != nil {
			t.Fatalf("read generated control-plane file: %v", err)
		}
		expectedControlPlane, err := os.ReadFile(controlPlaneTemplate)
		if err != nil {
			t.Fatalf("read control-plane template: %v", err)
		}
		if !bytes.Equal(generatedControlPlane, expectedControlPlane) {
			t.Fatalf("control-plane template content mismatch")
		}

		generatedWorker, err := os.ReadFile(workerPath)
		if err != nil {
			t.Fatalf("read generated worker file: %v", err)
		}
		expectedWorker, err := os.ReadFile(workerTemplate)
		if err != nil {
			t.Fatalf("read worker template: %v", err)
		}
		if !bytes.Equal(generatedWorker, expectedWorker) {
			t.Fatalf("worker template content mismatch")
		}
	})

	t.Run("overwrite fails without force", func(t *testing.T) {
		outputPath := filepath.Join(t.TempDir(), "single.yaml")
		if err := os.WriteFile(outputPath, []byte("existing"), 0o644); err != nil {
			t.Fatalf("seed existing file: %v", err)
		}

		_, err := runWithCapturedStdout([]string{"workflow", "init", "--template", "single", "--output", outputPath})
		if err == nil {
			t.Fatalf("expected overwrite error")
		}
		msg := err.Error()
		if !strings.Contains(msg, "destination already exists") || !strings.Contains(msg, outputPath) {
			t.Fatalf("expected destination path in error, got %q", msg)
		}
	})

	t.Run("overwrite succeeds with force", func(t *testing.T) {
		outputPath := filepath.Join(t.TempDir(), "single.yaml")
		if err := os.WriteFile(outputPath, []byte("existing"), 0o644); err != nil {
			t.Fatalf("seed existing file: %v", err)
		}

		if _, err := runWithCapturedStdout([]string{"workflow", "init", "--template", "single", "--output", outputPath, "--force"}); err != nil {
			t.Fatalf("workflow init --force failed: %v", err)
		}

		generated, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("read generated single file: %v", err)
		}
		expected, err := os.ReadFile(singleTemplate)
		if err != nil {
			t.Fatalf("read single template: %v", err)
		}
		if !bytes.Equal(generated, expected) {
			t.Fatalf("single template content mismatch after force overwrite")
		}
	})
}

func TestRunWorkflowConvertSingleFile(t *testing.T) {
	inputPath := filepath.Join("..", "..", "testdata", "workflows", "v1-sample.yaml")
	outputPath := filepath.Join(t.TempDir(), "v1alpha1.yaml")

	stdout, stderr, err := runWithCapturedOutput([]string{"workflow", "convert", "--file", inputPath, "--out", outputPath})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if stderr != "" {
		t.Fatalf("expected empty stderr, got %q", stderr)
	}
	if !strings.Contains(stdout, "workflow convert: wrote "+outputPath) {
		t.Fatalf("unexpected stdout: %q", stdout)
	}
	if !strings.Contains(stdout, "workflow convert: validate ok") {
		t.Fatalf("expected validate ok output, got %q", stdout)
	}

	raw, readErr := os.ReadFile(outputPath)
	if readErr != nil {
		t.Fatalf("read output workflow: %v", readErr)
	}
	text := string(raw)
	if !strings.Contains(text, "version: v1alpha1") {
		t.Fatalf("expected converted version, got %q", text)
	}
	if !strings.Contains(text, "apiVersion: deck/v1alpha1") {
		t.Fatalf("expected converted apiVersion, got %q", text)
	}
}

func TestWorkflowConvert_RewriteImports(t *testing.T) {
	t.Run("relative imports are converted recursively", func(t *testing.T) {
		dir := t.TempDir()
		childPath := filepath.Join(dir, "child.yaml")
		rootPath := filepath.Join(dir, "root.yaml")
		outPath := filepath.Join(dir, "root.v1alpha1.yaml")

		writeWorkflowYAML(t, childPath, strings.ReplaceAll(`version: v1
context: {}
phases:
  - name: install
    steps:
      - id: child-step
        apiVersion: deck/<<V1>>
        kind: RunCommand
        spec:
          command: ["true"]
`, "<<V1>>", "v1"))

		writeWorkflowYAML(t, rootPath, strings.ReplaceAll(`version: v1
imports:
  - child.yaml
context: {}
phases:
  - name: install
    steps:
      - id: root-step
        apiVersion: deck/<<V1>>
        kind: RunCommand
        spec:
          command: ["true"]
`, "<<V1>>", "v1"))

		stdout, stderr, err := runWithCapturedOutput([]string{"workflow", "convert", "--file", rootPath, "--out", outPath})
		if err != nil {
			t.Fatalf("expected success, got %v (stdout=%q, stderr=%q)", err, stdout, stderr)
		}

		convertedRoot, readErr := os.ReadFile(outPath)
		if readErr != nil {
			t.Fatalf("read converted root: %v", readErr)
		}
		if !strings.Contains(string(convertedRoot), "version: v1alpha1") {
			t.Fatalf("expected root version conversion, got %q", string(convertedRoot))
		}

		convertedChild, childErr := os.ReadFile(childPath)
		if childErr != nil {
			t.Fatalf("read converted child: %v", childErr)
		}
		childText := string(convertedChild)
		if !strings.Contains(childText, "version: v1alpha1") || !strings.Contains(childText, "apiVersion: deck/v1alpha1") {
			t.Fatalf("expected child conversion, got %q", childText)
		}
	})

	t.Run("url imports are skipped with warning", func(t *testing.T) {
		dir := t.TempDir()
		rootPath := filepath.Join(dir, "root-url.yaml")
		outPath := filepath.Join(dir, "root-url.v1alpha1.yaml")

		writeWorkflowYAML(t, rootPath, strings.ReplaceAll(`version: v1
imports:
  - https://example.com/workflow.yaml
context: {}
phases:
  - name: install
    steps:
      - id: root-step
        apiVersion: deck/<<V1>>
        kind: RunCommand
        spec:
          command: ["true"]
`, "<<V1>>", "v1"))

		stdout, stderr, err := runWithCapturedOutput([]string{"workflow", "convert", "--file", rootPath, "--out", outPath})
		if err == nil {
			t.Fatalf("expected validation failure due to unresolved URL import")
		}
		if !strings.Contains(stderr, "warning: skip URL import") {
			t.Fatalf("expected URL warning, got %q", stderr)
		}
		if !strings.Contains(stdout, "workflow convert: wrote "+outPath) {
			t.Fatalf("expected output file write message, got %q", stdout)
		}

		if _, statErr := os.Stat(outPath); statErr != nil {
			t.Fatalf("expected converted output file to remain on validation failure: %v", statErr)
		}
	})
}

func TestRunWorkflowRunDryRunPrintsPlan(t *testing.T) {
	wf := filepath.Join("..", "..", "testdata", "workflows", "install-true.yaml")

	out, err := runWithCapturedStdout([]string{"workflow", "run", "--file", wf, "--dry-run"})
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

func TestWorkflowRunServerWait(t *testing.T) {
	originalPollInterval := workflowRunPollInterval
	workflowRunPollInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		workflowRunPollInterval = originalPollInterval
	})

	enqueued := map[string]any{}
	reportCalls := 0
	testJobID := "wf-test-id"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/agent/job":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected method for job endpoint: %s", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&enqueued); err != nil {
				t.Fatalf("decode job payload: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"accepted"}`))
		case "/api/agent/reports":
			reportCalls++
			w.Header().Set("Content-Type", "application/json")
			if reportCalls < 2 {
				_, _ = w.Write([]byte(`{"status":"ok","reports":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"status":"ok","reports":[{"job_id":"wf-test-id","status":"success","detail":"done","attempt":1,"max_attempts":3,"received_at":"2026-03-05T12:00:00Z"}]}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	args := []string{
		"workflow", "run",
		"--use", "server",
		"--server", srv.URL,
		"--file", "https://control.example/files/install.yaml",
		"--phase", "install",
		"--bundle", "/bundle-root",
		"--job-id", testJobID,
		"--target-hostname", "node-a",
		"--wait",
		"--output", "json",
	}
	out, err := runWithCapturedStdout(args)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if enqueued["id"] != testJobID {
		t.Fatalf("unexpected payload id: %v", enqueued["id"])
	}
	if enqueued["type"] != "install" {
		t.Fatalf("unexpected payload type: %v", enqueued["type"])
	}
	if enqueued["workflow_file"] != "https://control.example/files/install.yaml" {
		t.Fatalf("unexpected payload workflow_file: %v", enqueued["workflow_file"])
	}
	if enqueued["bundle_root"] != "/bundle-root" {
		t.Fatalf("unexpected payload bundle_root: %v", enqueued["bundle_root"])
	}
	if enqueued["phase"] != "install" {
		t.Fatalf("unexpected payload phase: %v", enqueued["phase"])
	}
	if enqueued["target_hostname"] != "node-a" {
		t.Fatalf("unexpected payload target_hostname: %v", enqueued["target_hostname"])
	}
	if enqueued["max_attempts"] != float64(3) {
		t.Fatalf("unexpected payload max_attempts: %v", enqueued["max_attempts"])
	}
	if enqueued["retry_delay_sec"] != float64(10) {
		t.Fatalf("unexpected payload retry_delay_sec: %v", enqueued["retry_delay_sec"])
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("unexpected output lines: %q", out)
	}
	if lines[0] != "job_id=wf-test-id" {
		t.Fatalf("expected job_id line, got %q", lines[0])
	}

	var waitResult map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &waitResult); err != nil {
		t.Fatalf("decode wait json: %v", err)
	}
	if waitResult["job_id"] != "wf-test-id" || waitResult["status"] != "success" {
		t.Fatalf("unexpected wait json: %#v", waitResult)
	}
	if reportCalls < 2 {
		t.Fatalf("expected polling to happen, calls=%d", reportCalls)
	}
}

func TestRunWorkflowRunServerRejectsNonURLFile(t *testing.T) {
	err := run([]string{"workflow", "run", "--use", "server", "--server", "http://127.0.0.1:18080", "--file", "./cluster.yaml"})
	if err == nil {
		t.Fatalf("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "URL-only") || !strings.Contains(msg, "/files/") {
		t.Fatalf("unexpected error message: %q", msg)
	}
}

func TestWorkflowRunServerTimeoutExitCodeViaBinary(t *testing.T) {
	binaryPath := filepath.Join(t.TempDir(), "deck-test-bin")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/deck")
	buildCmd.Dir = filepath.Join("..", "..")
	buildOut, buildErr := buildCmd.CombinedOutput()
	if buildErr != nil {
		t.Fatalf("build deck binary: %v, output=%s", buildErr, string(buildOut))
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/agent/job":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"accepted"}`))
		case "/api/agent/reports":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok","reports":[]}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	cmd := exec.Command(binaryPath,
		"workflow", "run",
		"--use", "server",
		"--server", srv.URL,
		"--file", "https://control.example/files/install.yaml",
		"--wait",
		"--timeout", "40ms",
		"--job-id", "wf-timeout-id",
	)
	cmd.Dir = filepath.Join("..", "..")
	raw, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit, output=%s", string(raw))
	}

	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected exit error, got %T (%v), output=%s", err, err, string(raw))
	}
	if exitErr.ExitCode() != 2 {
		t.Fatalf("expected exit code 2, got %d, output=%s", exitErr.ExitCode(), string(raw))
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

func TestRunStrategyCurrentDefault(t *testing.T) {
	configPath := setupStrategyConfigEnv(t)

	out, err := runWithCapturedStdout([]string{"strategy", "current"})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	expected := fmt.Sprintf("strategy=local source=default config=%s server_url=\n", configPath)
	if out != expected {
		t.Fatalf("unexpected output\nwant: %q\ngot : %q", expected, out)
	}
}

func TestRunStrategyCurrentEnvOverride(t *testing.T) {
	configPath := setupStrategyConfigEnv(t)
	t.Setenv("DECK_STRATEGY", "server")

	out, err := runWithCapturedStdout([]string{"strategy", "current"})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	expected := fmt.Sprintf("strategy=server source=env config=%s server_url=\n", configPath)
	if out != expected {
		t.Fatalf("unexpected output\nwant: %q\ngot : %q", expected, out)
	}
}

func TestRunStrategyCurrentConfigOverride(t *testing.T) {
	configPath := setupStrategyConfigEnv(t)

	if _, err := runWithCapturedStdout([]string{"strategy", "use", "server", "--server", "https://control.example"}); err != nil {
		t.Fatalf("expected strategy use success, got %v", err)
	}

	out, err := runWithCapturedStdout([]string{"strategy", "current"})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	expected := fmt.Sprintf("strategy=server source=config config=%s server_url=https://control.example\n", configPath)
	if out != expected {
		t.Fatalf("unexpected output\nwant: %q\ngot : %q", expected, out)
	}
}

func TestRunStrategyCurrentInvalidEnvValue(t *testing.T) {
	setupStrategyConfigEnv(t)
	t.Setenv("DECK_STRATEGY", "invalid")

	_, err := runWithCapturedStdout([]string{"strategy", "current"})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "invalid DECK_STRATEGY value") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunStrategyCurrentInvalidYAMLConfig(t *testing.T) {
	configPath := setupStrategyConfigEnv(t)
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("strategy: ["), 0o644); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}

	_, err := runWithCapturedStdout([]string{"strategy", "current"})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), configPath) {
		t.Fatalf("expected error to include config path, got %v", err)
	}
	if !strings.Contains(err.Error(), "remediation: rm -f "+configPath) {
		t.Fatalf("expected remediation command, got %v", err)
	}
}

func setupStrategyConfigEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	xdgConfigHome := filepath.Join(home, "xdg-config")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdgConfigHome)
	t.Setenv("DECK_STRATEGY", "")

	configPath, err := strategycfg.ConfigPath()
	if err != nil {
		t.Fatalf("resolve config path: %v", err)
	}
	return configPath
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
	artifactRel := "artifacts/dummy.txt"
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
	if err := os.WriteFile(filepath.Join(bundleRoot, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func writeDoctorWorkflowFile(t *testing.T, bundleDir string, includePrepare bool) string {
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
	content := fmt.Sprintf(`version: v1alpha1
context:
  stateFile: %s/.deck/state.json
phases:
%s  - name: install
    steps:
      - id: install-sample
        apiVersion: deck/v1alpha1
        kind: RunCommand
        spec:
          command: ["true"]
`, bundleDir, prepareSection)

	wfPath := filepath.Join(t.TempDir(), "workflow.yaml")
	if err := os.WriteFile(wfPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}
	return wfPath
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
