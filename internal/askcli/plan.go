package askcli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Airgap-Castaways/deck/internal/askcontext"
	"github.com/Airgap-Castaways/deck/internal/askcontract"
	"github.com/Airgap-Castaways/deck/internal/askintent"
	"github.com/Airgap-Castaways/deck/internal/askknowledge"
	"github.com/Airgap-Castaways/deck/internal/askpolicy"
	"github.com/Airgap-Castaways/deck/internal/askprovider"
	"github.com/Airgap-Castaways/deck/internal/askretrieve"
)

func isAuthoringRoute(route askintent.Route) bool {
	return route == askintent.RouteDraft || route == askintent.RouteRefine
}

func needsComplexPlanner(prompt string, workspace askretrieve.WorkspaceSummary, decision askintent.Decision) bool {
	if !isAuthoringRoute(decision.Route) {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(prompt))
	if isGenericKubeadmStarter(lower) {
		return false
	}
	tokens := []string{"air-gapped", "airgapped", "prepare", "component", "components", "vars", "orchestration", "cluster"}
	hits := 0
	for _, token := range tokens {
		if strings.Contains(lower, token) {
			hits++
		}
	}
	if explicitClusterTopology(lower) {
		hits++
	}
	if hits >= 2 {
		return true
	}
	if workspace.HasWorkflowTree && strings.Contains(lower, "refine") {
		return true
	}
	return false
}

func isGenericKubeadmStarter(prompt string) bool {
	if !strings.Contains(prompt, "kubeadm") {
		return false
	}
	if explicitClusterTopology(prompt) {
		return false
	}
	return true
}

func explicitClusterTopology(prompt string) bool {
	tokens := []string{"multi-node", "3-node", "ha", "high-availability", "high availability", "worker", "workers", "join", "control-plane", "control plane"}
	for _, token := range tokens {
		if strings.Contains(prompt, token) {
			return true
		}
	}
	return false
}

func planSystemPrompt(decision askintent.Decision, retrieval askretrieve.RetrievalResult, prompt string, workspace askretrieve.WorkspaceSummary) string {
	requirements := askpolicy.BuildRequirementsForPrompt(prompt, retrieval, workspace, decision.Route)
	bundle := askknowledge.Current()
	b := &strings.Builder{}
	b.WriteString("You are deck ask planner. Return strict JSON only.\n")
	b.WriteString("JSON shape: {\"version\":number,\"request\":string,\"intent\":string,\"complexity\":string,\"authoringBrief\":{\"routeIntent\":string,\"targetScope\":string,\"targetPaths\":[],\"modeIntent\":string,\"connectivity\":string,\"completenessTarget\":string,\"topology\":string,\"nodeCount\":number,\"requiredCapabilities\":[]},\"executionModel\":{\"artifactContracts\":[{\"kind\":string,\"producerPath\":string,\"consumerPath\":string,\"description\":string}],\"sharedStateContracts\":[{\"name\":string,\"producerPath\":string,\"consumerPaths\":[],\"availabilityModel\":string,\"description\":string}],\"roleExecution\":{\"roleSelector\":string,\"controlPlaneFlow\":string,\"workerFlow\":string,\"perNodeInvocation\":boolean},\"verification\":{\"bootstrapPhase\":string,\"finalPhase\":string,\"expectedNodeCount\":number,\"expectedControlPlaneReady\":number},\"applyAssumptions\":[]},\"offlineAssumption\":string,\"needsPrepare\":boolean,\"artifactKinds\":[],\"varsRecommendation\":[],\"componentRecommendation\":[],\"blockers\":[],\"targetOutcome\":string,\"assumptions\":[],\"openQuestions\":[],\"entryScenario\":string,\"files\":[{\"path\":string,\"kind\":string,\"action\":string,\"purpose\":string}],\"validationChecklist\":[]}.\n")
	b.WriteString("Canonical authoringBrief values: targetScope=(workspace|scenario|vars|component), modeIntent=(prepare+apply|prepare-only|apply-only|workspace), completenessTarget=(starter|complete|refine), topology=(single-node|multi-node|ha|unspecified), requiredCapabilities should be short kebab-case strings.\n")
	b.WriteString("Canonical executionModel values: artifactContracts.kind=(package|image|repository-setup), sharedStateContracts.availabilityModel=(published-for-worker-consumption|local-only), roleExecution.roleSelector should be a short selector like vars.role.\n")
	b.WriteString(bundle.WorkflowPromptBlock())
	b.WriteString("\n")
	b.WriteString(bundle.PolicyPromptBlock())
	b.WriteString("\n")
	b.WriteString(askpolicy.RequirementsPromptBlock(requirements))
	b.WriteString("\n")
	b.WriteString("Use blockers only for missing information that should stop generation safely.\n")
	b.WriteString("Intent route: ")
	b.WriteString(string(decision.Route))
	b.WriteString("\n")
	b.WriteString("Project guide highlights:\n")
	b.WriteString("- Keep changes surgical to the requested scope.\n")
	b.WriteString("- Use blockers when the plan cannot safely continue without missing details.\n")
	b.WriteString("- Use retrieved context for topology, imports, vars guidance, and typed-step choices.\n")
	b.WriteString("Retrieved context:\n")
	b.WriteString(askretrieve.BuildChunkTextWithoutTopics(retrieval, askcontext.TopicWorkflowInvariants, askcontext.TopicPolicy, askcontext.TopicProjectPhilosophy))
	return b.String()
}

