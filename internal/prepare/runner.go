package prepare

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/Airgap-Castaways/deck/internal/config"
	"github.com/Airgap-Castaways/deck/internal/executil"
	"github.com/Airgap-Castaways/deck/internal/filemode"
	"github.com/Airgap-Castaways/deck/internal/fsutil"
	"github.com/Airgap-Castaways/deck/internal/workflowexec"
	"github.com/Airgap-Castaways/deck/internal/workspacepaths"
)

type RunOptions struct {
	BundleRoot       string
	CommandRunner    CommandRunner
	ForceRedownload  bool
	imageDownloadOps imageDownloadOps
	checksRuntime    checksRuntime
}

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) error
	RunWithIO(ctx context.Context, stdout io.Writer, stderr io.Writer, name string, args ...string) error
	LookPath(file string) (string, error)
}

type osCommandRunner struct{}

func (o osCommandRunner) Run(ctx context.Context, name string, args ...string) error {
	return executil.RunWorkflowCommandWithIO(ctx, os.Stdout, os.Stderr, name, args...)
}

func (o osCommandRunner) RunWithIO(ctx context.Context, stdout io.Writer, stderr io.Writer, name string, args ...string) error {
	return executil.RunWorkflowCommandWithIO(ctx, stdout, stderr, name, args...)
}

func (o osCommandRunner) LookPath(file string) (string, error) {
	return executil.LookPathWorkflowBinary(file)
}

const (
	errCodePrepareRuntimeMissing     = "E_PREPARE_RUNTIME_NOT_FOUND"
	errCodePrepareRuntimeUnsupported = "E_PREPARE_RUNTIME_UNSUPPORTED"
	errCodePrepareEngineUnsupported  = "E_PREPARE_ENGINE_UNSUPPORTED"
	errCodePrepareArtifactEmpty      = "E_PREPARE_NO_ARTIFACTS"
	errCodeArtifactSourceNotFound    = "E_PREPARE_SOURCE_NOT_FOUND"
	errCodePrepareChecksumMismatch   = "E_PREPARE_CHECKSUM_MISMATCH"
	errCodePrepareOfflinePolicyBlock = "E_PREPARE_OFFLINE_POLICY_BLOCK"
	errCodePrepareConditionEval      = "E_CONDITION_EVAL"
	errCodePrepareRegisterMissing    = "E_REGISTER_OUTPUT_NOT_FOUND"
	errCodePrepareCheckHostFailed    = "E_PREPARE_CHECKHOST_FAILED"
	errCodePrepareKindUnsupported    = "E_PREPARE_KIND_UNSUPPORTED"
	packageCacheMetaFile             = ".deck-cache-packages.json"
)

