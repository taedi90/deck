package install

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/taedi90/deck/internal/bundle"
	"github.com/taedi90/deck/internal/config"
)

type RunOptions struct {
	BundleRoot string
}

type State struct {
	Phase          string         `json:"phase"`
	CompletedSteps []string       `json:"completedSteps"`
	SkippedSteps   []string       `json:"skippedSteps,omitempty"`
	RuntimeVars    map[string]any `json:"runtimeVars,omitempty"`
	FailedStep     string         `json:"failedStep,omitempty"`
	Error          string         `json:"error,omitempty"`
}

var templateRefPattern = regexp.MustCompile(`\{\s*\.([A-Za-z0-9_\.]+)\s*\}`)

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
	errCodeConditionEval           = "E_CONDITION_EVAL"
	errCodeRegisterOutputMissing   = "E_REGISTER_OUTPUT_NOT_FOUND"
)

func Run(wf *config.Workflow, opts RunOptions) error {
	if wf == nil {
		return fmt.Errorf("workflow is nil")
	}

	installPhase, found := findPhase(wf, "install")
	if !found {
		return fmt.Errorf("install phase not found")
	}

	bundleRoot := strings.TrimSpace(opts.BundleRoot)
	if bundleRoot == "" {
		bundleRoot = strings.TrimSpace(wf.Context.BundleRoot)
	}
	if bundleRoot != "" {
		if err := verifyBundleManifest(bundleRoot); err != nil {
			return err
		}
	}

	statePath := strings.TrimSpace(wf.Context.StateFile)
	if statePath == "" {
		if bundleRoot == "" {
			bundleRoot = "."
		}
		statePath = filepath.Join(bundleRoot, ".deck", "state.json")
	}

	st, err := loadState(statePath)
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
	skipped := make(map[string]bool, len(st.SkippedSteps))
	for _, id := range st.SkippedSteps {
		skipped[id] = true
	}

	ctxData := map[string]any{"bundleRoot": wf.Context.BundleRoot, "stateFile": wf.Context.StateFile}
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
			_ = saveState(statePath, st)
			return fmt.Errorf("step %s (%s): %w", step.ID, step.Kind, err)
		}
		if !ok {
			skipped[step.ID] = true
			st.RuntimeVars = runtimeVars
			st.SkippedSteps = sortedStepIDs(skipped)
			if err := saveState(statePath, st); err != nil {
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
			rendered := renderSpec(step.Spec, wf, runtimeVars)
			execErr = executeStep(step.Kind, rendered)
			if execErr == nil {
				if err := applyRegister(step, rendered, runtimeVars); err != nil {
					execErr = err
				}
			}
			if execErr == nil {
				break
			}
		}

		if execErr != nil {
			st.FailedStep = step.ID
			st.Error = execErr.Error()
			st.RuntimeVars = runtimeVars
			st.SkippedSteps = sortedStepIDs(skipped)
			_ = saveState(statePath, st)
			return fmt.Errorf("step %s (%s): %w", step.ID, step.Kind, execErr)
		}

		st.CompletedSteps = append(st.CompletedSteps, step.ID)
		completed[step.ID] = true
		delete(skipped, step.ID)
		st.FailedStep = ""
		st.Error = ""
		st.RuntimeVars = runtimeVars
		st.SkippedSteps = sortedStepIDs(skipped)
		if err := saveState(statePath, st); err != nil {
			return err
		}
	}

	st.FailedStep = ""
	st.Error = ""
	st.RuntimeVars = runtimeVars
	st.SkippedSteps = sortedStepIDs(skipped)
	if err := saveState(statePath, st); err != nil {
		return err
	}

	return nil
}

func findPhase(wf *config.Workflow, name string) (config.Phase, bool) {
	for _, p := range wf.Phases {
		if p.Name == name {
			return p, true
		}
	}
	return config.Phase{}, false
}

func verifyBundleManifest(bundleRoot string) error {
	return bundle.VerifyManifest(bundleRoot)
}

