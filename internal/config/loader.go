package config

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/taedi90/deck/internal/fsutil"
)

type LoadOptions struct {
	VarOverrides map[string]any
}

func Load(ctx context.Context, source string) (*Workflow, error) {
	return LoadWithOptions(ctx, source, LoadOptions{})
}

func LoadWithOptions(ctx context.Context, source string, opts LoadOptions) (*Workflow, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}
	source = strings.TrimSpace(source)
	if source == "" {
		return nil, fmt.Errorf("workflow path is empty")
	}

	workflowBytes, origin, err := loadWorkflowSource(ctx, source)
	if err != nil {
		return nil, err
	}

	resolved, resolvedWorkflowBytes, err := loadWorkflowWithImports(ctx, workflowBytes, origin, map[string]bool{})
	if err != nil {
		return nil, err
	}
	if workflowHasMultipleModes(resolved) {
		return nil, fmt.Errorf("workflow cannot set both phases and steps")
	}

	effectiveVars := map[string]any{}
	baseVars, err := loadBaseVars(ctx, origin)
	if err != nil {
		return nil, err
	}
	mergeVars(effectiveVars, baseVars)
	mergeVars(effectiveVars, resolved.Vars)
	mergeVars(effectiveVars, opts.VarOverrides)

	resolved.Vars = effectiveVars
	resolved.StateKey = computeStateKey(resolvedWorkflowBytes, effectiveVars)
	resolved.WorkflowSHA256 = computeWorkflowSHA256(resolvedWorkflowBytes)
	return resolved, nil
}

func loadWorkflowWithImports(ctx context.Context, workflowBytes []byte, origin workflowOrigin, visiting map[string]bool) (*Workflow, []byte, error) {
	originKey := workflowOriginKey(origin)
	if visiting[originKey] {
		return nil, nil, fmt.Errorf("workflow import cycle detected at %s", originKey)
	}
	visiting[originKey] = true
	defer delete(visiting, originKey)

	var wf Workflow
	if err := unmarshalYAMLStrict(workflowBytes, &wf); err != nil {
		return nil, nil, fmt.Errorf("parse yaml: %w", err)
	}
	if workflowHasMultipleModes(&wf) {
		return nil, nil, fmt.Errorf("workflow cannot set both phases and steps")
	}

	if wf.Vars == nil {
		wf.Vars = map[string]any{}
	}

	if err := expandPhaseImports(ctx, &wf, origin, visiting); err != nil {
		return nil, nil, err
	}

	resolvedBytes, err := canonicalWorkflowBytes(&wf)
	if err != nil {
		return nil, nil, err
	}
	return &wf, resolvedBytes, nil
}

func expandPhaseImports(ctx context.Context, wf *Workflow, origin workflowOrigin, visiting map[string]bool) error {
	if wf == nil {
		return nil
	}
	for i := range wf.Phases {
		phase := &wf.Phases[i]
		if len(phase.Imports) == 0 {
			continue
		}
		importedSteps := make([]Step, 0)
		for _, phaseImport := range phase.Imports {
			pathRef := strings.TrimSpace(phaseImport.Path)
			if pathRef == "" {
				return fmt.Errorf("phase import path is empty in phase %q", phase.Name)
			}
			importBytes, importOrigin, err := loadComponentImportSource(ctx, origin, pathRef)
			if err != nil {
				return err
			}
			phaseSteps, err := loadComponentFragment(ctx, importBytes, importOrigin, visiting, pathRef)
			if err != nil {
				return err
			}
			for si := range phaseSteps {
				phaseSteps[si].When = combineWhen(phaseImport.When, phaseSteps[si].When)
			}
			importedSteps = append(importedSteps, phaseSteps...)
		}
		phase.Steps = append(importedSteps, phase.Steps...)
		phase.Imports = nil
	}
	return nil
}

type componentFragment struct {
	Steps []Step `yaml:"steps"`
}

