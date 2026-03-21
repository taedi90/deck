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
	case "InstallPackage":
		return runInstallPackages(ctx, spec)
	case "WriteFile":
		return runWriteFile(spec)
	case "CopyFile":
		return runCopyFile(ctx, execCtx.BundleRoot, spec)
	case "EditFile":
		return runEditFile(spec)
	case "ExtractArchive":
		return runExtractArchive(ctx, execCtx.BundleRoot, spec)
	case "Sysctl":
		return runSysctl(ctx, spec)
	case "ManageService":
		return runManageService(ctx, spec)
	case "EnsureDirectory":
		return runEnsureDir(spec)
	case "CreateSymlink":
		return runCreateSymlink(spec)
	case "WriteSystemdUnit":
		return runWriteSystemdUnit(ctx, spec)
	case "ConfigureRepository":
		return runRepoConfig(ctx, spec)
	case "RefreshRepository":
		return runRefreshRepository(ctx, spec)
	case "WriteContainerdConfig":
		return runWriteContainerdConfig(ctx, spec)
	case "WriteContainerdRegistryHosts":
		return runWriteContainerdRegistryHosts(spec)
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
	case "LoadImage":
		return runLoadImage(ctx, execCtx.BundleRoot, spec)
	case "VerifyImage":
		return runVerifyImages(ctx, spec)
	case "InitKubeadm":
		return runInitKubeadm(ctx, spec)
	case "JoinKubeadm":
		return runJoinKubeadm(ctx, spec)
	case "ResetKubeadm":
		return runResetKubeadm(ctx, spec)
	case "WaitForService", "WaitForCommand", "WaitForFile", "WaitForMissingFile", "WaitForTCPPort", "WaitForMissingTCPPort":
		decoded, err := workflowexec.DecodeSpec[waitSpec](spec)
		if err != nil {
			return fmt.Errorf("decode wait spec: %w", err)
		}
		return runWaitDecoded(ctx, kind, decoded, commandTimeout(spec))
	case "CheckHost", "DownloadPackage", "DownloadImage":
		return fmt.Errorf("%s: unsupported step kind %s for apply", errCodeInstallKindUnsupported, kind)
	default:
		return fmt.Errorf("%s: unsupported step kind %s", errCodeInstallKindUnsupported, kind)
	}
}
