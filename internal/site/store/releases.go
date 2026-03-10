package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (s *Store) ImportRelease(release Release, importedBundlePath string) error {
	if err := validateRecordID(release.ID, "release id"); err != nil {
		return err
	}
	if strings.TrimSpace(importedBundlePath) == "" {
		return fmt.Errorf("imported bundle path is empty")
	}
	stat, err := os.Stat(importedBundlePath)
	if err != nil {
		return fmt.Errorf("stat imported bundle path: %w", err)
	}
	if !stat.IsDir() {
		return fmt.Errorf("imported bundle path must be a directory")
	}

	releaseDir := s.releaseDir(release.ID)
	manifestPath := filepath.Join(releaseDir, "manifest.json")
	bundlePath := filepath.Join(releaseDir, "bundle")

	if _, err := os.Stat(manifestPath); err == nil {
		return fmt.Errorf("release %q already imported", release.ID)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check release manifest: %w", err)
	}
	if _, err := os.Stat(bundlePath); err == nil {
		return fmt.Errorf("release %q bundle already imported", release.ID)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check release bundle path: %w", err)
	}

	if err := os.MkdirAll(releaseDir, 0o755); err != nil {
		return fmt.Errorf("create release directory: %w", err)
	}
	if err := copyDir(importedBundlePath, bundlePath); err != nil {
		return fmt.Errorf("copy release bundle: %w", err)
	}
	if err := writeAtomicJSON(manifestPath, release); err != nil {
		return fmt.Errorf("write release manifest: %w", err)
	}
	return nil
}

func (s *Store) GetRelease(releaseID string) (Release, bool, error) {
	if err := validateRecordID(releaseID, "release id"); err != nil {
		return Release{}, false, err
	}
	return readJSON[Release](filepath.Join(s.releaseDir(releaseID), "manifest.json"))
}

func (s *Store) ListReleases() ([]Release, error) {
	entries, err := os.ReadDir(s.releasesDir())
	if err != nil {
		if os.IsNotExist(err) {
			return []Release{}, nil
		}
		return nil, fmt.Errorf("read releases directory: %w", err)
	}

	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			ids = append(ids, entry.Name())
		}
	}
	sort.Strings(ids)

	out := make([]Release, 0, len(ids))
	for _, id := range ids {
		release, found, err := s.GetRelease(id)
		if err != nil {
			return nil, err
		}
		if found {
			out = append(out, release)
		}
	}
	return out, nil
}
