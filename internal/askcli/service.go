package askcli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	lspaugment "github.com/Airgap-Castaways/deck/internal/askaugment/lsp"
	mcpaugment "github.com/Airgap-Castaways/deck/internal/askaugment/mcp"
	"github.com/Airgap-Castaways/deck/internal/askconfig"
	"github.com/Airgap-Castaways/deck/internal/askcontext"
	"github.com/Airgap-Castaways/deck/internal/askcontract"
	"github.com/Airgap-Castaways/deck/internal/askdiagnostic"
	"github.com/Airgap-Castaways/deck/internal/askhooks"
	"github.com/Airgap-Castaways/deck/internal/askintent"
	"github.com/Airgap-Castaways/deck/internal/askknowledge"
	"github.com/Airgap-Castaways/deck/internal/askpolicy"
	"github.com/Airgap-Castaways/deck/internal/askprovider"
	"github.com/Airgap-Castaways/deck/internal/askretrieve"
	"github.com/Airgap-Castaways/deck/internal/askreview"
	"github.com/Airgap-Castaways/deck/internal/askscaffold"
	"github.com/Airgap-Castaways/deck/internal/askstate"
	"github.com/Airgap-Castaways/deck/internal/validate"
	"github.com/Airgap-Castaways/deck/internal/workflowissues"
)

