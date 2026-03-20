package bundle

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/taedi90/deck/internal/fsutil"
)

type ManifestFile struct {
	Entries []ManifestEntry `json:"entries"`
}

type ManifestEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type tarFileInfo struct {
	sha256 string
	size   int64
}

const manifestRelativePath = ".deck/manifest.json"

func VerifyManifest(bundlePath string) error {
	info, err := os.Stat(bundlePath)
	if err != nil {
		return fmt.Errorf("stat bundle path: %w", err)
	}

	if info.IsDir() {
		return verifyEnsureDirectoryManifest(bundlePath)
	}

	return verifyTarManifest(bundlePath)
}

func verifyEnsureDirectoryManifest(bundleRoot string) error {
	entries, manifestPaths, err := loadManifestEntriesFromDir(bundleRoot)
	if err != nil {
		return err
	}

	for _, e := range entries {
		abs := filepath.Join(bundleRoot, filepath.FromSlash(e.Path))
		actualSize, actualSHA, err := fileDigest(abs)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("E_BUNDLE_INTEGRITY: missing artifact %s: %w", e.Path, err)
			}
			return fmt.Errorf("E_BUNDLE_INTEGRITY: read artifact %s: %w", e.Path, err)
		}
		if e.Size > 0 && actualSize != e.Size {
			return fmt.Errorf("E_BUNDLE_INTEGRITY: size mismatch for %s", e.Path)
		}
		if !strings.EqualFold(actualSHA, e.SHA256) {
			return fmt.Errorf("E_BUNDLE_INTEGRITY: sha256 mismatch for %s", e.Path)
		}
	}

	if err := verifyOfflineArtifactCoverage(bundleRoot, manifestPaths); err != nil {
		return err
	}

	return nil
}

func verifyTarManifest(archivePath string) error {
	manifestRaw, files, dirs, err := scanTarBundle(archivePath)
	if err != nil {
		return err
	}

	entries, manifestPaths, err := normalizeManifestEntriesFromBytes(manifestRaw, tarManifestPath())
	if err != nil {
		return err
	}

	for _, e := range entries {
		meta, ok := files[e.Path]
		if !ok {
			return fmt.Errorf("E_BUNDLE_INTEGRITY: missing artifact %s", e.Path)
		}
		if e.Size > 0 && meta.size != e.Size {
			return fmt.Errorf("E_BUNDLE_INTEGRITY: size mismatch for %s", e.Path)
		}
		if !strings.EqualFold(meta.sha256, e.SHA256) {
			return fmt.Errorf("E_BUNDLE_INTEGRITY: sha256 mismatch for %s", e.Path)
		}
	}

	if err := verifyOfflineArtifactCoverageFromTar(files, dirs, manifestPaths); err != nil {
		return err
	}

	return nil
}

func loadManifestEntriesFromDir(bundleRoot string) ([]ManifestEntry, map[string]struct{}, error) {
	raw, err := fsutil.ReadFile(filepath.Join(bundleRoot, filepath.FromSlash(manifestRelativePath)))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("E_MANIFEST_MISSING: %s", filepath.Join(bundleRoot, filepath.FromSlash(manifestRelativePath)))
		}
		return nil, nil, fmt.Errorf("read manifest: %w", err)
	}

	entries, manifestPaths, err := normalizeManifestEntriesFromBytes(raw, filepath.Join(bundleRoot, filepath.FromSlash(manifestRelativePath)))
	if err != nil {
		return nil, nil, err
	}

	return entries, manifestPaths, nil
}

func loadManifestEntries(bundlePath string) ([]ManifestEntry, error) {
	info, err := os.Stat(bundlePath)
	if err != nil {
		return nil, fmt.Errorf("stat bundle path: %w", err)
	}

	if info.IsDir() {
		entries, _, loadErr := loadManifestEntriesFromDir(bundlePath)
		return entries, loadErr
	}

	raw, loadErr := readManifestFromTar(bundlePath)
	if loadErr != nil {
		return nil, loadErr
	}

	entries, _, normalizeErr := normalizeManifestEntriesFromBytes(raw, tarManifestPath())
	if normalizeErr != nil {
		return nil, normalizeErr
	}

	return entries, nil
}

