package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkflowRejectsLegacyWhenOperators(t *testing.T) {
	wf := []byte(`version: v1alpha1
steps:
  - id: bad-when
    apiVersion: deck/v1alpha1
    kind: EnsureDirectory
    when: vars.enabled and runtime.ready
    spec:
      path: /var/lib/deck
`)

	err := Bytes("legacy-when.yaml", wf)
	if err == nil {
		t.Fatalf("expected legacy when operator to fail")
	}
	if !strings.Contains(err.Error(), "invalid when expression") {
		t.Fatalf("expected invalid when expression error, got %v", err)
	}
	if !strings.Contains(err.Error(), "use && instead of and") {
		t.Fatalf("expected migration hint, got %v", err)
	}
}

func TestDocsGuidesExamplesValidate(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "docs", "guides", "examples", "*.yaml"))
	if err != nil {
		t.Fatalf("glob examples: %v", err)
	}
	if len(paths) == 0 {
		t.Fatalf("no docs examples found")
	}
	for _, path := range paths {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			if err := File(path); err != nil {
				t.Fatalf("validate example %s: %v", path, err)
			}
		})
	}
}

func TestScenarioWorkspaceValidates(t *testing.T) {
	_, err := Workspace(filepath.Join("..", "..", "test"))
	if err != nil {
		t.Fatalf("validate scenario workspace: %v", err)
	}
	if err := File(filepath.Join("..", "..", "test", "workflows", "prepare.yaml")); err != nil {
		t.Fatalf("validate canonical test prepare workflow: %v", err)
	}
}

func TestWorkflowModelDocsDescribeRuntimeRegisterNamespace(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "reference", "workflow-model.md"))
	if err != nil {
		t.Fatalf("read workflow model docs: %v", err)
	}
	content := string(raw)
	if strings.Contains(content, "available to later steps via `vars.`") {
		t.Fatalf("workflow model docs must not describe register outputs as vars")
	}
	if !strings.Contains(content, "available to later steps via `runtime.`") {
		t.Fatalf("workflow model docs must describe runtime register namespace")
	}
}
