package askcli

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Airgap-Castaways/deck/internal/askintent"
	"github.com/Airgap-Castaways/deck/internal/askretrieve"
	"github.com/Airgap-Castaways/deck/internal/askreview"
)

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

func localClarify(prompt string) string {
	if strings.TrimSpace(prompt) == "" {
		return "Your request is empty. Please describe what you want to do."
	}
	return "Your request is too ambiguous to start workflow generation."
}

func clarificationSuggestions() []string {
	return []string{
		"Try: deck ask \"create an air-gapped rhel9 single-node kubeadm workflow\"",
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
	role := localWorkflowMode(file.Path, file.Content)
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
