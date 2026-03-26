package askauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type OpenAICodexEndpoints struct {
	AuthURL           string
	TokenURL          string
	DeviceUserCodeURL string
	DeviceTokenURL    string
	DeviceVerifyURL   string
	DeviceCallbackURI string
}

type OpenAICodexOptions struct {
	HTTPClient   *http.Client
	CallbackPort int
	OpenBrowser  bool
	Writer       io.Writer
	Now          func() time.Time
	Endpoints    OpenAICodexEndpoints
}

type pkceCodes struct {
	Verifier  string
	Challenge string
}

type deviceUserCodeResponse struct {
	DeviceAuthID string          `json:"device_auth_id"`
	UserCode     string          `json:"user_code"`
	UserCodeAlt  string          `json:"usercode"`
	Interval     json.RawMessage `json:"interval"`
}

type deviceTokenResponse struct {
	AuthorizationCode string `json:"authorization_code"`
	CodeVerifier      string `json:"code_verifier"`
	CodeChallenge     string `json:"code_challenge"`
}

type oauthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

type jwtClaims struct {
	Email  string `json:"email"`
	OpenAI struct {
		AccountID string `json:"chatgpt_account_id"`
	} `json:"https://api.openai.com/auth"`
}

func LoginOpenAICodexBrowser(ctx context.Context, opts OpenAICodexOptions) (Session, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	opts = normalizeOpenAICodexOptions(opts)
	pkce, err := generatePKCE()
	if err != nil {
		return Session{}, err
	}
	state, err := randomState()
	if err != nil {
		return Session{}, err
	}
	server := newCallbackServer(opts.CallbackPort)
	if err := server.Start(); err != nil {
		return Session{}, err
	}
	defer func() {
		_ = server.Stop(context.Background())
	}()
	redirectURI := callbackURL(opts.CallbackPort)
	authURL, err := buildAuthURL(opts.Endpoints, redirectURI, state, pkce)
	if err != nil {
		return Session{}, err
	}
	fprintf(opts.Writer, "Preparing OpenAI Codex browser login...\n")
	fprintf(opts.Writer, "OpenAI Codex login URL:\n%s\n", authURL)
	if opts.OpenBrowser {
		if err := openBrowser(authURL); err != nil {
			fprintf(opts.Writer, "Browser open failed, open the URL manually.\n")
		}
	}
	fprintf(opts.Writer, "Waiting for OpenAI OAuth callback on %s\n", redirectURI)
	result, err := server.Wait(5 * time.Minute)
	if err != nil {
		return Session{}, err
	}
	if result.Error != "" {
		return Session{}, fmt.Errorf("openai oauth error: %s", result.Error)
	}
	if result.State != state {
		return Session{}, fmt.Errorf("openai oauth state mismatch")
	}
	return exchangeOpenAICodexCode(ctx, opts, result.Code, redirectURI, pkce)
}

func LoginOpenAICodexDevice(ctx context.Context, opts OpenAICodexOptions) (Session, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	opts = normalizeOpenAICodexOptions(opts)
	fprintf(opts.Writer, "Requesting OpenAI Codex device code...\n")
	userCodeResp, err := requestDeviceUserCode(ctx, opts)
	if err != nil {
		return Session{}, err
	}
	userCode := strings.TrimSpace(userCodeResp.UserCode)
	if userCode == "" {
		userCode = strings.TrimSpace(userCodeResp.UserCodeAlt)
	}
	if userCode == "" || strings.TrimSpace(userCodeResp.DeviceAuthID) == "" {
		return Session{}, fmt.Errorf("openai device flow missing device auth id or user code")
	}
	fprintf(opts.Writer, "OpenAI Codex device verification URL: %s\n", opts.Endpoints.DeviceVerifyURL)
	fprintf(opts.Writer, "OpenAI Codex device code: %s\n", userCode)
	fprintf(opts.Writer, "Waiting for device authorization...\n")
	if opts.OpenBrowser {
		if err := openBrowser(opts.Endpoints.DeviceVerifyURL); err != nil {
			fprintf(opts.Writer, "Browser open failed, open the device URL manually.\n")
		}
	}
	deviceToken, err := pollDeviceToken(ctx, opts, userCodeResp.DeviceAuthID, userCode, parsePollInterval(userCodeResp.Interval))
	if err != nil {
		return Session{}, err
	}
	pkce := pkceCodes{Verifier: strings.TrimSpace(deviceToken.CodeVerifier), Challenge: strings.TrimSpace(deviceToken.CodeChallenge)}
	if pkce.Verifier == "" || strings.TrimSpace(deviceToken.AuthorizationCode) == "" {
		return Session{}, fmt.Errorf("openai device flow missing authorization code or PKCE verifier")
	}
	return exchangeOpenAICodexCode(ctx, opts, strings.TrimSpace(deviceToken.AuthorizationCode), opts.Endpoints.DeviceCallbackURI, pkce)
}

