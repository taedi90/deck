package askcontract

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/askcontext"
)

type GeneratedFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type GenerationResponse struct {
	Summary string          `json:"summary"`
	Review  []string        `json:"review"`
	Files   []GeneratedFile `json:"files"`
}

type PlanFile struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Action  string `json:"action"`
	Purpose string `json:"purpose"`
}

type PlanResponse struct {
	Version                 int            `json:"version"`
	Request                 string         `json:"request"`
	Intent                  string         `json:"intent"`
	Complexity              string         `json:"complexity"`
	AuthoringBrief          AuthoringBrief `json:"authoringBrief,omitempty"`
	OfflineAssumption       string         `json:"offlineAssumption,omitempty"`
	NeedsPrepare            bool           `json:"needsPrepare,omitempty"`
	ArtifactKinds           []string       `json:"artifactKinds,omitempty"`
	VarsRecommendation      []string       `json:"varsRecommendation,omitempty"`
	ComponentRecommendation []string       `json:"componentRecommendation,omitempty"`
	Blockers                []string       `json:"blockers"`
	TargetOutcome           string         `json:"targetOutcome"`
	Assumptions             []string       `json:"assumptions"`
	OpenQuestions           []string       `json:"openQuestions"`
	EntryScenario           string         `json:"entryScenario"`
	Files                   []PlanFile     `json:"files"`
	ValidationChecklist     []string       `json:"validationChecklist"`
}

type AuthoringBrief struct {
	RouteIntent          string   `json:"routeIntent,omitempty"`
	TargetScope          string   `json:"targetScope,omitempty"`
	TargetPaths          []string `json:"targetPaths,omitempty"`
	ModeIntent           string   `json:"modeIntent,omitempty"`
	Connectivity         string   `json:"connectivity,omitempty"`
	CompletenessTarget   string   `json:"completenessTarget,omitempty"`
	Topology             string   `json:"topology,omitempty"`
	NodeCount            int      `json:"nodeCount,omitempty"`
	RequiredCapabilities []string `json:"requiredCapabilities,omitempty"`
}

type CriticResponse struct {
	Blocking       []string `json:"blocking"`
	Advisory       []string `json:"advisory"`
	MissingFiles   []string `json:"missingFiles"`
	InvalidImports []string `json:"invalidImports"`
	CoverageGaps   []string `json:"coverageGaps"`
	RequiredFixes  []string `json:"requiredFixes"`
}

type JudgeResponse struct {
	Summary             string   `json:"summary"`
	Blocking            []string `json:"blocking"`
	Advisory            []string `json:"advisory"`
	MissingCapabilities []string `json:"missingCapabilities"`
	SuggestedFixes      []string `json:"suggestedFixes"`
}

type InfoResponse struct {
	Summary         string   `json:"summary"`
	Answer          string   `json:"answer"`
	Suggestions     []string `json:"suggestions"`
	Findings        []string `json:"findings"`
	SuggestedChange []string `json:"suggestedChanges"`
}

type ClassificationResponse struct {
	Route             string               `json:"route"`
	Confidence        float64              `json:"confidence"`
	Reason            string               `json:"reason"`
	Target            ClassificationTarget `json:"target"`
	GenerationAllowed *bool                `json:"generationAllowed,omitempty"`
	ReviewStyle       string               `json:"reviewStyle,omitempty"`
}

type ClassificationTarget struct {
	Kind string `json:"kind,omitempty"`
	Path string `json:"path,omitempty"`
	Name string `json:"name,omitempty"`
}

func ParseGeneration(raw string) (GenerationResponse, error) {
	cleaned := clean(raw)
	if cleaned == "" {
		return GenerationResponse{}, fmt.Errorf("model returned empty response")
	}
	var resp GenerationResponse
	if err := json.Unmarshal([]byte(cleaned), &resp); err != nil {
		return GenerationResponse{}, fmt.Errorf("parse generation response: %w", err)
	}
	if strings.TrimSpace(resp.Summary) == "" {
		resp.Summary = "No summary provided."
	}
	return resp, nil
}

func ParseInfo(raw string) InfoResponse {
	cleaned := clean(raw)
	if cleaned == "" {
		return InfoResponse{Summary: "No response returned.", Answer: ""}
	}
	var resp InfoResponse
	if err := json.Unmarshal([]byte(cleaned), &resp); err != nil {
		trimmed := strings.TrimSpace(raw)
		return InfoResponse{Summary: "Answer", Answer: trimmed}
	}
	if strings.TrimSpace(resp.Summary) == "" {
		resp.Summary = "Answer"
	}
	if strings.TrimSpace(resp.Answer) == "" {
		resp.Answer = strings.TrimSpace(raw)
	}
	return resp
}

func ParseClassification(raw string) (ClassificationResponse, error) {
	cleaned := clean(raw)
	if cleaned == "" {
		return ClassificationResponse{}, fmt.Errorf("classification response is empty")
	}
	var resp ClassificationResponse
	if err := json.Unmarshal([]byte(cleaned), &resp); err != nil {
		return ClassificationResponse{}, fmt.Errorf("parse classification response: %w", err)
	}
	resp.Route = strings.TrimSpace(resp.Route)
	resp.Reason = strings.TrimSpace(resp.Reason)
	resp.Target.Kind = strings.TrimSpace(resp.Target.Kind)
	resp.Target.Path = strings.TrimSpace(resp.Target.Path)
	resp.Target.Name = strings.TrimSpace(resp.Target.Name)
	if resp.Confidence < 0 {
		resp.Confidence = 0
	}
	if resp.Confidence > 1 {
		resp.Confidence = 1
	}
	return resp, nil
}

