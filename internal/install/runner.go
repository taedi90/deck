package install

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/taedi90/deck/internal/bundle"
	"github.com/taedi90/deck/internal/config"
)

type RunOptions struct {
	BundleRoot string
}

type State struct {
	Phase          string   `json:"phase"`
	CompletedSteps []string `json:"completedSteps"`
	FailedStep     string   `json:"failedStep,omitempty"`
	Error          string   `json:"error,omitempty"`
}

var templateRefPattern = regexp.MustCompile(`\{\s*\.([A-Za-z0-9_\.]+)\s*\}`)

const (
	errCodeInstallKindUnsupported  = "E_INSTALL_KIND_UNSUPPORTED"
	errCodeInstallPackagesRequired = "E_INSTALL_PACKAGES_REQUIRED"
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
	errCodeInstallInitJoinMissing  = "E_INSTALL_KUBEADM_INIT_JOINFILE_REQUIRED"
	errCodeInstallJoinPathMissing  = "E_INSTALL_KUBEADM_JOIN_JOINFILE_REQUIRED"
	errCodeInstallJoinFileMissing  = "E_INSTALL_KUBEADM_JOIN_FILE_NOT_FOUND"
	errCodeInstallInitModeInvalid  = "E_INSTALL_KUBEADM_INIT_MODE_INVALID"
	errCodeInstallJoinModeInvalid  = "E_INSTALL_KUBEADM_JOIN_MODE_INVALID"
	errCodeInstallInitFailed       = "E_INSTALL_KUBEADM_INIT_FAILED"
	errCodeInstallJoinFailed       = "E_INSTALL_KUBEADM_JOIN_FAILED"
	errCodeInstallJoinCmdInvalid   = "E_INSTALL_KUBEADM_JOIN_COMMAND_INVALID"
	errCodeInstallJoinCmdMissing   = "E_INSTALL_KUBEADM_JOIN_COMMAND_MISSING"
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
	for _, step := range installPhase.Steps {
		if completed[step.ID] {
			continue
		}

		rendered := renderSpec(step.Spec, wf, runtimeVars)
		if err := executeStep(step.Kind, rendered); err != nil {
			st.FailedStep = step.ID
			st.Error = err.Error()
			_ = saveState(statePath, st)
			return fmt.Errorf("step %s (%s): %w", step.ID, step.Kind, err)
		}

		st.CompletedSteps = append(st.CompletedSteps, step.ID)
		completed[step.ID] = true
		st.FailedStep = ""
		st.Error = ""
		if err := saveState(statePath, st); err != nil {
			return err
		}
	}

	st.FailedStep = ""
	st.Error = ""
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
		pkgs := stringSlice(spec["packages"])
		if len(pkgs) == 0 {
			return fmt.Errorf("%s: InstallPackages requires packages", errCodeInstallPackagesRequired)
		}
		return nil
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
	case "KubeadmInit":
		return runKubeadmInit(spec)
	case "KubeadmJoin":
		return runKubeadmJoin(spec)
	default:
		return fmt.Errorf("%s: unsupported step kind %s", errCodeInstallKindUnsupported, kind)
	}
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
