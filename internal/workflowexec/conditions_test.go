package workflowexec

import "testing"

func TestEvaluateWhen_AndBindsTighterThanOr(t *testing.T) {
	runtimeVars := map[string]any{
		"a": true,
		"b": false,
		"c": false,
	}

	result, err := EvaluateWhen("runtime.a or runtime.b and runtime.c", nil, runtimeVars, nil, "E_TEST")
	if err != nil {
		t.Fatalf("EvaluateWhen returned error: %v", err)
	}
	if !result {
		t.Fatalf("expected true when and binds tighter than or")
	}

	parenthesized, err := EvaluateWhen("(runtime.a or runtime.b) and runtime.c", nil, runtimeVars, nil, "E_TEST")
	if err != nil {
		t.Fatalf("EvaluateWhen with parentheses returned error: %v", err)
	}
	if parenthesized {
		t.Fatalf("expected false for explicitly parenthesized expression")
	}
}
