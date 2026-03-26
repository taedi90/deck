//go:build ai

package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Airgap-Castaways/deck/internal/askauth"
	"github.com/Airgap-Castaways/deck/internal/askconfig"
)

func newAskLoginCommand() *cobra.Command {
	var provider string
	var oauthToken string
	var refreshToken string
	var accountEmail string
	var expiresAt string
	var headless bool
	var noBrowser bool
	var stdinToken bool
	var callbackPort int
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate ask with OpenAI Codex OAuth",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			providerName, err := resolveAskProvider(provider)
			if err != nil {
				return err
			}
			if err := requireOpenAIProvider(providerName); err != nil {
				return err
			}
			accessToken, err := resolveImportedOAuthToken(cmd, oauthToken, stdinToken)
			if err != nil {
				return err
			}
			if accessToken == "" {
				session, err := runOpenAILoginFlow(cmd, callbackPort, noBrowser, headless)
				if err != nil {
					return err
				}
				overrideSessionMetadata(&session, refreshToken, accountEmail, expiresAt)
				if err := askauth.Save(session); err != nil {
					return err
				}
				return printSavedSession(providerName, session)
			}
			session, err := buildImportedSession(providerName, accessToken, refreshToken, accountEmail, expiresAt)
			if err != nil {
				return err
			}
			if err := askauth.Save(session); err != nil {
				return err
			}
			return printSavedSession(providerName, session)
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "", "provider to associate with this oauth session")
	cmd.Flags().StringVar(&oauthToken, "oauth-token", "", "oauth bearer token to save")
	cmd.Flags().StringVar(&refreshToken, "refresh-token", "", "optional refresh token to store for future flows")
	cmd.Flags().StringVar(&accountEmail, "account-email", "", "optional account email label for status output")
	cmd.Flags().StringVar(&expiresAt, "expires-at", "", "optional RFC3339 access token expiry time")
	cmd.Flags().BoolVar(&stdinToken, "stdin-token", false, "read the oauth bearer token from stdin for headless use")
	cmd.Flags().BoolVar(&headless, "headless", false, "use OpenAI Codex device login instead of browser callback login")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "print the login URL instead of opening it automatically")
	cmd.Flags().IntVar(&callbackPort, "callback-port", askauth.OpenAICodexDefaultCallbackPort, "local callback port for browser-based OAuth login")
	return cmd
}

func newAskLogoutCommand() *cobra.Command {
	var provider string
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Delete the saved OAuth session for ask",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			providerName, err := resolveAskProvider(provider)
			if err != nil {
				return err
			}
			if err := requireOpenAIProvider(providerName); err != nil {
				return err
			}
			if err := askauth.Delete(providerName); err != nil {
				return err
			}
			return stdoutPrintf("ask logout removed provider=%s\n", providerName)
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "", "provider whose saved oauth session should be deleted")
	return cmd
}

func newAskStatusCommand() *cobra.Command {
	var provider string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show saved ask OAuth session status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			providerName, err := resolveAskProvider(provider)
			if err != nil {
				return err
			}
			if err := requireOpenAIProvider(providerName); err != nil {
				return err
			}
			effective, err := askconfig.ResolveEffective(askconfig.Settings{Provider: providerName})
			if err != nil {
				return err
			}
			session, source, status, err := askconfig.ResolveRuntimeSession(providerName)
			if err != nil {
				return err
			}
			ok := strings.TrimSpace(session.AccessToken) != ""
			if ok {
				effective.OAuthTokenSource = source
				effective.AuthStatus = status
				effective.AccountID = session.AccountID
			}
			if err := stdoutPrintf("provider=%s\n", providerName); err != nil {
				return err
			}
			if err := stdoutPrintf("authenticated=%t\n", ok); err != nil {
				return err
			}
			if err := stdoutPrintf("oauthTokenSource=%s\n", effective.OAuthTokenSource); err != nil {
				return err
			}
			if !ok {
				if err := stdoutPrintf("status=missing\n"); err != nil {
					return err
				}
				return stdoutPrintf("accountEmail=\n")
			}
			if err := stdoutPrintf("status=%s\n", status); err != nil {
				return err
			}
			if err := stdoutPrintf("accountEmail=%s\n", session.AccountEmail); err != nil {
				return err
			}
			if err := stdoutPrintf("accountID=%s\n", session.AccountID); err != nil {
				return err
			}
			if err := stdoutPrintf("expiresAt=%s\n", formatExpiry(session.ExpiresAt)); err != nil {
				return err
			}
			return stdoutPrintf("hasRefreshToken=%t\n", strings.TrimSpace(session.RefreshToken) != "")
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "", "provider whose oauth session should be inspected")
	return cmd
}

