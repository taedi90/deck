package workspacepaths

import (
	"path/filepath"
	"testing"
)

func TestWorkflowPathHelpers(t *testing.T) {
	root := filepath.Join("workspace", "demo")
	if got := WorkflowRootPath(root); got != filepath.Join(root, WorkflowRootDir) {
		t.Fatalf("unexpected workflow root path: %s", got)
	}
	if got := WorkflowScenariosPath(root); got != filepath.Join(root, WorkflowRootDir, WorkflowScenariosDir) {
		t.Fatalf("unexpected workflow scenarios path: %s", got)
	}
	if got := WorkflowComponentsPath(root); got != filepath.Join(root, WorkflowRootDir, WorkflowComponentsDir) {
		t.Fatalf("unexpected workflow components path: %s", got)
	}
}

func TestWorkflowSubdirDetectors(t *testing.T) {
	root := t.TempDir()
	scenarioPath := filepath.Join(root, WorkflowRootDir, WorkflowScenariosDir, "apply.yaml")
	componentPath := filepath.Join(root, WorkflowRootDir, WorkflowComponentsDir, "k8s", "bootstrap.yaml")
	otherPath := filepath.Join(root, "docs", "architecture.md")

	if !IsScenarioWorkflowPath(scenarioPath) {
		t.Fatalf("expected scenario path to be detected: %s", scenarioPath)
	}
	if IsScenarioWorkflowPath(componentPath) {
		t.Fatalf("did not expect component path to be treated as scenario: %s", componentPath)
	}
	if !IsComponentWorkflowPath(componentPath) {
		t.Fatalf("expected component path to be detected: %s", componentPath)
	}
	if IsComponentWorkflowPath(otherPath) {
		t.Fatalf("did not expect unrelated path to be treated as component: %s", otherPath)
	}
}