func Execute(ctx context.Context, opts Options, client askprovider.Client) error {
	if client == nil {
		return fmt.Errorf("ask backend is not configured")
	}
	hooks := askhooks.Default()
	resolvedRoot, err := filepath.Abs(strings.TrimSpace(opts.Root))
	if err != nil {
		return fmt.Errorf("resolve workspace root: %w", err)
	}
	requestText, requestSource, err := loadRequestText(resolvedRoot, strings.TrimSpace(opts.Prompt), strings.TrimSpace(opts.FromPath))
	if err != nil {
		return err
	}
	requestText = strings.TrimSpace(hooks.PreClassify(requestText))
	if requestText == "" && !opts.Review {
		return fmt.Errorf("ask request is required")
	}
	state, err := askstate.Load(resolvedRoot)
	if err != nil {
		return err
	}
	workspace, err := askretrieve.InspectWorkspace(resolvedRoot)
	if err != nil {
		return err
	}
	heuristic := hooks.PostClassify(askintent.Classify(askintent.Input{
		Prompt:          requestText,
		WriteFlag:       opts.Write,
		ReviewFlag:      opts.Review,
		HasWorkflowTree: workspace.HasWorkflowTree,
		HasPrepare:      workspace.HasPrepare,
		HasApply:        workspace.HasApply,
	}))
	effective, err := askconfig.ResolveEffective(askconfig.Settings{Provider: opts.Provider, Model: opts.Model, Endpoint: opts.Endpoint})
	if err != nil {
		return err
	}
	if effective.OAuthTokenSource == "session" || effective.OAuthTokenSource == "session-expired" {
		session, source, status, err := askconfig.ResolveRuntimeSession(effective.Provider)
		if err != nil {
			return err
		}
		if strings.TrimSpace(session.AccessToken) != "" {
			effective.OAuthToken = session.AccessToken
			effective.OAuthTokenSource = source
			effective.AuthStatus = status
			effective.AccountID = session.AccountID
		}
	}
	logger := newAskLogger(opts.Stderr, effective.LogLevel)
	logger.logf("basic", "\n[ask][phase:request] routeCandidate=%s write=%t review=%t\n", heuristic.Route, opts.Write, opts.Review)
	logger.logf("basic", "[ask][config] provider=%s model=%s endpoint=%s apiKeySource=%s oauthTokenSource=%s accountID=%t logLevel=%s\n", effective.Provider, effective.Model, effective.Endpoint, effective.APIKeySource, effective.OAuthTokenSource, strings.TrimSpace(effective.AccountID) != "", effective.LogLevel)
	logger.logf("debug", "[ask][command] %s\n", renderUserCommand(opts))
	if requestSource != "" {
		logger.logf("debug", "[ask][request-source] type=%s from=%s\n", requestSource, strings.TrimSpace(opts.FromPath))
	}
	logger.logf("trace", "\n[ask][request]\n%s\n", strings.TrimSpace(requestText))

	decision := heuristic
	classifierLLM := false
	classifierSystem := classifierSystemPrompt()
	classifierUser := classifierUserPrompt(requestText, opts.Review, workspace)
	switch {
	case canUseLLM(effective) && (!isAuthoringRoute(heuristic.Route) || askFeatureEnabled("DECK_ASK_ENABLE_LLM_CLASSIFIER")):
		logger.logf("debug", "\n[ask][phase:classify:start] provider=%s model=%s\n", effective.Provider, effective.Model)
		classified, classifyErr := classifyWithLLM(ctx, client, effective, classifierSystem, classifierUser, logger)
		if classifyErr == nil {
			decision = classified
			classifierLLM = true
			logger.logf("basic", "[ask][phase:classify:done] route=%s confidence=%.2f reason=%s\n", decision.Route, decision.Confidence, decision.Reason)
		} else {
			logger.logf("debug", "[ask][phase:classify:fallback] error=%v\n", classifyErr)
		}
	case canUseLLM(effective):
		logger.logf("debug", "[ask][phase:classify:skip] reason=heuristic-authoring-default\n")
	default:
		logger.logf("debug", "[ask][phase:classify:skip] reason=no-llm-credentials\n")
	}
	decision = applyWriteOverride(decision, heuristic, opts.Write, logger)
	if decision.Route == askintent.RouteRefine && !workspace.HasWorkflowTree {
		return fmt.Errorf("cannot refine workflow files because this workspace has no workflow tree yet; run a draft generation first")
	}

	mcpChunks := []askretrieve.Chunk{}
	mcpEvents := []string{"mcp: disabled for default local pipeline"}
	lspChunks := []askretrieve.Chunk{}
	lspEvents := []string{"lsp: disabled for default local pipeline"}
	if !isAuthoringRoute(decision.Route) || askFeatureEnabled("DECK_ASK_ENABLE_AUGMENT") {
		mcpChunks, mcpEvents = mcpaugment.Gather(ctx, effective.MCP, decision.Route, requestText)
		lspChunks, lspEvents = lspaugment.Gather(ctx, effective.LSP, decision.Target, workspace)
	}
	externalChunks := append(append([]askretrieve.Chunk{}, mcpChunks...), lspChunks...)
	externalChunks = append(externalChunks, projectContextChunk(resolvedRoot))
	retrieval := askretrieve.Retrieve(decision.Route, requestText, decision.Target, workspace, state, externalChunks)
	requirements := askpolicy.BuildScenarioRequirements(requestText, retrieval, workspace, decision)
	authoringBrief := askpolicy.BriefFromRequirements(requirements, decision)
	bundle := askknowledge.Current()
	result := runResult{
		Route:         decision.Route,
		Target:        decision.Target,
		Confidence:    decision.Confidence,
		Reason:        decision.Reason,
		RetriesUsed:   0,
		Chunks:        retrieval.Chunks,
		DroppedChunks: retrieval.Dropped,
		ConfigSource:  effective,
		ClassifierLLM: classifierLLM,
		AugmentEvents: append(mcpEvents, lspEvents...),
		UserCommand:   renderUserCommand(opts),
	}
	if canUseLLM(effective) {
		result.PromptTraces = append(result.PromptTraces, promptTrace{Label: "classifier", SystemPrompt: classifierSystem, UserPrompt: classifierUser})
	}

	logger.logf("debug", "\n[ask][phase:augment:start] mcp=%t lsp=%t\n", effective.MCP.Enabled, effective.LSP.Enabled)
	for _, event := range result.AugmentEvents {
		prefix := "augment"
		if strings.HasPrefix(event, "mcp:") {
			prefix = "mcp"
		} else if strings.HasPrefix(event, "lsp") {
			prefix = "lsp"
		}
		logger.logf("debug", "[ask][augment:%s] %s\n", prefix, event)
	}
	logger.logf("debug", "[ask][phase:retrieve] chunks=%d dropped=%d\n", len(result.Chunks), len(result.DroppedChunks))

	if decision.LLMPolicy == askintent.LLMRequired && !canUseLLM(effective) {
		return fmt.Errorf("missing ask credentials for provider %q; set %s, %s, or run `deck ask config set --api-key ...` / `deck ask config set --oauth-token ...`", effective.Provider, "DECK_ASK_API_KEY", "DECK_ASK_OAUTH_TOKEN")
	}
	if opts.PlanOnly && !isAuthoringRoute(decision.Route) {
		return fmt.Errorf("ask plan is intended for draft/refine authoring requests; got route %s. Try `deck ask %q` instead", decision.Route, strings.TrimSpace(requestText))
	}

	planRequested := opts.PlanOnly
	planNeeded := isAuthoringRoute(decision.Route) && planRequested
	var plan askcontract.PlanResponse
	var planCritic askcontract.PlanCriticResponse
	if planNeeded {
		if !canUseLLM(effective) {
			return fmt.Errorf("route %s requires model access; configure provider credentials first", decision.Route)
		}
		logger.logf("basic", "\n[ask][phase:plan:start] route=%s\n", decision.Route)
		cfg := askconfigSettings{provider: effective.Provider, model: effective.Model, apiKey: effective.APIKey, oauthToken: effective.OAuthToken, accountID: effective.AccountID, endpoint: effective.Endpoint}
		planned, reviewedCritic, usedFallback, planErr := buildPlanWithReview(ctx, client, cfg, decision, retrieval, requestText, workspace, requirements, logger)
		planCritic = reviewedCritic
		if planErr != nil {
			return planErr
		}
		if usedFallback {
			logger.logf("debug", "[ask][phase:plan:fallback] using defaults after planner failure\n")
		}
		plan = planned
		result.Plan = &plan
		if planCritic.Summary != "" || len(planCritic.Blocking) > 0 || len(planCritic.Advisory) > 0 || len(planCritic.MissingContracts) > 0 || len(planCritic.SuggestedFixes) > 0 {
			result.PlanCritic = &planCritic
		}
		logger.logf("basic", "[ask][phase:plan:done] files=%d blockers=%d\n", len(plan.Files), len(plan.Blockers))
		planMD := renderPlanMarkdown(plan, ".deck/plan/latest.md")
		planMDPath, planJSONPath, saveErr := savePlanArtifact(resolvedRoot, opts, plan, planMD)
		if saveErr != nil {
			return saveErr
		}
		logger.logf("basic", "[ask][phase:plan:save] markdown=%s json=%s\n", planMDPath, planJSONPath)
		result.PlanMarkdown = planMDPath
		result.PlanJSON = planJSONPath
		planMarkdownFinal := renderPlanMarkdown(plan, planMDPath)
		if updateErr := os.WriteFile(filepath.Join(resolvedRoot, filepath.FromSlash(planMDPath)), []byte(planMarkdownFinal+"\n"), 0o600); updateErr == nil {
			_ = os.WriteFile(filepath.Join(filepath.Dir(filepath.Join(resolvedRoot, filepath.FromSlash(planMDPath))), "latest.md"), []byte(planMarkdownFinal+"\n"), 0o600)
		}
		if opts.PlanOnly {
			result.Summary = "generated plan artifact"
			result.Termination = "plan-only-requested"
			result.FallbackNote = "plan requested"
			result.ReviewLines = append(result.ReviewLines, renderPlanNotes(plan)...)
			result.ReviewLines = append(result.ReviewLines, renderPlanCriticNotes(planCritic)...)
			if err := askstate.Save(resolvedRoot, askstate.Context{
				LastMode:          "plan",
				LastRoute:         string(result.Route),
				LastPrompt:        strings.TrimSpace(requestText),
				LastFiles:         filePathsFromPlan(plan),
				LastLLMUsed:       true,
				LastClassifierLLM: result.ClassifierLLM,
				LastTermination:   result.Termination,
			}, requestText, resultToMarkdown(result)); err != nil {
				return err
			}
			return render(opts.Stdout, opts.Stderr, result)
		}
		if hasFatalPlanReviewIssues(plan, planCritic) {
			result.Summary = "plan generated with review blockers"
			result.Termination = "plan-only-review-blocked"
			result.FallbackNote = "generation stopped because plan review found blocking issues"
			result.ReviewLines = append(result.ReviewLines, renderPlanNotes(plan)...)
			result.ReviewLines = append(result.ReviewLines, renderPlanCriticNotes(planCritic)...)
			if err := askstate.Save(resolvedRoot, askstate.Context{
				LastMode:          "plan",
				LastRoute:         string(result.Route),
				LastPrompt:        strings.TrimSpace(requestText),
				LastFiles:         filePathsFromPlan(plan),
				LastLLMUsed:       true,
				LastClassifierLLM: result.ClassifierLLM,
				LastTermination:   result.Termination,
			}, requestText, resultToMarkdown(result)); err != nil {
				return err
			}
			return render(opts.Stdout, opts.Stderr, result)
		}
		secondPassExternal := append([]askretrieve.Chunk{}, externalChunks...)
		secondPassExternal = append(secondPassExternal, repoMapChunk(workspace), planChunk(plan))
		secondPassExternal = append(secondPassExternal, planWorkspaceChunks(plan, workspace)...)
		decision.Target = planTarget(plan, decision.Target)
		retrieval = askretrieve.Retrieve(decision.Route, requestText, decision.Target, workspace, state, secondPassExternal)
		requirements = askpolicy.MergeRequirementsWithPlan(askpolicy.BuildScenarioRequirements(requestText, retrieval, workspace, decision), plan)
		authoringBrief = plan.AuthoringBrief
		if strings.TrimSpace(authoringBrief.RouteIntent) == "" {
			authoringBrief = askpolicy.BriefFromRequirements(requirements, decision)
		}
		result.Chunks = retrieval.Chunks
		result.DroppedChunks = retrieval.Dropped
		logger.logf("debug", "[ask][phase:retrieve:second-pass] chunks=%d dropped=%d\n", len(result.Chunks), len(result.DroppedChunks))
	}

	switch decision.Route {
	case askintent.RouteDraft, askintent.RouteRefine:
		if !canUseLLM(effective) {
			return fmt.Errorf("route %s requires model access; configure provider credentials first", decision.Route)
		}
		attempts := generationAttempts(opts.MaxIterations, decision, requestText)
		scaffold := askscaffold.Build(requirements, workspace, decision, plan, bundle)
		executionModel := plan.ExecutionModel
		if len(executionModel.ArtifactContracts) == 0 && len(executionModel.SharedStateContracts) == 0 && strings.TrimSpace(executionModel.RoleExecution.RoleSelector) == "" && len(executionModel.ApplyAssumptions) == 0 {
			executionModel = askpolicy.ExecutionModelFromRequirements(requirements)
		}
		generationPrompt := generationUserPrompt(workspace, state, requestText, strings.TrimSpace(opts.FromPath), decision.Route)
		generationPrompt = appendPlanAdvisoryPrompt(generationPrompt, plan, planCritic)
		generationKind := "generate-fast"
		if askFeatureEnabled("DECK_ASK_ENABLE_JUDGE") {
			generationKind = "generate"
		}
		generationRequest := askprovider.Request{
			Kind:         generationKind,
			Provider:     effective.Provider,
			Model:        effective.Model,
			APIKey:       effective.APIKey,
			OAuthToken:   effective.OAuthToken,
			AccountID:    effective.AccountID,
			Endpoint:     effective.Endpoint,
			SystemPrompt: generationSystemPrompt(decision.Route, decision.Target, requestText, retrieval, requirements, authoringBrief, executionModel, scaffold),
			Prompt:       generationPrompt,
			MaxRetries:   providerRetryCount("generate"),
			Timeout:      askRequestTimeout("generate", attempts, generationPrompt, generationPrompt),
		}
		result.PromptTraces = append(result.PromptTraces, promptTrace{Label: "generation", SystemPrompt: generationRequest.SystemPrompt, UserPrompt: generationRequest.Prompt})
		logger.logf("basic", "\n[ask][phase:generation:start] route=%s attempts=%d\n", decision.Route, attempts)
		gen, lintSummary, critic, judge, retriesUsed, genErr := generateWithValidation(ctx, client, generationRequest, resolvedRoot, attempts, logger, decision, plan, authoringBrief, retrieval, planCritic)
		if genErr != nil {
			return genErr
		}
		if askFeatureEnabled("DECK_ASK_ENABLE_POSTPROCESS") {
			postSummary, postErr := maybePostProcessGeneration(ctx, client, generationRequest, resolvedRoot, logger, decision, plan, authoringBrief, retrieval, gen, lintSummary, critic, judge, planCritic)
			switch {
			case postErr != nil:
				logger.logf("debug", "[ask][phase:postprocess:skip] error=%v\n", postErr)
			case postSummary.Applied:
				gen = postSummary.Generation
				lintSummary = postSummary.LintSummary
				critic = postSummary.Critic
				judge = postSummary.Judge
				result.ReviewLines = append(result.ReviewLines, postSummary.Notes...)
			case len(postSummary.Notes) > 0:
				result.ReviewLines = append(result.ReviewLines, postSummary.Notes...)
			}
		}
		logger.logf("basic", "[ask][phase:generation:done] files=%d lint=%s\n", len(gen.Files), lintSummary)
		result.LLMUsed = true
		result.RetriesUsed = retriesUsed
		result.Files = gen.Files
		result.Summary = gen.Summary
		result.ReviewLines = append(result.ReviewLines, gen.Review...)
		result.ReviewLines = append(result.ReviewLines, renderPlanCriticNotes(planCritic)...)
		result.LintSummary = lintSummary
		result.LocalFindings = localFindings(result.Files)
		result.Critic = &critic
		if judge.Summary != "" || len(judge.Blocking) > 0 || len(judge.Advisory) > 0 || len(judge.MissingCapabilities) > 0 || len(judge.SuggestedFixes) > 0 {
			result.Judge = &judge
		}
		result.ReviewLines = append(result.ReviewLines, critic.Advisory...)
		if opts.Write {
			if err := writeFiles(resolvedRoot, result.Files); err != nil {
				return err
			}
			result.WroteFiles = true
		}
		if retriesUsed > 0 {
			result.Termination = "generated-after-repair"
		} else {
			result.Termination = "generated"
		}
	default:
		if decision.Route == askintent.RouteReview {
			result.LocalFindings = askreview.Workspace(resolvedRoot)
			result.ReviewLines = append(result.ReviewLines, findingsToLines(result.LocalFindings)...)
		}
		if canUseLLM(effective) {
			systemPrompt, userPrompt := infoPrompts(decision.Route, decision.Target, retrieval, requestText)
			result.PromptTraces = append(result.PromptTraces, promptTrace{Label: string(decision.Route), SystemPrompt: systemPrompt, UserPrompt: userPrompt})
			logger.logf("basic", "\n[ask][phase:answer:start] route=%s\n", decision.Route)
			info, infoErr := answerWithLLM(ctx, client, effective, decision, retrieval, requestText, logger)
			if infoErr == nil {
				result.LLMUsed = true
				result.Summary = info.Summary
				result.Answer = info.Answer
				result.ReviewLines = append(result.ReviewLines, info.Suggestions...)
				result.ReviewLines = append(result.ReviewLines, info.Findings...)
				result.ReviewLines = append(result.ReviewLines, info.SuggestedChange...)
				logger.logf("basic", "[ask][phase:answer:done] route=%s\n", decision.Route)
			} else {
				result.ReviewLines = append(result.ReviewLines, "LLM response failed; using local fallback: "+infoErr.Error())
				logger.logf("debug", "[ask][phase:answer:fallback] error=%v\n", infoErr)
				applyLocalFallback(&result, resolvedRoot, workspace, requestText)
			}
		} else {
			applyLocalFallback(&result, resolvedRoot, workspace, requestText)
		}
		if result.Termination == "" {
			result.Termination = "answered"
		}
	}

	if err := askstate.Save(resolvedRoot, askstate.Context{
		LastMode:            string(result.Route),
		LastRoute:           string(result.Route),
		LastConfidence:      result.Confidence,
		LastReason:          result.Reason,
		LastTargetKind:      result.Target.Kind,
		LastTargetPath:      result.Target.Path,
		LastTargetName:      result.Target.Name,
		LastPrompt:          strings.TrimSpace(requestText),
		LastFiles:           filePaths(result.Files),
		LastLint:            result.LintSummary,
		LastLLMUsed:         result.LLMUsed,
		LastClassifierLLM:   result.ClassifierLLM,
		LastChunkIDs:        chunkIDs(result.Chunks),
		LastDroppedChunkIDs: append([]string(nil), result.DroppedChunks...),
		LastAugmentEvents:   append([]string(nil), result.AugmentEvents...),
		LastMCPChunkIDs:     chunkIDsBySource(result.Chunks, "mcp"),
		LastLSPChunkIDs:     chunkIDsBySource(result.Chunks, "lsp"),
		LastRetries:         result.RetriesUsed,
		LastTermination:     result.Termination,
	}, requestText, resultToMarkdown(result)); err != nil {
		return err
	}

	return render(opts.Stdout, opts.Stderr, result)
}

