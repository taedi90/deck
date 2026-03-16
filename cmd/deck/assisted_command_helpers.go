package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/taedi90/deck/internal/filemode"
	"github.com/taedi90/deck/internal/nodeid"
	sitestore "github.com/taedi90/deck/internal/site/store"
)

type assistedExecutionConfig struct {
	Server    string
	Session   string
	AuthToken string
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

type assistedManifestEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type assistedManifest struct {
	Entries []assistedManifestEntry `json:"entries"`
}

func resolveAssistedExecutionConfig(server, session, apiToken string) (assistedExecutionConfig, bool, error) {
	resolvedServer, _, err := resolveServerURL(server)
	if err != nil {
		return assistedExecutionConfig{}, false, err
	}
	resolvedToken, _, err := resolveServerAuthToken(apiToken)
	if err != nil {
		return assistedExecutionConfig{}, false, err
	}
	resolved := assistedExecutionConfig{
		Server:    resolvedServer,
		Session:   strings.TrimSpace(session),
		AuthToken: resolvedToken,
	}
	assistedEnabled := resolved.Server != "" || resolved.Session != ""
	if !assistedEnabled {
		return resolved, false, nil
	}
	if resolved.Server == "" || resolved.Session == "" {
		return assistedExecutionConfig{}, false, errors.New("assisted mode requires both --session and a server from --server or \"deck server set <url>\"")
	}
	if resolved.AuthToken == "" {
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
	if err := filemode.EnsurePrivateDir(reportDir); err != nil {
		return "", fmt.Errorf("create local assisted report dir: %w", err)
	}
	name := fmt.Sprintf("%s-%s.json", action, time.Now().UTC().Format("20060102T150405Z"))
	path := filepath.Join(reportDir, name)
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode assisted execution report: %w", err)
	}
	if err := filemode.WritePrivateFile(path, raw); err != nil {
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
	req.Header.Set("Authorization", "Bearer "+config.AuthToken)
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
	req.Header.Set("Authorization", "Bearer "+config.AuthToken)
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
	req.Header.Set("Authorization", "Bearer "+config.AuthToken)
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
	if err := filemode.EnsureParentArtifactDir(absPath); err != nil {
		return fmt.Errorf("create release bundle path: %w", err)
	}
	if err := filemode.WriteArtifactFile(absPath, raw); err != nil {
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
	req.Header.Set("Authorization", "Bearer "+config.AuthToken)
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
