package install

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const defaultPackageCacheTimeout = 2 * time.Minute

func runPackageCache(spec map[string]any) error {
	return runPackageCacheWithRunner(spec, runTimedCommand)
}

func runPackageCacheWithRunner(spec map[string]any, runner func(name string, args []string, timeout time.Duration) error) error {
	manager, err := resolvePackageCacheManager(spec)
	if err != nil {
		return err
	}

	clean := boolValue(spec, "clean")
	update := boolValue(spec, "update")
	if !clean && !update {
		return fmt.Errorf("%s: PackageCache requires clean and/or update", errCodeInstallPackageCacheMgr)
	}

	return runPackageCacheCommands(
		manager,
		clean,
		update,
		packageRepoPolicyFromSpec(spec),
		commandTimeoutWithDefault(spec, defaultPackageCacheTimeout),
		runner,
		"package cache refresh",
	)
}

func resolvePackageCacheManager(spec map[string]any) (string, error) {
	manager := stringValue(spec, "manager")
	if manager == "" {
		manager = "auto"
	}

	switch manager {
	case "apt", "dnf":
		return manager, nil
	case "auto":
		autoFormat, err := resolveRepoConfigFormat(map[string]any{"format": "auto"})
		if err != nil {
			return "", err
		}
		return repoConfigFormatToPackageManager(autoFormat), nil
	default:
		return "", fmt.Errorf("%s: PackageCache manager must be one of auto, apt, dnf", errCodeInstallPackageCacheMgr)
	}
}

func repoConfigFormatToPackageManager(format string) string {
	if format == "apt" {
		return "apt"
	}
	return "dnf"
}

func runPackageCacheCommands(
	manager string,
	clean bool,
	update bool,
	policy packageRepoPolicy,
	timeout time.Duration,
	runner func(name string, args []string, timeout time.Duration) error,
	timeoutContext string,
) error {
	run := func(name string, args []string) error {
		err := runner(name, args, timeout)
		if err == nil {
			return nil
		}
		if errors.Is(err, ErrStepCommandTimeout) {
			return fmt.Errorf("%s timed out: %w", timeoutContext, err)
		}
		return err
	}

	switch strings.TrimSpace(manager) {
	case "apt":
		repoArgs, cleanup, err := aptRepoArgs(policy)
		if err != nil {
			return err
		}
		if cleanup != nil {
			defer cleanup()
		}
		if clean {
			if err := run("apt-get", []string{"clean"}); err != nil {
				return err
			}
		}
		if update {
			args := append([]string{}, repoArgs...)
			args = append(args, "update")
			if err := run("apt-get", args); err != nil {
				return err
			}
		}
	case "dnf":
		dnfArgs := dnfRepoArgs(policy)
		if clean {
			args := append([]string{}, dnfArgs...)
			args = append(args, "clean", "all")
			if err := run("dnf", args); err != nil {
				return err
			}
		}
		if update {
			args := append([]string{}, dnfArgs...)
			args = append(args, "makecache", "-y")
			if err := run("dnf", args); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("%s: unsupported package cache manager %q", errCodeInstallPackageCacheMgr, manager)
	}

	return nil
}
