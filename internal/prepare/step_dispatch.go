package prepare

import (
	"context"
	"fmt"

	"github.com/taedi90/deck/internal/config"
	"github.com/taedi90/deck/internal/errcode"
	"github.com/taedi90/deck/internal/stepspec"
	"github.com/taedi90/deck/internal/workflowexec"
)

func runPrepareRenderedStepWithKey(ctx context.Context, runner CommandRunner, bundleRoot string, step config.Step, rendered map[string]any, key workflowexec.StepTypeKey, inputVars map[string]string, opts RunOptions) ([]string, map[string]any, error) {
	kind := step.Kind
	if !workflowexec.StepAllowedForRoleForKey("prepare", key) {
		return nil, nil, errcode.Newf(errCodePrepareKindUnsupported, "unsupported step kind %s", kind)
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
		decoded, err := workflowexec.DecodeSpec[stepspec.CheckHost](rendered)
		if err != nil {
			return nil, nil, fmt.Errorf("decode checks spec: %w", err)
		}
		outputs, err := runCheckHostDecoded(runner, decoded, resolveCheckHostRuntime(opts))
		if err != nil {
			return nil, nil, err
		}
		return nil, outputs, nil
	default:
		return nil, nil, errcode.Newf(errCodePrepareKindUnsupported, "unsupported step kind %s", kind)
	}
}
