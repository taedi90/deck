package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	"github.com/taedi90/deck/internal/diagnose"
	"github.com/taedi90/deck/internal/install"
	ctrllogs "github.com/taedi90/deck/internal/logs"
	"github.com/taedi90/deck/internal/nodeid"
	"github.com/taedi90/deck/internal/prepare"
	"github.com/taedi90/deck/internal/server"
	sitestore "github.com/taedi90/deck/internal/site/store"
	"github.com/taedi90/deck/internal/validate"
)

type varFlag struct {
	values map[string]string
}

func (v *varFlag) String() string {
	if v == nil || len(v.values) == 0 {
		return ""
	}
	parts := make([]string, 0, len(v.values))
	for key, value := range v.values {
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, ",")
}

func (v *varFlag) Set(raw string) error {
	parts := strings.SplitN(raw, "=", 2)
	if len(parts) != 2 {
		return errors.New("--var must be key=value")
	}
	key := strings.TrimSpace(parts[0])
	if key == "" {
		return errors.New("--var must be key=value")
	}
	if v.values == nil {
		v.values = map[string]string{}
	}
	v.values[key] = parts[1]
	return nil
}

func (v *varFlag) AsMap() map[string]string {
	if v == nil || len(v.values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(v.values))
	for key, value := range v.values {
		cloned[key] = value
	}
	return cloned
}

func registerFileFlags(fs *flag.FlagSet, target *string, usage string) {
	fs.StringVar(target, "file", "", usage)
	fs.StringVar(target, "f", "", usage)
}

func registerOutputFormatFlags(fs *flag.FlagSet, target *string, defaultValue string) {
	fs.StringVar(target, "output", defaultValue, "output format (text|json)")
	fs.StringVar(target, "o", defaultValue, "output format (text|json)")
}

func varsAsAnyMap(vars map[string]string) map[string]any {
	if len(vars) == 0 {
		return nil
	}
	converted := make(map[string]any, len(vars))
	for key, value := range vars {
		converted[key] = value
	}
	return converted
}

func runPack(args []string) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help" || args[0] == "help") {
		return errors.New("usage: deck pack [flags]")
	}

	fs := flag.NewFlagSet("pack", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	outPath := fs.String("out", "", "output tar archive path")
	dryRun := fs.Bool("dry-run", false, "print pack plan without writing files")
	cacheDir := fs.String("cache-dir", "", "artifact cache directory")
	noCache := fs.Bool("no-cache", false, "disable artifact cache reuse")
	vars := &varFlag{}
	fs.Var(vars, "var", "set variable override (key=value), repeatable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolvedOut := strings.TrimSpace(*outPath)
	if !*dryRun && resolvedOut == "" {
		return errors.New("--out is required")
	}

	packWorkflowPath, err := discoverPackWorkflow()
	if err != nil {
		return err
	}
	workflowBaseDir := filepath.Dir(packWorkflowPath)
	applyWorkflowPath := filepath.Join(workflowBaseDir, "apply.yaml")
	varsWorkflowPath := filepath.Join(workflowBaseDir, "vars.yaml")
	for _, requiredPath := range []string{applyWorkflowPath, varsWorkflowPath} {
		info, statErr := os.Stat(requiredPath)
		if statErr != nil || info.IsDir() {
			return fmt.Errorf("required workflow file not found: %s", requiredPath)
		}
	}

	if *dryRun {
		fmt.Fprintf(os.Stdout, "PACK_WORKFLOW=%s\n", filepath.ToSlash(packWorkflowPath))
		fmt.Fprintf(os.Stdout, "WORKFLOW_INCLUDE=%s\n", filepath.ToSlash(packWorkflowPath))
		fmt.Fprintf(os.Stdout, "WORKFLOW_INCLUDE=%s\n", filepath.ToSlash(applyWorkflowPath))
		fmt.Fprintf(os.Stdout, "WORKFLOW_INCLUDE=%s\n", filepath.ToSlash(varsWorkflowPath))
		fmt.Fprintln(os.Stdout, "ARTIFACT_DEFAULT_DEST=packages->bundle/packages")
		fmt.Fprintln(os.Stdout, "ARTIFACT_DEFAULT_DEST=images->bundle/images")
		fmt.Fprintln(os.Stdout, "ARTIFACT_DEFAULT_DEST=files->bundle/files")
		fmt.Fprintln(os.Stdout, "WRITE=bundle/deck")
		fmt.Fprintln(os.Stdout, "WRITE=bundle/files/deck")
		fmt.Fprintln(os.Stdout, "WRITE=bundle/.deck/manifest.json")
		if resolvedOut != "" {
			fmt.Fprintf(os.Stdout, "WRITE_TAR=%s\n", resolvedOut)
		}
		return nil
	}

	resolvedOutAbs, err := filepath.Abs(resolvedOut)
	if err != nil {
		return fmt.Errorf("resolve --out: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(resolvedOutAbs), 0o755); err != nil {
		return fmt.Errorf("create output parent: %w", err)
	}

	stagingRoot, err := os.MkdirTemp(filepath.Dir(resolvedOutAbs), "deck-pack-")
	if err != nil {
		return fmt.Errorf("create staging root: %w", err)
	}
	defer os.RemoveAll(stagingRoot)
	bundleRoot := filepath.Join(stagingRoot, "bundle")

	packWorkflow, err := config.LoadWithOptions(packWorkflowPath, config.LoadOptions{VarOverrides: varsAsAnyMap(vars.AsMap())})
	if err != nil {
		return err
	}
	if strings.TrimSpace(packWorkflow.Role) != "pack" {
		return fmt.Errorf("pack workflow role must be pack: %s", packWorkflowPath)
	}

	artifactRoot := bundleRoot
	if !*noCache {
		if strings.TrimSpace(*cacheDir) != "" {
			artifactRoot, err = filepath.Abs(strings.TrimSpace(*cacheDir))
			if err != nil {
				return fmt.Errorf("resolve --cache-dir: %w", err)
			}
		}
	}

	prevForceDownload := os.Getenv("DECK_PREPARE_FORCE_REDOWNLOAD")
	if *noCache {
		if err := os.Setenv("DECK_PREPARE_FORCE_REDOWNLOAD", "1"); err != nil {
			return err
		}
	} else {
		if err := os.Unsetenv("DECK_PREPARE_FORCE_REDOWNLOAD"); err != nil {
			return err
		}
	}
	defer func() {
		if prevForceDownload == "" {
			_ = os.Unsetenv("DECK_PREPARE_FORCE_REDOWNLOAD")
			return
		}
		_ = os.Setenv("DECK_PREPARE_FORCE_REDOWNLOAD", prevForceDownload)
	}()

	if err := prepare.Run(packWorkflow, prepare.RunOptions{BundleRoot: artifactRoot}); err != nil {
		return err
	}

	if artifactRoot != bundleRoot {
		for _, artifactDir := range []string{"packages", "images", "files"} {
			if err := copySubtreeIfExists(filepath.Join(artifactRoot, artifactDir), filepath.Join(bundleRoot, artifactDir)); err != nil {
				return err
			}
		}
	}

	workflowOutDir := filepath.Join(bundleRoot, "workflows")
	if err := os.MkdirAll(workflowOutDir, 0o755); err != nil {
		return fmt.Errorf("create workflow output dir: %w", err)
	}
	if err := copySubtreeIfExists(workflowBaseDir, workflowOutDir); err != nil {
		return err
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve deck binary path: %w", err)
	}
	binaryBytes, err := os.ReadFile(execPath)
	if err != nil {
		return fmt.Errorf("read deck binary: %w", err)
	}
	if err := writeBytes(filepath.Join(bundleRoot, "deck"), binaryBytes, 0o755); err != nil {
		return err
	}
	if err := writeBytes(filepath.Join(bundleRoot, "files", "deck"), binaryBytes, 0o755); err != nil {
		return err
	}

	manifest, err := buildPackManifest(bundleRoot)
	if err != nil {
		return err
	}
	if err := writePackManifest(filepath.Join(bundleRoot, ".deck", "manifest.json"), manifest); err != nil {
		return err
	}

	if err := bundle.CollectArchive(bundleRoot, resolvedOutAbs); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "pack: ok (%s)\n", resolvedOutAbs)
	return nil
}

func discoverPackWorkflow() (string, error) {
	workflowDir := filepath.Join(".", "workflows")
	absWorkflowDir, err := filepath.Abs(workflowDir)
	if err != nil {
		return "", fmt.Errorf("resolve workflow directory: %w", err)
	}
	info, err := os.Stat(absWorkflowDir)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("workflow directory not found: %s", absWorkflowDir)
	}

	preferred := filepath.Join(absWorkflowDir, "pack.yaml")
	if preferredInfo, statErr := os.Stat(preferred); statErr == nil && !preferredInfo.IsDir() {
		wf, loadErr := config.Load(preferred)
		if loadErr != nil {
			return "", loadErr
		}
		if strings.TrimSpace(wf.Role) != "pack" {
			return "", fmt.Errorf("pack workflow role must be pack: %s", preferred)
		}
		return preferred, nil
	}

	entries, err := os.ReadDir(absWorkflowDir)
	if err != nil {
		return "", fmt.Errorf("read workflow directory: %w", err)
	}
	matches := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		candidate := filepath.Join(absWorkflowDir, entry.Name())
		wf, loadErr := config.Load(candidate)
		if loadErr != nil {
			return "", loadErr
		}
		if strings.TrimSpace(wf.Role) == "pack" {
			matches = append(matches, candidate)
		}
	}
	sort.Strings(matches)
	if len(matches) == 0 {
		return "", fmt.Errorf("pack workflow not found under %s", absWorkflowDir)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("multiple pack workflows found under %s", absWorkflowDir)
	}

	return matches[0], nil
}

func copySubtreeIfExists(srcRoot, dstRoot string) error {
	info, err := os.Stat(srcRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}

	return filepath.WalkDir(srcRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dstRoot, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target, 0o644)
	})
}

func copyFile(srcPath, dstPath string, mode os.FileMode) error {
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", srcPath, err)
	}
	return writeBytes(dstPath, raw, mode)
}

