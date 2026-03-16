package askstate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/taedi90/deck/internal/fsutil"
)

const dirName = ".deck/ask"

type Context struct {
	LastMode            string    `json:"lastMode,omitempty"`
	LastRoute           string    `json:"lastRoute,omitempty"`
	LastConfidence      float64   `json:"lastConfidence,omitempty"`
	LastReason          string    `json:"lastReason,omitempty"`
	LastTargetKind      string    `json:"lastTargetKind,omitempty"`
	LastTargetPath      string    `json:"lastTargetPath,omitempty"`
	LastTargetName      string    `json:"lastTargetName,omitempty"`
	LastPrompt          string    `json:"lastPrompt,omitempty"`
	LastFiles           []string  `json:"lastFiles,omitempty"`
	LastLint            string    `json:"lastLint,omitempty"`
	LastLLMUsed         bool      `json:"lastLlmUsed,omitempty"`
	LastClassifierLLM   bool      `json:"lastClassifierLlmUsed,omitempty"`
	LastChunkIDs        []string  `json:"lastChunkIds,omitempty"`
	LastDroppedChunkIDs []string  `json:"lastDroppedChunkIds,omitempty"`
	LastAugmentEvents   []string  `json:"lastAugmentEvents,omitempty"`
	LastMCPChunkIDs     []string  `json:"lastMcpChunkIds,omitempty"`
	LastLSPChunkIDs     []string  `json:"lastLspChunkIds,omitempty"`
	LastRetries         int       `json:"lastRetries,omitempty"`
	LastTermination     string    `json:"lastTermination,omitempty"`
	LastUpdatedAt       time.Time `json:"lastUpdatedAt,omitempty"`
}

func Dir(root string) (string, error) {
	resolvedRoot, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	return fsutil.ResolveUnder(resolvedRoot, strings.Split(filepath.ToSlash(dirName), "/")...)
}

func Load(root string) (Context, error) {
	dir, err := Dir(root)
	if err != nil {
		return Context{}, err
	}
	//nolint:gosec // Path stays under the current workspace .deck/ask directory.
	raw, err := os.ReadFile(filepath.Join(dir, "context.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return Context{}, nil
		}
		return Context{}, fmt.Errorf("read ask context: %w", err)
	}
	var state Context
	if err := json.Unmarshal(raw, &state); err != nil {
		return Context{}, fmt.Errorf("parse ask context: %w", err)
	}
	return state, nil
}

func Save(root string, state Context, lastRequest string, lastResponse string) error {
	dir, err := Dir(root)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create ask state directory: %w", err)
	}
	state.LastUpdatedAt = time.Now().UTC()
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal ask context: %w", err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(filepath.Join(dir, "context.json"), raw, 0o600); err != nil {
		return fmt.Errorf("write ask context: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "last-request.md"), []byte(lastRequest), 0o600); err != nil {
		return fmt.Errorf("write ask request: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "last-response.md"), []byte(lastResponse), 0o600); err != nil {
		return fmt.Errorf("write ask response: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "last-lint.txt"), []byte(state.LastLint), 0o600); err != nil {
		return fmt.Errorf("write ask lint summary: %w", err)
	}
	return nil
}
