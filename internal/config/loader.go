package config

import (
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

func Load(source string) (*Workflow, error) {
	return LoadWithOptions(source, LoadOptions{})
}

func LoadWithOptions(source string, opts LoadOptions) (*Workflow, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil, fmt.Errorf("workflow path is empty")
	}

	workflowBytes, origin, err := loadWorkflowSource(source)
	if err != nil {
		return nil, err
	}

	var wf Workflow
	if err := yaml.Unmarshal(workflowBytes, &wf); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}
	if len(wf.Phases) > 0 && len(wf.Steps) > 0 {
		return nil, fmt.Errorf("workflow cannot set both phases and steps")
	}

	effectiveVars := map[string]any{}
	baseVars, err := loadBaseVars(origin)
	if err != nil {
		return nil, err
	}
	mergeVars(effectiveVars, baseVars)
	mergeVars(effectiveVars, wf.Vars)
	mergeVars(effectiveVars, opts.VarOverrides)

	wf.Vars = effectiveVars
	wf.StateKey = computeStateKey(workflowBytes, effectiveVars)
	wf.WorkflowSHA256 = computeWorkflowSHA256(workflowBytes)
	return &wf, nil
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

func loadWorkflowSource(source string) ([]byte, workflowOrigin, error) {
	if u, ok := parseHTTPURL(source); ok {
		b, err := getRequiredHTTP(u.String())
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

func loadBaseVars(origin workflowOrigin) (map[string]any, error) {
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
		b, ok, err := getOptionalHTTP(varsURL.String())
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

func getRequiredHTTP(rawURL string) ([]byte, error) {
	resp, err := workflowHTTPClient.Get(rawURL)
	if err != nil {
		return nil, fmt.Errorf("get workflow url: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get workflow url: unexpected status %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read workflow url: %w", err)
	}
	return b, nil
}

func getOptionalHTTP(rawURL string) ([]byte, bool, error) {
	resp, err := workflowHTTPClient.Get(rawURL)
	if err != nil {
		return nil, false, fmt.Errorf("get vars url: %w", err)
	}
	defer resp.Body.Close()

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
