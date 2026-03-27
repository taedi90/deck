package askcontract

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/askcontext"
	"github.com/Airgap-Castaways/deck/internal/workflowissues"
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
	ExecutionModel          ExecutionModel `json:"executionModel,omitempty"`
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

type ExecutionModel struct {
	ArtifactContracts    []ArtifactContract    `json:"artifactContracts,omitempty"`
	SharedStateContracts []SharedStateContract `json:"sharedStateContracts,omitempty"`
	RoleExecution        RoleExecutionModel    `json:"roleExecution,omitempty"`
	Verification         VerificationStrategy  `json:"verification,omitempty"`
	ApplyAssumptions     []string              `json:"applyAssumptions,omitempty"`
}

type ArtifactContract struct {
	Kind         string `json:"kind,omitempty"`
	ProducerPath string `json:"producerPath,omitempty"`
	ConsumerPath string `json:"consumerPath,omitempty"`
	Description  string `json:"description,omitempty"`
}

type SharedStateContract struct {
	Name              string   `json:"name,omitempty"`
	ProducerPath      string   `json:"producerPath,omitempty"`
	ConsumerPaths     []string `json:"consumerPaths,omitempty"`
	AvailabilityModel string   `json:"availabilityModel,omitempty"`
	Description       string   `json:"description,omitempty"`
}

type RoleExecutionModel struct {
	RoleSelector      string `json:"roleSelector,omitempty"`
	ControlPlaneFlow  string `json:"controlPlaneFlow,omitempty"`
	WorkerFlow        string `json:"workerFlow,omitempty"`
	PerNodeInvocation bool   `json:"perNodeInvocation,omitempty"`
}