func executeStep(kind string, spec map[string]any) error {
	switch kind {
	case "InstallPackages":
		return runInstallPackages(spec)
	case "WriteFile":
		return runWriteFile(spec)
	case "EditFile":
		return runEditFile(spec)
	case "CopyFile":
		return runCopyFile(spec)
	case "Sysctl":
		return runSysctl(spec)
	case "Modprobe":
		return runModprobe(spec)
	case "RunCommand":
		return runCommand(spec)
	case "VerifyImages":
		return runVerifyImages(spec)
	case "KubeadmInit":
		return runKubeadmInit(spec)
	case "KubeadmJoin":
		return runKubeadmJoin(spec)
	default:
		return fmt.Errorf("%s: unsupported step kind %s", errCodeInstallKindUnsupported, kind)
	}
}

func runInstallPackages(spec map[string]any) error {
	pkgs := stringSlice(spec["packages"])
	if len(pkgs) == 0 {
		return fmt.Errorf("%s: InstallPackages requires packages", errCodeInstallPackagesRequired)
	}

	sourcePath := ""

	if src, ok := spec["source"].(map[string]any); ok {
		typeVal := stringValue(src, "type")
		if typeVal != "" && typeVal != "local-repo" {
			return fmt.Errorf("%s: unsupported source type %q", errCodeInstallPkgSourceInvalid, typeVal)
		}
		if path := stringValue(src, "path"); path != "" {
			if info, err := os.Stat(path); err != nil || !info.IsDir() {
				return fmt.Errorf("%s: source path must be an existing directory: %s", errCodeInstallPkgSourceInvalid, path)
			}
			sourcePath = path
		}
	}

	installer := ""
	if _, err := exec.LookPath("apt-get"); err == nil {
		installer = "apt-get"
	} else if _, err := exec.LookPath("dnf"); err == nil {
		installer = "dnf"
	}
	if installer == "" {
		return fmt.Errorf("%s: apt-get or dnf not found", errCodeInstallPkgMgrMissing)
	}

	if sourcePath != "" {
		if installer == "apt-get" {
			artifacts, err := collectPackageArtifacts(sourcePath, ".deb")
			if err != nil {
				return fmt.Errorf("%s: %w", errCodeInstallPkgSourceInvalid, err)
			}
			args := []string{"install", "-y"}
			args = append(args, artifacts...)
			if err := runTimedCommand("apt-get", args, commandTimeoutWithDefault(spec, 10*time.Minute)); err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					return fmt.Errorf("%s: package installation timed out: %w", errCodeInstallPkgFailed, err)
				}
				return fmt.Errorf("%s: package installation failed: %w", errCodeInstallPkgFailed, err)
			}
			return nil
		}

		artifacts, err := collectPackageArtifacts(sourcePath, ".rpm")
		if err != nil {
			return fmt.Errorf("%s: %w", errCodeInstallPkgSourceInvalid, err)
		}
		args := []string{"install", "-y"}
		args = append(args, artifacts...)
		if err := runTimedCommand("dnf", args, commandTimeoutWithDefault(spec, 10*time.Minute)); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return fmt.Errorf("%s: package installation timed out: %w", errCodeInstallPkgFailed, err)
			}
			return fmt.Errorf("%s: package installation failed: %w", errCodeInstallPkgFailed, err)
		}
		return nil
	}

	args := []string{"install", "-y"}
	args = append(args, pkgs...)
	if err := runTimedCommand(installer, args, commandTimeoutWithDefault(spec, 10*time.Minute)); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("%s: package installation timed out: %w", errCodeInstallPkgFailed, err)
		}
		return fmt.Errorf("%s: package installation failed: %w", errCodeInstallPkgFailed, err)
	}
	return nil
}

