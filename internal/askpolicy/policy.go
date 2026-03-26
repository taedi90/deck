package askpolicy

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/askcontext"
	"github.com/Airgap-Castaways/deck/internal/askcontract"
	"github.com/Airgap-Castaways/deck/internal/askintent"
	"github.com/Airgap-Castaways/deck/internal/askretrieve"
)

type ScenarioRequirements struct {
	AcceptanceLevel     string
	Connectivity        string
	NeedsPrepare        bool
	ArtifactKinds       []string
	RequiredFiles       []string
	EntryScenario       string
	TypedPreference     bool
	VarsAdvisories      []string
	ComponentAdvisories []string
	ScenarioIntent      []string
}

type EvaluationFinding struct {
	Severity string
	Code     string
	Message  string
	Fix      string
	Path     string
}

type EvaluationResult struct {
	Findings []EvaluationFinding
}

func BuildScenarioRequirements(prompt string, retrieval askretrieve.RetrievalResult, workspace askretrieve.WorkspaceSummary, decision askintent.Decision) ScenarioRequirements {
	artifactKinds := mergedArtifactKinds(prompt, retrieval)
	needsPrepare := len(artifactKinds) > 0 || strings.Contains(strings.ToLower(prompt), "prepare")
	req := ScenarioRequirements{
		AcceptanceLevel:     inferAcceptanceLevel(workspace, decision),
		Connectivity:        InferOfflineAssumption(prompt),
		NeedsPrepare:        needsPrepare,
		ArtifactKinds:       artifactKinds,
		RequiredFiles:       []string{"workflows/scenarios/apply.yaml"},
		EntryScenario:       "workflows/scenarios/apply.yaml",
		TypedPreference:     typedPreferenceRequested(prompt),
		VarsAdvisories:      inferVarsRecommendation(prompt),
		ComponentAdvisories: inferComponentRecommendation(prompt),
		ScenarioIntent:      inferScenarioIntent(prompt),
	}
	if req.AcceptanceLevel == "starter" {
		req.ComponentAdvisories = nil
	}
	if needsPrepare {
		req.RequiredFiles = append(req.RequiredFiles, "workflows/prepare.yaml")
	}
	if strings.Contains(strings.ToLower(prompt), "vars") || len(req.VarsAdvisories) > 0 {
		req.RequiredFiles = append(req.RequiredFiles, "workflows/vars.yaml")
	}
	if workspace.HasWorkflowTree && decision.Route == askintent.RouteRefine {
		// retain known required files for refine if prepare already exists
		if workspace.HasPrepare && !containsString(req.RequiredFiles, "workflows/prepare.yaml") && needsPrepare {
			req.RequiredFiles = append(req.RequiredFiles, "workflows/prepare.yaml")
		}
	}
	req.RequiredFiles = dedupeStrings(req.RequiredFiles)
	return req
}

func BuildPlanDefaults(req ScenarioRequirements, prompt string, decision askintent.Decision, workspace askretrieve.WorkspaceSummary) askcontract.PlanResponse {
	files := []askcontract.PlanFile{{Path: "workflows/scenarios/apply.yaml", Kind: "scenario", Action: "create", Purpose: "Primary workflow entrypoint"}}
	if req.NeedsPrepare {
		files = append(files, askcontract.PlanFile{Path: "workflows/prepare.yaml", Kind: "scenario", Action: "create", Purpose: "Prepare bundle inputs and dependencies"})
	}
	if strings.Contains(strings.ToLower(prompt), "vars") || len(req.VarsAdvisories) > 0 {
		files = append(files, askcontract.PlanFile{Path: "workflows/vars.yaml", Kind: "vars", Action: "create", Purpose: "Workspace variables"})
	}
	if workspace.HasWorkflowTree {
		for i := range files {
			if strings.HasPrefix(files[i].Path, "workflows/scenarios/") {
				files[i].Action = "update"
			}
		}
	}
	return askcontract.PlanResponse{
		Version:                 1,
		Request:                 strings.TrimSpace(prompt),
		Intent:                  string(decision.Route),
		Complexity:              "simple",
		OfflineAssumption:       req.Connectivity,
		NeedsPrepare:            req.NeedsPrepare,
		ArtifactKinds:           append([]string(nil), req.ArtifactKinds...),
		VarsRecommendation:      append([]string(nil), req.VarsAdvisories...),
		ComponentRecommendation: append([]string(nil), req.ComponentAdvisories...),
		TargetOutcome:           "Generate valid workflow files for the request.",
		Assumptions:             []string{"Use v1alpha1 workflow schema", "Prefer typed steps where possible"},
		EntryScenario:           req.EntryScenario,
		Files:                   files,
		ValidationChecklist:     defaultValidationChecklist(req),
	}
}

