package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/taedi90/deck/internal/bundle"
	"github.com/taedi90/deck/internal/config"
	"github.com/taedi90/deck/internal/install"
	"github.com/taedi90/deck/internal/validate"
)

type diffOptions struct {
	workflowPath  string
	server        string
	session       string
	apiToken      string
	selectedPhase string
	output        string
	varOverrides  map[string]string
}

func newPlanCommand() *cobra.Command {
	vars := &varFlag{}
	cmd := &cobra.Command{
		Use:     "plan",
		Aliases: []string{"diff"},
		Short:   "Show the planned install step execution",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDiffWithOptions(diffOptions{
				workflowPath:  cmdFlagValue(cmd, "file"),
				server:        cmdFlagValue(cmd, "server"),
				session:       cmdFlagValue(cmd, "session"),
				apiToken:      cmdFlagValue(cmd, "api-token"),
				selectedPhase: cmdFlagValue(cmd, "phase"),
				output:        cmdFlagValue(cmd, "output"),
				varOverrides:  vars.AsMap(),
			})
		},
	}
	cmd.Flags().SetInterspersed(false)
	cmd.Flags().StringP("file", "f", "", "path to workflow file")
	cmd.Flags().String("server", "", "site server URL (assisted mode requires --server and --session)")
	cmd.Flags().String("session", "", "site session id for assisted mode")
	cmd.Flags().String("api-token", "deck-site-v1", "bearer token for assisted site APIs")
	cmd.Flags().String("phase", "install", "phase name to plan")
	cmd.Flags().StringP("output", "o", "text", "output format (text|json)")
	cmd.Flags().Var(vars, "var", "set variable override (key=value), repeatable")
	return cmd
}

func runDiffWithOptions(opts diffOptions) error {
	assistedConfig, assistedMode, err := resolveAssistedExecutionConfig(opts.server, opts.session, opts.apiToken)
	if err != nil {
		return err
	}
	workflowPath := strings.TrimSpace(opts.workflowPath)
	selectedPhase := strings.TrimSpace(opts.selectedPhase)
	if assistedMode {
		return runAssistedAction(assistedConfig, "diff", func(ctx assistedExecutionContext) error {
			return executeDiff(ctx.WorkflowPath, selectedPhase, opts.output, varsAsAnyMap(opts.varOverrides))
		})
	}
	if workflowPath == "" {
		return errors.New("--file (or -f) is required")
	}
	return executeDiff(workflowPath, selectedPhase, opts.output, varsAsAnyMap(opts.varOverrides))
}

func executeDiff(workflowPath, selectedPhase, output string, varOverrides map[string]any) error {
	resolvedRequest, err := resolveApplyExecutionRequest(applyExecutionRequestOptions{
		CommandName:                  "diff",
		WorkflowPath:                 workflowPath,
		VarOverrides:                 varOverrides,
		SelectedPhase:                selectedPhase,
		DefaultPhase:                 "install",
		BuildExecutionWorkflow:       true,
		ResolveStatePath:             true,
		StatePathFromExecutionTarget: true,
	})
	if err != nil {
		return err
	}
	applyExecutionWorkflow := resolvedRequest.ExecutionWorkflow

	state, err := loadInstallDryRunState(applyExecutionWorkflow)
	if err != nil {
		return err
	}
	completed := make(map[string]bool, len(state.CompletedSteps))
	for _, stepID := range state.CompletedSteps {
		completed[stepID] = true
	}
	runtimeVars := map[string]any{}
	for k, v := range state.RuntimeVars {
		runtimeVars[k] = v
	}
	statePath := resolvedRequest.StatePath
	ctxData := map[string]any{"bundleRoot": "", "stateFile": statePath}
	phaseView, found := findWorkflowPhaseByName(applyExecutionWorkflow, "install")
	if !found {
		return errors.New("install phase not found")
	}

	type diffStep struct {
		ID     string `json:"id"`
		Kind   string `json:"kind"`
		Action string `json:"action"`
		Reason string `json:"reason,omitempty"`
	}
	steps := make([]diffStep, 0, len(phaseView.Steps))
	for _, step := range phaseView.Steps {
		if completed[step.ID] {
			steps = append(steps, diffStep{ID: step.ID, Kind: step.Kind, Action: "skip", Reason: "completed"})
			continue
		}
		ok, evalErr := install.EvaluateWhen(step.When, applyExecutionWorkflow.Vars, runtimeVars, ctxData)
		if evalErr != nil {
			return fmt.Errorf("WHEN_EVAL_ERROR: step %s (%s): %w", step.ID, step.Kind, evalErr)
		}
		if !ok {
			steps = append(steps, diffStep{ID: step.ID, Kind: step.Kind, Action: "skip", Reason: "when"})
			continue
		}
		steps = append(steps, diffStep{ID: step.ID, Kind: step.Kind, Action: "run"})
	}

	if output == "json" {
		payload := struct {
			Phase     string     `json:"phase"`
			StatePath string     `json:"statePath"`
			Steps     []diffStep `json:"steps"`
		}{Phase: resolvedRequest.SelectedPhase, StatePath: statePath, Steps: steps}
		enc := json.NewEncoder(os.Stdout)
		return enc.Encode(payload)
	}
	for _, s := range steps {
		if s.Action == "skip" && s.Reason != "" {
			if err := stdoutPrintf("%s %s SKIP (%s)\n", s.ID, s.Kind, s.Reason); err != nil {
				return err
			}
			continue
		}
		if err := stdoutPrintf("%s %s %s\n", s.ID, s.Kind, strings.ToUpper(s.Action)); err != nil {
			return err
		}
	}
	return nil
}

