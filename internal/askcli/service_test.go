package askcli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Airgap-Castaways/deck/internal/askconfig"
	"github.com/Airgap-Castaways/deck/internal/askcontract"
	"github.com/Airgap-Castaways/deck/internal/askintent"
	"github.com/Airgap-Castaways/deck/internal/askknowledge"
	"github.com/Airgap-Castaways/deck/internal/askpolicy"
	"github.com/Airgap-Castaways/deck/internal/askprovider"
	"github.com/Airgap-Castaways/deck/internal/askretrieve"
	"github.com/Airgap-Castaways/deck/internal/askscaffold"
)

type stubClient struct {
	responses []string
	calls     int
	prompts   []askprovider.Request
}

type flushBuffer struct {
	bytes.Buffer
	flushes int
}

func (b *flushBuffer) Flush() error {
	b.flushes++
	return nil
}

func (s *stubClient) Generate(_ context.Context, req askprovider.Request) (askprovider.Response, error) {
	s.prompts = append(s.prompts, req)
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
		`{"summary":"should not retry","review":[],"files":[{"path":"workflows/scenarios/apply.yaml","content":"version: v1alpha1\n"}]}`,
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

func TestApplyWriteOverrideKeepsRefineRouteAndEnablesGeneration(t *testing.T) {
	logger := newAskLogger(io.Discard, "trace")
	decision := askintent.Decision{Route: askintent.RouteRefine, AllowGeneration: false, AllowRetry: false, RequiresLint: false, Reason: "llm misflagged generationAllowed false"}
	heuristic := askintent.Decision{Route: askintent.RouteDraft, AllowGeneration: true, AllowRetry: true, RequiresLint: true}
	overridden := applyWriteOverride(decision, heuristic, true, logger)
	if overridden.Route != askintent.RouteRefine {
		t.Fatalf("expected refine route to be preserved, got %#v", overridden)
	}
	if !overridden.AllowGeneration || !overridden.AllowRetry || !overridden.RequiresLint {
		t.Fatalf("expected generation flags to be enabled, got %#v", overridden)
	}
}

func TestApplyWriteOverrideFallsBackToHeuristicForNonGenerationRoute(t *testing.T) {
	logger := newAskLogger(io.Discard, "trace")
	decision := askintent.Decision{Route: askintent.RouteExplain, AllowGeneration: false, Reason: "explain"}
	heuristic := askintent.Decision{Route: askintent.RouteDraft, AllowGeneration: true, AllowRetry: true, RequiresLint: true}
	overridden := applyWriteOverride(decision, heuristic, true, logger)
	if overridden.Route != askintent.RouteDraft || !overridden.AllowGeneration {
		t.Fatalf("expected heuristic generation route, got %#v", overridden)
	}
}

func TestNormalizeArtifactKindsDropsPlannerNoise(t *testing.T) {
	kinds := askpolicy.NormalizeArtifactKinds([]string{"workflow", "scenario", "image", "vars", "package"})
	if strings.Join(kinds, ",") != "image,package" {
		t.Fatalf("unexpected normalized artifact kinds: %v", kinds)
	}
}

func TestGenerateWithValidationRetriesParseFailure(t *testing.T) {
	client := &stubClient{responses: []string{
		`not-json`,
		`{"summary":"ok","review":[],"files":[{"path":"workflows/scenarios/apply.yaml","content":"version: v1alpha1\nsteps:\n  - id: run\n    kind: Command\n    spec:\n      command: [\"true\"]\n"}]}`,
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
		`{"summary":"missing vars","review":[],"files":[{"path":"workflows/scenarios/apply.yaml","content":"version: v1alpha1\nsteps:\n  - id: run\n    kind: Command\n    spec:\n      command: [\"true\"]\n"}]}`,
		`{"summary":"ok","review":[],"files":[{"path":"workflows/scenarios/apply.yaml","content":"version: v1alpha1\nsteps:\n  - id: run\n    kind: Command\n    spec:\n      command: [\"true\"]\n"},{"path":"workflows/vars.yaml","content":"{}\n"}]}`,
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

func TestGenerateWithValidationRepairsKubeadmStyleCheckHostFailure(t *testing.T) {
	client := &stubClient{responses: []string{
		`{"summary":"invalid kubeadm draft","review":[],"files":[{"path":"workflows/prepare.yaml","content":"version: v1alpha1\nsteps:\n  - id: preflight\n    kind: CheckHost\n    spec:\n      os:\n        type: rhel\n        version: \"9\"\n"},{"path":"workflows/scenarios/apply.yaml","content":"version: v1alpha1\nsteps:\n  - id: run\n    kind: Command\n    spec:\n      command: [\"true\"]\n"}]}`,
		`{"summary":"repaired kubeadm draft","review":[],"files":[{"path":"workflows/prepare.yaml","content":"version: v1alpha1\nsteps:\n  - id: preflight\n    kind: CheckHost\n    spec:\n      checks: [os, arch, swap]\n      failFast: true\n"},{"path":"workflows/scenarios/apply.yaml","content":"version: v1alpha1\nsteps:\n  - id: bootstrap\n    kind: UpgradeKubeadm\n    spec:\n      kubernetesVersion: v1.31.0\n  - id: verify-cluster\n    kind: CheckCluster\n    spec:\n      interval: 5s\n      nodes:\n        total: 1\n        ready: 1\n        controlPlaneReady: 1\n"}]}`,
	}}
	plan := askcontract.PlanResponse{
		Request:             "create an air-gapped rhel9 single-node kubeadm workflow using typed steps where possible",
		TargetOutcome:       "Generate a prepare and apply workflow for kubeadm",
		Files:               []askcontract.PlanFile{{Path: "workflows/prepare.yaml", Action: "create"}, {Path: "workflows/scenarios/apply.yaml", Action: "create"}},
		ValidationChecklist: []string{"Typed steps should be used where applicable"},
	}
	gen, _, _, retries, err := generateWithValidation(context.Background(), client, askprovider.Request{Kind: "generate", Provider: "openai", Model: "gpt-5.4", APIKey: "test-key", Prompt: plan.Request}, t.TempDir(), 2, newAskLogger(io.Discard, "trace"), askintent.Decision{Route: askintent.RouteDraft}, plan)
	if err != nil {
		t.Fatalf("expected kubeadm-style repair success: %v", err)
	}
	if retries != 1 || len(gen.Files) != 2 {
		t.Fatalf("unexpected result: retries=%d files=%d", retries, len(gen.Files))
	}
}

func TestGenerateWithValidationRetryPromptIncludesRawValidatorErrorAndRepairGuidance(t *testing.T) {
	client := &stubClient{responses: []string{
		`{"summary":"invalid kubeadm draft","review":[],"files":[{"path":"workflows/prepare.yaml","content":"version: v1alpha1\nsteps:\n  - id: preflight\n    kind: CheckHost\n    spec:\n      os:\n        type: rhel\n        version: \"9\"\n"}]}`,
		`{"summary":"repaired kubeadm draft","review":[],"files":[{"path":"workflows/prepare.yaml","content":"version: v1alpha1\nsteps:\n  - id: preflight\n    kind: CheckHost\n    spec:\n      checks: [os, arch, swap]\n      failFast: true\n"},{"path":"workflows/scenarios/apply.yaml","content":"version: v1alpha1\nsteps:\n  - id: bootstrap\n    kind: UpgradeKubeadm\n    spec:\n      kubernetesVersion: v1.31.0\n  - id: verify-cluster\n    kind: CheckCluster\n    spec:\n      interval: 5s\n      nodes:\n        total: 1\n        ready: 1\n        controlPlaneReady: 1\n"}]}`,
	}}
	plan := askcontract.PlanResponse{
		Request:       "create an air-gapped rhel9 single-node kubeadm workflow using typed steps where possible",
		TargetOutcome: "Generate a prepare workflow for kubeadm",
		Files:         []askcontract.PlanFile{{Path: "workflows/prepare.yaml", Action: "create"}, {Path: "workflows/scenarios/apply.yaml", Action: "create"}},
	}
	_, _, _, _, err := generateWithValidation(context.Background(), client, askprovider.Request{Kind: "generate", Provider: "openai", Model: "gpt-5.4", APIKey: "test-key", Prompt: plan.Request}, t.TempDir(), 2, newAskLogger(io.Discard, "trace"), askintent.Decision{Route: askintent.RouteDraft}, plan)
	if err != nil {
		t.Fatalf("expected repair success: %v", err)
	}
	if len(client.prompts) != 2 {
		t.Fatalf("expected two generate calls, got %d", len(client.prompts))
	}
	retryPrompt := client.prompts[1].Prompt
	for _, want := range []string{"Validator summary:", "Raw validator error:", "CheckHost", "spec.checks", "spec.os", "Structured diagnostics JSON:"} {
		if !strings.Contains(retryPrompt, want) {
			t.Fatalf("expected %q in retry prompt, got %q", want, retryPrompt)
		}
	}
}

func TestSummarizeValidationErrorHighlightsWorkflowSkeletonFixes(t *testing.T) {
	summary := summarizeValidationError("E_SCHEMA_INVALID: (root): version is required; steps.0: id is required; steps.1: id is required")
	for _, want := range []string{"Schema validation failure", "version: v1alpha1", "id` field"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("expected %q in summary, got %q", want, summary)
		}
	}
}

func TestSummarizeValidationErrorRejectsPhaseIDs(t *testing.T) {
	summary := summarizeValidationError("E_SCHEMA_INVALID: (root): version is required; phases.0: Additional property id is not allowed; phases.1: Additional property id is not allowed")
	for _, want := range []string{"Remove `id` from phases", "Phase objects support `name`, `steps`, `imports`, and optional `maxParallelism` only", "version: v1alpha1"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("expected %q in summary, got %q", want, summary)
		}
	}
}

func TestSummarizeValidationErrorHighlightsYAMLShapeFixes(t *testing.T) {
	summary := summarizeValidationError("parse yaml: yaml: line 10: did not find expected node content")
	for _, want := range []string{"YAML parse failure", "template", "valid YAML structure"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("expected %q in summary, got %q", want, summary)
		}
	}
}

func TestLocalExplainDescribesScenarioStructure(t *testing.T) {
	workspace := askretrieve.WorkspaceSummary{
		Files: []askretrieve.WorkspaceFile{
			{Path: "workflows/scenarios/apply.yaml", Content: "version: v1alpha1\nphases:\n  - name: bootstrap\n    imports:\n      - path: bootstrap.yaml\n  - name: verify\n    steps:\n      - id: report\n        kind: Command\n        spec:\n          command: [bash, -lc, \"true\"]\n"},
			{Path: "workflows/components/bootstrap.yaml", Content: "steps:\n  - id: step-one\n    kind: InitKubeadm\n    spec:\n"},
		},
	}
	summary, answer := localExplain(workspace, "explain apply", askintent.Target{Kind: "scenario", Path: "workflows/scenarios/apply.yaml", Name: "apply"})
	if summary == "" {
		t.Fatalf("expected explain summary")
	}
	for _, want := range []string{"version \"v1alpha1\"", "bootstrap, verify", "bootstrap.yaml", "Command x1", "Related component available: workflows/components/bootstrap.yaml"} {
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
	req := askpolicy.ScenarioRequirements{Connectivity: "offline", RequiredFiles: []string{"workflows/scenarios/apply.yaml"}}
	scaffold := askscaffold.Build(req, askretrieve.WorkspaceSummary{}, askintent.Decision{Route: askintent.RouteDraft}, askcontract.PlanResponse{}, askknowledge.Current())
	prompt := generationSystemPrompt(askintent.RouteDraft, askintent.Target{Kind: "workspace"}, askretrieve.RetrievalResult{}, req, scaffold)
	for _, want := range []string{"Workflow source-of-truth:", "Authoring policy from deck metadata:", "Validated scaffold:", "Use retrieved deck knowledge for topology, component/import shape, vars semantics, and typed-step choices."} {
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

func TestRequiredFixesForValidationFlagsTemplatedCollections(t *testing.T) {
	fixes := requiredFixesForValidation("parse yaml: yaml: invalid map key: map[string]interface {}{\".vars.dockerPackages\":interface {}(nil)}")
	if len(fixes) < 2 {
		t.Fatalf("expected extra required fixes, got %v", fixes)
	}
	joined := strings.Join(fixes, "\n")
	if !strings.Contains(joined, "whole-value template expressions") {
		t.Fatalf("unexpected templated collection fix: %v", fixes)
	}
}

func TestRequiredFixesForValidationIncludesCheckHostRepairHint(t *testing.T) {
	fixes := requiredFixesForValidation("E_SCHEMA_INVALID: step check-rhel9-host (CheckHost): spec: checks is required; spec: Additional property os is not allowed")
	joined := strings.Join(fixes, "\n")
	for _, want := range []string{"spec.checks", "spec.os"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %q in fixes, got %v", want, fixes)
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
	gen := askcontract.GenerationResponse{Files: []askcontract.GeneratedFile{{Path: "workflows/scenarios/apply.yaml", Content: "version: v1alpha1\nsteps:\n  - id: run\n    kind: Command\n    spec:\n      command: [\"true\"]\n"}, {Path: "workflows/components/new.yaml", Content: "steps: []\n"}}}
	plan := askcontract.PlanResponse{Files: []askcontract.PlanFile{{Path: "workflows/scenarios/apply.yaml", Action: "update"}}}
	err := validateSemanticGeneration(gen, askintent.Decision{Route: askintent.RouteRefine}, plan)
	if err == nil {
		t.Fatalf("expected refine semantic validation failure")
	}
}

func TestSemanticCriticWarnsWhenTypedStepsRequestedButOnlyCommandUsed(t *testing.T) {
	gen := askcontract.GenerationResponse{Files: []askcontract.GeneratedFile{{Path: "workflows/scenarios/apply.yaml", Content: "version: v1alpha1\nsteps:\n  - id: run\n    kind: Command\n    spec:\n      command: [\"true\"]\n"}}}
	plan := askcontract.PlanResponse{
		Request:             "create an air-gapped rhel9 single-node kubeadm workflow using typed steps where possible",
		TargetOutcome:       "Generate typed-step focused workflows",
		ValidationChecklist: []string{"Typed steps should be used where applicable"},
	}
	critic := semanticCritic(gen, askintent.Decision{Route: askintent.RouteRefine}, plan)
	joined := strings.Join(critic.Advisory, "\n")
	if !strings.Contains(joined, "Prefer") && !strings.Contains(joined, "typed") {
		t.Fatalf("expected typed-step advisory, got %#v", critic)
	}
}

func TestSemanticCriticBlocksOfflineApplyWithDownloads(t *testing.T) {
	gen := askcontract.GenerationResponse{Files: []askcontract.GeneratedFile{{Path: "workflows/scenarios/apply.yaml", Content: "version: v1alpha1\nsteps:\n  - id: fetch\n    kind: Command\n    spec:\n      command: [\"curl\",\"-L\",\"https://example.invalid/pkg.rpm\"]\n"}}}
	plan := askcontract.PlanResponse{Request: "create a package installation workflow", OfflineAssumption: "offline"}
	critic := semanticCritic(gen, askintent.Decision{Route: askintent.RouteDraft}, plan)
	if len(critic.Blocking) == 0 {
		t.Fatalf("expected offline apply blocking finding, got %#v", critic)
	}
}

func TestSemanticCriticRequiresPrepareForArtifactPlan(t *testing.T) {
	gen := askcontract.GenerationResponse{Files: []askcontract.GeneratedFile{{Path: "workflows/scenarios/apply.yaml", Content: "version: v1alpha1\nsteps:\n  - id: install\n    kind: InstallPackage\n    spec:\n      packages: [kubeadm]\n"}}}
	plan := askcontract.PlanResponse{Request: "create an air-gapped package workflow", OfflineAssumption: "offline", NeedsPrepare: true, ArtifactKinds: []string{"package"}}
	critic := semanticCritic(gen, askintent.Decision{Route: askintent.RouteDraft}, plan)
	if len(critic.Blocking) == 0 || !strings.Contains(strings.Join(critic.Blocking, "\n"), "prepare") {
		t.Fatalf("expected prepare blocking finding, got %#v", critic)
	}
}

func TestSemanticCriticKeepsVarsAndComponentsAsAdvisory(t *testing.T) {
	gen := askcontract.GenerationResponse{Files: []askcontract.GeneratedFile{{Path: "workflows/scenarios/apply.yaml", Content: "version: v1alpha1\nsteps:\n  - id: install\n    kind: InstallPackage\n    spec:\n      packages: [kubeadm]\n"}}}
	plan := askcontract.PlanResponse{
		Request:                 "refine the workflow to reuse repeated local values",
		VarsRecommendation:      []string{"Use workflows/vars.yaml for repeated package, image, path, or version values."},
		ComponentRecommendation: []string{"Consider workflows/components/ for reusable repeated logic across phases or scenarios."},
	}
	critic := semanticCritic(gen, askintent.Decision{Route: askintent.RouteDraft}, plan)
	if len(critic.Blocking) != 0 {
		t.Fatalf("expected only advisory findings, got %#v", critic)
	}
	joined := strings.Join(critic.Advisory, "\n")
	for _, want := range []string{"vars.yaml", "components/"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %q advisory, got %#v", want, critic)
		}
	}
}

func TestSemanticCriticDetectsRepeatedValuesForVarsAdvisory(t *testing.T) {
	gen := askcontract.GenerationResponse{Files: []askcontract.GeneratedFile{{Path: "workflows/prepare.yaml", Content: "version: v1alpha1\nsteps:\n  - id: download\n    kind: DownloadPackage\n    spec:\n      packages: [kubeadm]\n      outputDir: packages/kubernetes\n"}, {Path: "workflows/scenarios/apply.yaml", Content: "version: v1alpha1\nsteps:\n  - id: install\n    kind: InstallPackage\n    spec:\n      packages: [kubeadm]\n      source:\n        type: local-repo\n        path: packages/kubernetes\n"}}}
	critic := semanticCritic(gen, askintent.Decision{Route: askintent.RouteDraft}, askcontract.PlanResponse{})
	joined := strings.Join(critic.Advisory, "\n")
	if !strings.Contains(joined, "workflows/vars.yaml") {
		t.Fatalf("expected vars advisory from repeated values, got %#v", critic)
	}
}

func TestSemanticCriticDetectsRepeatedStepSequenceForComponentsAdvisory(t *testing.T) {
	content := "version: v1alpha1\nsteps:\n  - id: check\n    kind: CheckHost\n    spec:\n      checks: [os]\n  - id: verify\n    kind: CheckCluster\n    spec:\n      checks: [nodes_ready]\n"
	gen := askcontract.GenerationResponse{Files: []askcontract.GeneratedFile{{Path: "workflows/prepare.yaml", Content: content}, {Path: "workflows/scenarios/apply.yaml", Content: content}}}
	critic := semanticCritic(gen, askintent.Decision{Route: askintent.RouteDraft}, askcontract.PlanResponse{})
	joined := strings.Join(critic.Advisory, "\n")
	if !strings.Contains(joined, "workflows/components/") {
		t.Fatalf("expected component advisory from repeated sequence, got %#v", critic)
	}
}

func TestSemanticCriticBlocksVarsTemplateInConstrainedLiteralField(t *testing.T) {
	gen := askcontract.GenerationResponse{Files: []askcontract.GeneratedFile{{Path: "workflows/prepare.yaml", Content: "version: v1alpha1\nsteps:\n  - id: prepare-download-kubernetes-packages\n    kind: DownloadPackage\n    spec:\n      packages: [kubeadm]\n      distro:\n        family: rhel\n        release: rocky9\n      repo:\n        type: rpm\n      backend:\n        mode: container\n        runtime: '{{ .vars.packageBackendRuntime }}'\n        image: rockylinux:9\n"}}}
	critic := semanticCritic(gen, askintent.Decision{Route: askintent.RouteDraft}, askcontract.PlanResponse{})
	joined := strings.Join(critic.Blocking, "\n")
	if !strings.Contains(joined, "spec.backend.runtime") {
		t.Fatalf("expected constrained field violation, got %#v", critic)
	}
}

func TestSemanticCriticBlocksPrepareCommandForImageCollection(t *testing.T) {
	gen := askcontract.GenerationResponse{Files: []askcontract.GeneratedFile{{Path: "workflows/prepare.yaml", Content: "version: v1alpha1\nsteps:\n  - id: pull-images\n    kind: Command\n    spec:\n      command: [\"bash\",\"-lc\",\"docker pull registry.k8s.io/kube-apiserver:v1.31.0 && docker save registry.k8s.io/kube-apiserver:v1.31.0 -o images/control-plane/apiserver.tar\"]\n"}}}
	plan := askcontract.PlanResponse{ArtifactKinds: []string{"image"}, NeedsPrepare: true}
	critic := semanticCritic(gen, askintent.Decision{Route: askintent.RouteDraft}, plan)
	joined := strings.Join(critic.Blocking, "\n")
	if !strings.Contains(joined, "typed prepare step") {
		t.Fatalf("expected prepare Command artifact blocking, got %#v", critic)
	}
}

func TestSemanticCriticBlocksIncompleteKubeadmScenario(t *testing.T) {
	gen := askcontract.GenerationResponse{Files: []askcontract.GeneratedFile{{Path: "workflows/scenarios/apply.yaml", Content: "version: v1alpha1\nsteps:\n  - id: preflight\n    kind: CheckHost\n    spec:\n      checks: [os, arch, swap]\n"}}}
	plan := askcontract.PlanResponse{Request: "create an air-gapped rhel9 single-node kubeadm workflow", OfflineAssumption: "offline"}
	req := askpolicy.ScenarioRequirements{AcceptanceLevel: "refine", Connectivity: "offline", ScenarioIntent: []string{"kubeadm"}}
	eval := askpolicy.EvaluateGeneration(req, plan, gen)
	found := false
	for _, finding := range eval.Findings {
		if strings.Contains(finding.Message, "scenario intent") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected kubeadm scenario fidelity blocking, got %#v", eval)
	}
}

func TestRepoMapChunkIncludesImportsModeAndKinds(t *testing.T) {
	workspace := askretrieve.WorkspaceSummary{Files: []askretrieve.WorkspaceFile{{Path: "workflows/scenarios/apply.yaml", Content: "version: v1alpha1\nphases:\n  - name: bootstrap\n    imports:\n      - path: bootstrap.yaml\nsteps:\n  - id: run\n    kind: Command\n    spec:\n      command: [\"true\"]\n"}}}
	chunk := repoMapChunk(workspace)
	for _, want := range []string{"imports=bootstrap.yaml", "steps=Command"} {
		if !strings.Contains(chunk.Content, want) {
			t.Fatalf("expected %q in repo map chunk, got %q", want, chunk.Content)
		}
	}
}

func TestPlanWorkspaceChunksIncludeImportedComponents(t *testing.T) {
	workspace := askretrieve.WorkspaceSummary{Files: []askretrieve.WorkspaceFile{{Path: "workflows/scenarios/apply.yaml", Content: "version: v1alpha1\nphases:\n  - name: bootstrap\n    imports:\n      - path: bootstrap.yaml\n"}, {Path: "workflows/components/bootstrap.yaml", Content: "steps:\n  - id: run\n    kind: Command\n    spec:\n      command: [\"true\"]\n"}}}
	plan := askcontract.PlanResponse{Files: []askcontract.PlanFile{{Path: "workflows/scenarios/apply.yaml", Action: "update"}}}
	chunks := planWorkspaceChunks(plan, workspace)
	if len(chunks) < 2 {
		t.Fatalf("expected planned scenario and imported component chunks, got %d", len(chunks))
	}
}
