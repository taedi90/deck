package bundle

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/deckignore"
	"github.com/Airgap-Castaways/deck/internal/filemode"
	"github.com/Airgap-Castaways/deck/internal/fsutil"
)

func CollectArchive(bundleRoot, outputPath string) error {
	absRoot, err := filepath.Abs(bundleRoot)
	if err != nil {
		return fmt.Errorf("resolve bundle root: %w", err)
	}
	absOut, err := filepath.Abs(outputPath)
	if err != nil {
		return fmt.Errorf("resolve output path: %w", err)
	}

	if _, err := os.Stat(absRoot); err != nil {
		return fmt.Errorf("bundle root not found: %w", err)
	}
	if err := filemode.EnsureParentArtifactDir(absOut); err != nil {
		return fmt.Errorf("create output parent: %w", err)
	}

	ignoreMatcher, err := deckignore.Load(absRoot)
	if err != nil {
		return err
	}
	root, err := fsutil.NewRoot(absRoot)
	if err != nil {
		return err
	}

	out, err := fsutil.Create(absOut)
	if err != nil {
		return fmt.Errorf("create output archive: %w", err)
	}
	defer func() { _ = out.Close() }()

	tw := tar.NewWriter(out)
	defer func() { _ = tw.Close() }()

	for _, rel := range []string{"deck", "workflows", "outputs", ".deck/manifest.json"} {
		path, err := root.Resolve(filepath.FromSlash(rel))
		if err != nil {
			return err
		}
		if err := addPathToArchive(tw, absRoot, path, absOut, ignoreMatcher); err != nil {
			return fmt.Errorf("build archive: %w", err)
		}
	}

	return nil
}

func addPathToArchive(tw *tar.Writer, root string, path string, outPath string, ignore deckignore.Matcher) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if !info.IsDir() {
		return addFileToArchive(tw, root, path, outPath, ignore)
	}
	rootFS, err := fsutil.NewRoot(root)
	if err != nil {
		return err
	}

	return rootFS.WalkFiles(func(current string, d os.DirEntry) error {
		if sameFilePath(current, outPath) {
			return nil
		}
		rel, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if ignore.Matches(rel, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = pathJoinBundlePrefix(rel)
		if info.IsDir() {
			header.Name += "/"
			if err := tw.WriteHeader(header); err != nil {
				return err
			}
			return nil
		}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		f, err := fsutil.Open(current)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		if _, err := io.Copy(tw, f); err != nil {
			return err
		}
		return nil
	}, strings.TrimPrefix(filepath.ToSlash(strings.TrimPrefix(path, root)), "/"))
}

func addFileToArchive(tw *tar.Writer, root string, path string, outPath string, ignore deckignore.Matcher) error {
	if sameFilePath(path, outPath) {
		return nil
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(rel)
	if ignore.Matches(rel, false) {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	header.Name = pathJoinBundlePrefix(rel)
	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	f, err := fsutil.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err := io.Copy(tw, f); err != nil {
		return err
	}
	return nil
}

func sameFilePath(a, b string) bool {
	ca := filepath.Clean(a)
	cb := filepath.Clean(b)
	if ca == cb {
		return true
	}
	if strings.EqualFold(ca, cb) {
		return true
	}
	return false
}

func pathJoinBundlePrefix(rel string) string {
	rel = filepath.ToSlash(strings.TrimPrefix(rel, "./"))
	if rel == "" {
		return "bundle"
	}
	return "bundle/" + rel
}
