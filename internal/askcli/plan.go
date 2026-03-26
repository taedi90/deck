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
	tokens := []string{"air-gapped", "airgapped", "multi-node", "3-node", "prepare", "component", "components", "vars", "orchestration", "cluster"}
	hits := 0
	for _, token := range tokens {
		if strings.Contains(lower, token) {
			hits++
		}
	}
	if hits >= 2 {
		return true
	}
	if workspace.HasWorkflowTree && strings.Contains(lower, "refine") {
		return true
	}
	return false
}

func planSystemPrompt(decision askintent.Decision, retrieval askretrieve.RetrievalResult, prompt string, workspace askretrieve.WorkspaceSummary) string {
	requirements := askpolicy.BuildRequirementsForPrompt(prompt, retrieval, workspace, decision.Route)
	bundle := askknowledge.Current()
	b := &strings.Builder{}
	b.WriteString("You are deck ask planner. Return strict JSON only.\n")
	b.WriteString("JSON shape: {\"version\":number,\"request\":string,\"intent\":string,\"complexity\":string,\"offlineAssumption\":string,\"needsPrepare\":boolean,\"artifactKinds\":[]string,\"varsRecommendation\":[]string,\"componentRecommendation\":[]string,\"blockers\":[]string,\"targetOutcome\":string,\"assumptions\":[]string,\"openQuestions\":[]string,\"entryScenario\":string,\"files\":[{\"path\":string,\"kind\":string,\"action\":string,\"purpose\":string}],\"validationChecklist\":[]string}.\n")
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
		MaxRetries:   1,
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

type askconfigSettings struct {
	provider   string
	model      string
	apiKey     string
	oauthToken string
	accountID  string
	endpoint   string
}

func hasBlockingPlanItems(plan askcontract.PlanResponse) bool {
	for _, blocker := range plan.Blockers {
		if strings.TrimSpace(blocker) != "" {
			return true
		}
	}
	for _, q := range plan.OpenQuestions {
		lower := strings.ToLower(strings.TrimSpace(q))
		if strings.HasPrefix(lower, "blocking:") {
			return true
		}
	}
	return false
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
