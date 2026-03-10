package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSiteStoreReleaseImport(t *testing.T) {
	root := t.TempDir()
	importedBundle := filepath.Join(t.TempDir(), "bundle-src")
	if err := os.MkdirAll(filepath.Join(importedBundle, "workflows"), 0o755); err != nil {
		t.Fatalf("create imported bundle dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(importedBundle, "workflows", "apply.yaml"), []byte("role: apply\n"), 0o644); err != nil {
		t.Fatalf("write imported bundle file: %v", err)
	}

	st, err := New(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	release := Release{ID: "release-20260308", BundleSHA256: "sha256-abc", CreatedAt: "2026-03-08T10:00:00Z"}
	if err := st.ImportRelease(release, importedBundle); err != nil {
		t.Fatalf("import release: %v", err)
	}

	manifestPath := filepath.Join(root, ".deck", "site", "releases", release.ID, "manifest.json")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("expected release manifest at plan path: %v", err)
	}
	bundleFilePath := filepath.Join(root, ".deck", "site", "releases", release.ID, "bundle", "workflows", "apply.yaml")
	if _, err := os.Stat(bundleFilePath); err != nil {
		t.Fatalf("expected imported immutable bundle content at plan path: %v", err)
	}

	got, found, err := st.GetRelease(release.ID)
	if err != nil {
		t.Fatalf("get release: %v", err)
	}
	if !found {
		t.Fatalf("expected release %q", release.ID)
	}
	if got.ID != release.ID || got.BundleSHA256 != release.BundleSHA256 {
		t.Fatalf("unexpected release: %#v", got)
	}

	list, err := st.ListReleases()
	if err != nil {
		t.Fatalf("list releases: %v", err)
	}
	if len(list) != 1 || list[0].ID != release.ID {
		t.Fatalf("unexpected release list: %#v", list)
	}

	if err := st.ImportRelease(release, importedBundle); err == nil {
		t.Fatalf("expected second import to fail for immutable imported release")
	}
}

func TestSiteStoreSeparateFromInstallState(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	st, err := New(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	bundleSrc := filepath.Join(t.TempDir(), "bundle-src")
	if err := os.MkdirAll(bundleSrc, 0o755); err != nil {
		t.Fatalf("mkdir bundle source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleSrc, "bundle.tar.note"), []byte("placeholder"), 0o644); err != nil {
		t.Fatalf("write bundle source file: %v", err)
	}

	if err := st.ImportRelease(Release{ID: "release-separate"}, bundleSrc); err != nil {
		t.Fatalf("import release: %v", err)
	}
	if err := st.CreateSession(Session{ID: "session-separate"}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := st.SaveAssignment("session-separate", Assignment{ID: "assign-separate", NodeID: "node-separate"}); err != nil {
		t.Fatalf("save assignment: %v", err)
	}
	if err := st.SaveExecutionReport("session-separate", ExecutionReport{ID: "report-separate", NodeID: "node-separate"}); err != nil {
		t.Fatalf("save report: %v", err)
	}

	sitePath := filepath.Join(root, ".deck", "site")
	if _, err := os.Stat(sitePath); err != nil {
		t.Fatalf("expected site metadata under .deck/site: %v", err)
	}

	installStatePath := filepath.Join(home, ".deck", "state")
	if _, err := os.Stat(installStatePath); !os.IsNotExist(err) {
		t.Fatalf("expected no writes to local install-state path %q, got err=%v", installStatePath, err)
	}
}
