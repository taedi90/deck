package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServe_StaticETagAndPut(t *testing.T) {
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

	h, err := NewHandler(root, HandlerOptions{})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	t.Run("health and removed endpoints", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected /healthz 200, got %d", rr.Code)
		}

		for _, route := range []string{"/api/agent/job", "/api/agent/lease", "/v2/", "/v2"} {
			req = httptest.NewRequest(http.MethodGet, route, nil)
			rr = httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusNotFound {
				t.Fatalf("expected %s 404, got %d", route, rr.Code)
			}
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

	t.Run("PUT upload and atomic replacement", func(t *testing.T) {
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
			if putRR.Code != http.StatusCreated {
				t.Fatalf("expected PUT %s 201, got %d", tc.path, putRR.Code)
			}

			raw, err := os.ReadFile(tc.file)
			if err != nil {
				t.Fatalf("read uploaded file %s: %v", tc.file, err)
			}
			if string(raw) != tc.body {
				t.Fatalf("unexpected file content for %s: %q", tc.file, string(raw))
			}
		}
	})

	t.Run("deny traversal and .deck access", func(t *testing.T) {
		for _, tc := range []struct {
			method string
			path   string
		}{
			{method: http.MethodGet, path: "/files/../.deck/secret"},
			{method: http.MethodPut, path: "/files/../.deck/secret"},
			{method: http.MethodPut, path: "/files/.deck/secret"},
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
			if eventType == auditEventRegistrySeed {
				t.Fatalf("unexpected registry seed audit event")
			}
		}
	})
}
