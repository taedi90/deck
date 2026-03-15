package prepare

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/taedi90/deck/internal/config"
	"github.com/taedi90/deck/internal/workflowexec"
)

type RunOptions struct {
	BundleRoot      string
	CommandRunner   CommandRunner
	ForceRedownload bool
}

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) error
	LookPath(file string) (string, error)
}

type osCommandRunner struct{}

func (o osCommandRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (o osCommandRunner) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

const (
	errCodePrepareRuntimeMissing        = "E_PREPARE_RUNTIME_NOT_FOUND"
	errCodePrepareRuntimeUnsupported    = "E_PREPARE_RUNTIME_UNSUPPORTED"
	errCodePrepareEngineUnsupported     = "E_PREPARE_ENGINE_UNSUPPORTED"
	errCodePrepareArtifactsEmpty        = "E_PREPARE_NO_ARTIFACTS"
	errCodeArtifactSourceNotFound       = "E_PREPARE_SOURCE_NOT_FOUND"
	errCodePrepareSourceNotFound        = errCodeArtifactSourceNotFound
	errCodePrepareChecksumMismatch      = "E_PREPARE_CHECKSUM_MISMATCH"
	errCodePrepareOfflinePolicyBlock    = "E_PREPARE_OFFLINE_POLICY_BLOCK"
	errCodePrepareConditionEval         = "E_CONDITION_EVAL"
	errCodePrepareRegisterMissing       = "E_REGISTER_OUTPUT_NOT_FOUND"
	errCodePrepareInspectionCheckFailed = "E_PREPARE_CHECKHOST_FAILED"
	errCodePrepareCheckHostFailed       = errCodePrepareInspectionCheckFailed
	errCodePrepareKindUnsupported       = "E_PREPARE_KIND_UNSUPPORTED"
	packageCacheMetaFile                = ".deck-cache-packages.json"
)

func Run(ctx context.Context, wf *config.Workflow, opts RunOptions) error {
	if wf == nil {
		return fmt.Errorf("workflow is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	bundleRoot := strings.TrimSpace(opts.BundleRoot)
	if bundleRoot == "" {
		bundleRoot = "./bundle"
	}

	if err := os.MkdirAll(bundleRoot, 0o755); err != nil {
		return fmt.Errorf("create bundle root: %w", err)
	}

	runner := opts.CommandRunner
	if runner == nil {
		runner = osCommandRunner{}
	}

	runtimeVars := map[string]any{}
	entries := make([]manifestEntry, 0)
	prepareSteps, err := prepareExecutionSteps(wf)
	if err != nil {
		return err
	}
	packCacheEnabled := strings.TrimSpace(wf.Role) == "prepare"
	packCacheStatePath := ""
	packCachePlan := PackCachePlan{}
	if packCacheEnabled {
		workflowSHA := strings.TrimSpace(wf.WorkflowSHA256)
		if workflowSHA == "" {
			fallbackBytes, err := json.Marshal(wf)
			if err != nil {
				return fmt.Errorf("encode workflow for prepare cache: %w", err)
			}
			workflowSHA = computeWorkflowSHA256(fallbackBytes)
		}
		var err error
		packCacheStatePath, err = defaultPackCacheStatePath(workflowSHA)
		if err != nil {
			return fmt.Errorf("resolve prepare cache state path: %w", err)
		}
		prevPackCacheState, err := loadPackCacheState(packCacheStatePath)
		if err != nil {
			return err
		}
		workflowBytesForPlan, err := json.Marshal(wf)
		if err != nil {
			return fmt.Errorf("encode workflow for prepare cache plan: %w", err)
		}
		packCachePlan = ComputePackCachePlan(prevPackCacheState, workflowBytesForPlan, wf.Vars, prepareSteps)
		packCachePlan.WorkflowSHA256 = workflowSHA
	}
	ctxData := map[string]any{"bundleRoot": bundleRoot, "stateFile": ""}

	for _, step := range prepareSteps {
		ok, err := evaluateWhen(step.When, wf.Vars, runtimeVars, ctxData)
		if err != nil {
			return fmt.Errorf("step %s (%s): %w", step.ID, step.Kind, err)
		}
		if !ok {
			continue
		}

		attempts := step.Retry + 1
		if attempts < 1 {
			attempts = 1
		}

		var (
			stepFiles []string
			outputs   map[string]any
			execErr   error
		)
		for i := 0; i < attempts; i++ {
			rendered, renderErr := renderSpecWithContext(step.Spec, wf, runtimeVars, ctxData)
			if renderErr != nil {
				execErr = fmt.Errorf("render spec template: %w", renderErr)
				break
			}
			stepFiles, outputs, execErr = runPrepareStep(ctx, runner, bundleRoot, step.Kind, rendered, opts)
			if host, ok := outputs["host"]; ok {
				runtimeVars["host"] = host
			}
			if execErr == nil {
				execErr = applyRegister(step, outputs, runtimeVars)
			}
			if execErr == nil {
				break
			}
		}
		if execErr != nil {
			return fmt.Errorf("step %s (%s): %w", step.ID, step.Kind, execErr)
		}

		for _, f := range stepFiles {
			entry, err := fileManifestEntry(bundleRoot, f)
			if err != nil {
				return err
			}
			entries = append(entries, entry)
		}
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	manifestPath := filepath.Join(bundleRoot, ".deck", "manifest.json")
	if err := writeManifest(manifestPath, dedupeEntries(filterManifestEntries(entries))); err != nil {
		return err
	}
	if packCacheEnabled {
		if err := savePackCacheState(packCacheStatePath, packCacheStateFromPlan(packCachePlan)); err != nil {
			return err
		}
	}

	return nil
}

func prepareExecutionSteps(wf *config.Workflow) ([]config.Step, error) {
	if wf == nil {
		return nil, fmt.Errorf("workflow is nil")
	}
	if wf.Artifacts != nil && (len(wf.Artifacts.Files) > 0 || len(wf.Artifacts.Images) > 0 || len(wf.Artifacts.Packages) > 0) {
		return declaredPrepareSteps(wf)
	}
	preparePhase, found := findPhase(wf, "prepare")
	if !found {
		return nil, fmt.Errorf("prepare phase not found")
	}
	return preparePhase.Steps, nil
}

func applyRegister(step config.Step, outputs map[string]any, runtimeVars map[string]any) error {
	return workflowexec.ApplyRegister(step, outputs, runtimeVars, errCodePrepareRegisterMissing)
}

func evaluateWhen(expr string, vars map[string]any, runtime map[string]any, ctx map[string]any) (bool, error) {
	return workflowexec.EvaluateWhen(expr, vars, runtime, ctx, errCodePrepareConditionEval)
}

func EvaluateWhen(expr string, vars map[string]any, runtime map[string]any, ctx map[string]any) (bool, error) {
	return evaluateWhen(expr, vars, runtime, ctx)
}

func findPhase(wf *config.Workflow, name string) (config.Phase, bool) {
	return workflowexec.FindPhase(wf, name)
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

func stringSlice(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, it := range items {
		if s, ok := it.(string); ok && strings.TrimSpace(s) != "" {
			result = append(result, strings.TrimSpace(s))
		}
	}
	return result
}

func boolValue(v map[string]any, key string) bool {
	if v == nil {
		return false
	}
	raw, ok := v[key]
	if !ok {
		return false
	}
	b, ok := raw.(bool)
	if !ok {
		return false
	}
	return b
}

func fileManifestEntry(bundleRoot, rel string) (manifestEntry, error) {
	abs := filepath.Join(bundleRoot, rel)
	content, err := os.ReadFile(abs)
	if err != nil {
		return manifestEntry{}, fmt.Errorf("read artifact for manifest: %w", err)
	}

	h := sha256.Sum256(content)
	fi, err := os.Stat(abs)
	if err != nil {
		return manifestEntry{}, fmt.Errorf("stat artifact for manifest: %w", err)
	}

	return manifestEntry{
		Path:   filepath.ToSlash(rel),
		SHA256: hex.EncodeToString(h[:]),
		Size:   fi.Size(),
	}, nil
}

func writeManifest(path string, entries []manifestEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create manifest directory: %w", err)
	}

	payload, err := json.MarshalIndent(manifestFile{Entries: entries}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}

	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}

func dedupeEntries(entries []manifestEntry) []manifestEntry {
	seen := map[string]manifestEntry{}
	for _, e := range entries {
		seen[e.Path] = e
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]manifestEntry, 0, len(keys))
	for _, k := range keys {
		out = append(out, seen[k])
	}
	return out
}

func filterManifestEntries(entries []manifestEntry) []manifestEntry {
	filtered := make([]manifestEntry, 0, len(entries))
	for _, e := range entries {
		if isManifestTrackedPath(e.Path) {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

func isManifestTrackedPath(rel string) bool {
	normalized := filepath.ToSlash(filepath.Clean(filepath.FromSlash(strings.TrimSpace(rel))))
	if normalized == "." {
		return false
	}
	return strings.HasPrefix(normalized, "packages/") || strings.HasPrefix(normalized, "images/") || strings.HasPrefix(normalized, "files/")
}

func renderSpec(spec map[string]any, wf *config.Workflow, runtimeVars map[string]any) (map[string]any, error) {
	return renderSpecWithContext(spec, wf, runtimeVars, map[string]any{"bundleRoot": "", "stateFile": ""})
}

func renderSpecWithContext(spec map[string]any, wf *config.Workflow, runtimeVars map[string]any, ctxData map[string]any) (map[string]any, error) {
	return workflowexec.RenderSpec(spec, wf, runtimeVars, ctxData)
}

func listRelativeFiles(root string) ([]string, error) {
	results := []string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		results = append(results, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(results)
	return results, nil
}
