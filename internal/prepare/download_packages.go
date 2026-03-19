package prepare

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/taedi90/deck/internal/filemode"
	"github.com/taedi90/deck/internal/fsutil"
	"github.com/taedi90/deck/internal/userdirs"
)

type packageCacheMeta struct {
	Packages []string `json:"packages"`
	Files    []string `json:"files"`
}

type packageCacheMount struct {
	host      string
	container string
}

type rpmModuleSpec struct {
	Name   string
	Stream string
}

func runPackagesDownload(ctx context.Context, runner CommandRunner, bundleRoot string, spec map[string]any, defaultDir string, opts RunOptions) ([]string, error) {
	output := mapValue(spec, "output")
	dir := stringValue(output, "dir")
	if dir == "" {
		dir = defaultDir
	}

	packages := stringSlice(spec["packages"])
	if len(packages) == 0 {
		return nil, fmt.Errorf("packages action download requires packages")
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
				return nil, fmt.Errorf("packages action download repo mode requires distro.release")
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
				return nil, fmt.Errorf("packages action download repo.type must be apt-flat or yum")
			}

			if files, reused, err := tryReusePackageArtifacts(bundleRoot, repoRoot, packages, opts); err != nil {
				return nil, err
			} else if reused {
				return files, nil
			}

			files, err := runContainerPackageRepoBuild(ctx, runner, bundleRoot, repoRoot, family, repoType, generate, pkgsDir, spec, repo, packages, opts)
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

		files, err := runContainerPackageDownloadAll(ctx, runner, bundleRoot, dir, spec, repo, packages, opts)
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
	repo map[string]any,
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
	if err := filemode.EnsureDir(outAbs, filemode.PublishedArtifact); err != nil {
		return nil, err
	}

	modules, err := parseRPMModules(repo)
	if err != nil {
		return nil, err
	}
	cmdScript, err := buildPackageRepoBuildScript(family, packages, modules, repoType, generate, pkgsDir)
	if err != nil {
		return nil, err
	}
	cacheKey, err := packageDownloadCacheKey(spec, family, repoRoot)
	if err != nil {
		return nil, err
	}
	mounts, err := preparePackageCacheMounts(family, cacheKey)
	if err != nil {
		return nil, err
	}
	if err := runPackageDownloadContainer(ctx, runner, runtimeSel, image, outAbs, cmdScript, mounts); err != nil {
		return nil, fmt.Errorf("container package repo build failed: %w", err)
	}

	after, _ := listRelativeFiles(outAbs)
	files := packageFilesFromDirListing(repoRoot, after)
	if len(files) == 0 {
		return nil, fmt.Errorf("%s: no package artifacts generated in %s", errCodePrepareArtifactsEmpty, repoRoot)
	}
	return files, nil
}

func runContainerPackageDownloadAll(ctx context.Context, runner CommandRunner, bundleRoot, dir string, spec map[string]any, repo map[string]any, packages []string, opts RunOptions) ([]string, error) {
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
	if err := filemode.EnsureDir(outAbs, filemode.PublishedArtifact); err != nil {
		return nil, err
	}

	modules, err := parseRPMModules(repo)
	if err != nil {
		return nil, err
	}
	cmdScript, err := buildPackageDownloadAllScript(family, packages, modules)
	if err != nil {
		return nil, err
	}
	cacheKey, err := packageDownloadCacheKey(spec, family, dir)
	if err != nil {
		return nil, err
	}
	mounts, err := preparePackageCacheMounts(family, cacheKey)
	if err != nil {
		return nil, err
	}
	if err := runPackageDownloadContainer(ctx, runner, runtimeSel, image, outAbs, cmdScript, mounts); err != nil {
		return nil, fmt.Errorf("container package download failed: %w", err)
	}

	after, _ := listRelativeFiles(outAbs)
	files := packageFilesFromDirListing(dir, after)
	if len(files) == 0 {
		return nil, fmt.Errorf("%s: no package artifacts generated in %s", errCodePrepareArtifactsEmpty, dir)
	}
	return files, nil
}

