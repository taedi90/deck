//go:build ai

package openaiprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Airgap-Castaways/deck/internal/askprovider"
)

func TestDefaultBaseURL(t *testing.T) {
	tests := []struct {
		provider string
		want     string
	}{
		{provider: "openrouter", want: "https://openrouter.ai/api/v1"},
		{provider: "gemini", want: "https://generativelanguage.googleapis.com/v1beta/openai"},
		{provider: "google-openai", want: "https://generativelanguage.googleapis.com/v1beta/openai"},
	}
	for _, tt := range tests {
		if got := defaultBaseURL(tt.provider, "https://api.openai.com/v1"); got != tt.want {
			t.Fatalf("defaultBaseURL(%q) = %q, want %q", tt.provider, got, tt.want)
		}
	}
}

func TestDefaultModel(t *testing.T) {
	if got := askprovider.ProviderDefaultModel("gemini"); got != "gemini-2.5-flash" {
		t.Fatalf("unexpected gemini default model: %q", got)
	}
	if got := askprovider.ProviderDefaultModel("openai"); got != "gpt-5.3-codex-spark" {
		t.Fatalf("unexpected openai default model: %q", got)
	}
}

func TestBuildRequestOmitsTemperature(t *testing.T) {
	request := buildChatRequest("gemini", askprovider.Request{
		SystemPrompt: "system",
		Prompt:       "user",
	})
	if request.Temperature != 0 {
		t.Fatalf("expected temperature to be omitted, got %v", request.Temperature)
	}
	if request.Model != "gemini-2.5-flash" {
		t.Fatalf("unexpected model: %q", request.Model)
	}
	if request.ResponseFormat == nil || request.ResponseFormat.Type != "json_object" {
		t.Fatalf("unexpected response format: %#v", request.ResponseFormat)
	}
}

func TestRequestTokenPrefersOAuthToken(t *testing.T) {
	if got := requestToken(askprovider.Request{APIKey: "api-key", OAuthToken: "oauth-token"}); got != "oauth-token" {
		t.Fatalf("expected oauth token to be preferred, got %q", got)
	}
	if got := requestToken(askprovider.Request{APIKey: "api-key"}); got != "api-key" {
		t.Fatalf("expected api key fallback, got %q", got)
	}
}

func TestGenerateCodexUsesChatGPTEndpointAndAccountHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer oauth-token" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		if got := r.Header.Get("ChatGPT-Account-Id"); got != "acct-123" {
			t.Fatalf("unexpected account header: %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["model"] != "gpt-5.3-codex" {
			t.Fatalf("unexpected model: %#v", body["model"])
		}
		_, _ = w.Write([]byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"{\"route\":\"question\"}"}]}]}`))
	}))
	defer server.Close()
	client := &Client{httpClient: server.Client()}
	resp, err := client.Generate(context.Background(), askprovider.Request{Provider: "openai", Model: "gpt-5.3-codex", OAuthToken: "oauth-token", AccountID: "acct-123", Endpoint: server.URL, Prompt: "hello"})
	if err != nil {
		t.Fatalf("generate codex: %v", err)
	}
	if resp.Content != `{"route":"question"}` {
		t.Fatalf("unexpected codex content: %q", resp.Content)
	}
}

func TestShouldUseCodexOAuthOnlyForOpenAIOAuth(t *testing.T) {
	if !shouldUseCodexOAuth("openai", askprovider.Request{OAuthToken: "oauth"}) {
		t.Fatalf("expected openai oauth to use codex endpoint")
	}
	if shouldUseCodexOAuth("openai", askprovider.Request{APIKey: "api"}) {
		t.Fatalf("did not expect api key auth to use codex endpoint")
	}
	if shouldUseCodexOAuth("gemini", askprovider.Request{OAuthToken: "oauth"}) {
		t.Fatalf("did not expect non-openai provider to use codex endpoint")
	}
}

func TestParseCodexSSESupportsMultipleEventShapes(t *testing.T) {
	raw := []byte("event: response.output_text.delta\ndata: {\"delta\":\"hello \"}\n\nevent: response.output_text.added\ndata: {\"text\":\"world\"}\n\nevent: response.completed\ndata: {\"response\":{\"output_text\":\"ignored because delta already handled\"}}\n\ndata: [DONE]\n")
	if got := parseCodexSSE(raw); got != "hello world" {
		t.Fatalf("unexpected parsed SSE content: %q", got)
	}
}

func TestParseCodexSSEFallsBackToCompletedEnvelope(t *testing.T) {
	raw := []byte("event: response.completed\ndata: {\"response\":{\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"final answer\"}]}]}}\n")
	if got := parseCodexSSE(raw); got != "final answer" {
		t.Fatalf("unexpected completed-event fallback: %q", got)
	}
}

func TestGenerateCodexHonorsPerRequestTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"late"}]}]}`))
	}))
	defer server.Close()
	client := &Client{httpClient: server.Client()}
	_, err := client.Generate(context.Background(), askprovider.Request{Provider: "openai", Model: "gpt-5.3-codex", OAuthToken: "oauth-token", Endpoint: server.URL, Prompt: "hello", Timeout: 20 * time.Millisecond})
	if err == nil {
		t.Fatalf("expected timeout error")
	}
}

func TestGenerateCodexRetriesTransient503(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"message":"temporary upstream timeout"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`))
	}))
	defer server.Close()
	client := &Client{httpClient: server.Client()}
	resp, err := client.Generate(context.Background(), askprovider.Request{Provider: "openai", Model: "gpt-5.3-codex", OAuthToken: "oauth-token", Endpoint: server.URL, Prompt: "hello", MaxRetries: 3, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("expected retry success, got %v", err)
	}
	if resp.Content != "ok" || calls != 3 {
		t.Fatalf("expected success after retries, got content=%q calls=%d", resp.Content, calls)
	}
}
