package askcontract

import (
	"encoding/json"
	"fmt"
	"strings"
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
	Version             int        `json:"version"`
	Request             string     `json:"request"`
	Intent              string     `json:"intent"`
	Complexity          string     `json:"complexity"`
	Blockers            []string   `json:"blockers"`
	TargetOutcome       string     `json:"targetOutcome"`
	Assumptions         []string   `json:"assumptions"`
	OpenQuestions       []string   `json:"openQuestions"`
	EntryScenario       string     `json:"entryScenario"`
	Files               []PlanFile `json:"files"`
	ValidationChecklist []string   `json:"validationChecklist"`
}

type CriticResponse struct {
	Blocking       []string `json:"blocking"`
	Advisory       []string `json:"advisory"`
	MissingFiles   []string `json:"missingFiles"`
	InvalidImports []string `json:"invalidImports"`
	CoverageGaps   []string `json:"coverageGaps"`
	RequiredFixes  []string `json:"requiredFixes"`
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
	resp.TargetOutcome = strings.TrimSpace(resp.TargetOutcome)
	resp.EntryScenario = strings.TrimSpace(resp.EntryScenario)
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
