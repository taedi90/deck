package install

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/taedi90/deck/internal/errcode"
	"github.com/taedi90/deck/internal/filemode"
	"github.com/taedi90/deck/internal/fsutil"
	"github.com/taedi90/deck/internal/hostfs"
	"github.com/taedi90/deck/internal/stepspec"
	"github.com/taedi90/deck/internal/workflowexec"
)

var kubeadmAdminConfPath = "/etc/kubernetes/admin.conf"

var (
	kubeadmInitExecutor    = runInitKubeadmReal
	kubeadmJoinExecutor    = runJoinKubeadmReal
	kubeadmResetExecutor   = runResetKubeadmReal
	kubeadmUpgradeExecutor = runUpgradeKubeadmReal
)

func runInitKubeadm(ctx context.Context, spec map[string]any) error {
	decoded, err := workflowexec.DecodeSpec[stepspec.KubeadmInit](spec)
	if err != nil {
		return fmt.Errorf("decode InitKubeadm spec: %w", err)
	}
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	return kubeadmInitExecutor(ctx, decoded)
}

func runJoinKubeadm(ctx context.Context, spec map[string]any) error {
	decoded, err := workflowexec.DecodeSpec[stepspec.KubeadmJoin](spec)
	if err != nil {
		return fmt.Errorf("decode JoinKubeadm spec: %w", err)
	}
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	return kubeadmJoinExecutor(ctx, decoded)
}

func runUpgradeKubeadm(ctx context.Context, spec map[string]any) error {
	decoded, err := workflowexec.DecodeSpec[stepspec.KubeadmUpgrade](spec)
	if err != nil {
		return fmt.Errorf("decode UpgradeKubeadm spec: %w", err)
	}
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	return kubeadmUpgradeExecutor(ctx, decoded)
}

func runInitKubeadmReal(parent context.Context, spec stepspec.KubeadmInit) error {
	joinFile := strings.TrimSpace(spec.OutputJoinFile)
	if joinFile == "" {
		return errcode.Newf(errCodeInstallInitJoinMissing, "InitKubeadm requires outputJoinFile")
	}
	if shouldSkipInitKubeadm(spec) {
		return nil
	}
	timeout := parseStepTimeout(spec.Timeout, 10*time.Minute)
	criSocket := strings.TrimSpace(spec.CriSocket)
	kubernetesVersion := strings.TrimSpace(spec.KubernetesVersion)
	configFile := strings.TrimSpace(spec.ConfigFile)
	configTemplate := strings.TrimSpace(spec.ConfigTemplate)

	advertiseAddress, err := resolveKubeadmAdvertiseAddress(parent, spec, configTemplate, timeout)
	if err != nil {
		return errcode.New(errCodeInstallInitFailed, err)
	}

	if configTemplate != "" {
		if configFile == "" {
			return errcode.Newf(errCodeInstallInitFailed, "configTemplate requires configFile")
		}
		configBody := configTemplate
		if strings.EqualFold(configTemplate, "default") {
			configBody = renderDefaultInitKubeadmConfig(
				advertiseAddress,
				kubernetesVersion,
				strings.TrimSpace(spec.PodNetworkCIDR),
				criSocket,
			)
		}
		if !strings.HasSuffix(configBody, "\n") {
			configBody += "\n"
		}
		if err := filemode.WritePrivateFile(configFile, []byte(configBody)); err != nil {
			return err
		}
	}

	args := []string{"init"}
	if configFile != "" {
		args = append(args, "--config", configFile)
	} else {
		if advertiseAddress != "" {
			args = append(args, "--apiserver-advertise-address", advertiseAddress)
		}
		if podCIDR := strings.TrimSpace(spec.PodNetworkCIDR); podCIDR != "" {
			args = append(args, "--pod-network-cidr", podCIDR)
		}
		if criSocket != "" {
			args = append(args, "--cri-socket", criSocket)
		}
		if kubernetesVersion != "" {
			args = append(args, "--kubernetes-version", kubernetesVersion)
		}
	}
	if ignore := trimmedStringSlice(spec.IgnorePreflightErrors); len(ignore) > 0 {
		args = append(args, "--ignore-preflight-errors", strings.Join(ignore, ","))
	}
	if extra := trimmedStringSlice(spec.ExtraArgs); len(extra) > 0 {
		args = append(args, extra...)
	}

	if err := runTimedCommandWithContext(parent, "kubeadm", args, timeout); err != nil {
		if errors.Is(err, ErrStepCommandTimeout) {
			return errcode.New(errCodeInstallInitFailed, fmt.Errorf("kubeadm init timed out: %w", err))
		}
		return errcode.New(errCodeInstallInitFailed, fmt.Errorf("kubeadm init failed: %w", err))
	}

	joinArgs := []string{"token", "create", "--print-join-command"}
	joinOut, err := runCommandOutputWithContext(parent, append([]string{"kubeadm"}, joinArgs...), timeout)
	if err != nil {
		if errors.Is(err, ErrStepCommandTimeout) {
			return errcode.Newf(errCodeInstallInitFailed, "kubeadm token create timed out")
		}
		return errcode.New(errCodeInstallInitFailed, fmt.Errorf("kubeadm token create failed: %w", err))
	}
	joinCmd := strings.TrimSpace(joinOut)
	if joinCmd == "" {
		return errcode.Newf(errCodeInstallInitFailed, "empty kubeadm join command output")
	}

	return filemode.WritePrivateFile(joinFile, []byte(joinCmd+"\n"))
}

