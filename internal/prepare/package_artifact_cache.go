package prepare

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/taedi90/deck/internal/filemode"
	"github.com/taedi90/deck/internal/userdirs"
)

const exportedPackageCacheMetaFile = "meta.json"

var packageArtifactStageCounter uint64

type exportedPackageCacheMeta struct {
	RootRel  string   `json:"root_rel"`
	Packages []string `json:"packages"`
	Files    []string `json:"files"`
}

func exportedPackageCacheRoot() (string, error) {
	cacheRoot, err := userdirs.CacheRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheRoot, "artifacts", "package"), nil
}

func exportedPackageCachePath(cacheKey string) (string, error) {
	root, err := exportedPackageCacheRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, strings.TrimSpace(cacheKey), runtime.GOARCH), nil
}

func exportedPackageCacheKey(spec map[string]any, rootRel string) (string, error) {
	payload := map[string]any{
		"rootRel":  filepath.ToSlash(strings.TrimSpace(rootRel)),
		"packages": stringSlice(spec["packages"]),
		"spec":     spec,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode exported package cache key: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func loadExportedPackageCache(path string) (exportedPackageCacheMeta, bool, error) {
	metaPath := filepath.Join(path, exportedPackageCacheMetaFile)
	// #nosec G304 -- metaPath is derived from the internal cache layout under userdirs.CacheRoot.
	raw, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return exportedPackageCacheMeta{}, false, nil
		}
		return exportedPackageCacheMeta{}, false, fmt.Errorf("read exported package cache meta: %w", err)
	}
	var meta exportedPackageCacheMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return exportedPackageCacheMeta{}, false, fmt.Errorf("parse exported package cache meta: %w", err)
	}
	if meta.RootRel == "" {
		return exportedPackageCacheMeta{}, false, nil
	}
	meta.Files = normalizeStrings(meta.Files)
	meta.Packages = normalizeStrings(meta.Packages)
	return meta, true, nil
}

func saveExportedPackageCacheMeta(path string, meta exportedPackageCacheMeta) error {
	meta.Files = normalizeStrings(meta.Files)
	meta.Packages = normalizeStrings(meta.Packages)
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("encode exported package cache meta: %w", err)
	}
	metaPath := filepath.Join(path, exportedPackageCacheMetaFile)
	if err := filemode.EnsureParentPrivateDir(metaPath); err != nil {
		return err
	}
	return filemode.WritePrivateFile(metaPath, raw)
}

func buildExportedPackageCacheStage(cachePath string) string {
	return fmt.Sprintf("%s.stage-%d", cachePath, atomic.AddUint64(&packageArtifactStageCounter, 1))
}

func buildPublishedArtifactStage(path string) string {
	return fmt.Sprintf("%s.stage-%d", path, atomic.AddUint64(&packageArtifactStageCounter, 1))
}

func tryReuseExportedPackageArtifact(bundleRoot, rootRel, cachePath string, packages []string, opts RunOptions) ([]string, bool, error) {
	if opts.ForceRedownload {
		return nil, false, nil
	}
	meta, ok, err := loadExportedPackageCache(cachePath)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
	}
	if filepath.ToSlash(strings.TrimSpace(meta.RootRel)) != filepath.ToSlash(strings.TrimSpace(rootRel)) {
		return nil, false, nil
	}
	if !equalStrings(normalizeStrings(packages), meta.Packages) {
		return nil, false, nil
	}
	if len(meta.Files) == 0 {
		return nil, false, nil
	}
	for _, rel := range meta.Files {
		cacheFile := filepath.Join(cachePath, filepath.FromSlash(rel))
		info, err := os.Stat(cacheFile)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, false, nil
			}
			return nil, false, fmt.Errorf("stat exported package cache file %s: %w", cacheFile, err)
		}
		if info.IsDir() || info.Size() == 0 {
			return nil, false, nil
		}
	}
	if err := publishCachedPackageArtifact(bundleRoot, rootRel, cachePath, meta.Files); err != nil {
		return nil, false, err
	}
	return packageFilesFromDirListing(rootRel, meta.Files), true, nil
}

