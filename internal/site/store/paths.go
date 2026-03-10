package store

import "path/filepath"

func (s *Store) siteDir() string {
	return filepath.Join(s.root, ".deck", "site")
}

func (s *Store) releasesDir() string {
	return filepath.Join(s.siteDir(), "releases")
}

func (s *Store) releaseDir(releaseID string) string {
	return filepath.Join(s.releasesDir(), releaseID)
}

func (s *Store) sessionDir(sessionID string) string {
	return filepath.Join(s.siteDir(), "sessions", sessionID)
}
