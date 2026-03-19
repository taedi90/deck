package applycli

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/taedi90/deck/internal/install"
)

type PlanOptions struct {
	Request         ExecutionRequest
	Output          string
	Verbosef        func(level int, format string, args ...any) error
	StdoutPrintf    func(format string, args ...any) error
	JSONEncoderFunc func() *json.Encoder
}

type PlanStep struct {
	Phase         string            `json:"phase,omitempty"`
	ID            string            `json:"id"`
	Kind          string            `json:"kind"`
	Action        string            `json:"action"`
	Reason        string            `json:"reason,omitempty"`
	When          string            `json:"when,omitempty"`
	Retry         int               `json:"retry,omitempty"`
	Timeout       string            `json:"timeout,omitempty"`
	Register      map[string]string `json:"register,omitempty"`
	WhenEvaluated bool              `json:"whenEvaluated,omitempty"`
}

type PlanSummary struct {
	TotalSteps      int `json:"totalSteps"`
	RunSteps        int `json:"runSteps"`
	SkipSteps       int `json:"skipSteps"`
	SkipCompleted   int `json:"skipCompleted"`
	SkipWhen        int `json:"skipWhen"`
	PhaseCount      int `json:"phaseCount"`
	CompletedSteps  int `json:"completedSteps"`
	RuntimeVarCount int `json:"runtimeVarCount"`
}

type PlanReport struct {
	WorkflowPath   string      `json:"workflowPath"`
	SelectedPhase  string      `json:"selectedPhase,omitempty"`
	StatePath      string      `json:"statePath"`
	RuntimeVarKeys []string    `json:"runtimeVarKeys,omitempty"`
	Summary        PlanSummary `json:"summary"`
	Steps          []PlanStep  `json:"steps"`
}

func ExecutePlan(ctx context.Context, opts PlanOptions) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	if opts.StdoutPrintf == nil {
		return fmt.Errorf("stdout printf is nil")
	}
	report, err := BuildPlanReport(opts.Request)
	if err != nil {
		return err
	}
	workflowVarKeys := make([]string, 0, len(opts.Request.ExecutionWorkflow.Vars))
	for key := range opts.Request.ExecutionWorkflow.Vars {
		workflowVarKeys = append(workflowVarKeys, key)
	}
	slices.Sort(workflowVarKeys)
	if err := verbosef(opts.Verbosef, 1, "deck: plan workflow=%s phase=%s state=%s\n", report.WorkflowPath, report.SelectedPhase, report.StatePath); err != nil {
		return err
	}
	if err := verbosef(opts.Verbosef, 3, "deck: plan workflowVars=%s runtimeVars=%s completedSteps=%d\n", joinOrDash(workflowVarKeys), joinOrDash(report.RuntimeVarKeys), report.Summary.CompletedSteps); err != nil {
		return err
	}
	if err := logPlanDetails(opts.Verbosef, report); err != nil {
		return err
	}
	if strings.TrimSpace(opts.Output) == "json" {
		if opts.JSONEncoderFunc == nil {
			return fmt.Errorf("json encoder factory is nil")
		}
		enc := opts.JSONEncoderFunc()
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	return writePlanText(opts.StdoutPrintf, report)
}