func writeBytes(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent directory for %s: %w", path, err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

type packManifest struct {
	Entries []packManifestEntry `json:"entries"`
}

type packManifestEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func buildPackManifest(bundleRoot string) (packManifest, error) {
	entries := make([]packManifestEntry, 0)
	for _, root := range []string{"packages", "images", "files"} {
		rootPath := filepath.Join(bundleRoot, root)
		if _, err := os.Stat(rootPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return packManifest{}, err
		}
		if err := filepath.WalkDir(rootPath, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			info, err := os.Stat(path)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(bundleRoot, path)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(raw)
			entries = append(entries, packManifestEntry{
				Path:   filepath.ToSlash(rel),
				SHA256: hex.EncodeToString(sum[:]),
				Size:   info.Size(),
			})
			return nil
		}); err != nil {
			return packManifest{}, err
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return packManifest{Entries: entries}, nil
}

func writePackManifest(path string, manifest packManifest) error {
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode pack manifest: %w", err)
	}
	return writeBytes(path, raw, 0o644)
}

func runServe(args []string) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help" || args[0] == "help") {
		return errors.New("usage: deck serve [flags]")
	}
	return runServer(append([]string{"start"}, args...))
}

func runList(args []string) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help" || args[0] == "help") {
		return errors.New("usage: deck list [flags]")
	}

	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var server string
	var output string
	fs.StringVar(&server, "server", "", "server URL for index (optional; defaults to local workflows/)")
	registerOutputFormatFlags(fs, &output, "text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if output != "text" && output != "json" {
		return errors.New("--output must be text or json")
	}

	resolvedServer := strings.TrimSpace(server)
	localRoot := "."

	items := []string{}
	if resolvedServer == "" {
		localItems, err := discoverLocalWorkflowList(localRoot)
		if err != nil {
			return err
		}
		items = localItems
	} else {
		remoteItems, err := fetchWorkflowIndexFromServer(resolvedServer)
		if err != nil {
			return err
		}
		items = remoteItems
	}

	if output == "json" {
		enc := json.NewEncoder(os.Stdout)
		if err := enc.Encode(items); err != nil {
			return fmt.Errorf("list: encode output: %w", err)
		}
		return nil
	}

	w := bufio.NewWriter(os.Stdout)
	for _, it := range items {
		if _, err := fmt.Fprintln(w, it); err != nil {
			return err
		}
	}
	return w.Flush()
}

func fetchWorkflowIndexFromServer(server string) ([]string, error) {
	trimmed := strings.TrimRight(server, "/")
	indexURL := trimmed + "/workflows/index.json"
	resp, err := http.Get(indexURL)
	if err != nil {
		return nil, fmt.Errorf("list: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return []string{}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list: unexpected status %d", resp.StatusCode)
	}

	var items []string
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("list: decode response: %w", err)
	}
	return items, nil
}

func discoverLocalWorkflowList(root string) ([]string, error) {
	workflowDir := filepath.Join(root, "workflows")
	info, err := os.Stat(workflowDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("list: local workflows directory not found: %s", workflowDir)
		}
		return nil, fmt.Errorf("list: stat local workflows directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("list: local workflows path is not a directory: %s", workflowDir)
	}

	items := make([]string, 0)
	err = filepath.WalkDir(workflowDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		name := strings.ToLower(d.Name())
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			return nil
		}
		relPath, err := filepath.Rel(workflowDir, path)
		if err != nil {
			return err
		}
		items = append(items, filepath.ToSlash(filepath.Join("workflows", relPath)))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list: read local workflows directory: %w", err)
	}

	sort.Strings(items)
	return items, nil
}

func runDiff(args []string) error {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var file string
	registerFileFlags(fs, &file, "path to workflow file")
	server := ""
	session := ""
	apiToken := ""
	registerAssistedFlags(fs, &server, &session, &apiToken)
	phase := fs.String("phase", "install", "phase name to diff")
	output := ""
	registerOutputFormatFlags(fs, &output, "text")
	vars := &varFlag{}
	fs.Var(vars, "var", "set variable override (key=value), repeatable")
	if err := fs.Parse(args); err != nil {
		return err
	}

	assistedConfig, assistedMode, err := resolveAssistedExecutionConfig(server, session, apiToken)
	if err != nil {
		return err
	}
	workflowPath := strings.TrimSpace(file)
	if assistedMode {
		return runAssistedAction(assistedConfig, "diff", func(ctx assistedExecutionContext) error {
			return executeDiff(ctx.WorkflowPath, strings.TrimSpace(*phase), output, varsAsAnyMap(vars.AsMap()))
		})
	}
	if workflowPath == "" {
		return errors.New("--file (or -f) is required")
	}
	return executeDiff(workflowPath, strings.TrimSpace(*phase), output, varsAsAnyMap(vars.AsMap()))
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
			fmt.Fprintf(os.Stdout, "%s %s SKIP (%s)\n", s.ID, s.Kind, s.Reason)
			continue
		}
		fmt.Fprintf(os.Stdout, "%s %s %s\n", s.ID, s.Kind, strings.ToUpper(s.Action))
	}
	return nil
}

func runCache(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: deck cache list|clean [flags]")
	}
	if args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		return errors.New("usage: deck cache list|clean [flags]")
	}

	switch args[0] {
	case "list":
		return runCacheList(args[1:])
	case "clean":
		return runCacheClean(args[1:])
	default:
		return fmt.Errorf("unknown cache command %q", args[0])
	}
}

func runNode(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: deck node <id|assignment> [flags]")
	}
	if args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		return errors.New("usage: deck node <id|assignment> [flags]")
	}
	switch args[0] {
	case "id":
		return runNodeID(args[1:])
	case "assignment":
		return runNodeAssignment(args[1:])
	default:
		return fmt.Errorf("unknown node command %q", args[0])
	}
}

func runNodeID(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: deck node id <show|set|init>")
	}
	switch args[0] {
	case "show":
		return runNodeIDShow(args[1:])
	case "set":
		return runNodeIDSet(args[1:])
	case "init":
		return runNodeIDInit(args[1:])
	default:
		return fmt.Errorf("unknown node id command %q", args[0])
	}
}

func runNodeIDShow(args []string) error {
	fs := flag.NewFlagSet("node id show", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: deck node id show")
	}

	result, err := nodeid.Resolve(resolveNodeIDPathsFromEnv())
	if err != nil {
		return err
	}
	printNodeIDResult(result)
	return nil
}

func runNodeIDSet(args []string) error {
	fs := flag.NewFlagSet("node id set", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: deck node id set <node-id>")
	}

	result, err := nodeid.SetOperator(resolveNodeIDPathsFromEnv(), strings.TrimSpace(fs.Arg(0)))
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "node id set: %s\n", result.ID)
	printNodeIDResult(result)
	return nil
}

func runNodeIDInit(args []string) error {
	fs := flag.NewFlagSet("node id init", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: deck node id init")
	}

	result, err := nodeid.Init(resolveNodeIDPathsFromEnv())
	if err != nil {
		return err
	}
	if result.GeneratedCreated {
		fmt.Fprintln(os.Stdout, "node id init: created generated node-id")
	} else {
		fmt.Fprintln(os.Stdout, "node id init: generated node-id already exists")
	}
	printNodeIDResult(result)
	return nil
}

func resolveNodeIDPathsFromEnv() nodeid.Paths {
	paths := nodeid.DefaultPaths()
	if operatorPath := strings.TrimSpace(os.Getenv("DECK_NODE_ID_OPERATOR_PATH")); operatorPath != "" {
		paths.OperatorPath = operatorPath
	}
	if generatedPath := strings.TrimSpace(os.Getenv("DECK_NODE_ID_GENERATED_PATH")); generatedPath != "" {
		paths.GeneratedPath = generatedPath
	}
	return paths
}

func printNodeIDResult(result nodeid.Result) {
	fmt.Fprintf(os.Stdout, "node-id=%s\n", result.ID)
	fmt.Fprintf(os.Stdout, "source=%s\n", result.Source)
	fmt.Fprintf(os.Stdout, "hostname=%s\n", result.Hostname)
	if result.Mismatch {
		fmt.Fprintln(os.Stdout, "mismatch=true")
		fmt.Fprintf(os.Stdout, "operator-node-id=%s\n", result.OperatorID)
		fmt.Fprintf(os.Stdout, "generated-node-id=%s\n", result.GeneratedID)
	}
}

func runNodeAssignment(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: deck node assignment show [flags]")
	}
	switch args[0] {
	case "show":
		return runNodeAssignmentShow(args[1:])
	default:
		return fmt.Errorf("unknown node assignment command %q", args[0])
	}
}

func runNodeAssignmentShow(args []string) error {
	fs := flag.NewFlagSet("node assignment show", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	root := fs.String("root", ".", "site server root")
	sessionID := fs.String("session", "", "session id")
	action := fs.String("action", "apply", "assignment action (diff|doctor|apply)")
	output := ""
	registerOutputFormatFlags(fs, &output, "text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: deck node assignment show --session <session-id> [--action diff|doctor|apply] [--root <dir>] [--output text|json]")
	}
	resolvedSessionID := strings.TrimSpace(*sessionID)
	if resolvedSessionID == "" {
		return errors.New("--session is required")
	}
	resolvedAction := strings.TrimSpace(*action)
	if resolvedAction != "diff" && resolvedAction != "doctor" && resolvedAction != "apply" {
		return errors.New("--action must be one of diff|doctor|apply")
	}
	if output != "text" && output != "json" {
		return errors.New("--output must be text or json")
	}

	result, err := nodeid.Resolve(resolveNodeIDPathsFromEnv())
	if err != nil {
		return err
	}
	st, _, err := newSiteStore(strings.TrimSpace(*root))
	if err != nil {
		return err
	}
	assignment, err := st.ResolveAssignment(resolvedSessionID, result.ID, resolvedAction)
	if err != nil {
		return err
	}
	if output == "json" {
		return json.NewEncoder(os.Stdout).Encode(assignment)
	}
	fmt.Fprintf(os.Stdout, "session=%s\n", assignment.SessionID)
	fmt.Fprintf(os.Stdout, "node-id=%s\n", assignment.NodeID)
	fmt.Fprintf(os.Stdout, "assignment=%s\n", assignment.ID)
	fmt.Fprintf(os.Stdout, "role=%s\n", assignment.Role)
	fmt.Fprintf(os.Stdout, "workflow=%s\n", assignment.Workflow)
	return nil
}

type assistedExecutionConfig struct {
	Server   string
	Session  string
	APIToken string
}

