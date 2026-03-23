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

	"github.com/taedi90/deck/internal/errcode"
	"github.com/taedi90/deck/internal/executil"
	"github.com/taedi90/deck/internal/stepspec"
	"github.com/taedi90/deck/internal/workflowexec"
)

func runInstallPackages(ctx context.Context, spec map[string]any) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}

	decoded, err := workflowexec.DecodeSpec[stepspec.InstallPackage](spec)
	if err != nil {
		return fmt.Errorf("decode InstallPackage spec: %w", err)
	}
	pkgs := decoded.Packages
	if len(pkgs) == 0 {
		return errcode.Newf(errCodeInstallPackagesRequired, "InstallPackages requires packages")
	}

	sourcePath := ""

	if decoded.Source != nil {
		typeVal := strings.TrimSpace(decoded.Source.Type)
		if typeVal != "" && typeVal != "local-repo" {
			return errcode.Newf(errCodeInstallPkgSourceInvalid, "unsupported source type %q", typeVal)
		}
		if path := strings.TrimSpace(decoded.Source.Path); path != "" {
			if info, err := os.Stat(path); err != nil || !info.IsDir() {
				return errcode.Newf(errCodeInstallPkgSourceInvalid, "source path must be an existing directory: %s", path)
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
		return errcode.Newf(errCodeInstallPkgMgrMissing, "apt-get or dnf not found")
	}

	if sourcePath != "" {
		if installer == "apt-get" {
			artifacts, err := collectPackageArtifact(sourcePath, ".deb")
			if err != nil {
				return errcode.New(errCodeInstallPkgSourceInvalid, err)
			}
			args := []string{"install", "-y"}
			args = append(args, artifacts...)
			if err := runTimedCommandWithContext(ctx, "apt-get", args, parseStepTimeout(decoded.Timeout, 10*time.Minute)); err != nil {
				if errors.Is(err, ErrStepCommandTimeout) || errors.Is(err, context.DeadlineExceeded) {
					return errcode.New(errCodeInstallPkgFailed, fmt.Errorf("package installation timed out: %w", err))
				}
				return errcode.New(errCodeInstallPkgFailed, fmt.Errorf("package installation failed: %w", err))
			}
			return nil
		}

		artifacts, err := collectPackageArtifact(sourcePath, ".rpm")
		if err != nil {
			return errcode.New(errCodeInstallPkgSourceInvalid, err)
		}
		args := []string{"install", "-y"}
		args = append(args, artifacts...)
		if err := runTimedCommandWithContext(ctx, "dnf", args, parseStepTimeout(decoded.Timeout, 10*time.Minute)); err != nil {
			if errors.Is(err, ErrStepCommandTimeout) || errors.Is(err, context.DeadlineExceeded) {
				return errcode.New(errCodeInstallPkgFailed, fmt.Errorf("package installation timed out: %w", err))
			}
			return errcode.New(errCodeInstallPkgFailed, fmt.Errorf("package installation failed: %w", err))
		}
		return nil
	}

	args := []string{"install", "-y"}
	policy := buildPackageRepoPolicy(decoded.RestrictToRepos, decoded.ExcludeRepos)
	cleanup := func() {}
	if installer == "apt-get" {
		repoArgs, repoCleanup, err := aptRepoArgs(policy)
		if err != nil {
			return errcode.New(errCodeInstallPkgSourceInvalid, err)
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
	if err := runTimedCommandWithContext(ctx, installer, args, parseStepTimeout(decoded.Timeout, 10*time.Minute)); err != nil {
		if errors.Is(err, ErrStepCommandTimeout) || errors.Is(err, context.DeadlineExceeded) {
			return errcode.New(errCodeInstallPkgFailed, fmt.Errorf("package installation timed out: %w", err))
		}
		return errcode.New(errCodeInstallPkgFailed, fmt.Errorf("package installation failed: %w", err))
	}
	return nil
}

func collectPackageArtifact(root, ext string) ([]string, error) {
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
