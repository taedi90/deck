package askauth

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSaveLoadDeleteSession(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	session := Session{Provider: "openai", AccessToken: "token", RefreshToken: "refresh", AccountEmail: "user@example.com", ExpiresAt: time.Now().UTC().Add(time.Hour)}
	if err := Save(session); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, ok, err := Load("openai")
	if err != nil || !ok {
		t.Fatalf("load: ok=%t err=%v", ok, err)
	}
	if loaded.AccessToken != "token" || loaded.RefreshToken != "refresh" || loaded.AccountEmail != "user@example.com" {
		t.Fatalf("unexpected session: %#v", loaded)
	}
	if err := Delete("openai"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, ok, err = Load("openai")
	if err != nil {
		t.Fatalf("reload after delete: %v", err)
	}
	if ok {
		t.Fatalf("expected session to be deleted")
	}
}

func TestSessionStatusExpired(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	if err := Save(Session{Provider: "openai", AccessToken: "token", ExpiresAt: time.Now().UTC().Add(-time.Minute)}); err != nil {
		t.Fatalf("save: %v", err)
	}
	_, status, ok, err := SessionStatus("openai")
	if err != nil || !ok {
		t.Fatalf("status: ok=%t err=%v", ok, err)
	}
	if status != "expired" {
		t.Fatalf("unexpected status: %q", status)
	}
}
