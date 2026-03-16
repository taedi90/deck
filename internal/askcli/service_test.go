package askcli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/taedi90/deck/internal/askconfig"
	"github.com/taedi90/deck/internal/askintent"
	"github.com/taedi90/deck/internal/askprovider"
	"github.com/taedi90/deck/internal/askretrieve"
)

type stubClient struct {
	responses []string
	calls     int
}

func (s *stubClient) Generate(_ context.Context, _ askprovider.Request) (askprovider.Response, error) {
	defer func() { s.calls++ }()
	idx := s.calls
	if idx >= len(s.responses) {
		idx = len(s.responses) - 1
	}
	return askprovider.Response{Content: s.responses[idx]}, nil
}

func TestClassifyWithLLMRetriesMalformedJSON(t *testing.T) {
	client := &stubClient{responses: []string{
		"not-json",
		`{"route":"explain","confidence":0.9,"reason":"analyze existing scenario","target":{"kind":"scenario","path":"workflows/scenarios/apply.yaml","name":"apply"},"generationAllowed":false}`,
	}}
	decision, err := classifyWithLLM(
		context.Background(),
		client,
		askconfig.EffectiveSettings{Settings: askconfig.Settings{Provider: "openai", Model: "gpt-5.4", APIKey: "test-key"}},
		classifierSystemPrompt(),
		classifierUserPrompt("explain apply", false, askretrieve.WorkspaceSummary{HasWorkflowTree: true}),
		newAskLogger(io.Discard, "trace"),
	)
	if err != nil {
		t.Fatalf("classify with llm: %v", err)
	}
	if client.calls != 2 {
		t.Fatalf("expected retry on malformed classifier json, got %d calls", client.calls)
	}
	if decision.Route != askintent.RouteExplain || decision.Target.Path != "workflows/scenarios/apply.yaml" {
		t.Fatalf("unexpected decision: %#v", decision)
	}
}

func TestGenerateWithValidationStopsOnRouteMismatch(t *testing.T) {
	client := &stubClient{responses: []string{
		`{"summary":"wrong route","review":[],"files":[]}`,
		`{"summary":"should not retry","review":[],"files":[{"path":"workflows/scenarios/apply.yaml","content":"role: apply\nversion: v1alpha1\n"}]}`,
	}}
	_, _, _, err := generateWithValidation(context.Background(), client, askprovider.Request{Kind: "generate", Provider: "openai", Model: "gpt-5.4", APIKey: "test-key"}, t.TempDir(), 2, newAskLogger(io.Discard, "trace"))
	if err == nil {
		t.Fatalf("expected generation failure")
	}
	if !strings.Contains(err.Error(), "without repair") {
		t.Fatalf("expected non-repairable termination, got %v", err)
	}
	if client.calls != 1 {
		t.Fatalf("expected non-repairable failure to stop after one call, got %d", client.calls)
	}
}

func TestLocalExplainDescribesScenarioStructure(t *testing.T) {
	workspace := askretrieve.WorkspaceSummary{
		Files: []askretrieve.WorkspaceFile{
			{Path: "workflows/scenarios/apply.yaml", Content: "role: apply\nversion: v1alpha1\nphases:\n  - name: bootstrap\n    imports:\n      - path: bootstrap.yaml\n  - name: verify\n    steps:\n      - id: report\n        kind: Command\n        spec:\n          command: [bash, -lc, \"true\"]\n"},
			{Path: "workflows/components/bootstrap.yaml", Content: "steps:\n  - id: step-one\n    kind: Kubeadm\n    spec:\n      action: init\n"},
		},
	}
	summary, answer := localExplain(workspace, "explain apply", askintent.Target{Kind: "scenario", Path: "workflows/scenarios/apply.yaml", Name: "apply"})
	if summary == "" {
		t.Fatalf("expected explain summary")
	}
	for _, want := range []string{"role \"apply\"", "bootstrap, verify", "bootstrap.yaml", "Command step", "Related component available: workflows/components/bootstrap.yaml"} {
		if !strings.Contains(answer, want) {
			t.Fatalf("expected %q in answer, got %q", want, answer)
		}
	}
}

func TestAskLoggerDebugAndTrace(t *testing.T) {
	var buf bytes.Buffer
	logger := newAskLogger(&buf, "trace")
	logger.logf("debug", "deck ask command=%s\n", `deck ask "explain apply"`)
	logger.prompt("explain", "system text", "user text")
	logger.response("explain", `{"summary":"ok"}`)
	logText := buf.String()
	for _, want := range []string{"deck ask command=deck ask \"explain apply\"", "deck ask explain system-prompt:\nsystem text", "deck ask explain user-prompt:\nuser text", "deck ask explain raw-response:\n{\"summary\":\"ok\"}"} {
		if !strings.Contains(logText, want) {
			t.Fatalf("expected %q in log output, got %q", want, logText)
		}
	}
}
