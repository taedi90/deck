package server

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

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
	data, err := os.ReadFile(targetPath)
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
	_, _ = w.Write(data)
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

	data, err := os.ReadFile(targetPath)
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
	_, _ = w.Write(data)
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
	category := ""
	switch {
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

	relPath := strings.TrimPrefix(urlPath, "/"+category+"/")
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
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
	if cleanRel == ".deck" || strings.HasPrefix(cleanRel, ".deck/") || strings.Contains(cleanRel, "/.deck/") {
		return "", "", "", http.StatusForbidden
	}

	targetPath := filepath.Join(h.rootAbs, category, filepath.FromSlash(cleanRel))
	resolvedTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return "", "", "", http.StatusForbidden
	}
	rootPrefix := h.rootAbs + string(os.PathSeparator)
	if resolvedTarget != h.rootAbs && !strings.HasPrefix(resolvedTarget, rootPrefix) {
		return "", "", "", http.StatusForbidden
	}

	return category, cleanRel, resolvedTarget, 0
}
