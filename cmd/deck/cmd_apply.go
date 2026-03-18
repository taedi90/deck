package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/taedi90/deck/internal/applycli"
	"github.com/taedi90/deck/internal/config"
	"github.com/taedi90/deck/internal/install"
	"github.com/taedi90/deck/internal/validate"
)

type diffOptions struct {
	workflowPath  string
	scenario      string
	source        string
	selectedPhase string
	output        string
	varOverrides  map[string]string
}

func newPlanCommand() *cobra.Command {
	vars := &varFlag{}
	cmd := &cobra.Command{
		Use:     "plan",
		Aliases: []string{"diff"},
		Short:   "Show the planned apply step execution",
		RunE: func(cmd *cobra.Command, _ []string) error {
			workflowPath, err := cmdFlagValue(cmd, "workflow")
			if err != nil {
				return err
			}
			scenario, err := cmdFlagValue(cmd, "scenario")
			if err != nil {
				return err
			}
			source, err := cmdFlagValue(cmd, "source")
			if err != nil {
				return err
			}
			selectedPhase, err := cmdFlagValue(cmd, "phase")
			if err != nil {
				return err
			}
			output, err := cmdFlagValue(cmd, "output")
			if err != nil {
				return err
			}
			return runDiffWithOptions(cmd.Context(), diffOptions{
				workflowPath:  workflowPath,
				scenario:      scenario,
				source:        source,
				selectedPhase: selectedPhase,
				output:        output,
				varOverrides:  vars.AsMap(),
			})
		},
	}
	cmd.Flags().SetInterspersed(false)
	cmd.Flags().String("workflow", "", "path or URL to workflow file")
	cmd.Flags().String("scenario", "", "scenario name to plan")
	cmd.Flags().String("source", scenarioSourceLocal, "scenario source (local|server)")
	cmd.Flags().String("phase", "", "phase name to plan (defaults to all phases)")
	cmd.Flags().StringP("output", "o", "text", "output format (text|json)")
	cmd.Flags().Var(vars, "var", "set variable override (key=value), repeatable")
	registerScenarioSourceCompletion(cmd, "source", false)
	registerScenarioNameCompletion(cmd, "scenario", "source", "", false)
	return cmd
}

func runDiffWithOptions(ctx context.Context, opts diffOptions) error {
	workflowPath, err := resolvePlanWorkflowPath(strings.TrimSpace(opts.workflowPath), strings.TrimSpace(opts.scenario), strings.TrimSpace(opts.source))
	if err != nil {
		return err
	}
	selectedPhase := strings.TrimSpace(opts.selectedPhase)
	return executeDiff(ctx, workflowPath, selectedPhase, opts.output, varsAsAnyMap(opts.varOverrides))
}

