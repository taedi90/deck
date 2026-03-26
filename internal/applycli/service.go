package applycli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/install"
)

type RunLogger interface {
	Dir() string
	EventSink() install.StepEventSink
	CloseWithResult(status string, err error) error
}

type ExecuteOptions struct {
	Request        ExecutionRequest
	BundleRoot     string
	WorkflowSource string
	Scenario       string
	DryRun         bool
	Verbosef       func(level int, format string, args ...any) error
	StdoutPrintf   func(format string, args ...any) error
	StdoutPrintln  func(args ...any) error
	AdditionalSink install.StepEventSink
	NewRunLogger   func(workflowPath, workflowSource, scenario, bundleRoot, selectedPhase string) (RunLogger, error)
}

func Execute(ctx context.Context, opts ExecuteOptions) (err error) {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	request := opts.Request
	if request.Workflow == nil {
		return fmt.Errorf("workflow is nil")
	}
	if request.ExecutionWorkflow == nil {
		return fmt.Errorf("execution workflow is nil")
	}
	if err := verbosef(opts.Verbosef, 1, "deck: apply workflow=%s phase=%s state=%s bundle=%s dryRun=%t fresh=%t\n", request.WorkflowPath, request.SelectedPhase, request.StatePath, strings.TrimSpace(opts.BundleRoot), opts.DryRun, request.Fresh); err != nil {
		return err
	}
	if opts.DryRun {
		return writeApplyDryRun(opts.StdoutPrintf, request)
	}
	if opts.NewRunLogger == nil {
		return fmt.Errorf("run logger factory is nil")
	}
	runLogger, err := opts.NewRunLogger(request.WorkflowPath, strings.TrimSpace(opts.WorkflowSource), strings.TrimSpace(opts.Scenario), strings.TrimSpace(opts.BundleRoot), request.SelectedPhase)
	if err != nil {
		return err
	}
	if err := verbosef(opts.Verbosef, 1, "deck: apply runlog=%s\n", runLogger.Dir()); err != nil {
		return err
	}
	eventSink := combineStepEventSinks(runLogger.EventSink(), opts.AdditionalSink)
	defer func() {
		status := "ok"
		if err != nil {
			status = "failed"
		}
		closeErr := runLogger.CloseWithResult(status, err)
		if err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	if err := install.Run(ctx, request.ExecutionWorkflow, install.RunOptions{BundleRoot: opts.BundleRoot, StatePath: request.StatePath, EventSink: eventSink, Fresh: request.Fresh}); err != nil {
		return err
	}
	if opts.StdoutPrintln == nil {
		return nil
	}
	return opts.StdoutPrintln("apply: ok")
}

func writeApplyDryRun(stdoutPrintf func(format string, args ...any) error, request ExecutionRequest) error {
	if stdoutPrintf == nil {
		return fmt.Errorf("stdout printf is nil")
	}
	wf := request.ExecutionWorkflow
	selectedPhaseName := request.SelectedPhase
	if wf == nil || len(wf.Phases) == 0 {
		if selectedPhaseName == "" {
			return errors.New("no phases found")
		}
		return fmt.Errorf("%s phase not found", selectedPhaseName)
	}

	state, err := LoadInstallDryRunState(request)
	if err != nil {
		return err
	}

	runtimeVars := map[string]any{}
	for key, value := range state.RuntimeVars {
		runtimeVars[key] = value
	}
	runtimeVars["host"] = install.CurrentHostFacts()

	completed := make(map[string]bool, len(state.CompletedPhases))
	for _, phaseName := range state.CompletedPhases {
		completed[phaseName] = true
	}

	for _, phase := range wf.Phases {
		if err := stdoutPrintf("PHASE=%s\n", phase.Name); err != nil {
			return err
		}
		if completed[phase.Name] {
			if err := stdoutPrintf("SKIP (completed phase)\n"); err != nil {
				return err
			}
			continue
		}
		for _, step := range phase.Steps {
			ok, evalErr := install.EvaluateWhen(step.When, wf.Vars, runtimeVars)
			if evalErr != nil {
				return fmt.Errorf("WHEN_EVAL_ERROR: step %s (%s): %w", step.ID, step.Kind, evalErr)
			}

			status := "PLAN"
			if !ok {
				status = "SKIP"
			}
			if err := stdoutPrintf("%s %s %s\n", step.ID, step.Kind, status); err != nil {
				return err
			}
		}
	}

	return nil
}

func combineStepEventSinks(sinks ...install.StepEventSink) install.StepEventSink {
	filtered := make([]install.StepEventSink, 0, len(sinks))
	for _, sink := range sinks {
		if sink != nil {
			filtered = append(filtered, sink)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return func(event install.StepEvent) {
		for _, sink := range filtered {
			sink(event)
		}
	}
}

func verbosef(fn func(level int, format string, args ...any) error, level int, format string, args ...any) error {
	if fn == nil {
		return nil
	}
	return fn(level, format, args...)
}
