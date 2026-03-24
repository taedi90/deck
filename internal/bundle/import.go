package bundle

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/filemode"
	"github.com/Airgap-Castaways/deck/internal/fsutil"
)

const (
	errCodeBundleImportTraversal = "E_BUNDLE_IMPORT_PATH_TRAVERSAL"
	errCodeBundleImportType      = "E_BUNDLE_IMPORT_UNSUPPORTED_TYPE"
	errCodeBundleImportPrefix    = "E_BUNDLE_IMPORT_INVALID_PREFIX"
	maxBundleArchiveEntrySize    = 1 << 30
)

func ImportArchive(archivePath, destRoot string) error {
	src, err := fsutil.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open bundle archive: %w", err)
	}
	defer func() { _ = src.Close() }()

	if err := filemode.EnsureDir(destRoot, filemode.PublishedArtifact); err != nil {
		return fmt.Errorf("create import destination: %w", err)
	}

	tr := tar.NewReader(src)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read bundle archive: %w", err)
		}

		name := strings.TrimSpace(strings.ReplaceAll(hdr.Name, "\\", "/"))
		if name == "" {
			continue
		}
		if hasParentRef(name) {
			return fmt.Errorf("%s: %s", errCodeBundleImportTraversal, hdr.Name)
		}
		name = path.Clean(name)
		if name == "." {
			continue
		}
		cleanRel := filepath.Clean(filepath.FromSlash(name))
		if cleanRel != "bundle" && !strings.HasPrefix(cleanRel, "bundle/") {
			return fmt.Errorf("%s: %s", errCodeBundleImportPrefix, hdr.Name)
		}
		if cleanRel == "." {
			continue
		}
		if filepath.IsAbs(cleanRel) || strings.HasPrefix(cleanRel, ".."+string(filepath.Separator)) || cleanRel == ".." {
			return fmt.Errorf("%s: %s", errCodeBundleImportTraversal, hdr.Name)
		}

		target := filepath.Join(destRoot, cleanRel)
		rel, err := filepath.Rel(destRoot, target)
		if err != nil {
			return fmt.Errorf("resolve import path: %w", err)
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%s: %s", errCodeBundleImportTraversal, hdr.Name)
		}
		mode := os.FileMode(hdr.Mode & 0o777)

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := filemode.EnsureDir(target, filemode.PublishedArtifact); err != nil {
				return fmt.Errorf("create import directory: %w", err)
			}
			if err := os.Chmod(target, mode); err != nil {
				return fmt.Errorf("chmod import directory: %w", err)
			}
		case tar.TypeReg:
			if hdr.Size < 0 || hdr.Size > maxBundleArchiveEntrySize {
				return fmt.Errorf("archive entry too large: %s", hdr.Name)
			}
			if err := filemode.EnsureParentDir(target, filemode.PublishedArtifact); err != nil {
				return fmt.Errorf("create import parent directory: %w", err)
			}
			f, err := fsutil.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
			if err != nil {
				return fmt.Errorf("create import file: %w", err)
			}
			if _, err := io.CopyN(f, tr, hdr.Size); err != nil {
				_ = f.Close()
				return fmt.Errorf("write import file: %w", err)
			}
			if err := f.Close(); err != nil {
				return fmt.Errorf("close import file: %w", err)
			}
		default:
			return fmt.Errorf("%s: type=%d name=%s", errCodeBundleImportType, hdr.Typeflag, hdr.Name)
		}
	}

	return nil
}

func hasParentRef(name string) bool {
	for _, seg := range strings.Split(name, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}