var doctorVarRefPattern = regexp.MustCompile(`\.vars\.([A-Za-z_][A-Za-z0-9_]*)`)

type doctorReport struct {
	Timestamp string         `json:"timestamp"`
	Workflow  string         `json:"workflow"`
	Summary   doctorSummary  `json:"summary"`
	Checks    []doctorCheck  `json:"checks"`
	Vars      map[string]any `json:"vars"`
}

type doctorSummary struct {
	Passed int `json:"passed"`
	Failed int `json:"failed"`
}

type doctorCheck struct {
	Name    string   `json:"name"`
	Kind    string   `json:"kind"`
	Value   string   `json:"value"`
	Status  string   `json:"status"`
	Message string   `json:"message,omitempty"`
	UsedBy  []string `json:"used_by,omitempty"`
}

type doctorOptions struct {
	workflowPath string
	server       string
	session      string
	apiToken     string
	outPath      string
	varOverrides map[string]string
}

func newDoctorCommand() *cobra.Command {
	vars := &varFlag{}
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check referenced artifact inputs before apply",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDoctorWithOptions(doctorOptions{
				workflowPath: cmdFlagValue(cmd, "file"),
				server:       cmdFlagValue(cmd, "server"),
				session:      cmdFlagValue(cmd, "session"),
				apiToken:     cmdFlagValue(cmd, "api-token"),
				outPath:      cmdFlagValue(cmd, "out"),
				varOverrides: vars.AsMap(),
			})
		},
	}
	cmd.Flags().SetInterspersed(false)
	cmd.Flags().StringP("file", "f", "", "path or URL to workflow file")
	cmd.Flags().String("server", "", "site server URL (assisted mode requires --server and --session)")
	cmd.Flags().String("session", "", "site session id for assisted mode")
	cmd.Flags().String("api-token", "deck-site-v1", "bearer token for assisted site APIs")
	cmd.Flags().String("out", "", "output report path (required)")
	cmd.Flags().Var(vars, "var", "set variable override (key=value), repeatable")
	return cmd
}

func runDoctorWithOptions(opts doctorOptions) error {
	resolvedOut := strings.TrimSpace(opts.outPath)
	assistedConfig, assistedMode, err := resolveAssistedExecutionConfig(opts.server, opts.session, opts.apiToken)
	if err != nil {
		return err
	}
	if resolvedOut == "" && !assistedMode {
		return errors.New("--out is required")
	}
	if resolvedOut == "" && assistedMode {
		resolvedOut = filepath.Join(assistedDataRoot(), "reports", strings.TrimSpace(opts.session), "doctor-local.json")
	}

	if assistedMode {
		return runAssistedAction(assistedConfig, "doctor", func(ctx assistedExecutionContext) error {
			return executeDoctor(ctx.WorkflowPath, varsAsAnyMap(opts.varOverrides), resolvedOut)
		})
	}

	return executeDoctor(strings.TrimSpace(opts.workflowPath), varsAsAnyMap(opts.varOverrides), resolvedOut)
}

