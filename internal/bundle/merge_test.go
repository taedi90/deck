package bundle

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestBundleMergeRejectsHTTP(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "bundle.tar")
	writeMergeBundleTarFixture(t, archivePath)

	_, err := MergeArchive(archivePath, "http://127.0.0.1:8080", false)
	if err == nil {
		t.Fatal("expected HTTP merge destination to be rejected")
	}
	if !strings.Contains(err.Error(), "http merge destinations are not supported") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBundleMergeLocal(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "bundle.tar")
	writeMergeBundleTarFixture(t, archivePath)

	dest := t.TempDir()
	mustWriteFile(t, filepath.Join(dest, "packages", "pkg-a.txt"), []byte("same-package"))
	mustWriteFile(t, filepath.Join(dest, "images", "img-a.tar"), []byte("old-image"))
	mustWriteFile(t, filepath.Join(dest, "files", "deck"), []byte("deck-binary"))
	mustWriteFile(t, filepath.Join(dest, "workflows", "apply.yaml"), []byte("old-apply"))
	mustWriteFile(t, filepath.Join(dest, "workflows", "index.json"), []byte("[\"workflows/existing.yaml\"]\n"))

	report, err := MergeArchive(archivePath, dest, false)
	if err != nil {
		t.Fatalf("merge archive: %v", err)
	}
	if report.DryRun {
		t.Fatalf("expected non dry-run report")
	}

	resolvedDest, err := filepath.Abs(dest)
	if err != nil {
		t.Fatalf("resolve destination: %v", err)
	}
	if report.Destination != resolvedDest {
		t.Fatalf("unexpected destination: got=%q want=%q", report.Destination, resolvedDest)
	}

	actions := map[string]MergeAction{}
	for _, action := range report.Actions {
		actions[action.Path] = action
	}
	if actions["packages/pkg-a.txt"].Action != "skip" {
		t.Fatalf("expected packages/pkg-a.txt skip action, got %q", actions["packages/pkg-a.txt"].Action)
	}
	if actions["images/img-a.tar"].Action != "overwrite" {
		t.Fatalf("expected images/img-a.tar overwrite action, got %q", actions["images/img-a.tar"].Action)
	}
	if actions["files/new.conf"].Action != "upload" {
		t.Fatalf("expected files/new.conf upload action, got %q", actions["files/new.conf"].Action)
	}
	if actions["files/deck"].Action != "skip" {
		t.Fatalf("expected files/deck skip action, got %q", actions["files/deck"].Action)
	}
	if actions["workflows/apply.yaml"].Action != "overwrite" {
		t.Fatalf("expected workflows/apply.yaml overwrite action, got %q", actions["workflows/apply.yaml"].Action)
	}
	if actions["workflows/worker.yaml"].Action != "overwrite" {
		t.Fatalf("expected workflows/worker.yaml overwrite action, got %q", actions["workflows/worker.yaml"].Action)
	}
	if actions["workflows/index.json"].Action != "overwrite" {
		t.Fatalf("expected workflows/index.json overwrite action, got %q", actions["workflows/index.json"].Action)
	}

	if got := mustReadFileString(t, filepath.Join(dest, "images", "img-a.tar")); got != "new-image" {
		t.Fatalf("expected image overwrite, got %q", got)
	}
	if got := mustReadFileString(t, filepath.Join(dest, "files", "new.conf")); got != "new-config" {
		t.Fatalf("expected new config upload, got %q", got)
	}
	if got := mustReadFileString(t, filepath.Join(dest, "workflows", "apply.yaml")); got != "new-apply" {
		t.Fatalf("expected workflow overwrite, got %q", got)
	}

	indexRaw, err := os.ReadFile(filepath.Join(dest, "workflows", "index.json"))
	if err != nil {
		t.Fatalf("read workflow index: %v", err)
	}
	var workflowIndex []string
	if err := json.Unmarshal(indexRaw, &workflowIndex); err != nil {
		t.Fatalf("parse workflow index: %v", err)
	}
	sort.Strings(workflowIndex)
	expectedIndex := []string{"workflows/apply.yaml", "workflows/existing.yaml", "workflows/worker.yaml"}
	if strings.Join(workflowIndex, ",") != strings.Join(expectedIndex, ",") {
		t.Fatalf("unexpected workflow index: got=%v want=%v", workflowIndex, expectedIndex)
	}
}

func writeMergeBundleTarFixture(t *testing.T, archivePath string) {
	t.Helper()

	entries := map[string][]byte{
		"packages/pkg-a.txt":    []byte("same-package"),
		"images/img-a.tar":      []byte("new-image"),
		"files/new.conf":        []byte("new-config"),
		"files/deck":            []byte("deck-binary"),
		"workflows/apply.yaml":  []byte("new-apply"),
		"workflows/worker.yaml": []byte("new-worker"),
	}

	manifestEntries := make([]ManifestEntry, 0, 4)
	for _, rel := range []string{"packages/pkg-a.txt", "images/img-a.tar", "files/new.conf", "files/deck"} {
		body := entries[rel]
		sum := sha256.Sum256(body)
		manifestEntries = append(manifestEntries, ManifestEntry{
			Path:   rel,
			SHA256: hex.EncodeToString(sum[:]),
			Size:   int64(len(body)),
		})
	}
	manifestRaw, err := json.Marshal(ManifestFile{Entries: manifestEntries})
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}

	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer func() { _ = f.Close() }()

	tw := tar.NewWriter(f)
	defer func() { _ = tw.Close() }()

	orderedPaths := []string{".deck/manifest.json", "packages/pkg-a.txt", "images/img-a.tar", "files/new.conf", "files/deck", "workflows/apply.yaml", "workflows/worker.yaml"}
	for _, rel := range orderedPaths {
		name := path.Join("bundle", rel)
		body := entries[rel]
		if rel == ".deck/manifest.json" {
			body = manifestRaw
		}
		h := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatalf("write tar header %s: %v", name, err)
		}
		if _, err := io.Copy(tw, bytes.NewReader(body)); err != nil {
			t.Fatalf("write tar body %s: %v", name, err)
		}
	}
}

func mustWriteFile(t *testing.T, filePath string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(filePath), err)
	}
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", filePath, err)
	}
}

func mustReadFileString(t *testing.T, filePath string) string {
	t.Helper()
	raw, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read %s: %v", filePath, err)
	}
	return string(raw)
}
