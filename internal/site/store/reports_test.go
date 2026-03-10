package store

import (
	"strings"
	"testing"
)

func TestReportLatestAggregation(t *testing.T) {
	st := newSessionStore(t, "session-report-latest")
	if err := st.SaveAssignment("session-report-latest", Assignment{
		ID:       "assign-node",
		NodeID:   "node-1",
		Role:     "apply",
		Workflow: "workflows/apply.yaml",
	}); err != nil {
		t.Fatalf("save assignment: %v", err)
	}

	if err := st.SaveExecutionReport("session-report-latest", ExecutionReport{
		ID:          "report-001",
		NodeID:      "node-1",
		Action:      "apply",
		WorkflowRef: "workflows/apply.yaml",
		StartedAt:   "2026-03-08T10:00:00Z",
		EndedAt:     "2026-03-08T10:05:00Z",
	}); err != nil {
		t.Fatalf("save initial report: %v", err)
	}
	if err := st.SaveExecutionReport("session-report-latest", ExecutionReport{
		ID:          "report-002",
		NodeID:      "node-1",
		Action:      "apply",
		WorkflowRef: "workflows/apply.yaml",
		StartedAt:   "2026-03-08T10:10:00Z",
		EndedAt:     "2026-03-08T10:15:00Z",
	}); err != nil {
		t.Fatalf("save newer report: %v", err)
	}

	reports, err := st.ListExecutionReports("session-report-latest", "node-1")
	if err != nil {
		t.Fatalf("list reports: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("expected one latest report, got %#v", reports)
	}
	if reports[0].ID != "report-002" {
		t.Fatalf("expected latest report-002, got %#v", reports[0])
	}
}

func TestDuplicateReportHandling(t *testing.T) {
	st := newSessionStore(t, "session-report-duplicate")
	if err := st.SaveAssignment("session-report-duplicate", Assignment{
		ID:       "assign-node",
		NodeID:   "node-1",
		Role:     "apply",
		Workflow: "workflows/apply.yaml",
	}); err != nil {
		t.Fatalf("save assignment: %v", err)
	}

	if err := st.SaveExecutionReport("session-report-duplicate", ExecutionReport{
		ID:          "report-200",
		NodeID:      "node-1",
		Action:      "apply",
		WorkflowRef: "workflows/apply.yaml",
		StartedAt:   "2026-03-08T11:10:00Z",
		EndedAt:     "2026-03-08T11:15:00Z",
	}); err != nil {
		t.Fatalf("save newer report: %v", err)
	}
	if err := st.SaveExecutionReport("session-report-duplicate", ExecutionReport{
		ID:          "report-100",
		NodeID:      "node-1",
		Action:      "apply",
		WorkflowRef: "workflows/apply.yaml",
		StartedAt:   "2026-03-08T11:00:00Z",
		EndedAt:     "2026-03-08T11:05:00Z",
	}); err != nil {
		t.Fatalf("save older duplicate report: %v", err)
	}

	reports, err := st.ListExecutionReports("session-report-duplicate", "node-1")
	if err != nil {
		t.Fatalf("list reports: %v", err)
	}
	if len(reports) != 1 || reports[0].ID != "report-200" {
		t.Fatalf("expected deterministic latest duplicate handling, got %#v", reports)
	}
}

func TestReportRejectsClosedSession(t *testing.T) {
	st := newSessionStore(t, "session-report-closed")
	if err := st.SaveAssignment("session-report-closed", Assignment{ID: "assign-node", NodeID: "node-1", Role: "apply"}); err != nil {
		t.Fatalf("save assignment: %v", err)
	}
	if _, err := st.CloseSession("session-report-closed", "2026-03-08T12:00:00Z"); err != nil {
		t.Fatalf("close session: %v", err)
	}

	err := st.SaveExecutionReport("session-report-closed", ExecutionReport{ID: "report-1", NodeID: "node-1", Action: "apply"})
	if err == nil {
		t.Fatalf("expected closed-session rejection")
	}
	if !strings.Contains(err.Error(), "is closed") {
		t.Fatalf("expected explicit closed-session error, got %v", err)
	}

	err = st.SaveExecutionReport("session-does-not-exist", ExecutionReport{ID: "report-2", NodeID: "node-1", Action: "apply"})
	if err == nil {
		t.Fatalf("expected unknown-session rejection")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected explicit unknown-session error, got %v", err)
	}
}

func TestReportRejectsInvalidNodeID(t *testing.T) {
	st := newSessionStore(t, "session-report-invalid-node")
	if err := st.SaveAssignment("session-report-invalid-node", Assignment{ID: "assign-role", Role: "apply"}); err != nil {
		t.Fatalf("save role assignment: %v", err)
	}

	err := st.SaveExecutionReport("session-report-invalid-node", ExecutionReport{ID: "report-1", NodeID: "Node_1", Action: "apply"})
	if err == nil {
		t.Fatalf("expected invalid node id rejection")
	}
	if !strings.Contains(err.Error(), "report node_id") {
		t.Fatalf("expected explicit invalid node id error, got %v", err)
	}
}

func TestReportRejectsAssignmentMismatch(t *testing.T) {
	st := newSessionStore(t, "session-report-mismatch")
	if err := st.SaveAssignment("session-report-mismatch", Assignment{
		ID:       "assign-node",
		NodeID:   "node-1",
		Role:     "apply",
		Workflow: "workflows/apply.yaml",
	}); err != nil {
		t.Fatalf("save assignment: %v", err)
	}

	err := st.SaveExecutionReport("session-report-mismatch", ExecutionReport{
		ID:          "report-1",
		NodeID:      "node-1",
		Action:      "apply",
		WorkflowRef: "workflows/other.yaml",
	})
	if err == nil {
		t.Fatalf("expected assignment mismatch rejection")
	}
	if !strings.Contains(err.Error(), "assignment mismatch") {
		t.Fatalf("expected explicit mismatch error, got %v", err)
	}
}