type assistedExecutionContext struct {
	Config        assistedExecutionConfig
	NodeID        string
	Hostname      string
	ReleaseID     string
	Assignment    sitestore.Assignment
	WorkflowPath  string
	BundleRoot    string
	Skipped       bool
	SkipReason    string
	LocalReport   string
	ReportStarted time.Time
	ReportEnded   time.Time
}

type assistedManifest struct {
	Entries []packManifestEntry `json:"entries"`
}

func registerAssistedFlags(fs *flag.FlagSet, server *string, session *string, apiToken *string) {
	fs.StringVar(server, "server", "", "site server URL (assisted mode requires --server and --session)")
	fs.StringVar(session, "session", "", "site session id for assisted mode")
	fs.StringVar(apiToken, "api-token", "deck-site-v1", "bearer token for assisted site APIs")
}

func resolveAssistedExecutionConfig(server, session, apiToken string) (assistedExecutionConfig, bool, error) {
	resolved := assistedExecutionConfig{
		Server:   strings.TrimRight(strings.TrimSpace(server), "/"),
		Session:  strings.TrimSpace(session),
		APIToken: strings.TrimSpace(apiToken),
	}
	assistedEnabled := resolved.Server != "" || resolved.Session != ""
	if !assistedEnabled {
		return resolved, false, nil
	}
	if resolved.Server == "" || resolved.Session == "" {
		return assistedExecutionConfig{}, false, errors.New("assisted mode requires both --server and --session")
	}
	if resolved.APIToken == "" {
		return assistedExecutionConfig{}, false, errors.New("--api-token is required in assisted mode")
	}
	return resolved, true, nil
}

func runAssistedAction(config assistedExecutionConfig, action string, execute func(ctx assistedExecutionContext) error) error {
	ctx, err := prepareAssistedExecution(config, action)
	if err != nil {
		return err
	}
	if ctx.Skipped {
		fmt.Fprintf(os.Stdout, "%s: skipped (%s)\n", action, ctx.SkipReason)
		return nil
	}

	start := time.Now().UTC()
	execErr := execute(ctx)
	end := time.Now().UTC()
	status := "ok"
	if execErr != nil {
		status = "failed"
	}

	reportPath, report, err := persistAssistedExecutionReport(ctx, action, status, start, end)
	if err != nil {
		return err
	}
	if err := uploadAssistedExecutionReport(ctx.Config, report); err != nil {
		return fmt.Errorf("%s: report upload transport failure (local report: %s): %w", action, reportPath, err)
	}
	if execErr != nil {
		return execErr
	}
	return nil
}

func prepareAssistedExecution(config assistedExecutionConfig, action string) (assistedExecutionContext, error) {
	nodeResult, err := nodeid.Resolve(resolveNodeIDPathsFromEnv())
	if err != nil {
		return assistedExecutionContext{}, fmt.Errorf("%s assisted: resolve node_id: %w", action, err)
	}

	session, err := fetchAssistedSession(config, config.Session)
	if err != nil {
		return assistedExecutionContext{}, fmt.Errorf("%s assisted: fetch session: %w", action, err)
	}
	releaseID := strings.TrimSpace(session.ReleaseID)
	if releaseID == "" {
		return assistedExecutionContext{}, fmt.Errorf("%s assisted: session %q has no release_id", action, config.Session)
	}

	assignment, found, err := fetchAssistedAssignment(config, config.Session, nodeResult.ID, action)
	if err != nil {
		return assistedExecutionContext{}, fmt.Errorf("%s assisted: fetch assignment: %w", action, err)
	}
	if !found {
		localPath, persistErr := persistAssistedSkippedReport(config, action, nodeResult.ID, nodeResult.Hostname)
		if persistErr != nil {
			return assistedExecutionContext{}, persistErr
		}
		return assistedExecutionContext{
			Config:      config,
			NodeID:      nodeResult.ID,
			Hostname:    nodeResult.Hostname,
			ReleaseID:   releaseID,
			Skipped:     true,
			SkipReason:  "no-assignment",
			LocalReport: localPath,
		}, nil
	}

	workflowRef := strings.TrimSpace(assignment.Workflow)
	if workflowRef == "" {
		return assistedExecutionContext{}, fmt.Errorf("%s assisted: assignment %q has empty workflow", action, assignment.ID)
	}

	bundleRoot := assistedReleaseBundleRoot(releaseID)
	if err := syncAssistedReleaseBundle(config, releaseID, workflowRef, bundleRoot); err != nil {
		return assistedExecutionContext{}, fmt.Errorf("%s assisted: fetch release bundle: %w", action, err)
	}

	workflowPath := filepath.Join(bundleRoot, filepath.FromSlash(workflowRef))
	if info, err := os.Stat(workflowPath); err != nil || info.IsDir() {
		if err != nil {
			return assistedExecutionContext{}, fmt.Errorf("%s assisted: workflow not available in fetched release content: %w", action, err)
		}
		return assistedExecutionContext{}, fmt.Errorf("%s assisted: workflow not available in fetched release content: %s", action, workflowRef)
	}

	return assistedExecutionContext{
		Config:       config,
		NodeID:       nodeResult.ID,
		Hostname:     nodeResult.Hostname,
		ReleaseID:    releaseID,
		Assignment:   assignment,
		WorkflowPath: workflowPath,
		BundleRoot:   bundleRoot,
	}, nil
}

func persistAssistedSkippedReport(config assistedExecutionConfig, action, nodeID, hostname string) (string, error) {
	now := time.Now().UTC()
	report := sitestore.ExecutionReport{
		ID:        fmt.Sprintf("report-%s-%d", action, now.UnixNano()),
		SessionID: config.Session,
		NodeID:    nodeID,
		Hostname:  hostname,
		Action:    action,
		Status:    "skipped",
		StartedAt: now.Format(time.RFC3339),
		EndedAt:   now.Format(time.RFC3339),
	}
	return writeAssistedReportFile(config.Session, nodeID, action, report)
}

func persistAssistedExecutionReport(ctx assistedExecutionContext, action, status string, start, end time.Time) (string, sitestore.ExecutionReport, error) {
	report := sitestore.ExecutionReport{
		ID:           fmt.Sprintf("report-%s-%d", action, end.UnixNano()),
		SessionID:    ctx.Config.Session,
		AssignmentID: strings.TrimSpace(ctx.Assignment.ID),
		NodeID:       ctx.NodeID,
		Hostname:     ctx.Hostname,
		Action:       action,
		WorkflowRef:  strings.TrimSpace(ctx.Assignment.Workflow),
		Status:       status,
		StartedAt:    start.Format(time.RFC3339),
		EndedAt:      end.Format(time.RFC3339),
	}
	path, err := writeAssistedReportFile(ctx.Config.Session, ctx.NodeID, action, report)
	return path, report, err
}

func writeAssistedReportFile(sessionID, nodeID, action string, report sitestore.ExecutionReport) (string, error) {
	reportDir := filepath.Join(assistedDataRoot(), "reports", sessionID, nodeID)
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return "", fmt.Errorf("create local assisted report dir: %w", err)
	}
	name := fmt.Sprintf("%s-%s.json", action, time.Now().UTC().Format("20060102T150405Z"))
	path := filepath.Join(reportDir, name)
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode assisted execution report: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return "", fmt.Errorf("write local assisted report: %w", err)
	}
	return path, nil
}

