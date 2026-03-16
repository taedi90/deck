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

	"github.com/taedi90/deck/internal/filemode"
	"github.com/taedi90/deck/internal/fsutil"
)

func runContainerdConfig(ctx context.Context, spec map[string]any) error {
	path := stringValue(spec, "path")
	if path == "" {
		path = "/etc/containerd/config.toml"
	}
	if err := filemode.EnsureParentDir(path, filemode.PublishedArtifact); err != nil {
		return err
	}

	content, err := fsutil.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if createDefault, ok := spec["createDefault"].(bool); ok && !createDefault {
			content = []byte{}
		} else {
			generated, genErr := runCommandOutputWithContext(ctx, []string{"containerd", "config", "default"}, commandTimeoutWithDefault(spec, 30*time.Second))
			if genErr != nil {
				if errors.Is(genErr, ErrStepCommandTimeout) || errors.Is(genErr, context.DeadlineExceeded) {
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

	if err := writeContainerdRegistryHosts(path, spec); err != nil {
		return err
	}
	return nil
}

func writeContainerdRegistryHosts(configTomlPath string, spec map[string]any) error {
	rawHosts, ok := spec["registryHosts"]
	if !ok {
		return nil
	}

	hostItems, ok := rawHosts.([]any)
	if !ok {
		return fmt.Errorf("registryHosts must be an array")
	}

	configPath := stringValue(spec, "configPath")
	if configPath == "" {
		configPath = filepath.Join(filepath.Dir(configTomlPath), "certs.d")
	}

	for idx, raw := range hostItems {
		entry, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("registryHosts[%d] must be an object", idx)
		}

		registry := stringValue(entry, "registry")
		server := stringValue(entry, "server")
		host := stringValue(entry, "host")
		if registry == "" {
			return fmt.Errorf("registryHosts[%d].registry is required", idx)
		}
		if server == "" {
			return fmt.Errorf("registryHosts[%d].server is required", idx)
		}
		if host == "" {
			return fmt.Errorf("registryHosts[%d].host is required", idx)
		}

		caps, err := parseContainerdHostCapabilities(entry["capabilities"], idx)
		if err != nil {
			return err
		}

		skipVerify, ok := entry["skipVerify"].(bool)
		if !ok {
			return fmt.Errorf("registryHosts[%d].skipVerify must be a boolean", idx)
		}

		hostsPath := filepath.Join(configPath, registry, "hosts.toml")
		if err := filemode.EnsureParentDir(hostsPath, filemode.PublishedArtifact); err != nil {
			return err
		}

		content := renderContainerdHostsTOML(server, host, caps, skipVerify)
		if err := writeFileIfChanged(hostsPath, []byte(content), 0o644); err != nil {
			return err
		}
	}

	return nil
}

func parseContainerdHostCapabilities(raw any, idx int) ([]string, error) {
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("registryHosts[%d].capabilities must be an array", idx)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("registryHosts[%d].capabilities must not be empty", idx)
	}

	capabilities := make([]string, 0, len(items))
	for i, item := range items {
		capability, ok := item.(string)
		if !ok || strings.TrimSpace(capability) == "" {
			return nil, fmt.Errorf("registryHosts[%d].capabilities[%d] must be a non-empty string", idx, i)
		}
		capabilities = append(capabilities, strings.TrimSpace(capability))
	}

	return capabilities, nil
}

func renderContainerdHostsTOML(server string, host string, capabilities []string, skipVerify bool) string {
	tomlCaps := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		tomlCaps = append(tomlCaps, fmt.Sprintf("%q", capability))
	}

	return fmt.Sprintf("server = %q\n\n[host.%q]\n  capabilities = [%s]\n  skip_verify = %t\n", server, host, strings.Join(tomlCaps, ", "), skipVerify)
}
