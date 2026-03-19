package install

import (
	"fmt"
	"os"
	"strings"

	"github.com/taedi90/deck/internal/filemode"
)

func runKubeadmInitStub(spec kubeadmInitSpec) error {
	joinFile := strings.TrimSpace(spec.OutputJoinFile)
	if joinFile == "" {
		return fmt.Errorf("%s: KubeadmInit requires outputJoinFile", errCodeInstallInitJoinMissing)
	}
	if shouldSkipKubeadmInit(spec) {
		return nil
	}
	content := "kubeadm join 10.0.0.10:6443 --token dummy.token --discovery-token-ca-cert-hash sha256:dummy\n"
	return filemode.WritePrivateFile(joinFile, []byte(content))
}

func runKubeadmJoinStub(spec kubeadmJoinSpec) error {
	joinFile := strings.TrimSpace(spec.JoinFile)
	configFile := strings.TrimSpace(spec.ConfigFile)
	if joinFile != "" && configFile != "" {
		return fmt.Errorf("%s: KubeadmJoin accepts joinFile or configFile, not both", errCodeInstallJoinInputConflict)
	}
	if joinFile == "" && configFile == "" {
		return fmt.Errorf("%s: KubeadmJoin requires joinFile or configFile", errCodeInstallJoinPathMissing)
	}
	path := joinFile
	label := "join file"
	if configFile != "" {
		path = configFile
		label = "config file"
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("%s: %s not found: %w", errCodeInstallJoinFileMissing, label, err)
	}
	return nil
}

func runKubeadmResetStub(spec kubeadmResetSpec) error {
	_ = trimmedStringSlice(spec.RemovePaths)
	_ = trimmedStringSlice(spec.RemoveFiles)
	_ = trimmedStringSlice(spec.CleanupContainers)
	_ = trimmedStringSlice(spec.ExtraArgs)
	_ = strings.TrimSpace(spec.CriSocket)
	_ = strings.TrimSpace(spec.RestartRuntimeService)
	return nil
}
