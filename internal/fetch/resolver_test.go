package fetch

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveBytes(t *testing.T) {
	t.Run("uses local source first", func(t *testing.T) {
		local := t.TempDir()
		if err := os.MkdirAll(filepath.Join(local, "files"), 0o755); err != nil {
			t.Fatalf("mkdir local files: %v", err)
		}
		if err := os.WriteFile(filepath.Join(local, "files", "a.txt"), []byte("local"), 0o644); err != nil {
			t.Fatalf("write local file: %v", err)
		}

		raw, err := ResolveBytes("files/a.txt", []SourceConfig{{Type: "local", Path: local}}, ResolveOptions{})
		if err != nil {
			t.Fatalf("resolve bytes: %v", err)
		}
		if string(raw) != "local" {
			t.Fatalf("unexpected content: %q", string(raw))
		}
	})

	t.Run("falls back to repo then online", func(t *testing.T) {
		repo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		defer repo.Close()

		online := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/files/a.txt" {
				_, _ = w.Write([]byte("online"))
				return
			}
			http.NotFound(w, r)
		}))
		defer online.Close()

		raw, err := ResolveBytes("files/a.txt", []SourceConfig{
			{Type: "repo", URL: repo.URL},
			{Type: "online", URL: online.URL},
		}, ResolveOptions{})
		if err != nil {
			t.Fatalf("resolve bytes: %v", err)
		}
		if string(raw) != "online" {
			t.Fatalf("unexpected content: %q", string(raw))
		}
	})

	t.Run("returns deterministic miss error", func(t *testing.T) {
		_, err := ResolveBytes("files/missing.txt", []SourceConfig{{Type: "local", Path: t.TempDir()}}, ResolveOptions{})
		if err == nil {
			t.Fatalf("expected resolve error")
		}
		if !strings.Contains(err.Error(), "all fetch sources failed") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("blocks online in offline-only mode", func(t *testing.T) {
		online := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("online"))
		}))
		defer online.Close()

		_, err := ResolveBytes("files/a.txt", []SourceConfig{{Type: "online", URL: online.URL}}, ResolveOptions{OfflineOnly: true})
		if err == nil {
			t.Fatalf("expected offline policy error")
		}
		if !strings.Contains(err.Error(), "blocked-by-offline-policy") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
