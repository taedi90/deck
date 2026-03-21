package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/taedi90/deck/internal/applycli"
	"github.com/taedi90/deck/internal/install"
	"github.com/taedi90/deck/internal/lintcli"
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
	workflowPath, err := resolvePlanWorkflowPath(ctx, strings.TrimSpace(opts.workflowPath), strings.TrimSpace(opts.scenario), strings.TrimSpace(opts.source))
	if err != nil {
		return err
	}
	selectedPhase := strings.TrimSpace(opts.selectedPhase)
	return executeDiff(ctx, workflowPath, selectedPhase, opts.output, varsAsAnyMap(opts.varOverrides))
}

func executeDiff(ctx context.Context, workflowPath, selectedPhase, output string, varOverrides map[string]any) error {
	resolvedOutput, err := resolveOutputFormat(output)
	if err != nil {
		return err
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
	return applycli.ExecutePlan(ctx, applycli.PlanOptions{
		Request:         resolvedRequest,
		Output:          resolvedOutput,
		Verbosef:        verbosef,
		StdoutPrintf:    stdoutPrintf,
		JSONEncoderFunc: stdoutJSONEncoder,
	})
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
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
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
	resolvedRequest, err := applycli.ResolveExecutionRequest(ctx, applycli.ExecutionRequestOptions{
		CommandName:                  "apply",
		WorkflowPath:                 workflowPath,
		AllowRemoteWorkflow:          true,
		VarOverrides:                 varsAsAnyMap(opts.varOverrides),
		SelectedPhase:                strings.TrimSpace(opts.selectedPhase),
		DefaultPhase:                 "",
		BuildExecutionWorkflow:       true,
		ResolveStatePath:             true,
		StatePathFromExecutionTarget: false,
	})
	if err != nil {
		return err
	}
	return applycli.Execute(ctx, applycli.ExecuteOptions{
		Request:        resolvedRequest,
		BundleRoot:     bundleRoot,
		WorkflowSource: inferWorkflowSource(resolvedRequest.WorkflowPath, strings.TrimSpace(opts.source)),
		Scenario:       strings.TrimSpace(opts.scenario),
		Prefetch:       opts.prefetch,
		DryRun:         opts.dryRun,
		Verbosef:       verbosef,
		StdoutPrintf:   stdoutPrintf,
		StdoutPrintln:  stdoutPrintln,
		AdditionalSink: verboseApplyStepSink(),
		NewRunLogger: func(workflowPath, workflowSource, scenario, bundleRoot, selectedPhase string) (applycli.RunLogger, error) {
			return newApplyRunLogger(workflowPath, workflowSource, scenario, bundleRoot, selectedPhase)
		},
	})
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

func displayValueOrDash(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "-"
	}
	return trimmed
}

func resolvePlanWorkflowPath(ctx context.Context, workflowPath, scenario, source string) (string, error) {
	return applycli.ResolvePlanWorkflowPath(ctx, applycli.InvocationOptions{
		WorkflowPath:     workflowPath,
		Scenario:         scenario,
		Source:           source,
		DefaultLocalRoot: ".",
		ResolveScenario:  resolveScenarioWorkflowReference,
	})
}

func resolveApplyWorkflowAndBundle(ctx context.Context, opts applyOptions, positionalArgs []string) (string, string, error) {
	return applycli.ResolveApplyWorkflowAndBundle(ctx, applycli.InvocationOptions{
		WorkflowPath:    opts.workflowPath,
		Scenario:        opts.scenario,
		Source:          opts.source,
		PositionalArgs:  positionalArgs,
		ResolveScenario: resolveScenarioWorkflowReference,
	})
}

func executeLint(ctx context.Context, root string, file string, scenario string, output string) error {
	resolvedOutput, err := resolveOutputFormat(output)
	if err != nil {
		return err
	}
	return lintcli.Execute(ctx, lintcli.Options{
		Root:            root,
		File:            file,
		Scenario:        scenario,
		Output:          resolvedOutput,
		Verbosef:        verbosef,
		StdoutPrintf:    stdoutPrintf,
		JSONEncoderFunc: stdoutJSONEncoder,
		WorkflowRootDir: workflowRootDir,
		ScenarioDirName: workflowScenariosDir,
	})
}
