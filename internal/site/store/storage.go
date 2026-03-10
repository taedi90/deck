package store

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func readJSON[T any](path string) (T, bool, error) {
	var zero T
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return zero, false, nil
		}
		return zero, false, fmt.Errorf("read json %q: %w", path, err)
	}
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		return zero, false, fmt.Errorf("parse json %q: %w", path, err)
	}
	return value, true, nil
}

func writeAtomicJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("write temp json: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace json file: %w", err)
	}
	return nil
}

func listJSONFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("read directory %q: %w", dir, err)
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		out = append(out, entry.Name())
	}
	sort.Strings(out)
	return out, nil
}

func copyDir(srcDir, dstDir string) error {
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("read source directory: %w", err)
	}
	for _, entry := range entries {
		src := filepath.Join(srcDir, entry.Name())
		dst := filepath.Join(dstDir, entry.Name())
		if entry.IsDir() {
			if err := copyDir(src, dst); err != nil {
				return err
			}
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("read source file info: %w", err)
		}
		if err := copyFile(src, dst, info.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source file %q: %w", src, err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("create destination file %q: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy file %q to %q: %w", src, dst, err)
	}
	return nil
}
