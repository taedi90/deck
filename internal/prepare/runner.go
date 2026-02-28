package prepare

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/taedi90/deck/internal/config"
)

type RunOptions struct {
	BundleRoot string
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
			files, err := runDownloadPackages(bundleRoot, rendered, "packages/os")
			if err != nil {
				return fmt.Errorf("step %s (%s): %w", step.ID, step.Kind, err)
			}
			stepFiles = append(stepFiles, files...)
		case "DownloadK8sPackages":
			files, err := runDownloadK8sPackages(bundleRoot, rendered)
			if err != nil {
				return fmt.Errorf("step %s (%s): %w", step.ID, step.Kind, err)
			}
			stepFiles = append(stepFiles, files...)
		case "DownloadImages":
			files, err := runDownloadImages(bundleRoot, rendered)
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
	if err := writeManifest(manifestPath, entries); err != nil {
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

func runDownloadPackages(bundleRoot string, spec map[string]any, defaultDir string) ([]string, error) {
	output := mapValue(spec, "output")
	dir := stringValue(output, "dir")
	if dir == "" {
		dir = defaultDir
	}

	packages := stringSlice(spec["packages"])
	if len(packages) == 0 {
		return nil, fmt.Errorf("DownloadPackages requires packages")
	}

	files := make([]string, 0, len(packages))
	for _, pkg := range packages {
		filename := fmt.Sprintf("%s.txt", pkg)
		rel := filepath.ToSlash(filepath.Join(dir, filename))
		target := filepath.Join(bundleRoot, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		content := fmt.Sprintf("package=%s\n", pkg)
		if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
			return nil, err
		}
		files = append(files, rel)
	}
	return files, nil
}

func runDownloadK8sPackages(bundleRoot string, spec map[string]any) ([]string, error) {
	output := mapValue(spec, "output")
	dir := stringValue(output, "dir")
	if dir == "" {
		dir = "packages/k8s"
	}

	version := stringValue(spec, "kubernetesVersion")
	if version == "" {
		version = "v0.0.0"
	}
	components := stringSlice(spec["components"])
	if len(components) == 0 {
		return nil, fmt.Errorf("DownloadK8sPackages requires components")
	}

	files := make([]string, 0, len(components))
	for _, c := range components {
		filename := fmt.Sprintf("%s-%s.txt", c, version)
		rel := filepath.ToSlash(filepath.Join(dir, filename))
		target := filepath.Join(bundleRoot, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		content := fmt.Sprintf("component=%s\nversion=%s\n", c, version)
		if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
			return nil, err
		}
		files = append(files, rel)
	}

	return files, nil
}

func runDownloadImages(bundleRoot string, spec map[string]any) ([]string, error) {
	output := mapValue(spec, "output")
	dir := stringValue(output, "dir")
	if dir == "" {
		dir = "images"
	}

	images := stringSlice(spec["images"])
	if len(images) == 0 {
		return nil, fmt.Errorf("DownloadImages requires images")
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
