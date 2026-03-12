package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	ctrllogs "github.com/taedi90/deck/internal/logs"
	"github.com/taedi90/deck/internal/server"
)

func runServe(args []string) error {
	if wantsHelp(args) {
		return helpRequest{text: serveHelpText()}
	}
	return runServer(append([]string{"start"}, args...))
}

func runList(args []string) error {
	fs := newHelpFlagSet("list")
	var server string
	var output string
	fs.StringVar(&server, "server", "", "server URL for index (optional; defaults to local workflows/)")
	registerOutputFormatFlags(fs, &output, "text")
	if err := parseFlags(fs, args, listHelpText()); err != nil {
		return err
	}
	if output != "text" && output != "json" {
		return errors.New("--output must be text or json")
	}

	resolvedServer := strings.TrimSpace(server)
	localRoot := "."

	var items []string
	if resolvedServer == "" {
		localItems, err := discoverLocalWorkflowList(localRoot)
		if err != nil {
			return err
		}
		items = localItems
	} else {
		remoteItems, err := fetchWorkflowIndexFromServer(resolvedServer)
		if err != nil {
			return err
		}
		items = remoteItems
	}

	if output == "json" {
		enc := json.NewEncoder(os.Stdout)
		if err := enc.Encode(items); err != nil {
			return fmt.Errorf("list: encode output: %w", err)
		}
		return nil
	}

	w := bufio.NewWriter(os.Stdout)
	for _, it := range items {
		if _, err := fmt.Fprintln(w, it); err != nil {
			return err
		}
	}
	return w.Flush()
}

func fetchWorkflowIndexFromServer(server string) ([]string, error) {
	trimmed := strings.TrimRight(server, "/")
	indexURL := trimmed + "/workflows/index.json"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, indexURL, nil)
	if err != nil {
		return nil, fmt.Errorf("list: build request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list: request failed: %w", err)
	}
	defer closeSilently(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return []string{}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list: unexpected status %d", resp.StatusCode)
	}

	var items []string
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("list: decode response: %w", err)
	}
	return items, nil
}

func discoverLocalWorkflowList(root string) ([]string, error) {
	workflowDir := filepath.Join(root, "workflows")
	info, err := os.Stat(workflowDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("list: local workflows directory not found: %s", workflowDir)
		}
		return nil, fmt.Errorf("list: stat local workflows directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("list: local workflows path is not a directory: %s", workflowDir)
	}

	items := make([]string, 0)
	err = filepath.WalkDir(workflowDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		name := strings.ToLower(d.Name())
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			return nil
		}
		relPath, err := filepath.Rel(workflowDir, path)
		if err != nil {
			return err
		}
		items = append(items, filepath.ToSlash(filepath.Join("workflows", relPath)))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list: read local workflows directory: %w", err)
	}

	sort.Strings(items)
	return items, nil
}

func runHealth(args []string) error {
	fs := newHelpFlagSet("health")
	server := fs.String("server", "", "server base URL (required)")
	if err := parseFlags(fs, args, healthHelpText()); err != nil {
		return err
	}
	resolvedServer := strings.TrimSpace(*server)
	if resolvedServer == "" {
		return errors.New("--server is required (assisted mode is explicit: deck health --server <url>)")
	}

	client := &http.Client{Timeout: 5 * time.Second}
	healthURL := strings.TrimRight(resolvedServer, "/") + "/healthz"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, healthURL, nil)
	if err != nil {
		return fmt.Errorf("health: build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("health: request failed: %w", err)
	}
	defer closeSilently(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health: unexpected status %d", resp.StatusCode)
	}

	return stdoutPrintf("health: ok (%s)\n", resolvedServer)
}

func runLogs(args []string) error {
	fs := newHelpFlagSet("logs")
	root := fs.String("root", ".", "serve root directory")
	source := fs.String("source", "file", "log source (file|journal|both)")
	path := fs.String("path", "", "explicit audit log file path")
	unit := fs.String("unit", "", "systemd unit for journal logs")
	output := ""
	registerOutputFormatFlags(fs, &output, "text")
	if err := parseFlags(fs, args, logsHelpText()); err != nil {
		return err
	}
	resolvedSource := strings.ToLower(strings.TrimSpace(*source))
	if resolvedSource != "file" && resolvedSource != "journal" && resolvedSource != "both" {
		return errors.New("--source must be file, journal, or both")
	}
	if output != "text" && output != "json" {
		return errors.New("--output must be text or json")
	}

	records := []ctrllogs.LogRecord{}
	if resolvedSource == "file" || resolvedSource == "both" {
		logPath, err := resolveLogsFilePath(strings.TrimSpace(*root), strings.TrimSpace(*path))
		if err != nil {
			return err
		}
		fileRecords, err := readLogsFile(logPath)
		if err != nil {
			return err
		}
		records = append(records, fileRecords...)
	}
	if resolvedSource == "journal" || resolvedSource == "both" {
		resolvedUnit := strings.TrimSpace(*unit)
		if resolvedUnit == "" {
			return errors.New("--unit is required when --source includes journal")
		}
		journalRecords, err := readControlLogsJournal(resolvedUnit, 50, 0)
		if err != nil {
			return fmt.Errorf("logs: %w\nsuggestion: %s", err, suggestJournalctlCommand(resolvedUnit))
		}
		records = append(records, journalRecords...)
	}

	if output == "json" {
		enc := json.NewEncoder(os.Stdout)
		return enc.Encode(records)
	}
	for _, record := range records {
		if err := stdoutPrintln(ctrllogs.FormatLogText(record)); err != nil {
			return err
		}
	}
	return nil
}

