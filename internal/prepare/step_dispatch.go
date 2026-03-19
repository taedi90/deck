package prepare

import (
	"context"
	"fmt"

	"github.com/taedi90/deck/internal/workflowexec"
)

func runPrepareStep(ctx context.Context, runner CommandRunner, bundleRoot, kind string, rendered map[string]any, opts RunOptions) ([]string, map[string]any, error) {
	if !workflowexec.StepAllowedForRole("prepare", kind) {
		return nil, nil, fmt.Errorf("%s: unsupported step kind %s", errCodePrepareKindUnsupported, kind)
	}

	switch kind {
	case "FileDownload":
		f, err := runFileDownload(ctx, bundleRoot, rendered, opts)
		if err != nil {
			return nil, nil, err
		}
		return []string{f}, map[string]any{"path": f, "artifacts": []string{f}}, nil
	case "PackagesDownload":
		files, err := runPackagesDownload(ctx, runner, bundleRoot, rendered, "packages", opts)
		if err != nil {
			return nil, nil, err
		}
		return files, map[string]any{"artifacts": files}, nil
	case "ImageDownload":
		files, err := runImageDownload(ctx, runner, bundleRoot, rendered, opts)
		if err != nil {
			return nil, nil, err
		}
		return files, map[string]any{"artifacts": files}, nil
	case "Checks":
		decoded, err := workflowexec.DecodeSpec[checksSpec](rendered)
		if err != nil {
			return nil, nil, fmt.Errorf("decode checks spec: %w", err)
		}
		outputs, err := runChecksDecoded(runner, decoded, resolveChecksRuntime(opts))
		if err != nil {
			return nil, nil, err
		}
		return nil, outputs, nil
	default:
		return nil, nil, fmt.Errorf("%s: unsupported step kind %s", errCodePrepareKindUnsupported, kind)
	}
}
