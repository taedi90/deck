package bundle

import (
	"archive/tar"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportArchive(t *testing.T) {
	t.Run("imports regular files", func(t *testing.T) {
		root := t.TempDir()
		archive := filepath.Join(root, "bundle.tar")
		dest := filepath.Join(root, "imported")

		if err := writeTarArchive(archive, []tarEntry{
			{name: "bundle/.deck/manifest.json", body: []byte("{}")},
			{name: "bundle/files/a.txt", body: []byte("hello")},
		}); err != nil {
			t.Fatalf("write tar: %v", err)
		}

		if err := ImportArchive(archive, dest); err != nil {
			t.Fatalf("import archive: %v", err)
		}

		raw, err := os.ReadFile(filepath.Join(dest, "bundle", "files", "a.txt"))
		if err != nil {
			t.Fatalf("read imported file: %v", err)
		}
		if string(raw) != "hello" {
			t.Fatalf("unexpected imported content: %q", string(raw))
		}
	})

	t.Run("rejects path traversal", func(t *testing.T) {
		root := t.TempDir()
		archive := filepath.Join(root, "bundle.tar")
		dest := filepath.Join(root, "imported")

		if err := writeTarArchive(archive, []tarEntry{{name: "../evil.txt", body: []byte("x")}}); err != nil {
			t.Fatalf("write tar: %v", err)
		}

		err := ImportArchive(archive, dest)
		if err == nil {
			t.Fatalf("expected traversal error")
		}
		if !strings.Contains(err.Error(), errCodeBundleImportTraversal) {
			t.Fatalf("expected %s, got %v", errCodeBundleImportTraversal, err)
		}
	})

	t.Run("rejects archive root entries without bundle prefix", func(t *testing.T) {
		root := t.TempDir()
		archive := filepath.Join(root, "bundle.tar")
		dest := filepath.Join(root, "imported")

		if err := writeTarArchive(archive, []tarEntry{{name: "manifest.json", body: []byte("x")}}); err != nil {
			t.Fatalf("write tar: %v", err)
		}

		err := ImportArchive(archive, dest)
		if err == nil {
			t.Fatalf("expected prefix error")
		}
		if !strings.Contains(err.Error(), errCodeBundleImportPrefix) {
			t.Fatalf("expected %s, got %v", errCodeBundleImportPrefix, err)
		}
	})
}

type tarEntry struct {
	name string
	body []byte
}

func writeTarArchive(path string, entries []tarEntry) error {
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
