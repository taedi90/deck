package askcli

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/askcontract"
	"github.com/Airgap-Castaways/deck/internal/askintent"
	"github.com/Airgap-Castaways/deck/internal/askpolicy"
	"github.com/Airgap-Castaways/deck/internal/askprovider"
	"github.com/Airgap-Castaways/deck/internal/askretrieve"
	"github.com/Airgap-Castaways/deck/internal/workflowissues"
)

func buildPlanWithReview(ctx context.Context, client askprovider.Client, cfg askconfigSettings, decision askintent.Decision, retrieval askretrieve.RetrievalResult, requestText string, workspace askretrieve.WorkspaceSummary, requirements askpolicy.ScenarioRequirements, logger askLogger) (askcontract.PlanResponse, askcontract.PlanCriticResponse, bool, error) {
	var planned askcontract.PlanResponse
	var critic askcontract.PlanCriticResponse
	currentPrompt := requestText
	for attempt := 1; attempt <= 2; attempt++ {
		current, planErr := planWithLLM(ctx, client, cfg, decision, retrieval, currentPrompt, workspace, logger)
		if planErr != nil {
			logger.logf("debug", "[ask][phase:plan:fallback] error=%v\n", planErr)
			planned = askpolicy.BuildPlanDefaults(requirements, requestText, decision, workspace)
			return planned, askcontract.PlanCriticResponse{}, true, nil
		}
		planned = current
		criticResp, criticErr := critiquePlanWithLLM(ctx, client, cfg, planned, logger)
		if criticErr != nil {
			logger.logf("debug", "[ask][phase:plan-critic:skip] error=%v\n", criticErr)
			return planned, askcontract.PlanCriticResponse{}, false, nil
		}
		critic = criticResp
		critic = normalizePlanCritic(planned, critic)
		if len(critic.Blocking) == 0 && len(critic.MissingContracts) == 0 {
			return planned, critic, false, nil
		}
		logger.logf("debug", "[ask][phase:plan-critic:retry] attempt=%d blocking=%d missing=%d\n", attempt, len(critic.Blocking), len(critic.MissingContracts))
		if attempt == 2 {
			return planned, critic, false, nil
		}
		currentPrompt = appendPlanCriticRetryPrompt(requestText, critic)
	}
	return planned, critic, false, nil
}

func appendPlanCriticRetryPrompt(base string, critic askcontract.PlanCriticResponse) string {
	b := &strings.Builder{}
	b.WriteString(strings.TrimSpace(base))
	b.WriteString("\n\nPlan critic requested a stronger plan before generation. Address these issues in the next plan JSON:\n")
	for _, finding := range critic.Findings {
		message := strings.TrimSpace(finding.Message)
		if message == "" {
			continue
		}
		label := strings.TrimSpace(string(finding.Severity))
		if label == "" {
			label = string(workflowissues.SeverityAdvisory)
		}
		b.WriteString("- ")
		b.WriteString(label)
		if code := strings.TrimSpace(string(finding.Code)); code != "" {
			b.WriteString(" [")
			b.WriteString(code)
			b.WriteString("]")
		}
		if path := strings.TrimSpace(finding.Path); path != "" {
			b.WriteString(" @ ")
			b.WriteString(path)
		}
		b.WriteString(": ")
		b.WriteString(message)
		b.WriteString("\n")
	}
	for _, item := range critic.SuggestedFixes {
		if strings.TrimSpace(item) != "" {
			b.WriteString("- fix: ")
			b.WriteString(strings.TrimSpace(item))
			b.WriteString("\n")
		}
	}
	b.WriteString("Required plan updates before generation:\n")
	b.WriteString("- Ensure executionModel.artifactContracts explicitly cover every staged package/image handoff used by apply.\n")
	b.WriteString("- Ensure exactly one authoritative join handoff contract is present when workers join a cluster.\n")
	b.WriteString("- Ensure executionModel.roleExecution and executionModel.verification match the requested topology and role behavior.\n")
	b.WriteString("- Prefer recoverable omissions to be fixed in the plan rather than adding new blockers or open questions.\n")
	return strings.TrimSpace(b.String())
}

