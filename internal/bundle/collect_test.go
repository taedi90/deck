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
	t.Run("creates archive with workspace files", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "outputs", "files"), 0o755); err != nil {
			t.Fatalf("mkdir outputs files: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(root, ".deck"), 0o755); err != nil {
			t.Fatalf("mkdir manifest dir: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(root, "workflows", "scenarios"), 0o755); err != nil {
			t.Fatalf("mkdir workflows: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, ".deck", "manifest.json"), []byte(`{"entries":[]}`), 0o644); err != nil {
			t.Fatalf("write manifest: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "outputs", "files", "a.txt"), []byte("hello"), 0o644); err != nil {
			t.Fatalf("write output file: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "workflows", "scenarios", "apply.yaml"), []byte("version: v1alpha1\nsteps: []\n"), 0o644); err != nil {
			t.Fatalf("write workflow: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "deck"), []byte("bin"), 0o755); err != nil {
			t.Fatalf("write deck: %v", err)
		}

		out := filepath.Join(root, "bundle.tar")
		if err := CollectArchive(root, out); err != nil {
			t.Fatalf("collect archive: %v", err)
		}

		names, err := tarEntryNames(out)
		if err != nil {
			t.Fatalf("read tar entries: %v", err)
		}
		for _, want := range []string{"bundle/.deck/manifest.json", "bundle/outputs/files/a.txt", "bundle/workflows/scenarios/apply.yaml", "bundle/deck"} {
			if !containsName(names, want) {
				t.Fatalf("expected %s in archive, got %#v", want, names)
			}
		}
	})

	t.Run("excludes output when output inside workspace root", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, ".deck"), 0o755); err != nil {
			t.Fatalf("mkdir manifest dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, ".deck", "manifest.json"), []byte(`{"entries":[]}`), 0o644); err != nil {
			t.Fatalf("write manifest: %v", err)
		}

		out := filepath.Join(root, "bundle.tar")
		if err := CollectArchive(root, out); err != nil {
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

	t.Run("honors deckignore", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "outputs", "files"), 0o755); err != nil {
			t.Fatalf("mkdir outputs files: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(root, ".deck"), 0o755); err != nil {
			t.Fatalf("mkdir manifest dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, ".deck", "manifest.json"), []byte(`{"entries":[]}`), 0o644); err != nil {
			t.Fatalf("write manifest: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, ".deckignore"), []byte("outputs/files/\n"), 0o644); err != nil {
			t.Fatalf("write .deckignore: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "outputs", "files", "a.txt"), []byte("hello"), 0o644); err != nil {
			t.Fatalf("write output file: %v", err)
		}

		out := filepath.Join(root, "bundle.tar")
		if err := CollectArchive(root, out); err != nil {
			t.Fatalf("collect archive: %v", err)
		}
		names, err := tarEntryNames(out)
		if err != nil {
			t.Fatalf("read tar entries: %v", err)
		}
		if containsName(names, "bundle/outputs/files/a.txt") {
			t.Fatalf("expected outputs/files/a.txt to be ignored, got %#v", names)
		}
	})
}

func TestBundleTarRootRelative(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "outputs", "files"), 0o755); err != nil {
		t.Fatalf("mkdir outputs files: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".deck"), 0o755); err != nil {
		t.Fatalf("mkdir manifest dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".deck", "manifest.json"), []byte(`{"entries":[]}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "outputs", "files", "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write output file: %v", err)
	}

	archivePath := filepath.Join(root, "bundle.tar")
	if err := CollectArchive(root, archivePath); err != nil {
		t.Fatalf("collect archive: %v", err)
	}

	names, err := tarEntryNames(archivePath)
	if err != nil {
		t.Fatalf("read tar entries: %v", err)
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
		t.Fatalf("expected imported manifest: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "bundle", "outputs", "files", "a.txt")); err != nil {
		t.Fatalf("expected imported bundle file: %v", err)
	}
}

func tarEntryNames(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

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
