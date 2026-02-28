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

	t.Run("enqueues and dequeues alpha job", func(t *testing.T) {
		enqReq := httptest.NewRequest(http.MethodPost, "/api/agent/job", strings.NewReader(`{"id":"j-1","type":"echo","message":"hello"}`))
		enqRR := httptest.NewRecorder()
		h.ServeHTTP(enqRR, enqReq)
		if enqRR.Code != http.StatusOK {
			t.Fatalf("expected enqueue 200, got %d", enqRR.Code)
		}

		leaseReq := httptest.NewRequest(http.MethodPost, "/api/agent/lease", strings.NewReader(`{"agent":"x"}`))
		leaseRR := httptest.NewRecorder()
		h.ServeHTTP(leaseRR, leaseReq)
		if leaseRR.Code != http.StatusOK {
			t.Fatalf("expected lease 200, got %d", leaseRR.Code)
		}
		if !strings.Contains(leaseRR.Body.String(), `"id":"j-1"`) {
			t.Fatalf("expected leased job in response: %q", leaseRR.Body.String())
		}

		leaseReq2 := httptest.NewRequest(http.MethodPost, "/api/agent/lease", strings.NewReader(`{"agent":"x"}`))
		leaseRR2 := httptest.NewRecorder()
		h.ServeHTTP(leaseRR2, leaseReq2)
		if leaseRR2.Code != http.StatusOK {
			t.Fatalf("expected second lease 200, got %d", leaseRR2.Code)
		}
		if !strings.Contains(leaseRR2.Body.String(), `"job":null`) {
			t.Fatalf("expected empty queue on second lease: %q", leaseRR2.Body.String())
		}
	})

	t.Run("rejects invalid alpha job payload", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/agent/job", strings.NewReader(`{"id":"","type":"invalid"}`))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("accepts agent report", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/agent/report", strings.NewReader(`{"job_id":"j-1","status":"success"}`))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
		if !strings.Contains(rr.Body.String(), `"status":"accepted"`) {
			t.Fatalf("unexpected report response: %q", rr.Body.String())
		}
	})

	t.Run("lists recent agent reports", func(t *testing.T) {
		postReq := httptest.NewRequest(http.MethodPost, "/api/agent/report", strings.NewReader(`{"job_id":"j-2","job_type":"echo","status":"success","detail":"hello"}`))
		postRR := httptest.NewRecorder()
		h.ServeHTTP(postRR, postReq)
		if postRR.Code != http.StatusOK {
			t.Fatalf("expected report post 200, got %d", postRR.Code)
		}

		getReq := httptest.NewRequest(http.MethodGet, "/api/agent/reports", nil)
		getRR := httptest.NewRecorder()
		h.ServeHTTP(getRR, getReq)
		if getRR.Code != http.StatusOK {
			t.Fatalf("expected reports get 200, got %d", getRR.Code)
		}
		if !strings.Contains(getRR.Body.String(), `"status":"ok"`) || !strings.Contains(getRR.Body.String(), `"job_id":"j-2"`) {
			t.Fatalf("unexpected reports response: %q", getRR.Body.String())
		}
	})

	t.Run("filters reports by job_id", func(t *testing.T) {
		postA := httptest.NewRequest(http.MethodPost, "/api/agent/report", strings.NewReader(`{"job_id":"j-a","status":"success"}`))
		postARR := httptest.NewRecorder()
		h.ServeHTTP(postARR, postA)

		postB := httptest.NewRequest(http.MethodPost, "/api/agent/report", strings.NewReader(`{"job_id":"j-b","status":"success"}`))
		postBRR := httptest.NewRecorder()
		h.ServeHTTP(postBRR, postB)

		getReq := httptest.NewRequest(http.MethodGet, "/api/agent/reports?job_id=j-a", nil)
		getRR := httptest.NewRecorder()
		h.ServeHTTP(getRR, getReq)
		if getRR.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", getRR.Code)
		}
		if !strings.Contains(getRR.Body.String(), `"job_id":"j-a"`) || strings.Contains(getRR.Body.String(), `"job_id":"j-b"`) {
			t.Fatalf("unexpected filtered response: %q", getRR.Body.String())
		}
	})

	t.Run("limits reports count", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			req := httptest.NewRequest(http.MethodPost, "/api/agent/report", strings.NewReader(`{"job_id":"j-limit","status":"success"}`))
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
		}

		getReq := httptest.NewRequest(http.MethodGet, "/api/agent/reports?limit=1", nil)
		getRR := httptest.NewRecorder()
		h.ServeHTTP(getRR, getReq)
		if getRR.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", getRR.Code)
		}
		var payload struct {
			Status  string           `json:"status"`
			Reports []map[string]any `json:"reports"`
		}
		if err := json.Unmarshal(getRR.Body.Bytes(), &payload); err != nil {
			t.Fatalf("parse reports response: %v", err)
		}
		if payload.Status != "ok" || len(payload.Reports) != 1 {
			t.Fatalf("unexpected limited response: %+v", payload)
		}
	})

	t.Run("rejects invalid limit", func(t *testing.T) {
		getReq := httptest.NewRequest(http.MethodGet, "/api/agent/reports?limit=0", nil)
		getRR := httptest.NewRecorder()
		h.ServeHTTP(getRR, getReq)
		if getRR.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", getRR.Code)
		}
		if !strings.Contains(getRR.Body.String(), `"status":"invalid_limit"`) {
			t.Fatalf("unexpected invalid limit response: %q", getRR.Body.String())
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
