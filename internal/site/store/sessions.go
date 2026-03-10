package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (s *Store) CreateSession(session Session) error {
	if err := validateRecordID(session.ID, "session id"); err != nil {
		return err
	}
	if session.ReleaseID != "" {
		if err := validateRecordID(session.ReleaseID, "session release_id"); err != nil {
			return err
		}
	}
	if strings.TrimSpace(session.Status) == "" {
		session.Status = "open"
	}

	path := filepath.Join(s.sessionDir(session.ID), "session.json")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("session %q already exists", session.ID)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check session file: %w", err)
	}
	return writeAtomicJSON(path, session)
}

func (s *Store) GetSession(sessionID string) (Session, bool, error) {
	if err := validateRecordID(sessionID, "session id"); err != nil {
		return Session{}, false, err
	}
	return readJSON[Session](filepath.Join(s.sessionDir(sessionID), "session.json"))
}

func (s *Store) ListSessions() ([]Session, error) {
	entries, err := os.ReadDir(filepath.Join(s.siteDir(), "sessions"))
	if err != nil {
		if os.IsNotExist(err) {
			return []Session{}, nil
		}
		return nil, fmt.Errorf("read sessions directory: %w", err)
	}

	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			ids = append(ids, entry.Name())
		}
	}
	sort.Strings(ids)

	out := make([]Session, 0, len(ids))
	for _, id := range ids {
		session, found, err := s.GetSession(id)
		if err != nil {
			return nil, err
		}
		if found {
			out = append(out, session)
		}
	}
	return out, nil
}

func (s *Store) CloseSession(sessionID, closedAt string) (Session, error) {
	session, found, err := s.GetSession(sessionID)
	if err != nil {
		return Session{}, err
	}
	if !found {
		return Session{}, fmt.Errorf("session %q not found", sessionID)
	}
	session.Status = "closed"
	session.ClosedAt = strings.TrimSpace(closedAt)

	path := filepath.Join(s.sessionDir(sessionID), "session.json")
	if err := writeAtomicJSON(path, session); err != nil {
		return Session{}, err
	}
	return session, nil
}

func (s *Store) requireOpenSession(sessionID string) (Session, error) {
	session, found, err := s.GetSession(sessionID)
	if err != nil {
		return Session{}, err
	}
	if !found {
		return Session{}, fmt.Errorf("session %q not found", sessionID)
	}
	if strings.EqualFold(strings.TrimSpace(session.Status), "closed") {
		return Session{}, fmt.Errorf("session %q is closed", sessionID)
	}
	return session, nil
}
