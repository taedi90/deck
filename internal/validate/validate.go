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

	"github.com/taedi90/deck/internal/config"
	"github.com/taedi90/deck/internal/fsutil"
	"github.com/taedi90/deck/internal/workflowexec"
	deckschemas "github.com/taedi90/deck/schemas"
)

var (
	runtimeVarNamePattern      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	singleBraceTemplatePattern = regexp.MustCompile(`(^|[^\{])(\{\s*\.(vars|runtime)\.[^{}]+\})([^\}]|$)`)
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
func File(path string) error {
	if path == "" {
		return fmt.Errorf("file path is empty")
	}
	content, err := fsutil.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read workflow file: %w", err)
	}
	return withWorkflowName(path, Bytes(path, content))
}

func Workspace(root string) ([]string, error) {
	resolvedRoot := strings.TrimSpace(root)
	if resolvedRoot == "" {
		resolvedRoot = "."
	}
	workflowRoot := filepath.Join(resolvedRoot, "workflows")
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

	required := []string{filepath.Join(workflowRoot, "scenarios", "prepare.yaml")}
	for _, path := range required {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return nil, fmt.Errorf("required workflow file not found: %s", path)
		}
	}

	scenarioRoot := filepath.Join(workflowRoot, "scenarios")
	if info, err := os.Stat(scenarioRoot); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("workflow scenarios directory not found: %s", scenarioRoot)
	}

	files := make([]string, 0)
	err = filepath.WalkDir(scenarioRoot, func(path string, d os.DirEntry, walkErr error) error {
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
		validatedFiles, err := lintLocalEntrypoint(path, nil, visited)
		if err != nil {
			return nil, err
		}
		validated = append(validated, validatedFiles...)
	}
	return dedupeAndSort(validated), nil
}

func Entrypoint(path string) ([]string, error) {
	absPath, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return nil, fmt.Errorf("resolve workflow path: %w", err)
	}
	return lintLocalEntrypoint(absPath, nil, map[string]bool{})
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

