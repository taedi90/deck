package applycli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type InvocationOptions struct {
	WorkflowPath     string
	Scenario         string
	Source           string
	PositionalArgs   []string
	ResolveScenario  func(source, scenario, localRoot string) (string, error)
	DefaultLocalRoot string
}

func ResolvePlanWorkflowPath(ctx context.Context, opts InvocationOptions) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("context is nil")
	}
	resolvedWorkflow := strings.TrimSpace(opts.WorkflowPath)
	resolvedScenario := strings.TrimSpace(opts.Scenario)
	resolvedSource := strings.TrimSpace(opts.Source)
	localRoot := strings.TrimSpace(opts.DefaultLocalRoot)
	if localRoot == "" {
		localRoot = "."
	}
	if resolvedWorkflow != "" && resolvedScenario != "" {
		return "", fmt.Errorf("plan accepts either --workflow or --scenario, not both")
	}
	if resolvedWorkflow != "" {
		return resolvedWorkflow, nil
	}
	if resolvedScenario != "" {
		if opts.ResolveScenario == nil {
			return "", fmt.Errorf("scenario resolver is nil")
		}
		return opts.ResolveScenario(resolvedSource, resolvedScenario, localRoot)
	}
	if resolvedSource == "server" {
		return "", fmt.Errorf("plan with --source server requires --scenario or --workflow")
	}
	return DiscoverApplyWorkflow(ctx, localRoot)
}

func ResolveApplyWorkflowAndBundle(ctx context.Context, opts InvocationOptions) (string, string, error) {
	if ctx == nil {
		return "", "", fmt.Errorf("context is nil")
	}
	resolvedWorkflow := strings.TrimSpace(opts.WorkflowPath)
	resolvedScenario := strings.TrimSpace(opts.Scenario)
	resolvedSource := strings.TrimSpace(opts.Source)
	positionalWorkflow, positionalBundle, err := parseApplyPositionals(opts.PositionalArgs)
	if err != nil {
		return "", "", err
	}
	if resolvedWorkflow != "" && resolvedScenario != "" {
		return "", "", fmt.Errorf("apply accepts either --workflow or --scenario, not both")
	}
	if resolvedWorkflow != "" && positionalWorkflow != "" {
		return "", "", fmt.Errorf("apply accepts at most one workflow reference")
	}
	if resolvedWorkflow == "" && resolvedScenario == "" && resolvedSource == "server" {
		return "", "", fmt.Errorf("apply with --source server requires --scenario or --workflow")
	}
	if (resolvedWorkflow != "" || resolvedScenario != "") && len(opts.PositionalArgs) > 1 {
		return "", "", fmt.Errorf("apply accepts at most one positional bundle path when --workflow or --scenario is set")
	}

	if resolvedWorkflow == "" {
		resolvedWorkflow = positionalWorkflow
	}
	if resolvedWorkflow != "" {
		bundleRoot := ""
		if strings.TrimSpace(positionalBundle) != "" {
			bundleRoot, err = ResolveBundleRoot(positionalBundle)
			if err != nil {
				if !IsHTTPWorkflowPath(resolvedWorkflow) {
					return "", "", err
				}
				bundleRoot = ""
			}
		}
		return resolvedWorkflow, bundleRoot, nil
	}

	bundleRoot, err := ResolveBundleRoot(positionalBundle)
	if err != nil {
		return "", "", err
	}
	if resolvedScenario != "" {
		if opts.ResolveScenario == nil {
			return "", "", fmt.Errorf("scenario resolver is nil")
		}
		localRoot := "."
		if resolvedSource == "local" {
			localRoot = bundleRoot
		}
		workflowPath, err := opts.ResolveScenario(resolvedSource, resolvedScenario, localRoot)
		if err != nil {
			return "", "", err
		}
		return workflowPath, bundleRoot, nil
	}
	workflowPath, err := DiscoverApplyWorkflow(ctx, bundleRoot)
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
		if IsHTTPWorkflowPath(arg0) || looksLikeWorkflowArgument(arg0) {
			positionalWorkflow = arg0
		} else {
			positionalBundle = arg0
		}
	}
	if len(positionalArgs) == 2 {
		arg0 := strings.TrimSpace(positionalArgs[0])
		arg1 := strings.TrimSpace(positionalArgs[1])
		if !IsHTTPWorkflowPath(arg0) && !looksLikeWorkflowArgument(arg0) {
			return "", "", fmt.Errorf("apply with two positional arguments requires [workflow] [bundle]")
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
	if strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") {
		return true
	}
	resolved, err := filepath.Abs(trimmed)
	if err != nil {
		return false
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
