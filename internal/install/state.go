package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/taedi90/deck/internal/config"
)

type State struct {
	Phase          string         `json:"phase"`
	CompletedSteps []string       `json:"completedSteps"`
	SkippedSteps   []string       `json:"skippedSteps,omitempty"`
	RuntimeVars    map[string]any `json:"runtimeVars,omitempty"`
	FailedStep     string         `json:"failedStep,omitempty"`
	Error          string         `json:"error,omitempty"`
}

func LoadState(path string) (*State, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{CompletedSteps: []string{}}, nil
		}
		return nil, fmt.Errorf("read state file: %w", err)
	}

	var st State
	if err := json.Unmarshal(content, &st); err != nil {
		return nil, fmt.Errorf("parse state file: %w", err)
	}
	if st.CompletedSteps == nil {
		st.CompletedSteps = []string{}
	}
	if st.RuntimeVars == nil {
		st.RuntimeVars = map[string]any{}
	}
	if st.SkippedSteps == nil {
		st.SkippedSteps = []string{}
	}
	return &st, nil
}

func SaveState(path string, st *State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state file: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("write temp state file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace state file: %w", err)
	}
	return nil
}

func DefaultStatePath(wf *config.Workflow) (string, error) {
	if wf == nil {
		return "", fmt.Errorf("workflow is nil")
	}
	stateKey := strings.TrimSpace(wf.StateKey)
	if stateKey == "" {
		return "", fmt.Errorf("workflow state key is empty")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}
	return filepath.Join(home, ".deck", "state", stateKey+".json"), nil
}
