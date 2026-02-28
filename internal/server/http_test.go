package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewHandler(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "files"), 0o755); err != nil {
		t.Fatalf("mkdir files: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "packages"), 0o755); err != nil {
		t.Fatalf("mkdir packages: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "files", "a.txt"), []byte("file-data"), 0o644); err != nil {
		t.Fatalf("write files entry: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "packages", "pkg.txt"), []byte("pkg-data"), 0o644); err != nil {
		t.Fatalf("write packages entry: %v", err)
	}

	h, err := NewHandler(root)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	t.Run("serves files", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/files/a.txt", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
		if rr.Body.String() != "file-data" {
			t.Fatalf("unexpected body: %q", rr.Body.String())
		}
	})

	t.Run("serves packages", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/packages/pkg.txt", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
		if rr.Body.String() != "pkg-data" {
			t.Fatalf("unexpected body: %q", rr.Body.String())
		}
	})

	t.Run("returns 404 for unsupported routes", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/anything", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rr.Code)
		}
	})

	t.Run("serves api health", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
	})

	t.Run("accepts agent heartbeat", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/agent/heartbeat", strings.NewReader(`{"agent":"x"}`))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
	})

	t.Run("serves agent lease", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/agent/lease", strings.NewReader(`{"agent":"x"}`))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
		if !strings.Contains(rr.Body.String(), `"status":"ok"`) {
			t.Fatalf("unexpected lease response: %q", rr.Body.String())
		}
	})

	t.Run("writes audit logs", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}

		auditPath := filepath.Join(root, ".deck", "logs", "server-audit.log")
		raw, err := os.ReadFile(auditPath)
		if err != nil {
			t.Fatalf("read audit log: %v", err)
		}
		lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
		if len(lines) == 0 {
			t.Fatalf("expected at least one audit log line")
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(lines[len(lines)-1]), &entry); err != nil {
			t.Fatalf("parse audit log json: %v", err)
		}
		for _, k := range []string{"timestamp", "method", "path", "status", "duration_ms"} {
			if _, ok := entry[k]; !ok {
				t.Fatalf("missing audit field %s in %+v", k, entry)
			}
		}
	})
}