func uploadAssistedExecutionReport(config assistedExecutionConfig, report sitestore.ExecutionReport) error {
	urlPath := fmt.Sprintf("%s/api/site/v1/sessions/%s/reports", config.Server, url.PathEscape(config.Session))
	raw, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("encode report payload: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, urlPath, strings.NewReader(string(raw)))
	if err != nil {
		return fmt.Errorf("build report request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+config.APIToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, msg)
	}
	return nil
}

func fetchAssistedSession(config assistedExecutionConfig, sessionID string) (sitestore.Session, error) {
	endpoint := fmt.Sprintf("%s/api/site/v1/sessions/%s", config.Server, url.PathEscape(sessionID))
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return sitestore.Session{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+config.APIToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return sitestore.Session{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return sitestore.Session{}, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	out := sitestore.Session{}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return sitestore.Session{}, fmt.Errorf("decode session response: %w", err)
	}
	return out, nil
}

func fetchAssistedAssignment(config assistedExecutionConfig, sessionID, nodeID, action string) (sitestore.Assignment, bool, error) {
	endpoint := fmt.Sprintf("%s/api/site/v1/sessions/%s/assignment?node_id=%s&action=%s", config.Server, url.PathEscape(sessionID), url.QueryEscape(nodeID), url.QueryEscape(action))
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return sitestore.Assignment{}, false, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+config.APIToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return sitestore.Assignment{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		if strings.Contains(string(body), "no assignment matched") {
			return sitestore.Assignment{}, false, nil
		}
		return sitestore.Assignment{}, false, fmt.Errorf("unexpected status 404: %s", strings.TrimSpace(string(body)))
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return sitestore.Assignment{}, false, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	assignment := sitestore.Assignment{}
	if err := json.NewDecoder(resp.Body).Decode(&assignment); err != nil {
		return sitestore.Assignment{}, false, fmt.Errorf("decode assignment response: %w", err)
	}
	return assignment, true, nil
}

func syncAssistedReleaseBundle(config assistedExecutionConfig, releaseID, workflowRef, bundleRoot string) error {
	relPaths := map[string]struct{}{
		".deck/manifest.json": {},
		workflowRef:           {},
		"workflows/vars.yaml": {},
	}

	manifestRaw, err := fetchAssistedReleaseBundleFile(config, releaseID, ".deck/manifest.json")
	if err != nil {
		return err
	}
	manifest := assistedManifest{}
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		return fmt.Errorf("decode release manifest: %w", err)
	}
	for _, entry := range manifest.Entries {
		rel := strings.TrimSpace(entry.Path)
		if rel == "" {
			continue
		}
		relPaths[rel] = struct{}{}
	}

	for relPath := range relPaths {
		if err := writeAssistedBundleFile(config, releaseID, bundleRoot, relPath); err != nil {
			if relPath == "workflows/vars.yaml" && strings.Contains(err.Error(), "status 404") {
				continue
			}
			return err
		}
	}
	return nil
}

func writeAssistedBundleFile(config assistedExecutionConfig, releaseID, bundleRoot, relPath string) error {
	clean := filepath.Clean(filepath.FromSlash(relPath))
	if clean == "." || clean == "" || strings.HasPrefix(clean, "..") {
		return fmt.Errorf("invalid release bundle path %q", relPath)
	}
	raw, err := fetchAssistedReleaseBundleFile(config, releaseID, filepath.ToSlash(clean))
	if err != nil {
		return err
	}
	absPath := filepath.Join(bundleRoot, clean)
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return fmt.Errorf("create release bundle path: %w", err)
	}
	if err := os.WriteFile(absPath, raw, 0o644); err != nil {
		return fmt.Errorf("write release bundle file: %w", err)
	}
	return nil
}

func fetchAssistedReleaseBundleFile(config assistedExecutionConfig, releaseID, relPath string) ([]byte, error) {
	endpoint := fmt.Sprintf("%s/site/releases/%s/bundle/%s", config.Server, url.PathEscape(releaseID), relPath)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build release bundle request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+config.APIToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("release bundle fetch %q failed: status %d: %s", relPath, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read release bundle response: %w", err)
	}
	return raw, nil
}

func assistedDataRoot() string {
	if raw := strings.TrimSpace(os.Getenv("DECK_ASSISTED_ROOT")); raw != "" {
		return raw
	}
	return "/var/lib/deck"
}

func assistedReleaseBundleRoot(releaseID string) string {
	return filepath.Join(assistedDataRoot(), "releases", releaseID, "bundle")
}

type cacheEntry struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	ModTime   string `json:"mod_time"`
}

func runCacheList(args []string) error {
	fs := flag.NewFlagSet("cache list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	output := ""
	registerOutputFormatFlags(fs, &output, "text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if output != "text" && output != "json" {
		return errors.New("--output must be text or json")
	}

	root, err := defaultDeckCacheRoot()
	if err != nil {
		return err
	}
	entries, err := listCacheEntries(root)
	if err != nil {
		return err
	}
	if output == "json" {
		enc := json.NewEncoder(os.Stdout)
		return enc.Encode(entries)
	}
	for _, e := range entries {
		fmt.Fprintf(os.Stdout, "%s\t%d\t%s\n", e.Path, e.SizeBytes, e.ModTime)
	}
	return nil
}

func runCacheClean(args []string) error {
	fs := flag.NewFlagSet("cache clean", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	olderThan := fs.String("older-than", "", "delete entries not modified within this duration (e.g. 30d, 24h)")
	dryRun := fs.Bool("dry-run", false, "print deletion plan without deleting")
	if err := fs.Parse(args); err != nil {
		return err
	}

	root, err := defaultDeckCacheRoot()
	if err != nil {
		return err
	}
	cutoff, hasCutoff, err := parseOlderThan(*olderThan)
	if err != nil {
		return err
	}
	plan, err := computeCacheCleanPlan(root, cutoff, hasCutoff)
	if err != nil {
		return err
	}
	for _, p := range plan {
		fmt.Fprintln(os.Stdout, p)
	}
	if *dryRun {
		return nil
	}
	for _, p := range plan {
		if err := os.RemoveAll(p); err != nil {
			return fmt.Errorf("delete %s: %w", p, err)
		}
	}
	return nil
}

func defaultDeckCacheRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}
	return filepath.Join(home, ".deck", "cache"), nil
}

func listCacheEntries(root string) ([]cacheEntry, error) {
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return []cacheEntry{}, nil
		}
		return nil, fmt.Errorf("stat cache root: %w", err)
	}
	entries := []cacheEntry{}
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		entries = append(entries, cacheEntry{
			Path:      filepath.ToSlash(rel),
			SizeBytes: info.Size(),
			ModTime:   info.ModTime().UTC().Format(time.RFC3339),
		})
		return nil
	}); err != nil {
		return nil, fmt.Errorf("walk cache root: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

func parseOlderThan(raw string) (time.Time, bool, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, false, nil
	}
	var dur time.Duration
	if strings.HasSuffix(trimmed, "d") {
		n := strings.TrimSuffix(trimmed, "d")
		days, err := strconv.ParseInt(n, 10, 64)
		if err != nil || days < 0 {
			return time.Time{}, false, fmt.Errorf("invalid --older-than: %s", trimmed)
		}
		dur = time.Duration(days) * 24 * time.Hour
	} else {
		parsed, err := time.ParseDuration(trimmed)
		if err != nil || parsed < 0 {
			return time.Time{}, false, fmt.Errorf("invalid --older-than: %s", trimmed)
		}
		dur = parsed
	}
	return time.Now().Add(-dur), true, nil
}

func computeCacheCleanPlan(root string, cutoff time.Time, hasCutoff bool) ([]string, error) {
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("stat cache root: %w", err)
	}
	plan := []string{}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read cache root: %w", err)
	}
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		if !hasCutoff {
			plan = append(plan, path)
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if info.ModTime().Before(cutoff) {
			plan = append(plan, path)
		}
	}
	sort.Strings(plan)
	return plan, nil
}

func runSite(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: deck site <release|session|assign|status> [flags]")
	}
	if args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		return errors.New("usage: deck site <release|session|assign|status> [flags]")
	}

	switch args[0] {
	case "release":
		return runSiteRelease(args[1:])
	case "session":
		return runSiteSession(args[1:])
	case "assign":
		return runSiteAssign(args[1:])
	case "status":
		return runSiteStatus(args[1:])
	default:
		return fmt.Errorf("unknown site command %q", args[0])
	}
}

func runSiteRelease(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: deck site release <import|list> [flags]")
	}
	switch args[0] {
	case "import":
		return runSiteReleaseImport(args[1:])
	case "list":
		return runSiteReleaseList(args[1:])
	default:
		return fmt.Errorf("unknown site release command %q", args[0])
	}
}

func runSiteReleaseImport(args []string) error {
	fs := flag.NewFlagSet("site release import", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	root := fs.String("root", ".", "site server root")
	releaseID := fs.String("id", "", "release id")
	bundlePath := fs.String("bundle", "", "local bundle archive path")
	createdAt := fs.String("created-at", "", "release timestamp (RFC3339, optional)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: deck site release import --id <release-id> --bundle <bundle.tar> [--root <dir>]")
	}

	resolvedReleaseID := strings.TrimSpace(*releaseID)
	if resolvedReleaseID == "" {
		return errors.New("--id is required")
	}
	resolvedBundlePath := strings.TrimSpace(*bundlePath)
	if resolvedBundlePath == "" {
		return errors.New("--bundle is required")
	}
	resolvedCreatedAt := strings.TrimSpace(*createdAt)
	if resolvedCreatedAt == "" {
		resolvedCreatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	st, resolvedRoot, err := newSiteStore(strings.TrimSpace(*root))
	if err != nil {
		return err
	}

	bundleSHA256, err := sha256FileHex(resolvedBundlePath)
	if err != nil {
		return fmt.Errorf("site release import: read bundle hash: %w", err)
	}

	importRoot, err := os.MkdirTemp("", "deck-site-release-")
	if err != nil {
		return fmt.Errorf("site release import: create temp dir: %w", err)
	}
	defer os.RemoveAll(importRoot)

	if err := bundle.ImportArchive(resolvedBundlePath, importRoot); err != nil {
		return fmt.Errorf("site release import: %w", err)
	}
	importedBundlePath := filepath.Join(importRoot, "bundle")
	if !hasWorkflowDir(importedBundlePath) {
		return fmt.Errorf("site release import: extracted bundle missing workflows/: %s", importedBundlePath)
	}

	if err := st.ImportRelease(sitestore.Release{
		ID:           resolvedReleaseID,
		BundleSHA256: bundleSHA256,
		CreatedAt:    resolvedCreatedAt,
	}, importedBundlePath); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "site release import: ok (release=%s bundle=%s store=%s)\n", resolvedReleaseID, resolvedBundlePath, resolvedRoot)
	return nil
}

func runSiteReleaseList(args []string) error {
	fs := flag.NewFlagSet("site release list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	root := fs.String("root", ".", "site server root")
	output := ""
	registerOutputFormatFlags(fs, &output, "text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: deck site release list [--root <dir>] [--output text|json]")
	}
	if output != "text" && output != "json" {
		return errors.New("--output must be text or json")
	}

	st, _, err := newSiteStore(strings.TrimSpace(*root))
	if err != nil {
		return err
	}
	releases, err := st.ListReleases()
	if err != nil {
		return err
	}

	if output == "json" {
		return json.NewEncoder(os.Stdout).Encode(releases)
	}
	for _, release := range releases {
		if _, err := fmt.Fprintf(os.Stdout, "%s\t%s\t%s\n", release.ID, release.CreatedAt, release.BundleSHA256); err != nil {
			return err
		}
	}
	return nil
}

func runSiteSession(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: deck site session <create|close> [flags]")
	}
	switch args[0] {
	case "create":
		return runSiteSessionCreate(args[1:])
	case "close":
		return runSiteSessionClose(args[1:])
	default:
		return fmt.Errorf("unknown site session command %q", args[0])
	}
}

func runSiteSessionCreate(args []string) error {
	fs := flag.NewFlagSet("site session create", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	root := fs.String("root", ".", "site server root")
	sessionID := fs.String("id", "", "session id")
	releaseID := fs.String("release", "", "release id")
	startedAt := fs.String("started-at", "", "session start timestamp (RFC3339, optional)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: deck site session create --id <session-id> --release <release-id> [--root <dir>]")
	}

	resolvedSessionID := strings.TrimSpace(*sessionID)
	if resolvedSessionID == "" {
		return errors.New("--id is required")
	}
	resolvedReleaseID := strings.TrimSpace(*releaseID)
	if resolvedReleaseID == "" {
		return errors.New("--release is required")
	}
	resolvedStartedAt := strings.TrimSpace(*startedAt)
	if resolvedStartedAt == "" {
		resolvedStartedAt = time.Now().UTC().Format(time.RFC3339)
	}

	st, _, err := newSiteStore(strings.TrimSpace(*root))
	if err != nil {
		return err
	}
	if err := ensureReleaseExists(st, resolvedReleaseID); err != nil {
		return err
	}
	if err := st.CreateSession(sitestore.Session{
		ID:        resolvedSessionID,
		ReleaseID: resolvedReleaseID,
		Status:    "open",
		StartedAt: resolvedStartedAt,
	}); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "site session create: ok (session=%s release=%s)\n", resolvedSessionID, resolvedReleaseID)
	return nil
}