func executeDoctor(workflowPath string, varOverrides map[string]any, resolvedOut string) error {
	resolvedRequest, err := resolveApplyExecutionRequest(applyExecutionRequestOptions{
		CommandName:                "doctor",
		WorkflowPath:               strings.TrimSpace(workflowPath),
		DiscoverWorkflow:           func() (string, error) { return discoverApplyWorkflow(".") },
		AllowRemoteWorkflow:        true,
		NormalizeLocalWorkflowPath: true,
		VarOverrides:               varOverrides,
	})
	if err != nil {
		return err
	}
	resolvedWorkflowPath := resolvedRequest.WorkflowPath
	wf := resolvedRequest.Workflow

	checks := make([]doctorCheck, 0)
	checkByName := map[string]*doctorCheck{}
	addCheck := func(c doctorCheck) {
		if existing, ok := checkByName[c.Name]; ok {
			usedBy := append(existing.UsedBy, c.UsedBy...)
			sort.Strings(usedBy)
			existing.UsedBy = dedupeStrings(usedBy)
			if existing.Status == "passed" && c.Status == "failed" {
				existing.Status = "failed"
				existing.Message = c.Message
				existing.Value = c.Value
				existing.Kind = c.Kind
			}
			return
		}
		checks = append(checks, c)
		checkByName[c.Name] = &checks[len(checks)-1]
	}

	refs := collectDoctorArtifactVarRefs(wf)
	for name, usedBy := range refs {
		v, ok := wf.Vars[name]
		if !ok {
			addCheck(doctorCheck{Name: "vars." + name, Kind: "var", Status: "failed", Message: "missing", UsedBy: usedBy})
			continue
		}
		s, ok := v.(string)
		if !ok {
			addCheck(doctorCheck{Name: "vars." + name, Kind: "var", Status: "failed", Message: "not a string", UsedBy: usedBy})
			continue
		}
		resolved := strings.TrimSpace(s)
		if strings.HasPrefix(resolved, "http://") || strings.HasPrefix(resolved, "https://") {
			status, msg := doctorCheckHTTPReachable(resolved)
			addCheck(doctorCheck{Name: "vars." + name, Kind: "http", Value: resolved, Status: status, Message: msg, UsedBy: usedBy})
			continue
		}
		status, msg := doctorCheckPathExists(resolved)
		addCheck(doctorCheck{Name: "vars." + name, Kind: "path", Value: resolved, Status: status, Message: msg, UsedBy: usedBy})
	}

	report := doctorReport{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Workflow:  resolvedWorkflowPath,
		Checks:    checks,
		Vars:      wf.Vars,
	}
	for _, c := range checks {
		if c.Status == "failed" {
			report.Summary.Failed++
		} else {
			report.Summary.Passed++
		}
	}

	if err := os.MkdirAll(filepath.Dir(resolvedOut), 0o755); err != nil {
		return fmt.Errorf("create report parent dir: %w", err)
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode doctor report: %w", err)
	}
	if err := os.WriteFile(resolvedOut, raw, 0o644); err != nil {
		return fmt.Errorf("write doctor report: %w", err)
	}

	if err := stdoutPrintf("doctor: wrote %s\n", resolvedOut); err != nil {
		return err
	}
	if report.Summary.Failed > 0 {
		return fmt.Errorf("doctor: failed (%d failed checks)", report.Summary.Failed)
	}
	return nil
}

func collectDoctorArtifactVarRefs(wf *config.Workflow) map[string][]string {
	refs := map[string]map[string]bool{}
	if wf == nil {
		return map[string][]string{}
	}
	for _, phase := range wf.Phases {
		for _, step := range phase.Steps {
			fetchRaw, ok := step.Spec["fetch"].(map[string]any)
			if !ok {
				continue
			}
			sourcesRaw, ok := fetchRaw["sources"].([]any)
			if !ok {
				continue
			}
			for _, raw := range sourcesRaw {
				s, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				for _, key := range []string{"path", "url"} {
					vRaw, ok := s[key].(string)
					if !ok {
						continue
					}
					v := strings.TrimSpace(vRaw)
					if v == "" {
						continue
					}
					matches := doctorVarRefPattern.FindAllStringSubmatch(v, -1)
					for _, m := range matches {
						if len(m) != 2 {
							continue
						}
						name := m[1]
						if refs[name] == nil {
							refs[name] = map[string]bool{}
						}
						refs[name][step.ID] = true
					}
				}
			}
		}
	}
	out := map[string][]string{}
	for name, usedBy := range refs {
		steps := make([]string, 0, len(usedBy))
		for stepID := range usedBy {
			steps = append(steps, stepID)
		}
		sort.Strings(steps)
		out[name] = steps
	}
	return out
}

