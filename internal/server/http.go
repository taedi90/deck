package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
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

	auditEventRegistrySeed = "registry_seed"

	defaultAuditMaxSizeMB = 50
	defaultAuditMaxFiles  = 10
)

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

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

type HandlerOptions struct {
	ReportMax      int
	AuditMaxSizeMB int
	AuditMaxFiles  int
}

type serverHandler struct {
	rootAbs string
	logger  *auditLogger
	base    http.Handler
}

func NewHandler(root string, opts HandlerOptions) (http.Handler, error) {
	resolvedRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve server root: %w", err)
	}

	auditMaxSizeMB := opts.AuditMaxSizeMB
	if auditMaxSizeMB <= 0 {
		auditMaxSizeMB = defaultAuditMaxSizeMB
	}
	auditMaxFiles := opts.AuditMaxFiles
	if auditMaxFiles <= 0 {
		auditMaxFiles = defaultAuditMaxFiles
	}

	logger, err := newAuditLogger(resolvedRoot, auditLoggerOptions{maxSizeBytes: int64(auditMaxSizeMB) * 1024 * 1024, maxFiles: auditMaxFiles})
	if err != nil {
		return nil, err
	}

	h := &serverHandler{rootAbs: resolvedRoot, logger: logger}
	h.base = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			h.handleHealthz(w, r)
		case strings.HasPrefix(r.URL.Path, "/files/") || strings.HasPrefix(r.URL.Path, "/packages/") || strings.HasPrefix(r.URL.Path, "/images/") || strings.HasPrefix(r.URL.Path, "/workflows/"):
			h.handleStatic(w, r)
		default:
			http.NotFound(w, r)
		}
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now().UTC()
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		h.base.ServeHTTP(rw, r)
		level := "info"
		if rw.status >= http.StatusInternalServerError {
			level = "error"
		} else if rw.status >= http.StatusBadRequest {
			level = "warn"
		}
		entry := buildServerAuditRecord(start, auditEventRequest, level, "http request handled")
		addExtra(entry, map[string]any{
			"method":      r.Method,
			"path":        r.URL.Path,
			"status":      rw.status,
			"remote_addr": r.RemoteAddr,
			"duration_ms": time.Since(start).Milliseconds(),
		})
		logger.Write(entry)
	}), nil
}

func (h *serverHandler) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *serverHandler) handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodPut {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	category, relPath, targetPath, status := h.resolveCategoryPath(r.URL.Path)
	if status != 0 {
		w.WriteHeader(status)
		return
	}

	if r.Method == http.MethodPut {
		if err := writeFileAtomic(targetPath, r.Body); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		return
	}

	data, err := os.ReadFile(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	hash := sha256.Sum256(data)
	etag := fmt.Sprintf("\"sha256:%s\"", hex.EncodeToString(hash[:]))
	setStaticHeaders(w.Header(), category, relPath, etag, len(data), data)

	if matchETag(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func setStaticHeaders(h http.Header, category, relPath, etag string, size int, data []byte) {
	h.Set("Accept-Ranges", "bytes")
	h.Set("Cache-Control", "no-store")
	h.Set("ETag", etag)
	h.Set("Content-Length", strconv.Itoa(size))
	h.Set("Content-Type", http.DetectContentType(data))
	if category == "workflows" && relPath == "index.json" {
		h.Set("Content-Type", "application/json")
	}
}

func matchETag(rawHeader, expected string) bool {
	rawHeader = strings.TrimSpace(rawHeader)
	if rawHeader == "" {
		return false
	}
	if rawHeader == "*" {
		return true
	}
	for _, token := range strings.Split(rawHeader, ",") {
		if strings.TrimSpace(token) == expected {
			return true
		}
	}
	return false
}

func (h *serverHandler) resolveCategoryPath(urlPath string) (string, string, string, int) {
	category := ""
	switch {
	case strings.HasPrefix(urlPath, "/files/"):
		category = "files"
	case strings.HasPrefix(urlPath, "/packages/"):
		category = "packages"
	case strings.HasPrefix(urlPath, "/images/"):
		category = "images"
	case strings.HasPrefix(urlPath, "/workflows/"):
		category = "workflows"
	default:
		return "", "", "", http.StatusNotFound
	}

	relPath := strings.TrimPrefix(urlPath, "/"+category+"/")
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return "", "", "", http.StatusNotFound
	}
	if strings.Contains(relPath, "\\") {
		return "", "", "", http.StatusForbidden
	}
	for _, segment := range strings.Split(relPath, "/") {
		if segment == ".." {
			return "", "", "", http.StatusForbidden
		}
	}
	cleanRel := strings.TrimPrefix(path.Clean("/"+relPath), "/")
	if cleanRel == "." || cleanRel == "" {
		return "", "", "", http.StatusNotFound
	}
	if cleanRel == ".deck" || strings.HasPrefix(cleanRel, ".deck/") || strings.Contains(cleanRel, "/.deck/") {
		return "", "", "", http.StatusForbidden
	}

	targetPath := filepath.Join(h.rootAbs, category, filepath.FromSlash(cleanRel))
	resolvedTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return "", "", "", http.StatusForbidden
	}
	rootPrefix := h.rootAbs + string(os.PathSeparator)
	if resolvedTarget != h.rootAbs && !strings.HasPrefix(resolvedTarget, rootPrefix) {
		return "", "", "", http.StatusForbidden
	}

	return category, cleanRel, resolvedTarget, 0
}

func writeFileAtomic(targetPath string, src io.Reader) error {
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if _, err := io.Copy(tmpFile, src); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		return fmt.Errorf("replace file: %w", err)
	}
	return nil
}
