package install

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/taedi90/deck/internal/workflowexec"
)

var installArtifactsDetectHostFacts = detectHostFacts

type installArtifactsSpec struct {
	Fetch     installArtifactsFetchSpec `json:"fetch"`
	Artifacts []installArtifactItemSpec `json:"artifacts"`
}

type installArtifactsFetchSpec struct {
	OfflineOnly bool                          `json:"offlineOnly"`
	Strategy    string                        `json:"strategy"`
	Sources     []installArtifactsFetchSource `json:"sources"`
}

type installArtifactsFetchSource struct {
	Type string `json:"type"`
	Path string `json:"path"`
	URL  string `json:"url"`
}

type installArtifactItemSpec struct {
	Source        installArtifactSourcesSpec  `json:"source"`
	SkipIfPresent *installArtifactSkipSpec    `json:"skipIfPresent"`
	Install       *installArtifactInstallSpec `json:"install"`
	Extract       *installArtifactExtractSpec `json:"extract"`
}

type installArtifactSourcesSpec struct {
	AMD64 installArtifactSourceSpec `json:"amd64"`
	ARM64 installArtifactSourceSpec `json:"arm64"`
}

type installArtifactSourceSpec struct {
	URL    string `json:"url"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type installArtifactSkipSpec struct {
	Path       string `json:"path"`
	Executable bool   `json:"executable"`
}

type installArtifactInstallSpec struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
}

type installArtifactExtractSpec struct {
	Destination string   `json:"destination"`
	Include     []string `json:"include"`
	Mode        string   `json:"mode"`
}

func runInstallArtifacts(ctx context.Context, spec map[string]any) error {
	if ctx == nil {
		ctx = context.Background()
	}

	decoded, err := workflowexec.DecodeSpec[installArtifactsSpec](spec)
	if err != nil {
		return fmt.Errorf("decode InstallArtifacts spec: %w", err)
	}
	if len(decoded.Artifacts) == 0 {
		return fmt.Errorf("%s: InstallArtifacts requires at least one artifact", errCodeInstallArtifactsMissing)
	}

	arch, err := installArtifactsHostArch()
	if err != nil {
		return err
	}

	fetchCfg := fetchSpecMap(decoded.Fetch)
	for i, artifact := range decoded.Artifacts {
		if shouldSkipInstallArtifact(artifact.SkipIfPresent) {
			continue
		}

		source, err := sourceForArch(artifact.Source, arch)
		if err != nil {
			return fmt.Errorf("artifact[%d]: %w", i, err)
		}

		tmpDir, err := os.MkdirTemp("", "deck-install-artifacts-*")
		if err != nil {
			return fmt.Errorf("artifact[%d]: create temp directory: %w", i, err)
		}

		downloadSpec := map[string]any{
			"source": map[string]any{
				"url":    source.URL,
				"path":   source.Path,
				"sha256": source.SHA256,
			},
			"fetch": fetchCfg,
			"output": map[string]any{
				"path": "artifact.bin",
			},
		}
		relativePath, err := runDownloadFile(ctx, tmpDir, downloadSpec)
		if err != nil {
			_ = os.RemoveAll(tmpDir)
			return fmt.Errorf("artifact[%d]: %w", i, err)
		}

		tmpPath := filepath.Join(tmpDir, relativePath)
		if artifact.Install != nil {
			err = installArtifactFile(tmpPath, *artifact.Install)
		} else if artifact.Extract != nil {
			err = extractArtifactTarGz(tmpPath, *artifact.Extract)
		}
		_ = os.RemoveAll(tmpDir)
		if err != nil {
			return fmt.Errorf("artifact[%d]: %w", i, err)
		}
	}

	return nil
}

func installArtifactsHostArch() (string, error) {
	facts := installArtifactsDetectHostFacts()
	rawArch, _ := facts["arch"].(string)
	arch := strings.ToLower(strings.TrimSpace(rawArch))
	switch arch {
	case "amd64", "arm64":
		return arch, nil
	default:
		return "", fmt.Errorf("%s: unsupported host architecture %q", errCodeInstallArtifactArch, arch)
	}
}

func sourceForArch(sources installArtifactSourcesSpec, arch string) (installArtifactSourceSpec, error) {
	var source installArtifactSourceSpec
	switch arch {
	case "amd64":
		source = sources.AMD64
	case "arm64":
		source = sources.ARM64
	default:
		return installArtifactSourceSpec{}, fmt.Errorf("%s: unsupported host architecture %q", errCodeInstallArtifactArch, arch)
	}
	if strings.TrimSpace(source.Path) == "" && strings.TrimSpace(source.URL) == "" {
		return installArtifactSourceSpec{}, fmt.Errorf("%s: source for arch %s requires path or url", errCodeInstallArtifactSource, arch)
	}
	return source, nil
}

func fetchSpecMap(fetchSpec installArtifactsFetchSpec) map[string]any {
	sources := make([]any, 0, len(fetchSpec.Sources))
	for _, src := range fetchSpec.Sources {
		sources = append(sources, map[string]any{
			"type": src.Type,
			"path": src.Path,
			"url":  src.URL,
		})
	}
	return map[string]any{
		"offlineOnly": fetchSpec.OfflineOnly,
		"strategy":    fetchSpec.Strategy,
		"sources":     sources,
	}
}

func shouldSkipInstallArtifact(skip *installArtifactSkipSpec) bool {
	if skip == nil || strings.TrimSpace(skip.Path) == "" {
		return false
	}
	info, err := os.Stat(skip.Path)
	if err != nil {
		return false
	}
	if !skip.Executable {
		return true
	}
	return info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

func installArtifactFile(sourcePath string, installSpec installArtifactInstallSpec) error {
	if strings.TrimSpace(installSpec.Path) == "" {
		return fmt.Errorf("%s: install.path is required", errCodeInstallArtifactSource)
	}
	raw, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("read artifact source: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(installSpec.Path), 0o755); err != nil {
		return fmt.Errorf("create install directory: %w", err)
	}
	if err := writeFileIfChanged(installSpec.Path, raw, 0o644); err != nil {
		return fmt.Errorf("write installed artifact: %w", err)
	}
	if strings.TrimSpace(installSpec.Mode) != "" {
		modeVal, err := strconv.ParseUint(strings.TrimSpace(installSpec.Mode), 8, 32)
		if err != nil {
			return fmt.Errorf("invalid install mode: %w", err)
		}
		if err := os.Chmod(installSpec.Path, os.FileMode(modeVal)); err != nil {
			return fmt.Errorf("apply install mode: %w", err)
		}
	}
	return nil
}

func extractArtifactTarGz(sourcePath string, extractSpec installArtifactExtractSpec) error {
	destination := strings.TrimSpace(extractSpec.Destination)
	if destination == "" {
		return fmt.Errorf("%s: extract.destination is required", errCodeInstallArtifactSource)
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return fmt.Errorf("create extract destination: %w", err)
	}

	modeOverride := os.FileMode(0)
	if strings.TrimSpace(extractSpec.Mode) != "" {
		modeVal, err := strconv.ParseUint(strings.TrimSpace(extractSpec.Mode), 8, 32)
		if err != nil {
			return fmt.Errorf("invalid extract mode: %w", err)
		}
		modeOverride = os.FileMode(modeVal)
	}

	includeSet := map[string]bool{}
	for _, include := range extractSpec.Include {
		normalized := normalizeArchivePath(include)
		if normalized != "" {
			includeSet[normalized] = true
		}
	}

	f, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer func() { _ = f.Close() }()

	gzReader, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("open gzip stream: %w", err)
	}
	defer func() { _ = gzReader.Close() }()

	tarReader := tar.NewReader(gzReader)
	for {
		hdr, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read archive entry: %w", err)
		}

		relPath := normalizeArchivePath(hdr.Name)
		if relPath == "" {
			continue
		}
		if len(includeSet) > 0 && !includeSet[relPath] {
			continue
		}

		targetPath := filepath.Join(destination, relPath)
		if err := ensureArchiveTargetPath(destination, targetPath); err != nil {
			return err
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			mode := hdr.FileInfo().Mode().Perm()
			if mode == 0 {
				mode = 0o755
			}
			if err := os.MkdirAll(targetPath, mode); err != nil {
				return fmt.Errorf("create archive directory: %w", err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return fmt.Errorf("create archive file directory: %w", err)
			}
			content, err := io.ReadAll(tarReader)
			if err != nil {
				return fmt.Errorf("read archive file content: %w", err)
			}
			mode := hdr.FileInfo().Mode().Perm()
			if modeOverride != 0 {
				mode = modeOverride
			}
			if mode == 0 {
				mode = 0o644
			}
			if err := writeFileIfChanged(targetPath, content, mode); err != nil {
				return fmt.Errorf("write extracted file: %w", err)
			}
			if err := os.Chmod(targetPath, mode); err != nil {
				return fmt.Errorf("apply extracted file mode: %w", err)
			}
		}
	}

	return nil
}

func normalizeArchivePath(path string) string {
	cleaned := filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
	cleaned = strings.TrimPrefix(cleaned, "./")
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "." || cleaned == "" {
		return ""
	}
	return cleaned
}

func ensureArchiveTargetPath(destination, target string) error {
	cleanDest := filepath.Clean(destination)
	cleanTarget := filepath.Clean(target)
	if cleanTarget == cleanDest {
		return nil
	}
	prefix := cleanDest + string(filepath.Separator)
	if !strings.HasPrefix(cleanTarget, prefix) {
		return fmt.Errorf("archive entry resolves outside destination: %s", cleanTarget)
	}
	return nil
}