func MergeRequirementsWithPlan(req ScenarioRequirements, plan askcontract.PlanResponse) ScenarioRequirements {
	if strings.TrimSpace(plan.OfflineAssumption) != "" {
		req.Connectivity = strings.TrimSpace(plan.OfflineAssumption)
	}
	if plan.NeedsPrepare {
		req.NeedsPrepare = true
	}
	if len(plan.ArtifactKinds) > 0 {
		req.ArtifactKinds = NormalizeArtifactKinds(append(req.ArtifactKinds, plan.ArtifactKinds...))
	}
	if len(plan.VarsRecommendation) > 0 {
		req.VarsAdvisories = dedupeStrings(append(req.VarsAdvisories, plan.VarsRecommendation...))
	}
	if len(plan.ComponentRecommendation) > 0 {
		req.ComponentAdvisories = dedupeStrings(append(req.ComponentAdvisories, plan.ComponentRecommendation...))
	}
	if strings.TrimSpace(plan.EntryScenario) != "" {
		req.EntryScenario = strings.TrimSpace(plan.EntryScenario)
	}
	for _, file := range plan.Files {
		path := filepath.ToSlash(strings.TrimSpace(file.Path))
		if path != "" {
			req.RequiredFiles = append(req.RequiredFiles, path)
		}
	}
	req.RequiredFiles = dedupeStrings(req.RequiredFiles)
	return req
}

func EvaluateGeneration(req ScenarioRequirements, plan askcontract.PlanResponse, gen askcontract.GenerationResponse) EvaluationResult {
	findings := make([]EvaluationFinding, 0)
	if req.TypedPreference && countCommands(gen.Files) > 0 {
		alternatives := askcontext.StrongTypedAlternatives(plan.Request)
		if len(alternatives) > 0 {
			kinds := make([]string, 0, len(alternatives))
			for _, step := range alternatives {
				kinds = append(kinds, step.Kind)
			}
			findings = append(findings, EvaluationFinding{Severity: "advisory", Code: "typed_preference", Message: fmt.Sprintf("request prefers typed steps but generation still relies on Command; consider %s before falling back to Command", strings.Join(dedupeStrings(kinds), ", "))})
		}
	}
	applyPaths := scenarioLikePathsWithName(gen.Files, "apply")
	preparePaths := preparePathsFromGeneration(gen.Files)
	if strings.EqualFold(strings.TrimSpace(req.Connectivity), "offline") {
		for _, file := range gen.Files {
			clean := filepath.ToSlash(strings.TrimSpace(file.Path))
			if !containsString(applyPaths, clean) {
				continue
			}
			if onlineActivityDetected(file.Content) {
				findings = append(findings, EvaluationFinding{Severity: "blocking", Code: "offline_apply_online_retrieval", Message: fmt.Sprintf("offline apply workflow performs online retrieval: %s", clean), Fix: "Move online downloads, pulls, or mirror refresh work into prepare and keep apply offline", Path: clean})
			}
		}
	}
	for _, file := range gen.Files {
		clean := filepath.ToSlash(strings.TrimSpace(file.Path))
		for _, violation := range constrainedLiteralViolations(file.Content) {
			findings = append(findings, EvaluationFinding{Severity: "blocking", Code: "constrained_literal_template", Message: fmt.Sprintf("constrained typed field uses vars template in %s: %s", clean, violation), Fix: fmt.Sprintf("Keep %s as a literal allowed value instead of a vars template", violation), Path: clean})
		}
		if clean == "workflows/prepare.yaml" && prepareUsesCommandForArtifacts(file.Content, req.ArtifactKinds) {
			findings = append(findings, EvaluationFinding{Severity: "blocking", Code: "prepare_command_artifacts", Message: fmt.Sprintf("prepare workflow uses Command for artifact collection where a typed prepare step should be used: %s", clean), Fix: "Use typed prepare steps such as DownloadImage or DownloadPackage instead of Command for artifact collection in prepare", Path: clean})
		}
	}
	if req.NeedsPrepare && len(preparePaths) == 0 {
		findings = append(findings, EvaluationFinding{Severity: "blocking", Code: "missing_prepare", Message: "artifact-requiring request is missing workflows/prepare.yaml", Fix: "Add workflows/prepare.yaml when packages, images, binaries, or bundles must be prepared before apply", Path: "workflows/prepare.yaml"})
	}
	if req.NeedsPrepare && len(req.ArtifactKinds) > 0 && len(preparePaths) > 0 {
		generated := generatedMap(gen.Files)
		for _, path := range preparePaths {
			if !prepareAppearsArtifactOriented(generated[path].Content, req.ArtifactKinds) {
				findings = append(findings, EvaluationFinding{Severity: "blocking", Code: "prepare_not_artifact_oriented", Message: fmt.Sprintf("prepare workflow does not appear to stage requested artifacts: %s", path), Fix: "Ensure prepare collects or stages the packages, images, binaries, or bundles required before apply", Path: path})
			}
		}
	}
	if scenarioAppearsIncomplete(req, gen.Files) {
		findings = append(findings, EvaluationFinding{Severity: "blocking", Code: "scenario_intent_incomplete", Message: fmt.Sprintf("generated workflow does not fully match requested scenario intent: %s", strings.TrimSpace(plan.Request)), Fix: "Add the missing install or bootstrap stages required by the requested scenario before returning the workflow"})
	}
	if !generatedHas(gen.Files, "workflows/vars.yaml") {
		for _, reason := range req.VarsAdvisories {
			findings = append(findings, EvaluationFinding{Severity: "advisory", Code: "vars_advisory", Message: reason})
		}
		for _, reason := range inferVarsAdvisories(gen.Files) {
			findings = append(findings, EvaluationFinding{Severity: "advisory", Code: "vars_repetition", Message: reason})
		}
	}
	if !hasGeneratedComponents(gen.Files) {
		for _, reason := range req.ComponentAdvisories {
			findings = append(findings, EvaluationFinding{Severity: "advisory", Code: "component_advisory", Message: reason})
		}
		for _, reason := range inferComponentAdvisories(gen.Files) {
			findings = append(findings, EvaluationFinding{Severity: "advisory", Code: "component_repetition", Message: reason})
		}
	}
	return EvaluationResult{Findings: findings}
}

