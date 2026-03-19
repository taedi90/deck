package install

import (
	"context"
	"fmt"

	"github.com/taedi90/deck/internal/workflowexec"
)

func executeStep(ctx context.Context, kind string, spec map[string]any, execCtx ExecutionContext) error {
	if !workflowexec.StepAllowedForRole("apply", kind) {
		return fmt.Errorf("%s: unsupported step kind %s", errCodeInstallKindUnsupported, kind)
	}

	switch kind {
	case "Artifacts":
		return runInstallArtifacts(ctx, spec)
	case "PackagesInstall":
		return runInstallPackages(ctx, spec)
	case "FileDownload":
		_, err := runFileDownload(ctx, execCtx.BundleRoot, spec)
		return err
	case "FileWrite":
		return runWriteFile(spec)
	case "FileCopy":
		return runCopyFile(spec)
	case "FileEdit":
		return runEditFile(spec)
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
		return runRepoConfig(ctx, spec)
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
			return fmt.Errorf("decode command spec: %w", err)
		}
		return runCommandDecoded(ctx, decoded)
	case "ImageVerify":
		return runVerifyImages(ctx, spec)
	case "KubeadmInit":
		return runKubeadmInit(ctx, spec)
	case "KubeadmJoin":
		return runKubeadmJoin(ctx, spec)
	case "KubeadmReset":
		return runKubeadmReset(ctx, spec)
	case "WaitServiceActive", "WaitCommandSuccess", "WaitFileExists", "WaitFileAbsent", "WaitTCPPortOpen", "WaitTCPPortClosed":
		decoded, err := workflowexec.DecodeSpec[waitSpec](spec)
		if err != nil {
			return fmt.Errorf("decode wait spec: %w", err)
		}
		return runWaitDecoded(ctx, kind, decoded, commandTimeout(spec))
	case "Checks", "PackagesDownload", "ImageDownload":
		return fmt.Errorf("%s: unsupported step kind %s for apply", errCodeInstallKindUnsupported, kind)
	default:
		return fmt.Errorf("%s: unsupported step kind %s", errCodeInstallKindUnsupported, kind)
	}
}
