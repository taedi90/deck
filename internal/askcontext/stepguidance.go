package askcontext

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/askintent"
)

type StepGuidanceOptions struct {
	ModeIntent           string
	Topology             string
	RequiredCapabilities []string
}

const (
	confidenceHigh       = "high"
	confidenceMedium     = "medium"
	confidenceLow        = "low"
	candidatePromptLimit = 5
)

type candidateScore struct {
	step       StepKindContext
	score      int
	confidence string
	why        []string
}

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
		if shape := repairShapeForStep(item.Step); shape != "" {
			b.WriteString("  - minimal valid shape:\n")
			for _, line := range strings.Split(shape, "\n") {
				b.WriteString("      ")
				b.WriteString(strings.TrimRight(line, " "))
				b.WriteString("\n")
			}
		}
	}
	if rules := documentKindRepairRules(validationError); len(rules) > 0 {
		b.WriteString("Document-kind exact fixes:\n")
		for _, rule := range rules {
			b.WriteString("- ")
			b.WriteString(rule)
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func repairShapeForStep(step StepKindContext) string {
	if strings.TrimSpace(step.MinimalShape) != "" {
		return strings.TrimSpace(step.MinimalShape)
	}
	if len(step.PromptExamples) > 0 {
		return strings.TrimSpace(step.PromptExamples[0].YAML)
	}
	return ""
}

func documentKindRepairRules(validationError string) []string {
	lower := strings.ToLower(strings.TrimSpace(validationError))
	rules := make([]string, 0, 4)
	if strings.Contains(lower, "workflows/components/") {
		if strings.Contains(lower, "additional property version is not allowed") || strings.Contains(lower, "additional property phases is not allowed") || strings.Contains(lower, "expected: object, given: array") {
			rules = append(rules,
				"component fragments must not contain top-level version or phases",
				"component fragments usually contain only `steps:` with step items beneath it",
				"if you revise a component, keep it as a YAML object, not a bare array",
			)
		}
	}
	if strings.Contains(lower, "spec is required") {
		rules = append(rules, "every generated step item must include a non-empty `spec` mapping, even when only one field is needed")
	}
	return dedupeStrings(rules)
}

func StrongTypedAlternatives(prompt string) []StepKindContext {
	selected := DiscoverCandidateSteps(prompt)
	out := make([]StepKindContext, 0, len(selected))
	for _, item := range selected {
		if item.Step.Kind == "Command" {
			continue
		}
		out = append(out, item.Step)
	}
	return out
}

func StrongTypedAlternativesWithOptions(prompt string, options StepGuidanceOptions) []StepKindContext {
	selected := DiscoverCandidateStepsWithOptions(prompt, options)
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
	return DiscoverCandidateSteps(prompt)
}

func SelectStepGuidanceWithOptions(prompt string, options StepGuidanceOptions) []SelectedStepGuidance {
	return DiscoverCandidateStepsWithOptions(prompt, options)
}

func DiscoverCandidateSteps(prompt string) []SelectedStepGuidance {
	return DiscoverCandidateStepsWithOptions(prompt, StepGuidanceOptions{})
}

func DiscoverCandidateStepsWithOptions(prompt string, options StepGuidanceOptions) []SelectedStepGuidance {
	manifest := Current()
	lower := strings.ToLower(strings.TrimSpace(prompt))
	capabilities := map[string]bool{}
	for _, capability := range options.RequiredCapabilities {
		capability = strings.ToLower(strings.TrimSpace(capability))
		if capability != "" {
			capabilities[capability] = true
		}
	}
	modeIntent := strings.ToLower(strings.TrimSpace(options.ModeIntent))
	topology := strings.ToLower(strings.TrimSpace(options.Topology))
	scoredKinds := make([]candidateScore, 0, len(manifest.StepKinds))
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
			case "CheckCluster", "LoadImage", "CheckHost", "InitKubeadm", "UpgradeKubeadm", "JoinKubeadm":
				score += 20
			}
		}
		applyCapabilityScore(&score, &why, step, capabilities, modeIntent, topology)
		if score > 0 {
			scoredKinds = append(scoredKinds, candidateScore{step: step, score: score, confidence: confidenceForScore(score), why: dedupeStrings(why)})
		}
	}
	sort.Slice(scoredKinds, func(i, j int) bool {
		if scoredKinds[i].score == scoredKinds[j].score {
			return scoredKinds[i].step.Kind < scoredKinds[j].step.Kind
		}
		return scoredKinds[i].score > scoredKinds[j].score
	})
	selected := selectCandidateSteps(scoredKinds, capabilities)
	if len(selected) == 0 {
		for _, kind := range manifest.StepKinds {
			if kind.Kind == "WriteFile" || kind.Kind == "ConfigureRepository" || kind.Kind == "ManageService" || kind.Kind == "Command" {
				selected = append(selected, SelectedStepGuidance{Step: kind, Confidence: confidenceLow, Reasons: []string{"fallback guidance"}, WhyRelevant: "fallback guidance"})
			}
		}
	}
	return selected
}