func doctorCheckPathExists(path string) (string, string) {
	if strings.TrimSpace(path) == "" {
		return "failed", "empty path"
	}
	if _, err := os.Stat(path); err != nil {
		return "failed", err.Error()
	}
	return "passed", ""
}

func doctorCheckHTTPReachable(url string) (string, string) {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		return "failed", err.Error()
	}
	resp, err := client.Do(req)
	if err != nil {
		return "failed", err.Error()
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return "failed", fmt.Sprintf("unexpected status %d", resp.StatusCode)
	}
	return "passed", ""
}

func loadInstallDryRunState(wf *config.Workflow) (*install.State, error) {
	statePath, err := resolveInstallStatePath(wf)
	if err != nil {
		return nil, err
	}
	return install.LoadState(statePath)
}

func resolveInstallStatePath(wf *config.Workflow) (string, error) {
	return install.DefaultStatePath(wf)
}

type applyExecutionRequestOptions struct {
	CommandName                  string
	WorkflowPath                 string
	DiscoverWorkflow             func() (string, error)
	AllowRemoteWorkflow          bool
	NormalizeLocalWorkflowPath   bool
	VarOverrides                 map[string]any
	SelectedPhase                string
	DefaultPhase                 string
	BuildExecutionWorkflow       bool
	ResolveStatePath             bool
	StatePathFromExecutionTarget bool
}

type applyExecutionRequest struct {
	WorkflowPath      string
	Workflow          *config.Workflow
	SelectedPhase     string
	ExecutionWorkflow *config.Workflow
	StatePath         string
}

func resolveApplyExecutionRequest(opts applyExecutionRequestOptions) (applyExecutionRequest, error) {
	workflowPath := strings.TrimSpace(opts.WorkflowPath)
	if workflowPath == "" && opts.DiscoverWorkflow != nil {
		resolvedWorkflowPath, err := opts.DiscoverWorkflow()
		if err != nil {
			return applyExecutionRequest{}, err
		}
		workflowPath = strings.TrimSpace(resolvedWorkflowPath)
	}

	isRemoteWorkflow := opts.AllowRemoteWorkflow && isHTTPWorkflowPath(workflowPath)
	if isRemoteWorkflow {
		workflowBytes, err := fetchWorkflowForApplyValidation(workflowPath)
		if err != nil {
			return applyExecutionRequest{}, err
		}
		if err := validate.Bytes(workflowPath, workflowBytes); err != nil {
			return applyExecutionRequest{}, err
		}
	} else {
		if opts.NormalizeLocalWorkflowPath {
			resolvedWorkflowPath, err := filepath.Abs(workflowPath)
			if err != nil {
				return applyExecutionRequest{}, fmt.Errorf("resolve workflow path: %w", err)
			}
			workflowPath = resolvedWorkflowPath
		}
		if err := validate.File(workflowPath); err != nil {
			return applyExecutionRequest{}, err
		}
	}

	ctx := context.Background()
	wf, err := config.LoadWithOptions(ctx, workflowPath, config.LoadOptions{VarOverrides: opts.VarOverrides})
	if err != nil {
		return applyExecutionRequest{}, err
	}
	if strings.TrimSpace(wf.Role) != "apply" {
		return applyExecutionRequest{}, fmt.Errorf("%s workflow role must be apply: %s", opts.CommandName, workflowPath)
	}

	selectedPhase := strings.TrimSpace(opts.SelectedPhase)
	if selectedPhase == "" {
		selectedPhase = strings.TrimSpace(opts.DefaultPhase)
	}

	var executionWorkflow *config.Workflow
	if opts.BuildExecutionWorkflow {
		executionWorkflow, err = buildApplyExecutionWorkflow(wf, selectedPhase)
		if err != nil {
			return applyExecutionRequest{}, err
		}
	}

	statePath := ""
	if opts.ResolveStatePath {
		stateWorkflow := wf
		if opts.StatePathFromExecutionTarget {
			stateWorkflow = executionWorkflow
		}
		resolvedStatePath, err := resolveInstallStatePath(stateWorkflow)
		if err != nil {
			return applyExecutionRequest{}, err
		}
		statePath = resolvedStatePath
	}

	return applyExecutionRequest{
		WorkflowPath:      workflowPath,
		Workflow:          wf,
		SelectedPhase:     selectedPhase,
		ExecutionWorkflow: executionWorkflow,
		StatePath:         statePath,
	}, nil
}