func resolveLogsFilePath(root string, path string) (string, error) {
	if strings.TrimSpace(path) != "" {
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				return "", fmt.Errorf("logs: log file not found: %s", path)
			}
			return "", fmt.Errorf("logs: stat log file: %w", err)
		}
		return path, nil
	}
	resolvedRoot := strings.TrimSpace(root)
	if resolvedRoot == "" {
		resolvedRoot = "."
	}
	logPath := filepath.Join(resolvedRoot, ".deck", "logs", "server-audit.log")
	if _, err := os.Stat(logPath); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("logs: log file not found: %s", logPath)
		}
		return "", fmt.Errorf("logs: stat log file: %w", err)
	}
	return logPath, nil
}

func readLogsFile(path string) ([]ctrllogs.LogRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("logs: open log file: %w", err)
	}
	defer closeSilently(f)

	records := []ctrllogs.LogRecord{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		record, parseErr := ctrllogs.NormalizeJSONLine([]byte(line))
		if parseErr != nil {
			continue
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("logs: read log file: %w", err)
	}
	return records, nil
}

func readControlLogsJournal(unit string, tail int, since time.Duration) ([]ctrllogs.LogRecord, error) {
	args := []string{"-u", unit, "-o", "json", "--no-pager", "-n", strconv.Itoa(tail)}
	if since > 0 {
		args = append(args, "--since", formatJournalSince(since))
	}
	raw, err := exec.Command("journalctl", args...).CombinedOutput()
	if err != nil {
		return nil, classifyJournalctlError(err, strings.TrimSpace(string(raw)))
	}
	return parseJournalOutputLines(raw), nil
}

func parseJournalOutputLines(raw []byte) []ctrllogs.LogRecord {
	records := []ctrllogs.LogRecord{}
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		records = append(records, ctrllogs.NormalizeJournalRecord(entry))
	}
	return records
}

func classifyJournalctlError(err error, output string) error {
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return errors.New("journalctl not found")
	}
	if isPermissionError(output) {
		return errors.New("journalctl permission denied")
	}
	if output != "" {
		return fmt.Errorf("journalctl failed: %s", output)
	}
	return fmt.Errorf("journalctl failed: %w", err)
}

func suggestJournalctlCommand(unit string) string {
	return fmt.Sprintf("sudo journalctl -u %s --no-pager -n 50", unit)
}

func formatJournalSince(since time.Duration) string {
	return time.Now().Add(-since).Format(time.RFC3339)
}

func isPermissionError(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "permission denied") ||
		strings.Contains(lower, "access denied") ||
		strings.Contains(lower, "interactive authentication required")
}

func runServer(args []string) error {
	if len(args) == 0 || args[0] != "start" {
		return helpRequest{text: serveHelpText()}
	}

	fs := newHelpFlagSet("server start")
	root := fs.String("root", "./bundle", "server content root")
	addr := fs.String("addr", ":8080", "server listen address")
	apiToken := fs.String("api-token", "deck-site-v1", "bearer token required for /api/site/v1 endpoints")
	reportMax := fs.Int("report-max", 200, "max retained in-memory reports")
	auditMaxSizeMB := fs.Int("audit-max-size-mb", 50, "max audit log size in MB before rotation")
	auditMaxFiles := fs.Int("audit-max-files", 10, "max retained rotated audit files")
	tlsCert := fs.String("tls-cert", "", "TLS certificate path")
	tlsKey := fs.String("tls-key", "", "TLS private key path")
	tlsSelfSigned := fs.Bool("tls-self-signed", false, "auto-generate and use self-signed TLS cert")
	if err := parseFlags(fs, args[1:], serveHelpText()); err != nil {
		return err
	}

	if (*tlsCert == "") != (*tlsKey == "") {
		return errors.New("--tls-cert and --tls-key must be provided together")
	}
	if *tlsSelfSigned && (*tlsCert != "" || *tlsKey != "") {
		return errors.New("--tls-self-signed cannot be combined with --tls-cert/--tls-key")
	}
	if *reportMax <= 0 {
		return errors.New("--report-max must be > 0")
	}
	if *auditMaxSizeMB <= 0 {
		return errors.New("--audit-max-size-mb must be > 0")
	}
	if *auditMaxFiles <= 0 {
		return errors.New("--audit-max-files must be > 0")
	}

	certPath := *tlsCert
	keyPath := *tlsKey
	if *tlsSelfSigned {
		var err error
		certPath, keyPath, err = server.EnsureSelfSignedTLS(*root, *addr)
		if err != nil {
			return err
		}
	}

	h, err := server.NewHandler(*root, server.HandlerOptions{ReportMax: *reportMax, AuditMaxSizeMB: *auditMaxSizeMB, AuditMaxFiles: *auditMaxFiles, APIToken: *apiToken})
	if err != nil {
		return err
	}
	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() {
		if certPath != "" {
			errCh <- httpServer.ListenAndServeTLS(certPath, keyPath)
			return
		}
		errCh <- httpServer.ListenAndServe()
	}()
	if certPath != "" {
		if err := stdoutPrintf("server start: listening on https://%s (root=%s)\n", *addr, *root); err != nil {
			return err
		}
	} else {
		if err := stdoutPrintf("server start: listening on http://%s (root=%s)\n", *addr, *root); err != nil {
			return err
		}
	}
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("server shutdown: %w", err)
		}
		err := <-errCh
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
