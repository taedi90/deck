package structurededit

import (
	"encoding/json"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"

	"github.com/taedi90/deck/internal/stepspec"
)

func TestApplyTOMLQuotedPath(t *testing.T) {
	raw := []byte("[plugins.\"io.containerd.grpc.v1.cri\".registry]\nconfig_path = \"\"\n")
	updated, err := Apply(FormatTOML, raw, []stepspec.StructuredEdit{{Op: "set", RawPath: `plugins."io.containerd.grpc.v1.cri".registry.config_path`, Value: "/etc/containerd/certs.d"}})
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	var doc map[string]any
	if err := toml.Unmarshal(updated, &doc); err != nil {
		t.Fatalf("unmarshal TOML: %v", err)
	}
	plugins := doc["plugins"].(map[string]any)
	cri := plugins["io.containerd.grpc.v1.cri"].(map[string]any)
	registry := cri["registry"].(map[string]any)
	if got := registry["config_path"]; got != "/etc/containerd/certs.d" {
		t.Fatalf("unexpected config_path: %#v", got)
	}
}

func TestApplyYAMLCreatesNestedMaps(t *testing.T) {
	updated, err := Apply(FormatYAML, nil, []stepspec.StructuredEdit{{Op: "set", RawPath: "spec.template.spec.nodeSelector.role", Value: "control-plane"}})
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(updated, &doc); err != nil {
		t.Fatalf("unmarshal YAML: %v", err)
	}
	if got := doc["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)["nodeSelector"].(map[string]any)["role"]; got != "control-plane" {
		t.Fatalf("unexpected node selector: %#v", got)
	}
}

func TestApplyJSONEditsListByIndex(t *testing.T) {
	raw := []byte(`{"plugins":[{"type":"loopback","capabilities":["a"]}]}`)
	updated, err := Apply(FormatJSON, raw, []stepspec.StructuredEdit{
		{Op: "set", RawPath: "plugins.0.type", Value: "bridge"},
		{Op: "appendUnique", RawPath: "plugins.0.capabilities", Value: []any{"a", "b"}},
	})
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(updated, &doc); err != nil {
		t.Fatalf("unmarshal JSON: %v", err)
	}
	plugins := doc["plugins"].([]any)
	first := plugins[0].(map[string]any)
	if got := first["type"]; got != "bridge" {
		t.Fatalf("unexpected plugin type: %#v", got)
	}
	capabilities := first["capabilities"].([]any)
	if len(capabilities) != 2 || capabilities[0] != "a" || capabilities[1] != "b" {
		t.Fatalf("unexpected capabilities: %#v", capabilities)
	}
}

func TestApplyJSONSetAppendsAtSliceBoundary(t *testing.T) {
	raw := []byte(`{"plugins":[]}`)
	updated, err := Apply(FormatJSON, raw, []stepspec.StructuredEdit{{Op: "set", RawPath: "plugins.0.type", Value: "bridge"}})
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(updated, &doc); err != nil {
		t.Fatalf("unmarshal JSON: %v", err)
	}
	plugins := doc["plugins"].([]any)
	if len(plugins) != 1 {
		t.Fatalf("unexpected plugin length: %#v", plugins)
	}
	first := plugins[0].(map[string]any)
	if got := first["type"]; got != "bridge" {
		t.Fatalf("unexpected plugin type: %#v", got)
	}
}

func TestApplyRejectsArrayIndexOutOfRange(t *testing.T) {
	_, err := Apply(FormatJSON, []byte(`{"plugins":[]}`), []stepspec.StructuredEdit{{Op: "set", RawPath: "plugins.2.type", Value: "bridge"}})
	if err == nil {
		t.Fatalf("expected array range error")
	}
}
