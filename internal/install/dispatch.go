package install

import (
	"context"
	"fmt"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/config"
	"github.com/Airgap-Castaways/deck/internal/errcode"
	"github.com/Airgap-Castaways/deck/internal/executil"
	"github.com/Airgap-Castaways/deck/internal/hostcheck"
	"github.com/Airgap-Castaways/deck/internal/stepspec"
	"github.com/Airgap-Castaways/deck/internal/workflowexec"
)

type installLookPathRunner struct{}

func (installLookPathRunner) LookPath(file string) (string, error) {
	return executil.LookPathWorkflowBinary(file)
}

func executeWorkflowStep(ctx context.Context, step config.Step, rendered map[string]any, key workflowexec.StepTypeKey, execCtx ExecutionContext) (map[string]any, error) {
	kind := step.Kind
	effectiveSpec := specWithStepTimeout(rendered, step.Timeout)
	if !workflowexec.StepAllowedForRoleForKey("apply", key) {
		return nil, errcode.Newf(errCodeInstallKindUnsupported, "unsupported step kind %s", kind)
	}

	switch kind {
	case "InstallPackage":
		return nil, runInstallPackages(ctx, effectiveSpec)
	case "WriteFile":
		return nil, runWriteFile(rendered)
	case "CopyFile":
		return nil, runCopyFile(ctx, execCtx.BundleRoot, rendered)
	case "EditFile":
		return nil, runEditFile(rendered)
	case "EditTOML":
		return nil, runEditTOML(rendered)
	case "EditYAML":
		return nil, runEditYAML(rendered)
	case "EditJSON":
		return nil, runEditJSON(rendered)
	case "ExtractArchive":
		return nil, runExtractArchive(ctx, execCtx.BundleRoot, rendered)
	case "Sysctl":
		return nil, runSysctl(ctx, effectiveSpec)
	case "ManageService":
		return nil, runManageService(ctx, effectiveSpec)
	case "EnsureDirectory":
		return nil, runEnsureDir(rendered)
	case "CreateSymlink":
		return nil, runCreateSymlink(rendered)
	case "WriteSystemdUnit":
		return nil, runWriteSystemdUnit(ctx, effectiveSpec)
	case "ConfigureRepository":
		return nil, runRepoConfig(ctx, rendered)
	case "RefreshRepository":
		return nil, runRefreshRepository(ctx, effectiveSpec)
	case "WriteContainerdConfig":
		return nil, runWriteContainerdConfig(ctx, effectiveSpec)
	case "WriteContainerdRegistryHosts":
		return nil, runWriteContainerdRegistryHosts(rendered)
	case "Swap":
		return nil, runSwap(ctx, effectiveSpec)
	case "KernelModule":
		return nil, runKernelModule(ctx, effectiveSpec)
	case "Command":
		decoded, err := workflowexec.DecodeSpec[stepspec.Command](effectiveSpec)
		if err != nil {
			return nil, fmt.Errorf("decode command spec: %w", err)
		}
		return nil, runCommandDecoded(ctx, decoded)
	case "LoadImage":
		return nil, runLoadImage(ctx, execCtx.BundleRoot, effectiveSpec)
	case "VerifyImage":
		return nil, runVerifyImages(ctx, effectiveSpec)
	case "InitKubeadm":
		return nil, runInitKubeadm(ctx, effectiveSpec)
	case "JoinKubeadm":
		return nil, runJoinKubeadm(ctx, effectiveSpec)
	case "ResetKubeadm":
		return nil, runResetKubeadm(ctx, effectiveSpec)
	case "UpgradeKubeadm":
		return nil, runUpgradeKubeadm(ctx, effectiveSpec)
	case "CheckCluster":
		return nil, runCheckCluster(ctx, effectiveSpec)
	case "CheckHost":
		decoded, err := workflowexec.DecodeSpec[stepspec.CheckHost](effectiveSpec)
		if err != nil {
			return nil, fmt.Errorf("decode check host spec: %w", err)
		}
		return hostcheck.Run(decoded, installLookPathRunner{}, hostcheck.DefaultRuntime(), errCodeInstallCheckHostFailed)
	case "WaitForService", "WaitForCommand", "WaitForFile", "WaitForMissingFile", "WaitForTCPPort", "WaitForMissingTCPPort":
		decoded, err := workflowexec.DecodeSpec[stepspec.Wait](effectiveSpec)
		if err != nil {
			return nil, fmt.Errorf("decode wait spec: %w", err)
		}
		return nil, runWaitDecoded(ctx, kind, decoded, commandTimeout(effectiveSpec))
	case "DownloadPackage", "DownloadImage":
		return nil, errcode.Newf(errCodeInstallKindUnsupported, "unsupported step kind %s for apply", kind)
	default:
		return nil, errcode.Newf(errCodeInstallKindUnsupported, "unsupported step kind %s", kind)
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
