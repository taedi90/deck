package askcli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	lspaugment "github.com/Airgap-Castaways/deck/internal/askaugment/lsp"
	mcpaugment "github.com/Airgap-Castaways/deck/internal/askaugment/mcp"
	"github.com/Airgap-Castaways/deck/internal/askconfig"
	"github.com/Airgap-Castaways/deck/internal/askcontract"
	"github.com/Airgap-Castaways/deck/internal/askhooks"
	"github.com/Airgap-Castaways/deck/internal/askintent"
	"github.com/Airgap-Castaways/deck/internal/askknowledge"
	"github.com/Airgap-Castaways/deck/internal/askpolicy"
	"github.com/Airgap-Castaways/deck/internal/askprovider"
	"github.com/Airgap-Castaways/deck/internal/askretrieve"
	"github.com/Airgap-Castaways/deck/internal/askreview"
	"github.com/Airgap-Castaways/deck/internal/askscaffold"
	"github.com/Airgap-Castaways/deck/internal/askstate"
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
