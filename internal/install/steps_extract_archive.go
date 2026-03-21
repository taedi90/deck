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

	"github.com/taedi90/deck/internal/filemode"
	"github.com/taedi90/deck/internal/workflowexec"
)

type extractArchiveSpec struct {
	Source  fileDownloadSourceSpec `json:"source"`
	Fetch   fileDownloadFetchSpec  `json:"fetch"`
	Path    string                 `json:"path"`
	Include []string               `json:"include"`
	Mode    string                 `json:"mode"`
}

func runExtractArchive(ctx context.Context, bundleRoot string, spec map[string]any) error {
	decoded, err := workflowexec.DecodeSpec[extractArchiveSpec](spec)
	if err != nil {
		return fmt.Errorf("decode ExtractArchive spec: %w", err)
	}
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	if decoded.Path == "" {
		return fmt.Errorf("ExtractArchive requires path")
	}
	tmpDir, err := os.MkdirTemp("", "deck-extract-archive-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	downloadSpec := map[string]any{
		"source": map[string]any{
			"url":    decoded.Source.URL,
			"path":   decoded.Source.Path,
			"sha256": decoded.Source.SHA256,
		},
		"fetch": map[string]any{
			"offlineOnly": decoded.Fetch.OfflineOnly,
			"sources":     fetchSourcesAny(decoded.Fetch.Sources),
		},
		"outputPath": "archive.bin",
	}
	if decoded.Source.Bundle != nil {
		downloadSpec["source"].(map[string]any)["bundle"] = map[string]any{"root": decoded.Source.Bundle.Root, "path": decoded.Source.Bundle.Path}
	}
	downloadRoot := tmpDir
	if decoded.Source.Bundle != nil {
		downloadRoot = bundleRoot
	}
	relPath, err := runDownloadFile(ctx, downloadRoot, downloadSpec)
	if err != nil {
		return err
	}
	sourcePath := filepath.Join(tmpDir, "archive.bin")
	if decoded.Source.Bundle != nil {
		sourcePath = filepath.Join(bundleRoot, relPath)
	}
	return extractArchiveTarGz(sourcePath, decoded.Path, decoded.Include, decoded.Mode)
}

func extractArchiveTarGz(sourcePath, destination string, include []string, mode string) error {
	if strings.TrimSpace(destination) == "" {
		return fmt.Errorf("ExtractArchive requires path")
	}
	if err := filemode.EnsureDir(destination, filemode.PublishedArtifact); err != nil {
		return err
	}
	// #nosec G304 -- sourcePath is resolved from a declared workflow source into a temp file path.
	file, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer func() { _ = file.Close() }()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("open gzip reader: %w", err)
	}
	defer func() { _ = gz.Close() }()
	reader := tar.NewReader(gz)
	includeSet := map[string]bool{}
	for _, item := range include {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			includeSet[filepath.ToSlash(trimmed)] = true
		}
	}
	var forcedMode os.FileMode
	if strings.TrimSpace(mode) != "" {
		modeVal, err := strconv.ParseUint(strings.TrimSpace(mode), 8, 32)
		if err != nil {
			return fmt.Errorf("invalid archive mode: %w", err)
		}
		forcedMode = os.FileMode(modeVal)
	}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read archive entry: %w", err)
		}
		cleanName := filepath.ToSlash(filepath.Clean(strings.TrimSpace(header.Name)))
		if cleanName == "." || cleanName == "" {
			continue
		}
		if len(includeSet) > 0 && !includeSet[cleanName] {
			continue
		}
		targetPath := filepath.Join(destination, filepath.FromSlash(cleanName))
		if !strings.HasPrefix(filepath.Clean(targetPath), filepath.Clean(destination)+string(filepath.Separator)) && filepath.Clean(targetPath) != filepath.Clean(destination) {
			return fmt.Errorf("archive entry resolves outside destination: %s", cleanName)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := filemode.EnsureDir(targetPath, filemode.PublishedArtifact); err != nil {
				return fmt.Errorf("create archive directory: %w", err)
			}
			if forcedMode != 0 {
				if err := os.Chmod(targetPath, forcedMode); err != nil {
					return fmt.Errorf("apply archive directory mode: %w", err)
				}
			}
		case tar.TypeReg:
			if err := filemode.EnsureParentDir(targetPath, filemode.PublishedArtifact); err != nil {
				return err
			}
			content, err := io.ReadAll(reader)
			if err != nil {
				return fmt.Errorf("read archive file content: %w", err)
			}
			// #nosec G306 -- extracted artifacts are intentionally world-readable when mode permits.
			if err := os.WriteFile(targetPath, content, 0o644); err != nil {
				return fmt.Errorf("write archive file: %w", err)
			}
			appliedMode := os.FileMode(header.FileInfo().Mode().Perm())
			if forcedMode != 0 {
				appliedMode = forcedMode
			}
			if err := os.Chmod(targetPath, appliedMode); err != nil {
				return fmt.Errorf("apply archive file mode: %w", err)
			}
		}
	}
}
