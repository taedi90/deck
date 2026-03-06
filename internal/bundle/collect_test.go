package bundle

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectArchive(t *testing.T) {
	t.Run("creates archive with bundle files", func(t *testing.T) {
		root := t.TempDir()
		bundleRoot := filepath.Join(root, "bundle")
		if err := os.MkdirAll(filepath.Join(bundleRoot, "files"), 0o755); err != nil {
			t.Fatalf("mkdir bundle files: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(bundleRoot, ".deck"), 0o755); err != nil {
			t.Fatalf("mkdir manifest dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(bundleRoot, ".deck", "manifest.json"), []byte(`{"entries":[]}`), 0o644); err != nil {
			t.Fatalf("write manifest: %v", err)
		}
		if err := os.WriteFile(filepath.Join(bundleRoot, "files", "a.txt"), []byte("hello"), 0o644); err != nil {
			t.Fatalf("write bundle file: %v", err)
		}

		out := filepath.Join(root, "bundle.tar")
		if err := CollectArchive(bundleRoot, out); err != nil {
			t.Fatalf("collect archive: %v", err)
		}

		names, err := tarEntryNames(out)
		if err != nil {
			t.Fatalf("read tar entries: %v", err)
		}
		if !containsName(names, "bundle/.deck/manifest.json") {
			t.Fatalf("expected bundle/.deck/manifest.json in archive, got %#v", names)
		}
		if !containsName(names, "bundle/files/a.txt") {
			t.Fatalf("expected bundle/files/a.txt in archive, got %#v", names)
		}
	})

	t.Run("excludes output when output inside bundle root", func(t *testing.T) {
		root := t.TempDir()
		bundleRoot := filepath.Join(root, "bundle")
		if err := os.MkdirAll(bundleRoot, 0o755); err != nil {
			t.Fatalf("mkdir bundle root: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(bundleRoot, ".deck"), 0o755); err != nil {
			t.Fatalf("mkdir manifest dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(bundleRoot, ".deck", "manifest.json"), []byte(`{"entries":[]}`), 0o644); err != nil {
			t.Fatalf("write manifest: %v", err)
		}

		out := filepath.Join(bundleRoot, "bundle.tar")
		if err := CollectArchive(bundleRoot, out); err != nil {
			t.Fatalf("collect archive: %v", err)
		}

		names, err := tarEntryNames(out)
		if err != nil {
			t.Fatalf("read tar entries: %v", err)
		}
		for _, n := range names {
			if strings.Contains(n, "bundle.tar") {
				t.Fatalf("archive should not contain itself: %#v", names)
			}
		}
	})
}

func TestBundleTarPrefix(t *testing.T) {
	root := t.TempDir()
	bundleRoot := filepath.Join(root, "bundle")
	if err := os.MkdirAll(filepath.Join(bundleRoot, "files"), 0o755); err != nil {
		t.Fatalf("mkdir bundle files: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(bundleRoot, ".deck"), 0o755); err != nil {
		t.Fatalf("mkdir manifest dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleRoot, ".deck", "manifest.json"), []byte(`{"entries":[]}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleRoot, "files", "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write bundle file: %v", err)
	}

	archivePath := filepath.Join(root, "bundle.tar")
	if err := CollectArchive(bundleRoot, archivePath); err != nil {
		t.Fatalf("collect archive: %v", err)
	}

	names, err := tarEntryNames(archivePath)
	if err != nil {
		t.Fatalf("read tar entries: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("expected archive entries")
	}
	for _, name := range names {
		if !strings.HasPrefix(name, "bundle/") {
			t.Fatalf("entry must start with bundle/: %q", name)
		}
	}

	dest := filepath.Join(root, "imported")
	if err := ImportArchive(archivePath, dest); err != nil {
		t.Fatalf("import archive: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dest, "bundle", ".deck", "manifest.json")); err != nil {
		t.Fatalf("expected imported bundle manifest: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "bundle", "files", "a.txt")); err != nil {
		t.Fatalf("expected imported bundle file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "manifest.json")); err == nil {
		t.Fatal("manifest.json at archive root must not be extracted")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat root manifest: %v", err)
	}
}

func tarEntryNames(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	tr := tar.NewReader(f)
	var names []string
	for {
		hdr, err := tr.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		names = append(names, hdr.Name)
	}
	return names, nil
}

func containsName(names []string, target string) bool {
	for _, n := range names {
		if n == target {
			return true
		}
	}
	return false
}
