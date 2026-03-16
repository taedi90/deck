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

type InfoResponse struct {
	Summary         string   `json:"summary"`
	Answer          string   `json:"answer"`
	Suggestions     []string `json:"suggestions"`
	Findings        []string `json:"findings"`
	SuggestedChange []string `json:"suggestedChanges"`
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
