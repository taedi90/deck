package store

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

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
