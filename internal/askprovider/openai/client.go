//go:build ai

package openaiprovider

import (
	"context"
	"fmt"
	"strings"

	openai "github.com/sashabaranov/go-openai"

	"github.com/taedi90/deck/internal/askprovider"
)

type Client struct{}

func New() *Client {
	return &Client{}
}

func (c *Client) Generate(ctx context.Context, req askprovider.Request) (askprovider.Response, error) {
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	if provider == "" {
		provider = "openai"
	}
	apiKey := strings.TrimSpace(req.APIKey)
	config := openai.DefaultConfig(apiKey)
	if endpoint := strings.TrimSpace(req.Endpoint); endpoint != "" {
		config.BaseURL = strings.TrimRight(endpoint, "/")
	} else {
		config.BaseURL = defaultBaseURL(provider, config.BaseURL)
	}
	client := openai.NewClientWithConfig(config)
	request := buildRequest(provider, req)
	resp, err := client.CreateChatCompletion(ctx, request)
	if err != nil {
		return askprovider.Response{}, fmt.Errorf("ask provider request failed: %w", err)
	}
	if len(resp.Choices) == 0 {
		return askprovider.Response{}, fmt.Errorf("ask provider returned no choices")
	}
	return askprovider.Response{Content: strings.TrimSpace(resp.Choices[0].Message.Content)}, nil
}

func buildRequest(provider string, req askprovider.Request) openai.ChatCompletionRequest {
	request := openai.ChatCompletionRequest{
		Model: strings.TrimSpace(req.Model),
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: strings.TrimSpace(req.SystemPrompt)},
			{Role: openai.ChatMessageRoleUser, Content: strings.TrimSpace(req.Prompt)},
		},
		ResponseFormat: &openai.ChatCompletionResponseFormat{Type: openai.ChatCompletionResponseFormatTypeJSONObject},
	}
	if request.Model == "" {
		request.Model = defaultModel(provider)
	}
	return request
}

func defaultBaseURL(provider string, fallback string) string {
	switch provider {
	case "openrouter":
		return "https://openrouter.ai/api/v1"
	case "gemini", "google", "google-openai":
		return "https://generativelanguage.googleapis.com/v1beta/openai"
	default:
		return fallback
	}
}

func defaultModel(provider string) string {
	switch provider {
	case "gemini", "google", "google-openai":
		return "gemini-2.5-flash"
	default:
		return "gpt-5.4"
	}
}
