package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	recordIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	nodeIDPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
)

type Store struct {
	root string
}

type Release struct {
	ID           string `json:"id"`
	BundleSHA256 string `json:"bundle_sha256,omitempty"`
	CreatedAt    string `json:"created_at,omitempty"`
}

type Session struct {
	ID        string `json:"id"`
	ReleaseID string `json:"release_id,omitempty"`
	Status    string `json:"status"`
	StartedAt string `json:"started_at,omitempty"`
	ClosedAt  string `json:"closed_at,omitempty"`
}

type Node struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname,omitempty"`
}

type Assignment struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	NodeID    string `json:"node_id"`
	Role      string `json:"role,omitempty"`
	Workflow  string `json:"workflow,omitempty"`
	Status    string `json:"status,omitempty"`
}

type ExecutionReport struct {
	ID           string `json:"id"`
	SessionID    string `json:"session_id"`
	AssignmentID string `json:"assignment_id,omitempty"`
	NodeID       string `json:"node_id"`
	Hostname     string `json:"hostname,omitempty"`
	Action       string `json:"action,omitempty"`
	WorkflowRef  string `json:"workflow_ref,omitempty"`
	Status       string `json:"status,omitempty"`
	StartedAt    string `json:"started_at,omitempty"`
	EndedAt      string `json:"ended_at,omitempty"`
}

type SessionNodeActionStatus struct {
	Diff   string `json:"diff"`
	Doctor string `json:"doctor"`
	Apply  string `json:"apply"`
}

type SessionStatusNode struct {
	NodeID   string                  `json:"node_id"`
	Hostname string                  `json:"hostname,omitempty"`
	Actions  SessionNodeActionStatus `json:"actions"`
}

type SessionStatusBucket struct {
	OK      []string `json:"ok"`
	Failed  []string `json:"failed"`
	Skipped []string `json:"skipped"`
	NotRun  []string `json:"not_run"`
}

type SessionStatusGroups struct {
	Diff   SessionStatusBucket `json:"diff"`
	Doctor SessionStatusBucket `json:"doctor"`
	Apply  SessionStatusBucket `json:"apply"`
}

type SessionStatusAggregation struct {
	Nodes  map[string]SessionStatusNode `json:"nodes"`
	Groups SessionStatusGroups          `json:"groups"`
}

func New(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("store root is empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve store root: %w", err)
	}
	return &Store{root: abs}, nil
}

func (s *Store) ImportRelease(release Release, importedBundlePath string) error {
	if err := validateRecordID(release.ID, "release id"); err != nil {
		return err
	}
	if strings.TrimSpace(importedBundlePath) == "" {
		return fmt.Errorf("imported bundle path is empty")
	}
	stat, err := os.Stat(importedBundlePath)
	if err != nil {
		return fmt.Errorf("stat imported bundle path: %w", err)
	}
	if !stat.IsDir() {
		return fmt.Errorf("imported bundle path must be a directory")
	}

	releaseDir := s.releaseDir(release.ID)
	manifestPath := filepath.Join(releaseDir, "manifest.json")
	bundlePath := filepath.Join(releaseDir, "bundle")

	if _, err := os.Stat(manifestPath); err == nil {
		return fmt.Errorf("release %q already imported", release.ID)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check release manifest: %w", err)
	}
	if _, err := os.Stat(bundlePath); err == nil {
		return fmt.Errorf("release %q bundle already imported", release.ID)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check release bundle path: %w", err)
	}

	if err := os.MkdirAll(releaseDir, 0o755); err != nil {
		return fmt.Errorf("create release directory: %w", err)
	}
	if err := copyDir(importedBundlePath, bundlePath); err != nil {
		return fmt.Errorf("copy release bundle: %w", err)
	}
	if err := writeAtomicJSON(manifestPath, release); err != nil {
		return fmt.Errorf("write release manifest: %w", err)
	}
	return nil
}

func (s *Store) GetRelease(releaseID string) (Release, bool, error) {
	if err := validateRecordID(releaseID, "release id"); err != nil {
		return Release{}, false, err
	}
	return readJSON[Release](filepath.Join(s.releaseDir(releaseID), "manifest.json"))
}

