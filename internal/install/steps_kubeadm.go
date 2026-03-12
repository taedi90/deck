package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func runKubeadmInit(ctx context.Context, spec map[string]any) error {
	mode := stringValue(spec, "mode")
	if mode == "" {
		mode = "stub"
	}
	if mode == "stub" {
		return runKubeadmInitStub(spec)
	}
	if mode != "real" {
		return fmt.Errorf("%s: unsupported mode %q", errCodeInstallInitModeInvalid, mode)
	}
	return runKubeadmInitReal(ctx, spec)
}

func runKubeadmInitStub(spec map[string]any) error {
	joinFile := stringValue(spec, "outputJoinFile")
	if joinFile == "" {
		return fmt.Errorf("%s: KubeadmInit requires outputJoinFile", errCodeInstallInitJoinMissing)
	}
	if err := os.MkdirAll(filepath.Dir(joinFile), 0o755); err != nil {
		return err
	}
	content := "kubeadm join 10.0.0.10:6443 --token dummy.token --discovery-token-ca-cert-hash sha256:dummy\n"
	return os.WriteFile(joinFile, []byte(content), 0o644)
}

func runKubeadmJoin(ctx context.Context, spec map[string]any) error {
	mode := stringValue(spec, "mode")
	if mode == "" {
		mode = "stub"
	}
	if mode == "stub" {
		return runKubeadmJoinStub(spec)
	}
	if mode != "real" {
		return fmt.Errorf("%s: unsupported mode %q", errCodeInstallJoinModeInvalid, mode)
	}
	return runKubeadmJoinReal(ctx, spec)
}

func runKubeadmJoinStub(spec map[string]any) error {
	joinFile := stringValue(spec, "joinFile")
	if joinFile == "" {
		return fmt.Errorf("%s: KubeadmJoin requires joinFile", errCodeInstallJoinPathMissing)
	}
	if _, err := os.Stat(joinFile); err != nil {
		return fmt.Errorf("%s: join file not found: %w", errCodeInstallJoinFileMissing, err)
	}
	return nil
}

func runKubeadmInitReal(parent context.Context, spec map[string]any) error {
	joinFile := stringValue(spec, "outputJoinFile")
	if joinFile == "" {
		return fmt.Errorf("%s: KubeadmInit requires outputJoinFile", errCodeInstallInitJoinMissing)
	}

	args := []string{"init"}
	if configFile := stringValue(spec, "configFile"); configFile != "" {
		args = append(args, "--config", configFile)
	}
	if advertiseAddress := stringValue(spec, "advertiseAddress"); advertiseAddress != "" {
		args = append(args, "--apiserver-advertise-address", advertiseAddress)
	}
	if podCIDR := stringValue(spec, "podNetworkCIDR"); podCIDR != "" {
		args = append(args, "--pod-network-cidr", podCIDR)
	}
	if criSocket := stringValue(spec, "criSocket"); criSocket != "" {
		args = append(args, "--cri-socket", criSocket)
	}
	if ignore := stringSlice(spec["ignorePreflightErrors"]); len(ignore) > 0 {
		args = append(args, "--ignore-preflight-errors", strings.Join(ignore, ","))
	}
	if extra := stringSlice(spec["extraArgs"]); len(extra) > 0 {
		args = append(args, extra...)
	}

	if err := runTimedCommandWithContext(parent, "kubeadm", args, commandTimeoutWithDefault(spec, 10*time.Minute)); err != nil {
		if errors.Is(err, errStepCommandTimeout) {
			return fmt.Errorf("%s: kubeadm init timed out: %w", errCodeInstallInitFailed, err)
		}
		return fmt.Errorf("%s: kubeadm init failed: %w", errCodeInstallInitFailed, err)
	}

	joinArgs := []string{"token", "create", "--print-join-command"}
	joinOut, err := runCommandOutputWithContext(parent, append([]string{"kubeadm"}, joinArgs...), commandTimeoutWithDefault(spec, 10*time.Minute))
	if err != nil {
		if errors.Is(err, errStepCommandTimeout) {
			return fmt.Errorf("%s: kubeadm token create timed out", errCodeInstallInitFailed)
		}
		return fmt.Errorf("%s: kubeadm token create failed: %w", errCodeInstallInitFailed, err)
	}
	joinCmd := strings.TrimSpace(joinOut)
	if joinCmd == "" {
		return fmt.Errorf("%s: empty kubeadm join command output", errCodeInstallInitFailed)
	}

	if err := os.MkdirAll(filepath.Dir(joinFile), 0o755); err != nil {
		return err
	}
	return os.WriteFile(joinFile, []byte(joinCmd+"\n"), 0o644)
}

func runKubeadmJoinReal(ctx context.Context, spec map[string]any) error {
	joinFile := stringValue(spec, "joinFile")
	if joinFile == "" {
		return fmt.Errorf("%s: KubeadmJoin requires joinFile", errCodeInstallJoinPathMissing)
	}
	raw, err := os.ReadFile(joinFile)
	if err != nil {
		return fmt.Errorf("%s: join file not found: %w", errCodeInstallJoinFileMissing, err)
	}
	joinCommand := strings.TrimSpace(string(raw))
	if joinCommand == "" {
		return fmt.Errorf("%s: join command is empty", errCodeInstallJoinCmdMissing)
	}
	args := strings.Fields(joinCommand)
	if len(args) == 0 || args[0] != "kubeadm" {
		return fmt.Errorf("%s: join command must start with kubeadm", errCodeInstallJoinCmdInvalid)
	}
	if extra := stringSlice(spec["extraArgs"]); len(extra) > 0 {
		args = append(args, extra...)
	}

	if err := runTimedCommandWithContext(ctx, args[0], args[1:], commandTimeoutWithDefault(spec, 5*time.Minute)); err != nil {
		if errors.Is(err, errStepCommandTimeout) {
			return fmt.Errorf("%s: kubeadm join timed out: %w", errCodeInstallJoinFailed, err)
		}
		return fmt.Errorf("%s: kubeadm join failed: %w", errCodeInstallJoinFailed, err)
	}
	return nil
}
