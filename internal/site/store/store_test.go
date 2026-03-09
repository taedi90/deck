package store

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSiteStoreReleaseImport(t *testing.T) {
	root := t.TempDir()
	importedBundle := filepath.Join(t.TempDir(), "bundle-src")
	if err := os.MkdirAll(filepath.Join(importedBundle, "workflows"), 0o755); err != nil {
		t.Fatalf("create imported bundle dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(importedBundle, "workflows", "apply.yaml"), []byte("role: apply\n"), 0o644); err != nil {
		t.Fatalf("write imported bundle file: %v", err)
	}

	st, err := New(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	release := Release{ID: "release-20260308", BundleSHA256: "sha256-abc", CreatedAt: "2026-03-08T10:00:00Z"}
	if err := st.ImportRelease(release, importedBundle); err != nil {
		t.Fatalf("import release: %v", err)
	}

	manifestPath := filepath.Join(root, ".deck", "site", "releases", release.ID, "manifest.json")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("expected release manifest at plan path: %v", err)
	}
	bundleFilePath := filepath.Join(root, ".deck", "site", "releases", release.ID, "bundle", "workflows", "apply.yaml")
	if _, err := os.Stat(bundleFilePath); err != nil {
		t.Fatalf("expected imported immutable bundle content at plan path: %v", err)
	}

	got, found, err := st.GetRelease(release.ID)
	if err != nil {
		t.Fatalf("get release: %v", err)
	}
	if !found {
		t.Fatalf("expected release %q", release.ID)
	}
	if got.ID != release.ID || got.BundleSHA256 != release.BundleSHA256 {
		t.Fatalf("unexpected release: %#v", got)
	}

	list, err := st.ListReleases()
	if err != nil {
		t.Fatalf("list releases: %v", err)
	}
	if len(list) != 1 || list[0].ID != release.ID {
		t.Fatalf("unexpected release list: %#v", list)
	}

	if err := st.ImportRelease(release, importedBundle); err == nil {
		t.Fatalf("expected second import to fail for immutable imported release")
	}
}

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

func TestSiteStoreSeparateFromInstallState(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	st, err := New(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	bundleSrc := filepath.Join(t.TempDir(), "bundle-src")
	if err := os.MkdirAll(bundleSrc, 0o755); err != nil {
		t.Fatalf("mkdir bundle source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleSrc, "bundle.tar.note"), []byte("placeholder"), 0o644); err != nil {
		t.Fatalf("write bundle source file: %v", err)
	}

	if err := st.ImportRelease(Release{ID: "release-separate"}, bundleSrc); err != nil {
		t.Fatalf("import release: %v", err)
	}
	if err := st.CreateSession(Session{ID: "session-separate"}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := st.SaveAssignment("session-separate", Assignment{ID: "assign-separate", NodeID: "node-separate"}); err != nil {
		t.Fatalf("save assignment: %v", err)
	}
	if err := st.SaveExecutionReport("session-separate", ExecutionReport{ID: "report-separate", NodeID: "node-separate"}); err != nil {
		t.Fatalf("save report: %v", err)
	}

	sitePath := filepath.Join(root, ".deck", "site")
	if _, err := os.Stat(sitePath); err != nil {
		t.Fatalf("expected site metadata under .deck/site: %v", err)
	}

	installStatePath := filepath.Join(home, ".deck", "state")
	if _, err := os.Stat(installStatePath); !os.IsNotExist(err) {
		t.Fatalf("expected no writes to local install-state path %q, got err=%v", installStatePath, err)
	}
}

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

func newSessionStore(t *testing.T, sessionID string) *Store {
	t.Helper()
	root := t.TempDir()
	st, err := New(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := st.CreateSession(Session{ID: sessionID}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	return st
}