func defaultValidationChecklist(req ScenarioRequirements) []string {
	checklist := []string{"Workflow schema validates", "Entrypoint scenarios are loadable", "Planned files are generated"}
	if req.NeedsPrepare {
		checklist = append(checklist, "Artifact-requiring offline flows include a prepare workflow before apply")
	}
	if len(req.VarsAdvisories) > 0 {
		checklist = append(checklist, "Review whether repeated configurable values belong in workflows/vars.yaml")
	}
	if len(req.ComponentAdvisories) > 0 {
		checklist = append(checklist, "Review whether reusable repeated logic belongs in workflows/components/")
	}
	return checklist
}

func mergedArtifactKinds(prompt string, retrieval askretrieve.RetrievalResult) []string {
	kinds := InferArtifactKinds(strings.TrimSpace(prompt), nil)
	seen := map[string]bool{}
	for _, kind := range kinds {
		seen[kind] = true
	}
	for _, chunk := range retrieval.Chunks {
		if chunk.Evidence == nil {
			continue
		}
		for _, kind := range chunk.Evidence.ArtifactKinds {
			kind = strings.TrimSpace(strings.ToLower(kind))
			if kind == "" || seen[kind] {
				continue
			}
			seen[kind] = true
			kinds = append(kinds, kind)
		}
	}
	return dedupeStrings(kinds)
}

func inferVarsRecommendation(prompt string) []string {
	lower := strings.ToLower(strings.TrimSpace(prompt))
	if strings.Contains(lower, "package") || strings.Contains(lower, "packages") || strings.Contains(lower, "image") || strings.Contains(lower, "images") {
		return []string{"Use workflows/vars.yaml for repeated package, image, path, or version values."}
	}
	return nil
}

func inferComponentRecommendation(prompt string) []string {
	lower := strings.ToLower(strings.TrimSpace(prompt))
	if strings.Contains(lower, "prepare and apply") || strings.Contains(lower, "multi-step") || strings.Contains(lower, "reusable") {
		return []string{"Consider workflows/components/ for reusable repeated logic across phases or scenarios."}
	}
	return nil
}

func inferScenarioIntent(prompt string) []string {
	lower := strings.ToLower(strings.TrimSpace(prompt))
	intents := []string{}
	if strings.Contains(lower, "kubeadm") {
		intents = append(intents, "kubeadm")
	}
	if strings.Contains(lower, "registry mirror") || strings.Contains(lower, "mirror") {
		intents = append(intents, "registry-mirror")
	}
	return intents
}