func buildPlanWithReview(ctx context.Context, client askprovider.Client, cfg askconfigSettings, decision askintent.Decision, retrieval askretrieve.RetrievalResult, requestText string, workspace askretrieve.WorkspaceSummary, requirements askpolicy.ScenarioRequirements, logger askLogger) (askcontract.PlanResponse, askcontract.PlanCriticResponse, bool, error) {
	var planned askcontract.PlanResponse
	var critic askcontract.PlanCriticResponse
	currentPrompt := requestText
	for attempt := 1; attempt <= 2; attempt++ {
		current, planErr := planWithLLM(ctx, client, cfg, decision, retrieval, currentPrompt, workspace, logger)
		if planErr != nil {
			logger.logf("debug", "[ask][phase:plan:fallback] error=%v\n", planErr)
			planned = askpolicy.BuildPlanDefaults(requirements, requestText, decision, workspace)
			return planned, askcontract.PlanCriticResponse{}, true, nil
		}
		planned = current
		criticResp, criticErr := critiquePlanWithLLM(ctx, client, cfg, planned, logger)
		if criticErr != nil {
			logger.logf("debug", "[ask][phase:plan-critic:skip] error=%v\n", criticErr)
			return planned, askcontract.PlanCriticResponse{}, false, nil
		}
		critic = criticResp
		critic = normalizePlanCritic(planned, critic)
		if len(critic.Blocking) == 0 && len(critic.MissingContracts) == 0 {
			return planned, critic, false, nil
		}
		logger.logf("debug", "[ask][phase:plan-critic:retry] attempt=%d blocking=%d missing=%d\n", attempt, len(critic.Blocking), len(critic.MissingContracts))
		if attempt == 2 {
			return planned, critic, false, nil
		}
		currentPrompt = appendPlanCriticRetryPrompt(requestText, critic)
	}
	return planned, critic, false, nil
}