func buildPackageDownloadAllScript(family string, packages []string, modules []rpmModuleSpec) (string, error) {
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
		moduleEnable, err := buildRPMModuleEnableCommand(modules)
		if err != nil {
			return "", err
		}
		if moduleEnable != "" {
			return fmt.Sprintf(
				"set -euo pipefail; mkdir -p /out; dnf -y install 'dnf-command(download)' >/dev/null 2>&1 || true; %s; dnf -y download --resolve --destdir /out %s",
				moduleEnable,
				pkgList,
			), nil
		}
		return fmt.Sprintf(
			"set -euo pipefail; mkdir -p /out; (dnf -y install 'dnf-command(download)' >/dev/null 2>&1 || yum -y install yum-utils >/dev/null 2>&1 || true); (dnf -y download --resolve --destdir /out %s || yumdownloader --resolve --destdir /out %s)",
			pkgList,
			pkgList,
		), nil
	}

	return fmt.Sprintf(
		"set -euo pipefail; export DEBIAN_FRONTEND=noninteractive; mkdir -p /out; apt-get update -y >/dev/null; apt-get install -y --download-only --no-install-recommends %s >/dev/null; cp -a /var/cache/apt/archives/*.deb /out/ 2>/dev/null || true",
		pkgList,
	), nil
}

func buildPackageRepoBuildScript(family string, packages []string, modules []rpmModuleSpec, repoType string, generate bool, pkgsDir string) (string, error) {
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
		moduleEnable, err := buildRPMModuleEnableCommand(modules)
		if err != nil {
			return "", err
		}
		if moduleEnable != "" {
			return fmt.Sprintf(
				"set -euo pipefail; mkdir -p /out; dnf -y install 'dnf-command(download)' createrepo_c >/dev/null 2>&1 || true; %s; dnf -y download --resolve --destdir /out %s; if %s; then createrepo_c /out >/dev/null; fi",
				moduleEnable,
				pkgList,
				gen,
			), nil
		}
		return fmt.Sprintf(
			"set -euo pipefail; mkdir -p /out; (dnf -y install 'dnf-command(download)' createrepo_c >/dev/null 2>&1 || yum -y install yum-utils createrepo_c >/dev/null 2>&1 || true); (dnf -y download --resolve --destdir /out %s || yumdownloader --resolve --destdir /out %s); if %s; then createrepo_c /out >/dev/null; fi",
			pkgList,
			pkgList,
			gen,
		), nil
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
		"set -euo pipefail; export DEBIAN_FRONTEND=noninteractive; mkdir -p /out/%s; apt-get update -y >/dev/null; apt-get install -y apt-utils gzip >/dev/null; apt-get install -y --download-only --no-install-recommends %s >/dev/null; cp -a /var/cache/apt/archives/*.deb /out/%s/ 2>/dev/null || true; if %s; then cd /out; apt-ftparchive packages %s > Packages; gzip -c Packages > Packages.gz; apt-ftparchive release . > Release; fi",
		shellEscape(safePkgsDir),
		pkgList,
		shellEscape(safePkgsDir),
		gen,
		shellEscape(safePkgsDir),
	), nil
}

func shellEscape(v string) string {
	return strings.ReplaceAll(v, "'", "''")
}

func parseRPMModules(repo map[string]any) ([]rpmModuleSpec, error) {
	raw, ok := repo["modules"]
	if !ok || raw == nil {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("packages action download repo.modules must be an array")
	}
	modules := make([]rpmModuleSpec, 0, len(items))
	for _, item := range items {
		moduleMap, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("packages action download repo.modules entries must be objects")
		}
		name := strings.TrimSpace(stringValue(moduleMap, "name"))
		stream := strings.TrimSpace(stringValue(moduleMap, "stream"))
		if name == "" || stream == "" {
			return nil, fmt.Errorf("packages action download repo.modules entries require name and stream")
		}
		modules = append(modules, rpmModuleSpec{Name: name, Stream: stream})
	}
	sort.Slice(modules, func(i, j int) bool {
		if modules[i].Name == modules[j].Name {
			return modules[i].Stream < modules[j].Stream
		}
		return modules[i].Name < modules[j].Name
	})
	return modules, nil
}

func buildRPMModuleEnableCommand(modules []rpmModuleSpec) (string, error) {
	if len(modules) == 0 {
		return "", nil
	}
	parts := make([]string, 0, len(modules))
	for _, module := range modules {
		parts = append(parts, "'"+shellEscape(module.Name)+":"+shellEscape(module.Stream)+"'")
	}
	return "dnf -y module enable " + strings.Join(parts, " ") + " >/dev/null 2>&1", nil
}

