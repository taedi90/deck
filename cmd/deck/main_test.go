package main

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunApply(t *testing.T) {
	t.Run("success with preflight", func(t *testing.T) {
		root := t.TempDir()
		bundle := filepath.Join(root, "bundle")
		sourceFile := filepath.Join(root, "source.bin")
		stateFile := filepath.Join(root, "state", "state.json")
		if err := os.WriteFile(sourceFile, []byte("hello"), 0o644); err != nil {
			t.Fatalf("write source file: %v", err)
		}

		workflowPath := filepath.Join(root, "workflow.yaml")
		workflow := fmt.Sprintf(`version: v1
context:
  stateFile: %s
phases:
  - name: prepare
    steps:
      - id: download
        apiVersion: deck/v1
        kind: DownloadFile
        spec:
          source:
            path: %s
          output:
            path: files/fetched.bin
  - name: install
    steps:
      - id: run
        apiVersion: deck/v1
        kind: RunCommand
        spec:
          command: ["true"]
`, stateFile, sourceFile)
		if err := os.WriteFile(workflowPath, []byte(workflow), 0o644); err != nil {
			t.Fatalf("write workflow: %v", err)
		}

		if err := run([]string{"apply", "--file", workflowPath, "--bundle", bundle}); err != nil {
			t.Fatalf("expected apply success, got %v", err)
		}

		if _, err := os.Stat(filepath.Join(bundle, "manifest.json")); err != nil {
			t.Fatalf("expected manifest generated: %v", err)
		}
		if _, err := os.Stat(filepath.Join(stateFile)); err != nil {
			t.Fatalf("expected state file generated: %v", err)
		}
	})

	t.Run("preflight failure unless skipped", func(t *testing.T) {
		root := t.TempDir()
		bundle := filepath.Join(root, "bundle")
		sourceFile := filepath.Join(root, "source.bin")
		if err := os.WriteFile(sourceFile, []byte("hello"), 0o644); err != nil {
			t.Fatalf("write source file: %v", err)
		}

		workflowPath := filepath.Join(root, "workflow.yaml")
		workflow := fmt.Sprintf(`version: v1
phases:
  - name: prepare
    steps:
      - id: download
        apiVersion: deck/v1
        kind: DownloadFile
        spec:
          source:
            path: %s
          output:
            path: files/fetched.bin
  - name: install
    steps:
      - id: run
        apiVersion: deck/v1
        kind: RunCommand
        spec:
          command: ["true"]
`, sourceFile)
		if err := os.WriteFile(workflowPath, []byte(workflow), 0o644); err != nil {
			t.Fatalf("write workflow: %v", err)
		}

		err := run([]string{"apply", "--file", workflowPath, "--bundle", bundle})
		if err == nil {
			t.Fatalf("expected preflight failure")
		}
		if !strings.Contains(err.Error(), "preflight failed") {
			t.Fatalf("unexpected error: %v", err)
		}

		if err := run([]string{"apply", "--file", workflowPath, "--bundle", bundle, "--skip-preflight"}); err != nil {
			t.Fatalf("expected success with --skip-preflight, got %v", err)
		}
	})
}

func TestRunBundleVerify(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		bundle := t.TempDir()
		if err := os.MkdirAll(filepath.Join(bundle, "files"), 0o755); err != nil {
			t.Fatalf("mkdir files: %v", err)
		}
		content := []byte("ok")
		if err := os.WriteFile(filepath.Join(bundle, "files", "a.txt"), content, 0o644); err != nil {
			t.Fatalf("write artifact: %v", err)
		}
		if err := writeManifestForMainTest(bundle, "files/a.txt", content); err != nil {
			t.Fatalf("write manifest: %v", err)
		}

		if err := run([]string{"bundle", "verify", "--bundle", bundle}); err != nil {
			t.Fatalf("expected success, got %v", err)
		}
	})

	t.Run("integrity failure", func(t *testing.T) {
		bundle := t.TempDir()
		if err := os.MkdirAll(filepath.Join(bundle, "files"), 0o755); err != nil {
			t.Fatalf("mkdir files: %v", err)
		}
		if err := os.WriteFile(filepath.Join(bundle, "files", "a.txt"), []byte("ok"), 0o644); err != nil {
			t.Fatalf("write artifact: %v", err)
		}
		if err := writeManifestForMainTest(bundle, "files/a.txt", []byte("different")); err != nil {
			t.Fatalf("write manifest: %v", err)
		}

		err := run([]string{"bundle", "verify", "--bundle", bundle})
		if err == nil {
			t.Fatalf("expected failure")
		}
		if !strings.Contains(err.Error(), "E_BUNDLE_INTEGRITY") {
			t.Fatalf("expected E_BUNDLE_INTEGRITY, got %v", err)
		}
	})
}