func appendPlanCriticRetryPrompt(base string, critic askcontract.PlanCriticResponse) string {
	b := &strings.Builder{}
	b.WriteString(strings.TrimSpace(base))
	b.WriteString("\n\nPlan critic requested a stronger plan before generation. Address these issues in the next plan JSON:\n")
	for _, finding := range critic.Findings {
		message := strings.TrimSpace(finding.Message)
		if message == "" {
			continue
		}
		label := strings.TrimSpace(string(finding.Severity))
		if label == "" {
			label = string(workflowissues.SeverityAdvisory)
		}
		b.WriteString("- ")
		b.WriteString(label)
		if code := strings.TrimSpace(string(finding.Code)); code != "" {
			b.WriteString(" [")
			b.WriteString(code)
			b.WriteString("]")
		}
		if path := strings.TrimSpace(finding.Path); path != "" {
			b.WriteString(" @ ")
			b.WriteString(path)
		}
		b.WriteString(": ")
		b.WriteString(message)
		b.WriteString("\n")
	}
	for _, item := range critic.SuggestedFixes {
		if strings.TrimSpace(item) != "" {
			b.WriteString("- fix: ")
			b.WriteString(strings.TrimSpace(item))
			b.WriteString("\n")
		}
	}
	b.WriteString("Required plan updates before generation:\n")
	b.WriteString("- Ensure executionModel.artifactContracts explicitly cover every staged package/image handoff used by apply.\n")
	b.WriteString("- Ensure exactly one authoritative join handoff contract is present when workers join a cluster.\n")
	b.WriteString("- Ensure executionModel.roleExecution and executionModel.verification match the requested topology and role behavior.\n")
	b.WriteString("- Prefer recoverable omissions to be fixed in the plan rather than adding new blockers or open questions.\n")
	return strings.TrimSpace(b.String())
}

func normalizePlanCritic(plan askcontract.PlanResponse, critic askcontract.PlanCriticResponse) askcontract.PlanCriticResponse {
	findings := normalizedPlanCriticFindings(plan, critic)
	blocking := make([]string, 0, len(findings))
	advisory := make([]string, 0, len(findings))
	missing := make([]string, 0, len(findings))
	for _, finding := range findings {
		text := strings.TrimSpace(finding.Message)
		if text == "" {
			continue
		}
		switch {
		case planCriticFindingIsFatal(finding):
			blocking = append(blocking, text)
		case finding.Severity == workflowissues.SeverityMissingContract && !planCriticFindingIsRecoverable(finding):
			missing = append(missing, text)
		default:
			advisory = append(advisory, text)
		}
	}
	critic.Findings = findings
	critic.Blocking = dedupe(blocking)
	critic.MissingContracts = dedupe(missing)
	critic.Advisory = dedupe(advisory)
	return critic
}

func hasFatalPlanReviewIssues(plan askcontract.PlanResponse, critic askcontract.PlanCriticResponse) bool {
	return len(fatalPlanReviewReasons(plan, critic)) > 0
}

func fatalPlanReviewReasons(plan askcontract.PlanResponse, critic askcontract.PlanCriticResponse) []string {
	reasons := []string{}
	for _, finding := range normalizedPlanCriticFindings(plan, critic) {
		if !planCriticFindingIsFatal(finding) {
			continue
		}
		if text := strings.TrimSpace(finding.Message); text != "" {
			reasons = append(reasons, text)
		}
	}
	for _, item := range fatalPlanBlockers(plan) {
		if strings.TrimSpace(item) != "" {
			reasons = append(reasons, item)
		}
	}
	if strings.TrimSpace(plan.EntryScenario) == "" {
		reasons = append(reasons, "no viable entry scenario can be determined")
	}
	if strings.TrimSpace(plan.AuthoringBrief.ModeIntent) == "prepare+apply" && !hasPlannedPath(plan.Files, "workflows/prepare.yaml") {
		reasons = append(reasons, "required prepare/apply file structure is absent and cannot be defaulted")
	}
	if strings.TrimSpace(plan.AuthoringBrief.ModeIntent) == "prepare+apply" && !hasPlannedPath(plan.Files, filepath.ToSlash(strings.TrimSpace(plan.EntryScenario))) {
		reasons = append(reasons, "required prepare/apply entry scenario is absent and cannot be defaulted")
	}
	if isMultiRoleTopology(plan.AuthoringBrief.Topology) && strings.TrimSpace(plan.ExecutionModel.RoleExecution.RoleSelector) == "" && !authoringBriefSuggestsRoleSelector(plan.AuthoringBrief) {
		reasons = append(reasons, "multi-role request has no viable role selector or branching model")
	}
	if needsArtifactPreparation(plan) && len(plan.ExecutionModel.ArtifactContracts) == 0 && !planHasArtifactConsumerPath(plan) {
		reasons = append(reasons, "artifact-dependent request has no viable consumer path")
	}
	return dedupe(reasons)
}

func fatalPlanBlockers(plan askcontract.PlanResponse) []string {
	_ = plan
	return nil
}

func hasPlannedPath(files []askcontract.PlanFile, want string) bool {
	want = strings.TrimSpace(want)
	for _, file := range files {
		if filepath.ToSlash(strings.TrimSpace(file.Path)) == want {
			return true
		}
	}
	return false
}

func isMultiRoleTopology(topology string) bool {
	switch strings.TrimSpace(topology) {
	case "multi-node", "ha":
		return true
	default:
		return false
	}
}

func needsArtifactPreparation(plan askcontract.PlanResponse) bool {
	return plan.NeedsPrepare || len(plan.ArtifactKinds) > 0 || strings.TrimSpace(plan.AuthoringBrief.ModeIntent) == "prepare+apply"
}

func authoringBriefSuggestsRoleSelector(brief askcontract.AuthoringBrief) bool {
	if strings.TrimSpace(brief.ModeIntent) != "prepare+apply" {
		return false
	}
	for _, capability := range brief.RequiredCapabilities {
		capability = strings.TrimSpace(capability)
		if capability == "kubeadm-join" || capability == "cluster-verification" {
			return true
		}
	}
	return isMultiRoleTopology(brief.Topology)
}

func planHasArtifactConsumerPath(plan askcontract.PlanResponse) bool {
	for _, file := range plan.Files {
		path := filepath.ToSlash(strings.TrimSpace(file.Path))
		if strings.HasPrefix(path, "workflows/scenarios/") {
			return true
		}
	}
	return false
}

func normalizedPlanCriticFindings(plan askcontract.PlanResponse, critic askcontract.PlanCriticResponse) []askcontract.PlanCriticFinding {
	findings := make([]askcontract.PlanCriticFinding, 0, len(critic.Findings)+len(critic.Blocking)+len(critic.Advisory)+len(critic.MissingContracts))
	findings = append(findings, critic.Findings...)
	seen := map[string]bool{}
	for _, finding := range critic.Findings {
		seen[planCriticFindingKey(finding)] = true
	}
	appendLegacy := func(items []string, severity workflowissues.Severity) {
		for _, item := range items {
			text := strings.TrimSpace(item)
			if text == "" {
				continue
			}
			finding := legacyPlanCriticFinding(text, severity)
			key := planCriticFindingKey(finding)
			if seen[key] {
				continue
			}
			seen[key] = true
			findings = append(findings, finding)
		}
	}
	appendLegacy(critic.Blocking, workflowissues.SeverityBlocking)
	appendLegacy(critic.Advisory, workflowissues.SeverityAdvisory)
	appendLegacy(critic.MissingContracts, workflowissues.SeverityMissingContract)
	return dedupePlanCriticFindings(findings)
}

func dedupePlanCriticFindings(findings []askcontract.PlanCriticFinding) []askcontract.PlanCriticFinding {
	seen := map[string]bool{}
	out := make([]askcontract.PlanCriticFinding, 0, len(findings))
	for _, finding := range findings {
		finding.Code = workflowissues.Code(strings.TrimSpace(string(finding.Code)))
		finding.Severity = workflowissues.Severity(strings.TrimSpace(string(finding.Severity)))
		finding.Message = strings.TrimSpace(finding.Message)
		finding.Path = strings.TrimSpace(finding.Path)
		if finding.Message == "" {
			continue
		}
		if finding.Severity == "" {
			finding.Severity = workflowissues.SeverityAdvisory
		}
		key := planCriticFindingKey(finding)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, finding)
	}
	return out
}

func planCriticFindingKey(finding askcontract.PlanCriticFinding) string {
	return strings.Join([]string{strings.TrimSpace(string(finding.Code)), strings.TrimSpace(string(finding.Severity)), strings.TrimSpace(finding.Path), strings.TrimSpace(finding.Message)}, "|")
}

func planCriticFindingIsFatal(finding askcontract.PlanCriticFinding) bool {
	if spec, ok := workflowissues.SpecFor(finding.Code); ok {
		return spec.DefaultSeverity == workflowissues.SeverityBlocking && !spec.DefaultRecoverable
	}
	if finding.Recoverable {
		return false
	}
	return finding.Severity == workflowissues.SeverityBlocking
}