func normalizePlanCritic(plan askcontract.PlanResponse, critic askcontract.PlanCriticResponse) askcontract.PlanCriticResponse {
	findings := normalizedPlanCriticFindings(plan, critic)
	blocking := make([]string, 0, len(findings))
	advisory := make([]string, 0, len(findings))
	missing := make([]string, 0, len(findings))
	for _, finding := range findings {
		text := strings.TrimSpace(finding.Message)
		if text == "" {
			continue
		}
		switch {
		case planCriticFindingIsFatal(finding):
			blocking = append(blocking, text)
		case finding.Severity == workflowissues.SeverityMissingContract && !planCriticFindingIsRecoverable(finding):
			missing = append(missing, text)
		default:
			advisory = append(advisory, text)
		}
	}
	critic.Findings = findings
	critic.Blocking = dedupe(blocking)
	critic.MissingContracts = dedupe(missing)
	critic.Advisory = dedupe(advisory)
	return critic
}

func hasFatalPlanReviewIssues(plan askcontract.PlanResponse, critic askcontract.PlanCriticResponse) bool {
	return len(fatalPlanReviewReasons(plan, critic)) > 0
}

func fatalPlanReviewReasons(plan askcontract.PlanResponse, critic askcontract.PlanCriticResponse) []string {
	reasons := []string{}
	for _, finding := range normalizedPlanCriticFindings(plan, critic) {
		if !planCriticFindingIsFatal(finding) {
			continue
		}
		if text := strings.TrimSpace(finding.Message); text != "" {
			reasons = append(reasons, text)
		}
	}
	for _, item := range fatalPlanBlockers(plan) {
		if strings.TrimSpace(item) != "" {
			reasons = append(reasons, item)
		}
	}
	if strings.TrimSpace(plan.EntryScenario) == "" {
		reasons = append(reasons, "no viable entry scenario can be determined")
	}
	if strings.TrimSpace(plan.AuthoringBrief.ModeIntent) == "prepare+apply" && !hasPlannedPath(plan.Files, "workflows/prepare.yaml") {
		reasons = append(reasons, "required prepare/apply file structure is absent and cannot be defaulted")
	}
	if strings.TrimSpace(plan.AuthoringBrief.ModeIntent) == "prepare+apply" && !hasPlannedPath(plan.Files, filepath.ToSlash(strings.TrimSpace(plan.EntryScenario))) {
		reasons = append(reasons, "required prepare/apply entry scenario is absent and cannot be defaulted")
	}
	if isMultiRoleTopology(plan.AuthoringBrief.Topology) && strings.TrimSpace(plan.ExecutionModel.RoleExecution.RoleSelector) == "" && !authoringBriefSuggestsRoleSelector(plan.AuthoringBrief) {
		reasons = append(reasons, "multi-role request has no viable role selector or branching model")
	}
	if needsArtifactPreparation(plan) && len(plan.ExecutionModel.ArtifactContracts) == 0 && !planHasArtifactConsumerPath(plan) {
		reasons = append(reasons, "artifact-dependent request has no viable consumer path")
	}
	return dedupe(reasons)
}

func fatalPlanBlockers(plan askcontract.PlanResponse) []string {
	_ = plan
	return nil
}

func hasPlannedPath(files []askcontract.PlanFile, want string) bool {
	want = strings.TrimSpace(want)
	for _, file := range files {
		if filepath.ToSlash(strings.TrimSpace(file.Path)) == want {
			return true
		}
	}
	return false
}

func isMultiRoleTopology(topology string) bool {
	switch strings.TrimSpace(topology) {
	case "multi-node", "ha":
		return true
	default:
		return false
	}
}

func needsArtifactPreparation(plan askcontract.PlanResponse) bool {
	return plan.NeedsPrepare || len(plan.ArtifactKinds) > 0 || strings.TrimSpace(plan.AuthoringBrief.ModeIntent) == "prepare+apply"
}

