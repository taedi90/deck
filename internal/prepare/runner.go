package prepare

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"github.com/taedi90/deck/internal/config"
	"github.com/taedi90/deck/internal/fetch"
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

type manifestFile struct {
	Entries []manifestEntry `json:"entries"`
}

type manifestEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

const (
	errCodePrepareRuntimeMissing     = "E_PREPARE_RUNTIME_NOT_FOUND"
	errCodePrepareRuntimeUnsupported = "E_PREPARE_RUNTIME_UNSUPPORTED"
	errCodePrepareEngineUnsupported  = "E_PREPARE_ENGINE_UNSUPPORTED"
	errCodePrepareArtifactsEmpty     = "E_PREPARE_NO_ARTIFACTS"
	errCodePrepareSourceNotFound     = "E_PREPARE_SOURCE_NOT_FOUND"
	errCodePrepareChecksumMismatch   = "E_PREPARE_CHECKSUM_MISMATCH"
	errCodePrepareOfflinePolicyBlock = "E_PREPARE_OFFLINE_POLICY_BLOCK"
	errCodePrepareConditionEval      = "E_CONDITION_EVAL"
	errCodePrepareRegisterMissing    = "E_REGISTER_OUTPUT_NOT_FOUND"
	errCodePrepareCheckHostFailed    = "E_PREPARE_CHECKHOST_FAILED"
	packageCacheMetaFile             = ".deck-cache-packages.json"
)

type packageCacheMeta struct {
	Packages []string `json:"packages"`
	Files    []string `json:"files"`
}

