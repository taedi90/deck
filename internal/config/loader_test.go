package config

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

	wf, err := LoadWithOptions(context.Background(), workflowPath, LoadOptions{VarOverrides: map[string]any{
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

		wf, err := Load(context.Background(), ts.URL+"/workflows/apply.yaml")
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

		wf, err := Load(context.Background(), ts.URL+"/workflows/apply.yaml")
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
    kind: Command
    spec: {}
`)
	if err := os.WriteFile(workflowPath, content, 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	_, err := Load(context.Background(), workflowPath)
	if err == nil {
		t.Fatalf("expected error when both phases and steps are set")
	}
}

func TestLoadMergesPrepareImports(t *testing.T) {
	dir := t.TempDir()
	workflowsDir := filepath.Join(dir, "workflows")
	componentsDir := filepath.Join(workflowsDir, "components")
	if err := os.MkdirAll(componentsDir, 0o755); err != nil {
		t.Fatalf("mkdir components: %v", err)
	}
	rootPath := filepath.Join(workflowsDir, "prepare.yaml")
	filesPath := filepath.Join(componentsDir, "files.yaml")
	imagesPath := filepath.Join(componentsDir, "images.yaml")

	if err := os.WriteFile(filesPath, []byte("role: prepare\nversion: v1alpha1\nartifacts:\n  files:\n    - group: binaries\n      items:\n        - id: kubeadm\n          source:\n            url: https://example.local/kubeadm\n          output:\n            path: bin/kubeadm\n"), 0o644); err != nil {
		t.Fatalf("write files import: %v", err)
	}
	if err := os.WriteFile(imagesPath, []byte("role: prepare\nversion: v1alpha1\nartifacts:\n  images:\n    - group: control-plane\n      items:\n        - image: registry.k8s.io/kube-apiserver:v1.30.1\n"), 0o644); err != nil {
		t.Fatalf("write images import: %v", err)
	}
	if err := os.WriteFile(rootPath, []byte("role: prepare\nversion: v1alpha1\nimports:\n  - files.yaml\n  - images.yaml\n"), 0o644); err != nil {
		t.Fatalf("write root workflow: %v", err)
	}

	wf, err := Load(context.Background(), rootPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if wf.Artifacts == nil {
		t.Fatalf("expected prepare spec")
	}
	if len(wf.Artifacts.Files) != 1 || len(wf.Artifacts.Images) != 1 {
		t.Fatalf("unexpected merged prepare content: %+v", wf.Artifacts)
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
		wf1, err := LoadWithOptions(context.Background(), workflowPath, LoadOptions{})
		if err != nil {
			t.Fatalf("LoadWithOptions(context.Background(), 1) failed: %v", err)
		}

		if err := os.WriteFile(filepath.Join(dir, "vars.yaml"), []byte("name: beta\n"), 0o644); err != nil {
			t.Fatalf("write vars.yaml(2): %v", err)
		}
		wf2, err := LoadWithOptions(context.Background(), workflowPath, LoadOptions{})
		if err != nil {
			t.Fatalf("LoadWithOptions(context.Background(), 2) failed: %v", err)
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

		wf1, err := LoadWithOptions(context.Background(), workflowPath, LoadOptions{VarOverrides: map[string]any{"mode": "override-a"}})
		if err != nil {
			t.Fatalf("LoadWithOptions(context.Background(), override-a) failed: %v", err)
		}

		wf2, err := LoadWithOptions(context.Background(), workflowPath, LoadOptions{VarOverrides: map[string]any{"mode": "override-b"}})
		if err != nil {
			t.Fatalf("LoadWithOptions(context.Background(), override-b) failed: %v", err)
		}

		if wf1.StateKey == wf2.StateKey {
			t.Fatalf("expected state key to change when --var override changes")
		}
	})
}

func TestStateKeyIgnoresAssistedMetadata(t *testing.T) {
	workflow := []byte("role: apply\nversion: v1alpha1\nphases: []\n")
	effectiveVars := map[string]any{"mode": "manual", "region": "lab-a"}

	serverA := "https://site-a.example.invalid"
	sessionA := "session-a"
	nodeA := "node-a"
	serverB := "https://site-b.example.invalid"
	sessionB := "session-b"
	nodeB := "node-b"

	keyA := computeStateKey(workflow, effectiveVars)
	keyB := computeStateKey(workflow, effectiveVars)

	if keyA != keyB {
		t.Fatalf("expected assisted metadata to remain out-of-band: server=%q session=%q node=%q server=%q session=%q node=%q keyA=%q keyB=%q", serverA, sessionA, nodeA, serverB, sessionB, nodeB, keyA, keyB)
	}
}

func TestLoadWithImports_Local(t *testing.T) {
	dir := t.TempDir()
	workflowsDir := filepath.Join(dir, "workflows")
	fragmentDir := filepath.Join(workflowsDir, "components", "fragments")
	if err := os.MkdirAll(fragmentDir, 0o755); err != nil {
		t.Fatalf("mkdir fragments: %v", err)
	}
	rootPath := filepath.Join(workflowsDir, "apply.yaml")

	fragmentPath := filepath.Join(fragmentDir, "common.yaml")
	fragment := []byte(`role: apply
version: v1alpha1
vars:
  imageRepo: from-import
  importedOnly: true
steps:
  - id: imported-step
    kind: Command
    spec:
      command: ["true"]
`)
	if err := os.WriteFile(fragmentPath, fragment, 0o644); err != nil {
		t.Fatalf("write fragment: %v", err)
	}

	root := []byte(`role: apply
version: v1alpha1
imports:
  - fragments/common.yaml
vars:
  imageRepo: from-root
steps:
  - id: root-step
    kind: Command
    spec:
      command: ["true"]
`)
	if err := os.WriteFile(rootPath, root, 0o644); err != nil {
		t.Fatalf("write root workflow: %v", err)
	}

	wf, err := Load(context.Background(), rootPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if got := len(wf.Steps); got != 2 {
		t.Fatalf("expected 2 steps, got %d", got)
	}
	if wf.Steps[0].ID != "imported-step" || wf.Steps[1].ID != "root-step" {
		t.Fatalf("unexpected merged step order: %s, %s", wf.Steps[0].ID, wf.Steps[1].ID)
	}
	if got := wf.Vars["imageRepo"]; got != "from-root" {
		t.Fatalf("expected root var override, got %v", got)
	}
	if got := wf.Vars["importedOnly"]; got != true {
		t.Fatalf("expected imported var, got %v", got)
	}
}

func TestLoadWithImports_Cycle(t *testing.T) {
	dir := t.TempDir()
	workflowsDir := filepath.Join(dir, "workflows")
	componentsDir := filepath.Join(workflowsDir, "components")
	if err := os.MkdirAll(componentsDir, 0o755); err != nil {
		t.Fatalf("mkdir components: %v", err)
	}
	aPath := filepath.Join(workflowsDir, "apply.yaml")
	bPath := filepath.Join(componentsDir, "b.yaml")
	cPath := filepath.Join(componentsDir, "c.yaml")

	a := []byte(`role: apply
version: v1alpha1
imports:
  - b.yaml
steps:
  - id: a-step
    kind: Command
    spec:
      command: ["true"]
`)
	b := []byte(`role: apply
version: v1alpha1
imports:
  - c.yaml
steps:
  - id: b-step
    kind: Command
    spec:
      command: ["true"]
`)
	c := []byte(`role: apply
version: v1alpha1
imports:
  - b.yaml
steps:
  - id: c-step
    kind: Command
    spec:
      command: ["true"]
`)
	if err := os.WriteFile(aPath, a, 0o644); err != nil {
		t.Fatalf("write a.yaml: %v", err)
	}
	if err := os.WriteFile(bPath, b, 0o644); err != nil {
		t.Fatalf("write b.yaml: %v", err)
	}
	if err := os.WriteFile(cPath, c, 0o644); err != nil {
		t.Fatalf("write c.yaml: %v", err)
	}

	_, err := Load(context.Background(), aPath)
	if err == nil {
		t.Fatalf("expected import cycle error")
	}
	if !strings.Contains(err.Error(), "import cycle") {
		t.Fatalf("expected import cycle error, got %v", err)
	}
}

func TestLoadWithImports_RemoteRelative(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/wf/workflows/apply.yaml":
			_, _ = w.Write([]byte(`role: apply
version: v1alpha1
imports:
  - fragments/common.yaml
steps:
  - id: root-step
    kind: Command
    spec:
      command: ["true"]
`))
		case "/wf/workflows/components/fragments/common.yaml":
			_, _ = w.Write([]byte(`role: apply
version: v1alpha1
steps:
  - id: imported-step
    kind: Command
    spec:
      command: ["true"]
`))
		case "/wf/workflows/vars.yaml":
			_, _ = w.Write([]byte("fromVarsFile: true\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	wf, err := Load(context.Background(), ts.URL+"/wf/workflows/apply.yaml")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if got := len(wf.Steps); got != 2 {
		t.Fatalf("expected 2 steps, got %d", got)
	}
	if wf.Steps[0].ID != "imported-step" || wf.Steps[1].ID != "root-step" {
		t.Fatalf("unexpected merged step order: %s, %s", wf.Steps[0].ID, wf.Steps[1].ID)
	}
	if got := wf.Vars["fromVarsFile"]; got != true {
		t.Fatalf("expected vars.yaml value, got %v", got)
	}
}

func TestLoadWithImports_MergesPhasesByName(t *testing.T) {
	dir := t.TempDir()
	workflowsDir := filepath.Join(dir, "workflows")
	componentsDir := filepath.Join(workflowsDir, "components")
	if err := os.MkdirAll(componentsDir, 0o755); err != nil {
		t.Fatalf("mkdir components: %v", err)
	}
	rootPath := filepath.Join(workflowsDir, "apply.yaml")
	fragmentPath := filepath.Join(componentsDir, "phase-fragment.yaml")

	fragment := []byte(`role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: imported-step
        kind: Command
        spec:
          command: ["true"]
`)
	if err := os.WriteFile(fragmentPath, fragment, 0o644); err != nil {
		t.Fatalf("write fragment: %v", err)
	}

	root := []byte(`role: apply
version: v1alpha1
imports:
  - phase-fragment.yaml
phases:
  - name: install
    steps:
      - id: root-step
        kind: Command
        spec:
          command: ["true"]
`)
	if err := os.WriteFile(rootPath, root, 0o644); err != nil {
		t.Fatalf("write root workflow: %v", err)
	}

	wf, err := Load(context.Background(), rootPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if got := len(wf.Phases); got != 1 {
		t.Fatalf("expected 1 merged phase, got %d", got)
	}
	if got := len(wf.Phases[0].Steps); got != 2 {
		t.Fatalf("expected 2 merged steps, got %d", got)
	}
	if wf.Phases[0].Steps[0].ID != "imported-step" || wf.Phases[0].Steps[1].ID != "root-step" {
		t.Fatalf("unexpected merged step order: %s, %s", wf.Phases[0].Steps[0].ID, wf.Phases[0].Steps[1].ID)
	}
}

func TestLoadWithPhaseImports_CombinesWhenAndSteps(t *testing.T) {
	dir := t.TempDir()
	workflowsDir := filepath.Join(dir, "workflows")
	componentsDir := filepath.Join(workflowsDir, "components")
	if err := os.MkdirAll(componentsDir, 0o755); err != nil {
		t.Fatalf("mkdir components: %v", err)
	}
	rootPath := filepath.Join(workflowsDir, "apply.yaml")
	fragmentPath := filepath.Join(componentsDir, "phase-install.yaml")

	fragment := []byte(`role: apply
version: v1alpha1
steps:
  - id: imported-a
    kind: Command
    when: vars.enableCommon == true
    spec:
      command: ["true"]
  - id: imported-b
    kind: Command
    spec:
      command: ["true"]
`)
	if err := os.WriteFile(fragmentPath, fragment, 0o644); err != nil {
		t.Fatalf("write fragment: %v", err)
	}

	root := []byte(`role: apply
version: v1alpha1
phases:
  - name: install
    imports:
      - path: phase-install.yaml
        when: vars.osFamily == "rhel"
    steps:
      - id: root-step
        kind: Command
        spec:
          command: ["true"]
`)
	if err := os.WriteFile(rootPath, root, 0o644); err != nil {
		t.Fatalf("write root workflow: %v", err)
	}

	wf, err := LoadWithOptions(context.Background(), rootPath, LoadOptions{VarOverrides: map[string]any{"osFamily": "rhel", "enableCommon": true}})
	if err != nil {
		t.Fatalf("LoadWithOptions failed: %v", err)
	}
	if len(wf.Phases) != 1 {
		t.Fatalf("expected 1 phase, got %d", len(wf.Phases))
	}
	steps := wf.Phases[0].Steps
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(steps))
	}
	if steps[0].ID != "imported-a" || steps[1].ID != "imported-b" || steps[2].ID != "root-step" {
		t.Fatalf("unexpected step order: %s, %s, %s", steps[0].ID, steps[1].ID, steps[2].ID)
	}
	if steps[0].When != "(vars.osFamily == \"rhel\") && (vars.enableCommon == true)" {
		t.Fatalf("unexpected combined when for imported-a: %s", steps[0].When)
	}
	if steps[1].When != "vars.osFamily == \"rhel\"" {
		t.Fatalf("unexpected when for imported-b: %s", steps[1].When)
	}
}

func TestLoadWithVarsYAML_Precedence(t *testing.T) {
	dir := t.TempDir()
	rootPath := filepath.Join(dir, "apply.yaml")
	varsPath := filepath.Join(dir, "vars.yaml")

	if err := os.WriteFile(varsPath, []byte("clusterName: from-vars\nregion: global\n"), 0o644); err != nil {
		t.Fatalf("write vars.yaml: %v", err)
	}

	root := []byte(`role: apply
version: v1alpha1
vars:
  region: kr
steps:
  - id: root-step
    kind: Command
    spec:
      command: ["true"]
`)
	if err := os.WriteFile(rootPath, root, 0o644); err != nil {
		t.Fatalf("write root workflow: %v", err)
	}

	wf, err := LoadWithOptions(context.Background(), rootPath, LoadOptions{VarOverrides: map[string]any{"region": "cli"}})
	if err != nil {
		t.Fatalf("LoadWithOptions failed: %v", err)
	}
	if got := wf.Vars["clusterName"]; got != "from-vars" {
		t.Fatalf("expected clusterName from vars.yaml, got %v", got)
	}
	if got := wf.Vars["region"]; got != "cli" {
		t.Fatalf("expected cli override precedence for region, got %v", got)
	}
}
