package install

import (
	"context"
	"fmt"
)

func executeStep(ctx context.Context, kind string, spec map[string]any, bundleRoot string) error {
	switch kind {
	case "DownloadFile":
		_, err := runDownloadFile(ctx, bundleRoot, spec)
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
	case "Sysctl":
		return runSysctl(spec)
	case "Modprobe":
		return runModprobe(spec)
	case "Service":
		return runService(spec)
	case "EnsureDir":
		return runEnsureDir(spec)
	case "Symlink":
		return runSymlink(spec)
	case "InstallFile":
		return runInstallFile(spec)
	case "TemplateFile":
		return runTemplateFile(spec)
	case "SystemdUnit":
		return runSystemdUnit(spec)
	case "RepoConfig":
		return runRepoConfig(spec)
	case "PackageCache":
		return runPackageCache(spec)
	case "ContainerdConfig":
		return runContainerdConfig(ctx, spec)
	case "Swap":
		return runSwap(spec)
	case "KernelModule":
		return runKernelModule(spec)
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
