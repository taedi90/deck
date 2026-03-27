package config

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Airgap-Castaways/deck/internal/httpfetch"
)

var workflowHTTPClient = httpfetch.Client(10 * time.Second)

func getRequiredHTTP(ctx context.Context, rawURL string) ([]byte, error) {
	raw, err := httpfetch.GetBytes(ctx, workflowHTTPClient, rawURL, "get workflow url", http.StatusOK)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func getOptionalHTTP(ctx context.Context, rawURL string) ([]byte, bool, error) {
	resp, err := httpfetch.Do(ctx, workflowHTTPClient, http.MethodGet, rawURL, nil, "get vars url")
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("get vars url: unexpected status %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("read vars url: %w", err)
	}
	return b, true, nil
}
