package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

	h, err := NewHandler(root, HandlerOptions{})
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

		jobsReq := httptest.NewRequest(http.MethodGet, "/api/agent/jobs", nil)
		jobsRR := httptest.NewRecorder()
		h.ServeHTTP(jobsRR, jobsReq)
		if jobsRR.Code != http.StatusOK {
			t.Fatalf("expected jobs 200, got %d", jobsRR.Code)
		}
		if !strings.Contains(jobsRR.Body.String(), `"id":"j-1"`) {
			t.Fatalf("expected queued job in jobs response: %q", jobsRR.Body.String())
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

		jobsReq2 := httptest.NewRequest(http.MethodGet, "/api/agent/jobs", nil)
		jobsRR2 := httptest.NewRecorder()
		h.ServeHTTP(jobsRR2, jobsReq2)
		if jobsRR2.Code != http.StatusOK {
			t.Fatalf("expected jobs 200, got %d", jobsRR2.Code)
		}
		if !strings.Contains(jobsRR2.Body.String(), `"jobs":[]`) {
			t.Fatalf("expected empty jobs queue after lease: %q", jobsRR2.Body.String())
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

	t.Run("rejects invalid max_attempts", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/agent/job", strings.NewReader(`{"id":"j-bad","type":"noop","max_attempts":-1}`))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("rejects invalid retry_delay_sec", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/agent/job", strings.NewReader(`{"id":"j-bad-delay","type":"noop","retry_delay_sec":-1}`))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("requeues failed job when attempts remain", func(t *testing.T) {
		rootRetry := t.TempDir()
		hRetry, err := NewHandler(rootRetry, HandlerOptions{})
		if err != nil {
			t.Fatalf("NewHandler: %v", err)
		}

		enqReq := httptest.NewRequest(http.MethodPost, "/api/agent/job", strings.NewReader(`{"id":"j-retry","type":"echo","message":"hello","max_attempts":2}`))
		enqRR := httptest.NewRecorder()
		hRetry.ServeHTTP(enqRR, enqReq)
		if enqRR.Code != http.StatusOK {
			t.Fatalf("expected enqueue 200, got %d", enqRR.Code)
		}

		leaseReq := httptest.NewRequest(http.MethodPost, "/api/agent/lease", strings.NewReader(`{"agent":"x"}`))
		leaseRR := httptest.NewRecorder()
		hRetry.ServeHTTP(leaseRR, leaseReq)
		if leaseRR.Code != http.StatusOK {
			t.Fatalf("expected lease 200, got %d", leaseRR.Code)
		}
		if !strings.Contains(leaseRR.Body.String(), `"id":"j-retry"`) || !strings.Contains(leaseRR.Body.String(), `"attempt":1`) {
			t.Fatalf("unexpected lease response: %q", leaseRR.Body.String())
		}

		repReq := httptest.NewRequest(http.MethodPost, "/api/agent/report", strings.NewReader(`{"job_id":"j-retry","status":"failed"}`))
		repRR := httptest.NewRecorder()
		hRetry.ServeHTTP(repRR, repReq)
		if repRR.Code != http.StatusOK {
			t.Fatalf("expected report 200, got %d", repRR.Code)
		}

		jobsReq := httptest.NewRequest(http.MethodGet, "/api/agent/jobs", nil)
		jobsRR := httptest.NewRecorder()
		hRetry.ServeHTTP(jobsRR, jobsReq)
		if jobsRR.Code != http.StatusOK {
			t.Fatalf("expected jobs 200, got %d", jobsRR.Code)
		}
		if !strings.Contains(jobsRR.Body.String(), `"id":"j-retry"`) {
			t.Fatalf("expected retry job queued after failed report: %q", jobsRR.Body.String())
		}
	})

	t.Run("does not requeue failed job when attempts exhausted", func(t *testing.T) {
		rootFinal := t.TempDir()
		hFinal, err := NewHandler(rootFinal, HandlerOptions{})
		if err != nil {
			t.Fatalf("NewHandler: %v", err)
		}

		enqReq := httptest.NewRequest(http.MethodPost, "/api/agent/job", strings.NewReader(`{"id":"j-final","type":"noop","max_attempts":1}`))
		enqRR := httptest.NewRecorder()
		hFinal.ServeHTTP(enqRR, enqReq)
		if enqRR.Code != http.StatusOK {
			t.Fatalf("expected enqueue 200, got %d", enqRR.Code)
		}

		leaseReq := httptest.NewRequest(http.MethodPost, "/api/agent/lease", strings.NewReader(`{"agent":"x"}`))
		leaseRR := httptest.NewRecorder()
		hFinal.ServeHTTP(leaseRR, leaseReq)
		if leaseRR.Code != http.StatusOK {
			t.Fatalf("expected lease 200, got %d", leaseRR.Code)
		}

		repReq := httptest.NewRequest(http.MethodPost, "/api/agent/report", strings.NewReader(`{"job_id":"j-final","status":"failed"}`))
		repRR := httptest.NewRecorder()
		hFinal.ServeHTTP(repRR, repReq)
		if repRR.Code != http.StatusOK {
			t.Fatalf("expected report 200, got %d", repRR.Code)
		}

		jobsReq := httptest.NewRequest(http.MethodGet, "/api/agent/jobs", nil)
		jobsRR := httptest.NewRecorder()
		hFinal.ServeHTTP(jobsRR, jobsReq)
		if jobsRR.Code != http.StatusOK {
			t.Fatalf("expected jobs 200, got %d", jobsRR.Code)
		}
		if strings.Contains(jobsRR.Body.String(), `"id":"j-final"`) {
			t.Fatalf("expected exhausted job not requeued: %q", jobsRR.Body.String())
		}
	})

	t.Run("delays retry lease until next eligible time", func(t *testing.T) {
		rootDelay := t.TempDir()
		hDelay, err := NewHandler(rootDelay, HandlerOptions{})
		if err != nil {
			t.Fatalf("NewHandler: %v", err)
		}

		enqReq := httptest.NewRequest(http.MethodPost, "/api/agent/job", strings.NewReader(`{"id":"j-delay","type":"noop","max_attempts":2,"retry_delay_sec":1}`))
		enqRR := httptest.NewRecorder()
		hDelay.ServeHTTP(enqRR, enqReq)
		if enqRR.Code != http.StatusOK {
			t.Fatalf("expected enqueue 200, got %d", enqRR.Code)
		}

		leaseReq1 := httptest.NewRequest(http.MethodPost, "/api/agent/lease", strings.NewReader(`{"agent":"x"}`))
		leaseRR1 := httptest.NewRecorder()
		hDelay.ServeHTTP(leaseRR1, leaseReq1)
		if leaseRR1.Code != http.StatusOK {
			t.Fatalf("expected first lease 200, got %d", leaseRR1.Code)
		}
		if !strings.Contains(leaseRR1.Body.String(), `"id":"j-delay"`) {
			t.Fatalf("expected first lease job payload: %q", leaseRR1.Body.String())
		}

		repReq := httptest.NewRequest(http.MethodPost, "/api/agent/report", strings.NewReader(`{"job_id":"j-delay","status":"failed"}`))
		repRR := httptest.NewRecorder()
		hDelay.ServeHTTP(repRR, repReq)
		if repRR.Code != http.StatusOK {
			t.Fatalf("expected failed report 200, got %d", repRR.Code)
		}

		jobsReq := httptest.NewRequest(http.MethodGet, "/api/agent/jobs", nil)
		jobsRR := httptest.NewRecorder()
		hDelay.ServeHTTP(jobsRR, jobsReq)
		if jobsRR.Code != http.StatusOK {
			t.Fatalf("expected jobs 200, got %d", jobsRR.Code)
		}
		if !strings.Contains(jobsRR.Body.String(), `"id":"j-delay"`) || !strings.Contains(jobsRR.Body.String(), `"next_eligible_at"`) {
			t.Fatalf("expected queued delayed job with scheduling metadata: %q", jobsRR.Body.String())
		}

		leaseReq2 := httptest.NewRequest(http.MethodPost, "/api/agent/lease", strings.NewReader(`{"agent":"x"}`))
		leaseRR2 := httptest.NewRecorder()
		hDelay.ServeHTTP(leaseRR2, leaseReq2)
		if leaseRR2.Code != http.StatusOK {
			t.Fatalf("expected immediate second lease 200, got %d", leaseRR2.Code)
		}
		if !strings.Contains(leaseRR2.Body.String(), `"job":null`) {
			t.Fatalf("expected no lease before retry delay elapses: %q", leaseRR2.Body.String())
		}

		time.Sleep(1100 * time.Millisecond)

		leaseReq3 := httptest.NewRequest(http.MethodPost, "/api/agent/lease", strings.NewReader(`{"agent":"x"}`))
		leaseRR3 := httptest.NewRecorder()
		hDelay.ServeHTTP(leaseRR3, leaseReq3)
		if leaseRR3.Code != http.StatusOK {
			t.Fatalf("expected delayed third lease 200, got %d", leaseRR3.Code)
		}
		if !strings.Contains(leaseRR3.Body.String(), `"id":"j-delay"`) || !strings.Contains(leaseRR3.Body.String(), `"attempt":2`) {
			t.Fatalf("expected lease after delay with second attempt: %q", leaseRR3.Body.String())
		}
	})

	t.Run("persists queue and reports across handler restart", func(t *testing.T) {
		enqReq := httptest.NewRequest(http.MethodPost, "/api/agent/job", strings.NewReader(`{"id":"j-persist","type":"noop"}`))
		enqRR := httptest.NewRecorder()
		h.ServeHTTP(enqRR, enqReq)
		if enqRR.Code != http.StatusOK {
			t.Fatalf("expected enqueue 200, got %d", enqRR.Code)
		}

		repReq := httptest.NewRequest(http.MethodPost, "/api/agent/report", strings.NewReader(`{"job_id":"j-persist","status":"success"}`))
		repRR := httptest.NewRecorder()
		h.ServeHTTP(repRR, repReq)
		if repRR.Code != http.StatusOK {
			t.Fatalf("expected report 200, got %d", repRR.Code)
		}

		h2, err := NewHandler(root, HandlerOptions{})
		if err != nil {
			t.Fatalf("NewHandler restart: %v", err)
		}

		leaseReq := httptest.NewRequest(http.MethodPost, "/api/agent/lease", strings.NewReader(`{"agent":"x"}`))
		leaseRR := httptest.NewRecorder()
		h2.ServeHTTP(leaseRR, leaseReq)
		if leaseRR.Code != http.StatusOK {
			t.Fatalf("expected lease 200, got %d", leaseRR.Code)
		}
		if !strings.Contains(leaseRR.Body.String(), `"id":"j-persist"`) {
			t.Fatalf("expected persisted queued job in lease response: %q", leaseRR.Body.String())
		}

		reportsReq := httptest.NewRequest(http.MethodGet, "/api/agent/reports?job_id=j-persist", nil)
		reportsRR := httptest.NewRecorder()
		h2.ServeHTTP(reportsRR, reportsReq)
		if reportsRR.Code != http.StatusOK {
			t.Fatalf("expected reports 200, got %d", reportsRR.Code)
		}
		if !strings.Contains(reportsRR.Body.String(), `"job_id":"j-persist"`) {
			t.Fatalf("expected persisted report in response: %q", reportsRR.Body.String())
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

	t.Run("filters reports by job_type and status", func(t *testing.T) {
		req1 := httptest.NewRequest(http.MethodPost, "/api/agent/report", strings.NewReader(`{"job_id":"j-t1","job_type":"echo","status":"success"}`))
		rr1 := httptest.NewRecorder()
		h.ServeHTTP(rr1, req1)

		req2 := httptest.NewRequest(http.MethodPost, "/api/agent/report", strings.NewReader(`{"job_id":"j-t2","job_type":"noop","status":"failed"}`))
		rr2 := httptest.NewRecorder()
		h.ServeHTTP(rr2, req2)

		getReq := httptest.NewRequest(http.MethodGet, "/api/agent/reports?job_type=echo&status=success", nil)
		getRR := httptest.NewRecorder()
		h.ServeHTTP(getRR, getReq)
		if getRR.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", getRR.Code)
		}
		if !strings.Contains(getRR.Body.String(), `"job_id":"j-t1"`) || strings.Contains(getRR.Body.String(), `"job_id":"j-t2"`) {
			t.Fatalf("unexpected filtered response: %q", getRR.Body.String())
		}
	})

	t.Run("applies report max retention option", func(t *testing.T) {
		root2 := t.TempDir()
		h2, err := NewHandler(root2, HandlerOptions{ReportMax: 2})
		if err != nil {
			t.Fatalf("NewHandler: %v", err)
		}
		for i := 0; i < 3; i++ {
			req := httptest.NewRequest(http.MethodPost, "/api/agent/report", strings.NewReader(`{"job_id":"j-ret","job_type":"echo","status":"success"}`))
			rr := httptest.NewRecorder()
			h2.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("expected report post 200, got %d", rr.Code)
			}
		}

		getReq := httptest.NewRequest(http.MethodGet, "/api/agent/reports", nil)
		getRR := httptest.NewRecorder()
		h2.ServeHTTP(getRR, getReq)
		if getRR.Code != http.StatusOK {
			t.Fatalf("expected reports 200, got %d", getRR.Code)
		}
		var payload struct {
			Status  string           `json:"status"`
			Reports []map[string]any `json:"reports"`
		}
		if err := json.Unmarshal(getRR.Body.Bytes(), &payload); err != nil {
			t.Fatalf("parse reports payload: %v", err)
		}
		if len(payload.Reports) != 2 {
			t.Fatalf("expected 2 retained reports, got %d", len(payload.Reports))
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

	t.Run("writes alpha lifecycle audit events", func(t *testing.T) {
		rootAudit := t.TempDir()
		hAudit, err := NewHandler(rootAudit, HandlerOptions{})
		if err != nil {
			t.Fatalf("NewHandler: %v", err)
		}

		enqueueReq := httptest.NewRequest(http.MethodPost, "/api/agent/job", strings.NewReader(`{"id":"j-audit","type":"noop","max_attempts":2}`))
		enqueueRR := httptest.NewRecorder()
		hAudit.ServeHTTP(enqueueRR, enqueueReq)
		if enqueueRR.Code != http.StatusOK {
			t.Fatalf("expected enqueue 200, got %d", enqueueRR.Code)
		}

		leaseReq1 := httptest.NewRequest(http.MethodPost, "/api/agent/lease", strings.NewReader(`{"agent":"x"}`))
		leaseRR1 := httptest.NewRecorder()
		hAudit.ServeHTTP(leaseRR1, leaseReq1)
		if leaseRR1.Code != http.StatusOK {
			t.Fatalf("expected lease 200, got %d", leaseRR1.Code)
		}

		reportReq1 := httptest.NewRequest(http.MethodPost, "/api/agent/report", strings.NewReader(`{"job_id":"j-audit","status":"failed"}`))
		reportRR1 := httptest.NewRecorder()
		hAudit.ServeHTTP(reportRR1, reportReq1)
		if reportRR1.Code != http.StatusOK {
			t.Fatalf("expected report 200, got %d", reportRR1.Code)
		}

		leaseReq2 := httptest.NewRequest(http.MethodPost, "/api/agent/lease", strings.NewReader(`{"agent":"x"}`))
		leaseRR2 := httptest.NewRecorder()
		hAudit.ServeHTTP(leaseRR2, leaseReq2)
		if leaseRR2.Code != http.StatusOK {
			t.Fatalf("expected second lease 200, got %d", leaseRR2.Code)
		}

		reportReq2 := httptest.NewRequest(http.MethodPost, "/api/agent/report", strings.NewReader(`{"job_id":"j-audit","status":"failed"}`))
		reportRR2 := httptest.NewRecorder()
		hAudit.ServeHTTP(reportRR2, reportReq2)
		if reportRR2.Code != http.StatusOK {
			t.Fatalf("expected second report 200, got %d", reportRR2.Code)
		}

		auditPath := filepath.Join(rootAudit, ".deck", "logs", "server-audit.log")
		raw, err := os.ReadFile(auditPath)
		if err != nil {
			t.Fatalf("read audit log: %v", err)
		}
		lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
		if len(lines) == 0 {
			t.Fatalf("expected audit lines")
		}

		eventSeen := map[string]bool{}
		finalMatched := false
		for _, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var entry map[string]any
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				t.Fatalf("parse audit json: %v", err)
			}
			etype, _ := entry["event_type"].(string)
			if etype == "" {
				continue
			}
			eventSeen[etype] = true
			if etype == "alpha_job_final_failed" {
				decision, _ := entry["decision"].(string)
				jobID, _ := entry["job_id"].(string)
				attempt, _ := entry["attempt"].(float64)
				maxAttempts, _ := entry["max_attempts"].(float64)
				if decision == "exhausted" && jobID == "j-audit" && int(attempt) == 2 && int(maxAttempts) == 2 {
					finalMatched = true
				}
			}
		}

		for _, eventType := range []string{"alpha_job_enqueued", "alpha_job_leased", "alpha_job_requeued", "alpha_job_final_failed"} {
			if !eventSeen[eventType] {
				t.Fatalf("expected lifecycle event %s in audit log", eventType)
			}
		}
		if !finalMatched {
			t.Fatalf("expected final failure audit metadata for j-audit")
		}
	})
}
