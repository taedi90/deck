package askprovider

import "strings"

const (
	DefaultProvider = "openai"
	DefaultModel    = "gpt-5.3-codex-spark"

	OpenAIBaseURL       = "https://api.openai.com/v1"
	OpenRouterBaseURL   = "https://openrouter.ai/api/v1"
	GeminiOpenAIBaseURL = "https://generativelanguage.googleapis.com/v1beta/openai/"
)

func NormalizeProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

func ProviderDefaultModel(provider string) string {
	switch NormalizeProvider(provider) {
	case "gemini", "google", "google-openai":
		return "gemini-2.5-flash"
	default:
		return DefaultModel
	}
}

func ProviderDefaultEndpoint(provider string) string {
	switch NormalizeProvider(provider) {
	case "gemini", "google", "google-openai":
		return GeminiOpenAIBaseURL
	case "openrouter":
		return OpenRouterBaseURL
	default:
		return OpenAIBaseURL
	}
}
