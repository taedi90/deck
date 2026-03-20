package askconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/taedi90/deck/internal/userdirs"
)

const (
	defaultProvider = "openai"
	defaultModel    = "gpt-5.4"

	//nolint:gosec // Environment variable names are not credentials.
	envAPIKey   = "DECK_ASK_API_KEY"
	envEndpoint = "DECK_ASK_ENDPOINT"
	envProvider = "DECK_ASK_PROVIDER"
	envModel    = "DECK_ASK_MODEL"
)

type Settings struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	APIKey   string `json:"apiKey,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	LogLevel string `json:"logLevel,omitempty"`
	MCP      MCP    `json:"mcp,omitempty"`
	LSP      LSP    `json:"lsp,omitempty"`
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
	APIKeySource   string
	EndpointSource string
	ProviderSource string
	ModelSource    string
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
			Provider: defaultProvider,
			Model:    defaultModel,
			APIKey:   "",
			Endpoint: "",
			LogLevel: "basic",
			MCP:      stored.MCP,
			LSP:      stored.LSP,
		},
		ProviderSource: "default",
		ModelSource:    "default",
		APIKeySource:   "unset",
		EndpointSource: "unset",
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
	if stored.Endpoint != "" {
		effective.Endpoint = stored.Endpoint
		effective.EndpointSource = "config"
	}
	if stored.LogLevel != "" {
		effective.LogLevel = stored.LogLevel
	}
	if value := strings.TrimSpace(os.Getenv(envEndpoint)); value != "" {
		effective.Endpoint = value
		effective.EndpointSource = "env"
	}
	if value := strings.TrimSpace(os.Getenv(envProvider)); value != "" {
		effective.Provider = value
		effective.ProviderSource = "env"
	}
	if value := strings.TrimSpace(os.Getenv(envModel)); value != "" {
		effective.Model = value
		effective.ModelSource = "env"
	}
	if value := strings.TrimSpace(os.Getenv(envAPIKey)); value != "" {
		effective.APIKey = value
		effective.APIKeySource = "env"
	}
	if value := strings.TrimSpace(cli.Provider); value != "" {
		effective.Provider = value
		effective.ProviderSource = "flag"
	}
	if value := strings.TrimSpace(cli.Model); value != "" {
		effective.Model = value
		effective.ModelSource = "flag"
	}
	if value := strings.TrimSpace(cli.APIKey); value != "" {
		effective.APIKey = value
		effective.APIKeySource = "flag"
	}
	if value := strings.TrimSpace(cli.Endpoint); value != "" {
		effective.Endpoint = value
		effective.EndpointSource = "flag"
	}
	effective.Settings = normalize(effective.Settings)
	return effective, nil
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
	return strings.ToLower(strings.TrimSpace(provider))
}

func loadFileConfig(path string) (fileConfig, error) {
	//nolint:gosec // Path resolves from the user's XDG config location.
	raw, err := os.ReadFile(path)
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
		cfg.Ask.Endpoint == "" &&
		!cfg.Ask.MCP.Enabled &&
		len(cfg.Ask.MCP.Servers) == 0 &&
		!cfg.Ask.LSP.Enabled &&
		cfg.Ask.LSP.YAML.RunCommand == "" &&
		len(cfg.Ask.LSP.YAML.Args) == 0
}
