package install

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const defaultPackageCacheTimeout = 2 * time.Minute

var packageCacheRunTimedCommand = runTimedCommand

func runPackageCache(spec map[string]any) error {
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
		commandTimeoutWithDefault(spec, defaultPackageCacheTimeout),
		packageCacheRunTimedCommand,
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
	timeout time.Duration,
	runner func(name string, args []string, timeout time.Duration) error,
	timeoutContext string,
) error {
	run := func(name string, args []string) error {
		err := runner(name, args, timeout)
		if err == nil {
			return nil
		}
		if errors.Is(err, errStepCommandTimeout) {
			return fmt.Errorf("%s timed out: %w", timeoutContext, err)
		}
		return err
	}

	switch strings.TrimSpace(manager) {
	case "apt":
		if clean {
			if err := run("apt-get", []string{"clean"}); err != nil {
				return err
			}
		}
		if update {
			if err := run("apt-get", []string{"update"}); err != nil {
				return err
			}
		}
	case "dnf":
		if clean {
			if err := run("dnf", []string{"clean", "all"}); err != nil {
				return err
			}
		}
		if update {
			if err := run("dnf", []string{"makecache", "-y"}); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("%s: unsupported package cache manager %q", errCodeInstallPackageCacheMgr, manager)
	}

	return nil
}
