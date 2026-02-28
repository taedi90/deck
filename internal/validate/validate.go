package validate

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// File validates that the given path exists and is parseable YAML.
func File(path string) error {
	if path == "" {
		return fmt.Errorf("file path is empty")
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read workflow file: %w", err)
	}

	var v any
	if err := yaml.Unmarshal(content, &v); err != nil {
		return fmt.Errorf("parse yaml: %w", err)
	}

	return nil
}
