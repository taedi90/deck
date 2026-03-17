package askcli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/taedi90/deck/internal/askconfig"
	"github.com/taedi90/deck/internal/askcontract"
	"github.com/taedi90/deck/internal/askintent"
	"github.com/taedi90/deck/internal/askprovider"
	"github.com/taedi90/deck/internal/askretrieve"
)

type stubClient struct {
	responses []string
	calls     int
}

type flushBuffer struct {
	bytes.Buffer
	flushes int
}

func (b *flushBuffer) Flush() error {
	b.flushes++
	return nil
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
	_, _, _, _, err := generateWithValidation(context.Background(), client, askprovider.Request{Kind: "generate", Provider: "openai", Model: "gpt-5.4", APIKey: "test-key"}, t.TempDir(), 2, newAskLogger(io.Discard, "trace"), askintent.Decision{Route: askintent.RouteDraft}, askcontract.PlanResponse{})
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

func TestGenerateWithValidationRetriesParseFailure(t *testing.T) {
	client := &stubClient{responses: []string{
		`not-json`,
		`{"summary":"ok","review":[],"files":[{"path":"workflows/scenarios/apply.yaml","content":"role: apply\nversion: v1alpha1\nsteps:\n  - id: run\n    kind: Command\n    spec:\n      command: [\"true\"]\n"}]}`,
	}}
	gen, _, _, retries, err := generateWithValidation(context.Background(), client, askprovider.Request{Kind: "generate", Provider: "openai", Model: "gpt-5.4", APIKey: "test-key", Prompt: "generate"}, t.TempDir(), 2, newAskLogger(io.Discard, "trace"), askintent.Decision{Route: askintent.RouteDraft}, askcontract.PlanResponse{})
	if err != nil {
		t.Fatalf("expected parse retry success: %v", err)
	}
	if retries != 1 || len(gen.Files) != 1 {
		t.Fatalf("unexpected result: retries=%d files=%d", retries, len(gen.Files))
	}
}

func TestGenerateWithValidationRepairsSemanticFailure(t *testing.T) {
	client := &stubClient{responses: []string{
		`{"summary":"missing vars","review":[],"files":[{"path":"workflows/scenarios/apply.yaml","content":"role: apply\nversion: v1alpha1\nsteps:\n  - id: run\n    kind: Command\n    spec:\n      command: [\"true\"]\n"}]}`,
		`{"summary":"ok","review":[],"files":[{"path":"workflows/scenarios/apply.yaml","content":"role: apply\nversion: v1alpha1\nsteps:\n  - id: run\n    kind: Command\n    spec:\n      command: [\"true\"]\n"},{"path":"workflows/vars.yaml","content":"{}\n"}]}`,
	}}
	plan := askcontract.PlanResponse{Files: []askcontract.PlanFile{{Path: "workflows/scenarios/apply.yaml", Action: "create"}, {Path: "workflows/vars.yaml", Action: "create"}}, ValidationChecklist: []string{"vars are defined"}}
	gen, _, _, retries, err := generateWithValidation(context.Background(), client, askprovider.Request{Kind: "generate", Provider: "openai", Model: "gpt-5.4", APIKey: "test-key", Prompt: "generate"}, t.TempDir(), 2, newAskLogger(io.Discard, "trace"), askintent.Decision{Route: askintent.RouteDraft}, plan)
	if err != nil {
		t.Fatalf("expected semantic repair success: %v", err)
	}
	if retries != 1 || len(gen.Files) != 2 {
		t.Fatalf("unexpected result: retries=%d files=%d", retries, len(gen.Files))
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
	var buf flushBuffer
	logger := newAskLogger(&buf, "trace")
	logger.logf("debug", "[ask][command] %s\n", `deck ask "explain apply"`)
	logger.prompt("explain", "system text", "user text")
	logger.response("explain", `{"summary":"ok"}`)
	logText := buf.String()
	for _, want := range []string{"[ask][command] deck ask \"explain apply\"", "[ask][prompt:explain][system]\nsystem text", "[ask][prompt:explain][user]\nuser text", "[ask][response:explain]\n{\"summary\":\"ok\"}"} {
		if !strings.Contains(logText, want) {
			t.Fatalf("expected %q in log output, got %q", want, logText)
		}
	}
	if buf.flushes == 0 {
		t.Fatalf("expected logger to flush output")
	}
}

func TestGenerationSystemPromptIncludesAskContextBlocks(t *testing.T) {
	prompt := generationSystemPrompt(askintent.RouteDraft, askintent.Target{Kind: "workspace"}, askretrieve.RetrievalResult{})
	for _, want := range []string{"Workflow invariants:", "Workflow authoring policy:", "Detailed topology, component/import guidance, vars guidance, and typed-step references are provided through retrieved context."} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected %q in generation prompt, got %q", want, prompt)
		}
	}
	for _, avoid := range []string{"Workspace topology:", "Prepare/apply guidance:", "Components and imports:", "Variables guidance:", "Relevant CLI usage:", "Relevant typed steps:"} {
		if strings.Contains(prompt, avoid) {
			t.Fatalf("expected generation prompt to avoid duplicated context block %q, got %q", avoid, prompt)
		}
	}
}

