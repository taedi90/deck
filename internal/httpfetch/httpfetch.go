package httpfetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

const DefaultTimeout = 30 * time.Second

var defaultClient = &http.Client{Timeout: DefaultTimeout}

func Client(timeout time.Duration) *http.Client {
	if timeout < 0 {
		timeout = 0
	}
	return &http.Client{Timeout: timeout}
}

func Do(ctx context.Context, client *http.Client, method string, rawURL string, body io.Reader, action string) (*http.Response, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}
	if client == nil {
		client = defaultClient
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", action, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", action, err)
	}
	return resp, nil
}

func GetBytes(ctx context.Context, client *http.Client, rawURL string, action string, okStatuses ...int) ([]byte, error) {
	resp, err := Do(ctx, client, http.MethodGet, rawURL, nil, action)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if !statusAllowed(resp.StatusCode, okStatuses) {
		return nil, fmt.Errorf("%s: unexpected status %d", action, resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", action, err)
	}
	return raw, nil
}

func statusAllowed(status int, allowed []int) bool {
	if len(allowed) == 0 {
		return status == http.StatusOK
	}
	for _, candidate := range allowed {
		if status == candidate {
			return true
		}
	}
	return false
}
