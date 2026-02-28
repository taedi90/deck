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
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/taedi90/deck/internal/config"
)

type RunOptions struct {
	BundleRoot    string
	CommandRunner CommandRunner
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

var templateRefPattern = regexp.MustCompile(`\{\s*\.([A-Za-z0-9_\.]+)\s*\}`)

const (
	errCodePrepareRuntimeMissing     = "E_PREPARE_RUNTIME_NOT_FOUND"
	errCodePrepareRuntimeUnsupported = "E_PREPARE_RUNTIME_UNSUPPORTED"
	errCodePrepareArtifactsEmpty     = "E_PREPARE_NO_ARTIFACTS"
	errCodePrepareSkopeoMissing      = "E_PREPARE_SKOPEO_NOT_FOUND"
)

func Run(wf *config.Workflow, opts RunOptions) error {
	if wf == nil {
		return fmt.Errorf("workflow is nil")
	}

	bundleRoot := strings.TrimSpace(opts.BundleRoot)
	if bundleRoot == "" {
		bundleRoot = strings.TrimSpace(wf.Context.BundleRoot)
	}
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

	for _, step := range preparePhase.Steps {
		rendered := renderSpec(step.Spec, wf, runtimeVars)
		var stepFiles []string

		switch step.Kind {
		case "DownloadFile":
			f, err := runDownloadFile(bundleRoot, rendered)
			if err != nil {
				return fmt.Errorf("step %s (%s): %w", step.ID, step.Kind, err)
			}
			stepFiles = append(stepFiles, f)
		case "DownloadPackages":
			files, err := runDownloadPackages(runner, bundleRoot, rendered, "packages/os")
			if err != nil {
				return fmt.Errorf("step %s (%s): %w", step.ID, step.Kind, err)
			}
			stepFiles = append(stepFiles, files...)
		case "DownloadK8sPackages":
			files, err := runDownloadK8sPackages(runner, bundleRoot, rendered)
			if err != nil {
				return fmt.Errorf("step %s (%s): %w", step.ID, step.Kind, err)
			}
			stepFiles = append(stepFiles, files...)
		case "DownloadImages":
			files, err := runDownloadImages(runner, bundleRoot, rendered)
			if err != nil {
				return fmt.Errorf("step %s (%s): %w", step.ID, step.Kind, err)
			}
			stepFiles = append(stepFiles, files...)
		default:
			continue
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
	manifestPath := filepath.Join(bundleRoot, "manifest.json")
	if err := writeManifest(manifestPath, dedupeEntries(entries)); err != nil {
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

func runDownloadFile(bundleRoot string, spec map[string]any) (string, error) {
	source := mapValue(spec, "source")
	output := mapValue(spec, "output")
	url := stringValue(source, "url")
	outPath := stringValue(output, "path")
	if strings.TrimSpace(url) == "" || strings.TrimSpace(outPath) == "" {
		return "", fmt.Errorf("DownloadFile requires source.url and output.path")
	}

	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return "", fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("download %s: unexpected status %d", url, resp.StatusCode)
	}

	target := filepath.Join(bundleRoot, outPath)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", fmt.Errorf("create output directory: %w", err)
	}

	f, err := os.Create(target)
	if err != nil {
		return "", fmt.Errorf("create output file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return "", fmt.Errorf("write output file: %w", err)
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

func runDownloadPackages(runner CommandRunner, bundleRoot string, spec map[string]any, defaultDir string) ([]string, error) {
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
		return runContainerPackageDownload(runner, bundleRoot, dir, spec, packages)
	}

	return writePackagePlaceholders(bundleRoot, dir, packages), nil
}

func runDownloadK8sPackages(runner CommandRunner, bundleRoot string, spec map[string]any) ([]string, error) {
	output := mapValue(spec, "output")
	dir := stringValue(output, "dir")
	if dir == "" {
		dir = "packages/k8s"
	}

	version := strings.TrimPrefix(stringValue(spec, "kubernetesVersion"), "v")
	if version == "" {
		version = "0.0.0"
	}
	components := stringSlice(spec["components"])
	if len(components) == 0 {
		return nil, fmt.Errorf("DownloadK8sPackages requires components")
	}

	pkgs := make([]string, 0, len(components))
	for _, c := range components {
		pkgs = append(pkgs, c)
	}

	backend := mapValue(spec, "backend")
	if stringValue(backend, "mode") == "container" && stringValue(backend, "image") != "" {
		files, err := runContainerPackageDownload(runner, bundleRoot, dir, spec, pkgs)
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

func runContainerPackageDownload(runner CommandRunner, bundleRoot, dir string, spec map[string]any, packages []string) ([]string, error) {
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
	if err := os.MkdirAll(outAbs, 0o755); err != nil {
		return nil, err
	}
	before, _ := listRelativeFiles(outAbs)

	for _, pkg := range packages {
		cmdScript := buildPackageDownloadScript(family, pkg)
		args := []string{"run", "--rm", "-v", outAbs + ":/out", image, "bash", "-lc", cmdScript}
		if err := runner.Run(context.Background(), runtimeSel, args...); err != nil {
			return nil, fmt.Errorf("container package download failed for %s: %w", pkg, err)
		}
	}

	after, _ := listRelativeFiles(outAbs)
	newFiles := make([]string, 0)
	seen := map[string]bool{}
	for _, f := range before {
		seen[f] = true
	}
	for _, f := range after {
		if !seen[f] {
			newFiles = append(newFiles, filepath.ToSlash(filepath.Join(dir, f)))
		}
	}
	if len(newFiles) == 0 {
		return nil, fmt.Errorf("%s: no package artifacts generated in %s", errCodePrepareArtifactsEmpty, dir)
	}
	return newFiles, nil
}

func buildPackageDownloadScript(family, pkg string) string {
	safePkg := shellEscape(pkg)
	if family == "rhel" {
		return fmt.Sprintf("set -euo pipefail; (dnf -y install 'dnf-command(download)' >/dev/null 2>&1 || yum -y install yum-utils >/dev/null 2>&1 || true); (dnf -y download --destdir /out %s || yumdownloader --destdir /out %s)", safePkg, safePkg)
	}
	return fmt.Sprintf("set -euo pipefail; apt-get update -y >/dev/null; (apt-get download %s || true); cp -a ./*.deb /out/ 2>/dev/null || true", safePkg)
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

func runDownloadImages(runner CommandRunner, bundleRoot string, spec map[string]any) ([]string, error) {
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
		engine = "skopeo"
	}

	if engine == "skopeo" && len(backend) > 0 {
		return runSkopeoDownloads(runner, bundleRoot, dir, images, backend)
	}

	files := make([]string, 0, len(images))
	for _, img := range images {
		safe := sanitizeImageName(img)
		rel := filepath.ToSlash(filepath.Join(dir, safe+".tar"))
		target := filepath.Join(bundleRoot, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		content := fmt.Sprintf("image=%s\n", img)
		if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
			return nil, err
		}
		files = append(files, rel)
	}
	return files, nil
}

func runSkopeoDownloads(runner CommandRunner, bundleRoot, dir string, images []string, backend map[string]any) ([]string, error) {
	files := make([]string, 0, len(images))
	sandbox := mapValue(backend, "sandbox")
	useSandbox := stringValue(sandbox, "mode") == "container"

	absBundle, err := filepath.Abs(bundleRoot)
	if err != nil {
		return nil, err
	}

	var runtimeSel string
	var sandboxImage string
	if useSandbox {
		runtimeSel, err = detectRuntime(runner, stringValue(sandbox, "runtime"))
		if err != nil {
			return nil, err
		}
		sandboxImage = stringValue(sandbox, "image")
		if sandboxImage == "" {
			sandboxImage = "quay.io/skopeo/stable:latest"
		}
	} else {
		if _, err := runner.LookPath("skopeo"); err != nil {
			return nil, fmt.Errorf("%s: skopeo not found in PATH", errCodePrepareSkopeoMissing)
		}
	}

	for _, img := range images {
		rel := filepath.ToSlash(filepath.Join(dir, sanitizeImageName(img)+".tar"))
		abs := filepath.Join(bundleRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return nil, err
		}

		dest := "docker-archive:" + abs + ":" + img
		if useSandbox {
			bundleMountDest := "/bundle/" + rel
			args := []string{"run", "--rm", "-v", absBundle + ":/bundle", sandboxImage, "skopeo", "copy", "docker://" + img, "docker-archive:" + bundleMountDest + ":" + img}
			if err := runner.Run(context.Background(), runtimeSel, args...); err != nil {
				return nil, fmt.Errorf("skopeo sandbox copy failed for %s: %w", img, err)
			}
		} else {
			if err := runner.Run(context.Background(), "skopeo", "copy", "docker://"+img, dest); err != nil {
				return nil, fmt.Errorf("skopeo copy failed for %s: %w", img, err)
			}
		}
		files = append(files, rel)
	}
	return files, nil
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