func runSiteSessionClose(args []string) error {
	fs := flag.NewFlagSet("site session close", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	root := fs.String("root", ".", "site server root")
	sessionID := fs.String("id", "", "session id")
	closedAt := fs.String("closed-at", "", "session close timestamp (RFC3339, optional)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: deck site session close --id <session-id> [--root <dir>]")
	}

	resolvedSessionID := strings.TrimSpace(*sessionID)
	if resolvedSessionID == "" {
		return errors.New("--id is required")
	}
	resolvedClosedAt := strings.TrimSpace(*closedAt)
	if resolvedClosedAt == "" {
		resolvedClosedAt = time.Now().UTC().Format(time.RFC3339)
	}

	st, _, err := newSiteStore(strings.TrimSpace(*root))
	if err != nil {
		return err
	}
	closed, err := st.CloseSession(resolvedSessionID, resolvedClosedAt)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "site session close: ok (session=%s status=%s)\n", closed.ID, closed.Status)
	return nil
}

func runSiteAssign(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: deck site assign <role|node> [flags]")
	}
	switch args[0] {
	case "role":
		return runSiteAssignRole(args[1:])
	case "node":
		return runSiteAssignNode(args[1:])
	default:
		return fmt.Errorf("unknown site assign command %q", args[0])
	}
}

func runSiteAssignRole(args []string) error {
	fs := flag.NewFlagSet("site assign role", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	root := fs.String("root", ".", "site server root")
	sessionID := fs.String("session", "", "session id")
	assignmentID := fs.String("assignment", "", "assignment id")
	role := fs.String("role", "", "role")
	workflow := fs.String("workflow", "", "workflow path inside release bundle")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: deck site assign role --session <session-id> --assignment <assignment-id> --role <role> --workflow <path> [--root <dir>]")
	}

	resolvedSessionID := strings.TrimSpace(*sessionID)
	if resolvedSessionID == "" {
		return errors.New("--session is required")
	}
	resolvedAssignmentID := strings.TrimSpace(*assignmentID)
	if resolvedAssignmentID == "" {
		return errors.New("--assignment is required")
	}
	resolvedRole := strings.TrimSpace(*role)
	if resolvedRole == "" {
		return errors.New("--role is required")
	}
	resolvedWorkflow, err := normalizeWorkflowRef(strings.TrimSpace(*workflow))
	if err != nil {
		return err
	}

	st, resolvedRoot, err := newSiteStore(strings.TrimSpace(*root))
	if err != nil {
		return err
	}
	resolvedSession, err := validateSiteAssignmentTarget(st, resolvedRoot, resolvedSessionID, resolvedWorkflow)
	if err != nil {
		return err
	}

	if err := st.SaveAssignment(resolvedSession.ID, sitestore.Assignment{
		ID:        resolvedAssignmentID,
		SessionID: resolvedSession.ID,
		Role:      resolvedRole,
		Workflow:  resolvedWorkflow,
	}); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "site assign role: ok (session=%s assignment=%s role=%s)\n", resolvedSession.ID, resolvedAssignmentID, resolvedRole)
	return nil
}

func runSiteAssignNode(args []string) error {
	fs := flag.NewFlagSet("site assign node", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	root := fs.String("root", ".", "site server root")
	sessionID := fs.String("session", "", "session id")
	assignmentID := fs.String("assignment", "", "assignment id")
	nodeID := fs.String("node", "", "node id")
	role := fs.String("role", "", "role (optional)")
	workflow := fs.String("workflow", "", "workflow path inside release bundle")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: deck site assign node --session <session-id> --assignment <assignment-id> --node <node-id> --workflow <path> [--role <role>] [--root <dir>]")
	}

	resolvedSessionID := strings.TrimSpace(*sessionID)
	if resolvedSessionID == "" {
		return errors.New("--session is required")
	}
	resolvedAssignmentID := strings.TrimSpace(*assignmentID)
	if resolvedAssignmentID == "" {
		return errors.New("--assignment is required")
	}
	resolvedNodeID := strings.TrimSpace(*nodeID)
	if resolvedNodeID == "" {
		return errors.New("--node is required")
	}
	resolvedRole := strings.TrimSpace(*role)
	resolvedWorkflow, err := normalizeWorkflowRef(strings.TrimSpace(*workflow))
	if err != nil {
		return err
	}

	st, resolvedRoot, err := newSiteStore(strings.TrimSpace(*root))
	if err != nil {
		return err
	}
	resolvedSession, err := validateSiteAssignmentTarget(st, resolvedRoot, resolvedSessionID, resolvedWorkflow)
	if err != nil {
		return err
	}

	if err := st.SaveAssignment(resolvedSession.ID, sitestore.Assignment{
		ID:        resolvedAssignmentID,
		SessionID: resolvedSession.ID,
		NodeID:    resolvedNodeID,
		Role:      resolvedRole,
		Workflow:  resolvedWorkflow,
	}); err != nil {
		return err
	}

	resolvedAssignment, err := st.ResolveAssignment(resolvedSession.ID, resolvedNodeID, resolvedRole)
	if err != nil {
		return err
	}
	if resolvedAssignment.ID != resolvedAssignmentID {
		return fmt.Errorf("site assign node: node assignment did not take precedence for session %q node %q", resolvedSession.ID, resolvedNodeID)
	}

	fmt.Fprintf(os.Stdout, "site assign node: ok (session=%s assignment=%s node=%s)\n", resolvedSession.ID, resolvedAssignmentID, resolvedNodeID)
	return nil
}

type siteSessionStatus struct {
	Session sitestore.Session                  `json:"session"`
	Status  sitestore.SessionStatusAggregation `json:"status"`
}

type siteStatusOutput struct {
	Releases int                 `json:"releases"`
	Sessions []siteSessionStatus `json:"sessions"`
}

func runSiteStatus(args []string) error {
	fs := flag.NewFlagSet("site status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	root := fs.String("root", ".", "site server root")
	output := ""
	registerOutputFormatFlags(fs, &output, "text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: deck site status [--root <dir>] [--output text|json]")
	}
	if output != "text" && output != "json" {
		return errors.New("--output must be text or json")
	}

	st, _, err := newSiteStore(strings.TrimSpace(*root))
	if err != nil {
		return err
	}
	releases, err := st.ListReleases()
	if err != nil {
		return err
	}
	sessions, err := st.ListSessions()
	if err != nil {
		return err
	}

	status := siteStatusOutput{
		Releases: len(releases),
		Sessions: make([]siteSessionStatus, 0, len(sessions)),
	}
	for _, session := range sessions {
		aggregated, err := st.SessionStatusAggregation(session.ID)
		if err != nil {
			return err
		}
		status.Sessions = append(status.Sessions, siteSessionStatus{
			Session: session,
			Status:  aggregated,
		})
	}

	if output == "json" {
		return json.NewEncoder(os.Stdout).Encode(status)
	}

	fmt.Fprintf(os.Stdout, "site status: releases=%d sessions=%d\n", status.Releases, len(status.Sessions))
	for _, session := range status.Sessions {
		fmt.Fprintf(os.Stdout, "session %s release=%s status=%s\n", session.Session.ID, session.Session.ReleaseID, session.Session.Status)
		nodeIDs := make([]string, 0, len(session.Status.Nodes))
		for nodeID := range session.Status.Nodes {
			nodeIDs = append(nodeIDs, nodeID)
		}
		sort.Strings(nodeIDs)
		for _, nodeID := range nodeIDs {
			node := session.Status.Nodes[nodeID]
			fmt.Fprintf(os.Stdout, "  node %s hostname=%s diff=%s doctor=%s apply=%s\n", node.NodeID, node.Hostname, node.Actions.Diff, node.Actions.Doctor, node.Actions.Apply)
		}
		fmt.Fprintf(os.Stdout, "  groups diff(ok=%v failed=%v skipped=%v not-run=%v) doctor(ok=%v failed=%v skipped=%v not-run=%v) apply(ok=%v failed=%v skipped=%v not-run=%v)\n",
			session.Status.Groups.Diff.OK,
			session.Status.Groups.Diff.Failed,
			session.Status.Groups.Diff.Skipped,
			session.Status.Groups.Diff.NotRun,
			session.Status.Groups.Doctor.OK,
			session.Status.Groups.Doctor.Failed,
			session.Status.Groups.Doctor.Skipped,
			session.Status.Groups.Doctor.NotRun,
			session.Status.Groups.Apply.OK,
			session.Status.Groups.Apply.Failed,
			session.Status.Groups.Apply.Skipped,
			session.Status.Groups.Apply.NotRun,
		)
	}
	return nil
}

func newSiteStore(root string) (*sitestore.Store, string, error) {
	resolvedRoot := strings.TrimSpace(root)
	if resolvedRoot == "" {
		resolvedRoot = "."
	}
	absRoot, err := filepath.Abs(resolvedRoot)
	if err != nil {
		return nil, "", fmt.Errorf("resolve --root: %w", err)
	}
	st, err := sitestore.New(absRoot)
	if err != nil {
		return nil, "", err
	}
	return st, absRoot, nil
}

func validateSiteAssignmentTarget(st *sitestore.Store, siteRoot, sessionID, workflowRef string) (sitestore.Session, error) {
	session, found, err := st.GetSession(sessionID)
	if err != nil {
		return sitestore.Session{}, err
	}
	if !found {
		return sitestore.Session{}, fmt.Errorf("session %q not found", sessionID)
	}
	if strings.EqualFold(strings.TrimSpace(session.Status), "closed") {
		return sitestore.Session{}, fmt.Errorf("session %q is closed", sessionID)
	}
	if strings.TrimSpace(session.ReleaseID) == "" {
		return sitestore.Session{}, fmt.Errorf("session %q has no release_id", sessionID)
	}
	if err := ensureReleaseExists(st, session.ReleaseID); err != nil {
		return sitestore.Session{}, err
	}

	releaseWorkflowPath := filepath.Join(siteRoot, ".deck", "site", "releases", session.ReleaseID, "bundle", filepath.FromSlash(workflowRef))
	info, err := os.Stat(releaseWorkflowPath)
	if err != nil {
		if os.IsNotExist(err) {
			return sitestore.Session{}, fmt.Errorf("workflow %q not found in release %q", workflowRef, session.ReleaseID)
		}
		return sitestore.Session{}, fmt.Errorf("stat workflow %q in release %q: %w", workflowRef, session.ReleaseID, err)
	}
	if info.IsDir() {
		return sitestore.Session{}, fmt.Errorf("workflow %q in release %q must be a file", workflowRef, session.ReleaseID)
	}
	return session, nil
}