func ParsePlan(raw string) (PlanResponse, error) {
	cleaned := clean(raw)
	if cleaned == "" {
		return PlanResponse{}, fmt.Errorf("plan response is empty")
	}
	var resp PlanResponse
	if err := json.Unmarshal([]byte(cleaned), &resp); err != nil {
		return PlanResponse{}, fmt.Errorf("parse plan response: %w", err)
	}
	if resp.Version == 0 {
		resp.Version = 1
	}
	resp.Request = strings.TrimSpace(resp.Request)
	resp.Intent = strings.TrimSpace(resp.Intent)
	resp.Complexity = strings.TrimSpace(resp.Complexity)
	resp.AuthoringBrief.RouteIntent = strings.TrimSpace(resp.AuthoringBrief.RouteIntent)
	resp.AuthoringBrief.TargetScope = strings.TrimSpace(resp.AuthoringBrief.TargetScope)
	resp.AuthoringBrief.ModeIntent = strings.TrimSpace(resp.AuthoringBrief.ModeIntent)
	resp.AuthoringBrief.Connectivity = strings.TrimSpace(resp.AuthoringBrief.Connectivity)
	resp.AuthoringBrief.CompletenessTarget = strings.TrimSpace(resp.AuthoringBrief.CompletenessTarget)
	resp.AuthoringBrief.Topology = strings.TrimSpace(resp.AuthoringBrief.Topology)
	resp.OfflineAssumption = strings.TrimSpace(resp.OfflineAssumption)
	resp.TargetOutcome = strings.TrimSpace(resp.TargetOutcome)
	resp.EntryScenario = strings.TrimSpace(resp.EntryScenario)
	for i := range resp.AuthoringBrief.TargetPaths {
		resp.AuthoringBrief.TargetPaths[i] = strings.TrimSpace(resp.AuthoringBrief.TargetPaths[i])
	}
	for i := range resp.AuthoringBrief.RequiredCapabilities {
		resp.AuthoringBrief.RequiredCapabilities[i] = strings.TrimSpace(resp.AuthoringBrief.RequiredCapabilities[i])
	}
	if resp.Request == "" {
		return PlanResponse{}, fmt.Errorf("plan response is missing request")
	}
	if resp.Intent == "" {
		return PlanResponse{}, fmt.Errorf("plan response is missing intent")
	}
	if len(resp.Files) == 0 {
		return PlanResponse{}, fmt.Errorf("plan response is missing files")
	}
	for i := range resp.Files {
		resp.Files[i].Path = strings.TrimSpace(resp.Files[i].Path)
		resp.Files[i].Kind = strings.TrimSpace(resp.Files[i].Kind)
		resp.Files[i].Action = strings.TrimSpace(resp.Files[i].Action)
		resp.Files[i].Purpose = strings.TrimSpace(resp.Files[i].Purpose)
		if resp.Files[i].Path == "" {
			return PlanResponse{}, fmt.Errorf("plan response has file with empty path")
		}
		if resp.Files[i].Action == "" {
			resp.Files[i].Action = "create"
		}
		switch resp.Files[i].Action {
		case "modify", "update", "create-or-modify", "create-or-update":
			if strings.HasPrefix(resp.Files[i].Path, "workflows/") {
				resp.Files[i].Action = "update"
			}
		case "create":
			// keep as-is
		}
		if !askcontext.AllowedGeneratedPath(resp.Files[i].Path) {
			return PlanResponse{}, fmt.Errorf("plan response has file outside allowed ask paths: %s", resp.Files[i].Path)
		}
	}
	if resp.EntryScenario != "" {
		if !askcontext.AllowedGeneratedPath(resp.EntryScenario) || !strings.HasPrefix(resp.EntryScenario, "workflows/scenarios/") {
			return PlanResponse{}, fmt.Errorf("plan response entryScenario must be a scenario path under workflows/scenarios/: %s", resp.EntryScenario)
		}
		matched := false
		for _, file := range resp.Files {
			if file.Path == resp.EntryScenario {
				matched = true
				break
			}
		}
		if !matched {
			return PlanResponse{}, fmt.Errorf("plan response entryScenario must match a planned file: %s", resp.EntryScenario)
		}
	}
	return resp, nil
}

func ParseJudge(raw string) (JudgeResponse, error) {
	cleaned := clean(raw)
	if cleaned == "" {
		return JudgeResponse{}, fmt.Errorf("judge response is empty")
	}
	var resp JudgeResponse
	if err := json.Unmarshal([]byte(cleaned), &resp); err != nil {
		return JudgeResponse{}, fmt.Errorf("parse judge response: %w", err)
	}
	resp.Summary = strings.TrimSpace(resp.Summary)
	for i := range resp.Blocking {
		resp.Blocking[i] = strings.TrimSpace(resp.Blocking[i])
	}
	for i := range resp.Advisory {
		resp.Advisory[i] = strings.TrimSpace(resp.Advisory[i])
	}
	for i := range resp.MissingCapabilities {
		resp.MissingCapabilities[i] = strings.TrimSpace(resp.MissingCapabilities[i])
	}
	for i := range resp.SuggestedFixes {
		resp.SuggestedFixes[i] = strings.TrimSpace(resp.SuggestedFixes[i])
	}
	return resp, nil
}

func clean(response string) string {
	response = strings.TrimSpace(response)
	response = strings.TrimPrefix(response, "```json")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	response = strings.TrimSpace(response)
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")
	if start >= 0 && end > start {
		response = response[start : end+1]
	}
	return strings.TrimSpace(response)
}