func loadComponentFragment(ctx context.Context, workflowBytes []byte, origin workflowOrigin, visiting map[string]bool, sourceRef string) ([]Step, error) {
	originKey := workflowOriginKey(origin)
	if visiting[originKey] {
		return nil, fmt.Errorf("workflow import cycle detected at %s", originKey)
	}
	visiting[originKey] = true
	defer delete(visiting, originKey)

	var fragment componentFragment
	if err := unmarshalYAMLStrict(workflowBytes, &fragment); err != nil {
		return nil, fmt.Errorf("parse component fragment %q: %w", sourceRef, err)
	}
	if len(fragment.Steps) == 0 {
		return nil, fmt.Errorf("phase import %q does not contain steps", sourceRef)
	}
	return append([]Step(nil), fragment.Steps...), nil
}

func combineWhen(outer string, inner string) string {
	outer = strings.TrimSpace(outer)
	inner = strings.TrimSpace(inner)
	if outer == "" {
		return inner
	}
	if inner == "" {
		return outer
	}
	return fmt.Sprintf("(%s) && (%s)", outer, inner)
}

func unmarshalYAMLStrict(content []byte, target any) error {
	dec := yaml.NewDecoder(bytes.NewReader(content))
	dec.KnownFields(true)
	return dec.Decode(target)
}

func workflowOriginKey(origin workflowOrigin) string {
	if origin.localPath != "" {
		return "file:" + origin.localPath
	}
	if origin.remoteURL != nil {
		return "url:" + origin.remoteURL.String()
	}
	return "unknown"
}

func loadComponentImportSource(ctx context.Context, origin workflowOrigin, importRef string) ([]byte, workflowOrigin, error) {
	ref := strings.TrimSpace(importRef)
	if ref == "" {
		return nil, workflowOrigin{}, fmt.Errorf("workflow import path is empty")
	}
	ref, err := normalizeComponentImportRef(ref)
	if err != nil {
		return nil, workflowOrigin{}, err
	}

	if origin.localPath != "" {
		componentsRoot, err := localComponentsRoot(origin.localPath)
		if err != nil {
			return nil, workflowOrigin{}, err
		}
		root, err := fsutil.NewRoot(componentsRoot)
		if err != nil {
			return nil, workflowOrigin{}, err
		}
		abs, err := root.Resolve(filepath.FromSlash(ref))
		if err != nil {
			return nil, workflowOrigin{}, err
		}
		b, _, err := root.ReadFile(filepath.FromSlash(ref))
		if err != nil {
			return nil, workflowOrigin{}, fmt.Errorf("read workflow file: %w", err)
		}
		return b, workflowOrigin{localPath: abs}, nil
	}

	if origin.remoteURL != nil {
		componentsRoot, err := remoteComponentsRoot(origin.remoteURL)
		if err != nil {
			return nil, workflowOrigin{}, err
		}
		importURL := *componentsRoot
		importURL.Path = path.Join(importURL.Path, ref)
		b, err := getRequiredHTTP(ctx, importURL.String())
		if err != nil {
			return nil, workflowOrigin{}, err
		}
		return b, workflowOrigin{remoteURL: &importURL}, nil
	}

	return nil, workflowOrigin{}, fmt.Errorf("cannot resolve workflow import %q", ref)
}

func canonicalWorkflowBytes(wf *Workflow) ([]byte, error) {
	if wf == nil {
		return nil, fmt.Errorf("workflow is nil")
	}
	type canonicalWorkflow struct {
		Version string         `json:"version"`
		Vars    map[string]any `json:"vars,omitempty"`
		Phases  []Phase        `json:"phases,omitempty"`
		Steps   []Step         `json:"steps,omitempty"`
	}
	payload := canonicalWorkflow{
		Version: wf.Version,
		Vars:    wf.Vars,
		Phases:  wf.Phases,
		Steps:   wf.Steps,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal workflow: %w", err)
	}
	return raw, nil
}

func workflowHasMultipleModes(wf *Workflow) bool {
	if wf == nil {
		return false
	}
	return modeCount(len(wf.Phases) > 0, len(wf.Steps) > 0) > 1
}

func modeCount(flags ...bool) int {
	count := 0
	for _, flag := range flags {
		if flag {
			count++
		}
	}
	return count
}