func TestLoadRequestTextReadsWorkspaceFile(t *testing.T) {
	root := t.TempDir()
	requestPath := filepath.Join(root, "request.md")
	if err := os.WriteFile(requestPath, []byte("extra details\n"), 0o600); err != nil {
		t.Fatalf("write request file: %v", err)
	}
	text, source, err := loadRequestText(root, "base prompt", "request.md")
	if err != nil {
		t.Fatalf("load request text: %v", err)
	}
	if source != "file" {
		t.Fatalf("unexpected source: %s", source)
	}
	if !strings.Contains(text, "base prompt") || !strings.Contains(text, "extra details") {
		t.Fatalf("unexpected request text: %q", text)
	}
}

func TestLoadRequestTextRejectsEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "request.md")
	if err := os.WriteFile(outside, []byte("secret\n"), 0o600); err != nil {
		t.Fatalf("write outside request file: %v", err)
	}
	_, _, err := loadRequestText(root, "", outside)
	if err == nil {
		t.Fatalf("expected escape rejection")
	}
	if !strings.Contains(err.Error(), "resolve ask request file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRequestTextPrefersPlanJSON(t *testing.T) {
	root := t.TempDir()
	planDir := filepath.Join(root, ".deck", "plan")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatalf("mkdir plan dir: %v", err)
	}
	mdPath := filepath.Join(planDir, "sample.md")
	jsonPath := filepath.Join(planDir, "sample.json")
	if err := os.WriteFile(mdPath, []byte("freeform markdown"), 0o600); err != nil {
		t.Fatalf("write md: %v", err)
	}
	json := `{"version":1,"request":"create workflow","intent":"draft","complexity":"complex","blockers":[],"targetOutcome":"generate files","assumptions":[],"openQuestions":[],"entryScenario":"workflows/scenarios/apply.yaml","files":[{"path":"workflows/scenarios/apply.yaml","kind":"scenario","action":"create","purpose":"entry"}],"validationChecklist":["lint"]}`
	if err := os.WriteFile(jsonPath, []byte(json), 0o600); err != nil {
		t.Fatalf("write json: %v", err)
	}
	text, source, err := loadRequestText(root, "", ".deck/plan/sample.md")
	if err != nil {
		t.Fatalf("load request text: %v", err)
	}
	if source != "plan-json" {
		t.Fatalf("expected plan-json source, got %s", source)
	}
	if !strings.Contains(text, "Plan request") || !strings.Contains(text, "workflows/scenarios/apply.yaml") {
		t.Fatalf("expected plan-derived request text, got %q", text)
	}
}

func TestLoadRequestTextFallsBackToPlanMarkdown(t *testing.T) {
	root := t.TempDir()
	planDir := filepath.Join(root, ".deck", "plan")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatalf("mkdir plan dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(planDir, "sample.md"), []byte("freeform markdown"), 0o600); err != nil {
		t.Fatalf("write markdown: %v", err)
	}
	text, source, err := loadRequestText(root, "", ".deck/plan/sample.md")
	if err != nil {
		t.Fatalf("load request text: %v", err)
	}
	if source != "plan-markdown" {
		t.Fatalf("expected plan-markdown source, got %s", source)
	}
	if text != "freeform markdown" {
		t.Fatalf("unexpected text: %q", text)
	}
}

func TestValidateSemanticGenerationRefineRejectsUnplannedFile(t *testing.T) {
	gen := askcontract.GenerationResponse{Files: []askcontract.GeneratedFile{{Path: "workflows/scenarios/apply.yaml", Content: "role: apply\nversion: v1alpha1\nsteps:\n  - id: run\n    kind: Command\n    spec:\n      command: [\"true\"]\n"}, {Path: "workflows/components/new.yaml", Content: "steps: []\n"}}}
	plan := askcontract.PlanResponse{Files: []askcontract.PlanFile{{Path: "workflows/scenarios/apply.yaml", Action: "update"}}}
	err := validateSemanticGeneration(gen, askintent.Decision{Route: askintent.RouteRefine}, plan)
	if err == nil {
		t.Fatalf("expected refine semantic validation failure")
	}
}

func TestRepoMapChunkIncludesImportsRoleAndKinds(t *testing.T) {
	workspace := askretrieve.WorkspaceSummary{Files: []askretrieve.WorkspaceFile{{Path: "workflows/scenarios/apply.yaml", Content: "role: apply\nversion: v1alpha1\nphases:\n  - name: bootstrap\n    imports:\n      - path: bootstrap.yaml\nsteps:\n  - id: run\n    kind: Command\n    spec:\n      command: [\"true\"]\n"}}}
	chunk := repoMapChunk(workspace)
	for _, want := range []string{"role=apply", "imports=bootstrap.yaml", "steps=Command"} {
		if !strings.Contains(chunk.Content, want) {
			t.Fatalf("expected %q in repo map chunk, got %q", want, chunk.Content)
		}
	}
}

func TestPlanWorkspaceChunksIncludeImportedComponents(t *testing.T) {
	workspace := askretrieve.WorkspaceSummary{Files: []askretrieve.WorkspaceFile{{Path: "workflows/scenarios/apply.yaml", Content: "role: apply\nversion: v1alpha1\nphases:\n  - name: bootstrap\n    imports:\n      - path: bootstrap.yaml\n"}, {Path: "workflows/components/bootstrap.yaml", Content: "steps:\n  - id: run\n    kind: Command\n    spec:\n      command: [\"true\"]\n"}}}
	plan := askcontract.PlanResponse{Files: []askcontract.PlanFile{{Path: "workflows/scenarios/apply.yaml", Action: "update"}}}
	chunks := planWorkspaceChunks(plan, workspace)
	if len(chunks) < 2 {
		t.Fatalf("expected planned scenario and imported component chunks, got %d", len(chunks))
	}
}
