package askprovider

import "context"

type Request struct {
	Kind         string
	Provider     string
	Model        string
	APIKey       string
	OAuthToken   string
	AccountID    string
	Endpoint     string
	SystemPrompt string
	Prompt       string
	MaxRetries   int
}

type Response struct {
	Content string
}

type Client interface {
	Generate(ctx context.Context, req Request) (Response, error)
}
