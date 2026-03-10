package store

import (
	"strings"
	"testing"
)

func TestAssignmentPrefersNodeOverride(t *testing.T) {
	st := newSessionStore(t, "session-assign-override")

	if err := st.SaveAssignment("session-assign-override", Assignment{
		ID:       "assign-role",
		Role:     "apply",
		Workflow: "workflows/role.yaml",
	}); err != nil {
		t.Fatalf("save role assignment: %v", err)
	}
	if err := st.SaveAssignment("session-assign-override", Assignment{
		ID:       "assign-node",
		NodeID:   "node-1",
		Role:     "apply",
		Workflow: "workflows/node.yaml",
	}); err != nil {
		t.Fatalf("save node assignment: %v", err)
	}

	assignment, err := st.ResolveAssignment("session-assign-override", "node-1", "apply")
	if err != nil {
		t.Fatalf("resolve assignment: %v", err)
	}
	if assignment.ID != "assign-node" {
		t.Fatalf("expected node override, got %#v", assignment)
	}
}

func TestAssignmentFallsBackToRole(t *testing.T) {
	st := newSessionStore(t, "session-assign-role")

	if err := st.SaveAssignment("session-assign-role", Assignment{
		ID:       "assign-role",
		Role:     "apply",
		Workflow: "workflows/apply.yaml",
	}); err != nil {
		t.Fatalf("save role assignment: %v", err)
	}

	assignment, err := st.ResolveAssignment("session-assign-role", "node-2", "apply")
	if err != nil {
		t.Fatalf("resolve assignment: %v", err)
	}
	if assignment.ID != "assign-role" {
		t.Fatalf("expected role assignment fallback, got %#v", assignment)
	}
}

func TestAssignmentMissingMatch(t *testing.T) {
	st := newSessionStore(t, "session-assign-missing")

	if err := st.SaveAssignment("session-assign-missing", Assignment{ID: "assign-pack", Role: "pack"}); err != nil {
		t.Fatalf("save assignment: %v", err)
	}

	_, err := st.ResolveAssignment("session-assign-missing", "node-3", "apply")
	if err == nil {
		t.Fatalf("expected missing assignment error")
	}
	if !strings.Contains(err.Error(), "no assignment matched") {
		t.Fatalf("expected explicit assignment miss error, got %v", err)
	}
}
