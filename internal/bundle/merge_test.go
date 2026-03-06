package bundle

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

func TestBundleMergeHTTP(t *testing.T) {
	type storedFile struct {
		body []byte
		etag string
	}

	files := map[string]storedFile{}
	putCount := map[string]int{}
	headCount := map[string]int{}

	setFile := func(rel string, body []byte) {
		sum := sha256.Sum256(body)
		files[rel] = storedFile{body: append([]byte{}, body...), etag: "\"sha256:" + hex.EncodeToString(sum[:]) + "\""}
	}

	setFile("packages/pkg-a.txt", []byte("same-package"))
	setFile("images/img-a.tar", []byte("old-image"))
	setFile("files/deck", []byte("deck-binary"))
	setFile("workflows/apply.yaml", []byte("old-apply"))
	setFile("workflows/index.json", []byte("[\"workflows/existing.yaml\"]\n"))

	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(path.Clean(r.URL.Path), "/")

		mu.Lock()
		defer mu.Unlock()

		switch r.Method {
		case http.MethodHead:
			headCount[rel]++
			stored, ok := files[rel]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("ETag", stored.etag)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			stored, ok := files[rel]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(stored.body)
		case http.MethodPut:
			putCount[rel]++
			body, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			setFile(rel, body)
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	archivePath := filepath.Join(t.TempDir(), "bundle.tar")
	writeMergeBundleTarFixture(t, archivePath)

	report, err := MergeArchive(archivePath, server.URL+"/", false)
	if err != nil {
		t.Fatalf("merge archive: %v", err)
	}
	if report.DryRun {
		t.Fatalf("expected non dry-run report")
	}

	mu.Lock()
	defer mu.Unlock()

	if putCount["packages/pkg-a.txt"] != 0 {
		t.Fatalf("expected packages/pkg-a.txt skip, put=%d", putCount["packages/pkg-a.txt"])
	}
	if putCount["images/img-a.tar"] != 1 {
		t.Fatalf("expected images/img-a.tar overwrite, put=%d", putCount["images/img-a.tar"])
	}
	if putCount["files/new.conf"] != 1 {
		t.Fatalf("expected files/new.conf upload, put=%d", putCount["files/new.conf"])
	}
	if putCount["files/deck"] != 0 {
		t.Fatalf("expected files/deck skip on hash match, put=%d", putCount["files/deck"])
	}
	if putCount["workflows/apply.yaml"] != 1 {
		t.Fatalf("expected workflows/apply.yaml always overwrite, put=%d", putCount["workflows/apply.yaml"])
	}
	if putCount["workflows/worker.yaml"] != 1 {
		t.Fatalf("expected workflows/worker.yaml upload, put=%d", putCount["workflows/worker.yaml"])
	}
	if putCount["workflows/index.json"] != 1 {
		t.Fatalf("expected workflows/index.json write, put=%d", putCount["workflows/index.json"])
	}

	if headCount["packages/pkg-a.txt"] == 0 || headCount["images/img-a.tar"] == 0 || headCount["files/new.conf"] == 0 {
		t.Fatalf("expected HEAD checks for manifest entries, got %#v", headCount)
	}

	if string(files["images/img-a.tar"].body) != "new-image" {
		t.Fatalf("expected image to be overwritten")
	}
	if string(files["files/new.conf"].body) != "new-config" {
		t.Fatalf("expected new file to be uploaded")
	}
	if string(files["workflows/apply.yaml"].body) != "new-apply" {
		t.Fatalf("expected workflow apply overwrite")
	}

	var workflowIndex []string
	if err := json.Unmarshal(files["workflows/index.json"].body, &workflowIndex); err != nil {
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
	defer f.Close()

	tw := tar.NewWriter(f)
	defer tw.Close()

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
