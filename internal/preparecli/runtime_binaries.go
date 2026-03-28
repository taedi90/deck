package preparecli

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/buildinfo"
	"github.com/Airgap-Castaways/deck/internal/fsutil"
	"github.com/Airgap-Castaways/deck/internal/httpfetch"
	"github.com/Airgap-Castaways/deck/internal/workspacepaths"
)

var runtimeBinaryDownloadHTTPClient = httpfetch.Client(0)

const (
	binarySourceAuto    = "auto"
	binarySourceLocal   = "local"
	binarySourceRelease = "release"
)

type runtimeBinaryTarget struct {
	OS   string
	Arch string
}

type runtimeBinaryDeps struct {
	currentGOOS   func() string
	currentGOARCH func() string
	readFile      func(string) ([]byte, error)
	osExecutable  func() (string, error)
	fetchRelease  func(ctx context.Context, version string, target runtimeBinaryTarget) ([]byte, error)
}

func defaultRuntimeBinaryDeps() runtimeBinaryDeps {
	return runtimeBinaryDeps{
		currentGOOS:   func() string { return runtime.GOOS },
		currentGOARCH: func() string { return runtime.GOARCH },
		readFile:      fsutil.ReadFile,
		osExecutable:  os.Executable,
		fetchRelease:  fetchReleaseRuntimeBinary,
	}
}

func stageRuntimeBinariesWithContext(ctx context.Context, preparedRootAbs string, opts Options) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	deps := opts.runtimeBinaryDeps
	if deps.currentGOOS == nil || deps.currentGOARCH == nil || deps.readFile == nil || deps.osExecutable == nil || deps.fetchRelease == nil {
		deps = defaultRuntimeBinaryDeps()
	}
	source, err := resolveBinarySource(opts, deps)
	if err != nil {
		return err
	}
	targets, err := resolveBinaryTargets(opts, source, deps)
	if err != nil {
		return err
	}
	for _, target := range targets {
		raw, err := loadRuntimeBinary(ctx, opts, deps, source, target)
		if err != nil {
			return err
		}
		relPath := filepath.Join(workspacepaths.PreparedBinRoot, target.OS, target.Arch, "deck")
		if err := writeBytes(filepath.Join(preparedRootAbs, relPath), raw, 0o755); err != nil {
			return err
		}
		if err := emitDiagnostic(opts, 2, "deck: prepare runtimeBinary=%s source=%s\n", filepath.ToSlash(filepath.Join(preparedRootAbs, relPath)), source); err != nil {
			return err
		}
	}
	return nil
}

func dryRunRuntimeBinaryWrites(preparedRootAbs string, opts Options) ([]string, error) {
	deps := opts.runtimeBinaryDeps
	if deps.currentGOOS == nil || deps.currentGOARCH == nil || deps.readFile == nil || deps.osExecutable == nil || deps.fetchRelease == nil {
		deps = defaultRuntimeBinaryDeps()
	}
	source, err := resolveBinarySource(opts, deps)
	if err != nil {
		return nil, err
	}
	targets, err := resolveBinaryTargets(opts, source, deps)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(targets))
	for _, target := range targets {
		paths = append(paths, filepath.Join(preparedRootAbs, workspacepaths.PreparedBinRoot, target.OS, target.Arch, "deck"))
	}
	return paths, nil
}

func resolveBinarySource(opts Options, deps runtimeBinaryDeps) (string, error) {
	requested := strings.ToLower(strings.TrimSpace(opts.BinarySource))
	if requested == "" {
		requested = binarySourceAuto
	}
	switch requested {
	case binarySourceAuto:
		if buildinfo.Current().Version == "dev" {
			return binarySourceLocal, nil
		}
		return binarySourceRelease, nil
	case binarySourceLocal, binarySourceRelease:
		return requested, nil
	default:
		return "", fmt.Errorf("--bundle-binary-source must be auto, local, or release")
	}
}

func resolveBinaryTargets(opts Options, source string, deps runtimeBinaryDeps) ([]runtimeBinaryTarget, error) {
	if len(opts.Binaries) > 0 {
		seen := map[string]bool{}
		targets := make([]runtimeBinaryTarget, 0, len(opts.Binaries))
		for _, raw := range opts.Binaries {
			target, err := parseRuntimeBinaryTarget(raw)
			if err != nil {
				return nil, err
			}
			key := target.OS + "/" + target.Arch
			if seen[key] {
				continue
			}
			seen[key] = true
			targets = append(targets, target)
		}
		return targets, nil
	}
	if source == binarySourceRelease {
		return []runtimeBinaryTarget{{OS: "linux", Arch: "amd64"}, {OS: "linux", Arch: "arm64"}}, nil
	}
	return []runtimeBinaryTarget{{OS: deps.currentGOOS(), Arch: deps.currentGOARCH()}}, nil
}

