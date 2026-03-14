package install

import (
	"fmt"
	"os"
)

func waitPathConditionMet(path, state, pathType string, nonEmpty bool) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return state == "absent", nil
		}
		return false, err
	}

	if state == "absent" {
		return false, nil
	}

	switch pathType {
	case "file":
		if !info.Mode().IsRegular() {
			return false, nil
		}
	case "dir":
		if !info.IsDir() {
			return false, nil
		}
	}

	if nonEmpty {
		if !info.Mode().IsRegular() {
			return false, nil
		}
		if info.Size() <= 0 {
			return false, nil
		}
	}

	return true, nil
}

func waitPathExpectedCondition(path, state, pathType string, nonEmpty bool) string {
	if state == "absent" {
		return fmt.Sprintf("path %q to be absent", path)
	}

	suffix := "exist"
	if nonEmpty {
		suffix = "exist as a non-empty file"
	} else {
		switch pathType {
		case "file":
			suffix = "exist as a file"
		case "dir":
			suffix = "exist as a directory"
		}
	}

	return fmt.Sprintf("path %q to %s", path, suffix)
}