func packageDownloadCacheKey(spec map[string]any, family string, artifactRoot string) (string, error) {
	repo := mapValue(spec, "repo")
	modules, err := parseRPMModules(repo)
	if err != nil {
		return "", err
	}
	normalizedRepo := map[string]any{}
	for key, value := range repo {
		if key == "modules" {
			continue
		}
		normalizedRepo[key] = value
	}
	if len(modules) > 0 {
		normalizedModules := make([]map[string]any, 0, len(modules))
		for _, module := range modules {
			normalizedModules = append(normalizedModules, map[string]any{"name": module.Name, "stream": module.Stream})
		}
		normalizedRepo["modules"] = normalizedModules
	}
	payload := map[string]any{
		"artifactRoot": filepath.ToSlash(strings.TrimSpace(artifactRoot)),
		"backend":      mapValue(spec, "backend"),
		"distro":       mapValue(spec, "distro"),
		"family":       strings.TrimSpace(family),
		"repo":         normalizedRepo,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode package cache key: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func preparePackageCacheMounts(family string, cacheKey string) ([]packageCacheMount, error) {
	cacheRoot, err := userdirs.CacheRoot()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(cacheRoot, "packages", cacheKey)
	if _, err := os.Stat(root); err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("stat package cache root: %w", err)
		}
		legacyPath, ok, legacyErr := resolveLegacyPackageCacheRoot(cacheKey)
		if legacyErr != nil {
			return nil, legacyErr
		}
		if ok {
			root = legacyPath
		}
	}
	if strings.TrimSpace(family) == "rhel" {
		dnfRoot := filepath.Join(root, "dnf")
		if err := filemode.EnsureDir(dnfRoot, filemode.PublishedArtifact); err != nil {
			return nil, err
		}
		return []packageCacheMount{{host: dnfRoot, container: "/var/cache/dnf"}}, nil
	}

	archives := filepath.Join(root, "apt", "archives")
	lists := filepath.Join(root, "apt", "lists")
	for _, dir := range []string{archives, filepath.Join(archives, "partial"), lists, filepath.Join(lists, "partial")} {
		if err := filemode.EnsureDir(dir, filemode.PublishedArtifact); err != nil {
			return nil, err
		}
	}
	return []packageCacheMount{
		{host: archives, container: "/var/cache/apt/archives"},
		{host: lists, container: "/var/lib/apt/lists"},
	}, nil
}

func runPackageDownloadContainer(ctx context.Context, runner CommandRunner, runtimeSel, image, outAbs, script string, mounts []packageCacheMount) error {
	runArgs := []string{"run", "--rm", "-v", outAbs + ":/out"}
	for _, mount := range mounts {
		runArgs = append(runArgs, "-v", mount.host+":"+mount.container)
	}
	runArgs = append(runArgs, image, "bash", "-lc", script)
	return runner.Run(ctx, runtimeSel, runArgs...)
}

func writePackagePlaceholders(bundleRoot, dir string, packages []string) []string {
	files := make([]string, 0, len(packages))
	for _, pkg := range packages {
		filename := fmt.Sprintf("%s.txt", pkg)
		rel := filepath.ToSlash(filepath.Join(dir, filename))
		target := filepath.Join(bundleRoot, rel)
		_ = filemode.EnsureParentArtifactDir(target)
		_ = filemode.WriteArtifactFile(target, []byte(fmt.Sprintf("package=%s\n", pkg)))
		files = append(files, rel)
	}
	return files
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
	raw, err := fsutil.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var meta packageCacheMeta
	if decodeErr := json.Unmarshal(raw, &meta); decodeErr != nil {
		return nil, false, fmt.Errorf("decode package cache metadata: %w", decodeErr)
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
		if statErr != nil {
			return nil, false, fmt.Errorf("stat cached package artifact %s: %w", abs, statErr)
		}
		if info.Size() == 0 {
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
	if err := filemode.EnsureParentArtifactDir(metaPath); err != nil {
		return err
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return filemode.WriteArtifactFile(metaPath, raw)
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
