package prepare

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type packageCacheMeta struct {
	Packages []string `json:"packages"`
	Files    []string `json:"files"`
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
