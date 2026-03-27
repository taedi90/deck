package askcli

import (
	"context"
	"fmt"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/askcontract"
	"github.com/Airgap-Castaways/deck/internal/askintent"
	"github.com/Airgap-Castaways/deck/internal/askprovider"
	"github.com/Airgap-Castaways/deck/internal/askretrieve"
)

type postProcessSummary struct {
	Applied     bool
	Generation  askcontract.GenerationResponse
	LintSummary string
	Critic      askcontract.CriticResponse
	Judge       askcontract.JudgeResponse
	Notes       []string
}

func maybePostProcessGeneration(ctx context.Context, client askprovider.Client, req askprovider.Request, root string, logger askLogger, decision askintent.Decision, plan askcontract.PlanResponse, brief askcontract.AuthoringBrief, retrieval askretrieve.RetrievalResult, gen askcontract.GenerationResponse, lintSummary string, critic askcontract.CriticResponse, judge askcontract.JudgeResponse, planCritic askcontract.PlanCriticResponse) (postProcessSummary, error) {
	if !shouldAutoPostProcess(brief, judge, gen) {
		return postProcessSummary{}, fmt.Errorf("post-process not needed")
	}
	findings, err := critiquePostProcess(ctx, client, req, plan, brief, gen, judge, critic, planCritic, logger)
	if err != nil {
		return postProcessSummary{}, err
	}
	notes := renderPostProcessNotes(findings)
	if len(findings.Blocking) == 0 {
		if shouldAttemptStructuralCleanup(findings, gen) {
			edited, err := applyStructuralCleanupEdit(ctx, client, req, plan, brief, findings, gen, logger)
			if err != nil {
				return postProcessSummary{}, err
			}
			newLint, newCritic, err := validateGeneration(ctx, root, edited, decision, plan, brief, retrieval)
			if err != nil {
				return postProcessSummary{}, err
			}
			newJudge, err := maybeJudgeGeneration(ctx, client, req, edited, newLint, newCritic, plan, brief, logger)
			if err != nil {
				logger.logf("debug", "[ask][phase:postprocess-structural:judge-skip] error=%v\n", err)
				newJudge = judge
			}
			return postProcessSummary{Applied: true, Generation: edited, LintSummary: newLint, Critic: newCritic, Judge: newJudge, Notes: append([]string{"post-process: applied optional structural cleanup"}, notes...)}, nil
		}
		return postProcessSummary{Applied: false, Notes: notes}, nil
	}
	edited, err := applyPostProcessEdit(ctx, client, req, plan, brief, findings, gen, planCritic, logger)
	if err != nil {
		return postProcessSummary{}, err
	}
	newLint, newCritic, err := validateGeneration(ctx, root, edited, decision, plan, brief, retrieval)
	if err != nil {
		return postProcessSummary{}, err
	}
	newJudge, err := maybeJudgeGeneration(ctx, client, req, edited, newLint, newCritic, plan, brief, logger)
	if err != nil {
		logger.logf("debug", "[ask][phase:postprocess:judge-skip] error=%v\n", err)
		newJudge = judge
	}
	return postProcessSummary{Applied: true, Generation: edited, LintSummary: newLint, Critic: newCritic, Judge: newJudge, Notes: append([]string{"post-process: applied targeted operational refinement"}, notes...)}, nil
}

func shouldAutoPostProcess(brief askcontract.AuthoringBrief, judge askcontract.JudgeResponse, gen askcontract.GenerationResponse) bool {
	if len(gen.Files) < 2 {
		return false
	}
	if strings.TrimSpace(brief.CompletenessTarget) != "complete" {
		return false
	}
	if strings.TrimSpace(brief.ModeIntent) != "prepare+apply" {
		return false
	}
	topology := strings.TrimSpace(brief.Topology)
	if topology != "multi-node" && topology != "ha" {
		return false
	}
	text := strings.ToLower(strings.Join(append(append([]string{}, judge.Advisory...), judge.Blocking...), " "))
	for _, token := range []string{"worker", "verification", "join", "artifact", "handoff", "kubeconfig", "runtime", "publish"} {
		if strings.Contains(text, token) {
			return true
		}
	}
	return false
}