func shouldSkipInitKubeadm(spec stepspec.KubeadmInit) bool {
	skip := true
	if spec.SkipIfAdminConfExists != nil {
		skip = *spec.SkipIfAdminConfExists
	}
	if !skip {
		return false
	}
	_, err := os.Stat(kubeadmAdminConfPath)
	return err == nil
}

func resolveKubeadmAdvertiseAddress(ctx context.Context, spec stepspec.KubeadmInit, configTemplate string, timeout time.Duration) (string, error) {
	advertiseAddress := strings.TrimSpace(spec.AdvertiseAddress)
	if strings.EqualFold(advertiseAddress, "auto") || (advertiseAddress == "" && strings.EqualFold(configTemplate, "default")) {
		resolved, err := detectKubeadmAdvertiseAddress(ctx, timeout)
		if err != nil {
			return "", fmt.Errorf("failed to detect node IPv4 for kubeadm init: %w", err)
		}
		return resolved, nil
	}
	return advertiseAddress, nil
}

func detectKubeadmAdvertiseAddress(ctx context.Context, timeout time.Duration) (string, error) {
	routeOut, routeErr := runCommandOutputWithContext(ctx, []string{"ip", "-4", "route", "get", "1.1.1.1"}, timeout)
	if routeErr == nil {
		if routeSrc := parseRouteSourceIPv4(routeOut); routeSrc != "" {
			return routeSrc, nil
		}
	}
	if routeErr != nil && (errors.Is(routeErr, context.Canceled) || errors.Is(routeErr, context.DeadlineExceeded)) {
		return "", routeErr
	}

	addrOut, addrErr := runCommandOutputWithContext(ctx, []string{"ip", "-4", "-o", "addr", "show", "scope", "global"}, timeout)
	if addrErr == nil {
		if globalIP := parseFirstGlobalIPv4(addrOut); globalIP != "" {
			return globalIP, nil
		}
	}
	if addrErr != nil && (errors.Is(addrErr, context.Canceled) || errors.Is(addrErr, context.DeadlineExceeded)) {
		return "", addrErr
	}

	return "", errors.New("no default-route source IPv4 and no global IPv4 found")
}

