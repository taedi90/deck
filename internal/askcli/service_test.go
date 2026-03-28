package askcli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Airgap-Castaways/deck/internal/askconfig"
	"github.com/Airgap-Castaways/deck/internal/askcontext"
	"github.com/Airgap-Castaways/deck/internal/askcontract"
	"github.com/Airgap-Castaways/deck/internal/askintent"
	"github.com/Airgap-Castaways/deck/internal/askknowledge"
	"github.com/Airgap-Castaways/deck/internal/askpolicy"
	"github.com/Airgap-Castaways/deck/internal/askprovider"
	"github.com/Airgap-Castaways/deck/internal/askretrieve"
	"github.com/Airgap-Castaways/deck/internal/askscaffold"
	"github.com/Airgap-Castaways/deck/internal/askstate"
	"github.com/Airgap-Castaways/deck/internal/schemadoc"
	"github.com/Airgap-Castaways/deck/internal/workflowcontract"
	"github.com/Airgap-Castaways/deck/internal/workflowissues"
	"github.com/Airgap-Castaways/deck/schemas"
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
	_, _, _, _, _, err := generateWithValidation(context.Background(), client, askprovider.Request{Kind: "generate", Provider: "openai", Model: "gpt-5.4", APIKey: "test-key"}, t.TempDir(), 2, newAskLogger(io.Discard, "trace"), askintent.Decision{Route: askintent.RouteDraft}, askcontract.PlanResponse{}, askcontract.AuthoringBrief{}, askretrieve.RetrievalResult{}, askcontract.PlanCriticResponse{})
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

func TestBuildPlanWithReviewRetriesOnPlanCriticBlocking(t *testing.T) {
	client := &stubClient{responses: []string{
		`{"version":1,"request":"create 3-node kubeadm workflow","intent":"draft","complexity":"complex","authoringBrief":{"routeIntent":"draft","targetScope":"workspace","targetPaths":["workflows/prepare.yaml","workflows/scenarios/apply.yaml"],"modeIntent":"prepare+apply","connectivity":"offline","completenessTarget":"complete","topology":"multi-node","nodeCount":3,"requiredCapabilities":["prepare-artifacts","kubeadm-bootstrap","kubeadm-join"]},"executionModel":{"artifactContracts":[{"kind":"package","producerPath":"workflows/prepare.yaml","consumerPath":"workflows/scenarios/apply.yaml","description":"offline package flow"}],"roleExecution":{"roleSelector":"vars.role","controlPlaneFlow":"bootstrap","workerFlow":"join","perNodeInvocation":true},"verification":{"bootstrapPhase":"bootstrap-control-plane","finalPhase":"verify-cluster","expectedNodeCount":3,"expectedControlPlaneReady":1},"applyAssumptions":["apply consumes local artifacts"]},"offlineAssumption":"offline","needsPrepare":true,"artifactKinds":["package"],"blockers":[],"targetOutcome":"generate files","assumptions":[],"openQuestions":[],"entryScenario":"workflows/scenarios/apply.yaml","files":[{"path":"workflows/prepare.yaml","kind":"workflow","action":"create","purpose":"prepare"},{"path":"workflows/scenarios/apply.yaml","kind":"scenario","action":"create","purpose":"apply"}],"validationChecklist":["lint"]}`,
		`{"summary":"plan is not viable yet","blocking":["multi-role request has no viable role selector or branching model"],"advisory":[],"missingContracts":[],"suggestedFixes":["Add executionModel.roleExecution.roleSelector for control-plane and worker branching"],"findings":[{"code":"missing_role_selector","severity":"blocking","message":"multi-role request has no viable role selector or branching model","path":"executionModel.roleExecution.roleSelector"}]}`,
		`{"version":1,"request":"create 3-node kubeadm workflow","intent":"draft","complexity":"complex","authoringBrief":{"routeIntent":"draft","targetScope":"workspace","targetPaths":["workflows/prepare.yaml","workflows/scenarios/apply.yaml"],"modeIntent":"prepare+apply","connectivity":"offline","completenessTarget":"complete","topology":"multi-node","nodeCount":3,"requiredCapabilities":["prepare-artifacts","kubeadm-bootstrap","kubeadm-join"]},"executionModel":{"artifactContracts":[{"kind":"package","producerPath":"workflows/prepare.yaml","consumerPath":"workflows/scenarios/apply.yaml","description":"offline package flow"}],"sharedStateContracts":[{"name":"join-file","producerPath":"/tmp/deck/join.txt","consumerPaths":["/tmp/deck/join.txt"],"availabilityModel":"published-for-worker-consumption","description":"publish join file for workers"}],"roleExecution":{"roleSelector":"vars.role","controlPlaneFlow":"bootstrap","workerFlow":"join","perNodeInvocation":true},"verification":{"bootstrapPhase":"bootstrap-control-plane","finalPhase":"verify-cluster","expectedNodeCount":3,"expectedControlPlaneReady":1},"applyAssumptions":["apply consumes local artifacts"]},"offlineAssumption":"offline","needsPrepare":true,"artifactKinds":["package"],"blockers":[],"targetOutcome":"generate files","assumptions":[],"openQuestions":[],"entryScenario":"workflows/scenarios/apply.yaml","files":[{"path":"workflows/prepare.yaml","kind":"workflow","action":"create","purpose":"prepare"},{"path":"workflows/scenarios/apply.yaml","kind":"scenario","action":"create","purpose":"apply"}],"validationChecklist":["lint"]}`,
		`{"summary":"plan is ready","blocking":[],"advisory":["role-aware execution is explicit"],"missingContracts":[],"suggestedFixes":[]}`,
	}}
	plan, critic, usedFallback, err := buildPlanWithReview(context.Background(), client, askconfigSettings{provider: "openai", model: "gpt-5.4", apiKey: "test-key"}, askintent.Decision{Route: askintent.RouteDraft}, askretrieve.RetrievalResult{}, "create an air-gapped rhel9 3-node kubeadm workflow", askretrieve.WorkspaceSummary{}, askpolicy.BuildScenarioRequirements("create an air-gapped rhel9 3-node kubeadm workflow", askretrieve.RetrievalResult{}, askretrieve.WorkspaceSummary{}, askintent.Decision{Route: askintent.RouteDraft}), newAskLogger(io.Discard, "trace"))
	if err != nil {
		t.Fatalf("buildPlanWithReview: %v", err)
	}
	if usedFallback {
		t.Fatalf("expected reviewed plan without fallback")
	}
	if client.calls != 4 {
		t.Fatalf("expected two plan attempts and two critic attempts, got %d calls", client.calls)
	}
	if len(plan.ExecutionModel.SharedStateContracts) != 1 {
		t.Fatalf("expected second plan to include shared-state contract, got %#v", plan.ExecutionModel)
	}
	if len(critic.Blocking) != 0 || critic.Summary != "plan is ready" {
		t.Fatalf("expected final non-blocking critic result, got %#v", critic)
	}
	if len(client.prompts) < 3 || (!strings.Contains(client.prompts[2].Prompt, "role selector") && !strings.Contains(client.prompts[2].Prompt, "Required plan updates before generation")) {
		t.Fatalf("expected replanning prompt to include plan critic findings, got %#v", client.prompts)
	}
}

func TestNormalizePlanCriticDowngradesRecoverableIssues(t *testing.T) {
	plan := askcontract.PlanResponse{ExecutionModel: askcontract.ExecutionModel{
		ArtifactContracts:    []askcontract.ArtifactContract{{Kind: "package", ProducerPath: "workflows/prepare.yaml", ConsumerPath: "workflows/scenarios/apply.yaml"}},
		SharedStateContracts: []askcontract.SharedStateContract{{Name: "join-file", ProducerPath: "/tmp/deck/join.txt", ConsumerPaths: []string{"/tmp/deck/join.txt"}, AvailabilityModel: "published-for-worker-consumption"}},
		RoleExecution:        askcontract.RoleExecutionModel{RoleSelector: "vars.role"},
		Verification:         askcontract.VerificationStrategy{FinalVerificationRole: "control-plane"},
	}}
	critic := normalizePlanCritic(plan, askcontract.PlanCriticResponse{
		Blocking: []string{
			"artifact consumers should bind to explicit artifact contracts",
			"running CheckCluster in both control-plane and worker flows is not realistic",
		},
		MissingContracts: []string{"join-file publication contract"},
	})
	if len(critic.Blocking) != 0 || len(critic.MissingContracts) != 0 {
		t.Fatalf("expected recoverable issues to downgrade, got %#v", critic)
	}
	joined := strings.Join(critic.Advisory, "\n")
	for _, want := range []string{"artifact consumers should bind", "CheckCluster", "join-file publication contract"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %q in advisory, got %#v", want, critic)
		}
	}
}

func TestNormalizePlanCriticPrefersStructuredFindingCodes(t *testing.T) {
	plan := askcontract.PlanResponse{}
	critic := normalizePlanCritic(plan, askcontract.PlanCriticResponse{
		Findings: []askcontract.PlanCriticFinding{
			{Code: workflowissues.CodeMissingRoleSelector, Severity: workflowissues.SeverityBlocking, Message: "role selector missing"},
			{Code: workflowissues.CodeAmbiguousJoinContract, Severity: workflowissues.SeverityMissingContract, Message: "join publication path should be explicit", Recoverable: true},
		},
	})
	if len(critic.Blocking) != 1 || critic.Blocking[0] != "role selector missing" {
		t.Fatalf("expected fatal finding to remain blocking, got %#v", critic)
	}
	if len(critic.MissingContracts) != 0 {
		t.Fatalf("expected recoverable missing contract to downgrade, got %#v", critic)
	}
	if len(critic.Advisory) != 1 || critic.Advisory[0] != "join publication path should be explicit" {
		t.Fatalf("expected recoverable finding to become advisory, got %#v", critic)
	}
}