func planCriticFindingIsRecoverable(finding askcontract.PlanCriticFinding) bool {
	if spec, ok := workflowissues.SpecFor(finding.Code); ok {
		return spec.DefaultRecoverable
	}
	return finding.Recoverable
}

func legacyPlanCriticFinding(text string, severity workflowissues.Severity) askcontract.PlanCriticFinding {
	recoverable := severity != workflowissues.SeverityBlocking
	return askcontract.PlanCriticFinding{
		Code:        workflowissues.CodeAskUnclassifiedCriticFinding,
		Severity:    severity,
		Message:     text,
		Recoverable: recoverable,
	}
}

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

func applyWriteOverride(decision askintent.Decision, heuristic askintent.Decision, write bool, logger askLogger) askintent.Decision {
	if !write {
		return decision
	}
	if (decision.Route == askintent.RouteDraft || decision.Route == askintent.RouteRefine) && !decision.AllowGeneration {
		logger.logf("debug", "[ask][phase:classify:override] route=%s reason=write-flag-enable-generation\n", decision.Route)
		decision.AllowGeneration = true
		decision.AllowRetry = true
		decision.RequiresLint = true
		decision.Reason = "write flag enables generation for authoring route"
		return decision
	}
	if !decision.AllowGeneration && heuristic.AllowGeneration {
		logger.logf("debug", "[ask][phase:classify:override] from=%s to=%s reason=write-flag\n", decision.Route, heuristic.Route)
		decision = heuristic
		decision.Reason = "write flag overrides non-generation classification"
	}
	return decision
}

func canUseLLM(cfg askconfig.EffectiveSettings) bool {
	if !askconfig.NeedsAPIKey(cfg.Provider) {
		return true
	}
	return strings.TrimSpace(cfg.APIKey) != "" || strings.TrimSpace(cfg.OAuthToken) != ""
}

func classifyWithLLM(ctx context.Context, client askprovider.Client, cfg askconfig.EffectiveSettings, systemPrompt string, userPrompt string, logger askLogger) (askintent.Decision, error) {
	logger.prompt("classifier", systemPrompt, userPrompt)
	request := askprovider.Request{
		Kind:         "classify",
		Provider:     cfg.Provider,
		Model:        cfg.Model,
		APIKey:       cfg.APIKey,
		OAuthToken:   cfg.OAuthToken,
		AccountID:    cfg.AccountID,
		Endpoint:     cfg.Endpoint,
		SystemPrompt: systemPrompt,
		Prompt:       userPrompt,
		MaxRetries:   providerRetryCount("classify"),
		Timeout:      askRequestTimeout("classify", 1, systemPrompt, userPrompt),
	}
	var parsed askcontract.ClassificationResponse
	for attempt := 0; attempt < 2; attempt++ {
		resp, err := client.Generate(ctx, request)
		if err != nil {
			return askintent.Decision{}, err
		}
		logger.response("classifier", resp.Content)
		parsed, err = askcontract.ParseClassification(resp.Content)
		if err == nil {
			break
		}
		if attempt == 1 {
			return askintent.Decision{}, err
		}
	}
	route := askintent.ParseRoute(parsed.Route)
	decision := routeDefaults(route)
	decision.Confidence = parsed.Confidence
	if decision.Confidence == 0 {
		decision.Confidence = 0.6
	}
	if strings.TrimSpace(parsed.Reason) != "" {
		decision.Reason = parsed.Reason
	}
	decision.Target = askintent.Target{Kind: parsed.Target.Kind, Path: parsed.Target.Path, Name: parsed.Target.Name}
	if decision.Target.Kind == "" {
		decision.Target = askintent.Target{Kind: "workspace"}
	}
	if parsed.GenerationAllowed != nil && !*parsed.GenerationAllowed {
		decision.AllowGeneration = false
		decision.AllowRetry = false
		decision.RequiresLint = false
		decision.LLMPolicy = askintent.LLMOptional
	}
	return decision, nil
}

func routeDefaults(route askintent.Route) askintent.Decision {
	switch route {
	case askintent.RouteDraft:
		return askintent.Decision{Route: route, Confidence: 0.8, Reason: "draft route", Target: askintent.Target{Kind: "workspace"}, AllowGeneration: true, AllowRetry: true, RequiresLint: true, LLMPolicy: askintent.LLMRequired}
	case askintent.RouteRefine:
		return askintent.Decision{Route: route, Confidence: 0.8, Reason: "refine route", Target: askintent.Target{Kind: "workspace"}, AllowGeneration: true, AllowRetry: true, RequiresLint: true, LLMPolicy: askintent.LLMRequired}
	case askintent.RouteReview:
		return askintent.Decision{Route: route, Confidence: 0.75, Reason: "review route", Target: askintent.Target{Kind: "workspace"}, AllowGeneration: false, AllowRetry: false, RequiresLint: false, LLMPolicy: askintent.LLMOptional}
	case askintent.RouteExplain:
		return askintent.Decision{Route: route, Confidence: 0.75, Reason: "explain route", Target: askintent.Target{Kind: "workspace"}, AllowGeneration: false, AllowRetry: false, RequiresLint: false, LLMPolicy: askintent.LLMOptional}
	case askintent.RouteQuestion:
		return askintent.Decision{Route: route, Confidence: 0.75, Reason: "question route", Target: askintent.Target{Kind: "workspace"}, AllowGeneration: false, AllowRetry: false, RequiresLint: false, LLMPolicy: askintent.LLMOptional}
	default:
		return askintent.Decision{Route: askintent.RouteClarify, Confidence: 0.8, Reason: "clarify route", Target: askintent.Target{Kind: "unknown"}, AllowGeneration: false, AllowRetry: false, RequiresLint: false, LLMPolicy: askintent.LLMOptional}
	}
}

func answerWithLLM(ctx context.Context, client askprovider.Client, cfg askconfig.EffectiveSettings, decision askintent.Decision, retrieval askretrieve.RetrievalResult, prompt string, logger askLogger) (askcontract.InfoResponse, error) {
	systemPrompt, userPrompt := infoPrompts(decision.Route, decision.Target, retrieval, prompt)
	logger.prompt(string(decision.Route), systemPrompt, userPrompt)
	resp, err := client.Generate(ctx, askprovider.Request{
		Kind:         string(decision.Route),
		Provider:     cfg.Provider,
		Model:        cfg.Model,
		APIKey:       cfg.APIKey,
		OAuthToken:   cfg.OAuthToken,
		AccountID:    cfg.AccountID,
		Endpoint:     cfg.Endpoint,
		SystemPrompt: systemPrompt,
		Prompt:       userPrompt,
		MaxRetries:   providerRetryCount(string(decision.Route)),
		Timeout:      askRequestTimeout(string(decision.Route), 1, systemPrompt, userPrompt),
	})
	if err != nil {
		return askcontract.InfoResponse{}, err
	}
	logger.response(string(decision.Route), resp.Content)
	return askcontract.ParseInfo(resp.Content), nil
}

func generationAttempts(requested int, decision askintent.Decision, prompt string) int {
	if !decision.AllowRetry {
		return 1
	}
	if !requestSpecificEnough(prompt) {
		return 1
	}
	if requested > 0 {
		return requested
	}
	return 2
}

func askRequestTimeout(kind string, maxIterations int, systemPrompt string, prompt string) time.Duration {
	base := 90 * time.Second
	switch strings.TrimSpace(kind) {
	case "classify":
		base = 45 * time.Second
	case "plan", "plan-critic":
		base = 90 * time.Second
	case "generate", "generate-fast":
		base = 180 * time.Second
	case "judge":
		base = 90 * time.Second
	case "postprocess-critic", "postprocess-edit", "postprocess-structural":
		base = 120 * time.Second
	}
	if maxIterations > 1 {
		base += time.Duration(minInt(maxIterations-1, 4)) * 30 * time.Second
	}
	promptBytes := len(systemPrompt) + len(prompt)
	base += time.Duration(minInt(promptBytes/4000, 8)) * 10 * time.Second
	return minDuration(base, 10*time.Minute)
}

