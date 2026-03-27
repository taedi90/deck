//go:build ai

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Airgap-Castaways/deck/internal/askconfig"
	"github.com/Airgap-Castaways/deck/internal/askcontext"
	"github.com/Airgap-Castaways/deck/internal/askprovider"
)

type mockAskClient struct {
	responses []string
	index     int
	calls     int
}

func (m *mockAskClient) Generate(_ context.Context, _ askprovider.Request) (askprovider.Response, error) {
	m.calls++
	if m.index >= len(m.responses) {
		return askprovider.Response{Content: m.responses[len(m.responses)-1]}, nil
	}
	resp := m.responses[m.index]
	m.index++
	return askprovider.Response{Content: resp}, nil
}

func TestAskConfigCommands(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	out, err := runWithCapturedStdout([]string{"ask", "config", "set", "--provider", "openrouter", "--model", "anthropic/claude-3.5-sonnet", "--endpoint", "https://openrouter.ai/api/v1", "--api-key", "secret-token", "--oauth-token", "oauth-token", "--log-level", "debug"})
	if err != nil {
		t.Fatalf("config set: %v", err)
	}
	if !strings.Contains(out, "ask config saved") {
		t.Fatalf("unexpected config set output: %q", out)
	}
	out, err = runWithCapturedStdout([]string{"ask", "config", "show"})
	if err != nil {
		t.Fatalf("config show: %v", err)
	}
	for _, want := range []string{"provider=openrouter", "model=anthropic/claude-3.5-sonnet", "endpoint=https://openrouter.ai/api/v1", "endpointSource=config", "logLevel=debug", "mcpEnabled=false", "lspEnabled=false", "apiKey=secr****oken", "apiKeySource=config", "oauthToken=oaut***oken", "oauthTokenSource=config", "authStatus="} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in config show output, got %q", want, out)
		}
	}
	out, err = runWithCapturedStdout([]string{"ask", "config", "unset"})
	if err != nil {
		t.Fatalf("config unset: %v", err)
	}
	if !strings.Contains(out, "ask config cleared") {
		t.Fatalf("unexpected config unset output: %q", out)
	}
}

func TestAskLoginStatusLogoutHeadless(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	out, err := runWithCapturedStdout([]string{"ask", "login", "--provider", "openai", "--headless", "--oauth-token", "oauth-token", "--refresh-token", "refresh-token", "--account-email", "user@example.com", "--expires-at", "2030-01-02T03:04:05Z"})
	if err != nil {
		t.Fatalf("ask login: %v", err)
	}
	if !strings.Contains(out, "ask login saved provider=openai") {
		t.Fatalf("unexpected login output: %q", out)
	}
	out, err = runWithCapturedStdout([]string{"ask", "status", "--provider", "openai"})
	if err != nil {
		t.Fatalf("ask status: %v", err)
	}
	for _, want := range []string{"provider=openai", "authenticated=true", "status=valid", "accountEmail=user@example.com", "hasRefreshToken=true"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in status output, got %q", want, out)
		}
	}
	out, err = runWithCapturedStdout([]string{"ask", "logout", "--provider", "openai"})
	if err != nil {
		t.Fatalf("ask logout: %v", err)
	}
	if !strings.Contains(out, "ask logout removed provider=openai") {
		t.Fatalf("unexpected logout output: %q", out)
	}
	out, err = runWithCapturedStdout([]string{"ask", "status", "--provider", "openai"})
	if err != nil {
		t.Fatalf("ask status after logout: %v", err)
	}
	if !strings.Contains(out, "authenticated=false") {
		t.Fatalf("expected missing auth after logout, got %q", out)
	}
}

func TestAskLoginRejectsNonOpenAIProvider(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	if _, err := runWithCapturedStdout([]string{"ask", "login", "--provider", "gemini", "--headless", "--oauth-token", "token"}); err == nil {
		t.Fatalf("expected non-openai provider to fail")
	} else if !strings.Contains(err.Error(), "supports only provider") {
		t.Fatalf("expected provider guard, got %v", err)
	}
}

func TestAskCommandMetadataMatchesAskContext(t *testing.T) {
	cmd := newAskCommand()
	meta := askcontext.AskCommandMeta()
	if cmd.Short != meta.Short {
		t.Fatalf("unexpected ask short help: %q", cmd.Short)
	}
	plan, _, err := cmd.Find([]string{"plan"})
	if err != nil {
		t.Fatalf("find ask plan: %v", err)
	}
	if plan == nil || plan.Short != meta.Plan.Short || plan.Long != meta.Plan.Long {
		t.Fatalf("unexpected ask plan metadata")
	}
}

