package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/taedi90/deck/internal/bundle"
	"github.com/taedi90/deck/internal/nodeid"
	sitestore "github.com/taedi90/deck/internal/site/store"
)

type assistedExecutionConfig struct {
	Server   string
	Session  string
	APIToken string
}

type assistedExecutionContext struct {
	Config        assistedExecutionConfig
	NodeID        string
	Hostname      string
	ReleaseID     string
	Assignment    sitestore.Assignment
	WorkflowPath  string
	BundleRoot    string
	Skipped       bool
	SkipReason    string
	LocalReport   string
	ReportStarted time.Time
	ReportEnded   time.Time
}

type assistedManifest struct {
	Entries []packManifestEntry `json:"entries"`
}

func registerAssistedFlags(fs *flag.FlagSet, server *string, session *string, apiToken *string) {
	fs.StringVar(server, "server", "", "site server URL (assisted mode requires --server and --session)")
	fs.StringVar(session, "session", "", "site session id for assisted mode")
	fs.StringVar(apiToken, "api-token", "deck-site-v1", "bearer token for assisted site APIs")
}

func resolveAssistedExecutionConfig(server, session, apiToken string) (assistedExecutionConfig, bool, error) {
	resolved := assistedExecutionConfig{
		Server:   strings.TrimRight(strings.TrimSpace(server), "/"),
		Session:  strings.TrimSpace(session),
		APIToken: strings.TrimSpace(apiToken),
	}
	assistedEnabled := resolved.Server != "" || resolved.Session != ""
	if !assistedEnabled {
		return resolved, false, nil
	}
	if resolved.Server == "" || resolved.Session == "" {
		return assistedExecutionConfig{}, false, errors.New("assisted mode requires both --server and --session")
	}
	if resolved.APIToken == "" {
		return assistedExecutionConfig{}, false, errors.New("--api-token is required in assisted mode")
	}
	return resolved, true, nil
}

func runAssistedAction(config assistedExecutionConfig, action string, execute func(ctx assistedExecutionContext) error) error {
	ctx, err := prepareAssistedExecution(config, action)
	if err != nil {
		return err
	}
	if ctx.Skipped {
		return stdoutPrintf("%s: skipped (%s)\n", action, ctx.SkipReason)
	}

	start := time.Now().UTC()
	execErr := execute(ctx)
	end := time.Now().UTC()
	status := "ok"
	if execErr != nil {
		status = "failed"
	}

	reportPath, report, err := persistAssistedExecutionReport(ctx, action, status, start, end)
	if err != nil {
		return err
	}
	if err := uploadAssistedExecutionReport(ctx.Config, report); err != nil {
		return fmt.Errorf("%s: report upload transport failure (local report: %s): %w", action, reportPath, err)
	}
	if execErr != nil {
		return execErr
	}
	return nil
}

func prepareAssistedExecution(config assistedExecutionConfig, action string) (assistedExecutionContext, error) {
	nodeResult, err := nodeid.Resolve(resolveNodeIDPathsFromEnv())
	if err != nil {
		return assistedExecutionContext{}, fmt.Errorf("%s assisted: resolve node_id: %w", action, err)
	}

	session, err := fetchAssistedSession(config, config.Session)
	if err != nil {
		return assistedExecutionContext{}, fmt.Errorf("%s assisted: fetch session: %w", action, err)
	}
	releaseID := strings.TrimSpace(session.ReleaseID)
	if releaseID == "" {
		return assistedExecutionContext{}, fmt.Errorf("%s assisted: session %q has no release_id", action, config.Session)
	}

	assignment, found, err := fetchAssistedAssignment(config, config.Session, nodeResult.ID, action)
	if err != nil {
		return assistedExecutionContext{}, fmt.Errorf("%s assisted: fetch assignment: %w", action, err)
	}
	if !found {
		localPath, persistErr := persistAssistedSkippedReport(config, action, nodeResult.ID, nodeResult.Hostname)
		if persistErr != nil {
			return assistedExecutionContext{}, persistErr
		}
		return assistedExecutionContext{
			Config:      config,
			NodeID:      nodeResult.ID,
			Hostname:    nodeResult.Hostname,
			ReleaseID:   releaseID,
			Skipped:     true,
			SkipReason:  "no-assignment",
			LocalReport: localPath,
		}, nil
	}

	workflowRef := strings.TrimSpace(assignment.Workflow)
	if workflowRef == "" {
		return assistedExecutionContext{}, fmt.Errorf("%s assisted: assignment %q has empty workflow", action, assignment.ID)
	}

	bundleRoot := assistedReleaseBundleRoot(releaseID)
	if err := syncAssistedReleaseBundle(config, releaseID, workflowRef, bundleRoot); err != nil {
		return assistedExecutionContext{}, fmt.Errorf("%s assisted: fetch release bundle: %w", action, err)
	}

	workflowPath := filepath.Join(bundleRoot, filepath.FromSlash(workflowRef))
	if info, err := os.Stat(workflowPath); err != nil || info.IsDir() {
		if err != nil {
			return assistedExecutionContext{}, fmt.Errorf("%s assisted: workflow not available in fetched release content: %w", action, err)
		}
		return assistedExecutionContext{}, fmt.Errorf("%s assisted: workflow not available in fetched release content: %s", action, workflowRef)
	}

	return assistedExecutionContext{
		Config:       config,
		NodeID:       nodeResult.ID,
		Hostname:     nodeResult.Hostname,
		ReleaseID:    releaseID,
		Assignment:   assignment,
		WorkflowPath: workflowPath,
		BundleRoot:   bundleRoot,
	}, nil
}

