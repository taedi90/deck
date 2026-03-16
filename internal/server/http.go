package server

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/taedi90/deck/internal/site/store"
)

func defaultSiteAuthTokenValue() string {
	return strings.Join([]string{"deck", "site", "v1"}, "-")
}

type HandlerOptions struct {
	ReportMax      int
	AuditMaxSizeMB int
	AuditMaxFiles  int
	AuthToken      string
}

type serverHandler struct {
	rootAbs   string
	logger    *auditLogger
	siteStore *store.Store
	apiToken  string
	base      http.Handler
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
	siteStore, err := store.New(resolvedRoot)
	if err != nil {
		return nil, fmt.Errorf("init site store: %w", err)
	}
	apiToken := strings.TrimSpace(opts.AuthToken)
	if apiToken == "" {
		apiToken = defaultSiteAuthTokenValue()
	}

	h := &serverHandler{rootAbs: resolvedRoot, logger: logger, siteStore: siteStore, apiToken: apiToken}
	h.base = http.HandlerFunc(h.routeRequest)

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