func parseRouteSourceIPv4(routeOut string) string {
	fields := strings.Fields(routeOut)
	for i := 0; i < len(fields)-1; i++ {
		if fields[i] != "src" {
			continue
		}
		if parsed := net.ParseIP(fields[i+1]); parsed != nil && parsed.To4() != nil {
			return fields[i+1]
		}
	}
	return ""
}

func parseFirstGlobalIPv4(addrOut string) string {
	for _, line := range strings.Split(addrOut, "\n") {
		for _, token := range strings.Fields(line) {
			if !strings.Contains(token, "/") {
				continue
			}
			ip := strings.SplitN(token, "/", 2)[0]
			if parsed := net.ParseIP(ip); parsed != nil && parsed.To4() != nil {
				return ip
			}
		}
	}
	return ""
}

func renderDefaultInitKubeadmConfig(advertiseAddress, kubernetesVersion, podSubnet, criSocket string) string {
	b := strings.Builder{}
	b.WriteString("apiVersion: kubeadm.k8s.io/v1beta3\n")
	b.WriteString("kind: InitConfiguration\n")
	b.WriteString("localAPIEndpoint:\n")
	_, _ = fmt.Fprintf(&b, "  advertiseAddress: %s\n", advertiseAddress)
	b.WriteString("  bindPort: 6443\n")
	if criSocket != "" {
		b.WriteString("nodeRegistration:\n")
		_, _ = fmt.Fprintf(&b, "  criSocket: %s\n", criSocket)
	}
	b.WriteString("---\n")
	b.WriteString("apiVersion: kubeadm.k8s.io/v1beta3\n")
	b.WriteString("kind: ClusterConfiguration\n")
	if kubernetesVersion != "" {
		_, _ = fmt.Fprintf(&b, "kubernetesVersion: %s\n", kubernetesVersion)
	}
	if podSubnet != "" {
		b.WriteString("networking:\n")
		_, _ = fmt.Fprintf(&b, "  podSubnet: %s\n", podSubnet)
	}
	return b.String()
}

func runJoinKubeadmReal(ctx context.Context, spec stepspec.KubeadmJoin) error {
	joinFile := strings.TrimSpace(spec.JoinFile)
	configFile := strings.TrimSpace(spec.ConfigFile)
	if joinFile != "" && configFile != "" {
		return errcode.Newf(errCodeInstallJoinInputConflict, "JoinKubeadm accepts joinFile or configFile, not both")
	}
	if joinFile == "" && configFile == "" {
		return errcode.Newf(errCodeInstallJoinPathMissing, "JoinKubeadm requires joinFile or configFile")
	}

	args := []string{"kubeadm", "join"}
	if configFile != "" {
		if _, err := os.Stat(configFile); err != nil {
			return errcode.New(errCodeInstallJoinFileMissing, fmt.Errorf("config file not found: %w", err))
		}
		args = append(args, "--config", configFile)
	} else {
		raw, err := fsutil.ReadFile(joinFile)
		if err != nil {
			return errcode.New(errCodeInstallJoinFileMissing, fmt.Errorf("join file not found: %w", err))
		}
		joinCommand := strings.TrimSpace(string(raw))
		if joinCommand == "" {
			return errcode.Newf(errCodeInstallJoinCmdMissing, "join command is empty")
		}
		args = strings.Fields(joinCommand)
		if len(args) == 0 || args[0] != "kubeadm" {
			return errcode.Newf(errCodeInstallJoinCmdInvalid, "join command must start with kubeadm")
		}
	}
	if spec.AsControlPlane && !stringSliceContains(args[1:], "--control-plane") {
		args = append(args, "--control-plane")
	}
	if extra := trimmedStringSlice(spec.ExtraArgs); len(extra) > 0 {
		args = append(args, extra...)
	}

	if err := runTimedCommandWithContext(ctx, args[0], args[1:], parseStepTimeout(spec.Timeout, 5*time.Minute)); err != nil {
		if errors.Is(err, ErrStepCommandTimeout) {
			return errcode.New(errCodeInstallJoinFailed, fmt.Errorf("kubeadm join timed out: %w", err))
		}
		return errcode.New(errCodeInstallJoinFailed, fmt.Errorf("kubeadm join failed: %w", err))
	}
	return nil
}

