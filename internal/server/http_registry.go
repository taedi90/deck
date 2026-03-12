package server

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

type registryCatalogEntry struct {
	repoTag string
	repo    string
	tag     string
	tarPath string
}

type registryResolvedImage struct {
	repo        string
	tag         string
	tarPath     string
	image       v1.Image
	manifest    *v1.Manifest
	rawManifest []byte
	digest      v1.Hash
}

type registryManifestRequest struct {
	repo string
	ref  string
}

type registryBlobRequest struct {
	repo   string
	digest string
}

func (h *serverHandler) handleRegistry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
	if r.URL.Path == "/v2" || r.URL.Path == "/v2/" {
		w.WriteHeader(http.StatusOK)
		return
	}
	if req, ok := parseRegistryManifestRequest(r.URL.Path); ok {
		h.handleRegistryManifest(w, r, req)
		return
	}
	if req, ok := parseRegistryBlobRequest(r.URL.Path); ok {
		h.handleRegistryBlob(w, r, req)
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

func parseRegistryManifestRequest(urlPath string) (registryManifestRequest, bool) {
	const token = "/manifests/"
	if !strings.HasPrefix(urlPath, "/v2/") || !strings.Contains(urlPath, token) {
		return registryManifestRequest{}, false
	}
	rest := strings.TrimPrefix(urlPath, "/v2/")
	parts := strings.SplitN(rest, token, 2)
	if len(parts) != 2 {
		return registryManifestRequest{}, false
	}
	repo := strings.Trim(parts[0], "/")
	ref := strings.TrimSpace(parts[1])
	if repo == "" || ref == "" || strings.Contains(repo, "..") {
		return registryManifestRequest{}, false
	}
	return registryManifestRequest{repo: repo, ref: ref}, true
}

func parseRegistryBlobRequest(urlPath string) (registryBlobRequest, bool) {
	const token = "/blobs/"
	if !strings.HasPrefix(urlPath, "/v2/") || !strings.Contains(urlPath, token) {
		return registryBlobRequest{}, false
	}
	rest := strings.TrimPrefix(urlPath, "/v2/")
	parts := strings.SplitN(rest, token, 2)
	if len(parts) != 2 {
		return registryBlobRequest{}, false
	}
	repo := strings.Trim(parts[0], "/")
	digest := strings.TrimSpace(parts[1])
	if repo == "" || digest == "" || strings.Contains(repo, "..") {
		return registryBlobRequest{}, false
	}
	return registryBlobRequest{repo: repo, digest: digest}, true
}

func (h *serverHandler) handleRegistryManifest(w http.ResponseWriter, r *http.Request, req registryManifestRequest) {
	resolved, err := h.resolveRegistryImage(req.repo, req.ref)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	mediaType := string(types.DockerManifestSchema2)
	if resolved.manifest != nil && strings.TrimSpace(string(resolved.manifest.MediaType)) != "" {
		mediaType = string(resolved.manifest.MediaType)
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Docker-Content-Digest", resolved.digest.String())
	w.Header().Set("Content-Length", strconv.Itoa(len(resolved.rawManifest)))
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resolved.rawManifest)
}

func (h *serverHandler) handleRegistryBlob(w http.ResponseWriter, r *http.Request, req registryBlobRequest) {
	resolved, err := h.resolveRegistryImage(req.repo, req.digest)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	body, contentType, found, err := resolveRegistryBlobContent(resolved, req.digest)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if !found {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Docker-Content-Digest", req.digest)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (h *serverHandler) resolveRegistryImage(repo, ref string) (*registryResolvedImage, error) {
	entries, err := h.scanRegistryCatalog()
	if err != nil {
		return nil, err
	}
	candidates := make([]registryCatalogEntry, 0)
	for _, entry := range entries {
		if entry.repo == repo {
			candidates = append(candidates, entry)
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("repo not found: %s", repo)
	}
	if !strings.HasPrefix(ref, "sha256:") {
		for _, entry := range candidates {
			if entry.tag == ref {
				return loadRegistryResolvedImage(entry)
			}
		}
		return nil, fmt.Errorf("tag not found: %s:%s", repo, ref)
	}
	for _, entry := range candidates {
		resolved, loadErr := loadRegistryResolvedImage(entry)
		if loadErr != nil {
			continue
		}
		if resolved.digest.String() == ref {
			return resolved, nil
		}
	}
	for _, entry := range candidates {
		resolved, loadErr := loadRegistryResolvedImage(entry)
		if loadErr != nil {
			continue
		}
		if manifestContainsDigest(resolved.manifest, ref) {
			return resolved, nil
		}
	}
	return nil, fmt.Errorf("digest not found: %s@%s", repo, ref)
}

func (h *serverHandler) scanRegistryCatalog() ([]registryCatalogEntry, error) {
	imagesRoot := filepath.Join(h.rootAbs, "images")
	if _, err := os.Stat(imagesRoot); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	entries := make([]registryCatalogEntry, 0)
	err := filepath.WalkDir(imagesRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || strings.ToLower(filepath.Ext(path)) != ".tar" {
			return nil
		}
		manifest, err := tarball.LoadManifest(func() (io.ReadCloser, error) { return os.Open(path) })
		if err != nil {
			return nil
		}
		for _, descriptor := range manifest {
			for _, repoTag := range descriptor.RepoTags {
				tag, err := name.NewTag(repoTag, name.WeakValidation)
				if err != nil {
					continue
				}
				aliases := registryRepositoryAliases(tag.Repository.Name())
				for _, alias := range aliases {
					entries = append(entries, registryCatalogEntry{
						repoTag: repoTag,
						repo:    alias,
						tag:     tag.TagStr(),
						tarPath: path,
					})
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].repo == entries[j].repo {
			if entries[i].tag == entries[j].tag {
				return entries[i].tarPath < entries[j].tarPath
			}
			return entries[i].tag < entries[j].tag
		}
		return entries[i].repo < entries[j].repo
	})
	return entries, nil
}

func registryRepositoryAliases(repo string) []string {
	trimmed := strings.TrimSpace(strings.Trim(repo, "/"))
	if trimmed == "" {
		return nil
	}
	aliases := []string{trimmed}
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) == 2 && looksLikeRegistryDomain(parts[0]) {
		aliases = append(aliases, parts[1])
	}
	return dedupeStrings(aliases)
}

func looksLikeRegistryDomain(v string) bool {
	return strings.Contains(v, ".") || strings.Contains(v, ":") || v == "localhost"
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	return out
}

func loadRegistryResolvedImage(entry registryCatalogEntry) (*registryResolvedImage, error) {
	tag, err := name.NewTag(entry.repoTag, name.WeakValidation)
	if err != nil {
		return nil, err
	}
	imageObj, err := tarball.ImageFromPath(entry.tarPath, &tag)
	if err != nil {
		return nil, err
	}
	rawManifest, err := imageObj.RawManifest()
	if err != nil {
		return nil, err
	}
	manifest, err := imageObj.Manifest()
	if err != nil {
		return nil, err
	}
	digest, err := imageObj.Digest()
	if err != nil {
		return nil, err
	}
	return &registryResolvedImage{
		repo:        entry.repo,
		tag:         entry.tag,
		tarPath:     entry.tarPath,
		image:       imageObj,
		manifest:    manifest,
		rawManifest: rawManifest,
		digest:      digest,
	}, nil
}

func manifestContainsDigest(manifest *v1.Manifest, digest string) bool {
	if manifest == nil {
		return false
	}
	if manifest.Config.Digest.String() == digest {
		return true
	}
	for _, layer := range manifest.Layers {
		if layer.Digest.String() == digest {
			return true
		}
	}
	return false
}

func resolveRegistryBlobContent(resolved *registryResolvedImage, digest string) ([]byte, string, bool, error) {
	if resolved == nil || resolved.manifest == nil {
		return nil, "", false, nil
	}
	if resolved.manifest.Config.Digest.String() == digest {
		body, err := resolved.image.RawConfigFile()
		if err != nil {
			return nil, "", false, err
		}
		mediaType := strings.TrimSpace(string(resolved.manifest.Config.MediaType))
		if mediaType == "" {
			mediaType = "application/octet-stream"
		}
		return body, mediaType, true, nil
	}
	layers, err := resolved.image.Layers()
	if err != nil {
		return nil, "", false, err
	}
	for idx, layer := range layers {
		layerDigest, err := layer.Digest()
		if err != nil {
			return nil, "", false, err
		}
		if layerDigest.String() != digest {
			continue
		}
		body, err := readLayerCompressed(layer)
		if err != nil {
			return nil, "", false, err
		}
		mediaType := "application/octet-stream"
		if idx < len(resolved.manifest.Layers) && strings.TrimSpace(string(resolved.manifest.Layers[idx].MediaType)) != "" {
			mediaType = string(resolved.manifest.Layers[idx].MediaType)
		}
		return body, mediaType, true, nil
	}
	return nil, "", false, nil
}

func readLayerCompressed(layer v1.Layer) ([]byte, error) {
	rc, err := layer.Compressed()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	buf := bytes.Buffer{}
	if _, err := io.Copy(&buf, rc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
