package config

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestVarsPrecedence(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "apply.yaml")

	if err := os.WriteFile(filepath.Join(dir, "vars.yaml"), []byte("imageRepo: from-vars-file\nroleHint: from-vars-file\n"), 0o644); err != nil {
		t.Fatalf("write vars.yaml: %v", err)
	}
	if err := os.WriteFile(workflowPath, []byte("role: apply\nversion: v1alpha1\nvars:\n  imageRepo: from-workflow\n  kubeVersion: v1.31.0\nphases: []\n"), 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	wf, err := LoadWithOptions(workflowPath, LoadOptions{VarOverrides: map[string]any{
		"imageRepo": "from-cli",
		"cliOnly":   "present",
	}})
	if err != nil {
		t.Fatalf("LoadWithOptions failed: %v", err)
	}

	if got := wf.Vars["imageRepo"]; got != "from-cli" {
		t.Fatalf("imageRepo precedence mismatch, got %v", got)
	}
	if got := wf.Vars["kubeVersion"]; got != "v1.31.0" {
		t.Fatalf("workflow var missing, got %v", got)
	}
	if got := wf.Vars["roleHint"]; got != "from-vars-file" {
		t.Fatalf("vars.yaml var missing, got %v", got)
	}
	if got := wf.Vars["cliOnly"]; got != "present" {
		t.Fatalf("cli override missing, got %v", got)
	}
}

func TestVarsURLFetch(t *testing.T) {
	t.Run("loads vars yaml when present", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/workflows/apply.yaml":
				_, _ = w.Write([]byte("role: apply\nversion: v1alpha1\nvars:\n  fromWorkflow: true\nphases: []\n"))
			case "/workflows/vars.yaml":
				_, _ = w.Write([]byte("fromVarsFile: true\n"))
			default:
				http.NotFound(w, r)
			}
		}))
		defer ts.Close()

		wf, err := Load(ts.URL + "/workflows/apply.yaml")
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		if got := wf.Vars["fromVarsFile"]; got != true {
			t.Fatalf("vars.yaml from URL not loaded, got %v", got)
		}
		if got := wf.Vars["fromWorkflow"]; got != true {
			t.Fatalf("workflow vars missing, got %v", got)
		}
	})

	t.Run("skips vars yaml on 404", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/workflows/apply.yaml":
				_, _ = w.Write([]byte("role: apply\nversion: v1alpha1\nvars:\n  fromWorkflow: true\nphases: []\n"))
			case "/workflows/vars.yaml":
				http.NotFound(w, r)
			default:
				http.NotFound(w, r)
			}
		}))
		defer ts.Close()

		wf, err := Load(ts.URL + "/workflows/apply.yaml")
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		if _, ok := wf.Vars["fromVarsFile"]; ok {
			t.Fatalf("unexpected vars.yaml value loaded on 404")
		}
		if got := wf.Vars["fromWorkflow"]; got != true {
			t.Fatalf("workflow vars missing, got %v", got)
		}
	})
}

func TestLoadRejectsBothPhasesAndSteps(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "workflow.yaml")

	content := []byte(`role: apply
version: v1alpha1
phases:
  - name: phase-a
    steps: []
steps:
  - id: step-a
    kind: RunCommand
    spec: {}
`)
	if err := os.WriteFile(workflowPath, content, 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	_, err := Load(workflowPath)
	if err == nil {
		t.Fatalf("expected error when both phases and steps are set")
	}
}

func TestStateKey(t *testing.T) {
	t.Run("changes when vars yaml changes", func(t *testing.T) {
		dir := t.TempDir()
		workflowPath := filepath.Join(dir, "apply.yaml")
		workflowContent := []byte("role: apply\nversion: v1alpha1\nphases: []\n")
		if err := os.WriteFile(workflowPath, workflowContent, 0o644); err != nil {
			t.Fatalf("write workflow: %v", err)
		}

		if err := os.WriteFile(filepath.Join(dir, "vars.yaml"), []byte("name: alpha\n"), 0o644); err != nil {
			t.Fatalf("write vars.yaml(1): %v", err)
		}
		wf1, err := LoadWithOptions(workflowPath, LoadOptions{})
		if err != nil {
			t.Fatalf("LoadWithOptions(1) failed: %v", err)
		}

		if err := os.WriteFile(filepath.Join(dir, "vars.yaml"), []byte("name: beta\n"), 0o644); err != nil {
			t.Fatalf("write vars.yaml(2): %v", err)
		}
		wf2, err := LoadWithOptions(workflowPath, LoadOptions{})
		if err != nil {
			t.Fatalf("LoadWithOptions(2) failed: %v", err)
		}

		if wf1.StateKey == wf2.StateKey {
			t.Fatalf("expected state key to change when vars.yaml changes")
		}
	})

	t.Run("changes when var override changes", func(t *testing.T) {
		dir := t.TempDir()
		workflowPath := filepath.Join(dir, "apply.yaml")
		workflowContent := []byte("role: apply\nversion: v1alpha1\nvars:\n  mode: workflow\nphases: []\n")
		if err := os.WriteFile(workflowPath, workflowContent, 0o644); err != nil {
			t.Fatalf("write workflow: %v", err)
		}

		wf1, err := LoadWithOptions(workflowPath, LoadOptions{VarOverrides: map[string]any{"mode": "override-a"}})
		if err != nil {
			t.Fatalf("LoadWithOptions(override-a) failed: %v", err)
		}

		wf2, err := LoadWithOptions(workflowPath, LoadOptions{VarOverrides: map[string]any{"mode": "override-b"}})
		if err != nil {
			t.Fatalf("LoadWithOptions(override-b) failed: %v", err)
		}

		if wf1.StateKey == wf2.StateKey {
			t.Fatalf("expected state key to change when --var override changes")
		}
	})
}
