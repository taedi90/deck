package store

import "testing"

func newSessionStore(t *testing.T, sessionID string) *Store {
	t.Helper()
	root := t.TempDir()
	st, err := New(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := st.CreateSession(Session{ID: sessionID}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	return st
}
