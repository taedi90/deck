package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAuditSchema(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "files"), 0o755); err != nil {
		t.Fatalf("mkdir files: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "files", "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	h, err := NewHandler(root, HandlerOptions{})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/files/a.txt", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	entries := readAuditEntries(t, root)
	if len(entries) == 0 {
		t.Fatalf("expected audit entries")
	}

	for _, entry := range entries {
		requireAuditSchemaFields(t, entry)
	}
}

func requireAuditSchemaFields(t *testing.T, entry map[string]any) {
	t.Helper()
	for _, key := range []string{"ts", "schema_version", "source", "event_type", "level", "message"} {
		if _, ok := entry[key]; !ok {
			t.Fatalf("missing required audit key %s in %+v", key, entry)
		}
	}

	ts, _ := entry["ts"].(string)
	if _, err := time.Parse(time.RFC3339Nano, ts); err != nil {
		t.Fatalf("invalid ts format %q: %v", ts, err)
	}

	schemaVersion, ok := entry["schema_version"].(float64)
	if !ok || int(schemaVersion) != auditSchemaVersion {
		t.Fatalf("expected schema_version=%d, got %+v", auditSchemaVersion, entry["schema_version"])
	}

	source, _ := entry["source"].(string)
	if source != auditSourceServer {
		t.Fatalf("expected source=%q, got %q", auditSourceServer, source)
	}
}
