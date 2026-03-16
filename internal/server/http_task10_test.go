package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/taedi90/deck/internal/site/store"
)

func TestAssignmentAPI(t *testing.T) {
	root := t.TempDir()
	apiToken := "task10-token"
	seedTask10Store(t, root)

	h, err := NewHandler(root, HandlerOptions{AuthToken: apiToken})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/site/v1/sessions/sess-1/assignment?node_id=node-1&action=apply", nil)
	req.Header.Set("Authorization", "Bearer "+apiToken)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	assignment := store.Assignment{}
	if err := json.Unmarshal(rr.Body.Bytes(), &assignment); err != nil {
		t.Fatalf("unmarshal assignment response: %v", err)
	}
	if assignment.ID != "asg-apply" {
		t.Fatalf("unexpected assignment id: %q", assignment.ID)
	}
}

func TestSessionReportAPI(t *testing.T) {
	root := t.TempDir()
	apiToken := "task10-token"
	st := seedTask10Store(t, root)

	h, err := NewHandler(root, HandlerOptions{AuthToken: apiToken})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	body := []byte(`{"id":"rep-1","node_id":"node-1","hostname":"n1.local","action":"apply","workflow_ref":"workflows/apply.yaml","status":"ok","started_at":"2026-03-09T10:00:00Z","ended_at":"2026-03-09T10:01:00Z"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/site/v1/sessions/sess-1/reports", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+apiToken)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", rr.Code, rr.Body.String())
	}

	reports, err := st.ListExecutionReports("sess-1", "node-1")
	if err != nil {
		t.Fatalf("ListExecutionReports: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("expected 1 report, got %d", len(reports))
	}
	if reports[0].AssignmentID != "asg-apply" {
		t.Fatalf("expected assignment resolution to asg-apply, got %q", reports[0].AssignmentID)
	}
}

func TestSessionStatusAPI(t *testing.T) {
	root := t.TempDir()
	apiToken := "task10-token"
	st := seedTask10Store(t, root)

	report := store.ExecutionReport{ID: "rep-1", SessionID: "sess-1", NodeID: "node-1", Hostname: "n1.local", Action: "apply", WorkflowRef: "workflows/apply.yaml", Status: "ok", StartedAt: "2026-03-09T10:00:00Z", EndedAt: "2026-03-09T10:01:00Z"}
	if err := st.SaveExecutionReport("sess-1", report); err != nil {
		t.Fatalf("SaveExecutionReport: %v", err)
	}

	h, err := NewHandler(root, HandlerOptions{AuthToken: apiToken})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/site/v1/sessions/sess-1/status", nil)
	req.Header.Set("Authorization", "Bearer "+apiToken)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	status := sessionStatusResponse{}
	if err := json.Unmarshal(rr.Body.Bytes(), &status); err != nil {
		t.Fatalf("unmarshal status response: %v", err)
	}
	if status.Session.ID != "sess-1" {
		t.Fatalf("unexpected session id: %q", status.Session.ID)
	}
	node, ok := status.Status.Nodes["node-1"]
	if !ok {
		t.Fatalf("expected node-1 in status payload: %+v", status.Status.Nodes)
	}
	if node.Actions.Apply != "ok" {
		t.Fatalf("expected node-1 apply status ok, got %+v", node.Actions)
	}
}

func TestStatusUsesLatestReport(t *testing.T) {
	root := t.TempDir()
	apiToken := "task10-token"
	st := seedTask10Store(t, root)

	if err := st.SaveExecutionReport("sess-1", store.ExecutionReport{ID: "rep-new", SessionID: "sess-1", NodeID: "node-1", Action: "apply", WorkflowRef: "workflows/apply.yaml", Status: "ok", StartedAt: "2026-03-09T10:10:00Z", EndedAt: "2026-03-09T10:15:00Z"}); err != nil {
		t.Fatalf("SaveExecutionReport newest: %v", err)
	}
	if err := st.SaveExecutionReport("sess-1", store.ExecutionReport{ID: "rep-old", SessionID: "sess-1", NodeID: "node-1", Action: "apply", WorkflowRef: "workflows/apply.yaml", Status: "failed", StartedAt: "2026-03-09T10:00:00Z", EndedAt: "2026-03-09T10:05:00Z"}); err != nil {
		t.Fatalf("SaveExecutionReport older: %v", err)
	}

	h, err := NewHandler(root, HandlerOptions{AuthToken: apiToken})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/site/v1/sessions/sess-1/status", nil)
	req.Header.Set("Authorization", "Bearer "+apiToken)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	status := sessionStatusResponse{}
	if err := json.Unmarshal(rr.Body.Bytes(), &status); err != nil {
		t.Fatalf("unmarshal status response: %v", err)
	}
	if status.Status.Nodes["node-1"].Actions.Apply != "ok" {
		t.Fatalf("expected latest status ok, got %+v", status.Status.Nodes["node-1"].Actions)
	}
}

func TestSiteAPIRejectsMissingToken(t *testing.T) {
	root := t.TempDir()
	apiToken := "task10-token"
	seedTask10Store(t, root)

	h, err := NewHandler(root, HandlerOptions{AuthToken: apiToken})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	for _, tc := range []struct {
		method string
		path   string
		body   []byte
	}{
		{method: http.MethodGet, path: "/api/site/v1/releases/rel-1"},
		{method: http.MethodGet, path: "/api/site/v1/sessions/sess-1"},
		{method: http.MethodGet, path: "/api/site/v1/sessions/sess-1/assignment?node_id=node-1&action=apply"},
		{method: http.MethodPost, path: "/api/site/v1/sessions/sess-1/reports", body: []byte(`{"id":"rep-missing","node_id":"node-1","action":"apply"}`)},
		{method: http.MethodGet, path: "/api/site/v1/sessions/sess-1/status"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, bytes.NewReader(tc.body))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected %s %s to return 401 without token, got %d", tc.method, tc.path, rr.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/site/v1/releases/rel-1", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid token, got %d", rr.Code)
	}
}

func TestReleaseBundleReadOnly(t *testing.T) {
	root := t.TempDir()
	seedTask10Store(t, root)
	h, err := NewHandler(root, HandlerOptions{AuthToken: "task10-token"})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/site/releases/rel-1/bundle/files/payload.txt", nil)
	getRR := httptest.NewRecorder()
	h.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("expected release bundle GET 200, got %d", getRR.Code)
	}
	if getRR.Body.String() != "release-payload\n" {
		t.Fatalf("unexpected release bundle payload: %q", getRR.Body.String())
	}

	headReq := httptest.NewRequest(http.MethodHead, "/site/releases/rel-1/bundle/files/payload.txt", nil)
	headRR := httptest.NewRecorder()
	h.ServeHTTP(headRR, headReq)
	if headRR.Code != http.StatusOK {
		t.Fatalf("expected release bundle HEAD 200, got %d", headRR.Code)
	}
	if headRR.Body.Len() != 0 {
		t.Fatalf("expected empty HEAD body")
	}

	putReq := httptest.NewRequest(http.MethodPut, "/site/releases/rel-1/bundle/files/payload.txt", bytes.NewReader([]byte("mutate")))
	putRR := httptest.NewRecorder()
	h.ServeHTTP(putRR, putReq)
	if putRR.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected release bundle PUT 405, got %d", putRR.Code)
	}
}

func seedTask10Store(t *testing.T, root string) *store.Store {
	t.Helper()
	st, err := store.New(root)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	bundleRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(bundleRoot, "files"), 0o755); err != nil {
		t.Fatalf("mkdir bundle files: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleRoot, "files", "payload.txt"), []byte("release-payload\n"), 0o644); err != nil {
		t.Fatalf("write release payload: %v", err)
	}
	if err := st.ImportRelease(store.Release{ID: "rel-1", BundleSHA256: "sha256:test", CreatedAt: "2026-03-09T00:00:00Z"}, bundleRoot); err != nil {
		t.Fatalf("ImportRelease: %v", err)
	}
	if err := st.CreateSession(store.Session{ID: "sess-1", ReleaseID: "rel-1", Status: "open", StartedAt: "2026-03-09T10:00:00Z"}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := st.SaveAssignment("sess-1", store.Assignment{ID: "asg-apply", SessionID: "sess-1", NodeID: "node-1", Role: "apply", Workflow: "workflows/apply.yaml", Status: "ready"}); err != nil {
		t.Fatalf("SaveAssignment node apply: %v", err)
	}
	if err := st.SaveAssignment("sess-1", store.Assignment{ID: "asg-diff", SessionID: "sess-1", NodeID: "node-1", Role: "diff", Workflow: "workflows/diff.yaml", Status: "ready"}); err != nil {
		t.Fatalf("SaveAssignment node diff: %v", err)
	}
	if err := st.SaveAssignment("sess-1", store.Assignment{ID: "asg-doctor", SessionID: "sess-1", NodeID: "node-1", Role: "doctor", Workflow: "workflows/doctor.yaml", Status: "ready"}); err != nil {
		t.Fatalf("SaveAssignment node doctor: %v", err)
	}
	return st
}
