package store

import (
	"reflect"
	"testing"
)

func TestSessionStatusAggregation(t *testing.T) {
	st := newSessionStore(t, "session-status-aggregation")
	for _, assignment := range []Assignment{
		{ID: "asg-node1-diff", NodeID: "node-1", Role: "diff", Workflow: "workflows/diff.yaml"},
		{ID: "asg-node1-doctor", NodeID: "node-1", Role: "doctor", Workflow: "workflows/doctor.yaml"},
		{ID: "asg-node1-apply", NodeID: "node-1", Role: "apply", Workflow: "workflows/apply.yaml"},
		{ID: "asg-node2-apply", NodeID: "node-2", Role: "apply", Workflow: "workflows/apply.yaml"},
	} {
		if err := st.SaveAssignment("session-status-aggregation", assignment); err != nil {
			t.Fatalf("save assignment %s: %v", assignment.ID, err)
		}
	}
	for _, report := range []ExecutionReport{
		{ID: "rep-node1-diff", NodeID: "node-1", Hostname: "n1.local", Action: "diff", WorkflowRef: "workflows/diff.yaml", Status: "ok", EndedAt: "2026-03-09T10:01:00Z"},
		{ID: "rep-node1-doctor", NodeID: "node-1", Action: "doctor", WorkflowRef: "workflows/doctor.yaml", Status: "failed", EndedAt: "2026-03-09T10:02:00Z"},
		{ID: "rep-node1-apply", NodeID: "node-1", Action: "apply", WorkflowRef: "workflows/apply.yaml", Status: "skipped", EndedAt: "2026-03-09T10:03:00Z"},
	} {
		if err := st.SaveExecutionReport("session-status-aggregation", report); err != nil {
			t.Fatalf("save report %s: %v", report.ID, err)
		}
	}

	aggregated, err := st.SessionStatusAggregation("session-status-aggregation")
	if err != nil {
		t.Fatalf("SessionStatusAggregation: %v", err)
	}

	node1, ok := aggregated.Nodes["node-1"]
	if !ok {
		t.Fatalf("expected node-1 in aggregated status")
	}
	if node1.Actions.Diff != "ok" || node1.Actions.Doctor != "failed" || node1.Actions.Apply != "skipped" {
		t.Fatalf("unexpected node-1 action status: %#v", node1.Actions)
	}
	node2, ok := aggregated.Nodes["node-2"]
	if !ok {
		t.Fatalf("expected node-2 in aggregated status")
	}
	if node2.Actions.Apply != "not-run" {
		t.Fatalf("expected node-2 apply not-run, got %#v", node2.Actions)
	}

	if !reflect.DeepEqual(aggregated.Groups.Diff.OK, []string{"node-1"}) {
		t.Fatalf("unexpected diff ok group: %#v", aggregated.Groups.Diff.OK)
	}
	if !reflect.DeepEqual(aggregated.Groups.Doctor.Failed, []string{"node-1"}) {
		t.Fatalf("unexpected doctor failed group: %#v", aggregated.Groups.Doctor.Failed)
	}
	if !reflect.DeepEqual(aggregated.Groups.Apply.Skipped, []string{"node-1"}) {
		t.Fatalf("unexpected apply skipped group: %#v", aggregated.Groups.Apply.Skipped)
	}
	if !reflect.DeepEqual(aggregated.Groups.Apply.NotRun, []string{"node-2"}) {
		t.Fatalf("unexpected apply not-run group: %#v", aggregated.Groups.Apply.NotRun)
	}
}

func TestStatusShowsNotRunNodes(t *testing.T) {
	st := newSessionStore(t, "session-status-not-run")
	if err := st.SaveAssignment("session-status-not-run", Assignment{ID: "asg-node-1-apply", NodeID: "node-1", Role: "apply", Workflow: "workflows/apply.yaml"}); err != nil {
		t.Fatalf("save assignment: %v", err)
	}

	aggregated, err := st.SessionStatusAggregation("session-status-not-run")
	if err != nil {
		t.Fatalf("SessionStatusAggregation: %v", err)
	}

	node, ok := aggregated.Nodes["node-1"]
	if !ok {
		t.Fatalf("expected node-1 in aggregated status")
	}
	if node.Actions.Apply != "not-run" {
		t.Fatalf("expected apply status not-run, got %q", node.Actions.Apply)
	}
	if !reflect.DeepEqual(aggregated.Groups.Apply.NotRun, []string{"node-1"}) {
		t.Fatalf("unexpected apply not-run group: %#v", aggregated.Groups.Apply.NotRun)
	}
}

func TestStatusIgnoresSupersededReports(t *testing.T) {
	st := newSessionStore(t, "session-status-superseded")
	if err := st.SaveAssignment("session-status-superseded", Assignment{ID: "asg-node-1-apply", NodeID: "node-1", Role: "apply", Workflow: "workflows/apply.yaml"}); err != nil {
		t.Fatalf("save assignment: %v", err)
	}
	if err := st.SaveExecutionReport("session-status-superseded", ExecutionReport{
		ID:          "rep-new",
		NodeID:      "node-1",
		Action:      "apply",
		WorkflowRef: "workflows/apply.yaml",
		Status:      "ok",
		StartedAt:   "2026-03-09T11:00:00Z",
		EndedAt:     "2026-03-09T11:05:00Z",
	}); err != nil {
		t.Fatalf("save newest report: %v", err)
	}
	if err := st.SaveExecutionReport("session-status-superseded", ExecutionReport{
		ID:          "rep-old",
		NodeID:      "node-1",
		Action:      "apply",
		WorkflowRef: "workflows/apply.yaml",
		Status:      "failed",
		StartedAt:   "2026-03-09T10:00:00Z",
		EndedAt:     "2026-03-09T10:05:00Z",
	}); err != nil {
		t.Fatalf("save older report: %v", err)
	}

	aggregated, err := st.SessionStatusAggregation("session-status-superseded")
	if err != nil {
		t.Fatalf("SessionStatusAggregation: %v", err)
	}
	if aggregated.Nodes["node-1"].Actions.Apply != "ok" {
		t.Fatalf("expected latest apply status to remain ok, got %#v", aggregated.Nodes["node-1"].Actions)
	}
}
