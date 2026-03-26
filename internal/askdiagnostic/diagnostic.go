package askdiagnostic

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/askcontract"
	"github.com/Airgap-Castaways/deck/internal/askknowledge"
	"github.com/Airgap-Castaways/deck/internal/askpolicy"
)

type Diagnostic struct {
	Code         string   `json:"code"`
	Severity     string   `json:"severity"`
	File         string   `json:"file,omitempty"`
	Path         string   `json:"path,omitempty"`
	Message      string   `json:"message"`
	Expected     string   `json:"expected,omitempty"`
	Actual       string   `json:"actual,omitempty"`
	Allowed      []string `json:"allowed,omitempty"`
	Pattern      string   `json:"pattern,omitempty"`
	SourceRef    string   `json:"sourceRef,omitempty"`
	SuggestedFix string   `json:"suggestedFix,omitempty"`
}

func FromValidationError(message string, bundle askknowledge.Bundle) []Diagnostic {
	message = strings.TrimSpace(message)
	if message == "" {
		return nil
	}
	lower := strings.ToLower(message)
	diags := []Diagnostic{}
	appendDiag := func(diag Diagnostic) {
		if strings.TrimSpace(diag.Severity) == "" {
			diag.Severity = "blocking"
		}
		if strings.TrimSpace(diag.Message) == "" {
			return
		}
		diags = append(diags, diag)
	}
	appendDiag(Diagnostic{Code: "validation_error", Severity: "blocking", Message: message, SourceRef: "validate"})
	if strings.Contains(lower, "invalid map key") && strings.Contains(lower, ".vars.") {
		appendDiag(Diagnostic{Code: "typed_collection_template", Severity: "blocking", Message: "typed YAML array or object was templated as a single vars scalar", Expected: "native YAML array or object", Actual: "whole-value vars template", SourceRef: "workflow schema", SuggestedFix: "Inline schema-valid YAML arrays and objects instead of using a whole-value vars template."})
	}
	if strings.Contains(lower, "imports.0") && strings.Contains(lower, "expected: object") {
		appendDiag(Diagnostic{Code: "import_shape", Severity: "blocking", Path: "phases[].imports[]", Message: "phase import must be an object with path", Expected: "{path: component.yaml}", Actual: "string import entry", SourceRef: "workflow import rule", SuggestedFix: "Use imports entries like `- path: check-host.yaml`."})
	}
	if strings.Contains(lower, "additional property version is not allowed") && strings.Contains(lower, "workflows/components/") {
		appendDiag(Diagnostic{Code: "component_fragment_shape", Severity: "blocking", File: "workflows/components/*.yaml", Message: "component fragment includes workflow-level fields", Expected: "fragment object with top-level steps only", Actual: "full workflow document with version/phases", Allowed: bundle.Components.AllowedRootKeys, SourceRef: "deck-component-fragment.schema.json", SuggestedFix: "Keep component files as fragment documents with a top-level `steps:` key only."})
	}
	if strings.Contains(lower, "invalid type. expected: object, given: array") && strings.Contains(lower, "workflows/components/") {
		appendDiag(Diagnostic{Code: "component_fragment_shape", Severity: "blocking", File: "workflows/components/*.yaml", Message: "component fragment was emitted as a bare YAML array", Expected: "YAML object", Actual: "array", Allowed: bundle.Components.AllowedRootKeys, SourceRef: "deck-component-fragment.schema.json", SuggestedFix: "Wrap component steps under a top-level `steps:` mapping."})
	}
	if strings.Contains(lower, "is not supported for role prepare") {
		appendDiag(Diagnostic{Code: "role_support", Severity: "blocking", Message: "step kind is not supported for prepare", Expected: "prepare-supported typed step", Actual: "unsupported step kind for prepare", SourceRef: "workflow step role declarations", SuggestedFix: "Use a typed prepare step such as DownloadImage or DownloadPackage for artifact collection."})
	}
	if stepKind, specMessage, ok := extractStepSpecFailure(message); ok {
		if step, found := findStep(bundle, stepKind); found {
			if prop, ok := extractAdditionalProperty(specMessage); ok {
				appendDiag(Diagnostic{
					Code:         "unknown_step_field",
					Severity:     "blocking",
					Path:         fmt.Sprintf("%s.spec.%s", stepKind, prop),
					Message:      fmt.Sprintf("%s does not support spec.%s", stepKind, prop),
					Expected:     renderKeyFieldList(step),
					Actual:       "spec." + prop,
					SourceRef:    step.SchemaFile,
					SuggestedFix: fmt.Sprintf("Use documented %s fields such as %s.", stepKind, renderKeyFieldList(step)),
				})
			}
			if field, ok := extractRequiredField(specMessage); ok {
				suggested := fmt.Sprintf("Add required field spec.%s to %s.", field, stepKind)
				for _, key := range step.KeyFields {
					if strings.TrimSpace(key.Path) == "spec."+field && strings.TrimSpace(key.Description) != "" {
						suggested = fmt.Sprintf("Add required field spec.%s to %s. %s", field, stepKind, strings.TrimSpace(key.Description))
						break
					}
				}
				appendDiag(Diagnostic{
					Code:         "missing_step_field",
					Severity:     "blocking",
					Path:         fmt.Sprintf("%s.spec.%s", stepKind, field),
					Message:      fmt.Sprintf("%s requires spec.%s", stepKind, field),
					Expected:     "spec." + field,
					SourceRef:    step.SchemaFile,
					SuggestedFix: suggested,
				})
			}
		}
	}
	for _, constraint := range bundle.Constraints {
		if strings.Contains(lower, strings.ToLower(constraint.Path)) && strings.Contains(lower, "must be one of") {
			appendDiag(Diagnostic{Code: "constrained_literal", Severity: "blocking", Path: constraint.Path, Message: "constrained field rejected a non-literal value", Allowed: append([]string(nil), constraint.AllowedValues...), SourceRef: constraint.SourceRef, SuggestedFix: constraint.Guidance})
		}
		if strings.Contains(lower, strings.ToLower(constraint.Path)) && strings.Contains(lower, "does not match pattern") {
			appendDiag(Diagnostic{Code: "constrained_pattern", Severity: "blocking", Path: constraint.Path, Message: "pattern-constrained field failed validation", Pattern: "schema pattern", SourceRef: constraint.SourceRef, SuggestedFix: constraint.Guidance})
		}
	}
	return dedupe(diags)
}

