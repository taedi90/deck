package askconfig

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Airgap-Castaways/deck/internal/askauth"
)

func TestSaveStoredAndLoadStored(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	settings := Settings{Provider: "openrouter", Model: "anthropic/claude-3.5-sonnet", APIKey: "stored-api-value", OAuthToken: "stored-session-value", Endpoint: "https://example.invalid/v1", LogLevel: "trace", MCP: MCP{Enabled: true, Servers: []MCPServer{{Name: "web-search", RunCommand: "node", Args: []string{"mcp.js"}}}}, LSP: LSP{Enabled: true, YAML: LSPEntry{RunCommand: "yaml-language-server", Args: []string{"--stdio"}}}}
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
		Provider:   "openai",
		Model:      "stored-model",
		APIKey:     "stored-api-value",
		OAuthToken: "stored-session-value",
		Endpoint:   "https://stored.invalid/v1",
		MCP:        MCP{Enabled: true, Servers: []MCPServer{{Name: "context7", RunCommand: "context7-mcp"}}},
		LSP:        LSP{Enabled: true, YAML: LSPEntry{RunCommand: "yaml-language-server", Args: []string{"--stdio"}}},
	}); err != nil {
		t.Fatalf("save stored: %v", err)
	}
	t.Setenv(envServiceEndpoint, "https://env.invalid/v1")
	t.Setenv(envProviderChoice, "env-provider")
	t.Setenv(envModelChoice, "env-model")
	t.Setenv(envPrimaryCredential, "env-api-value")
	t.Setenv(envSessionValue, "env-session-value")
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
	if effective.APIKey != "env-api-value" || effective.APIKeySource != "env" {
		t.Fatalf("unexpected api key resolution: %#v", effective)
	}
	if effective.OAuthToken != "env-session-value" || effective.OAuthTokenSource != "env" {
		t.Fatalf("unexpected oauth token resolution: %#v", effective)
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

func TestResolveEffectiveUsesSavedOAuthSessionBeforeConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	configSessionValue := "config-session-value"
	if err := SaveStored(Settings{Provider: "openai", OAuthToken: configSessionValue}); err != nil {
		t.Fatalf("save stored: %v", err)
	}
	if err := askauth.Save(askauth.Session{Provider: "openai", AccessToken: "session-access-value", ExpiresAt: time.Now().UTC().Add(time.Hour)}); err != nil {
		t.Fatalf("save session: %v", err)
	}
	effective, err := ResolveEffective(Settings{})
	if err != nil {
		t.Fatalf("resolve effective: %v", err)
	}
	if effective.OAuthToken != "session-access-value" || effective.OAuthTokenSource != "session" || effective.AuthStatus != "valid" {
		t.Fatalf("unexpected effective oauth session: %#v", effective)
	}
}

func TestResolveEffectiveMarksExpiredOpenAISessionWithoutRefreshing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	if err := askauth.Save(askauth.Session{Provider: "openai", AccessToken: "stale", RefreshToken: "refresh-token", ExpiresAt: time.Now().UTC().Add(-time.Minute)}); err != nil {
		t.Fatalf("save session: %v", err)
	}
	effective, err := ResolveEffective(Settings{})
	if err != nil {
		t.Fatalf("resolve effective: %v", err)
	}
	if effective.OAuthToken != "" || effective.OAuthTokenSource != "session-expired" || effective.AuthStatus != "expired" {
		t.Fatalf("unexpected expired session state: %#v", effective)
	}
}

func TestResolveRuntimeSessionRefreshesExpiredOpenAISession(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	idToken := testJWT(t, map[string]any{"email": "user@example.com"})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		_, _ = fmt.Fprintf(w, `{"access_token":"fresh-access-value","refresh_token":"fresh-refresh-value","id_token":%q,"expires_in":3600}`, idToken)
	}))
	defer server.Close()
	t.Setenv("DECK_ASK_OPENAI_TOKEN_URL", server.URL)
	t.Setenv("DECK_ASK_OPENAI_AUTH_URL", server.URL)
	t.Setenv("DECK_ASK_OPENAI_DEVICE_USERCODE_URL", server.URL)
	t.Setenv("DECK_ASK_OPENAI_DEVICE_TOKEN_URL", server.URL)
	t.Setenv("DECK_ASK_OPENAI_DEVICE_VERIFY_URL", server.URL)
	t.Setenv("DECK_ASK_OPENAI_DEVICE_CALLBACK_URI", server.URL)
	if err := askauth.Save(askauth.Session{Provider: "openai", AccessToken: "stale", RefreshToken: "refresh-token", ExpiresAt: time.Now().UTC().Add(-time.Minute)}); err != nil {
		t.Fatalf("save session: %v", err)
	}
	session, source, status, err := ResolveRuntimeSession("openai")
	if err != nil {
		t.Fatalf("resolve runtime session: %v", err)
	}
	if session.AccessToken != "fresh-access-value" || source != "session" || status != "valid" {
		t.Fatalf("unexpected refreshed runtime session: %#v %q %q", session, source, status)
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
	if err := SaveStored(Settings{Provider: "openai", Model: "gpt-5.3-codex-spark", APIKey: "clear-api-value", OAuthToken: "clear-session-value", Endpoint: "https://example.invalid/v1"}); err != nil {
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

func TestResolveEffectiveRealignsOpenAIDefaultsFromGeminiConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	if err := SaveStored(Settings{Provider: "openai", Model: "gemini-3.1-flash-lite-preview", Endpoint: "https://generativelanguage.googleapis.com/v1beta/openai/"}); err != nil {
		t.Fatalf("save stored: %v", err)
	}
	effective, err := ResolveEffective(Settings{})
	if err != nil {
		t.Fatalf("resolve effective: %v", err)
	}
	if effective.Model != "gpt-5.3-codex-spark" || effective.ModelSource != "provider-default" {
		t.Fatalf("unexpected openai model realignment: %#v", effective)
	}
	if effective.Endpoint != "https://api.openai.com/v1" || effective.EndpointSource != "provider-default" {
		t.Fatalf("unexpected openai endpoint realignment: %#v", effective)
	}
}

func TestMaskAPIKey(t *testing.T) {
	if got := MaskAPIKey("sk-1234567890"); got != "sk-1*****7890" {
		t.Fatalf("unexpected masked value: %q", got)
	}
}

func testJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	return "header." + base64.RawURLEncoding.EncodeToString(raw) + ".sig"
}
