package askcli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

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
	"github.com/taedi90/deck/internal/fsutil"
	"github.com/taedi90/deck/internal/validate"
	"github.com/taedi90/deck/internal/workspacepaths"
)

type Options struct {
	Root          string
	Prompt        string
	FromPath      string
	Write         bool
	Review        bool
	MaxIterations int
	Provider      string
	Model         string
	Endpoint      string
	Stdout        io.Writer
	Stderr        io.Writer
}

type runResult struct {
	Route         askintent.Route
	Target        askintent.Target
	Confidence    float64
	Reason        string
	Summary       string
	Answer        string
	ReviewLines   []string
	LintSummary   string
	LocalFindings []askreview.Finding
	Files         []askcontract.GeneratedFile
	WroteFiles    bool
	RetriesUsed   int
	LLMUsed       bool
	ClassifierLLM bool
	Termination   string
	Chunks        []askretrieve.Chunk
	DroppedChunks []string
	AugmentEvents []string
	UserCommand   string
	PromptTraces  []promptTrace
	ConfigSource  askconfig.EffectiveSettings
}

type promptTrace struct {
	Label        string
	SystemPrompt string
	UserPrompt   string
}

type askLogger struct {
	writer io.Writer
	level  string
}

func newAskLogger(writer io.Writer, level string) askLogger {
	if writer == nil {
		writer = io.Discard
	}
	return askLogger{writer: writer, level: askconfigLogLevel(level)}
}

func (l askLogger) enabled(required string) bool {
	return shouldLogAsk(l.level, required)
}

func (l askLogger) logf(required string, format string, args ...any) {
	if !l.enabled(required) {
		return
	}
	_, _ = fmt.Fprintf(l.writer, format, args...)
}

func (l askLogger) prompt(label string, systemPrompt string, userPrompt string) {
	if !l.enabled("trace") {
		return
	}
	l.logf("trace", "deck ask %s system-prompt:\n%s\n", label, strings.TrimSpace(systemPrompt))
	l.logf("trace", "deck ask %s user-prompt:\n%s\n", label, strings.TrimSpace(userPrompt))
}

func (l askLogger) response(label string, content string) {
	if !l.enabled("trace") {
		return
	}
	l.logf("trace", "deck ask %s raw-response:\n%s\n", label, strings.TrimSpace(content))
}