func critiquePostProcess(ctx context.Context, client askprovider.Client, req askprovider.Request, plan askcontract.PlanResponse, brief askcontract.AuthoringBrief, gen askcontract.GenerationResponse, judge askcontract.JudgeResponse, critic askcontract.CriticResponse, planCritic askcontract.PlanCriticResponse, logger askLogger) (askcontract.PostProcessResponse, error) {
	systemPrompt := postProcessCriticSystemPrompt(brief, plan)
	userPrompt := postProcessCriticUserPrompt(plan, gen, judge, critic, planCritic)
	logger.prompt("postprocess-critic", systemPrompt, userPrompt)
	resp, err := client.Generate(ctx, askprovider.Request{Kind: "postprocess-critic", Provider: req.Provider, Model: req.Model, APIKey: req.APIKey, OAuthToken: req.OAuthToken, AccountID: req.AccountID, Endpoint: req.Endpoint, SystemPrompt: systemPrompt, Prompt: userPrompt, MaxRetries: providerRetryCount("postprocess-critic"), Timeout: askRequestTimeout("postprocess-critic", 1, systemPrompt, userPrompt)})
	if err != nil {
		return askcontract.PostProcessResponse{}, err
	}
	logger.response("postprocess-critic", resp.Content)
	parsed, err := askcontract.ParsePostProcess(resp.Content)
	if err != nil {
		return askcontract.PostProcessResponse{}, err
	}
	return enrichPostProcessFindings(parsed, gen), nil
}

func applyPostProcessEdit(ctx context.Context, client askprovider.Client, req askprovider.Request, plan askcontract.PlanResponse, brief askcontract.AuthoringBrief, findings askcontract.PostProcessResponse, gen askcontract.GenerationResponse, planCritic askcontract.PlanCriticResponse, logger askLogger) (askcontract.GenerationResponse, error) {
	systemPrompt := postProcessEditSystemPrompt(brief, plan)
	userPrompt := postProcessEditUserPrompt(gen, findings, planCritic)
	logger.prompt("postprocess-edit", systemPrompt, userPrompt)
	resp, err := client.Generate(ctx, askprovider.Request{Kind: "postprocess-edit", Provider: req.Provider, Model: req.Model, APIKey: req.APIKey, OAuthToken: req.OAuthToken, AccountID: req.AccountID, Endpoint: req.Endpoint, SystemPrompt: systemPrompt, Prompt: userPrompt, MaxRetries: providerRetryCount("postprocess-edit"), Timeout: askRequestTimeout("postprocess-edit", 1, systemPrompt, userPrompt)})
	if err != nil {
		return askcontract.GenerationResponse{}, err
	}
	logger.response("postprocess-edit", resp.Content)
	return askcontract.ParseGeneration(resp.Content)
}

func renderPostProcessNotes(findings askcontract.PostProcessResponse) []string {
	lines := []string{}
	if strings.TrimSpace(findings.Summary) != "" {
		lines = append(lines, "post-process review: "+strings.TrimSpace(findings.Summary))
	}
	for _, item := range findings.Advisory {
		if strings.TrimSpace(item) != "" {
			lines = append(lines, "post-process advisory: "+strings.TrimSpace(item))
		}
	}
	for _, item := range findings.UpgradeCandidates {
		if strings.TrimSpace(item) != "" {
			lines = append(lines, "post-process candidate: "+strings.TrimSpace(item))
		}
	}
	for _, item := range findings.RequiredEdits {
		if strings.TrimSpace(item) != "" {
			lines = append(lines, "post-process required edit: "+strings.TrimSpace(item))
		}
	}
	return lines
}

func shouldAttemptStructuralCleanup(findings askcontract.PostProcessResponse, gen askcontract.GenerationResponse) bool {
	if len(findings.Blocking) > 0 {
		return false
	}
	if !hasStrongStructuralCleanupCandidate(findings.UpgradeCandidates) {
		return false
	}
	return repeatedPathOrVersionLiterals(gen) || repeatedLargeRuntimeBlocks(gen)
}

func hasStrongStructuralCleanupCandidate(candidates []string) bool {
	for _, item := range candidates {
		switch strings.TrimSpace(item) {
		case "extract-vars", "extract-component":
			return true
		}
	}
	return false
}

