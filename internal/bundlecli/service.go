package bundlecli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/bundle"
)

type VerifyOptions struct {
	FilePath       string
	PositionalArgs []string
	Output         string
	Verbosef       func(level int, format string, args ...any) error
	JSONEncoder    func(any) error
	StdoutPrintf   func(format string, args ...any) error
}

type BuildOptions struct {
	Root         string
	Out          string
	Verbosef     func(level int, format string, args ...any) error
	StdoutPrintf func(format string, args ...any) error
}

type verifyReport struct {
	Status string `json:"status"`
	Path   string `json:"path"`
}

type manifestSummary struct {
	Entries  int
	Files    int
	Images   int
	Packages int
	Other    int
}

func Verify(opts VerifyOptions) error {
	resolvedPath, err := resolveBundlePathArg(opts.FilePath, opts.PositionalArgs, "bundle verify accepts a single <path>")
	if err != nil {
		return err
	}
	if err := verbosef(opts.Verbosef, 1, "deck: bundle verify path=%s\n", resolvedPath); err != nil {
		return err
	}
	if err := bundle.VerifyManifest(resolvedPath); err != nil {
		_ = verbosef(opts.Verbosef, 2, "deck: bundle verify error=%v\n", err)
		return err
	}
	entries, err := bundle.InspectManifest(resolvedPath)
	if err != nil {
		return err
	}
	summary := summarizeBundleManifest(entries)
	if err := verbosef(opts.Verbosef, 2, "deck: bundle verify manifestEntries=%d files=%d images=%d packages=%d other=%d\n", summary.Entries, summary.Files, summary.Images, summary.Packages, summary.Other); err != nil {
		return err
	}
	report := verifyReport{Status: "ok", Path: resolvedPath}
	if strings.TrimSpace(opts.Output) == "json" {
		if opts.JSONEncoder == nil {
			return nil
		}
		return opts.JSONEncoder(report)
	}
	if opts.StdoutPrintf == nil {
		return nil
	}
	return opts.StdoutPrintf("bundle verify: ok (%s)\n", report.Path)
}

func Build(opts BuildOptions) error {
	resolvedRoot := strings.TrimSpace(opts.Root)
	if resolvedRoot == "" {
		resolvedRoot = "."
	}
	if strings.TrimSpace(opts.Out) == "" {
		return errors.New("--out is required")
	}
	if err := verbosef(opts.Verbosef, 1, "deck: bundle build root=%s out=%s\n", resolvedRoot, strings.TrimSpace(opts.Out)); err != nil {
		return err
	}
	manifestPath := filepath.Join(resolvedRoot, ".deck", "manifest.json")
	entries, err := bundle.InspectManifest(resolvedRoot)
	if err != nil {
		if err := verbosef(opts.Verbosef, 2, "deck: bundle build manifestInspectError=%v\n", err); err != nil {
			return err
		}
	} else {
		summary := summarizeBundleManifest(entries)
		if err := verbosef(opts.Verbosef, 1, "deck: bundle build manifest=%s entries=%d\n", manifestPath, summary.Entries); err != nil {
			return err
		}
		if err := verbosef(opts.Verbosef, 2, "deck: bundle build manifest files=%d images=%d packages=%d other=%d\n", summary.Files, summary.Images, summary.Packages, summary.Other); err != nil {
			return err
		}
	}
	if err := bundle.CollectArchive(resolvedRoot, opts.Out); err != nil {
		return err
	}
	if info, err := os.Stat(opts.Out); err == nil {
		if err := verbosef(opts.Verbosef, 2, "deck: bundle build archiveSize=%d\n", info.Size()); err != nil {
			return err
		}
	}
	if opts.StdoutPrintf == nil {
		return nil
	}
	return opts.StdoutPrintf("bundle build: ok (%s -> %s)\n", resolvedRoot, opts.Out)
}

func resolveBundlePathArg(filePath string, positionalArgs []string, tooManyArgsErr string) (string, error) {
	if len(positionalArgs) > 1 {
		return "", errors.New(tooManyArgsErr)
	}
	resolvedPath := strings.TrimSpace(filePath)
	if resolvedPath == "" && len(positionalArgs) == 1 {
		resolvedPath = strings.TrimSpace(positionalArgs[0])
	}
	if resolvedPath == "" {
		return "", errors.New("bundle path is required")
	}
	return resolvedPath, nil
}

func summarizeBundleManifest(entries []bundle.ManifestEntry) manifestSummary {
	summary := manifestSummary{Entries: len(entries)}
	for _, entry := range entries {
		path := strings.TrimSpace(entry.Path)
		switch {
		case strings.HasPrefix(path, "outputs/files/") || strings.HasPrefix(path, "files/"):
			summary.Files++
		case strings.HasPrefix(path, "outputs/images/") || strings.HasPrefix(path, "images/"):
			summary.Images++
		case strings.HasPrefix(path, "outputs/packages/") || strings.HasPrefix(path, "packages/"):
			summary.Packages++
		default:
			summary.Other++
		}
	}
	return summary
}

func verbosef(fn func(level int, format string, args ...any) error, level int, format string, args ...any) error {
	if fn == nil {
		return nil
	}
	return fn(level, format, args...)
}
