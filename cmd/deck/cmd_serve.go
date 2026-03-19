package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/taedi90/deck/internal/executil"
	"github.com/taedi90/deck/internal/fsutil"
	ctrllogs "github.com/taedi90/deck/internal/logs"
	"github.com/taedi90/deck/internal/server"
)

func executeServe(ctx context.Context, root string, addr string, auditMaxSizeMB int, auditMaxFiles int, tlsCert string, tlsKey string, tlsSelfSigned bool) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	resolvedRoot := strings.TrimSpace(root)
	if resolvedRoot == "" {
		resolvedRoot = "."
	}
	resolvedAddr := strings.TrimSpace(addr)
	if resolvedAddr == "" {
		resolvedAddr = ":8080"
	}
	resolvedTLSCert := strings.TrimSpace(tlsCert)
	resolvedTLSKey := strings.TrimSpace(tlsKey)

	if (resolvedTLSCert == "") != (resolvedTLSKey == "") {
		return errors.New("--tls-cert and --tls-key must be provided together")
	}
	if tlsSelfSigned && (resolvedTLSCert != "" || resolvedTLSKey != "") {
		return errors.New("--tls-self-signed cannot be combined with --tls-cert/--tls-key")
	}
	if auditMaxSizeMB <= 0 {
		return errors.New("--audit-max-size-mb must be > 0")
	}
	if auditMaxFiles <= 0 {
		return errors.New("--audit-max-files must be > 0")
	}

	certPath := resolvedTLSCert
	keyPath := resolvedTLSKey
	if tlsSelfSigned {
		var err error
		certPath, keyPath, err = server.EnsureSelfSignedTLS(resolvedRoot, resolvedAddr)
		if err != nil {
			return fmt.Errorf("ensure self-signed tls: %w", err)
		}
	}

	h, err := server.NewHandler(resolvedRoot, server.HandlerOptions{AuditMaxSizeMB: auditMaxSizeMB, AuditMaxFiles: auditMaxFiles})
	if err != nil {
		return fmt.Errorf("init server handler: %w", err)
	}
	httpServer := &http.Server{
		Addr:              resolvedAddr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
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
		if err := stdoutPrintf("server start: listening on https://%s (root=%s)\n", resolvedAddr, resolvedRoot); err != nil {
			return err
		}
	} else {
		if err := stdoutPrintf("server start: listening on http://%s (root=%s)\n", resolvedAddr, resolvedRoot); err != nil {
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
			return fmt.Errorf("serve: %w", err)
		}
		return nil
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve: %w", err)
		}
		return nil
	}
}

type healthReport struct {
	Status     string `json:"status"`
	Server     string `json:"server"`
	HealthURL  string `json:"healthUrl"`
	HTTPStatus int    `json:"httpStatus"`
}

func executeHealth(ctx context.Context, server string, output string) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	resolvedOutput, err := resolveOutputFormat(output)
	if err != nil {
		return err
	}
	resolvedServer, _, err := resolveRequiredSourceURL(server)
	if err != nil {
		return err
	}
	if err := verbosef(1, "deck: server health server=%s\n", resolvedServer); err != nil {
		return err
	}

	client := &http.Client{Timeout: 5 * time.Second}
	healthURL := strings.TrimRight(resolvedServer, "/") + "/healthz"
	if err := verbosef(2, "deck: server health url=%s\n", healthURL); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return fmt.Errorf("health: build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		_ = verbosef(2, "deck: server health requestError=%v\n", err)
		return fmt.Errorf("health: request failed: %w", err)
	}
	defer closeSilently(resp.Body)
	if resp.StatusCode != http.StatusOK {
		_ = verbosef(2, "deck: server health httpStatus=%d\n", resp.StatusCode)
		return fmt.Errorf("health: unexpected status %d", resp.StatusCode)
	}
	if err := verbosef(2, "deck: server health httpStatus=%d\n", resp.StatusCode); err != nil {
		return err
	}
	report := healthReport{Status: "ok", Server: resolvedServer, HealthURL: healthURL, HTTPStatus: resp.StatusCode}
	if resolvedOutput == "json" {
		enc := stdoutJSONEncoder()
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}

	return stdoutPrintf("health: ok (%s)\n", report.Server)
}

func executeLogs(ctx context.Context, root string, source string, path string, unit string, output string) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	resolvedSource := strings.ToLower(strings.TrimSpace(source))
	resolvedOutput, err := resolveOutputFormat(output)
	if err != nil {
		return err
	}
	if err := verbosef(1, "deck: server logs root=%s source=%s path=%s unit=%s output=%s\n", strings.TrimSpace(root), resolvedSource, strings.TrimSpace(path), strings.TrimSpace(unit), strings.TrimSpace(output)); err != nil {
		return err
	}
	if resolvedSource != "file" && resolvedSource != "journal" && resolvedSource != "both" {
		return errors.New("--source must be file, journal, or both")
	}

	records := []ctrllogs.LogRecord{}
	if resolvedSource == "file" || resolvedSource == "both" {
		logPath, err := resolveLogsFilePath(strings.TrimSpace(root), strings.TrimSpace(path))
		if err != nil {
			return err
		}
		if err := verbosef(1, "deck: server logs file=%s\n", logPath); err != nil {
			return err
		}
		fileRecords, err := readLogsFile(logPath)
		if err != nil {
			return err
		}
		if err := verbosef(1, "deck: server logs fileRecords=%d\n", len(fileRecords)); err != nil {
			return err
		}
		records = append(records, fileRecords...)
	}
	if resolvedSource == "journal" || resolvedSource == "both" {
		resolvedUnit := strings.TrimSpace(unit)
		if resolvedUnit == "" {
			return errors.New("--unit is required when --source includes journal")
		}
		if err := verbosef(1, "deck: server logs unit=%s\n", resolvedUnit); err != nil {
			return err
		}
		journalRecords, err := readControlLogsJournal(ctx, resolvedUnit, 50, 0)
		if err != nil {
			return fmt.Errorf("logs: %w\nsuggestion: %s", err, suggestJournalctlCommand(resolvedUnit))
		}
		if err := verbosef(1, "deck: server logs journalRecords=%d\n", len(journalRecords)); err != nil {
			return err
		}
		records = append(records, journalRecords...)
	}
	if err := verbosef(1, "deck: server logs records=%d\n", len(records)); err != nil {
		return err
	}

	if resolvedOutput == "json" {
		enc := stdoutJSONEncoder()
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
	f, err := fsutil.Open(path)
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

func readControlLogsJournal(ctx context.Context, unit string, tail int, since time.Duration) ([]ctrllogs.LogRecord, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}
	args := []string{"-u", unit, "-o", "json", "--no-pager", "-n", strconv.Itoa(tail)}
	if since > 0 {
		args = append(args, "--since", formatJournalSince(since))
	}
	raw, err := executil.CombinedOutputJournalctl(ctx, args...)
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
	if executil.IsExecutableNotFound(err) {
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