func TestAskConfigShowIncludesStoredAugmentSettings(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	if err := askconfig.SaveStored(askconfig.Settings{
		Provider:   "openai",
		Model:      "gpt-5.4",
		APIKey:     "secret-token",
		OAuthToken: "oauth-token",
		LogLevel:   "trace",
		MCP:        askconfig.MCP{Enabled: true, Servers: []askconfig.MCPServer{{Name: "context7", RunCommand: "context7-mcp"}}},
		LSP:        askconfig.LSP{Enabled: true, YAML: askconfig.LSPEntry{RunCommand: "yaml-language-server", Args: []string{"--stdio"}}},
	}); err != nil {
		t.Fatalf("save stored config: %v", err)
	}
	out, err := runWithCapturedStdout([]string{"ask", "config", "show"})
	if err != nil {
		t.Fatalf("config show: %v", err)
	}
	for _, want := range []string{"logLevel=trace", "mcpEnabled=true", "lspEnabled=true"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in config show output, got %q", want, out)
		}
	}
}

func TestAskPreviewAndWrite(t *testing.T) {
	t.Setenv("DECK_ASK_API_KEY", "env-key")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	root := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	originalFactory := newAskBackend
	newAskBackend = func() askprovider.Client {
		return &mockAskClient{responses: []string{validAskJSON()}}
	}
	defer func() { newAskBackend = originalFactory }()

	preview, err := runWithCapturedStdout([]string{"ask", "rhel9 kubeadm cluster scenario"})
	if err != nil {
		t.Fatalf("ask preview: %v", err)
	}
	if !strings.Contains(preview, "preview:") {
		t.Fatalf("expected preview output, got %q", preview)
	}
	if _, err := os.Stat(filepath.Join(root, "workflows", "scenarios", "apply.yaml")); !os.IsNotExist(err) {
		t.Fatalf("preview must not write workflow files")
	}

	writeOut, err := runWithCapturedStdout([]string{"ask", "--write", "rhel9 kubeadm cluster scenario"})
	if err != nil {
		t.Fatalf("ask write: %v", err)
	}
	if !strings.Contains(writeOut, "ask write: ok") {
		t.Fatalf("expected write confirmation, got %q", writeOut)
	}
	if _, err := os.Stat(filepath.Join(root, "workflows", "scenarios", "apply.yaml")); err != nil {
		t.Fatalf("expected written workflow file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".deck", "ask", "context.json")); err != nil {
		t.Fatalf("expected ask context state: %v", err)
	}
}

func TestAskClarifyDoesNotGenerate(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	t.Setenv("DECK_ASK_API_KEY", "env-key")
	root := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	client := &mockAskClient{responses: []string{`{"route":"clarify","confidence":0.9,"reason":"prompt is ambiguous","target":{"kind":"unknown"},"generationAllowed":false}`, `{"summary":"Need clarification","answer":"Please provide a concrete action for ask to perform.","suggestions":["explain workflows/scenarios/apply.yaml","review current apply scenario"]}`}}
	originalFactory := newAskBackend
	newAskBackend = func() askprovider.Client {
		return client
	}
	defer func() { newAskBackend = originalFactory }()

	out, err := runWithCapturedStdout([]string{"ask", "test"})
	if err != nil {
		t.Fatalf("ask clarify: %v", err)
	}
	if !strings.Contains(out, "clarification") && !strings.Contains(out, "Need clarification") {
		t.Fatalf("expected clarification output, got %q", out)
	}
	if client.calls == 0 {
		t.Fatalf("clarify route should use llm when available")
	}
}

func TestAskRepairLoop(t *testing.T) {
	t.Setenv("DECK_ASK_API_KEY", "env-key")
	root := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	originalFactory := newAskBackend
	newAskBackend = func() askprovider.Client {
		return &mockAskClient{responses: []string{`{"summary":"bad","files":[{"path":"workflows/scenarios/apply.yaml","content":"version: v1alpha1\nsteps: ["}]}`, validAskJSON()}}
	}
	defer func() { newAskBackend = originalFactory }()

	out, err := runWithCapturedStdout([]string{"ask", "--write", "--max-iterations", "2", "repair test scenario"})
	if err != nil {
		t.Fatalf("ask write with repair: %v", err)
	}
	if !strings.Contains(out, "lint: lint ok") {
		t.Fatalf("expected lint success after repair, got %q", out)
	}
}

func TestAskReviewMode(t *testing.T) {
	t.Setenv("DECK_ASK_API_KEY", "env-key")
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "workflows", "scenarios"), 0o755); err != nil {
		t.Fatalf("mkdir scenarios: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "workflows", "scenarios", "apply.yaml"), []byte("version: v1alpha1\nsteps:\n  - id: run\n    kind: Command\n    spec:\n      command: [\"true\"]\n"), 0o644); err != nil {
		t.Fatalf("write apply: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "workflows", "prepare.yaml"), []byte("version: v1alpha1\nphases:\n  - name: collect\n    steps: []\n"), 0o644); err != nil {
		t.Fatalf("write prepare: %v", err)
	}
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	client := &mockAskClient{responses: []string{validClassificationReview(), `{"summary":"reviewed workspace","answer":"The apply scenario currently uses a Command step and would benefit from typed steps.","suggestions":["Replace generic Command usage with typed steps where possible."]}`}}
	originalFactory := newAskBackend
	newAskBackend = func() askprovider.Client { return client }
	defer func() { newAskBackend = originalFactory }()

	out, err := runWithCapturedStdout([]string{"ask", "--review"})
	if err != nil {
		t.Fatalf("ask review: %v", err)
	}
	if !strings.Contains(out, "reviewed workspace") || !strings.Contains(out, "local-findings:") {
		t.Fatalf("unexpected review output: %q", out)
	}
	if client.calls == 0 {
		t.Fatalf("review route should use llm when available")
	}
}

func TestAskPlanWritesArtifact(t *testing.T) {
	t.Setenv("DECK_ASK_API_KEY", "env-key")
	root := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	originalFactory := newAskBackend
	newAskBackend = func() askprovider.Client {
		return &mockAskClient{responses: []string{validPlanJSON()}}
	}
	defer func() { newAskBackend = originalFactory }()

	out, err := runWithCapturedStdout([]string{"ask", "plan", "create multi-node cluster workflow"})
	if err != nil {
		t.Fatalf("ask plan: %v", err)
	}
	for _, want := range []string{"plan:", "plan-json:", "next:", "deck ask --from"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in output, got %q", want, out)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".deck", "plan", "latest.md")); err != nil {
		t.Fatalf("expected latest markdown plan: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".deck", "plan", "latest.json")); err != nil {
		t.Fatalf("expected latest json plan: %v", err)
	}
}

func TestAskPlanRejectsNonAuthoringRoute(t *testing.T) {
	t.Setenv("DECK_ASK_API_KEY", "env-key")
	root := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	originalFactory := newAskBackend
	newAskBackend = func() askprovider.Client {
		return &mockAskClient{responses: []string{`{"route":"question","confidence":0.9,"reason":"question","target":{"kind":"workspace"},"generationAllowed":false}`}}
	}
	defer func() { newAskBackend = originalFactory }()

	if _, err := runWithCapturedStdout([]string{"ask", "plan", "what is this workspace"}); err == nil {
		t.Fatalf("expected non-authoring ask plan to fail")
	} else if !strings.Contains(err.Error(), "Try `deck ask") {
		t.Fatalf("expected helpful guidance, got %v", err)
	}
}

func TestAskFromPlanPrefersJSONArtifact(t *testing.T) {
	t.Setenv("DECK_ASK_API_KEY", "env-key")
	root := t.TempDir()
	planDir := filepath.Join(root, ".deck", "plan")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatalf("mkdir plan dir: %v", err)
	}
	mdPath := filepath.Join(planDir, "sample.md")
	jsonPath := filepath.Join(planDir, "sample.json")
	if err := os.WriteFile(mdPath, []byte("human plan text"), 0o600); err != nil {
		t.Fatalf("write md: %v", err)
	}
	if err := os.WriteFile(jsonPath, []byte(validPlanJSON()), 0o600); err != nil {
		t.Fatalf("write json: %v", err)
	}
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	originalFactory := newAskBackend
	newAskBackend = func() askprovider.Client {
		return &mockAskClient{responses: []string{validAskJSON()}}
	}
	defer func() { newAskBackend = originalFactory }()

	out, err := runWithCapturedStdout([]string{"ask", "--from", ".deck/plan/sample.md", "implement this plan"})
	if err != nil {
		t.Fatalf("ask from plan: %v", err)
	}
	if !strings.Contains(out, "preview:") {
		t.Fatalf("expected generation preview, got %q", out)
	}
}

func TestAskPlanShowsBlockers(t *testing.T) {
	t.Setenv("DECK_ASK_API_KEY", "env-key")
	root := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	originalFactory := newAskBackend
	newAskBackend = func() askprovider.Client {
		return &mockAskClient{responses: []string{`{"version":1,"request":"create cluster workflow","intent":"draft","complexity":"complex","blockers":["missing os image details"],"targetOutcome":"Generate workflows","assumptions":[],"openQuestions":["blocking: choose base image"],"entryScenario":"workflows/scenarios/apply.yaml","files":[{"path":"workflows/scenarios/apply.yaml","kind":"scenario","action":"create","purpose":"entry scenario"}],"validationChecklist":["lint"]}`}}
	}
	defer func() { newAskBackend = originalFactory }()

	out, err := runWithCapturedStdout([]string{"ask", "plan", "create air-gapped cluster workflow"})
	if err != nil {
		t.Fatalf("ask plan: %v", err)
	}
	for _, want := range []string{"plan:", "blocker: missing os image details", "next:", "deck ask --from"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in plan output, got %q", want, out)
		}
	}
}