var (
	readFileFn            = os.ReadFile
	goosFn                = func() string { return runtime.GOOS }
	goarchFn              = func() string { return runtime.GOARCH }
	parseImageReferenceFn = func(v string) (name.Reference, error) { return name.ParseReference(v, name.WeakValidation) }
	remoteImageFetchFn    = remote.Image
	tarballWriteToFileFn  = tarball.WriteToFile
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
	preparePhase, found := findPhase(wf, "prepare")
	if !found {
		return fmt.Errorf("prepare phase not found")
	}
	packCacheEnabled := strings.TrimSpace(wf.Role) == "pack"
	packCacheStatePath := ""
	packCachePlan := PackCachePlan{}
	if packCacheEnabled {
		workflowSHA := strings.TrimSpace(wf.WorkflowSHA256)
		if workflowSHA == "" {
			fallbackBytes, err := json.Marshal(wf)
			if err != nil {
				return fmt.Errorf("encode workflow for pack cache: %w", err)
			}
			workflowSHA = computeWorkflowSHA256(fallbackBytes)
		}
		var err error
		packCacheStatePath, err = defaultPackCacheStatePath(workflowSHA)
		if err != nil {
			return fmt.Errorf("resolve pack cache state path: %w", err)
		}
		prevPackCacheState, err := loadPackCacheState(packCacheStatePath)
		if err != nil {
			return err
		}
		workflowBytesForPlan, err := json.Marshal(wf)
		if err != nil {
			return fmt.Errorf("encode workflow for pack cache plan: %w", err)
		}
		packCachePlan = ComputePackCachePlan(prevPackCacheState, workflowBytesForPlan, wf.Vars, preparePhase.Steps)
		packCachePlan.WorkflowSHA256 = workflowSHA
	}
	ctxData := map[string]any{"bundleRoot": bundleRoot, "stateFile": ""}

	for _, step := range preparePhase.Steps {
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

func runPrepareStep(ctx context.Context, runner CommandRunner, bundleRoot, kind string, rendered map[string]any, opts RunOptions) ([]string, map[string]any, error) {
	switch kind {
	case "DownloadFile":
		f, err := runDownloadFile(ctx, bundleRoot, rendered, opts)
		if err != nil {
			return nil, nil, err
		}
		return []string{f}, map[string]any{"path": f, "artifacts": []string{f}}, nil
	case "DownloadPackages":
		files, err := runDownloadPackages(ctx, runner, bundleRoot, rendered, "packages", opts)
		if err != nil {
			return nil, nil, err
		}
		return files, map[string]any{"artifacts": files}, nil
	case "DownloadK8sPackages":
		files, err := runDownloadK8sPackages(ctx, runner, bundleRoot, rendered, opts)
		if err != nil {
			return nil, nil, err
		}
		return files, map[string]any{"artifacts": files}, nil
	case "DownloadImages":
		files, err := runDownloadImages(ctx, runner, bundleRoot, rendered, opts)
		if err != nil {
			return nil, nil, err
		}
		return files, map[string]any{"artifacts": files}, nil
	case "CheckHost":
		outputs, err := runCheckHost(runner, rendered)
		if err != nil {
			return nil, nil, err
		}
		return nil, outputs, nil
	default:
		return nil, map[string]any{}, nil
	}
}

func runCheckHost(runner CommandRunner, spec map[string]any) (map[string]any, error) {
	checks := stringSlice(spec["checks"])
	if len(checks) == 0 {
		return nil, fmt.Errorf("%s: CheckHost requires checks", errCodePrepareCheckHostFailed)
	}
	host := detectHostFacts()

	failFast := true
	if raw, ok := spec["failFast"]; ok {
		if b, ok := raw.(bool); ok {
			failFast = b
		}
	}

	failed := make([]string, 0)
	fail := func(name, reason string) error {
		failed = append(failed, name+":"+reason)
		if failFast {
			return fmt.Errorf("%s: %s", errCodePrepareCheckHostFailed, strings.Join(failed, ", "))
		}
		return nil
	}

	for _, chk := range checks {
		switch chk {
		case "os":
			if goosFn() != "linux" {
				if err := fail("os", "expected linux"); err != nil {
					return nil, err
				}
			}
		case "arch":
			arch := goarchFn()
			if arch != "amd64" && arch != "arm64" {
				if err := fail("arch", "expected amd64 or arm64"); err != nil {
					return nil, err
				}
			}
		case "kernelModules":
			raw, err := readFileFn("/proc/modules")
			if err != nil {
				if err := fail("kernelModules", "cannot read /proc/modules"); err != nil {
					return nil, err
				}
				break
			}
			mods := string(raw)
			if !strings.Contains(mods, "overlay ") || !strings.Contains(mods, "br_netfilter ") {
				if err := fail("kernelModules", "overlay/br_netfilter missing"); err != nil {
					return nil, err
				}
			}
		case "swap":
			raw, err := readFileFn("/proc/swaps")
			if err != nil {
				if err := fail("swap", "cannot read /proc/swaps"); err != nil {
					return nil, err
				}
				break
			}
			lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
			if len(lines) > 1 {
				if err := fail("swap", "swap enabled"); err != nil {
					return nil, err
				}
			}
		case "binaries":
			bins := stringSlice(spec["binaries"])
			if len(bins) == 0 {
				if err := fail("binaries", "binaries list required"); err != nil {
					return nil, err
				}
				break
			}
			missing := make([]string, 0)
			for _, b := range bins {
				if _, err := runner.LookPath(b); err != nil {
					missing = append(missing, b)
				}
			}
			if len(missing) > 0 {
				if err := fail("binaries", strings.Join(missing, ",")); err != nil {
					return nil, err
				}
			}
		default:
			if err := fail(chk, "unsupported check"); err != nil {
				return nil, err
			}
		}
	}

	if len(failed) > 0 {
		return map[string]any{"passed": false, "failedChecks": failed, "host": host}, fmt.Errorf("%s: %s", errCodePrepareCheckHostFailed, strings.Join(failed, ", "))
	}
	return map[string]any{"passed": true, "failedChecks": []string{}, "host": host}, nil
}

func applyRegister(step config.Step, outputs map[string]any, runtimeVars map[string]any) error {
	return workflowexec.ApplyRegister(step, outputs, runtimeVars, errCodePrepareRegisterMissing)
}

func detectHostFacts() map[string]any {
	return workflowexec.DetectHostFacts(goosFn(), goarchFn(), readFileFn)
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

func runDownloadFile(ctx context.Context, bundleRoot string, spec map[string]any, opts RunOptions) (string, error) {
	source := mapValue(spec, "source")
	output := mapValue(spec, "output")
	fetchCfg := mapValue(spec, "fetch")
	url := stringValue(source, "url")
	sourcePath := stringValue(source, "path")
	expectedSHA := strings.ToLower(stringValue(source, "sha256"))
	offlineOnly := boolValue(fetchCfg, "offlineOnly")
	outPath := stringValue(output, "path")
	if strings.TrimSpace(outPath) == "" {
		outPath = filepath.ToSlash(filepath.Join("files", inferDownloadFileName(sourcePath, url)))
	}
	if strings.TrimSpace(sourcePath) == "" && strings.TrimSpace(url) == "" {
		return "", fmt.Errorf("DownloadFile requires source.path or source.url")
	}

	target := filepath.Join(bundleRoot, outPath)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", fmt.Errorf("create output directory: %w", err)
	}

	reuse, err := canReuseDownloadFile(bundleRoot, spec, target, opts)
	if err != nil {
		return "", err
	}
	if reuse {
		return outPath, nil
	}

	f, err := os.Create(target)
	if err != nil {
		return "", fmt.Errorf("create output file: %w", err)
	}
	defer func() { _ = f.Close() }()

	if sourcePath != "" {
		raw, err := resolveSourceBytes(spec, sourcePath)
		if err == nil {
			if _, err := f.Write(raw); err != nil {
				return "", fmt.Errorf("write output file: %w", err)
			}
		} else {
			if url == "" {
				return "", err
			}
			if offlineOnly {
				return "", fmt.Errorf("%s: source.url fallback blocked by offline policy", errCodePrepareOfflinePolicyBlock)
			}
			if _, err := f.Seek(0, 0); err != nil {
				return "", fmt.Errorf("reset output file cursor: %w", err)
			}
			if err := f.Truncate(0); err != nil {
				return "", fmt.Errorf("truncate output file: %w", err)
			}
			if err := downloadURLToFile(ctx, f, url); err != nil {
				return "", err
			}
		}
	} else {
		if offlineOnly {
			return "", fmt.Errorf("%s: source.url blocked by offline policy", errCodePrepareOfflinePolicyBlock)
		}
		if err := downloadURLToFile(ctx, f, url); err != nil {
			return "", err
		}
	}

	if expectedSHA != "" {
		if err := verifyFileSHA256(target, expectedSHA); err != nil {
			return "", err
		}
	}

	if modeRaw := stringValue(output, "chmod"); modeRaw != "" {
		modeVal, err := strconv.ParseUint(modeRaw, 8, 32)
		if err != nil {
			return "", fmt.Errorf("invalid chmod: %w", err)
		}
		if err := os.Chmod(target, os.FileMode(modeVal)); err != nil {
			return "", fmt.Errorf("apply chmod: %w", err)
		}
	}

	return outPath, nil
}

func downloadURLToFile(ctx context.Context, target *os.File, url string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download %s: unexpected status %d", url, resp.StatusCode)
	}
	if _, err := io.Copy(target, resp.Body); err != nil {
		return fmt.Errorf("write output file: %w", err)
	}
	return nil
}

func resolveSourceBytes(spec map[string]any, sourcePath string) ([]byte, error) {
	fetchCfg := mapValue(spec, "fetch")
	sourcesRaw, ok := fetchCfg["sources"].([]any)
	offlineOnly := boolValue(fetchCfg, "offlineOnly")
	if ok && len(sourcesRaw) > 0 {
		sources := make([]fetch.SourceConfig, 0, len(sourcesRaw))
		for _, raw := range sourcesRaw {
			s, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			sources = append(sources, fetch.SourceConfig{
				Type: stringValue(s, "type"),
				Path: stringValue(s, "path"),
				URL:  stringValue(s, "url"),
			})
		}
		if len(sources) == 0 {
			return nil, fmt.Errorf("%s: source.path %s not found in configured fetch sources", errCodePrepareSourceNotFound, sourcePath)
		}
		raw, err := fetch.ResolveBytes(sourcePath, sources, fetch.ResolveOptions{OfflineOnly: offlineOnly})
		if err == nil {
			return raw, nil
		}
		return nil, fmt.Errorf("%s: source.path %s not found in configured fetch sources", errCodePrepareSourceNotFound, sourcePath)
	}

	raw, err := os.ReadFile(sourcePath)
	if err == nil {
		return raw, nil
	}
	return nil, fmt.Errorf("%s: source.path %s not found", errCodePrepareSourceNotFound, sourcePath)
}

func verifyFileSHA256(path, expected string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read downloaded file for checksum: %w", err)
	}
	sum := sha256.Sum256(raw)
	actual := hex.EncodeToString(sum[:])
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("%s: expected %s got %s", errCodePrepareChecksumMismatch, expected, actual)
	}
	return nil
}

