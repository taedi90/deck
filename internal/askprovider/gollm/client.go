//go:build ai

package gollmprovider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/teilomillet/gollm"

	"github.com/taedi90/deck/internal/askprovider"
)

type Client struct{}

func New() *Client {
	return &Client{}
}

func (c *Client) Generate(ctx context.Context, req askprovider.Request) (askprovider.Response, error) {
	provider := normalizeProvider(req.Provider)
	llm, err := gollm.NewLLM(
		gollm.SetProvider(provider),
		gollm.SetModel(strings.TrimSpace(req.Model)),
		gollm.SetAPIKey(strings.TrimSpace(req.APIKey)),
		gollm.SetMaxRetries(req.MaxRetries),
		gollm.SetRetryDelay(2*time.Second),
		gollm.SetMaxTokens(2400),
		gollm.SetTemperature(0.1),
		gollm.SetLogLevel(gollm.LogLevelWarn),
	)
	if err != nil {
		return askprovider.Response{}, fmt.Errorf("configure ask backend: %w", err)
	}
	prompt := gollm.NewPrompt(
		strings.TrimSpace(req.Prompt),
		gollm.WithSystemPrompt(strings.TrimSpace(req.SystemPrompt), gollm.CacheTypeEphemeral),
		gollm.WithDirectives(
			"Return strict JSON only.",
			"Do not wrap the response in markdown code fences.",
		),
	)
	content, err := llm.Generate(ctx, prompt)
	if err != nil {
		return askprovider.Response{}, fmt.Errorf("ask generation failed: %w", err)
	}
	return askprovider.Response{Content: content}, nil
}

func normalizeProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "claude", "anthropic":
		return "anthropic"
	case "gemini", "google", "google-openai":
		return "google-openai"
	case "openrouter":
		return "openrouter"
	case "ollama":
		return "ollama"
	default:
		return "openai"
	}
}