func planUserPrompt(prompt string, workspace askretrieve.WorkspaceSummary) string {
	b := &strings.Builder{}
	b.WriteString("User request:\n")
	b.WriteString(strings.TrimSpace(prompt))
	b.WriteString("\n")
	_, _ = fmt.Fprintf(b, "Workspace has workflow tree: %t\n", workspace.HasWorkflowTree)
	_, _ = fmt.Fprintf(b, "Workspace has prepare scenario: %t\n", workspace.HasPrepare)
	_, _ = fmt.Fprintf(b, "Workspace has apply scenario: %t\n", workspace.HasApply)
	b.WriteString("Workspace files:\n")
	for _, file := range workspace.Files {
		b.WriteString("- ")
		b.WriteString(file.Path)
		b.WriteString("\n")
	}
	return b.String()
}

func planWithLLM(ctx context.Context, client askprovider.Client, cfg askconfigSettings, decision askintent.Decision, retrieval askretrieve.RetrievalResult, prompt string, workspace askretrieve.WorkspaceSummary, logger askLogger) (askcontract.PlanResponse, error) {
	systemPrompt := planSystemPrompt(decision, retrieval, prompt, workspace)
	userPrompt := planUserPrompt(prompt, workspace)
	logger.prompt("plan", systemPrompt, userPrompt)
	resp, err := client.Generate(ctx, askprovider.Request{
		Kind:         "plan",
		Provider:     cfg.provider,
		Model:        cfg.model,
		APIKey:       cfg.apiKey,
		OAuthToken:   cfg.oauthToken,
		AccountID:    cfg.accountID,
		Endpoint:     cfg.endpoint,
		SystemPrompt: systemPrompt,
		Prompt:       userPrompt,
		MaxRetries:   providerRetryCount("plan"),
		Timeout:      askRequestTimeout("plan", 1, systemPrompt, userPrompt),
	})
	if err != nil {
		return askcontract.PlanResponse{}, err
	}
	logger.response("plan", resp.Content)
	planned, err := askcontract.ParsePlan(resp.Content)
	if err != nil {
		return askcontract.PlanResponse{}, err
	}
	planned = askpolicy.NormalizePlan(planned, prompt, retrieval, workspace, decision)
	if err := askpolicy.ValidatePlanStructure(planned); err != nil {
		return askcontract.PlanResponse{}, err
	}
	return planned, nil
}

