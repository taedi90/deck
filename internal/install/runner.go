package install

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/taedi90/deck/internal/bundle"
	"github.com/taedi90/deck/internal/config"
	"github.com/taedi90/deck/internal/workflowexec"
)

type RunOptions struct {
	BundleRoot string
	StatePath  string
	EventSink  StepEventSink
}

const (
	errCodeInstallKindUnsupported  = "E_INSTALL_KIND_UNSUPPORTED"
	errCodeInstallPackagesRequired = "E_INSTALL_PACKAGES_REQUIRED"
	errCodeInstallPkgMgrMissing    = "E_INSTALL_PACKAGES_MANAGER_NOT_FOUND"
	errCodeInstallPkgSourceInvalid = "E_INSTALL_PACKAGES_SOURCE_INVALID"
	errCodeInstallPkgFailed        = "E_INSTALL_PACKAGES_INSTALL_FAILED"
	errCodeInstallWritePathMissing = "E_INSTALL_WRITEFILE_PATH_REQUIRED"
	errCodeInstallEditPathMissing  = "E_INSTALL_EDITFILE_PATH_REQUIRED"
	errCodeInstallEditsMissing     = "E_INSTALL_EDITFILE_EDITS_REQUIRED"
	errCodeInstallCopyPathMissing  = "E_INSTALL_COPYFILE_PATH_REQUIRED"
	errCodeInstallSysctlPathMiss   = "E_INSTALL_SYSCTL_PATH_REQUIRED"
	errCodeInstallSysctlValsMiss   = "E_INSTALL_SYSCTL_VALUES_REQUIRED"
	errCodeInstallModulesMissing   = "E_INSTALL_MODPROBE_MODULES_REQUIRED"
	errCodeInstallServiceNameMiss  = "E_INSTALL_SERVICE_NAME_REQUIRED"
	errCodeInstallEnsureDirPathMis = "E_INSTALL_ENSUREDIR_PATH_REQUIRED"
	errCodeInstallSymlinkPathMiss  = "E_INSTALL_SYMLINK_PATH_REQUIRED"
	errCodeInstallSymlinkTargetMis = "E_INSTALL_SYMLINK_TARGET_REQUIRED"
	errCodeInstallInstallFilePath  = "E_INSTALL_INSTALLFILE_PATH_REQUIRED"
	errCodeInstallInstallFileInput = "E_INSTALL_INSTALLFILE_CONTENT_REQUIRED"
	errCodeInstallRepoConfigPath   = "E_INSTALL_REPOCONFIG_PATH_REQUIRED"
	errCodeInstallPackageCacheMgr  = "E_INSTALL_PACKAGECACHE_MANAGER_INVALID"
	errCodeInstallKernelModuleMiss = "E_INSTALL_KERNELMODULE_NAME_REQUIRED"
	errCodeInstallTemplatePathMiss = "E_INSTALL_TEMPLATEFILE_PATH_REQUIRED"
	errCodeInstallTemplateBodyMiss = "E_INSTALL_TEMPLATEFILE_TEMPLATE_REQUIRED"
	errCodeInstallSystemdUnitPath  = "E_INSTALL_SYSTEMD_UNIT_PATH_REQUIRED"
	errCodeInstallSystemdUnitInput = "E_INSTALL_SYSTEMD_UNIT_CONTENT_REQUIRED"
	errCodeInstallSystemdUnitBoth  = "E_INSTALL_SYSTEMD_UNIT_CONTENT_CONFLICT"
	errCodeInstallSystemdUnitSvc   = "E_INSTALL_SYSTEMD_UNIT_SERVICE_NAME_REQUIRED"
	errCodeInstallCommandMissing   = "E_INSTALL_RUNCOMMAND_REQUIRED"
	errCodeInstallCommandTimeout   = "E_INSTALL_RUNCOMMAND_TIMEOUT"
	errCodeInstallCommandFailed    = "E_INSTALL_RUNCOMMAND_FAILED"
	errCodeInstallImagesMissing    = "E_INSTALL_VERIFY_IMAGES_REQUIRED"
	errCodeInstallImagesCmdFailed  = "E_INSTALL_VERIFY_IMAGES_COMMAND_FAILED"
	errCodeInstallImagesNotFound   = "E_INSTALL_VERIFY_IMAGES_NOT_FOUND"
	errCodeInstallInitJoinMissing  = "E_INSTALL_KUBEADM_INIT_JOINFILE_REQUIRED"
	errCodeInstallJoinPathMissing  = "E_INSTALL_KUBEADM_JOIN_JOINFILE_REQUIRED"
	errCodeInstallJoinFileMissing  = "E_INSTALL_KUBEADM_JOIN_FILE_NOT_FOUND"
	errCodeInstallInitModeInvalid  = "E_INSTALL_KUBEADM_INIT_MODE_INVALID"
	errCodeInstallJoinModeInvalid  = "E_INSTALL_KUBEADM_JOIN_MODE_INVALID"
	errCodeInstallInitFailed       = "E_INSTALL_KUBEADM_INIT_FAILED"
	errCodeInstallJoinFailed       = "E_INSTALL_KUBEADM_JOIN_FAILED"
	errCodeInstallResetFailed      = "E_INSTALL_KUBEADM_RESET_FAILED"
	errCodeInstallJoinCmdInvalid   = "E_INSTALL_KUBEADM_JOIN_COMMAND_INVALID"
	errCodeInstallJoinCmdMissing   = "E_INSTALL_KUBEADM_JOIN_COMMAND_MISSING"
	errCodeInstallWaitTimeout      = "E_INSTALL_WAIT_TIMEOUT"
	errCodeInstallWaitPathRequired = "E_INSTALL_WAITPATH_PATH_REQUIRED"
	errCodeInstallWaitPathState    = "E_INSTALL_WAITPATH_STATE_INVALID"
	errCodeInstallWaitPathType     = "E_INSTALL_WAITPATH_TYPE_INVALID"
	errCodeInstallWaitPathPoll     = "E_INSTALL_WAITPATH_POLL_INTERVAL_INVALID"
	errCodeInstallWaitPathTimeout  = "E_INSTALL_WAITPATH_TIMEOUT"
	errCodeInstallSourceNotFound   = "E_INSTALL_SOURCE_NOT_FOUND"
	errCodeInstallChecksumMismatch = "E_INSTALL_CHECKSUM_MISMATCH"
	errCodeInstallOfflineBlocked   = "E_INSTALL_OFFLINE_POLICY_BLOCK"
	errCodeInstallArtifactsMissing = "E_INSTALL_ARTIFACTS_REQUIRED"
	errCodeInstallArtifactArch     = "E_INSTALL_ARTIFACT_ARCH_UNSUPPORTED"
	errCodeInstallArtifactSource   = "E_INSTALL_ARTIFACT_SOURCE_INVALID"
	errCodeConditionEval           = "E_CONDITION_EVAL"
	errCodeRegisterOutputMissing   = "E_REGISTER_OUTPUT_NOT_FOUND"
)

