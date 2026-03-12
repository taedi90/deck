package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type LoadOptions struct {
	VarOverrides map[string]any
}

var workflowHTTPClient = &http.Client{Timeout: 10 * time.Second}

func Load(ctx context.Context, source string) (*Workflow, error) {
	return LoadWithOptions(ctx, source, LoadOptions{})
}

func LoadWithOptions(ctx context.Context, source string, opts LoadOptions) (*Workflow, error) {
	if ctx == nil {
		ctx = context.Background()
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
	if len(resolved.Phases) > 0 && len(resolved.Steps) > 0 {
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
	if err := yaml.Unmarshal(workflowBytes, &wf); err != nil {
		return nil, nil, fmt.Errorf("parse yaml: %w", err)
	}
	if len(wf.Phases) > 0 && len(wf.Steps) > 0 {
		return nil, nil, fmt.Errorf("workflow cannot set both phases and steps")
	}

	workflowImportVars, err := loadWorkflowVarImports(ctx, origin, wf.VarImports)
	if err != nil {
		return nil, nil, err
	}
	if wf.Vars == nil {
		wf.Vars = map[string]any{}
	}
	baseWorkflowVars := map[string]any{}
	mergeVars(baseWorkflowVars, workflowImportVars)
	mergeVars(baseWorkflowVars, wf.Vars)
	wf.Vars = baseWorkflowVars
	wf.VarImports = nil

	if err := expandPhaseImports(ctx, &wf, origin, visiting); err != nil {
		return nil, nil, err
	}

	aggregated := &Workflow{}
	for _, importRef := range wf.Imports {
		importBytes, importOrigin, err := loadImportSource(ctx, origin, importRef)
		if err != nil {
			return nil, nil, err
		}
		importedWorkflow, _, err := loadWorkflowWithImports(ctx, importBytes, importOrigin, visiting)
		if err != nil {
			return nil, nil, err
		}
		if err := mergeWorkflow(aggregated, importedWorkflow, fmt.Sprintf("import %q", importRef)); err != nil {
			return nil, nil, err
		}
	}

	wf.Imports = nil
	if err := mergeWorkflow(aggregated, &wf, "workflow"); err != nil {
		return nil, nil, err
	}

	resolvedBytes, err := canonicalWorkflowBytes(aggregated)
	if err != nil {
		return nil, nil, err
	}
	return aggregated, resolvedBytes, nil
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
			importBytes, importOrigin, err := loadImportSource(ctx, origin, pathRef)
			if err != nil {
				return err
			}
			importedWorkflow, _, err := loadWorkflowWithImports(ctx, importBytes, importOrigin, visiting)
			if err != nil {
				return err
			}
			phaseSteps, err := extractPhaseImportSteps(importedWorkflow, phase.Name, pathRef)
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

func extractPhaseImportSteps(imported *Workflow, targetPhaseName string, sourceRef string) ([]Step, error) {
	if imported == nil {
		return nil, fmt.Errorf("phase import %q is nil", sourceRef)
	}
	if len(imported.Steps) > 0 {
		steps := append([]Step(nil), imported.Steps...)
		return steps, nil
	}
	if len(imported.Phases) == 0 {
		return nil, fmt.Errorf("phase import %q does not contain steps", sourceRef)
	}

	matched := make([]Step, 0)
	for _, p := range imported.Phases {
		if p.Name == targetPhaseName {
			matched = append(matched, p.Steps...)
		}
	}
	if len(matched) > 0 {
		return matched, nil
	}
	if len(imported.Phases) == 1 {
		steps := append([]Step(nil), imported.Phases[0].Steps...)
		return steps, nil
	}
	return nil, fmt.Errorf("phase import %q has phases but none match %q", sourceRef, targetPhaseName)
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

func mergeWorkflow(target *Workflow, src *Workflow, sourceLabel string) error {
	if target == nil || src == nil {
		return nil
	}

	srcRole := strings.TrimSpace(src.Role)
	if srcRole != "" {
		targetRole := strings.TrimSpace(target.Role)
		if targetRole == "" {
			target.Role = srcRole
		} else if targetRole != srcRole {
			return fmt.Errorf("workflow role mismatch in %s: expected %s, got %s", sourceLabel, targetRole, srcRole)
		}
	}

	srcVersion := strings.TrimSpace(src.Version)
	if srcVersion != "" {
		targetVersion := strings.TrimSpace(target.Version)
		if targetVersion == "" {
			target.Version = srcVersion
		} else if targetVersion != srcVersion {
			return fmt.Errorf("workflow version mismatch in %s: expected %s, got %s", sourceLabel, targetVersion, srcVersion)
		}
	}

	hasSrcPhases := len(src.Phases) > 0
	hasSrcSteps := len(src.Steps) > 0
	if hasSrcPhases && hasSrcSteps {
		return fmt.Errorf("workflow cannot set both phases and steps in %s", sourceLabel)
	}
	if hasSrcPhases && len(target.Steps) > 0 {
		return fmt.Errorf("workflow phase/step mode mismatch in %s: cannot merge phases into steps workflow", sourceLabel)
	}
	if hasSrcSteps && len(target.Phases) > 0 {
		return fmt.Errorf("workflow phase/step mode mismatch in %s: cannot merge steps into phases workflow", sourceLabel)
	}
	if hasSrcPhases {
		for _, srcPhase := range src.Phases {
			merged := false
			for i := range target.Phases {
				if target.Phases[i].Name == srcPhase.Name {
					target.Phases[i].Steps = append(target.Phases[i].Steps, srcPhase.Steps...)
					merged = true
					break
				}
			}
			if !merged {
				target.Phases = append(target.Phases, srcPhase)
			}
		}
	}
	if hasSrcSteps {
		target.Steps = append(target.Steps, src.Steps...)
	}

	if target.Vars == nil {
		target.Vars = map[string]any{}
	}
	mergeVars(target.Vars, src.Vars)
	return nil
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

func loadImportSource(ctx context.Context, origin workflowOrigin, importRef string) ([]byte, workflowOrigin, error) {
	ref := strings.TrimSpace(importRef)
	if ref == "" {
		return nil, workflowOrigin{}, fmt.Errorf("workflow import path is empty")
	}

	if u, ok := parseHTTPURL(ref); ok {
		b, err := getRequiredHTTP(ctx, u.String())
		if err != nil {
			return nil, workflowOrigin{}, err
		}
		return b, workflowOrigin{remoteURL: u}, nil
	}

	if origin.localPath != "" {
		joined := filepath.Clean(filepath.Join(filepath.Dir(origin.localPath), ref))
		abs, err := filepath.Abs(joined)
		if err != nil {
			return nil, workflowOrigin{}, fmt.Errorf("resolve import path: %w", err)
		}
		b, err := os.ReadFile(abs)
		if err != nil {
			return nil, workflowOrigin{}, fmt.Errorf("read workflow file: %w", err)
		}
		return b, workflowOrigin{localPath: abs}, nil
	}

	if origin.remoteURL != nil {
		importURL, err := resolveImportURL(origin.remoteURL, ref)
		if err != nil {
			return nil, workflowOrigin{}, err
		}
		b, err := getRequiredHTTP(ctx, importURL.String())
		if err != nil {
			return nil, workflowOrigin{}, err
		}
		return b, workflowOrigin{remoteURL: importURL}, nil
	}

	return nil, workflowOrigin{}, fmt.Errorf("cannot resolve workflow import %q", ref)
}

func resolveImportURL(base *url.URL, ref string) (*url.URL, error) {
	rel, err := url.Parse(ref)
	if err != nil {
		return nil, fmt.Errorf("parse import url: %w", err)
	}
	if rel.Scheme != "" && rel.Scheme != "http" && rel.Scheme != "https" {
		return nil, fmt.Errorf("unsupported import url scheme: %s", rel.Scheme)
	}
	resolved := base.ResolveReference(rel)
	if strings.TrimSpace(resolved.Host) == "" {
		return nil, fmt.Errorf("invalid import url: %s", resolved.String())
	}
	return resolved, nil
}

func canonicalWorkflowBytes(wf *Workflow) ([]byte, error) {
	if wf == nil {
		return nil, fmt.Errorf("workflow is nil")
	}
	type canonicalWorkflow struct {
		Role    string         `json:"role"`
		Version string         `json:"version"`
		Vars    map[string]any `json:"vars,omitempty"`
		Phases  []Phase        `json:"phases,omitempty"`
		Steps   []Step         `json:"steps,omitempty"`
	}
	payload := canonicalWorkflow{
		Role:    wf.Role,
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

func computeStateKey(workflowBytes []byte, effectiveVars map[string]any) string {
	normalizedWorkflow := normalizeWorkflowBytes(workflowBytes)
	varLines := renderEffectiveVars(effectiveVars)

	h := sha256.New()
	_, _ = h.Write(normalizedWorkflow)
	_, _ = h.Write([]byte("\n--vars--\n"))
	_, _ = h.Write([]byte(varLines))
	return hex.EncodeToString(h.Sum(nil))
}

func normalizeWorkflowBytes(workflowBytes []byte) []byte {
	if len(workflowBytes) == 0 {
		return nil
	}
	return []byte(strings.ReplaceAll(string(workflowBytes), "\r\n", "\n"))
}

func computeWorkflowSHA256(workflowBytes []byte) string {
	normalizedWorkflow := normalizeWorkflowBytes(workflowBytes)
	h := sha256.Sum256(normalizedWorkflow)
	return hex.EncodeToString(h[:])
}

func renderEffectiveVars(effectiveVars map[string]any) string {
	if len(effectiveVars) == 0 {
		return ""
	}
	keys := make([]string, 0, len(effectiveVars))
	for key := range effectiveVars {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, key := range keys {
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(stableVarValue(effectiveVars[key]))
		b.WriteString("\n")
	}
	return b.String()
}

func stableVarValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	encoded, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(encoded)
}

type workflowOrigin struct {
	localPath string
	remoteURL *url.URL
}

func loadWorkflowSource(ctx context.Context, source string) ([]byte, workflowOrigin, error) {
	if u, ok := parseHTTPURL(source); ok {
		b, err := getRequiredHTTP(ctx, u.String())
		if err != nil {
			return nil, workflowOrigin{}, err
		}
		return b, workflowOrigin{remoteURL: u}, nil
	}

	abs, err := filepath.Abs(source)
	if err != nil {
		return nil, workflowOrigin{}, fmt.Errorf("resolve path: %w", err)
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil, workflowOrigin{}, fmt.Errorf("read workflow file: %w", err)
	}
	return b, workflowOrigin{localPath: abs}, nil
}

func loadBaseVars(ctx context.Context, origin workflowOrigin) (map[string]any, error) {
	if origin.localPath != "" {
		varsPath := filepath.Join(filepath.Dir(origin.localPath), "vars.yaml")
		b, err := os.ReadFile(varsPath)
		if err != nil {
			if os.IsNotExist(err) {
				return map[string]any{}, nil
			}
			return nil, fmt.Errorf("read vars file: %w", err)
		}
		return parseVarsYAML(b)
	}

	if origin.remoteURL != nil {
		varsURL := siblingURL(origin.remoteURL, "vars.yaml")
		b, ok, err := getOptionalHTTP(ctx, varsURL.String())
		if err != nil {
			return nil, err
		}
		if !ok {
			return map[string]any{}, nil
		}
		return parseVarsYAML(b)
	}

	return map[string]any{}, nil
}

func loadWorkflowVarImports(ctx context.Context, origin workflowOrigin, refs []string) (map[string]any, error) {
	merged := map[string]any{}
	for _, ref := range refs {
		trimmed := strings.TrimSpace(ref)
		if trimmed == "" {
			return nil, fmt.Errorf("var import path is empty")
		}
		b, _, err := loadImportSource(ctx, origin, trimmed)
		if err != nil {
			return nil, err
		}
		vars, err := parseVarsYAML(b)
		if err != nil {
			return nil, fmt.Errorf("parse var import %q: %w", trimmed, err)
		}
		mergeVars(merged, vars)
	}
	return merged, nil
}

func parseVarsYAML(content []byte) (map[string]any, error) {
	if len(content) == 0 {
		return map[string]any{}, nil
	}

	vars := map[string]any{}
	if err := yaml.Unmarshal(content, &vars); err != nil {
		return nil, fmt.Errorf("parse vars yaml: %w", err)
	}
	if vars == nil {
		return map[string]any{}, nil
	}
	return vars, nil
}

func mergeVars(dst map[string]any, src map[string]any) {
	for k, v := range src {
		dst[k] = v
	}
}

func parseHTTPURL(raw string) (*url.URL, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, false
	}
	if strings.TrimSpace(u.Host) == "" {
		return nil, false
	}
	return u, true
}

func siblingURL(u *url.URL, fileName string) *url.URL {
	v := *u
	v.Path = path.Join(path.Dir(u.Path), fileName)
	v.RawQuery = ""
	v.Fragment = ""
	return &v
}

func getRequiredHTTP(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("get workflow url: %w", err)
	}
	resp, err := workflowHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get workflow url: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get workflow url: unexpected status %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read workflow url: %w", err)
	}
	return b, nil
}

func getOptionalHTTP(ctx context.Context, rawURL string) ([]byte, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, fmt.Errorf("get vars url: %w", err)
	}
	resp, err := workflowHTTPClient.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("get vars url: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("get vars url: unexpected status %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("read vars url: %w", err)
	}
	return b, true, nil
}