func critiquePlanWithLLM(ctx context.Context, client askprovider.Client, cfg askconfigSettings, plan askcontract.PlanResponse, logger askLogger) (askcontract.PlanCriticResponse, error) {
	systemPrompt := planCriticSystemPrompt(plan.AuthoringBrief, plan)
	userPrompt := planCriticUserPrompt(plan)
	logger.prompt("plan-critic", systemPrompt, userPrompt)
	resp, err := client.Generate(ctx, askprovider.Request{
		Kind:         "plan-critic",
		Provider:     cfg.provider,
		Model:        cfg.model,
		APIKey:       cfg.apiKey,
		OAuthToken:   cfg.oauthToken,
		AccountID:    cfg.accountID,
		Endpoint:     cfg.endpoint,
		SystemPrompt: systemPrompt,
		Prompt:       userPrompt,
		MaxRetries:   providerRetryCount("plan-critic"),
		Timeout:      askRequestTimeout("plan-critic", 1, systemPrompt, userPrompt),
	})
	if err != nil {
		return askcontract.PlanCriticResponse{}, err
	}
	logger.response("plan-critic", resp.Content)
	return askcontract.ParsePlanCritic(resp.Content)
}

type askconfigSettings struct {
	provider   string
	model      string
	apiKey     string
	oauthToken string
	accountID  string
	endpoint   string
}

