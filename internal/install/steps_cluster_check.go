package install

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/taedi90/deck/internal/errcode"
	"github.com/taedi90/deck/internal/filemode"
	"github.com/taedi90/deck/internal/hostfs"
	"github.com/taedi90/deck/internal/stepspec"
	"github.com/taedi90/deck/internal/workflowexec"
)

type clusterNodeSummary struct {
	Total             int
	Ready             int
	ControlPlaneReady int
}

type clusterPodContainerStatus struct {
	Ready bool `json:"ready"`
}

type clusterCheckResult struct {
	NodesText      string
	NodesSummary   clusterNodeSummary
	ServerVersion  string
	KubeletVersion string
	KubeadmVersion string
	PodsText       string
	PodsJSON       string
}

var checkClusterExecutor = runCheckClusterReal

func runCheckCluster(ctx context.Context, spec map[string]any) error {
	decoded, err := workflowexec.DecodeSpec[stepspec.ClusterCheck](spec)
	if err != nil {
		return fmt.Errorf("decode CheckCluster spec: %w", err)
	}
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	return checkClusterExecutor(ctx, decoded)
}

func runCheckClusterReal(parent context.Context, spec stepspec.ClusterCheck) error {
	timeout := parseStepTimeout(spec.Timeout, 10*time.Minute)
	interval := parseStepTimeout(spec.Interval, 5*time.Second)
	initialDelay := time.Duration(0)
	if strings.TrimSpace(spec.InitialDelay) != "" {
		initialDelay = parseStepTimeout(spec.InitialDelay, 0)
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	if initialDelay > 0 {
		select {
		case <-ctx.Done():
			return errcode.New(errCodeInstallClusterCheckFailed, ctx.Err())
		case <-time.After(initialDelay):
		}
	}
	var lastErr error
	for {
		result, err := collectClusterCheckResult(ctx, spec)
		if err == nil {
			err = verifyClusterCheckResult(spec, result)
		}
		if err == nil {
			return nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return errcode.New(errCodeInstallClusterCheckFailed, lastErr)
		case <-time.After(interval):
		}
	}
}

func collectClusterCheckResult(ctx context.Context, spec stepspec.ClusterCheck) (clusterCheckResult, error) {
	var result clusterCheckResult
	needNodes := spec.Nodes.Total != nil || spec.Nodes.Ready != nil || spec.Nodes.ControlPlaneReady != nil || strings.TrimSpace(spec.Reports.NodesPath) != "" || strings.TrimSpace(spec.Reports.ClusterNodesPath) != ""
	if needNodes {
		out, err := runClusterKubectl(ctx, spec.Kubeconfig, "get", "nodes")
		if err != nil {
			return result, err
		}
		result.NodesText = out
		result.NodesSummary = parseClusterNodeSummary(out)
		if err := writeOptionalReport(spec.Reports.NodesPath, out); err != nil {
			return result, err
		}
		if err := writeOptionalReport(spec.Reports.ClusterNodesPath, out); err != nil {
			return result, err
		}
	}
	needVersions := strings.TrimSpace(spec.Versions.Server) != "" || strings.TrimSpace(spec.Versions.Kubelet) != "" || strings.TrimSpace(spec.Versions.Kubeadm) != "" || strings.TrimSpace(spec.Versions.ReportPath) != ""
	if needVersions {
		serverVersion, err := fetchKubectlServerVersion(ctx, spec.Kubeconfig)
		if err != nil {
			return result, err
		}
		result.ServerVersion = serverVersion
		nodeName := strings.TrimSpace(spec.Versions.NodeName)
		if nodeName == "" {
			nodeName = "control-plane"
		}
		kubeletVersion, err := runClusterKubectl(ctx, spec.Kubeconfig, "get", "node", nodeName, "-o", "jsonpath={.status.nodeInfo.kubeletVersion}")
		if err != nil {
			return result, err
		}
		result.KubeletVersion = strings.TrimSpace(kubeletVersion)
		kubeadmVersion, err := runCommandOutputWithContext(ctx, []string{"kubeadm", "version", "-o", "short"}, 30*time.Second)
		if err != nil {
			return result, err
		}
		result.KubeadmVersion = strings.TrimSpace(kubeadmVersion)
		if strings.TrimSpace(spec.Versions.ReportPath) != "" {
			report := strings.Join([]string{
				"targetVersion=" + strings.TrimSpace(spec.Versions.TargetVersion),
				"serverVersion=" + result.ServerVersion,
				"kubeletVersion=" + result.KubeletVersion,
				"kubeadmVersion=" + result.KubeadmVersion,
				"",
			}, "\n")
			if err := writeOptionalReport(spec.Versions.ReportPath, report); err != nil {
				return result, err
			}
		}
	}
	needPods := len(spec.KubeSystem.ReadyNames) > 0 || len(spec.KubeSystem.ReadyPrefixes) > 0 || len(spec.KubeSystem.ReadyPrefixMinimums) > 0 || strings.TrimSpace(spec.KubeSystem.ReportPath) != "" || strings.TrimSpace(spec.KubeSystem.JSONReportPath) != ""
	if needPods {
		podsText, err := runClusterKubectl(ctx, spec.Kubeconfig, "get", "pods", "-n", "kube-system")
		if err != nil {
			return result, err
		}
		result.PodsText = podsText
		podsJSON, err := runClusterKubectl(ctx, spec.Kubeconfig, "get", "pods", "-n", "kube-system", "-o", "json")
		if err != nil {
			return result, err
		}
		result.PodsJSON = podsJSON
		if err := writeOptionalReport(spec.KubeSystem.ReportPath, podsText); err != nil {
			return result, err
		}
		if err := writeOptionalReport(spec.KubeSystem.JSONReportPath, podsJSON); err != nil {
			return result, err
		}
	}
	return result, runClusterFileAssertions(spec.FileChecks)
}

func verifyClusterCheckResult(spec stepspec.ClusterCheck, result clusterCheckResult) error {
	if spec.Nodes.Total != nil && result.NodesSummary.Total != *spec.Nodes.Total {
		return fmt.Errorf("expected %d nodes, got %d", *spec.Nodes.Total, result.NodesSummary.Total)
	}
	if spec.Nodes.Ready != nil && result.NodesSummary.Ready != *spec.Nodes.Ready {
		return fmt.Errorf("expected %d Ready nodes, got %d", *spec.Nodes.Ready, result.NodesSummary.Ready)
	}
	if spec.Nodes.ControlPlaneReady != nil && result.NodesSummary.ControlPlaneReady != *spec.Nodes.ControlPlaneReady {
		return fmt.Errorf("expected %d Ready control-plane nodes, got %d", *spec.Nodes.ControlPlaneReady, result.NodesSummary.ControlPlaneReady)
	}
	if want := strings.TrimSpace(spec.Versions.Server); want != "" && result.ServerVersion != want {
		return fmt.Errorf("expected server version %s, got %s", want, result.ServerVersion)
	}
	if want := strings.TrimSpace(spec.Versions.Kubelet); want != "" && result.KubeletVersion != want {
		return fmt.Errorf("expected kubelet version %s, got %s", want, result.KubeletVersion)
	}
	if want := strings.TrimSpace(spec.Versions.Kubeadm); want != "" && result.KubeadmVersion != want {
		return fmt.Errorf("expected kubeadm version %s, got %s", want, result.KubeadmVersion)
	}
	if err := verifyKubeSystemPods(spec.KubeSystem, result.PodsJSON); err != nil {
		return err
	}
	return nil
}

func runClusterKubectl(ctx context.Context, kubeconfig string, args ...string) (string, error) {
	path := strings.TrimSpace(kubeconfig)
	if path == "" {
		path = "/etc/kubernetes/admin.conf"
	}
	cmd := []string{"sudo", "-n", "env", "KUBECONFIG=" + path, "kubectl"}
	cmd = append(cmd, args...)
	return runCommandOutputWithContext(ctx, cmd, 30*time.Second)
}

func fetchKubectlServerVersion(ctx context.Context, kubeconfig string) (string, error) {
	out, err := runClusterKubectl(ctx, kubeconfig, "version", "-o", "json")
	if err != nil {
		return "", err
	}
	var decoded struct {
		ServerVersion struct {
			GitVersion string `json:"gitVersion"`
		} `json:"serverVersion"`
	}
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		return "", err
	}
	return strings.TrimSpace(decoded.ServerVersion.GitVersion), nil
}

