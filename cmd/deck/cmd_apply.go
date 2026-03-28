package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Airgap-Castaways/deck/internal/applycli"
)

type diffOptions struct {
	workflowPath  string
	scenario      string
	source        string
	fresh         bool
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
			fresh, err := cmdFlagBoolValue(cmd, "fresh")
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
				fresh:         fresh,
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
	cmd.Flags().Bool("fresh", false, "ignore saved apply state for this invocation")
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
	return executeDiff(ctx, workflowPath, selectedPhase, opts.output, opts.fresh, varsAsAnyMap(opts.varOverrides))
}

func executeDiff(ctx context.Context, workflowPath, selectedPhase, output string, fresh bool, varOverrides map[string]any) error {
	return applycli.RunPlanCommand(ctx, applycli.PlanCommandOptions{
		WorkflowPath:    workflowPath,
		SelectedPhase:   selectedPhase,
		Output:          output,
		Fresh:           fresh,
		VarOverrides:    varOverrides,
		Verbosef:        verbosef,
		StdoutPrintf:    stdoutPrintf,
		JSONEncoderFunc: stdoutJSONEncoder,
		ResolveOutput:   resolveOutputFormat,
	})
}

type applyOptions struct {
	workflowPath  string
	scenario      string
	source        string
	selectedPhase string
	fresh         bool
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
			fresh, err := cmdFlagBoolValue(cmd, "fresh")
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
				fresh:         fresh,
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
	cmd.Flags().Bool("fresh", false, "ignore saved apply state for this invocation")
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
	return applycli.RunApplyCommand(ctx, applycli.ApplyCommandOptions{
		WorkflowPath:   workflowPath,
		BundleRoot:     bundleRoot,
		WorkflowSource: inferWorkflowSource(workflowPath, strings.TrimSpace(opts.source)),
		Scenario:       strings.TrimSpace(opts.scenario),
		SelectedPhase:  opts.selectedPhase,
		Fresh:          opts.fresh,
		DryRun:         opts.dryRun,
		VarOverrides:   varsAsAnyMap(opts.varOverrides),
		Verbosef:       verbosef,
		StdoutPrintf:   stdoutPrintf,
		StdoutPrintln:  stdoutPrintln,
		AdditionalSink: verboseApplyStepSink(),
		NewRunLogger: func(workflowPath, workflowSource, scenario, bundleRoot, selectedPhase string) (applycli.RunLogger, error) {
			return newApplyRunLogger(workflowPath, workflowSource, scenario, bundleRoot, selectedPhase)
		},
	})
}
