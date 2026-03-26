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
	if canUseLLM(effective) {
		logger.logf("debug", "\n[ask][phase:classify:start] provider=%s model=%s\n", effective.Provider, effective.Model)
		classified, classifyErr := classifyWithLLM(ctx, client, effective, classifierSystem, classifierUser, logger)
		if classifyErr == nil {
			decision = classified
			classifierLLM = true
			logger.logf("basic", "[ask][phase:classify:done] route=%s confidence=%.2f reason=%s\n", decision.Route, decision.Confidence, decision.Reason)
		} else {
			logger.logf("debug", "[ask][phase:classify:fallback] error=%v\n", classifyErr)
		}
	} else {
		logger.logf("debug", "[ask][phase:classify:skip] reason=no-llm-credentials\n")
	}
	decision = applyWriteOverride(decision, heuristic, opts.Write, logger)
	if decision.Route == askintent.RouteRefine && !workspace.HasWorkflowTree {
		return fmt.Errorf("cannot refine workflow files because this workspace has no workflow tree yet; run a draft generation first")
	}

	mcpChunks, mcpEvents := mcpaugment.Gather(ctx, effective.MCP, decision.Route, requestText)
	lspChunks, lspEvents := lspaugment.Gather(ctx, effective.LSP, decision.Target, workspace)
	externalChunks := append(append([]askretrieve.Chunk{}, mcpChunks...), lspChunks...)
	externalChunks = append(externalChunks, projectContextChunk(resolvedRoot))
	retrieval := askretrieve.Retrieve(decision.Route, requestText, decision.Target, workspace, state, externalChunks)
	requirements := askpolicy.BuildScenarioRequirements(requestText, retrieval, workspace, decision)
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
	planNeeded := isAuthoringRoute(decision.Route) && (planRequested || needsComplexPlanner(requestText, workspace, decision))
	var plan askcontract.PlanResponse
	if planNeeded {
		if !canUseLLM(effective) {
			return fmt.Errorf("route %s requires model access; configure provider credentials first", decision.Route)
		}
		logger.logf("basic", "\n[ask][phase:plan:start] route=%s\n", decision.Route)
		planned, planErr := planWithLLM(ctx, client, askconfigSettings{provider: effective.Provider, model: effective.Model, apiKey: effective.APIKey, oauthToken: effective.OAuthToken, accountID: effective.AccountID, endpoint: effective.Endpoint}, decision, retrieval, requestText, workspace, logger)
		if planErr != nil {
			logger.logf("debug", "[ask][phase:plan:fallback] error=%v\n", planErr)
			planned = askpolicy.BuildPlanDefaults(requirements, requestText, decision, workspace)
		}
		plan = planned
		result.Plan = &plan
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
		if hasBlockingPlanItems(plan) {
			result.Summary = "plan generated with blockers"
			result.Termination = "plan-only-blocked"
			result.FallbackNote = "generation stopped because plan has blockers"
			result.ReviewLines = append(result.ReviewLines, renderPlanNotes(plan)...)
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
		generationRequest := askprovider.Request{
			Kind:         "generate",
			Provider:     effective.Provider,
			Model:        effective.Model,
			APIKey:       effective.APIKey,
			OAuthToken:   effective.OAuthToken,
			AccountID:    effective.AccountID,
			Endpoint:     effective.Endpoint,
			SystemPrompt: generationSystemPrompt(decision.Route, decision.Target, retrieval, requirements, scaffold),
			Prompt:       generationUserPrompt(workspace, state, requestText, strings.TrimSpace(opts.FromPath), decision.Route),
			MaxRetries:   attempts,
		}
		result.PromptTraces = append(result.PromptTraces, promptTrace{Label: "generation", SystemPrompt: generationRequest.SystemPrompt, UserPrompt: generationRequest.Prompt})
		logger.logf("basic", "\n[ask][phase:generation:start] route=%s attempts=%d\n", decision.Route, attempts)
		gen, lintSummary, critic, retriesUsed, genErr := generateWithValidation(ctx, client, generationRequest, resolvedRoot, attempts, logger, decision, plan)
		if genErr != nil {
			return genErr
		}
		logger.logf("basic", "[ask][phase:generation:done] files=%d lint=%s\n", len(gen.Files), lintSummary)
		result.LLMUsed = true
		result.RetriesUsed = retriesUsed
		result.Files = gen.Files
		result.Summary = gen.Summary
		result.ReviewLines = append(result.ReviewLines, gen.Review...)
		result.LintSummary = lintSummary
		result.LocalFindings = localFindings(result.Files)
		result.Critic = &critic
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
		MaxRetries:   1,
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
		MaxRetries:   1,
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
	return 3
}

func generateWithValidation(ctx context.Context, client askprovider.Client, req askprovider.Request, root string, attempts int, logger askLogger, decision askintent.Decision, plan askcontract.PlanResponse) (askcontract.GenerationResponse, string, askcontract.CriticResponse, int, error) {
	var lastValidation string
	var lastCritic askcontract.CriticResponse
	bundle := askknowledge.Current()
	for attempt := 1; attempt <= attempts; attempt++ {
		currentPrompt := req.Prompt
		if attempt > 1 && lastValidation != "" {
			diags := askdiagnostic.FromValidationError(lastValidation, bundle)
			diags = append(diags, askdiagnostic.FromCritic(lastCritic)...)
			currentPrompt += "\n\nLocal validation failed. Fix the response and return full JSON again."
			currentPrompt += "\nValidator summary:\n" + summarizeValidationError(lastValidation)
			currentPrompt += "\nRaw validator error:\n" + strings.TrimSpace(lastValidation)
			for _, chunk := range askretrieve.RepairChunks(req.Prompt, lastValidation) {
				currentPrompt += "\n" + chunk.Content
			}
			logger.logf("debug", "\n[ask][phase:repair:diagnostics]\n%s\n", askdiagnostic.JSON(diags))
			currentPrompt += "\n" + askdiagnostic.RepairPromptBlock(diags)
		}
		logger.logf("basic", "[ask][phase:generation:attempt] attempt=%d/%d\n", attempt, attempts)
		logger.prompt("generation", req.SystemPrompt, currentPrompt)
		resp, err := client.Generate(ctx, askprovider.Request{
			Kind:         req.Kind,
			Provider:     req.Provider,
			Model:        req.Model,
			APIKey:       req.APIKey,
			OAuthToken:   req.OAuthToken,
			Endpoint:     req.Endpoint,
			SystemPrompt: req.SystemPrompt,
			Prompt:       currentPrompt,
			MaxRetries:   1,
		})
		if err != nil {
			return askcontract.GenerationResponse{}, lastValidation, lastCritic, attempt - 1, err
		}
		logger.response("generation", resp.Content)
		gen, err := askcontract.ParseGeneration(resp.Content)
		if err != nil {
			lastValidation = err.Error()
			logger.logf("debug", "[ask][phase:generation:parse-error] error=%s\n", lastValidation)
			if attempt < attempts {
				continue
			}
			return askcontract.GenerationResponse{}, lastValidation, lastCritic, attempt - 1, fmt.Errorf("ask generation returned invalid JSON: %s", lastValidation)
		}
		logger.logf("debug", "[ask][phase:semantic-validate] attempt=%d/%d\n", attempt, attempts)
		lintSummary, critic, err := validateGeneration(ctx, root, gen, decision, plan)
		lastCritic = critic
		if err == nil {
			return gen, lintSummary, critic, attempt - 1, nil
		}
		lastValidation = err.Error()
		logger.logf("debug", "[ask][phase:generation:validation-error] error=%s\n", lastValidation)
		if !repairableValidationError(lastValidation) {
			return askcontract.GenerationResponse{}, lastValidation, critic, attempt - 1, fmt.Errorf("ask generation stopped without repair: %s", lastValidation)
		}
	}
	if lastValidation == "" {
		lastValidation = "generation failed without a parseable response"
	}
	return askcontract.GenerationResponse{}, lastValidation, lastCritic, attempts - 1, fmt.Errorf("ask generation did not validate after %d attempts: %s", attempts, lastValidation)
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

func validateGeneration(ctx context.Context, root string, gen askcontract.GenerationResponse, decision askintent.Decision, plan askcontract.PlanResponse) (string, askcontract.CriticResponse, error) {
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
	for _, file := range gen.Files {
		if err := validateGeneratedFile(staged, file); err != nil {
			return "", askcontract.CriticResponse{Blocking: []string{err.Error()}, RequiredFixes: requiredFixesForValidation(err.Error())}, err
		}
		paths = append(paths, file.Path)
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
	critic := semanticCritic(gen, decision, plan)
	if len(critic.Blocking) > 0 {
		return "", critic, fmt.Errorf("semantic validation failed: %s", strings.Join(critic.Blocking, "; "))
	}
	return fmt.Sprintf("lint ok (%d workflows)", len(validated)), critic, nil
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