func collectPackageArtifacts(root, ext string) ([]string, error) {
	artifacts := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(d.Name()), strings.ToLower(ext)) {
			artifacts = append(artifacts, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(artifacts) == 0 {
		return nil, fmt.Errorf("no %s artifacts found under %s", ext, root)
	}
	sort.Strings(artifacts)
	return artifacts, nil
}

func runWriteFile(spec map[string]any) error {
	path := stringValue(spec, "path")
	if path == "" {
		return fmt.Errorf("%s: WriteFile requires path", errCodeInstallWritePathMissing)
	}

	content := stringValue(spec, "content")
	if content == "" {
		if tmpl := stringValue(spec, "contentFromTemplate"); tmpl != "" {
			content = fmt.Sprintf("template:%s\n", tmpl)
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	return nil
}

func runEditFile(spec map[string]any) error {
	path := stringValue(spec, "path")
	if path == "" {
		return fmt.Errorf("%s: EditFile requires path", errCodeInstallEditPathMissing)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	updated := string(content)

	edits, ok := spec["edits"].([]any)
	if !ok || len(edits) == 0 {
		return fmt.Errorf("%s: EditFile requires edits", errCodeInstallEditsMissing)
	}

	for _, e := range edits {
		em, ok := e.(map[string]any)
		if !ok {
			continue
		}
		match := stringValue(em, "match")
		with := stringValue(em, "with")
		if match == "" {
			continue
		}
		updated = strings.Replace(updated, match, with, 1)
	}

	return os.WriteFile(path, []byte(updated), 0o644)
}

func runCopyFile(spec map[string]any) error {
	src := stringValue(spec, "src")
	dest := stringValue(spec, "dest")
	if src == "" || dest == "" {
		return fmt.Errorf("%s: CopyFile requires src and dest", errCodeInstallCopyPathMissing)
	}

	content, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dest, content, 0o644)
}

func runSysctl(spec map[string]any) error {
	path := stringValue(spec, "writeFile")
	if path == "" {
		path = stringValue(spec, "dest")
	}
	if path == "" {
		return fmt.Errorf("%s: Sysctl requires writeFile or dest", errCodeInstallSysctlPathMiss)
	}

	values, ok := spec["values"].(map[string]any)
	if !ok || len(values) == 0 {
		return fmt.Errorf("%s: Sysctl requires values", errCodeInstallSysctlValsMiss)
	}

	lines := make([]string, 0, len(values))
	for k, v := range values {
		lines = append(lines, fmt.Sprintf("%s=%v", k, v))
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

func runModprobe(spec map[string]any) error {
	persistPath := stringValue(spec, "persistFile")
	if persistPath == "" {
		return nil
	}

	mods := stringSlice(spec["modules"])
	if len(mods) == 0 {
		return fmt.Errorf("%s: Modprobe requires modules", errCodeInstallModulesMissing)
	}

	if err := os.MkdirAll(filepath.Dir(persistPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(persistPath, []byte(strings.Join(mods, "\n")+"\n"), 0o644)
}

func runCommand(spec map[string]any) error {
	cmdArgs := stringSlice(spec["command"])
	if len(cmdArgs) == 0 {
		return fmt.Errorf("%s: RunCommand requires command", errCodeInstallCommandMissing)
	}

	err := runTimedCommand(cmdArgs[0], cmdArgs[1:], commandTimeout(spec))
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: command timed out after %s", errCodeInstallCommandTimeout, commandTimeout(spec))
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return fmt.Errorf("%s: command exited non-zero: %w", errCodeInstallCommandFailed, err)
	}
	return err
}

func runVerifyImages(spec map[string]any) error {
	required := stringSlice(spec["images"])
	if len(required) == 0 {
		return fmt.Errorf("%s: VerifyImages requires images", errCodeInstallImagesMissing)
	}

	cmdArgs := stringSlice(spec["command"])
	if len(cmdArgs) == 0 {
		cmdArgs = []string{"ctr", "-n", "k8s.io", "images", "list", "-q"}
	}

	timeout := 20 * time.Second
	if ts := stringValue(spec, "timeout"); ts != "" {
		d, err := time.ParseDuration(ts)
		if err == nil && d > 0 {
			timeout = d
		}
	}

	output, err := runCommandOutput(cmdArgs, timeout)
	if err != nil {
		return fmt.Errorf("%s: %w", errCodeInstallImagesCmdFailed, err)
	}

	available := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		available[line] = true
	}

	missing := make([]string, 0)
	for _, image := range required {
		if !available[image] {
			missing = append(missing, image)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("%s: missing images: %s", errCodeInstallImagesNotFound, strings.Join(missing, ", "))
	}

	return nil
}

func runKubeadmInit(spec map[string]any) error {
	mode := stringValue(spec, "mode")
	if mode == "" {
		mode = "stub"
	}
	if mode == "stub" {
		return runKubeadmInitStub(spec)
	}
	if mode != "real" {
		return fmt.Errorf("%s: unsupported mode %q", errCodeInstallInitModeInvalid, mode)
	}
	return runKubeadmInitReal(spec)
}

func runKubeadmInitStub(spec map[string]any) error {
	joinFile := stringValue(spec, "outputJoinFile")
	if joinFile == "" {
		return fmt.Errorf("%s: KubeadmInit requires outputJoinFile", errCodeInstallInitJoinMissing)
	}
	if err := os.MkdirAll(filepath.Dir(joinFile), 0o755); err != nil {
		return err
	}
	content := "kubeadm join 10.0.0.10:6443 --token dummy.token --discovery-token-ca-cert-hash sha256:dummy\n"
	return os.WriteFile(joinFile, []byte(content), 0o644)
}

func runKubeadmJoin(spec map[string]any) error {
	mode := stringValue(spec, "mode")
	if mode == "" {
		mode = "stub"
	}
	if mode == "stub" {
		return runKubeadmJoinStub(spec)
	}
	if mode != "real" {
		return fmt.Errorf("%s: unsupported mode %q", errCodeInstallJoinModeInvalid, mode)
	}
	return runKubeadmJoinReal(spec)
}

func runKubeadmJoinStub(spec map[string]any) error {
	joinFile := stringValue(spec, "joinFile")
	if joinFile == "" {
		return fmt.Errorf("%s: KubeadmJoin requires joinFile", errCodeInstallJoinPathMissing)
	}
	if _, err := os.Stat(joinFile); err != nil {
		return fmt.Errorf("%s: join file not found: %w", errCodeInstallJoinFileMissing, err)
	}
	return nil
}

func runKubeadmInitReal(spec map[string]any) error {
	joinFile := stringValue(spec, "outputJoinFile")
	if joinFile == "" {
		return fmt.Errorf("%s: KubeadmInit requires outputJoinFile", errCodeInstallInitJoinMissing)
	}

	args := []string{"init"}
	if configFile := stringValue(spec, "configFile"); configFile != "" {
		args = append(args, "--config", configFile)
	}
	if advertiseAddress := stringValue(spec, "advertiseAddress"); advertiseAddress != "" {
		args = append(args, "--apiserver-advertise-address", advertiseAddress)
	}
	if podCIDR := stringValue(spec, "podNetworkCIDR"); podCIDR != "" {
		args = append(args, "--pod-network-cidr", podCIDR)
	}
	if criSocket := stringValue(spec, "criSocket"); criSocket != "" {
		args = append(args, "--cri-socket", criSocket)
	}
	if ignore := stringSlice(spec["ignorePreflightErrors"]); len(ignore) > 0 {
		args = append(args, "--ignore-preflight-errors", strings.Join(ignore, ","))
	}
	if extra := stringSlice(spec["extraArgs"]); len(extra) > 0 {
		args = append(args, extra...)
	}

	if err := runTimedCommand("kubeadm", args, commandTimeoutWithDefault(spec, 10*time.Minute)); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("%s: kubeadm init timed out: %w", errCodeInstallInitFailed, err)
		}
		return fmt.Errorf("%s: kubeadm init failed: %w", errCodeInstallInitFailed, err)
	}

	joinArgs := []string{"token", "create", "--print-join-command"}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeoutWithDefault(spec, 10*time.Minute))
	defer cancel()
	cmd := exec.CommandContext(ctx, "kubeadm", joinArgs...)
	joinOut, err := cmd.Output()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("%s: kubeadm token create timed out", errCodeInstallInitFailed)
		}
		return fmt.Errorf("%s: kubeadm token create failed: %w", errCodeInstallInitFailed, err)
	}
	joinCmd := strings.TrimSpace(string(joinOut))
	if joinCmd == "" {
		return fmt.Errorf("%s: empty kubeadm join command output", errCodeInstallInitFailed)
	}

	if err := os.MkdirAll(filepath.Dir(joinFile), 0o755); err != nil {
		return err
	}
	return os.WriteFile(joinFile, []byte(joinCmd+"\n"), 0o644)
}

func runKubeadmJoinReal(spec map[string]any) error {
	joinFile := stringValue(spec, "joinFile")
	if joinFile == "" {
		return fmt.Errorf("%s: KubeadmJoin requires joinFile", errCodeInstallJoinPathMissing)
	}
	raw, err := os.ReadFile(joinFile)
	if err != nil {
		return fmt.Errorf("%s: join file not found: %w", errCodeInstallJoinFileMissing, err)
	}
	joinCommand := strings.TrimSpace(string(raw))
	if joinCommand == "" {
		return fmt.Errorf("%s: join command is empty", errCodeInstallJoinCmdMissing)
	}
	args := strings.Fields(joinCommand)
	if len(args) == 0 || args[0] != "kubeadm" {
		return fmt.Errorf("%s: join command must start with kubeadm", errCodeInstallJoinCmdInvalid)
	}
	if extra := stringSlice(spec["extraArgs"]); len(extra) > 0 {
		args = append(args, extra...)
	}

	if err := runTimedCommand(args[0], args[1:], commandTimeoutWithDefault(spec, 5*time.Minute)); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("%s: kubeadm join timed out: %w", errCodeInstallJoinFailed, err)
		}
		return fmt.Errorf("%s: kubeadm join failed: %w", errCodeInstallJoinFailed, err)
	}
	return nil
}