func Run(ctx context.Context, wf *config.Workflow, opts RunOptions) error {
	if wf == nil {
		return fmt.Errorf("workflow is nil")
	}
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}

	bundleRoot := strings.TrimSpace(opts.BundleRoot)
	if bundleRoot == "" {
		bundleRoot = "./bundle"
	}

	if err := filemode.EnsureDir(bundleRoot, filemode.PublishedArtifact); err != nil {
		return fmt.Errorf("create bundle root: %w", err)
	}

	runner := opts.CommandRunner
	if runner == nil {
		runner = osCommandRunner{}
	}

	phases, prepareSteps, err := prepareExecutionPlan(wf)
	if err != nil {
		return err
	}
	resolvedChecksRuntime := resolveCheckHostRuntime(opts)
	runtimeVars := map[string]any{"host": detectHostFactsForRuntime(resolvedChecksRuntime)}
	entries := make([]manifestEntry, 0)
	packCacheEnabled := true
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

	for _, phase := range phases {
		for _, batch := range workflowexec.BuildPhaseBatches(phase) {
			batchFiles, err := executePrepareBatch(ctx, runner, bundleRoot, wf, runtimeVars, ctxData, batch, opts)
			if err != nil {
				return err
			}
			for _, f := range batchFiles {
				entry, err := fileManifestEntry(bundleRoot, f)
				if err != nil {
					return err
				}
				entries = append(entries, entry)
			}
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

func prepareExecutionPlan(wf *config.Workflow) ([]config.Phase, []config.Step, error) {
	if wf == nil {
		return nil, nil, fmt.Errorf("workflow is nil")
	}
	phases := config.NormalizedPhases(wf)
	if len(phases) == 0 {
		return nil, nil, fmt.Errorf("prepare workflow has no steps")
	}
	steps := make([]config.Step, 0)
	for _, phase := range phases {
		steps = append(steps, phase.Steps...)
	}
	if len(steps) == 0 {
		return nil, nil, fmt.Errorf("prepare workflow has no steps")
	}
	return phases, steps, nil
}

type prepareBatchResult struct {
	files   []string
	outputs map[string]any
}

func executePrepareBatch(ctx context.Context, runner CommandRunner, bundleRoot string, wf *config.Workflow, runtimeVars map[string]any, ctxData map[string]any, batch workflowexec.StepBatch, opts RunOptions) ([]string, error) {
	if len(batch.Steps) == 0 {
		return nil, nil
	}
	snapshot := cloneRuntimeVars(runtimeVars)
	results := make([]prepareBatchResult, len(batch.Steps))
	if !batch.Parallel() {
		result, err := executePrepareStep(ctx, runner, bundleRoot, wf, snapshot, ctxData, batch.Steps[0], opts)
		if err != nil {
			return nil, err
		}
		results[0] = result
	} else {
		group, groupCtx := errgroup.WithContext(ctx)
		limit := batch.MaxParallelism
		if limit <= 0 || limit > len(batch.Steps) {
			limit = len(batch.Steps)
		}
		group.SetLimit(limit)
		for i, step := range batch.Steps {
			i := i
			step := step
			group.Go(func() error {
				result, err := executePrepareStep(groupCtx, runner, bundleRoot, wf, snapshot, ctxData, step, opts)
				if err != nil {
					return err
				}
				results[i] = result
				return nil
			})
		}
		if err := group.Wait(); err != nil {
			return nil, err
		}
	}
	files := make([]string, 0)
	for i, step := range batch.Steps {
		result := results[i]
		if err := applyRegister(step, result.outputs, runtimeVars); err != nil {
			return nil, fmt.Errorf("step %s (%s): %w", step.ID, step.Kind, err)
		}
		files = append(files, result.files...)
	}
	return files, nil
}

func executePrepareStep(ctx context.Context, runner CommandRunner, bundleRoot string, wf *config.Workflow, runtimeSnapshot map[string]any, ctxData map[string]any, step config.Step, opts RunOptions) (prepareBatchResult, error) {
	ok, err := evaluateWhen(step.When, wf.Vars, runtimeSnapshot)
	if err != nil {
		return prepareBatchResult{}, fmt.Errorf("step %s (%s): %w", step.ID, step.Kind, err)
	}
	if !ok {
		return prepareBatchResult{outputs: map[string]any{}}, nil
	}
	attempts := step.Retry + 1
	if attempts < 1 {
		attempts = 1
	}
	var execErr error
	for i := 0; i < attempts; i++ {
		rendered, renderErr := renderSpecWithContext(step.Spec, wf, runtimeSnapshot, ctxData)
		if renderErr != nil {
			execErr = fmt.Errorf("render spec template: %w", renderErr)
			break
		}
		inputVars := collectStepInputVarValues(step.Spec, wf.Vars)
		key, keyErr := workflowexec.ResolveStepTypeKey(wf.Version, step.APIVersion, step.Kind)
		if keyErr != nil {
			execErr = keyErr
			continue
		}
		stepFiles, outputs, stepErr := runPrepareRenderedStepWithKey(ctx, runner, bundleRoot, step, rendered, key, inputVars, opts)
		if stepErr == nil {
			return prepareBatchResult{files: stepFiles, outputs: outputs}, nil
		}
		execErr = stepErr
	}
	return prepareBatchResult{}, fmt.Errorf("step %s (%s): %w", step.ID, step.Kind, execErr)
}

func cloneRuntimeVars(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func applyRegister(step config.Step, outputs map[string]any, runtimeVars map[string]any) error {
	return workflowexec.ApplyRegister(step, outputs, runtimeVars, errCodePrepareRegisterMissing)
}

func evaluateWhen(expr string, vars map[string]any, runtime map[string]any) (bool, error) {
	return workflowexec.EvaluateWhen(expr, vars, runtime, errCodePrepareConditionEval)
}

func EvaluateWhen(expr string, vars map[string]any, runtime map[string]any) (bool, error) {
	return evaluateWhen(expr, vars, runtime)
}

func fileManifestEntry(bundleRoot, rel string) (manifestEntry, error) {
	abs := filepath.Join(bundleRoot, rel)
	content, err := fsutil.ReadFile(abs)
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
	if err := filemode.EnsureParentPrivateDir(path); err != nil {
		return fmt.Errorf("create manifest directory: %w", err)
	}

	payload, err := json.MarshalIndent(manifestFile{Entries: entries}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}

	if err := filemode.WritePrivateFile(path, payload); err != nil {
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
	return workspacepaths.IsCanonicalPreparedPath(normalized)
}

func renderSpec(spec map[string]any, wf *config.Workflow, runtimeVars map[string]any) (map[string]any, error) {
	return renderSpecWithContext(spec, wf, runtimeVars, map[string]any{"bundleRoot": "", "stateFile": ""})
}

func renderSpecWithContext(spec map[string]any, wf *config.Workflow, runtimeVars map[string]any, ctxData map[string]any) (map[string]any, error) {
	return workflowexec.RenderSpec(spec, wf, runtimeVars, ctxData)
}
