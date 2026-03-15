package install

import (
	"context"
	"fmt"

	"github.com/taedi90/deck/internal/workflowexec"
)

func executeStep(ctx context.Context, kind string, spec map[string]any, bundleRoot string) error {
	if !workflowexec.StepAllowedForRole("apply", kind, spec) {
		return fmt.Errorf("%s: unsupported step kind %s", errCodeInstallKindUnsupported, kind)
	}

	switch kind {
	case "Artifacts":
		return runArtifactsApply(ctx, spec, bundleRoot)
	case "Packages":
		return runPackages(ctx, spec)
	case "File":
		if fileAction(spec) == "download" {
			_, err := runFileDownload(ctx, bundleRoot, spec)
			return err
		}
		return runFile(spec)
	case "Sysctl":
		return runSysctl(spec)
	case "Service":
		return runService(spec)
	case "Directory":
		return runEnsureDir(spec)
	case "Symlink":
		return runSymlink(spec)
	case "SystemdUnit":
		return runSystemdUnit(spec)
	case "Repository":
		return runRepository(spec)
	case "PackageCache":
		return runPackageCache(spec)
	case "Containerd":
		return runContainerdConfigure(ctx, spec)
	case "Swap":
		return runSwap(spec)
	case "KernelModule":
		return runKernelModule(spec)
	case "Command":
		return runCommand(ctx, spec)
	case "Image":
		return runImage(ctx, spec)
	case "Kubeadm":
		return runKubeadm(ctx, spec)
	case "Wait":
		return runWait(ctx, spec)
	case "Inspection":
		return fmt.Errorf("%s: unsupported step kind %s for apply", errCodeInstallKindUnsupported, kind)
	case "DownloadFile":
		_, err := runFileDownload(ctx, bundleRoot, spec)
		return err
	case "InstallArtifacts":
		return runInstallArtifacts(ctx, spec)
	case "InstallPackages":
		return runInstallPackages(ctx, spec)
	case "WriteFile":
		return runWriteFile(spec)
	case "EditFile":
		return runEditFile(spec)
	case "CopyFile":
		return runCopyFile(spec)
	case "EnsureDir":
		return runEnsureDir(spec)
	case "InstallFile":
		return runInstallFile(spec)
	case "TemplateFile":
		return runTemplateFile(spec)
	case "RepoConfig":
		return runRepoConfig(spec)
	case "ContainerdConfig":
		return runContainerdConfig(ctx, spec)
	case "Modprobe":
		return runModprobe(spec)
	case "SysctlApply":
		return runSysctlApply(spec)
	case "RunCommand":
		return runCommand(ctx, spec)
	case "VerifyImages":
		return runVerifyImages(ctx, spec)
	case "KubeadmInit":
		return runKubeadmInit(ctx, spec)
	case "KubeadmJoin":
		return runKubeadmJoin(ctx, spec)
	case "KubeadmReset":
		return runKubeadmReset(ctx, spec)
	case "WaitPath":
		return runWaitPath(ctx, spec)
	default:
		return fmt.Errorf("%s: unsupported step kind %s", errCodeInstallKindUnsupported, kind)
	}
}
