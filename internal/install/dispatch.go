package install

import (
	"context"
	"fmt"

	"github.com/taedi90/deck/internal/workflowexec"
)

func executeStep(ctx context.Context, kind string, spec map[string]any, execCtx ExecutionContext) error {
	if !workflowexec.StepAllowedForRole("apply", kind, spec) {
		return fmt.Errorf("%s: unsupported step kind %s", errCodeInstallKindUnsupported, kind)
	}

	switch kind {
	case "Artifacts":
		return runInstallArtifacts(ctx, spec)
	case "Packages":
		return runPackages(ctx, spec)
	case "File":
		action, err := decodeStepAction(spec)
		if err != nil {
			return fmt.Errorf("decode File action: %w", err)
		}
		if action == "download" {
			_, err := runFileDownload(ctx, execCtx.BundleRoot, spec)
			return err
		}
		return runFileAction(action, spec)
	case "Sysctl":
		return runSysctl(ctx, spec)
	case "Service":
		return runService(ctx, spec)
	case "Directory":
		return runEnsureDir(spec)
	case "Symlink":
		return runSymlink(spec)
	case "SystemdUnit":
		return runSystemdUnit(ctx, spec)
	case "Repository":
		return runRepository(ctx, spec)
	case "PackageCache":
		return runPackageCache(ctx, spec)
	case "Containerd":
		return runContainerdConfig(ctx, spec)
	case "Swap":
		return runSwap(ctx, spec)
	case "KernelModule":
		return runKernelModule(ctx, spec)
	case "Command":
		decoded, err := workflowexec.DecodeSpec[runCommandSpec](spec)
		if err != nil {
			return fmt.Errorf("decode Command spec: %w", err)
		}
		return runCommandDecoded(ctx, decoded)
	case "Image":
		return runImage(ctx, spec)
	case "Kubeadm":
		return runKubeadm(ctx, spec)
	case "Wait":
		decoded, err := workflowexec.DecodeSpec[waitSpec](spec)
		if err != nil {
			return fmt.Errorf("decode Wait spec: %w", err)
		}
		return runWaitDecoded(ctx, decoded, commandTimeout(spec))
	case "Checks":
		return fmt.Errorf("%s: unsupported step kind %s for apply", errCodeInstallKindUnsupported, kind)
	default:
		return fmt.Errorf("%s: unsupported step kind %s", errCodeInstallKindUnsupported, kind)
	}
}