func ensureReleaseExists(st *sitestore.Store, releaseID string) error {
	_, found, err := st.GetRelease(releaseID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("release %q not found", releaseID)
	}
	return nil
}

func normalizeWorkflowRef(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("--workflow is required")
	}
	cleaned := filepath.ToSlash(filepath.Clean(trimmed))
	if strings.HasPrefix(cleaned, "/") || cleaned == "." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("--workflow must be a relative path inside release bundle")
	}
	return cleaned, nil
}

func runHealth(args []string) error {
	fs := flag.NewFlagSet("health", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	server := fs.String("server", "", "server base URL (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolvedServer := strings.TrimSpace(*server)
	if resolvedServer == "" {
		return errors.New("--server is required (assisted mode is explicit: deck health --server <url>)")
	}

	client := &http.Client{Timeout: 5 * time.Second}
	healthURL := strings.TrimRight(resolvedServer, "/") + "/healthz"
	resp, err := client.Get(healthURL)
	if err != nil {
		return fmt.Errorf("health: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health: unexpected status %d", resp.StatusCode)
	}

	fmt.Fprintf(os.Stdout, "health: ok (%s)\n", resolvedServer)
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

func runDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var file string
	registerFileFlags(fs, &file, "path or URL to workflow file")
	server := ""
	session := ""
	apiToken := ""
	registerAssistedFlags(fs, &server, &session, &apiToken)
	out := fs.String("out", "", "output report path (required)")
	vars := &varFlag{}
	fs.Var(vars, "var", "set variable override (key=value), repeatable")
	if err := fs.Parse(args); err != nil {
		return err
	}

	resolvedOut := strings.TrimSpace(*out)
	assistedConfig, assistedMode, err := resolveAssistedExecutionConfig(server, session, apiToken)
	if err != nil {
		return err
	}
	if resolvedOut == "" && !assistedMode {
		return errors.New("--out is required")
	}
	if resolvedOut == "" && assistedMode {
		resolvedOut = filepath.Join(assistedDataRoot(), "reports", strings.TrimSpace(session), "doctor-local.json")
	}

	if assistedMode {
		return runAssistedAction(assistedConfig, "doctor", func(ctx assistedExecutionContext) error {
			return executeDoctor(ctx.WorkflowPath, varsAsAnyMap(vars.AsMap()), resolvedOut)
		})
	}

	return executeDoctor(strings.TrimSpace(file), varsAsAnyMap(vars.AsMap()), resolvedOut)
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

	fmt.Fprintf(os.Stdout, "doctor: wrote %s\n", resolvedOut)
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

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return values
	}
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, v := range values {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func runLogs(args []string) error {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	root := fs.String("root", ".", "serve root directory")
	source := fs.String("source", "file", "log source (file|journal|both)")
	path := fs.String("path", "", "explicit audit log file path")
	unit := fs.String("unit", "", "systemd unit for journal logs")
	output := ""
	registerOutputFormatFlags(fs, &output, "text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolvedSource := strings.ToLower(strings.TrimSpace(*source))
	if resolvedSource != "file" && resolvedSource != "journal" && resolvedSource != "both" {
		return errors.New("--source must be file, journal, or both")
	}
	if output != "text" && output != "json" {
		return errors.New("--output must be text or json")
	}

	records := []ctrllogs.LogRecord{}
	if resolvedSource == "file" || resolvedSource == "both" {
		logPath, err := resolveLogsFilePath(strings.TrimSpace(*root), strings.TrimSpace(*path))
		if err != nil {
			return err
		}
		fileRecords, err := readLogsFile(logPath)
		if err != nil {
			return err
		}
		records = append(records, fileRecords...)
	}
	if resolvedSource == "journal" || resolvedSource == "both" {
		resolvedUnit := strings.TrimSpace(*unit)
		if resolvedUnit == "" {
			return errors.New("--unit is required when --source includes journal")
		}
		journalRecords, err := readControlLogsJournal(resolvedUnit, 50, 0)
		if err != nil {
			return fmt.Errorf("logs: %w\nsuggestion: %s", err, suggestJournalctlCommand(resolvedUnit))
		}
		records = append(records, journalRecords...)
	}

	if output == "json" {
		enc := json.NewEncoder(os.Stdout)
		return enc.Encode(records)
	}
	for _, record := range records {
		fmt.Fprintln(os.Stdout, ctrllogs.FormatLogText(record))
	}
	return nil
}

func resolveLogsFilePath(root string, path string) (string, error) {
	if strings.TrimSpace(path) != "" {
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				return "", fmt.Errorf("logs: log file not found: %s", path)
			}
			return "", fmt.Errorf("logs: stat log file: %w", err)
		}
		return path, nil
	}
	resolvedRoot := strings.TrimSpace(root)
	if resolvedRoot == "" {
		resolvedRoot = "."
	}
	logPath := filepath.Join(resolvedRoot, ".deck", "logs", "server-audit.log")
	if _, err := os.Stat(logPath); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("logs: log file not found: %s", logPath)
		}
		return "", fmt.Errorf("logs: stat log file: %w", err)
	}
	return logPath, nil
}

func readLogsFile(path string) ([]ctrllogs.LogRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("logs: open log file: %w", err)
	}
	defer f.Close()

	records := []ctrllogs.LogRecord{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		record, parseErr := ctrllogs.NormalizeJSONLine([]byte(line))
		if parseErr != nil {
			continue
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("logs: read log file: %w", err)
	}
	return records, nil
}

func resolveControlLogFilePath(cliPath string) (string, error) {
	if cliPath != "" {
		if _, err := os.Stat(cliPath); err != nil {
			if os.IsNotExist(err) {
				return "", fmt.Errorf("control logs: log file not found: %s", cliPath)
			}
			return "", fmt.Errorf("control logs: stat log file: %w", err)
		}
		return cliPath, nil
	}
	candidates := []string{
		filepath.Join(".", "bundle", ".deck", "logs", "server-audit.log"),
		filepath.Join(".", ".deck", "logs", "server-audit.log"),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", errors.New("control logs: server audit log not found (use --path)")
}

func readControlLogsFile(path string, since time.Duration) ([]ctrllogs.LogRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("control logs: open log file: %w", err)
	}
	defer f.Close()

	var cutoff time.Time
	if since > 0 {
		cutoff = time.Now().Add(-since)
	}

	records := []ctrllogs.LogRecord{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		record, parseErr := ctrllogs.NormalizeJSONLine([]byte(line))
		if parseErr != nil {
			continue
		}
		if since > 0 && !recordAfterCutoff(record, cutoff) {
			continue
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("control logs: read log file: %w", err)
	}
	return records, nil
}

func readControlLogsJournal(unit string, tail int, since time.Duration) ([]ctrllogs.LogRecord, error) {
	args := []string{"-u", unit, "-o", "json", "--no-pager", "-n", strconv.Itoa(tail)}
	if since > 0 {
		args = append(args, "--since", formatJournalSince(since))
	}
	raw, err := exec.Command("journalctl", args...).CombinedOutput()
	if err != nil {
		return nil, classifyJournalctlError(err, strings.TrimSpace(string(raw)))
	}
	return parseJournalOutputLines(raw), nil
}

func followControlLogsJournal(unit string, tail int, since time.Duration, filters ctrllogs.LogFilters, output string) error {
	args := []string{"-u", unit, "-o", "json", "--no-pager", "-n", strconv.Itoa(tail), "-f"}
	if since > 0 {
		args = append(args, "--since", formatJournalSince(since))
	}
	cmd := exec.Command("journalctl", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("control logs: journal stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("control logs: journal stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		base := classifyJournalctlError(err, "")
		return fmt.Errorf("control logs: %w\nsuggestion: %s", base, suggestJournalctlCommand(unit))
	}

	stderrCh := make(chan string, 1)
	go func() {
		buf, _ := io.ReadAll(stderr)
		stderrCh <- strings.TrimSpace(string(buf))
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		record := ctrllogs.NormalizeJournalRecord(raw)
		if !ctrllogs.MatchesLogFilters(record, filters) {
			continue
		}
		if err := printControlLogRecord(record, output); err != nil {
			return err
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return fmt.Errorf("control logs: follow journal: %w", scanErr)
	}
	waitErr := cmd.Wait()
	stderrText := <-stderrCh
	if waitErr != nil {
		base := classifyJournalctlError(waitErr, stderrText)
		return fmt.Errorf("control logs: %w\nsuggestion: %s", base, suggestJournalctlCommand(unit))
	}
	return nil
}

func followControlLogsFile(path string, filters ctrllogs.LogFilters, output string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("control logs: stat log file: %w", err)
	}
	offset := info.Size()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		if _, err := f.Seek(offset, 0); err != nil {
			_ = f.Close()
			continue
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			record, parseErr := ctrllogs.NormalizeJSONLine([]byte(line))
			if parseErr != nil {
				continue
			}
			if !ctrllogs.MatchesLogFilters(record, filters) {
				continue
			}
			if err := printControlLogRecord(record, output); err != nil {
				_ = f.Close()
				return err
			}
		}
		nextOffset, err := f.Seek(0, 1)
		if err == nil {
			offset = nextOffset
		}
		_ = f.Close()
	}
	return nil
}

func parseJournalOutputLines(raw []byte) []ctrllogs.LogRecord {
	records := []ctrllogs.LogRecord{}
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		records = append(records, ctrllogs.NormalizeJournalRecord(entry))
	}
	return records
}

func classifyJournalctlError(err error, output string) error {
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return errors.New("journalctl not found")
	}
	if isPermissionError(output) {
		return errors.New("journalctl permission denied")
	}
	if output != "" {
		return fmt.Errorf("journalctl failed: %s", output)
	}
	return fmt.Errorf("journalctl failed: %w", err)
}

func suggestJournalctlCommand(unit string) string {
	return fmt.Sprintf("sudo journalctl -u %s --no-pager -n 50", unit)
}

func formatJournalSince(since time.Duration) string {
	return time.Now().Add(-since).Format(time.RFC3339)
}

