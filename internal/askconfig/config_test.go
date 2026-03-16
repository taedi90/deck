package askconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveStoredAndLoadStored(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	settings := Settings{Provider: "openrouter", Model: "anthropic/claude-3.5-sonnet", APIKey: "secret-token"}
	if err := SaveStored(settings); err != nil {
		t.Fatalf("save stored: %v", err)
	}
	loaded, err := LoadStored()
	if err != nil {
		t.Fatalf("load stored: %v", err)
	}
	if loaded != settings {
		t.Fatalf("unexpected settings: %#v", loaded)
	}
	path, err := ConfigPath()
	if err != nil {
		t.Fatalf("config path: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 config mode, got %v", info.Mode().Perm())
	}
}

func TestResolveEffectivePrecedence(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	if err := SaveStored(Settings{Provider: "openai", Model: "stored-model", APIKey: "stored-key"}); err != nil {
		t.Fatalf("save stored: %v", err)
	}
	t.Setenv(envProvider, "env-provider")
	t.Setenv(envModel, "env-model")
	t.Setenv(envAPIKey, "env-key")
	effective, err := ResolveEffective(Settings{Provider: "flag-provider", Model: "flag-model"})
	if err != nil {
		t.Fatalf("resolve effective: %v", err)
	}
	if effective.Provider != "flag-provider" || effective.ProviderSource != "flag" {
		t.Fatalf("unexpected provider resolution: %#v", effective)
	}
	if effective.Model != "flag-model" || effective.ModelSource != "flag" {
		t.Fatalf("unexpected model resolution: %#v", effective)
	}
	if effective.APIKey != "env-key" || effective.APIKeySource != "env" {
		t.Fatalf("unexpected api key resolution: %#v", effective)
	}
}

func TestClearStored(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	if err := SaveStored(Settings{Provider: "openai", Model: "gpt-5.4", APIKey: "secret"}); err != nil {
		t.Fatalf("save stored: %v", err)
	}
	if err := ClearStored(); err != nil {
		t.Fatalf("clear stored: %v", err)
	}
	loaded, err := LoadStored()
	if err != nil {
		t.Fatalf("load stored: %v", err)
	}
	if loaded != (Settings{}) {
		t.Fatalf("expected cleared settings, got %#v", loaded)
	}
}

func TestMaskAPIKey(t *testing.T) {
	if got := MaskAPIKey("sk-1234567890"); got != "sk-1*****7890" {
		t.Fatalf("unexpected masked value: %q", got)
	}
}