func (s *Store) ListReleases() ([]Release, error) {
	entries, err := os.ReadDir(s.releasesDir())
	if err != nil {
		if os.IsNotExist(err) {
			return []Release{}, nil
		}
		return nil, fmt.Errorf("read releases directory: %w", err)
	}

	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			ids = append(ids, entry.Name())
		}
	}
	sort.Strings(ids)

	out := make([]Release, 0, len(ids))
	for _, id := range ids {
		release, found, err := s.GetRelease(id)
		if err != nil {
			return nil, err
		}
		if found {
			out = append(out, release)
		}
	}
	return out, nil
}

func (s *Store) CreateSession(session Session) error {
	if err := validateRecordID(session.ID, "session id"); err != nil {
		return err
	}
	if session.ReleaseID != "" {
		if err := validateRecordID(session.ReleaseID, "session release_id"); err != nil {
			return err
		}
	}
	if strings.TrimSpace(session.Status) == "" {
		session.Status = "open"
	}

	path := filepath.Join(s.sessionDir(session.ID), "session.json")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("session %q already exists", session.ID)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check session file: %w", err)
	}
	return writeAtomicJSON(path, session)
}

func (s *Store) GetSession(sessionID string) (Session, bool, error) {
	if err := validateRecordID(sessionID, "session id"); err != nil {
		return Session{}, false, err
	}
	return readJSON[Session](filepath.Join(s.sessionDir(sessionID), "session.json"))
}

func (s *Store) ListSessions() ([]Session, error) {
	entries, err := os.ReadDir(filepath.Join(s.siteDir(), "sessions"))
	if err != nil {
		if os.IsNotExist(err) {
			return []Session{}, nil
		}
		return nil, fmt.Errorf("read sessions directory: %w", err)
	}

	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			ids = append(ids, entry.Name())
		}
	}
	sort.Strings(ids)

	out := make([]Session, 0, len(ids))
	for _, id := range ids {
		session, found, err := s.GetSession(id)
		if err != nil {
			return nil, err
		}
		if found {
			out = append(out, session)
		}
	}
	return out, nil
}

func (s *Store) CloseSession(sessionID, closedAt string) (Session, error) {
	session, found, err := s.GetSession(sessionID)
	if err != nil {
		return Session{}, err
	}
	if !found {
		return Session{}, fmt.Errorf("session %q not found", sessionID)
	}
	session.Status = "closed"
	session.ClosedAt = strings.TrimSpace(closedAt)

	path := filepath.Join(s.sessionDir(sessionID), "session.json")
	if err := writeAtomicJSON(path, session); err != nil {
		return Session{}, err
	}
	return session, nil
}

func (s *Store) SaveAssignment(sessionID string, assignment Assignment) error {
	if err := validateRecordID(sessionID, "session id"); err != nil {
		return err
	}
	if err := validateRecordID(assignment.ID, "assignment id"); err != nil {
		return err
	}
	assignment.NodeID = strings.TrimSpace(assignment.NodeID)
	assignment.Role = strings.TrimSpace(assignment.Role)
	if assignment.NodeID == "" && assignment.Role == "" {
		return fmt.Errorf("assignment must include node_id or role")
	}
	if assignment.NodeID != "" {
		if err := validateNodeID(assignment.NodeID, "assignment node_id"); err != nil {
			return err
		}
	}
	if assignment.SessionID == "" {
		assignment.SessionID = sessionID
	}
	if assignment.SessionID != sessionID {
		return fmt.Errorf("assignment session_id must match %q", sessionID)
	}

	path := filepath.Join(s.sessionDir(sessionID), "assignments", assignment.ID+".json")
	return writeAtomicJSON(path, assignment)
}

func (s *Store) ResolveAssignment(sessionID, nodeID, role string) (Assignment, error) {
	if err := validateRecordID(sessionID, "session id"); err != nil {
		return Assignment{}, err
	}
	if err := validateNodeID(nodeID, "node id"); err != nil {
		return Assignment{}, err
	}
	role = strings.TrimSpace(role)

	if _, err := s.requireOpenSession(sessionID); err != nil {
		return Assignment{}, err
	}

	assignments, err := s.listAssignments(sessionID)
	if err != nil {
		return Assignment{}, err
	}

	nodeMatches := make([]Assignment, 0)
	for _, assignment := range assignments {
		if assignment.NodeID == nodeID {
			nodeMatches = append(nodeMatches, assignment)
		}
	}
	if resolved, ok := chooseAssignment(nodeMatches, role); ok {
		return resolved, nil
	}

	roleMatches := make([]Assignment, 0)
	for _, assignment := range assignments {
		if assignment.NodeID == "" && assignment.Role == role {
			roleMatches = append(roleMatches, assignment)
		}
	}
	if resolved, ok := chooseAssignment(roleMatches, role); ok {
		return resolved, nil
	}

	if role == "" {
		return Assignment{}, fmt.Errorf("no assignment matched session %q node_id %q", sessionID, nodeID)
	}
	return Assignment{}, fmt.Errorf("no assignment matched session %q node_id %q role %q", sessionID, nodeID, role)
}

