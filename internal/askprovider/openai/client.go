//go:build ai

package openaiprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"

	"github.com/Airgap-Castaways/deck/internal/askprovider"
)

const codexResponsesURL = "https://chatgpt.com/backend-api/codex/responses"

type codexInputText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type codexInputMessage struct {
	Role    string           `json:"role"`
	Content []codexInputText `json:"content"`
}

type codexReasoning struct {
	Effort string `json:"effort"`
}

type codexText struct {
	Verbosity string `json:"verbosity"`
}

type codexRequest struct {
	Model        string              `json:"model"`
	Store        bool                `json:"store"`
	Stream       bool                `json:"stream"`
	Input        []codexInputMessage `json:"input"`
	Reasoning    codexReasoning      `json:"reasoning"`
	Text         codexText           `json:"text"`
	Instructions string              `json:"instructions,omitempty"`
}

type codexOutputPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type codexOutputItem struct {
	Type    string            `json:"type"`
	Content []codexOutputPart `json:"content"`
}

type codexResponseEnvelope struct {
	Output     []codexOutputItem `json:"output"`
	OutputText string            `json:"output_text"`
	Response   *struct {
		Output     []codexOutputItem `json:"output"`
		OutputText string            `json:"output_text"`
	} `json:"response"`
}

type codexSSEDelta struct {
	Delta string `json:"delta"`
	Text  string `json:"text"`
}

type Client struct {
	httpClient *http.Client
}

func New() *Client {
	return &Client{httpClient: &http.Client{Timeout: 120 * time.Second}}
}

func (c *Client) Generate(ctx context.Context, req askprovider.Request) (askprovider.Response, error) {
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	if provider == "" {
		provider = askprovider.DefaultProvider
	}
	if shouldUseCodexOAuth(provider, req) {
		return c.generateCodex(ctx, req)
	}
	authToken := requestToken(req)
	config := openai.DefaultConfig(authToken)
	if endpoint := strings.TrimSpace(req.Endpoint); endpoint != "" {
		config.BaseURL = strings.TrimRight(endpoint, "/")
	} else {
		config.BaseURL = defaultBaseURL(provider, config.BaseURL)
	}
	client := openai.NewClientWithConfig(config)
	request := buildChatRequest(provider, req)
	resp, err := client.CreateChatCompletion(ctx, request)
	if err != nil {
		return askprovider.Response{}, fmt.Errorf("ask provider request failed: %w", err)
	}
	if len(resp.Choices) == 0 {
		return askprovider.Response{}, fmt.Errorf("ask provider returned no choices")
	}
	return askprovider.Response{Content: strings.TrimSpace(resp.Choices[0].Message.Content)}, nil
}

func buildChatRequest(provider string, req askprovider.Request) openai.ChatCompletionRequest {
	request := openai.ChatCompletionRequest{
		Model: strings.TrimSpace(req.Model),
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: strings.TrimSpace(req.SystemPrompt)},
			{Role: openai.ChatMessageRoleUser, Content: strings.TrimSpace(req.Prompt)},
		},
		ResponseFormat: &openai.ChatCompletionResponseFormat{Type: openai.ChatCompletionResponseFormatTypeJSONObject},
	}
	if request.Model == "" {
		request.Model = askprovider.ProviderDefaultModel(provider)
	}
	return request
}