func commandTimeout(spec map[string]any) time.Duration {
	return commandTimeoutWithDefault(spec, 30*time.Second)
}

func commandTimeoutWithDefault(spec map[string]any, def time.Duration) time.Duration {
	timeout := def
	if ts := stringValue(spec, "timeout"); ts != "" {
		d, err := time.ParseDuration(ts)
		if err == nil && d > 0 {
			timeout = d
		}
	}
	return timeout
}

func runTimedCommand(name string, args []string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return err
}

func runCommandOutput(cmdArgs []string, timeout time.Duration) (string, error) {
	if len(cmdArgs) == 0 {
		return "", fmt.Errorf("empty command")
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
	output, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("command timed out after %s", timeout)
	}
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg != "" {
			return "", fmt.Errorf("command failed: %w: %s", err, msg)
		}
		return "", fmt.Errorf("command failed: %w", err)
	}
	return string(output), nil
}

func applyRegister(step config.Step, rendered map[string]any, runtimeVars map[string]any) error {
	if len(step.Register) == 0 {
		return nil
	}
	outputs := stepOutputs(step.Kind, rendered)
	for runtimeKey, outputKey := range step.Register {
		v, ok := outputs[outputKey]
		if !ok {
			return fmt.Errorf("%s: step %s kind %s has no output key %s", errCodeRegisterOutputMissing, step.ID, step.Kind, outputKey)
		}
		runtimeVars[runtimeKey] = v
	}
	return nil
}