func renderPlanMarkdown(plan askcontract.PlanResponse, mdPath string) string {
	b := &strings.Builder{}
	b.WriteString("# deck ask plan\n\n")
	b.WriteString("## Request\n")
	b.WriteString(strings.TrimSpace(plan.Request))
	b.WriteString("\n\n## Intent\n")
	b.WriteString(strings.TrimSpace(plan.Intent))
	b.WriteString("\n\n## Target outcome\n")
	b.WriteString(strings.TrimSpace(plan.TargetOutcome))
	b.WriteString("\n\n## Assumptions\n")
	if len(plan.Assumptions) == 0 {
		b.WriteString("- None\n")
	} else {
		for _, line := range plan.Assumptions {
			b.WriteString("- ")
			b.WriteString(strings.TrimSpace(line))
			b.WriteString("\n")
		}
	}
	b.WriteString("\n## Blockers\n")
	if len(plan.Blockers) == 0 {
		b.WriteString("- None\n")
	} else {
		for _, line := range plan.Blockers {
			b.WriteString("- ")
			b.WriteString(strings.TrimSpace(line))
			b.WriteString("\n")
		}
	}
	b.WriteString("\n## Open questions\n")
	if len(plan.OpenQuestions) == 0 {
		b.WriteString("- None\n")
	} else {
		for _, line := range plan.OpenQuestions {
			b.WriteString("- ")
			b.WriteString(strings.TrimSpace(line))
			b.WriteString("\n")
		}
	}
	b.WriteString("\n## Planned files\n")
	for _, file := range plan.Files {
		b.WriteString("- ")
		b.WriteString(file.Path)
		if strings.TrimSpace(file.Action) != "" {
			b.WriteString(" (")
			b.WriteString(strings.TrimSpace(file.Action))
			b.WriteString(")")
		}
		if strings.TrimSpace(file.Purpose) != "" {
			b.WriteString(": ")
			b.WriteString(strings.TrimSpace(file.Purpose))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n## Execution model\n")
	if isExecutionModelEmpty(plan.ExecutionModel) {
		b.WriteString("- None\n")
	} else {
		for _, item := range plan.ExecutionModel.ArtifactContracts {
			b.WriteString("- artifact ")
			b.WriteString(strings.TrimSpace(item.Kind))
			b.WriteString(": ")
			b.WriteString(strings.TrimSpace(item.ProducerPath))
			b.WriteString(" -> ")
			b.WriteString(strings.TrimSpace(item.ConsumerPath))
			if strings.TrimSpace(item.Description) != "" {
				b.WriteString(" (")
				b.WriteString(strings.TrimSpace(item.Description))
				b.WriteString(")")
			}
			b.WriteString("\n")
		}
		for _, item := range plan.ExecutionModel.SharedStateContracts {
			b.WriteString("- shared state ")
			b.WriteString(strings.TrimSpace(item.Name))
			b.WriteString(": ")
			b.WriteString(strings.TrimSpace(item.ProducerPath))
			if len(item.ConsumerPaths) > 0 {
				b.WriteString(" -> ")
				b.WriteString(strings.Join(item.ConsumerPaths, ", "))
			}
			if strings.TrimSpace(item.AvailabilityModel) != "" {
				b.WriteString(" [")
				b.WriteString(strings.TrimSpace(item.AvailabilityModel))
				b.WriteString("]")
			}
			b.WriteString("\n")
		}
		if selector := strings.TrimSpace(plan.ExecutionModel.RoleExecution.RoleSelector); selector != "" {
			b.WriteString("- role selector: ")
			b.WriteString(selector)
			b.WriteString("\n")
		}
		if strings.TrimSpace(plan.ExecutionModel.RoleExecution.ControlPlaneFlow) != "" {
			b.WriteString("- control-plane flow: ")
			b.WriteString(strings.TrimSpace(plan.ExecutionModel.RoleExecution.ControlPlaneFlow))
			b.WriteString("\n")
		}
		if strings.TrimSpace(plan.ExecutionModel.RoleExecution.WorkerFlow) != "" {
			b.WriteString("- worker flow: ")
			b.WriteString(strings.TrimSpace(plan.ExecutionModel.RoleExecution.WorkerFlow))
			b.WriteString("\n")
		}
		for _, line := range plan.ExecutionModel.ApplyAssumptions {
			b.WriteString("- apply assumption: ")
			b.WriteString(strings.TrimSpace(line))
			b.WriteString("\n")
		}
	}
	b.WriteString("\n## Validation checklist\n")
	for _, line := range plan.ValidationChecklist {
		b.WriteString("- ")
		b.WriteString(strings.TrimSpace(line))
		b.WriteString("\n")
	}
	b.WriteString("\n## Next commands\n")
	b.WriteString("deck ask --from ")
	b.WriteString(mdPath)
	b.WriteString(" \"implement this plan\"\n")
	b.WriteString("deck ask --write --from ")
	b.WriteString(mdPath)
	b.WriteString(" \"implement this plan\"\n")
	return b.String()
}

func isExecutionModelEmpty(model askcontract.ExecutionModel) bool {
	return len(model.ArtifactContracts) == 0 &&
		len(model.SharedStateContracts) == 0 &&
		strings.TrimSpace(model.RoleExecution.RoleSelector) == "" &&
		len(model.ApplyAssumptions) == 0
}

func planChunk(plan askcontract.PlanResponse) askretrieve.Chunk {
	b := &strings.Builder{}
	b.WriteString("Plan intent: ")
	b.WriteString(plan.Intent)
	b.WriteString("\n")
	b.WriteString("Target outcome: ")
	b.WriteString(plan.TargetOutcome)
	b.WriteString("\n")
	b.WriteString("Planned files:\n")
	for _, file := range plan.Files {
		b.WriteString("- ")
		b.WriteString(file.Path)
		b.WriteString(" (")
		b.WriteString(file.Action)
		b.WriteString(")\n")
	}
	if selector := strings.TrimSpace(plan.ExecutionModel.RoleExecution.RoleSelector); selector != "" {
		b.WriteString("Role selector: ")
		b.WriteString(selector)
		b.WriteString("\n")
	}
	if len(plan.ExecutionModel.ArtifactContracts) > 0 {
		b.WriteString("Artifact contracts:\n")
		for _, item := range plan.ExecutionModel.ArtifactContracts {
			b.WriteString("- ")
			b.WriteString(item.Kind)
			b.WriteString(": ")
			b.WriteString(item.ProducerPath)
			b.WriteString(" -> ")
			b.WriteString(item.ConsumerPath)
			b.WriteString("\n")
		}
	}
	return askretrieve.Chunk{ID: "plan-artifact", Source: "plan", Label: "plan", Topic: askcontext.Topic("plan-artifact"), Content: b.String(), Score: 90}
}

func repoMapChunk(workspace askretrieve.WorkspaceSummary) askretrieve.Chunk {
	b := &strings.Builder{}
	b.WriteString("Workflow repo map:\n")
	type repoLine struct {
		path    string
		imports []string
		steps   []string
		mode    string
	}
	lines := make([]repoLine, 0, len(workspace.Files))
	for _, file := range workspace.Files {
		clean := filepath.ToSlash(file.Path)
		if !strings.HasPrefix(clean, "workflows/") {
			continue
		}
		lines = append(lines, repoLine{path: clean, imports: localImportPaths(file.Content), steps: localStepKinds(file.Content), mode: localWorkflowMode(clean, file.Content)})
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].path < lines[j].path })
	for _, line := range lines {
		b.WriteString("- ")
		b.WriteString(line.path)
		if line.mode != "" {
			b.WriteString(" mode=")
			b.WriteString(line.mode)
		}
		if len(line.steps) > 0 {
			b.WriteString(" steps=")
			b.WriteString(strings.Join(line.steps, ","))
		}
		if len(line.imports) > 0 {
			b.WriteString(" imports=")
			b.WriteString(strings.Join(line.imports, ","))
		}
		b.WriteString("\n")
	}
	return askretrieve.Chunk{ID: "workflow-repo-map", Source: "repo-map", Label: "repo-map", Topic: askcontext.Topic("repo-map"), Content: b.String(), Score: 60}
}