func recordAfterCutoff(record ctrllogs.LogRecord, cutoff time.Time) bool {
	ts := strings.TrimSpace(record.TS)
	if ts == "" {
		return true
	}
	parsed, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return true
	}
	return !parsed.Before(cutoff)
}

func filterControlLogRecords(records []ctrllogs.LogRecord, filters ctrllogs.LogFilters) []ctrllogs.LogRecord {
	filtered := make([]ctrllogs.LogRecord, 0, len(records))
	for _, record := range records {
		if ctrllogs.MatchesLogFilters(record, filters) {
			filtered = append(filtered, record)
		}
	}
	return filtered
}

func tailControlLogRecords(records []ctrllogs.LogRecord, tail int) []ctrllogs.LogRecord {
	if len(records) <= tail {
		return records
	}
	return records[len(records)-tail:]
}

func printControlLogRecords(records []ctrllogs.LogRecord, output string) error {
	if output == "json" {
		encoder := json.NewEncoder(os.Stdout)
		for _, record := range records {
			if err := encoder.Encode(record); err != nil {
				return fmt.Errorf("control logs: encode output: %w", err)
			}
		}
		return nil
	}
	for _, record := range records {
		if _, err := fmt.Fprintln(os.Stdout, ctrllogs.FormatLogText(record)); err != nil {
			return fmt.Errorf("control logs: write output: %w", err)
		}
	}
	return nil
}

func printControlLogRecord(record ctrllogs.LogRecord, output string) error {
	if output == "json" {
		if err := json.NewEncoder(os.Stdout).Encode(record); err != nil {
			return fmt.Errorf("control logs: encode output: %w", err)
		}
		return nil
	}
	_, err := fmt.Fprintln(os.Stdout, ctrllogs.FormatLogText(record))
	if err != nil {
		return fmt.Errorf("control logs: write output: %w", err)
	}
	return nil
}

func isPermissionError(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "permission denied") ||
		strings.Contains(lower, "access denied") ||
		strings.Contains(lower, "interactive authentication required")
}

func runWorkflowInit(args []string) error {
	fs := flag.NewFlagSet("workflow init", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	output := fs.String("out", ".", "output directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: deck init [--out <dir>]")
	}
	resolvedOutput := strings.TrimSpace(*output)
	if resolvedOutput == "" {
		resolvedOutput = "."
	}
	workflowsDir := filepath.Join(resolvedOutput, "workflows")

	if info, err := os.Stat(workflowsDir); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("init: workflows path exists and is not a directory: %s", workflowsDir)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("init: stat workflows directory %s: %w", workflowsDir, err)
	}

	templates := initTemplateFiles()
	overwriteTargets := make([]string, 0, len(templates))
	for fileName := range templates {
		targetPath := filepath.Join(workflowsDir, fileName)
		if _, err := os.Stat(targetPath); err == nil {
			overwriteTargets = append(overwriteTargets, targetPath)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("init: stat target file %s: %w", targetPath, err)
		}
	}
	if len(overwriteTargets) > 0 {
		sort.Strings(overwriteTargets)
		return fmt.Errorf("init: workflows already contains target files; refusing to overwrite: %s (choose another --out or remove these files)", strings.Join(overwriteTargets, ", "))
	}

	if err := os.MkdirAll(workflowsDir, 0o755); err != nil {
		return fmt.Errorf("init: create workflows directory %s: %w", workflowsDir, err)
	}
	for fileName, body := range templates {
		targetPath := filepath.Join(workflowsDir, fileName)
		if err := os.WriteFile(targetPath, []byte(body), 0o644); err != nil {
			return fmt.Errorf("init: write %s: %w", targetPath, err)
		}
	}
	created := make([]string, 0, len(templates))
	for fileName := range templates {
		created = append(created, filepath.Join(workflowsDir, fileName))
	}
	sort.Strings(created)
	fmt.Fprintf(os.Stdout, "init: wrote %s\n", strings.Join(created, ", "))
	return nil
}

func initTemplateFiles() map[string]string {
	packContent := strings.Join([]string{
		"role: pack",
		"version: v1alpha1",
		"steps: []",
		"",
	}, "\n")
	applyContent := strings.Join([]string{
		"role: apply",
		"version: v1alpha1",
		"steps: []",
		"",
	}, "\n")

	return map[string]string{
		"vars.yaml":  "{}\n",
		"pack.yaml":  packContent,
		"apply.yaml": applyContent,
	}
}

type installDryRunState struct {
	CompletedSteps []string       `json:"completedSteps"`
	RuntimeVars    map[string]any `json:"runtimeVars"`
}

func loadInstallDryRunState(wf *config.Workflow) (installDryRunState, error) {
	statePath, err := resolveInstallStatePath(wf)
	if err != nil {
		return installDryRunState{}, err
	}

	raw, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return installDryRunState{CompletedSteps: []string{}, RuntimeVars: map[string]any{}}, nil
		}
		return installDryRunState{}, fmt.Errorf("read state file: %w", err)
	}

	var state installDryRunState
	if err := json.Unmarshal(raw, &state); err != nil {
		return installDryRunState{}, fmt.Errorf("parse state file: %w", err)
	}
	if state.CompletedSteps == nil {
		state.CompletedSteps = []string{}
	}
	if state.RuntimeVars == nil {
		state.RuntimeVars = map[string]any{}
	}
	return state, nil
}

