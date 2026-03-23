package install

import (
	"context"
	"fmt"
	"strings"

	"github.com/taedi90/deck/internal/config"
	"github.com/taedi90/deck/internal/errcode"
	"github.com/taedi90/deck/internal/stepspec"
	"github.com/taedi90/deck/internal/workflowexec"
)

func executeWorkflowStep(ctx context.Context, step config.Step, rendered map[string]any, key workflowexec.StepTypeKey, execCtx ExecutionContext) error {
	kind := step.Kind
	effectiveSpec := specWithStepTimeout(rendered, step.Timeout)
	if !workflowexec.StepAllowedForRoleForKey("apply", key) {
		return errcode.Newf(errCodeInstallKindUnsupported, "unsupported step kind %s", kind)
	}

	switch kind {
	case "InstallPackage":
		return runInstallPackages(ctx, effectiveSpec)
	case "WriteFile":
		return runWriteFile(rendered)
	case "CopyFile":
		return runCopyFile(ctx, execCtx.BundleRoot, rendered)
	case "EditFile":
		return runEditFile(rendered)
	case "EditTOML":
		return runEditTOML(rendered)
	case "EditYAML":
		return runEditYAML(rendered)
	case "EditJSON":
		return runEditJSON(rendered)
	case "ExtractArchive":
		return runExtractArchive(ctx, execCtx.BundleRoot, rendered)
	case "Sysctl":
		return runSysctl(ctx, effectiveSpec)
	case "ManageService":
		return runManageService(ctx, effectiveSpec)
	case "EnsureDirectory":
		return runEnsureDir(rendered)
	case "CreateSymlink":
		return runCreateSymlink(rendered)
	case "WriteSystemdUnit":
		return runWriteSystemdUnit(ctx, effectiveSpec)
	case "ConfigureRepository":
		return runRepoConfig(ctx, rendered)
	case "RefreshRepository":
		return runRefreshRepository(ctx, effectiveSpec)
	case "WriteContainerdConfig":
		return runWriteContainerdConfig(ctx, effectiveSpec)
	case "WriteContainerdRegistryHosts":
		return runWriteContainerdRegistryHosts(rendered)
	case "Swap":
		return runSwap(ctx, effectiveSpec)
	case "KernelModule":
		return runKernelModule(ctx, effectiveSpec)
	case "Command":
		decoded, err := workflowexec.DecodeSpec[stepspec.Command](effectiveSpec)
		if err != nil {
			return fmt.Errorf("decode command spec: %w", err)
		}
		return runCommandDecoded(ctx, decoded)
	case "LoadImage":
		return runLoadImage(ctx, execCtx.BundleRoot, effectiveSpec)
	case "VerifyImage":
		return runVerifyImages(ctx, effectiveSpec)
	case "InitKubeadm":
		return runInitKubeadm(ctx, effectiveSpec)
	case "JoinKubeadm":
		return runJoinKubeadm(ctx, effectiveSpec)
	case "ResetKubeadm":
		return runResetKubeadm(ctx, effectiveSpec)
	case "UpgradeKubeadm":
		return runUpgradeKubeadm(ctx, effectiveSpec)
	case "CheckCluster":
		return runCheckCluster(ctx, effectiveSpec)
	case "WaitForService", "WaitForCommand", "WaitForFile", "WaitForMissingFile", "WaitForTCPPort", "WaitForMissingTCPPort":
		decoded, err := workflowexec.DecodeSpec[stepspec.Wait](effectiveSpec)
		if err != nil {
			return fmt.Errorf("decode wait spec: %w", err)
		}
		return runWaitDecoded(ctx, kind, decoded, commandTimeout(effectiveSpec))
	case "CheckHost", "DownloadPackage", "DownloadImage":
		return errcode.Newf(errCodeInstallKindUnsupported, "unsupported step kind %s for apply", kind)
	default:
		return errcode.Newf(errCodeInstallKindUnsupported, "unsupported step kind %s", kind)
	}
}

func specWithStepTimeout(rendered map[string]any, stepTimeout string) map[string]any {
	trimmed := strings.TrimSpace(stepTimeout)
	if trimmed == "" {
		return rendered
	}
	cloned := make(map[string]any, len(rendered)+1)
	for key, value := range rendered {
		cloned[key] = value
	}
	cloned["timeout"] = trimmed
	return cloned
}
