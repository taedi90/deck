package prepare

import (
	"context"
	"fmt"

	"github.com/taedi90/deck/internal/config"
	"github.com/taedi90/deck/internal/workflowexec"
)

func runPrepareStep(ctx context.Context, runner CommandRunner, bundleRoot, kind string, rendered map[string]any, opts RunOptions) ([]string, map[string]any, error) {
	return runPrepareRenderedStep(ctx, runner, bundleRoot, config.Step{Kind: kind, Spec: rendered}, rendered, nil, opts)
}

func runPrepareRenderedStep(ctx context.Context, runner CommandRunner, bundleRoot string, step config.Step, rendered map[string]any, inputVars map[string]string, opts RunOptions) ([]string, map[string]any, error) {
	kind := step.Kind
	if !workflowexec.StepAllowedForRole("prepare", kind) {
		return nil, nil, fmt.Errorf("%s: unsupported step kind %s", errCodePrepareKindUnsupported, kind)
	}

	switch kind {
	case "DownloadFile":
		f, err := runDownloadFile(ctx, bundleRoot, rendered, opts)
		if err != nil {
			return nil, nil, err
		}
		return []string{f}, map[string]any{"outputPath": f, "artifacts": []string{f}}, nil
	case "DownloadPackage":
		files, err := runDownloadPackage(ctx, runner, bundleRoot, step, rendered, inputVars, "packages", opts)
		if err != nil {
			return nil, nil, err
		}
		return files, map[string]any{"artifacts": files}, nil
	case "DownloadImage":
		files, err := runDownloadImage(ctx, runner, bundleRoot, rendered, opts)
		if err != nil {
			return nil, nil, err
		}
		return files, map[string]any{"artifacts": files}, nil
	case "CheckHost":
		decoded, err := workflowexec.DecodeSpec[checksSpec](rendered)
		if err != nil {
			return nil, nil, fmt.Errorf("decode checks spec: %w", err)
		}
		outputs, err := runCheckHostDecoded(runner, decoded, resolveCheckHostRuntime(opts))
		if err != nil {
			return nil, nil, err
		}
		return nil, outputs, nil
	default:
		return nil, nil, fmt.Errorf("%s: unsupported step kind %s", errCodePrepareKindUnsupported, kind)
	}
}
