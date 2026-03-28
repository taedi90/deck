package applycli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/install"
)

type PlanCommandOptions struct {
	WorkflowPath    string
	SelectedPhase   string
	Output          string
	Fresh           bool
	VarOverrides    map[string]any
	Verbosef        func(level int, format string, args ...any) error
	StdoutPrintf    func(format string, args ...any) error
	JSONEncoderFunc func() *json.Encoder
	ResolveOutput   func(string) (string, error)
}

type ApplyCommandOptions struct {
	WorkflowPath   string
	BundleRoot     string
	WorkflowSource string
	Scenario       string
	SelectedPhase  string
	Fresh          bool
	DryRun         bool
	VarOverrides   map[string]any
	Verbosef       func(level int, format string, args ...any) error
	StdoutPrintf   func(format string, args ...any) error
	StdoutPrintln  func(args ...any) error
	AdditionalSink install.StepEventSink
	NewRunLogger   func(workflowPath, workflowSource, scenario, bundleRoot, selectedPhase string) (RunLogger, error)
}

func RunPlanCommand(ctx context.Context, opts PlanCommandOptions) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	if opts.ResolveOutput == nil {
		return fmt.Errorf("resolve output format is nil")
	}
	resolvedOutput, err := opts.ResolveOutput(opts.Output)
	if err != nil {
		return err
	}
	resolvedRequest, err := ResolveExecutionRequest(ctx, ExecutionRequestOptions{
		CommandName:                  "diff",
		WorkflowPath:                 strings.TrimSpace(opts.WorkflowPath),
		VarOverrides:                 opts.VarOverrides,
		Fresh:                        opts.Fresh,
		SelectedPhase:                strings.TrimSpace(opts.SelectedPhase),
		DefaultPhase:                 "",
		BuildExecutionWorkflow:       true,
		ResolveStatePath:             true,
		StatePathFromExecutionTarget: true,
	})
	if err != nil {
		return err
	}
	return ExecutePlan(ctx, PlanOptions{
		Request:         resolvedRequest,
		Output:          resolvedOutput,
		Verbosef:        opts.Verbosef,
		StdoutPrintf:    opts.StdoutPrintf,
		JSONEncoderFunc: opts.JSONEncoderFunc,
	})
}

func RunApplyCommand(ctx context.Context, opts ApplyCommandOptions) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	resolvedRequest, err := ResolveExecutionRequest(ctx, ExecutionRequestOptions{
		CommandName:                  "apply",
		WorkflowPath:                 strings.TrimSpace(opts.WorkflowPath),
		AllowRemoteWorkflow:          true,
		VarOverrides:                 opts.VarOverrides,
		Fresh:                        opts.Fresh,
		SelectedPhase:                strings.TrimSpace(opts.SelectedPhase),
		DefaultPhase:                 "",
		BuildExecutionWorkflow:       true,
		ResolveStatePath:             true,
		StatePathFromExecutionTarget: false,
	})
	if err != nil {
		return err
	}
	return Execute(ctx, ExecuteOptions{
		Request:        resolvedRequest,
		BundleRoot:     opts.BundleRoot,
		WorkflowSource: strings.TrimSpace(opts.WorkflowSource),
		Scenario:       strings.TrimSpace(opts.Scenario),
		DryRun:         opts.DryRun,
		Verbosef:       opts.Verbosef,
		StdoutPrintf:   opts.StdoutPrintf,
		StdoutPrintln:  opts.StdoutPrintln,
		AdditionalSink: opts.AdditionalSink,
		NewRunLogger:   opts.NewRunLogger,
	})
}
