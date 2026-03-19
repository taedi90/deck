package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/taedi90/deck/internal/userdirs"
)

type cacheEntry struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	ModTime   string `json:"mod_time"`
}

func executeCacheList(output string) error {
	resolvedOutput, err := resolveOutputFormat(output)
	if err != nil {
		return err
	}

	root, err := defaultDeckCacheRoot()
	if err != nil {
		return err
	}
	if err := verbosef(1, "deck: cache list root=%s output=%s\n", root, strings.TrimSpace(output)); err != nil {
		return err
	}
	entries, err := listCacheEntries(root)
	if err != nil {
		return err
	}
	if err := verbosef(1, "deck: cache list entries=%d\n", len(entries)); err != nil {
		return err
	}
	if resolvedOutput == "json" {
		enc := stdoutJSONEncoder()
		return enc.Encode(entries)
	}
	for _, e := range entries {
		if err := stdoutPrintf("%s\t%d\t%s\n", e.Path, e.SizeBytes, e.ModTime); err != nil {
			return err
		}
	}
	return nil
}

func executeCacheClean(olderThan string, dryRun bool) error {
	root, err := defaultDeckCacheRoot()
	if err != nil {
		return err
	}
	if err := verbosef(1, "deck: cache clean root=%s olderThan=%s dryRun=%t\n", root, strings.TrimSpace(olderThan), dryRun); err != nil {
		return err
	}
	cutoff, hasCutoff, err := parseOlderThan(olderThan)
	if err != nil {
		return err
	}
	plan, err := computeCacheCleanPlan(root, cutoff, hasCutoff)
	if err != nil {
		return err
	}
	if err := verbosef(1, "deck: cache clean matches=%d\n", len(plan)); err != nil {
		return err
	}
	for _, p := range plan {
		if err := verbosef(2, "deck: cache clean path=%s\n", p); err != nil {
			return err
		}
		if err := stdoutPrintln(p); err != nil {
			return err
		}
	}
	if dryRun {
		return nil
	}
	for _, p := range plan {
		if err := os.RemoveAll(p); err != nil {
			return fmt.Errorf("delete %s: %w", p, err)
		}
	}
	return nil
}

func defaultDeckCacheRoot() (string, error) {
	root, err := userdirs.CacheRoot()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(root); err == nil {
		return root, nil
	} else if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("stat cache root: %w", err)
	}
	legacyRoot, found, err := resolveLegacyDeckCacheRoot()
	if err != nil {
		return "", err
	}
	if found {
		return legacyRoot, nil
	}
	return root, nil
}

func listCacheEntries(root string) ([]cacheEntry, error) {
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return []cacheEntry{}, nil
		}
		return nil, fmt.Errorf("stat cache root: %w", err)
	}
	entries := []cacheEntry{}
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		entries = append(entries, cacheEntry{
			Path:      filepath.ToSlash(rel),
			SizeBytes: info.Size(),
			ModTime:   info.ModTime().UTC().Format(time.RFC3339),
		})
		return nil
	}); err != nil {
		return nil, fmt.Errorf("walk cache root: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

func parseOlderThan(raw string) (time.Time, bool, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, false, nil
	}
	var dur time.Duration
	if strings.HasSuffix(trimmed, "d") {
		n := strings.TrimSuffix(trimmed, "d")
		days, err := strconv.ParseInt(n, 10, 64)
		if err != nil || days < 0 {
			return time.Time{}, false, fmt.Errorf("invalid --older-than: %s", trimmed)
		}
		dur = time.Duration(days) * 24 * time.Hour
	} else {
		parsed, err := time.ParseDuration(trimmed)
		if err != nil || parsed < 0 {
			return time.Time{}, false, fmt.Errorf("invalid --older-than: %s", trimmed)
		}
		dur = parsed
	}
	return time.Now().Add(-dur), true, nil
}

func computeCacheCleanPlan(root string, cutoff time.Time, hasCutoff bool) ([]string, error) {
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("stat cache root: %w", err)
	}
	plan := []string{}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read cache root: %w", err)
	}
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		if !hasCutoff {
			plan = append(plan, path)
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if info.ModTime().Before(cutoff) {
			plan = append(plan, path)
		}
	}
	sort.Strings(plan)
	return plan, nil
}
