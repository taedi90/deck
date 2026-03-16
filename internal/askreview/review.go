package askreview

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/taedi90/deck/internal/workspacepaths"
)

type Finding struct {
	Severity string
	Message  string
}

func Workspace(root string) []Finding {
	findings := make([]Finding, 0)
	preparePath := workspacepaths.CanonicalPrepareWorkflowPath(root)
	applyPath := workspacepaths.CanonicalApplyWorkflowPath(root)
	files := []string{preparePath, applyPath}
	commandCount := 0
	for _, path := range files {
		//nolint:gosec // Review paths are derived from the current workspace layout.
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		count, hasTopLevelSteps := inspectWorkflow(raw)
		commandCount += count
		if strings.HasSuffix(path, "apply.yaml") && hasTopLevelSteps {
			findings = append(findings, Finding{
				Severity: "warn",
				Message:  fmt.Sprintf("%s uses top-level steps; named phases are usually easier to review for apply workflows", filepath.ToSlash(path)),
			})
		}
	}
	if commandCount >= 3 {
		findings = append(findings, Finding{
			Severity: "warn",
			Message:  fmt.Sprintf("workspace uses %d Command steps; prefer typed steps where possible", commandCount),
		})
	}
	return findings
}

func Candidate(files map[string]string) []Finding {
	findings := make([]Finding, 0)
	commandCount := 0
	for path, content := range files {
		count, hasTopLevelSteps := inspectWorkflow([]byte(content))
		commandCount += count
		if strings.HasSuffix(path, "/apply.yaml") && hasTopLevelSteps {
			findings = append(findings, Finding{
				Severity: "warn",
				Message:  fmt.Sprintf("%s uses top-level steps; consider named phases for apply workflows", filepath.ToSlash(path)),
			})
		}
	}
	if commandCount >= 3 {
		findings = append(findings, Finding{
			Severity: "warn",
			Message:  fmt.Sprintf("candidate output uses %d Command steps; prefer typed steps where possible", commandCount),
		})
	}
	return findings
}

type workflowDoc struct {
	Steps  []stepDoc  `yaml:"steps"`
	Phases []phaseDoc `yaml:"phases"`
}

type phaseDoc struct {
	Steps []stepDoc `yaml:"steps"`
}

type stepDoc struct {
	Kind string `yaml:"kind"`
}

func inspectWorkflow(raw []byte) (commandCount int, hasTopLevelSteps bool) {
	var doc workflowDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return 0, false
	}
	hasTopLevelSteps = len(doc.Steps) > 0
	for _, step := range doc.Steps {
		if step.Kind == "Command" {
			commandCount++
		}
	}
	for _, phase := range doc.Phases {
		for _, step := range phase.Steps {
			if step.Kind == "Command" {
				commandCount++
			}
		}
	}
	return commandCount, hasTopLevelSteps
}