func Execute(ctx context.Context, opts Options, client askprovider.Client) error {
	if client == nil {
		return fmt.Errorf("ask backend is not configured")
	}
	hooks := askhooks.Default()
	resolvedRoot, err := filepath.Abs(strings.TrimSpace(opts.Root))
	if err != nil {
		return fmt.Errorf("resolve workspace root: %w", err)
	}
	requestText, err := loadRequestText(strings.TrimSpace(opts.Prompt), strings.TrimSpace(opts.FromPath))
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
	logger.logf("basic", "deck ask phase=request routeCandidate=%s write=%t review=%t\n", heuristic.Route, opts.Write, opts.Review)
	logger.logf("basic", "deck ask using provider=%s model=%s endpoint=%s apiKeySource=%s logLevel=%s\n", effective.Provider, effective.Model, effective.Endpoint, effective.APIKeySource, effective.LogLevel)
	logger.logf("debug", "deck ask command=%s\n", renderUserCommand(opts))
	logger.logf("trace", "deck ask user-request:\n%s\n", strings.TrimSpace(requestText))
	decision := heuristic
	classifierLLM := false
	classifierSystem := classifierSystemPrompt()
	classifierUser := classifierUserPrompt(requestText, opts.Review, workspace)
	if canUseLLM(effective) {
		logger.logf("debug", "deck ask phase=classify provider=%s model=%s\n", effective.Provider, effective.Model)
		classified, classifyErr := classifyWithLLM(ctx, client, effective, classifierSystem, classifierUser, logger)
		if classifyErr == nil {
			decision = classified
			classifierLLM = true
			logger.logf("basic", "deck ask phase=classify-complete route=%s confidence=%.2f reason=%s\n", decision.Route, decision.Confidence, decision.Reason)
		} else {
			logger.logf("debug", "deck ask phase=classify-fallback error=%v\n", classifyErr)
		}
	} else {
		logger.logf("debug", "deck ask phase=classify-skip reason=no-llm-credentials\n")
	}
	if opts.Write && !decision.AllowGeneration && heuristic.AllowGeneration {
		logger.logf("debug", "deck ask phase=classify-override from=%s to=%s reason=write-flag\n", decision.Route, heuristic.Route)
		decision = heuristic
		decision.Reason = "write flag overrides non-generation classification"
	}

	mcpChunks, mcpEvents := mcpaugment.Gather(ctx, effective.MCP, decision.Route, requestText)
	lspChunks, lspEvents := lspaugment.Gather(ctx, effective.LSP, decision.Target, workspace)
	externalChunks := append(append([]askretrieve.Chunk{}, mcpChunks...), lspChunks...)
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

	logger.logf("debug", "deck ask phase=augment-start mcp=%t lsp=%t\n", effective.MCP.Enabled, effective.LSP.Enabled)
	for _, event := range result.AugmentEvents {
		prefix := "augment"
		if strings.HasPrefix(event, "mcp:") {
			prefix = "mcp"
		} else if strings.HasPrefix(event, "lsp") {
			prefix = "lsp"
		}
		logger.logf("debug", "deck ask %s=%s\n", prefix, event)
	}
	logger.logf("debug", "deck ask phase=retrieval chunks=%d dropped=%d\n", len(result.Chunks), len(result.DroppedChunks))

	if decision.LLMPolicy == askintent.LLMRequired && !canUseLLM(effective) {
		return fmt.Errorf("missing ask api key for provider %q; set %s or run `deck ask auth set --api-key ...`", effective.Provider, "DECK_ASK_API_KEY")
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
		logger.logf("basic", "deck ask phase=generation-start route=%s attempts=%d\n", decision.Route, attempts)
		gen, lintSummary, retriesUsed, genErr := generateWithValidation(ctx, client, generationRequest, resolvedRoot, attempts, logger)
		if genErr != nil {
			return genErr
		}
		logger.logf("basic", "deck ask phase=generation-complete files=%d lint=%s\n", len(gen.Files), lintSummary)
		result.LLMUsed = true
		result.RetriesUsed = retriesUsed
		result.Files = gen.Files
		result.Summary = gen.Summary
		result.ReviewLines = append(result.ReviewLines, gen.Review...)
		result.LintSummary = lintSummary
		result.LocalFindings = localFindings(result.Files)
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
			logger.logf("basic", "deck ask phase=answer-start route=%s\n", decision.Route)
			info, infoErr := answerWithLLM(ctx, client, effective, decision, retrieval, requestText, logger)
			if infoErr == nil {
				result.LLMUsed = true
				result.Summary = info.Summary
				result.Answer = info.Answer
				result.ReviewLines = append(result.ReviewLines, info.Suggestions...)
				result.ReviewLines = append(result.ReviewLines, info.Findings...)
				result.ReviewLines = append(result.ReviewLines, info.SuggestedChange...)
				logger.logf("basic", "deck ask phase=answer-complete route=%s\n", decision.Route)
			} else {
				result.ReviewLines = append(result.ReviewLines, "LLM response failed; using local fallback: "+infoErr.Error())
				logger.logf("debug", "deck ask phase=answer-fallback error=%v\n", infoErr)
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

func applyLocalFallback(result *runResult, root string, workspace askretrieve.WorkspaceSummary, prompt string) {
	switch result.Route {
	case askintent.RouteReview:
		result.Summary = "Workspace review"
		result.LocalFindings = askreview.Workspace(root)
		result.ReviewLines = append(result.ReviewLines, findingsToLines(result.LocalFindings)...)
		result.Termination = "reviewed-locally"
	case askintent.RouteExplain:
		result.Summary, result.Answer = localExplain(workspace, prompt, result.Target)
		result.Termination = "explained-locally"
	case askintent.RouteQuestion:
		result.Summary = "Question received"
		result.Answer = "I need model access for a complete answer."
		result.ReviewLines = append(result.ReviewLines, clarificationSuggestions()...)
		result.Termination = "answered-locally"
	default:
		result.Summary = localClarify(prompt)
		result.ReviewLines = append(result.ReviewLines, clarificationSuggestions()...)
		result.Termination = "clarified"
	}
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

func generateWithValidation(ctx context.Context, client askprovider.Client, req askprovider.Request, root string, attempts int, logger askLogger) (askcontract.GenerationResponse, string, int, error) {
	var lastValidation string
	for attempt := 1; attempt <= attempts; attempt++ {
		currentPrompt := req.Prompt
		if attempt > 1 && lastValidation != "" {
			currentPrompt += "\n\nLocal validation failed. Fix the response and return full JSON again. Errors:\n" + lastValidation
		}
		logger.logf("basic", "deck ask phase=generation-attempt attempt=%d/%d\n", attempt, attempts)
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
			return askcontract.GenerationResponse{}, lastValidation, attempt - 1, err
		}
		logger.response("generation", resp.Content)
		gen, err := askcontract.ParseGeneration(resp.Content)
		if err != nil {
			lastValidation = err.Error()
			logger.logf("debug", "deck ask phase=generation-parse-error error=%s\n", lastValidation)
			return askcontract.GenerationResponse{}, lastValidation, attempt - 1, fmt.Errorf("ask generation returned invalid JSON: %s", lastValidation)
		}
		lintSummary, err := validateGeneration(root, gen)
		if err == nil {
			return gen, lintSummary, attempt - 1, nil
		}
		lastValidation = err.Error()
		logger.logf("debug", "deck ask phase=generation-validation-error error=%s\n", lastValidation)
		if !repairableValidationError(lastValidation) {
			return askcontract.GenerationResponse{}, lastValidation, attempt - 1, fmt.Errorf("ask generation stopped without repair: %s", lastValidation)
		}
	}
	if lastValidation == "" {
		lastValidation = "generation failed without a parseable response"
	}
	return askcontract.GenerationResponse{}, lastValidation, attempts - 1, fmt.Errorf("ask generation did not validate after %d attempts: %s", attempts, lastValidation)
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

func validateGeneration(root string, gen askcontract.GenerationResponse) (string, error) {
	if len(gen.Files) == 0 {
		return "", fmt.Errorf("response did not include any files")
	}
	staged, err := stageWorkspace(root, gen.Files)
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(staged) }()
	paths := make([]string, 0, len(gen.Files))
	for _, file := range gen.Files {
		if err := validateGeneratedFile(staged, file); err != nil {
			return "", err
		}
		paths = append(paths, file.Path)
	}
	entrypoints := scenarioPaths(staged, paths)
	validated := make([]string, 0, len(entrypoints))
	for _, path := range entrypoints {
		files, err := validate.Entrypoint(path)
		if err != nil {
			return "", err
		}
		validated = append(validated, files...)
	}
	validated = dedupe(validated)
	return fmt.Sprintf("lint ok (%d workflows)", len(validated)), nil
}

func classifierSystemPrompt() string {
	return strings.Join([]string{
		"You are a classifier for deck ask.",
		"Return strict JSON only.",
		"Valid route values: clarify, question, explain, review, refine, draft.",
		"Only choose draft/refine when user clearly asks to create or modify workflow files.",
		"When user asks analyze/explain/summarize existing scenario, choose explain or review.",
		"Include target.kind (workspace|scenario|component|vars|unknown) and optional target.path/name when inferable.",
		"JSON shape: {\"route\":string,\"confidence\":number,\"reason\":string,\"target\":{\"kind\":string,\"path\":string,\"name\":string},\"generationAllowed\":boolean}",
	}, "\n")
}

func classifierUserPrompt(prompt string, reviewFlag bool, workspace askretrieve.WorkspaceSummary) string {
	b := &strings.Builder{}
	b.WriteString("User prompt:\n")
	b.WriteString(strings.TrimSpace(prompt))
	b.WriteString("\n")
	_, _ = fmt.Fprintf(b, "review flag: %t\n", reviewFlag)
	_, _ = fmt.Fprintf(b, "has workflow tree: %t\n", workspace.HasWorkflowTree)
	_, _ = fmt.Fprintf(b, "has prepare scenario: %t\n", workspace.HasPrepare)
	_, _ = fmt.Fprintf(b, "has apply scenario: %t\n", workspace.HasApply)
	b.WriteString("workspace files:\n")
	for _, file := range workspace.Files {
		b.WriteString("- ")
		b.WriteString(file.Path)
		b.WriteString("\n")
	}
	return b.String()
}

func generationSystemPrompt(route askintent.Route, target askintent.Target, retrieval askretrieve.RetrievalResult) string {
	b := &strings.Builder{}
	b.WriteString("You are deck ask, a workflow authoring assistant.\n")
	b.WriteString("Route: ")
	b.WriteString(string(route))
	b.WriteString("\n")
	b.WriteString("Target kind: ")
	b.WriteString(target.Kind)
	b.WriteString("\n")
	if target.Path != "" {
		b.WriteString("Target path: ")
		b.WriteString(target.Path)
		b.WriteString("\n")
	}
	b.WriteString("Rules:\n")
	b.WriteString("- Produce only strict JSON.\n")
	b.WriteString("- JSON shape: {\"summary\":string,\"review\":[]string,\"files\":[{\"path\":string,\"content\":string}]}.\n")
	b.WriteString("- Allowed paths: workflows/scenarios/*.yaml, workflows/components/*.yaml, workflows/vars.yaml.\n")
	b.WriteString("- Every workflow YAML must be schema-valid. Scenario files need top-level role and version.\n")
	b.WriteString("- Use version: v1alpha1 for generated workflow files unless the workspace clearly uses something else.\n")
	b.WriteString("- A workflow must define at least one of artifacts, phases, or steps.\n")
	b.WriteString("- Each step must contain id, kind, and spec. Command steps must use spec.command as a YAML list of arguments.\n")
	b.WriteString("- Never place summary, description, or review fields inside workflow YAML content.\n")
	b.WriteString("- For a new workspace draft, prefer creating workflows/scenarios/apply.yaml and workflows/vars.yaml only when needed.\n")
	b.WriteString("- Prefer typed steps over Command.\n")
	b.WriteString("- If the request is simply to print text in the terminal, a minimal valid apply scenario with one Command step is acceptable.\n")
	b.WriteString("- Do not invent unsupported fields.\n")
	b.WriteString("Retrieved context follows.\n")
	b.WriteString(askretrieve.BuildChunkText(retrieval))
	return b.String()
}

func infoPrompts(route askintent.Route, target askintent.Target, retrieval askretrieve.RetrievalResult, prompt string) (string, string) {
	switch route {
	case askintent.RouteExplain:
		return explainSystemPrompt(target, retrieval), explainUserPrompt(prompt, target)
	case askintent.RouteReview:
		return reviewSystemPrompt(target, retrieval), reviewUserPrompt(prompt, target)
	case askintent.RouteQuestion:
		return questionSystemPrompt(target, retrieval), questionUserPrompt(prompt, target)
	default:
		return infoSystemPrompt(route, target, retrieval), infoUserPrompt(prompt, route, target)
	}
}

func generationUserPrompt(workspace askretrieve.WorkspaceSummary, state askstate.Context, prompt string, fromLabel string, route askintent.Route) string {
	b := &strings.Builder{}
	b.WriteString("Workspace root: ")
	b.WriteString(workspace.Root)
	b.WriteString("\n")
	_, _ = fmt.Fprintf(b, "Has workflow tree: %t\n", workspace.HasWorkflowTree)
	_, _ = fmt.Fprintf(b, "Has prepare scenario: %t\n", workspace.HasPrepare)
	_, _ = fmt.Fprintf(b, "Has apply scenario: %t\n", workspace.HasApply)
	b.WriteString("Route: ")
	b.WriteString(string(route))
	b.WriteString("\n")
	if state.LastLint != "" {
		b.WriteString("Last lint summary: ")
		b.WriteString(state.LastLint)
		b.WriteString("\n")
	}
	if fromLabel != "" {
		b.WriteString("Attached request source: ")
		b.WriteString(fromLabel)
		b.WriteString("\n")
	}
	b.WriteString("User request:\n")
	b.WriteString(strings.TrimSpace(prompt))
	b.WriteString("\n")
	if !workspace.HasWorkflowTree && route == askintent.RouteDraft {
		b.WriteString("This is an empty workspace. Return the minimum valid starter workflow files needed to satisfy the request.\n")
		b.WriteString("At minimum, the result should usually include a valid workflows/scenarios/apply.yaml file.\n")
	}
	b.WriteString("Return the minimum complete file set needed for this request.\n")
	return b.String()
}

func infoSystemPrompt(route askintent.Route, target askintent.Target, retrieval askretrieve.RetrievalResult) string {
	b := &strings.Builder{}
	b.WriteString("You are deck ask.\n")
	b.WriteString("Route: ")
	b.WriteString(string(route))
	b.WriteString("\n")
	b.WriteString("Target kind: ")
	b.WriteString(target.Kind)
	b.WriteString("\n")
	if target.Path != "" {
		b.WriteString("Target path: ")
		b.WriteString(target.Path)
		b.WriteString("\n")
	}
	b.WriteString("Return strict JSON with shape {\"summary\":string,\"answer\":string,\"suggestions\":[]string,\"findings\":[]string,\"suggestedChanges\":[]string}.\n")
	b.WriteString("Do not return file content for this route.\n")
	b.WriteString(askretrieve.BuildChunkText(retrieval))
	return b.String()
}

func questionSystemPrompt(target askintent.Target, retrieval askretrieve.RetrievalResult) string {
	b := &strings.Builder{}
	b.WriteString("You are deck ask answering a workflow question.\n")
	b.WriteString("Answer the user's question directly and use retrieved evidence.\n")
	b.WriteString("Keep the answer concise but specific.\n")
	b.WriteString("Return strict JSON with shape {\"summary\":string,\"answer\":string,\"suggestions\":[]string}.\n")
	b.WriteString("If evidence is incomplete, say what is known from the workspace and avoid speculation.\n")
	if target.Path != "" {
		b.WriteString("Target path: ")
		b.WriteString(target.Path)
		b.WriteString("\n")
	}
	b.WriteString(askretrieve.BuildChunkText(retrieval))
	return b.String()
}

func explainSystemPrompt(target askintent.Target, retrieval askretrieve.RetrievalResult) string {
	b := &strings.Builder{}
	b.WriteString("You are deck ask explaining an existing deck workspace file or workflow.\n")
	b.WriteString("Explain what the target does, how it fits into the workflow, and call out imports, phases, major step kinds, and Command usage when present.\n")
	b.WriteString("Do not give a shallow file count summary.\n")
	b.WriteString("Return strict JSON with shape {\"summary\":string,\"answer\":string,\"suggestions\":[]string}.\n")
	if target.Path != "" {
		b.WriteString("Target path: ")
		b.WriteString(target.Path)
		b.WriteString("\n")
	}
	b.WriteString(askretrieve.BuildChunkText(retrieval))
	return b.String()
}

func reviewSystemPrompt(target askintent.Target, retrieval askretrieve.RetrievalResult) string {
	b := &strings.Builder{}
	b.WriteString("You are deck ask reviewing an existing deck workspace.\n")
	b.WriteString("Use the retrieved evidence and any local findings to produce a scoped review with practical concerns and suggested changes.\n")
	b.WriteString("Narrate the findings instead of only repeating raw warnings.\n")
	b.WriteString("Return strict JSON with shape {\"summary\":string,\"answer\":string,\"findings\":[]string,\"suggestedChanges\":[]string}.\n")
	if target.Path != "" {
		b.WriteString("Target path: ")
		b.WriteString(target.Path)
		b.WriteString("\n")
	}
	b.WriteString(askretrieve.BuildChunkText(retrieval))
	return b.String()
}

func infoUserPrompt(prompt string, route askintent.Route, target askintent.Target) string {
	b := &strings.Builder{}
	b.WriteString("Route: ")
	b.WriteString(string(route))
	b.WriteString("\n")
	if target.Path != "" {
		b.WriteString("Target path: ")
		b.WriteString(target.Path)
		b.WriteString("\n")
	}
	b.WriteString("User request:\n")
	b.WriteString(strings.TrimSpace(prompt))
	return b.String()
}

func questionUserPrompt(prompt string, target askintent.Target) string {
	b := &strings.Builder{}
	if target.Path != "" {
		b.WriteString("Target path: ")
		b.WriteString(target.Path)
		b.WriteString("\n")
	}
	b.WriteString("User question:\n")
	b.WriteString(strings.TrimSpace(prompt))
	return b.String()
}

func explainUserPrompt(prompt string, target askintent.Target) string {
	b := &strings.Builder{}
	if target.Path != "" {
		b.WriteString("Explain target: ")
		b.WriteString(target.Path)
		b.WriteString("\n")
	}
	b.WriteString("User request:\n")
	b.WriteString(strings.TrimSpace(prompt))
	return b.String()
}

func reviewUserPrompt(prompt string, target askintent.Target) string {
	b := &strings.Builder{}
	if target.Path != "" {
		b.WriteString("Review target: ")
		b.WriteString(target.Path)
		b.WriteString("\n")
	}
	b.WriteString("User request:\n")
	b.WriteString(strings.TrimSpace(prompt))
	b.WriteString("\nProvide a scoped review with concrete suggested changes.")
	return b.String()
}

func localClarify(prompt string) string {
	if strings.TrimSpace(prompt) == "" {
		return "Your request is empty. Please describe what you want to do."
	}
	return "Your request is too ambiguous to start workflow generation."
}

func clarificationSuggestions() []string {
	return []string{
		"Try: deck ask \"rhel9 single-node kubeadm cluster scenario\"",
		"Try: deck ask --review",
		"Try: deck ask \"explain what workflows/scenarios/apply.yaml does\"",
	}
}

func localExplain(workspace askretrieve.WorkspaceSummary, prompt string, target askintent.Target) (string, string) {
	resolved := resolveExplainTarget(workspace, target, prompt)
	if resolved.Path != "" {
		if summary, answer := explainWorkspaceFile(workspace, resolved); strings.TrimSpace(answer) != "" {
			return summary, answer
		}
	}
	b := &strings.Builder{}
	b.WriteString("Workspace summary:\n")
	_, _ = fmt.Fprintf(b, "- workflow tree: %t\n", workspace.HasWorkflowTree)
	_, _ = fmt.Fprintf(b, "- prepare scenario: %t\n", workspace.HasPrepare)
	_, _ = fmt.Fprintf(b, "- apply scenario: %t\n", workspace.HasApply)
	_, _ = fmt.Fprintf(b, "- relevant files: %d\n", len(workspace.Files))
	if resolved.Path != "" {
		b.WriteString("Target: ")
		b.WriteString(resolved.Path)
		b.WriteString("\n")
	}
	if strings.TrimSpace(prompt) != "" {
		b.WriteString("Prompt interpreted as explain request for current workspace.\n")
	}
	return "Workspace explanation", strings.TrimSpace(b.String())
}

func resolveExplainTarget(workspace askretrieve.WorkspaceSummary, target askintent.Target, prompt string) askintent.Target {
	if target.Path != "" {
		return target
	}
	lowerPrompt := strings.ToLower(strings.TrimSpace(prompt))
	for _, file := range workspace.Files {
		lowerPath := strings.ToLower(file.Path)
		base := strings.ToLower(filepath.Base(file.Path))
		name := strings.TrimSuffix(base, filepath.Ext(base))
		if strings.Contains(lowerPrompt, lowerPath) || strings.Contains(lowerPrompt, base) || (name != "" && strings.Contains(lowerPrompt, name)) {
			kind := "component"
			switch {
			case strings.HasPrefix(filepath.ToSlash(file.Path), "workflows/scenarios/"):
				kind = "scenario"
			case filepath.ToSlash(file.Path) == "workflows/vars.yaml":
				kind = "vars"
			}
			return askintent.Target{Kind: kind, Path: file.Path, Name: name}
		}
	}
	return target
}

func explainWorkspaceFile(workspace askretrieve.WorkspaceSummary, target askintent.Target) (string, string) {
	for _, file := range workspace.Files {
		if filepath.ToSlash(file.Path) != filepath.ToSlash(target.Path) {
			continue
		}
		return describeWorkspaceFile(workspace, file)
	}
	return "", ""
}

func describeWorkspaceFile(workspace askretrieve.WorkspaceSummary, file askretrieve.WorkspaceFile) (string, string) {
	cleanPath := filepath.ToSlash(file.Path)
	if cleanPath == "workflows/vars.yaml" {
		return describeVarsFile(file)
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(file.Content), &doc); err != nil {
		return filepath.Base(file.Path) + " explanation", fmt.Sprintf("%s exists, but it could not be parsed locally: %v", file.Path, err)
	}
	if strings.HasPrefix(cleanPath, "workflows/scenarios/") {
		return describeScenarioFile(workspace, file, doc)
	}
	if strings.HasPrefix(cleanPath, "workflows/components/") {
		return describeComponentFile(file, doc)
	}
	return filepath.Base(file.Path) + " explanation", fmt.Sprintf("%s is present in the workspace.", file.Path)
}

func describeVarsFile(file askretrieve.WorkspaceFile) (string, string) {
	var vars map[string]any
	if err := yaml.Unmarshal([]byte(file.Content), &vars); err != nil {
		return "Vars explanation", fmt.Sprintf("%s stores workspace variables, but it could not be parsed locally: %v", file.Path, err)
	}
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	b := &strings.Builder{}
	_, _ = fmt.Fprintf(b, "%s defines %d workspace variables.", file.Path, len(keys))
	if len(keys) > 0 {
		b.WriteString(" Keys: ")
		b.WriteString(strings.Join(keys, ", "))
		b.WriteString(".")
	}
	return "Vars explanation", strings.TrimSpace(b.String())
}

func describeScenarioFile(workspace askretrieve.WorkspaceSummary, file askretrieve.WorkspaceFile, doc map[string]any) (string, string) {
	role, _ := doc["role"].(string)
	version, _ := doc["version"].(string)
	phaseNames := make([]string, 0)
	imports := make([]string, 0)
	stepKinds := make(map[string]int)
	commandCount := 0
	if phases, ok := doc["phases"].([]any); ok {
		for _, rawPhase := range phases {
			phase, ok := rawPhase.(map[string]any)
			if !ok {
				continue
			}
			if name, _ := phase["name"].(string); strings.TrimSpace(name) != "" {
				phaseNames = append(phaseNames, name)
			}
			imports = append(imports, collectImports(phase)...)
			stepKinds, commandCount = collectStepKinds(phase, stepKinds, commandCount)
		}
	}
	b := &strings.Builder{}
	_, _ = fmt.Fprintf(b, "%s is a scenario workflow", file.Path)
	if role != "" {
		_, _ = fmt.Fprintf(b, " with role %q", role)
	}
	if version != "" {
		_, _ = fmt.Fprintf(b, " and version %q", version)
	}
	b.WriteString(". ")
	if len(phaseNames) > 0 {
		b.WriteString("It defines phases: ")
		b.WriteString(strings.Join(phaseNames, ", "))
		b.WriteString(". ")
	}
	if len(imports) > 0 {
		b.WriteString("It imports components: ")
		b.WriteString(strings.Join(dedupe(imports), ", "))
		b.WriteString(". ")
	}
	stepSummary := formatStepKinds(stepKinds)
	if stepSummary != "" {
		b.WriteString("Inline steps use: ")
		b.WriteString(stepSummary)
		b.WriteString(". ")
	}
	if commandCount > 0 {
		_, _ = fmt.Fprintf(b, "There are %d inline Command step(s), which may deserve extra review for shell complexity. ", commandCount)
	}
	switch role {
	case "apply":
		b.WriteString("This file fits into the apply path for executing host changes in phase order. ")
	case "prepare":
		b.WriteString("This file fits into the prepare path for assembling offline artifacts and package inputs. ")
	}
	for _, importPath := range dedupe(imports) {
		resolved := "workflows/components/" + strings.TrimPrefix(filepath.ToSlash(importPath), "./")
		for _, related := range workspace.Files {
			if filepath.ToSlash(related.Path) == resolved {
				b.WriteString("Related component available: ")
				b.WriteString(resolved)
				b.WriteString(". ")
				break
			}
		}
	}
	return filepath.Base(file.Path) + " explanation", strings.TrimSpace(b.String())
}

func describeComponentFile(file askretrieve.WorkspaceFile, doc map[string]any) (string, string) {
	stepKinds := make(map[string]int)
	commandCount := 0
	stepIDs := make([]string, 0)
	if steps, ok := doc["steps"].([]any); ok {
		for _, rawStep := range steps {
			step, ok := rawStep.(map[string]any)
			if !ok {
				continue
			}
			if id, _ := step["id"].(string); strings.TrimSpace(id) != "" {
				stepIDs = append(stepIDs, id)
			}
			stepKinds, commandCount = collectStepKinds(step, stepKinds, commandCount)
		}
	}
	b := &strings.Builder{}
	_, _ = fmt.Fprintf(b, "%s is a reusable component with %d step(s). ", file.Path, len(stepIDs))
	if len(stepIDs) > 0 {
		b.WriteString("Step ids: ")
		b.WriteString(strings.Join(stepIDs, ", "))
		b.WriteString(". ")
	}
	if stepSummary := formatStepKinds(stepKinds); stepSummary != "" {
		b.WriteString("Step kinds: ")
		b.WriteString(stepSummary)
		b.WriteString(". ")
	}
	if commandCount > 0 {
		_, _ = fmt.Fprintf(b, "It contains %d Command step(s). ", commandCount)
	}
	return filepath.Base(file.Path) + " explanation", strings.TrimSpace(b.String())
}

func collectImports(phase map[string]any) []string {
	imports := make([]string, 0)
	rawImports, ok := phase["imports"].([]any)
	if !ok {
		return imports
	}
	for _, rawImport := range rawImports {
		entry, ok := rawImport.(map[string]any)
		if !ok {
			continue
		}
		path, _ := entry["path"].(string)
		if strings.TrimSpace(path) != "" {
			imports = append(imports, filepath.ToSlash(path))
		}
	}
	return imports
}

func collectStepKinds(scope map[string]any, stepKinds map[string]int, commandCount int) (map[string]int, int) {
	rawSteps, ok := scope["steps"].([]any)
	if !ok {
		return stepKinds, commandCount
	}
	for _, rawStep := range rawSteps {
		step, ok := rawStep.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := step["kind"].(string)
		kind = strings.TrimSpace(kind)
		if kind == "" {
			kind = "unknown"
		}
		stepKinds[kind]++
		if kind == "Command" {
			commandCount++
		}
	}
	return stepKinds, commandCount
}

func formatStepKinds(stepKinds map[string]int) string {
	if len(stepKinds) == 0 {
		return ""
	}
	kinds := make([]string, 0, len(stepKinds))
	for kind := range stepKinds {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	parts := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		parts = append(parts, fmt.Sprintf("%s x%d", kind, stepKinds[kind]))
	}
	return strings.Join(parts, ", ")
}

func requestSpecificEnough(prompt string) bool {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return false
	}
	if strings.Contains(prompt, "workflows/") {
		return true
	}
	words := strings.Fields(prompt)
	if len(words) >= 4 {
		return true
	}
	lower := strings.ToLower(prompt)
	keywords := []string{"apply", "prepare", "component", "vars", "scenario", "workflow", "cluster", "kubeadm", "improve", "refine", "draft"}
	for _, keyword := range keywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}

func chunkIDsBySource(chunks []askretrieve.Chunk, source string) []string {
	ids := make([]string, 0)
	for _, chunk := range chunks {
		if chunk.Source == source {
			ids = append(ids, chunk.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

func findingsToLines(findings []askreview.Finding) []string {
	if len(findings) == 0 {
		return []string{"No local style findings detected."}
	}
	out := make([]string, 0, len(findings))
	for _, finding := range findings {
		out = append(out, fmt.Sprintf("[%s] %s", finding.Severity, finding.Message))
	}
	return out
}

func resultToMarkdown(result runResult) string {
	b := &strings.Builder{}
	b.WriteString("# ask result\n\n")
	b.WriteString("- route: ")
	b.WriteString(string(result.Route))
	b.WriteString("\n")
	_, _ = fmt.Fprintf(b, "- confidence: %.2f\n", result.Confidence)
	b.WriteString("- reason: ")
	b.WriteString(result.Reason)
	b.WriteString("\n")
	b.WriteString("- termination: ")
	b.WriteString(result.Termination)
	b.WriteString("\n")
	if result.Target.Path != "" {
		b.WriteString("- target: ")
		b.WriteString(result.Target.Path)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(result.Answer)
	b.WriteString("\n")
	return b.String()
}

func render(stdout io.Writer, stderr io.Writer, result runResult) error {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if _, err := fmt.Fprintf(stdout, "ask: %s\n", result.Summary); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "route: %s (confidence %.2f)\n", result.Route, result.Confidence); err != nil {
		return err
	}
	if result.Target.Path != "" {
		if _, err := fmt.Fprintf(stdout, "target: %s\n", result.Target.Path); err != nil {
			return err
		}
	}
	if result.Answer != "" {
		if _, err := fmt.Fprintf(stdout, "answer: %s\n", result.Answer); err != nil {
			return err
		}
	}
	if result.LintSummary != "" {
		if _, err := fmt.Fprintf(stdout, "lint: %s\n", result.LintSummary); err != nil {
			return err
		}
	}
	if len(result.ReviewLines) > 0 {
		if _, err := io.WriteString(stdout, "notes:\n"); err != nil {
			return err
		}
		for _, line := range result.ReviewLines {
			if _, err := fmt.Fprintf(stdout, "- %s\n", line); err != nil {
				return err
			}
		}
	}
	if len(result.LocalFindings) > 0 {
		if _, err := io.WriteString(stdout, "local-findings:\n"); err != nil {
			return err
		}
		for _, finding := range result.LocalFindings {
			if _, err := fmt.Fprintf(stdout, "- [%s] %s\n", finding.Severity, finding.Message); err != nil {
				return err
			}
		}
	}
	if len(result.AugmentEvents) > 0 {
		if _, err := io.WriteString(stdout, "augment:\n"); err != nil {
			return err
		}
		for _, event := range result.AugmentEvents {
			if _, err := fmt.Fprintf(stdout, "- %s\n", event); err != nil {
				return err
			}
		}
	}
	if len(result.Files) > 0 {
		label := "preview"
		if result.WroteFiles {
			label = "wrote"
		}
		if _, err := fmt.Fprintf(stdout, "%s:\n", label); err != nil {
			return err
		}
		for _, file := range result.Files {
			if _, err := fmt.Fprintf(stdout, "--- %s\n%s", file.Path, file.Content); err != nil {
				return err
			}
			if !strings.HasSuffix(file.Content, "\n") {
				if _, err := io.WriteString(stdout, "\n"); err != nil {
					return err
				}
			}
		}
	}
	if result.WroteFiles {
		if _, err := io.WriteString(stdout, "ask write: ok\n"); err != nil {
			return err
		}
	}
	if shouldLogAsk(result.ConfigSource.LogLevel, "basic") {
		if _, err := fmt.Fprintf(stderr, "deck ask phase=done route=%s reason=%s target=%s classifierLlmUsed=%t llmUsed=%t retries=%d termination=%s\n", result.Route, result.Reason, result.Target.Path, result.ClassifierLLM, result.LLMUsed, result.RetriesUsed, result.Termination); err != nil {
			return err
		}
	}
	return nil
}

func shouldLogAsk(current string, required string) bool {
	levels := map[string]int{"basic": 1, "debug": 2, "trace": 3}
	current = askconfigLogLevel(current)
	required = askconfigLogLevel(required)
	return levels[current] >= levels[required]
}

func askconfigLogLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return "debug"
	case "trace":
		return "trace"
	default:
		return "basic"
	}
}

func validateGeneratedPath(path string) error {
	clean := filepath.ToSlash(strings.TrimSpace(path))
	if clean == "" {
		return fmt.Errorf("generated file path is empty")
	}
	allowed := strings.HasPrefix(clean, "workflows/scenarios/") || strings.HasPrefix(clean, "workflows/components/") || clean == "workflows/vars.yaml"
	if !allowed {
		return fmt.Errorf("generated file path is not allowed: %s", clean)
	}
	if strings.Contains(clean, "..") {
		return fmt.Errorf("generated file path escapes workspace: %s", clean)
	}
	return nil
}

func validateGeneratedFile(root string, file askcontract.GeneratedFile) error {
	if err := validateGeneratedPath(file.Path); err != nil {
		return err
	}
	target, err := fsutil.ResolveUnder(root, strings.Split(filepath.ToSlash(file.Path), "/")...)
	if err != nil {
		return err
	}
	if strings.HasSuffix(file.Path, ".yaml") || strings.HasSuffix(file.Path, ".yml") {
		if isVarsPath(file.Path) {
			var vars map[string]any
			if err := yaml.Unmarshal([]byte(file.Content), &vars); err != nil {
				return fmt.Errorf("%s: parse vars yaml: %w", file.Path, err)
			}
			return nil
		}
		if err := validate.Bytes(target, []byte(file.Content)); err != nil {
			return err
		}
	}
	return nil
}

func writeFiles(root string, files []askcontract.GeneratedFile) error {
	if err := ensureScaffold(root); err != nil {
		return err
	}
	for _, file := range files {
		if err := validateGeneratedPath(file.Path); err != nil {
			return err
		}
		target, err := fsutil.ResolveUnder(root, strings.Split(filepath.ToSlash(file.Path), "/")...)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return fmt.Errorf("create ask target directory: %w", err)
		}
		if err := os.WriteFile(target, []byte(file.Content), 0o600); err != nil {
			return fmt.Errorf("write %s: %w", file.Path, err)
		}
	}
	return nil
}

func stageWorkspace(root string, files []askcontract.GeneratedFile) (string, error) {
	tempRoot, err := os.MkdirTemp("", "deck-ask-workspace-")
	if err != nil {
		return "", fmt.Errorf("create ask staging workspace: %w", err)
	}
	workflowRoot := filepath.Join(root, workspacepaths.WorkflowRootDir)
	if info, err := os.Stat(workflowRoot); err == nil && info.IsDir() {
		if err := copyTree(workflowRoot, filepath.Join(tempRoot, workspacepaths.WorkflowRootDir)); err != nil {
			return "", err
		}
	}
	for _, file := range files {
		if err := validateGeneratedPath(file.Path); err != nil {
			return "", err
		}
		target, err := fsutil.ResolveUnder(tempRoot, strings.Split(filepath.ToSlash(file.Path), "/")...)
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return "", fmt.Errorf("create ask staging directory: %w", err)
		}
		if err := os.WriteFile(target, []byte(file.Content), 0o600); err != nil {
			return "", fmt.Errorf("write ask staging file: %w", err)
		}
	}
	return tempRoot, nil
}

func scenarioPaths(root string, candidatePaths []string) []string {
	paths := make([]string, 0)
	seen := map[string]bool{}
	for _, rel := range candidatePaths {
		clean := filepath.ToSlash(strings.TrimSpace(rel))
		if !strings.HasPrefix(clean, "workflows/scenarios/") {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(clean))
		if !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	if len(paths) > 0 {
		sort.Strings(paths)
		return paths
	}
	scenarioDir := filepath.Join(root, workspacepaths.WorkflowRootDir, workspacepaths.WorkflowScenariosDir)
	entries, err := os.ReadDir(scenarioDir)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
			paths = append(paths, filepath.Join(scenarioDir, entry.Name()))
		}
	}
	sort.Strings(paths)
	return paths
}

func localFindings(files []askcontract.GeneratedFile) []askreview.Finding {
	content := make(map[string]string, len(files))
	for _, file := range files {
		content[file.Path] = file.Content
	}
	return askreview.Candidate(content)
}

func ensureScaffold(root string) error {
	for _, dir := range []string{
		filepath.Join(root, ".deck"),
		filepath.Join(root, workspacepaths.WorkflowRootDir, workspacepaths.WorkflowScenariosDir),
		filepath.Join(root, workspacepaths.WorkflowRootDir, workspacepaths.WorkflowComponentsDir),
		filepath.Join(root, workspacepaths.PreparedDirRel, "files"),
		filepath.Join(root, workspacepaths.PreparedDirRel, "images"),
		filepath.Join(root, workspacepaths.PreparedDirRel, "packages"),
	} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create workspace scaffold: %w", err)
		}
	}
	defaults := map[string]string{
		filepath.Join(root, ".gitignore"):                                       strings.Join([]string{"/.deck/", "/deck", "/outputs/", "*.tar", ""}, "\n"),
		filepath.Join(root, ".deckignore"):                                      strings.Join([]string{".git/", ".gitignore", ".deckignore", "/*.tar", ""}, "\n"),
		filepath.Join(root, workspacepaths.PreparedDirRel, "files", ".keep"):    "",
		filepath.Join(root, workspacepaths.PreparedDirRel, "images", ".keep"):   "",
		filepath.Join(root, workspacepaths.PreparedDirRel, "packages", ".keep"): "",
	}
	for path, content := range defaults {
		if _, err := os.Stat(path); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat scaffold file %s: %w", path, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return fmt.Errorf("write scaffold file %s: %w", path, err)
		}
	}
	return nil
}

