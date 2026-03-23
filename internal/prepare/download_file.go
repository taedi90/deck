package prepare

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

	"github.com/taedi90/deck/internal/errcode"
	"github.com/taedi90/deck/internal/fetch"
	"github.com/taedi90/deck/internal/filemode"
	"github.com/taedi90/deck/internal/fsutil"
	"github.com/taedi90/deck/internal/workflowexec"
)

type prepareDownloadFileBundleSpec struct {
	Root string `json:"root"`
	Path string `json:"path"`
}

type prepareDownloadFileSourceSpec struct {
	URL    string                         `json:"url"`
	Path   string                         `json:"path"`
	SHA256 string                         `json:"sha256"`
	Bundle *prepareDownloadFileBundleSpec `json:"bundle"`
}

type prepareFileFetchSourceSpec struct {
	Type string `json:"type"`
	Path string `json:"path"`
	URL  string `json:"url"`
}

type prepareFileFetchSpec struct {
	OfflineOnly bool                         `json:"offlineOnly"`
	Sources     []prepareFileFetchSourceSpec `json:"sources"`
}

type prepareDownloadFileSpec struct {
	Source     prepareDownloadFileSourceSpec `json:"source"`
	Fetch      prepareFileFetchSpec          `json:"fetch"`
	OutputPath string                        `json:"outputPath"`
	Mode       string                        `json:"mode"`
}

func runDownloadFile(ctx context.Context, bundleRoot string, spec map[string]any, opts RunOptions) (string, error) {
	decoded, err := workflowexec.DecodeSpec[prepareDownloadFileSpec](spec)
	if err != nil {
		return "", fmt.Errorf("decode prepare File spec: %w", err)
	}
	bundleRef := decoded.Source.Bundle
	if bundleRef != nil {
		root := strings.TrimSpace(bundleRef.Root)
		refPath := strings.TrimSpace(bundleRef.Path)
		if root == "" || refPath == "" {
			return "", fmt.Errorf("DownloadFile bundle source requires root and path")
		}
		decoded.Source.Path = filepath.ToSlash(filepath.Join(root, refPath))
		decoded.Source.Bundle = nil
		if bundleRoot != "" {
			decoded.Fetch.Sources = append([]prepareFileFetchSourceSpec{{Type: "bundle", Path: bundleRoot}}, decoded.Fetch.Sources...)
		}
	}
	url := strings.TrimSpace(decoded.Source.URL)
	sourcePath := strings.TrimSpace(decoded.Source.Path)
	expectedSHA := strings.ToLower(strings.TrimSpace(decoded.Source.SHA256))
	offlineOnly := decoded.Fetch.OfflineOnly
	outPath := strings.TrimSpace(decoded.OutputPath)
	if strings.TrimSpace(outPath) == "" {
		outPath = filepath.ToSlash(filepath.Join("files", inferDownloadFileName(sourcePath, url)))
	}
	if strings.TrimSpace(sourcePath) == "" && strings.TrimSpace(url) == "" {
		return "", fmt.Errorf("file action download requires source.path or source.url")
	}

	target := filepath.Join(bundleRoot, outPath)
	if err := filemode.EnsureParentDir(target, filemode.PublishedArtifact); err != nil {
		return "", fmt.Errorf("create output directory: %w", err)
	}

	reuse, err := canReuseDownloadFile(ctx, bundleRoot, decoded, target, opts)
	if err != nil {
		return "", err
	}
	if reuse {
		return outPath, nil
	}

	f, err := fsutil.Create(target)
	if err != nil {
		return "", fmt.Errorf("create output file: %w", err)
	}
	defer func() { _ = f.Close() }()

	if sourcePath != "" {
		raw, err := resolveSourceBytesFromSpec(ctx, decoded, sourcePath)
		if err == nil {
			if _, err := f.Write(raw); err != nil {
				return "", fmt.Errorf("write output file: %w", err)
			}
		} else {
			if url == "" {
				return "", err
			}
			if offlineOnly {
				return "", errcode.Newf(errCodePrepareOfflinePolicyBlock, "source.url fallback blocked by offline policy")
			}
			if _, err := f.Seek(0, 0); err != nil {
				return "", fmt.Errorf("reset output file cursor: %w", err)
			}
			if err := f.Truncate(0); err != nil {
				return "", fmt.Errorf("truncate output file: %w", err)
			}
			if err := downloadURLToFile(ctx, f, url); err != nil {
				return "", err
			}
		}
	} else {
		if offlineOnly {
			return "", errcode.Newf(errCodePrepareOfflinePolicyBlock, "source.url blocked by offline policy")
		}
		if err := downloadURLToFile(ctx, f, url); err != nil {
			return "", err
		}
	}

	if expectedSHA != "" {
		if err := verifyFileSHA256(target, expectedSHA); err != nil {
			return "", err
		}
	}

	if modeRaw := strings.TrimSpace(decoded.Mode); modeRaw != "" {
		modeVal, err := strconv.ParseUint(modeRaw, 8, 32)
		if err != nil {
			return "", fmt.Errorf("invalid mode: %w", err)
		}
		if err := os.Chmod(target, os.FileMode(modeVal)); err != nil {
			return "", fmt.Errorf("apply mode: %w", err)
		}
	}

	return outPath, nil
}

