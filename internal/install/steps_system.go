package install

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/taedi90/deck/internal/errcode"
	"github.com/taedi90/deck/internal/filemode"
	"github.com/taedi90/deck/internal/fsutil"
	"github.com/taedi90/deck/internal/hostfs"
	"github.com/taedi90/deck/internal/stepspec"
	"github.com/taedi90/deck/internal/workflowexec"
)

func runSysctl(ctx context.Context, spec map[string]any) error {
	decoded, err := workflowexec.DecodeSpec[stepspec.Sysctl](spec)
	if err != nil {
		return fmt.Errorf("decode Sysctl spec: %w", err)
	}
	path := strings.TrimSpace(decoded.WriteFile)
	if path == "" {
		return errcode.Newf(errCodeInstallSysctlPathMiss, "Sysctl requires writeFile")
	}
	hostPath, err := hostfs.NewHostPath(path)
	if err != nil {
		return err
	}

	values := decoded.Values
	if len(values) == 0 {
		return errcode.Newf(errCodeInstallSysctlValsMiss, "Sysctl requires values")
	}

	lines := make([]string, 0, len(values))
	for k, v := range values {
		lines = append(lines, fmt.Sprintf("%s=%v", k, v))
	}

	if err := hostPath.WriteFile([]byte(strings.Join(lines, "\n")+"\n"), filemode.PublishedArtifact); err != nil {
		return err
	}
	if decoded.Apply {
		applySpec := map[string]any{"file": path}
		if timeout := strings.TrimSpace(decoded.Timeout); timeout != "" {
			applySpec["timeout"] = timeout
		}
		return runSysctlApply(ctx, applySpec)
	}
	return nil
}

func runManageService(ctx context.Context, spec map[string]any) error {
	decoded, err := workflowexec.DecodeSpec[stepspec.ManageService](spec)
	if err != nil {
		return fmt.Errorf("decode ManageService spec: %w", err)
	}
	name := strings.TrimSpace(decoded.Name)
	names := decoded.Names
	if name == "" && len(names) == 0 {
		return errcode.Newf(errCodeInstallManageServiceNameMiss, "ManageService requires name or names")
	}
	if name != "" && len(names) > 0 {
		return errcode.Newf(errCodeInstallManageServiceNameMiss, "ManageService accepts either name or names")
	}
	if name != "" {
		names = []string{name}
	}

	timeout := parseStepTimeout(decoded.Timeout, 30*time.Second)
	if decoded.DaemonReload {
		if err := runTimedCommandWithContext(ctx, "systemctl", []string{"daemon-reload"}, timeout); err != nil {
			return err
		}
	}
	ifExists := decoded.IfExists
	ignoreMissing := decoded.IgnoreMissing

	for _, serviceName := range names {
		if ifExists {
			exists, err := serviceUnitExists(ctx, serviceName, timeout)
			if err != nil {
				return err
			}
			if !exists {
				continue
			}
		}

		if decoded.Enabled != nil {
			enabled := *decoded.Enabled
			isEnabled, err := isManageServiceEnabled(ctx, serviceName, timeout)
			if err != nil {
				return err
			}
			if enabled != isEnabled {
				action := "disable"
				if enabled {
					action = "enable"
				}
				if err := runServiceCommand(ctx, serviceName, []string{action, serviceName}, timeout, ignoreMissing); err != nil {
					return err
				}
			}
		}

		state := strings.ToLower(strings.TrimSpace(decoded.State))
		switch state {
		case "", "unchanged":
			continue
		case "started":
			active, err := isManageServiceActive(ctx, serviceName, timeout)
			if err != nil {
				return err
			}
			if active {
				continue
			}
			if err := runServiceCommand(ctx, serviceName, []string{"start", serviceName}, timeout, ignoreMissing); err != nil {
				return err
			}
		case "stopped":
			active, err := isManageServiceActive(ctx, serviceName, timeout)
			if err != nil {
				return err
			}
			if !active {
				continue
			}
			if err := runServiceCommand(ctx, serviceName, []string{"stop", serviceName}, timeout, ignoreMissing); err != nil {
				return err
			}
		case "restarted":
			if err := runServiceCommand(ctx, serviceName, []string{"restart", serviceName}, timeout, ignoreMissing); err != nil {
				return err
			}
		case "reloaded":
			if err := runServiceCommand(ctx, serviceName, []string{"reload", serviceName}, timeout, ignoreMissing); err != nil {
				return err
			}
		default:
			return fmt.Errorf("invalid service state %q", state)
		}
	}

	return nil
}

