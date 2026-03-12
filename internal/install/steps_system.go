package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func runSysctl(spec map[string]any) error {
	path := stringValue(spec, "writeFile")
	if path == "" {
		path = stringValue(spec, "dest")
	}
	if path == "" {
		return fmt.Errorf("%s: Sysctl requires writeFile or dest", errCodeInstallSysctlPathMiss)
	}

	values, ok := spec["values"].(map[string]any)
	if !ok || len(values) == 0 {
		return fmt.Errorf("%s: Sysctl requires values", errCodeInstallSysctlValsMiss)
	}

	lines := make([]string, 0, len(values))
	for k, v := range values {
		lines = append(lines, fmt.Sprintf("%s=%v", k, v))
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

func runModprobe(spec map[string]any) error {
	persistPath := stringValue(spec, "persistFile")
	if persistPath == "" {
		return nil
	}

	mods := stringSlice(spec["modules"])
	if len(mods) == 0 {
		return fmt.Errorf("%s: Modprobe requires modules", errCodeInstallModulesMissing)
	}

	if err := os.MkdirAll(filepath.Dir(persistPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(persistPath, []byte(strings.Join(mods, "\n")+"\n"), 0o644)
}

func runService(spec map[string]any) error {
	name := stringValue(spec, "name")
	if name == "" {
		return fmt.Errorf("%s: Service requires name", errCodeInstallServiceNameMiss)
	}

	timeout := commandTimeoutWithDefault(spec, 30*time.Second)
	if enabled, ok := spec["enabled"].(bool); ok {
		isEnabled, err := isServiceEnabled(name, timeout)
		if err != nil {
			return err
		}
		if enabled != isEnabled {
			if enabled {
				if err := runTimedCommand("systemctl", []string{"enable", name}, timeout); err != nil {
					return err
				}
			} else {
				if err := runTimedCommand("systemctl", []string{"disable", name}, timeout); err != nil {
					return err
				}
			}
		}
	}

	state := strings.ToLower(stringValue(spec, "state"))
	switch state {
	case "", "unchanged":
		return nil
	case "started":
		active, err := isServiceActive(name, timeout)
		if err != nil {
			return err
		}
		if active {
			return nil
		}
		return runTimedCommand("systemctl", []string{"start", name}, timeout)
	case "stopped":
		active, err := isServiceActive(name, timeout)
		if err != nil {
			return err
		}
		if !active {
			return nil
		}
		return runTimedCommand("systemctl", []string{"stop", name}, timeout)
	case "restarted":
		return runTimedCommand("systemctl", []string{"restart", name}, timeout)
	case "reloaded":
		return runTimedCommand("systemctl", []string{"reload", name}, timeout)
	default:
		return fmt.Errorf("invalid service state %q", state)
	}
}

func runSwap(spec map[string]any) error {
	disable := true
	if v, ok := spec["disable"].(bool); ok {
		disable = v
	}
	persist := true
	if v, ok := spec["persist"].(bool); ok {
		persist = v
	}

	if disable {
		active, err := swapActive()
		if err != nil {
			return err
		}
		if active {
			if err := runTimedCommand("swapoff", []string{"-a"}, commandTimeoutWithDefault(spec, 30*time.Second)); err != nil {
				return err
			}
		}
	}

	if persist {
		fstabPath := stringValue(spec, "fstabPath")
		if fstabPath == "" {
			fstabPath = "/etc/fstab"
		}
		content, err := os.ReadFile(fstabPath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		lines := strings.Split(string(content), "\n")
		changed := false
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			parts := strings.Fields(trimmed)
			if len(parts) > 2 && parts[2] == "swap" {
				lines[i] = "# " + line
				changed = true
			}
		}
		if changed {
			updated := strings.Join(lines, "\n")
			if !strings.HasSuffix(updated, "\n") {
				updated += "\n"
			}
			if err := os.WriteFile(fstabPath, []byte(updated), 0o644); err != nil {
				return err
			}
		}
	}

	return nil
}

func runKernelModule(spec map[string]any) error {
	name := stringValue(spec, "name")
	if name == "" {
		return fmt.Errorf("%s: KernelModule requires name", errCodeInstallKernelModuleMiss)
	}

	load := true
	if v, ok := spec["load"].(bool); ok {
		load = v
	}
	persist := true
	if v, ok := spec["persist"].(bool); ok {
		persist = v
	}

	if persist {
		persistFile := stringValue(spec, "persistFile")
		if persistFile == "" {
			persistFile = "/etc/modules-load.d/k8s.conf"
		}
		if err := os.MkdirAll(filepath.Dir(persistFile), 0o755); err != nil {
			return err
		}
		raw, err := os.ReadFile(persistFile)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		lines := strings.Split(string(raw), "\n")
		present := false
		for _, line := range lines {
			if strings.TrimSpace(line) == name {
				present = true
				break
			}
		}
		if !present {
			content := strings.TrimRight(string(raw), "\n")
			if content != "" {
				content += "\n"
			}
			content += name + "\n"
			if err := os.WriteFile(persistFile, []byte(content), 0o644); err != nil {
				return err
			}
		}
	}

	if load {
		loaded, err := kernelModuleLoaded(name)
		if err != nil {
			return err
		}
		if !loaded {
			if err := runTimedCommand("modprobe", []string{name}, commandTimeoutWithDefault(spec, 30*time.Second)); err != nil {
				return err
			}
		}
	}

	return nil
}

func runSysctlApply(spec map[string]any) error {
	file := stringValue(spec, "file")
	args := stringSlice(spec["command"])
	if len(args) == 0 {
		if file != "" {
			args = []string{"sysctl", "-p", file}
		} else {
			args = []string{"sysctl", "--system"}
		}
	}
	return runTimedCommand(args[0], args[1:], commandTimeoutWithDefault(spec, 30*time.Second))
}

func swapActive() (bool, error) {
	raw, err := os.ReadFile("/proc/swaps")
	if err != nil {
		return false, err
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	return len(lines) > 1, nil
}

func kernelModuleLoaded(name string) (bool, error) {
	raw, err := os.ReadFile("/proc/modules")
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	target := strings.ReplaceAll(name, "-", "_")
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == name || fields[0] == target {
			return true, nil
		}
	}
	return false, nil
}
