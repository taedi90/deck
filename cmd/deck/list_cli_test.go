package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListScenarios(t *testing.T) {
	t.Run("local json", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "workflows", "scenarios", "nested"), 0o755); err != nil {
			t.Fatalf("mkdir scenarios: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "workflows", "scenarios", "apply.yaml"), []byte("version: v1alpha1\nsteps: []\n"), 0o644); err != nil {
			t.Fatalf("write apply: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "workflows", "scenarios", "nested", "extra.yaml"), []byte("version: v1alpha1\nsteps: []\n"), 0o644); err != nil {
			t.Fatalf("write extra: %v", err)
		}

		oldWD, _ := os.Getwd()
		if err := os.Chdir(root); err != nil {
			t.Fatalf("chdir: %v", err)
		}
		defer func() { _ = os.Chdir(oldWD) }()

		out, err := runWithCapturedStdout([]string{"list", "--source", "local", "-o", "json"})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		var got []scenarioEntry
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("decode output: %v", err)
		}
		if len(got) != 2 || got[0].Source != scenarioSourceLocal {
			t.Fatalf("unexpected output: %#v", got)
		}
	})

	t.Run("server text", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/workflows/index.json" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			_ = json.NewEncoder(w).Encode([]string{"apply", "nested/extra"})
		}))
		defer srv.Close()
		t.Setenv("DECK_SERVER_CONFIG_PATH", filepath.Join(t.TempDir(), "server.json"))
		if _, err := runWithCapturedStdout([]string{"server", "remote", "set", srv.URL}); err != nil {
			t.Fatalf("server remote set failed: %v", err)
		}

		out, err := runWithCapturedStdout([]string{"list", "--source", "server"})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		for _, want := range []string{"server\tapply\t" + srv.URL + "/workflows/scenarios/apply.yaml", "server\tnested/extra\t" + srv.URL + "/workflows/scenarios/nested/extra.yaml"} {
			if !strings.Contains(out, want) {
				t.Fatalf("expected %q in output, got %q", want, out)
			}
		}
	})

	t.Run("all without saved remote falls back to local only", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "workflows", "scenarios"), 0o755); err != nil {
			t.Fatalf("mkdir scenarios: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "workflows", "scenarios", "apply.yaml"), []byte("version: v1alpha1\nsteps: []\n"), 0o644); err != nil {
			t.Fatalf("write apply: %v", err)
		}
		t.Setenv("DECK_SERVER_CONFIG_PATH", filepath.Join(t.TempDir(), "server.json"))

		oldWD, _ := os.Getwd()
		if err := os.Chdir(root); err != nil {
			t.Fatalf("chdir: %v", err)
		}
		defer func() { _ = os.Chdir(oldWD) }()

		out, err := runWithCapturedStdout([]string{"list"})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if !strings.Contains(out, "local\tapply\t") {
			t.Fatalf("unexpected output: %q", out)
		}
	})
}
