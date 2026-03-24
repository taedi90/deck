package validate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/xeipuuv/gojsonschema"
	"gopkg.in/yaml.v3"

	"github.com/Airgap-Castaways/deck/internal/config"
	"github.com/Airgap-Castaways/deck/internal/fsutil"
	"github.com/Airgap-Castaways/deck/internal/workflowcontract"
	"github.com/Airgap-Castaways/deck/internal/workflowexec"
	"github.com/Airgap-Castaways/deck/internal/workflowexpr"
	"github.com/Airgap-Castaways/deck/internal/workspacepaths"
	deckschemas "github.com/Airgap-Castaways/deck/schemas"
)

var (
	runtimeVarNamePattern      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	singleBraceTemplatePattern = regexp.MustCompile(`(^|[^\{])(\{\s*\.(vars|runtime)\.[^{}]+\})([^\}]|$)`)
	runtimeWhenRefPattern      = regexp.MustCompile(`\bruntime\.([A-Za-z_][A-Za-z0-9_]*)`)
	runtimeTemplateRefPattern  = regexp.MustCompile(`\.runtime\.([A-Za-z_][A-Za-z0-9_]*)`)
)

type documentKind string

const (
	documentKindWorkflow          documentKind = "workflow"
	documentKindComponentFragment documentKind = "component-fragment"
)

type componentFragment struct {
	Steps []config.Step `yaml:"steps"`
}

// File validates workflow structure and semantic rules.
// It is a convenience wrapper for CLI-style callers that do not own a request context.
func File(path string) error {
	return FileWithContext(context.Background(), path)
}

func FileWithContext(ctx context.Context, path string) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	if path == "" {
		return fmt.Errorf("file path is empty")
	}
	content, err := fsutil.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read workflow file: %w", err)
	}
	return withWorkflowName(path, Bytes(path, content))
}

// Workspace validates every scenario entrypoint under a workflow root.
// It is a convenience wrapper for callers that do not own a request context.
func Workspace(root string) ([]string, error) {
	return WorkspaceWithContext(context.Background(), root)
}

func WorkspaceWithContext(ctx context.Context, root string) ([]string, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resolvedRoot := strings.TrimSpace(root)
	if resolvedRoot == "" {
		resolvedRoot = "."
	}
	workflowRoot := workspacepaths.WorkflowRootPath(resolvedRoot)
	info, err := os.Stat(workflowRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("workflow directory not found: %s", workflowRoot)
		}
		return nil, fmt.Errorf("stat workflow directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workflow directory is not a directory: %s", workflowRoot)
	}

	required := []string{filepath.Join(workflowRoot, "prepare.yaml")}
	for _, path := range required {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return nil, fmt.Errorf("required workflow file not found: %s", path)
		}
	}

	scenarioRoot := filepath.Join(workflowRoot, workspacepaths.WorkflowScenariosDir)
	if info, err := os.Stat(scenarioRoot); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("workflow scenarios directory not found: %s", scenarioRoot)
	}

	files := make([]string, 0)
	err = filepath.WalkDir(scenarioRoot, func(path string, d os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		name := strings.ToLower(d.Name())
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read workflow directory: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no scenario workflow files found under: %s", scenarioRoot)
	}

	validated := make([]string, 0)
	visited := map[string]bool{}
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		validatedFiles, err := lintLocalEntrypoint(ctx, path, nil, workflowSupportedVersion, visited)
		if err != nil {
			return nil, err
		}
		validated = append(validated, validatedFiles...)
	}
	return dedupeAndSort(validated), nil
}

// Entrypoint validates a scenario entrypoint and its imported components.
// It is a convenience wrapper for callers that do not own a request context.
func Entrypoint(path string) ([]string, error) {
	return EntrypointWithContext(context.Background(), path)
}

func EntrypointWithContext(ctx context.Context, path string) ([]string, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	absPath, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return nil, fmt.Errorf("resolve workflow path: %w", err)
	}
	return lintLocalEntrypoint(ctx, absPath, nil, workflowSupportedVersion, map[string]bool{})
}