func authoringBriefSuggestsRoleSelector(brief askcontract.AuthoringBrief) bool {
	if strings.TrimSpace(brief.ModeIntent) != "prepare+apply" {
		return false
	}
	for _, capability := range brief.RequiredCapabilities {
		capability = strings.TrimSpace(capability)
		if capability == "kubeadm-join" || capability == "cluster-verification" {
			return true
		}
	}
	return isMultiRoleTopology(brief.Topology)
}

func planHasArtifactConsumerPath(plan askcontract.PlanResponse) bool {
	for _, file := range plan.Files {
		path := filepath.ToSlash(strings.TrimSpace(file.Path))
		if strings.HasPrefix(path, "workflows/scenarios/") {
			return true
		}
	}
	return false
}

func normalizedPlanCriticFindings(plan askcontract.PlanResponse, critic askcontract.PlanCriticResponse) []askcontract.PlanCriticFinding {
	findings := make([]askcontract.PlanCriticFinding, 0, len(critic.Findings)+len(critic.Blocking)+len(critic.Advisory)+len(critic.MissingContracts))
	findings = append(findings, critic.Findings...)
	seen := map[string]bool{}
	for _, finding := range critic.Findings {
		seen[planCriticFindingKey(finding)] = true
	}
	appendLegacy := func(items []string, severity workflowissues.Severity) {
		for _, item := range items {
			text := strings.TrimSpace(item)
			if text == "" {
				continue
			}
			finding := legacyPlanCriticFinding(text, severity)
			key := planCriticFindingKey(finding)
			if seen[key] {
				continue
			}
			seen[key] = true
			findings = append(findings, finding)
		}
	}
	appendLegacy(critic.Blocking, workflowissues.SeverityBlocking)
	appendLegacy(critic.Advisory, workflowissues.SeverityAdvisory)
	appendLegacy(critic.MissingContracts, workflowissues.SeverityMissingContract)
	return dedupePlanCriticFindings(findings)
}

func dedupePlanCriticFindings(findings []askcontract.PlanCriticFinding) []askcontract.PlanCriticFinding {
	seen := map[string]bool{}
	out := make([]askcontract.PlanCriticFinding, 0, len(findings))
	for _, finding := range findings {
		finding.Code = workflowissues.Code(strings.TrimSpace(string(finding.Code)))
		finding.Severity = workflowissues.Severity(strings.TrimSpace(string(finding.Severity)))
		finding.Message = strings.TrimSpace(finding.Message)
		finding.Path = strings.TrimSpace(finding.Path)
		if finding.Message == "" {
			continue
		}
		if finding.Severity == "" {
			finding.Severity = workflowissues.SeverityAdvisory
		}
		key := planCriticFindingKey(finding)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, finding)
	}
	return out
}

func planCriticFindingKey(finding askcontract.PlanCriticFinding) string {
	return strings.Join([]string{strings.TrimSpace(string(finding.Code)), strings.TrimSpace(string(finding.Severity)), strings.TrimSpace(finding.Path), strings.TrimSpace(finding.Message)}, "|")
}

func planCriticFindingIsFatal(finding askcontract.PlanCriticFinding) bool {
	if spec, ok := workflowissues.SpecFor(finding.Code); ok {
		return spec.DefaultSeverity == workflowissues.SeverityBlocking && !spec.DefaultRecoverable
	}
	if finding.Recoverable {
		return false
	}
	return finding.Severity == workflowissues.SeverityBlocking
}

func planCriticFindingIsRecoverable(finding askcontract.PlanCriticFinding) bool {
	if spec, ok := workflowissues.SpecFor(finding.Code); ok {
		return spec.DefaultRecoverable
	}
	return finding.Recoverable
}

func legacyPlanCriticFinding(text string, severity workflowissues.Severity) askcontract.PlanCriticFinding {
	recoverable := severity != workflowissues.SeverityBlocking
	return askcontract.PlanCriticFinding{
		Code:        workflowissues.CodeAskUnclassifiedCriticFinding,
		Severity:    severity,
		Message:     text,
		Recoverable: recoverable,
	}
}