func normalizeManifestEntriesFromBytes(raw []byte, source string) ([]ManifestEntry, map[string]struct{}, error) {
	var mf ManifestFile
	if err := json.Unmarshal(raw, &mf); err != nil {
		return nil, nil, fmt.Errorf("parse manifest: %w", err)
	}
	if len(mf.Entries) == 0 {
		return nil, nil, fmt.Errorf("E_MANIFEST_EMPTY: %s", source)
	}

	normalized := make([]ManifestEntry, 0, len(mf.Entries))
	manifestPaths := make(map[string]struct{}, len(mf.Entries))
	for _, e := range mf.Entries {
		rel, err := normalizeManifestEntryPath(e.Path)
		if err != nil {
			return nil, nil, err
		}
		e.Path = rel
		normalized = append(normalized, e)
		manifestPaths[rel] = struct{}{}
	}

	return normalized, manifestPaths, nil
}

func normalizeManifestEntryPath(raw string) (string, error) {
	cleaned := path.Clean(strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/")))
	if cleaned == "" || cleaned == "." {
		return "", fmt.Errorf("E_BUNDLE_INTEGRITY: empty path entry")
	}
	if strings.HasPrefix(cleaned, "/") || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("E_BUNDLE_INTEGRITY: invalid manifest entry path %s", raw)
	}
	if !isManifestTrackedPath(cleaned) {
		return "", fmt.Errorf("E_BUNDLE_INTEGRITY: invalid manifest entry path %s", raw)
	}

	return cleaned, nil
}

func fileDigest(filePath string) (int64, string, error) {
	f, err := fsutil.Open(filePath)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	size, err := io.Copy(h, f)
	if err != nil {
		return 0, "", err
	}

	return size, hex.EncodeToString(h.Sum(nil)), nil
}

func scanTarBundle(archivePath string) ([]byte, map[string]tarFileInfo, map[string]struct{}, error) {
	src, err := fsutil.Open(archivePath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open bundle archive: %w", err)
	}
	defer func() { _ = src.Close() }()

	files := make(map[string]tarFileInfo)
	dirs := map[string]struct{}{".": {}}
	tr := tar.NewReader(src)

	var manifestRaw []byte
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, nil, fmt.Errorf("read bundle archive: %w", err)
		}

		rel, err := normalizeTarBundleEntry(hdr.Name)
		if err != nil {
			return nil, nil, nil, err
		}
		if rel == "" {
			continue
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			addTarPathDirectories(dirs, rel)
		case tar.TypeReg:
			if hdr.Size < 0 || hdr.Size > maxBundleArchiveEntrySize {
				return nil, nil, nil, fmt.Errorf("E_BUNDLE_INTEGRITY: archive entry too large: %s", rel)
			}
			addTarPathDirectories(dirs, rel)
			h := sha256.New()
			if rel == manifestRelativePath {
				raw, readErr := io.ReadAll(io.TeeReader(io.LimitReader(tr, hdr.Size), h))
				if readErr != nil {
					return nil, nil, nil, fmt.Errorf("read manifest from archive: %w", readErr)
				}
				manifestRaw = raw
				continue
			}

			size, copyErr := io.CopyN(h, tr, hdr.Size)
			if copyErr != nil {
				return nil, nil, nil, fmt.Errorf("read archive entry %s: %w", rel, copyErr)
			}
			files[rel] = tarFileInfo{sha256: hex.EncodeToString(h.Sum(nil)), size: size}
		}
	}

	if len(manifestRaw) == 0 {
		return nil, nil, nil, fmt.Errorf("E_MANIFEST_MISSING: %s", tarManifestPath())
	}

	return manifestRaw, files, dirs, nil
}

func readManifestFromTar(archivePath string) ([]byte, error) {
	src, err := fsutil.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open bundle archive: %w", err)
	}
	defer func() { _ = src.Close() }()

	tr := tar.NewReader(src)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read bundle archive: %w", err)
		}

		rel, err := normalizeTarBundleEntry(hdr.Name)
		if err != nil {
			return nil, err
		}
		if rel != manifestRelativePath {
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("E_BUNDLE_INTEGRITY: manifest entry must be regular file")
		}

		raw, readErr := io.ReadAll(tr)
		if readErr != nil {
			return nil, fmt.Errorf("read manifest from archive: %w", readErr)
		}
		return raw, nil
	}

	return nil, fmt.Errorf("E_MANIFEST_MISSING: %s", tarManifestPath())
}

