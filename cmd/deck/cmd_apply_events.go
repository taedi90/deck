package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/applycli"
	"github.com/Airgap-Castaways/deck/internal/install"
	"github.com/Airgap-Castaways/deck/internal/lintcli"
)

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