func findWorkflowPhaseByName(wf *config.Workflow, name string) (config.Phase, bool) {
	for _, phase := range wf.Phases {
		if phase.Name == name {
			return phase, true
		}
	}
	return config.Phase{}, false
}

type applyOptions struct {
	workflowPath  string
	server        string
	session       string
	apiToken      string
	selectedPhase string
	prefetch      bool
	dryRun        bool
	varOverrides  map[string]string
	positional    []string
}

func newApplyCommand() *cobra.Command {
	vars := &varFlag{}
	cmd := &cobra.Command{
		Use:   "apply [workflow] [bundle]",
		Short: "Execute an apply file against a bundle",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 2 {
				return errors.New("apply accepts at most two positional arguments: [workflow] [bundle]")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runApplyWithOptions(applyOptions{
				workflowPath:  cmdFlagValue(cmd, "file"),
				server:        cmdFlagValue(cmd, "server"),
				session:       cmdFlagValue(cmd, "session"),
				apiToken:      cmdFlagValue(cmd, "api-token"),
				selectedPhase: cmdFlagValue(cmd, "phase"),
				prefetch:      cmdFlagBoolValue(cmd, "prefetch"),
				dryRun:        cmdFlagBoolValue(cmd, "dry-run"),
				varOverrides:  vars.AsMap(),
				positional:    args,
			})
		},
	}
	cmd.Flags().SetInterspersed(false)
	cmd.Flags().StringP("file", "f", "", "path or URL to workflow file")
	cmd.Flags().String("server", "", "site server URL (assisted mode requires --server and --session)")
	cmd.Flags().String("session", "", "site session id for assisted mode")
	cmd.Flags().String("api-token", "deck-site-v1", "bearer token for assisted site APIs")
	cmd.Flags().String("phase", "install", "phase name to execute")
	cmd.Flags().Bool("prefetch", false, "execute DownloadFile steps before other steps")
	cmd.Flags().Bool("dry-run", false, "print apply plan without executing steps")
	cmd.Flags().Var(vars, "var", "set variable override (key=value), repeatable")
	return cmd
}

func runApplyWithOptions(opts applyOptions) error {
	if len(opts.positional) > 2 {
		return errors.New("apply accepts at most two positional arguments: [workflow] [bundle]")
	}
	positionalArgs := make([]string, 0, len(opts.positional))
	for _, arg := range opts.positional {
		positionalArgs = append(positionalArgs, strings.TrimSpace(arg))
	}

	assistedConfig, assistedMode, err := resolveAssistedExecutionConfig(opts.server, opts.session, opts.apiToken)
	if err != nil {
		return err
	}
	if assistedMode {
		return runAssistedAction(assistedConfig, "apply", func(ctx assistedExecutionContext) error {
			return executeApply(ctx.WorkflowPath, ctx.BundleRoot, strings.TrimSpace(opts.selectedPhase), opts.prefetch, opts.dryRun, varsAsAnyMap(opts.varOverrides))
		})
	}

	workflowPath, bundleRoot, err := resolveApplyWorkflowAndBundle(strings.TrimSpace(opts.workflowPath), positionalArgs)
	if err != nil {
		return err
	}
	return executeApply(workflowPath, bundleRoot, strings.TrimSpace(opts.selectedPhase), opts.prefetch, opts.dryRun, varsAsAnyMap(opts.varOverrides))
}

