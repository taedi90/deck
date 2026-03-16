package server

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/taedi90/deck/internal/deckignore"
	"github.com/taedi90/deck/internal/fsutil"
)

const serverOutputsDir = "outputs"

func (h *serverHandler) handleReleaseBundleRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	releaseID, relPath, targetPath, status := h.resolveReleaseBundlePath(r.URL.Path)
	if status != 0 {
		w.WriteHeader(status)
		return
	}
	if _, found, err := h.siteStore.GetRelease(releaseID); err != nil || !found {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	root, err := fsutil.NewRoot(h.rootAbs)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	data, _, err := root.ReadFile(strings.TrimPrefix(targetPath, h.rootAbs+string(os.PathSeparator)))
	if err != nil {
		if os.IsNotExist(err) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	hash := sha256.Sum256(data)
	etag := fmt.Sprintf("\"sha256:%s\"", hex.EncodeToString(hash[:]))
	setStaticHeaders(w.Header(), "site-release-bundle", relPath, etag, len(data), data)
	if matchETag(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusOK)
	writeResponseBody(w, data)
}

func buildWorkflowIndex(root string) ([]byte, error) {
	ignore, err := deckignore.Load(root)
	if err != nil {
		return nil, err
	}
	scenarioRoot := filepath.Join(root, "workflows", "scenarios")
	items := make([]string, 0)
	err = filepath.WalkDir(scenarioRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relFromRoot, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relFromRoot = filepath.ToSlash(relFromRoot)
		if relFromRoot != "." && ignore.Matches(relFromRoot, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		name := strings.ToLower(d.Name())
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			return nil
		}
		rel, err := filepath.Rel(scenarioRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		rel = strings.TrimSuffix(rel, filepath.Ext(rel))
		items = append(items, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(items)
	quoted := make([]string, 0, len(items))
	for _, item := range items {
		quoted = append(quoted, strconv.Quote(item))
	}
	return []byte("[" + strings.Join(quoted, ",") + "]"), nil
}

func (h *serverHandler) resolveReleaseBundlePath(urlPath string) (string, string, string, int) {
	const prefix = "/site/releases/"
	if !strings.HasPrefix(urlPath, prefix) {
		return "", "", "", http.StatusNotFound
	}
	rest := strings.TrimPrefix(urlPath, prefix)
	parts := strings.SplitN(rest, "/bundle/", 2)
	if len(parts) != 2 {
		return "", "", "", http.StatusNotFound
	}
	releaseID := strings.TrimSpace(parts[0])
	relPath := strings.TrimSpace(parts[1])
	if releaseID == "" || relPath == "" {
		return "", "", "", http.StatusNotFound
	}
	if strings.Contains(relPath, "\\") {
		return "", "", "", http.StatusForbidden
	}
	for _, segment := range strings.Split(relPath, "/") {
		if segment == ".." {
			return "", "", "", http.StatusForbidden
		}
	}
	cleanRel := strings.TrimPrefix(path.Clean("/"+relPath), "/")
	if cleanRel == "." || cleanRel == "" {
		return "", "", "", http.StatusNotFound
	}
	bundleRoot := filepath.Join(h.rootAbs, ".deck", "site", "releases", releaseID, "bundle")
	targetPath := filepath.Join(bundleRoot, filepath.FromSlash(cleanRel))
	resolvedTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return "", "", "", http.StatusForbidden
	}
	resolvedBundleRoot, err := filepath.Abs(bundleRoot)
	if err != nil {
		return "", "", "", http.StatusForbidden
	}
	rootPrefix := resolvedBundleRoot + string(os.PathSeparator)
	if resolvedTarget != resolvedBundleRoot && !strings.HasPrefix(resolvedTarget, rootPrefix) {
		return "", "", "", http.StatusForbidden
	}
	return releaseID, cleanRel, resolvedTarget, 0
}

func (h *serverHandler) handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	category, relPath, targetPath, status := h.resolveCategoryPath(r.URL.Path)
	if status != 0 {
		w.WriteHeader(status)
		return
	}

	root, err := fsutil.NewRoot(h.rootAbs)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	data, _, err := root.ReadFile(strings.TrimPrefix(targetPath, h.rootAbs+string(os.PathSeparator)))
	if err != nil {
		if os.IsNotExist(err) && category == "workflows" && relPath == "index.json" {
			data, err = buildWorkflowIndex(h.rootAbs)
		}
		if err != nil {
			if os.IsNotExist(err) {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}

	hash := sha256.Sum256(data)
	etag := fmt.Sprintf("\"sha256:%s\"", hex.EncodeToString(hash[:]))
	setStaticHeaders(w.Header(), category, relPath, etag, len(data), data)

	if matchETag(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusOK)
	writeResponseBody(w, data)
}

func setStaticHeaders(h http.Header, category, relPath, etag string, size int, data []byte) {
	h.Set("Accept-Ranges", "bytes")
	h.Set("Cache-Control", "no-store")
	h.Set("ETag", etag)
	h.Set("Content-Length", strconv.Itoa(size))
	h.Set("Content-Type", http.DetectContentType(data))
	if category == "workflows" && relPath == "index.json" {
		h.Set("Content-Type", "application/json")
	}
}

func matchETag(rawHeader, expected string) bool {
	rawHeader = strings.TrimSpace(rawHeader)
	if rawHeader == "" {
		return false
	}
	if rawHeader == "*" {
		return true
	}
	for _, token := range strings.Split(rawHeader, ",") {
		if strings.TrimSpace(token) == expected {
			return true
		}
	}
	return false
}

func (h *serverHandler) resolveCategoryPath(urlPath string) (string, string, string, int) {
	ignore, err := deckignore.Load(h.rootAbs)
	if err != nil {
		return "", "", "", http.StatusInternalServerError
	}
	category := ""
	switch {
	case urlPath == "/deck":
		category = "deck"
	case strings.HasPrefix(urlPath, "/files/"):
		category = "files"
	case strings.HasPrefix(urlPath, "/packages/"):
		category = "packages"
	case strings.HasPrefix(urlPath, "/images/"):
		category = "images"
	case strings.HasPrefix(urlPath, "/workflows/"):
		category = "workflows"
	default:
		return "", "", "", http.StatusNotFound
	}

	if category == "deck" {
		if ignore.Matches("deck", false) {
			return "", "", "", http.StatusNotFound
		}
		root, err := fsutil.NewRoot(h.rootAbs)
		if err != nil {
			return "", "", "", http.StatusInternalServerError
		}
		resolvedTarget, err := root.Resolve("deck")
		if err != nil {
			return "", "", "", http.StatusForbidden
		}
		return category, "deck", resolvedTarget, 0
	}

	relPath := strings.TrimPrefix(urlPath, "/"+category+"/")
	if urlPath == "/"+category || urlPath == "/"+category+"/" {
		relPath = ""
	}
	relPath = strings.TrimSpace(relPath)
	if strings.Contains(relPath, "\\") {
		return "", "", "", http.StatusForbidden
	}
	for _, segment := range strings.Split(relPath, "/") {
		if segment == ".." {
			return "", "", "", http.StatusForbidden
		}
	}
	cleanRel := strings.TrimPrefix(path.Clean("/"+relPath), "/")
	if cleanRel == "." || cleanRel == "" {
		cleanRel = ""
	}
	ignorePath := filepath.ToSlash(filepath.Join(category, cleanRel))
	if category == "files" || category == "packages" || category == "images" {
		ignorePath = filepath.ToSlash(filepath.Join(serverOutputsDir, category, cleanRel))
	}
	if cleanRel != "" && ignore.Matches(ignorePath, false) {
		return "", "", "", http.StatusNotFound
	}
	if cleanRel == ".deck" || strings.HasPrefix(cleanRel, ".deck/") || strings.Contains(cleanRel, "/.deck/") {
		return "", "", "", http.StatusForbidden
	}

	baseDir := category
	if category == "files" || category == "packages" || category == "images" {
		root, err := fsutil.NewRoot(h.rootAbs)
		if err != nil {
			return "", "", "", http.StatusInternalServerError
		}
		if _, _, err := root.Stat(serverOutputsDir, category, filepath.FromSlash(cleanRel)); err == nil {
			baseDir = filepath.ToSlash(filepath.Join(serverOutputsDir, category))
		}
	}
	root, err := fsutil.NewRoot(h.rootAbs)
	if err != nil {
		return "", "", "", http.StatusInternalServerError
	}
	resolvedTarget, err := root.Resolve(filepath.FromSlash(baseDir), filepath.FromSlash(cleanRel))
	if err != nil {
		return "", "", "", http.StatusForbidden
	}

	return category, cleanRel, resolvedTarget, 0
}