func resolveInstallStatePath(wf *config.Workflow) (string, error) {
	if wf == nil {
		return "", errors.New("workflow is nil")
	}
	stateKey := strings.TrimSpace(wf.StateKey)
	if stateKey == "" {
		return "", errors.New("workflow state key is empty")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}
	return filepath.Join(home, ".deck", "state", stateKey+".json"), nil
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

	wf, err := config.LoadWithOptions(workflowPath, config.LoadOptions{VarOverrides: opts.VarOverrides})
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

func runApply(args []string) error {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var file string
	registerFileFlags(fs, &file, "path or URL to workflow file")
	server := ""
	session := ""
	apiToken := ""
	registerAssistedFlags(fs, &server, &session, &apiToken)
	phase := fs.String("phase", "install", "phase name to execute")
	prefetch := fs.Bool("prefetch", false, "execute DownloadFile steps before other steps")
	dryRun := fs.Bool("dry-run", false, "print apply plan without executing steps")
	vars := &varFlag{}
	fs.Var(vars, "var", "set variable override (key=value), repeatable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 2 {
		return errors.New("apply accepts at most two positional arguments: [workflow] [bundle]")
	}
	positionalArgs := make([]string, 0, fs.NArg())
	for i := 0; i < fs.NArg(); i++ {
		positionalArgs = append(positionalArgs, strings.TrimSpace(fs.Arg(i)))
	}

	assistedConfig, assistedMode, err := resolveAssistedExecutionConfig(server, session, apiToken)
	if err != nil {
		return err
	}
	if assistedMode {
		return runAssistedAction(assistedConfig, "apply", func(ctx assistedExecutionContext) error {
			return executeApply(ctx.WorkflowPath, ctx.BundleRoot, strings.TrimSpace(*phase), *prefetch, *dryRun, varsAsAnyMap(vars.AsMap()))
		})
	}

	workflowPath, bundleRoot, err := resolveApplyWorkflowAndBundle(strings.TrimSpace(file), positionalArgs)
	if err != nil {
		return err
	}
	return executeApply(workflowPath, bundleRoot, strings.TrimSpace(*phase), *prefetch, *dryRun, varsAsAnyMap(vars.AsMap()))
}

func executeApply(workflowPath, bundleRoot, selectedPhase string, prefetch, dryRun bool, varOverrides map[string]any) error {
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
			if err := install.Run(prefetchWorkflow, install.RunOptions{BundleRoot: bundleRoot, StatePath: statePath}); err != nil {
				return err
			}
		}
	}

	if err := install.Run(applyExecutionWorkflow, install.RunOptions{BundleRoot: bundleRoot, StatePath: statePath}); err != nil {
		return err
	}

	fmt.Fprintln(os.Stdout, "apply: ok")
	return nil
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

	fmt.Fprintf(os.Stdout, "PHASE=%s\n", selectedPhaseName)

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
			fmt.Fprintf(os.Stdout, "%s %s SKIP (completed)\n", step.ID, step.Kind)
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
		fmt.Fprintf(os.Stdout, "%s %s %s\n", step.ID, step.Kind, status)
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
	resp, err := http.Get(rawURL)
	if err != nil {
		return nil, fmt.Errorf("get workflow url: %w", err)
	}
	defer resp.Body.Close()

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
	defer f.Close()

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
	workflowDir := filepath.Join(bundleRoot, "workflows")
	if !hasWorkflowDir(bundleRoot) {
		return "", fmt.Errorf("workflow directory not found: %s", workflowDir)
	}

	preferred := filepath.Join(workflowDir, "apply.yaml")
	if info, err := os.Stat(preferred); err == nil && !info.IsDir() {
		wf, loadErr := config.Load(preferred)
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
		wf, loadErr := config.Load(candidate)
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

func runServer(args []string) error {
	if len(args) == 0 || args[0] != "start" {
		return errors.New("usage: deck serve --root <dir> --addr <host:port> [--api-token <token>] [--report-max <n>] [--audit-max-size-mb <n>] [--audit-max-files <n>] [--tls-cert <crt> --tls-key <key> | --tls-self-signed]")
	}

	fs := flag.NewFlagSet("server start", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	root := fs.String("root", "./bundle", "server content root")
	addr := fs.String("addr", ":8080", "server listen address")
	apiToken := fs.String("api-token", "deck-site-v1", "bearer token required for /api/site/v1 endpoints")
	reportMax := fs.Int("report-max", 200, "max retained in-memory reports")
	auditMaxSizeMB := fs.Int("audit-max-size-mb", 50, "max audit log size in MB before rotation")
	auditMaxFiles := fs.Int("audit-max-files", 10, "max retained rotated audit files")
	tlsCert := fs.String("tls-cert", "", "TLS certificate path")
	tlsKey := fs.String("tls-key", "", "TLS private key path")
	tlsSelfSigned := fs.Bool("tls-self-signed", false, "auto-generate and use self-signed TLS cert")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	if (*tlsCert == "") != (*tlsKey == "") {
		return errors.New("--tls-cert and --tls-key must be provided together")
	}
	if *tlsSelfSigned && (*tlsCert != "" || *tlsKey != "") {
		return errors.New("--tls-self-signed cannot be combined with --tls-cert/--tls-key")
	}
	if *reportMax <= 0 {
		return errors.New("--report-max must be > 0")
	}
	if *auditMaxSizeMB <= 0 {
		return errors.New("--audit-max-size-mb must be > 0")
	}
	if *auditMaxFiles <= 0 {
		return errors.New("--audit-max-files must be > 0")
	}

	certPath := *tlsCert
	keyPath := *tlsKey
	if *tlsSelfSigned {
		var err error
		certPath, keyPath, err = server.EnsureSelfSignedTLS(*root, *addr)
		if err != nil {
			return err
		}
	}

	h, err := server.NewHandler(*root, server.HandlerOptions{ReportMax: *reportMax, AuditMaxSizeMB: *auditMaxSizeMB, AuditMaxFiles: *auditMaxFiles, APIToken: *apiToken})
	if err != nil {
		return err
	}
	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	if certPath != "" {
		fmt.Fprintf(os.Stdout, "server start: listening on https://%s (root=%s)\n", *addr, *root)
		return httpServer.ListenAndServeTLS(certPath, keyPath)
	}

	fmt.Fprintf(os.Stdout, "server start: listening on http://%s (root=%s)\n", *addr, *root)
	return httpServer.ListenAndServe()
}

func runWorkflowBundle(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: bundle verify <path> | bundle inspect <path> [--output text|json] | bundle import --file <bundle.tar> --dest <dir> | bundle collect --root <dir> --out <bundle.tar> | bundle merge <bundle.tar> --to <dir> [--dry-run]")
	}

	switch args[0] {
	case "verify":
		fs := flag.NewFlagSet("workflow bundle verify", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		bundlePath := fs.String("file", "", "bundle path (directory or bundle.tar)")
		parseArgs := append([]string{}, args[1:]...)
		positionalPath := ""
		if len(parseArgs) > 0 && !strings.HasPrefix(parseArgs[0], "-") {
			positionalPath = strings.TrimSpace(parseArgs[0])
			parseArgs = parseArgs[1:]
		}
		if err := fs.Parse(parseArgs); err != nil {
			return err
		}
		if fs.NArg() > 0 {
			return errors.New("bundle verify accepts a single <path>")
		}
		resolvedPath := strings.TrimSpace(*bundlePath)
		if resolvedPath == "" {
			resolvedPath = positionalPath
		}
		if resolvedPath == "" {
			return errors.New("bundle path is required")
		}

		if err := bundle.VerifyManifest(resolvedPath); err != nil {
			return err
		}

		fmt.Fprintf(os.Stdout, "bundle verify: ok (%s)\n", resolvedPath)
		return nil

	case "inspect":
		fs := flag.NewFlagSet("workflow bundle inspect", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		bundlePath := fs.String("file", "", "bundle path (directory or bundle.tar)")
		output := ""
		registerOutputFormatFlags(fs, &output, "text")
		parseArgs := append([]string{}, args[1:]...)
		positionalPath := ""
		if len(parseArgs) > 0 && !strings.HasPrefix(parseArgs[0], "-") {
			positionalPath = strings.TrimSpace(parseArgs[0])
			parseArgs = parseArgs[1:]
		}
		if err := fs.Parse(parseArgs); err != nil {
			return err
		}
		if fs.NArg() > 0 {
			return errors.New("bundle inspect accepts a single <path>")
		}
		resolvedPath := strings.TrimSpace(*bundlePath)
		if resolvedPath == "" {
			resolvedPath = positionalPath
		}
		if resolvedPath == "" {
			return errors.New("bundle path is required")
		}
		if output != "text" && output != "json" {
			return errors.New("--output must be text or json")
		}

		entries, err := bundle.InspectManifest(resolvedPath)
		if err != nil {
			return err
		}

		if output == "json" {
			return json.NewEncoder(os.Stdout).Encode(map[string]any{"entries": entries})
		}
		for _, entry := range entries {
			if _, err := fmt.Fprintln(os.Stdout, entry.Path); err != nil {
				return fmt.Errorf("bundle inspect: write output: %w", err)
			}
		}
		return nil

	case "import":
		fs := flag.NewFlagSet("workflow bundle import", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		archiveFile := fs.String("file", "", "bundle archive file path")
		destDir := fs.String("dest", "", "destination directory")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *archiveFile == "" {
			return errors.New("--file is required")
		}
		if *destDir == "" {
			return errors.New("--dest is required")
		}

		if err := bundle.ImportArchive(*archiveFile, *destDir); err != nil {
			return err
		}

		fmt.Fprintf(os.Stdout, "bundle import: ok (%s -> %s)\n", *archiveFile, *destDir)
		return nil

	case "collect":
		fs := flag.NewFlagSet("workflow bundle collect", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		bundleDir := fs.String("root", "", "bundle directory")
		outputFile := fs.String("out", "", "output tar archive path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *bundleDir == "" {
			return errors.New("--root is required")
		}
		if *outputFile == "" {
			return errors.New("--out is required")
		}

		if err := bundle.CollectArchive(*bundleDir, *outputFile); err != nil {
			return err
		}

		fmt.Fprintf(os.Stdout, "bundle collect: ok (%s -> %s)\n", *bundleDir, *outputFile)
		return nil

	case "merge":
		fs := flag.NewFlagSet("workflow bundle merge", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		to := fs.String("to", "", "merge destination (local directory)")
		dryRun := fs.Bool("dry-run", false, "print merge plan without writing")
		parseArgs := append([]string{}, args[1:]...)
		archivePath := ""
		if len(parseArgs) > 0 && !strings.HasPrefix(parseArgs[0], "-") {
			archivePath = strings.TrimSpace(parseArgs[0])
			parseArgs = parseArgs[1:]
		}
		if err := fs.Parse(parseArgs); err != nil {
			return err
		}
		if archivePath == "" {
			return errors.New("bundle merge requires <bundle.tar>")
		}
		if strings.TrimSpace(*to) == "" {
			return errors.New("--to is required")
		}

		report, err := bundle.MergeArchive(archivePath, strings.TrimSpace(*to), *dryRun)
		if err != nil {
			return err
		}

		if *dryRun {
			fmt.Fprintf(os.Stdout, "bundle merge: dry-run (%s -> %s)\n", archivePath, report.Destination)
			for _, action := range report.Actions {
				fmt.Fprintf(os.Stdout, "PLAN %s %s (%s)\n", action.Action, action.Path, action.Reason)
			}
			return nil
		}

		fmt.Fprintf(os.Stdout, "bundle merge: ok (%s -> %s)\n", archivePath, report.Destination)
		return nil

	default:
		return fmt.Errorf("unknown bundle command %q", args[0])
	}
}

func runValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var file string
	registerFileFlags(fs, &file, "path or URL to workflow file")
	vars := &varFlag{}
	fs.Var(vars, "var", "set variable override (key=value), repeatable")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if file == "" {
		return errors.New("--file (or -f) is required")
	}

	if err := validate.File(file); err != nil {
		return err
	}

	fmt.Fprintln(os.Stdout, "validate: ok")
	return nil
}

func runRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var file string
	registerFileFlags(fs, &file, "path or URL to workflow file")
	phase := fs.String("phase", "", "phase to execute (prepare/install)")
	bundle := fs.String("root", "", "bundle output path")
	vars := &varFlag{}
	fs.Var(vars, "var", "set variable override (key=value), repeatable")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if file == "" {
		return errors.New("--file (or -f) is required")
	}
	if *phase == "" {
		return errors.New("--phase is required")
	}

	if err := validate.File(file); err != nil {
		return err
	}

	wf, err := config.LoadWithOptions(file, config.LoadOptions{VarOverrides: varsAsAnyMap(vars.AsMap())})
	if err != nil {
		return err
	}

	switch *phase {
	case "prepare":
		if err := prepare.Run(wf, prepare.RunOptions{BundleRoot: *bundle}); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, "run prepare: ok")
		return nil
	case "install":
		if err := install.Run(wf, install.RunOptions{BundleRoot: *bundle}); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, "run install: ok")
		return nil
	default:
		return fmt.Errorf("unsupported phase: %s", *phase)
	}
}

func runResume(args []string) error {
	fs := flag.NewFlagSet("resume", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var file string
	registerFileFlags(fs, &file, "path or URL to workflow file")
	bundle := fs.String("root", "", "bundle path")
	vars := &varFlag{}
	fs.Var(vars, "var", "set variable override (key=value), repeatable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if file == "" {
		return errors.New("--file (or -f) is required")
	}

	if err := validate.File(file); err != nil {
		return err
	}

	wf, err := config.LoadWithOptions(file, config.LoadOptions{VarOverrides: varsAsAnyMap(vars.AsMap())})
	if err != nil {
		return err
	}
	if err := install.Run(wf, install.RunOptions{BundleRoot: *bundle}); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, "resume install: ok")
	return nil
}

func runDiagnose(args []string) error {
	fs := flag.NewFlagSet("diagnose", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var file string
	registerFileFlags(fs, &file, "path or URL to workflow file")
	bundle := fs.String("root", "", "bundle path")
	preflight := fs.Bool("preflight", false, "run preflight checks")
	out := fs.String("out", "reports/diagnose.json", "diagnose report output path")
	vars := &varFlag{}
	fs.Var(vars, "var", "set variable override (key=value), repeatable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if file == "" {
		return errors.New("--file (or -f) is required")
	}
	if !*preflight {
		return errors.New("only --preflight mode is supported")
	}

	if err := validate.File(file); err != nil {
		return err
	}

	wf, err := config.LoadWithOptions(file, config.LoadOptions{VarOverrides: varsAsAnyMap(vars.AsMap())})
	if err != nil {
		return err
	}

	report, err := diagnose.Preflight(wf, diagnose.RunOptions{WorkflowPath: file, BundleRoot: *bundle, OutputPath: *out, EnforceHostChecks: true})
	if err != nil {
		fmt.Fprintf(os.Stdout, "diagnose preflight: failed (%d failed checks)\n", report.Summary.Failed)
		return err
	}

	fmt.Fprintf(os.Stdout, "diagnose preflight: ok (%d checks)\n", report.Summary.Passed)
	fmt.Fprintf(os.Stdout, "diagnose report: %s\n", *out)
	return nil
}
