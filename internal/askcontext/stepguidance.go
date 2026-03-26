package askcontext

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/askintent"
)

func StepKind(kind string) (StepKindContext, bool) {
	for _, step := range Current().StepKinds {
		if step.Kind == strings.TrimSpace(kind) {
			return step, true
		}
	}
	return StepKindContext{}, false
}

func ValidationFixesForError(message string) []string {
	message = strings.ToLower(strings.TrimSpace(message))
	if message == "" {
		return nil
	}
	fixes := make([]string, 0)
	for _, step := range Current().StepKinds {
		for _, hint := range step.ValidationHints {
			needle := strings.ToLower(strings.TrimSpace(hint.ErrorContains))
			if needle == "" || !strings.Contains(message, needle) {
				continue
			}
			if fix := strings.TrimSpace(hint.Fix); fix != "" {
				fixes = append(fixes, fix)
			}
		}
	}
	return dedupeStrings(fixes)
}

func RepairGuidanceBlock(prompt string, validationError string) string {
	validationError = strings.TrimSpace(validationError)
	if validationError == "" {
		return ""
	}
	selected := selectRepairGuidance(prompt, validationError)
	if len(selected) == 0 {
		return ""
	}
	b := &strings.Builder{}
	b.WriteString("Relevant repair guidance:\n")
	for _, item := range selected {
		b.WriteString("- ")
		b.WriteString(item.Step.Kind)
		if item.WhyRelevant != "" {
			b.WriteString(": ")
			b.WriteString(item.WhyRelevant)
		}
		b.WriteString("\n")
		for _, hint := range item.Step.ValidationHints {
			needle := strings.ToLower(strings.TrimSpace(hint.ErrorContains))
			if needle == "" || !strings.Contains(strings.ToLower(validationError), needle) {
				continue
			}
			if fix := strings.TrimSpace(hint.Fix); fix != "" {
				b.WriteString("  - fix: ")
				b.WriteString(fix)
				b.WriteString("\n")
			}
		}
		for _, hint := range item.Step.RepairHints {
			if strings.TrimSpace(hint) == "" {
				continue
			}
			b.WriteString("  - hint: ")
			b.WriteString(hint)
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func StrongTypedAlternatives(prompt string) []StepKindContext {
	selected := SelectStepGuidance(prompt)
	out := make([]StepKindContext, 0, len(selected))
	for _, item := range selected {
		if item.Step.Kind == "Command" {
			continue
		}
		out = append(out, item.Step)
	}
	return out
}

func SelectStepGuidance(prompt string) []SelectedStepGuidance {
	manifest := Current()
	lower := strings.ToLower(strings.TrimSpace(prompt))
	type scored struct {
		step  StepKindContext
		score int
		why   []string
	}
	scoredKinds := make([]scored, 0, len(manifest.StepKinds))
	for _, step := range manifest.StepKinds {
		score := 0
		why := make([]string, 0, 4)
		if strings.Contains(lower, strings.ToLower(step.Kind)) {
			score += 100
			why = append(why, "request names the step kind")
		}
		if category := strings.ToLower(strings.TrimSpace(step.Category)); category != "" && strings.Contains(lower, category) {
			score += 20
			why = append(why, fmt.Sprintf("matches %s category", step.Category))
		}
		for _, signal := range step.MatchSignals {
			signal = strings.ToLower(strings.TrimSpace(signal))
			if signal == "" || !strings.Contains(lower, signal) {
				continue
			}
			score += 28
			why = append(why, fmt.Sprintf("matches %q", signal))
		}
		for _, token := range strings.Fields(strings.ToLower(step.WhenToUse)) {
			if len(token) > 4 && strings.Contains(lower, token) {
				score += 4
			}
		}
		for _, anti := range step.AntiSignals {
			anti = strings.ToLower(strings.TrimSpace(anti))
			if anti != "" && strings.Contains(lower, anti) {
				score -= 10
			}
		}
		if strings.Contains(lower, "typed") || strings.Contains(lower, "where possible") {
			if step.Kind == "Command" {
				score -= 40
			} else {
				score += 10
			}
		}
		if step.Kind == "Command" {
			score -= 15
		}
		if strings.Contains(lower, "repo") || strings.Contains(lower, "repository") {
			switch step.Kind {
			case "ConfigureRepository":
				score += 45
				why = append(why, "request mentions repositories")
			case "RefreshRepository":
				score += 20
			}
		}
		if strings.Contains(lower, "service") || strings.Contains(lower, "enable") || strings.Contains(lower, "restart") {
			if step.Kind == "ManageService" {
				score += 25
				why = append(why, "request mentions service lifecycle")
			}
		}
		if strings.Contains(lower, "docker") || strings.Contains(lower, "package") || strings.Contains(lower, "dnf") {
			switch step.Kind {
			case "InstallPackage", "DownloadPackage", "ManageService":
				score += 25
			case "ConfigureRepository":
				score += 40
			case "RefreshRepository":
				score += 15
			}
		}
		if strings.Contains(lower, "rocky") || strings.Contains(lower, "rhel") {
			if step.Kind == "ConfigureRepository" {
				score += 10
			}
		}
		if strings.Contains(lower, "kubeadm") || strings.Contains(lower, "kubernetes") {
			switch step.Kind {
			case "CheckCluster", "LoadImage", "CheckHost", "InitKubeadm", "UpgradeKubeadm":
				score += 20
			}
		}
		if score > 0 {
			scoredKinds = append(scoredKinds, scored{step: step, score: score, why: why})
		}
	}
	sort.Slice(scoredKinds, func(i, j int) bool {
		if scoredKinds[i].score == scoredKinds[j].score {
			return scoredKinds[i].step.Kind < scoredKinds[j].step.Kind
		}
		return scoredKinds[i].score > scoredKinds[j].score
	})
	limit := 5
	if len(scoredKinds) < limit {
		limit = len(scoredKinds)
	}
	out := make([]SelectedStepGuidance, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, SelectedStepGuidance{Step: scoredKinds[i].step, WhyRelevant: strings.Join(dedupeStrings(scoredKinds[i].why), "; ")})
	}
	if len(out) == 0 {
		for _, kind := range manifest.StepKinds {
			if kind.Kind == "WriteFile" || kind.Kind == "ConfigureRepository" || kind.Kind == "ManageService" || kind.Kind == "Command" {
				out = append(out, SelectedStepGuidance{Step: kind, WhyRelevant: "fallback guidance"})
			}
		}
	}
	return out
}

func selectRepairGuidance(prompt string, validationError string) []SelectedStepGuidance {
	validationLower := strings.ToLower(strings.TrimSpace(validationError))
	selected := make([]SelectedStepGuidance, 0)
	for _, item := range SelectStepGuidance(prompt) {
		matched := false
		if strings.Contains(validationLower, strings.ToLower(item.Step.Kind)) {
			matched = true
		}
		for _, hint := range item.Step.ValidationHints {
			needle := strings.ToLower(strings.TrimSpace(hint.ErrorContains))
			if needle != "" && strings.Contains(validationLower, needle) {
				matched = true
				break
			}
		}
		if matched {
			selected = append(selected, item)
		}
	}
	if len(selected) > 0 {
		return selected
	}
	for _, step := range Current().StepKinds {
		for _, hint := range step.ValidationHints {
			needle := strings.ToLower(strings.TrimSpace(hint.ErrorContains))
			if needle == "" || !strings.Contains(validationLower, needle) {
				continue
			}
			selected = append(selected, SelectedStepGuidance{Step: step, WhyRelevant: "validator output matches known step repair hint"})
			break
		}
	}
	return selected
}

func RelevantStepKinds(prompt string) []StepKindContext {
	selected := SelectStepGuidance(prompt)
	out := make([]StepKindContext, 0, len(selected))
	for _, item := range selected {
		out = append(out, item.Step)
	}
	return out
}

func StepGuidanceBlock(route askintent.Route, prompt string) string {
	selected := SelectStepGuidance(prompt)
	if len(selected) == 0 {
		return ""
	}
	b := &strings.Builder{}
	switch route {
	case askintent.RouteReview, askintent.RouteExplain, askintent.RouteQuestion:
		b.WriteString("Relevant typed steps:\n")
		for _, item := range selected {
			b.WriteString("- ")
			b.WriteString(item.Step.Kind)
			b.WriteString(": ")
			b.WriteString(item.Step.Summary)
			if item.Step.WhenToUse != "" {
				b.WriteString(" When to use: ")
				b.WriteString(item.Step.WhenToUse)
			}
			if item.WhyRelevant != "" {
				b.WriteString(" Relevant because: ")
				b.WriteString(item.WhyRelevant)
			}
			b.WriteString("\n")
			for _, rule := range item.Step.QualityRules {
				if strings.TrimSpace(rule.Message) == "" {
					continue
				}
				b.WriteString("  - quality: ")
				b.WriteString(rule.Message)
				b.WriteString("\n")
			}
		}
	default:
		b.WriteString("Relevant typed steps:\n")
		for _, item := range selected {
			b.WriteString("- ")
			b.WriteString(item.Step.Kind)
			b.WriteString(": ")
			b.WriteString(item.Step.Summary)
			if item.Step.WhenToUse != "" {
				b.WriteString(" When to use: ")
				b.WriteString(item.Step.WhenToUse)
			}
			if item.WhyRelevant != "" {
				b.WriteString(" Relevant because: ")
				b.WriteString(item.WhyRelevant)
			}
			b.WriteString("\n")
			for _, field := range item.Step.KeyFields {
				b.WriteString("  - ")
				b.WriteString(field.Path)
				b.WriteString(": ")
				b.WriteString(field.Description)
				if field.Example != "" {
					b.WriteString(" Example: ")
					b.WriteString(field.Example)
				}
				b.WriteString("\n")
			}
			for _, example := range item.Step.PromptExamples {
				if strings.TrimSpace(example.YAML) == "" {
					continue
				}
				b.WriteString("  - example")
				if example.Purpose != "" {
					b.WriteString(" (")
					b.WriteString(example.Purpose)
					b.WriteString(")")
				}
				b.WriteString(":\n")
				for _, line := range strings.Split(strings.TrimSpace(example.YAML), "\n") {
					b.WriteString("      ")
					b.WriteString(strings.TrimRight(line, " "))
					b.WriteString("\n")
				}
			}
			for _, mistake := range item.Step.CommonMistakes {
				b.WriteString("  - mistake: ")
				b.WriteString(mistake)
				b.WriteString("\n")
			}
			for _, field := range item.Step.ConstrainedLiteralFields {
				b.WriteString("  - constrained: ")
				b.WriteString(field.Path)
				b.WriteString(" must stay literal")
				if len(field.AllowedValues) > 0 {
					b.WriteString(" (allowed: ")
					b.WriteString(strings.Join(field.AllowedValues, ", "))
					b.WriteString(")")
				}
				if strings.TrimSpace(field.Guidance) != "" {
					b.WriteString(": ")
					b.WriteString(strings.TrimSpace(field.Guidance))
				}
				b.WriteString("\n")
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func RelevantStepKindsBlock(prompt string) string {
	return StepGuidanceBlock(askintent.RouteDraft, prompt)
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