func resolveImportedOAuthToken(cmd *cobra.Command, oauthToken string, stdinToken bool) (string, error) {
	accessToken := strings.TrimSpace(oauthToken)
	if !stdinToken {
		return accessToken, nil
	}
	raw, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return "", fmt.Errorf("read oauth token from stdin: %w", err)
	}
	return strings.TrimSpace(string(raw)), nil
}

func runOpenAILoginFlow(cmd *cobra.Command, callbackPort int, noBrowser bool, headless bool) (askauth.Session, error) {
	if err := askLoginProgressf("Starting OpenAI Codex login...\n"); err != nil {
		return askauth.Session{}, err
	}
	authOpts := askauth.OpenAICodexOptions{CallbackPort: callbackPort, OpenBrowser: !noBrowser, Writer: os.Stderr}
	if headless {
		return askauth.LoginOpenAICodexDevice(cmd.Context(), authOpts)
	}
	return askauth.LoginOpenAICodexBrowser(cmd.Context(), authOpts)
}

func buildImportedSession(providerName string, accessToken string, refreshToken string, accountEmail string, expiresAt string) (askauth.Session, error) {
	session := askauth.Session{Provider: providerName, AccessToken: accessToken, RefreshToken: strings.TrimSpace(refreshToken), AccountEmail: strings.TrimSpace(accountEmail)}
	if strings.TrimSpace(expiresAt) == "" {
		return session, nil
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(expiresAt))
	if err != nil {
		return askauth.Session{}, fmt.Errorf("parse --expires-at: %w", err)
	}
	session.ExpiresAt = parsed.UTC()
	return session, nil
}

func overrideSessionMetadata(session *askauth.Session, refreshToken string, accountEmail string, expiresAt string) error {
	if session == nil {
		return nil
	}
	if accountEmail != "" {
		session.AccountEmail = strings.TrimSpace(accountEmail)
	}
	if refreshToken != "" {
		session.RefreshToken = strings.TrimSpace(refreshToken)
	}
	if strings.TrimSpace(expiresAt) == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(expiresAt))
	if err != nil {
		return fmt.Errorf("parse --expires-at: %w", err)
	}
	session.ExpiresAt = parsed.UTC()
	return nil
}

func printSavedSession(providerName string, session askauth.Session) error {
	return stdoutPrintf("ask login saved provider=%s account=%s expiresAt=%s\n", providerName, fallbackValue(session.AccountEmail, "unknown"), formatExpiry(session.ExpiresAt))
}

func formatExpiry(value time.Time) string {
	if value.IsZero() {
		return "unknown"
	}
	return value.UTC().Format(time.RFC3339)
}

func fallbackValue(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func askLoginProgressf(format string, args ...any) error {
	_, err := fmt.Fprintf(os.Stderr, format, args...)
	return err
}

func resolveAskProvider(flagValue string) (string, error) {
	if value := strings.TrimSpace(flagValue); value != "" {
		return value, nil
	}
	if value := strings.TrimSpace(os.Getenv("DECK_ASK_PROVIDER")); value != "" {
		return value, nil
	}
	stored, err := askconfig.LoadStored()
	if err != nil {
		return "", err
	}
	if value := strings.TrimSpace(stored.Provider); value != "" {
		return value, nil
	}
	return "openai", nil
}

func requireOpenAIProvider(provider string) error {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" || provider == "openai" {
		return nil
	}
	return fmt.Errorf("ask oauth login currently supports only provider %q", "openai")
}
