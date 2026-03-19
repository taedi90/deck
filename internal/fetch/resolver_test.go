package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func nilContextForTest() context.Context { return nil }

func TestResolveBytes(t *testing.T) {
	t.Run("rejects nil context", func(t *testing.T) {
		_, err := ResolveBytes(nilContextForTest(), "files/a.txt", nil, ResolveOptions{})
		if err == nil {
			t.Fatalf("expected nil context error")
		}
		if !strings.Contains(err.Error(), "context is nil") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("uses local source first", func(t *testing.T) {
		local := t.TempDir()
		if err := os.MkdirAll(filepath.Join(local, "files"), 0o755); err != nil {
			t.Fatalf("mkdir local files: %v", err)
		}
		if err := os.WriteFile(filepath.Join(local, "files", "a.txt"), []byte("local"), 0o644); err != nil {
			t.Fatalf("write local file: %v", err)
		}

		raw, err := ResolveBytes(context.Background(), "files/a.txt", []SourceConfig{{Type: "local", Path: local}}, ResolveOptions{})
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

		raw, err := ResolveBytes(context.Background(), "files/a.txt", []SourceConfig{
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
		_, err := ResolveBytes(context.Background(), "files/missing.txt", []SourceConfig{{Type: "local", Path: t.TempDir()}}, ResolveOptions{})
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

		_, err := ResolveBytes(context.Background(), "files/a.txt", []SourceConfig{{Type: "online", URL: online.URL}}, ResolveOptions{OfflineOnly: true})
		if err == nil {
			t.Fatalf("expected offline policy error")
		}
		if !strings.Contains(err.Error(), "blocked-by-offline-policy") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects oversized http response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("0123456789ABCDEF"))
		}))
		defer srv.Close()

		_, err := ResolveBytes(context.Background(), "files/a.txt", []SourceConfig{{Type: "online", URL: srv.URL}}, ResolveOptions{MaxBytes: 10, Timeout: time.Second})
		if err == nil {
			t.Fatalf("expected max-bytes error")
		}
		if !strings.Contains(err.Error(), "exceeds max bytes") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("returns context cancellation when parent is done", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(250 * time.Millisecond)
			_, _ = w.Write([]byte("online"))
		}))
		defer srv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()

		_, err := ResolveBytes(ctx, "files/a.txt", []SourceConfig{{Type: "online", URL: srv.URL}}, ResolveOptions{Timeout: time.Second})
		if err == nil {
			t.Fatalf("expected cancellation error")
		}
		if !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
			t.Fatalf("expected context deadline exceeded text, got %v", err)
		}
	})
}
