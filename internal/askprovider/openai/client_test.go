//go:build ai

package openaiprovider

import (
	"testing"

	"github.com/taedi90/deck/internal/askprovider"
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
	if got := defaultModel("gemini"); got != "gemini-2.5-flash" {
		t.Fatalf("unexpected gemini default model: %q", got)
	}
	if got := defaultModel("openai"); got != "gpt-5.4" {
		t.Fatalf("unexpected openai default model: %q", got)
	}
}

func TestBuildRequestOmitsTemperature(t *testing.T) {
	request := buildRequest("gemini", askprovider.Request{
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
