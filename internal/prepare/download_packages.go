package prepare

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/taedi90/deck/internal/config"
	"github.com/taedi90/deck/internal/errcode"
	"github.com/taedi90/deck/internal/filemode"
	"github.com/taedi90/deck/internal/fsutil"
	"github.com/taedi90/deck/internal/stepspec"
	"github.com/taedi90/deck/internal/workflowexec"
)

type packageCacheMeta struct {
	Packages []string `json:"packages"`
	Files    []string `json:"files"`
}

const containerOutputRoot = "/out"

type rpmModuleSpec struct {
	Name   string
	Stream string
}

func runDownloadPackage(ctx context.Context, runner CommandRunner, bundleRoot string, step config.Step, spec map[string]any, inputVars map[string]string, defaultDir string, opts RunOptions) ([]string, error) {
	decoded, err := workflowexec.DecodeSpec[stepspec.DownloadPackage](spec)
	if err != nil {
		return nil, fmt.Errorf("decode DownloadPackage spec: %w", err)
	}
	dir := strings.TrimSpace(decoded.OutputDir)
	if dir == "" {
		dir = defaultDir
	}

	packages := decoded.Packages
	if len(packages) == 0 {
		return nil, fmt.Errorf("packages action download requires packages")
	}

	if decoded.Backend.Mode == "container" && decoded.Backend.Image != "" {
		repo := decoded.Repo
		if repo.Type != "" || repo.Generate || repo.PkgsDir != "" || len(repo.Modules) > 0 {
			family := decoded.Distro.Family
			if family == "" {
				family = "debian"
			}
			release := strings.TrimSpace(decoded.Distro.Release)
			if release == "" {
				return nil, fmt.Errorf("packages action download repo mode requires distro.release")
			}

			repoType := strings.TrimSpace(repo.Type)
			generate := repo.Generate
			pkgsDir := strings.TrimSpace(repo.PkgsDir)
			if pkgsDir == "" {
				pkgsDir = "pkgs"
			}

			var repoRoot string
			switch repoType {
			case "deb-flat":
				repoRoot = filepath.ToSlash(filepath.Join("packages", "deb", release))
			case "rpm":
				repoRoot = filepath.ToSlash(filepath.Join("packages", "rpm", release))
			default:
				return nil, fmt.Errorf("packages action download repo.type must be deb-flat or rpm")
			}

			if files, reused, err := tryReusePackageArtifact(bundleRoot, repoRoot, packages, opts); err != nil {
				return nil, err
			} else if reused {
				return files, nil
			}
			files, err := runContainerPackageRepoBuild(ctx, runner, bundleRoot, repoRoot, family, repoType, generate, pkgsDir, step, decoded, inputVars, packages, opts)
			if err != nil {
				return nil, err
			}
			if err := writePackageArtifactMeta(bundleRoot, repoRoot, packages, files); err != nil {
				return nil, err
			}
			return files, nil
		}

		if files, reused, err := tryReusePackageArtifact(bundleRoot, dir, packages, opts); err != nil {
			return nil, err
		} else if reused {
			return files, nil
		}

		files, err := runContainerDownloadPackageAll(ctx, runner, bundleRoot, dir, step, decoded, inputVars, packages, opts)
		if err != nil {
			return nil, err
		}
		if err := writePackageArtifactMeta(bundleRoot, dir, packages, files); err != nil {
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
	step config.Step,
	spec stepspec.DownloadPackage,
	inputVars map[string]string,
	packages []string,
	opts RunOptions,
) ([]string, error) {
	runtimeSel, err := detectRuntime(runner, spec.Backend.Runtime)
	if err != nil {
		return nil, err
	}
	image := spec.Backend.Image
	if image == "" {
		return nil, fmt.Errorf("backend.image is required for container package download")
	}

	modules, err := parseRPMModules(spec.Repo)
	if err != nil {
		return nil, err
	}
	cmdScript, err := buildPackageRepoBuildScript(family, packages, modules, repoType, generate, pkgsDir)
	if err != nil {
		return nil, err
	}
	return runContainerDownloadPackageToCache(ctx, runner, runtimeSel, image, bundleRoot, repoRoot, step, inputVars, packages, cmdScript, opts)
}

func runContainerDownloadPackageAll(ctx context.Context, runner CommandRunner, bundleRoot, dir string, step config.Step, decoded stepspec.DownloadPackage, inputVars map[string]string, packages []string, opts RunOptions) ([]string, error) {
	runtimeSel, err := detectRuntime(runner, decoded.Backend.Runtime)
	if err != nil {
		return nil, err
	}

	image := decoded.Backend.Image
	if image == "" {
		return nil, fmt.Errorf("backend.image is required for container package download")
	}

	family := decoded.Distro.Family
	if family == "" {
		family = "debian"
	}

	modules, err := parseRPMModules(decoded.Repo)
	if err != nil {
		return nil, err
	}
	cmdScript, err := buildDownloadPackageAllScript(family, packages, modules)
	if err != nil {
		return nil, err
	}
	return runContainerDownloadPackageToCache(ctx, runner, runtimeSel, image, bundleRoot, dir, step, inputVars, packages, cmdScript, opts)
}

func buildDownloadPackageAllScript(family string, packages []string, modules []rpmModuleSpec) (string, error) {
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
		"set -euo pipefail; export DEBIAN_FRONTEND=noninteractive; mkdir -p %s; apt-get update -y >/dev/null; apt-get install -y --download-only --no-install-recommends %s >/dev/null; cp -a /var/cache/apt/archives/*.deb %s/ 2>/dev/null || true",
		shellEscape(containerOutputRoot),
		pkgList,
		shellEscape(containerOutputRoot),
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

	if repoType == "rpm" || family == "rhel" {
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
		"set -euo pipefail; export DEBIAN_FRONTEND=noninteractive; mkdir -p %s/%s; apt-get update -y >/dev/null; apt-get install -y apt-utils gzip >/dev/null; apt-get install -y --download-only --no-install-recommends %s >/dev/null; cp -a /var/cache/apt/archives/*.deb %s/%s/ 2>/dev/null || true; if %s; then cd %s; apt-ftparchive packages %s > Packages; gzip -c Packages > Packages.gz; apt-ftparchive release . > Release; fi",
		shellEscape(containerOutputRoot),
		shellEscape(safePkgsDir),
		pkgList,
		shellEscape(containerOutputRoot),
		shellEscape(safePkgsDir),
		gen,
		shellEscape(containerOutputRoot),
		shellEscape(safePkgsDir),
	), nil
}

func shellEscape(v string) string {
	return strings.ReplaceAll(v, "'", "''")
}

func parseRPMModules(repo stepspec.DownloadPackageRepo) ([]rpmModuleSpec, error) {
	if len(repo.Modules) == 0 {
		return nil, nil
	}
	modules := make([]rpmModuleSpec, 0, len(repo.Modules))
	for _, item := range repo.Modules {
		name := strings.TrimSpace(item.Name)
		stream := strings.TrimSpace(item.Stream)
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

func runContainerDownloadPackageToCache(ctx context.Context, runner CommandRunner, runtimeSel, image, bundleRoot, rootRel string, step config.Step, inputVars map[string]string, packages []string, script string, opts RunOptions) ([]string, error) {
	cacheKey := computeStepCacheKey(step)
	cachePath, err := exportedPackageCachePath(cacheKey, inputVars)
	if err != nil {
		return nil, err
	}
	if files, reused, err := tryReuseExportedPackageArtifact(bundleRoot, rootRel, cachePath, packages, opts); err != nil {
		return nil, err
	} else if reused {
		if err := writePackageArtifactMeta(bundleRoot, rootRel, packages, files); err != nil {
			return nil, err
		}
		return files, nil
	}
	exported, err := runPackageContainerWithExport(ctx, runner, runtimeSel, image, script)
	if err != nil {
		return nil, err
	}
	cacheStage := buildExportedPackageCacheStage(cachePath)
	relFiles, err := exportContainerTarToStage(exported, cacheStage)
	if err != nil {
		_ = os.RemoveAll(cacheStage)
		return nil, err
	}
	if len(relFiles) == 0 {
		_ = os.RemoveAll(cacheStage)
		return nil, errcode.Newf(errCodePrepareArtifactEmpty, "no package artifacts generated in %s", rootRel)
	}
	meta := exportedPackageCacheMeta{RootRel: rootRel, Packages: packages, Files: relFiles}
	if err := saveExportedPackageCacheMeta(cacheStage, meta); err != nil {
		_ = os.RemoveAll(cacheStage)
		return nil, err
	}
	if err := replacePublishedArtifactDir(cacheStage, cachePath); err != nil {
		_ = os.RemoveAll(cacheStage)
		return nil, err
	}
	if err := publishCachedPackageArtifact(bundleRoot, rootRel, cachePath, relFiles); err != nil {
		return nil, err
	}
	files := packageFilesFromDirListing(rootRel, relFiles)
	if err := writePackageArtifactMeta(bundleRoot, rootRel, packages, files); err != nil {
		return nil, err
	}
	return files, nil
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

func tryReusePackageArtifact(bundleRoot, rootRel string, packages []string, opts RunOptions) ([]string, bool, error) {
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

func writePackageArtifactMeta(bundleRoot, rootRel string, packages, files []string) error {
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
		return "", errcode.Newf(errCodePrepareRuntimeMissing, "no supported container runtime found (docker/podman)")
	}

	if pref != "docker" && pref != "podman" {
		return "", errcode.Newf(errCodePrepareRuntimeUnsupported, "unsupported runtime: %s", pref)
	}
	if _, err := runner.LookPath(pref); err != nil {
		return "", errcode.Newf(errCodePrepareRuntimeMissing, "runtime not found: %s", pref)
	}
	return pref, nil
}