func BuildPlanReport(request ExecutionRequest) (PlanReport, error) {
	if request.ExecutionWorkflow == nil {
		return PlanReport{}, fmt.Errorf("execution workflow is nil")
	}
	state, err := LoadInstallDryRunState(request.ExecutionWorkflow)
	if err != nil {
		return PlanReport{}, err
	}
	completed := make(map[string]bool, len(state.CompletedSteps))
	for _, stepID := range state.CompletedSteps {
		completed[stepID] = true
	}
	runtimeVars := map[string]any{}
	for key, value := range state.RuntimeVars {
		runtimeVars[key] = value
	}
	steps := make([]PlanStep, 0)
	summary := PlanSummary{PhaseCount: len(request.ExecutionWorkflow.Phases), CompletedSteps: len(state.CompletedSteps), RuntimeVarCount: len(runtimeVars)}
	for _, phase := range request.ExecutionWorkflow.Phases {
		for _, step := range phase.Steps {
			entry := PlanStep{Phase: phase.Name, ID: step.ID, Kind: step.Kind, When: strings.TrimSpace(step.When), Retry: step.Retry, Timeout: strings.TrimSpace(step.Timeout)}
			if len(step.Register) > 0 {
				entry.Register = make(map[string]string, len(step.Register))
				for key, value := range step.Register {
					entry.Register[key] = value
				}
			}
			summary.TotalSteps++
			if completed[step.ID] {
				entry.Action = "skip"
				entry.Reason = "completed"
				summary.SkipSteps++
				summary.SkipCompleted++
				steps = append(steps, entry)
				continue
			}
			ok, evalErr := install.EvaluateWhen(step.When, request.ExecutionWorkflow.Vars, runtimeVars)
			if evalErr != nil {
				return PlanReport{}, fmt.Errorf("WHEN_EVAL_ERROR: step %s (%s): %w", step.ID, step.Kind, evalErr)
			}
			entry.WhenEvaluated = true
			if !ok {
				entry.Action = "skip"
				entry.Reason = "when"
				summary.SkipSteps++
				summary.SkipWhen++
				steps = append(steps, entry)
				continue
			}
			entry.Action = "run"
			summary.RunSteps++
			steps = append(steps, entry)
		}
	}
	runtimeVarKeys := make([]string, 0, len(runtimeVars))
	for key := range runtimeVars {
		runtimeVarKeys = append(runtimeVarKeys, key)
	}
	slices.Sort(runtimeVarKeys)
	return PlanReport{WorkflowPath: request.WorkflowPath, SelectedPhase: request.SelectedPhase, StatePath: request.StatePath, RuntimeVarKeys: runtimeVarKeys, Summary: summary, Steps: steps}, nil
}

func writePlanText(stdoutPrintf func(format string, args ...any) error, report PlanReport) error {
	multiPhase := report.Summary.PhaseCount > 1
	currentPhase := ""
	if err := stdoutPrintf("PLAN workflow=%s state=%s selectedPhase=%s runtimeVars=%d completed=%d\n", report.WorkflowPath, displayValueOrDash(report.SelectedPhase), report.StatePath, len(report.RuntimeVarKeys), report.Summary.CompletedSteps); err != nil {
		return err
	}
	for _, step := range report.Steps {
		if multiPhase && step.Phase != currentPhase {
			currentPhase = step.Phase
			if err := stdoutPrintf("PHASE=%s\n", currentPhase); err != nil {
				return err
			}
		}
		if step.Action == "skip" && step.Reason != "" {
			if err := stdoutPrintf("%s %s SKIP (%s)\n", step.ID, step.Kind, step.Reason); err != nil {
				return err
			}
			continue
		}
		if err := stdoutPrintf("%s %s %s\n", step.ID, step.Kind, strings.ToUpper(step.Action)); err != nil {
			return err
		}
	}
	return stdoutPrintf("SUMMARY steps=%d run=%d skip=%d skipCompleted=%d skipWhen=%d phases=%d\n", report.Summary.TotalSteps, report.Summary.RunSteps, report.Summary.SkipSteps, report.Summary.SkipCompleted, report.Summary.SkipWhen, report.Summary.PhaseCount)
}

func logPlanDetails(verbose func(level int, format string, args ...any) error, report PlanReport) error {
	for _, step := range report.Steps {
		if err := verbosef(verbose, 2, "deck: plan step=%s kind=%s phase=%s action=%s reason=%s when=%q retry=%d timeout=%q register=%d\n", step.ID, step.Kind, step.Phase, step.Action, step.Reason, step.When, step.Retry, step.Timeout, len(step.Register)); err != nil {
			return err
		}
		if err := verbosef(verbose, 3, "deck: plan stepEval step=%s whenEvaluated=%t registerKeys=%s\n", step.ID, step.WhenEvaluated, joinOrDash(sortedRegisterKeys(step.Register))); err != nil {
			return err
		}
	}
	return nil
}

func displayValueOrDash(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "-"
	}
	return trimmed
}

func joinOrDash(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, ",")
}

func sortedRegisterKeys(values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