func copyTree(src string, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		raw, err := os.ReadFile(path) //nolint:gosec // Paths come from walking selected workspace tree.
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}
		return os.WriteFile(target, raw, 0o600) //nolint:gosec // Target stays inside staging root.
	})
}

func loadRequestText(prompt string, fromPath string) (string, error) {
	prompt = strings.TrimSpace(prompt)
	fromPath = strings.TrimSpace(fromPath)
	if fromPath == "" {
		return prompt, nil
	}
	raw, err := os.ReadFile(fromPath) //nolint:gosec // User-selected request file path.
	if err != nil {
		return "", fmt.Errorf("read ask request file: %w", err)
	}
	fromText := strings.TrimSpace(string(raw))
	if prompt == "" {
		return fromText, nil
	}
	return prompt + "\n\nAttached request details:\n" + fromText, nil
}

func renderUserCommand(opts Options) string {
	parts := []string{"deck", "ask"}
	if opts.Write {
		parts = append(parts, "--write")
	}
	if opts.Review {
		parts = append(parts, "--review")
	}
	if opts.MaxIterations > 0 {
		parts = append(parts, "--max-iterations", fmt.Sprintf("%d", opts.MaxIterations))
	}
	if strings.TrimSpace(opts.FromPath) != "" {
		parts = append(parts, "--from", strings.TrimSpace(opts.FromPath))
	}
	if strings.TrimSpace(opts.Provider) != "" {
		parts = append(parts, "--provider", strings.TrimSpace(opts.Provider))
	}
	if strings.TrimSpace(opts.Model) != "" {
		parts = append(parts, "--model", strings.TrimSpace(opts.Model))
	}
	if strings.TrimSpace(opts.Endpoint) != "" {
		parts = append(parts, "--endpoint", strings.TrimSpace(opts.Endpoint))
	}
	if strings.TrimSpace(opts.Prompt) != "" {
		parts = append(parts, strconv.Quote(strings.TrimSpace(opts.Prompt)))
	}
	return strings.Join(parts, " ")
}

func isVarsPath(path string) bool {
	return filepath.ToSlash(strings.TrimSpace(path)) == "workflows/vars.yaml"
}

func filePaths(files []askcontract.GeneratedFile) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
	}
	sort.Strings(paths)
	return paths
}

func chunkIDs(chunks []askretrieve.Chunk) []string {
	ids := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		ids = append(ids, chunk.ID)
	}
	return ids
}

func dedupe(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