func extractStepSpecFailure(message string) (string, string, bool) {
	marker := "): spec: "
	start := strings.Index(message, "(")
	end := strings.Index(message, marker)
	if start < 0 || end < 0 || end <= start+1 {
		return "", "", false
	}
	return strings.TrimSpace(message[start+1 : strings.Index(message[start:], ")")+start]), strings.TrimSpace(message[end+len(marker):]), true
}

func extractAdditionalProperty(message string) (string, bool) {
	const prefix = "Additional property "
	if !strings.HasPrefix(strings.TrimSpace(message), prefix) {
		return "", false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(message), prefix))
	idx := strings.Index(rest, " ")
	if idx < 0 {
		return strings.TrimSpace(rest), true
	}
	return strings.TrimSpace(rest[:idx]), true
}

func extractRequiredField(message string) (string, bool) {
	const suffix = " is required"
	trimmed := strings.TrimSpace(message)
	if !strings.HasSuffix(trimmed, suffix) {
		return "", false
	}
	field := strings.TrimSpace(strings.TrimSuffix(trimmed, suffix))
	field = strings.ReplaceAll(field, ": ", ".")
	field = strings.ReplaceAll(field, ":", ".")
	field = strings.ReplaceAll(field, " ", "")
	return field, true
}

func findStep(bundle askknowledge.Bundle, kind string) (askknowledge.StepKnowledge, bool) {
	for _, step := range bundle.Steps {
		if step.Kind == strings.TrimSpace(kind) {
			return step, true
		}
	}
	return askknowledge.StepKnowledge{}, false
}

func renderKeyFieldList(step askknowledge.StepKnowledge) string {
	paths := make([]string, 0, len(step.KeyFields))
	for _, field := range step.KeyFields {
		if strings.TrimSpace(field.Path) != "" {
			paths = append(paths, strings.TrimSpace(field.Path))
		}
	}
	if len(paths) == 0 {
		return "documented step fields"
	}
	return strings.Join(paths, ", ")
}

func FromEvaluation(findings []askpolicy.EvaluationFinding) []Diagnostic {
	diags := make([]Diagnostic, 0, len(findings))
	for _, finding := range findings {
		diags = append(diags, Diagnostic{
			Code:         finding.Code,
			Severity:     finding.Severity,
			File:         finding.Path,
			Path:         finding.Path,
			Message:      finding.Message,
			SuggestedFix: finding.Fix,
		})
	}
	return dedupe(diags)
}

func FromCritic(critic askcontract.CriticResponse) []Diagnostic {
	diags := []Diagnostic{}
	for _, item := range critic.Blocking {
		diags = append(diags, Diagnostic{Code: "critic_blocking", Severity: "blocking", Message: item})
	}
	for _, item := range critic.Advisory {
		diags = append(diags, Diagnostic{Code: "critic_advisory", Severity: "advisory", Message: item})
	}
	for _, item := range critic.RequiredFixes {
		diags = append(diags, Diagnostic{Code: "required_fix", Severity: "blocking", Message: item, SuggestedFix: item})
	}
	return dedupe(diags)
}

func JSON(diags []Diagnostic) string {
	raw, err := json.MarshalIndent(diags, "", "  ")
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func RepairPromptBlock(diags []Diagnostic) string {
	if len(diags) == 0 {
		return ""
	}
	b := &strings.Builder{}
	b.WriteString("Structured diagnostics JSON:\n")
	b.WriteString(JSON(diags))
	b.WriteString("\nDiagnostic repair priorities:\n")
	for _, diag := range diags {
		b.WriteString("- ")
		b.WriteString(diag.Code)
		b.WriteString(": ")
		b.WriteString(diag.Message)
		if strings.TrimSpace(diag.SuggestedFix) != "" {
			b.WriteString(" Fix: ")
			b.WriteString(strings.TrimSpace(diag.SuggestedFix))
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func dedupe(diags []Diagnostic) []Diagnostic {
	seen := map[string]bool{}
	out := make([]Diagnostic, 0, len(diags))
	for _, diag := range diags {
		key := strings.Join([]string{diag.Code, diag.Severity, diag.File, diag.Path, diag.Message, diag.SuggestedFix}, "|")
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, diag)
	}
	return out
}