func runDownloadPackages(ctx context.Context, runner CommandRunner, bundleRoot string, spec map[string]any, defaultDir string, opts RunOptions) ([]string, error) {
	output := mapValue(spec, "output")
	dir := stringValue(output, "dir")
	if dir == "" {
		dir = defaultDir
	}

	packages := stringSlice(spec["packages"])
	if len(packages) == 0 {
		return nil, fmt.Errorf("DownloadPackages requires packages")
	}

	backend := mapValue(spec, "backend")
	if stringValue(backend, "mode") == "container" && stringValue(backend, "image") != "" {
		repo := mapValue(spec, "repo")
		if len(repo) > 0 {
			distro := mapValue(spec, "distro")
			family := stringValue(distro, "family")
			if family == "" {
				family = "debian"
			}
			release := strings.TrimSpace(stringValue(distro, "release"))
			if release == "" {
				return nil, fmt.Errorf("DownloadPackages repo mode requires distro.release")
			}

			repoType := strings.TrimSpace(stringValue(repo, "type"))
			generate := boolValue(repo, "generate")
			pkgsDir := strings.TrimSpace(stringValue(repo, "pkgsDir"))
			if pkgsDir == "" {
				pkgsDir = "pkgs"
			}

			var repoRoot string
			switch repoType {
			case "apt-flat":
				repoRoot = filepath.ToSlash(filepath.Join("packages", "apt", release))
			case "yum":
				repoRoot = filepath.ToSlash(filepath.Join("packages", "yum", release))
			default:
				return nil, fmt.Errorf("DownloadPackages repo.type must be apt-flat or yum")
			}

			if files, reused, err := tryReusePackageArtifacts(bundleRoot, repoRoot, packages, opts); err != nil {
				return nil, err
			} else if reused {
				return files, nil
			}

			files, err := runContainerPackageRepoBuild(ctx, runner, bundleRoot, repoRoot, family, repoType, generate, pkgsDir, spec, packages, opts)
			if err != nil {
				return nil, err
			}
			if err := writePackageArtifactsMeta(bundleRoot, repoRoot, packages, files); err != nil {
				return nil, err
			}
			return files, nil
		}

		if files, reused, err := tryReusePackageArtifacts(bundleRoot, dir, packages, opts); err != nil {
			return nil, err
		} else if reused {
			return files, nil
		}

		files, err := runContainerPackageDownloadAll(ctx, runner, bundleRoot, dir, spec, packages, opts)
		if err != nil {
			return nil, err
		}
		if err := writePackageArtifactsMeta(bundleRoot, dir, packages, files); err != nil {
			return nil, err
		}
		return files, nil
	}

	return writePackagePlaceholders(bundleRoot, dir, packages), nil
}