func publishCachedPackageArtifact(bundleRoot, rootRel, cachePath string, relFiles []string) error {
	stageRoot := buildPublishedArtifactStage(filepath.Join(bundleRoot, filepath.FromSlash(rootRel)))
	_ = os.RemoveAll(stageRoot)
	if err := filemode.EnsureDir(stageRoot, filemode.PublishedArtifact); err != nil {
		return err
	}
	for _, rel := range normalizeStrings(relFiles) {
		src := filepath.Join(cachePath, filepath.FromSlash(rel))
		dst := filepath.Join(stageRoot, filepath.FromSlash(rel))
		if err := copyArtifactFile(src, dst); err != nil {
			return err
		}
	}
	return replacePublishedArtifactDir(stageRoot, filepath.Join(bundleRoot, filepath.FromSlash(rootRel)))
}

func copyArtifactFile(src, dst string) error {
	if err := filemode.EnsureParentArtifactDir(dst); err != nil {
		return err
	}
	// #nosec G304 -- src is derived from validated cache paths managed by deck.
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open src artifact: %w", err)
	}
	defer func() { _ = in.Close() }()
	// #nosec G304 -- dst is derived from bundle/cache paths controlled by deck.
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, filemode.ArtifactFileMode)
	if err != nil {
		return fmt.Errorf("open dst artifact: %w", err)
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy artifact: %w", err)
	}
	return nil
}

func replacePublishedArtifactDir(stagePath, finalPath string) error {
	backupPath := fmt.Sprintf("%s.old-%d", finalPath, atomic.AddUint64(&packageArtifactStageCounter, 1))
	_ = os.RemoveAll(backupPath)
	if err := filemode.EnsureParentArtifactDir(finalPath); err != nil {
		return err
	}
	if _, err := os.Stat(finalPath); err == nil {
		if err := os.Rename(finalPath, backupPath); err != nil {
			return fmt.Errorf("stage existing artifact dir: %w", err)
		}
	}
	if err := os.Rename(stagePath, finalPath); err != nil {
		if _, statErr := os.Stat(backupPath); statErr == nil {
			_ = os.Rename(backupPath, finalPath)
		}
		return fmt.Errorf("publish artifact dir: %w", err)
	}
	_ = os.RemoveAll(backupPath)
	return nil
}

func exportContainerTarToStage(data []byte, stageRoot string) ([]string, error) {
	_ = os.RemoveAll(stageRoot)
	if err := filemode.EnsureDir(stageRoot, filemode.PublishedArtifact); err != nil {
		return nil, err
	}
	reader := tar.NewReader(bytes.NewReader(data))
	files := make([]string, 0)
	for {
		hdr, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read container export tar: %w", err)
		}
		rel, ok := normalizeTarPath(hdr.Name)
		if !ok {
			continue
		}
		target := filepath.Join(stageRoot, filepath.FromSlash(rel))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := filemode.EnsureDir(target, filemode.PublishedArtifact); err != nil {
				return nil, err
			}
		case tar.TypeReg:
			if err := filemode.EnsureParentArtifactDir(target); err != nil {
				return nil, err
			}
			// #nosec G304 -- target is sanitized via normalizeTarPath and rooted under a deck-owned stage dir.
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, filemode.ArtifactFileMode)
			if err != nil {
				return nil, fmt.Errorf("create exported artifact file: %w", err)
			}
			if _, err := io.CopyN(out, reader, hdr.Size); err != nil {
				_ = out.Close()
				return nil, fmt.Errorf("write exported artifact file: %w", err)
			}
			if err := out.Close(); err != nil {
				return nil, fmt.Errorf("close exported artifact file: %w", err)
			}
			files = append(files, rel)
		default:
			return nil, fmt.Errorf("unsupported exported package artifact entry type: %s", rel)
		}
	}
	return sortedUniqueStrings(files), nil
}

func normalizeTarPath(name string) (string, bool) {
	clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(name)))
	if clean == "." || clean == "/" || clean == "" {
		return "", false
	}
	clean = strings.TrimPrefix(clean, "./")
	clean = strings.TrimPrefix(clean, "/")
	if clean == "" || clean == "." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", false
	}
	return clean, true
}

func sortedUniqueStrings(values []string) []string {
	set := map[string]bool{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			set[filepath.ToSlash(trimmed)] = true
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