func persistAssistedSkippedReport(config assistedExecutionConfig, action, nodeID, hostname string) (string, error) {
	now := time.Now().UTC()
	report := sitestore.ExecutionReport{
		ID:        fmt.Sprintf("report-%s-%d", action, now.UnixNano()),
		SessionID: config.Session,
		NodeID:    nodeID,
		Hostname:  hostname,
		Action:    action,
		Status:    "skipped",
		StartedAt: now.Format(time.RFC3339),
		EndedAt:   now.Format(time.RFC3339),
	}
	return writeAssistedReportFile(config.Session, nodeID, action, report)
}

func persistAssistedExecutionReport(ctx assistedExecutionContext, action, status string, start, end time.Time) (string, sitestore.ExecutionReport, error) {
	report := sitestore.ExecutionReport{
		ID:           fmt.Sprintf("report-%s-%d", action, end.UnixNano()),
		SessionID:    ctx.Config.Session,
		AssignmentID: strings.TrimSpace(ctx.Assignment.ID),
		NodeID:       ctx.NodeID,
		Hostname:     ctx.Hostname,
		Action:       action,
		WorkflowRef:  strings.TrimSpace(ctx.Assignment.Workflow),
		Status:       status,
		StartedAt:    start.Format(time.RFC3339),
		EndedAt:      end.Format(time.RFC3339),
	}
	path, err := writeAssistedReportFile(ctx.Config.Session, ctx.NodeID, action, report)
	return path, report, err
}

func writeAssistedReportFile(sessionID, nodeID, action string, report sitestore.ExecutionReport) (string, error) {
	reportDir := filepath.Join(assistedDataRoot(), "reports", sessionID, nodeID)
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return "", fmt.Errorf("create local assisted report dir: %w", err)
	}
	name := fmt.Sprintf("%s-%s.json", action, time.Now().UTC().Format("20060102T150405Z"))
	path := filepath.Join(reportDir, name)
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode assisted execution report: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return "", fmt.Errorf("write local assisted report: %w", err)
	}
	return path, nil
}

func uploadAssistedExecutionReport(config assistedExecutionConfig, report sitestore.ExecutionReport) error {
	urlPath := fmt.Sprintf("%s/api/site/v1/sessions/%s/reports", config.Server, url.PathEscape(config.Session))
	raw, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("encode report payload: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, urlPath, strings.NewReader(string(raw)))
	if err != nil {
		return fmt.Errorf("build report request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+config.APIToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer closeSilently(resp.Body)
	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, msg)
	}
	return nil
}

func fetchAssistedSession(config assistedExecutionConfig, sessionID string) (sitestore.Session, error) {
	endpoint := fmt.Sprintf("%s/api/site/v1/sessions/%s", config.Server, url.PathEscape(sessionID))
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return sitestore.Session{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+config.APIToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return sitestore.Session{}, err
	}
	defer closeSilently(resp.Body)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return sitestore.Session{}, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	out := sitestore.Session{}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return sitestore.Session{}, fmt.Errorf("decode session response: %w", err)
	}
	return out, nil
}

