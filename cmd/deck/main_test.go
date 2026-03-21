package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/taedi90/deck/internal/buildinfo"
)

func TestVersionCommand(t *testing.T) {
	originalVersion := buildinfo.Version
	originalCommit := buildinfo.Commit
	originalDate := buildinfo.Date
	originalVariant := buildinfo.Variant
	originalDirty := buildinfo.Dirty
	t.Cleanup(func() {
		buildinfo.Version = originalVersion
		buildinfo.Commit = originalCommit
		buildinfo.Date = originalDate
		buildinfo.Variant = originalVariant
		buildinfo.Dirty = originalDirty
	})

	buildinfo.Version = "v0.1.0"
	buildinfo.Commit = "abc1234"
	buildinfo.Date = "2026-03-17T10:00:00Z"
	buildinfo.Variant = "core"
	buildinfo.Dirty = "true"

	out, err := runWithCapturedStdout([]string{"version"})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if out != "deck v0.1.0\n" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestVersionCommandJSON(t *testing.T) {
	originalVersion := buildinfo.Version
	originalCommit := buildinfo.Commit
	originalDate := buildinfo.Date
	originalVariant := buildinfo.Variant
	originalDirty := buildinfo.Dirty
	t.Cleanup(func() {
		buildinfo.Version = originalVersion
		buildinfo.Commit = originalCommit
		buildinfo.Date = originalDate
		buildinfo.Variant = originalVariant
		buildinfo.Dirty = originalDirty
	})

	buildinfo.Version = "dev"
	buildinfo.Commit = "abc1234"
	buildinfo.Date = "2026-03-17T10:00:00Z"
	buildinfo.Variant = "ai"
	buildinfo.Dirty = "false"

	out, err := runWithCapturedStdout([]string{"version", "-o", "json"})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("parse json output: %v output=%q", err, out)
	}
	if payload["name"] != "deck" {
		t.Fatalf("unexpected name: %#v", payload["name"])
	}
	if payload["version"] != "dev" {
		t.Fatalf("unexpected version: %#v", payload["version"])
	}
	if payload["commit"] != "abc1234" {
		t.Fatalf("unexpected commit: %#v", payload["commit"])
	}
	if payload["date"] != "2026-03-17T10:00:00Z" {
		t.Fatalf("unexpected date: %#v", payload["date"])
	}
	if payload["variant"] != "ai" {
		t.Fatalf("unexpected variant: %#v", payload["variant"])
	}
	dirty, ok := payload["dirty"].(bool)
	if !ok || dirty {
		t.Fatalf("unexpected dirty value: %#v", payload["dirty"])
	}
}

