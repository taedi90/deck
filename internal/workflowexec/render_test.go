package workflowexec

import (
	"strings"
	"testing"

	"github.com/taedi90/deck/internal/config"
)

func TestRenderSpec_MissingKeyReportsNestedMapPath(t *testing.T) {
	spec := map[string]any{
		"top": map[string]any{
			"inner": "{{ .context.cluster.name }}",
		},
	}

	_, err := RenderSpec(spec, &config.Workflow{}, nil, map[string]any{
		"cluster": map[string]any{"id": "node-1"},
	})
	if err == nil {
		t.Fatalf("expected missing key error")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "spec.top") {
		t.Fatalf("expected top-level spec path in error, got %q", errMsg)
	}
	if !strings.Contains(errMsg, "spec.inner") {
		t.Fatalf("expected nested spec path in error, got %q", errMsg)
	}
	if !strings.Contains(errMsg, "name") {
		t.Fatalf("expected missing key name in error, got %q", errMsg)
	}
}

func TestRenderSpec_MissingKeyReportsListIndexPath(t *testing.T) {
	spec := map[string]any{
		"items": []any{
			map[string]any{
				"value": "{{ .vars.missing }}",
			},
		},
	}

	_, err := RenderSpec(spec, &config.Workflow{Vars: map[string]any{}}, nil, nil)
	if err == nil {
		t.Fatalf("expected missing key error")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "spec.items") {
		t.Fatalf("expected list field path in error, got %q", errMsg)
	}
	if !strings.Contains(errMsg, "[0]") {
		t.Fatalf("expected list index path in error, got %q", errMsg)
	}
	if !strings.Contains(errMsg, "spec.value") {
		t.Fatalf("expected nested value path in error, got %q", errMsg)
	}
	if !strings.Contains(errMsg, "missing") {
		t.Fatalf("expected missing key name in error, got %q", errMsg)
	}
}
