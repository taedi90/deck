package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type auditLogger struct {
	mu sync.Mutex
	f  *os.File

	path         string
	maxSizeBytes int64
	maxFiles     int
}

type auditLoggerOptions struct {
	maxSizeBytes int64
	maxFiles     int
}

const (
	auditSchemaVersion = 1
	auditSourceServer  = "server"
	auditEventRequest  = "http_request"

	defaultAuditMaxSizeMB = 50
	defaultAuditMaxFiles  = 10
	defaultSiteAPIToken   = "deck-site-v1"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func newAuditLogger(root string, opts auditLoggerOptions) (*auditLogger, error) {
	logPath := filepath.Join(root, ".deck", "logs", "server-audit.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return nil, fmt.Errorf("create audit log directory: %w", err)
	}
	if opts.maxSizeBytes <= 0 {
		opts.maxSizeBytes = int64(defaultAuditMaxSizeMB) * 1024 * 1024
	}
	if opts.maxFiles <= 0 {
		opts.maxFiles = defaultAuditMaxFiles
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open audit log file: %w", err)
	}
	return &auditLogger{f: f, path: logPath, maxSizeBytes: opts.maxSizeBytes, maxFiles: opts.maxFiles}, nil
}

func (a *auditLogger) Write(entry map[string]any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.shouldRotateLocked() {
		_ = a.rotateLocked()
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_, _ = a.f.Write(append(raw, '\n'))
}

func (a *auditLogger) shouldRotateLocked() bool {
	if a.maxSizeBytes <= 0 {
		return false
	}
	info, err := a.f.Stat()
	if err != nil {
		return false
	}
	return info.Size() > a.maxSizeBytes
}

func (a *auditLogger) rotateLocked() error {
	if err := a.f.Close(); err != nil {
		return err
	}
	var firstErr error

	oldestPath := fmt.Sprintf("%s.%d", a.path, a.maxFiles)
	if err := os.Remove(oldestPath); err != nil && !os.IsNotExist(err) {
		firstErr = err
	}
	if firstErr == nil {
		for i := a.maxFiles - 1; i >= 1; i-- {
			src := fmt.Sprintf("%s.%d", a.path, i)
			dst := fmt.Sprintf("%s.%d", a.path, i+1)
			if err := os.Rename(src, dst); err != nil && !os.IsNotExist(err) {
				firstErr = err
				break
			}
		}
	}
	if firstErr == nil {
		if err := os.Rename(a.path, fmt.Sprintf("%s.1", a.path)); err != nil && !os.IsNotExist(err) {
			firstErr = err
		}
	}
	f, err := os.OpenFile(a.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		if firstErr != nil {
			return fmt.Errorf("%v; reopen audit log: %w", firstErr, err)
		}
		return err
	}
	a.f = f
	return firstErr
}

func buildServerAuditRecord(ts time.Time, eventType, level, message string) map[string]any {
	return map[string]any{
		"ts":             ts.UTC().Format(time.RFC3339Nano),
		"schema_version": auditSchemaVersion,
		"source":         auditSourceServer,
		"event_type":     eventType,
		"level":          level,
		"message":        message,
	}
}

func addExtra(entry map[string]any, extra map[string]any) {
	if len(extra) == 0 {
		return
	}
	entry["extra"] = extra
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}