func providerRetryCount(kind string) int {
	switch strings.TrimSpace(kind) {
	case "generate", "generate-fast", "plan", "plan-critic", "judge", "postprocess-critic", "postprocess-edit", "postprocess-structural", "classify":
		return 2
	default:
		return 1
	}
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func minDuration(a time.Duration, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func askFeatureEnabled(name string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	switch value {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func generateWithValidation(ctx context.Context, client askprovider.Client, req askprovider.Request, root string, attempts int, logger askLogger, decision askintent.Decision, plan askcontract.PlanResponse, brief askcontract.AuthoringBrief, retrieval askretrieve.RetrievalResult, planCritic askcontract.PlanCriticResponse) (askcontract.GenerationResponse, string, askcontract.CriticResponse, askcontract.JudgeResponse, int, error) {
	_ = planCritic
	var lastValidation string
	var lastCritic askcontract.CriticResponse
	var lastJudge askcontract.JudgeResponse
	var lastGeneration askcontract.GenerationResponse
	taintedFiles := map[string]bool{}
	bundle := askknowledge.Current()
	for attempt := 1; attempt <= attempts; attempt++ {
		currentPrompt := req.Prompt
		currentSystemPrompt := req.SystemPrompt
		if attempt > 1 && lastValidation != "" {
			validationDiags := askdiagnostic.FromValidationError(lastValidation, bundle)
			markTaintedFiles(taintedFiles, validationDiags)
			repairPaths := repairTargetFiles(lastGeneration, validationDiags, taintedFiles)
			diags := append([]askdiagnostic.Diagnostic{}, validationDiags...)
			if !isYAMLParseFailure(lastValidation) {
				diags = append(diags, askdiagnostic.FromPlanCritic(planCritic)...)
				diags = append(diags, askdiagnostic.FromCritic(lastCritic)...)
			}
			logger.logf("debug", "\n[ask][phase:repair:diagnostics]\n%s\n", askdiagnostic.JSON(diags))
			if isYAMLParseFailure(lastValidation) {
				currentSystemPrompt = yamlRepairSystemPrompt(normalizedAuthoringBrief(plan, brief), plan)
				currentPrompt = yamlRepairUserPrompt(lastGeneration, lastValidation, validationDiags, repairPaths)
			} else {
				currentPrompt += "\n\nLocal validation failed. Enter targeted repair mode and return full JSON again."
				currentPrompt += "\nValidator summary:\n" + summarizeValidationError(lastValidation)
				currentPrompt += "\nRaw validator error:\n" + strings.TrimSpace(lastValidation)
				for _, chunk := range askretrieve.RepairChunks(req.Prompt, lastValidation) {
					currentPrompt += "\n" + chunk.Content
				}
				currentPrompt += "\n" + askdiagnostic.RepairPromptBlock(diags)
				currentPrompt += "\n" + yamlStructureRepairPromptBlock(lastGeneration, lastValidation, repairPaths)
				currentPrompt += "\n" + targetedRepairPromptBlock(lastGeneration, diags, repairPaths)
			}
		}
		logger.logf("basic", "[ask][phase:generation:attempt] attempt=%d/%d\n", attempt, attempts)
		logger.prompt("generation", currentSystemPrompt, currentPrompt)
		resp, err := client.Generate(ctx, askprovider.Request{
			Kind:         req.Kind,
			Provider:     req.Provider,
			Model:        req.Model,
			APIKey:       req.APIKey,
			OAuthToken:   req.OAuthToken,
			Endpoint:     req.Endpoint,
			SystemPrompt: currentSystemPrompt,
			Prompt:       currentPrompt,
			MaxRetries:   providerRetryCount(req.Kind),
			Timeout:      askRequestTimeout(req.Kind, attempts, currentSystemPrompt, currentPrompt),
		})
		if err != nil {
			return askcontract.GenerationResponse{}, lastValidation, lastCritic, lastJudge, attempt - 1, err
		}
		logger.response("generation", resp.Content)
		gen, err := askcontract.ParseGeneration(resp.Content)
		if err != nil {
			lastValidation = err.Error()
			logger.logf("debug", "[ask][phase:generation:parse-error] error=%s\n", lastValidation)
			if attempt < attempts {
				continue
			}
			return askcontract.GenerationResponse{}, lastValidation, lastCritic, lastJudge, attempt - 1, fmt.Errorf("ask generation returned invalid JSON: %s", lastValidation)
		}
		if attempt > 1 && len(lastGeneration.Files) > 0 {
			gen = mergeGeneratedFiles(dropGeneratedFiles(lastGeneration, mapKeys(taintedFiles)), gen)
		}
		gen = normalizeGeneratedFiles(gen)
		logger.logf("debug", "[ask][phase:semantic-validate] attempt=%d/%d\n", attempt, attempts)
		lastGeneration = gen
		lintSummary, critic, err := validateGeneration(ctx, root, gen, decision, plan, brief, retrieval)
		lastCritic = critic
		if err == nil {
			judge, judgeErr := maybeJudgeGeneration(ctx, client, req, gen, lintSummary, critic, plan, brief, logger)
			if judgeErr == nil {
				lastJudge = judge
				critic = mergeJudgeIntoCritic(critic, judge, attempt == attempts)
				if len(judge.Blocking) > 0 && attempt < attempts {
					lastValidation = "semantic judge requested revision: " + strings.Join(judge.Blocking, "; ")
					lastCritic = critic
					logger.logf("debug", "[ask][phase:judge:retry] blocking=%d\n", len(judge.Blocking))
					continue
				}
			} else {
				logger.logf("debug", "[ask][phase:judge:skip] error=%v\n", judgeErr)
			}
			return gen, lintSummary, critic, lastJudge, attempt - 1, nil
		}
		lastValidation = err.Error()
		logger.logf("debug", "[ask][phase:generation:validation-error] error=%s\n", lastValidation)
		if !repairableValidationError(lastValidation) {
			return askcontract.GenerationResponse{}, lastValidation, critic, lastJudge, attempt - 1, fmt.Errorf("ask generation stopped without repair: %s", lastValidation)
		}
	}
	if lastValidation == "" {
		lastValidation = "generation failed without a parseable response"
	}
	return askcontract.GenerationResponse{}, lastValidation, lastCritic, lastJudge, attempts - 1, fmt.Errorf("ask generation did not validate after %d attempts: %s", attempts, lastValidation)
}

func isYAMLParseFailure(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	return strings.Contains(lower, "parse yaml") || strings.Contains(lower, "parse vars yaml") || strings.Contains(lower, "yaml: line ") || strings.Contains(lower, "yaml: did not") || strings.Contains(lower, "yaml: could not")
}

func maybeJudgeGeneration(ctx context.Context, client askprovider.Client, req askprovider.Request, gen askcontract.GenerationResponse, lintSummary string, critic askcontract.CriticResponse, plan askcontract.PlanResponse, brief askcontract.AuthoringBrief, logger askLogger) (askcontract.JudgeResponse, error) {
	if strings.TrimSpace(req.Kind) != "generate" {
		return askcontract.JudgeResponse{}, fmt.Errorf("judge disabled for default generation path")
	}
	if strings.TrimSpace(brief.RouteIntent) == "" {
		return askcontract.JudgeResponse{}, fmt.Errorf("judge skipped without authoring brief")
	}
	systemPrompt := judgeSystemPrompt(brief, plan)
	userPrompt := judgeUserPrompt(gen, lintSummary, critic)
	logger.prompt("judge", systemPrompt, userPrompt)
	resp, err := client.Generate(ctx, askprovider.Request{
		Kind:         "judge",
		Provider:     req.Provider,
		Model:        req.Model,
		APIKey:       req.APIKey,
		OAuthToken:   req.OAuthToken,
		AccountID:    req.AccountID,
		Endpoint:     req.Endpoint,
		SystemPrompt: systemPrompt,
		Prompt:       userPrompt,
		MaxRetries:   providerRetryCount("judge"),
		Timeout:      askRequestTimeout("judge", 1, systemPrompt, userPrompt),
	})
	if err != nil {
		return askcontract.JudgeResponse{}, err
	}
	logger.response("judge", resp.Content)
	return askcontract.ParseJudge(resp.Content)
}

func targetedRepairPromptBlock(prev askcontract.GenerationResponse, diags []askdiagnostic.Diagnostic, repairPaths []string) string {
	if len(prev.Files) == 0 {
		return ""
	}
	affected := map[string]bool{}
	for _, path := range repairPaths {
		if strings.TrimSpace(path) != "" {
			affected[strings.TrimSpace(path)] = true
		}
	}
	if len(affected) == 0 {
		for _, file := range prev.Files {
			affected[strings.TrimSpace(file.Path)] = true
		}
	}
	b := &strings.Builder{}
	b.WriteString("Targeted repair mode:\n")
	b.WriteString("- Preserve unchanged files when they are already valid.\n")
	b.WriteString("- For files marked preserve-if-valid, keep content byte-for-byte unless a diagnostic explicitly requires a change.\n")
	b.WriteString("- Prefer editing only the files implicated by diagnostics or execution/design review findings.\n")
	if hasDiagnosticCode(diags, string(workflowissues.CodeDuplicateStepID)) {
		b.WriteString("- Duplicate step id repair: rename only the conflicting ids; do not duplicate or rewrite unaffected steps.\n")
		spec := workflowissues.MustSpec(workflowissues.CodeDuplicateStepID)
		if strings.TrimSpace(spec.PromptHint) != "" {
			b.WriteString("- ")
			b.WriteString(strings.TrimSpace(spec.PromptHint))
			b.WriteString(" For example `control-plane-preflight-host` and `worker-preflight-host`.\n")
		}
	}
	b.WriteString("- When revising YAML, keep `version: v1alpha1` and top-level keys at column 1, indent mapping children by two spaces, and indent list items under their parent key.\n")
	b.WriteString("- Do not collapse YAML indentation, remove required list markers, or rewrite every file from scratch when only one file is broken.\n")
	b.WriteString("- Return the full JSON response with all files that should remain in the final result.\n")
	if len(affected) > 0 {
		b.WriteString("Affected files to revise first:\n")
		for _, file := range prev.Files {
			if affected[strings.TrimSpace(file.Path)] {
				b.WriteString("- ")
				b.WriteString(strings.TrimSpace(file.Path))
				b.WriteString("\n")
			}
		}
	}
	b.WriteString("File status from previous attempt:\n")
	for _, file := range prev.Files {
		path := strings.TrimSpace(file.Path)
		status := "preserve-if-valid"
		if affected[path] {
			status = "revise"
		}
		b.WriteString("- path: ")
		b.WriteString(path)
		b.WriteString(" [")
		b.WriteString(status)
		b.WriteString("]\n")
		for _, detail := range diagnosticDetailsForFile(path, diags) {
			b.WriteString("  - ")
			b.WriteString(detail)
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func hasDiagnosticCode(diags []askdiagnostic.Diagnostic, code string) bool {
	code = strings.TrimSpace(code)
	for _, diag := range diags {
		if strings.TrimSpace(diag.Code) == code {
			return true
		}
	}
	return false
}

func diagnosticDetailsForFile(path string, diags []askdiagnostic.Diagnostic) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	items := []string{}
	for _, diag := range diags {
		diagPath := strings.TrimSpace(diag.Path)
		if diagPath == "" {
			diagPath = strings.TrimSpace(diag.File)
		}
		if diagPath == "" {
			diagPath = diagnosticMessageFile(diag.Message)
		}
		if diagPath != path {
			continue
		}
		msg := strings.TrimSpace(diag.Message)
		if msg != "" {
			items = append(items, msg)
		}
		fix := strings.TrimSpace(diag.SuggestedFix)
		if fix != "" {
			items = append(items, "suggested fix: "+fix)
		}
	}
	return dedupe(items)
}

func yamlStructureRepairPromptBlock(prev askcontract.GenerationResponse, validation string, repairPaths []string) string {
	lower := strings.ToLower(strings.TrimSpace(validation))
	if !strings.Contains(lower, "parse yaml") && !strings.Contains(lower, "yaml:") {
		return ""
	}
	affected := repairPaths
	if len(affected) == 0 {
		affected = affectedFilesFromDiagnostics(prev, nil)
	}
	b := &strings.Builder{}
	b.WriteString("YAML structure repair requirements:\n")
	b.WriteString("- Fix YAML structure before changing workflow design. Prioritize indentation, list markers, and key nesting.\n")
	b.WriteString("- Keep every revised file as plain YAML text with stable indentation; do not compress nested objects onto the wrong column.\n")
	b.WriteString("- Preserve already-valid files exactly; only revise files implicated by the parse error when possible.\n")
	b.WriteString("- For workflow files, keep top-level `version: v1alpha1` at column 1, then `phases:` or `steps:` at column 1.\n")
	b.WriteString("- Under `phases:` or `steps:`, each list item must start with `  -` and nested keys must be indented consistently beneath it.\n")
	if len(affected) > 0 {
		b.WriteString("- Parse-error files to fix first:\n")
		for _, path := range affected {
			b.WriteString("  - ")
			b.WriteString(path)
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func repairTargetFiles(prev askcontract.GenerationResponse, diags []askdiagnostic.Diagnostic, tainted map[string]bool) []string {
	targets := diagnosticFiles(diags)
	if len(targets) == 0 {
		targets = affectedFilesFromDiagnostics(prev, diags)
	}
	for path := range tainted {
		if !stringSliceContains(targets, path) {
			targets = append(targets, path)
		}
	}
	return targets
}

func markTaintedFiles(tainted map[string]bool, diags []askdiagnostic.Diagnostic) {
	for _, path := range diagnosticFiles(diags) {
		tainted[path] = true
	}
}

func diagnosticFiles(diags []askdiagnostic.Diagnostic) []string {
	paths := []string{}
	for _, diag := range diags {
		path := strings.TrimSpace(diag.Path)
		if path == "" {
			path = strings.TrimSpace(diag.File)
		}
		if path == "" {
			path = diagnosticMessageFile(diag.Message)
		}
		if path != "" && !stringSliceContains(paths, path) {
			paths = append(paths, path)
		}
	}
	return paths
}

func mapKeys(items map[string]bool) []string {
	out := make([]string, 0, len(items))
	for key := range items {
		out = append(out, key)
	}
	return out
}

func affectedFilesFromDiagnostics(prev askcontract.GenerationResponse, diags []askdiagnostic.Diagnostic) []string {
	affected := map[string]bool{}
	for _, diag := range diags {
		path := strings.TrimSpace(diag.Path)
		if path == "" {
			path = strings.TrimSpace(diag.File)
		}
		if path != "" {
			affected[path] = true
		}
	}
	if len(affected) == 0 {
		for _, file := range prev.Files {
			affected[strings.TrimSpace(file.Path)] = true
		}
	}
	out := make([]string, 0, len(affected))
	for _, file := range prev.Files {
		path := strings.TrimSpace(file.Path)
		if affected[path] {
			out = append(out, path)
		}
	}
	for path := range affected {
		if !stringSliceContains(out, path) {
			out = append(out, path)
		}
	}
	return out
}

func stringSliceContains(items []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, item := range items {
		if strings.TrimSpace(item) == want {
			return true
		}
	}
	return false
}

func diagnosticMessageFile(message string) string {
	message = strings.TrimSpace(message)
	if !strings.HasPrefix(message, "workflows/") {
		return ""
	}
	idx := strings.Index(message, ":")
	if idx <= 0 {
		return ""
	}
	path := strings.TrimSpace(message[:idx])
	if !strings.HasPrefix(path, "workflows/") {
		return ""
	}
	return path
}

func mergeJudgeIntoCritic(critic askcontract.CriticResponse, judge askcontract.JudgeResponse, finalAttempt bool) askcontract.CriticResponse {
	critic.Advisory = append(critic.Advisory, judge.Advisory...)
	critic.Advisory = append(critic.Advisory, judge.MissingCapabilities...)
	if finalAttempt {
		critic.Advisory = append(critic.Advisory, judge.Blocking...)
	}
	critic.RequiredFixes = append(critic.RequiredFixes, judge.SuggestedFixes...)
	critic.Advisory = dedupe(critic.Advisory)
	critic.RequiredFixes = dedupe(critic.RequiredFixes)
	return critic
}

func repairableValidationError(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	if message == "" {
		return false
	}
	nonRepairable := []string{
		"response did not include any files",
		"generated file path is empty",
		"generated file path is not allowed",
		"generated file path escapes workspace",
	}
	for _, token := range nonRepairable {
		if strings.Contains(message, token) {
			return false
		}
	}
	return true
}

func normalizedAuthoringBrief(plan askcontract.PlanResponse, fallback askcontract.AuthoringBrief) askcontract.AuthoringBrief {
	if strings.TrimSpace(plan.AuthoringBrief.RouteIntent) != "" {
		return plan.AuthoringBrief
	}
	return fallback
}

func validateGeneration(ctx context.Context, root string, gen askcontract.GenerationResponse, decision askintent.Decision, plan askcontract.PlanResponse, brief askcontract.AuthoringBrief, retrieval askretrieve.RetrievalResult) (string, askcontract.CriticResponse, error) {
	if len(gen.Files) == 0 {
		critic := askcontract.CriticResponse{Blocking: []string{"response did not include any files"}, MissingFiles: filePathsFromPlan(plan), RequiredFixes: []string{"Return the planned workflow files"}}
		return "", critic, fmt.Errorf("response did not include any files")
	}
	staged, err := stageWorkspace(root, gen.Files)
	if err != nil {
		return "", askcontract.CriticResponse{Blocking: []string{err.Error()}}, err
	}
	defer func() { _ = os.RemoveAll(staged) }()
	paths := make([]string, 0, len(gen.Files))
	directValidated := 0
	for _, file := range gen.Files {
		if err := validateGeneratedFile(staged, file); err != nil {
			return "", askcontract.CriticResponse{Blocking: []string{err.Error()}, RequiredFixes: requiredFixesForValidation(err.Error())}, err
		}
		paths = append(paths, file.Path)
		if strings.HasSuffix(strings.ToLower(strings.TrimSpace(file.Path)), ".yaml") || strings.HasSuffix(strings.ToLower(strings.TrimSpace(file.Path)), ".yml") {
			directValidated++
		}
	}
	entrypoints := scenarioPaths(staged, paths)
	validated := make([]string, 0, len(entrypoints))
	for _, path := range entrypoints {
		files, err := validate.EntrypointWithContext(ctx, path)
		if err != nil {
			return "", askcontract.CriticResponse{Blocking: []string{err.Error()}, RequiredFixes: requiredFixesForValidation(err.Error())}, err
		}
		validated = append(validated, files...)
	}
	validated = dedupe(validated)
	critic := semanticCritic(gen, decision, plan, normalizedAuthoringBrief(plan, brief), retrieval)
	if len(critic.Blocking) > 0 {
		return "", critic, fmt.Errorf("semantic validation failed: %s", strings.Join(critic.Blocking, "; "))
	}
	return fmt.Sprintf("lint ok (%d yaml files, %d scenario entrypoints)", directValidated, len(validated)), critic, nil
}

func requiredFixesForValidation(message string) []string {
	fixes := []string{"Return only schema-valid files under allowed workflow paths"}
	lower := strings.ToLower(strings.TrimSpace(message))
	if strings.Contains(lower, "invalid map key") && (strings.Contains(lower, "{{") || strings.Contains(lower, ".vars.")) {
		fixes = append(fixes, "Do not use whole-value template expressions like `{{ .vars.* }}` for typed YAML arrays or objects such as spec.packages or spec.repositories; inline concrete YAML lists or objects instead")
	}
	if strings.Contains(lower, "parse yaml") && strings.Contains(lower, ".vars.") {
		fixes = append(fixes, "Keep workflows/vars.yaml as plain YAML data only. Do not place template expressions inside vars values, and quote any literal strings that contain special YAML characters")
	}
	if strings.Contains(lower, "imports.0") && strings.Contains(lower, "expected: object") && strings.Contains(lower, "given: string") {
		fixes = append(fixes, "Use phase imports as objects like `imports: [{path: check-host.yaml}]` rather than bare strings")
	}
	if strings.Contains(lower, "additional property version is not allowed") {
		fixes = append(fixes, "Do not add workflow-level fields like version to component fragments under workflows/components/. Component files should usually contain only a top-level steps mapping")
	}
	if strings.Contains(lower, "invalid type. expected: object, given: array") {
		fixes = append(fixes, "Do not make a component file a bare YAML array. Component files should be YAML objects, usually with a top-level steps: key")
	}
	if strings.Contains(lower, "workflows/components/") {
		fixes = append(fixes, "For starter drafts, avoid generating workflows/components/ unless reusable fragments are clearly required; inline the first working version into prepare/apply instead")
	}
	if strings.Contains(lower, "command") && strings.Contains(lower, "is not supported for role prepare") {
		fixes = append(fixes, "Use typed prepare steps like DownloadImage or DownloadPackage instead of Command when collecting offline artifacts in prepare")
	}
	fixes = append(fixes, askcontext.ValidationFixesForError(message)...)
	return dedupe(fixes)
}

func summarizeValidationError(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return "- validation failed with no additional detail"
	}
	lower := strings.ToLower(message)
	workflowRules := askcontext.Current().Workflow
	points := []string{}
	appendPoint := func(point string) {
		point = strings.TrimSpace(point)
		if point == "" {
			return
		}
		points = append(points, point)
	}
	switch {
	case strings.Contains(lower, "parse yaml") || strings.Contains(lower, "yaml:"):
		appendPoint("- YAML parse failure: fix indentation, list markers, or template placement before changing step logic")
	case strings.Contains(lower, "e_schema_invalid") || strings.Contains(lower, " is required") || strings.Contains(lower, "additional property"):
		appendPoint("- Schema validation failure: keep only supported fields and include required workflow and step fields")
	case strings.Contains(lower, "semantic validation failed"):
		appendPoint("- Semantic validation failure: generated files are inconsistent with the request or plan")
	}
	for _, line := range strings.Split(message, ";") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(strings.ToLower(line), "(root): version is required") {
			appendPoint("- Add top-level `version: " + workflowRules.SupportedVersion + "` to every workflow file")
		}
		if strings.Contains(strings.ToLower(line), ": id is required") {
			appendPoint("- Add an `id` field to every step item")
		}
		if strings.Contains(strings.ToLower(line), "additional property id is not allowed") && strings.Contains(strings.ToLower(line), "phases.") {
			appendPoint("- Remove `id` from phases and keep a non-empty `name`; only steps carry ids")
		}
		if strings.Contains(strings.ToLower(line), "additional property") && strings.Contains(strings.ToLower(line), "phases.") {
			appendPoint("- Phase objects support `name`, `steps`, `imports`, and optional `maxParallelism` only")
		}
		if strings.Contains(strings.ToLower(line), "invalid map key") {
			appendPoint("- Do not use whole-value template expressions where YAML arrays or objects are required")
		}
		if strings.Contains(strings.ToLower(line), "must be one of") {
			appendPoint("- Keep constrained enum fields as literal allowed values instead of replacing them with vars templates")
		}
		if strings.Contains(strings.ToLower(line), "does not match pattern") {
			appendPoint("- Keep pattern-constrained scalar fields as literal values that satisfy the documented schema pattern instead of replacing them with vars templates")
		}
		if strings.Contains(strings.ToLower(line), "did not find expected node content") {
			appendPoint("- Keep YAML list items and template directives in valid YAML structure")
		}
	}
	if len(points) == 0 {
		appendPoint("- Fix the validator error exactly as reported and keep the response schema-valid")
	}
	return strings.Join(dedupe(points), "\n")
}