func executeApply(workflowPath, bundleRoot, selectedPhase string, prefetch, dryRun bool, varOverrides map[string]any) error {
	ctx := context.Background()

	resolvedRequest, err := resolveApplyExecutionRequest(applyExecutionRequestOptions{
		CommandName:                  "apply",
		WorkflowPath:                 workflowPath,
		AllowRemoteWorkflow:          true,
		VarOverrides:                 varOverrides,
		SelectedPhase:                selectedPhase,
		DefaultPhase:                 "install",
		BuildExecutionWorkflow:       true,
		ResolveStatePath:             true,
		StatePathFromExecutionTarget: false,
	})
	if err != nil {
		return err
	}

	wf := resolvedRequest.Workflow
	applyExecutionWorkflow := resolvedRequest.ExecutionWorkflow
	statePath := resolvedRequest.StatePath
	if dryRun {
		return runApplyDryRun(applyExecutionWorkflow, resolvedRequest.SelectedPhase, bundleRoot)
	}

	if prefetch {
		prefetchWorkflow := buildApplyPrefetchWorkflow(wf)
		if len(prefetchWorkflow.Phases) > 0 && len(prefetchWorkflow.Phases[0].Steps) > 0 {
			if err := install.Run(ctx, prefetchWorkflow, install.RunOptions{BundleRoot: bundleRoot, StatePath: statePath}); err != nil {
				return err
			}
		}
	}

	if err := install.Run(ctx, applyExecutionWorkflow, install.RunOptions{BundleRoot: bundleRoot, StatePath: statePath}); err != nil {
		return err
	}

	return stdoutPrintln("apply: ok")
}

func resolveApplyWorkflowAndBundle(fileFlagValue string, positionalArgs []string) (string, string, error) {
	workflowPath := strings.TrimSpace(fileFlagValue)
	positionalWorkflow := ""
	positionalBundle := ""

	if workflowPath != "" {
		if len(positionalArgs) == 2 {
			return "", "", errors.New("apply accepts at most one positional bundle path when --file is set")
		}
		if len(positionalArgs) == 1 {
			positionalBundle = strings.TrimSpace(positionalArgs[0])
		}
	} else {
		if len(positionalArgs) == 1 {
			arg0 := strings.TrimSpace(positionalArgs[0])
			if looksLikeWorkflowReference(arg0) {
				positionalWorkflow = arg0
			} else {
				positionalBundle = arg0
			}
		}
		if len(positionalArgs) == 2 {
			arg0 := strings.TrimSpace(positionalArgs[0])
			arg1 := strings.TrimSpace(positionalArgs[1])
			if !looksLikeWorkflowReference(arg0) {
				return "", "", errors.New("apply with two positional arguments requires [workflow] [bundle]")
			}
			positionalWorkflow = arg0
			positionalBundle = arg1
		}
	}

	if workflowPath == "" {
		workflowPath = positionalWorkflow
	}

	isRemoteWorkflow := isHTTPWorkflowPath(workflowPath)
	bundleRoot := ""

	if !isRemoteWorkflow {
		resolvedBundleRoot, err := resolveApplyBundleRoot(positionalBundle)
		if err != nil {
			return "", "", err
		}
		bundleRoot = resolvedBundleRoot

		if workflowPath == "" {
			resolvedWorkflowPath, err := discoverApplyWorkflow(bundleRoot)
			if err != nil {
				return "", "", err
			}
			workflowPath = resolvedWorkflowPath
		} else {
			resolvedWorkflowPath, err := filepath.Abs(workflowPath)
			if err != nil {
				return "", "", fmt.Errorf("resolve workflow path: %w", err)
			}
			workflowPath = resolvedWorkflowPath
		}
	}

	if workflowPath == "" {
		resolvedBundleRoot, err := resolveApplyBundleRoot(positionalBundle)
		if err != nil {
			return "", "", err
		}
		bundleRoot = resolvedBundleRoot
		resolvedWorkflowPath, err := discoverApplyWorkflow(bundleRoot)
		if err != nil {
			return "", "", err
		}
		workflowPath = resolvedWorkflowPath
	}

	return workflowPath, bundleRoot, nil
}

func looksLikeWorkflowReference(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	if isHTTPWorkflowPath(trimmed) {
		return true
	}
	lower := strings.ToLower(trimmed)
	if strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") {
		return true
	}
	resolved, err := filepath.Abs(trimmed)
	if err != nil {
		return false
	}
	info, statErr := os.Stat(resolved)
	if statErr != nil {
		return false
	}
	return !info.IsDir()
}

