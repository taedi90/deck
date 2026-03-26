package install

import (
	"context"
	"fmt"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/Airgap-Castaways/deck/internal/bundle"
	"github.com/Airgap-Castaways/deck/internal/config"
	"github.com/Airgap-Castaways/deck/internal/workflowexec"
)

type RunOptions struct {
	BundleRoot string
	StatePath  string
	EventSink  StepEventSink
	Fresh      bool
}

const (
	errCodeInstallKindUnsupported        = "E_INSTALL_KIND_UNSUPPORTED"
	errCodeInstallPackagesRequired       = "E_INSTALL_PACKAGES_REQUIRED"
	errCodeInstallPkgMgrMissing          = "E_INSTALL_PACKAGES_MANAGER_NOT_FOUND"
	errCodeInstallPkgSourceInvalid       = "E_INSTALL_PACKAGES_SOURCE_INVALID"
	errCodeInstallPkgFailed              = "E_INSTALL_PACKAGES_INSTALL_FAILED"
	errCodeInstallWritePathMissing       = "E_INSTALL_WRITEFILE_PATH_REQUIRED"
	errCodeInstallEditPathMissing        = "E_INSTALL_EDITFILE_PATH_REQUIRED"
	errCodeInstallEditsMissing           = "E_INSTALL_EDITFILE_EDITS_REQUIRED"
	errCodeInstallCopyPathMissing        = "E_INSTALL_COPYFILE_PATH_REQUIRED"
	errCodeInstallSysctlPathMiss         = "E_INSTALL_SYSCTL_PATH_REQUIRED"
	errCodeInstallSysctlValsMiss         = "E_INSTALL_SYSCTL_VALUES_REQUIRED"
	errCodeInstallModulesMissing         = "E_INSTALL_MODPROBE_MODULES_REQUIRED"
	errCodeInstallManageServiceNameMiss  = "E_INSTALL_SERVICE_NAME_REQUIRED"
	errCodeInstallEnsureDirPathMis       = "E_INSTALL_ENSUREDIR_PATH_REQUIRED"
	errCodeInstallCreateSymlinkPathMiss  = "E_INSTALL_SYMLINK_PATH_REQUIRED"
	errCodeInstallCreateSymlinkTargetMis = "E_INSTALL_SYMLINK_TARGET_REQUIRED"
	errCodeInstallInstallFilePath        = "E_INSTALL_INSTALLFILE_PATH_REQUIRED"
	errCodeInstallInstallFileInput       = "E_INSTALL_INSTALLFILE_CONTENT_REQUIRED"
	errCodeInstallRepoConfigPath         = "E_INSTALL_REPOCONFIG_PATH_REQUIRED"
	errCodeInstallRefreshRepositoryMgr   = "E_INSTALL_PACKAGECACHE_MANAGER_INVALID"
	errCodeInstallKernelModuleMiss       = "E_INSTALL_KERNELMODULE_NAME_REQUIRED"
	errCodeInstallTemplatePathMiss       = "E_INSTALL_TEMPLATEFILE_PATH_REQUIRED"
	errCodeInstallTemplateBodyMiss       = "E_INSTALL_TEMPLATEFILE_TEMPLATE_REQUIRED"
	errCodeInstallWriteSystemdUnitPath   = "E_INSTALL_SYSTEMD_UNIT_PATH_REQUIRED"
	errCodeInstallWriteSystemdUnitInput  = "E_INSTALL_SYSTEMD_UNIT_CONTENT_REQUIRED"
	errCodeInstallWriteSystemdUnitBoth   = "E_INSTALL_SYSTEMD_UNIT_CONTENT_CONFLICT"
	errCodeInstallWriteSystemdUnitSvc    = "E_INSTALL_SYSTEMD_UNIT_SERVICE_NAME_REQUIRED"
	errCodeInstallCommandMissing         = "E_INSTALL_RUNCOMMAND_REQUIRED"
	errCodeInstallCommandTimeout         = "E_INSTALL_RUNCOMMAND_TIMEOUT"
	errCodeInstallCommandFailed          = "E_INSTALL_RUNCOMMAND_FAILED"
	errCodeInstallImagesMissing          = "E_INSTALL_VERIFY_IMAGES_REQUIRED"
	errCodeInstallImagesCmdFailed        = "E_INSTALL_VERIFY_IMAGES_COMMAND_FAILED"
	errCodeInstallImagesNotFound         = "E_INSTALL_VERIFY_IMAGES_NOT_FOUND"
	errCodeInstallInitJoinMissing        = "E_INSTALL_KUBEADM_INIT_JOINFILE_REQUIRED"
	errCodeInstallJoinPathMissing        = "E_INSTALL_KUBEADM_JOIN_JOINFILE_REQUIRED"
	errCodeInstallJoinFileMissing        = "E_INSTALL_KUBEADM_JOIN_FILE_NOT_FOUND"
	errCodeInstallJoinInputConflict      = "E_INSTALL_KUBEADM_JOIN_INPUT_CONFLICT"
	errCodeInstallInitModeInvalid        = "E_INSTALL_KUBEADM_INIT_MODE_INVALID"
	errCodeInstallJoinModeInvalid        = "E_INSTALL_KUBEADM_JOIN_MODE_INVALID"
	errCodeInstallInitFailed             = "E_INSTALL_KUBEADM_INIT_FAILED"
	errCodeInstallJoinFailed             = "E_INSTALL_KUBEADM_JOIN_FAILED"
	errCodeInstallResetFailed            = "E_INSTALL_KUBEADM_RESET_FAILED"
	errCodeInstallUpgradeFailed          = "E_INSTALL_KUBEADM_UPGRADE_FAILED"
	errCodeInstallJoinCmdInvalid         = "E_INSTALL_KUBEADM_JOIN_COMMAND_INVALID"
	errCodeInstallJoinCmdMissing         = "E_INSTALL_KUBEADM_JOIN_COMMAND_MISSING"
	errCodeInstallClusterCheckFailed     = "E_INSTALL_CLUSTER_CHECK_FAILED"
	errCodeInstallCheckHostFailed        = "E_INSTALL_CHECKHOST_FAILED"
	errCodeInstallWaitTimeout            = "E_INSTALL_WAIT_TIMEOUT"
	errCodeInstallWaitPathRequired       = "E_INSTALL_WAITPATH_PATH_REQUIRED"
	errCodeInstallWaitPathState          = "E_INSTALL_WAITPATH_STATE_INVALID"
	errCodeInstallWaitPathType           = "E_INSTALL_WAITPATH_TYPE_INVALID"
	errCodeInstallWaitPathPoll           = "E_INSTALL_WAITPATH_POLL_INTERVAL_INVALID"
	errCodeInstallWaitPathTimeout        = "E_INSTALL_WAITPATH_TIMEOUT"
	errCodeInstallSourceNotFound         = "E_INSTALL_SOURCE_NOT_FOUND"
	errCodeInstallChecksumMismatch       = "E_INSTALL_CHECKSUM_MISMATCH"
	errCodeInstallOfflineBlocked         = "E_INSTALL_OFFLINE_POLICY_BLOCK"
	errCodeInstallArtifactMissing        = "E_INSTALL_ARTIFACTS_REQUIRED"
	errCodeInstallArtifactArch           = "E_INSTALL_ARTIFACT_ARCH_UNSUPPORTED"
	errCodeInstallArtifactSource         = "E_INSTALL_ARTIFACT_SOURCE_INVALID"
	errCodeConditionEval                 = "E_CONDITION_EVAL"
	errCodeRegisterOutputMissing         = "E_REGISTER_OUTPUT_NOT_FOUND"
)