func TestAskComplexPromptShowsJudgeFindingsAndRepairsLoosePlanJSON(t *testing.T) {
	t.Setenv("DECK_ASK_API_KEY", "env-key")
	root := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	originalFactory := newAskBackend
	newAskBackend = func() askprovider.Client {
		return &mockAskClient{responses: []string{
			`{"version":1,"request":"create an air-gapped rhel9 3-node kubeadm workflow","intent":"draft","complexity":"complex","authoringBrief":{"routeIntent":"draft","targetScope":"workspace","targetPaths":["workflows/prepare.yaml","workflows/scenarios/apply.yaml",],"modeIntent":"prepare+apply","connectivity":"offline","completenessTarget":"complete","topology":"multi-node","nodeCount":3,"requiredCapabilities":["prepare-artifacts","kubeadm-bootstrap","kubeadm-join",]},"blockers":[],"targetOutcome":"Generate workflows","assumptions":[],"openQuestions":[],"entryScenario":"workflows/scenarios/apply.yaml","files":[{"path":"workflows/prepare.yaml","kind":"workflow","action":"create","purpose":"prepare"},{"path":"workflows/scenarios/apply.yaml","kind":"scenario","action":"create","purpose":"apply"},],"validationChecklist":["lint",]}`,
			`{"summary":"generated multi-node draft","review":[],"files":[{"path":"workflows/prepare.yaml","content":"version: v1alpha1\nphases:\n  - name: collect\n    steps:\n      - id: collect-packages\n        kind: DownloadPackage\n        spec:\n          packages: [kubeadm]\n          distro:\n            family: rhel\n            release: rocky9\n          repo:\n            type: rpm\n"},{"path":"workflows/scenarios/apply.yaml","content":"version: v1alpha1\nphases:\n  - name: runtime\n    steps:\n      - id: install\n        kind: InstallPackage\n        spec:\n          packages: [kubeadm]\n          source:\n            type: local-repo\n            path: /tmp/packages\n  - name: bootstrap\n    steps:\n      - id: init\n        kind: InitKubeadm\n        spec:\n          outputJoinFile: /tmp/join.sh\n      - id: join\n        kind: JoinKubeadm\n        spec:\n          joinFile: /tmp/join.sh\n      - id: verify\n        kind: CheckCluster\n        spec:\n          interval: 5s\n          nodes:\n            total: 3\n            ready: 3\n            controlPlaneReady: 1\n"}]}`,
		}}
	}
	defer func() { newAskBackend = originalFactory }()

	out, err := runWithCapturedStdout([]string{"ask", "create an air-gapped rhel9 3-node kubeadm cluster workflow with prepare and apply workflows for offline package and image staging"})
	if err != nil {
		t.Fatalf("ask complex prompt: %v", err)
	}
	for _, want := range []string{"preview:", "workflows/prepare.yaml", "workflows/scenarios/apply.yaml"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in output, got %q", want, out)
		}
	}
}