func buildApplyPrefetchWorkflow(wf *config.Workflow) *config.Workflow {
	if wf == nil {
		return &config.Workflow{}
	}

	prefetchSteps := make([]config.Step, 0)
	for _, phase := range wf.Phases {
		for _, step := range phase.Steps {
			if step.Kind == "DownloadFile" {
				prefetchSteps = append(prefetchSteps, step)
			}
		}
	}

	if len(prefetchSteps) == 0 {
		return &config.Workflow{}
	}

	return &config.Workflow{
		Role:           wf.Role,
		Version:        wf.Version,
		Vars:           wf.Vars,
		Phases:         []config.Phase{{Name: "install", Steps: prefetchSteps}},
		StateKey:       wf.StateKey,
		WorkflowSHA256: wf.WorkflowSHA256,
	}
}

func buildApplyExecutionWorkflow(wf *config.Workflow, phaseName string) (*config.Workflow, error) {
	if wf == nil {
		return nil, errors.New("workflow is nil")
	}
	selectedPhase, found := findWorkflowPhaseByName(wf, phaseName)
	if !found {
		return nil, fmt.Errorf("%s phase not found", phaseName)
	}

	return &config.Workflow{
		Role:           wf.Role,
		Version:        wf.Version,
		Vars:           wf.Vars,
		Phases:         []config.Phase{{Name: "install", Steps: selectedPhase.Steps}},
		StateKey:       wf.StateKey,
		WorkflowSHA256: wf.WorkflowSHA256,
	}, nil
}

func runApplyDryRun(wf *config.Workflow, selectedPhaseName string, bundleRoot string) error {
	phaseView, found := findWorkflowPhaseByName(wf, "install")
	if !found {
		return fmt.Errorf("%s phase not found", selectedPhaseName)
	}

	if err := stdoutPrintf("PHASE=%s\n", selectedPhaseName); err != nil {
		return err
	}

	state, err := loadInstallDryRunState(wf)
	if err != nil {
		return err
	}

	runtimeVars := map[string]any{}
	for key, value := range state.RuntimeVars {
		runtimeVars[key] = value
	}

	completed := make(map[string]bool, len(state.CompletedSteps))
	for _, stepID := range state.CompletedSteps {
		completed[stepID] = true
	}

	statePath, err := resolveInstallStatePath(wf)
	if err != nil {
		return err
	}
	ctxData := map[string]any{"bundleRoot": bundleRoot, "stateFile": statePath}

	for _, step := range phaseView.Steps {
		if completed[step.ID] {
			if err := stdoutPrintf("%s %s SKIP (completed)\n", step.ID, step.Kind); err != nil {
				return err
			}
			continue
		}

		ok, evalErr := install.EvaluateWhen(step.When, wf.Vars, runtimeVars, ctxData)
		if evalErr != nil {
			return fmt.Errorf("WHEN_EVAL_ERROR: step %s (%s): %w", step.ID, step.Kind, evalErr)
		}

		status := "PLAN"
		if !ok {
			status = "SKIP"
		}
		if err := stdoutPrintf("%s %s %s\n", step.ID, step.Kind, status); err != nil {
			return err
		}
	}

	return nil
}

func isHTTPWorkflowPath(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	return strings.TrimSpace(parsed.Host) != ""
}

func fetchWorkflowForApplyValidation(rawURL string) ([]byte, error) {
	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("get workflow url: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get workflow url: %w", err)
	}
	defer closeSilently(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get workflow url: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read workflow url: %w", err)
	}
	return body, nil
}

func resolveApplyBundleRoot(positionalBundle string) (string, error) {
	if strings.TrimSpace(positionalBundle) != "" {
		return resolveApplyBundleCandidate(positionalBundle, true)
	}

	for _, candidate := range []string{"./bundle.tar", "./bundle", "."} {
		resolved, err := resolveApplyBundleCandidate(candidate, false)
		if err != nil {
			return "", err
		}
		if resolved != "" {
			return resolved, nil
		}
	}

	return "", errors.New("bundle not found: expected positional bundle path, ./bundle.tar, ./bundle, or current directory with workflows/")
}

