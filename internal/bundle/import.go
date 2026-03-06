package bundle

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	errCodeBundleImportTraversal = "E_BUNDLE_IMPORT_PATH_TRAVERSAL"
	errCodeBundleImportType      = "E_BUNDLE_IMPORT_UNSUPPORTED_TYPE"
	errCodeBundleImportPrefix    = "E_BUNDLE_IMPORT_INVALID_PREFIX"
)

func ImportArchive(archivePath, destRoot string) error {
	src, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open bundle archive: %w", err)
	}
	defer src.Close()

	if err := os.MkdirAll(destRoot, 0o755); err != nil {
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
		if name != "bundle" && !strings.HasPrefix(name, "bundle/") {
			return fmt.Errorf("%s: %s", errCodeBundleImportPrefix, hdr.Name)
		}

		cleanRel := filepath.Clean(filepath.FromSlash(name))
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

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)); err != nil {
				return fmt.Errorf("create import directory: %w", err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create import parent directory: %w", err)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode))
			if err != nil {
				return fmt.Errorf("create import file: %w", err)
			}
			if _, err := io.Copy(f, tr); err != nil {
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
