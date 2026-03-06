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
	"github.com/taedi90/deck/internal/prepare"
	"github.com/taedi90/deck/internal/server"
	"github.com/taedi90/deck/internal/validate"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "deck: %v\n", err)
		if code, ok := extractExitCode(err); ok {
			os.Exit(code)
		}
		os.Exit(1)
	}
}

type exitCodeError struct {
	code int
	err  error
}

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

func (e *exitCodeError) Error() string {
	return e.err.Error()
}

func (e *exitCodeError) Unwrap() error {
	return e.err
}

func extractExitCode(err error) (int, bool) {
	var coded *exitCodeError
	if !errors.As(err, &coded) {
		return 0, false
	}
	if coded.code <= 0 {
		return 1, true
	}
	return coded.code, true
}

func run(args []string) error {
	if len(args) == 0 {
		return usageError()
	}

	switch args[0] {
	case "-h", "--help", "help":
		return usageError()
	case "pack":
		return runPack(args[1:])
	case "apply":
		return runApply(args[1:])
	case "serve":
		return runServe(args[1:])
	case "bundle":
		return runWorkflowBundle(args[1:])
	case "list":
		return runList(args[1:])
	case "validate":
		return runValidate(args[1:])
	case "diff":
		return runDiff(args[1:])
	case "init":
		return runWorkflowInit(args[1:])
	case "doctor":
		return runDoctor(args[1:])
	case "health":
		return runHealth(args[1:])
	case "logs":
		return runLogs(args[1:])
	case "cache":
		return runCache(args[1:])
	case "service":
		return runService(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
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

	packWorkflow, err := config.Load(packWorkflowPath)
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
	if err := copyFile(packWorkflowPath, filepath.Join(workflowOutDir, filepath.Base(packWorkflowPath)), 0o644); err != nil {
		return err
	}
	if err := copyFile(applyWorkflowPath, filepath.Join(workflowOutDir, "apply.yaml"), 0o644); err != nil {
		return err
	}
	if err := copyFile(varsWorkflowPath, filepath.Join(workflowOutDir, "vars.yaml"), 0o644); err != nil {
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
	// usage: deck list [flags]
	if len(args) == 0 {
		return errors.New("usage: deck list [flags]")
	}
	if args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		return errors.New("usage: deck list [flags]")
	}

	// Flags for this subcommand
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var server string
	var output string
	// --server is required
	fs.StringVar(&server, "server", "", "server URL for index (required)")
	// reuse common output flag definitions
	registerOutputFormatFlags(fs, &output, "text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(server) == "" {
		return errors.New("--server is required")
	}
	if output != "text" && output != "json" {
		return errors.New("--output must be text or json")
	}

	// Fetch the server index JSON
	trimmed := strings.TrimRight(server, "/")
	indexURL := trimmed + "/workflows/index.json"
	resp, err := http.Get(indexURL)
	if err != nil {
		return fmt.Errorf("list: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("list: unexpected status %d", resp.StatusCode)
	}
	var items []string
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return fmt.Errorf("list: decode response: %w", err)
	}

	// Output according to the requested format
	if output == "json" {
		enc := json.NewEncoder(os.Stdout)
		if err := enc.Encode(items); err != nil {
			return fmt.Errorf("list: encode output: %w", err)
		}
		return nil
	}
	// text output: one path per line
	w := bufio.NewWriter(os.Stdout)
	for _, it := range items {
		if _, err := fmt.Fprintln(w, it); err != nil {
			return err
		}
	}
	return w.Flush()
}

func runDiff(args []string) error {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var file string
	registerFileFlags(fs, &file, "path to workflow file")
	phase := fs.String("phase", "install", "phase name to diff")
	output := ""
	registerOutputFormatFlags(fs, &output, "text")
	vars := &varFlag{}
	fs.Var(vars, "var", "set variable override (key=value), repeatable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	workflowPath := strings.TrimSpace(file)
	if workflowPath == "" {
		return errors.New("--file (or -f) is required")
	}
	if err := validate.File(workflowPath); err != nil {
		return err
	}

	wf, err := config.LoadWithOptions(workflowPath, config.LoadOptions{VarOverrides: varsAsAnyMap(vars.AsMap())})
	if err != nil {
		return err
	}
	if strings.TrimSpace(wf.Role) != "apply" {
		return fmt.Errorf("diff workflow role must be apply: %s", workflowPath)
	}
	selectedPhase := strings.TrimSpace(*phase)
	if selectedPhase == "" {
		selectedPhase = "install"
	}
	applyExecutionWorkflow, err := buildApplyExecutionWorkflow(wf, selectedPhase)
	if err != nil {
		return err
	}

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
	statePath, err := resolveInstallStatePath(applyExecutionWorkflow)
	if err != nil {
		return err
	}
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
		}{Phase: selectedPhase, StatePath: statePath, Steps: steps}
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

func runService(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: deck service <install|status|stop>")
	}

	switch args[0] {
	case "status":
		return runServiceStatus(args[1:])
	case "stop":
		return runServiceStop(args[1:])
	case "install":
		return runServiceInstall(args[1:])
	default:
		return fmt.Errorf("unknown service command %q", args[0])
	}
}

func runServiceInstall(args []string) error {
	fs := flag.NewFlagSet("service install", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	outDir := fs.String("out", ".", "output directory")
	root := fs.String("root", "", "serve root directory")
	addr := fs.String("addr", ":8080", "serve listen address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: deck service install [--out <dir>] --root <root> [--addr <addr>]")
	}
	resolvedOut := strings.TrimSpace(*outDir)
	if resolvedOut == "" {
		resolvedOut = "."
	}
	resolvedRoot := strings.TrimSpace(*root)
	if resolvedRoot == "" {
		return errors.New("--root is required")
	}
	resolvedAddr := strings.TrimSpace(*addr)
	if resolvedAddr == "" {
		resolvedAddr = ":8080"
	}

	if err := os.MkdirAll(resolvedOut, 0o755); err != nil {
		return fmt.Errorf("service install: create output dir: %w", err)
	}

	servicePath := filepath.Join(resolvedOut, "deck-server.service")
	serviceContent := fmt.Sprintf(`[Unit]
Description=deck server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=deck serve --root %s --addr %s
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
`, resolvedRoot, resolvedAddr)
	if err := os.WriteFile(servicePath, []byte(serviceContent), 0o644); err != nil {
		return fmt.Errorf("service install: write service file: %w", err)
	}

	fmt.Fprintf(os.Stdout, "service install: wrote %s\n", servicePath)
	return nil
}

func runServiceStatus(args []string) error {
	fs := flag.NewFlagSet("service status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: deck service status")
	}
	unit := "deck-server.service"
	rawState, err := runSystemctlIsActive(unit, false)
	if err != nil {
		return fmt.Errorf("service status: %w\nsuggestion: %s", err, suggestSystemctlStatusCommand(unit, false))
	}
	mappedState := mapSystemctlState(rawState)
	fmt.Fprintf(os.Stdout, "service status: %s (%s)\n", mappedState, unit)
	return nil
}

func runServiceStop(args []string) error {
	fs := flag.NewFlagSet("service stop", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: deck service stop")
	}
	unit := "deck-server.service"
	if err := runSystemctlStop(unit, false); err != nil {
		return fmt.Errorf("service stop: %w\nsuggestion: %s", err, suggestSystemctlStatusCommand(unit, false))
	}
	fmt.Fprintf(os.Stdout, "service stop: ok (%s)\n", unit)
	return nil
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
		return errors.New("--server is required")
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
	out := fs.String("out", "", "output report path (required)")
	vars := &varFlag{}
	fs.Var(vars, "var", "set variable override (key=value), repeatable")
	if err := fs.Parse(args); err != nil {
		return err
	}

	resolvedOut := strings.TrimSpace(*out)
	if resolvedOut == "" {
		return errors.New("--out is required")
	}

	workflowPath := strings.TrimSpace(file)
	if workflowPath == "" {
		resolved, err := discoverApplyWorkflow(".")
		if err != nil {
			return err
		}
		workflowPath = resolved
	}

	isRemoteWorkflow := isHTTPWorkflowPath(workflowPath)
	if !isRemoteWorkflow {
		abs, err := filepath.Abs(workflowPath)
		if err != nil {
			return fmt.Errorf("resolve workflow path: %w", err)
		}
		workflowPath = abs
	}

	if isRemoteWorkflow {
		workflowBytes, err := fetchWorkflowForApplyValidation(workflowPath)
		if err != nil {
			return err
		}
		if err := validate.Bytes(workflowPath, workflowBytes); err != nil {
			return err
		}
	} else {
		if err := validate.File(workflowPath); err != nil {
			return err
		}
	}

	wf, err := config.LoadWithOptions(workflowPath, config.LoadOptions{VarOverrides: varsAsAnyMap(vars.AsMap())})
	if err != nil {
		return err
	}
	if strings.TrimSpace(wf.Role) != "apply" {
		return fmt.Errorf("doctor workflow role must be apply: %s", workflowPath)
	}

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
		Workflow:  workflowPath,
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

func runControlDoctor(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: deck doctor preflight --file <wf> [--root <dir>] [--out <path>] [--host-checks] [--var key=value]")
	}

	switch args[0] {
	case "preflight":
		return runControlDoctorPreflight(args[1:])
	default:
		return fmt.Errorf("unknown control doctor command %q", args[0])
	}
}

func runControlDoctorPreflight(args []string) error {
	fs := flag.NewFlagSet("control doctor preflight", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var file string
	registerFileFlags(fs, &file, "path or URL to workflow file")
	bundleRoot := fs.String("root", "", "bundle path")
	out := fs.String("out", "reports/diagnose.json", "doctor report output path")
	hostChecks := fs.Bool("host-checks", false, "enforce host prerequisite checks")
	vars := &varFlag{}
	fs.Var(vars, "var", "set variable override (key=value), repeatable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(file) == "" {
		return errors.New("--file (or -f) is required")
	}

	if err := validate.File(file); err != nil {
		return err
	}

	wf, err := config.LoadWithOptions(file, config.LoadOptions{VarOverrides: varsAsAnyMap(vars.AsMap())})
	if err != nil {
		return err
	}

	report, err := diagnose.Preflight(wf, diagnose.RunOptions{
		WorkflowPath:      file,
		BundleRoot:        *bundleRoot,
		OutputPath:        *out,
		EnforceHostChecks: *hostChecks,
	})
	if err != nil {
		failedChecks := 0
		if report != nil {
			failedChecks = report.Summary.Failed
		}
		fmt.Fprintf(os.Stdout, "doctor preflight: failed (%d failed checks)\n", failedChecks)
		fmt.Fprintf(os.Stdout, "doctor report: %s\n", *out)
		return err
	}

	fmt.Fprintf(os.Stdout, "doctor preflight: ok (%d checks)\n", len(report.Checks))
	fmt.Fprintf(os.Stdout, "doctor report: %s\n", *out)
	return nil
}

func runControlStatus(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: deck service status <server|agent> [--unit <name>] [--user] [--output text|json]")
	}
	unitType := strings.TrimSpace(args[0])
	fs := flag.NewFlagSet("control status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	unit := fs.String("unit", "", "systemd unit name")
	userScope := fs.Bool("user", false, "use user systemd scope")
	output := ""
	registerOutputFormatFlags(fs, &output, "text")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	resolvedUnit, err := resolveSystemdUnit(unitType, strings.TrimSpace(*unit))
	if err != nil {
		return err
	}
	if output != "text" && output != "json" {
		return errors.New("--output must be text or json")
	}

	rawState, err := runSystemctlIsActive(resolvedUnit, *userScope)
	if err != nil {
		return fmt.Errorf("control status: %w\nsuggestion: %s", err, suggestSystemctlStatusCommand(resolvedUnit, *userScope))
	}

	mappedState := mapSystemctlState(rawState)
	if output == "json" {
		return json.NewEncoder(os.Stdout).Encode(map[string]string{
			"type":   unitType,
			"unit":   resolvedUnit,
			"status": mappedState,
		})
	}

	fmt.Fprintf(os.Stdout, "control status: %s (%s)\n", mappedState, resolvedUnit)
	return nil
}

func runControlStop(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: deck service stop <server|agent> [--unit <name>] [--user]")
	}
	unitType := strings.TrimSpace(args[0])
	fs := flag.NewFlagSet("control stop", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	unit := fs.String("unit", "", "systemd unit name")
	userScope := fs.Bool("user", false, "use user systemd scope")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	resolvedUnit, err := resolveSystemdUnit(unitType, strings.TrimSpace(*unit))
	if err != nil {
		return err
	}

	if err := runSystemctlStop(resolvedUnit, *userScope); err != nil {
		return fmt.Errorf("control stop: %w\nsuggestion: %s", err, suggestSystemctlStatusCommand(resolvedUnit, *userScope))
	}

	fmt.Fprintf(os.Stdout, "control stop: ok (%s)\n", resolvedUnit)
	return nil
}

func runControlLogs(args []string) error {
	fs := flag.NewFlagSet("control logs", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	source := fs.String("source", "both", "log source (file|journal|both)")
	path := fs.String("path", "", "audit log file path")
	unit := fs.String("unit", "deck-server.service", "systemd unit for journal logs")
	eventType := fs.String("event-type", "", "filter by event type")
	jobID := fs.String("job-id", "", "filter by job id")
	status := fs.String("status", "", "filter by status")
	level := fs.String("level", "", "filter by level")
	tail := fs.Int("tail", 200, "number of records to print")
	follow := fs.Bool("follow", false, "follow new log records")
	since := fs.Duration("since", 0, "only show records newer than duration")
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
	if *tail <= 0 {
		return errors.New("--tail must be > 0")
	}
	if *tail > 5000 {
		return errors.New("--tail must be <= 5000")
	}
	if *since < 0 {
		return errors.New("--since must be >= 0")
	}
	if *follow && resolvedSource == "both" {
		return errors.New("--follow requires --source file or --source journal")
	}

	filters := ctrllogs.LogFilters{
		EventType: strings.TrimSpace(*eventType),
		JobID:     strings.TrimSpace(*jobID),
		Status:    strings.TrimSpace(*status),
		Level:     strings.TrimSpace(*level),
	}

	if resolvedSource == "journal" && *follow {
		resolvedUnit := strings.TrimSpace(*unit)
		if resolvedUnit == "" {
			resolvedUnit = "deck-server.service"
		}
		return followControlLogsJournal(resolvedUnit, *tail, *since, filters, output)
	}

	var records []ctrllogs.LogRecord
	resolvedFilePath := ""
	if resolvedSource == "file" || resolvedSource == "both" {
		filePath, err := resolveControlLogFilePath(strings.TrimSpace(*path))
		if err != nil {
			return err
		}
		resolvedFilePath = filePath
		fileRecords, err := readControlLogsFile(filePath, *since)
		if err != nil {
			return err
		}
		records = append(records, fileRecords...)
	}

	if resolvedSource == "journal" || resolvedSource == "both" {
		resolvedUnit := strings.TrimSpace(*unit)
		if resolvedUnit == "" {
			resolvedUnit = "deck-server.service"
		}
		journalRecords, err := readControlLogsJournal(resolvedUnit, *tail, *since)
		if err != nil {
			return fmt.Errorf("control logs: %w\nsuggestion: %s", err, suggestJournalctlCommand(resolvedUnit))
		}
		records = append(records, journalRecords...)
	}

	filtered := filterControlLogRecords(records, filters)
	tailed := tailControlLogRecords(filtered, *tail)
	if err := printControlLogRecords(tailed, output); err != nil {
		return err
	}

	if *follow && resolvedSource == "file" {
		return followControlLogsFile(resolvedFilePath, filters, output)
	}
	return nil
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

func resolveSystemdUnit(unitType, unit string) (string, error) {
	if unitType != "server" && unitType != "agent" {
		return "", errors.New("service target must be server or agent")
	}
	if unit != "" {
		return unit, nil
	}
	if unitType == "server" {
		return "deck-server.service", nil
	}
	return "deck-agent.service", nil
}

func runSystemctlIsActive(unit string, userScope bool) (string, error) {
	args := []string{}
	if userScope {
		args = append(args, "--user")
	}
	args = append(args, "is-active", unit)

	raw, err := exec.Command("systemctl", args...).CombinedOutput()
	state := strings.TrimSpace(string(raw))
	if err == nil {
		return state, nil
	}

	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return "", errors.New("systemctl not found")
	}
	if isPermissionError(state) {
		return "", errors.New("systemctl permission denied")
	}
	if state != "" {
		mapped := mapSystemctlState(state)
		if mapped != "unknown" || strings.EqualFold(state, "unknown") {
			return state, nil
		}
		return "", fmt.Errorf("systemctl is-active failed: %s", state)
	}

	return "", fmt.Errorf("systemctl is-active failed: %w", err)
}

func runSystemctlStop(unit string, userScope bool) error {
	args := []string{}
	if userScope {
		args = append(args, "--user")
	}
	args = append(args, "stop", unit)

	raw, err := exec.Command("systemctl", args...).CombinedOutput()
	if err == nil {
		return nil
	}

	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return errors.New("systemctl not found")
	}
	msg := strings.TrimSpace(string(raw))
	if msg != "" {
		return fmt.Errorf("systemctl stop failed: %s", msg)
	}
	return fmt.Errorf("systemctl stop failed: %w", err)
}

func mapSystemctlState(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "active":
		return "active"
	case "inactive", "deactivating":
		return "inactive"
	case "failed":
		return "failed"
	default:
		return "unknown"
	}
}

func suggestSystemctlStatusCommand(unit string, userScope bool) string {
	if userScope {
		return fmt.Sprintf("systemctl --user status %s", unit)
	}
	return fmt.Sprintf("sudo systemctl status %s", unit)
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
	template := fs.String("template", "", "template name (single|multi)")
	output := fs.String("out", ".", "output directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: deck init --template single|multi [--out <dir>]")
	}

	resolvedTemplate := strings.TrimSpace(*template)
	if resolvedTemplate != "single" && resolvedTemplate != "multi" {
		return errors.New("init: --template is required and must be single or multi")
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

	templates := initTemplateFiles(resolvedTemplate)
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

func initTemplateFiles(template string) map[string]string {
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

	varsContent := "{}\n"
	if template == "multi" {
		varsContent = "nodes: []\n"
	}

	return map[string]string{
		"vars.yaml":  varsContent,
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
	phase := fs.String("phase", "install", "phase name to execute")
	prefetch := fs.Bool("prefetch", false, "execute DownloadFile steps before other steps")
	dryRun := fs.Bool("dry-run", false, "print apply plan without executing steps")
	vars := &varFlag{}
	fs.Var(vars, "var", "set variable override (key=value), repeatable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return errors.New("apply accepts at most one positional bundle path")
	}
	positionalBundle := ""
	if fs.NArg() == 1 {
		positionalBundle = strings.TrimSpace(fs.Arg(0))
	}

	workflowPath := strings.TrimSpace(file)
	isRemoteWorkflow := isHTTPWorkflowPath(workflowPath)
	bundleRoot := ""
	var err error

	if !isRemoteWorkflow {
		bundleRoot, err = resolveApplyBundleRoot(positionalBundle)
		if err != nil {
			return err
		}

		if workflowPath == "" {
			workflowPath, err = discoverApplyWorkflow(bundleRoot)
			if err != nil {
				return err
			}
		} else {
			workflowPath, err = filepath.Abs(workflowPath)
			if err != nil {
				return fmt.Errorf("resolve workflow path: %w", err)
			}
		}
	}

	if workflowPath == "" {
		bundleRoot, err = resolveApplyBundleRoot(positionalBundle)
		if err != nil {
			return err
		}
		workflowPath, err = discoverApplyWorkflow(bundleRoot)
		if err != nil {
			return err
		}
	}

	if isRemoteWorkflow {
		workflowBytes, err := fetchWorkflowForApplyValidation(workflowPath)
		if err != nil {
			return err
		}
		if err := validate.Bytes(workflowPath, workflowBytes); err != nil {
			return err
		}
	} else {
		if err := validate.File(workflowPath); err != nil {
			return err
		}
	}

	wf, err := config.LoadWithOptions(workflowPath, config.LoadOptions{VarOverrides: varsAsAnyMap(vars.AsMap())})
	if err != nil {
		return err
	}
	if strings.TrimSpace(wf.Role) != "apply" {
		return fmt.Errorf("apply workflow role must be apply: %s", workflowPath)
	}

	selectedPhase := strings.TrimSpace(*phase)
	if selectedPhase == "" {
		selectedPhase = "install"
	}
	applyExecutionWorkflow, err := buildApplyExecutionWorkflow(wf, selectedPhase)
	if err != nil {
		return err
	}

	statePath, err := resolveInstallStatePath(wf)
	if err != nil {
		return err
	}
	if *dryRun {
		return runApplyDryRun(applyExecutionWorkflow, selectedPhase, bundleRoot)
	}

	if *prefetch {
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
		return errors.New("usage: deck serve --root <dir> --addr <host:port> [--report-max <n>] [--audit-max-size-mb <n>] [--audit-max-files <n>] [--tls-cert <crt> --tls-key <key> | --tls-self-signed]")
	}

	fs := flag.NewFlagSet("server start", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	root := fs.String("root", "./bundle", "server content root")
	addr := fs.String("addr", ":8080", "server listen address")
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

	h, err := server.NewHandler(*root, server.HandlerOptions{ReportMax: *reportMax, AuditMaxSizeMB: *auditMaxSizeMB, AuditMaxFiles: *auditMaxFiles})
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
		return errors.New("usage: bundle verify <path> | bundle inspect <path> [--output text|json] | bundle import --file <bundle.tar> --dest <dir> | bundle collect --root <dir> --out <bundle.tar> | bundle merge <bundle.tar> --to <http-url|dir> [--dry-run]")
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
		to := fs.String("to", "", "merge destination (http URL or local directory)")
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

func usageError() error {
	return errors.New("usage: deck <command> [flags]\n\ncommands:\n  pack\n  apply\n  serve\n  bundle\n  list\n  validate\n  diff\n  init\n  doctor\n  health\n  logs\n  cache\n  service")
}
