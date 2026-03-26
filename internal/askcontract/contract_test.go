package askcontract

import "testing"

func TestParseInfoFallback(t *testing.T) {
	resp := ParseInfo("plain text answer")
	if resp.Answer == "" {
		t.Fatalf("expected fallback answer")
	}
}

func TestParseGeneration(t *testing.T) {
	raw := `{"summary":"ok","files":[{"path":"workflows/scenarios/apply.yaml","content":"version: v1alpha1\nsteps:\n  - id: run\n    kind: Command\n    spec:\n      command: [\"true\"]\n"}]}`
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

func TestParsePlan(t *testing.T) {
	raw := `{"version":1,"request":"create workflow","intent":"draft","complexity":"complex","offlineAssumption":"offline","needsPrepare":false,"artifactKinds":[],"blockers":[],"targetOutcome":"generate files","assumptions":["use v1alpha1"],"openQuestions":[],"entryScenario":"workflows/scenarios/apply.yaml","files":[{"path":"workflows/scenarios/apply.yaml","kind":"scenario","action":"create","purpose":"entry"}],"validationChecklist":["lint"]}`
	resp, err := ParsePlan(raw)
	if err != nil {
		t.Fatalf("parse plan: %v", err)
	}
	if resp.Intent != "draft" || len(resp.Files) != 1 || resp.OfflineAssumption != "offline" {
		t.Fatalf("unexpected plan: %#v", resp)
	}
}

func TestParsePlanInfersOfflineAndArtifacts(t *testing.T) {
	resp, err := ParsePlan(`{"version":1,"request":"create an air-gapped package and image workflow","intent":"draft","complexity":"complex","blockers":[],"targetOutcome":"generate files","assumptions":[],"openQuestions":[],"entryScenario":"workflows/scenarios/apply.yaml","files":[{"path":"workflows/prepare.yaml","kind":"scenario","action":"create","purpose":"prepare"},{"path":"workflows/scenarios/apply.yaml","kind":"scenario","action":"create","purpose":"entry"}],"validationChecklist":["lint"]}`)
	if err != nil {
		t.Fatalf("parse plan: %v", err)
	}
	if resp.OfflineAssumption != "" || resp.NeedsPrepare || len(resp.ArtifactKinds) != 0 {
		t.Fatalf("expected inferred offline prepare plan, got %#v", resp)
	}
}

func TestParsePlanMissingRequiredFields(t *testing.T) {
	if _, err := ParsePlan(`{"version":1,"intent":"draft","files":[]}`); err == nil {
		t.Fatalf("expected parse error")
	}
}

func TestParsePlanRejectsDescriptiveEntryScenario(t *testing.T) {
	_, err := ParsePlan(`{"version":1,"request":"create workflow","intent":"draft","complexity":"complex","blockers":[],"targetOutcome":"generate files","assumptions":[],"openQuestions":[],"entryScenario":"the apply scenario for this workspace","files":[{"path":"workflows/scenarios/apply.yaml","kind":"scenario","action":"create","purpose":"entry"}],"validationChecklist":["lint"]}`)
	if err == nil {
		t.Fatalf("expected semantic entryScenario error")
	}
	if err != nil && err.Error() == "" {
		t.Fatalf("expected non-empty error")
	}
}

func TestParsePlanRejectsEntryScenarioMissingFromFiles(t *testing.T) {
	_, err := ParsePlan(`{"version":1,"request":"create workflow","intent":"draft","complexity":"complex","blockers":[],"targetOutcome":"generate files","assumptions":[],"openQuestions":[],"entryScenario":"workflows/scenarios/apply.yaml","files":[{"path":"workflows/components/base.yaml","kind":"component","action":"create","purpose":"support"}],"validationChecklist":["lint"]}`)
	if err == nil {
		t.Fatalf("expected entryScenario planned file mismatch")
	}
}
