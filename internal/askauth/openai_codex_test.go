package askauth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBuildAuthURLIncludesExpectedParameters(t *testing.T) {
	authURL, err := buildAuthURL(DefaultOpenAICodexEndpoints(), "http://localhost:1455/auth/callback", "state-123", pkceCodes{Verifier: "verifier", Challenge: "challenge"})
	if err != nil {
		t.Fatalf("build auth url: %v", err)
	}
	for _, want := range []string{"client_id=" + OpenAICodexClientID, "response_type=code", "redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fauth%2Fcallback", "scope=openid+email+profile+offline_access", "code_challenge=challenge", "state=state-123"} {
		if !strings.Contains(authURL, want) {
			t.Fatalf("expected %q in auth url, got %q", want, authURL)
		}
	}
}

func TestLoginOpenAICodexDevice(t *testing.T) {
	now := time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC)
	idToken := testIDToken(t, map[string]any{
		"email":                       "user@example.com",
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "acct_123"},
	})
	var polls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/device/usercode":
			_, _ = w.Write([]byte(`{"device_auth_id":"device-1","user_code":"ABCD-EFGH","interval":1}`))
		case "/device/token":
			polls++
			if polls == 1 {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"status":"pending"}`))
				return
			}
			_, _ = w.Write([]byte(`{"authorization_code":"auth-code","code_verifier":"verifier-123","code_challenge":"challenge-123"}`))
		case "/oauth/token":
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if got := r.Form.Get("grant_type"); got != "authorization_code" {
				t.Fatalf("unexpected grant_type: %q", got)
			}
			if got := r.Form.Get("redirect_uri"); got != OpenAICodexDefaultDeviceCallback {
				t.Fatalf("unexpected redirect_uri: %q", got)
			}
			_, _ = fmt.Fprintf(w, `{"access_token":"access-token","refresh_token":"refresh-token","id_token":%q,"expires_in":3600}`, idToken)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	buf := &bytes.Buffer{}
	session, err := LoginOpenAICodexDevice(context.Background(), OpenAICodexOptions{
		HTTPClient: &http.Client{},
		Writer:     buf,
		Now:        func() time.Time { return now },
		Endpoints: OpenAICodexEndpoints{
			AuthURL:           server.URL + "/oauth/authorize",
			TokenURL:          server.URL + "/oauth/token",
			DeviceUserCodeURL: server.URL + "/device/usercode",
			DeviceTokenURL:    server.URL + "/device/token",
			DeviceVerifyURL:   server.URL + "/verify",
			DeviceCallbackURI: OpenAICodexDefaultDeviceCallback,
		},
	})
	if err != nil {
		t.Fatalf("device login: %v", err)
	}
	if session.AccessToken != "access-token" || session.RefreshToken != "refresh-token" || session.AccountEmail != "user@example.com" || session.AccountID != "acct_123" {
		t.Fatalf("unexpected session: %#v", session)
	}
	if session.ExpiresAt != now.Add(time.Hour) {
		t.Fatalf("unexpected expiry: %s", session.ExpiresAt)
	}
	for _, want := range []string{"Requesting OpenAI Codex device code...", "OpenAI Codex device verification URL", "ABCD-EFGH", "Waiting for device authorization..."} {
		if !strings.Contains(buf.String(), want) {
			t.Fatalf("expected %q in output, got %q", want, buf.String())
		}
	}
}

func TestRefreshOpenAICodex(t *testing.T) {
	now := time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC)
	idToken := testIDToken(t, map[string]any{"email": "user@example.com"})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "refresh_token" {
			t.Fatalf("unexpected grant_type: %q", got)
		}
		_, _ = fmt.Fprintf(w, `{"access_token":"new-access","refresh_token":"new-refresh","id_token":%q,"expires_in":7200}`, idToken)
	}))
	defer server.Close()
	session, err := RefreshOpenAICodex(context.Background(), OpenAICodexOptions{Now: func() time.Time { return now }, Endpoints: OpenAICodexEndpoints{TokenURL: server.URL, AuthURL: "x", DeviceUserCodeURL: "x", DeviceTokenURL: "x", DeviceVerifyURL: "x", DeviceCallbackURI: "x"}}, "refresh-token")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if session.AccessToken != "new-access" || session.RefreshToken != "new-refresh" || session.ExpiresAt != now.Add(2*time.Hour) {
		t.Fatalf("unexpected refreshed session: %#v", session)
	}
}

func testIDToken(t *testing.T, claims map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	return "header." + base64.RawURLEncoding.EncodeToString(raw) + ".sig"
}
