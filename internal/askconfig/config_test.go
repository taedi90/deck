package askconfig

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSaveStoredAndLoadStored(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	settings := Settings{Provider: "openrouter", Model: "anthropic/claude-3.5-sonnet", APIKey: "secret-token", Endpoint: "https://example.invalid/v1", LogLevel: "trace", MCP: MCP{Enabled: true, Servers: []MCPServer{{Name: "web-search", RunCommand: "node", Args: []string{"mcp.js"}}}}, LSP: LSP{Enabled: true, YAML: LSPEntry{RunCommand: "yaml-language-server", Args: []string{"--stdio"}}}}
	if err := SaveStored(settings); err != nil {
		t.Fatalf("save stored: %v", err)
	}
	loaded, err := LoadStored()
	if err != nil {
		t.Fatalf("load stored: %v", err)
	}
	if !reflect.DeepEqual(loaded, settings) {
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
	if err := SaveStored(Settings{
		Provider: "openai",
		Model:    "stored-model",
		APIKey:   "stored-key",
		Endpoint: "https://stored.invalid/v1",
		MCP:      MCP{Enabled: true, Servers: []MCPServer{{Name: "context7", RunCommand: "context7-mcp"}}},
		LSP:      LSP{Enabled: true, YAML: LSPEntry{RunCommand: "yaml-language-server", Args: []string{"--stdio"}}},
	}); err != nil {
		t.Fatalf("save stored: %v", err)
	}
	t.Setenv(envEndpoint, "https://env.invalid/v1")
	t.Setenv(envProvider, "env-provider")
	t.Setenv(envModel, "env-model")
	t.Setenv(envAPIKey, "env-key")
	effective, err := ResolveEffective(Settings{Provider: "flag-provider", Model: "flag-model", Endpoint: "https://flag.invalid/v1"})
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
	if effective.Endpoint != "https://flag.invalid/v1" || effective.EndpointSource != "flag" {
		t.Fatalf("unexpected endpoint resolution: %#v", effective)
	}
	if !effective.MCP.Enabled || len(effective.MCP.Servers) != 1 {
		t.Fatalf("expected stored mcp config in effective settings: %#v", effective)
	}
	if !effective.LSP.Enabled || effective.LSP.YAML.RunCommand != "yaml-language-server" {
		t.Fatalf("expected stored lsp config in effective settings: %#v", effective)
	}
	if effective.LogLevel != "basic" {
		t.Fatalf("expected default log level to be basic, got %#v", effective)
	}
}

func TestNormalizeLogLevel(t *testing.T) {
	for input, want := range map[string]string{"": "basic", "basic": "basic", "DEBUG": "debug", "trace": "trace", "loud": "basic"} {
		if got := normalizeLogLevel(input); got != want {
			t.Fatalf("normalizeLogLevel(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestClearStored(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	if err := SaveStored(Settings{Provider: "openai", Model: "gpt-5.4", APIKey: "secret", Endpoint: "https://example.invalid/v1"}); err != nil {
		t.Fatalf("save stored: %v", err)
	}
	if err := ClearStored(); err != nil {
		t.Fatalf("clear stored: %v", err)
	}
	loaded, err := LoadStored()
	if err != nil {
		t.Fatalf("load stored: %v", err)
	}
	if !reflect.DeepEqual(loaded, Settings{}) {
		t.Fatalf("expected cleared settings, got %#v", loaded)
	}
}

func TestMaskAPIKey(t *testing.T) {
	if got := MaskAPIKey("sk-1234567890"); got != "sk-1*****7890" {
		t.Fatalf("unexpected masked value: %q", got)
	}
}