func TestNormalizePlanCriticDowngradesRecoverableChecksumAndCardinalityRequests(t *testing.T) {
	plan := askcontract.PlanResponse{ExecutionModel: askcontract.ExecutionModel{
		ArtifactContracts:    []askcontract.ArtifactContract{{Kind: "package", ProducerPath: "workflows/prepare.yaml", ConsumerPath: "workflows/scenarios/apply.yaml"}, {Kind: "image", ProducerPath: "workflows/prepare.yaml", ConsumerPath: "workflows/scenarios/apply.yaml"}},
		SharedStateContracts: []askcontract.SharedStateContract{{Name: "join-file", ProducerPath: "/tmp/deck/join.txt", ConsumerPaths: []string{"/tmp/deck/join.txt"}, AvailabilityModel: "published-for-worker-consumption"}},
		RoleExecution:        askcontract.RoleExecutionModel{RoleSelector: "vars.role"},
		Verification:         askcontract.VerificationStrategy{FinalVerificationRole: "control-plane", ExpectedNodeCount: 3, ExpectedControlPlaneReady: 1},
	}}
	critic := normalizePlanCritic(plan, askcontract.PlanCriticResponse{
		Blocking:         []string{"the plan defines two join-state paths and should use a single, canonical worker-consumed join contract"},
		MissingContracts: []string{"vars.artifacts.images.checksum contract object", "role cardinality contract for vars"},
	})
	if len(critic.Blocking) != 0 || len(critic.MissingContracts) != 0 {
		t.Fatalf("expected recoverable checksum/cardinality issues to downgrade, got %#v", critic)
	}
	joined := strings.Join(critic.Advisory, "\n")
	for _, want := range []string{"single, canonical worker-consumed join contract", "checksum contract object", "role cardinality contract"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %q in advisory, got %#v", want, critic)
		}
	}
}

func TestNormalizePlanCriticDowngradesGpt54OperationalCompletenessLanguage(t *testing.T) {
	plan := askcontract.PlanResponse{ExecutionModel: askcontract.ExecutionModel{
		ArtifactContracts:    []askcontract.ArtifactContract{{Kind: "package", ProducerPath: "workflows/prepare.yaml", ConsumerPath: "workflows/scenarios/apply.yaml"}, {Kind: "image", ProducerPath: "workflows/prepare.yaml", ConsumerPath: "workflows/scenarios/apply.yaml"}},
		SharedStateContracts: []askcontract.SharedStateContract{{Name: "join-file", ProducerPath: "workflows/scenarios/apply.yaml", ConsumerPaths: []string{"workflows/scenarios/apply.yaml"}, AvailabilityModel: "published-for-worker-consumption"}},
		RoleExecution:        askcontract.RoleExecutionModel{RoleSelector: "vars.role"},
		Verification:         askcontract.VerificationStrategy{FinalVerificationRole: "control-plane", ExpectedNodeCount: 3, ExpectedControlPlaneReady: 1},
	}}
	critic := normalizePlanCritic(plan, askcontract.PlanCriticResponse{
		Blocking: []string{
			"workflows/scenarios/apply.yaml: The join handoff is not executable as written. reachable through the prepared local file-serving path or equivalent offline shared path is too vague for a shared-state contract",
			"Execution model / workflows/scenarios/apply.yaml: Final CheckCluster on the control-plane can race worker joins because apply is described as per-node role-based execution with no barrier or wait contract",
			"workflows/prepare.yaml -> workflows/scenarios/apply.yaml: The offline image artifact contract is underspecified. The plan does not define the produced image format/bundle layout or the exact path apply LoadImage consumes",
		},
		MissingContracts: []string{
			"Execution model: Synchronization contract between worker joins and the final control-plane CheckCluster step.",
			"Execution model / topology: Role cardinality contract for 3 nodes = 1 control-plane + 2 workers",
		},
	})
	if len(critic.Blocking) != 0 || len(critic.MissingContracts) != 0 {
		t.Fatalf("expected operational-completeness gaps to downgrade, got %#v", critic)
	}
	joined := strings.Join(critic.Advisory, "\n")
	for _, want := range []string{"join handoff is not executable as written", "can race worker joins", "image artifact contract is underspecified", "Synchronization contract", "Role cardinality contract"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %q in advisory, got %#v", want, critic)
		}
	}
}

func TestHasFatalPlanReviewIssuesOnlyForNonViablePlans(t *testing.T) {
	viable := askcontract.PlanResponse{
		AuthoringBrief: askcontract.AuthoringBrief{ModeIntent: "prepare+apply", Topology: "multi-node"},
		EntryScenario:  "workflows/scenarios/apply.yaml",
		Files:          []askcontract.PlanFile{{Path: "workflows/prepare.yaml"}, {Path: "workflows/scenarios/apply.yaml"}},
		ExecutionModel: askcontract.ExecutionModel{RoleExecution: askcontract.RoleExecutionModel{RoleSelector: "vars.role"}, ArtifactContracts: []askcontract.ArtifactContract{{Kind: "package", ProducerPath: "workflows/prepare.yaml", ConsumerPath: "workflows/scenarios/apply.yaml"}}},
		NeedsPrepare:   true,
	}
	critic := askcontract.PlanCriticResponse{Advisory: []string{"join publication path could be more explicit"}, MissingContracts: []string{"topology cardinality vars contract"}}
	if hasFatalPlanReviewIssues(viable, critic) {
		t.Fatalf("expected recoverable review issues to proceed to generation")
	}
	fatalPlan := viable
	fatalPlan.EntryScenario = ""
	if !hasFatalPlanReviewIssues(fatalPlan, askcontract.PlanCriticResponse{}) {
		t.Fatalf("expected missing entry scenario to remain fatal")
	}
}

func TestHasFatalPlanReviewIssuesAllowsRecoverableMissingContracts(t *testing.T) {
	plan := askcontract.PlanResponse{
		AuthoringBrief: askcontract.AuthoringBrief{ModeIntent: "prepare+apply", Topology: "multi-node", RequiredCapabilities: []string{"kubeadm-join"}},
		EntryScenario:  "workflows/scenarios/apply.yaml",
		Files:          []askcontract.PlanFile{{Path: "workflows/prepare.yaml"}, {Path: "workflows/scenarios/apply.yaml"}},
		NeedsPrepare:   true,
	}
	critic := askcontract.PlanCriticResponse{MissingContracts: []string{"join publication contract", "topology cardinality contract"}}
	if hasFatalPlanReviewIssues(plan, critic) {
		t.Fatalf("expected recoverable missing contracts to proceed to generation")
	}
}

func TestHasFatalPlanReviewIssuesIgnoresRecoverablePlannerBlockers(t *testing.T) {
	plan := askcontract.PlanResponse{
		AuthoringBrief: askcontract.AuthoringBrief{ModeIntent: "prepare+apply", Topology: "multi-node"},
		EntryScenario:  "workflows/scenarios/apply.yaml",
		Files:          []askcontract.PlanFile{{Path: "workflows/prepare.yaml"}, {Path: "workflows/scenarios/apply.yaml"}},
		ExecutionModel: askcontract.ExecutionModel{RoleExecution: askcontract.RoleExecutionModel{RoleSelector: "vars.role"}, ArtifactContracts: []askcontract.ArtifactContract{{Kind: "package", ProducerPath: "workflows/prepare.yaml", ConsumerPath: "workflows/scenarios/apply.yaml"}}},
		NeedsPrepare:   true,
		Blockers:       []string{"join publication path is still underspecified", "final verification placement could be stronger"},
	}
	if hasFatalPlanReviewIssues(plan, askcontract.PlanCriticResponse{}) {
		t.Fatalf("expected recoverable planner blockers to continue to generation")
	}
}

func TestHasFatalPlanReviewIssuesDoesNotGateOnPlannerProseAlone(t *testing.T) {
	plan := askcontract.PlanResponse{
		AuthoringBrief: askcontract.AuthoringBrief{ModeIntent: "prepare+apply", Topology: "multi-node"},
		EntryScenario:  "workflows/scenarios/apply.yaml",
		Files:          []askcontract.PlanFile{{Path: "workflows/prepare.yaml"}, {Path: "workflows/scenarios/apply.yaml"}},
		ExecutionModel: askcontract.ExecutionModel{RoleExecution: askcontract.RoleExecutionModel{RoleSelector: "vars.role"}, ArtifactContracts: []askcontract.ArtifactContract{{Kind: "package", ProducerPath: "workflows/prepare.yaml", ConsumerPath: "workflows/scenarios/apply.yaml"}}},
		NeedsPrepare:   true,
		Blockers:       []string{"no viable role selector is available for the worker/control-plane branching model"},
	}
	if hasFatalPlanReviewIssues(plan, askcontract.PlanCriticResponse{}) {
		t.Fatalf("expected planner prose alone not to stop generation")
	}
}

func TestHasFatalPlanReviewIssuesIgnoresRecoverableGpt54PlanCriticBlockers(t *testing.T) {
	plan := askcontract.PlanResponse{
		AuthoringBrief: askcontract.AuthoringBrief{ModeIntent: "prepare+apply", Topology: "multi-node", RequiredCapabilities: []string{"kubeadm-join", "cluster-verification"}},
		EntryScenario:  "workflows/scenarios/apply.yaml",
		Files:          []askcontract.PlanFile{{Path: "workflows/prepare.yaml"}, {Path: "workflows/scenarios/apply.yaml"}, {Path: "workflows/vars.yaml"}},
		ExecutionModel: askcontract.ExecutionModel{
			ArtifactContracts:    []askcontract.ArtifactContract{{Kind: "package", ProducerPath: "workflows/prepare.yaml", ConsumerPath: "workflows/scenarios/apply.yaml"}, {Kind: "image", ProducerPath: "workflows/prepare.yaml", ConsumerPath: "workflows/scenarios/apply.yaml"}},
			SharedStateContracts: []askcontract.SharedStateContract{{Name: "join-file", ProducerPath: "workflows/scenarios/apply.yaml", ConsumerPaths: []string{"workflows/scenarios/apply.yaml"}, AvailabilityModel: "published-for-worker-consumption"}},
			RoleExecution:        askcontract.RoleExecutionModel{RoleSelector: "vars.role"},
			Verification:         askcontract.VerificationStrategy{FinalVerificationRole: "control-plane", ExpectedNodeCount: 3, ExpectedControlPlaneReady: 1},
		},
		NeedsPrepare: true,
	}
	critic := normalizePlanCritic(plan, askcontract.PlanCriticResponse{Blocking: []string{
		"workflows/scenarios/apply.yaml join handoff: the join artifact path is named, but the contract does not specify whether the published file contains a full reusable kubeadm join command",
		"workflows/vars.yaml + apply role logic: the plan says role selector is vars.topology.nodeRole, but it does not define how each running node resolves to one topology entry",
		"Execution model / workflows/scenarios/apply.yaml: Final CheckCluster on the control-plane can race worker joins because apply is described as per-node role-based execution with no barrier or wait contract",
	}})
	if hasFatalPlanReviewIssues(plan, critic) {
		t.Fatalf("expected recoverable gpt-5.4 blocker language to proceed to generation")
	}
}