func stepOutputs(kind string, rendered map[string]any) map[string]any {
	outputs := map[string]any{}
	switch kind {
	case "WriteFile":
		if path := stringValue(rendered, "path"); path != "" {
			outputs["path"] = path
		}
	case "CopyFile":
		if dest := stringValue(rendered, "dest"); dest != "" {
			outputs["dest"] = dest
		}
	case "KubeadmInit":
		if joinFile := stringValue(rendered, "outputJoinFile"); joinFile != "" {
			outputs["joinFile"] = joinFile
		}
	}
	return outputs
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

func evaluateWhen(expr string, vars map[string]any, runtime map[string]any, ctx map[string]any) (bool, error) {
	trimmed := strings.TrimSpace(expr)
	if trimmed == "" {
		return true, nil
	}

	tokens, err := tokenizeCondition(trimmed)
	if err != nil {
		return false, fmt.Errorf("%s: %w", errCodeConditionEval, err)
	}
	p := &condParser{tokens: tokens, vars: vars, runtime: runtime, ctx: ctx}
	value, err := p.parseExpr()
	if err != nil {
		return false, fmt.Errorf("%s: %w", errCodeConditionEval, err)
	}
	if p.hasNext() {
		return false, fmt.Errorf("%s: unexpected token %q", errCodeConditionEval, p.peek().value)
	}
	b, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("%s: condition must evaluate to boolean", errCodeConditionEval)
	}
	return b, nil
}

type condToken struct {
	kind  string
	value string
}

type condParser struct {
	tokens  []condToken
	pos     int
	vars    map[string]any
	runtime map[string]any
	ctx     map[string]any
}

