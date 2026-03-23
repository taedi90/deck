package fsutil

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

func (r Root) WalkDir(fn fs.WalkDirFunc, segments ...string) error {
	rootPath, err := r.Resolve(segments...)
	if err != nil {
		return err
	}
	return filepath.WalkDir(rootPath, fn)
}

func (r Root) WalkDirWithContext(ctx context.Context, fn fs.WalkDirFunc, segments ...string) error {
	if ctx == nil {
		return fmt.Errorf("context cannot be nil")
	}
	return r.WalkDir(func(path string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return fn(path, d, walkErr)
	}, segments...)
}

func (r Root) WalkFiles(fn func(path string, d os.DirEntry) error, segments ...string) error {
	return r.WalkDir(func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		return fn(path, d)
	}, segments...)
}

func (r Root) WalkFilesWithContext(ctx context.Context, fn func(path string, d os.DirEntry) error, segments ...string) error {
	return r.WalkDirWithContext(ctx, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		return fn(path, d)
	}, segments...)
}