func selectRepairGuidance(prompt string, validationError string) []SelectedStepGuidance {
	validationLower := strings.ToLower(strings.TrimSpace(validationError))
	selected := make([]SelectedStepGuidance, 0)
	for _, item := range DiscoverCandidateSteps(prompt) {
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
			selected = append(selected, SelectedStepGuidance{Step: step, Confidence: confidenceHigh, Reasons: []string{"validator output matches known step repair hint"}, WhyRelevant: "validator output matches known step repair hint"})
			break
		}
	}
	return selected
}

func RelevantStepKinds(prompt string) []StepKindContext {
	selected := DiscoverCandidateSteps(prompt)
	out := make([]StepKindContext, 0, len(selected))
	for _, item := range selected {
		out = append(out, item.Step)
	}
	return out
}

func RelevantStepKindsWithOptions(prompt string, options StepGuidanceOptions) []StepKindContext {
	selected := DiscoverCandidateStepsWithOptions(prompt, options)
	out := make([]StepKindContext, 0, len(selected))
	for _, item := range selected {
		out = append(out, item.Step)
	}
	return out
}

func StepGuidanceBlock(route askintent.Route, prompt string) string {
	return StepGuidanceBlockWithOptions(route, prompt, StepGuidanceOptions{})
}

func StepGuidanceBlockWithOptions(route askintent.Route, prompt string, options StepGuidanceOptions) string {
	selected := DiscoverCandidateStepsWithOptions(prompt, options)
	if len(selected) == 0 {
		return ""
	}
	b := &strings.Builder{}
	switch route {
	case askintent.RouteReview, askintent.RouteExplain, askintent.RouteQuestion:
		b.WriteString("Candidate typed steps:\n")
		for _, item := range selected {
			b.WriteString("- ")
			b.WriteString(item.Step.Kind)
			b.WriteString(": ")
			b.WriteString(item.Step.Summary)
			if strings.TrimSpace(item.Confidence) != "" {
				b.WriteString(" Confidence: ")
				b.WriteString(item.Confidence)
			}
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
		b.WriteString("Candidate typed steps you may choose from:\n")
		b.WriteString("- These are hints, not required selections. You do not need to use every candidate. Choose the smallest valid typed-step set that satisfies the request.\n")
		b.WriteString("- If you use a candidate, follow its exact schema shape. `required` fields must always be present, `optional` fields can be omitted, and `conditional` fields are only needed when that branch of the schema is used.\n")
		for _, item := range selected {
			b.WriteString("- ")
			b.WriteString(item.Step.Kind)
			b.WriteString(": ")
			b.WriteString(item.Step.Summary)
			if strings.TrimSpace(item.Confidence) != "" {
				b.WriteString(" Confidence: ")
				b.WriteString(item.Confidence)
			}
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
				requirement := strings.TrimSpace(field.Requirement)
				if requirement == "" {
					requirement = "optional"
				}
				b.WriteString(" [")
				b.WriteString(requirement)
				b.WriteString("]")
				b.WriteString(": ")
				b.WriteString(field.Description)
				if field.Example != "" {
					b.WriteString(" Example: ")
					b.WriteString(field.Example)
				}
				b.WriteString("\n")
			}
			for _, rule := range item.Step.SchemaRuleSummaries {
				if strings.TrimSpace(rule) == "" {
					continue
				}
				b.WriteString("  - rule: ")
				b.WriteString(strings.TrimSpace(rule))
				b.WriteString("\n")
			}
			showExamples := item.Confidence == confidenceHigh || (item.Confidence == confidenceMedium && strings.Contains(strings.ToLower(prompt), strings.ToLower(item.Step.Kind)))
			for _, example := range item.Step.PromptExamples {
				if !showExamples {
					continue
				}
				if strings.TrimSpace(example.YAML) == "" {
					continue
				}
				b.WriteString("  - example")
				if example.Purpose != "" {
					b.WriteString(" (")
					b.WriteString(example.Purpose)
					b.WriteString(")")
				}
				b.WriteString(" [minimal valid shape]")
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

func confidenceForScore(score int) string {
	switch {
	case score >= 70:
		return confidenceHigh
	case score >= 35:
		return confidenceMedium
	default:
		return confidenceLow
	}
}

func selectCandidateSteps(scoredKinds []candidateScore, capabilities map[string]bool) []SelectedStepGuidance {
	selectedKinds := map[string]bool{}
	out := make([]SelectedStepGuidance, 0, candidatePromptLimit)
	appendCandidate := func(item candidateScore) {
		if selectedKinds[item.step.Kind] {
			return
		}
		selectedKinds[item.step.Kind] = true
		out = append(out, SelectedStepGuidance{Step: item.step, Confidence: item.confidence, Reasons: append([]string(nil), item.why...), WhyRelevant: strings.Join(item.why, "; ")})
	}
	for _, item := range scoredKinds {
		if item.confidence == confidenceHigh {
			appendCandidate(item)
		}
	}
	ensureCapabilityCandidate(&out, selectedKinds, scoredKinds, capabilities, "kubeadm-bootstrap", []string{"InitKubeadm", "CheckHost", "LoadImage", "CheckCluster"})
	ensureCapabilityCandidate(&out, selectedKinds, scoredKinds, capabilities, "kubeadm-join", []string{"JoinKubeadm"})
	ensureCapabilityCandidate(&out, selectedKinds, scoredKinds, capabilities, "cluster-verification", []string{"CheckCluster"})
	ensureCapabilityCandidate(&out, selectedKinds, scoredKinds, capabilities, "package-staging", []string{"DownloadPackage", "InstallPackage"})
	ensureCapabilityCandidate(&out, selectedKinds, scoredKinds, capabilities, "image-staging", []string{"DownloadImage", "LoadImage"})
	ensureCapabilityCandidate(&out, selectedKinds, scoredKinds, capabilities, "repository-setup", []string{"ConfigureRepository", "RefreshRepository"})
	for _, item := range scoredKinds {
		if len(out) >= candidatePromptLimit {
			break
		}
		if item.confidence == confidenceMedium {
			appendCandidate(item)
		}
	}
	for _, item := range scoredKinds {
		if len(out) >= candidatePromptLimit {
			break
		}
		if item.confidence == confidenceLow {
			appendCandidate(item)
		}
	}
	return out
}

func ensureCapabilityCandidate(out *[]SelectedStepGuidance, selectedKinds map[string]bool, scoredKinds []candidateScore, capabilities map[string]bool, capability string, preferredKinds []string) {
	if !capabilities[capability] {
		return
	}
	for _, selected := range *out {
		if containsGuidanceString(preferredKinds, selected.Step.Kind) {
			return
		}
	}
	for _, item := range scoredKinds {
		if containsGuidanceString(preferredKinds, item.step.Kind) {
			if !selectedKinds[item.step.Kind] {
				selectedKinds[item.step.Kind] = true
				*out = append(*out, SelectedStepGuidance{Step: item.step, Confidence: item.confidence, Reasons: append([]string(nil), item.why...), WhyRelevant: strings.Join(item.why, "; ")})
			}
			return
		}
	}
}

func containsGuidanceString(values []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, value := range values {
		if strings.TrimSpace(value) == want {
			return true
		}
	}
	return false
}

func applyCapabilityScore(score *int, why *[]string, step StepKindContext, capabilities map[string]bool, modeIntent string, topology string) {
	boost := func(points int, reason string) {
		*score += points
		*why = append(*why, reason)
	}
	if capabilities["package-staging"] {
		switch step.Kind {
		case "DownloadPackage", "InstallPackage":
			boost(35, "supports package staging capability")
		}
	}
	if capabilities["image-staging"] {
		switch step.Kind {
		case "DownloadImage", "LoadImage":
			boost(35, "supports image staging capability")
		}
	}
	if capabilities["repository-setup"] {
		switch step.Kind {
		case "ConfigureRepository", "RefreshRepository":
			boost(30, "supports repository setup capability")
		}
	}
	if capabilities["prepare-artifacts"] && modeIntent == "prepare+apply" {
		switch step.Kind {
		case "DownloadPackage", "DownloadImage":
			boost(20, "fits prepare stage in prepare+apply workflow")
		case "Command":
			*score -= 10
		}
	}
	if capabilities["kubeadm-bootstrap"] {
		switch step.Kind {
		case "CheckHost", "InitKubeadm", "CheckCluster", "LoadImage", "InstallPackage":
			boost(28, "supports kubeadm bootstrap capability")
		}
	}
	if capabilities["kubeadm-join"] {
		switch step.Kind {
		case "JoinKubeadm":
			boost(40, "supports kubeadm join capability")
		case "InitKubeadm", "CheckCluster":
			boost(15, "supports multi-node kubeadm flow")
		}
	}
	if capabilities["cluster-verification"] && step.Kind == "CheckCluster" {
		boost(30, "supports cluster verification capability")
	}
	if topology == "multi-node" || topology == "ha" {
		switch step.Kind {
		case "JoinKubeadm":
			boost(35, "topology requires node join flow")
		case "CheckCluster":
			boost(18, "topology benefits from cluster verification")
		}
	}
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
