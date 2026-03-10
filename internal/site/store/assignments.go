package store

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

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
