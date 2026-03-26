package askauth

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Airgap-Castaways/deck/internal/userdirs"
)

type Session struct {
	Provider     string    `json:"provider,omitempty"`
	AccessToken  string    `json:"accessToken,omitempty"`
	RefreshToken string    `json:"refreshToken,omitempty"`
	ExpiresAt    time.Time `json:"expiresAt,omitempty"`
	AccountEmail string    `json:"accountEmail,omitempty"`
	AccountID    string    `json:"accountId,omitempty"`
	IDToken      string    `json:"idToken,omitempty"`
	CreatedAt    time.Time `json:"createdAt,omitempty"`
	UpdatedAt    time.Time `json:"updatedAt,omitempty"`
}

type fileData struct {
	Sessions map[string]Session `json:"sessions,omitempty"`
}

func CredentialsPath() (string, error) {
	return userdirs.ConfigFile("credentials.json")
}

func Load(provider string) (Session, bool, error) {
	data, err := loadAll()
	if err != nil {
		return Session{}, false, err
	}
	provider = normalizeProvider(provider)
	session, ok := data.Sessions[provider]
	if !ok {
		return Session{}, false, nil
	}
	return session, true, nil
}

func Save(session Session) error {
	provider := normalizeProvider(session.Provider)
	if provider == "" {
		return fmt.Errorf("provider is required")
	}
	session.Provider = provider
	session.AccessToken = strings.TrimSpace(session.AccessToken)
	session.RefreshToken = strings.TrimSpace(session.RefreshToken)
	session.AccountEmail = strings.TrimSpace(session.AccountEmail)
	session.AccountID = strings.TrimSpace(session.AccountID)
	session.IDToken = strings.TrimSpace(session.IDToken)
	if session.AccessToken == "" {
		return fmt.Errorf("oauth access token is required")
	}
	now := time.Now().UTC()
	if session.CreatedAt.IsZero() {
		session.CreatedAt = now
	}
	session.UpdatedAt = now
	data, err := loadAll()
	if err != nil {
		return err
	}
	if data.Sessions == nil {
		data.Sessions = map[string]Session{}
	}
	data.Sessions[provider] = session
	return writeAll(data)
}

func Delete(provider string) error {
	provider = normalizeProvider(provider)
	if provider == "" {
		return fmt.Errorf("provider is required")
	}
	data, err := loadAll()
	if err != nil {
		return err
	}
	delete(data.Sessions, provider)
	if len(data.Sessions) == 0 {
		path, pathErr := CredentialsPath()
		if pathErr != nil {
			return pathErr
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove ask credentials: %w", err)
		}
		return nil
	}
	return writeAll(data)
}

func SessionStatus(provider string) (Session, string, bool, error) {
	session, ok, err := Load(provider)
	if err != nil || !ok {
		return session, "", ok, err
	}
	if !session.ExpiresAt.IsZero() && time.Now().UTC().After(session.ExpiresAt) {
		return session, "expired", true, nil
	}
	if !session.ExpiresAt.IsZero() && time.Until(session.ExpiresAt) < 15*time.Minute {
		return session, "expiring-soon", true, nil
	}
	return session, "valid", true, nil
}

func loadAll() (fileData, error) {
	path, err := CredentialsPath()
	if err != nil {
		return fileData{}, err
	}
	raw, err := readTrustedFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fileData{Sessions: map[string]Session{}}, nil
		}
		return fileData{}, fmt.Errorf("read ask credentials: %w", err)
	}
	if strings.TrimSpace(string(raw)) == "" {
		return fileData{Sessions: map[string]Session{}}, nil
	}
	var data fileData
	if err := json.Unmarshal(raw, &data); err != nil {
		return fileData{}, fmt.Errorf("parse ask credentials: %w", err)
	}
	if data.Sessions == nil {
		data.Sessions = map[string]Session{}
	}
	return data, nil
}

func writeAll(data fileData) error {
	path, err := CredentialsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create credentials directory: %w", err)
	}
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal ask credentials: %w", err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("write ask credentials: %w", err)
	}
	return nil
}

func readTrustedFile(path string) ([]byte, error) {
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

func normalizeProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}
