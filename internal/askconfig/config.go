package askconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Airgap-Castaways/deck/internal/askauth"
	"github.com/Airgap-Castaways/deck/internal/askprovider"
	"github.com/Airgap-Castaways/deck/internal/userdirs"
)

const (
	envPrimaryCredential = "DECK_ASK_API" + "_KEY"
	envSessionValue      = "DECK_ASK_OAUTH_TOKEN"
	envServiceEndpoint   = "DECK_ASK_ENDPOINT"
	envProviderChoice    = "DECK_ASK_PROVIDER"
	envModelChoice       = "DECK_ASK_MODEL"
)

type Settings struct {
	Provider   string `json:"provider,omitempty"`
	Model      string `json:"model,omitempty"`
	APIKey     string `json:"apiKey,omitempty"`
	OAuthToken string `json:"oauthToken,omitempty"`
	Endpoint   string `json:"endpoint,omitempty"`
	LogLevel   string `json:"logLevel,omitempty"`
	MCP        MCP    `json:"mcp,omitempty"`
	LSP        LSP    `json:"lsp,omitempty"`
}

type MCP struct {
	Enabled bool        `json:"enabled,omitempty"`
	Servers []MCPServer `json:"servers,omitempty"`
}

type MCPServer struct {
	Name       string   `json:"name,omitempty"`
	RunCommand string   `json:"command,omitempty"`
	Args       []string `json:"args,omitempty"`
}

type LSP struct {
	Enabled bool     `json:"enabled,omitempty"`
	YAML    LSPEntry `json:"yaml,omitempty"`
}

type LSPEntry struct {
	RunCommand string   `json:"command,omitempty"`
	Args       []string `json:"args,omitempty"`
}

type fileConfig struct {
	Ask Settings `json:"ask,omitempty"`
}

type EffectiveSettings struct {
	Settings
	APIKeySource     string
	OAuthTokenSource string
	EndpointSource   string
	ProviderSource   string
	ModelSource      string
	AuthStatus       string
	AccountID        string
}

func ConfigPath() (string, error) {
	return userdirs.ConfigFile("config.json")
}

func LoadStored() (Settings, error) {
	path, err := ConfigPath()
	if err != nil {
		return Settings{}, err
	}
	//nolint:gosec // Path resolves from the user's XDG config location.
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Settings{}, nil
		}
		return Settings{}, fmt.Errorf("read ask config: %w", err)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return Settings{}, nil
	}
	var cfg fileConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Settings{}, fmt.Errorf("parse ask config: %w", err)
	}
	return normalize(cfg.Ask), nil
}

func SaveStored(settings Settings) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	settings = normalize(settings)
	cfg, err := loadFileConfig(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg = fileConfig{}
		} else {
			return err
		}
	}
	cfg.Ask = settings
	return writeFileConfig(path, cfg)
}

func ClearStored() error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	cfg, err := loadFileConfig(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	cfg.Ask = Settings{}
	if isEmptyConfig(cfg) {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove ask config: %w", err)
		}
		return nil
	}
	return writeFileConfig(path, cfg)
}

func ResolveEffective(cli Settings) (EffectiveSettings, error) {
	stored, err := LoadStored()
	if err != nil {
		return EffectiveSettings{}, err
	}
	effective := EffectiveSettings{
		Settings: Settings{
			Provider:   askprovider.DefaultProvider,
			Model:      askprovider.DefaultModel,
			APIKey:     "",
			OAuthToken: "",
			Endpoint:   "",
			LogLevel:   "basic",
			MCP:        stored.MCP,
			LSP:        stored.LSP,
		},
		ProviderSource:   "default",
		ModelSource:      "default",
		APIKeySource:     "unset",
		OAuthTokenSource: "unset",
		EndpointSource:   "unset",
	}
	if stored.Provider != "" {
		effective.Provider = stored.Provider
		effective.ProviderSource = "config"
	}
	if stored.Model != "" {
		effective.Model = stored.Model
		effective.ModelSource = "config"
	}
	if stored.APIKey != "" {
		effective.APIKey = stored.APIKey
		effective.APIKeySource = "config"
	}
	if stored.OAuthToken != "" {
		effective.OAuthToken = stored.OAuthToken
		effective.OAuthTokenSource = "config"
	}
	if stored.Endpoint != "" {
		effective.Endpoint = stored.Endpoint
		effective.EndpointSource = "config"
	}
	if stored.LogLevel != "" {
		effective.LogLevel = stored.LogLevel
	}
	if value := strings.TrimSpace(os.Getenv(envServiceEndpoint)); value != "" {
		effective.Endpoint = value
		effective.EndpointSource = "env"
	}
	if value := strings.TrimSpace(os.Getenv(envProviderChoice)); value != "" {
		effective.Provider = value
		effective.ProviderSource = "env"
	}
	if value := strings.TrimSpace(os.Getenv(envModelChoice)); value != "" {
		effective.Model = value
		effective.ModelSource = "env"
	}
	if value := strings.TrimSpace(os.Getenv(envPrimaryCredential)); value != "" {
		effective.APIKey = value
		effective.APIKeySource = "env"
	}
	if value := strings.TrimSpace(os.Getenv(envSessionValue)); value != "" {
		effective.OAuthToken = value
		effective.OAuthTokenSource = "env"
		effective.AuthStatus = ""
	}
	if value := strings.TrimSpace(cli.Provider); value != "" {
		effective.Provider = value
		effective.ProviderSource = "flag"
	}
	if value := strings.TrimSpace(cli.Model); value != "" {
		effective.Model = value
		effective.ModelSource = "flag"
	}
	applyProviderDefaults(&effective)
	applyStoredSession(&effective)
	if value := strings.TrimSpace(cli.APIKey); value != "" {
		effective.APIKey = value
		effective.APIKeySource = "flag"
	}
	if value := strings.TrimSpace(cli.OAuthToken); value != "" {
		effective.OAuthToken = value
		effective.OAuthTokenSource = "flag"
		effective.AuthStatus = ""
	}
	if value := strings.TrimSpace(cli.Endpoint); value != "" {
		effective.Endpoint = value
		effective.EndpointSource = "flag"
	}
	effective.Settings = normalize(effective.Settings)
	return effective, nil
}