func (s *Store) SaveExecutionReport(sessionID string, report ExecutionReport) error {
	if err := validateRecordID(sessionID, "session id"); err != nil {
		return err
	}
	if err := validateRecordID(report.ID, "report id"); err != nil {
		return err
	}
	if err := validateNodeID(report.NodeID, "report node_id"); err != nil {
		return err
	}
	if report.SessionID == "" {
		report.SessionID = sessionID
	}
	if report.SessionID != sessionID {
		return fmt.Errorf("report session_id must match %q", sessionID)
	}
	if _, err := s.requireOpenSession(sessionID); err != nil {
		return err
	}

	resolved, err := s.ResolveAssignment(sessionID, report.NodeID, report.Action)
	if err != nil {
		return err
	}
	if report.AssignmentID != "" && report.AssignmentID != resolved.ID {
		return fmt.Errorf("report assignment mismatch: node_id %q action %q expected assignment_id %q but got %q", report.NodeID, report.Action, resolved.ID, report.AssignmentID)
	}
	if resolved.Workflow != "" && report.WorkflowRef != "" && resolved.Workflow != report.WorkflowRef {
		return fmt.Errorf("report assignment mismatch: node_id %q action %q expected workflow_ref %q but got %q", report.NodeID, report.Action, resolved.Workflow, report.WorkflowRef)
	}
	if report.AssignmentID == "" {
		report.AssignmentID = resolved.ID
	}

	identity := reportIdentityKey(report)
	path := filepath.Join(s.sessionDir(sessionID), "reports", report.NodeID, identity+".json")
	existing, found, err := readJSON[ExecutionReport](path)
	if err != nil {
		return err
	}
	if found && !shouldReplaceReport(existing, report) {
		return nil
	}
	return writeAtomicJSON(path, report)
}

func (s *Store) ListExecutionReports(sessionID, nodeID string) ([]ExecutionReport, error) {
	if err := validateRecordID(sessionID, "session id"); err != nil {
		return nil, err
	}
	if err := validateNodeID(nodeID, "node id"); err != nil {
		return nil, err
	}

	reportsDir := filepath.Join(s.sessionDir(sessionID), "reports", nodeID)
	files, err := listJSONFiles(reportsDir)
	if err != nil {
		return nil, err
	}

	out := make([]ExecutionReport, 0, len(files))
	byIdentity := map[string]ExecutionReport{}
	for _, name := range files {
		report, found, err := readJSON[ExecutionReport](filepath.Join(reportsDir, name))
		if err != nil {
			return nil, err
		}
		if found {
			identity := reportIdentityKey(report)
			existing, ok := byIdentity[identity]
			if !ok || shouldReplaceReport(existing, report) {
				byIdentity[identity] = report
			}
		}
	}
	keys := make([]string, 0, len(byIdentity))
	for key := range byIdentity {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out = make([]ExecutionReport, 0, len(keys))
	for _, key := range keys {
		out = append(out, byIdentity[key])
	}
	return out, nil
}

func (s *Store) requireOpenSession(sessionID string) (Session, error) {
	session, found, err := s.GetSession(sessionID)
	if err != nil {
		return Session{}, err
	}
	if !found {
		return Session{}, fmt.Errorf("session %q not found", sessionID)
	}
	if strings.EqualFold(strings.TrimSpace(session.Status), "closed") {
		return Session{}, fmt.Errorf("session %q is closed", sessionID)
	}
	return session, nil
}

func (s *Store) listAssignments(sessionID string) ([]Assignment, error) {
	assignmentsDir := filepath.Join(s.sessionDir(sessionID), "assignments")
	files, err := listJSONFiles(assignmentsDir)
	if err != nil {
		return nil, err
	}
	assignments := make([]Assignment, 0, len(files))
	for _, name := range files {
		assignment, found, err := readJSON[Assignment](filepath.Join(assignmentsDir, name))
		if err != nil {
			return nil, err
		}
		if found {
			assignments = append(assignments, assignment)
		}
	}
	return assignments, nil
}

func (s *Store) ListAssignments(sessionID string) ([]Assignment, error) {
	if err := validateRecordID(sessionID, "session id"); err != nil {
		return nil, err
	}
	return s.listAssignments(sessionID)
}

func chooseAssignment(candidates []Assignment, role string) (Assignment, bool) {
	if len(candidates) == 0 {
		return Assignment{}, false
	}
	sorted := make([]Assignment, len(candidates))
	copy(sorted, candidates)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ID < sorted[j].ID
	})
	if role != "" {
		for _, candidate := range sorted {
			if candidate.Role == role {
				return candidate, true
			}
		}
	}
	for _, candidate := range sorted {
		if candidate.Role == "" {
			return candidate, true
		}
	}
	if role == "" {
		return sorted[0], true
	}
	return Assignment{}, false
}

