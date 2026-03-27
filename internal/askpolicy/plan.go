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
	fallbackBrief := BriefFromRequirements(req, decision)
	fallbackExecutionModel := ExecutionModelFromRequirements(req)
	plan.AuthoringBrief = normalizeAuthoringBrief(plan.AuthoringBrief, fallbackBrief)
	plan.ExecutionModel = normalizeExecutionModel(plan.ExecutionModel, fallbackExecutionModel)
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

func normalizeExecutionModel(model askcontract.ExecutionModel, fallback askcontract.ExecutionModel) askcontract.ExecutionModel {
	model.ArtifactContracts = normalizeArtifactContracts(model.ArtifactContracts, fallback.ArtifactContracts)
	model.SharedStateContracts = normalizeSharedStateContracts(model.SharedStateContracts, fallback.SharedStateContracts)
	if strings.TrimSpace(model.RoleExecution.RoleSelector) == "" {
		model.RoleExecution.RoleSelector = fallback.RoleExecution.RoleSelector
	}
	if strings.TrimSpace(model.RoleExecution.ControlPlaneFlow) == "" {
		model.RoleExecution.ControlPlaneFlow = fallback.RoleExecution.ControlPlaneFlow
	}
	if strings.TrimSpace(model.RoleExecution.WorkerFlow) == "" {
		model.RoleExecution.WorkerFlow = fallback.RoleExecution.WorkerFlow
	}
	if !model.RoleExecution.PerNodeInvocation {
		model.RoleExecution.PerNodeInvocation = fallback.RoleExecution.PerNodeInvocation
	}
	if strings.TrimSpace(model.Verification.BootstrapPhase) == "" {
		model.Verification.BootstrapPhase = fallback.Verification.BootstrapPhase
	}
	if strings.TrimSpace(model.Verification.FinalPhase) == "" {
		model.Verification.FinalPhase = fallback.Verification.FinalPhase
	}
	if !isCanonicalVerificationRole(model.Verification.FinalVerificationRole) {
		model.Verification.FinalVerificationRole = fallback.Verification.FinalVerificationRole
	}
	if model.Verification.ExpectedNodeCount <= 0 {
		model.Verification.ExpectedNodeCount = fallback.Verification.ExpectedNodeCount
	}
	if model.Verification.ExpectedControlPlaneReady <= 0 {
		model.Verification.ExpectedControlPlaneReady = fallback.Verification.ExpectedControlPlaneReady
	}
	if len(model.ApplyAssumptions) == 0 {
		model.ApplyAssumptions = append([]string(nil), fallback.ApplyAssumptions...)
	} else {
		model.ApplyAssumptions = dedupeStrings(append(normalizeStringList(model.ApplyAssumptions), fallback.ApplyAssumptions...))
	}
	return model
}

func isCanonicalVerificationRole(value string) bool {
	switch strings.TrimSpace(value) {
	case "control-plane", "worker", "local", "any":
		return true
	default:
		return false
	}
}

func normalizeArtifactContracts(contracts []askcontract.ArtifactContract, fallback []askcontract.ArtifactContract) []askcontract.ArtifactContract {
	allowedKinds := map[string]bool{"package": true, "image": true, "repository-setup": true}
	out := make([]askcontract.ArtifactContract, 0, len(contracts)+len(fallback))
	presentKinds := map[string]bool{}
	for _, item := range contracts {
		kind := strings.ToLower(strings.TrimSpace(item.Kind))
		if !allowedKinds[kind] {
			continue
		}
		item.Kind = kind
		item.ProducerPath = filepath.ToSlash(strings.TrimSpace(item.ProducerPath))
		item.ConsumerPath = filepath.ToSlash(strings.TrimSpace(item.ConsumerPath))
		item.Description = strings.TrimSpace(item.Description)
		if !askcontractPathAllowed(item.ProducerPath) || !askcontractPathAllowed(item.ConsumerPath) {
			continue
		}
		out = append(out, item)
		presentKinds[kind] = true
	}
	if len(out) == 0 {
		out = append(out, fallback...)
	} else {
		for _, item := range fallback {
			if !presentKinds[item.Kind] {
				out = append(out, item)
			}
		}
	}
	return dedupeArtifactContracts(out)
}

