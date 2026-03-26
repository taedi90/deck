package install

import (
	"path/filepath"
	"testing"

	"github.com/Airgap-Castaways/deck/internal/config"
	"github.com/Airgap-Castaways/deck/internal/filemode"
	"github.com/Airgap-Castaways/deck/internal/workflowcontract"
	"github.com/Airgap-Castaways/deck/internal/workflowexec"
)

func TestStepOutputsCoverApplyContracts(t *testing.T) {
	tmp := t.TempDir()
	joinPath := filepath.Join(tmp, "join.txt")
	if err := writePrivateTestFile(joinPath, []byte("kubeadm join fake\n")); err != nil {
		t.Fatalf("write join file: %v", err)
	}

	tests := []struct {
		name   string
		kind   string
		spec   map[string]any
		output []string
	}{
		{name: "directory path", kind: "EnsureDirectory", spec: map[string]any{"path": "/tmp/example"}, output: []string{"path"}},
		{name: "symlink path", kind: "CreateSymlink", spec: map[string]any{"path": "/usr/local/bin/kubectl", "target": "/opt/bin/kubectl"}, output: []string{"path"}},
		{name: "systemd unit path", kind: "WriteSystemdUnit", spec: map[string]any{"path": "/etc/systemd/system/kubelet.service"}, output: []string{"path"}},
		{name: "containerd path", kind: "WriteContainerdConfig", spec: map[string]any{"path": "/etc/containerd/config.toml"}, output: []string{"path"}},
		{name: "containerd registry hosts path", kind: "WriteContainerdRegistryHosts", spec: map[string]any{"path": "/etc/containerd/certs.d", "registryHosts": []any{map[string]any{"registry": "registry.k8s.io", "server": "https://registry.k8s.io", "host": "http://mirror.local:5000", "capabilities": []any{"pull", "resolve"}, "skipVerify": true}}}, output: []string{"path"}},
		{name: "file write path", kind: "WriteFile", spec: map[string]any{"path": "/tmp/example", "content": "hello"}, output: []string{"path"}},
		{name: "file copy path", kind: "CopyFile", spec: map[string]any{"source": map[string]any{"path": "/tmp/source"}, "path": "/tmp/copied"}, output: []string{"path"}},
		{name: "file edit path", kind: "EditFile", spec: map[string]any{"path": "/tmp/edited", "edits": []any{map[string]any{"match": "x"}}}, output: []string{"path"}},
		{name: "edit toml path", kind: "EditTOML", spec: map[string]any{"path": "/tmp/config.toml", "edits": []any{map[string]any{"op": "set", "rawPath": "a.b", "value": "c"}}}, output: []string{"path"}},
		{name: "edit yaml path", kind: "EditYAML", spec: map[string]any{"path": "/tmp/config.yaml", "edits": []any{map[string]any{"op": "set", "rawPath": "a.b", "value": "c"}}}, output: []string{"path"}},
		{name: "edit json path", kind: "EditJSON", spec: map[string]any{"path": "/tmp/config.json", "edits": []any{map[string]any{"op": "set", "rawPath": "a.b", "value": "c"}}}, output: []string{"path"}},
		{name: "extract archive path", kind: "ExtractArchive", spec: map[string]any{"source": map[string]any{"path": "/tmp/source.tar.gz"}, "path": "/opt/cni/bin"}, output: []string{"path"}},
		{name: "repository path", kind: "ConfigureRepository", spec: map[string]any{"path": "/etc/apt/sources.list.d/offline.list", "repositories": []any{map[string]any{"id": "offline"}}}, output: []string{"path"}},
		{name: "service name", kind: "ManageService", spec: map[string]any{"name": "containerd"}, output: []string{"name"}},
		{name: "service names", kind: "ManageService", spec: map[string]any{"names": []any{"containerd", "kubelet"}}, output: []string{"names"}},
		{name: "kernel module name", kind: "KernelModule", spec: map[string]any{"name": "overlay"}, output: []string{"name"}},
		{name: "kernel module names", kind: "KernelModule", spec: map[string]any{"names": []any{"overlay", "br_netfilter"}}, output: []string{"names"}},
		{name: "kubeadm join file", kind: "InitKubeadm", spec: map[string]any{"outputJoinFile": joinPath}, output: []string{"joinFile"}},
		{name: "check host outputs", kind: "CheckHost", spec: map[string]any{"checks": []any{"os", "arch"}}, output: []string{"passed", "failedChecks"}},
	}
	covered := map[string]bool{}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stepKey := workflowexec.StepTypeKey{APIVersion: workflowcontract.BuiltInStepAPIVersion, Kind: tc.kind}
			outputs := stepOutputs(tc.kind, tc.spec)
			if tc.kind == "CheckHost" {
				var err error
				outputs, err = executeWorkflowStep(t.Context(), config.Step{Kind: tc.kind, Spec: tc.spec}, tc.spec, stepKey, ExecutionContext{})
				if err != nil {
					t.Fatalf("execute CheckHost: %v", err)
				}
			}
			for _, outputKey := range tc.output {
				covered[coverageKey(tc.kind, outputKey)] = true
				if _, ok := outputs[outputKey]; !ok {
					t.Fatalf("expected runtime output %q for %s", outputKey, tc.kind)
				}
				if !workflowexec.StepHasOutputForKey(stepKey, outputKey) {
					t.Fatalf("contract missing output %q for %s", outputKey, tc.kind)
				}
			}
		})
	}

	for _, def := range workflowexec.StepDefinitions() {
		if !contains(def.Roles, "apply") {
			continue
		}
		for _, key := range def.Outputs {
			if !covered[coverageKey(def.Kind, key)] {
				t.Fatalf("missing apply output coverage for %s output %s", def.Kind, key)
			}
		}
	}
}

func writePrivateTestFile(path string, content []byte) error {
	return filemode.WritePrivateFile(path, content)
}

func coverageKey(kind, output string) string {
	return kind + ":" + output
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
