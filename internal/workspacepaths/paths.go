package workspacepaths

import (
	"fmt"
	"path/filepath"
	"strings"
)

const (
	WorkflowRootDir             = "workflows"
	WorkflowScenariosDir        = "scenarios"
	WorkflowComponentsDir       = "components"
	CanonicalPrepareWorkflowRel = "prepare.yaml"
	CanonicalApplyWorkflowRel   = "scenarios/apply.yaml"
	WorkflowVarsRel             = "vars.yaml"
	PreparedDirRel              = "outputs"
)

func WorkflowPath(root string, rel string) string {
	parts := append([]string{root, WorkflowRootDir}, strings.Split(filepath.ToSlash(rel), "/")...)
	return filepath.Join(parts...)
}

func CanonicalPrepareWorkflowPath(root string) string {
	return filepath.Join(root, CanonicalPrepareWorkflowRel)
}

func CanonicalApplyWorkflowPath(root string) string {
	return WorkflowPath(root, CanonicalApplyWorkflowRel)
}

func CanonicalVarsPath(root string) string {
	return WorkflowPath(root, WorkflowVarsRel)
}

func DefaultPreparedRoot(root string) string {
	return filepath.Join(root, PreparedDirRel)
}

func LocateWorkflowTreeRoot(workflowPath string) (string, error) {
	resolved, err := filepath.Abs(strings.TrimSpace(workflowPath))
	if err != nil {
		return "", fmt.Errorf("resolve workflow path: %w", err)
	}
	dir := filepath.Dir(resolved)
	for {
		if filepath.Base(dir) == WorkflowRootDir {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("workflow path is not under %s/: %s", WorkflowRootDir, resolved)
}
