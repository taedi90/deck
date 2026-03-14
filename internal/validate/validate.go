package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/xeipuuv/gojsonschema"
	"gopkg.in/yaml.v3"

	"github.com/taedi90/deck/internal/config"
	"github.com/taedi90/deck/internal/workflowexec"
	deckschemas "github.com/taedi90/deck/schemas"
)

var (
	runtimeVarNamePattern      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	singleBraceTemplatePattern = regexp.MustCompile(`(^|[^\{])(\{\s*\.(vars|runtime)\.[^{}]+\})([^\}]|$)`)
)

// File validates workflow structure and semantic rules.
func File(path string) error {
	if path == "" {
		return fmt.Errorf("file path is empty")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read workflow file: %w", err)
	}
	return Bytes(path, content)
}

func Bytes(name string, content []byte) error {
	if strings.TrimSpace(name) == "" {
		name = "<workflow>"
	}

	if err := validateSingleBraceTemplates(name, content); err != nil {
		return err
	}

	var wf config.Workflow
	if err := yaml.Unmarshal(content, &wf); err != nil {
		return fmt.Errorf("parse yaml: %w", err)
	}
	if len(wf.Phases) > 0 && len(wf.Steps) > 0 {
		return fmt.Errorf("workflow cannot set both phases and steps")
	}

	if wf.Role == "" {
		return fmt.Errorf("role is required")
	}
	if strings.TrimSpace(wf.Role) != "prepare" && strings.TrimSpace(wf.Role) != "apply" {
		return fmt.Errorf("unsupported role: %s (supported: prepare, apply)", wf.Role)
	}

	if wf.Version == "" {
		return fmt.Errorf("version is required")
	}
	if strings.TrimSpace(wf.Version) != "v1alpha1" {
		return fmt.Errorf("unsupported version: %s (supported: v1alpha1)", wf.Version)
	}

	if err := validateSchema(name, content); err != nil {
		return err
	}
	if err := validateWorkflowMode(&wf); err != nil {
		return err
	}
	if err := validateToolSchemas(&wf); err != nil {
		return err
	}
	if err := validateSemantics(&wf); err != nil {
		return err
	}

	return nil
}

func validateWorkflowMode(wf *config.Workflow) error {
	if wf == nil {
		return nil
	}
	hasArtifacts := wf.Artifacts != nil && (len(wf.Artifacts.Files) > 0 || len(wf.Artifacts.Images) > 0 || len(wf.Artifacts.Packages) > 0)
	hasPhases := len(wf.Phases) > 0
	hasSteps := len(wf.Steps) > 0
	modeCount := 0
	if hasArtifacts {
		modeCount++
	}
	if hasPhases {
		modeCount++
	}
	if hasSteps {
		modeCount++
	}
	if modeCount > 1 {
		return fmt.Errorf("workflow cannot set multiple execution modes")
	}
	if hasArtifacts && strings.TrimSpace(wf.Role) != "prepare" {
		return fmt.Errorf("E_SCHEMA_INVALID: artifacts is only supported for role prepare")
	}
	return nil
}

func validateToolSchemas(wf *config.Workflow) error {
	for _, step := range workflowSteps(wf) {
		schemaFile, ok := workflowexec.StepSchemaFile(step.Kind)
		if !ok {
			continue
		}
		toolSchemaRaw, err := deckschemas.ToolSchema(schemaFile)
		if err != nil {
			return fmt.Errorf("E_SCHEMA_INVALID: tool schema not found for kind %s", step.Kind)
		}

		stepForSchema := step
		if strings.TrimSpace(stepForSchema.APIVersion) == "" {
			stepForSchema.APIVersion = "deck/v1alpha1"
		}

		raw, err := json.Marshal(stepForSchema)
		if err != nil {
			return fmt.Errorf("marshal step for schema validation: %w", err)
		}

		result, err := gojsonschema.Validate(
			gojsonschema.NewBytesLoader(toolSchemaRaw),
			gojsonschema.NewBytesLoader(raw),
		)
		if err != nil {
			return fmt.Errorf("run tool schema validation: %w", err)
		}
		if result.Valid() {
			continue
		}

		msgs := make([]string, 0, len(result.Errors()))
		for _, e := range result.Errors() {
			msgs = append(msgs, e.String())
		}
		return fmt.Errorf("E_SCHEMA_INVALID: step %s (%s): %s", step.ID, step.Kind, strings.Join(msgs, "; "))
	}

	return nil
}

func validateSchema(name string, content []byte) error {
	schemaRaw, err := deckschemas.WorkflowSchema()
	if err != nil {
		return fmt.Errorf("workflow schema not found: schemas/deck-workflow.schema.json")
	}

	var doc any
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return fmt.Errorf("parse yaml: %w", err)
	}

	normalized, err := normalizeYAMLForJSON(doc)
	if err != nil {
		return fmt.Errorf("normalize workflow for schema validation: %w", err)
	}

	raw, err := json.Marshal(normalized)
	if err != nil {
		return fmt.Errorf("marshal workflow for schema validation: %w", err)
	}

	result, err := gojsonschema.Validate(
		gojsonschema.NewBytesLoader(schemaRaw),
		gojsonschema.NewBytesLoader(raw),
	)
	if err != nil {
		return fmt.Errorf("run schema validation: %w", err)
	}

	if result.Valid() {
		return nil
	}

	msgs := make([]string, 0, len(result.Errors()))
	for _, e := range result.Errors() {
		msgs = append(msgs, e.String())
	}
	return fmt.Errorf("E_SCHEMA_INVALID: %s", strings.Join(msgs, "; "))
}

