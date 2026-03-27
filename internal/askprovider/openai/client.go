package openaiprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
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

type codexSSEEvent struct {
	Name string
	Data string
}

type temporaryError interface {
	Temporary() bool
}

type Client struct {
	httpClient *http.Client
}

func New() *Client {
	return &Client{httpClient: &http.Client{}}
}

func (c *Client) Generate(ctx context.Context, req askprovider.Request) (askprovider.Response, error) {
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}
	attempts := maxAttempts(req.MaxRetries)
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		resp, err := c.generateOnce(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if attempt == attempts || !retryableProviderError(err) || ctx.Err() != nil {
			break
		}
		if !sleepWithContext(ctx, retryBackoff(attempt)) {
			break
		}
	}
	return askprovider.Response{}, lastErr
}

func (c *Client) generateOnce(ctx context.Context, req askprovider.Request) (askprovider.Response, error) {
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	if provider == "" {
		provider = askprovider.DefaultProvider
	}
	if shouldUseCodexOAuth(provider, req) {
		return c.generateCodex(ctx, req)
	}
	return c.generateChat(ctx, provider, req)
}

func (c *Client) generateChat(ctx context.Context, provider string, req askprovider.Request) (askprovider.Response, error) {
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

func maxAttempts(retries int) int {
	if retries <= 1 {
		return 1
	}
	if retries > 4 {
		return 4
	}
	return retries
}

func retryableProviderError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	var tempErr temporaryError
	if errors.As(err, &tempErr) {
		return tempErr.Temporary()
	}
	var apiErr *openai.APIError
	if errors.As(err, &apiErr) {
		return retryableStatus(apiErr.HTTPStatusCode)
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(message, "status code:") {
		for _, code := range []string{"408", "409", "425", "429", "500", "502", "503", "504"} {
			if strings.Contains(message, "status code: "+code) {
				return true
			}
		}
	}
	return strings.Contains(message, "connection timeout") || strings.Contains(message, "timeout") || strings.Contains(message, "temporarily unavailable") || strings.Contains(message, "connection reset")
}

func retryableStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusConflict, http.StatusTooEarly, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func retryBackoff(attempt int) time.Duration {
	switch attempt {
	case 1:
		return 500 * time.Millisecond
	case 2:
		return 1500 * time.Millisecond
	default:
		return 3 * time.Second
	}
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
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
		return askprovider.Response{}, &codexResponseError{StatusCode: resp.StatusCode, Body: append([]byte(nil), raw...)}
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
	for _, event := range parseSSEEvents(raw) {
		text := parseCodexSSEEvent(event)
		if strings.TrimSpace(text) == "" {
			continue
		}
		if b.Len() > 0 && strings.HasSuffix(event.Name, ".completed") {
			continue
		}
		b.WriteString(text)
	}
	return b.String()
}

func parseSSEEvents(raw []byte) []codexSSEEvent {
	chunks := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n\n")
	out := make([]codexSSEEvent, 0, len(chunks))
	for _, chunk := range chunks {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		var event codexSSEEvent
		dataLines := make([]string, 0, 2)
		for _, line := range strings.Split(chunk, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, ":") {
				continue
			}
			if strings.HasPrefix(line, "event:") {
				event.Name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
				continue
			}
			if strings.HasPrefix(line, "data:") {
				dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
		}
		event.Data = strings.Join(dataLines, "\n")
		if strings.TrimSpace(event.Data) == "" {
			continue
		}
		out = append(out, event)
	}
	return out
}

func parseCodexSSEEvent(event codexSSEEvent) string {
	data := strings.TrimSpace(event.Data)
	if data == "" || data == "[DONE]" {
		return ""
	}
	if event.Name == "response.output_text.delta" || event.Name == "response.output_text.added" || event.Name == "response.output_item.added" {
		var payload codexSSEDelta
		if err := json.Unmarshal([]byte(data), &payload); err == nil {
			if payload.Delta != "" {
				return payload.Delta
			}
			if payload.Text != "" {
				return payload.Text
			}
		}
	}
	if event.Name == "response.completed" || event.Name == "response.output_item.done" || event.Name == "response.refusal.done" || event.Name == "" {
		if text, err := parseCodexResponse([]byte(data), "application/json"); err == nil && strings.TrimSpace(text) != "" {
			return text
		}
	}
	return ""
}

type codexResponseError struct {
	StatusCode int
	Body       []byte
}

func (e *codexResponseError) Error() string {
	if e == nil {
		return ""
	}
	return codexErrorMessage(e.StatusCode, e.Body)
}

func codexErrorMessage(status int, raw []byte) string {
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