func downloadURLToFile(ctx context.Context, target *os.File, url string) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	resp, err := http.DefaultClient.Do(req)
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
	decoded, err := workflowexec.DecodeSpec[prepareDownloadFileSpec](spec)
	if err != nil {
		return nil, fmt.Errorf("decode prepare File spec: %w", err)
	}
	return resolveSourceBytesFromSpec(ctx, decoded, sourcePath)
}

func resolveSourceBytesFromSpec(ctx context.Context, spec prepareDownloadFileSpec, sourcePath string) ([]byte, error) {
	if len(spec.Fetch.Sources) > 0 {
		sources := make([]fetch.SourceConfig, 0, len(spec.Fetch.Sources))
		for _, source := range spec.Fetch.Sources {
			sources = append(sources, fetch.SourceConfig{Type: source.Type, Path: source.Path, URL: source.URL})
		}
		if len(sources) == 0 {
			return nil, errcode.Newf(errCodeArtifactSourceNotFound, "source.path %s not found in configured fetch sources", sourcePath)
		}
		raw, err := fetch.ResolveBytes(ctx, sourcePath, sources, fetch.ResolveOptions{OfflineOnly: spec.Fetch.OfflineOnly})
		if err == nil {
			return raw, nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		if ctx != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errcode.Newf(errCodeArtifactSourceNotFound, "source.path %s not found in configured fetch sources", sourcePath)
	}

	raw, err := fsutil.ReadFile(sourcePath)
	if err == nil {
		return raw, nil
	}
	return nil, errcode.Newf(errCodeArtifactSourceNotFound, "source.path %s not found", sourcePath)
}

func verifyFileSHA256(path, expected string) error {
	raw, err := fsutil.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read downloaded file for checksum: %w", err)
	}
	sum := sha256.Sum256(raw)
	actual := hex.EncodeToString(sum[:])
	if !strings.EqualFold(actual, expected) {
		return errcode.Newf(errCodePrepareChecksumMismatch, "expected %s got %s", expected, actual)
	}
	return nil
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

func fileSHA256(path string) (string, error) {
	raw, err := fsutil.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func canReuseDownloadFile(ctx context.Context, bundleRoot string, spec prepareDownloadFileSpec, target string, opts RunOptions) (bool, error) {
	if opts.ForceRedownload {
		return false, nil
	}
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

	expectedSHA := strings.ToLower(strings.TrimSpace(spec.Source.SHA256))
	if expectedSHA != "" {
		if err := verifyFileSHA256(target, expectedSHA); err == nil {
			return true, nil
		}
		return false, nil
	}

	sourcePath := strings.TrimSpace(spec.Source.Path)
	if sourcePath == "" {
		return false, nil
	}
	raw, resolveErr := resolveSourceBytesFromSpec(ctx, spec, sourcePath)
	if resolveErr != nil {
		return false, resolveErr
	}
	targetSHA, err := fileSHA256(target)
	if err != nil {
		return false, err
	}
	sum := sha256.Sum256(raw)
	return strings.EqualFold(targetSHA, hex.EncodeToString(sum[:])), nil
}