func Workflow(name string, wf *config.Workflow) error {
	if strings.TrimSpace(name) == "" {
		name = "<workflow>"
	}
	if err := validateLoadedWorkflow(name, wf); err != nil {
		return withWorkflowName(name, err)
	}
	return nil
}

func Bytes(name string, content []byte) error {
	if strings.TrimSpace(name) == "" {
		name = "<workflow>"
	}

	if err := validateSingleBraceTemplates(name, content); err != nil {
		return err
	}

	kind := detectDocumentKind(name)
	if err := validateSchema(name, content, kind); err != nil {
		return err
	}
	if kind == documentKindComponentFragment {
		fragment, err := parseComponentFragment(content)
		if err != nil {
			return err
		}
		return validateComponentFragment(name, fragment)
	}

	wf, err := parseWorkflow(content)
	if err != nil {
		return err
	}
	if err := validateLoadedWorkflow(name, wf); err != nil {
		return err
	}

	return nil
}

func withWorkflowName(name string, err error) error {
	if err == nil {
		return nil
	}
	prefix := strings.TrimSpace(name) + ": "
	if strings.HasPrefix(err.Error(), prefix) {
		return err
	}
	return fmt.Errorf("%s%w", prefix, err)
}

func lintLocalEntrypoint(ctx context.Context, path string, inheritedVars map[string]any, workflowVersion string, visiting map[string]bool) ([]string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve workflow path: %w", err)
	}
	if visiting[absPath] {
		return nil, nil
	}
	visiting[absPath] = true

	content, err := fsutil.ReadFile(absPath)
	if err != nil {
		return nil, withWorkflowName(absPath, fmt.Errorf("read workflow file: %w", err))
	}
	if err := validateSingleBraceTemplates(absPath, content); err != nil {
		return nil, withWorkflowName(absPath, err)
	}
	kind := detectDocumentKind(absPath)
	if err := validateSchema(absPath, content, kind); err != nil {
		return nil, withWorkflowName(absPath, err)
	}
	if kind == documentKindComponentFragment {
		fragment, err := parseComponentFragment(content)
		if err != nil {
			return nil, withWorkflowName(absPath, err)
		}
		if err := validateComponentFragmentWithWorkflowVersion(absPath, fragment, workflowVersion, inheritedVars); err != nil {
			return nil, withWorkflowName(absPath, err)
		}
		delete(visiting, absPath)
		return []string{absPath}, nil
	}

	raw, err := parseWorkflow(content)
	if err != nil {
		return nil, withWorkflowName(absPath, fmt.Errorf("parse yaml: %w", err))
	}

	loadOpts := config.LoadOptions{VarOverrides: cloneAnyMap(inheritedVars)}
	wf, err := config.LoadWithOptions(ctx, absPath, loadOpts)
	if err != nil {
		return nil, withWorkflowName(absPath, err)
	}

	validated := []string{absPath}
	for _, phase := range raw.Phases {
		for _, phaseImport := range phase.Imports {
			child, err := resolveLocalWorkflowImport(absPath, phaseImport.Path)
			if err != nil {
				return nil, withWorkflowName(absPath, err)
			}
			files, err := lintLocalEntrypoint(ctx, child, wf.Vars, wf.Version, visiting)
			if err != nil {
				return nil, err
			}
			validated = append(validated, files...)
		}
	}
	if err := Workflow(absPath, wf); err != nil {
		return nil, err
	}
	return dedupeAndSort(validated), nil
}

func parseWorkflow(content []byte) (*config.Workflow, error) {
	var wf config.Workflow
	if err := unmarshalYAMLStrict(content, &wf); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}
	return &wf, nil
}

func parseComponentFragment(content []byte) (*componentFragment, error) {
	var fragment componentFragment
	if err := unmarshalYAMLStrict(content, &fragment); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}
	return &fragment, nil
}

