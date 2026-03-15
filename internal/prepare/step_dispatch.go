package prepare

import (
	"context"
	"fmt"

	"github.com/taedi90/deck/internal/workflowexec"
)

func runPrepareStep(ctx context.Context, runner CommandRunner, bundleRoot, kind string, rendered map[string]any, opts RunOptions) ([]string, map[string]any, error) {
	if !workflowexec.StepAllowedForRole("prepare", kind, rendered) {
		return nil, nil, fmt.Errorf("%s: unsupported step kind %s", errCodePrepareKindUnsupported, kind)
	}

	switch kind {
	case "File":
		f, err := runFileDownload(ctx, bundleRoot, rendered, opts)
		if err != nil {
			return nil, nil, err
		}
		return []string{f}, map[string]any{"path": f, "artifacts": []string{f}}, nil
	case "Packages":
		files, err := runPackagesDownload(ctx, runner, bundleRoot, rendered, "packages", opts)
		if err != nil {
			return nil, nil, err
		}
		return files, map[string]any{"artifacts": files}, nil
	case "Image":
		files, err := runImageDownload(ctx, runner, bundleRoot, rendered, opts)
		if err != nil {
			return nil, nil, err
		}
		return files, map[string]any{"artifacts": files}, nil
	case "Inspection":
		outputs, err := runCheckHost(runner, rendered)
		if err != nil {
			return nil, nil, err
		}
		return nil, outputs, nil
	default:
		return nil, nil, fmt.Errorf("%s: unsupported step kind %s", errCodePrepareKindUnsupported, kind)
	}
}