func projectContextChunk(root string) askretrieve.Chunk {
	b := &strings.Builder{}
	b.WriteString("Project context:\n")
	agentPath := filepath.Join(root, "AGENTS.md")
	if raw, err := os.ReadFile(agentPath); err == nil { //nolint:gosec // Project-local metadata file only.
		text := strings.TrimSpace(string(raw))
		if len(text) > 2000 {
			text = text[:2000]
		}
		b.WriteString("AGENTS.md excerpt:\n")
		b.WriteString(text)
		b.WriteString("\n")
	}
	b.WriteString("Authoring defaults: Prefer typed steps over Command; keep changes surgical and goal-driven.\n")
	b.WriteString("Planner defaults: use retrieved context for workspace topology, component imports, vars placement, and relevant typed steps.\n")
	return askretrieve.Chunk{ID: "project-context", Source: "project", Label: "project-context", Topic: askcontext.TopicProjectPhilosophy, Content: b.String(), Score: 70}
}

func renderPlanNotes(plan askcontract.PlanResponse) []string {
	lines := make([]string, 0, len(plan.Assumptions)+len(plan.Blockers)+len(plan.OpenQuestions))
	for _, assumption := range plan.Assumptions {
		if strings.TrimSpace(assumption) != "" {
			lines = append(lines, "assumption: "+strings.TrimSpace(assumption))
		}
	}
	for _, blocker := range plan.Blockers {
		if strings.TrimSpace(blocker) != "" {
			lines = append(lines, "blocker: "+strings.TrimSpace(blocker))
		}
	}
	for _, question := range plan.OpenQuestions {
		if strings.TrimSpace(question) != "" {
			lines = append(lines, "open question: "+strings.TrimSpace(question))
		}
	}
	return lines
}

func renderPlanCriticNotes(critic askcontract.PlanCriticResponse) []string {
	lines := make([]string, 0, len(critic.Blocking)+len(critic.Advisory)+len(critic.MissingContracts)+1)
	if strings.TrimSpace(critic.Summary) != "" {
		lines = append(lines, "plan review: "+strings.TrimSpace(critic.Summary))
	}
	for _, item := range critic.Blocking {
		if strings.TrimSpace(item) != "" {
			lines = append(lines, "plan blocking: "+strings.TrimSpace(item))
		}
	}
	for _, item := range critic.MissingContracts {
		if strings.TrimSpace(item) != "" {
			lines = append(lines, "missing contract: "+strings.TrimSpace(item))
		}
	}
	for _, item := range critic.Advisory {
		if strings.TrimSpace(item) != "" {
			lines = append(lines, "plan advisory: "+strings.TrimSpace(item))
		}
	}
	return lines
}