func TestBuildPlanWithReviewFallsBackOnPlannerFailure(t *testing.T) {
	client := &stubClient{responses: []string{"not-json"}}
	req := askpolicy.BuildScenarioRequirements("create an air-gapped rhel9 3-node kubeadm workflow", askretrieve.RetrievalResult{}, askretrieve.WorkspaceSummary{}, askintent.Decision{Route: askintent.RouteDraft})
	plan, critic, usedFallback, err := buildPlanWithReview(context.Background(), client, askconfigSettings{provider: "openai", model: "gpt-5.4", apiKey: "test-key"}, askintent.Decision{Route: askintent.RouteDraft}, askretrieve.RetrievalResult{}, "create an air-gapped rhel9 3-node kubeadm workflow", askretrieve.WorkspaceSummary{}, req, newAskLogger(io.Discard, "trace"))
	if err != nil {
		t.Fatalf("buildPlanWithReview fallback: %v", err)
	}
	if !usedFallback {
		t.Fatalf("expected fallback path")
	}
	if len(critic.Blocking) != 0 {
		t.Fatalf("expected empty plan critic on fallback, got %#v", critic)
	}
	if plan.ExecutionModel.RoleExecution.RoleSelector != "vars.role" {
		t.Fatalf("expected fallback execution model defaults, got %#v", plan.ExecutionModel)
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
	gen, _, _, _, retries, err := generateWithValidation(context.Background(), client, askprovider.Request{Kind: "generate", Provider: "openai", Model: "gpt-5.4", APIKey: "test-key", Prompt: "generate"}, t.TempDir(), 2, newAskLogger(io.Discard, "trace"), askintent.Decision{Route: askintent.RouteDraft}, askcontract.PlanResponse{}, askcontract.AuthoringBrief{}, askretrieve.RetrievalResult{}, askcontract.PlanCriticResponse{})
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
	gen, _, _, _, retries, err := generateWithValidation(context.Background(), client, askprovider.Request{Kind: "generate", Provider: "openai", Model: "gpt-5.4", APIKey: "test-key", Prompt: "generate"}, t.TempDir(), 2, newAskLogger(io.Discard, "trace"), askintent.Decision{Route: askintent.RouteDraft}, plan, askcontract.AuthoringBrief{}, askretrieve.RetrievalResult{}, askcontract.PlanCriticResponse{})
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
	gen, _, _, _, retries, err := generateWithValidation(context.Background(), client, askprovider.Request{Kind: "generate", Provider: "openai", Model: "gpt-5.4", APIKey: "test-key", Prompt: plan.Request}, t.TempDir(), 2, newAskLogger(io.Discard, "trace"), askintent.Decision{Route: askintent.RouteDraft}, plan, askcontract.AuthoringBrief{}, askretrieve.RetrievalResult{}, askcontract.PlanCriticResponse{})
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
	_, _, _, _, _, err := generateWithValidation(context.Background(), client, askprovider.Request{Kind: "generate", Provider: "openai", Model: "gpt-5.4", APIKey: "test-key", Prompt: plan.Request}, t.TempDir(), 2, newAskLogger(io.Discard, "trace"), askintent.Decision{Route: askintent.RouteDraft}, plan, askcontract.AuthoringBrief{}, askretrieve.RetrievalResult{}, askcontract.PlanCriticResponse{Blocking: []string{"artifact contract is too vague"}, MissingContracts: []string{"package producer path contract"}})
	if err != nil {
		t.Fatalf("expected repair success: %v", err)
	}
	if len(client.prompts) != 2 {
		t.Fatalf("expected two generate calls, got %d", len(client.prompts))
	}
	retryPrompt := client.prompts[1].Prompt
	for _, want := range []string{"Validator summary:", "Raw validator error:", "CheckHost", "spec.checks", "spec.os", "Structured diagnostics JSON:", "Targeted repair mode:", "Affected files to revise first:", "path: workflows/prepare.yaml [revise]", "Preserve unchanged files when they are already valid", "package producer path contract"} {
		if !strings.Contains(retryPrompt, want) {
			t.Fatalf("expected %q in retry prompt, got %q", want, retryPrompt)
		}
	}
	for _, avoid := range []string{"Previously generated files:", "kind: CheckHost\n    spec:\n      os:", "version: v1alpha1\nsteps:"} {
		if strings.Contains(retryPrompt, avoid) {
			t.Fatalf("expected retry prompt to avoid raw previous file exemplar %q, got %q", avoid, retryPrompt)
		}
	}
}

func TestGenerateWithValidationRetryPromptIncludesDuplicateStepIDRepairGuidance(t *testing.T) {
	client := &stubClient{responses: []string{
		`{"summary":"duplicate ids","review":[],"files":[{"path":"workflows/scenarios/apply.yaml","content":"version: v1alpha1\nphases:\n  - name: control-plane\n    steps:\n      - id: preflight-host\n        kind: CheckHost\n        spec:\n          checks: [os, arch, swap]\n  - name: worker\n    steps:\n      - id: preflight-host\n        kind: CheckHost\n        spec:\n          checks: [os, arch, swap]\n"}]}`,
		`{"summary":"repaired ids","review":[],"files":[{"path":"workflows/scenarios/apply.yaml","content":"version: v1alpha1\nphases:\n  - name: control-plane\n    steps:\n      - id: control-plane-preflight-host\n        kind: CheckHost\n        spec:\n          checks: [os, arch, swap]\n  - name: worker\n    steps:\n      - id: worker-preflight-host\n        kind: CheckHost\n        spec:\n          checks: [os, arch, swap]\n"}]}`,
	}}
	plan := askcontract.PlanResponse{Request: "create a multi-phase workflow", Files: []askcontract.PlanFile{{Path: "workflows/scenarios/apply.yaml", Action: "create"}}}
	_, _, _, _, _, err := generateWithValidation(context.Background(), client, askprovider.Request{Kind: "generate-fast", Provider: "openai", Model: "gpt-5.4", APIKey: "test-key", Prompt: plan.Request}, t.TempDir(), 2, newAskLogger(io.Discard, "trace"), askintent.Decision{Route: askintent.RouteDraft}, plan, askcontract.AuthoringBrief{}, askretrieve.RetrievalResult{}, askcontract.PlanCriticResponse{})
	if err != nil {
		t.Fatalf("expected duplicate-id repair success: %v", err)
	}
	if len(client.prompts) != 2 {
		t.Fatalf("expected two generate calls, got %d", len(client.prompts))
	}
	retryPrompt := client.prompts[1].Prompt
	for _, want := range []string{string(workflowissues.CodeDuplicateStepID), "Duplicate step id repair", "control-plane-preflight-host", "worker-preflight-host", "Return the full JSON response with all files"} {
		if !strings.Contains(retryPrompt, want) {
			t.Fatalf("expected %q in duplicate-id retry prompt, got %q", want, retryPrompt)
		}
	}
}

func TestGenerateWithValidationRetryPromptIncludesYamlStructureRepairGuidance(t *testing.T) {
	client := &stubClient{responses: []string{
		`{"summary":"broken yaml","review":[],"files":[{"path":"workflows/scenarios/apply.yaml","content":"version: v1alpha1\nphases:\n  - name: broken\n    steps:\n      - id: run\n        kind: Command\n        spec:\n          command: [\"true\"\n"},{"path":"workflows/vars.yaml","content":"role: control-plane\n"}]}`,
		`{"summary":"repaired yaml","review":[],"files":[{"path":"workflows/scenarios/apply.yaml","content":"version: v1alpha1\nphases:\n  - name: broken\n    steps:\n      - id: run\n        kind: Command\n        spec:\n          command: [\"true\"]\n"},{"path":"workflows/vars.yaml","content":"role: control-plane\n"}]}`,
	}}
	plan := askcontract.PlanResponse{Request: "repair yaml structure", Files: []askcontract.PlanFile{{Path: "workflows/scenarios/apply.yaml", Action: "create"}, {Path: "workflows/vars.yaml", Action: "create"}}}
	_, _, _, _, _, err := generateWithValidation(context.Background(), client, askprovider.Request{Kind: "generate", Provider: "openai", Model: "gpt-5.4", APIKey: "test-key", Prompt: plan.Request}, t.TempDir(), 2, newAskLogger(io.Discard, "trace"), askintent.Decision{Route: askintent.RouteDraft}, plan, askcontract.AuthoringBrief{}, askretrieve.RetrievalResult{}, askcontract.PlanCriticResponse{})
	if err != nil {
		t.Fatalf("expected yaml repair success: %v", err)
	}
	if len(client.prompts) != 2 {
		t.Fatalf("expected two generate calls, got %d", len(client.prompts))
	}
	retryPrompt := client.prompts[1].Prompt
	retrySystem := client.prompts[1].SystemPrompt
	for _, want := range []string{"workflows/scenarios/apply.yaml:", "YAML structure repair requirements:", "Parse-error files to fix first:", "workflows/scenarios/apply.yaml", "keep content byte-for-byte", "top-level `version: v1alpha1` at column 1"} {
		if !strings.Contains(retryPrompt, want) {
			t.Fatalf("expected %q in retry prompt, got %q", want, retryPrompt)
		}
	}
	if !strings.Contains(retrySystem, "YAML repair assistant") {
		t.Fatalf("expected parse-yaml retry to switch system prompt, got %q", retrySystem)
	}
	if !strings.Contains(retrySystem, "only the revised files") {
		t.Fatalf("expected yaml repair system prompt to allow partial file returns, got %q", retrySystem)
	}
	if !strings.Contains(retryPrompt, "workflows/vars.yaml [preserve-if-valid]") {
		t.Fatalf("expected valid vars file to remain preserve-if-valid, got %q", retryPrompt)
	}
	if strings.Contains(retryPrompt, "Previously generated files:") {
		t.Fatalf("expected yaml repair retry prompt to avoid raw previous files, got %q", retryPrompt)
	}
	if !strings.Contains(retryPrompt, "Broken file context:") || !strings.Contains(retryPrompt, "path: workflows/scenarios/apply.yaml") {
		t.Fatalf("expected yaml repair retry prompt to include broken target file context, got %q", retryPrompt)
	}
	if strings.Contains(retryPrompt, "path: workflows/vars.yaml\n````yaml\nrole: control-plane") {
		t.Fatalf("expected preserve-if-valid vars file to stay out of raw broken context, got %q", retryPrompt)
	}
	for _, avoid := range []string{"plan_critic_advisory", "role cardinality", "join-file publication contract", "artifact contract is too vague"} {
		if strings.Contains(retryPrompt, avoid) {
			t.Fatalf("expected yaml repair retry prompt to exclude non-validator advisory %q, got %q", avoid, retryPrompt)
		}
	}
}

func TestGenerateWithValidationMergesPartialYamlRepairResponse(t *testing.T) {
	client := &stubClient{responses: []string{
		`{"summary":"broken yaml","review":[],"files":[{"path":"workflows/prepare.yaml","content":"version: v1alpha1\nphases:\n  - name: collect\n    steps:\n      - id: collect\n        kind: DownloadPackage\n        spec:\n          packages: [kubeadm\n"},{"path":"workflows/vars.yaml","content":"role: control-plane\n"}]}`,
		`{"summary":"patched prepare","review":["repaired prepare indentation"],"files":[{"path":"workflows/prepare.yaml","content":"version: v1alpha1\nphases:\n  - name: collect\n    steps:\n      - id: collect\n        kind: DownloadPackage\n        spec:\n          packages: [kubeadm]\n          distro:\n            family: rhel\n            release: rhel9\n          repo:\n            type: rpm\n"}]}`,
	}}
	plan := askcontract.PlanResponse{Request: "repair yaml structure", Files: []askcontract.PlanFile{{Path: "workflows/prepare.yaml", Action: "create"}, {Path: "workflows/vars.yaml", Action: "create"}}}
	gen, _, _, _, retries, err := generateWithValidation(context.Background(), client, askprovider.Request{Kind: "generate", Provider: "openai", Model: "gpt-5.4", APIKey: "test-key", Prompt: plan.Request}, t.TempDir(), 2, newAskLogger(io.Discard, "trace"), askintent.Decision{Route: askintent.RouteDraft}, plan, askcontract.AuthoringBrief{}, askretrieve.RetrievalResult{}, askcontract.PlanCriticResponse{})
	if err != nil {
		t.Fatalf("expected merged yaml repair success: %v", err)
	}
	if retries != 1 || len(gen.Files) != 2 {
		t.Fatalf("expected merged result with preserved vars file, got retries=%d files=%d", retries, len(gen.Files))
	}
	if !strings.Contains(gen.Files[0].Content+gen.Files[1].Content, "role: control-plane") {
		t.Fatalf("expected preserve-if-valid vars file to survive partial repair merge, got %#v", gen.Files)
	}
}

func TestGenerateWithValidationMergesPartialValidationRepairResponse(t *testing.T) {
	client := &stubClient{responses: []string{
		`{"summary":"draft","review":[],"files":[{"path":"workflows/prepare.yaml","content":"version: v1alpha1\nphases:\n  - name: collect\n    steps:\n      - id: collect\n        kind: DownloadPackage\n        spec:\n          packages: [kubeadm]\n          distro:\n            family: rhel\n            release: rhel9\n          repo:\n            type: rpm\n          outputDir: /tmp/bad\n"},{"path":"workflows/scenarios/apply.yaml","content":"version: v1alpha1\nsteps:\n  - id: init\n    kind: InitKubeadm\n    spec:\n      outputJoinFile: /tmp/join.sh\n  - id: verify\n    kind: CheckCluster\n    spec:\n      interval: 5s\n      nodes:\n        total: 3\n        ready: 1\n        controlPlaneReady: 1\n"}]}`,
		`{"summary":"patched prepare only","review":["repaired prepare output root"],"files":[{"path":"workflows/prepare.yaml","content":"version: v1alpha1\nphases:\n  - name: collect\n    steps:\n      - id: collect\n        kind: DownloadPackage\n        spec:\n          packages: [kubeadm]\n          distro:\n            family: rhel\n            release: rhel9\n          repo:\n            type: rpm\n          outputDir: packages/\n"}]}`,
	}}
	plan := askcontract.PlanResponse{
		Request:        "create an air-gapped 3-node kubeadm prepare and apply workflow",
		Files:          []askcontract.PlanFile{{Path: "workflows/prepare.yaml", Action: "create"}, {Path: "workflows/scenarios/apply.yaml", Action: "create"}},
		AuthoringBrief: askcontract.AuthoringBrief{RouteIntent: "draft", TargetScope: "workspace", ModeIntent: "prepare+apply", Topology: "multi-node", NodeCount: 3},
	}
	brief := plan.AuthoringBrief
	gen, _, _, _, retries, err := generateWithValidation(context.Background(), client, askprovider.Request{Kind: "generate-fast", Provider: "openai", Model: "gpt-5.4", APIKey: "test-key", Prompt: plan.Request}, t.TempDir(), 2, newAskLogger(io.Discard, "trace"), askintent.Decision{Route: askintent.RouteDraft}, plan, brief, askretrieve.RetrievalResult{}, askcontract.PlanCriticResponse{})
	if err != nil {
		t.Fatalf("expected merged validation repair success: %v", err)
	}
	if retries != 1 || len(gen.Files) != 2 {
		t.Fatalf("expected merged result with preserved apply file, got retries=%d files=%d", retries, len(gen.Files))
	}
	joined := gen.Files[0].Content + "\n" + gen.Files[1].Content
	if !strings.Contains(joined, "outputDir: packages/") || !strings.Contains(joined, "kind: InitKubeadm") {
		t.Fatalf("expected prepare patch plus preserved apply file, got %#v", gen.Files)
	}
}

func TestGenerateWithValidationRepairsOnJudgeBlocking(t *testing.T) {
	client := &stubClient{responses: []string{
		`{"summary":"draft one","review":[],"files":[{"path":"workflows/prepare.yaml","content":"version: v1alpha1\nsteps:\n  - id: collect\n    kind: DownloadPackage\n    spec:\n      packages: [kubeadm]\n      repo:\n        type: rpm\n      distro:\n        family: rhel\n        release: rocky9\n"},{"path":"workflows/scenarios/apply.yaml","content":"version: v1alpha1\nsteps:\n  - id: install\n    kind: InstallPackage\n    spec:\n      packages: [kubeadm]\n      source:\n        type: local-repo\n        path: /tmp/packages\n  - id: init\n    kind: InitKubeadm\n    spec:\n      outputJoinFile: /tmp/join.sh\n  - id: join\n    kind: JoinKubeadm\n    spec:\n      joinFile: /tmp/join.sh\n  - id: verify\n    kind: CheckCluster\n    spec:\n      interval: 5s\n      nodes:\n        total: 3\n        ready: 3\n        controlPlaneReady: 1\n"}]}`,
		`{"summary":"platform mismatch","blocking":["request asked for rhel9 but prepare uses rocky9"],"advisory":[],"missingCapabilities":[],"suggestedFixes":["Use an rhel9-compatible distro.release value instead of rocky9 in DownloadPackage"]}`,
		`{"summary":"draft two","review":[],"files":[{"path":"workflows/prepare.yaml","content":"version: v1alpha1\nsteps:\n  - id: collect\n    kind: DownloadPackage\n    spec:\n      packages: [kubeadm]\n      repo:\n        type: rpm\n      distro:\n        family: rhel\n        release: \"9\"\n"},{"path":"workflows/scenarios/apply.yaml","content":"version: v1alpha1\nsteps:\n  - id: install\n    kind: InstallPackage\n    spec:\n      packages: [kubeadm]\n      source:\n        type: local-repo\n        path: /tmp/packages\n  - id: init\n    kind: InitKubeadm\n    spec:\n      outputJoinFile: /tmp/join.sh\n  - id: join\n    kind: JoinKubeadm\n    spec:\n      joinFile: /tmp/join.sh\n  - id: verify\n    kind: CheckCluster\n    spec:\n      interval: 5s\n      nodes:\n        total: 3\n        ready: 3\n        controlPlaneReady: 1\n"}]}`,
		`{"summary":"looks good","blocking":[],"advisory":["prepare now matches the requested rhel9 platform"],"missingCapabilities":[],"suggestedFixes":[]}`,
	}}
	plan := askcontract.PlanResponse{Request: "create an air-gapped 3-node kubeadm prepare and apply workflow", AuthoringBrief: askcontract.AuthoringBrief{RouteIntent: "draft", TargetScope: "workspace", ModeIntent: "prepare+apply", Topology: "multi-node", NodeCount: 3, RequiredCapabilities: []string{"prepare-artifacts", "kubeadm-bootstrap", "kubeadm-join"}}}
	brief := plan.AuthoringBrief
	gen, _, critic, _, retries, err := generateWithValidation(context.Background(), client, askprovider.Request{Kind: "generate", Provider: "openai", Model: "gpt-5.4", APIKey: "test-key", Prompt: plan.Request}, t.TempDir(), 2, newAskLogger(io.Discard, "trace"), askintent.Decision{Route: askintent.RouteDraft}, plan, brief, askretrieve.RetrievalResult{}, askcontract.PlanCriticResponse{})
	if err != nil {
		t.Fatalf("expected judge-driven repair success: %v", err)
	}
	if retries != 1 {
		t.Fatalf("expected one retry, got %d", retries)
	}
	if len(gen.Files) != 2 || client.calls != 4 {
		t.Fatalf("unexpected result files=%d calls=%d", len(gen.Files), client.calls)
	}
	joined := strings.Join(critic.Advisory, "\n")
	if !strings.Contains(joined, "prepare now matches the requested rhel9 platform") {
		t.Fatalf("expected judge advisory to be preserved, got %#v", critic)
	}
	if len(client.prompts) < 3 || !strings.Contains(client.prompts[2].Prompt, "semantic judge requested revision") {
		t.Fatalf("expected retry prompt to include judge revision request, got %#v", client.prompts)
	}
}

func TestGenerateWithValidationKeepsFinalJudgeBlockingAsAdvisory(t *testing.T) {
	client := &stubClient{responses: []string{
		`{"summary":"draft","review":[],"files":[{"path":"workflows/scenarios/apply.yaml","content":"version: v1alpha1\nsteps:\n  - id: init\n    kind: InitKubeadm\n    spec:\n      outputJoinFile: /tmp/join.sh\n  - id: verify\n    kind: CheckCluster\n    spec:\n      interval: 5s\n      nodes:\n        total: 3\n        ready: 1\n        controlPlaneReady: 1\n"}]}`,
		`{"summary":"still thin","blocking":["requested worker join behavior is still missing"],"advisory":[],"missingCapabilities":["kubeadm-join"],"suggestedFixes":["Add worker join steps"]}`,
	}}
	plan := askcontract.PlanResponse{Request: "create an air-gapped 3-node kubeadm workflow", AuthoringBrief: askcontract.AuthoringBrief{RouteIntent: "draft", TargetScope: "workspace", ModeIntent: "apply-only", Topology: "multi-node", NodeCount: 3, RequiredCapabilities: []string{"kubeadm-bootstrap", "kubeadm-join"}}}
	_, _, critic, _, retries, err := generateWithValidation(context.Background(), client, askprovider.Request{Kind: "generate", Provider: "openai", Model: "gpt-5.4", APIKey: "test-key", Prompt: plan.Request}, t.TempDir(), 1, newAskLogger(io.Discard, "trace"), askintent.Decision{Route: askintent.RouteDraft}, plan, plan.AuthoringBrief, askretrieve.RetrievalResult{}, askcontract.PlanCriticResponse{})
	if err != nil {
		t.Fatalf("expected final judge blocking to stay advisory, got %v", err)
	}
	if retries != 0 {
		t.Fatalf("expected no retries, got %d", retries)
	}
	joined := strings.Join(critic.Advisory, "\n")
	for _, want := range []string{"requested worker join behavior is still missing", "kubeadm-join"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %q in final advisory, got %#v", want, critic)
		}
	}
}

