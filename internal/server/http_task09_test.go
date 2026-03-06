package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServeTask09_AuditAndSafety(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "files"), 0o755); err != nil {
		t.Fatalf("mkdir files: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "files", "ok.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("write ok file: %v", err)
	}

	h, err := NewHandler(root, HandlerOptions{})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/files/ok.txt", nil),
		httptest.NewRequest(http.MethodGet, "/files/../.deck/secret", nil),
	} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
	}

	entries := readAuditEntries(t, root)
	if len(entries) < 2 {
		t.Fatalf("expected audit entries, got %d", len(entries))
	}

	for _, entry := range entries {
		etype, _ := entry["event_type"].(string)
		if etype != auditEventRequest {
			t.Fatalf("unexpected audit event type: %q", etype)
		}
		extra, ok := entry["extra"].(map[string]any)
		if !ok {
			t.Fatalf("expected extra object in audit entry: %+v", entry)
		}
		if _, ok := extra["method"]; !ok {
			t.Fatalf("missing method in audit extra: %+v", entry)
		}
		if _, ok := extra["path"]; !ok {
			t.Fatalf("missing path in audit extra: %+v", entry)
		}
		if _, ok := extra["status"]; !ok {
			t.Fatalf("missing status in audit extra: %+v", entry)
		}
	}
}

func readAuditEntries(t *testing.T, root string) []map[string]any {
	t.Helper()
	auditPath := filepath.Join(root, ".deck", "logs", "server-audit.log")
	raw, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	entries := make([]map[string]any, 0)
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		entry := map[string]any{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("parse audit entry: %v", err)
		}
		entries = append(entries, entry)
	}
	return entries
}
