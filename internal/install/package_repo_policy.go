package install

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/taedi90/deck/internal/errcode"
	"github.com/taedi90/deck/internal/filemode"
	"github.com/taedi90/deck/internal/hostfs"
)

type packageRepoPolicy struct {
	RestrictTo []string
	Exclude    []string
}

type aptRepoSelection struct {
	MainFile string
	PartsDir string
	Cleanup  func()
}

func buildPackageRepoPolicy(restrictTo, exclude []string) packageRepoPolicy {
	return packageRepoPolicy{
		RestrictTo: restrictTo,
		Exclude:    exclude,
	}
}

func aptRepoArgs(policy packageRepoPolicy) ([]string, func(), error) {
	if len(policy.RestrictTo) == 0 && len(policy.Exclude) == 0 {
		return nil, nil, nil
	}
	selection, err := prepareAPTRepoSelection(policy)
	if err != nil {
		return nil, nil, err
	}
	args := []string{
		"-o", "Dir::Etc::sourcelist=" + selection.MainFile,
		"-o", "Dir::Etc::sourceparts=" + selection.PartsDir,
	}
	return args, selection.Cleanup, nil
}

func prepareAPTRepoSelection(policy packageRepoPolicy) (aptRepoSelection, error) {
	selected, err := selectAPTRepoPaths(policy)
	if err != nil {
		return aptRepoSelection{}, err
	}
	tmpRoot, err := os.MkdirTemp("", "deck-apt-repos-*")
	if err != nil {
		return aptRepoSelection{}, err
	}
	cleanup := func() { _ = os.RemoveAll(tmpRoot) }
	mainFile := filepath.Join(tmpRoot, "sources.list")
	partsDir := filepath.Join(tmpRoot, "sources.list.d")
	if err := filemode.EnsureArtifactDir(partsDir); err != nil {
		cleanup()
		return aptRepoSelection{}, err
	}
	if err := filemode.WriteArtifactFile(mainFile, nil); err != nil {
		cleanup()
		return aptRepoSelection{}, err
	}
	for _, path := range selected {
		hostPath, err := hostfs.NewHostPath(path)
		if err != nil {
			cleanup()
			return aptRepoSelection{}, err
		}
		raw, err := hostPath.ReadFile()
		if err != nil {
			cleanup()
			return aptRepoSelection{}, err
		}
		dest := partsDir
		name := filepath.Base(path)
		if filepath.Clean(path) == "/etc/apt/sources.list" {
			dest = filepath.Dir(mainFile)
			name = filepath.Base(mainFile)
		}
		if err := filemode.WriteArtifactFile(filepath.Join(dest, name), raw); err != nil {
			cleanup()
			return aptRepoSelection{}, err
		}
	}
	return aptRepoSelection{MainFile: mainFile, PartsDir: partsDir, Cleanup: cleanup}, nil
}

func selectAPTRepoPaths(policy packageRepoPolicy) ([]string, error) {
	var paths []string
	var err error
	if len(policy.RestrictTo) > 0 {
		paths, err = resolveRepoConfigPaths(policy.RestrictTo)
		if err != nil {
			return nil, err
		}
	} else {
		paths, err = resolveRepoConfigPaths(defaultRepoConfigCleanupPatterns("deb"))
		if err != nil {
			return nil, err
		}
	}
	excluded, err := resolveRepoConfigPaths(policy.Exclude)
	if err != nil {
		return nil, err
	}
	excludedSet := map[string]bool{}
	for _, path := range excluded {
		excludedSet[filepath.Clean(path)] = true
	}
	filtered := make([]string, 0, len(paths))
	for _, path := range paths {
		if excludedSet[filepath.Clean(path)] {
			continue
		}
		filtered = append(filtered, path)
	}
	if len(filtered) == 0 {
		return nil, errcode.Newf(errCodeInstallPkgSourceInvalid, "no apt repo files remain after applying repo policy")
	}
	return filtered, nil
}

func dnfRepoArgs(policy packageRepoPolicy) []string {
	args := make([]string, 0, len(policy.RestrictTo)+len(policy.Exclude)+1)
	if len(policy.RestrictTo) > 0 {
		args = append(args, "--disablerepo=*")
		for _, repo := range policy.RestrictTo {
			repo = strings.TrimSpace(repo)
			if repo != "" {
				args = append(args, "--enablerepo="+repo)
			}
		}
	}
	for _, repo := range policy.Exclude {
		repo = strings.TrimSpace(repo)
		if repo != "" {
			args = append(args, "--disablerepo="+repo)
		}
	}
	return args
}