func normalizeOpenAICodexOptions(opts OpenAICodexOptions) OpenAICodexOptions {
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if opts.CallbackPort <= 0 {
		opts.CallbackPort = OpenAICodexDefaultCallbackPort
	}
	if opts.Writer == nil {
		opts.Writer = io.Discard
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}
	if opts.Endpoints.AuthURL == "" {
		opts.Endpoints = DefaultOpenAICodexEndpoints()
	}
	return opts
}

func buildAuthURL(endpoints OpenAICodexEndpoints, redirectURI string, state string, pkce pkceCodes) (string, error) {
	params := url.Values{
		"client_id":                  {OpenAICodexClientID},
		"response_type":              {"code"},
		"redirect_uri":               {redirectURI},
		"scope":                      {"openid email profile offline_access"},
		"state":                      {state},
		"code_challenge":             {pkce.Challenge},
		"code_challenge_method":      {"S256"},
		"prompt":                     {"login"},
		"id_token_add_organizations": {"true"},
		"codex_cli_simplified_flow":  {"true"},
	}
	base, err := url.Parse(strings.TrimSpace(endpoints.AuthURL))
	if err != nil {
		return "", fmt.Errorf("parse openai auth url: %w", err)
	}
	base.RawQuery = params.Encode()
	return base.String(), nil
}

func exchangeOpenAICodexCode(ctx context.Context, opts OpenAICodexOptions, code string, redirectURI string, pkce pkceCodes) (Session, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {OpenAICodexClientID},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {pkce.Verifier},
	}
	resp, err := doTokenRequest(ctx, opts, form)
	if err != nil {
		return Session{}, err
	}
	return buildSessionFromTokenResponse(resp, opts.Now), nil
}

func RefreshOpenAICodex(ctx context.Context, opts OpenAICodexOptions, refreshToken string) (Session, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return Session{}, fmt.Errorf("refresh token is required")
	}
	opts = normalizeOpenAICodexOptions(opts)
	form := url.Values{
		"client_id":     {OpenAICodexClientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"scope":         {"openid profile email"},
	}
	resp, err := doTokenRequest(ctx, opts, form)
	if err != nil {
		return Session{}, err
	}
	if strings.TrimSpace(resp.RefreshToken) == "" {
		resp.RefreshToken = refreshToken
	}
	return buildSessionFromTokenResponse(resp, opts.Now), nil
}