func executeDiff(ctx context.Context, workflowPath, selectedPhase, output string, varOverrides map[string]any) error {
	resolvedOutput := strings.ToLower(strings.TrimSpace(output))
	if resolvedOutput == "" {
		resolvedOutput = "text"
	}
	if resolvedOutput != "text" && resolvedOutput != "json" {
		return errors.New("--output must be text or json")
	}
	resolvedRequest, err := applycli.ResolveExecutionRequest(ctx, applycli.ExecutionRequestOptions{
		CommandName:                  "diff",
		WorkflowPath:                 workflowPath,
		VarOverrides:                 varOverrides,
		SelectedPhase:                selectedPhase,
		DefaultPhase:                 "",
		BuildExecutionWorkflow:       true,
		ResolveStatePath:             true,
		StatePathFromExecutionTarget: true,
	})
	if err != nil {
		return err
	}
	if err := verbosef(1, "deck: plan workflow=%s phase=%s state=%s\n", resolvedRequest.WorkflowPath, resolvedRequest.SelectedPhase, resolvedRequest.StatePath); err != nil {
		return err
	}
	applyExecutionWorkflow := resolvedRequest.ExecutionWorkflow

	state, err := applycli.LoadInstallDryRunState(applyExecutionWorkflow)
	if err != nil {
		return err
	}
	completed := make(map[string]bool, len(state.CompletedSteps))
	for _, stepID := range state.CompletedSteps {
		completed[stepID] = true
	}
	runtimeVars := map[string]any{}
	for k, v := range state.RuntimeVars {
		runtimeVars[k] = v
	}
	statePath := resolvedRequest.StatePath
	ctxData := map[string]any{"bundleRoot": "", "stateFile": statePath}
	type planStep struct {
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
	type planSummary struct {
		TotalSteps      int `json:"totalSteps"`
		RunSteps        int `json:"runSteps"`
		SkipSteps       int `json:"skipSteps"`
		SkipCompleted   int `json:"skipCompleted"`
		SkipWhen        int `json:"skipWhen"`
		PhaseCount      int `json:"phaseCount"`
		CompletedSteps  int `json:"completedSteps"`
		RuntimeVarCount int `json:"runtimeVarCount"`
	}
	type planReport struct {
		WorkflowPath   string      `json:"workflowPath"`
		SelectedPhase  string      `json:"selectedPhase,omitempty"`
		StatePath      string      `json:"statePath"`
		RuntimeVarKeys []string    `json:"runtimeVarKeys,omitempty"`
		Summary        planSummary `json:"summary"`
		Steps          []planStep  `json:"steps"`
	}
	steps := make([]planStep, 0)
	summary := planSummary{PhaseCount: len(applyExecutionWorkflow.Phases), CompletedSteps: len(state.CompletedSteps), RuntimeVarCount: len(runtimeVars)}
	for _, phase := range applyExecutionWorkflow.Phases {
		for _, step := range phase.Steps {
			entry := planStep{
				Phase:   phase.Name,
				ID:      step.ID,
				Kind:    step.Kind,
				When:    strings.TrimSpace(step.When),
				Retry:   step.Retry,
				Timeout: strings.TrimSpace(step.Timeout),
			}
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
			ok, evalErr := install.EvaluateWhen(step.When, applyExecutionWorkflow.Vars, runtimeVars, ctxData)
			if evalErr != nil {
				return fmt.Errorf("WHEN_EVAL_ERROR: step %s (%s): %w", step.ID, step.Kind, evalErr)
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
	report := planReport{
		WorkflowPath:   resolvedRequest.WorkflowPath,
		SelectedPhase:  resolvedRequest.SelectedPhase,
		StatePath:      statePath,
		RuntimeVarKeys: runtimeVarKeys,
		Summary:        summary,
		Steps:          steps,
	}
	for _, s := range steps {
		if err := verbosef(2, "deck: plan step=%s kind=%s phase=%s action=%s reason=%s when=%q retry=%d timeout=%q register=%d\n", s.ID, s.Kind, s.Phase, s.Action, s.Reason, s.When, s.Retry, s.Timeout, len(s.Register)); err != nil {
			return err
		}
	}

	if resolvedOutput == "json" {
		enc := stdoutJSONEncoder()
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	multiPhase := len(applyExecutionWorkflow.Phases) > 1
	currentPhase := ""
	if err := stdoutPrintf("PLAN workflow=%s state=%s selectedPhase=%s runtimeVars=%d completed=%d\n", resolvedRequest.WorkflowPath, statePath, displayValueOrDash(resolvedRequest.SelectedPhase), len(runtimeVarKeys), len(state.CompletedSteps)); err != nil {
		return err
	}
	for _, s := range steps {
		if multiPhase && s.Phase != currentPhase {
			currentPhase = s.Phase
			if err := stdoutPrintf("PHASE=%s\n", currentPhase); err != nil {
				return err
			}
		}
		if s.Action == "skip" && s.Reason != "" {
			if err := stdoutPrintf("%s %s SKIP (%s)\n", s.ID, s.Kind, s.Reason); err != nil {
				return err
			}
			continue
		}
		if err := stdoutPrintf("%s %s %s\n", s.ID, s.Kind, strings.ToUpper(s.Action)); err != nil {
			return err
		}
	}
	return stdoutPrintf("SUMMARY steps=%d run=%d skip=%d skipCompleted=%d skipWhen=%d phases=%d\n", summary.TotalSteps, summary.RunSteps, summary.SkipSteps, summary.SkipCompleted, summary.SkipWhen, summary.PhaseCount)
}

func displayValueOrDash(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "-"
	}
	return trimmed
}

type applyOptions struct {
	workflowPath  string
	scenario      string
	source        string
	selectedPhase string
	prefetch      bool
	dryRun        bool
	varOverrides  map[string]string
	positional    []string
}

func newApplyCommand() *cobra.Command {
	vars := &varFlag{}
	cmd := &cobra.Command{
		Use:   "apply [workflow] [bundle]",
		Short: "Execute an apply file against a bundle",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 2 {
				return errors.New("apply accepts at most two positional arguments: [workflow] [bundle]")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			workflowPath, err := cmdFlagValue(cmd, "workflow")
			if err != nil {
				return err
			}
			scenario, err := cmdFlagValue(cmd, "scenario")
			if err != nil {
				return err
			}
			source, err := cmdFlagValue(cmd, "source")
			if err != nil {
				return err
			}
			selectedPhase, err := cmdFlagValue(cmd, "phase")
			if err != nil {
				return err
			}
			prefetch, err := cmdFlagBoolValue(cmd, "prefetch")
			if err != nil {
				return err
			}
			dryRun, err := cmdFlagBoolValue(cmd, "dry-run")
			if err != nil {
				return err
			}
			return runApplyWithOptions(cmd.Context(), applyOptions{
				workflowPath:  workflowPath,
				scenario:      scenario,
				source:        source,
				selectedPhase: selectedPhase,
				prefetch:      prefetch,
				dryRun:        dryRun,
				varOverrides:  vars.AsMap(),
				positional:    args,
			})
		},
	}
	cmd.Flags().SetInterspersed(false)
	cmd.Flags().String("workflow", "", "path or URL to workflow file")
	cmd.Flags().String("scenario", "", "scenario name to execute")
	cmd.Flags().String("source", scenarioSourceLocal, "scenario source (local|server)")
	cmd.Flags().String("phase", "", "phase name to execute (defaults to all phases)")
	cmd.Flags().Bool("prefetch", false, "execute File download steps before other steps")
	cmd.Flags().Bool("dry-run", false, "print apply plan without executing steps")
	cmd.Flags().Var(vars, "var", "set variable override (key=value), repeatable")
	registerScenarioSourceCompletion(cmd, "source", false)
	registerScenarioNameCompletion(cmd, "scenario", "source", "", false)
	return cmd
}

func runApplyWithOptions(ctx context.Context, opts applyOptions) error {
	if len(opts.positional) > 2 {
		return errors.New("apply accepts at most two positional arguments: [workflow] [bundle]")
	}
	positionalArgs := make([]string, 0, len(opts.positional))
	for _, arg := range opts.positional {
		positionalArgs = append(positionalArgs, strings.TrimSpace(arg))
	}

	workflowPath, bundleRoot, err := resolveApplyWorkflowAndBundle(ctx, opts, positionalArgs)
	if err != nil {
		return err
	}
	return executeApply(ctx, workflowPath, bundleRoot, strings.TrimSpace(opts.selectedPhase), strings.TrimSpace(opts.scenario), strings.TrimSpace(opts.source), opts.prefetch, opts.dryRun, varsAsAnyMap(opts.varOverrides))
}

func executeApply(ctx context.Context, workflowPath, bundleRoot, selectedPhase, scenario, source string, prefetch, dryRun bool, varOverrides map[string]any) (err error) {
	resolvedRequest, err := applycli.ResolveExecutionRequest(ctx, applycli.ExecutionRequestOptions{
		CommandName:                  "apply",
		WorkflowPath:                 workflowPath,
		AllowRemoteWorkflow:          true,
		VarOverrides:                 varOverrides,
		SelectedPhase:                selectedPhase,
		DefaultPhase:                 "",
		BuildExecutionWorkflow:       true,
		ResolveStatePath:             true,
		StatePathFromExecutionTarget: false,
	})
	if err != nil {
		return err
	}
	if err := verbosef(1, "deck: apply workflow=%s phase=%s state=%s bundle=%s dryRun=%t prefetch=%t\n", resolvedRequest.WorkflowPath, resolvedRequest.SelectedPhase, resolvedRequest.StatePath, strings.TrimSpace(bundleRoot), dryRun, prefetch); err != nil {
		return err
	}

	wf := resolvedRequest.Workflow
	applyExecutionWorkflow := resolvedRequest.ExecutionWorkflow
	statePath := resolvedRequest.StatePath
	if dryRun {
		return runApplyDryRun(applyExecutionWorkflow, resolvedRequest.SelectedPhase, bundleRoot)
	}
	runLogger, err := newApplyRunLogger(resolvedRequest.WorkflowPath, inferWorkflowSource(resolvedRequest.WorkflowPath, source), scenario, bundleRoot, resolvedRequest.SelectedPhase)
	if err != nil {
		return err
	}
	if err := verbosef(1, "deck: apply runlog=%s\n", runLogger.Dir()); err != nil {
		return err
	}
	eventSink := combineStepEventSinks(runLogger.EventSink(), verboseApplyStepSink())
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

	if prefetch {
		prefetchWorkflow := applycli.BuildPrefetchWorkflow(wf)
		if len(prefetchWorkflow.Phases) > 0 && len(prefetchWorkflow.Phases[0].Steps) > 0 {
			if err := verbosef(1, "deck: apply prefetchSteps=%d\n", len(prefetchWorkflow.Phases[0].Steps)); err != nil {
				return err
			}
			if err := install.Run(ctx, prefetchWorkflow, install.RunOptions{BundleRoot: bundleRoot, StatePath: statePath, EventSink: eventSink}); err != nil {
				return err
			}
		}
	}

	if err := install.Run(ctx, applyExecutionWorkflow, install.RunOptions{BundleRoot: bundleRoot, StatePath: statePath, EventSink: eventSink}); err != nil {
		return err
	}

	return stdoutPrintln("apply: ok")
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

func verboseApplyStepSink() install.StepEventSink {
	if cliVerbosity < 1 {
		return nil
	}
	return func(event install.StepEvent) {
		status := strings.TrimSpace(event.Status)
		level := 1
		if status == "started" {
			level = 2
		}
		parts := []string{
			fmt.Sprintf("deck: apply step=%s", strings.TrimSpace(event.StepID)),
			fmt.Sprintf("kind=%s", strings.TrimSpace(event.Kind)),
			fmt.Sprintf("phase=%s", displayValueOrDash(event.Phase)),
			fmt.Sprintf("status=%s", displayValueOrDash(status)),
		}
		if event.Attempt > 0 {
			parts = append(parts, fmt.Sprintf("attempt=%d", event.Attempt))
		}
		if strings.TrimSpace(event.Reason) != "" {
			parts = append(parts, fmt.Sprintf("reason=%s", strings.TrimSpace(event.Reason)))
		}
		if strings.TrimSpace(event.Error) != "" {
			parts = append(parts, fmt.Sprintf("error=%s", strings.TrimSpace(event.Error)))
		}
		_ = verbosef(level, "%s\n", strings.Join(parts, " "))
	}
}

func resolvePlanWorkflowPath(workflowPath, scenario, source string) (string, error) {
	resolvedWorkflow := strings.TrimSpace(workflowPath)
	resolvedScenario := strings.TrimSpace(scenario)
	resolvedSource := strings.TrimSpace(source)
	if resolvedWorkflow != "" && resolvedScenario != "" {
		return "", errors.New("plan accepts either --workflow or --scenario, not both")
	}
	if resolvedWorkflow != "" {
		return resolvedWorkflow, nil
	}
	if resolvedScenario != "" {
		return resolveScenarioWorkflowReference(resolvedSource, resolvedScenario, ".")
	}
	if resolvedSource == scenarioSourceServer {
		return "", errors.New("plan with --source server requires --scenario or --workflow")
	}
	return applycli.DiscoverApplyWorkflow(context.Background(), ".")
}

func resolveApplyWorkflowAndBundle(ctx context.Context, opts applyOptions, positionalArgs []string) (string, string, error) {
	resolvedWorkflow := strings.TrimSpace(opts.workflowPath)
	resolvedScenario := strings.TrimSpace(opts.scenario)
	resolvedSource := strings.TrimSpace(opts.source)
	positionalWorkflow, positionalBundle, err := parseApplyPositionals(positionalArgs)
	if err != nil {
		return "", "", err
	}

	if resolvedWorkflow != "" && resolvedScenario != "" {
		return "", "", errors.New("apply accepts either --workflow or --scenario, not both")
	}
	if resolvedWorkflow != "" && positionalWorkflow != "" {
		return "", "", errors.New("apply accepts at most one workflow reference")
	}
	if resolvedWorkflow == "" && resolvedScenario == "" && strings.TrimSpace(resolvedSource) == scenarioSourceServer {
		return "", "", errors.New("apply with --source server requires --scenario or --workflow")
	}
	if (resolvedWorkflow != "" || resolvedScenario != "") && len(positionalArgs) > 1 {
		return "", "", errors.New("apply accepts at most one positional bundle path when --workflow or --scenario is set")
	}

	if resolvedWorkflow == "" {
		resolvedWorkflow = positionalWorkflow
	}
	if resolvedWorkflow != "" {
		bundleRoot := ""
		if strings.TrimSpace(positionalBundle) != "" {
			bundleRoot, err = applycli.ResolveBundleRoot(positionalBundle)
			if err != nil {
				if !applycli.IsHTTPWorkflowPath(resolvedWorkflow) {
					return "", "", err
				}
				bundleRoot = ""
			}
		}
		return resolvedWorkflow, bundleRoot, nil
	}

	bundleRoot, err := applycli.ResolveBundleRoot(positionalBundle)
	if err != nil {
		return "", "", err
	}
	if resolvedScenario != "" {
		localRoot := "."
		if resolvedSource == scenarioSourceLocal {
			localRoot = bundleRoot
		}
		workflowPath, err := resolveScenarioWorkflowReference(resolvedSource, resolvedScenario, localRoot)
		if err != nil {
			return "", "", err
		}
		return workflowPath, bundleRoot, nil
	}
	workflowPath, err := applycli.DiscoverApplyWorkflow(ctx, bundleRoot)
	if err != nil {
		return "", "", err
	}
	return workflowPath, bundleRoot, nil
}

func parseApplyPositionals(positionalArgs []string) (string, string, error) {
	positionalWorkflow := ""
	positionalBundle := ""

	if len(positionalArgs) == 1 {
		arg0 := strings.TrimSpace(positionalArgs[0])
		if applycli.IsHTTPWorkflowPath(arg0) || looksLikeWorkflowArgument(arg0) {
			positionalWorkflow = arg0
		} else {
			positionalBundle = arg0
		}
	}
	if len(positionalArgs) == 2 {
		arg0 := strings.TrimSpace(positionalArgs[0])
		arg1 := strings.TrimSpace(positionalArgs[1])
		if !applycli.IsHTTPWorkflowPath(arg0) && !looksLikeWorkflowArgument(arg0) {
			return "", "", errors.New("apply with two positional arguments requires [workflow] [bundle]")
		}
		positionalWorkflow = arg0
		positionalBundle = arg1
	}
	return positionalWorkflow, positionalBundle, nil
}

func looksLikeWorkflowArgument(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	return strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml")
}

func runApplyDryRun(wf *config.Workflow, selectedPhaseName string, bundleRoot string) error {
	if wf == nil || len(wf.Phases) == 0 {
		if selectedPhaseName == "" {
			return errors.New("no phases found")
		}
		return fmt.Errorf("%s phase not found", selectedPhaseName)
	}

	state, err := applycli.LoadInstallDryRunState(wf)
	if err != nil {
		return err
	}

	runtimeVars := map[string]any{}
	for key, value := range state.RuntimeVars {
		runtimeVars[key] = value
	}

	completed := make(map[string]bool, len(state.CompletedSteps))
	for _, stepID := range state.CompletedSteps {
		completed[stepID] = true
	}

	statePath, err := applycli.ResolveInstallStatePath(wf)
	if err != nil {
		return err
	}
	ctxData := map[string]any{"bundleRoot": bundleRoot, "stateFile": statePath}

	for _, phase := range wf.Phases {
		if err := stdoutPrintf("PHASE=%s\n", phase.Name); err != nil {
			return err
		}
		for _, step := range phase.Steps {
			if completed[step.ID] {
				if err := stdoutPrintf("%s %s SKIP (completed)\n", step.ID, step.Kind); err != nil {
					return err
				}
				continue
			}

			ok, evalErr := install.EvaluateWhen(step.When, wf.Vars, runtimeVars, ctxData)
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

type lintReport struct {
	Status     string        `json:"status"`
	Mode       string        `json:"mode"`
	Root       string        `json:"root,omitempty"`
	Entrypoint string        `json:"entrypoint,omitempty"`
	Scenario   string        `json:"scenario,omitempty"`
	Workflows  []string      `json:"workflows"`
	Summary    lintSummary   `json:"summary"`
	Contracts  lintContracts `json:"contracts"`
	Findings   []lintFinding `json:"findings"`
}

type lintSummary struct {
	WorkflowCount int `json:"workflowCount"`
	WarningCount  int `json:"warningCount"`
	ErrorCount    int `json:"errorCount"`
}

type lintContracts struct {
	SupportedVersion string   `json:"supportedVersion"`
	SupportedRoles   []string `json:"supportedRoles"`
	TopLevelModes    []string `json:"topLevelModes"`
	ImportRule       string   `json:"importRule"`
	InvariantNotes   []string `json:"invariantNotes"`
}

type lintFinding struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Hint     string `json:"hint,omitempty"`
	Path     string `json:"path,omitempty"`
	Phase    string `json:"phase,omitempty"`
	StepID   string `json:"stepId,omitempty"`
	Kind     string `json:"kind,omitempty"`
}

func executeLint(ctx context.Context, root string, file string, scenario string, output string) error {
	resolvedOutput := strings.ToLower(strings.TrimSpace(output))
	if resolvedOutput == "" {
		resolvedOutput = "text"
	}
	if resolvedOutput != "text" && resolvedOutput != "json" {
		return errors.New("--output must be text or json")
	}
	resolvedFile := strings.TrimSpace(file)
	resolvedScenario := strings.TrimSpace(scenario)
	if err := verbosef(1, "deck: lint root=%s file=%s scenario=%s\n", strings.TrimSpace(root), resolvedFile, resolvedScenario); err != nil {
		return err
	}
	if resolvedScenario != "" {
		if resolvedFile != "" {
			return fmt.Errorf("lint accepts either --file or a scenario name, not both")
		}
		resolvedPath, err := resolveLintScenarioPath(root, resolvedScenario)
		if err != nil {
			return err
		}
		if err := verbosef(1, "deck: lint entrypoint=%s\n", resolvedPath); err != nil {
			return err
		}
		files, err := validate.Entrypoint(resolvedPath)
		if err != nil {
			return err
		}
		return writeLintReport(resolvedOutput, lintReport{Mode: "scenario", Root: strings.TrimSpace(root), Entrypoint: resolvedPath, Scenario: resolvedScenario, Workflows: files, Summary: lintSummary{WorkflowCount: len(files)}})
	}
	if resolvedFile != "" {
		if isLocalComponentWorkflowPath(resolvedFile) {
			return fmt.Errorf("lint entrypoints must live under workflows/scenarios/: %s", resolvedFile)
		}
		if isLocalScenarioWorkflowPath(resolvedFile) {
			if err := verbosef(1, "deck: lint entrypoint=%s\n", resolvedFile); err != nil {
				return err
			}
			files, err := validate.Entrypoint(resolvedFile)
			if err != nil {
				return err
			}
			return writeLintReport(resolvedOutput, lintReport{Mode: "entrypoint", Entrypoint: resolvedFile, Workflows: files, Summary: lintSummary{WorkflowCount: len(files)}})
		}
		if err := validate.File(resolvedFile); err != nil {
			return err
		}
		wf, err := config.Load(ctx, resolvedFile)
		if err != nil {
			return err
		}
		if err := validate.Workflow(resolvedFile, wf); err != nil {
			return err
		}
		return writeLintReport(resolvedOutput, lintReport{Mode: "file", Entrypoint: resolvedFile, Workflows: []string{resolvedFile}, Summary: lintSummary{WorkflowCount: 1}})
	}

	files, err := validate.Workspace(root)
	if err != nil {
		return err
	}
	if err := verbosef(1, "deck: lint workspace=%s workflows=%d\n", strings.TrimSpace(root), len(files)); err != nil {
		return err
	}
	return writeLintReport(resolvedOutput, lintReport{Mode: "workspace", Root: strings.TrimSpace(root), Workflows: files, Summary: lintSummary{WorkflowCount: len(files)}})
}

func writeLintReport(output string, report lintReport) error {
	if len(report.Workflows) == 0 {
		report.Workflows = []string{}
	}
	if len(report.Findings) == 0 {
		report.Findings = []lintFinding{}
	}
	if report.Contracts.SupportedVersion == "" {
		report.Contracts = lintContracts{
			SupportedVersion: validate.SupportedWorkflowVersion(),
			SupportedRoles:   validate.SupportedWorkflowRoles(),
			TopLevelModes:    validate.WorkflowTopLevelModes(),
			ImportRule:       validate.WorkflowImportRule(),
			InvariantNotes:   validate.WorkflowInvariantNotes(),
		}
	}
	if report.Summary.WorkflowCount == 0 {
		report.Summary.WorkflowCount = len(report.Workflows)
	}
	if len(report.Findings) == 0 {
		findings, err := validate.AnalyzeFiles(report.Workflows)
		if err != nil {
			return err
		}
		for _, finding := range findings {
			report.Findings = append(report.Findings, lintFinding{
				Severity: finding.Severity,
				Code:     finding.Code,
				Message:  finding.Message,
				Hint:     finding.Hint,
				Path:     finding.Path,
				Phase:    finding.Phase,
				StepID:   finding.StepID,
				Kind:     finding.Kind,
			})
		}
	}
	report.Summary.WarningCount = 0
	report.Summary.ErrorCount = 0
	for _, finding := range report.Findings {
		switch strings.ToLower(strings.TrimSpace(finding.Severity)) {
		case "error":
			report.Summary.ErrorCount++
		default:
			report.Summary.WarningCount++
		}
	}
	report.Status = "ok"
	if err := verbosef(2, "deck: lint summary mode=%s workflows=%d warnings=%d errors=%d version=%s\n", report.Mode, report.Summary.WorkflowCount, report.Summary.WarningCount, report.Summary.ErrorCount, report.Contracts.SupportedVersion); err != nil {
		return err
	}
	for _, workflow := range report.Workflows {
		if err := verbosef(2, "deck: lint workflow=%s\n", workflow); err != nil {
			return err
		}
	}
	for _, finding := range report.Findings {
		if err := verbosef(2, "deck: lint finding code=%s severity=%s path=%s phase=%s step=%s kind=%s\n", finding.Code, finding.Severity, displayValueOrDash(finding.Path), displayValueOrDash(finding.Phase), displayValueOrDash(finding.StepID), displayValueOrDash(finding.Kind)); err != nil {
			return err
		}
	}
	if output == "json" {
		enc := stdoutJSONEncoder()
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	if report.Summary.WorkflowCount == 1 && report.Entrypoint != "" && report.Mode == "file" {
		if err := stdoutPrintf("lint: ok (%s)\n", report.Entrypoint); err != nil {
			return err
		}
	} else {
		if err := stdoutPrintf("lint: ok (%d workflows)\n", report.Summary.WorkflowCount); err != nil {
			return err
		}
	}
	return stdoutPrintf("SUMMARY mode=%s workflows=%d warnings=%d errors=%d supportedVersion=%s roles=%s topLevelModes=%s\n", report.Mode, report.Summary.WorkflowCount, report.Summary.WarningCount, report.Summary.ErrorCount, report.Contracts.SupportedVersion, strings.Join(report.Contracts.SupportedRoles, ","), strings.Join(report.Contracts.TopLevelModes, ","))
}

func resolveLintScenarioPath(root string, scenario string) (string, error) {
	trimmed := strings.TrimSpace(scenario)
	if trimmed == "" {
		return "", fmt.Errorf("scenario name is required")
	}
	if strings.Contains(trimmed, "..") || strings.Contains(trimmed, "\\") || strings.Contains(trimmed, "/") {
		return "", fmt.Errorf("scenario shorthand must not contain path separators: %s", trimmed)
	}

	resolvedRoot := strings.TrimSpace(root)
	if resolvedRoot == "" {
		resolvedRoot = "."
	}
	workflowDir := filepath.Join(resolvedRoot, workflowRootDir, workflowScenariosDir)
	for _, suffix := range []string{"", ".yaml", ".yml"} {
		candidate := filepath.Join(workflowDir, trimmed+suffix)
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("scenario not found under %s: %s", workflowDir, trimmed)
}

func isLocalComponentWorkflowPath(path string) bool {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" || strings.Contains(trimmed, "://") {
		return false
	}
	resolved, err := filepath.Abs(trimmed)
	if err != nil {
		return false
	}
	marker := string(filepath.Separator) + workflowRootDir + string(filepath.Separator) + workflowComponentsDir + string(filepath.Separator)
	return strings.Contains(resolved, marker)
}

func isLocalScenarioWorkflowPath(path string) bool {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" || strings.Contains(trimmed, "://") {
		return false
	}
	resolved, err := filepath.Abs(trimmed)
	if err != nil {
		return false
	}
	marker := string(filepath.Separator) + workflowRootDir + string(filepath.Separator) + workflowScenariosDir + string(filepath.Separator)
	return strings.Contains(resolved, marker)
}