func Run(ctx context.Context, wf *config.Workflow, opts RunOptions) error {
	if wf == nil {
		return fmt.Errorf("workflow is nil")
	}
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}

	phases := config.NormalizedPhases(wf)
	if len(phases) == 0 {
		return fmt.Errorf("no phases found")
	}

	bundleRoot := strings.TrimSpace(opts.BundleRoot)
	if bundleRoot != "" {
		if err := verifyBundleManifest(bundleRoot); err != nil {
			return err
		}
	}

	statePath := strings.TrimSpace(opts.StatePath)
	if statePath == "" {
		resolvedStatePath, err := DefaultStatePath(wf)
		if err != nil {
			return err
		}
		statePath = resolvedStatePath
	}
	st := &State{CompletedPhases: []string{}, RuntimeVars: map[string]any{}}
	if !opts.Fresh {
		stateReadPath, err := resolveStateReadPath(wf, statePath)
		if err != nil {
			return err
		}
		loadedState, err := LoadState(stateReadPath)
		if err != nil {
			return err
		}
		st = loadedState
	}
	completed := make(map[string]bool, len(st.CompletedPhases))
	for _, id := range st.CompletedPhases {
		completed[id] = true
	}

	runtimeVars := map[string]any{}
	for k, v := range st.RuntimeVars {
		runtimeVars[k] = v
	}
	runtimeVars["host"] = detectHostFacts()
	execCtx := ExecutionContext{BundleRoot: bundleRoot, StatePath: statePath}
	ctxData := execCtx.RenderContext()
	for _, phase := range phases {
		st.Phase = phase.Name
		if completed[phase.Name] {
			continue
		}
		for _, batch := range workflowexec.BuildPhaseBatches(phase) {
			if err := executeInstallBatch(ctx, wf, runtimeVars, ctxData, execCtx, batch, opts.EventSink); err != nil {
				st.FailedPhase = phase.Name
				st.Error = err.Error()
				st.RuntimeVars = runtimeVars
				_ = SaveState(statePath, st)
				return err
			}
		}
		st.CompletedPhases = append(st.CompletedPhases, phase.Name)
		completed[phase.Name] = true
		st.FailedPhase = ""
		st.Error = ""
		st.RuntimeVars = runtimeVars
		if err := SaveState(statePath, st); err != nil {
			return err
		}
	}

	st.Phase = "completed"
	st.FailedPhase = ""
	st.Error = ""
	st.RuntimeVars = runtimeVars
	if err := SaveState(statePath, st); err != nil {
		return err
	}

	return nil
}