func TestSemanticCriticUsesExecutionModelForRoleAndJoinContracts(t *testing.T) {
	gen := askcontract.GenerationResponse{Files: []askcontract.GeneratedFile{{Path: "workflows/scenarios/apply.yaml", Content: "version: v1alpha1\nphases:\n  - name: bootstrap\n    steps:\n      - id: init\n        kind: InitKubeadm\n        spec:\n          outputJoinFile: /tmp/join.sh\n"}}}
	plan := askcontract.PlanResponse{
		Request:        "create 3-node kubeadm workflow",
		AuthoringBrief: askcontract.AuthoringBrief{RouteIntent: "draft", TargetScope: "workspace", ModeIntent: "prepare+apply", Topology: "multi-node", NodeCount: 3},
		ExecutionModel: askcontract.ExecutionModel{
			ArtifactContracts:    []askcontract.ArtifactContract{{Kind: "package", ProducerPath: "workflows/prepare.yaml", ConsumerPath: "workflows/scenarios/apply.yaml"}},
			SharedStateContracts: []askcontract.SharedStateContract{{Name: "join-file", ProducerPath: "/tmp/deck/join.txt", ConsumerPaths: []string{"/tmp/deck/join.txt"}, AvailabilityModel: "published-for-worker-consumption"}},
			RoleExecution:        askcontract.RoleExecutionModel{RoleSelector: "vars.role", PerNodeInvocation: true},
			Verification:         askcontract.VerificationStrategy{ExpectedNodeCount: 3, ExpectedControlPlaneReady: 1},
		},
	}
	critic := semanticCritic(gen, askintent.Decision{Route: askintent.RouteDraft}, plan, plan.AuthoringBrief, askretrieve.RetrievalResult{})
	joinedBlocking := strings.Join(critic.Blocking, "\n")
	if !strings.Contains(joinedBlocking, "artifact producer required by execution model") {
		t.Fatalf("expected artifact producer failure to stay blocking, got %#v", critic)
	}
	joinedAdvisory := strings.Join(critic.Advisory, "\n")
	for _, want := range []string{"role-aware per-node invocation via vars.role"} {
		if !strings.Contains(joinedAdvisory, want) {
			t.Fatalf("expected %q in execution-model critic output, got %#v", want, critic)
		}
	}
}