func planWorkspaceChunks(plan askcontract.PlanResponse, workspace askretrieve.WorkspaceSummary) []askretrieve.Chunk {
	byPath := make(map[string]askretrieve.WorkspaceFile, len(workspace.Files))
	for _, file := range workspace.Files {
		byPath[filepath.ToSlash(file.Path)] = file
	}
	chunks := make([]askretrieve.Chunk, 0)
	seen := map[string]bool{}
	for _, planned := range plan.Files {
		path := filepath.ToSlash(strings.TrimSpace(planned.Path))
		if path == "" {
			continue
		}
		if file, ok := byPath[path]; ok && !seen[path] {
			seen[path] = true
			chunks = append(chunks, askretrieve.Chunk{ID: "planned-" + strings.ReplaceAll(path, "/", "_"), Source: "plan-workspace", Label: path, Topic: askcontext.Topic("workspace:" + path), Content: file.Content, Score: 95})
		}
		if strings.HasPrefix(path, "workflows/scenarios/") {
			file, ok := byPath[path]
			if !ok {
				continue
			}
			for _, importPath := range localImportPaths(file.Content) {
				resolved := filepath.ToSlash(filepath.Join("workflows/components", importPath))
				if component, exists := byPath[resolved]; exists && !seen[resolved] {
					seen[resolved] = true
					chunks = append(chunks, askretrieve.Chunk{ID: "planned-import-" + strings.ReplaceAll(resolved, "/", "_"), Source: "plan-workspace", Label: resolved, Topic: askcontext.Topic("workspace:" + resolved), Content: component.Content, Score: 92})
				}
			}
		}
	}
	return chunks
}

func planTarget(plan askcontract.PlanResponse, fallback askintent.Target) askintent.Target {
	for _, file := range plan.Files {
		path := filepath.ToSlash(strings.TrimSpace(file.Path))
		if strings.HasPrefix(path, "workflows/scenarios/") {
			name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			return askintent.Target{Kind: "scenario", Path: path, Name: name}
		}
	}
	return fallback
}

func localImportPaths(content string) []string {
	parsed := parseWorkflowOutline(content)
	paths := make([]string, 0)
	for _, phase := range parsed.Phases {
		for _, imp := range phase.Imports {
			if strings.TrimSpace(imp.Path) != "" {
				paths = append(paths, filepath.ToSlash(strings.TrimSpace(imp.Path)))
			}
		}
	}
	return dedupe(paths)
}

func localStepKinds(content string) []string {
	parsed := parseWorkflowOutline(content)
	kinds := make([]string, 0)
	for _, step := range parsed.Steps {
		if strings.TrimSpace(step.Kind) != "" {
			kinds = append(kinds, strings.TrimSpace(step.Kind))
		}
	}
	for _, phase := range parsed.Phases {
		for _, step := range phase.Steps {
			if strings.TrimSpace(step.Kind) != "" {
				kinds = append(kinds, strings.TrimSpace(step.Kind))
			}
		}
	}
	return dedupe(kinds)
}

func localWorkflowMode(path string, content string) string {
	clean := filepath.ToSlash(strings.TrimSpace(path))
	if filepath.Base(clean) == "prepare.yaml" {
		return "prepare"
	}
	if strings.Contains(clean, "/workflows/scenarios/") || strings.HasPrefix(clean, "workflows/scenarios/") {
		return "apply"
	}
	return ""
}

type workflowOutline struct {
	Steps  []outlineStep  `yaml:"steps"`
	Phases []outlinePhase `yaml:"phases"`
}

type outlinePhase struct {
	Imports []outlineImport `yaml:"imports"`
	Steps   []outlineStep   `yaml:"steps"`
}

type outlineImport struct {
	Path string `yaml:"path"`
}

type outlineStep struct {
	Kind string `yaml:"kind"`
}

func parseWorkflowOutline(content string) workflowOutline {
	var parsed workflowOutline
	if err := yaml.Unmarshal([]byte(content), &parsed); err != nil {
		return workflowOutline{}
	}
	return parsed
}
