package config

import (
	"bytes"
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
		abs := filepath.Join(componentsRoot, filepath.FromSlash(ref))
		b, err := os.ReadFile(abs)
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
		Role      string         `json:"role"`
		Version   string         `json:"version"`
		Vars      map[string]any `json:"vars,omitempty"`
		Artifacts *ArtifactsSpec `json:"artifacts,omitempty"`
		Phases    []Phase        `json:"phases,omitempty"`
		Steps     []Step         `json:"steps,omitempty"`
	}
	payload := canonicalWorkflow{
		Role:      wf.Role,
		Version:   wf.Version,
		Vars:      wf.Vars,
		Artifacts: wf.Artifacts,
		Phases:    wf.Phases,
		Steps:     wf.Steps,
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
	hasArtifacts := wf.Artifacts != nil && (len(wf.Artifacts.Files) > 0 || len(wf.Artifacts.Images) > 0 || len(wf.Artifacts.Packages) > 0)
	return modeCount(hasArtifacts, len(wf.Phases) > 0, len(wf.Steps) > 0) > 1
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
		workflowRoot, err := localWorkflowRoot(origin.localPath)
		varsPath := ""
		if err == nil {
			varsPath = filepath.Join(workflowRoot, "vars.yaml")
		} else {
			varsPath = filepath.Join(filepath.Dir(origin.localPath), "vars.yaml")
		}
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
		workflowRoot, err := remoteWorkflowRoot(origin.remoteURL)
		varsURL := url.URL{}
		if err == nil {
			varsURL = *workflowRoot
			varsURL.Path = path.Join(varsURL.Path, "vars.yaml")
		} else {
			varsURL = *siblingURL(origin.remoteURL, "vars.yaml")
		}
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

func normalizeComponentImportRef(ref string) (string, error) {
	ref = strings.TrimSpace(strings.ReplaceAll(ref, "\\", "/"))
	if ref == "" {
		return "", fmt.Errorf("workflow import path is empty")
	}
	if strings.HasPrefix(ref, "/") {
		return "", fmt.Errorf("workflow import path must be components-relative: %s", ref)
	}
	if strings.Contains(ref, "://") {
		return "", fmt.Errorf("workflow import path must not be a URL: %s", ref)
	}
	cleaned := path.Clean(ref)
	if cleaned == "." || cleaned == "" || strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", fmt.Errorf("workflow import path must stay under components root: %s", ref)
	}
	return cleaned, nil
}

func localComponentsRoot(localPath string) (string, error) {
	workflowRoot, err := localWorkflowRoot(localPath)
	if err != nil {
		return "", err
	}
	return filepath.Join(workflowRoot, "components"), nil
}

func localWorkflowRoot(localPath string) (string, error) {
	current := filepath.Dir(localPath)
	for {
		if filepath.Base(current) == "workflows" {
			return current, nil
		}
		next := filepath.Dir(current)
		if next == current {
			return "", fmt.Errorf("workflow import requires file under workflows/: %s", localPath)
		}
		current = next
	}
}

func remoteComponentsRoot(u *url.URL) (*url.URL, error) {
	workflowRoot, err := remoteWorkflowRoot(u)
	if err != nil {
		return nil, err
	}
	v := *workflowRoot
	v.Path = path.Join(v.Path, "components")
	v.RawQuery = ""
	v.Fragment = ""
	return &v, nil
}

func remoteWorkflowRoot(u *url.URL) (*url.URL, error) {
	cleanPath := path.Clean(u.Path)
	marker := "/workflows/"
	idx := strings.LastIndex(cleanPath, marker)
	if idx < 0 {
		return nil, fmt.Errorf("workflow import requires URL under /workflows/: %s", u.String())
	}
	rootPath := cleanPath[:idx+len("/workflows")]
	v := *u
	v.Path = rootPath
	v.RawQuery = ""
	v.Fragment = ""
	return &v, nil
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