func runDownloadK8sPackages(ctx context.Context, runner CommandRunner, bundleRoot string, spec map[string]any, opts RunOptions) ([]string, error) {
	output := mapValue(spec, "output")
	dir := stringValue(output, "dir")
	if dir == "" {
		dir = "packages"
	}

	version := strings.TrimPrefix(stringValue(spec, "kubernetesVersion"), "v")
	if version == "" {
		version = "0.0.0"
	}
	components := stringSlice(spec["components"])
	if len(components) == 0 {
		return nil, fmt.Errorf("DownloadK8sPackages requires components")
	}

	pkgs := append([]string{}, components...)

	distro := mapValue(spec, "distro")
	family := stringValue(distro, "family")
	if family == "" {
		family = "debian"
	}
	if family == "debian" {
		pkgs = append(pkgs, "cri-tools", "kubernetes-cni")
	}

	backend := mapValue(spec, "backend")
	if stringValue(backend, "mode") == "container" && stringValue(backend, "image") != "" {
		repo := mapValue(spec, "repo")
		if len(repo) > 0 {
			release := strings.TrimSpace(stringValue(distro, "release"))
			if release == "" {
				return nil, fmt.Errorf("DownloadK8sPackages repo mode requires distro.release")
			}

			repoType := strings.TrimSpace(stringValue(repo, "type"))
			generate := boolValue(repo, "generate")
			pkgsDir := strings.TrimSpace(stringValue(repo, "pkgsDir"))
			if pkgsDir == "" {
				pkgsDir = "pkgs"
			}

			var repoRoot string
			switch repoType {
			case "apt-flat":
				repoRoot = filepath.ToSlash(filepath.Join("packages", "apt-k8s", release))
			case "yum":
				repoRoot = filepath.ToSlash(filepath.Join("packages", "yum-k8s", release))
			default:
				return nil, fmt.Errorf("DownloadK8sPackages repo.type must be apt-flat or yum")
			}

			files, err := runContainerK8sPackageRepoBuild(ctx, runner, bundleRoot, repoRoot, family, repoType, generate, pkgsDir, version, pkgs, spec, opts)
			if err != nil {
				return nil, err
			}

			metaRel := filepath.ToSlash(filepath.Join(repoRoot, "kubernetes-version.txt"))
			metaAbs := filepath.Join(bundleRoot, filepath.FromSlash(metaRel))
			if err := os.MkdirAll(filepath.Dir(metaAbs), 0o755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(metaAbs, []byte(version+"\n"), 0o644); err != nil {
				return nil, err
			}

			return append(files, metaRel), nil
		}

		versionLine := strings.TrimSpace(version)
		files, err := runContainerPackageDownloadWithScript(ctx, runner, bundleRoot, dir, spec, pkgs, func(family, pkg string) string {
			return buildK8sPackageDownloadScript(family, pkg, versionLine)
		}, opts)
		if err != nil {
			return nil, err
		}
		metaRel := filepath.ToSlash(filepath.Join(dir, "kubernetes-version.txt"))
		metaAbs := filepath.Join(bundleRoot, metaRel)
		if err := os.MkdirAll(filepath.Dir(metaAbs), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(metaAbs, []byte(version+"\n"), 0o644); err != nil {
			return nil, err
		}
		return append(files, metaRel), nil
	}

	placeholderPkgs := make([]string, 0, len(pkgs))
	for _, p := range pkgs {
		placeholderPkgs = append(placeholderPkgs, fmt.Sprintf("%s-v%s", p, version))
	}
	files := writePackagePlaceholders(bundleRoot, dir, placeholderPkgs)
	metaRel := filepath.ToSlash(filepath.Join(dir, "kubernetes-version.txt"))
	_ = os.WriteFile(filepath.Join(bundleRoot, metaRel), []byte(version+"\n"), 0o644)
	return append(files, metaRel), nil
}

func runContainerPackageRepoBuild(
	ctx context.Context,
	runner CommandRunner,
	bundleRoot string,
	repoRoot string,
	family string,
	repoType string,
	generate bool,
	pkgsDir string,
	spec map[string]any,
	packages []string,
	opts RunOptions,
) ([]string, error) {
	backend := mapValue(spec, "backend")
	runtimeSel, err := detectRuntime(runner, stringValue(backend, "runtime"))
	if err != nil {
		return nil, err
	}
	image := stringValue(backend, "image")
	if image == "" {
		return nil, fmt.Errorf("backend.image is required for container package download")
	}

	outAbs := filepath.Join(bundleRoot, filepath.FromSlash(repoRoot))
	if opts.ForceRedownload {
		if err := os.RemoveAll(outAbs); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(outAbs, 0o755); err != nil {
		return nil, err
	}

	cmdScript := buildPackageRepoBuildScript(family, packages, repoType, generate, pkgsDir)
	args := []string{"run", "--rm", "-v", outAbs + ":/out", image, "bash", "-lc", cmdScript}
	if err := runner.Run(ctx, runtimeSel, args...); err != nil {
		return nil, fmt.Errorf("container package repo build failed: %w", err)
	}

	after, _ := listRelativeFiles(outAbs)
	files := packageFilesFromDirListing(repoRoot, after)
	if len(files) == 0 {
		return nil, fmt.Errorf("%s: no package artifacts generated in %s", errCodePrepareArtifactsEmpty, repoRoot)
	}
	return files, nil
}

func runContainerPackageDownloadAll(ctx context.Context, runner CommandRunner, bundleRoot, dir string, spec map[string]any, packages []string, opts RunOptions) ([]string, error) {
	backend := mapValue(spec, "backend")
	runtimeSel, err := detectRuntime(runner, stringValue(backend, "runtime"))
	if err != nil {
		return nil, err
	}

	image := stringValue(backend, "image")
	if image == "" {
		return nil, fmt.Errorf("backend.image is required for container package download")
	}

	distro := mapValue(spec, "distro")
	family := stringValue(distro, "family")
	if family == "" {
		family = "debian"
	}

	outAbs := filepath.Join(bundleRoot, filepath.FromSlash(dir))
	if opts.ForceRedownload {
		if err := os.RemoveAll(outAbs); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(outAbs, 0o755); err != nil {
		return nil, err
	}

	cmdScript := buildPackageDownloadAllScript(family, packages)
	args := []string{"run", "--rm", "-v", outAbs + ":/out", image, "bash", "-lc", cmdScript}
	if err := runner.Run(ctx, runtimeSel, args...); err != nil {
		return nil, fmt.Errorf("container package download failed: %w", err)
	}

	after, _ := listRelativeFiles(outAbs)
	files := packageFilesFromDirListing(dir, after)
	if len(files) == 0 {
		return nil, fmt.Errorf("%s: no package artifacts generated in %s", errCodePrepareArtifactsEmpty, dir)
	}
	return files, nil
}

func buildPackageDownloadAllScript(family string, packages []string) string {
	parts := make([]string, 0, len(packages))
	for _, p := range packages {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		parts = append(parts, "'"+shellEscape(p)+"'")
	}
	pkgList := strings.Join(parts, " ")

	if family == "rhel" {
		return fmt.Sprintf(
			"set -euo pipefail; (dnf -y install 'dnf-command(download)' >/dev/null 2>&1 || yum -y install yum-utils >/dev/null 2>&1 || true); (dnf -y download --resolve --destdir /out %s || yumdownloader --resolve --destdir /out %s)",
			pkgList,
			pkgList,
		)
	}

	return fmt.Sprintf(
		"set -euo pipefail; export DEBIAN_FRONTEND=noninteractive; apt-get update -y >/dev/null; apt-get install -y --download-only --no-install-recommends %s >/dev/null; cp -a /var/cache/apt/archives/*.deb /out/ 2>/dev/null || true",
		pkgList,
	)
}

func buildPackageRepoBuildScript(family string, packages []string, repoType string, generate bool, pkgsDir string) string {
	parts := make([]string, 0, len(packages))
	for _, p := range packages {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		parts = append(parts, "'"+shellEscape(p)+"'")
	}
	pkgList := strings.Join(parts, " ")

	if repoType == "yum" || family == "rhel" {
		gen := "true"
		if !generate {
			gen = "false"
		}
		return fmt.Sprintf(
			"set -euo pipefail; (dnf -y install 'dnf-command(download)' createrepo_c >/dev/null 2>&1 || yum -y install yum-utils createrepo_c >/dev/null 2>&1 || true); (dnf -y download --resolve --destdir /out %s || yumdownloader --resolve --destdir /out %s); if %s; then createrepo_c /out >/dev/null; fi",
			pkgList,
			pkgList,
			gen,
		)
	}

	gen := "true"
	if !generate {
		gen = "false"
	}

	safePkgsDir := strings.TrimSpace(pkgsDir)
	if safePkgsDir == "" {
		safePkgsDir = "pkgs"
	}
	return fmt.Sprintf(
		"set -euo pipefail; export DEBIAN_FRONTEND=noninteractive; apt-get update -y >/dev/null; apt-get install -y apt-utils gzip >/dev/null; mkdir -p /out/%s; apt-get install -y --download-only --no-install-recommends %s >/dev/null; cp -a /var/cache/apt/archives/*.deb /out/%s/ 2>/dev/null || true; if %s; then cd /out; apt-ftparchive packages %s > Packages; gzip -c Packages > Packages.gz; apt-ftparchive release . > Release; fi",
		shellEscape(safePkgsDir),
		pkgList,
		shellEscape(safePkgsDir),
		gen,
		shellEscape(safePkgsDir),
	)
}

func runContainerK8sPackageRepoBuild(
	ctx context.Context,
	runner CommandRunner,
	bundleRoot string,
	repoRoot string,
	family string,
	repoType string,
	generate bool,
	pkgsDir string,
	kubernetesVersion string,
	packages []string,
	spec map[string]any,
	opts RunOptions,
) ([]string, error) {
	backend := mapValue(spec, "backend")
	runtimeSel, err := detectRuntime(runner, stringValue(backend, "runtime"))
	if err != nil {
		return nil, err
	}
	image := stringValue(backend, "image")
	if image == "" {
		return nil, fmt.Errorf("backend.image is required for container package download")
	}

	outAbs := filepath.Join(bundleRoot, filepath.FromSlash(repoRoot))
	if opts.ForceRedownload {
		if err := os.RemoveAll(outAbs); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(outAbs, 0o755); err != nil {
		return nil, err
	}
	before, _ := listRelativeFiles(outAbs)

	cmdScript := buildK8sPackageRepoBuildScript(family, packages, kubernetesVersion, repoType, generate, pkgsDir)
	args := []string{"run", "--rm", "-v", outAbs + ":/out", image, "bash", "-lc", cmdScript}
	if err := runner.Run(ctx, runtimeSel, args...); err != nil {
		return nil, fmt.Errorf("container k8s package repo build failed: %w", err)
	}

	after, _ := listRelativeFiles(outAbs)
	newFiles := make([]string, 0)
	seen := map[string]bool{}
	for _, f := range before {
		seen[f] = true
	}
	for _, f := range after {
		if !seen[f] {
			newFiles = append(newFiles, filepath.ToSlash(filepath.Join(repoRoot, f)))
		}
	}
	if len(newFiles) == 0 {
		return nil, fmt.Errorf("%s: no package artifacts generated in %s", errCodePrepareArtifactsEmpty, repoRoot)
	}
	return newFiles, nil
}

func buildK8sPackageRepoBuildScript(family string, packages []string, kubernetesVersion string, repoType string, generate bool, pkgsDir string) string {
	parts := make([]string, 0, len(packages))
	for _, p := range packages {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		parts = append(parts, "'"+shellEscape(p)+"'")
	}
	pkgList := strings.Join(parts, " ")
	channel := kubernetesStableChannel(kubernetesVersion)

	if repoType == "yum" || family == "rhel" {
		gen := "true"
		if !generate {
			gen = "false"
		}
		repoURL := shellEscape(fmt.Sprintf("https://pkgs.k8s.io/core:/stable:/%s/rpm/", channel))
		return fmt.Sprintf(
			"set -euo pipefail; cat > /etc/yum.repos.d/kubernetes.repo <<'EOF'\n[kubernetes]\nname=Kubernetes\nbaseurl=%s\nenabled=1\ngpgcheck=0\nrepo_gpgcheck=0\nEOF\n(dnf -y install 'dnf-command(download)' createrepo_c >/dev/null 2>&1 || yum -y install yum-utils createrepo_c >/dev/null 2>&1 || true); (dnf -y download --resolve --destdir /out %s || yumdownloader --resolve --destdir /out %s); if %s; then createrepo_c /out >/dev/null; fi",
			repoURL,
			pkgList,
			pkgList,
			gen,
		)
	}

	gen := "true"
	if !generate {
		gen = "false"
	}
	safePkgsDir := strings.TrimSpace(pkgsDir)
	if safePkgsDir == "" {
		safePkgsDir = "pkgs"
	}
	repoURL := shellEscape(fmt.Sprintf("https://pkgs.k8s.io/core:/stable:/%s/deb/", channel))
	return fmt.Sprintf(
		"set -euo pipefail; export DEBIAN_FRONTEND=noninteractive; apt-get update -y >/dev/null; apt-get install -y ca-certificates curl gpg apt-utils gzip >/dev/null; install -d -m 0755 /etc/apt/keyrings; curl -fsSL %sRelease.key | gpg --dearmor -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg; echo 'deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] %s /' > /etc/apt/sources.list.d/kubernetes.list; apt-get update -y >/dev/null; mkdir -p /out/%s; apt-get install -y --download-only --no-install-recommends %s >/dev/null; cp -a /var/cache/apt/archives/*.deb /out/%s/ 2>/dev/null || true; if %s; then cd /out; apt-ftparchive packages %s > Packages; gzip -c Packages > Packages.gz; apt-ftparchive release . > Release; fi",
		repoURL,
		repoURL,
		shellEscape(safePkgsDir),
		pkgList,
		shellEscape(safePkgsDir),
		gen,
		shellEscape(safePkgsDir),
	)
}

func runContainerPackageDownloadWithScript(ctx context.Context, runner CommandRunner, bundleRoot, dir string, spec map[string]any, packages []string, scriptBuilder func(family, pkg string) string, opts RunOptions) ([]string, error) {
	backend := mapValue(spec, "backend")
	runtimeSel, err := detectRuntime(runner, stringValue(backend, "runtime"))
	if err != nil {
		return nil, err
	}

	image := stringValue(backend, "image")
	if image == "" {
		return nil, fmt.Errorf("backend.image is required for container package download")
	}

	distro := mapValue(spec, "distro")
	family := stringValue(distro, "family")
	if family == "" {
		family = "debian"
	}

	outAbs := filepath.Join(bundleRoot, filepath.FromSlash(dir))
	if opts.ForceRedownload {
		if err := os.RemoveAll(outAbs); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(outAbs, 0o755); err != nil {
		return nil, err
	}

	for _, pkg := range packages {
		cmdScript := scriptBuilder(family, pkg)
		args := []string{"run", "--rm", "-v", outAbs + ":/out", image, "bash", "-lc", cmdScript}
		if err := runner.Run(ctx, runtimeSel, args...); err != nil {
			return nil, fmt.Errorf("container package download failed for %s: %w", pkg, err)
		}
	}

	after, _ := listRelativeFiles(outAbs)
	files := packageFilesFromDirListing(dir, after)
	if len(files) == 0 {
		return nil, fmt.Errorf("%s: no package artifacts generated in %s", errCodePrepareArtifactsEmpty, dir)
	}
	return files, nil
}

func buildK8sPackageDownloadScript(family, pkg, version string) string {
	safePkg := shellEscape(pkg)
	channel := kubernetesStableChannel(version)
	if family == "rhel" {
		repoURL := shellEscape(fmt.Sprintf("https://pkgs.k8s.io/core:/stable:/%s/rpm/", channel))
		return fmt.Sprintf("set -euo pipefail; cat > /etc/yum.repos.d/kubernetes.repo <<'EOF'\n[kubernetes]\nname=Kubernetes\nbaseurl=%s\nenabled=1\ngpgcheck=0\nrepo_gpgcheck=0\nEOF\n(dnf -y install 'dnf-command(download)' >/dev/null 2>&1 || yum -y install yum-utils >/dev/null 2>&1 || true); (dnf -y download --destdir /out %s || yumdownloader --destdir /out %s)", repoURL, safePkg, safePkg)
	}
	repoURL := shellEscape(fmt.Sprintf("https://pkgs.k8s.io/core:/stable:/%s/deb/", channel))
	return fmt.Sprintf("set -euo pipefail; export DEBIAN_FRONTEND=noninteractive; mkdir -p /tmp/deck-pkg-download; cd /tmp/deck-pkg-download; apt-get update -y >/dev/null; apt-get install -y ca-certificates curl gpg >/dev/null; install -d -m 0755 /etc/apt/keyrings; curl -fsSL %sRelease.key | gpg --dearmor -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg; echo 'deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] %s /' > /etc/apt/sources.list.d/kubernetes.list; apt-get update -y >/dev/null; (apt-get download %s || true); cp -a ./*.deb /out/ 2>/dev/null || true", repoURL, repoURL, safePkg)
}

func kubernetesStableChannel(version string) string {
	trimmed := strings.TrimPrefix(strings.TrimSpace(version), "v")
	parts := strings.Split(trimmed, ".")
	if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
		return "v" + parts[0] + "." + parts[1]
	}
	return "v1.30"
}

func shellEscape(v string) string {
	return strings.ReplaceAll(v, "'", "''")
}

func writePackagePlaceholders(bundleRoot, dir string, packages []string) []string {
	files := make([]string, 0, len(packages))
	for _, pkg := range packages {
		filename := fmt.Sprintf("%s.txt", pkg)
		rel := filepath.ToSlash(filepath.Join(dir, filename))
		target := filepath.Join(bundleRoot, rel)
		_ = os.MkdirAll(filepath.Dir(target), 0o755)
		_ = os.WriteFile(target, []byte(fmt.Sprintf("package=%s\n", pkg)), 0o644)
		files = append(files, rel)
	}
	return files
}

func runDownloadImages(ctx context.Context, runner CommandRunner, bundleRoot string, spec map[string]any, opts RunOptions) ([]string, error) {
	_ = runner
	output := mapValue(spec, "output")
	dir := stringValue(output, "dir")
	if dir == "" {
		dir = "images"
	}

	images := stringSlice(spec["images"])
	if len(images) == 0 {
		return nil, fmt.Errorf("DownloadImages requires images")
	}

	backend := mapValue(spec, "backend")
	engine := stringValue(backend, "engine")
	if engine == "" {
		engine = "go-containerregistry"
	}

	if engine != "go-containerregistry" {
		return nil, fmt.Errorf("%s: unsupported image engine: %s", errCodePrepareEngineUnsupported, engine)
	}

	return runGoContainerRegistryDownloads(ctx, bundleRoot, dir, images, opts)
}

func inferDownloadFileName(sourcePath, sourceURL string) string {
	if strings.TrimSpace(sourcePath) != "" {
		base := filepath.Base(filepath.FromSlash(strings.TrimSpace(sourcePath)))
		if base != "" && base != "." && base != string(filepath.Separator) {
			return base
		}
	}
	if strings.TrimSpace(sourceURL) != "" {
		trimmed := strings.TrimSpace(sourceURL)
		if idx := strings.Index(trimmed, "?"); idx >= 0 {
			trimmed = trimmed[:idx]
		}
		base := filepath.Base(filepath.FromSlash(trimmed))
		if base != "" && base != "." && base != string(filepath.Separator) {
			return base
		}
	}
	return "downloaded.bin"
}

func runGoContainerRegistryDownloads(ctx context.Context, bundleRoot, dir string, images []string, opts RunOptions) ([]string, error) {
	files := make([]string, 0, len(images))
	for _, img := range images {
		rel := filepath.ToSlash(filepath.Join(dir, sanitizeImageName(img)+".tar"))
		target := filepath.Join(bundleRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		if !opts.ForceRedownload {
			if info, err := os.Stat(target); err == nil {
				if info.Size() > 0 {
					files = append(files, rel)
					continue
				}
			} else if !os.IsNotExist(err) {
				return nil, err
			}
		} else if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return nil, err
		}

		ref, err := parseImageReferenceFn(img)
		if err != nil {
			return nil, fmt.Errorf("parse image reference %s: %w", img, err)
		}

		imageObj, err := remoteImageFetchFn(
			ref,
			remote.WithAuthFromKeychain(authn.DefaultKeychain),
			remote.WithContext(ctx),
		)
		if err != nil {
			return nil, fmt.Errorf("pull image %s: %w", img, err)
		}

		if err := tarballWriteToFileFn(target, ref, imageObj); err != nil {
			return nil, fmt.Errorf("write image archive %s: %w", img, err)
		}

		if info, err := os.Stat(target); err != nil {
			return nil, err
		} else if info.Size() == 0 {
			return nil, fmt.Errorf("write image archive %s: empty archive", img)
		}

		files = append(files, rel)
	}
	return files, nil
}

func fileSHA256(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func canReuseDownloadFile(bundleRoot string, spec map[string]any, target string, opts RunOptions) (bool, error) {
	if opts.ForceRedownload {
		return false, nil
	}
	info, err := os.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if info.Size() == 0 {
		return false, nil
	}

	source := mapValue(spec, "source")
	expectedSHA := strings.ToLower(stringValue(source, "sha256"))
	if expectedSHA != "" {
		if err := verifyFileSHA256(target, expectedSHA); err == nil {
			return true, nil
		}
		return false, nil
	}

	sourcePath := stringValue(source, "path")
	if sourcePath == "" {
		return false, nil
	}
	raw, err := resolveSourceBytes(spec, sourcePath)
	if err != nil {
		return false, nil
	}
	targetSHA, err := fileSHA256(target)
	if err != nil {
		return false, err
	}
	sum := sha256.Sum256(raw)
	return strings.EqualFold(targetSHA, hex.EncodeToString(sum[:])), nil
}

func packageFilesFromDirListing(base string, relFiles []string) []string {
	out := make([]string, 0, len(relFiles))
	for _, f := range relFiles {
		if filepath.ToSlash(f) == packageCacheMetaFile {
			continue
		}
		out = append(out, filepath.ToSlash(filepath.Join(base, f)))
	}
	sort.Strings(out)
	return out
}

func normalizeStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		s := strings.TrimSpace(v)
		if s == "" {
			continue
		}
		out = append(out, filepath.ToSlash(s))
	}
	sort.Strings(out)
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func packageMetaFileAbs(bundleRoot, rootRel string) string {
	return filepath.Join(bundleRoot, filepath.FromSlash(rootRel), packageCacheMetaFile)
}

func tryReusePackageArtifacts(bundleRoot, rootRel string, packages []string, opts RunOptions) ([]string, bool, error) {
	if opts.ForceRedownload {
		return nil, false, nil
	}
	metaPath := packageMetaFileAbs(bundleRoot, rootRel)
	raw, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var meta packageCacheMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, false, nil
	}
	want := normalizeStrings(packages)
	got := normalizeStrings(meta.Packages)
	if !equalStrings(want, got) {
		return nil, false, nil
	}
	files := normalizeStrings(meta.Files)
	if len(files) == 0 {
		return nil, false, nil
	}
	for _, rel := range files {
		abs := filepath.Join(bundleRoot, filepath.FromSlash(rel))
		info, statErr := os.Stat(abs)
		if statErr != nil || info.Size() == 0 {
			return nil, false, nil
		}
	}
	return files, true, nil
}

func writePackageArtifactsMeta(bundleRoot, rootRel string, packages, files []string) error {
	meta := packageCacheMeta{
		Packages: normalizeStrings(packages),
		Files:    normalizeStrings(files),
	}
	metaPath := packageMetaFileAbs(bundleRoot, rootRel)
	if err := os.MkdirAll(filepath.Dir(metaPath), 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, raw, 0o644)
}

func detectRuntime(runner CommandRunner, preferred string) (string, error) {
	pref := strings.TrimSpace(preferred)
	if pref == "" {
		pref = "auto"
	}

	if pref == "auto" {
		for _, candidate := range []string{"docker", "podman"} {
			if _, err := runner.LookPath(candidate); err == nil {
				return candidate, nil
			}
		}
		return "", fmt.Errorf("%s: no supported container runtime found (docker/podman)", errCodePrepareRuntimeMissing)
	}

	if pref != "docker" && pref != "podman" {
		return "", fmt.Errorf("%s: unsupported runtime: %s", errCodePrepareRuntimeUnsupported, pref)
	}
	if _, err := runner.LookPath(pref); err != nil {
		return "", fmt.Errorf("%s: runtime not found: %s", errCodePrepareRuntimeMissing, pref)
	}
	return pref, nil
}

func sanitizeImageName(v string) string {
	replacer := strings.NewReplacer(
		"/", "_",
		":", "_",
		"@", "_",
	)
	return replacer.Replace(v)
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
