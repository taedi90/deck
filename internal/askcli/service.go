package askcli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	lspaugment "github.com/taedi90/deck/internal/askaugment/lsp"
	mcpaugment "github.com/taedi90/deck/internal/askaugment/mcp"
	"github.com/taedi90/deck/internal/askconfig"
	"github.com/taedi90/deck/internal/askcontract"
	"github.com/taedi90/deck/internal/askhooks"
	"github.com/taedi90/deck/internal/askintent"
	"github.com/taedi90/deck/internal/askprovider"
	"github.com/taedi90/deck/internal/askretrieve"
	"github.com/taedi90/deck/internal/askreview"
	"github.com/taedi90/deck/internal/askstate"
	"github.com/taedi90/deck/internal/validate"
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
	logger := newAskLogger(opts.Stderr, effective.LogLevel)
	logger.logf("basic", "\n[ask][phase:request] routeCandidate=%s write=%t review=%t\n", heuristic.Route, opts.Write, opts.Review)
	logger.logf("basic", "[ask][config] provider=%s model=%s endpoint=%s apiKeySource=%s logLevel=%s\n", effective.Provider, effective.Model, effective.Endpoint, effective.APIKeySource, effective.LogLevel)
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
	if opts.Write && !decision.AllowGeneration && heuristic.AllowGeneration {
		logger.logf("debug", "[ask][phase:classify:override] from=%s to=%s reason=write-flag\n", decision.Route, heuristic.Route)
		decision = heuristic
		decision.Reason = "write flag overrides non-generation classification"
	}

	mcpChunks, mcpEvents := mcpaugment.Gather(ctx, effective.MCP, decision.Route, requestText)
	lspChunks, lspEvents := lspaugment.Gather(ctx, effective.LSP, decision.Target, workspace)
	externalChunks := append(append([]askretrieve.Chunk{}, mcpChunks...), lspChunks...)
	externalChunks = append(externalChunks, projectContextChunk(resolvedRoot))
	retrieval := askretrieve.Retrieve(decision.Route, requestText, decision.Target, workspace, state, externalChunks)
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
		return fmt.Errorf("missing ask api key for provider %q; set %s or run `deck ask auth set --api-key ...`", effective.Provider, "DECK_ASK_API_KEY")
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
		planned, planErr := planWithLLM(ctx, client, askconfigSettings{provider: effective.Provider, model: effective.Model, apiKey: effective.APIKey, endpoint: effective.Endpoint}, decision, retrieval, requestText, workspace, logger)
		if planErr != nil {
			logger.logf("debug", "[ask][phase:plan:fallback] error=%v\n", planErr)
			planned = localPlan(requestText, decision, workspace)
		}
		plan = planned
		result.Plan = &plan
		logger.logf("basic", "[ask][phase:plan:done] files=%d blockers=%d\n", len(plan.Files), len(plan.Blockers))
		planMD := renderPlanMarkdown(plan, ".deck/plan/latest.md")
		planMDPath, planJSONPath, saveErr := savePlanArtifacts(resolvedRoot, opts, plan, planMD)
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
		generationRequest := askprovider.Request{
			Kind:         "generate",
			Provider:     effective.Provider,
			Model:        effective.Model,
			APIKey:       effective.APIKey,
			Endpoint:     effective.Endpoint,
			SystemPrompt: generationSystemPrompt(decision.Route, decision.Target, retrieval),
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

func canUseLLM(cfg askconfig.EffectiveSettings) bool {
	return !askconfig.NeedsAPIKey(cfg.Provider) || strings.TrimSpace(cfg.APIKey) != ""
}

func classifyWithLLM(ctx context.Context, client askprovider.Client, cfg askconfig.EffectiveSettings, systemPrompt string, userPrompt string, logger askLogger) (askintent.Decision, error) {
	logger.prompt("classifier", systemPrompt, userPrompt)
	request := askprovider.Request{
		Kind:         "classify",
		Provider:     cfg.Provider,
		Model:        cfg.Model,
		APIKey:       cfg.APIKey,
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
	return 2
}

func generateWithValidation(ctx context.Context, client askprovider.Client, req askprovider.Request, root string, attempts int, logger askLogger, decision askintent.Decision, plan askcontract.PlanResponse) (askcontract.GenerationResponse, string, askcontract.CriticResponse, int, error) {
	var lastValidation string
	var lastCritic askcontract.CriticResponse
	for attempt := 1; attempt <= attempts; attempt++ {
		currentPrompt := req.Prompt
		if attempt > 1 && lastValidation != "" {
			currentPrompt += "\n\nLocal validation failed. Fix the response and return full JSON again."
			if len(lastCritic.Blocking) > 0 || len(lastCritic.Advisory) > 0 {
				logger.logf("debug", "\n[ask][phase:repair:critic]\n%s\n", criticJSON(lastCritic))
				currentPrompt += "\nBlocking and advisory feedback as JSON:\n" + criticJSON(lastCritic)
			} else {
				currentPrompt += "\nErrors:\n" + lastValidation
			}
		}
		logger.logf("basic", "[ask][phase:generation:attempt] attempt=%d/%d\n", attempt, attempts)
		logger.prompt("generation", req.SystemPrompt, currentPrompt)
		resp, err := client.Generate(ctx, askprovider.Request{
			Kind:         req.Kind,
			Provider:     req.Provider,
			Model:        req.Model,
			APIKey:       req.APIKey,
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
		lintSummary, critic, err := validateGeneration(root, gen, decision, plan)
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

func validateGeneration(root string, gen askcontract.GenerationResponse, decision askintent.Decision, plan askcontract.PlanResponse) (string, askcontract.CriticResponse, error) {
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
			return "", askcontract.CriticResponse{Blocking: []string{err.Error()}, RequiredFixes: []string{"Return only schema-valid files under allowed workflow paths"}}, err
		}
		paths = append(paths, file.Path)
	}
	entrypoints := scenarioPaths(staged, paths)
	validated := make([]string, 0, len(entrypoints))
	for _, path := range entrypoints {
		files, err := validate.Entrypoint(path)
		if err != nil {
			return "", askcontract.CriticResponse{Blocking: []string{err.Error()}, RequiredFixes: []string{"Fix workflow lint and schema errors"}}, err
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
