package askauth

import (
	"os"
	"strings"
	"time"
)

const (
	OpenAICodexClientID                  = "app_EMoamEEZ73f0CkXaXp7hrann"
	OpenAICodexDefaultAuthURL            = "https://auth.openai.com/oauth/authorize"
	OpenAICodexDefaultExchangeURL        = "https://auth.openai.com/oauth/token"
	OpenAICodexDefaultDeviceUserCodeURL  = "https://auth.openai.com/api/accounts/deviceauth/usercode"
	OpenAICodexDefaultDeviceExchangeURL  = "https://auth.openai.com/api/accounts/deviceauth/token"
	OpenAICodexDefaultDeviceVerification = "https://auth.openai.com/codex/device"
	OpenAICodexDefaultDeviceCallback     = "https://auth.openai.com/deviceauth/callback"
	OpenAICodexDefaultCallbackPort       = 1455
	openAICodexDefaultPollInterval       = 5 * time.Second
	openAICodexDefaultDeviceTimeout      = 15 * time.Minute
)

func DefaultOpenAICodexEndpoints() OpenAICodexEndpoints {
	return OpenAICodexEndpoints{
		AuthURL:           getenvDefault("DECK_ASK_OPENAI_AUTH_URL", OpenAICodexDefaultAuthURL),
		TokenURL:          getenvDefault("DECK_ASK_OPENAI_TOKEN_URL", OpenAICodexDefaultExchangeURL),
		DeviceUserCodeURL: getenvDefault("DECK_ASK_OPENAI_DEVICE_USERCODE_URL", OpenAICodexDefaultDeviceUserCodeURL),
		DeviceTokenURL:    getenvDefault("DECK_ASK_OPENAI_DEVICE_TOKEN_URL", OpenAICodexDefaultDeviceExchangeURL),
		DeviceVerifyURL:   getenvDefault("DECK_ASK_OPENAI_DEVICE_VERIFY_URL", OpenAICodexDefaultDeviceVerification),
		DeviceCallbackURI: getenvDefault("DECK_ASK_OPENAI_DEVICE_CALLBACK_URI", OpenAICodexDefaultDeviceCallback),
	}
}

func getenvDefault(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