func enrichPostProcessFindings(findings askcontract.PostProcessResponse, gen askcontract.GenerationResponse) askcontract.PostProcessResponse {
	files := filePathSet(gen.Files)
	if len(findings.ReviseFiles) == 0 && len(findings.Blocking) > 0 {
		if files["workflows/scenarios/apply.yaml"] {
			findings.ReviseFiles = append(findings.ReviseFiles, "workflows/scenarios/apply.yaml")
		}
	}
	for path := range files {
		if !containsTrimmed(findings.ReviseFiles, path) && !containsTrimmed(findings.PreserveFiles, path) {
			findings.PreserveFiles = append(findings.PreserveFiles, path)
		}
	}
	advisory, candidates := localStructuralCleanupHeuristics(gen)
	findings.Advisory = dedupe(append(findings.Advisory, advisory...))
	findings.UpgradeCandidates = dedupe(append(findings.UpgradeCandidates, candidates...))
	if len(findings.UpgradeCandidates) == 0 {
		findings.UpgradeCandidates = []string{"preserve-inline"}
	}
	if len(findings.Blocking) == 0 {
		findings.ReviseFiles = nil
	}
	findings.PreserveFiles = dedupe(findings.PreserveFiles)
	findings.ReviseFiles = dedupe(findings.ReviseFiles)
	return findings
}

func localStructuralCleanupHeuristics(gen askcontract.GenerationResponse) ([]string, []string) {
	advisory := []string{}
	candidates := []string{"preserve-inline"}
	if repeatedPathOrVersionLiterals(gen) {
		advisory = append(advisory, "extract-vars is optional because repeated path/version literals appear more than once, but preserve inline structure if readability is already good")
		candidates = append(candidates, "extract-vars")
	}
	if repeatedLargeRuntimeBlocks(gen) {
		advisory = append(advisory, "extract-component is optional because repeated runtime/setup step groups appear large enough to consider reuse")
		candidates = append(candidates, "extract-component")
	}
	return advisory, candidates
}

func repeatedPathOrVersionLiterals(gen askcontract.GenerationResponse) bool {
	counts := map[string]int{}
	for _, file := range gen.Files {
		for _, line := range strings.Split(file.Content, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(strings.ToLower(trimmed), "version:") {
				continue
			}
			value := trimmed
			if parts := strings.SplitN(trimmed, ":", 2); len(parts) == 2 {
				value = strings.TrimSpace(parts[1])
			}
			lowerValue := strings.ToLower(value)
			if !strings.Contains(value, "/") && !strings.Contains(lowerValue, "registry") && !strings.Contains(lowerValue, "v1.") {
				continue
			}
			if strings.Contains(value, "{{") {
				continue
			}
			counts[value]++
			if counts[value] >= 2 {
				return true
			}
		}
	}
	return false
}

func repeatedLargeRuntimeBlocks(gen askcontract.GenerationResponse) bool {
	stepSets := map[string]int{}
	for _, file := range gen.Files {
		content := strings.ToLower(file.Content)
		marker := 0
		for _, token := range []string{"kind: installpackage", "kind: loadimage", "kind: manageservice", "kind: writefile"} {
			if strings.Contains(content, token) {
				marker++
			}
		}
		if marker >= 3 {
			key := fmt.Sprintf("%d-runtime-block", marker)
			stepSets[key]++
			if stepSets[key] >= 2 {
				return true
			}
		}
	}
	return false
}

func applyStructuralCleanupEdit(ctx context.Context, client askprovider.Client, req askprovider.Request, plan askcontract.PlanResponse, brief askcontract.AuthoringBrief, findings askcontract.PostProcessResponse, gen askcontract.GenerationResponse, logger askLogger) (askcontract.GenerationResponse, error) {
	systemPrompt := structuralCleanupEditSystemPrompt(brief, plan)
	userPrompt := structuralCleanupEditUserPrompt(gen, findings)
	logger.prompt("postprocess-structural", systemPrompt, userPrompt)
	resp, err := client.Generate(ctx, askprovider.Request{Kind: "postprocess-structural", Provider: req.Provider, Model: req.Model, APIKey: req.APIKey, OAuthToken: req.OAuthToken, AccountID: req.AccountID, Endpoint: req.Endpoint, SystemPrompt: systemPrompt, Prompt: userPrompt, MaxRetries: providerRetryCount("postprocess-structural"), Timeout: askRequestTimeout("postprocess-structural", 1, systemPrompt, userPrompt)})
	if err != nil {
		return askcontract.GenerationResponse{}, err
	}
	logger.response("postprocess-structural", resp.Content)
	return askcontract.ParseGeneration(resp.Content)
}

func filePathSet(files []askcontract.GeneratedFile) map[string]bool {
	out := map[string]bool{}
	for _, file := range files {
		path := strings.TrimSpace(file.Path)
		if path != "" {
			out[path] = true
		}
	}
	return out
}

func containsTrimmed(values []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, value := range values {
		if strings.TrimSpace(value) == want {
			return true
		}
	}
	return false
}