func TestSemanticCriticBlocksWorkerOnlyFinalVerificationAndMissingJoinPublish(t *testing.T) {
	gen := askcontract.GenerationResponse{Files: []askcontract.GeneratedFile{{Path: "workflows/prepare.yaml", Content: "version: v1alpha1\nsteps:\n  - id: collect\n    kind: DownloadPackage\n    spec:\n      packages: [kubeadm]\n      distro:\n        family: rhel\n        release: \"9\"\n      repo:\n        type: rpm\n      outputDir: /tmp/packages\n"}, {Path: "workflows/scenarios/apply.yaml", Content: "version: v1alpha1\nphases:\n  - name: bootstrap\n    steps:\n      - id: init\n        when: .vars.role == \"control-plane\"\n        kind: InitKubeadm\n        spec:\n          outputJoinFile: /tmp/deck/join.txt\n  - name: join\n    steps:\n      - id: worker-join\n        when: .vars.role == \"worker\"\n        kind: JoinKubeadm\n        spec:\n          joinFile: /tmp/deck/join.txt\n  - name: verify\n    steps:\n      - id: verify-final\n        when: .vars.role == \"worker\"\n        kind: CheckCluster\n        spec:\n          interval: 5s\n          nodes:\n            total: 3\n            ready: 3\n            controlPlaneReady: 3\n"}}}
	plan := askcontract.PlanResponse{Request: "create 3-node kubeadm workflow", AuthoringBrief: askcontract.AuthoringBrief{RouteIntent: "draft", TargetScope: "workspace", ModeIntent: "prepare+apply", Topology: "multi-node", NodeCount: 3}, ExecutionModel: askcontract.ExecutionModel{ArtifactContracts: []askcontract.ArtifactContract{{Kind: "package", ProducerPath: "workflows/prepare.yaml", ConsumerPath: "workflows/scenarios/apply.yaml"}}, SharedStateContracts: []askcontract.SharedStateContract{{Name: "join-file", ProducerPath: "/tmp/deck/server-root/files/cluster/join.txt", ConsumerPaths: []string{"/tmp/deck/server-root/files/cluster/join.txt"}, AvailabilityModel: "published-for-worker-consumption"}}, RoleExecution: askcontract.RoleExecutionModel{RoleSelector: "vars.role", PerNodeInvocation: true}, Verification: askcontract.VerificationStrategy{FinalVerificationRole: "control-plane", ExpectedNodeCount: 3, ExpectedControlPlaneReady: 1}}}
	critic := semanticCritic(gen, askintent.Decision{Route: askintent.RouteDraft}, plan, plan.AuthoringBrief, askretrieve.RetrievalResult{})
	if len(critic.Blocking) != 0 {
		t.Fatalf("expected recoverable design issues to stay advisory, got %#v", critic)
	}
	joined := strings.Join(critic.Advisory, "\n")
	for _, want := range []string{"published shared-state availability", "expected control-plane role", "expected nodes=3 controlPlaneReady=1"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %q in execution-model critic output, got %#v", want, critic)
		}
	}
}

