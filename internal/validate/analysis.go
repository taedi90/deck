package validate

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/taedi90/deck/internal/config"
	"github.com/taedi90/deck/internal/fsutil"
)

type Finding struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Hint     string `json:"hint,omitempty"`
	Path     string `json:"path,omitempty"`
	Phase    string `json:"phase,omitempty"`
	StepID   string `json:"stepId,omitempty"`
	Kind     string `json:"kind,omitempty"`
}

func AnalyzeFiles(paths []string) ([]Finding, error) {
	return AnalyzeFilesWithContext(context.Background(), paths)
}

func AnalyzeFilesWithContext(ctx context.Context, paths []string) ([]Finding, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}
	findings := make([]Finding, 0)
	for _, path := range dedupeAndSort(paths) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		displayPath := relativeOrOriginal(path)
		content, err := fsutil.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read workflow file: %w", err)
		}
		kind := detectDocumentKind(path)
		if kind == documentKindComponentFragment {
			fragment, err := parseComponentFragment(content)
			if err != nil {
				return nil, withWorkflowName(path, err)
			}
			findings = append(findings, analyzeStepsWithContext(ctx, displayPath, "", fragment.Steps)...)
			continue
		}
		wf, err := parseWorkflow(content)
		if err != nil {
			return nil, withWorkflowName(path, err)
		}
		findings = append(findings, analyzeWorkflowWithContext(ctx, displayPath, wf)...)
	}
	slices.SortFunc(findings, func(a, b Finding) int {
		if c := strings.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		if c := strings.Compare(a.Phase, b.Phase); c != 0 {
			return c
		}
		if c := strings.Compare(a.StepID, b.StepID); c != 0 {
			return c
		}
		return strings.Compare(a.Code, b.Code)
	})
	return findings, nil
}

func analyzeWorkflowWithContext(ctx context.Context, path string, wf *config.Workflow) []Finding {
	if wf == nil {
		return nil
	}
	findings := make([]Finding, 0)
	for _, step := range wf.Steps {
		if ctx.Err() != nil {
			return findings
		}
		findings = append(findings, analyzeStep(path, "", step)...)
	}
	for _, phase := range wf.Phases {
		if ctx.Err() != nil {
			return findings
		}
		findings = append(findings, analyzeStepsWithContext(ctx, path, phase.Name, phase.Steps)...)
	}
	return findings
}

func analyzeStepsWithContext(ctx context.Context, path string, phase string, steps []config.Step) []Finding {
	findings := make([]Finding, 0, len(steps))
	for _, step := range steps {
		if ctx.Err() != nil {
			return findings
		}
		findings = append(findings, analyzeStep(path, phase, step)...)
	}
	return findings
}

func analyzeStep(path string, phase string, step config.Step) []Finding {
	if !strings.EqualFold(strings.TrimSpace(step.Kind), "Command") {
		return nil
	}
	return []Finding{{
		Severity: "warning",
		Code:     "W_COMMAND_OPAQUE",
		Message:  "Command step relies on opaque shell behavior; deck cannot infer idempotency or side effects.",
		Hint:     "Prefer typed steps when available, or keep command steps small and explicit.",
		Path:     path,
		Phase:    strings.TrimSpace(phase),
		StepID:   strings.TrimSpace(step.ID),
		Kind:     step.Kind,
	}}
}

func relativeOrOriginal(path string) string {
	if rel, err := filepath.Rel(".", path); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return path
}