func Run(ctx context.Context, wf *config.Workflow, opts RunOptions) error {
	if wf == nil {
		return fmt.Errorf("workflow is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	if len(wf.Phases) == 0 {
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
	stateReadPath, err := resolveStateReadPath(wf, statePath)
	if err != nil {
		return err
	}

	st, err := LoadState(stateReadPath)
	if err != nil {
		return err
	}
	completed := make(map[string]bool, len(st.CompletedSteps))
	for _, id := range st.CompletedSteps {
		completed[id] = true
	}

	runtimeVars := map[string]any{}
	for k, v := range st.RuntimeVars {
		runtimeVars[k] = v
	}
	runtimeVars["host"] = detectHostFacts()
	skipped := make(map[string]bool, len(st.SkippedSteps))
	for _, id := range st.SkippedSteps {
		skipped[id] = true
	}

	ctxData := map[string]any{"bundleRoot": bundleRoot, "stateFile": statePath}
	for _, phase := range wf.Phases {
		st.Phase = phase.Name
		for _, step := range phase.Steps {
			if completed[step.ID] {
				emitStepEvent(opts.EventSink, StepEvent{StepID: step.ID, Kind: step.Kind, Phase: phase.Name, Status: "skipped", Reason: "completed"})
				continue
			}

			ok, err := evaluateWhen(step.When, wf.Vars, runtimeVars, ctxData)
			if err != nil {
				st.FailedStep = step.ID
				st.Error = err.Error()
				st.RuntimeVars = runtimeVars
				st.SkippedSteps = sortedStepIDs(skipped)
				_ = SaveState(statePath, st)
				return fmt.Errorf("step %s (%s): %w", step.ID, step.Kind, err)
			}
			if !ok {
				skipped[step.ID] = true
				emitStepEvent(opts.EventSink, StepEvent{StepID: step.ID, Kind: step.Kind, Phase: phase.Name, Status: "skipped", Reason: "when"})
				st.RuntimeVars = runtimeVars
				st.SkippedSteps = sortedStepIDs(skipped)
				if err := SaveState(statePath, st); err != nil {
					return err
				}
				continue
			}

			var execErr error
			attempts := step.Retry + 1
			if attempts < 1 {
				attempts = 1
			}
			for i := 0; i < attempts; i++ {
				if err := ctx.Err(); err != nil {
					execErr = err
					break
				}
				startedAt := time.Now().UTC().Format(time.RFC3339Nano)
				emitStepEvent(opts.EventSink, StepEvent{StepID: step.ID, Kind: step.Kind, Phase: phase.Name, Status: "started", Attempt: i + 1, StartedAt: startedAt})
				rendered, renderErr := workflowexec.RenderSpec(step.Spec, wf, runtimeVars, ctxData)
				if renderErr != nil {
					execErr = fmt.Errorf("render spec template: %w", renderErr)
					emitStepEvent(opts.EventSink, StepEvent{StepID: step.ID, Kind: step.Kind, Phase: phase.Name, Status: "failed", Attempt: i + 1, StartedAt: startedAt, EndedAt: time.Now().UTC().Format(time.RFC3339Nano), Error: execErr.Error()})
					break
				}
				if strings.TrimSpace(step.Timeout) != "" {
					if _, exists := rendered["timeout"]; !exists {
						rendered["timeout"] = strings.TrimSpace(step.Timeout)
					}
				}
				execErr = executeStep(ctx, step.Kind, rendered, bundleRoot)
				if execErr == nil {
					if err := applyRegister(step, rendered, runtimeVars); err != nil {
						execErr = err
					}
				}
				endedAt := time.Now().UTC().Format(time.RFC3339Nano)
				if execErr != nil {
					emitStepEvent(opts.EventSink, StepEvent{StepID: step.ID, Kind: step.Kind, Phase: phase.Name, Status: "failed", Attempt: i + 1, StartedAt: startedAt, EndedAt: endedAt, Error: execErr.Error()})
				} else {
					emitStepEvent(opts.EventSink, StepEvent{StepID: step.ID, Kind: step.Kind, Phase: phase.Name, Status: "succeeded", Attempt: i + 1, StartedAt: startedAt, EndedAt: endedAt})
				}
				if execErr == nil {
					break
				}
				if ctx.Err() != nil {
					break
				}
			}

			if execErr != nil {
				st.FailedStep = step.ID
				st.Error = execErr.Error()
				st.RuntimeVars = runtimeVars
				st.SkippedSteps = sortedStepIDs(skipped)
				_ = SaveState(statePath, st)
				return fmt.Errorf("step %s (%s): %w", step.ID, step.Kind, execErr)
			}

			st.CompletedSteps = append(st.CompletedSteps, step.ID)
			completed[step.ID] = true
			delete(skipped, step.ID)
			st.FailedStep = ""
			st.Error = ""
			st.RuntimeVars = runtimeVars
			st.SkippedSteps = sortedStepIDs(skipped)
			if err := SaveState(statePath, st); err != nil {
				return err
			}
		}
	}

	st.Phase = "completed"
	st.FailedStep = ""
	st.Error = ""
	st.RuntimeVars = runtimeVars
	st.SkippedSteps = sortedStepIDs(skipped)
	if err := SaveState(statePath, st); err != nil {
		return err
	}

	return nil
}

func verifyBundleManifest(bundleRoot string) error {
	return bundle.VerifyManifest(bundleRoot)
}

func sortedStepIDs(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	items := make([]string, 0, len(m))
	for k := range m {
		items = append(items, k)
	}
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j] < items[i] {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
	return items
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