func applyProviderDefaults(effective *EffectiveSettings) {
	if effective == nil {
		return
	}
	provider := normalizeProvider(effective.Provider)
	defaultEndpoint := askprovider.ProviderDefaultEndpoint(provider)
	defaultModel := askprovider.ProviderDefaultModel(provider)
	if modelLooksMismatched(provider, effective.Model, effective.ModelSource) {
		effective.Model = defaultModel
		effective.ModelSource = "provider-default"
	}
	if endpointLooksMismatched(provider, effective.Endpoint, effective.EndpointSource) {
		effective.Endpoint = defaultEndpoint
		effective.EndpointSource = "provider-default"
	}
}

func nowUTC() time.Time {
	return time.Now().UTC()
}

func applyStoredSession(effective *EffectiveSettings) {
	if effective == nil {
		return
	}
	session, ok, err := askauth.Load(effective.Provider)
	if err != nil || !ok {
		return
	}
	if !session.ExpiresAt.IsZero() && session.ExpiresAt.Before(nowUTC()) {
		effective.OAuthTokenSource = "session-expired"
		effective.AuthStatus = "expired"
		effective.AccountID = session.AccountID
		return
	}
	effective.OAuthToken = session.AccessToken
	effective.OAuthTokenSource = "session"
	effective.AccountID = session.AccountID
	if !session.ExpiresAt.IsZero() && session.ExpiresAt.Sub(nowUTC()) < 15*time.Minute {
		effective.AuthStatus = "expiring-soon"
		return
	}
	effective.AuthStatus = "valid"
}

func ResolveRuntimeSession(provider string) (askauth.Session, string, string, error) {
	session, ok, err := askauth.Load(provider)
	if err != nil || !ok {
		return askauth.Session{}, "", "", err
	}
	return resolveRuntimeSession(session)
}

func resolveRuntimeSession(session askauth.Session) (askauth.Session, string, string, error) {
	now := nowUTC()
	if !session.ExpiresAt.IsZero() && session.ExpiresAt.Before(now) {
		if session.Provider == "openai" && strings.TrimSpace(session.RefreshToken) != "" {
			refreshed, err := askauth.RefreshOpenAICodex(context.Background(), askauth.OpenAICodexOptions{}, session.RefreshToken)
			if err == nil {
				if refreshed.AccountEmail == "" {
					refreshed.AccountEmail = session.AccountEmail
				}
				if refreshed.AccountID == "" {
					refreshed.AccountID = session.AccountID
				}
				if err := askauth.Save(refreshed); err == nil {
					return refreshed, "session", "valid", nil
				}
			}
		}
		return session, "session-expired", "expired", nil
	}
	if !session.ExpiresAt.IsZero() && session.ExpiresAt.Sub(now) < 15*time.Minute {
		return session, "session", "expiring-soon", nil
	}
	return session, "session", "valid", nil
}

func MaskAPIKey(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) <= 8 {
		return strings.Repeat("*", len(trimmed))
	}
	return trimmed[:4] + strings.Repeat("*", len(trimmed)-8) + trimmed[len(trimmed)-4:]
}

func NeedsAPIKey(provider string) bool {
	switch normalizeProvider(provider) {
	case "ollama":
		return false
	default:
		return true
	}
}