func resolveApplyBundleCandidate(candidate string, strict bool) (string, error) {
	resolved, err := filepath.Abs(strings.TrimSpace(candidate))
	if err != nil {
		return "", fmt.Errorf("resolve bundle path: %w", err)
	}

	info, err := os.Stat(resolved)
	if err != nil {
		if os.IsNotExist(err) && !strict {
			return "", nil
		}
		if os.IsNotExist(err) {
			return "", fmt.Errorf("bundle path not found: %s", resolved)
		}
		return "", fmt.Errorf("stat bundle path: %w", err)
	}

	if info.IsDir() {
		if hasWorkflowDir(resolved) {
			return resolved, nil
		}
		if strict {
			return "", fmt.Errorf("bundle directory must contain workflows/: %s", resolved)
		}
		return "", nil
	}

	if strings.ToLower(filepath.Ext(resolved)) != ".tar" {
		if strict {
			return "", fmt.Errorf("bundle path must be a directory or .tar archive: %s", resolved)
		}
		return "", nil
	}

	extractedRoot, err := extractApplyBundleArchive(resolved)
	if err != nil {
		return "", err
	}
	return extractedRoot, nil
}

func extractApplyBundleArchive(archivePath string) (string, error) {
	sum, err := sha256FileHex(archivePath)
	if err != nil {
		return "", fmt.Errorf("hash bundle archive: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}

	extractRoot := filepath.Join(home, ".deck", "extract", sum)
	bundleRoot := filepath.Join(extractRoot, "bundle")
	if hasWorkflowDir(bundleRoot) {
		return bundleRoot, nil
	}

	if err := os.RemoveAll(extractRoot); err != nil {
		return "", fmt.Errorf("reset extract cache: %w", err)
	}
	if err := os.MkdirAll(extractRoot, 0o755); err != nil {
		return "", fmt.Errorf("create extract cache directory: %w", err)
	}
	if err := bundle.ImportArchive(archivePath, extractRoot); err != nil {
		return "", err
	}
	if !hasWorkflowDir(bundleRoot) {
		return "", fmt.Errorf("extracted bundle missing workflows/: %s", bundleRoot)
	}

	return bundleRoot, nil
}

func sha256FileHex(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer closeSilently(f)

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hasWorkflowDir(root string) bool {
	workflowDir := filepath.Join(root, "workflows")
	info, err := os.Stat(workflowDir)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func discoverApplyWorkflow(bundleRoot string) (string, error) {
	ctx := context.Background()

	workflowDir := filepath.Join(bundleRoot, "workflows")
	if !hasWorkflowDir(bundleRoot) {
		return "", fmt.Errorf("workflow directory not found: %s", workflowDir)
	}

	preferred := filepath.Join(workflowDir, "apply.yaml")
	if info, err := os.Stat(preferred); err == nil && !info.IsDir() {
		wf, loadErr := config.Load(ctx, preferred)
		if loadErr != nil {
			return "", loadErr
		}
		if strings.TrimSpace(wf.Role) == "pack" {
			return "", fmt.Errorf("apply workflow role must be apply: %s", preferred)
		}
		if strings.TrimSpace(wf.Role) != "apply" {
			return "", fmt.Errorf("apply workflow role must be apply: %s", preferred)
		}
		return preferred, nil
	}

	entries, err := os.ReadDir(workflowDir)
	if err != nil {
		return "", fmt.Errorf("read workflow directory: %w", err)
	}

	matches := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		lower := strings.ToLower(entry.Name())
		if !strings.HasSuffix(lower, ".yaml") && !strings.HasSuffix(lower, ".yml") {
			continue
		}

		candidate := filepath.Join(workflowDir, entry.Name())
		wf, loadErr := config.Load(ctx, candidate)
		if loadErr != nil {
			return "", loadErr
		}
		if strings.TrimSpace(wf.Role) == "apply" {
			matches = append(matches, candidate)
		}
	}

	sort.Strings(matches)
	if len(matches) == 0 {
		return "", fmt.Errorf("apply workflow not found under %s", workflowDir)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("multiple apply workflows found under %s", workflowDir)
	}
	return matches[0], nil
}

func executeValidate(file string) error {
	if file == "" {
		return errors.New("--file (or -f) is required")
	}

	if err := validate.File(file); err != nil {
		return err
	}

	return stdoutPrintln("validate: ok")
}