func tokenizeCondition(expr string) ([]condToken, error) {
	tokens := make([]condToken, 0)
	for i := 0; i < len(expr); {
		ch := expr[i]
		if ch == ' ' || ch == '\t' || ch == '\n' {
			i++
			continue
		}
		if ch == '(' || ch == ')' {
			tokens = append(tokens, condToken{kind: string(ch), value: string(ch)})
			i++
			continue
		}
		if i+1 < len(expr) {
			two := expr[i : i+2]
			if two == "==" || two == "!=" {
				tokens = append(tokens, condToken{kind: two, value: two})
				i += 2
				continue
			}
		}
		if ch == '"' {
			j := i + 1
			for j < len(expr) && expr[j] != '"' {
				if expr[j] == '\\' && j+1 < len(expr) {
					j += 2
					continue
				}
				j++
			}
			if j >= len(expr) {
				return nil, fmt.Errorf("unterminated string literal")
			}
			raw := expr[i+1 : j]
			unquoted, err := strconv.Unquote("\"" + strings.ReplaceAll(raw, "\"", "\\\"") + "\"")
			if err != nil {
				return nil, fmt.Errorf("invalid string literal")
			}
			tokens = append(tokens, condToken{kind: "string", value: unquoted})
			i = j + 1
			continue
		}
		if isIdentStart(ch) {
			j := i + 1
			for j < len(expr) && isIdentPart(expr[j]) {
				j++
			}
			word := expr[i:j]
			tokens = append(tokens, condToken{kind: "ident", value: word})
			i = j
			continue
		}
		return nil, fmt.Errorf("invalid character %q", ch)
	}
	return tokens, nil
}

func (p *condParser) parseExpr() (any, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.matchIdent("or") {
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		lb, ok := left.(bool)
		if !ok {
			return nil, fmt.Errorf("left operand of or is not boolean")
		}
		rb, ok := right.(bool)
		if !ok {
			return nil, fmt.Errorf("right operand of or is not boolean")
		}
		left = lb || rb
	}
	return left, nil
}

func (p *condParser) parseAnd() (any, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.matchIdent("and") {
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		lb, ok := left.(bool)
		if !ok {
			return nil, fmt.Errorf("left operand of and is not boolean")
		}
		rb, ok := right.(bool)
		if !ok {
			return nil, fmt.Errorf("right operand of and is not boolean")
		}
		left = lb && rb
	}
	return left, nil
}

func (p *condParser) parseUnary() (any, error) {
	if p.matchIdent("not") {
		v, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("operand of not is not boolean")
		}
		return !b, nil
	}
	return p.parsePrimary()
}

func (p *condParser) parsePrimary() (any, error) {
	if p.matchKind("(") {
		v, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if !p.matchKind(")") {
			return nil, fmt.Errorf("missing closing parenthesis")
		}
		return v, nil
	}

	left, err := p.parseValue()
	if err != nil {
		return nil, err
	}
	if p.matchKind("==") {
		right, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		return compareValues(left, right), nil
	}
	if p.matchKind("!=") {
		right, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		return !compareValues(left, right), nil
	}
	return left, nil
}

func (p *condParser) parseValue() (any, error) {
	if !p.hasNext() {
		return nil, fmt.Errorf("unexpected end of expression")
	}
	tok := p.next()
	if tok.kind == "string" {
		return tok.value, nil
	}
	if tok.kind == "ident" {
		if tok.value == "true" {
			return true, nil
		}
		if tok.value == "false" {
			return false, nil
		}
		if v, ok := p.resolveIdentifier(tok.value); ok {
			return v, nil
		}
		return nil, fmt.Errorf("unknown identifier %q", tok.value)
	}
	return nil, fmt.Errorf("unexpected token %q", tok.value)
}

func (p *condParser) resolveIdentifier(id string) (any, bool) {
	if strings.HasPrefix(id, "vars.") {
		return resolveNestedMap(p.vars, strings.TrimPrefix(id, "vars."))
	}
	if strings.HasPrefix(id, "runtime.") {
		return resolveNestedMap(p.runtime, strings.TrimPrefix(id, "runtime."))
	}
	if strings.HasPrefix(id, "context.") {
		return resolveNestedMap(p.ctx, strings.TrimPrefix(id, "context."))
	}
	if v, ok := p.vars[id]; ok {
		return v, true
	}
	if v, ok := p.runtime[id]; ok {
		return v, true
	}
	if v, ok := p.ctx[id]; ok {
		return v, true
	}
	if strings.Contains(id, ".") {
		if v, ok := resolvePath(id, map[string]any{"vars": p.vars, "runtime": p.runtime, "context": p.ctx}); ok {
			return v, true
		}
	}
	return nil, false
}