func reportIdentityKey(report ExecutionReport) string {
	raw := strings.Join([]string{
		strings.TrimSpace(report.SessionID),
		strings.TrimSpace(report.NodeID),
		strings.TrimSpace(report.Action),
		strings.TrimSpace(report.WorkflowRef),
	}, "\x1f")
	sum := sha256.Sum256([]byte(raw))
	return "report-" + hex.EncodeToString(sum[:12])
}

func shouldReplaceReport(existing, incoming ExecutionReport) bool {
	if isAfter(incoming.EndedAt, existing.EndedAt) {
		return true
	}
	if isAfter(existing.EndedAt, incoming.EndedAt) {
		return false
	}
	if isAfter(incoming.StartedAt, existing.StartedAt) {
		return true
	}
	if isAfter(existing.StartedAt, incoming.StartedAt) {
		return false
	}
	return incoming.ID > existing.ID
}

func isAfter(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == right {
		return false
	}
	if left == "" {
		return false
	}
	if right == "" {
		return true
	}
	return left > right
}

func (s *Store) ListNodes(sessionID string) ([]Node, error) {
	if err := validateRecordID(sessionID, "session id"); err != nil {
		return nil, err
	}
	seen := map[string]Node{}

	assignmentsDir := filepath.Join(s.sessionDir(sessionID), "assignments")
	assignmentFiles, err := listJSONFiles(assignmentsDir)
	if err != nil {
		return nil, err
	}
	for _, name := range assignmentFiles {
		assignment, found, err := readJSON[Assignment](filepath.Join(assignmentsDir, name))
		if err != nil {
			return nil, err
		}
		if found {
			if strings.TrimSpace(assignment.NodeID) == "" {
				continue
			}
			seen[assignment.NodeID] = Node{ID: assignment.NodeID}
		}
	}

	reportsRoot := filepath.Join(s.sessionDir(sessionID), "reports")
	reportNodeDirs, err := os.ReadDir(reportsRoot)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read session reports directory: %w", err)
	}
	for _, entry := range reportNodeDirs {
		if !entry.IsDir() {
			continue
		}
		nodeID := entry.Name()
		if err := validateNodeID(nodeID, "report node directory"); err != nil {
			continue
		}
		reports, err := s.ListExecutionReports(sessionID, nodeID)
		if err != nil {
			return nil, err
		}
		node := Node{ID: nodeID}
		for _, report := range reports {
			if strings.TrimSpace(report.Hostname) != "" {
				node.Hostname = report.Hostname
				break
			}
		}
		if existing, ok := seen[nodeID]; ok && existing.Hostname != "" {
			node.Hostname = existing.Hostname
		}
		seen[nodeID] = node
	}

	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Node, 0, len(ids))
	for _, id := range ids {
		out = append(out, seen[id])
	}
	return out, nil
}