func fetchAssistedAssignment(config assistedExecutionConfig, sessionID, nodeID, action string) (sitestore.Assignment, bool, error) {
	endpoint := fmt.Sprintf("%s/api/site/v1/sessions/%s/assignment?node_id=%s&action=%s", config.Server, url.PathEscape(sessionID), url.QueryEscape(nodeID), url.QueryEscape(action))
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return sitestore.Assignment{}, false, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+config.APIToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return sitestore.Assignment{}, false, err
	}
	defer closeSilently(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		if strings.Contains(string(body), "no assignment matched") {
			return sitestore.Assignment{}, false, nil
		}
		return sitestore.Assignment{}, false, fmt.Errorf("unexpected status 404: %s", strings.TrimSpace(string(body)))
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return sitestore.Assignment{}, false, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	assignment := sitestore.Assignment{}
	if err := json.NewDecoder(resp.Body).Decode(&assignment); err != nil {
		return sitestore.Assignment{}, false, fmt.Errorf("decode assignment response: %w", err)
	}
	return assignment, true, nil
}

func syncAssistedReleaseBundle(config assistedExecutionConfig, releaseID, workflowRef, bundleRoot string) error {
	relPaths := map[string]struct{}{
		".deck/manifest.json": {},
		workflowRef:           {},
		"workflows/vars.yaml": {},
	}

	manifestRaw, err := fetchAssistedReleaseBundleFile(config, releaseID, ".deck/manifest.json")
	if err != nil {
		return err
	}
	manifest := assistedManifest{}
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		return fmt.Errorf("decode release manifest: %w", err)
	}
	for _, entry := range manifest.Entries {
		rel := strings.TrimSpace(entry.Path)
		if rel == "" {
			continue
		}
		relPaths[rel] = struct{}{}
	}

	for relPath := range relPaths {
		if err := writeAssistedBundleFile(config, releaseID, bundleRoot, relPath); err != nil {
			if relPath == "workflows/vars.yaml" && strings.Contains(err.Error(), "status 404") {
				continue
			}
			return err
		}
	}
	return nil
}

func writeAssistedBundleFile(config assistedExecutionConfig, releaseID, bundleRoot, relPath string) error {
	clean := filepath.Clean(filepath.FromSlash(relPath))
	if clean == "." || clean == "" || strings.HasPrefix(clean, "..") {
		return fmt.Errorf("invalid release bundle path %q", relPath)
	}
	raw, err := fetchAssistedReleaseBundleFile(config, releaseID, filepath.ToSlash(clean))
	if err != nil {
		return err
	}
	absPath := filepath.Join(bundleRoot, clean)
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return fmt.Errorf("create release bundle path: %w", err)
	}
	if err := os.WriteFile(absPath, raw, 0o644); err != nil {
		return fmt.Errorf("write release bundle file: %w", err)
	}
	return nil
}

func fetchAssistedReleaseBundleFile(config assistedExecutionConfig, releaseID, relPath string) ([]byte, error) {
	endpoint := fmt.Sprintf("%s/site/releases/%s/bundle/%s", config.Server, url.PathEscape(releaseID), relPath)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build release bundle request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+config.APIToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer closeSilently(resp.Body)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("release bundle fetch %q failed: status %d: %s", relPath, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read release bundle response: %w", err)
	}
	return raw, nil
}

func assistedDataRoot() string {
	if raw := strings.TrimSpace(os.Getenv("DECK_ASSISTED_ROOT")); raw != "" {
		return raw
	}
	return "/var/lib/deck"
}

func assistedReleaseBundleRoot(releaseID string) string {
	return filepath.Join(assistedDataRoot(), "releases", releaseID, "bundle")
}

func runSite(args []string) error {
	if len(args) == 0 {
		return helpRequest{text: siteHelpText()}
	}
	if wantsHelp(args) {
		text, err := renderSiteHelp(args[1:])
		if err != nil {
			return err
		}
		return helpRequest{text: text}
	}

	switch args[0] {
	case "release":
		return runSiteRelease(args[1:])
	case "session":
		return runSiteSession(args[1:])
	case "assign":
		return runSiteAssign(args[1:])
	case "status":
		return runSiteStatus(args[1:])
	default:
		return fmt.Errorf("unknown site command %q", args[0])
	}
}