func (c *Client) generateCodex(ctx context.Context, req askprovider.Request) (askprovider.Response, error) {
	endpoint := strings.TrimSpace(req.Endpoint)
	if endpoint == "" || strings.Contains(strings.ToLower(endpoint), "api.openai.com") {
		endpoint = codexResponsesURL
	}
	body, err := json.Marshal(buildCodexRequest(req))
	if err != nil {
		return askprovider.Response{}, fmt.Errorf("marshal codex request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return askprovider.Response{}, fmt.Errorf("create codex request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(req.OAuthToken))
	httpReq.Header.Set("Originator", "deck")
	httpReq.Header.Set("User-Agent", "deck/ask")
	if accountID := strings.TrimSpace(req.AccountID); accountID != "" {
		httpReq.Header.Set("ChatGPT-Account-Id", accountID)
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return askprovider.Response{}, fmt.Errorf("ask provider request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return askprovider.Response{}, fmt.Errorf("read codex response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return askprovider.Response{}, fmt.Errorf("ask provider request failed: %s", codexError(resp.StatusCode, raw))
	}
	content, err := parseCodexResponse(raw, resp.Header.Get("Content-Type"))
	if err != nil {
		return askprovider.Response{}, fmt.Errorf("decode codex response: %w", err)
	}
	if strings.TrimSpace(content) == "" {
		return askprovider.Response{}, fmt.Errorf("ask provider returned no text output")
	}
	return askprovider.Response{Content: strings.TrimSpace(content)}, nil
}

func shouldUseCodexOAuth(provider string, req askprovider.Request) bool {
	return normalizeProvider(provider) == "openai" && strings.TrimSpace(req.OAuthToken) != ""
}

func buildCodexRequest(req askprovider.Request) codexRequest {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = askprovider.ProviderDefaultModel(askprovider.DefaultProvider)
	}
	request := codexRequest{
		Model:  model,
		Store:  false,
		Stream: true,
		Input: []codexInputMessage{{
			Role:    "user",
			Content: []codexInputText{{Type: "input_text", Text: strings.TrimSpace(req.Prompt)}},
		}},
		Reasoning: codexReasoning{Effort: "medium"},
		Text:      codexText{Verbosity: codexVerbosity(model)},
	}
	if instructions := strings.TrimSpace(req.SystemPrompt); instructions != "" {
		request.Instructions = instructions
	}
	return request
}

func parseCodexResponse(raw []byte, contentType string) (string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if strings.Contains(strings.ToLower(contentType), "text/event-stream") || strings.HasPrefix(trimmed, "event:") || strings.HasPrefix(trimmed, "data:") {
		if content := parseCodexSSE(raw); strings.TrimSpace(content) != "" {
			return content, nil
		}
	}
	var parsed codexResponseEnvelope
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	if strings.TrimSpace(parsed.OutputText) != "" {
		return parsed.OutputText, nil
	}
	if parsed.Response != nil {
		if strings.TrimSpace(parsed.Response.OutputText) != "" {
			return parsed.Response.OutputText, nil
		}
		if len(parsed.Output) == 0 {
			parsed.Output = parsed.Response.Output
		}
	}
	b := &strings.Builder{}
	for _, item := range parsed.Output {
		if item.Type != "message" {
			continue
		}
		for _, part := range item.Content {
			if part.Type == "output_text" && strings.TrimSpace(part.Text) != "" {
				if b.Len() > 0 {
					b.WriteString("\n")
				}
				b.WriteString(strings.TrimSpace(part.Text))
			}
		}
	}
	return b.String(), nil
}

func parseCodexSSE(raw []byte) string {
	b := &strings.Builder{}
	for _, chunk := range strings.Split(string(raw), "\n\n") {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		var event string
		var data string
		for _, line := range strings.Split(chunk, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "event: ") {
				event = strings.TrimSpace(strings.TrimPrefix(line, "event: "))
			}
			if strings.HasPrefix(line, "data: ") {
				data = strings.TrimSpace(strings.TrimPrefix(line, "data: "))
			}
		}
		if data == "" {
			continue
		}
		if event == "response.output_text.delta" {
			var payload codexSSEDelta
			if err := json.Unmarshal([]byte(data), &payload); err == nil {
				if payload.Delta != "" {
					b.WriteString(payload.Delta)
					continue
				}
				if payload.Text != "" {
					b.WriteString(payload.Text)
					continue
				}
			}
		}
		if event == "response.completed" {
			if text, err := parseCodexResponse([]byte(data), "application/json"); err == nil && strings.TrimSpace(text) != "" {
				if b.Len() == 0 {
					b.WriteString(text)
				}
			}
		}
	}
	return b.String()
}

func codexError(status int, raw []byte) string {
	trimmed := strings.TrimSpace(string(raw))
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    any    `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &payload); err == nil && strings.TrimSpace(payload.Error.Message) != "" {
		return fmt.Sprintf("error, status code: %d, status: %d %s, message: %s", status, status, http.StatusText(status), strings.TrimSpace(payload.Error.Message))
	}
	return fmt.Sprintf("error, status code: %d, status: %d %s, message: %s", status, status, http.StatusText(status), trimmed)
}

func codexVerbosity(model string) string {
	if strings.TrimSpace(model) == "gpt-5-codex" {
		return "medium"
	}
	return "low"
}

func defaultBaseURL(provider string, fallback string) string {
	switch normalizeProvider(provider) {
	case "openrouter":
		return askprovider.OpenRouterBaseURL
	case "gemini", "google", "google-openai":
		return strings.TrimRight(askprovider.GeminiOpenAIBaseURL, "/")
	default:
		return fallback
	}
}

func requestToken(req askprovider.Request) string {
	if token := strings.TrimSpace(req.OAuthToken); token != "" {
		return token
	}
	return strings.TrimSpace(req.APIKey)
}

func normalizeProvider(provider string) string {
	return askprovider.NormalizeProvider(provider)
}