func normalizeSharedStateContracts(contracts []askcontract.SharedStateContract, fallback []askcontract.SharedStateContract) []askcontract.SharedStateContract {
	allowedAvailability := map[string]bool{"published-for-worker-consumption": true, "local-only": true}
	out := make([]askcontract.SharedStateContract, 0, len(contracts)+len(fallback))
	presentNames := map[string]bool{}
	for _, item := range contracts {
		item.Name = canonicalSharedStateName(strings.TrimSpace(item.Name))
		item.ProducerPath = strings.TrimSpace(item.ProducerPath)
		item.AvailabilityModel = strings.TrimSpace(item.AvailabilityModel)
		item.Description = strings.TrimSpace(item.Description)
		if item.Name == "" || item.ProducerPath == "" || !allowedAvailability[item.AvailabilityModel] {
			continue
		}
		item.ConsumerPaths = normalizeStringList(item.ConsumerPaths)
		out = append(out, item)
		presentNames[item.Name] = true
	}
	if len(out) == 0 {
		out = append(out, fallback...)
	} else {
		for _, item := range fallback {
			if !presentNames[canonicalSharedStateName(item.Name)] {
				out = append(out, item)
			}
		}
	}
	return dedupeSharedStateContracts(out)
}

func canonicalSharedStateName(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	if strings.Contains(lower, "join") {
		return "join-file"
	}
	return strings.TrimSpace(name)
}