func doTokenRequest(ctx context.Context, opts OpenAICodexOptions, form url.Values) (oauthTokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, opts.Endpoints.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return oauthTokenResponse{}, fmt.Errorf("create openai token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := opts.HTTPClient.Do(req)
	if err != nil {
		return oauthTokenResponse{}, fmt.Errorf("openai token request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return oauthTokenResponse{}, fmt.Errorf("read openai token response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return oauthTokenResponse{}, fmt.Errorf("openai token request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var parsed oauthTokenResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return oauthTokenResponse{}, fmt.Errorf("decode openai token response: %w", err)
	}
	if parsed.Error != "" {
		return oauthTokenResponse{}, fmt.Errorf("openai token request failed: %s %s", parsed.Error, parsed.ErrorDesc)
	}
	return parsed, nil
}

func buildSessionFromTokenResponse(resp oauthTokenResponse, now func() time.Time) Session {
	claims, _ := parseJWTClaims(resp.IDToken)
	session := Session{
		Provider:     "openai",
		AccessToken:  strings.TrimSpace(resp.AccessToken),
		RefreshToken: strings.TrimSpace(resp.RefreshToken),
		AccountEmail: strings.TrimSpace(claims.Email),
		AccountID:    strings.TrimSpace(claims.OpenAI.AccountID),
		IDToken:      strings.TrimSpace(resp.IDToken),
	}
	if resp.ExpiresIn > 0 {
		session.ExpiresAt = now().Add(time.Duration(resp.ExpiresIn) * time.Second).UTC()
	}
	return session
}

func requestDeviceUserCode(ctx context.Context, opts OpenAICodexOptions) (deviceUserCodeResponse, error) {
	body, err := json.Marshal(map[string]string{"client_id": OpenAICodexClientID})
	if err != nil {
		return deviceUserCodeResponse{}, fmt.Errorf("encode openai device request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, opts.Endpoints.DeviceUserCodeURL, bytes.NewReader(body))
	if err != nil {
		return deviceUserCodeResponse{}, fmt.Errorf("create openai device request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := opts.HTTPClient.Do(req)
	if err != nil {
		return deviceUserCodeResponse{}, fmt.Errorf("openai device request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return deviceUserCodeResponse{}, fmt.Errorf("read openai device response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return deviceUserCodeResponse{}, fmt.Errorf("openai device request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var parsed deviceUserCodeResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return deviceUserCodeResponse{}, fmt.Errorf("decode openai device response: %w", err)
	}
	return parsed, nil
}

func pollDeviceToken(ctx context.Context, opts OpenAICodexOptions, deviceAuthID string, userCode string, interval time.Duration) (deviceTokenResponse, error) {
	if interval <= 0 {
		interval = openAICodexDefaultPollInterval
	}
	deadline := opts.Now().Add(openAICodexDefaultDeviceTimeout)
	for {
		if opts.Now().After(deadline) {
			return deviceTokenResponse{}, fmt.Errorf("openai device authentication timed out")
		}
		payload, err := json.Marshal(map[string]string{"device_auth_id": deviceAuthID, "user_code": userCode})
		if err != nil {
			return deviceTokenResponse{}, fmt.Errorf("encode openai device token poll request: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, opts.Endpoints.DeviceTokenURL, bytes.NewReader(payload))
		if err != nil {
			return deviceTokenResponse{}, fmt.Errorf("create openai device token poll request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		resp, err := opts.HTTPClient.Do(req)
		if err != nil {
			return deviceTokenResponse{}, fmt.Errorf("poll openai device token: %w", err)
		}
		raw, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return deviceTokenResponse{}, fmt.Errorf("read openai device token response: %w", readErr)
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			var parsed deviceTokenResponse
			if err := json.Unmarshal(raw, &parsed); err != nil {
				return deviceTokenResponse{}, fmt.Errorf("decode openai device token response: %w", err)
			}
			return parsed, nil
		}
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
			select {
			case <-ctx.Done():
				return deviceTokenResponse{}, ctx.Err()
			case <-time.After(interval):
				continue
			}
		}
		return deviceTokenResponse{}, fmt.Errorf("openai device token polling failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
}

func parsePollInterval(raw json.RawMessage) time.Duration {
	defaultInterval := openAICodexDefaultPollInterval
	if len(raw) == 0 {
		return defaultInterval
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		if seconds, convErr := strconv.Atoi(strings.TrimSpace(asString)); convErr == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	var asInt int
	if err := json.Unmarshal(raw, &asInt); err == nil && asInt > 0 {
		return time.Duration(asInt) * time.Second
	}
	return defaultInterval
}

func generatePKCE() (pkceCodes, error) {
	bytes := make([]byte, 96)
	if _, err := rand.Read(bytes); err != nil {
		return pkceCodes{}, fmt.Errorf("generate pkce verifier: %w", err)
	}
	verifier := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(bytes)
	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(hash[:])
	return pkceCodes{Verifier: verifier, Challenge: challenge}, nil
}

func randomState() (string, error) {
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate oauth state: %w", err)
	}
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(bytes), nil
}

type callbackServer struct {
	port int
	srv  *http.Server
	ln   net.Listener
	mu   sync.Mutex
	res  chan callbackResult
	err  chan error
}

type callbackResult struct {
	Code  string
	State string
	Error string
}

func newCallbackServer(port int) *callbackServer {
	return &callbackServer{port: port, res: make(chan callbackResult, 1), err: make(chan error, 1)}
}

func (s *callbackServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("oauth callback port %d is already in use or unavailable: %w", s.port, err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/callback", s.handle)
	mux.HandleFunc("/success", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("OpenAI login complete. You can return to the terminal."))
	})
	s.srv = &http.Server{Handler: mux, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second}
	s.ln = listener
	go func() {
		if err := s.srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			s.err <- err
		}
	}()
	return nil
}

func (s *callbackServer) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := s.srv.Shutdown(ctx)
	s.srv = nil
	s.ln = nil
	return err
}

func (s *callbackServer) Wait(timeout time.Duration) (callbackResult, error) {
	select {
	case result := <-s.res:
		return result, nil
	case err := <-s.err:
		return callbackResult{}, err
	case <-time.After(timeout):
		return callbackResult{}, fmt.Errorf("timeout waiting for oauth callback")
	}
}

func (s *callbackServer) handle(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	result := callbackResult{Code: query.Get("code"), State: query.Get("state"), Error: query.Get("error")}
	if result.Code == "" && result.Error == "" {
		result.Error = "missing authorization code"
	}
	select {
	case s.res <- result:
	default:
	}
	http.Redirect(w, r, "/success", http.StatusFound)
}

func callbackURL(port int) string {
	return fmt.Sprintf("http://localhost:%d/auth/callback", port)
}

func parseJWTClaims(token string) (jwtClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return jwtClaims{}, fmt.Errorf("invalid jwt token format")
	}
	raw := parts[1]
	switch len(raw) % 4 {
	case 2:
		raw += "=="
	case 3:
		raw += "="
	}
	decoded, err := base64.URLEncoding.DecodeString(raw)
	if err != nil {
		return jwtClaims{}, err
	}
	var claims jwtClaims
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return jwtClaims{}, err
	}
	return claims, nil
}

func openBrowser(target string) error {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(target))
	if err != nil {
		return fmt.Errorf("invalid browser target: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("unsupported browser target scheme: %s", parsed.Scheme)
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open")
		cmd.Args = []string{"open", parsed.String()}
	case "windows":
		cmd = exec.Command("rundll32")
		cmd.Args = []string{"rundll32", "url.dll,FileProtocolHandler", parsed.String()}
	default:
		cmd = exec.Command("xdg-open")
		cmd.Args = []string{"xdg-open", parsed.String()}
	}
	return cmd.Start()
}

func fprintf(w io.Writer, format string, args ...any) {
	if w == nil {
		return
	}
	_, _ = fmt.Fprintf(w, format, args...)
}
