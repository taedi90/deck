package fetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/taedi90/deck/internal/fsutil"
)

type SourceConfig struct {
	Type string
	Path string
	URL  string
}

type ResolveOptions struct {
	OfflineOnly bool
	MaxBytes    int64
	Timeout     time.Duration
}

const (
	defaultHTTPMaxBytes = int64(32 << 20) // 32MiB
	defaultHTTPTimeout  = 30 * time.Second
)

func ResolveBytes(ctx context.Context, relPath string, sources []SourceConfig, opts ResolveOptions) ([]byte, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}
	if strings.TrimSpace(relPath) == "" {
		return nil, fmt.Errorf("relative path is empty")
	}

	var attempts []string
	for _, src := range sources {
		typ := strings.TrimSpace(src.Type)
		switch typ {
		case "local", "bundle":
			base := strings.TrimSpace(src.Path)
			if base == "" {
				attempts = append(attempts, fmt.Sprintf("%s(path=missing)", typ))
				continue
			}
			candidate := filepath.Join(base, filepath.FromSlash(relPath))
			raw, err := fsutil.ReadFile(candidate)
			if err == nil {
				return raw, nil
			}
			attempts = append(attempts, fmt.Sprintf("%s(%s)", typ, candidate))

		case "repo", "online":
			if opts.OfflineOnly && typ == "online" {
				attempts = append(attempts, "online(blocked-by-offline-policy)")
				continue
			}
			baseURL := strings.TrimSpace(src.URL)
			if baseURL == "" {
				attempts = append(attempts, fmt.Sprintf("%s(url=missing)", typ))
				continue
			}
			targetURL := strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(filepath.ToSlash(relPath), "/")
			raw, err := readHTTP(ctx, targetURL, opts)
			if err == nil {
				return raw, nil
			}
			attempts = append(attempts, fmt.Sprintf("%s(%s:%s)", typ, targetURL, strings.TrimSpace(err.Error())))

		default:
			attempts = append(attempts, fmt.Sprintf("unknown(%s)", typ))
		}
	}

	if len(attempts) == 0 {
		return nil, fmt.Errorf("no fetch sources configured")
	}
	return nil, fmt.Errorf("all fetch sources failed for %s: %s", relPath, strings.Join(attempts, ", "))
}

func readHTTP(ctx context.Context, url string, opts ResolveOptions) ([]byte, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultHTTPMaxBytes
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultHTTPTimeout
	}

	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	if resp.ContentLength > maxBytes && resp.ContentLength > 0 {
		return nil, fmt.Errorf("http response exceeds max bytes: %d > %d", resp.ContentLength, maxBytes)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > maxBytes {
		return nil, fmt.Errorf("http response exceeds max bytes: %d > %d", len(raw), maxBytes)
	}
	return raw, nil
}