func normalizeTarBundleEntry(raw string) (string, error) {
	name := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if name == "" {
		return "", nil
	}
	if tarPathHasParentRef(name) {
		return "", fmt.Errorf("E_BUNDLE_IMPORT_PATH_TRAVERSAL: %s", raw)
	}

	cleaned := path.Clean(name)
	if cleaned == "." || cleaned == "bundle" {
		return "", nil
	}
	if !strings.HasPrefix(cleaned, "bundle/") {
		return "", fmt.Errorf("E_BUNDLE_IMPORT_INVALID_PREFIX: %s", raw)
	}
	rel := strings.TrimPrefix(cleaned, "bundle/")
	if rel == "" || rel == "." {
		return "", nil
	}
	if strings.HasPrefix(rel, "/") || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("E_BUNDLE_IMPORT_PATH_TRAVERSAL: %s", raw)
	}
	return rel, nil
}

func tarPathHasParentRef(name string) bool {
	for _, seg := range strings.Split(name, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

func addTarPathDirectories(dirs map[string]struct{}, relPath string) {
	current := path.Dir(relPath)
	for current != "." && current != "/" {
		dirs[current] = struct{}{}
		current = path.Dir(current)
	}
}

func tarManifestPath() string {
	return path.Join("bundle", manifestRelativePath)
}

func isManifestTrackedPath(rel string) bool {
	return strings.HasPrefix(rel, "outputs/packages/") || strings.HasPrefix(rel, "outputs/images/") || strings.HasPrefix(rel, "outputs/files/") || strings.HasPrefix(rel, "packages/") || strings.HasPrefix(rel, "images/") || strings.HasPrefix(rel, "files/")
}

func verifyOfflineArtifactCoverage(bundleRoot string, manifestPaths map[string]struct{}) error {
	for _, repoRoot := range []string{filepath.Join("outputs", "packages", "apt"), filepath.Join("packages", "apt")} {
		if err := verifyAPTRepoCoverage(bundleRoot, manifestPaths, repoRoot); err != nil {
			return err
		}
	}
	for _, repoRoot := range []string{filepath.Join("outputs", "packages", "apt-k8s"), filepath.Join("packages", "apt-k8s")} {
		if err := verifyAPTRepoCoverage(bundleRoot, manifestPaths, repoRoot); err != nil {
			return err
		}
	}
	for _, repoRoot := range []string{filepath.Join("outputs", "packages", "yum"), filepath.Join("packages", "yum")} {
		if err := verifyYUMRepoCoverage(bundleRoot, manifestPaths, repoRoot); err != nil {
			return err
		}
	}
	for _, repoRoot := range []string{filepath.Join("outputs", "packages", "yum-k8s"), filepath.Join("packages", "yum-k8s")} {
		if err := verifyYUMRepoCoverage(bundleRoot, manifestPaths, repoRoot); err != nil {
			return err
		}
	}

	return verifyImageTarCoverage(bundleRoot, manifestPaths)
}

func verifyOfflineArtifactCoverageFromTar(files map[string]tarFileInfo, dirs map[string]struct{}, manifestPaths map[string]struct{}) error {
	for _, repoRoot := range []string{path.Join("outputs", "packages", "apt"), path.Join("packages", "apt")} {
		if err := verifyAPTRepoCoverageFromTar(files, dirs, manifestPaths, repoRoot); err != nil {
			return err
		}
	}
	for _, repoRoot := range []string{path.Join("outputs", "packages", "apt-k8s"), path.Join("packages", "apt-k8s")} {
		if err := verifyAPTRepoCoverageFromTar(files, dirs, manifestPaths, repoRoot); err != nil {
			return err
		}
	}
	for _, repoRoot := range []string{path.Join("outputs", "packages", "yum"), path.Join("packages", "yum")} {
		if err := verifyYUMRepoCoverageFromTar(files, dirs, manifestPaths, repoRoot); err != nil {
			return err
		}
	}
	for _, repoRoot := range []string{path.Join("outputs", "packages", "yum-k8s"), path.Join("packages", "yum-k8s")} {
		if err := verifyYUMRepoCoverageFromTar(files, dirs, manifestPaths, repoRoot); err != nil {
			return err
		}
	}

	for rel := range files {
		if (strings.HasPrefix(rel, "outputs/images/") || strings.HasPrefix(rel, "images/")) && path.Ext(rel) == ".tar" {
			if err := requireInTarAndManifest(files, manifestPaths, rel); err != nil {
				return err
			}
		}
	}

	return nil
}

func verifyAPTRepoCoverage(bundleRoot string, manifestPaths map[string]struct{}, repoRoot string) error {
	releases, err := listSubdirectories(filepath.Join(bundleRoot, repoRoot))
	if err != nil {
		return fmt.Errorf("E_BUNDLE_INTEGRITY: scan apt repos %s: %w", filepath.ToSlash(repoRoot), err)
	}

	for _, release := range releases {
		releaseRoot := filepath.Join(repoRoot, release)
		if err := requireInBundleAndManifest(bundleRoot, manifestPaths, filepath.Join(releaseRoot, "Release")); err != nil {
			return err
		}
		if err := requireInBundleAndManifest(bundleRoot, manifestPaths, filepath.Join(releaseRoot, "Packages.gz")); err != nil {
			return err
		}
	}

	return nil
}

func verifyAPTRepoCoverageFromTar(files map[string]tarFileInfo, dirs map[string]struct{}, manifestPaths map[string]struct{}, repoRoot string) error {
	for _, release := range listTarSubdirectories(dirs, repoRoot) {
		releaseRoot := path.Join(repoRoot, release)
		if err := requireInTarAndManifest(files, manifestPaths, path.Join(releaseRoot, "Release")); err != nil {
			return err
		}
		if err := requireInTarAndManifest(files, manifestPaths, path.Join(releaseRoot, "Packages.gz")); err != nil {
			return err
		}
	}

	return nil
}

func verifyYUMRepoCoverage(bundleRoot string, manifestPaths map[string]struct{}, repoRoot string) error {
	repos, err := listSubdirectories(filepath.Join(bundleRoot, repoRoot))
	if err != nil {
		return fmt.Errorf("E_BUNDLE_INTEGRITY: scan yum repos %s: %w", filepath.ToSlash(repoRoot), err)
	}

	for _, repo := range repos {
		repomdRel := filepath.Join(repoRoot, repo, "repodata", "repomd.xml")
		if err := requireInBundleAndManifest(bundleRoot, manifestPaths, repomdRel); err != nil {
			return err
		}
	}

	return nil
}

func verifyYUMRepoCoverageFromTar(files map[string]tarFileInfo, dirs map[string]struct{}, manifestPaths map[string]struct{}, repoRoot string) error {
	for _, repo := range listTarSubdirectories(dirs, repoRoot) {
		repomdRel := path.Join(repoRoot, repo, "repodata", "repomd.xml")
		if err := requireInTarAndManifest(files, manifestPaths, repomdRel); err != nil {
			return err
		}
	}

	return nil
}

func verifyImageTarCoverage(bundleRoot string, manifestPaths map[string]struct{}) error {
	for _, relDir := range []string{filepath.Join("outputs", "images"), "images"} {
		imagesDir := filepath.Join(bundleRoot, relDir)
		entries, err := os.ReadDir(imagesDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("E_BUNDLE_INTEGRITY: scan images dir: %w", err)
		}

		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".tar" {
				continue
			}
			if err := requireInBundleAndManifest(bundleRoot, manifestPaths, filepath.Join(relDir, e.Name())); err != nil {
				return err
			}
		}
	}

	return nil
}

func listTarSubdirectories(dirs map[string]struct{}, root string) []string {
	children := map[string]struct{}{}
	prefix := root + "/"
	for rel := range dirs {
		if !strings.HasPrefix(rel, prefix) {
			continue
		}
		rest := strings.TrimPrefix(rel, prefix)
		if rest == "" || rest == "." {
			continue
		}
		first := strings.Split(rest, "/")[0]
		if first != "" {
			children[first] = struct{}{}
		}
	}

	names := make([]string, 0, len(children))
	for name := range children {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func requireInBundleAndManifest(bundleRoot string, manifestPaths map[string]struct{}, relPath string) error {
	relSlash := filepath.ToSlash(relPath)
	abs := filepath.Join(bundleRoot, filepath.FromSlash(relSlash))
	if _, err := os.Stat(abs); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("E_BUNDLE_INTEGRITY: required offline artifact missing from bundle: %s", relSlash)
		}
		return fmt.Errorf("E_BUNDLE_INTEGRITY: stat required offline artifact %s: %w", relSlash, err)
	}
	if _, ok := manifestPaths[relSlash]; !ok {
		return fmt.Errorf("E_BUNDLE_INTEGRITY: required offline artifact missing from manifest: %s", relSlash)
	}

	return nil
}

func requireInTarAndManifest(files map[string]tarFileInfo, manifestPaths map[string]struct{}, relPath string) error {
	relSlash := path.Clean(relPath)
	if _, ok := files[relSlash]; !ok {
		return fmt.Errorf("E_BUNDLE_INTEGRITY: required offline artifact missing from bundle: %s", relSlash)
	}
	if _, ok := manifestPaths[relSlash]; !ok {
		return fmt.Errorf("E_BUNDLE_INTEGRITY: required offline artifact missing from manifest: %s", relSlash)
	}

	return nil
}

func listSubdirectories(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	return names, nil
}
