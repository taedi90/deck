package askintent

import "strings"

type Route string

const (
	RouteClarify  Route = "clarify"
	RouteQuestion Route = "question"
	RouteExplain  Route = "explain"
	RouteReview   Route = "review"
	RouteRefine   Route = "refine"
	RouteDraft    Route = "draft"
)

type LLMPolicy string

const (
	LLMDisallowed LLMPolicy = "disallowed"
	LLMOptional   LLMPolicy = "optional"
	LLMRequired   LLMPolicy = "required"
)

type Input struct {
	Prompt          string
	WriteFlag       bool
	ReviewFlag      bool
	HasWorkflowTree bool
	HasPrepare      bool
	HasApply        bool
}

type Decision struct {
	Route           Route
	Confidence      float64
	Reason          string
	Target          Target
	AllowGeneration bool
	AllowRetry      bool
	RequiresLint    bool
	LLMPolicy       LLMPolicy
}

type Target struct {
	Kind string
	Path string
	Name string
}

func Classify(input Input) Decision {
	if input.ReviewFlag {
		return Decision{
			Route:           RouteReview,
			Confidence:      1.0,
			Reason:          "explicit --review flag",
			Target:          Target{Kind: "workspace"},
			AllowGeneration: false,
			AllowRetry:      false,
			RequiresLint:    false,
			LLMPolicy:       LLMOptional,
		}
	}
	prompt := strings.TrimSpace(strings.ToLower(input.Prompt))
	if prompt == "" {
		return clarify("empty prompt")
	}
	words := strings.Fields(prompt)
	if len(words) <= 2 && len(prompt) <= 12 {
		return clarify("low-information prompt")
	}
	if hasAny(prompt, reviewTokens) {
		return Decision{Route: RouteReview, Confidence: 0.9, Reason: "review intent tokens", Target: inferTarget(prompt), AllowGeneration: false, AllowRetry: false, RequiresLint: false, LLMPolicy: LLMOptional}
	}
	if hasAny(prompt, explainTokens) {
		return Decision{Route: RouteExplain, Confidence: 0.85, Reason: "explain intent tokens", Target: inferTarget(prompt), AllowGeneration: false, AllowRetry: false, RequiresLint: false, LLMPolicy: LLMOptional}
	}
	if hasAny(prompt, questionTokens) {
		return Decision{Route: RouteQuestion, Confidence: 0.8, Reason: "question intent tokens", Target: inferTarget(prompt), AllowGeneration: false, AllowRetry: false, RequiresLint: false, LLMPolicy: LLMOptional}
	}
	if hasAny(prompt, refineTokens) {
		if input.HasWorkflowTree || input.HasPrepare || input.HasApply {
			return Decision{Route: RouteRefine, Confidence: 0.86, Reason: "refinement tokens with existing workflow", Target: inferTarget(prompt), AllowGeneration: true, AllowRetry: true, RequiresLint: true, LLMPolicy: LLMRequired}
		}
		return Decision{Route: RouteDraft, Confidence: 0.72, Reason: "refinement tokens without existing workflow", Target: inferTarget(prompt), AllowGeneration: true, AllowRetry: true, RequiresLint: true, LLMPolicy: LLMRequired}
	}
	if hasAny(prompt, draftTokens) {
		if input.HasWorkflowTree {
			return Decision{Route: RouteRefine, Confidence: 0.7, Reason: "authoring tokens with existing workflow", Target: inferTarget(prompt), AllowGeneration: true, AllowRetry: true, RequiresLint: true, LLMPolicy: LLMRequired}
		}
		return Decision{Route: RouteDraft, Confidence: 0.86, Reason: "authoring tokens", Target: inferTarget(prompt), AllowGeneration: true, AllowRetry: true, RequiresLint: true, LLMPolicy: LLMRequired}
	}
	if input.WriteFlag {
		if input.HasWorkflowTree {
			return Decision{Route: RouteRefine, Confidence: 0.76, Reason: "explicit --write flag with existing workflow", Target: inferTarget(prompt), AllowGeneration: true, AllowRetry: true, RequiresLint: true, LLMPolicy: LLMRequired}
		}
		return Decision{Route: RouteDraft, Confidence: 0.76, Reason: "explicit --write flag", Target: inferTarget(prompt), AllowGeneration: true, AllowRetry: true, RequiresLint: true, LLMPolicy: LLMRequired}
	}
	if input.HasWorkflowTree {
		return Decision{Route: RouteExplain, Confidence: 0.52, Reason: "default to explain for ambiguous prompt", Target: Target{Kind: "workspace"}, AllowGeneration: false, AllowRetry: false, RequiresLint: false, LLMPolicy: LLMOptional}
	}
	return clarify("ambiguous prompt")
}

func clarify(reason string) Decision {
	return Decision{Route: RouteClarify, Confidence: 0.95, Reason: reason, Target: Target{Kind: "unknown"}, AllowGeneration: false, AllowRetry: false, RequiresLint: false, LLMPolicy: LLMDisallowed}
}

func ParseRoute(value string) Route {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(RouteClarify):
		return RouteClarify
	case string(RouteQuestion):
		return RouteQuestion
	case string(RouteExplain):
		return RouteExplain
	case string(RouteReview):
		return RouteReview
	case string(RouteRefine):
		return RouteRefine
	case string(RouteDraft):
		return RouteDraft
	default:
		return RouteClarify
	}
}

func inferTarget(prompt string) Target {
	if strings.Contains(prompt, "apply") {
		return Target{Kind: "scenario", Name: "apply", Path: "workflows/scenarios/apply.yaml"}
	}
	if strings.Contains(prompt, "prepare") {
		return Target{Kind: "scenario", Name: "prepare", Path: "workflows/scenarios/prepare.yaml"}
	}
	if strings.Contains(prompt, "component") {
		return Target{Kind: "component"}
	}
	if strings.Contains(prompt, "vars") {
		return Target{Kind: "vars", Path: "workflows/vars.yaml"}
	}
	return Target{Kind: "workspace"}
}

func hasAny(prompt string, tokens []string) bool {
	for _, token := range tokens {
		if strings.Contains(prompt, token) {
			return true
		}
	}
	return false
}

var (
	reviewTokens   = []string{"review", "audit", "check", "검토", "리뷰", "점검"}
	explainTokens  = []string{"explain", "what does", "summary", "구조", "설명", "파악"}
	questionTokens = []string{"what", "how", "why", "help", "usage", "무엇", "어떻게", "왜"}
	refineTokens   = []string{"refine", "fix", "improve", "update", "change", "convert", "수정", "개선", "고쳐", "repair"}
	draftTokens    = []string{"draft", "generate", "create", "write", "scenario", "workflow", "작성", "생성", "시나리오"}
)
