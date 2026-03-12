package install

import "fmt"

func executeStep(kind string, spec map[string]any, bundleRoot string) error {
	switch kind {
	case "DownloadFile":
		_, err := runDownloadFile(bundleRoot, spec)
		return err
	case "InstallPackages":
		return runInstallPackages(spec)
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
	case "InstallFile":
		return runInstallFile(spec)
	case "TemplateFile":
		return runTemplateFile(spec)
	case "RepoConfig":
		return runRepoConfig(spec)
	case "ContainerdConfig":
		return runContainerdConfig(spec)
	case "Swap":
		return runSwap(spec)
	case "KernelModule":
		return runKernelModule(spec)
	case "SysctlApply":
		return runSysctlApply(spec)
	case "RunCommand":
		return runCommand(spec)
	case "VerifyImages":
		return runVerifyImages(spec)
	case "KubeadmInit":
		return runKubeadmInit(spec)
	case "KubeadmJoin":
		return runKubeadmJoin(spec)
	default:
		return fmt.Errorf("%s: unsupported step kind %s", errCodeInstallKindUnsupported, kind)
	}
}