func parseClusterNodeSummary(text string) clusterNodeSummary {
	var out clusterNodeSummary
	for i, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		out.Total++
		roles := ""
		if len(fields) > 2 {
			roles = fields[2]
		}
		if fields[1] == "Ready" {
			out.Ready++
			if strings.Contains(roles, "control-plane") || strings.Contains(roles, "master") {
				out.ControlPlaneReady++
			}
		}
	}
	return out
}

func runClusterFileAssertions(items []stepspec.ClusterCheckFileCheck) error {
	for _, item := range items {
		if strings.TrimSpace(item.Path) == "" {
			continue
		}
		raw, err := os.ReadFile(item.Path)
		if err != nil {
			return err
		}
		content := string(raw)
		for _, token := range item.Contains {
			if !strings.Contains(content, token) {
				return fmt.Errorf("expected %s to contain %q", item.Path, token)
			}
		}
	}
	return nil
}

func verifyKubeSystemPods(spec stepspec.ClusterCheckKubeSystem, podsJSON string) error {
	if len(spec.ReadyNames) == 0 && len(spec.ReadyPrefixes) == 0 && len(spec.ReadyPrefixMinimums) == 0 {
		return nil
	}
	var decoded struct {
		Items []struct {
			Metadata struct {
				Name              string `json:"name"`
				DeletionTimestamp string `json:"deletionTimestamp"`
			} `json:"metadata"`
			Status struct {
				Phase             string                      `json:"phase"`
				ContainerStatuses []clusterPodContainerStatus `json:"containerStatuses"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(podsJSON), &decoded); err != nil {
		return err
	}
	byName := map[string]bool{}
	for _, item := range decoded.Items {
		byName[item.Metadata.Name] = podReady(item.Metadata.DeletionTimestamp, item.Status.Phase, item.Status.ContainerStatuses)
	}
	for _, name := range spec.ReadyNames {
		if !byName[name] {
			return fmt.Errorf("required kube-system pod not ready: %s", name)
		}
	}
	for _, prefix := range spec.ReadyPrefixes {
		matched := false
		for name, ready := range byName {
			if strings.HasPrefix(name, prefix) {
				matched = true
				if !ready {
					return fmt.Errorf("required kube-system pod prefix not fully ready: %s", prefix)
				}
			}
		}
		if !matched {
			return fmt.Errorf("required kube-system pod prefix missing: %s", prefix)
		}
	}
	for _, item := range spec.ReadyPrefixMinimums {
		readyCount := 0
		for name, ready := range byName {
			if strings.HasPrefix(name, item.Prefix) && ready {
				readyCount++
			}
		}
		if readyCount < item.MinReady {
			return fmt.Errorf("required kube-system pod prefix %s has %d ready pods, want at least %d", item.Prefix, readyCount, item.MinReady)
		}
	}
	return nil
}

func podReady(deletionTimestamp, phase string, statuses []clusterPodContainerStatus) bool {
	if deletionTimestamp != "" || phase != "Running" || len(statuses) == 0 {
		return false
	}
	for _, status := range statuses {
		if !status.Ready {
			return false
		}
	}
	return true
}

func writeOptionalReport(path, content string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	ref, err := hostfs.NewHostPath(path)
	if err != nil {
		return err
	}
	if err := ref.EnsureParentDir(filemode.PublishedArtifact); err != nil {
		return err
	}
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return ref.WriteFile([]byte(content), filemode.PublishedArtifact)
}
