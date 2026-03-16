package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	type diffStep struct {
		Phase  string `json:"phase,omitempty"`
		ID     string `json:"id"`
		Kind   string `json:"kind"`
		Action string `json:"action"`
		Reason string `json:"reason,omitempty"`
	}
	steps := make([]diffStep, 0)
	for _, phase := range applyExecutionWorkflow.Phases {
		for _, step := range phase.Steps {
			entry := diffStep{Phase: phase.Name, ID: step.ID, Kind: step.Kind}
			if completed[step.ID] {
				entry.Action = "skip"
				entry.Reason = "completed"
				steps = append(steps, entry)
				continue
			}
			ok, evalErr := install.EvaluateWhen(step.When, applyExecutionWorkflow.Vars, runtimeVars, ctxData)
			if evalErr != nil {
				return fmt.Errorf("WHEN_EVAL_ERROR: step %s (%s): %w", step.ID, step.Kind, evalErr)
			}
			if !ok {
				entry.Action = "skip"
				entry.Reason = "when"
				steps = append(steps, entry)
				continue
			}
			entry.Action = "run"
			steps = append(steps, entry)
		}
	}

	if output == "json" {
		payload := struct {
			Phase     string     `json:"phase"`
			StatePath string     `json:"statePath"`
			Steps     []diffStep `json:"steps"`
		}{Phase: resolvedRequest.SelectedPhase, StatePath: statePath, Steps: steps}
		enc := json.NewEncoder(os.Stdout)
		return enc.Encode(payload)
	}
	multiPhase := len(applyExecutionWorkflow.Phases) > 1
	currentPhase := ""
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
	return nil
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
			if err := install.Run(ctx, prefetchWorkflow, install.RunOptions{BundleRoot: bundleRoot, StatePath: statePath, EventSink: runLogger.EventSink()}); err != nil {
				return err
			}
		}
	}

	if err := install.Run(ctx, applyExecutionWorkflow, install.RunOptions{BundleRoot: bundleRoot, StatePath: statePath, EventSink: runLogger.EventSink()}); err != nil {
		return err
	}

	return stdoutPrintln("apply: ok")
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

func executeLint(ctx context.Context, root string, file string, scenario string) error {
	resolvedFile := strings.TrimSpace(file)
	resolvedScenario := strings.TrimSpace(scenario)
	if resolvedScenario != "" {
		if resolvedFile != "" {
			return fmt.Errorf("lint accepts either --file or a scenario name, not both")
		}
		resolvedPath, err := resolveLintScenarioPath(root, resolvedScenario)
		if err != nil {
			return err
		}
		files, err := validate.Entrypoint(resolvedPath)
		if err != nil {
			return err
		}
		return stdoutPrintf("lint: ok (%d workflows)\n", len(files))
	}
	if resolvedFile != "" {
		if isLocalComponentWorkflowPath(resolvedFile) {
			return fmt.Errorf("lint entrypoints must live under workflows/scenarios/: %s", resolvedFile)
		}
		if isLocalScenarioWorkflowPath(resolvedFile) {
			files, err := validate.Entrypoint(resolvedFile)
			if err != nil {
				return err
			}
			return stdoutPrintf("lint: ok (%d workflows)\n", len(files))
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
		return stdoutPrintf("lint: ok (%s)\n", resolvedFile)
	}

	files, err := validate.Workspace(root)
	if err != nil {
		return err
	}
	return stdoutPrintf("lint: ok (%d workflows)\n", len(files))
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