func resolveNestedMap(root map[string]any, path string) (any, bool) {
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return nil, false
	}
	cur := any(root)
	for _, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := m[p]
		if !ok {
			return nil, false
		}
		cur = next
	}
	return cur, true
}

func (p *condParser) hasNext() bool {
	return p.pos < len(p.tokens)
}

func (p *condParser) peek() condToken {
	return p.tokens[p.pos]
}

func (p *condParser) next() condToken {
	tok := p.tokens[p.pos]
	p.pos++
	return tok
}

func (p *condParser) matchKind(kind string) bool {
	if !p.hasNext() {
		return false
	}
	if p.peek().kind != kind {
		return false
	}
	p.pos++
	return true
}

func (p *condParser) matchIdent(word string) bool {
	if !p.hasNext() {
		return false
	}
	tok := p.peek()
	if tok.kind != "ident" || tok.value != word {
		return false
	}
	p.pos++
	return true
}

func compareValues(a, b any) bool {
	switch av := a.(type) {
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case int:
		bf, ok := numberAsFloat64(b)
		return ok && float64(av) == bf
	case int64:
		bf, ok := numberAsFloat64(b)
		return ok && float64(av) == bf
	case float64:
		bf, ok := numberAsFloat64(b)
		return ok && math.Abs(av-bf) < 1e-9
	default:
		return fmt.Sprint(a) == fmt.Sprint(b)
	}
}

func numberAsFloat64(v any) (float64, bool) {
	switch nv := v.(type) {
	case int:
		return float64(nv), true
	case int64:
		return float64(nv), true
	case float64:
		return nv, true
	default:
		return 0, false
	}
}

func isIdentStart(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

func isIdentPart(ch byte) bool {
	return isIdentStart(ch) || (ch >= '0' && ch <= '9') || ch == '.'
}

func loadState(path string) (*State, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{Phase: "install", CompletedSteps: []string{}}, nil
		}
		return nil, fmt.Errorf("read state file: %w", err)
	}

	var st State
	if err := json.Unmarshal(content, &st); err != nil {
		return nil, fmt.Errorf("parse state file: %w", err)
	}
	if st.CompletedSteps == nil {
		st.CompletedSteps = []string{}
	}
	if st.RuntimeVars == nil {
		st.RuntimeVars = map[string]any{}
	}
	if st.SkippedSteps == nil {
		st.SkippedSteps = []string{}
	}
	return &st, nil
}

func saveState(path string, st *State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state file: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("write temp state file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace state file: %w", err)
	}
	return nil
}

func renderSpec(spec map[string]any, wf *config.Workflow, runtimeVars map[string]any) map[string]any {
	if spec == nil {
		return map[string]any{}
	}
	ctx := map[string]any{
		"vars":    wf.Vars,
		"context": map[string]any{"bundleRoot": wf.Context.BundleRoot, "stateFile": wf.Context.StateFile},
		"runtime": runtimeVars,
	}
	return renderMap(spec, ctx)
}

func renderMap(input map[string]any, ctx map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for k, v := range input {
		out[k] = renderAny(v, ctx)
	}
	return out
}

func renderAny(v any, ctx map[string]any) any {
	switch tv := v.(type) {
	case string:
		return renderString(tv, ctx)
	case map[string]any:
		return renderMap(tv, ctx)
	case []any:
		out := make([]any, 0, len(tv))
		for _, item := range tv {
			out = append(out, renderAny(item, ctx))
		}
		return out
	default:
		return v
	}
}

func renderString(input string, ctx map[string]any) string {
	return templateRefPattern.ReplaceAllStringFunc(input, func(full string) string {
		m := templateRefPattern.FindStringSubmatch(full)
		if len(m) != 2 {
			return full
		}
		path := m[1]
		if val, ok := resolvePath(path, ctx); ok {
			return fmt.Sprint(val)
		}
		return full
	})
}

func resolvePath(path string, ctx map[string]any) (any, bool) {
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return nil, false
	}

	cur := any(ctx)
	for i, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}

		next, exists := m[p]
		if !exists {
			if i == 0 {
				if vars, vok := ctx["vars"].(map[string]any); vok {
					next, exists = vars[p]
				}
			}
			if !exists {
				return nil, false
			}
		}
		cur = next
	}
	return cur, true
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
