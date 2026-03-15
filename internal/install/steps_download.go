package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/taedi90/deck/internal/fetch"
)

func runFileDownload(ctx context.Context, bundleRoot string, spec map[string]any) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	source := mapValue(spec, "source")
	output := mapValue(spec, "output")
	fetchCfg := mapValue(spec, "fetch")
	bundleRef := mapValue(source, "bundle")
	if len(bundleRef) > 0 {
		root := stringValue(bundleRef, "root")
		refPath := stringValue(bundleRef, "path")
		if root == "" || refPath == "" {
			return "", fmt.Errorf("DownloadFile bundle source requires root and path")
		}
		source["path"] = filepath.ToSlash(filepath.Join(root, refPath))
		delete(source, "bundle")
		if bundleRoot != "" {
			sourcesRaw, _ := fetchCfg["sources"].([]any)
			fetchCfg["sources"] = append([]any{map[string]any{"type": "bundle", "path": bundleRoot}}, sourcesRaw...)
		}
		spec["fetch"] = fetchCfg
	}
	url := stringValue(source, "url")
	sourcePath := stringValue(source, "path")
	expectedSHA := strings.ToLower(stringValue(source, "sha256"))
	offlineOnly := boolValue(fetchCfg, "offlineOnly")
	outPath := stringValue(output, "path")
	if strings.TrimSpace(outPath) == "" {
		outPath = filepath.ToSlash(filepath.Join("files", inferDownloadFileName(sourcePath, url)))
	}
	if strings.TrimSpace(sourcePath) == "" && strings.TrimSpace(url) == "" {
		return "", fmt.Errorf("file action download requires source.path or source.url")
	}

	target := filepath.Join(bundleRoot, outPath)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", fmt.Errorf("create output directory: %w", err)
	}

	reuse, err := canReuseDownloadFile(ctx, spec, target)
	if err != nil {
		return "", err
	}
	if reuse {
		return outPath, nil
	}

	f, err := os.Create(target)
	if err != nil {
		return "", fmt.Errorf("create output file: %w", err)
	}
	defer func() { _ = f.Close() }()

	if sourcePath != "" {
		raw, resolveErr := resolveSourceBytes(ctx, spec, sourcePath)
		if resolveErr == nil {
			if _, err := f.Write(raw); err != nil {
				return "", fmt.Errorf("write output file: %w", err)
			}
		} else {
			if url == "" {
				return "", resolveErr
			}
			if offlineOnly {
				return "", fmt.Errorf("%s: source.url fallback blocked by offline policy", errCodeInstallOfflineBlocked)
			}
			if _, err := f.Seek(0, 0); err != nil {
				return "", fmt.Errorf("reset output file cursor: %w", err)
			}
			if err := f.Truncate(0); err != nil {
				return "", fmt.Errorf("truncate output file: %w", err)
			}
			if err := downloadURLToFile(ctx, f, url, commandTimeout(spec)); err != nil {
				return "", err
			}
		}
	} else {
		if offlineOnly {
			return "", fmt.Errorf("%s: source.url blocked by offline policy", errCodeInstallOfflineBlocked)
		}
		if err := downloadURLToFile(ctx, f, url, commandTimeout(spec)); err != nil {
			return "", err
		}
	}

	if expectedSHA != "" {
		if err := verifyFileSHA256(target, expectedSHA); err != nil {
			return "", err
		}
	}

	if modeRaw := stringValue(output, "chmod"); modeRaw != "" {
		modeVal, err := strconv.ParseUint(modeRaw, 8, 32)
		if err != nil {
			return "", fmt.Errorf("invalid chmod: %w", err)
		}
		if err := os.Chmod(target, os.FileMode(modeVal)); err != nil {
			return "", fmt.Errorf("apply chmod: %w", err)
		}
	}

	return outPath, nil
}

func downloadURLToFile(ctx context.Context, target *os.File, url string, timeout time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download %s: unexpected status %d", url, resp.StatusCode)
	}
	if _, err := io.Copy(target, resp.Body); err != nil {
		return fmt.Errorf("write output file: %w", err)
	}
	return nil
}

func resolveSourceBytes(ctx context.Context, spec map[string]any, sourcePath string) ([]byte, error) {
	fetchCfg := mapValue(spec, "fetch")
	sourcesRaw, ok := fetchCfg["sources"].([]any)
	offlineOnly := boolValue(fetchCfg, "offlineOnly")
	if ok && len(sourcesRaw) > 0 {
		sources := make([]fetch.SourceConfig, 0, len(sourcesRaw))
		for _, raw := range sourcesRaw {
			s, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			sources = append(sources, fetch.SourceConfig{
				Type: stringValue(s, "type"),
				Path: stringValue(s, "path"),
				URL:  stringValue(s, "url"),
			})
		}
		if len(sources) == 0 {
			return nil, fmt.Errorf("%s: source.path %s not found in configured fetch sources", errCodeInstallSourceNotFound, sourcePath)
		}
		raw, err := fetch.ResolveBytes(ctx, sourcePath, sources, fetch.ResolveOptions{OfflineOnly: offlineOnly})
		if err == nil {
			return raw, nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		if ctx != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%s: source.path %s not found in configured fetch sources", errCodeInstallSourceNotFound, sourcePath)
	}

	raw, err := os.ReadFile(sourcePath)
	if err == nil {
		return raw, nil
	}
	return nil, fmt.Errorf("%s: source.path %s not found", errCodeInstallSourceNotFound, sourcePath)
}

func verifyFileSHA256(path, expected string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read downloaded file for checksum: %w", err)
	}
	sum := sha256.Sum256(raw)
	actual := hex.EncodeToString(sum[:])
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("%s: expected %s got %s", errCodeInstallChecksumMismatch, expected, actual)
	}
	return nil
}

func canReuseDownloadFile(ctx context.Context, spec map[string]any, target string) (bool, error) {
	info, err := os.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if info.Size() == 0 {
		return false, nil
	}

	source := mapValue(spec, "source")
	expectedSHA := strings.ToLower(stringValue(source, "sha256"))
	if expectedSHA != "" {
		if err := verifyFileSHA256(target, expectedSHA); err == nil {
			return true, nil
		}
		return false, nil
	}

	sourcePath := stringValue(source, "path")
	if sourcePath == "" {
		return false, nil
	}
	raw, err := resolveSourceBytes(ctx, spec, sourcePath)
	if err != nil {
		return false, nil
	}
	targetSHA, err := fileSHA256(target)
	if err != nil {
		return false, err
	}
	sourceSHA := sha256.Sum256(raw)
	return strings.EqualFold(targetSHA, hex.EncodeToString(sourceSHA[:])), nil
}

func fileSHA256(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func inferDownloadFileName(sourcePath, sourceURL string) string {
	if strings.TrimSpace(sourcePath) != "" {
		base := filepath.Base(filepath.FromSlash(strings.TrimSpace(sourcePath)))
		if base != "" && base != "." && base != string(filepath.Separator) {
			return base
		}
	}
	if strings.TrimSpace(sourceURL) != "" {
		trimmed := strings.TrimSpace(sourceURL)
		if idx := strings.Index(trimmed, "?"); idx >= 0 {
			trimmed = trimmed[:idx]
		}
		base := filepath.Base(filepath.FromSlash(trimmed))
		if base != "" && base != "." && base != string(filepath.Separator) {
			return base
		}
	}
	return "downloaded.bin"
}
