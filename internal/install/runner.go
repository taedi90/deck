package install

import (
	"context"
	"fmt"
	"strings"

	"github.com/taedi90/deck/internal/bundle"
	"github.com/taedi90/deck/internal/config"
	"github.com/taedi90/deck/internal/workflowexec"
)

type RunOptions struct {
	BundleRoot string
	StatePath  string
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
	errCodeInstallInstallFilePath  = "E_INSTALL_INSTALLFILE_PATH_REQUIRED"
	errCodeInstallInstallFileInput = "E_INSTALL_INSTALLFILE_CONTENT_REQUIRED"
	errCodeInstallRepoConfigPath   = "E_INSTALL_REPOCONFIG_PATH_REQUIRED"
	errCodeInstallKernelModuleMiss = "E_INSTALL_KERNELMODULE_NAME_REQUIRED"
	errCodeInstallTemplatePathMiss = "E_INSTALL_TEMPLATEFILE_PATH_REQUIRED"
	errCodeInstallTemplateBodyMiss = "E_INSTALL_TEMPLATEFILE_TEMPLATE_REQUIRED"
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
	errCodeInstallJoinCmdInvalid   = "E_INSTALL_KUBEADM_JOIN_COMMAND_INVALID"
	errCodeInstallJoinCmdMissing   = "E_INSTALL_KUBEADM_JOIN_COMMAND_MISSING"
	errCodeInstallSourceNotFound   = "E_INSTALL_SOURCE_NOT_FOUND"
	errCodeInstallChecksumMismatch = "E_INSTALL_CHECKSUM_MISMATCH"
	errCodeInstallOfflineBlocked   = "E_INSTALL_OFFLINE_POLICY_BLOCK"
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

	installPhase, found := workflowexec.FindPhase(wf, "install")
	if !found {
		return fmt.Errorf("install phase not found")
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

	st, err := LoadState(statePath)
	if err != nil {
		return err
	}
	st.Phase = "install"

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
	for _, step := range installPhase.Steps {
		if completed[step.ID] {
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
			rendered, renderErr := workflowexec.RenderSpec(step.Spec, wf, runtimeVars, ctxData)
			if renderErr != nil {
				execErr = fmt.Errorf("render spec template: %w", renderErr)
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