func runResetKubeadm(ctx context.Context, spec map[string]any) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}

	decoded, err := workflowexec.DecodeSpec[stepspec.KubeadmReset](spec)
	if err != nil {
		return fmt.Errorf("decode ResetKubeadm spec: %w", err)
	}
	return kubeadmResetExecutor(ctx, decoded)
}

func runResetKubeadmReal(ctx context.Context, decoded stepspec.KubeadmReset) error {
	stopKubelet := true
	if decoded.StopKubelet != nil {
		stopKubelet = *decoded.StopKubelet
	}
	if stopKubelet {
		_ = runTimedCommandWithContext(ctx, "systemctl", []string{"stop", "kubelet"}, parseStepTimeout(decoded.Timeout, 2*time.Minute))
	}

	kubeadmArgs := []string{"reset"}
	if decoded.Force {
		kubeadmArgs = append(kubeadmArgs, "--force")
	}
	if strings.TrimSpace(decoded.CriSocket) != "" {
		kubeadmArgs = append(kubeadmArgs, "--cri-socket", strings.TrimSpace(decoded.CriSocket))
	}
	if extra := trimmedStringSlice(decoded.ExtraArgs); len(extra) > 0 {
		kubeadmArgs = append(kubeadmArgs, extra...)
	}

	resetErr := runTimedCommandWithContext(ctx, "kubeadm", kubeadmArgs, parseStepTimeout(decoded.Timeout, 10*time.Minute))
	if resetErr != nil && !decoded.IgnoreErrors {
		if errors.Is(resetErr, ErrStepCommandTimeout) {
			return errcode.New(errCodeInstallResetFailed, fmt.Errorf("kubeadm reset timed out: %w", resetErr))
		}
		return errcode.New(errCodeInstallResetFailed, fmt.Errorf("kubeadm reset failed: %w", resetErr))
	}

	if err := removeResetPaths(decoded.RemovePaths); err != nil {
		return errcode.New(errCodeInstallResetFailed, fmt.Errorf("remove reset paths: %w", err))
	}
	if err := removeResetFiles(decoded.RemoveFiles); err != nil {
		return errcode.New(errCodeInstallResetFailed, fmt.Errorf("remove reset files: %w", err))
	}

	cleanupContainers := trimmedStringSlice(decoded.CleanupContainers)
	for _, name := range cleanupContainers {
		if err := cleanupContainerByName(ctx, name, strings.TrimSpace(decoded.CriSocket), parseStepTimeout(decoded.Timeout, 2*time.Minute)); err != nil {
			return errcode.New(errCodeInstallResetFailed, fmt.Errorf("cleanup stale container %s: %w", name, err))
		}
	}

	restartRuntime := strings.TrimSpace(decoded.RestartRuntimeManageService)
	if restartRuntime != "" {
		if err := runTimedCommandWithContext(ctx, "systemctl", []string{"restart", restartRuntime}, parseStepTimeout(decoded.Timeout, 2*time.Minute)); err != nil {
			if errors.Is(err, ErrStepCommandTimeout) {
				return errcode.New(errCodeInstallResetFailed, fmt.Errorf("restart runtime service %s timed out: %w", restartRuntime, err))
			}
			return errcode.New(errCodeInstallResetFailed, fmt.Errorf("restart runtime service %s failed: %w", restartRuntime, err))
		}
	}
	if decoded.WaitForRuntimeService && restartRuntime != "" {
		if err := runWaitDecoded(ctx, "WaitForService", stepspec.Wait{Name: restartRuntime}, parseStepTimeout(decoded.Timeout, 2*time.Minute)); err != nil {
			return errcode.New(errCodeInstallResetFailed, fmt.Errorf("runtime service did not become active: %w", err))
		}
	}
	if decoded.WaitForRuntimeReady {
		command := []string{"crictl"}
		if socket := strings.TrimSpace(decoded.CriSocket); socket != "" {
			command = append(command, "--runtime-endpoint", socket)
		}
		command = append(command, "info")
		if err := runWaitDecoded(ctx, "WaitForCommand", stepspec.Wait{Command: command}, parseStepTimeout(decoded.Timeout, 2*time.Minute)); err != nil {
			return errcode.New(errCodeInstallResetFailed, fmt.Errorf("runtime did not become ready: %w", err))
		}
	}
	if glob := strings.TrimSpace(decoded.WaitForMissingManifestsGlob); glob != "" {
		if err := runWaitDecoded(ctx, "WaitForMissingFile", stepspec.Wait{Glob: glob}, parseStepTimeout(decoded.Timeout, 2*time.Minute)); err != nil {
			return errcode.New(errCodeInstallResetFailed, fmt.Errorf("manifests did not disappear: %w", err))
		}
	}
	for _, name := range trimmedStringSlice(decoded.VerifyContainersAbsent) {
		present, err := kubeadmContainerPresent(ctx, name, strings.TrimSpace(decoded.CriSocket), parseStepTimeout(decoded.Timeout, 2*time.Minute))
		if err != nil {
			return errcode.New(errCodeInstallResetFailed, fmt.Errorf("verify stale container %s: %w", name, err))
		}
		if present {
			return errcode.Newf(errCodeInstallResetFailed, "stale container still present: %s", name)
		}
	}
	if decoded.StopKubeletAfterReset {
		_ = runTimedCommandWithContext(ctx, "systemctl", []string{"stop", "kubelet"}, parseStepTimeout(decoded.Timeout, 2*time.Minute))
	}
	if reportPath := strings.TrimSpace(decoded.ReportFile); reportPath != "" {
		if err := writeResetReport(ctx, decoded, reportPath); err != nil {
			return errcode.New(errCodeInstallResetFailed, fmt.Errorf("write reset report: %w", err))
		}
	}

	return nil
}