func TestRunWorkflowRunInstallLocalSuccess(t *testing.T) {
	wf := writeInstallTrueWorkflowFixture(t)
	bundle := t.TempDir()
	createValidBundleManifest(t, bundle)
	if err := os.MkdirAll(filepath.Join(bundle, "workflows"), 0o755); err != nil {
		t.Fatalf("mkdir bundle workflows: %v", err)
	}

	out, err := runWithCapturedStdout([]string{"apply", "--workflow", wf, bundle})
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
			filepath.Join("workflows", "vars.yaml"): wantVars,
			filepath.Join("workflows", "prepare.yaml"): strings.Join([]string{
				"version: v1alpha1",
				"phases:",
				"  - name: prepare",
				"    steps: []",
				"",
			}, "\n"),
			filepath.Join("workflows", "scenarios", "apply.yaml"): strings.Join([]string{
				"version: v1alpha1",
				"phases:",
				"  - name: install",
				"    imports:",
				"      - path: example-apply.yaml",
				"",
			}, "\n"),
			filepath.Join("workflows", "components", "example-apply.yaml"): "steps: []\n",
			".gitignore":  strings.Join([]string{"/.deck/", "/deck", "/outputs/", "*.tar", ""}, "\n"),
			".deckignore": strings.Join([]string{".git/", ".gitignore", ".deckignore", "/*.tar", ""}, "\n"),
			filepath.Join("outputs", "files", ".keep"):    "",
			filepath.Join("outputs", "images", ".keep"):   "",
			filepath.Join("outputs", "packages", ".keep"): "",
		}
		for relPath, expected := range want {
			path := filepath.Join(outDir, relPath)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read generated file %s: %v", path, err)
			}
			if string(raw) != expected {
				t.Fatalf("unexpected content for %s\nwant:\n%s\ngot:\n%s", relPath, expected, string(raw))
			}
		}
		for _, dir := range []string{"outputs", ".deck"} {
			if info, err := os.Stat(filepath.Join(outDir, dir)); err != nil || !info.IsDir() {
				t.Fatalf("expected %s directory, err=%v", dir, err)
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
		conflictDir := outputDir
		if err := os.MkdirAll(conflictDir, 0o755); err != nil {
			t.Fatalf("mkdir workflows: %v", err)
		}
		existingPath := filepath.Join(conflictDir, "workflows", "prepare.yaml")
		if err := os.MkdirAll(filepath.Dir(existingPath), 0o755); err != nil {
			t.Fatalf("mkdir workflows: %v", err)
		}
		if err := os.WriteFile(existingPath, []byte("seed\n"), 0o644); err != nil {
			t.Fatalf("seed prepare.yaml: %v", err)
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
			t.Fatalf("read seeded prepare.yaml: %v", readErr)
		}
		if string(raw) != "seed\n" {
			t.Fatalf("existing file must remain unchanged, got %q", string(raw))
		}
		if _, statErr := os.Stat(filepath.Join(outputDir, "workflows", "scenarios", "apply.yaml")); !os.IsNotExist(statErr) {
			t.Fatalf("apply.yaml must not be created on failure, stat err=%v", statErr)
		}
	})

	t.Run("fails when target path exists as directory", func(t *testing.T) {
		outputDir := t.TempDir()
		targetDir := filepath.Join(outputDir, "workflows", "prepare.yaml")
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
		if _, statErr := os.Stat(filepath.Join(outputDir, "workflows", "scenarios", "apply.yaml")); !os.IsNotExist(statErr) {
			t.Fatalf("apply.yaml must not be created on failure, stat err=%v", statErr)
		}
		if _, statErr := os.Stat(filepath.Join(outputDir, "workflows", "vars.yaml")); !os.IsNotExist(statErr) {
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

	out, err := runWithCapturedStdout([]string{"apply", "--workflow", wf, "--dry-run", bundle})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !strings.Contains(out, "PHASE=install") {
		t.Fatalf("expected phase line in output, got %q", out)
	}
	if !strings.Contains(out, "run-true Command PLAN") {
		t.Fatalf("expected step plan line in output, got %q", out)
	}
}

func TestRunWorkflowRunDryRunWithNonInstallPhaseName(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Setenv("HOME", home)
	bundle := t.TempDir()
	if err := os.MkdirAll(filepath.Join(bundle, "workflows"), 0o755); err != nil {
		t.Fatalf("mkdir bundle workflows: %v", err)
	}

	wf := writeApplyTrueWorkflowFixture(t, "bootstrap")

	out, err := runWithCapturedStdout([]string{"apply", "--workflow", wf, "--dry-run", bundle})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !strings.Contains(out, "PHASE=bootstrap") {
		t.Fatalf("expected bootstrap phase line in output, got %q", out)
	}
	if !strings.Contains(out, "run-true Command PLAN") {
		t.Fatalf("expected step plan line in output, got %q", out)
	}

	planOut, err := runWithCapturedStdout([]string{"plan", "--workflow", wf})
	if err != nil {
		t.Fatalf("expected plan success, got %v", err)
	}
	if strings.Contains(planOut, "install phase not found") {
		t.Fatalf("plan must not require install phase, got %q", planOut)
	}
	if !strings.Contains(planOut, "PLAN workflow=") {
		t.Fatalf("expected plan header, got %q", planOut)
	}
	if !strings.Contains(planOut, "run-true Command RUN") {
		t.Fatalf("expected plan output, got %q", planOut)
	}
	if !strings.Contains(planOut, "SUMMARY steps=1 run=1 skip=0") {
		t.Fatalf("expected plan summary, got %q", planOut)
	}
}
