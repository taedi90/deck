package prepare

import (
	"context"
	"fmt"

	"github.com/taedi90/deck/internal/workflowexec"
)

type prepareActionSelectorSpec struct {
	Action string `json:"action"`
}

func runPrepareStep(ctx context.Context, runner CommandRunner, bundleRoot, kind string, rendered map[string]any, opts RunOptions) ([]string, map[string]any, error) {
	if !workflowexec.StepAllowedForRole("prepare", kind, rendered) {
		return nil, nil, fmt.Errorf("%s: unsupported step kind %s", errCodePrepareKindUnsupported, kind)
	}

	switch kind {
	case "File":
		action, err := decodePrepareAction(rendered)
		if err != nil {
			return nil, nil, fmt.Errorf("decode File action: %w", err)
		}
		if action != "download" {
			return nil, nil, fmt.Errorf("unsupported File action %q", action)
		}
		f, err := runFileDownload(ctx, bundleRoot, rendered, opts)
		if err != nil {
			return nil, nil, err
		}
		return []string{f}, map[string]any{"path": f, "artifacts": []string{f}}, nil
	case "Packages":
		action, err := decodePrepareAction(rendered)
		if err != nil {
			return nil, nil, fmt.Errorf("decode Packages action: %w", err)
		}
		if action != "download" {
			return nil, nil, fmt.Errorf("unsupported Packages action %q", action)
		}
		files, err := runPackagesDownload(ctx, runner, bundleRoot, rendered, "packages", opts)
		if err != nil {
			return nil, nil, err
		}
		return files, map[string]any{"artifacts": files}, nil
	case "Image":
		action, err := decodePrepareAction(rendered)
		if err != nil {
			return nil, nil, fmt.Errorf("decode Image action: %w", err)
		}
		if action != "download" {
			return nil, nil, fmt.Errorf("unsupported Image action %q", action)
		}
		files, err := runImageDownload(ctx, runner, bundleRoot, rendered, opts)
		if err != nil {
			return nil, nil, err
		}
		return files, map[string]any{"artifacts": files}, nil
	case "Checks":
		decoded, err := workflowexec.DecodeSpec[checksSpec](rendered)
		if err != nil {
			return nil, nil, fmt.Errorf("decode Checks spec: %w", err)
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

func decodePrepareAction(spec map[string]any) (string, error) {
	decoded, err := workflowexec.DecodeSpec[prepareActionSelectorSpec](spec)
	if err != nil {
		return "", err
	}
	return decoded.Action, nil
}