type VerificationStrategy struct {
	BootstrapPhase            string `json:"bootstrapPhase,omitempty"`
	FinalPhase                string `json:"finalPhase,omitempty"`
	FinalVerificationRole     string `json:"finalVerificationRole,omitempty"`
	ExpectedNodeCount         int    `json:"expectedNodeCount,omitempty"`
	ExpectedControlPlaneReady int    `json:"expectedControlPlaneReady,omitempty"`
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

type PlanCriticResponse struct {
	Summary          string              `json:"summary"`
	Blocking         []string            `json:"blocking"`
	Advisory         []string            `json:"advisory"`
	MissingContracts []string            `json:"missingContracts"`
	SuggestedFixes   []string            `json:"suggestedFixes"`
	Findings         []PlanCriticFinding `json:"findings,omitempty"`
}

type PlanCriticFinding struct {
	Code        workflowissues.Code     `json:"code"`
	Severity    workflowissues.Severity `json:"severity"`
	Message     string                  `json:"message"`
	Path        string                  `json:"path,omitempty"`
	Recoverable bool                    `json:"recoverable,omitempty"`
}

type PostProcessResponse struct {
	Summary                  string   `json:"summary"`
	Blocking                 []string `json:"blocking"`
	Advisory                 []string `json:"advisory"`
	UpgradeCandidates        []string `json:"upgradeCandidates"`
	ReviseFiles              []string `json:"reviseFiles"`
	PreserveFiles            []string `json:"preserveFiles"`
	RequiredEdits            []string `json:"requiredEdits"`
	VerificationExpectations []string `json:"verificationExpectations"`
	SuggestedFixes           []string `json:"suggestedFixes"`
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
		repaired := repairLooseJSON(cleaned)
		if repaired == cleaned || json.Unmarshal([]byte(repaired), &resp) != nil {
			return PlanResponse{}, fmt.Errorf("parse plan response: %w", err)
		}
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
	resp.ExecutionModel.RoleExecution.RoleSelector = strings.TrimSpace(resp.ExecutionModel.RoleExecution.RoleSelector)
	resp.ExecutionModel.RoleExecution.ControlPlaneFlow = strings.TrimSpace(resp.ExecutionModel.RoleExecution.ControlPlaneFlow)
	resp.ExecutionModel.RoleExecution.WorkerFlow = strings.TrimSpace(resp.ExecutionModel.RoleExecution.WorkerFlow)
	resp.ExecutionModel.Verification.BootstrapPhase = strings.TrimSpace(resp.ExecutionModel.Verification.BootstrapPhase)
	resp.ExecutionModel.Verification.FinalPhase = strings.TrimSpace(resp.ExecutionModel.Verification.FinalPhase)
	resp.ExecutionModel.Verification.FinalVerificationRole = strings.TrimSpace(resp.ExecutionModel.Verification.FinalVerificationRole)
	resp.OfflineAssumption = strings.TrimSpace(resp.OfflineAssumption)
	resp.TargetOutcome = strings.TrimSpace(resp.TargetOutcome)
	resp.EntryScenario = strings.TrimSpace(resp.EntryScenario)
	for i := range resp.AuthoringBrief.TargetPaths {
		resp.AuthoringBrief.TargetPaths[i] = strings.TrimSpace(resp.AuthoringBrief.TargetPaths[i])
	}
	for i := range resp.AuthoringBrief.RequiredCapabilities {
		resp.AuthoringBrief.RequiredCapabilities[i] = strings.TrimSpace(resp.AuthoringBrief.RequiredCapabilities[i])
	}
	for i := range resp.ExecutionModel.ArtifactContracts {
		resp.ExecutionModel.ArtifactContracts[i].Kind = strings.TrimSpace(resp.ExecutionModel.ArtifactContracts[i].Kind)
		resp.ExecutionModel.ArtifactContracts[i].ProducerPath = strings.TrimSpace(resp.ExecutionModel.ArtifactContracts[i].ProducerPath)
		resp.ExecutionModel.ArtifactContracts[i].ConsumerPath = strings.TrimSpace(resp.ExecutionModel.ArtifactContracts[i].ConsumerPath)
		resp.ExecutionModel.ArtifactContracts[i].Description = strings.TrimSpace(resp.ExecutionModel.ArtifactContracts[i].Description)
	}
	for i := range resp.ExecutionModel.SharedStateContracts {
		resp.ExecutionModel.SharedStateContracts[i].Name = strings.TrimSpace(resp.ExecutionModel.SharedStateContracts[i].Name)
		resp.ExecutionModel.SharedStateContracts[i].ProducerPath = strings.TrimSpace(resp.ExecutionModel.SharedStateContracts[i].ProducerPath)
		resp.ExecutionModel.SharedStateContracts[i].AvailabilityModel = strings.TrimSpace(resp.ExecutionModel.SharedStateContracts[i].AvailabilityModel)
		resp.ExecutionModel.SharedStateContracts[i].Description = strings.TrimSpace(resp.ExecutionModel.SharedStateContracts[i].Description)
		for j := range resp.ExecutionModel.SharedStateContracts[i].ConsumerPaths {
			resp.ExecutionModel.SharedStateContracts[i].ConsumerPaths[j] = strings.TrimSpace(resp.ExecutionModel.SharedStateContracts[i].ConsumerPaths[j])
		}
	}
	for i := range resp.ExecutionModel.ApplyAssumptions {
		resp.ExecutionModel.ApplyAssumptions[i] = strings.TrimSpace(resp.ExecutionModel.ApplyAssumptions[i])
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

func ParsePlanCritic(raw string) (PlanCriticResponse, error) {
	cleaned := clean(raw)
	if cleaned == "" {
		return PlanCriticResponse{}, fmt.Errorf("plan critic response is empty")
	}
	var resp PlanCriticResponse
	if err := json.Unmarshal([]byte(cleaned), &resp); err != nil {
		return PlanCriticResponse{}, fmt.Errorf("parse plan critic response: %w", err)
	}
	resp.Summary = strings.TrimSpace(resp.Summary)
	for i := range resp.Blocking {
		resp.Blocking[i] = strings.TrimSpace(resp.Blocking[i])
	}
	for i := range resp.Advisory {
		resp.Advisory[i] = strings.TrimSpace(resp.Advisory[i])
	}
	for i := range resp.MissingContracts {
		resp.MissingContracts[i] = strings.TrimSpace(resp.MissingContracts[i])
	}
	for i := range resp.SuggestedFixes {
		resp.SuggestedFixes[i] = strings.TrimSpace(resp.SuggestedFixes[i])
	}
	for i := range resp.Findings {
		resp.Findings[i].Code = workflowissues.Code(strings.TrimSpace(string(resp.Findings[i].Code)))
		resp.Findings[i].Severity = workflowissues.Severity(strings.TrimSpace(string(resp.Findings[i].Severity)))
		resp.Findings[i].Message = strings.TrimSpace(resp.Findings[i].Message)
		resp.Findings[i].Path = strings.TrimSpace(resp.Findings[i].Path)
		if resp.Findings[i].Code == "" {
			return PlanCriticResponse{}, fmt.Errorf("plan critic finding is missing code")
		}
		if !workflowissues.IsSupportedCriticCode(resp.Findings[i].Code) {
			return PlanCriticResponse{}, fmt.Errorf("plan critic finding %q uses unsupported code", resp.Findings[i].Code)
		}
		if resp.Findings[i].Message == "" {
			return PlanCriticResponse{}, fmt.Errorf("plan critic finding %q is missing message", resp.Findings[i].Code)
		}
		switch resp.Findings[i].Severity {
		case workflowissues.SeverityBlocking, workflowissues.SeverityAdvisory, workflowissues.SeverityMissingContract:
			// ok
		case "":
			resp.Findings[i].Severity = workflowissues.SeverityAdvisory
		default:
			return PlanCriticResponse{}, fmt.Errorf("plan critic finding %q has invalid severity %q", resp.Findings[i].Code, resp.Findings[i].Severity)
		}
	}
	return resp, nil
}

func ParsePostProcess(raw string) (PostProcessResponse, error) {
	cleaned := clean(raw)
	if cleaned == "" {
		return PostProcessResponse{}, fmt.Errorf("post-process response is empty")
	}
	var resp PostProcessResponse
	if err := json.Unmarshal([]byte(cleaned), &resp); err != nil {
		return PostProcessResponse{}, fmt.Errorf("parse post-process response: %w", err)
	}
	resp.Summary = strings.TrimSpace(resp.Summary)
	for i := range resp.Blocking {
		resp.Blocking[i] = strings.TrimSpace(resp.Blocking[i])
	}
	for i := range resp.Advisory {
		resp.Advisory[i] = strings.TrimSpace(resp.Advisory[i])
	}
	for i := range resp.UpgradeCandidates {
		resp.UpgradeCandidates[i] = strings.TrimSpace(resp.UpgradeCandidates[i])
	}
	for i := range resp.ReviseFiles {
		resp.ReviseFiles[i] = strings.TrimSpace(resp.ReviseFiles[i])
	}
	for i := range resp.PreserveFiles {
		resp.PreserveFiles[i] = strings.TrimSpace(resp.PreserveFiles[i])
	}
	for i := range resp.RequiredEdits {
		resp.RequiredEdits[i] = strings.TrimSpace(resp.RequiredEdits[i])
	}
	for i := range resp.VerificationExpectations {
		resp.VerificationExpectations[i] = strings.TrimSpace(resp.VerificationExpectations[i])
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

func repairLooseJSON(response string) string {
	response = strings.ReplaceAll(response, ",]", "]")
	response = strings.ReplaceAll(response, ", }", " }")
	response = strings.ReplaceAll(response, ",}", "}")
	response = strings.ReplaceAll(response, ",\n]", "\n]")
	response = strings.ReplaceAll(response, ",\n}", "\n}")
	return response
}