func runUpgradeKubeadmReal(ctx context.Context, spec stepspec.KubeadmUpgrade) error {
	version := strings.TrimSpace(spec.KubernetesVersion)
	if version == "" {
		return errcode.Newf(errCodeInstallUpgradeFailed, "UpgradeKubeadm requires kubernetesVersion")
	}
	timeout := parseStepTimeout(spec.Timeout, 20*time.Minute)
	args := []string{"upgrade", "apply", "-y", version}
	if ignore := trimmedStringSlice(spec.IgnorePreflightErrors); len(ignore) > 0 {
		args = append(args, "--ignore-preflight-errors", strings.Join(ignore, ","))
	}
	if extra := trimmedStringSlice(spec.ExtraArgs); len(extra) > 0 {
		args = append(args, extra...)
	}
	if err := runTimedCommandWithContext(ctx, "kubeadm", args, timeout); err != nil {
		if errors.Is(err, ErrStepCommandTimeout) {
			return errcode.New(errCodeInstallUpgradeFailed, fmt.Errorf("kubeadm upgrade timed out: %w", err))
		}
		return errcode.New(errCodeInstallUpgradeFailed, fmt.Errorf("kubeadm upgrade failed: %w", err))
	}
	restartKubelet := true
	if spec.RestartKubelet != nil {
		restartKubelet = *spec.RestartKubelet
	}
	if !restartKubelet {
		return nil
	}
	service := strings.TrimSpace(spec.KubeletService)
	if service == "" {
		service = "kubelet"
	}
	if err := runTimedCommandWithContext(ctx, "systemctl", []string{"restart", service}, parseStepTimeout(spec.Timeout, 2*time.Minute)); err != nil {
		if errors.Is(err, ErrStepCommandTimeout) {
			return errcode.New(errCodeInstallUpgradeFailed, fmt.Errorf("restart service %s timed out: %w", service, err))
		}
		return errcode.New(errCodeInstallUpgradeFailed, fmt.Errorf("restart service %s failed: %w", service, err))
	}
	return nil
}

