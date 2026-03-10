package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSiteStoreSessionCreateClose(t *testing.T) {
	root := t.TempDir()
	st, err := New(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	session := Session{ID: "session-1", ReleaseID: "release-1", StartedAt: "2026-03-08T10:05:00Z"}
	if err := st.CreateSession(session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	if err := st.SaveAssignment("session-1", Assignment{ID: "assign-1", NodeID: "node-1", Workflow: "workflows/apply.yaml"}); err != nil {
		t.Fatalf("save assignment: %v", err)
	}
	assignmentPath := filepath.Join(root, ".deck", "site", "sessions", "session-1", "assignments", "assign-1.json")
	if _, err := os.Stat(assignmentPath); err != nil {
		t.Fatalf("expected assignment at plan path: %v", err)
	}

	report := ExecutionReport{
		ID:          "report-1",
		SessionID:   "session-1",
		NodeID:      "node-1",
		Hostname:    "worker-01",
		Action:      "apply",
		WorkflowRef: "workflows/apply.yaml",
		Status:      "ok",
	}
	if err := st.SaveExecutionReport("session-1", report); err != nil {
		t.Fatalf("save execution report: %v", err)
	}
	reportPath := filepath.Join(root, ".deck", "site", "sessions", "session-1", "reports", "node-1", reportIdentityKey(report)+".json")
	if _, err := os.Stat(reportPath); err != nil {
		t.Fatalf("expected report at plan path: %v", err)
	}

	reports, err := st.ListExecutionReports("session-1", "node-1")
	if err != nil {
		t.Fatalf("list reports: %v", err)
	}
	if len(reports) != 1 || reports[0].NodeID != "node-1" || reports[0].Hostname != "worker-01" {
		t.Fatalf("unexpected reports: %#v", reports)
	}

	nodes, err := st.ListNodes("session-1")
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	if len(nodes) != 1 || nodes[0].ID != "node-1" {
		t.Fatalf("unexpected node list: %#v", nodes)
	}

	stored, found, err := st.GetSession("session-1")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if !found {
		t.Fatalf("expected session session-1")
	}
	if stored.Status != "open" {
		t.Fatalf("expected open session status, got %q", stored.Status)
	}

	closed, err := st.CloseSession("session-1", "2026-03-08T11:00:00Z")
	if err != nil {
		t.Fatalf("close session: %v", err)
	}
	if closed.Status != "closed" {
		t.Fatalf("expected closed status, got %q", closed.Status)
	}

	stored, found, err = st.GetSession("session-1")
	if err != nil {
		t.Fatalf("get closed session: %v", err)
	}
	if !found || stored.Status != "closed" || stored.ClosedAt == "" {
		t.Fatalf("unexpected closed session state: %#v found=%v", stored, found)
	}
}
