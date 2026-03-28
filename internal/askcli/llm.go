package askcli

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/Airgap-Castaways/deck/internal/askconfig"
	"github.com/Airgap-Castaways/deck/internal/askcontract"
	"github.com/Airgap-Castaways/deck/internal/askintent"
	"github.com/Airgap-Castaways/deck/internal/askprovider"
	"github.com/Airgap-Castaways/deck/internal/askretrieve"
)

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
