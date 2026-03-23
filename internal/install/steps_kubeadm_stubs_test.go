package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/taedi90/deck/internal/errcode"
	"github.com/taedi90/deck/internal/filemode"
	"github.com/taedi90/deck/internal/stepspec"
)

func runInitKubeadmStub(spec stepspec.KubeadmInit) error {
	joinFile := strings.TrimSpace(spec.OutputJoinFile)
	if joinFile == "" {
		return errcode.Newf(errCodeInstallInitJoinMissing, "InitKubeadm requires outputJoinFile")
	}
	if shouldSkipInitKubeadm(spec) {
		return nil
	}
	content := "kubeadm join 10.0.0.10:6443 --token dummy.token --discovery-token-ca-cert-hash sha256:dummy\n"
	return filemode.WritePrivateFile(joinFile, []byte(content))
}

func runJoinKubeadmStub(spec stepspec.KubeadmJoin) error {
	joinFile := strings.TrimSpace(spec.JoinFile)
	configFile := strings.TrimSpace(spec.ConfigFile)
	if joinFile != "" && configFile != "" {
		return errcode.Newf(errCodeInstallJoinInputConflict, "JoinKubeadm accepts joinFile or configFile, not both")
	}
	if joinFile == "" && configFile == "" {
		return errcode.Newf(errCodeInstallJoinPathMissing, "JoinKubeadm requires joinFile or configFile")
	}
	path := joinFile
	label := "join file"
	if configFile != "" {
		path = configFile
		label = "config file"
	}
	if _, err := os.Stat(path); err != nil {
		return errcode.New(errCodeInstallJoinFileMissing, fmt.Errorf("%s not found: %w", label, err))
	}
	return nil
}

func runResetKubeadmStub(spec stepspec.KubeadmReset) error {
	_ = trimmedStringSlice(spec.RemovePaths)
	_ = trimmedStringSlice(spec.RemoveFiles)
	_ = trimmedStringSlice(spec.CleanupContainers)
	_ = trimmedStringSlice(spec.VerifyContainersAbsent)
	_ = trimmedStringSlice(spec.ExtraArgs)
	_ = strings.TrimSpace(spec.CriSocket)
	_ = strings.TrimSpace(spec.RestartRuntimeManageService)
	if strings.TrimSpace(spec.ReportFile) != "" {
		if err := filemode.WritePrivateFile(spec.ReportFile, []byte("kubeadmReset=ok\ncontainerd=active\nkubeletService=inactive\n")); err != nil {
			return err
		}
	}
	return nil
}

func runUpgradeKubeadmStub(spec stepspec.KubeadmUpgrade) error {
	if strings.TrimSpace(spec.KubernetesVersion) == "" {
		return errcode.Newf(errCodeInstallUpgradeFailed, "UpgradeKubeadm requires kubernetesVersion")
	}
	return nil
}

func runCheckClusterStub(spec stepspec.ClusterCheck) error {
	if strings.TrimSpace(spec.Reports.NodesPath) != "" {
		if err := os.MkdirAll(filepath.Dir(spec.Reports.NodesPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(spec.Reports.NodesPath, []byte("NAME STATUS ROLES AGE VERSION\ncontrol-plane Ready control-plane 1m v1.31.0\n"), 0o644); err != nil {
			return err
		}
	}
	if strings.TrimSpace(spec.Reports.ClusterNodesPath) != "" {
		if err := os.MkdirAll(filepath.Dir(spec.Reports.ClusterNodesPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(spec.Reports.ClusterNodesPath, []byte("NAME STATUS ROLES AGE VERSION\ncontrol-plane Ready control-plane 1m v1.31.0\n"), 0o644); err != nil {
			return err
		}
	}
	if strings.TrimSpace(spec.Versions.ReportPath) != "" {
		if err := os.MkdirAll(filepath.Dir(spec.Versions.ReportPath), 0o755); err != nil {
			return err
		}
		body := fmt.Sprintf("targetVersion=%s\nserverVersion=%s\nkubeletVersion=%s\nkubeadmVersion=%s\n", spec.Versions.TargetVersion, spec.Versions.Server, spec.Versions.Kubelet, spec.Versions.Kubeadm)
		if err := os.WriteFile(spec.Versions.ReportPath, []byte(body), 0o644); err != nil {
			return err
		}
	}
	if strings.TrimSpace(spec.KubeSystem.ReportPath) != "" {
		if err := os.MkdirAll(filepath.Dir(spec.KubeSystem.ReportPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(spec.KubeSystem.ReportPath, []byte("ok\n"), 0o644); err != nil {
			return err
		}
	}
	if strings.TrimSpace(spec.KubeSystem.JSONReportPath) != "" {
		if err := os.MkdirAll(filepath.Dir(spec.KubeSystem.JSONReportPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(spec.KubeSystem.JSONReportPath, []byte("{\"items\":[]}\n"), 0o644); err != nil {
			return err
		}
	}
	return nil
}
