package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestBundleInspectTar(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "bundle.tar")
	content := []byte("ok\n")
	sum := sha256.Sum256(content)
	manifestRaw, err := json.Marshal(ManifestFile{Entries: []ManifestEntry{{
		Path:   "files/dummy.txt",
		SHA256: hex.EncodeToString(sum[:]),
		Size:   int64(len(content)),
	}}})
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}

	if err := writeTarArchive(archive, []tarEntry{
		{name: "bundle/.deck/manifest.json", body: manifestRaw},
		{name: "bundle/files/dummy.txt", body: content},
	}); err != nil {
		t.Fatalf("write tar: %v", err)
	}

	entries, err := InspectManifest(archive)
	if err != nil {
		t.Fatalf("inspect tar manifest: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Path != "files/dummy.txt" {
		t.Fatalf("unexpected entry path: %s", entries[0].Path)
	}
}
