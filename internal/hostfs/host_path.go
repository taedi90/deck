package hostfs

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/taedi90/deck/internal/filemode"
	"github.com/taedi90/deck/internal/fsutil"
)

type HostPath struct {
	abs string
}

func NewHostPath(raw string) (HostPath, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return HostPath{}, fmt.Errorf("host path is empty")
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return HostPath{}, fmt.Errorf("resolve host path: %w", err)
	}
	return HostPath{abs: abs}, nil
}

func (p HostPath) Abs() string {
	return p.abs
}

func (p HostPath) ReadFile() ([]byte, error) {
	return fsutil.ReadFile(p.abs)
}

func (p HostPath) Stat() (os.FileInfo, error) {
	return os.Stat(p.abs)
}

func (p HostPath) Lstat() (os.FileInfo, error) {
	return os.Lstat(p.abs)
}

func (p HostPath) Readlink() (string, error) {
	return os.Readlink(p.abs)
}

func (p HostPath) Remove() error {
	return os.Remove(p.abs)
}

func (p HostPath) EnsureParentDir(class filemode.StorageClass) error {
	return filemode.EnsureParentDir(p.abs, class)
}

func (p HostPath) EnsureDir(class filemode.StorageClass) error {
	return filemode.EnsureDir(p.abs, class)
}

func (p HostPath) WriteFile(data []byte, class filemode.StorageClass) error {
	return filemode.WriteFile(p.abs, data, class)
}

func (p HostPath) WriteFileMode(data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(p.abs), filemode.ArtifactDirMode); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}
	if err := os.WriteFile(p.abs, data, mode); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

func (p HostPath) RemoveAll() error {
	return os.RemoveAll(p.abs)
}

func (p HostPath) Chmod(mode os.FileMode) error {
	return os.Chmod(p.abs, mode)
}

func (p HostPath) CreateSymlink(target string) error {
	return os.Symlink(target, p.abs)
}

func WriteFileIfChanged(path HostPath, content []byte, mode os.FileMode) error {
	existing, err := path.ReadFile()
	if err == nil && bytes.Equal(existing, content) {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := path.EnsureParentDir(filemode.PublishedArtifact); err != nil {
		return err
	}
	return os.WriteFile(path.Abs(), content, mode)
}
