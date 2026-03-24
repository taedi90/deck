package prepare

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/taedi90/deck/internal/config"
	"github.com/taedi90/deck/internal/workflowcontract"
	"github.com/taedi90/deck/internal/workflowexec"
)

func TestRunPrepareStepOutputsCoverContracts(t *testing.T) {
	bundle := t.TempDir()
	localFile := filepath.Join(t.TempDir(), "payload.txt")
	if err := os.WriteFile(localFile, []byte("payload"), 0o644); err != nil {
		t.Fatalf("write local file: %v", err)
	}

	tests := []struct {
		name   string
		kind   string
		spec   map[string]any
		runner CommandRunner
		opts   RunOptions
		expect []string
	}{
		{
			name:   "file download",
			kind:   "DownloadFile",
			spec:   map[string]any{"source": map[string]any{"path": localFile}},
			runner: &noArtifactRunner{},
			expect: []string{"outputPath", "outputPaths", "artifacts"},
		},
		{
			name:   "packages download",
			kind:   "DownloadPackage",
			spec:   map[string]any{"packages": []any{"containerd"}},
			runner: &noArtifactRunner{},
			expect: []string{"artifacts"},
		},
		{
			name:   "image download",
			kind:   "DownloadImage",
			spec:   map[string]any{"images": []any{"registry.k8s.io/pause:3.9"}},
			runner: &noArtifactRunner{},
			opts:   RunOptions{imageDownloadOps: stubDownloadImageOps()},
			expect: []string{"artifacts"},
		},
		{
			name:   "checks outputs",
			kind:   "CheckHost",
			spec:   map[string]any{"checks": []any{"os", "arch", "kernelModules"}},
			runner: &noArtifactRunner{},
			opts: RunOptions{checksRuntime: checksRuntime{
				readHostFile: func(path string) ([]byte, error) {
					switch path {
					case "/proc/modules":
						return []byte("overlay 0 0 - Live 0x0\nbr_netfilter 0 0 - Live 0x0\n"), nil
					case "/proc/swaps":
						return []byte("Filename\tType\tSize\tUsed\tPriority\n"), nil
					default:
						return nil, os.ErrNotExist
					}
				},
				currentGOOS:   func() string { return "linux" },
				currentGOARCH: func() string { return "amd64" },
			}},
			expect: []string{"passed", "failedChecks"},
		},
	}
	covered := map[string]bool{}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			step := config.Step{Kind: tc.kind, Spec: tc.spec}
			key := workflowexec.StepTypeKey{APIVersion: workflowcontract.BuiltInStepAPIVersion, Kind: tc.kind}
			_, outputs, err := runPrepareRenderedStepWithKey(context.Background(), tc.runner, bundle, step, tc.spec, key, nil, tc.opts)
			if err != nil {
				t.Fatalf("runPrepareStep failed: %v", err)
			}
			for _, outputKey := range tc.expect {
				covered[coverageKey(tc.kind, outputKey)] = true
				if _, ok := outputs[outputKey]; !ok {
					t.Fatalf("expected runtime output %q for %s", outputKey, tc.kind)
				}
				if !workflowexec.StepHasOutputForKey(key, outputKey) {
					t.Fatalf("contract missing output %q for %s", outputKey, tc.kind)
				}
			}
		})
	}

	for _, def := range workflowexec.StepDefinitions() {
		if !contains(def.Roles, "prepare") {
			continue
		}
		for _, key := range def.Outputs {
			if !covered[coverageKey(def.Kind, key)] {
				t.Fatalf("missing prepare output coverage for %s output %s", def.Kind, key)
			}
		}
	}
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