func validateSemantics(wf *config.Workflow) error {
	if err := validatePrepareSemantics(wf); err != nil {
		return err
	}
	if err := validateRoleKinds(wf); err != nil {
		return err
	}

	seenStepID := map[string]bool{}
	assignedRuntime := map[string]string{}

	for _, step := range workflowSteps(wf) {
		if step.ID == "" {
			continue
		}
		if seenStepID[step.ID] {
			return fmt.Errorf("E_DUPLICATE_STEP_ID: %s", step.ID)
		}
		seenStepID[step.ID] = true

		for runtimeVar, outputKey := range step.Register {
			if !runtimeVarNamePattern.MatchString(runtimeVar) {
				return fmt.Errorf("E_REGISTER_VAR_INVALID: %s", runtimeVar)
			}
			if isReservedRuntimeVar(runtimeVar) {
				return fmt.Errorf("E_RUNTIME_VAR_RESERVED: %s", runtimeVar)
			}
			if strings.TrimSpace(outputKey) == "" {
				return fmt.Errorf("E_REGISTER_OUTPUT_NOT_FOUND: empty output key in step %s", step.ID)
			}
			if !isValidOutputKey(step.Kind, step.Spec, outputKey) {
				return fmt.Errorf("E_REGISTER_OUTPUT_NOT_FOUND: step %s (%s) has no output key %s", step.ID, step.Kind, outputKey)
			}
			if previous, exists := assignedRuntime[runtimeVar]; exists {
				return fmt.Errorf("E_RUNTIME_VAR_REDEFINED: %s (previous step: %s)", runtimeVar, previous)
			}
			assignedRuntime[runtimeVar] = step.ID
		}

		if step.Kind == "Wait" {
			action, _ := step.Spec["action"].(string)
			if action == "" {
				if state, _ := step.Spec["state"].(string); state == "absent" {
					action = "fileAbsent"
				} else {
					action = "fileExists"
				}
			}
			nonEmpty, _ := step.Spec["nonEmpty"].(bool)
			if action == "fileAbsent" && nonEmpty {
				return fmt.Errorf("E_SCHEMA_INVALID: step %s (Wait): nonEmpty is only valid for fileExists", step.ID)
			}
		}
	}

	return nil
}

func validatePrepareSemantics(wf *config.Workflow) error {
	if wf == nil || wf.Artifacts == nil {
		return nil
	}
	for _, group := range wf.Artifacts.Files {
		for _, item := range group.Items {
			path := strings.TrimSpace(item.Output.Path)
			if path == "" {
				return fmt.Errorf("E_SCHEMA_INVALID: artifacts.files group %s item %s requires output.path", group.Group, item.ID)
			}
			normalized := filepath.ToSlash(strings.TrimSpace(path))
			if strings.HasPrefix(normalized, "/") {
				return fmt.Errorf("E_SCHEMA_INVALID: artifacts.files group %s item %s output.path must be relative", group.Group, item.ID)
			}
			if strings.HasPrefix(normalized, "files/") {
				return fmt.Errorf("E_SCHEMA_INVALID: artifacts.files group %s item %s output.path must be relative to files root", group.Group, item.ID)
			}
		}
	}
	for _, group := range wf.Artifacts.Packages {
		for _, target := range group.Targets {
			if strings.TrimSpace(target.OSFamily) == "" || strings.TrimSpace(target.Release) == "" {
				return fmt.Errorf("E_SCHEMA_INVALID: artifacts.packages group %s targets require osFamily and release", group.Group)
			}
		}
	}
	return nil
}

func validateRoleKinds(wf *config.Workflow) error {
	role := strings.TrimSpace(wf.Role)
	for _, step := range workflowSteps(wf) {
		if workflowexec.StepAllowedForRole(role, step.Kind, step.Spec) {
			continue
		}
		return fmt.Errorf("E_KIND_ROLE_MISMATCH: step %s (%s) is not supported for role %s", step.ID, step.Kind, role)
	}
	return nil
}

func isReservedRuntimeVar(runtimeVar string) bool {
	trimmed := strings.TrimSpace(runtimeVar)
	return trimmed == "host" || strings.HasPrefix(trimmed, "host.")
}

func isValidOutputKey(kind string, spec map[string]any, outputKey string) bool {
	return workflowexec.StepHasOutput(kind, spec, outputKey)
}

func workflowSteps(wf *config.Workflow) []config.Step {
	if len(wf.Phases) > 0 {
		steps := make([]config.Step, 0)
		for _, phase := range wf.Phases {
			steps = append(steps, phase.Steps...)
		}
		return steps
	}

	return wf.Steps
}

func validateSingleBraceTemplates(name string, content []byte) error {
	lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	for idx, line := range lines {
		matches := singleBraceTemplatePattern.FindStringSubmatch(line)
		if len(matches) < 3 {
			continue
		}
		return fmt.Errorf("E_TEMPLATE_SINGLE_BRACE: %s:%d: unsupported single-brace template %s", name, idx+1, strings.TrimSpace(matches[2]))
	}

	return nil
}

func normalizeYAMLForJSON(value any) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for k, v := range typed {
			normalized, err := normalizeYAMLForJSON(v)
			if err != nil {
				return nil, err
			}
			out[k] = normalized
		}
		return out, nil
	case map[any]any:
		out := make(map[string]any, len(typed))
		for k, v := range typed {
			key, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("non-string map key type %T", k)
			}
			normalized, err := normalizeYAMLForJSON(v)
			if err != nil {
				return nil, err
			}
			out[key] = normalized
		}
		return out, nil
	case []any:
		out := make([]any, len(typed))
		for i := range typed {
			normalized, err := normalizeYAMLForJSON(typed[i])
			if err != nil {
				return nil, err
			}
			out[i] = normalized
		}
		return out, nil
	default:
		return typed, nil
	}
}