func runServiceCommand(ctx context.Context, name string, args []string, timeout time.Duration, ignoreMissing bool) error {
	err := runTimedCommandWithContext(ctx, "systemctl", args, timeout)
	if err == nil || !ignoreMissing {
		return err
	}
	exists, existsErr := serviceUnitExists(ctx, name, timeout)
	if existsErr != nil {
		return err
	}
	if !exists {
		return nil
	}
	return err
}

func runSwap(ctx context.Context, spec map[string]any) error {
	decoded, err := workflowexec.DecodeSpec[stepspec.Swap](spec)
	if err != nil {
		return fmt.Errorf("decode Swap spec: %w", err)
	}
	disable := true
	if decoded.Disable != nil {
		disable = *decoded.Disable
	}
	persist := true
	if decoded.Persist != nil {
		persist = *decoded.Persist
	}

	if disable {
		active, err := swapActive()
		if err != nil {
			return err
		}
		if active {
			if err := runTimedCommandWithContext(ctx, "swapoff", []string{"-a"}, parseStepTimeout(decoded.Timeout, 30*time.Second)); err != nil {
				return err
			}
		}
	}

	if persist {
		fstabPath := strings.TrimSpace(decoded.FstabPath)
		if fstabPath == "" {
			fstabPath = "/etc/fstab"
		}
		fstabRef, err := hostfs.NewHostPath(fstabPath)
		if err != nil {
			return err
		}
		content, err := fstabRef.ReadFile()
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
			if err := fstabRef.WriteFile([]byte(updated), filemode.PublishedArtifact); err != nil {
				return err
			}
		}
	}

	return nil
}

func runKernelModule(ctx context.Context, spec map[string]any) error {
	decoded, err := workflowexec.DecodeSpec[stepspec.KernelModule](spec)
	if err != nil {
		return fmt.Errorf("decode KernelModule spec: %w", err)
	}
	modules := kernelModuleNames(decoded)
	if len(modules) == 0 {
		return errcode.Newf(errCodeInstallKernelModuleMiss, "KernelModule requires name or names")
	}
	if decoded.Name != "" && len(decoded.Names) > 0 {
		return errcode.Newf(errCodeInstallKernelModuleMiss, "KernelModule accepts either name or names")
	}

	load := true
	if decoded.Load != nil {
		load = *decoded.Load
	}
	persist := true
	if decoded.Persist != nil {
		persist = *decoded.Persist
	}

	if persist {
		persistFile := strings.TrimSpace(decoded.PersistFile)
		if persistFile == "" {
			persistFile = "/etc/modules-load.d/k8s.conf"
		}
		persistRef, err := hostfs.NewHostPath(persistFile)
		if err != nil {
			return err
		}
		raw, err := persistRef.ReadFile()
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
		seen := map[string]bool{}
		contentLines := make([]string, 0, len(lines)+len(modules))
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			contentLines = append(contentLines, trimmed)
			seen[trimmed] = true
		}
		changed := false
		for _, module := range modules {
			if seen[module] {
				continue
			}
			contentLines = append(contentLines, module)
			seen[module] = true
			changed = true
		}
		if changed {
			content := strings.Join(contentLines, "\n") + "\n"
			if err := persistRef.WriteFile([]byte(content), filemode.PublishedArtifact); err != nil {
				return err
			}
		}
	}

	if load {
		for _, module := range modules {
			loaded, err := kernelModuleLoaded(module)
			if err != nil {
				return err
			}
			if !loaded {
				if err := runTimedCommandWithContext(ctx, "modprobe", []string{module}, parseStepTimeout(decoded.Timeout, 30*time.Second)); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func kernelModuleNames(spec stepspec.KernelModule) []string {
	items := make([]string, 0, 1+len(spec.Names))
	seen := map[string]bool{}
	appendName := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		items = append(items, name)
	}
	appendName(spec.Name)
	for _, name := range spec.Names {
		appendName(name)
	}
	return items
}

func runSysctlApply(ctx context.Context, spec map[string]any) error {
	file := stringValue(spec, "file")
	args := stringSlice(spec["command"])
	if len(args) == 0 {
		if file != "" {
			args = []string{"sysctl", "-p", file}
		} else {
			args = []string{"sysctl", "--system"}
		}
	}
	return runTimedCommandWithContext(ctx, args[0], args[1:], commandTimeoutWithDefault(spec, 30*time.Second))
}

func swapActive() (bool, error) {
	raw, err := fsutil.ReadFile("/proc/swaps")
	if err != nil {
		return false, err
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	return len(lines) > 1, nil
}

func kernelModuleLoaded(name string) (bool, error) {
	raw, err := fsutil.ReadFile("/proc/modules")
	if err != nil {
		if os.IsNotExist(err) {
			// /proc/modules is Linux-only; treat module as already loaded on other platforms.
			return true, nil
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