func TestMaybePostProcessGenerationAppliesOperationalRepairOnly(t *testing.T) {
	client := &stubClient{responses: []string{
		`{"summary":"final verification should be gated to control-plane and inline structure is acceptable","blocking":["final cluster verification should run only on the control-plane role for this draft"],"advisory":["preserve inline structure for now"],"upgradeCandidates":["preserve-inline"],"reviseFiles":["workflows/scenarios/apply.yaml"],"preserveFiles":["workflows/prepare.yaml","workflows/vars.yaml"],"suggestedFixes":["Gate the final CheckCluster step with .vars.role == \"control-plane\""]}`,
		`{"summary":"post-processed draft","review":[],"files":[{"path":"workflows/prepare.yaml","content":"version: v1alpha1\nphases:\n  - name: collect\n    steps:\n      - id: packages\n        kind: DownloadPackage\n        spec:\n          packages: [kubeadm]\n          distro:\n            family: rhel\n            release: \"9\"\n          repo:\n            type: rpm\n          outputDir: packages/kubernetes\n"},{"path":"workflows/scenarios/apply.yaml","content":"version: v1alpha1\nphases:\n  - name: bootstrap\n    steps:\n      - id: init\n        when: .vars.role == \"control-plane\"\n        kind: InitKubeadm\n        spec:\n          outputJoinFile: /tmp/deck/join.txt\n      - id: join\n        when: .vars.role == \"worker\"\n        kind: JoinKubeadm\n        spec:\n          joinFile: /tmp/deck/join.txt\n      - id: verify\n        when: .vars.role == \"control-plane\"\n        kind: CheckCluster\n        spec:\n          interval: 5s\n          nodes:\n            total: 3\n            ready: 3\n            controlPlaneReady: 1\n"},{"path":"workflows/vars.yaml","content":"role: control-plane\n"}]}`,
		`{"summary":"post-processed workflow now has control-plane scoped final verification","blocking":[],"advisory":["inline structure is fine for now"],"missingCapabilities":[],"suggestedFixes":[]}`,
	}}
	plan := askcontract.PlanResponse{Request: "create 3-node kubeadm workflow", AuthoringBrief: askcontract.AuthoringBrief{ModeIntent: "prepare+apply", CompletenessTarget: "complete", Topology: "multi-node"}, ExecutionModel: askcontract.ExecutionModel{ArtifactContracts: []askcontract.ArtifactContract{{Kind: "package", ProducerPath: "workflows/prepare.yaml", ConsumerPath: "workflows/scenarios/apply.yaml"}}, SharedStateContracts: []askcontract.SharedStateContract{{Name: "join-file", ProducerPath: "/tmp/deck/join.txt", ConsumerPaths: []string{"/tmp/deck/join.txt"}, AvailabilityModel: "local-only"}}, RoleExecution: askcontract.RoleExecutionModel{RoleSelector: "vars.role", PerNodeInvocation: true}, Verification: askcontract.VerificationStrategy{ExpectedNodeCount: 3, ExpectedControlPlaneReady: 1}}}
	brief := plan.AuthoringBrief
	gen := askcontract.GenerationResponse{Summary: "draft", Files: []askcontract.GeneratedFile{{Path: "workflows/prepare.yaml", Content: "version: v1alpha1\nphases:\n  - name: collect\n    steps:\n      - id: packages\n        kind: DownloadPackage\n        spec:\n          packages: [kubeadm]\n          distro:\n            family: rhel\n            release: \"9\"\n          repo:\n            type: rpm\n          outputDir: packages/kubernetes\n"}, {Path: "workflows/scenarios/apply.yaml", Content: "version: v1alpha1\nphases:\n  - name: bootstrap\n    steps:\n      - id: init\n        when: .vars.role == \"control-plane\"\n        kind: InitKubeadm\n        spec:\n          outputJoinFile: /tmp/deck/join.txt\n      - id: join\n        when: .vars.role == \"worker\"\n        kind: JoinKubeadm\n        spec:\n          joinFile: /tmp/deck/join.txt\n      - id: verify\n        kind: CheckCluster\n        spec:\n          interval: 5s\n          nodes:\n            total: 3\n            ready: 3\n            controlPlaneReady: 1\n"}, {Path: "workflows/vars.yaml", Content: "role: control-plane\n"}}}
	judge := askcontract.JudgeResponse{Summary: "usable but final verification should not run on workers", Advisory: []string{"final verification placement should be control-plane only"}}
	summary, err := maybePostProcessGeneration(context.Background(), client, askprovider.Request{Kind: "generate", Provider: "openai", Model: "gpt-5.4", APIKey: "test-key"}, t.TempDir(), newAskLogger(io.Discard, "trace"), askintent.Decision{Route: askintent.RouteDraft}, plan, brief, askretrieve.RetrievalResult{}, gen, "lint ok (1 workflows)", askcontract.CriticResponse{}, judge, askcontract.PlanCriticResponse{Advisory: []string{"shared-state publish path should stay explicit"}})
	if err != nil {
		t.Fatalf("maybePostProcessGeneration: %v", err)
	}
	if !summary.Applied {
		t.Fatalf("expected post-processing edit to apply")
	}
	if !strings.Contains(strings.Join(summary.Notes, "\n"), "preserve-inline") {
		t.Fatalf("expected preserve-inline advisory in notes, got %#v", summary.Notes)
	}
	if !strings.Contains(summary.Generation.Files[1].Content, "when: .vars.role == \"control-plane\"") {
		t.Fatalf("expected final verification to be gated after post-process, got %q", summary.Generation.Files[1].Content)
	}
}

func TestMaybePostProcessGenerationSkipsStructuralCleanupOnlyAdvice(t *testing.T) {
	client := &stubClient{responses: []string{
		`{"summary":"draft is operationally sound; only optional cleanup remains","blocking":[],"advisory":["extract-vars"],"upgradeCandidates":["extract-vars","preserve-inline"],"reviseFiles":[],"preserveFiles":["workflows/prepare.yaml","workflows/scenarios/apply.yaml","workflows/vars.yaml"],"suggestedFixes":[]}`,
	}}
	plan := askcontract.PlanResponse{Request: "create 3-node kubeadm workflow", AuthoringBrief: askcontract.AuthoringBrief{ModeIntent: "prepare+apply", CompletenessTarget: "complete", Topology: "multi-node"}}
	gen := askcontract.GenerationResponse{Summary: "draft", Files: []askcontract.GeneratedFile{{Path: "workflows/prepare.yaml", Content: "version: v1alpha1\nsteps:\n  - id: collect\n    kind: DownloadPackage\n    spec:\n      packages: [kubeadm]\n      distro:\n        family: rhel\n        release: \"9\"\n      repo:\n        type: rpm\n"}, {Path: "workflows/scenarios/apply.yaml", Content: "version: v1alpha1\nsteps:\n  - id: verify\n    when: .vars.role == \"control-plane\"\n    kind: CheckCluster\n    spec:\n      interval: 5s\n      nodes:\n        total: 3\n        ready: 3\n        controlPlaneReady: 1\n"}}}
	judge := askcontract.JudgeResponse{Summary: "mostly good", Advisory: []string{"worker join and verification are acceptable"}}
	summary, err := maybePostProcessGeneration(context.Background(), client, askprovider.Request{Kind: "generate", Provider: "openai", Model: "gpt-5.4", APIKey: "test-key"}, t.TempDir(), newAskLogger(io.Discard, "trace"), askintent.Decision{Route: askintent.RouteDraft}, plan, plan.AuthoringBrief, askretrieve.RetrievalResult{}, gen, "lint ok (1 workflows)", askcontract.CriticResponse{}, judge, askcontract.PlanCriticResponse{})
	if err != nil {
		t.Fatalf("maybePostProcessGeneration skip structural: %v", err)
	}
	if summary.Applied {
		t.Fatalf("expected no post-process edit for structural-only advice")
	}
	if !strings.Contains(strings.Join(summary.Notes, "\n"), "extract-vars") {
		t.Fatalf("expected structural advisory note, got %#v", summary.Notes)
	}
}

func TestMaybePostProcessGenerationAppliesOptionalStructuralCleanupWhenHeuristicsAreStrong(t *testing.T) {
	client := &stubClient{responses: []string{
		`{"summary":"operationally sound; repeated paths suggest optional cleanup","blocking":[],"advisory":["extract-vars could reduce repeated path literals"],"upgradeCandidates":["extract-vars","preserve-inline"],"reviseFiles":[],"preserveFiles":["workflows/prepare.yaml","workflows/scenarios/apply.yaml"],"requiredEdits":[],"verificationExpectations":["lint stays green"],"suggestedFixes":[]}`,
		`{"summary":"structurally cleaned draft","review":[],"files":[{"path":"workflows/vars.yaml","content":"artifactRoot: /srv/offline/kubernetes\n"},{"path":"workflows/prepare.yaml","content":"version: v1alpha1\nsteps:\n  - id: collect\n    kind: DownloadPackage\n    spec:\n      packages: [kubeadm]\n      distro:\n        family: rhel\n        release: \"9\"\n      repo:\n        type: rpm\n      outputDir: \"{{ .vars.artifactRoot }}\"\n"},{"path":"workflows/scenarios/apply.yaml","content":"version: v1alpha1\nsteps:\n  - id: init\n    kind: InitKubeadm\n    spec:\n      outputJoinFile: /tmp/join.sh\n  - id: install\n    kind: InstallPackage\n    spec:\n      packages: [kubeadm]\n      source:\n        type: local-repo\n        path: \"{{ .vars.artifactRoot }}\"\n  - id: verify\n    kind: CheckCluster\n    spec:\n      interval: 5s\n      nodes:\n        total: 3\n        ready: 1\n        controlPlaneReady: 1\n"}]}`,
		`{"summary":"structural cleanup preserved behavior","blocking":[],"advisory":["vars extraction is acceptable"],"missingCapabilities":[],"suggestedFixes":[]}`,
	}}
	plan := askcontract.PlanResponse{Request: "create 3-node kubeadm workflow", AuthoringBrief: askcontract.AuthoringBrief{ModeIntent: "prepare+apply", CompletenessTarget: "complete", Topology: "multi-node"}}
	gen := askcontract.GenerationResponse{Summary: "draft", Files: []askcontract.GeneratedFile{{Path: "workflows/prepare.yaml", Content: "version: v1alpha1\nsteps:\n  - id: collect\n    kind: DownloadPackage\n    spec:\n      packages: [kubeadm]\n      distro:\n        family: rhel\n        release: \"9\"\n      repo:\n        type: rpm\n      outputDir: /srv/offline/kubernetes\n"}, {Path: "workflows/scenarios/apply.yaml", Content: "version: v1alpha1\nsteps:\n  - id: init\n    kind: InitKubeadm\n    spec:\n      outputJoinFile: /tmp/join.sh\n  - id: install\n    kind: InstallPackage\n    spec:\n      packages: [kubeadm]\n      source:\n        type: local-repo\n        path: /srv/offline/kubernetes\n  - id: verify\n    kind: CheckCluster\n    spec:\n      interval: 5s\n      nodes:\n        total: 3\n        ready: 1\n        controlPlaneReady: 1\n"}}}
	judge := askcontract.JudgeResponse{Summary: "operationally sound", Advisory: []string{"artifact paths are consistent"}}
	summary, err := maybePostProcessGeneration(context.Background(), client, askprovider.Request{Kind: "generate", Provider: "openai", Model: "gpt-5.4", APIKey: "test-key"}, t.TempDir(), newAskLogger(io.Discard, "trace"), askintent.Decision{Route: askintent.RouteDraft}, plan, plan.AuthoringBrief, askretrieve.RetrievalResult{}, gen, "lint ok (1 workflows)", askcontract.CriticResponse{}, judge, askcontract.PlanCriticResponse{})
	if err != nil {
		t.Fatalf("maybePostProcessGeneration structural cleanup: %v", err)
	}
	if !summary.Applied {
		t.Fatalf("expected structural cleanup to apply")
	}
	if !strings.Contains(strings.Join(summary.Notes, "\n"), "optional structural cleanup") {
		t.Fatalf("expected structural cleanup note, got %#v", summary.Notes)
	}
	if len(summary.Generation.Files) != 3 || !strings.Contains(summary.Generation.Files[0].Path, "vars") {
		t.Fatalf("expected vars extraction result, got %#v", summary.Generation.Files)
	}
}