func inferAcceptanceLevel(workspace askretrieve.WorkspaceSummary, decision askintent.Decision) string {
	if decision.Route == askintent.RouteRefine {
		if !workspace.HasWorkflowTree {
			return "starter"
		}
		return "refine"
	}
	if decision.Route == askintent.RouteDraft && !workspace.HasWorkflowTree {
		return "starter"
	}
	return "complete"
}

func typedPreferenceRequested(prompt string) bool {
	lower := strings.ToLower(strings.TrimSpace(prompt))
	return strings.Contains(lower, "typed step") || strings.Contains(lower, "typed steps") || strings.Contains(lower, "where possible")
}

func generatedMap(files []askcontract.GeneratedFile) map[string]askcontract.GeneratedFile {
	out := map[string]askcontract.GeneratedFile{}
	for _, file := range files {
		out[filepath.ToSlash(strings.TrimSpace(file.Path))] = file
	}
	return out
}

func generatedHas(files []askcontract.GeneratedFile, want string) bool {
	for _, file := range files {
		if filepath.ToSlash(strings.TrimSpace(file.Path)) == want {
			return true
		}
	}
	return false
}

func countCommands(files []askcontract.GeneratedFile) int {
	count := 0
	for _, file := range files {
		for _, line := range strings.Split(file.Content, "\n") {
			if strings.TrimSpace(line) == "kind: Command" {
				count++
			}
		}
	}
	return count
}

func scenarioLikePathsWithName(files []askcontract.GeneratedFile, name string) []string {
	paths := []string{}
	name = strings.ToLower(strings.TrimSpace(name))
	for _, file := range files {
		clean := filepath.ToSlash(strings.TrimSpace(file.Path))
		if strings.HasPrefix(clean, "workflows/scenarios/") && strings.Contains(strings.ToLower(clean), name) {
			paths = append(paths, clean)
		}
	}
	return dedupeStrings(paths)
}

func preparePathsFromGeneration(files []askcontract.GeneratedFile) []string {
	paths := []string{}
	for _, file := range files {
		if filepath.ToSlash(strings.TrimSpace(file.Path)) == "workflows/prepare.yaml" {
			paths = append(paths, "workflows/prepare.yaml")
		}
	}
	return dedupeStrings(paths)
}

func onlineActivityDetected(content string) bool {
	lower := strings.ToLower(content)
	hints := []string{"curl", "wget", "dnf download", "apt-get download", "docker pull", "ctr image pull", "podman pull", "repo sync", "refreshrepository", "downloadpackage", "downloadimage"}
	for _, hint := range hints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

func prepareAppearsArtifactOriented(content string, artifactKinds []string) bool {
	lower := strings.ToLower(content)
	hasPackages := strings.Contains(lower, "kind: downloadpackage") || strings.Contains(lower, "packages:")
	hasImages := strings.Contains(lower, "kind: downloadimage") || strings.Contains(lower, "images:")
	hasOutputDir := strings.Contains(lower, "outputdir:")
	for _, kind := range artifactKinds {
		switch strings.ToLower(strings.TrimSpace(kind)) {
		case "package":
			if hasPackages || strings.Contains(lower, "installpackage") || strings.Contains(lower, "/prepared/packages") || hasOutputDir {
				return true
			}
		case "image":
			if hasImages || strings.Contains(lower, "loadimage") || strings.Contains(lower, "/prepared/images") || hasOutputDir {
				return true
			}
		case "binary", "archive", "bundle", "repository-mirror":
			if strings.Contains(lower, "writefile") || strings.Contains(lower, "/prepared/files") || strings.Contains(lower, "configurerepository") || strings.Contains(lower, "command") {
				return true
			}
		}
	}
	return len(artifactKinds) == 0
}

func constrainedLiteralViolations(content string) []string {
	violations := []string{}
	for _, step := range askcontext.Current().StepKinds {
		for _, field := range step.ConstrainedLiteralFields {
			if fieldUsesVarsTemplate(content, field.Path) {
				violations = append(violations, field.Path)
			}
		}
	}
	return dedupeStrings(violations)
}

func fieldUsesVarsTemplate(content string, path string) bool {
	segments := strings.Split(strings.TrimSpace(path), ".")
	if len(segments) < 2 {
		return false
	}
	keys := segments[1:]
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != keys[0]+":" {
			continue
		}
		baseIndent := len(line) - len(strings.TrimLeft(line, " "))
		idx := i + 1
		for segIdx := 1; segIdx < len(keys) && idx < len(lines); {
			line = lines[idx]
			idx++
			if strings.TrimSpace(line) == "" {
				continue
			}
			indent := len(line) - len(strings.TrimLeft(line, " "))
			trimmed = strings.TrimSpace(line)
			if indent <= baseIndent {
				break
			}
			want := keys[segIdx] + ":"
			if trimmed == want {
				baseIndent = indent
				segIdx++
				continue
			}
			if segIdx == len(keys)-1 && strings.HasPrefix(trimmed, want) {
				value := strings.TrimSpace(strings.TrimPrefix(trimmed, want))
				return strings.Contains(value, "{{ .vars.")
			}
		}
	}
	return false
}

