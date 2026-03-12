package prepare

import "context"

func runPrepareStep(ctx context.Context, runner CommandRunner, bundleRoot, kind string, rendered map[string]any, opts RunOptions) ([]string, map[string]any, error) {
	switch kind {
	case "DownloadFile":
		f, err := runDownloadFile(ctx, bundleRoot, rendered, opts)
		if err != nil {
			return nil, nil, err
		}
		return []string{f}, map[string]any{"path": f, "artifacts": []string{f}}, nil
	case "DownloadPackages":
		files, err := runDownloadPackages(ctx, runner, bundleRoot, rendered, "packages", opts)
		if err != nil {
			return nil, nil, err
		}
		return files, map[string]any{"artifacts": files}, nil
	case "DownloadK8sPackages":
		files, err := runDownloadK8sPackages(ctx, runner, bundleRoot, rendered, opts)
		if err != nil {
			return nil, nil, err
		}
		return files, map[string]any{"artifacts": files}, nil
	case "DownloadImages":
		files, err := runDownloadImages(ctx, runner, bundleRoot, rendered, opts)
		if err != nil {
			return nil, nil, err
		}
		return files, map[string]any{"artifacts": files}, nil
	case "CheckHost":
		outputs, err := runCheckHost(runner, rendered)
		if err != nil {
			return nil, nil, err
		}
		return nil, outputs, nil
	default:
		return nil, map[string]any{}, nil
	}
}