func removeResetPaths(paths []string) error {
	for _, path := range trimmedStringSlice(paths) {
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	return nil
}

func removeResetFiles(paths []string) error {
	for _, path := range trimmedStringSlice(paths) {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func cleanupContainerByName(ctx context.Context, name, criSocket string, timeout time.Duration) error {
	listArgs := []string{}
	if criSocket != "" {
		listArgs = append(listArgs, "--runtime-endpoint", criSocket)
	}
	listArgs = append(listArgs, "ps", "-a", "--name", name, "-q")

	out, err := runCommandOutputWithContext(ctx, append([]string{"crictl"}, listArgs...), timeout)
	if err != nil {
		return err
	}

	ids := strings.Fields(strings.TrimSpace(out))
	if len(ids) == 0 {
		return nil
	}

	rmArgs := []string{}
	if criSocket != "" {
		rmArgs = append(rmArgs, "--runtime-endpoint", criSocket)
	}
	rmArgs = append(rmArgs, "rm", "-f")
	rmArgs = append(rmArgs, ids...)
	return runTimedCommandWithContext(ctx, "crictl", rmArgs, timeout)
}

func kubeadmContainerPresent(ctx context.Context, name, criSocket string, timeout time.Duration) (bool, error) {
	listArgs := []string{}
	if criSocket != "" {
		listArgs = append(listArgs, "--runtime-endpoint", criSocket)
	}
	listArgs = append(listArgs, "ps", "-a", "--name", name, "-q")
	out, err := runCommandOutputWithContext(ctx, append([]string{"crictl"}, listArgs...), timeout)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func writeResetReport(ctx context.Context, decoded stepspec.KubeadmReset, reportPath string) error {
	ref, err := hostfs.NewHostPath(reportPath)
	if err != nil {
		return err
	}
	if err := ref.EnsureParentDir(filemode.PublishedArtifact); err != nil {
		return err
	}
	manifests := "absent"
	if glob := strings.TrimSpace(decoded.WaitForMissingManifestsGlob); glob != "" {
		matches, err := filepath.Glob(glob)
		if err != nil {
			return err
		}
		if len(matches) > 0 {
			manifests = "present"
		}
	}
	stale := "absent"
	for _, name := range trimmedStringSlice(decoded.VerifyContainersAbsent) {
		present, err := kubeadmContainerPresent(ctx, name, strings.TrimSpace(decoded.CriSocket), 2*time.Minute)
		if err != nil {
			return err
		}
		if present {
			stale = "present"
			break
		}
	}
	kubeletConfig := "absent"
	if info, err := os.Stat("/var/lib/kubelet/config.yaml"); err == nil && info.Size() > 0 {
		kubeletConfig = "present"
	}
	kubeletService := "inactive"
	if err := runTimedCommandWithContext(ctx, "systemctl", []string{"is-active", "--quiet", "kubelet"}, 10*time.Second); err == nil {
		kubeletService = "active"
	}
	content := strings.Join([]string{
		"resetReason=" + strings.TrimSpace(decoded.ReportResetReason),
		"kubeadmReset=ok",
		"manifests=" + manifests,
		"staleControlPlaneContainers=" + stale,
		"containerd=active",
		"kubeletConfig=" + kubeletConfig,
		"kubeletService=" + kubeletService,
		"",
	}, "\n")
	return ref.WriteFile([]byte(content), filemode.PublishedArtifact)
}

func trimmedStringSlice(items []string) []string {
	trimmed := make([]string, 0, len(items))
	for _, item := range items {
		v := strings.TrimSpace(item)
		if v != "" {
			trimmed = append(trimmed, v)
		}
	}
	return trimmed
}

func stringSliceContains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
