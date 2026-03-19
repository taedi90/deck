package workflowexpr

import (
	"strings"
	"testing"
)

func TestEvaluateWhen_CELPrecedence(t *testing.T) {
	runtimeVars := map[string]any{
		"a": true,
		"b": false,
		"c": false,
	}

	result, err := EvaluateWhen("runtime.a || runtime.b && runtime.c", Inputs{Runtime: runtimeVars})
	if err != nil {
		t.Fatalf("EvaluateWhen returned error: %v", err)
	}
	if !result {
		t.Fatalf("expected true when && binds tighter than ||")
	}

	parenthesized, err := EvaluateWhen("(runtime.a || runtime.b) && runtime.c", Inputs{Runtime: runtimeVars})
	if err != nil {
		t.Fatalf("EvaluateWhen with parentheses returned error: %v", err)
	}
	if parenthesized {
		t.Fatalf("expected false for explicitly parenthesized expression")
	}
}

func TestEvaluateWhen_NamespaceEnforced(t *testing.T) {
	inputs := Inputs{
		Vars:    map[string]any{"nodeRole": "worker"},
		Runtime: map[string]any{"hostPassed": true},
	}

	ok, err := EvaluateWhen(`vars.nodeRole == "worker"`, inputs)
	if err != nil {
		t.Fatalf("expected vars namespace expression to pass, got %v", err)
	}
	if !ok {
		t.Fatalf("expected vars namespace expression to be true")
	}

	_, err = EvaluateWhen(`nodeRole == "worker"`, inputs)
	if err == nil {
		t.Fatalf("expected bare identifier to fail")
	}
	if !strings.Contains(err.Error(), `unknown identifier "nodeRole"; use vars.nodeRole`) {
		t.Fatalf("expected bare identifier guidance, got %v", err)
	}

	_, err = EvaluateWhen(`context.nodeRole == "worker"`, inputs)
	if err == nil {
		t.Fatalf("expected context namespace to fail")
	}
	if !strings.Contains(err.Error(), `unknown identifier "context.nodeRole"; supported prefixes are vars. and runtime`) {
		t.Fatalf("expected namespace restriction message, got %v", err)
	}

	_, err = EvaluateWhen(`other.nodeRole == "worker"`, inputs)
	if err == nil {
		t.Fatalf("expected unknown dotted namespace to fail")
	}
	if !strings.Contains(err.Error(), `unknown identifier "other.nodeRole"; supported prefixes are vars. and runtime`) {
		t.Fatalf("expected namespace restriction message, got %v", err)
	}
}

func TestEvaluateWhen_LegacyOperatorHints(t *testing.T) {
	_, err := EvaluateWhen(`runtime.ready and vars.enabled == true`, Inputs{Runtime: map[string]any{"ready": true}, Vars: map[string]any{"enabled": true}})
	if err == nil {
		t.Fatalf("expected legacy operator failure")
	}
	if !strings.Contains(err.Error(), "use && instead of and") {
		t.Fatalf("expected legacy operator hint, got %v", err)
	}
}

func TestEvaluateWhen_RuntimeAndVarsNamespaces(t *testing.T) {
	result, err := EvaluateWhen(`runtime.hostPassed == true && vars.want == "ok" && runtime.host.os.family == "debian" && runtime.host.arch == "arm64"`, Inputs{
		Vars: map[string]any{"want": "ok"},
		Runtime: map[string]any{
			"hostPassed": true,
			"host": map[string]any{
				"os":   map[string]any{"family": "debian"},
				"arch": "arm64",
			},
		},
	})
	if err != nil {
		t.Fatalf("expected mixed namespace expression to pass, got %v", err)
	}
	if !result {
		t.Fatalf("expected expression to evaluate true")
	}
}
