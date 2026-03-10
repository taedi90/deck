package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

func TestServe_StaticReadOnly(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "files"), 0o755); err != nil {
		t.Fatalf("mkdir files: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "packages"), 0o755); err != nil {
		t.Fatalf("mkdir packages: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "images"), 0o755); err != nil {
		t.Fatalf("mkdir images: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "workflows"), 0o755); err != nil {
		t.Fatalf("mkdir workflows: %v", err)
	}

	originalBody := []byte("payload-123\n")
	if err := os.WriteFile(filepath.Join(root, "files", "a.txt"), originalBody, 0o644); err != nil {
		t.Fatalf("write seed file: %v", err)
	}
	registryTag, err := name.NewTag("registry.k8s.io/kube-apiserver:v1.30.1", name.WeakValidation)
	if err != nil {
		t.Fatalf("name.NewTag: %v", err)
	}
	registryImage, err := random.Image(1024, 1)
	if err != nil {
		t.Fatalf("random.Image: %v", err)
	}
	registryTarPath := filepath.Join(root, "images", "registry.k8s.io_kube-apiserver_v1.30.1.tar")
	if err := tarball.WriteToFile(registryTarPath, registryTag, registryImage); err != nil {
		t.Fatalf("tarball.WriteToFile: %v", err)
	}
	rawManifest, err := registryImage.RawManifest()
	if err != nil {
		t.Fatalf("RawManifest: %v", err)
	}
	manifestDigest, err := registryImage.Digest()
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	manifest, err := registryImage.Manifest()
	if err != nil {
		t.Fatalf("Manifest: %v", err)
	}
	rawConfig, err := registryImage.RawConfigFile()
	if err != nil {
		t.Fatalf("RawConfigFile: %v", err)
	}
	layers, err := registryImage.Layers()
	if err != nil {
		t.Fatalf("Layers: %v", err)
	}
	if len(layers) == 0 {
		t.Fatal("expected at least one layer")
	}
	layerRC, err := layers[0].Compressed()
	if err != nil {
		t.Fatalf("Compressed: %v", err)
	}
	firstLayerBytes, err := io.ReadAll(layerRC)
	_ = layerRC.Close()
	if err != nil {
		t.Fatalf("ReadAll layer: %v", err)
	}
	nestedTag, err := name.NewTag("registry.k8s.io/coredns/coredns:v1.11.1", name.WeakValidation)
	if err != nil {
		t.Fatalf("name.NewTag nested: %v", err)
	}
	nestedImage, err := random.Image(512, 1)
	if err != nil {
		t.Fatalf("random.Image nested: %v", err)
	}
	nestedTarPath := filepath.Join(root, "images", "registry.k8s.io_coredns_coredns_v1.11.1.tar")
	if err := tarball.WriteToFile(nestedTarPath, nestedTag, nestedImage); err != nil {
		t.Fatalf("tarball.WriteToFile nested: %v", err)
	}

	h, err := NewHandler(root, HandlerOptions{})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	t.Run("health and removed alpha endpoints", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected /healthz 200, got %d", rr.Code)
		}

		for _, route := range []string{"/api/agent/job", "/api/agent/lease"} {
			req = httptest.NewRequest(http.MethodGet, route, nil)
			rr = httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusNotFound {
				t.Fatalf("expected %s 404, got %d", route, rr.Code)
			}
		}
	})

	t.Run("v2 registry manifest and blob serving", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v2/", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected /v2/ 200, got %d", rr.Code)
		}
		if got := rr.Header().Get("Docker-Distribution-API-Version"); got != "registry/2.0" {
			t.Fatalf("unexpected registry header: %q", got)
		}

		manifestReq := httptest.NewRequest(http.MethodGet, "/v2/kube-apiserver/manifests/v1.30.1", nil)
		manifestRR := httptest.NewRecorder()
		h.ServeHTTP(manifestRR, manifestReq)
		if manifestRR.Code != http.StatusOK {
			t.Fatalf("expected manifest GET 200, got %d", manifestRR.Code)
		}
		if !bytes.Equal(manifestRR.Body.Bytes(), rawManifest) {
			t.Fatalf("unexpected manifest body")
		}
		if got := manifestRR.Header().Get("Docker-Content-Digest"); got != manifestDigest.String() {
			t.Fatalf("unexpected manifest digest header: %q", got)
		}

		headReq := httptest.NewRequest(http.MethodHead, "/v2/kube-apiserver/manifests/"+manifestDigest.String(), nil)
		headRR := httptest.NewRecorder()
		h.ServeHTTP(headRR, headReq)
		if headRR.Code != http.StatusOK {
			t.Fatalf("expected manifest HEAD 200, got %d", headRR.Code)
		}
		if headRR.Body.Len() != 0 {
			t.Fatalf("expected manifest HEAD empty body")
		}

		configReq := httptest.NewRequest(http.MethodGet, "/v2/kube-apiserver/blobs/"+manifest.Config.Digest.String(), nil)
		configRR := httptest.NewRecorder()
		h.ServeHTTP(configRR, configReq)
		if configRR.Code != http.StatusOK {
			t.Fatalf("expected config blob GET 200, got %d", configRR.Code)
		}
		if !bytes.Equal(configRR.Body.Bytes(), rawConfig) {
			t.Fatalf("unexpected config blob body")
		}

		layerReq := httptest.NewRequest(http.MethodGet, "/v2/kube-apiserver/blobs/"+manifest.Layers[0].Digest.String(), nil)
		layerRR := httptest.NewRecorder()
		h.ServeHTTP(layerRR, layerReq)
		if layerRR.Code != http.StatusOK {
			t.Fatalf("expected layer blob GET 200, got %d", layerRR.Code)
		}
		if !bytes.Equal(layerRR.Body.Bytes(), firstLayerBytes) {
			t.Fatalf("unexpected layer blob body")
		}

		nestedReq := httptest.NewRequest(http.MethodGet, "/v2/coredns/coredns/manifests/v1.11.1", nil)
		nestedRR := httptest.NewRecorder()
		h.ServeHTTP(nestedRR, nestedReq)
		if nestedRR.Code != http.StatusOK {
			t.Fatalf("expected nested repo manifest GET 200, got %d", nestedRR.Code)
		}
	})

	t.Run("GET HEAD parity, ETag, and If-None-Match", func(t *testing.T) {
		getReq := httptest.NewRequest(http.MethodGet, "/files/a.txt", nil)
		getRR := httptest.NewRecorder()
		h.ServeHTTP(getRR, getReq)
		if getRR.Code != http.StatusOK {
			t.Fatalf("expected GET 200, got %d", getRR.Code)
		}
		if !bytes.Equal(getRR.Body.Bytes(), originalBody) {
			t.Fatalf("unexpected GET body: %q", getRR.Body.String())
		}

		sum := sha256.Sum256(originalBody)
		expectedETag := "\"sha256:" + hex.EncodeToString(sum[:]) + "\""
		if got := getRR.Header().Get("ETag"); got != expectedETag {
			t.Fatalf("unexpected ETag: got=%q want=%q", got, expectedETag)
		}

		headReq := httptest.NewRequest(http.MethodHead, "/files/a.txt", nil)
		headRR := httptest.NewRecorder()
		h.ServeHTTP(headRR, headReq)
		if headRR.Code != http.StatusOK {
			t.Fatalf("expected HEAD 200, got %d", headRR.Code)
		}
		if headRR.Body.Len() != 0 {
			t.Fatalf("expected HEAD empty body, got %q", headRR.Body.String())
		}
		for _, headerKey := range []string{"ETag", "Content-Type", "Content-Length", "Accept-Ranges", "Cache-Control"} {
			if getRR.Header().Get(headerKey) != headRR.Header().Get(headerKey) {
				t.Fatalf("header mismatch for %s: GET=%q HEAD=%q", headerKey, getRR.Header().Get(headerKey), headRR.Header().Get(headerKey))
			}
		}

		notModifiedReq := httptest.NewRequest(http.MethodGet, "/files/a.txt", nil)
		notModifiedReq.Header.Set("If-None-Match", expectedETag)
		notModifiedRR := httptest.NewRecorder()
		h.ServeHTTP(notModifiedRR, notModifiedReq)
		if notModifiedRR.Code != http.StatusNotModified {
			t.Fatalf("expected 304, got %d", notModifiedRR.Code)
		}
		if notModifiedRR.Body.Len() != 0 {
			t.Fatalf("expected 304 empty body, got %q", notModifiedRR.Body.String())
		}
		if notModifiedRR.Header().Get("ETag") != expectedETag {
			t.Fatalf("304 response must keep ETag header")
		}
	})

	t.Run("legacy PUT uploads rejected", func(t *testing.T) {
		for _, tc := range []struct {
			path string
			body string
			file string
		}{
			{path: "/files/new/file.txt", body: "file-data", file: filepath.Join(root, "files", "new", "file.txt")},
			{path: "/packages/deb/pkg.txt", body: "pkg-data", file: filepath.Join(root, "packages", "deb", "pkg.txt")},
			{path: "/images/manifests/app.json", body: "img-data", file: filepath.Join(root, "images", "manifests", "app.json")},
			{path: "/workflows/flow.yaml", body: "wf-data", file: filepath.Join(root, "workflows", "flow.yaml")},
			{path: "/workflows/index.json", body: "{\"v\":1}", file: filepath.Join(root, "workflows", "index.json")},
		} {
			putReq := httptest.NewRequest(http.MethodPut, tc.path, strings.NewReader(tc.body))
			putRR := httptest.NewRecorder()
			h.ServeHTTP(putRR, putReq)
			if putRR.Code != http.StatusMethodNotAllowed {
				t.Fatalf("expected PUT %s 405, got %d", tc.path, putRR.Code)
			}

			raw, err := os.ReadFile(tc.file)
			if !os.IsNotExist(err) {
				t.Fatalf("expected PUT %s to keep file absent, got err=%v content=%q", tc.path, err, string(raw))
			}
		}
	})

	t.Run("deny traversal and .deck access", func(t *testing.T) {
		for _, tc := range []struct {
			method string
			path   string
		}{
			{method: http.MethodGet, path: "/files/../.deck/secret"},
			{method: http.MethodGet, path: "/files/%2e%2e/.deck/secret"},
		} {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("x"))
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusForbidden {
				t.Fatalf("expected %s %s to be 403, got %d", tc.method, tc.path, rr.Code)
			}
		}
	})

	t.Run("audit log excludes alpha events", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/files/a.txt", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)

		auditPath := filepath.Join(root, ".deck", "logs", "server-audit.log")
		raw, err := os.ReadFile(auditPath)
		if err != nil {
			t.Fatalf("read audit log: %v", err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			entry := map[string]any{}
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				t.Fatalf("parse audit line: %v", err)
			}
			eventType, _ := entry["event_type"].(string)
			if strings.HasPrefix(eventType, "alpha_") {
				t.Fatalf("unexpected alpha audit event: %q", eventType)
			}
			if eventType == "registry_seed" {
				t.Fatalf("unexpected registry seed audit event")
			}
		}
	})
}

