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
	"github.com/taedi90/deck/internal/stepspec"
	"github.com/taedi90/deck/internal/workflowexec"
)

func runWriteContainerdConfig(ctx context.Context, spec map[string]any) error {
	decoded, err := workflowexec.DecodeSpec[stepspec.WriteContainerdConfig](spec)
	if err != nil {
		return fmt.Errorf("decode WriteContainerdConfig spec: %w", err)
	}
	path := strings.TrimSpace(decoded.Path)
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
		if decoded.CreateDefault != nil && !*decoded.CreateDefault {
			content = []byte{}
		} else {
			generated, genErr := runCommandOutputWithContext(ctx, []string{"containerd", "config", "default"}, parseStepTimeout(decoded.Timeout, 30*time.Second))
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
	if configPath := strings.TrimSpace(decoded.ConfigPath); configPath != "" {
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

	if decoded.SystemdCgroup != nil {
		target := fmt.Sprintf("            SystemdCgroup = %t", *decoded.SystemdCgroup)
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

func runWriteContainerdRegistryHosts(spec map[string]any) error {
	decoded, err := workflowexec.DecodeSpec[stepspec.WriteContainerdRegistryHosts](spec)
	if err != nil {
		return fmt.Errorf("decode WriteContainerdRegistryHosts spec: %w", err)
	}
	path := strings.TrimSpace(decoded.Path)
	if path == "" {
		path = "/etc/containerd/certs.d"
	}
	return writeContainerdRegistryHosts(path, decoded)
}

func writeContainerdRegistryHosts(configRoot string, spec stepspec.WriteContainerdRegistryHosts) error {
	if len(spec.RegistryHosts) == 0 {
		return nil
	}

	configPath := configRoot

	for idx, entry := range spec.RegistryHosts {
		registry := strings.TrimSpace(entry.Registry)
		server := strings.TrimSpace(entry.Server)
		host := strings.TrimSpace(entry.Host)
		if registry == "" {
			return fmt.Errorf("registryHosts[%d].registry is required", idx)
		}
		if server == "" {
			return fmt.Errorf("registryHosts[%d].server is required", idx)
		}
		if host == "" {
			return fmt.Errorf("registryHosts[%d].host is required", idx)
		}

		caps, err := parseContainerdHostCapabilities(entry.Capabilities, idx)
		if err != nil {
			return err
		}

		hostsPath := filepath.Join(configPath, registry, "hosts.toml")
		if err := filemode.EnsureParentDir(hostsPath, filemode.PublishedArtifact); err != nil {
			return err
		}

		content := renderWriteContainerdConfigHostsTOML(server, host, caps, entry.SkipVerify)
		if err := writeFileIfChanged(hostsPath, []byte(content), 0o644); err != nil {
			return err
		}
	}

	return nil
}

func parseContainerdHostCapabilities(items []string, idx int) ([]string, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("registryHosts[%d].capabilities must not be empty", idx)
	}

	capabilities := make([]string, 0, len(items))
	for i, item := range items {
		capability := item
		if strings.TrimSpace(capability) == "" {
			return nil, fmt.Errorf("registryHosts[%d].capabilities[%d] must be a non-empty string", idx, i)
		}
		capabilities = append(capabilities, strings.TrimSpace(capability))
	}

	return capabilities, nil
}

func renderWriteContainerdConfigHostsTOML(server string, host string, capabilities []string, skipVerify bool) string {
	tomlCaps := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		tomlCaps = append(tomlCaps, fmt.Sprintf("%q", capability))
	}

	return fmt.Sprintf("server = %q\n\n[host.%q]\n  capabilities = [%s]\n  skip_verify = %t\n", server, host, strings.Join(tomlCaps, ", "), skipVerify)
}
