package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/taedi90/deck/internal/bundle"
	sitestore "github.com/taedi90/deck/internal/site/store"
)

func executeSiteReleaseImport(root string, releaseID string, bundlePath string, createdAt string) error {
	resolvedReleaseID := strings.TrimSpace(releaseID)
	if resolvedReleaseID == "" {
		return errors.New("--id is required")
	}
	resolvedBundlePath := strings.TrimSpace(bundlePath)
	if resolvedBundlePath == "" {
		return errors.New("--bundle is required")
	}
	resolvedCreatedAt := strings.TrimSpace(createdAt)
	if resolvedCreatedAt == "" {
		resolvedCreatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	st, resolvedRoot, err := newSiteStore(strings.TrimSpace(root))
	if err != nil {
		return err
	}

	bundleSHA256, err := sha256FileHex(resolvedBundlePath)
	if err != nil {
		return fmt.Errorf("site release import: read bundle hash: %w", err)
	}

	importRoot, err := os.MkdirTemp("", "deck-site-release-")
	if err != nil {
		return fmt.Errorf("site release import: create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(importRoot) }()

	if err := bundle.ImportArchive(resolvedBundlePath, importRoot); err != nil {
		return fmt.Errorf("site release import: %w", err)
	}
	importedBundlePath := filepath.Join(importRoot, "bundle")
	if !hasWorkflowDir(importedBundlePath) {
		return fmt.Errorf("site release import: extracted bundle missing workflows/: %s", importedBundlePath)
	}

	if err := st.ImportRelease(sitestore.Release{
		ID:           resolvedReleaseID,
		BundleSHA256: bundleSHA256,
		CreatedAt:    resolvedCreatedAt,
	}, importedBundlePath); err != nil {
		return err
	}

	return stdoutPrintf("site release import: ok (release=%s bundle=%s store=%s)\n", resolvedReleaseID, resolvedBundlePath, resolvedRoot)
}

func executeSiteReleaseList(root string, output string) error {
	if output != "text" && output != "json" {
		return errors.New("--output must be text or json")
	}

	st, _, err := newSiteStore(strings.TrimSpace(root))
	if err != nil {
		return err
	}
	releases, err := st.ListReleases()
	if err != nil {
		return err
	}

	if output == "json" {
		return json.NewEncoder(os.Stdout).Encode(releases)
	}
	for _, release := range releases {
		if _, err := fmt.Fprintf(os.Stdout, "%s\t%s\t%s\n", release.ID, release.CreatedAt, release.BundleSHA256); err != nil {
			return err
		}
	}
	return nil
}

func executeSiteSessionCreate(root string, sessionID string, releaseID string, startedAt string) error {
	resolvedSessionID := strings.TrimSpace(sessionID)
	if resolvedSessionID == "" {
		return errors.New("--id is required")
	}
	resolvedReleaseID := strings.TrimSpace(releaseID)
	if resolvedReleaseID == "" {
		return errors.New("--release is required")
	}
	resolvedStartedAt := strings.TrimSpace(startedAt)
	if resolvedStartedAt == "" {
		resolvedStartedAt = time.Now().UTC().Format(time.RFC3339)
	}

	st, _, err := newSiteStore(strings.TrimSpace(root))
	if err != nil {
		return err
	}
	if err := ensureReleaseExists(st, resolvedReleaseID); err != nil {
		return err
	}
	if err := st.CreateSession(sitestore.Session{
		ID:        resolvedSessionID,
		ReleaseID: resolvedReleaseID,
		Status:    "open",
		StartedAt: resolvedStartedAt,
	}); err != nil {
		return err
	}

	return stdoutPrintf("site session create: ok (session=%s release=%s)\n", resolvedSessionID, resolvedReleaseID)
}

func executeSiteSessionClose(root string, sessionID string, closedAt string) error {
	resolvedSessionID := strings.TrimSpace(sessionID)
	if resolvedSessionID == "" {
		return errors.New("--id is required")
	}
	resolvedClosedAt := strings.TrimSpace(closedAt)
	if resolvedClosedAt == "" {
		resolvedClosedAt = time.Now().UTC().Format(time.RFC3339)
	}

	st, _, err := newSiteStore(strings.TrimSpace(root))
	if err != nil {
		return err
	}
	closed, err := st.CloseSession(resolvedSessionID, resolvedClosedAt)
	if err != nil {
		return err
	}

	return stdoutPrintf("site session close: ok (session=%s status=%s)\n", closed.ID, closed.Status)
}

func executeSiteAssignRole(root string, sessionID string, assignmentID string, role string, workflow string) error {
	resolvedSessionID := strings.TrimSpace(sessionID)
	if resolvedSessionID == "" {
		return errors.New("--session is required")
	}
	resolvedAssignmentID := strings.TrimSpace(assignmentID)
	if resolvedAssignmentID == "" {
		return errors.New("--assignment is required")
	}
	resolvedRole := strings.TrimSpace(role)
	if resolvedRole == "" {
		return errors.New("--role is required")
	}
	resolvedWorkflow, err := normalizeWorkflowRef(strings.TrimSpace(workflow))
	if err != nil {
		return err
	}

	st, resolvedRoot, err := newSiteStore(strings.TrimSpace(root))
	if err != nil {
		return err
	}
	resolvedSession, err := validateSiteAssignmentTarget(st, resolvedRoot, resolvedSessionID, resolvedWorkflow)
	if err != nil {
		return err
	}

	if err := st.SaveAssignment(resolvedSession.ID, sitestore.Assignment{
		ID:        resolvedAssignmentID,
		SessionID: resolvedSession.ID,
		Role:      resolvedRole,
		Workflow:  resolvedWorkflow,
	}); err != nil {
		return err
	}

	return stdoutPrintf("site assign role: ok (session=%s assignment=%s role=%s)\n", resolvedSession.ID, resolvedAssignmentID, resolvedRole)
}

func executeSiteAssignNode(root string, sessionID string, assignmentID string, nodeID string, role string, workflow string) error {
	resolvedSessionID := strings.TrimSpace(sessionID)
	if resolvedSessionID == "" {
		return errors.New("--session is required")
	}
	resolvedAssignmentID := strings.TrimSpace(assignmentID)
	if resolvedAssignmentID == "" {
		return errors.New("--assignment is required")
	}
	resolvedNodeID := strings.TrimSpace(nodeID)
	if resolvedNodeID == "" {
		return errors.New("--node is required")
	}
	resolvedRole := strings.TrimSpace(role)
	resolvedWorkflow, err := normalizeWorkflowRef(strings.TrimSpace(workflow))
	if err != nil {
		return err
	}

	st, resolvedRoot, err := newSiteStore(strings.TrimSpace(root))
	if err != nil {
		return err
	}
	resolvedSession, err := validateSiteAssignmentTarget(st, resolvedRoot, resolvedSessionID, resolvedWorkflow)
	if err != nil {
		return err
	}

	if err := st.SaveAssignment(resolvedSession.ID, sitestore.Assignment{
		ID:        resolvedAssignmentID,
		SessionID: resolvedSession.ID,
		NodeID:    resolvedNodeID,
		Role:      resolvedRole,
		Workflow:  resolvedWorkflow,
	}); err != nil {
		return err
	}

	resolvedAssignment, err := st.ResolveAssignment(resolvedSession.ID, resolvedNodeID, resolvedRole)
	if err != nil {
		return err
	}
	if resolvedAssignment.ID != resolvedAssignmentID {
		return fmt.Errorf("site assign node: node assignment did not take precedence for session %q node %q", resolvedSession.ID, resolvedNodeID)
	}

	return stdoutPrintf("site assign node: ok (session=%s assignment=%s node=%s)\n", resolvedSession.ID, resolvedAssignmentID, resolvedNodeID)
}

type siteSessionStatus struct {
	Session sitestore.Session                  `json:"session"`
	Status  sitestore.SessionStatusAggregation `json:"status"`
}

type siteStatusOutput struct {
	Releases int                 `json:"releases"`
	Sessions []siteSessionStatus `json:"sessions"`
}

func executeSiteStatus(root string, output string) error {
	if output != "text" && output != "json" {
		return errors.New("--output must be text or json")
	}

	st, _, err := newSiteStore(strings.TrimSpace(root))
	if err != nil {
		return err
	}
	releases, err := st.ListReleases()
	if err != nil {
		return err
	}
	sessions, err := st.ListSessions()
	if err != nil {
		return err
	}

	status := siteStatusOutput{
		Releases: len(releases),
		Sessions: make([]siteSessionStatus, 0, len(sessions)),
	}
	for _, session := range sessions {
		aggregated, err := st.SessionStatusAggregation(session.ID)
		if err != nil {
			return err
		}
		status.Sessions = append(status.Sessions, siteSessionStatus{
			Session: session,
			Status:  aggregated,
		})
	}

	if output == "json" {
		return json.NewEncoder(os.Stdout).Encode(status)
	}

	if err := stdoutPrintf("site status: releases=%d sessions=%d\n", status.Releases, len(status.Sessions)); err != nil {
		return err
	}
	for _, session := range status.Sessions {
		if err := stdoutPrintf("session %s release=%s status=%s\n", session.Session.ID, session.Session.ReleaseID, session.Session.Status); err != nil {
			return err
		}
		nodeIDs := make([]string, 0, len(session.Status.Nodes))
		for nodeID := range session.Status.Nodes {
			nodeIDs = append(nodeIDs, nodeID)
		}
		sort.Strings(nodeIDs)
		for _, nodeID := range nodeIDs {
			node := session.Status.Nodes[nodeID]
			if err := stdoutPrintf("  node %s hostname=%s diff=%s doctor=%s apply=%s\n", node.NodeID, node.Hostname, node.Actions.Diff, node.Actions.Doctor, node.Actions.Apply); err != nil {
				return err
			}
		}
		if err := stdoutPrintf("  groups diff(ok=%v failed=%v skipped=%v not-run=%v) doctor(ok=%v failed=%v skipped=%v not-run=%v) apply(ok=%v failed=%v skipped=%v not-run=%v)\n",
			session.Status.Groups.Diff.OK,
			session.Status.Groups.Diff.Failed,
			session.Status.Groups.Diff.Skipped,
			session.Status.Groups.Diff.NotRun,
			session.Status.Groups.Doctor.OK,
			session.Status.Groups.Doctor.Failed,
			session.Status.Groups.Doctor.Skipped,
			session.Status.Groups.Doctor.NotRun,
			session.Status.Groups.Apply.OK,
			session.Status.Groups.Apply.Failed,
			session.Status.Groups.Apply.Skipped,
			session.Status.Groups.Apply.NotRun,
		); err != nil {
			return err
		}
	}
	return nil
}

func newSiteStore(root string) (*sitestore.Store, string, error) {
	resolvedRoot := strings.TrimSpace(root)
	if resolvedRoot == "" {
		resolvedRoot = "."
	}
	absRoot, err := filepath.Abs(resolvedRoot)
	if err != nil {
		return nil, "", fmt.Errorf("resolve --root: %w", err)
	}
	st, err := sitestore.New(absRoot)
	if err != nil {
		return nil, "", err
	}
	return st, absRoot, nil
}

func validateSiteAssignmentTarget(st *sitestore.Store, siteRoot, sessionID, workflowRef string) (sitestore.Session, error) {
	session, found, err := st.GetSession(sessionID)
	if err != nil {
		return sitestore.Session{}, err
	}
	if !found {
		return sitestore.Session{}, fmt.Errorf("session %q not found", sessionID)
	}
	if strings.EqualFold(strings.TrimSpace(session.Status), "closed") {
		return sitestore.Session{}, fmt.Errorf("session %q is closed", sessionID)
	}
	if strings.TrimSpace(session.ReleaseID) == "" {
		return sitestore.Session{}, fmt.Errorf("session %q has no release_id", sessionID)
	}
	if err := ensureReleaseExists(st, session.ReleaseID); err != nil {
		return sitestore.Session{}, err
	}

	releaseWorkflowPath := filepath.Join(siteRoot, ".deck", "site", "releases", session.ReleaseID, "bundle", filepath.FromSlash(workflowRef))
	info, err := os.Stat(releaseWorkflowPath)
	if err != nil {
		if os.IsNotExist(err) {
			return sitestore.Session{}, fmt.Errorf("workflow %q not found in release %q", workflowRef, session.ReleaseID)
		}
		return sitestore.Session{}, fmt.Errorf("stat workflow %q in release %q: %w", workflowRef, session.ReleaseID, err)
	}
	if info.IsDir() {
		return sitestore.Session{}, fmt.Errorf("workflow %q in release %q must be a file", workflowRef, session.ReleaseID)
	}
	return session, nil
}

func ensureReleaseExists(st *sitestore.Store, releaseID string) error {
	_, found, err := st.GetRelease(releaseID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("release %q not found", releaseID)
	}
	return nil
}

func normalizeWorkflowRef(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("--workflow is required")
	}
	cleaned := filepath.ToSlash(filepath.Clean(trimmed))
	if strings.HasPrefix(cleaned, "/") || cleaned == "." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("--workflow must be a relative path inside release bundle")
	}
	return cleaned, nil
}
