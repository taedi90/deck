package store

import (
	"fmt"
	"path/filepath"
	"strings"
)

func New(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("store root is empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve store root: %w", err)
	}
	return &Store{root: abs}, nil
}