func validateLoadedWorkflow(name string, wf *config.Workflow) error {
	if wf == nil {
		return fmt.Errorf("workflow is nil")
	}
	if len(wf.Phases) > 0 && len(wf.Steps) > 0 {
		return fmt.Errorf("workflow cannot set both phases and steps")
	}
	if wf.Version == "" {
		return fmt.Errorf("version is required")
	}
	if _, ok := workflowcontract.DefaultStepAPIVersionForWorkflowVersion(wf.Version); !ok {
		return fmt.Errorf("unsupported version: %s (supported: %s)", wf.Version, workflowSupportedVersion)
	}
	if err := validateWorkflowMode(wf); err != nil {
		return err
	}
	if err := validateToolSchemas(wf); err != nil {
		return err
	}
	if err := validateSemantics(name, wf); err != nil {
		return err
	}
	_ = name
	return nil
}

func validateComponentFragment(name string, fragment *componentFragment) error {
	return validateComponentFragmentWithWorkflowVersion(name, fragment, workflowSupportedVersion, nil)
}

func validateComponentFragmentWithWorkflowVersion(name string, fragment *componentFragment, workflowVersion string, vars map[string]any) error {
	if fragment == nil {
		return fmt.Errorf("component fragment is nil")
	}
	if len(fragment.Steps) == 0 {
		return fmt.Errorf("steps is required")
	}
	wf := &config.Workflow{Version: workflowVersion, Steps: fragment.Steps, Vars: cloneAnyMap(vars)}
	if err := validateToolSchemas(wf); err != nil {
		return err
	}
	if err := validateSemantics(name, wf); err != nil {
		return err
	}
	_ = name
	return nil
}

func detectDocumentKind(name string) documentKind {
	trimmed := filepath.ToSlash(strings.TrimSpace(name))
	if workspacepaths.IsComponentWorkflowPath(trimmed) {
		return documentKindComponentFragment
	}
	return documentKindWorkflow
}

func unmarshalYAMLStrict(content []byte, target any) error {
	dec := yaml.NewDecoder(bytes.NewReader(content))
	dec.KnownFields(true)
	return dec.Decode(target)
}

func resolveLocalWorkflowImport(originPath string, importRef string) (string, error) {
	ref := strings.TrimSpace(importRef)
	if ref == "" {
		return "", fmt.Errorf("workflow import path is empty")
	}
	if strings.Contains(ref, "://") {
		return "", fmt.Errorf("remote imports are not supported during workspace lint: %s", ref)
	}
	cleaned := filepath.ToSlash(strings.TrimSpace(ref))
	cleaned = strings.TrimPrefix(cleaned, "../components/")
	cleaned = strings.TrimPrefix(cleaned, "components/")
	cleaned = strings.TrimPrefix(cleaned, "./")
	if strings.HasPrefix(cleaned, "../") || cleaned == ".." || strings.HasPrefix(cleaned, "/") || cleaned == "" {
		return "", fmt.Errorf("workflow import path must stay under components root: %s", ref)
	}
	workflowRoot, err := config.WorkflowRootForPath(originPath)
	if err != nil {
		return "", err
	}
	return filepath.Join(workflowRoot, workspacepaths.WorkflowComponentsDir, filepath.FromSlash(filepath.Clean(cleaned))), nil
}

func cloneAnyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for k, v := range input {
		out[k] = v
	}
	return out
}

