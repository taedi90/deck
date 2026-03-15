package prepare

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

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

		files, err := runContainerPackageDownloadWithScript(ctx, runner, bundleRoot, dir, spec, pkgs, func(family, pkg string) string {
			return buildK8sPackageDownloadScript(family, pkg, strings.TrimSpace(version))
		}, opts)
		if err != nil {
			return nil, err
		}
		metaRel := filepath.ToSlash(filepath.Join(dir, "kubernetes-version.txt"))
		metaAbs := filepath.Join(bundleRoot, filepath.FromSlash(metaRel))
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

func runContainerK8sPackageRepoBuild(ctx context.Context, runner CommandRunner, bundleRoot, repoRoot, family, repoType string, generate bool, pkgsDir, version string, packages []string, spec map[string]any, opts RunOptions) ([]string, error) {
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

	cmdScript := buildK8sPackageDownloadScript(family, strings.Join(packages, " "), version)
	if repoType == "apt-flat" && generate {
		cmdScript += "; apt-ftparchive packages /out/pkgs > /out/Packages; gzip -c /out/Packages > /out/Packages.gz; apt-ftparchive release /out > /out/Release"
	}
	if repoType == "yum" && generate {
		cmdScript += "; mkdir -p /out/repodata; touch /out/repodata/repomd.xml"
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

func runContainerPackageDownloadWithScript(ctx context.Context, runner CommandRunner, bundleRoot, dir string, spec map[string]any, packages []string, buildScript func(family, pkg string) string, opts RunOptions) ([]string, error) {
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
	parts := make([]string, 0, len(packages))
	for _, pkg := range packages {
		parts = append(parts, buildScript(family, pkg))
	}
	cacheKey, err := packageDownloadCacheKey(spec, family, dir)
	if err != nil {
		return nil, err
	}
	mounts, err := preparePackageCacheMounts(family, cacheKey)
	if err != nil {
		return nil, err
	}
	if err := runPackageDownloadContainer(ctx, runner, runtimeSel, image, outAbs, strings.Join(parts, "; "), mounts); err != nil {
		return nil, fmt.Errorf("container package download failed: %w", err)
	}
	after, _ := listRelativeFiles(outAbs)
	files := packageFilesFromDirListing(dir, after)
	if len(files) == 0 {
		return nil, fmt.Errorf("%s: no package artifacts generated in %s", errCodePrepareArtifactsEmpty, dir)
	}
	sort.Strings(files)
	return files, nil
}

func buildK8sPackageDownloadScript(family, pkg, version string) string {
	if family == "rhel" {
		return fmt.Sprintf("mkdir -p /out; dnf -y download --resolve --destdir /out %s-%s || yumdownloader --resolve --destdir /out %s-%s", pkg, version, pkg, version)
	}
	return fmt.Sprintf("mkdir -p /out; apt-get update -y >/dev/null; apt-get install -y --download-only %s=%s >/dev/null || apt-get install -y --download-only %s >/dev/null; cp -a /var/cache/apt/archives/*.deb /out/ 2>/dev/null || true", pkg, version, pkg)
}