func TestEnrichPostProcessFindingsAddsPreserveInlineAndVarCleanupAdvisory(t *testing.T) {
	gen := askcontract.GenerationResponse{Files: []askcontract.GeneratedFile{{Path: "workflows/prepare.yaml", Content: "version: v1alpha1\nsteps:\n  - id: fetch\n    kind: DownloadPackage\n    spec:\n      outputDir: /srv/offline/kubernetes\n"}, {Path: "workflows/scenarios/apply.yaml", Content: "version: v1alpha1\nsteps:\n  - id: install\n    kind: InstallPackage\n    spec:\n      source:\n        type: local-repo\n        path: /srv/offline/kubernetes\n"}}}
	findings := enrichPostProcessFindings(askcontract.PostProcessResponse{}, gen)
	joined := strings.Join(findings.Advisory, "\n")
	if !strings.Contains(joined, "extract-vars") {
		t.Fatalf("expected extract-vars advisory, got %#v", findings)
	}
	if !containsTrimmed(findings.UpgradeCandidates, "preserve-inline") {
		t.Fatalf("expected preserve-inline candidate, got %#v", findings)
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
	retrieval := askretrieve.RetrievalResult{Chunks: []askretrieve.Chunk{{ID: "example-1", Source: "example", Label: "test/workflows/scenarios/kubeadm.yaml", Content: "Reference example:\n- path: test/workflows/scenarios/kubeadm.yaml\nversion: v1alpha1\nsteps:\n  - id: init\n    kind: InitKubeadm\n", Score: 90}, {ID: "workspace-apply", Source: "workspace", Label: "workflows/scenarios/apply.yaml", Content: "version: v1alpha1\nsteps:\n  - id: join\n    kind: JoinKubeadm\n", Score: 80}, {ID: "typed-steps-draft", Source: "askcontext", Topic: askcontext.TopicTypedSteps, Label: "typed-steps", Content: "typed guidance", Score: 70}}}
	prompt := generationSystemPrompt(askintent.RouteDraft, askintent.Target{Kind: "workspace"}, "create an air-gapped 3-node kubeadm workflow", retrieval, req, askcontract.AuthoringBrief{ModeIntent: "prepare+apply", Connectivity: "offline", CompletenessTarget: "starter", Topology: "multi-node", RequiredCapabilities: []string{"kubeadm-join", "cluster-verification"}}, askcontract.ExecutionModel{ArtifactContracts: []askcontract.ArtifactContract{{Kind: "package", ProducerPath: "workflows/prepare.yaml", ConsumerPath: "workflows/scenarios/apply.yaml", Description: "offline package flow"}}, SharedStateContracts: []askcontract.SharedStateContract{{Name: "join-file", ProducerPath: "/tmp/deck/join.txt", ConsumerPaths: []string{"/tmp/deck/join.txt"}, AvailabilityModel: "published-for-worker-consumption"}}, RoleExecution: askcontract.RoleExecutionModel{RoleSelector: "vars.role", ControlPlaneFlow: "bootstrap", WorkerFlow: "join", PerNodeInvocation: true}, Verification: askcontract.VerificationStrategy{FinalVerificationRole: "control-plane", ExpectedNodeCount: 3, ExpectedControlPlaneReady: 1}, ApplyAssumptions: []string{"apply consumes local artifacts"}}, scaffold)
	for _, want := range []string{"Workflow source-of-truth:", "Authoring policy from deck metadata:", "Validated scaffold:", "Use retrieved deck knowledge for topology, component/import shape, vars semantics, and typed-step choices."} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected %q in generation prompt, got %q", want, prompt)
		}
	}
	if !strings.Contains(prompt, "Primary repository context follows.") || !strings.Contains(prompt, "Reference example:") || !strings.Contains(prompt, "JoinKubeadm") {
		t.Fatalf("expected repository context to appear in generation prompt, got %q", prompt)
	}
	if strings.Index(prompt, "Primary repository context follows.") > strings.Index(prompt, "Workflow source-of-truth:") {
		t.Fatalf("expected repository context before abstract policy blocks, got %q", prompt)
	}
	for _, want := range []string{"Normalized authoring brief:", "mode intent: prepare+apply", "connectivity: offline", "completeness target: starter", "Normalized execution model:", "artifact package: workflows/prepare.yaml -> workflows/scenarios/apply.yaml", "shared state join-file:", "role selector: vars.role", "verification expected nodes: 3", "verification final role: control-plane", "JoinKubeadm", "supports kubeadm join capability", "Step composition guidance:", "Multi-node kubeadm flow:"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected %q in generation prompt, got %q", want, prompt)
		}
	}
	for _, avoid := range []string{"Workspace topology:", "Prepare/apply guidance:", "Components and imports:", "Variables guidance:", "Relevant CLI usage:", "typed guidance"} {
		if strings.Contains(prompt, avoid) {
			t.Fatalf("expected generation prompt to avoid duplicated context block %q, got %q", avoid, prompt)
		}
	}
}

func TestAskRequestTimeoutScalesGenerationByIterationsAndPromptSize(t *testing.T) {
	small := askRequestTimeout("generate", 1, "sys", "user")
	large := askRequestTimeout("generate", 5, strings.Repeat("s", 12000), strings.Repeat("u", 12000))
	if large <= small {
		t.Fatalf("expected larger generation timeout for more iterations and prompt bytes, got small=%s large=%s", small, large)
	}
	if askRequestTimeout("classify", 1, "sys", "user") >= small {
		t.Fatalf("expected classify timeout to stay below generation timeout")
	}
}

func TestGenerationRetrievalPromptBlockSkipsProjectContextAndCapsExamples(t *testing.T) {
	retrieval := askretrieve.RetrievalResult{Chunks: []askretrieve.Chunk{
		{ID: "example-1", Source: "example", Label: "a", Content: "Reference example:\n- path: a\n", Score: 100},
		{ID: "example-2", Source: "example", Label: "b", Content: "Reference example:\n- path: b\n", Score: 99},
		{ID: "example-3", Source: "example", Label: "c", Content: "Reference example:\n- path: c\n", Score: 98},
		{ID: "project", Source: "project", Label: "project-context", Content: "Project context:\nvery long", Score: 97},
	}}
	block := generationRetrievalPromptBlock(retrieval)
	if strings.Contains(block, "Project context:") {
		t.Fatalf("expected project context to be skipped, got %q", block)
	}
	if strings.Count(block, "Reference example:") != 2 {
		t.Fatalf("expected exactly two examples in generation retrieval block, got %q", block)
	}
}

func TestGenerationSystemPromptCarriesExplicitStepFieldRequirements(t *testing.T) {
	req := askpolicy.ScenarioRequirements{Connectivity: "unspecified", RequiredFiles: []string{"workflows/prepare.yaml"}, NeedsPrepare: true}
	scaffold := askscaffold.Build(req, askretrieve.WorkspaceSummary{}, askintent.Decision{Route: askintent.RouteDraft, Target: askintent.Target{Kind: "scenario", Path: "workflows/prepare.yaml"}}, askcontract.PlanResponse{}, askknowledge.Current())
	prompt := generationSystemPrompt(askintent.RouteDraft, askintent.Target{Kind: "scenario", Path: "workflows/prepare.yaml"}, "create a prepare workflow using DownloadFile and default output location", askretrieve.RetrievalResult{}, req, askcontract.AuthoringBrief{ModeIntent: "prepare-only", CompletenessTarget: "complete"}, askcontract.ExecutionModel{}, scaffold)
	for _, want := range []string{"Candidate typed steps you may choose from:", "DownloadFile", "spec.source [conditional]", "spec.fetch [optional]", "spec.mode [optional]", "rule: At least one of `spec.source` or `spec.items` must be set.", "minimal valid shape:"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected %q in generation prompt, got %q", want, prompt)
		}
	}
}

func TestGenerationSystemPromptTreatsTypedStepsAsCandidatesAndShowsBootstrapShape(t *testing.T) {
	req := askpolicy.ScenarioRequirements{Connectivity: "offline", RequiredFiles: []string{"workflows/scenarios/apply.yaml"}}
	scaffold := askscaffold.Build(req, askretrieve.WorkspaceSummary{}, askintent.Decision{Route: askintent.RouteDraft}, askcontract.PlanResponse{}, askknowledge.Current())
	prompt := generationSystemPrompt(askintent.RouteDraft, askintent.Target{Kind: "workspace"}, "create an air-gapped rhel9 single-node kubeadm workflow", askretrieve.RetrievalResult{}, req, askcontract.AuthoringBrief{ModeIntent: "apply-only", Connectivity: "offline", CompletenessTarget: "starter", Topology: "single-node", RequiredCapabilities: []string{"kubeadm-bootstrap", "cluster-verification"}}, askcontract.ExecutionModel{}, scaffold)
	for _, want := range []string{"Candidate typed steps you may choose from:", "These are hints, not required selections.", "InitKubeadm", "CheckHost", "minimal valid shape:"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected %q in generation prompt, got %q", want, prompt)
		}
	}
}

func TestGenerationSystemPromptCarriesWorkflowStepIDUniquenessRule(t *testing.T) {
	req := askpolicy.ScenarioRequirements{Connectivity: "offline", RequiredFiles: []string{"workflows/prepare.yaml", "workflows/scenarios/apply.yaml"}, NeedsPrepare: true}
	scaffold := askscaffold.Build(req, askretrieve.WorkspaceSummary{}, askintent.Decision{Route: askintent.RouteDraft}, askcontract.PlanResponse{}, askknowledge.Current())
	prompt := generationSystemPrompt(askintent.RouteDraft, askintent.Target{Kind: "workspace"}, "create a 3-node workflow", askretrieve.RetrievalResult{}, req, askcontract.AuthoringBrief{ModeIntent: "prepare+apply", CompletenessTarget: "complete"}, askcontract.ExecutionModel{}, scaffold)
	for _, want := range []string{"Every step id must be unique across top-level steps and steps nested under phases.", "Rename duplicate step ids with role- or phase-specific prefixes instead of reusing the same id."} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected %q in generation prompt, got %q", want, prompt)
		}
	}
}

