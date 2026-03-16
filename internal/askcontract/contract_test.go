package askcontract

import "testing"

func TestParseInfoFallback(t *testing.T) {
	resp := ParseInfo("plain text answer")
	if resp.Answer == "" {
		t.Fatalf("expected fallback answer")
	}
}

func TestParseGeneration(t *testing.T) {
	raw := `{"summary":"ok","files":[{"path":"workflows/scenarios/apply.yaml","content":"role: apply\nversion: v1alpha1\nsteps:\n  - id: run\n    kind: Command\n    spec:\n      command: [\"true\"]\n"}]}`
	resp, err := ParseGeneration(raw)
	if err != nil {
		t.Fatalf("parse generation: %v", err)
	}
	if len(resp.Files) != 1 {
		t.Fatalf("expected one file, got %d", len(resp.Files))
	}
}

func TestParseClassification(t *testing.T) {
	raw := `{"route":"explain","confidence":0.82,"reason":"analysis request","target":{"kind":"scenario","path":"workflows/scenarios/apply.yaml","name":"apply"},"generationAllowed":false}`
	resp, err := ParseClassification(raw)
	if err != nil {
		t.Fatalf("parse classification: %v", err)
	}
	if resp.Route != "explain" {
		t.Fatalf("unexpected route: %s", resp.Route)
	}
	if resp.Target.Path != "workflows/scenarios/apply.yaml" {
		t.Fatalf("unexpected target: %#v", resp.Target)
	}
}