func runSiteRelease(args []string) error {
	if len(args) == 0 {
		return helpRequest{text: siteReleaseHelpText()}
	}
	if wantsHelp(args) {
		text, err := renderSiteReleaseHelp(args[1:])
		if err != nil {
			return err
		}
		return helpRequest{text: text}
	}
	switch args[0] {
	case "import":
		return runSiteReleaseImport(args[1:])
	case "list":
		return runSiteReleaseList(args[1:])
	default:
		return fmt.Errorf("unknown site release command %q", args[0])
	}
}

func runSiteReleaseImport(args []string) error {
	fs := newHelpFlagSet("site release import")
	root := fs.String("root", ".", "site server root")
	releaseID := fs.String("id", "", "release id")
	bundlePath := fs.String("bundle", "", "local bundle archive path")
	createdAt := fs.String("created-at", "", "release timestamp (RFC3339, optional)")
	if err := parseFlags(fs, args, siteReleaseImportHelpText()); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return helpRequest{text: siteReleaseImportHelpText()}
	}

	resolvedReleaseID := strings.TrimSpace(*releaseID)
	if resolvedReleaseID == "" {
		return errors.New("--id is required")
	}
	resolvedBundlePath := strings.TrimSpace(*bundlePath)
	if resolvedBundlePath == "" {
		return errors.New("--bundle is required")
	}
	resolvedCreatedAt := strings.TrimSpace(*createdAt)
	if resolvedCreatedAt == "" {
		resolvedCreatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	st, resolvedRoot, err := newSiteStore(strings.TrimSpace(*root))
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

func runSiteReleaseList(args []string) error {
	fs := newHelpFlagSet("site release list")
	root := fs.String("root", ".", "site server root")
	output := ""
	registerOutputFormatFlags(fs, &output, "text")
	if err := parseFlags(fs, args, siteReleaseListHelpText()); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return helpRequest{text: siteReleaseListHelpText()}
	}
	if output != "text" && output != "json" {
		return errors.New("--output must be text or json")
	}

	st, _, err := newSiteStore(strings.TrimSpace(*root))
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

func runSiteSession(args []string) error {
	if len(args) == 0 {
		return helpRequest{text: siteSessionHelpText()}
	}
	if wantsHelp(args) {
		text, err := renderSiteSessionHelp(args[1:])
		if err != nil {
			return err
		}
		return helpRequest{text: text}
	}
	switch args[0] {
	case "create":
		return runSiteSessionCreate(args[1:])
	case "close":
		return runSiteSessionClose(args[1:])
	default:
		return fmt.Errorf("unknown site session command %q", args[0])
	}
}

func runSiteSessionCreate(args []string) error {
	fs := newHelpFlagSet("site session create")
	root := fs.String("root", ".", "site server root")
	sessionID := fs.String("id", "", "session id")
	releaseID := fs.String("release", "", "release id")
	startedAt := fs.String("started-at", "", "session start timestamp (RFC3339, optional)")
	if err := parseFlags(fs, args, siteSessionCreateHelpText()); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return helpRequest{text: siteSessionCreateHelpText()}
	}

	resolvedSessionID := strings.TrimSpace(*sessionID)
	if resolvedSessionID == "" {
		return errors.New("--id is required")
	}
	resolvedReleaseID := strings.TrimSpace(*releaseID)
	if resolvedReleaseID == "" {
		return errors.New("--release is required")
	}
	resolvedStartedAt := strings.TrimSpace(*startedAt)
	if resolvedStartedAt == "" {
		resolvedStartedAt = time.Now().UTC().Format(time.RFC3339)
	}

	st, _, err := newSiteStore(strings.TrimSpace(*root))
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

func runSiteSessionClose(args []string) error {
	fs := newHelpFlagSet("site session close")
	root := fs.String("root", ".", "site server root")
	sessionID := fs.String("id", "", "session id")
	closedAt := fs.String("closed-at", "", "session close timestamp (RFC3339, optional)")
	if err := parseFlags(fs, args, siteSessionCloseHelpText()); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return helpRequest{text: siteSessionCloseHelpText()}
	}

	resolvedSessionID := strings.TrimSpace(*sessionID)
	if resolvedSessionID == "" {
		return errors.New("--id is required")
	}
	resolvedClosedAt := strings.TrimSpace(*closedAt)
	if resolvedClosedAt == "" {
		resolvedClosedAt = time.Now().UTC().Format(time.RFC3339)
	}

	st, _, err := newSiteStore(strings.TrimSpace(*root))
	if err != nil {
		return err
	}
	closed, err := st.CloseSession(resolvedSessionID, resolvedClosedAt)
	if err != nil {
		return err
	}

	return stdoutPrintf("site session close: ok (session=%s status=%s)\n", closed.ID, closed.Status)
}

