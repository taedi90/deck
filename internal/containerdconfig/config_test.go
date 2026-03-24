package containerdconfig

import (
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/Airgap-Castaways/deck/internal/stepspec"
)

func TestApplyPreservesVersionAndMapsLogicalKeys(t *testing.T) {
	raw := []byte("version = 2\n[plugins.\"io.containerd.grpc.v1.cri\".registry]\n  config_path = \"\"\n[plugins.\"io.containerd.grpc.v1.cri\".containerd.runtimes.runc.options]\n  SystemdCgroup = false\n")
	settings := []stepspec.ContainerdConfigSetting{
		{Op: "set", Key: "registry.configPath", Value: "/etc/containerd/certs.d"},
		{Op: "set", Key: "runtime.runtimes.runc.options.SystemdCgroup", Value: true},
	}

	updated, err := Apply(raw, settings, "preserve")
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	doc := decodeConfig(t, updated)
	if got := mustPath(t, doc, "version"); got != int64(2) {
		t.Fatalf("unexpected version: %#v", got)
	}
	if got := mustPath(t, doc, "plugins", "io.containerd.grpc.v1.cri", "registry", "config_path"); got != "/etc/containerd/certs.d" {
		t.Fatalf("unexpected registry config_path: %#v", got)
	}
	if got := mustPath(t, doc, "plugins", "io.containerd.grpc.v1.cri", "containerd", "runtimes", "runc", "options", "SystemdCgroup"); got != true {
		t.Fatalf("unexpected SystemdCgroup: %#v", got)
	}
	if strings.Contains(string(updated), "io.containerd.cri.v1.runtime") {
		t.Fatalf("unexpected v3 runtime path in v2 config: %s", string(updated))
	}
}

func TestApplyRequireV3BuildsV3PathsForEmptyConfig(t *testing.T) {
	settings := []stepspec.ContainerdConfigSetting{
		{Op: "set", RawPath: "plugins.\"io.containerd.cri.v1.images\".snapshotter", Value: "overlayfs"},
		{Op: "appendUnique", Key: "runtime.runtimes.runc.podAnnotations", Value: []any{"a.example/key", "b.example/key"}},
	}

	updated, err := Apply(nil, settings, "require-v3")
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	doc := decodeConfig(t, updated)
	if got := mustPath(t, doc, "version"); got != int64(3) {
		t.Fatalf("unexpected version: %#v", got)
	}
	if got := mustPath(t, doc, "plugins", "io.containerd.cri.v1.images", "snapshotter"); got != "overlayfs" {
		t.Fatalf("unexpected snapshotter: %#v", got)
	}
	annotations := mustPath(t, doc, "plugins", "io.containerd.cri.v1.runtime", "containerd", "runtimes", "runc", "pod_annotations")
	assertStringList(t, annotations, []string{"a.example/key", "b.example/key"})
}

func TestApplyRawPathParsesQuotedSegments(t *testing.T) {
	updated, err := Apply([]byte("version = 2\n"), []stepspec.ContainerdConfigSetting{{Op: "set", RawPath: "plugins.\"io.containerd.grpc.v1.cri\".registry.config_path", Value: "/etc/containerd/certs.d"}}, "preserve")
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	doc := decodeConfig(t, updated)
	if got := mustPath(t, doc, "plugins", "io.containerd.grpc.v1.cri", "registry", "config_path"); got != "/etc/containerd/certs.d" {
		t.Fatalf("unexpected registry config_path: %#v", got)
	}
}

func TestApplyAppendUniqueKeepsExistingStringListUnique(t *testing.T) {
	raw := []byte("version = 3\n[plugins.\"io.containerd.cri.v1.runtime\".containerd.runtimes.runc]\n  pod_annotations = [\"a.example/key\"]\n")
	settings := []stepspec.ContainerdConfigSetting{{Op: "appendUnique", Key: "runtime.runtimes.runc.podAnnotations", Value: []any{"a.example/key", "b.example/key"}}}

	updated, err := Apply(raw, settings, "preserve")
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	doc := decodeConfig(t, updated)
	annotations := mustPath(t, doc, "plugins", "io.containerd.cri.v1.runtime", "containerd", "runtimes", "runc", "pod_annotations")
	assertStringList(t, annotations, []string{"a.example/key", "b.example/key"})
}

func TestApplyRejectsUnsupportedLogicalKeyForVersion(t *testing.T) {
	_, err := Apply([]byte("version = 2\n"), []stepspec.ContainerdConfigSetting{{Op: "set", Key: "runtime.cni.binDirs", Value: []any{"/opt/cni/bin"}}}, "preserve")
	if err == nil || !strings.Contains(err.Error(), "only supported for containerd config version 3") {
		t.Fatalf("expected v3-only key error, got %v", err)
	}
}

func TestApplyRejectsPolicyMismatch(t *testing.T) {
	_, err := Apply([]byte("version = 2\n"), []stepspec.ContainerdConfigSetting{{Op: "set", Key: "image.snapshotter", Value: "overlayfs"}}, "require-v3")
	if err == nil || !strings.Contains(err.Error(), "does not match existing version 2") {
		t.Fatalf("expected policy mismatch error, got %v", err)
	}
}

func decodeConfig(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	doc := map[string]any{}
	if err := toml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	return doc
}

func mustPath(t *testing.T, root map[string]any, path ...string) any {
	t.Helper()
	var current any = root
	for _, segment := range path {
		table, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("segment %q is not a table in path %v", segment, path)
		}
		next, ok := table[segment]
		if !ok {
			t.Fatalf("missing segment %q in path %v", segment, path)
		}
		current = next
	}
	return current
}

func assertStringList(t *testing.T, raw any, expected []string) {
	t.Helper()
	items, err := readStringList(raw)
	if err != nil {
		t.Fatalf("readStringList failed: %v", err)
	}
	if len(items) != len(expected) {
		t.Fatalf("unexpected list length: got %v want %v", items, expected)
	}
	for i := range items {
		if items[i] != expected[i] {
			t.Fatalf("unexpected list contents: got %v want %v", items, expected)
		}
	}
}
