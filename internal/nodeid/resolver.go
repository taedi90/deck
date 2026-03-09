package nodeid

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	DefaultOperatorPath  = "/etc/deck/node-id"
	DefaultGeneratedPath = "/var/lib/deck/node-id"
)

type Source string

const (
	SourceOperator      Source = "operator"
	SourceGenerated     Source = "generated"
	SourceGeneratedInit Source = "generated-new"
)

type Paths struct {
	OperatorPath  string
	GeneratedPath string
}

type Result struct {
	ID               string
	Source           Source
	Hostname         string
	OperatorID       string
	GeneratedID      string
	Mismatch         bool
	GeneratedCreated bool
}

var nodeIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

func DefaultPaths() Paths {
	return Paths{OperatorPath: DefaultOperatorPath, GeneratedPath: DefaultGeneratedPath}
}

func Resolve(paths Paths) (Result, error) {
	resolvedPaths := normalizePaths(paths)

	operatorID, operatorExists, err := readNodeID(resolvedPaths.OperatorPath)
	if err != nil {
		return Result{}, fmt.Errorf("resolve operator node-id: %w", err)
	}

	generatedID, generatedExists, err := readNodeID(resolvedPaths.GeneratedPath)
	if err != nil {
		return Result{}, fmt.Errorf("resolve generated node-id: %w", err)
	}

	result := Result{
		OperatorID:  operatorID,
		GeneratedID: generatedID,
	}
	result.Hostname, _ = os.Hostname()

	if operatorExists {
		result.ID = operatorID
		result.Source = SourceOperator
		if generatedExists && generatedID != operatorID {
			result.Mismatch = true
		}
		return result, nil
	}

	if generatedExists {
		result.ID = generatedID
		result.Source = SourceGenerated
		return result, nil
	}

	generatedID, err = generateNodeID()
	if err != nil {
		return Result{}, fmt.Errorf("generate node-id: %w", err)
	}
	if err := writeNodeID(resolvedPaths.GeneratedPath, generatedID); err != nil {
		return Result{}, fmt.Errorf("persist generated node-id: %w", err)
	}

	result.ID = generatedID
	result.GeneratedID = generatedID
	result.Source = SourceGeneratedInit
	result.GeneratedCreated = true
	return result, nil
}

func Init(paths Paths) (Result, error) {
	resolvedPaths := normalizePaths(paths)

	generatedID, generatedExists, err := readNodeID(resolvedPaths.GeneratedPath)
	if err != nil {
		return Result{}, fmt.Errorf("resolve generated node-id: %w", err)
	}
	if !generatedExists {
		generatedID, err = generateNodeID()
		if err != nil {
			return Result{}, fmt.Errorf("generate node-id: %w", err)
		}
		if err := writeNodeID(resolvedPaths.GeneratedPath, generatedID); err != nil {
			return Result{}, fmt.Errorf("persist generated node-id: %w", err)
		}
	}

	result, err := Resolve(resolvedPaths)
	if err != nil {
		return Result{}, err
	}
	if !generatedExists {
		result.GeneratedCreated = true
	}
	return result, nil
}

func SetOperator(paths Paths, id string) (Result, error) {
	resolvedPaths := normalizePaths(paths)

	normalized := strings.TrimSpace(id)
	if err := Validate(normalized); err != nil {
		return Result{}, err
	}
	if err := writeNodeID(resolvedPaths.OperatorPath, normalized); err != nil {
		return Result{}, fmt.Errorf("write operator node-id: %w", err)
	}
	return Resolve(resolvedPaths)
}

func Validate(id string) error {
	normalized := strings.TrimSpace(id)
	if normalized == "" {
		return errors.New("node-id is empty")
	}
	if !nodeIDPattern.MatchString(normalized) {
		return errors.New("node-id must match ^[a-z0-9][a-z0-9-]{0,62}$")
	}
	return nil
}

func normalizePaths(paths Paths) Paths {
	resolved := paths
	if strings.TrimSpace(resolved.OperatorPath) == "" {
		resolved.OperatorPath = DefaultOperatorPath
	}
	if strings.TrimSpace(resolved.GeneratedPath) == "" {
		resolved.GeneratedPath = DefaultGeneratedPath
	}
	return resolved
}

func readNodeID(path string) (string, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}

	id := strings.TrimSpace(string(raw))
	if err := Validate(id); err != nil {
		return "", false, fmt.Errorf("invalid node-id at %s: %w", path, err)
	}
	return id, true, nil
}

func writeNodeID(path string, id string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(id+"\n"), 0o644)
}

func generateNodeID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "node-" + hex.EncodeToString(buf), nil
}