func verifyBundleManifest(bundleRoot string) error {
	return bundle.VerifyManifest(bundleRoot)
}

type installBatchResult struct {
	rendered map[string]any
	outputs  map[string]any
	skipped  bool
}

func executeInstallBatch(ctx context.Context, wf *config.Workflow, runtimeVars map[string]any, ctxData map[string]any, execCtx ExecutionContext, batch workflowexec.StepBatch, sink StepEventSink) error {
	if len(batch.Steps) == 0 {
		return nil
	}
	snapshot := cloneRuntimeVars(runtimeVars)
	results := make([]installBatchResult, len(batch.Steps))
	if !batch.Parallel() {
		result, err := executeInstallStep(ctx, wf, snapshot, ctxData, execCtx, batch.PhaseName, batch.Steps[0], sink)
		if err != nil {
			return err
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
				result, err := executeInstallStep(groupCtx, wf, snapshot, ctxData, execCtx, batch.PhaseName, step, sink)
				if err != nil {
					return err
				}
				results[i] = result
				return nil
			})
		}
		if err := group.Wait(); err != nil {
			return err
		}
	}
	for i, step := range batch.Steps {
		if results[i].skipped {
			continue
		}
		if err := applyRegister(step, results[i].rendered, results[i].outputs, runtimeVars); err != nil {
			return fmt.Errorf("step %s (%s): %w", step.ID, step.Kind, err)
		}
	}
	return nil
}

func executeInstallStep(ctx context.Context, wf *config.Workflow, runtimeSnapshot map[string]any, ctxData map[string]any, execCtx ExecutionContext, phaseName string, step config.Step, sink StepEventSink) (installBatchResult, error) {
	ok, err := evaluateWhen(step.When, wf.Vars, runtimeSnapshot)
	if err != nil {
		return installBatchResult{}, fmt.Errorf("step %s (%s): %w", step.ID, step.Kind, err)
	}
	if !ok {
		emitStepEvent(sink, StepEvent{StepID: step.ID, Kind: step.Kind, Phase: phaseName, Status: "skipped", Reason: "when"})
		return installBatchResult{skipped: true}, nil
	}
	attempts := step.Retry + 1
	if attempts < 1 {
		attempts = 1
	}
	var execErr error
	for i := 0; i < attempts; i++ {
		if err := ctx.Err(); err != nil {
			execErr = err
			break
		}
		startedAt := time.Now().UTC().Format(time.RFC3339Nano)
		emitStepEvent(sink, StepEvent{StepID: step.ID, Kind: step.Kind, Phase: phaseName, Status: "started", Attempt: i + 1, StartedAt: startedAt})
		rendered, renderErr := workflowexec.RenderSpec(step.Spec, wf, runtimeSnapshot, ctxData)
		if renderErr != nil {
			execErr = fmt.Errorf("render spec template: %w", renderErr)
			emitStepEvent(sink, StepEvent{StepID: step.ID, Kind: step.Kind, Phase: phaseName, Status: "failed", Attempt: i + 1, StartedAt: startedAt, EndedAt: time.Now().UTC().Format(time.RFC3339Nano), Error: execErr.Error()})
			break
		}
		key, keyErr := workflowexec.ResolveStepTypeKey(wf.Version, step.APIVersion, step.Kind)
		if keyErr != nil {
			execErr = keyErr
		} else {
			var outputs map[string]any
			outputs, execErr = executeWorkflowStep(ctx, step, rendered, key, execCtx)
			if execErr == nil {
				endedAt := time.Now().UTC().Format(time.RFC3339Nano)
				emitStepEvent(sink, StepEvent{StepID: step.ID, Kind: step.Kind, Phase: phaseName, Status: "succeeded", Attempt: i + 1, StartedAt: startedAt, EndedAt: endedAt})
				return installBatchResult{rendered: rendered, outputs: outputs}, nil
			}
		}
		endedAt := time.Now().UTC().Format(time.RFC3339Nano)
		if execErr != nil {
			emitStepEvent(sink, StepEvent{StepID: step.ID, Kind: step.Kind, Phase: phaseName, Status: "failed", Attempt: i + 1, StartedAt: startedAt, EndedAt: endedAt, Error: execErr.Error()})
		}
		if ctx.Err() != nil {
			break
		}
	}
	return installBatchResult{}, fmt.Errorf("step %s (%s): %w", step.ID, step.Kind, execErr)
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

func renderSpec(spec map[string]any, wf *config.Workflow, runtimeVars map[string]any) (map[string]any, error) {
	return workflowexec.RenderSpec(spec, wf, runtimeVars, map[string]any{"bundleRoot": "", "stateFile": ""})
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