func normalizeStringList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func dedupeSharedStateContracts(contracts []askcontract.SharedStateContract) []askcontract.SharedStateContract {
	seen := map[string]bool{}
	out := make([]askcontract.SharedStateContract, 0, len(contracts))
	for _, item := range contracts {
		key := item.Name + "|" + item.ProducerPath + "|" + item.AvailabilityModel
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

func normalizeAuthoringBrief(brief askcontract.AuthoringBrief, fallback askcontract.AuthoringBrief) askcontract.AuthoringBrief {
	if !isCanonicalTargetScope(brief.TargetScope) {
		brief.TargetScope = fallback.TargetScope
	}
	if !isCanonicalModeIntent(brief.ModeIntent) {
		brief.ModeIntent = fallback.ModeIntent
	}
	if !isCanonicalCompleteness(brief.CompletenessTarget) {
		brief.CompletenessTarget = fallback.CompletenessTarget
	}
	if !isCanonicalTopology(brief.Topology) {
		brief.Topology = fallback.Topology
	}
	if strings.TrimSpace(brief.RouteIntent) == "" || len(strings.Fields(brief.RouteIntent)) > 6 {
		brief.RouteIntent = fallback.RouteIntent
	}
	if strings.TrimSpace(brief.Connectivity) == "" || len(strings.Fields(brief.Connectivity)) > 4 {
		brief.Connectivity = fallback.Connectivity
	}
	if brief.NodeCount <= 0 && fallback.NodeCount > 0 {
		brief.NodeCount = fallback.NodeCount
	}
	if len(brief.TargetPaths) == 0 {
		brief.TargetPaths = append([]string(nil), fallback.TargetPaths...)
	}
	brief.TargetPaths = normalizeAllowedPaths(brief.TargetPaths, fallback.TargetPaths)
	brief.RequiredCapabilities = normalizeCapabilities(brief.RequiredCapabilities, fallback.RequiredCapabilities)
	if len(brief.RequiredCapabilities) == 0 {
		brief.RequiredCapabilities = append([]string(nil), fallback.RequiredCapabilities...)
	}
	return brief
}

func normalizeAllowedPaths(paths []string, fallback []string) []string {
	allowed := make([]string, 0, len(paths))
	for _, path := range paths {
		path = filepath.ToSlash(strings.TrimSpace(path))
		if askcontractPathAllowed(path) {
			allowed = append(allowed, path)
		}
	}
	if len(allowed) == 0 {
		return append([]string(nil), fallback...)
	}
	return dedupeStrings(allowed)
}

func normalizeCapabilities(values []string, fallback []string) []string {
	allowed := map[string]bool{
		"prepare-artifacts":    true,
		"package-staging":      true,
		"image-staging":        true,
		"repository-setup":     true,
		"kubeadm-bootstrap":    true,
		"kubeadm-join":         true,
		"cluster-verification": true,
	}
	canonical := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		value = strings.ReplaceAll(value, "_", "-")
		value = strings.ReplaceAll(value, " ", "-")
		if value == "" || strings.ContainsAny(value, ":/()") || !allowed[value] {
			continue
		}
		canonical = append(canonical, value)
	}
	canonical = append(canonical, fallback...)
	return dedupeStrings(canonical)
}

func isCanonicalTargetScope(value string) bool {
	switch strings.TrimSpace(value) {
	case "workspace", "scenario", "vars", "component":
		return true
	default:
		return false
	}
}

func isCanonicalModeIntent(value string) bool {
	switch strings.TrimSpace(value) {
	case "prepare+apply", "prepare-only", "apply-only", "workspace":
		return true
	default:
		return false
	}
}

func isCanonicalCompleteness(value string) bool {
	switch strings.TrimSpace(value) {
	case "starter", "complete", "refine":
		return true
	default:
		return false
	}
}

func isCanonicalTopology(value string) bool {
	switch strings.TrimSpace(value) {
	case "single-node", "multi-node", "ha", "unspecified":
		return true
	default:
		return false
	}
}

func askcontractPathAllowed(path string) bool {
	return path == "workflows/prepare.yaml" || path == "workflows/vars.yaml" || strings.HasPrefix(path, "workflows/scenarios/") || strings.HasPrefix(path, "workflows/components/")
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

// ValidatePlanStructure enforces only pre-generation viability.
// Recoverable execution-detail weaknesses are carried forward into generation,
// judge, repair, and post-processing instead of stopping planning.
func ValidatePlanStructure(plan askcontract.PlanResponse) error {
	if plan.NeedsPrepare && !containsPlannedPath(plan.Files, "workflows/prepare.yaml") {
		return fmt.Errorf("plan response requires prepare but does not include workflows/prepare.yaml")
	}
	if strings.TrimSpace(plan.AuthoringBrief.ModeIntent) == "prepare+apply" {
		if !containsPlannedPath(plan.Files, "workflows/prepare.yaml") {
			return fmt.Errorf("plan response authoring brief requires prepare+apply but does not include workflows/prepare.yaml")
		}
		if entry := strings.TrimSpace(plan.EntryScenario); entry == "" || !containsPlannedPath(plan.Files, entry) {
			return fmt.Errorf("plan response authoring brief requires prepare+apply with a scenario entrypoint")
		}
	}
	if strings.TrimSpace(plan.AuthoringBrief.Topology) == "multi-node" || strings.TrimSpace(plan.AuthoringBrief.Topology) == "ha" {
		if strings.TrimSpace(plan.ExecutionModel.RoleExecution.RoleSelector) == "" && strings.TrimSpace(plan.AuthoringBrief.ModeIntent) != "prepare+apply" {
			return fmt.Errorf("plan response multi-node topology requires executionModel.roleExecution.roleSelector")
		}
		if plan.ExecutionModel.Verification.ExpectedNodeCount <= 0 && plan.AuthoringBrief.NodeCount <= 0 {
			return fmt.Errorf("plan response multi-node topology requires executionModel.verification.expectedNodeCount")
		}
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
