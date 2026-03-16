package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/taedi90/deck/internal/executil"
)

func runInstallPackages(ctx context.Context, spec map[string]any) error {
	if ctx == nil {
		ctx = context.Background()
	}

	pkgs := stringSlice(spec["packages"])
	if len(pkgs) == 0 {
		return fmt.Errorf("%s: InstallPackages requires packages", errCodeInstallPackagesRequired)
	}

	sourcePath := ""

	if src, ok := spec["source"].(map[string]any); ok {
		typeVal := stringValue(src, "type")
		if typeVal != "" && typeVal != "local-repo" {
			return fmt.Errorf("%s: unsupported source type %q", errCodeInstallPkgSourceInvalid, typeVal)
		}
		if path := stringValue(src, "path"); path != "" {
			if info, err := os.Stat(path); err != nil || !info.IsDir() {
				return fmt.Errorf("%s: source path must be an existing directory: %s", errCodeInstallPkgSourceInvalid, path)
			}
			sourcePath = path
		}
	}

	installer := ""
	if _, err := executil.LookPathAptGet(); err == nil {
		installer = "apt-get"
	} else if _, err := executil.LookPathDnf(); err == nil {
		installer = "dnf"
	}
	if installer == "" {
		return fmt.Errorf("%s: apt-get or dnf not found", errCodeInstallPkgMgrMissing)
	}

	if sourcePath != "" {
		if installer == "apt-get" {
			artifacts, err := collectPackageArtifacts(sourcePath, ".deb")
			if err != nil {
				return fmt.Errorf("%s: %w", errCodeInstallPkgSourceInvalid, err)
			}
			args := []string{"install", "-y"}
			args = append(args, artifacts...)
			if err := runTimedCommandWithContext(ctx, "apt-get", args, commandTimeoutWithDefault(spec, 10*time.Minute)); err != nil {
				if errors.Is(err, ErrStepCommandTimeout) || errors.Is(err, context.DeadlineExceeded) {
					return fmt.Errorf("%s: package installation timed out: %w", errCodeInstallPkgFailed, err)
				}
				return fmt.Errorf("%s: package installation failed: %w", errCodeInstallPkgFailed, err)
			}
			return nil
		}

		artifacts, err := collectPackageArtifacts(sourcePath, ".rpm")
		if err != nil {
			return fmt.Errorf("%s: %w", errCodeInstallPkgSourceInvalid, err)
		}
		args := []string{"install", "-y"}
		args = append(args, artifacts...)
		if err := runTimedCommandWithContext(ctx, "dnf", args, commandTimeoutWithDefault(spec, 10*time.Minute)); err != nil {
			if errors.Is(err, ErrStepCommandTimeout) || errors.Is(err, context.DeadlineExceeded) {
				return fmt.Errorf("%s: package installation timed out: %w", errCodeInstallPkgFailed, err)
			}
			return fmt.Errorf("%s: package installation failed: %w", errCodeInstallPkgFailed, err)
		}
		return nil
	}

	args := []string{"install", "-y"}
	policy := packageRepoPolicyFromSpec(spec)
	cleanup := func() {}
	if installer == "apt-get" {
		repoArgs, repoCleanup, err := aptRepoArgs(policy)
		if err != nil {
			return fmt.Errorf("%s: %w", errCodeInstallPkgSourceInvalid, err)
		}
		if repoCleanup != nil {
			cleanup = repoCleanup
		}
		args = append(args, repoArgs...)
	} else {
		args = append(args, dnfRepoArgs(policy)...)
	}
	defer cleanup()
	args = append(args, pkgs...)
	if err := runTimedCommandWithContext(ctx, installer, args, commandTimeoutWithDefault(spec, 10*time.Minute)); err != nil {
		if errors.Is(err, ErrStepCommandTimeout) || errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("%s: package installation timed out: %w", errCodeInstallPkgFailed, err)
		}
		return fmt.Errorf("%s: package installation failed: %w", errCodeInstallPkgFailed, err)
	}
	return nil
}

func collectPackageArtifacts(root, ext string) ([]string, error) {
	artifacts := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(d.Name()), strings.ToLower(ext)) {
			artifacts = append(artifacts, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(artifacts) == 0 {
		return nil, fmt.Errorf("no %s artifacts found under %s", ext, root)
	}
	sort.Strings(artifacts)
	return artifacts, nil
}
