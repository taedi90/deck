package askcli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Airgap-Castaways/deck/internal/askcontract"
	"github.com/Airgap-Castaways/deck/internal/askintent"
)

func TestRenderIncludesJudgeFindings(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err := render(stdout, stderr, runResult{
		Route:       askintent.RouteDraft,
		Confidence:  0.9,
		Summary:     "generated workflow",
		LintSummary: "lint ok (1 workflows)",
		Judge: &askcontract.JudgeResponse{
			Summary:             "workflow is close but still thin",
			Blocking:            []string{"worker join targeting is ambiguous"},
			MissingCapabilities: []string{"explicit worker fan-out"},
		},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	output := stdout.String()
	for _, want := range []string{"judge: workflow is close but still thin", "judge-missing-capabilities:", "explicit worker fan-out", "judge-blocking:", "worker join targeting is ambiguous"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in render output, got %q", want, output)
		}
	}
}

func TestRenderIncludesPlanCriticSummary(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err := render(stdout, stderr, runResult{
		Route:        askintent.RouteDraft,
		Confidence:   0.9,
		Summary:      "generated plan artifact",
		PlanMarkdown: ".deck/plan/latest.md",
		PlanJSON:     ".deck/plan/latest.json",
		PlanCritic: &askcontract.PlanCriticResponse{
			Summary:          "plan needs stronger join publication detail",
			MissingContracts: []string{"join-file publication contract"},
		},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	output := stdout.String()
	if !strings.Contains(output, "plan-review: plan needs stronger join publication detail") {
		t.Fatalf("expected plan review summary in output, got %q", output)
	}
}