func prepareUsesCommandForArtifacts(content string, artifactKinds []string) bool {
	lower := strings.ToLower(content)
	if !strings.Contains(lower, "kind: command") {
		return false
	}
	if containsString(artifactKinds, "image") && (strings.Contains(lower, "docker pull") || strings.Contains(lower, "docker save") || strings.Contains(lower, "ctr image pull") || strings.Contains(lower, "podman pull")) {
		return true
	}
	if containsString(artifactKinds, "package") && (strings.Contains(lower, "dnf download") || strings.Contains(lower, "apt-get download")) {
		return true
	}
	return false
}

func inferVarsAdvisories(files []askcontract.GeneratedFile) []string {
	counts := map[string]int{}
	for _, file := range files {
		for _, line := range strings.Split(file.Content, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			if !strings.Contains(trimmed, ":") && !strings.HasPrefix(trimmed, "- ") {
				continue
			}
			if strings.Contains(trimmed, "{{") {
				continue
			}
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
			if idx := strings.Index(value, ":"); idx >= 0 {
				value = strings.TrimSpace(value[idx+1:])
			}
			value = strings.Trim(value, `"'`)
			if len(value) < 4 {
				continue
			}
			if looksInterestingRepeatedValue(value) {
				counts[value]++
			}
		}
	}
	advisories := []string{}
	for value, count := range counts {
		if count >= 2 {
			advisories = append(advisories, fmt.Sprintf("Repeated configurable value %q appears multiple times; consider moving it into workflows/vars.yaml", value))
		}
	}
	return dedupeStrings(advisories)
}

func inferComponentAdvisories(files []askcontract.GeneratedFile) []string {
	sequenceCounts := map[string]int{}
	for _, file := range files {
		kinds := []string{}
		for _, line := range strings.Split(file.Content, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "kind: ") {
				kinds = append(kinds, strings.TrimSpace(strings.TrimPrefix(trimmed, "kind: ")))
			}
		}
		if len(kinds) >= 2 {
			sequenceCounts[strings.Join(kinds, ">")] += 1
		}
	}
	advisories := []string{}
	for seq, count := range sequenceCounts {
		if count >= 2 {
			advisories = append(advisories, fmt.Sprintf("Repeated step sequence %q appears across generated files; consider moving it into workflows/components/", seq))
		}
	}
	return dedupeStrings(advisories)
}

func looksInterestingRepeatedValue(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.Contains(lower, "/") || strings.Contains(lower, ".") || strings.Contains(lower, "v1.") || strings.Contains(lower, "registry") || strings.Contains(lower, "kube") || strings.Contains(lower, "containerd")
}

func scenarioAppearsIncomplete(req ScenarioRequirements, files []askcontract.GeneratedFile) bool {
	if req.AcceptanceLevel == "starter" {
		return false
	}
	combined := strings.ToLower(combinedWorkflowContent(files))
	for _, intent := range req.ScenarioIntent {
		if intent == "kubeadm" {
			if !strings.Contains(combined, "initkubeadm") && !strings.Contains(combined, "upgradekubeadm") {
				return true
			}
			if !strings.Contains(combined, "checkcluster") {
				return true
			}
		}
	}
	if len(req.ArtifactKinds) > 0 && req.NeedsPrepare && len(preparePathsFromGeneration(files)) == 0 {
		return true
	}
	return false
}

func combinedWorkflowContent(files []askcontract.GeneratedFile) string {
	b := &strings.Builder{}
	for _, file := range files {
		b.WriteString("\n")
		b.WriteString(filepath.ToSlash(strings.TrimSpace(file.Path)))
		b.WriteString("\n")
		b.WriteString(file.Content)
	}
	return b.String()
}

func hasGeneratedComponents(files []askcontract.GeneratedFile) bool {
	for _, file := range files {
		if strings.HasPrefix(filepath.ToSlash(strings.TrimSpace(file.Path)), "workflows/components/") {
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func dedupeStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