func TestRunBundleImport(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		root := t.TempDir()
		archive := filepath.Join(root, "bundle.tar")
		dest := filepath.Join(root, "imported")

		if err := writeTarForMainTest(archive, []tarTestEntry{{name: "files/a.txt", body: []byte("hello")}}); err != nil {
			t.Fatalf("write tar: %v", err)
		}

		if err := run([]string{"bundle", "import", "--file", archive, "--dest", dest}); err != nil {
			t.Fatalf("expected success, got %v", err)
		}

		raw, err := os.ReadFile(filepath.Join(dest, "files", "a.txt"))
		if err != nil {
			t.Fatalf("read imported file: %v", err)
		}
		if string(raw) != "hello" {
			t.Fatalf("unexpected imported content: %q", string(raw))
		}
	})

	t.Run("path traversal failure", func(t *testing.T) {
		root := t.TempDir()
		archive := filepath.Join(root, "bundle.tar")
		dest := filepath.Join(root, "imported")

		if err := writeTarForMainTest(archive, []tarTestEntry{{name: "../evil.txt", body: []byte("x")}}); err != nil {
			t.Fatalf("write tar: %v", err)
		}

		err := run([]string{"bundle", "import", "--file", archive, "--dest", dest})
		if err == nil {
			t.Fatalf("expected failure")
		}
		if !strings.Contains(err.Error(), "E_BUNDLE_IMPORT_PATH_TRAVERSAL") {
			t.Fatalf("expected traversal error code, got %v", err)
		}
	})
}

func TestRunBundleCollect(t *testing.T) {
	root := t.TempDir()
	bundleDir := filepath.Join(root, "bundle")
	if err := os.MkdirAll(filepath.Join(bundleDir, "files"), 0o755); err != nil {
		t.Fatalf("mkdir files: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "files", "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write bundle file: %v", err)
	}

	out := filepath.Join(root, "bundle.tar")
	if err := run([]string{"bundle", "collect", "--bundle", bundleDir, "--output", out}); err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if _, err := os.Stat(out); err != nil {
		t.Fatalf("expected output archive: %v", err)
	}
}

func TestRunServerUsageValidation(t *testing.T) {
	err := run([]string{"server"})
	if err == nil {
		t.Fatalf("expected usage error")
	}
	if !strings.Contains(err.Error(), "usage: deck server start") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunServerTLSFlagValidation(t *testing.T) {
	err := run([]string{"server", "start", "--tls-cert", "server.crt"})
	if err == nil {
		t.Fatalf("expected tls flag validation error")
	}
	if !strings.Contains(err.Error(), "--tls-cert and --tls-key must be provided together") {
		t.Fatalf("unexpected error: %v", err)
	}

	err = run([]string{"server", "start", "--tls-key", "server.key"})
	if err == nil {
		t.Fatalf("expected tls flag validation error")
	}
	if !strings.Contains(err.Error(), "--tls-cert and --tls-key must be provided together") {
		t.Fatalf("unexpected error: %v", err)
	}

	err = run([]string{"server", "start", "--tls-self-signed", "--tls-cert", "server.crt", "--tls-key", "server.key"})
	if err == nil {
		t.Fatalf("expected self-signed conflict error")
	}
	if !strings.Contains(err.Error(), "--tls-self-signed cannot be combined") {
		t.Fatalf("unexpected error: %v", err)
	}

	err = run([]string{"server", "start", "--report-max", "0"})
	if err == nil {
		t.Fatalf("expected report-max validation error")
	}
	if !strings.Contains(err.Error(), "--report-max must be > 0") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunAgent(t *testing.T) {
	t.Run("usage validation", func(t *testing.T) {
		err := run([]string{"agent"})
		if err == nil {
			t.Fatalf("expected usage error")
		}
		if !strings.Contains(err.Error(), "usage: deck agent start") {
			t.Fatalf("unexpected error: %v", err)
		}

		err = run([]string{"agent", "start"})
		if err == nil {
			t.Fatalf("expected --server required error")
		}
		if !strings.Contains(err.Error(), "--server is required") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("once heartbeat success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/agent/heartbeat":
				w.WriteHeader(http.StatusOK)
			case "/api/agent/lease":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"status":"ok","job":null}`))
			default:
				http.NotFound(w, r)
			}
		}))
		defer srv.Close()

		err := run([]string{"agent", "start", "--server", srv.URL, "--once"})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
	})
}

func writeManifestForMainTest(bundleRoot, rel string, content []byte) error {
	sum := sha256.Sum256(content)
	manifest := map[string]any{
		"entries": []any{map[string]any{
			"path":   rel,
			"sha256": hex.EncodeToString(sum[:]),
			"size":   len(content),
		}},
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(bundleRoot, "manifest.json"), raw, 0o644)
}

type tarTestEntry struct {
	name string
	body []byte
}

func writeTarForMainTest(path string, entries []tarTestEntry) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	tw := tar.NewWriter(f)
	defer tw.Close()

	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.body)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if _, err := tw.Write(e.body); err != nil {
			return err
		}
	}
	return nil
}