func TestHealth(t *testing.T) {
	root := t.TempDir()
	h, err := NewHandler(root, HandlerOptions{})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected GET /healthz 200, got %d", rr.Code)
	}
}

func TestHandlerRejectsLegacyPutUploads(t *testing.T) {
	root := t.TempDir()
	for _, category := range []string{"files", "packages", "images", "workflows"} {
		if err := os.MkdirAll(filepath.Join(root, category), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", category, err)
		}
	}

	h, err := NewHandler(root, HandlerOptions{})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	for _, tc := range []struct {
		path string
		file string
		body string
	}{
		{path: "/files/new/file.txt", file: filepath.Join(root, "files", "new", "file.txt"), body: "file-data"},
		{path: "/packages/deb/pkg.txt", file: filepath.Join(root, "packages", "deb", "pkg.txt"), body: "pkg-data"},
		{path: "/images/manifests/app.json", file: filepath.Join(root, "images", "manifests", "app.json"), body: "img-data"},
		{path: "/workflows/flow.yaml", file: filepath.Join(root, "workflows", "flow.yaml"), body: "wf-data"},
	} {
		putReq := httptest.NewRequest(http.MethodPut, tc.path, strings.NewReader(tc.body))
		putRR := httptest.NewRecorder()
		h.ServeHTTP(putRR, putReq)
		if putRR.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected PUT %s 405, got %d", tc.path, putRR.Code)
		}

		raw, readErr := os.ReadFile(tc.file)
		if !os.IsNotExist(readErr) {
			t.Fatalf("expected no file write for PUT %s, got err=%v content=%q", tc.path, readErr, string(raw))
		}
	}
}