func normalize(settings Settings) Settings {
	settings.Provider = strings.TrimSpace(settings.Provider)
	settings.Model = strings.TrimSpace(settings.Model)
	settings.APIKey = strings.TrimSpace(settings.APIKey)
	settings.OAuthToken = strings.TrimSpace(settings.OAuthToken)
	settings.Endpoint = strings.TrimSpace(settings.Endpoint)
	settings.LogLevel = normalizeLogLevel(settings.LogLevel)
	for i := range settings.MCP.Servers {
		settings.MCP.Servers[i].Name = strings.TrimSpace(settings.MCP.Servers[i].Name)
		settings.MCP.Servers[i].RunCommand = strings.TrimSpace(settings.MCP.Servers[i].RunCommand)
		trimmed := make([]string, 0, len(settings.MCP.Servers[i].Args))
		for _, arg := range settings.MCP.Servers[i].Args {
			if value := strings.TrimSpace(arg); value != "" {
				trimmed = append(trimmed, value)
			}
		}
		settings.MCP.Servers[i].Args = trimmed
	}
	settings.LSP.YAML.RunCommand = strings.TrimSpace(settings.LSP.YAML.RunCommand)
	trimmedLSP := make([]string, 0, len(settings.LSP.YAML.Args))
	for _, arg := range settings.LSP.YAML.Args {
		if value := strings.TrimSpace(arg); value != "" {
			trimmedLSP = append(trimmedLSP, value)
		}
	}
	settings.LSP.YAML.Args = trimmedLSP
	return settings
}

func normalizeLogLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", "basic":
		return "basic"
	case "debug":
		return "debug"
	case "trace":
		return "trace"
	default:
		return "basic"
	}
}

func normalizeProvider(provider string) string {
	return askprovider.NormalizeProvider(provider)
}

func modelLooksMismatched(provider string, model string, source string) bool {
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return true
	}
	switch normalizeProvider(provider) {
	case "openai":
		return strings.HasPrefix(strings.ToLower(trimmed), "gemini")
	case "gemini", "google", "google-openai":
		return strings.HasPrefix(strings.ToLower(trimmed), "gpt-")
	default:
		return source == "default" && trimmed != askprovider.ProviderDefaultModel(provider)
	}
}

func endpointLooksMismatched(provider string, endpoint string, source string) bool {
	trimmed := strings.TrimSpace(endpoint)
	if trimmed == "" {
		return true
	}
	lower := strings.ToLower(trimmed)
	switch normalizeProvider(provider) {
	case "openai":
		return strings.Contains(lower, "generativelanguage.googleapis.com")
	case "gemini", "google", "google-openai":
		return strings.Contains(lower, "api.openai.com")
	default:
		return source == "default" && trimmed != askprovider.ProviderDefaultEndpoint(provider)
	}
}

func loadFileConfig(path string) (fileConfig, error) {
	raw, err := readTrustedConfigFile(path)
	if err != nil {
		return fileConfig{}, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return fileConfig{}, nil
	}
	var cfg fileConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fileConfig{}, fmt.Errorf("parse ask config: %w", err)
	}
	return cfg, nil
}

func readTrustedConfigFile(path string) ([]byte, error) {
	base, relPath, err := trustedConfigPath(path)
	if err != nil {
		return nil, err
	}
	return fs.ReadFile(os.DirFS(base), relPath)
}

func trustedConfigPath(path string) (string, string, error) {
	path = filepath.Clean(path)
	base, err := userdirs.ConfigRoot()
	if err != nil {
		return "", "", err
	}
	base, err = filepath.Abs(base)
	if err != nil {
		return "", "", fmt.Errorf("resolve config dir: %w", err)
	}
	fullPath, err := filepath.Abs(path)
	if err != nil {
		return "", "", fmt.Errorf("resolve config path: %w", err)
	}
	if fullPath != base && !strings.HasPrefix(fullPath, base+string(filepath.Separator)) {
		return "", "", fmt.Errorf("config path %q escapes config dir", path)
	}
	relPath, err := filepath.Rel(base, fullPath)
	if err != nil {
		return "", "", fmt.Errorf("resolve config relative path: %w", err)
	}
	return base, filepath.ToSlash(relPath), nil
}

func writeFileConfig(path string, cfg fileConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal ask config: %w", err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("write ask config: %w", err)
	}
	return nil
}

func isEmptyConfig(cfg fileConfig) bool {
	return cfg.Ask.Provider == "" &&
		cfg.Ask.Model == "" &&
		cfg.Ask.APIKey == "" &&
		cfg.Ask.OAuthToken == "" &&
		cfg.Ask.Endpoint == "" &&
		!cfg.Ask.MCP.Enabled &&
		len(cfg.Ask.MCP.Servers) == 0 &&
		!cfg.Ask.LSP.Enabled &&
		cfg.Ask.LSP.YAML.RunCommand == "" &&
		len(cfg.Ask.LSP.YAML.Args) == 0
}
