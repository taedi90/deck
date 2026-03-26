package askpolicy

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/askcontract"
	"github.com/Airgap-Castaways/deck/internal/askintent"
	"github.com/Airgap-Castaways/deck/internal/askretrieve"
)

func NormalizePlan(plan askcontract.PlanResponse, prompt string, retrieval askretrieve.RetrievalResult, workspace askretrieve.WorkspaceSummary, decision askintent.Decision) askcontract.PlanResponse {
	req := BuildScenarioRequirements(prompt, retrieval, workspace, decision)
	if strings.TrimSpace(plan.OfflineAssumption) == "" {
		plan.OfflineAssumption = req.Connectivity
	}
	plan.ArtifactKinds = NormalizeArtifactKinds(plan.ArtifactKinds)
	if len(plan.ArtifactKinds) == 0 {
		plan.ArtifactKinds = append([]string(nil), req.ArtifactKinds...)
	}
	if len(plan.ArtifactKinds) > 0 {
		plan.NeedsPrepare = true
	}
	if len(plan.VarsRecommendation) == 0 {
		plan.VarsRecommendation = append([]string(nil), req.VarsAdvisories...)
	}
	if len(plan.ComponentRecommendation) == 0 {
		plan.ComponentRecommendation = append([]string(nil), req.ComponentAdvisories...)
	}
	if strings.TrimSpace(plan.EntryScenario) == "" {
		plan.EntryScenario = req.EntryScenario
	}
	for i := range plan.Files {
		plan.Files[i].Action = normalizePlannedAction(plan.Files[i].Action, plan.Files[i].Path)
	}
	if req.AcceptanceLevel == "starter" {
		filtered := make([]askcontract.PlanFile, 0, len(plan.Files))
		for _, file := range plan.Files {
			clean := filepath.ToSlash(strings.TrimSpace(file.Path))
			if strings.HasPrefix(clean, "workflows/components/") {
				continue
			}
			filtered = append(filtered, file)
		}
		plan.Files = filtered
		plan.ComponentRecommendation = nil
	}
	return plan
}

func EvaluatePlanConformance(plan askcontract.PlanResponse, gen askcontract.GenerationResponse, decision askintent.Decision) EvaluationResult {
	findings := []EvaluationFinding{}
	generated := generatedMap(gen.Files)
	planned := map[string]string{}
	for _, file := range plan.Files {
		planned[filepath.ToSlash(strings.TrimSpace(file.Path))] = strings.ToLower(strings.TrimSpace(file.Action))
	}
	for path := range planned {
		if _, ok := generated[path]; !ok {
			findings = append(findings, EvaluationFinding{Severity: "blocking", Code: "planned_file_missing", Message: fmt.Sprintf("planned file missing from generation: %s", path), Path: path})
		}
	}
	checklistText := strings.ToLower(strings.Join(plan.ValidationChecklist, "\n"))
	if strings.Contains(checklistText, "vars") {
		if _, ok := generated["workflows/vars.yaml"]; !ok {
			findings = append(findings, EvaluationFinding{Severity: "blocking", Code: "vars_required_by_checklist", Message: "validation checklist requires vars but workflows/vars.yaml was not generated", Path: "workflows/vars.yaml"})
		}
	}
	if decision.Route == askintent.RouteRefine {
		for _, file := range gen.Files {
			clean := filepath.ToSlash(strings.TrimSpace(file.Path))
			action, ok := planned[clean]
			if !ok {
				findings = append(findings, EvaluationFinding{Severity: "blocking", Code: "refine_unplanned_file", Message: fmt.Sprintf("refine generated unplanned file: %s", clean), Fix: "Only update or create files declared in the plan during refine", Path: clean})
			}
			if action != "" && action != "update" && action != "create" {
				findings = append(findings, EvaluationFinding{Severity: "blocking", Code: "invalid_planned_action", Message: fmt.Sprintf("invalid planned action for %s", clean), Path: clean})
			}
			if action == "update" && strings.HasPrefix(clean, "workflows/scenarios/") && strings.Contains(strings.ToLower(clean), "apply") {
				findings = append(findings, EvaluationFinding{Severity: "advisory", Code: "refine_updates_entry", Message: fmt.Sprintf("refine updates existing entry scenario: %s", clean), Path: clean})
			}
		}
	}
	if entry := filepath.ToSlash(strings.TrimSpace(plan.EntryScenario)); entry != "" {
		if _, ok := generated[entry]; !ok {
			findings = append(findings, EvaluationFinding{Severity: "blocking", Code: "entry_scenario_missing", Message: fmt.Sprintf("planned entry scenario missing from generation: %s", entry), Path: entry})
		}
	}
	return EvaluationResult{Findings: findings}
}

func ValidatePlanStructure(plan askcontract.PlanResponse) error {
	if plan.NeedsPrepare && !containsPlannedPath(plan.Files, "workflows/prepare.yaml") {
		return fmt.Errorf("plan response requires prepare but does not include workflows/prepare.yaml")
	}
	return nil
}

func containsPlannedPath(files []askcontract.PlanFile, want string) bool {
	want = strings.TrimSpace(want)
	for _, file := range files {
		if strings.TrimSpace(file.Path) == want {
			return true
		}
	}
	return false
}

func normalizePlannedAction(action string, path string) string {
	action = strings.ToLower(strings.TrimSpace(action))
	action = strings.ReplaceAll(action, "_", "-")
	switch action {
	case "create", "update":
		return action
	case "add":
		return "create"
	case "modify", "create-or-modify", "create-or-update":
		if strings.HasPrefix(strings.TrimSpace(path), "workflows/") {
			return "update"
		}
	}
	if action == "" {
		return "create"
	}
	return action
}
