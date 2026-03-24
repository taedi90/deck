package install

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/config"
	"github.com/Airgap-Castaways/deck/internal/filemode"
	"github.com/Airgap-Castaways/deck/internal/fsutil"
	"github.com/Airgap-Castaways/deck/internal/userdirs"
)

type State struct {
	Phase           string         `json:"phase,omitempty"`
	CompletedPhases []string       `json:"completedPhases,omitempty"`
	FailedPhase     string         `json:"failedPhase,omitempty"`
	RuntimeVars     map[string]any `json:"runtimeVars,omitempty"`
	Error           string         `json:"error,omitempty"`
}

type legacyState struct {
	Phase           string         `json:"phase,omitempty"`
	CompletedPhases []string       `json:"completedPhases,omitempty"`
	FailedPhase     string         `json:"failedPhase,omitempty"`
	RuntimeVars     map[string]any `json:"runtimeVars,omitempty"`
	Error           string         `json:"error,omitempty"`
	CompletedSteps  []string       `json:"completedSteps,omitempty"`
	SkippedSteps    []string       `json:"skippedSteps,omitempty"`
	FailedStep      string         `json:"failedStep,omitempty"`
}

func LoadState(path string) (*State, error) {
	content, err := fsutil.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{CompletedPhases: []string{}, RuntimeVars: map[string]any{}}, nil
		}
		return nil, fmt.Errorf("read state file: %w", err)
	}

	var legacy legacyState
	var st State
	if err := json.Unmarshal(content, &legacy); err != nil {
		return nil, fmt.Errorf("parse state file: %w", err)
	}
	st = State{
		Phase:           legacy.Phase,
		CompletedPhases: legacy.CompletedPhases,
		FailedPhase:     legacy.FailedPhase,
		RuntimeVars:     legacy.RuntimeVars,
		Error:           legacy.Error,
	}
	if len(st.CompletedPhases) == 0 && len(legacy.CompletedSteps) > 0 {
		st.CompletedPhases = []string{}
	}
	if st.CompletedPhases == nil {
		st.CompletedPhases = []string{}
	}
	if st.RuntimeVars == nil {
		st.RuntimeVars = map[string]any{}
	}
	return &st, nil
}

func SaveState(path string, st *State) error {
	if err := filemode.EnsureParentPrivateDir(path); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state file: %w", err)
	}

	tmp := path + ".tmp"
	if err := filemode.WritePrivateFile(tmp, raw); err != nil {
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
	return userdirs.StateFile(stateKey + ".json")
}

func LegacyStatePath(wf *config.Workflow) (string, error) {
	if wf == nil {
		return "", fmt.Errorf("workflow is nil")
	}
	stateKey := strings.TrimSpace(wf.StateKey)
	if stateKey == "" {
		return "", fmt.Errorf("workflow state key is empty")
	}
	return userdirs.LegacyStateFile(stateKey + ".json")
}

func resolveStateReadPath(wf *config.Workflow, preferredPath string) (string, error) {
	resolved := strings.TrimSpace(preferredPath)
	if resolved == "" {
		return preferredPath, nil
	}
	if _, err := os.Stat(resolved); err == nil {
		return resolved, nil
	} else if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("stat state file: %w", err)
	}
	if wf == nil || strings.TrimSpace(wf.StateKey) == "" {
		return resolved, nil
	}
	legacyPath, _, err := resolveLegacyStateReadPath(wf, resolved)
	if err != nil {
		return "", err
	}
	return legacyPath, nil
}

func ResolveStateReadPathForWorkflow(wf *config.Workflow, preferredPath string) (string, error) {
	return resolveStateReadPath(wf, preferredPath)
}
