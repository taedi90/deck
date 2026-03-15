package install

import "context"

func runArtifactsApply(ctx context.Context, spec map[string]any, bundleRoot string) error {
	return runInstallArtifacts(ctx, spec)
}

func runPackagesApply(ctx context.Context, spec map[string]any) error {
	return runInstallPackages(ctx, spec)
}

func runFileInstall(spec map[string]any) error {
	return runInstallFile(spec)
}

func runFileCopy(spec map[string]any) error {
	return runCopyFile(spec)
}

func runFileEdit(spec map[string]any) error {
	return runEditFile(spec)
}

func runContainerdConfigure(ctx context.Context, spec map[string]any) error {
	return runContainerdConfig(ctx, spec)
}

func runRepositoryConfigure(spec map[string]any) error {
	return runRepoConfig(spec)
}

func runImagePresent(ctx context.Context, spec map[string]any) error {
	return runVerifyImages(ctx, spec)
}
