package store

import (
	"fmt"
	"strings"
)

func validateRecordID(id, field string) error {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return fmt.Errorf("%s is empty", field)
	}
	if !recordIDPattern.MatchString(trimmed) {
		return fmt.Errorf("%s must match ^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$", field)
	}
	return nil
}

func validateNodeID(nodeID, field string) error {
	trimmed := strings.TrimSpace(nodeID)
	if trimmed == "" {
		return fmt.Errorf("%s is empty", field)
	}
	if !nodeIDPattern.MatchString(trimmed) {
		return fmt.Errorf("%s must match ^[a-z0-9][a-z0-9-]{0,62}$", field)
	}
	return nil
}
