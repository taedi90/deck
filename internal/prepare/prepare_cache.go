package prepare

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/taedi90/deck/internal/config"
)

const (
	prepareCacheActionFetch = "FETCH"
	prepareCacheActionReuse = "REUSE"
)

var (
	prepareCacheTemplateExprPattern = regexp.MustCompile(`\{\{[^}]*\}\}`)
	prepareCacheVarRefPattern       = regexp.MustCompile(`\.vars\.([A-Za-z_][A-Za-z0-9_]*)`)
)

type prepareCacheState struct {
	Artifacts []prepareCacheArtifactState `json:"artifacts"`
}

type prepareCacheArtifactState struct {
	StepID    string            `json:"step_id"`
	Type      string            `json:"type"`
	CacheKey  string            `json:"cache_key"`
	InputVars map[string]string `json:"input_vars"`
}

type PrepareCachePlan struct {
	WorkflowSHA256 string                     `json:"workflow_sha256"`
	Artifacts      []prepareCacheArtifactPlan `json:"artifacts"`
}

type prepareCacheArtifactPlan struct {
	StepID    string            `json:"step_id"`
	Type      string            `json:"type"`
	CacheKey  string            `json:"cache_key"`
	Action    string            `json:"action"`
	InputVars map[string]string `json:"input_vars"`
}

func ComputePrepareCachePlan(prevState prepareCacheState, workflowBytes []byte, effectiveVars map[string]any, steps []config.Step) PrepareCachePlan {
	workflowSHA := computeWorkflowSHA256(workflowBytes)
	currentArtifacts := collectPrepareCacheArtifacts(steps, effectiveVars)
	prevByIdentity := map[string]prepareCacheArtifactState{}
	for _, item := range prevState.Artifacts {
		prevByIdentity[prepareCacheIdentity(item.StepID, item.Type)] = item
	}

	artifacts := make([]prepareCacheArtifactPlan, 0, len(currentArtifacts))
	for _, item := range currentArtifacts {
		action := prepareCacheActionFetch
		if prev, ok := prevByIdentity[prepareCacheIdentity(item.StepID, item.Type)]; ok {
			if prev.CacheKey == item.CacheKey && equalStringMap(prev.InputVars, item.InputVars) {
				action = prepareCacheActionReuse
			}
		}
		artifacts = append(artifacts, prepareCacheArtifactPlan{
			StepID:    item.StepID,
			Type:      item.Type,
			CacheKey:  item.CacheKey,
			Action:    action,
			InputVars: item.InputVars,
		})
	}

	return PrepareCachePlan{
		WorkflowSHA256: workflowSHA,
		Artifacts:      artifacts,
	}
}

func prepareCacheStateFromPlan(plan PrepareCachePlan) prepareCacheState {
	artifacts := make([]prepareCacheArtifactState, 0, len(plan.Artifacts))
	for _, item := range plan.Artifacts {
		artifacts = append(artifacts, prepareCacheArtifactState{
			StepID:    item.StepID,
			Type:      item.Type,
			CacheKey:  item.CacheKey,
			InputVars: cloneStringMap(item.InputVars),
		})
	}
	return prepareCacheState{Artifacts: artifacts}
}

func defaultPrepareCacheStatePath(workflowSHA string) (string, error) {
	if strings.TrimSpace(workflowSHA) == "" {
		return "", fmt.Errorf("workflow sha256 is empty")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}
	return filepath.Join(home, ".deck", "cache", "state", workflowSHA+".json"), nil
}

func loadPrepareCacheState(path string) (prepareCacheState, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return prepareCacheState{Artifacts: []prepareCacheArtifactState{}}, nil
		}
		return prepareCacheState{}, fmt.Errorf("read prepare cache state: %w", err)
	}

	var st prepareCacheState
	if err := json.Unmarshal(raw, &st); err != nil {
		return prepareCacheState{}, fmt.Errorf("parse prepare cache state: %w", err)
	}
	if st.Artifacts == nil {
		st.Artifacts = []prepareCacheArtifactState{}
	}
	return st, nil
}

func savePrepareCacheState(path string, st prepareCacheState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create prepare cache state directory: %w", err)
	}

	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode prepare cache state: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("write temp prepare cache state: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace prepare cache state: %w", err)
	}
	return nil
}

func computeWorkflowSHA256(workflowBytes []byte) string {
	normalized := []byte(strings.ReplaceAll(string(workflowBytes), "\r\n", "\n"))
	sum := sha256.Sum256(normalized)
	return hex.EncodeToString(sum[:])
}

func collectPrepareCacheArtifacts(steps []config.Step, effectiveVars map[string]any) []prepareCacheArtifactState {
	out := make([]prepareCacheArtifactState, 0)
	for _, step := range steps {
		artifactType, ok := stepArtifactType(step.Kind)
		if !ok {
			continue
		}
		inputVarNames := collectStepInputVarNames(step.Spec)
		inputVars := map[string]string{}
		for _, name := range inputVarNames {
			value, ok := effectiveVars[name]
			if !ok {
				inputVars[name] = "__MISSING__"
				continue
			}
			inputVars[name] = stablePrepareCacheVarValue(value)
		}
		out = append(out, prepareCacheArtifactState{
			StepID:    step.ID,
			Type:      artifactType,
			CacheKey:  computeStepCacheKey(step),
			InputVars: inputVars,
		})
	}
	return out
}

func stepArtifactType(kind string) (string, bool) {
	switch kind {
	case "PackageFetch":
		return "package", true
	case "ImageFetch":
		return "image", true
	case "FileFetch":
		return "file", true
	default:
		return "", false
	}
}

func collectStepInputVarNames(spec map[string]any) []string {
	seen := map[string]bool{}
	collectInputVarNamesFromAny(spec, seen)
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func collectInputVarNamesFromAny(v any, seen map[string]bool) {
	switch tv := v.(type) {
	case string:
		for _, expr := range prepareCacheTemplateExprPattern.FindAllString(tv, -1) {
			matches := prepareCacheVarRefPattern.FindAllStringSubmatch(expr, -1)
			for _, match := range matches {
				if len(match) == 2 {
					seen[match[1]] = true
				}
			}
		}
	case map[string]any:
		for _, item := range tv {
			collectInputVarNamesFromAny(item, seen)
		}
	case []any:
		for _, item := range tv {
			collectInputVarNamesFromAny(item, seen)
		}
	}
}

func computeStepCacheKey(step config.Step) string {
	raw, err := json.Marshal(step.Spec)
	specHashInput := ""
	if err == nil {
		specHashInput = string(raw)
	} else {
		specHashInput = fmt.Sprintf("%v", step.Spec)
	}
	sum := sha256.Sum256([]byte(step.Kind + "\n" + step.ID + "\n" + specHashInput))
	return hex.EncodeToString(sum[:])
}

func stablePrepareCacheVarValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(raw)
}

func prepareCacheIdentity(stepID, artifactType string) string {
	return stepID + "\n" + artifactType
}

func equalStringMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
