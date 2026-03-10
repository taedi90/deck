package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

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
