package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var installPackagesRunTimedCommandWithContext = runTimedCommandWithContext

var installPackagesLookPath = exec.LookPath

func runInstallPackages(ctx context.Context, spec map[string]any) error {
	if ctx == nil {
		ctx = context.Background()
	}

	pkgs := stringSlice(spec["packages"])
	if len(pkgs) == 0 {
		return fmt.Errorf("%s: InstallPackages requires packages", errCodeInstallPackagesRequired)
	}
	policy := packageRepoPolicyFromSpec(spec)

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
	if _, err := installPackagesLookPath("apt-get"); err == nil {
		installer = "apt-get"
	} else if _, err := installPackagesLookPath("dnf"); err == nil {
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
			if err := installPackagesRunTimedCommandWithContext(ctx, "apt-get", args, commandTimeoutWithDefault(spec, 10*time.Minute)); err != nil {
				if errors.Is(err, errStepCommandTimeout) || errors.Is(err, context.DeadlineExceeded) {
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
		if err := installPackagesRunTimedCommandWithContext(ctx, "dnf", args, commandTimeoutWithDefault(spec, 10*time.Minute)); err != nil {
			if errors.Is(err, errStepCommandTimeout) || errors.Is(err, context.DeadlineExceeded) {
				return fmt.Errorf("%s: package installation timed out: %w", errCodeInstallPkgFailed, err)
			}
			return fmt.Errorf("%s: package installation failed: %w", errCodeInstallPkgFailed, err)
		}
		return nil
	}

	args := []string{}
	cleanup := func() {}
	switch installer {
	case "apt-get":
		repoArgs, cleanupFn, err := aptRepoArgs(policy)
		if err != nil {
			return err
		}
		args = append(args, repoArgs...)
		if cleanupFn != nil {
			cleanup = cleanupFn
		}
	case "dnf":
		args = append(args, dnfRepoArgs(policy)...)
	}
	defer cleanup()
	args = append(args, "install", "-y")
	args = append(args, pkgs...)
	if err := installPackagesRunTimedCommandWithContext(ctx, installer, args, commandTimeoutWithDefault(spec, 10*time.Minute)); err != nil {
		if errors.Is(err, errStepCommandTimeout) || errors.Is(err, context.DeadlineExceeded) {
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
