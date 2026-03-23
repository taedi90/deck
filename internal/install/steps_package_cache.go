package install

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/taedi90/deck/internal/errcode"
	"github.com/taedi90/deck/internal/stepspec"
	"github.com/taedi90/deck/internal/workflowexec"
)

const defaultRefreshRepositoryTimeout = 2 * time.Minute

func runRefreshRepository(ctx context.Context, spec map[string]any) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	decoded, err := workflowexec.DecodeSpec[stepspec.RefreshRepository](spec)
	if err != nil {
		return fmt.Errorf("decode RefreshRepository spec: %w", err)
	}
	return runRefreshRepositoryWithRunnerSpec(decoded, func(name string, args []string, timeout time.Duration) error {
		return runTimedCommandWithContext(ctx, name, args, timeout)
	})
}

func runRefreshRepositoryWithRunner(spec map[string]any, runner func(name string, args []string, timeout time.Duration) error) error {
	decoded, err := workflowexec.DecodeSpec[stepspec.RefreshRepository](spec)
	if err != nil {
		return fmt.Errorf("decode RefreshRepository spec: %w", err)
	}
	return runRefreshRepositoryWithRunnerSpec(decoded, runner)
}

func runRefreshRepositoryWithRunnerSpec(spec stepspec.RefreshRepository, runner func(name string, args []string, timeout time.Duration) error) error {
	manager, err := resolveRefreshRepositoryManager(spec)
	if err != nil {
		return err
	}

	clean := spec.Clean
	update := spec.Update
	if !clean && !update {
		return errcode.Newf(errCodeInstallRefreshRepositoryMgr, "RefreshRepository requires clean and/or update")
	}

	return runRefreshRepositoryCommands(
		manager,
		clean,
		update,
		buildPackageRepoPolicy(spec.RestrictToRepos, spec.ExcludeRepos),
		parseStepTimeout(spec.Timeout, defaultRefreshRepositoryTimeout),
		runner,
		"package cache refresh",
	)
}

func resolveRefreshRepositoryManager(spec stepspec.RefreshRepository) (string, error) {
	manager := strings.TrimSpace(spec.Manager)
	if manager == "" {
		manager = "auto"
	}

	switch manager {
	case "apt", "dnf":
		return manager, nil
	case "auto":
		autoFormat, err := resolveRepoConfigFormat("auto")
		if err != nil {
			return "", err
		}
		return repoConfigFormatToPackageManager(autoFormat), nil
	default:
		return "", errcode.Newf(errCodeInstallRefreshRepositoryMgr, "RefreshRepository manager must be one of auto, apt, dnf")
	}
}

func repoConfigFormatToPackageManager(format string) string {
	if format == "deb" {
		return "apt"
	}
	return "dnf"
}

func runRefreshRepositoryCommands(
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
		return errcode.Newf(errCodeInstallRefreshRepositoryMgr, "unsupported package cache manager %q", manager)
	}

	return nil
}