func runSiteAssign(args []string) error {
	if len(args) == 0 {
		return helpRequest{text: siteAssignHelpText()}
	}
	if wantsHelp(args) {
		text, err := renderSiteAssignHelp(args[1:])
		if err != nil {
			return err
		}
		return helpRequest{text: text}
	}
	switch args[0] {
	case "role":
		return runSiteAssignRole(args[1:])
	case "node":
		return runSiteAssignNode(args[1:])
	default:
		return fmt.Errorf("unknown site assign command %q", args[0])
	}
}

func runSiteAssignRole(args []string) error {
	fs := newHelpFlagSet("site assign role")
	root := fs.String("root", ".", "site server root")
	sessionID := fs.String("session", "", "session id")
	assignmentID := fs.String("assignment", "", "assignment id")
	role := fs.String("role", "", "role")
	workflow := fs.String("workflow", "", "workflow path inside release bundle")
	if err := parseFlags(fs, args, siteAssignRoleHelpText()); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return helpRequest{text: siteAssignRoleHelpText()}
	}

	resolvedSessionID := strings.TrimSpace(*sessionID)
	if resolvedSessionID == "" {
		return errors.New("--session is required")
	}
	resolvedAssignmentID := strings.TrimSpace(*assignmentID)
	if resolvedAssignmentID == "" {
		return errors.New("--assignment is required")
	}
	resolvedRole := strings.TrimSpace(*role)
	if resolvedRole == "" {
		return errors.New("--role is required")
	}
	resolvedWorkflow, err := normalizeWorkflowRef(strings.TrimSpace(*workflow))
	if err != nil {
		return err
	}

	st, resolvedRoot, err := newSiteStore(strings.TrimSpace(*root))
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

func runSiteAssignNode(args []string) error {
	fs := newHelpFlagSet("site assign node")
	root := fs.String("root", ".", "site server root")
	sessionID := fs.String("session", "", "session id")
	assignmentID := fs.String("assignment", "", "assignment id")
	nodeID := fs.String("node", "", "node id")
	role := fs.String("role", "", "role (optional)")
	workflow := fs.String("workflow", "", "workflow path inside release bundle")
	if err := parseFlags(fs, args, siteAssignNodeHelpText()); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return helpRequest{text: siteAssignNodeHelpText()}
	}

	resolvedSessionID := strings.TrimSpace(*sessionID)
	if resolvedSessionID == "" {
		return errors.New("--session is required")
	}
	resolvedAssignmentID := strings.TrimSpace(*assignmentID)
	if resolvedAssignmentID == "" {
		return errors.New("--assignment is required")
	}
	resolvedNodeID := strings.TrimSpace(*nodeID)
	if resolvedNodeID == "" {
		return errors.New("--node is required")
	}
	resolvedRole := strings.TrimSpace(*role)
	resolvedWorkflow, err := normalizeWorkflowRef(strings.TrimSpace(*workflow))
	if err != nil {
		return err
	}

	st, resolvedRoot, err := newSiteStore(strings.TrimSpace(*root))
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

func runSiteStatus(args []string) error {
	fs := newHelpFlagSet("site status")
	root := fs.String("root", ".", "site server root")
	output := ""
	registerOutputFormatFlags(fs, &output, "text")
	if err := parseFlags(fs, args, siteStatusHelpText()); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return helpRequest{text: siteStatusHelpText()}
	}
	if output != "text" && output != "json" {
		return errors.New("--output must be text or json")
	}

	st, _, err := newSiteStore(strings.TrimSpace(*root))
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