func TestPromptAndDocsShareDownloadFileRuleSummary(t *testing.T) {
	page := testSchemaDocFamilyPageInput(t, "file")
	rendered := string(schemadoc.RenderToolPage(page))
	req := askpolicy.ScenarioRequirements{Connectivity: "unspecified", RequiredFiles: []string{"workflows/prepare.yaml"}, NeedsPrepare: true}
	scaffold := askscaffold.Build(req, askretrieve.WorkspaceSummary{}, askintent.Decision{Route: askintent.RouteDraft, Target: askintent.Target{Kind: "scenario", Path: "workflows/prepare.yaml"}}, askcontract.PlanResponse{}, askknowledge.Current())
	prompt := generationSystemPrompt(askintent.RouteDraft, askintent.Target{Kind: "scenario", Path: "workflows/prepare.yaml"}, "create a prepare workflow using DownloadFile", askretrieve.RetrievalResult{}, req, askcontract.AuthoringBrief{ModeIntent: "prepare-only", CompletenessTarget: "complete"}, askcontract.ExecutionModel{}, scaffold)
	rule := "At least one of `spec.source` or `spec.items` must be set."
	if !strings.Contains(rendered, rule) {
		t.Fatalf("expected docs to include %q, got %q", rule, rendered)
	}
	if !strings.Contains(prompt, rule) {
		t.Fatalf("expected prompt to include %q, got %q", rule, prompt)
	}
}

func testSchemaDocFamilyPageInput(t *testing.T, family string) schemadoc.PageInput {
	t.Helper()
	defs := workflowcontract.StepDefinitions()
	page := schemadoc.PageInput{Family: family}
	for _, def := range defs {
		if def.Family != family || def.Visibility != "public" {
			continue
		}
		raw, err := schemas.ToolSchema(def.SchemaFile)
		if err != nil {
			t.Fatalf("ToolSchema(%q): %v", def.SchemaFile, err)
		}
		var schema map[string]any
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("unmarshal tool schema %q: %v", def.SchemaFile, err)
		}
		properties, _ := schema["properties"].(map[string]any)
		spec, _ := properties["spec"].(map[string]any)
		if page.PageSlug == "" {
			page.PageSlug = def.DocsPage
			page.Title = def.FamilyTitle
			page.Summary = "Reference for the `" + def.FamilyTitle + "` family of typed workflow steps."
		}
		page.Variants = append(page.Variants, schemadoc.VariantInput{
			Kind:        def.Kind,
			Title:       def.FamilyTitle,
			Description: def.Summary,
			SchemaPath:  filepath.ToSlash(filepath.Join("schemas", "tools", def.SchemaFile)),
			Schema:      schema,
			Meta:        schemadoc.ToolMetaForDefinition(def),
			Spec:        spec,
			Outputs:     append([]string(nil), def.Outputs...),
			DocsOrder:   def.DocsOrder,
		})
	}
	if page.PageSlug == "" {
		t.Fatalf("missing test page for family %s", family)
	}
	return page
}

func TestAppendPlanAdvisoryPromptCarriesRecoverableReviewIntoGeneration(t *testing.T) {
	base := generationUserPrompt(askretrieve.WorkspaceSummary{}, askstate.Context{}, "create a 3-node kubeadm workflow", "", askintent.RouteDraft)
	plan := askcontract.PlanResponse{Blockers: []string{"join publication path is still underspecified"}, OpenQuestions: []string{"artifact checksum naming can be refined later"}}
	critic := askcontract.PlanCriticResponse{Advisory: []string{"final verification should stay on control-plane"}, MissingContracts: []string{"role cardinality contract"}, SuggestedFixes: []string{"publish join state explicitly before worker JoinKubeadm"}}
	prompt := appendPlanAdvisoryPrompt(base, plan, critic)
	for _, want := range []string{"Plan review carry-forward:", "generate the best viable draft", "join publication path is still underspecified", "recoverable missing contract: role cardinality contract", "plan suggested fix: publish join state explicitly before worker JoinKubeadm"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected %q in generation prompt, got %q", want, prompt)
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
	critic := semanticCritic(gen, askintent.Decision{Route: askintent.RouteRefine}, plan, plan.AuthoringBrief, askretrieve.RetrievalResult{})
	joined := strings.Join(critic.Advisory, "\n")
	if !strings.Contains(joined, "Prefer") && !strings.Contains(joined, "typed") {
		t.Fatalf("expected typed-step advisory, got %#v", critic)
	}
}

func TestSemanticCriticBlocksOfflineApplyWithDownloads(t *testing.T) {
	gen := askcontract.GenerationResponse{Files: []askcontract.GeneratedFile{{Path: "workflows/scenarios/apply.yaml", Content: "version: v1alpha1\nsteps:\n  - id: fetch\n    kind: Command\n    spec:\n      command: [\"curl\",\"-L\",\"https://example.invalid/pkg.rpm\"]\n"}}}
	plan := askcontract.PlanResponse{Request: "create a package installation workflow", OfflineAssumption: "offline"}
	critic := semanticCritic(gen, askintent.Decision{Route: askintent.RouteDraft}, plan, plan.AuthoringBrief, askretrieve.RetrievalResult{})
	if len(critic.Blocking) == 0 {
		t.Fatalf("expected offline apply blocking finding, got %#v", critic)
	}
}

func TestSemanticCriticRequiresPrepareForArtifactPlan(t *testing.T) {
	gen := askcontract.GenerationResponse{Files: []askcontract.GeneratedFile{{Path: "workflows/scenarios/apply.yaml", Content: "version: v1alpha1\nsteps:\n  - id: install\n    kind: InstallPackage\n    spec:\n      packages: [kubeadm]\n"}}}
	plan := askcontract.PlanResponse{Request: "create an air-gapped package workflow", OfflineAssumption: "offline", NeedsPrepare: true, ArtifactKinds: []string{"package"}}
	critic := semanticCritic(gen, askintent.Decision{Route: askintent.RouteDraft}, plan, plan.AuthoringBrief, askretrieve.RetrievalResult{})
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
	critic := semanticCritic(gen, askintent.Decision{Route: askintent.RouteDraft}, plan, plan.AuthoringBrief, askretrieve.RetrievalResult{})
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
	critic := semanticCritic(gen, askintent.Decision{Route: askintent.RouteDraft}, askcontract.PlanResponse{}, askcontract.AuthoringBrief{}, askretrieve.RetrievalResult{})
	joined := strings.Join(critic.Advisory, "\n")
	if !strings.Contains(joined, "workflows/vars.yaml") {
		t.Fatalf("expected vars advisory from repeated values, got %#v", critic)
	}
}

func TestSemanticCriticDetectsRepeatedStepSequenceForComponentsAdvisory(t *testing.T) {
	content := "version: v1alpha1\nsteps:\n  - id: check\n    kind: CheckHost\n    spec:\n      checks: [os]\n  - id: verify\n    kind: CheckCluster\n    spec:\n      checks: [nodes_ready]\n"
	gen := askcontract.GenerationResponse{Files: []askcontract.GeneratedFile{{Path: "workflows/prepare.yaml", Content: content}, {Path: "workflows/scenarios/apply.yaml", Content: content}}}
	critic := semanticCritic(gen, askintent.Decision{Route: askintent.RouteDraft}, askcontract.PlanResponse{}, askcontract.AuthoringBrief{}, askretrieve.RetrievalResult{})
	joined := strings.Join(critic.Advisory, "\n")
	if !strings.Contains(joined, "workflows/components/") {
		t.Fatalf("expected component advisory from repeated sequence, got %#v", critic)
	}
}

func TestSemanticCriticBlocksVarsTemplateInConstrainedLiteralField(t *testing.T) {
	gen := askcontract.GenerationResponse{Files: []askcontract.GeneratedFile{{Path: "workflows/prepare.yaml", Content: "version: v1alpha1\nsteps:\n  - id: prepare-download-kubernetes-packages\n    kind: DownloadPackage\n    spec:\n      packages: [kubeadm]\n      distro:\n        family: rhel\n        release: rocky9\n      repo:\n        type: rpm\n      backend:\n        mode: container\n        runtime: '{{ .vars.packageBackendRuntime }}'\n        image: rockylinux:9\n"}}}
	critic := semanticCritic(gen, askintent.Decision{Route: askintent.RouteDraft}, askcontract.PlanResponse{}, askcontract.AuthoringBrief{}, askretrieve.RetrievalResult{})
	joined := strings.Join(critic.Blocking, "\n")
	if !strings.Contains(joined, "spec.backend.runtime") {
		t.Fatalf("expected constrained field violation, got %#v", critic)
	}
}

func TestSemanticCriticBlocksPrepareCommandForImageCollection(t *testing.T) {
	gen := askcontract.GenerationResponse{Files: []askcontract.GeneratedFile{{Path: "workflows/prepare.yaml", Content: "version: v1alpha1\nsteps:\n  - id: pull-images\n    kind: Command\n    spec:\n      command: [\"bash\",\"-lc\",\"docker pull registry.k8s.io/kube-apiserver:v1.31.0 && docker save registry.k8s.io/kube-apiserver:v1.31.0 -o images/control-plane/apiserver.tar\"]\n"}}}
	plan := askcontract.PlanResponse{ArtifactKinds: []string{"image"}, NeedsPrepare: true}
	critic := semanticCritic(gen, askintent.Decision{Route: askintent.RouteDraft}, plan, plan.AuthoringBrief, askretrieve.RetrievalResult{})
	joined := strings.Join(critic.Blocking, "\n")
	if !strings.Contains(joined, "typed prepare step") {
		t.Fatalf("expected prepare Command artifact blocking, got %#v", critic)
	}
}

func TestSemanticCriticBlocksWhenAuthoringBriefLosesPrepareApplyScope(t *testing.T) {
	gen := askcontract.GenerationResponse{Files: []askcontract.GeneratedFile{{Path: "workflows/scenarios/apply.yaml", Content: "version: v1alpha1\nsteps:\n  - id: run\n    kind: Command\n    spec:\n      command: [\"true\"]\n"}}}
	plan := askcontract.PlanResponse{Request: "create prepare and apply workflows", AuthoringBrief: askcontract.AuthoringBrief{TargetScope: "workspace", ModeIntent: "prepare+apply", TargetPaths: []string{"workflows/prepare.yaml", "workflows/scenarios/apply.yaml"}}}
	critic := semanticCritic(gen, askintent.Decision{Route: askintent.RouteDraft}, plan, plan.AuthoringBrief, askretrieve.RetrievalResult{})
	joined := strings.Join(critic.Blocking, "\n")
	for _, want := range []string{"workflows/prepare.yaml", "prepare and apply"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %q in blocking findings, got %#v", want, critic)
		}
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