func validAskJSON() string {
	return `{"summary":"generated starter workflows","review":["Prefer typed steps where possible."],"files":[{"path":"workflows/vars.yaml","content":"{}\n"},{"path":"workflows/prepare.yaml","content":"version: v1alpha1\nphases:\n  - name: collect\n    steps: []\n"},{"path":"workflows/scenarios/apply.yaml","content":"version: v1alpha1\nphases:\n  - name: install\n    imports:\n      - path: example-apply.yaml\n"},{"path":"workflows/components/example-apply.yaml","content":"steps:\n  - id: wait-runtime\n    kind: WaitForFile\n    spec:\n      path: /etc/containerd/config.toml\n      interval: 1s\n      timeout: 5s\n"}]}`
}

func validClassificationDraft() string {
	return `{"route":"draft","confidence":0.92,"reason":"user asked to create a scenario","target":{"kind":"workspace"},"generationAllowed":true}`
}

func validClassificationReview() string {
	return `{"route":"review","confidence":0.94,"reason":"user explicitly requested review","target":{"kind":"workspace"},"generationAllowed":false}`
}

func validPlanJSON() string {
	return `{"version":1,"request":"create multi-node cluster workflow","intent":"draft","complexity":"complex","blockers":[],"targetOutcome":"Generate workflows","assumptions":["Use v1alpha1"],"openQuestions":[],"entryScenario":"workflows/scenarios/apply.yaml","files":[{"path":"workflows/scenarios/apply.yaml","kind":"scenario","action":"create","purpose":"entry scenario"},{"path":"workflows/vars.yaml","kind":"vars","action":"create","purpose":"variables"}],"validationChecklist":["lint"]}`
}