func parseRuntimeBinaryTarget(raw string) (runtimeBinaryTarget, error) {
	parts := strings.Split(strings.TrimSpace(raw), "/")
	if len(parts) != 2 {
		return runtimeBinaryTarget{}, fmt.Errorf("--bundle-binary must use os/arch")
	}
	osVal := strings.ToLower(strings.TrimSpace(parts[0]))
	archVal := strings.ToLower(strings.TrimSpace(parts[1]))
	if (osVal != "linux" && osVal != "darwin") || (archVal != "amd64" && archVal != "arm64") {
		return runtimeBinaryTarget{}, fmt.Errorf("unsupported bundle binary target %s", raw)
	}
	return runtimeBinaryTarget{OS: osVal, Arch: archVal}, nil
}

func loadRuntimeBinary(ctx context.Context, opts Options, deps runtimeBinaryDeps, source string, target runtimeBinaryTarget) ([]byte, error) {
	switch source {
	case binarySourceLocal:
		return loadLocalRuntimeBinary(opts, deps, target)
	case binarySourceRelease:
		version := strings.TrimSpace(opts.BinaryVer)
		if version == "" {
			version = buildinfo.Current().Version
		}
		if version == "" || version == "dev" {
			return nil, fmt.Errorf("--bundle-binary-source=release requires a release build or --bundle-binary-version")
		}
		return deps.fetchRelease(ctx, version, target)
	default:
		return nil, fmt.Errorf("unsupported binary source %s", source)
	}
}

func loadLocalRuntimeBinary(opts Options, deps runtimeBinaryDeps, target runtimeBinaryTarget) ([]byte, error) {
	dir := strings.TrimSpace(opts.BinaryDir)
	if dir == "" {
		if target.OS != deps.currentGOOS() || target.Arch != deps.currentGOARCH() {
			return nil, fmt.Errorf("--bundle-binary-source=local without --bundle-binary-dir only supports the current host target %s/%s", deps.currentGOOS(), deps.currentGOARCH())
		}
		execPath, err := deps.osExecutable()
		if err != nil {
			return nil, fmt.Errorf("resolve deck binary path: %w", err)
		}
		return deps.readFile(execPath)
	}
	path, err := resolveLocalRuntimeBinaryPath(dir, target)
	if err != nil {
		return nil, err
	}
	return deps.readFile(path)
}

func resolveLocalRuntimeBinaryPath(dir string, target runtimeBinaryTarget) (string, error) {
	base := strings.TrimSpace(dir)
	candidates := []string{
		filepath.Join(base, fmt.Sprintf("deck-%s-%s", target.OS, target.Arch)),
		filepath.Join(base, fmt.Sprintf("deck_%s_%s", target.OS, target.Arch)),
		filepath.Join(base, target.OS, target.Arch, "deck"),
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("local runtime binary not found for %s/%s under %s", target.OS, target.Arch, base)
}

func fetchReleaseRuntimeBinary(ctx context.Context, version string, target runtimeBinaryTarget) ([]byte, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		return nil, fmt.Errorf("release version is required")
	}
	versions := []string{version}
	trimmed := strings.TrimPrefix(version, "v")
	if trimmed != version {
		versions = append(versions, trimmed)
	} else {
		versions = append(versions, "v"+version)
	}
	var lastErr error
	for _, archiveVersion := range versions {
		url := fmt.Sprintf("https://github.com/Airgap-Castaways/deck/releases/download/%s/deck_%s_%s_%s.tar.gz", version, archiveVersion, target.OS, target.Arch)
		raw, err := downloadArchiveDeckBinary(ctx, url)
		if err == nil {
			return raw, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("release runtime binary not found")
	}
	return nil, lastErr
}

func downloadArchiveDeckBinary(ctx context.Context, url string) ([]byte, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}
	parsed, err := urlpkgParseHTTPS(url)
	if err != nil {
		return nil, err
	}
	resp, err := httpfetch.Do(ctx, runtimeBinaryDownloadHTTPClient, http.MethodGet, parsed.String(), nil, "download release archive "+parsed.String())
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download release archive %s: unexpected status %d", parsed.String(), resp.StatusCode)
	}
	gzr, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read release archive %s: %w", parsed.String(), err)
	}
	defer func() { _ = gzr.Close() }()
	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read release archive %s: %w", parsed.String(), err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if pathBase(hdr.Name) != "deck" {
			continue
		}
		return io.ReadAll(tr)
	}
	return nil, fmt.Errorf("release archive %s does not contain deck", parsed.String())
}

func urlpkgParseHTTPS(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("parse release archive URL %s: %w", raw, err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return nil, fmt.Errorf("release archive URL must use http or https: %s", raw)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("release archive URL host is required: %s", raw)
	}
	return parsed, nil
}

func pathBase(path string) string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		return path
	}
	return parts[len(parts)-1]
}