func dedupeAndSort(values []string) []string {
	if len(values) == 0 {
		return values
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func validateWorkflowMode(wf *config.Workflow) error {
	if wf == nil {
		return nil
	}
	hasPhases := len(wf.Phases) > 0
	hasSteps := len(wf.Steps) > 0
	modeCount := 0
	if hasPhases {
		modeCount++
	}
	if hasSteps {
		modeCount++
	}
	if modeCount > 1 {
		return fmt.Errorf("workflow cannot set multiple execution modes")
	}
	if !hasPhases && !hasSteps {
		return fmt.Errorf("workflow must define phases or steps")
	}
	return nil
}

func validateToolSchemas(wf *config.Workflow) error {
	for _, step := range workflowSteps(wf) {
		key, err := effectiveStepTypeKey(wf.Version, step)
		if err != nil {
			return err
		}
		schemaFile, ok := workflowexec.StepSchemaFileForKey(key)
		if !ok {
			continue
		}
		toolSchemaRaw, err := deckschemas.ToolSchema(schemaFile)
		if err != nil {
			return fmt.Errorf("E_SCHEMA_INVALID: tool schema not found for kind %s", step.Kind)
		}

		stepForSchema := step
		stepForSchema.APIVersion = key.APIVersion
		stepForSchema.Spec = materializeVarTemplateValues(stepForSchema.Spec, wf.Vars)

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

func materializeVarTemplateValues(input map[string]any, vars map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	ctx := map[string]any{"vars": vars}
	for key, value := range input {
		out[key] = materializeVarTemplateValue(value, ctx)
	}
	return out
}

func materializeVarTemplateValue(value any, ctx map[string]any) any {
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if !strings.Contains(trimmed, "{{") || !strings.Contains(trimmed, ".vars.") {
			return typed
		}
		resolved, ok, err := workflowexec.ResolveWholeValueTemplate(typed, ctx)
		if err != nil || !ok {
			return typed
		}
		if s, ok := resolved.(string); ok {
			if strings.TrimSpace(s) == "" {
				return typed
			}
			return s
		}
		return resolved
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = materializeVarTemplateValue(item, ctx)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, materializeVarTemplateValue(item, ctx))
		}
		return out
	default:
		return value
	}
}

func validateSchema(name string, content []byte, kind documentKind) error {
	var (
		schemaRaw []byte
		err       error
	)
	if kind == documentKindComponentFragment {
		schemaRaw, err = deckschemas.ComponentFragmentSchema()
		if err != nil {
			return fmt.Errorf("component fragment schema not found: schemas/deck-component-fragment.schema.json")
		}
	} else {
		schemaRaw, err = deckschemas.WorkflowSchema()
		if err != nil {
			return fmt.Errorf("workflow schema not found: schemas/deck-workflow.schema.json")
		}
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

func validateSemantics(name string, wf *config.Workflow) error {
	if err := validateRoleKinds(name, wf); err != nil {
		return err
	}
	if err := validatePhaseSemantics(wf, inferWorkflowMode(name, wf)); err != nil {
		return err
	}

	seenStepID := map[string]bool{}
	assignedRuntime := map[string]string{}

	for _, step := range workflowSteps(wf) {
		if _, hasLegacyAction := step.Spec["action"]; hasLegacyAction {
			return fmt.Errorf("E_SCHEMA_INVALID: step %s (%s): spec.action is no longer supported; move the operation into kind (for example `DownloadFile`)", step.ID, step.Kind)
		}
		if step.Kind == "ConfigureRepository" {
			if _, hasRefreshCache := step.Spec["refreshCache"]; hasRefreshCache {
				return fmt.Errorf("E_SCHEMA_INVALID: step %s (%s): spec.refreshCache is no longer supported; use a separate `RefreshRepository` step", step.ID, step.Kind)
			}
		}
		if output := literalPrepareOutputRoot(step); output != "" {
			if err := validatePrepareOutputRoot(step, output); err != nil {
				return err
			}
		}

		if strings.TrimSpace(step.When) != "" {
			if _, err := workflowexpr.CompileWhen(step.When); err != nil {
				return fmt.Errorf("E_SCHEMA_INVALID: step %s (%s): invalid when expression: %v", step.ID, step.Kind, err)
			}
		}
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
			if !isValidOutputKey(wf.Version, step, outputKey) {
				return fmt.Errorf("E_REGISTER_OUTPUT_NOT_FOUND: step %s (%s) has no output key %s", step.ID, step.Kind, outputKey)
			}
			if previous, exists := assignedRuntime[runtimeVar]; exists {
				return fmt.Errorf("E_RUNTIME_VAR_REDEFINED: %s (previous step: %s)", runtimeVar, previous)
			}
			assignedRuntime[runtimeVar] = step.ID
		}

		if step.Kind == "WaitForMissingFile" {
			nonEmpty, _ := step.Spec["nonEmpty"].(bool)
			if nonEmpty {
				return fmt.Errorf("E_SCHEMA_INVALID: step %s (wait.file-absent): nonEmpty is only valid for wait.file-exists", step.ID)
			}
		}
		if step.Kind == "CreateSymlink" {
			requireTarget, _ := step.Spec["requireTarget"].(bool)
			ignoreMissingTarget, _ := step.Spec["ignoreMissingTarget"].(bool)
			if requireTarget && ignoreMissingTarget {
				return fmt.Errorf("E_SCHEMA_INVALID: step %s (%s): requireTarget and ignoreMissingTarget cannot both be true", step.ID, step.Kind)
			}
		}
	}

	return nil
}

func validatePhaseSemantics(wf *config.Workflow, role string) error {
	phases := config.NormalizedPhases(wf)
	seenPhase := map[string]bool{}
	for _, phase := range phases {
		phaseName := strings.TrimSpace(phase.Name)
		if phaseName == "" {
			return fmt.Errorf("E_DUPLICATE_PHASE_NAME: empty phase name")
		}
		if seenPhase[phaseName] {
			return fmt.Errorf("E_DUPLICATE_PHASE_NAME: %s", phaseName)
		}
		seenPhase[phaseName] = true
		if err := validatePhaseParallelSemantics(phase, role); err != nil {
			return err
		}
	}
	return nil
}

func validatePhaseParallelSemantics(phase config.Phase, role string) error {
	if phase.MaxParallelism < 0 {
		return fmt.Errorf("E_SCHEMA_INVALID: phase %s: maxParallelism must be >= 0", phase.Name)
	}
	closedGroups := map[string]bool{}
	currentGroup := ""
	for _, step := range phase.Steps {
		group := strings.TrimSpace(step.ParallelGroup)
		if group == "" {
			if currentGroup != "" {
				closedGroups[currentGroup] = true
				currentGroup = ""
			}
			continue
		}
		if currentGroup != "" && currentGroup != group {
			closedGroups[currentGroup] = true
		}
		if closedGroups[group] && currentGroup != group {
			return fmt.Errorf("E_PARALLEL_GROUP_DISCONTIGUOUS: phase %s group %s must be contiguous", phase.Name, group)
		}
		currentGroup = group
	}
	batches := workflowexec.BuildPhaseBatches(phase)
	for _, batch := range batches {
		if !batch.Parallel() {
			continue
		}
		if err := validateParallelBatch(batch, role); err != nil {
			return err
		}
	}
	return nil
}

func validateParallelBatch(batch workflowexec.StepBatch, role string) error {
	registered := map[string]string{}
	paths := map[string]string{}
	prepareOutputs := map[string]string{}
	for _, step := range batch.Steps {
		if role == "apply" && !parallelApplyKindAllowed(step.Kind) {
			return fmt.Errorf("E_PARALLEL_KIND_UNSAFE: phase %s step %s (%s) is not allowed in parallelGroup", batch.PhaseName, step.ID, step.Kind)
		}
		for runtimeVar := range step.Register {
			registered[runtimeVar] = step.ID
		}
		if path := literalApplyTargetPath(step); path != "" {
			if prev, exists := paths[path]; exists {
				return fmt.Errorf("E_PARALLEL_PATH_CONFLICT: phase %s steps %s and %s both target %s in the same parallelGroup", batch.PhaseName, prev, step.ID, path)
			}
			paths[path] = step.ID
		}
		if output := literalPrepareOutputRoot(step); output != "" {
			if prev, exists := prepareOutputs[output]; exists {
				return fmt.Errorf("E_PARALLEL_OUTPUT_CONFLICT: phase %s steps %s and %s both write %s in the same parallelGroup", batch.PhaseName, prev, step.ID, output)
			}
			prepareOutputs[output] = step.ID
		}
	}
	for _, step := range batch.Steps {
		for _, runtimeVar := range referencedRuntimeVars(step) {
			if producer, exists := registered[runtimeVar]; exists {
				return fmt.Errorf("E_PARALLEL_RUNTIME_DEPENDENCY: phase %s step %s references runtime.%s from same parallelGroup producer %s", batch.PhaseName, step.ID, runtimeVar, producer)
			}
		}
	}
	return nil
}

func parallelApplyKindAllowed(kind string) bool {
	switch kind {
	case "Command", "CopyFile", "EnsureDirectory", "ExtractArchive", "WaitForCommand", "WaitForFile", "WaitForMissingFile", "WaitForService", "WaitForTCPPort", "WaitForMissingTCPPort", "WriteFile":
		return true
	default:
		return false
	}
}

func referencedRuntimeVars(step config.Step) []string {
	seen := map[string]bool{}
	for _, match := range runtimeWhenRefPattern.FindAllStringSubmatch(strings.TrimSpace(step.When), -1) {
		if len(match) == 2 {
			seen[match[1]] = true
		}
	}
	collectRuntimeTemplateRefs(step.Spec, seen)
	vars := make([]string, 0, len(seen))
	for key := range seen {
		vars = append(vars, key)
	}
	sort.Strings(vars)
	return vars
}

func collectRuntimeTemplateRefs(value any, seen map[string]bool) {
	switch typed := value.(type) {
	case string:
		for _, match := range runtimeTemplateRefPattern.FindAllStringSubmatch(typed, -1) {
			if len(match) == 2 {
				seen[match[1]] = true
			}
		}
	case map[string]any:
		for _, item := range typed {
			collectRuntimeTemplateRefs(item, seen)
		}
	case []any:
		for _, item := range typed {
			collectRuntimeTemplateRefs(item, seen)
		}
	}
}

func literalApplyTargetPath(step config.Step) string {
	if step.Kind == "WriteFile" || step.Kind == "CopyFile" || step.Kind == "EnsureDirectory" || step.Kind == "CreateSymlink" || step.Kind == "WriteContainerdConfig" || step.Kind == "WriteContainerdRegistryHosts" || step.Kind == "ConfigureRepository" || step.Kind == "EditTOML" || step.Kind == "EditYAML" || step.Kind == "EditJSON" {
		return stableLiteralPath(stringValue(step.Spec, "path"))
	}
	if step.Kind == "ExtractArchive" || step.Kind == "EditFile" || step.Kind == "WriteSystemdUnit" {
		if nested := mapValue(step.Spec, "output"); len(nested) > 0 {
			if path := stableLiteralPath(stringValue(nested, "path")); path != "" {
				return path
			}
		}
		return stableLiteralPath(stringValue(step.Spec, "path"))
	}
	return ""
}

func literalPrepareOutputRoot(step config.Step) string {
	switch step.Kind {
	case "DownloadPackage", "DownloadImage":
		return stableLiteralPath(stringValue(step.Spec, "outputDir"))
	case "DownloadFile":
		return stableLiteralPath(stringValue(step.Spec, "outputPath"))
	default:
		return ""
	}
}

func validatePrepareOutputRoot(step config.Step, output string) error {
	trimmed := strings.TrimSpace(output)
	switch step.Kind {
	case "DownloadFile":
		if workspacepaths.IsPreparedPathUnderRoot(trimmed, workspacepaths.PreparedFilesRoot) {
			return nil
		}
		return fmt.Errorf("E_PREPARE_OUTPUT_ROOT_INVALID: step %s (%s) outputPath must stay under %s/", step.ID, step.Kind, workspacepaths.PreparedFilesRoot)
	case "DownloadImage":
		if workspacepaths.IsPreparedPathUnderRoot(trimmed, workspacepaths.PreparedImagesRoot) {
			return nil
		}
		return fmt.Errorf("E_PREPARE_OUTPUT_ROOT_INVALID: step %s (%s) outputDir must stay under %s/", step.ID, step.Kind, workspacepaths.PreparedImagesRoot)
	case "DownloadPackage":
		if workspacepaths.IsPreparedPathUnderRoot(trimmed, workspacepaths.PreparedPackagesRoot) {
			return nil
		}
		return fmt.Errorf("E_PREPARE_OUTPUT_ROOT_INVALID: step %s (%s) outputDir must stay under %s/", step.ID, step.Kind, workspacepaths.PreparedPackagesRoot)
	default:
		return nil
	}
}

func stableLiteralPath(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.Contains(trimmed, "{{") {
		return ""
	}
	return filepath.ToSlash(trimmed)
}

func validateRoleKinds(name string, wf *config.Workflow) error {
	role := inferWorkflowMode(name, wf)
	if role == "" {
		return nil
	}
	for _, step := range workflowSteps(wf) {
		key, err := effectiveStepTypeKey(wf.Version, step)
		if err != nil {
			return err
		}
		if workflowexec.StepAllowedForRoleForKey(role, key) {
			continue
		}
		return fmt.Errorf("E_KIND_ROLE_MISMATCH: step %s (%s) is not supported for role %s", step.ID, step.Kind, role)
	}
	return nil
}

func inferWorkflowMode(name string, wf *config.Workflow) string {
	trimmed := filepath.ToSlash(strings.TrimSpace(name))
	if filepath.Base(trimmed) == "prepare.yaml" {
		return "prepare"
	}
	if strings.Contains(trimmed, "/workflows/scenarios/") || strings.HasPrefix(trimmed, "workflows/scenarios/") {
		return "apply"
	}
	seenPrepare := false
	seenApply := false
	for _, step := range workflowSteps(wf) {
		key, err := effectiveStepTypeKey(wf.Version, step)
		if err != nil {
			continue
		}
		if workflowexec.StepAllowedForRoleForKey("prepare", key) {
			seenPrepare = true
		}
		if workflowexec.StepAllowedForRoleForKey("apply", key) {
			seenApply = true
		}
	}
	if seenPrepare && !seenApply {
		return "prepare"
	}
	if seenApply && !seenPrepare {
		return "apply"
	}
	return ""
}

func isReservedRuntimeVar(runtimeVar string) bool {
	trimmed := strings.TrimSpace(runtimeVar)
	return trimmed == "host" || strings.HasPrefix(trimmed, "host.")
}

func isValidOutputKey(workflowVersion string, step config.Step, outputKey string) bool {
	key, err := effectiveStepTypeKey(workflowVersion, step)
	if err != nil {
		return false
	}
	return workflowexec.StepHasOutputForKey(key, outputKey)
}

func effectiveStepTypeKey(workflowVersion string, step config.Step) (workflowexec.StepTypeKey, error) {
	apiVersion, err := workflowcontract.ResolveStepAPIVersion(workflowVersion, step.APIVersion)
	if err != nil {
		return workflowexec.StepTypeKey{}, fmt.Errorf("E_SCHEMA_INVALID: step %s (%s): %w", step.ID, step.Kind, err)
	}
	return workflowexec.StepTypeKey{APIVersion: strings.TrimSpace(apiVersion), Kind: strings.TrimSpace(step.Kind)}, nil
}

func workflowSteps(wf *config.Workflow) []config.Step {
	steps := make([]config.Step, 0)
	for _, phase := range config.NormalizedPhases(wf) {
		steps = append(steps, phase.Steps...)
	}
	return steps
}

func stringValue(v map[string]any, key string) string {
	if v == nil {
		return ""
	}
	raw, ok := v[key]
	if !ok {
		return ""
	}
	s, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func mapValue(v map[string]any, key string) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	raw, ok := v[key]
	if !ok {
		return map[string]any{}
	}
	m, ok := raw.(map[string]any)
	if !ok || m == nil {
		return map[string]any{}
	}
	return m
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
