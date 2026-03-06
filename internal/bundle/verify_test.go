package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyManifest(t *testing.T) {
	t.Run("fails when only legacy root manifest exists", func(t *testing.T) {
		root := t.TempDir()
		writeBundleFile(t, root, "files/a.txt", []byte("ok"))

		sum := sha256.Sum256([]byte("ok"))
		legacyRaw, err := json.Marshal(ManifestFile{Entries: []ManifestEntry{{
			Path:   "files/a.txt",
			SHA256: hex.EncodeToString(sum[:]),
			Size:   2,
		}}})
		if err != nil {
			t.Fatalf("marshal legacy manifest: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "manifest.json"), legacyRaw, 0o644); err != nil {
			t.Fatalf("write legacy manifest: %v", err)
		}

		err = VerifyManifest(root)
		if err == nil {
			t.Fatalf("expected missing new-location manifest error")
		}
		if !strings.Contains(err.Error(), "E_MANIFEST_MISSING") {
			t.Fatalf("expected E_MANIFEST_MISSING, got %v", err)
		}
	})

	t.Run("passes for valid manifest", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "files"), 0o755); err != nil {
			t.Fatalf("mkdir files: %v", err)
		}
		content := []byte("ok")
		if err := os.WriteFile(filepath.Join(root, "files", "a.txt"), content, 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		if err := writeManifest(root, "files/a.txt", content); err != nil {
			t.Fatalf("write manifest: %v", err)
		}

		if err := VerifyManifest(root); err != nil {
			t.Fatalf("expected valid manifest, got %v", err)
		}
	})

	t.Run("fails on hash mismatch", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "files"), 0o755); err != nil {
			t.Fatalf("mkdir files: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "files", "a.txt"), []byte("ok"), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		if err := writeManifest(root, "files/a.txt", []byte("different")); err != nil {
			t.Fatalf("write manifest: %v", err)
		}

		err := VerifyManifest(root)
		if err == nil {
			t.Fatalf("expected integrity error")
		}
		if !strings.Contains(err.Error(), "E_BUNDLE_INTEGRITY") {
			t.Fatalf("expected E_BUNDLE_INTEGRITY, got %v", err)
		}
	})

	t.Run("fails when apt metadata missing from manifest", func(t *testing.T) {
		root := t.TempDir()
		writeBundleFile(t, root, "packages/apt/jammy/Release", []byte("release"))
		writeBundleFile(t, root, "packages/apt/jammy/Packages.gz", []byte("packages"))

		if err := writeManifestForPaths(root, "packages/apt/jammy/Packages.gz"); err != nil {
			t.Fatalf("write manifest: %v", err)
		}

		err := VerifyManifest(root)
		if err == nil {
			t.Fatalf("expected manifest coverage error")
		}
		if !strings.Contains(err.Error(), "required offline artifact missing from manifest") {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(err.Error(), "packages/apt/jammy/Release") {
			t.Fatalf("expected missing Release path, got %v", err)
		}
	})

	t.Run("fails when yum metadata missing from manifest", func(t *testing.T) {
		root := t.TempDir()
		writeBundleFile(t, root, "files/a.txt", []byte("ok"))
		writeBundleFile(t, root, "packages/yum/el8/repodata/repomd.xml", []byte("repomd"))

		if err := writeManifestForPaths(root, "files/a.txt"); err != nil {
			t.Fatalf("write manifest: %v", err)
		}

		err := VerifyManifest(root)
		if err == nil {
			t.Fatalf("expected manifest coverage error")
		}
		if !strings.Contains(err.Error(), "required offline artifact missing from manifest") {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(err.Error(), "packages/yum/el8/repodata/repomd.xml") {
			t.Fatalf("expected missing repomd path, got %v", err)
		}
	})

	t.Run("fails when image tar missing from manifest", func(t *testing.T) {
		root := t.TempDir()
		writeBundleFile(t, root, "files/a.txt", []byte("ok"))
		writeBundleFile(t, root, "images/k8s.tar", []byte("image"))

		if err := writeManifestForPaths(root, "files/a.txt"); err != nil {
			t.Fatalf("write manifest: %v", err)
		}

		err := VerifyManifest(root)
		if err == nil {
			t.Fatalf("expected manifest coverage error")
		}
		if !strings.Contains(err.Error(), "required offline artifact missing from manifest") {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(err.Error(), "images/k8s.tar") {
			t.Fatalf("expected missing image tar path, got %v", err)
		}
	})

	t.Run("passes when offline artifacts are included in manifest", func(t *testing.T) {
		root := t.TempDir()
		paths := []string{
			"packages/apt/noble/Release",
			"packages/apt/noble/Packages.gz",
			"packages/apt-k8s/v1.32/Release",
			"packages/apt-k8s/v1.32/Packages.gz",
			"packages/yum/el9/repodata/repomd.xml",
			"packages/yum-k8s/el9-k8s/repodata/repomd.xml",
			"images/pause.tar",
		}
		for i, p := range paths {
			writeBundleFile(t, root, p, []byte(fmt.Sprintf("content-%d", i)))
		}

		if err := writeManifestForPaths(root, paths...); err != nil {
			t.Fatalf("write manifest: %v", err)
		}

		if err := VerifyManifest(root); err != nil {
			t.Fatalf("expected valid offline bundle, got %v", err)
		}
	})
}

func TestVerifyManifest_NewLocation(t *testing.T) {
	root := t.TempDir()
	writeBundleFile(t, root, "files/a.txt", []byte("ok"))

	if err := writeManifestForPaths(root, "files/a.txt"); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := VerifyManifest(root); err != nil {
		t.Fatalf("expected verify success with .deck/manifest.json, got %v", err)
	}
}

func writeManifest(root, rel string, content []byte) error {
	sum := sha256.Sum256(content)
	mf := ManifestFile{Entries: []ManifestEntry{{
		Path:   rel,
		SHA256: hex.EncodeToString(sum[:]),
		Size:   int64(len(content)),
	}}}
	raw, err := json.Marshal(mf)
	if err != nil {
		return err
	}
	manifestPath := filepath.Join(root, filepath.FromSlash(manifestRelativePath))
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(manifestPath, raw, 0o644)
}

func writeBundleFile(t *testing.T, root, rel string, content []byte) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", rel, err)
	}
	if err := os.WriteFile(full, content, 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func writeManifestForPaths(root string, relPaths ...string) error {
	entries := make([]ManifestEntry, 0, len(relPaths))
	for _, rel := range relPaths {
		full := filepath.Join(root, filepath.FromSlash(rel))
		content, err := os.ReadFile(full)
		if err != nil {
			return fmt.Errorf("read %s: %w", rel, err)
		}
		sum := sha256.Sum256(content)
		entries = append(entries, ManifestEntry{
			Path:   filepath.ToSlash(rel),
			SHA256: hex.EncodeToString(sum[:]),
			Size:   int64(len(content)),
		})
	}
	raw, err := json.Marshal(ManifestFile{Entries: entries})
	if err != nil {
		return err
	}
	manifestPath := filepath.Join(root, filepath.FromSlash(manifestRelativePath))
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(manifestPath, raw, 0o644)
}

func TestBundleVerifyTar(t *testing.T) {
	t.Run("verifies tar bundle from stream", func(t *testing.T) {
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

		if err := VerifyManifest(archive); err != nil {
			t.Fatalf("expected tar verify success, got %v", err)
		}
	})

	t.Run("fails when tar manifest is missing", func(t *testing.T) {
		archive := filepath.Join(t.TempDir(), "bundle.tar")
		if err := writeTarArchive(archive, []tarEntry{{name: "bundle/files/dummy.txt", body: []byte("ok\n")}}); err != nil {
			t.Fatalf("write tar: %v", err)
		}

		err := VerifyManifest(archive)
		if err == nil {
			t.Fatal("expected missing manifest error")
		}
		if !strings.Contains(err.Error(), "E_MANIFEST_MISSING") {
			t.Fatalf("expected E_MANIFEST_MISSING, got %v", err)
		}
	})

	t.Run("fails when tar artifact hash mismatches", func(t *testing.T) {
		archive := filepath.Join(t.TempDir(), "bundle.tar")
		manifestRaw, err := json.Marshal(ManifestFile{Entries: []ManifestEntry{{
			Path:   "files/dummy.txt",
			SHA256: strings.Repeat("0", 64),
			Size:   int64(len("ok\n")),
		}}})
		if err != nil {
			t.Fatalf("marshal manifest: %v", err)
		}

		if err := writeTarArchive(archive, []tarEntry{
			{name: "bundle/.deck/manifest.json", body: manifestRaw},
			{name: "bundle/files/dummy.txt", body: []byte("ok\n")},
		}); err != nil {
			t.Fatalf("write tar: %v", err)
		}

		err = VerifyManifest(archive)
		if err == nil {
			t.Fatal("expected integrity error")
		}
		if !strings.Contains(err.Error(), "sha256 mismatch") {
			t.Fatalf("expected sha256 mismatch, got %v", err)
		}
	})
}