func lintLocalEntrypoint(path string, inheritedVars map[string]any, visiting map[string]bool) ([]string, error) {
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
		if err := validateComponentFragment(absPath, fragment); err != nil {
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
	wf, err := config.LoadWithOptions(context.Background(), absPath, loadOpts)
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
			files, err := lintLocalEntrypoint(child, wf.Vars, visiting)
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
	if wf.Role == "" {
		return fmt.Errorf("role is required")
	}
	if strings.TrimSpace(wf.Role) != "prepare" && strings.TrimSpace(wf.Role) != "apply" {
		return fmt.Errorf("unsupported role: %s (supported: prepare, apply)", wf.Role)
	}
	if wf.Version == "" {
		return fmt.Errorf("version is required")
	}
	if strings.TrimSpace(wf.Version) != workflowSupportedVersion {
		return fmt.Errorf("unsupported version: %s (supported: v1alpha1)", wf.Version)
	}
	if err := validateWorkflowMode(wf); err != nil {
		return err
	}
	if err := validateToolSchemas(wf); err != nil {
		return err
	}
	if err := validateSemantics(wf); err != nil {
		return err
	}
	_ = name
	return nil
}

func validateComponentFragment(name string, fragment *componentFragment) error {
	if fragment == nil {
		return fmt.Errorf("component fragment is nil")
	}
	if len(fragment.Steps) == 0 {
		return fmt.Errorf("steps is required")
	}
	wf := &config.Workflow{Role: "apply", Version: "v1alpha1", Steps: fragment.Steps}
	if err := validateToolSchemas(wf); err != nil {
		return err
	}
	if err := validateSemantics(wf); err != nil {
		return err
	}
	_ = name
	return nil
}

func detectDocumentKind(name string) documentKind {
	trimmed := filepath.ToSlash(strings.TrimSpace(name))
	if strings.Contains(trimmed, "/workflows/components/") || strings.HasPrefix(trimmed, "workflows/components/") {
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
	return filepath.Join(workflowRoot, "components", filepath.FromSlash(filepath.Clean(cleaned))), nil
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
		if contract, ok := workflowexec.StepContractForKind(step.Kind); ok && len(contract.Actions) > 0 {
			action, _ := step.Spec["action"].(string)
			if strings.TrimSpace(action) == "" {
				return fmt.Errorf("E_SCHEMA_INVALID: step %s (%s): spec.action is required", step.ID, step.Kind)
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
	fileOutputs := make(map[string]string)
	imageOutputs := make(map[string]string)
	packageRoots := make(map[string]string)
	for _, group := range wf.Artifacts.Files {
		if err := validateArtifactExecution(group.Execution, fmt.Sprintf("artifacts.files group %s", group.Group)); err != nil {
			return err
		}
		for _, target := range expandArtifactTargets(group.Targets) {
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
					return fmt.Errorf("E_SCHEMA_INVALID: artifacts.files group %s item %s output.path must not include the files/ prefix; it is added automatically", group.Group, item.ID)
				}
				rendered, err := workflowexec.RenderSpecWithExtra(map[string]any{"output": map[string]any{"path": filepath.ToSlash(filepath.Join("files", path))}}, wf, nil, map[string]any{"bundleRoot": "", "stateFile": ""}, map[string]any{"target": prepareTargetMap(target)})
				if err != nil {
					return fmt.Errorf("E_SCHEMA_INVALID: artifacts.files group %s item %s output render failed: %v", group.Group, item.ID, err)
				}
				finalPath := strings.TrimSpace(stringValue(mapValue(rendered, "output"), "path"))
				if prev, exists := fileOutputs[finalPath]; exists {
					return fmt.Errorf("E_SCHEMA_INVALID: duplicate file artifact output path %s (%s and %s)", finalPath, prev, item.ID)
				}
				fileOutputs[finalPath] = item.ID
			}
		}
	}
	for _, group := range wf.Artifacts.Packages {
		if err := validateArtifactExecution(group.Execution, fmt.Sprintf("artifacts.packages group %s", group.Group)); err != nil {
			return err
		}
		for _, target := range group.Targets {
			if strings.TrimSpace(target.OSFamily) == "" || strings.TrimSpace(target.Release) == "" {
				return fmt.Errorf("E_SCHEMA_INVALID: artifacts.packages group %s targets require osFamily and release", group.Group)
			}
			renderedRepo := map[string]any{}
			if len(group.Repo) > 0 {
				var err error
				renderedRepo, err = workflowexec.RenderSpecWithExtra(group.Repo, wf, nil, map[string]any{"bundleRoot": "", "stateFile": ""}, map[string]any{"target": prepareTargetMap(target)})
				if err != nil {
					return fmt.Errorf("E_SCHEMA_INVALID: artifacts.packages group %s repo render failed: %v", group.Group, err)
				}
			}
			renderedBackend := map[string]any{}
			if len(group.Backend) > 0 {
				var err error
				renderedBackend, err = workflowexec.RenderSpecWithExtra(group.Backend, wf, nil, map[string]any{"bundleRoot": "", "stateFile": ""}, map[string]any{"target": prepareTargetMap(target)})
				if err != nil {
					return fmt.Errorf("E_SCHEMA_INVALID: artifacts.packages group %s backend render failed: %v", group.Group, err)
				}
			}
			spec := map[string]any{"distro": map[string]any{"family": target.OSFamily, "release": target.Release, "arch": target.Arch}, "packages": []any{"placeholder"}}
			if len(renderedRepo) > 0 {
				spec["repo"] = renderedRepo
			}
			if len(renderedBackend) > 0 {
				spec["backend"] = renderedBackend
			}
			root, err := packageArtifactRootForValidation(spec, "packages")
			if err != nil {
				return fmt.Errorf("E_SCHEMA_INVALID: artifacts.packages group %s target %s/%s: %v", group.Group, target.OSFamily, target.Release, err)
			}
			jobID := fmt.Sprintf("%s:%s/%s", group.Group, target.OSFamily, target.Release)
			if prev, exists := packageRoots[root]; exists {
				return fmt.Errorf("E_SCHEMA_INVALID: duplicate package artifact root %s (%s and %s)", root, prev, jobID)
			}
			packageRoots[root] = jobID
		}
	}
	for _, group := range wf.Artifacts.Images {
		if err := validateArtifactExecution(group.Execution, fmt.Sprintf("artifacts.images group %s", group.Group)); err != nil {
			return err
		}
		for _, target := range expandArtifactTargets(group.Targets) {
			renderedOutput := map[string]any{}
			if len(group.Output) > 0 {
				var err error
				renderedOutput, err = workflowexec.RenderSpecWithExtra(group.Output, wf, nil, map[string]any{"bundleRoot": "", "stateFile": ""}, map[string]any{"target": prepareTargetMap(target)})
				if err != nil {
					return fmt.Errorf("E_SCHEMA_INVALID: artifacts.images group %s output render failed: %v", group.Group, err)
				}
			}
			for _, item := range group.Items {
				renderedImage, err := workflowexec.RenderSpecWithExtra(map[string]any{"image": item.Image}, wf, nil, map[string]any{"bundleRoot": "", "stateFile": ""}, map[string]any{"target": prepareTargetMap(target)})
				if err != nil {
					return fmt.Errorf("E_SCHEMA_INVALID: artifacts.images group %s image render failed: %v", group.Group, err)
				}
				image := strings.TrimSpace(stringValue(renderedImage, "image"))
				path := imageArchivePathForValidation(renderedOutput, image)
				jobID := fmt.Sprintf("%s:%s", group.Group, image)
				if prev, exists := imageOutputs[path]; exists {
					return fmt.Errorf("E_SCHEMA_INVALID: duplicate image artifact output path %s (%s and %s)", path, prev, jobID)
				}
				imageOutputs[path] = jobID
			}
		}
	}
	return nil
}

func validateArtifactExecution(spec *config.ArtifactExecutionSpec, scope string) error {
	if spec == nil {
		return nil
	}
	if spec.Parallelism != 0 && spec.Parallelism < 1 {
		return fmt.Errorf("E_SCHEMA_INVALID: %s execution.parallelism must be >= 1", scope)
	}
	if spec.Retry < 0 {
		return fmt.Errorf("E_SCHEMA_INVALID: %s execution.retry must be >= 0", scope)
	}
	return nil
}

func expandArtifactTargets(targets []config.ArtifactTarget) []config.ArtifactTarget {
	if len(targets) == 0 {
		return []config.ArtifactTarget{{}}
	}
	return targets
}

func prepareTargetMap(target config.ArtifactTarget) map[string]any {
	return map[string]any{"os": target.OS, "osFamily": target.OSFamily, "release": target.Release, "arch": target.Arch}
}

func packageArtifactRootForValidation(spec map[string]any, defaultDir string) (string, error) {
	output := mapValue(spec, "output")
	dir := stringValue(output, "dir")
	if dir == "" {
		dir = defaultDir
	}
	backend := mapValue(spec, "backend")
	if stringValue(backend, "mode") == "container" && stringValue(backend, "image") != "" {
		repo := mapValue(spec, "repo")
		if len(repo) > 0 {
			distro := mapValue(spec, "distro")
			release := strings.TrimSpace(stringValue(distro, "release"))
			if release == "" {
				return "", fmt.Errorf("packages action download repo mode requires distro.release")
			}
			switch strings.TrimSpace(stringValue(repo, "type")) {
			case "apt-flat":
				return filepath.ToSlash(filepath.Join("packages", "apt", release)), nil
			case "yum":
				return filepath.ToSlash(filepath.Join("packages", "yum", release)), nil
			default:
				return "", fmt.Errorf("packages action download repo.type must be apt-flat or yum")
			}
		}
	}
	return filepath.ToSlash(dir), nil
}

func imageArchivePathForValidation(output map[string]any, image string) string {
	dir := stringValue(output, "dir")
	if dir == "" {
		dir = "images"
	}
	replacer := strings.NewReplacer("/", "_", ":", "_", "@", "_")
	return filepath.ToSlash(filepath.Join(dir, replacer.Replace(image)+".tar"))
}

func mapValue(v map[string]any, key string) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	if mv, ok := v[key].(map[string]any); ok {
		return mv
	}
	return map[string]any{}
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