func (s *Store) SessionStatusAggregation(sessionID string) (SessionStatusAggregation, error) {
	if err := validateRecordID(sessionID, "session id"); err != nil {
		return SessionStatusAggregation{}, err
	}

	nodes, err := s.ListNodes(sessionID)
	if err != nil {
		return SessionStatusAggregation{}, err
	}

	aggregation := SessionStatusAggregation{
		Nodes: make(map[string]SessionStatusNode, len(nodes)),
		Groups: SessionStatusGroups{
			Diff:   emptySessionStatusBucket(),
			Doctor: emptySessionStatusBucket(),
			Apply:  emptySessionStatusBucket(),
		},
	}

	for _, node := range nodes {
		aggregation.Nodes[node.ID] = SessionStatusNode{
			NodeID:   node.ID,
			Hostname: node.Hostname,
			Actions: SessionNodeActionStatus{
				Diff:   "not-run",
				Doctor: "not-run",
				Apply:  "not-run",
			},
		}
	}

	for _, node := range nodes {
		reports, err := s.ListExecutionReports(sessionID, node.ID)
		if err != nil {
			return SessionStatusAggregation{}, err
		}
		current := aggregation.Nodes[node.ID]
		for _, report := range reports {
			switch strings.TrimSpace(report.Action) {
			case "diff":
				current.Actions.Diff = normalizeSessionStatus(report.Status)
			case "doctor":
				current.Actions.Doctor = normalizeSessionStatus(report.Status)
			case "apply":
				current.Actions.Apply = normalizeSessionStatus(report.Status)
			}
		}
		aggregation.Nodes[node.ID] = current
	}

	nodeIDs := make([]string, 0, len(aggregation.Nodes))
	for nodeID := range aggregation.Nodes {
		nodeIDs = append(nodeIDs, nodeID)
	}
	sort.Strings(nodeIDs)

	for _, nodeID := range nodeIDs {
		node := aggregation.Nodes[nodeID]
		appendNodeToStatusBucket(&aggregation.Groups.Diff, nodeID, node.Actions.Diff)
		appendNodeToStatusBucket(&aggregation.Groups.Doctor, nodeID, node.Actions.Doctor)
		appendNodeToStatusBucket(&aggregation.Groups.Apply, nodeID, node.Actions.Apply)
	}

	return aggregation, nil
}

func emptySessionStatusBucket() SessionStatusBucket {
	return SessionStatusBucket{OK: []string{}, Failed: []string{}, Skipped: []string{}, NotRun: []string{}}
}

func normalizeSessionStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "ok":
		return "ok"
	case "failed":
		return "failed"
	case "skipped":
		return "skipped"
	case "not-run":
		return "not-run"
	default:
		return "failed"
	}
}

func appendNodeToStatusBucket(bucket *SessionStatusBucket, nodeID, status string) {
	switch status {
	case "ok":
		bucket.OK = append(bucket.OK, nodeID)
	case "failed":
		bucket.Failed = append(bucket.Failed, nodeID)
	case "skipped":
		bucket.Skipped = append(bucket.Skipped, nodeID)
	default:
		bucket.NotRun = append(bucket.NotRun, nodeID)
	}
}

func (s *Store) siteDir() string {
	return filepath.Join(s.root, ".deck", "site")
}

func (s *Store) releasesDir() string {
	return filepath.Join(s.siteDir(), "releases")
}

func (s *Store) releaseDir(releaseID string) string {
	return filepath.Join(s.releasesDir(), releaseID)
}

func (s *Store) sessionDir(sessionID string) string {
	return filepath.Join(s.siteDir(), "sessions", sessionID)
}

func readJSON[T any](path string) (T, bool, error) {
	var zero T
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return zero, false, nil
		}
		return zero, false, fmt.Errorf("read json %q: %w", path, err)
	}
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		return zero, false, fmt.Errorf("parse json %q: %w", path, err)
	}
	return value, true, nil
}

func writeAtomicJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("write temp json: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace json file: %w", err)
	}
	return nil
}

func listJSONFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("read directory %q: %w", dir, err)
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		out = append(out, entry.Name())
	}
	sort.Strings(out)
	return out, nil
}

func copyDir(srcDir, dstDir string) error {
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("read source directory: %w", err)
	}
	for _, entry := range entries {
		src := filepath.Join(srcDir, entry.Name())
		dst := filepath.Join(dstDir, entry.Name())
		if entry.IsDir() {
			if err := copyDir(src, dst); err != nil {
				return err
			}
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("read source file info: %w", err)
		}
		if err := copyFile(src, dst, info.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source file %q: %w", src, err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("create destination file %q: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy file %q to %q: %w", src, dst, err)
	}
	return nil
}

func validateRecordID(id, field string) error {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return fmt.Errorf("%s is empty", field)
	}
	if !recordIDPattern.MatchString(trimmed) {
		return fmt.Errorf("%s must match ^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$", field)
	}
	return nil
}

func validateNodeID(nodeID, field string) error {
	trimmed := strings.TrimSpace(nodeID)
	if trimmed == "" {
		return fmt.Errorf("%s is empty", field)
	}
	if !nodeIDPattern.MatchString(trimmed) {
		return fmt.Errorf("%s must match ^[a-z0-9][a-z0-9-]{0,62}$", field)
	}
	return nil
}
