package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func runContainerdConfig(ctx context.Context, spec map[string]any) error {
	path := stringValue(spec, "path")
	if path == "" {
		path = "/etc/containerd/config.toml"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	content, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if createDefault, ok := spec["createDefault"].(bool); ok && !createDefault {
			content = []byte{}
		} else {
			generated, genErr := runCommandOutputWithContext(ctx, []string{"containerd", "config", "default"}, commandTimeoutWithDefault(spec, 30*time.Second))
			if genErr != nil {
				if errors.Is(genErr, errStepCommandTimeout) || errors.Is(genErr, context.DeadlineExceeded) {
					return fmt.Errorf("containerd config default generation timed out: %w", genErr)
				}
				return genErr
			}
			content = []byte(generated)
		}
	}

	updated := string(content)
	if configPath := stringValue(spec, "configPath"); configPath != "" {
		target := fmt.Sprintf("config_path = %q", configPath)
		re := regexp.MustCompile(`(?m)^\s*config_path\s*=\s*"[^"]*"\s*$`)
		if re.MatchString(updated) {
			updated = re.ReplaceAllString(updated, target)
		} else {
			if !strings.HasSuffix(updated, "\n") && updated != "" {
				updated += "\n"
			}
			updated += target + "\n"
		}
	}

	if raw, ok := spec["systemdCgroup"].(bool); ok {
		target := fmt.Sprintf("            SystemdCgroup = %t", raw)
		re := regexp.MustCompile(`(?m)^\s*SystemdCgroup\s*=\s*(true|false)\s*$`)
		if re.MatchString(updated) {
			updated = re.ReplaceAllString(updated, target)
		} else {
			if !strings.HasSuffix(updated, "\n") && updated != "" {
				updated += "\n"
			}
			updated += target + "\n"
		}
	}

	if err := writeFileIfChanged(path, []byte(updated), 0o644); err != nil {
		return err
	}
	return nil
}
