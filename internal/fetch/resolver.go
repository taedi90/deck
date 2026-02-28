package fetch

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type SourceConfig struct {
	Type string
	Path string
	URL  string
}

type ResolveOptions struct {
	OfflineOnly bool
}

func ResolveBytes(relPath string, sources []SourceConfig, opts ResolveOptions) ([]byte, error) {
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
			raw, err := os.ReadFile(candidate)
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
			raw, err := readHTTP(targetURL)
			if err == nil {
				return raw, nil
			}
			attempts = append(attempts, fmt.Sprintf("%s(%s)", typ, targetURL))

		default:
			attempts = append(attempts, fmt.Sprintf("unknown(%s)", typ))
		}
	}

	if len(attempts) == 0 {
		return nil, fmt.Errorf("no fetch sources configured")
	}
	return nil, fmt.Errorf("all fetch sources failed for %s: %s", relPath, strings.Join(attempts, ", "))
}

func readHTTP(url string) ([]byte, error) {
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return raw, nil
}
