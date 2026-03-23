package install

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/taedi90/deck/internal/errcode"
	"github.com/taedi90/deck/internal/filemode"
	"github.com/taedi90/deck/internal/fsutil"
	"github.com/taedi90/deck/internal/hostfs"
	"github.com/taedi90/deck/internal/stepspec"
	"github.com/taedi90/deck/internal/structurededit"
	"github.com/taedi90/deck/internal/workflowexec"
)

var (
	repoConfigDetectHostFacts = detectHostFacts
	repoConfigRunTimedCommand = runTimedCommandWithContext
	repoConfigDefaultPathFunc = defaultRepoConfigPath
)

var yumEnabledTruePattern = regexp.MustCompile(`(?i)^\s*enabled\s*=\s*(1|yes|true)\s*$`)

func runEditFile(spec map[string]any) error {
	decoded, err := workflowexec.DecodeSpec[stepspec.EditFile](spec)
	if err != nil {
		return fmt.Errorf("decode EditFile spec: %w", err)
	}
	path := strings.TrimSpace(decoded.Path)
	if path == "" {
		return errcode.Newf(errCodeInstallEditPathMissing, "EditFile requires path")
	}
	hostPath, err := hostfs.NewHostPath(path)
	if err != nil {
		return err
	}

	content, err := hostPath.ReadFile()
	if err != nil {
		return err
	}
	if editFileBackupEnabledValue(decoded.Backup) {
		backupPath, err := createEditFileBackup(hostPath.Abs(), content)
		if err != nil {
			return fmt.Errorf("create backup %s: %w", backupPath, err)
		}
		if err := trimEditFileBackups(path, 10); err != nil {
			return fmt.Errorf("trim backups after %s: %w", backupPath, err)
		}
	}
	updated := string(content)

	if len(decoded.Edits) == 0 {
		return errcode.Newf(errCodeInstallEditsMissing, "EditFile requires edits")
	}

	for _, edit := range decoded.Edits {
		match := strings.TrimSpace(edit.Match)
		with := edit.ReplaceWith
		if match == "" {
			continue
		}
		switch strings.TrimSpace(edit.Op) {
		case "", "replace":
			updated = strings.ReplaceAll(updated, match, with)
		case "append":
			updated = strings.ReplaceAll(updated, match, match+with)
		default:
			return errcode.Newf(errCodeInstallEditsMissing, "unsupported edit op %q", edit.Op)
		}
	}

	if err := hostPath.WriteFile([]byte(updated), filemode.PublishedArtifact); err != nil {
		return err
	}
	return applyOptionalFileMode(hostPath, strings.TrimSpace(decoded.Mode))
}

func runEditTOML(spec map[string]any) error {
	decoded, err := workflowexec.DecodeSpec[stepspec.EditTOML](spec)
	if err != nil {
		return fmt.Errorf("decode EditTOML spec: %w", err)
	}
	return runStructuredEdit(decoded.Path, decoded.CreateIfMissing, decoded.Edits, decoded.Mode, structurededit.FormatTOML)
}

func runEditYAML(spec map[string]any) error {
	decoded, err := workflowexec.DecodeSpec[stepspec.EditYAML](spec)
	if err != nil {
		return fmt.Errorf("decode EditYAML spec: %w", err)
	}
	return runStructuredEdit(decoded.Path, decoded.CreateIfMissing, decoded.Edits, decoded.Mode, structurededit.FormatYAML)
}

func runEditJSON(spec map[string]any) error {
	decoded, err := workflowexec.DecodeSpec[stepspec.EditJSON](spec)
	if err != nil {
		return fmt.Errorf("decode EditJSON spec: %w", err)
	}
	return runStructuredEdit(decoded.Path, decoded.CreateIfMissing, decoded.Edits, decoded.Mode, structurededit.FormatJSON)
}

func runStructuredEdit(path string, createIfMissing *bool, edits []stepspec.StructuredEdit, mode string, format structurededit.Format) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("structured edit requires path")
	}
	if len(edits) == 0 {
		return fmt.Errorf("structured edit requires edits")
	}
	content, err := readStructuredEditContent(path, createIfMissing)
	if err != nil {
		return err
	}
	return applyStructuredEdits(path, content, edits, mode, format)
}

func readStructuredEditContent(path string, createIfMissing *bool) ([]byte, error) {
	hostPath, err := hostfs.NewHostPath(path)
	if err != nil {
		return nil, err
	}
	content, err := hostPath.ReadFile()
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		if createIfMissing == nil || !*createIfMissing {
			return nil, err
		}
		return []byte{}, nil
	}
	return content, nil
}

func applyStructuredEdits(path string, content []byte, edits []stepspec.StructuredEdit, mode string, format structurededit.Format) error {
	hostPath, err := hostfs.NewHostPath(path)
	if err != nil {
		return err
	}
	updated, err := structurededit.Apply(format, content, edits)
	if err != nil {
		return err
	}
	if err := hostPath.EnsureParentDir(filemode.PublishedArtifact); err != nil {
		return err
	}
	if err := hostPath.WriteFile(updated, filemode.PublishedArtifact); err != nil {
		return err
	}
	return applyOptionalFileMode(hostPath, strings.TrimSpace(mode))
}

func runCopyFile(ctx context.Context, bundleRoot string, spec map[string]any) error {
	decoded, err := workflowexec.DecodeSpec[stepspec.CopyFile](spec)
	if err != nil {
		return fmt.Errorf("decode CopyFile spec: %w", err)
	}
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	dest := strings.TrimSpace(decoded.Path)
	if dest == "" {
		return errcode.Newf(errCodeInstallCopyPathMissing, "CopyFile requires path")
	}
	if strings.TrimSpace(decoded.Source.Path) == "" && strings.TrimSpace(decoded.Source.URL) == "" && decoded.Source.Bundle == nil {
		return errcode.Newf(errCodeInstallCopyPathMissing, "CopyFile requires source")
	}
	destPath, err := hostfs.NewHostPath(dest)
	if err != nil {
		return err
	}
	tmpDir, err := os.MkdirTemp("", "deck-copy-file-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	downloadSpec := map[string]any{
		"source": map[string]any{
			"url":    decoded.Source.URL,
			"path":   decoded.Source.Path,
			"sha256": decoded.Source.SHA256,
		},
		"fetch": map[string]any{
			"offlineOnly": decoded.Fetch.OfflineOnly,
			"sources":     fetchSourcesAny(decoded.Fetch.Sources),
		},
		"outputPath": "copy.bin",
	}
	if decoded.Source.Bundle != nil {
		downloadSpec["source"].(map[string]any)["bundle"] = map[string]any{"root": decoded.Source.Bundle.Root, "path": decoded.Source.Bundle.Path}
	}
	downloadRoot := tmpDir
	if decoded.Source.Bundle != nil {
		downloadRoot = bundleRoot
	}
	relPath, err := runDownloadFile(ctx, downloadRoot, downloadSpec)
	if err != nil {
		return err
	}
	contentPath := filepath.Join(tmpDir, "copy.bin")
	if decoded.Source.Bundle != nil {
		contentPath = filepath.Join(bundleRoot, relPath)
	}
	content, err := fsutil.ReadFile(contentPath)
	if err != nil {
		return err
	}
	if err := destPath.WriteFile(content, filemode.PublishedArtifact); err != nil {
		return err
	}
	return applyOptionalFileMode(destPath, strings.TrimSpace(decoded.Mode))
}

func runEnsureDir(spec map[string]any) error {
	decoded, err := workflowexec.DecodeSpec[stepspec.EnsureDirectory](spec)
	if err != nil {
		return fmt.Errorf("decode EnsureDirectory spec: %w", err)
	}
	path := strings.TrimSpace(decoded.Path)
	if path == "" {
		return errcode.Newf(errCodeInstallEnsureDirPathMis, "EnsureDir requires path")
	}
	hostPath, err := hostfs.NewHostPath(path)
	if err != nil {
		return err
	}
	if err := hostPath.EnsureDir(filemode.PublishedArtifact); err != nil {
		return err
	}
	if modeRaw := strings.TrimSpace(decoded.Mode); modeRaw != "" {
		modeVal, err := strconv.ParseUint(modeRaw, 8, 32)
		if err != nil {
			return fmt.Errorf("invalid mode: %w", err)
		}
		if err := os.Chmod(path, os.FileMode(modeVal)); err != nil {
			return err
		}
	}
	return nil
}

func runCreateSymlink(spec map[string]any) error {
	decoded, err := workflowexec.DecodeSpec[stepspec.CreateSymlink](spec)
	if err != nil {
		return fmt.Errorf("decode CreateSymlink spec: %w", err)
	}
	path := strings.TrimSpace(decoded.Path)
	if path == "" {
		return errcode.Newf(errCodeInstallCreateSymlinkPathMiss, "CreateSymlink requires path")
	}
	target := strings.TrimSpace(decoded.Target)
	if target == "" {
		return errcode.Newf(errCodeInstallCreateSymlinkTargetMis, "CreateSymlink requires target")
	}

	pathRef, err := hostfs.NewHostPath(path)
	if err != nil {
		return err
	}
	if decoded.CreateParent {
		if err := pathRef.EnsureParentDir(filemode.PublishedArtifact); err != nil {
			return err
		}
	}

	if decoded.RequireTarget {
		if _, err := os.Lstat(target); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("symlink target does not exist: %s", target)
			}
			return err
		}
	} else if decoded.IgnoreMissingTarget {
		if _, err := os.Lstat(target); err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
	}

	if info, err := pathRef.Lstat(); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			currentTarget, readErr := pathRef.Readlink()
			if readErr != nil {
				return readErr
			}
			if currentTarget == target {
				return nil
			}
		}

		if !decoded.Force {
			return fmt.Errorf("destination already exists: %s", path)
		}
		if info.IsDir() {
			return fmt.Errorf("destination is a directory and cannot be replaced: %s", path)
		}

		if removeErr := pathRef.Remove(); removeErr != nil {
			return removeErr
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	return pathRef.CreateSymlink(target)
}

func runWriteFile(spec map[string]any) error {
	decoded, err := workflowexec.DecodeSpec[stepspec.WriteFile](spec)
	if err != nil {
		return fmt.Errorf("decode WriteFile spec: %w", err)
	}
	path := strings.TrimSpace(decoded.Path)
	if path == "" {
		return errcode.Newf(errCodeInstallInstallFilePath, "WriteFile requires path")
	}
	hostPath, err := hostfs.NewHostPath(path)
	if err != nil {
		return err
	}
	content := decoded.Content
	if content == "" {
		if from := decoded.Template; from != "" {
			content = from
		}
	}
	if content == "" {
		return errcode.Newf(errCodeInstallInstallFileInput, "WriteFile requires content")
	}
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	if err := hostfs.WriteFileIfChanged(hostPath, []byte(content), 0o644); err != nil {
		return err
	}
	return applyOptionalFileMode(hostPath, strings.TrimSpace(decoded.Mode))
}

func fetchSourcesAny(items []stepspec.FileFetchSource) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{"type": item.Type, "path": item.Path, "url": item.URL})
	}
	return out
}

func applyOptionalFileMode(path hostfs.HostPath, modeRaw string) error {
	if strings.TrimSpace(modeRaw) == "" {
		return nil
	}
	modeVal, err := strconv.ParseUint(strings.TrimSpace(modeRaw), 8, 32)
	if err != nil {
		return fmt.Errorf("invalid mode: %w", err)
	}
	return path.Chmod(os.FileMode(modeVal))
}

func runTemplateFile(spec map[string]any) error {
	decoded, err := workflowexec.DecodeSpec[stepspec.WriteFile](spec)
	if err != nil {
		return fmt.Errorf("decode TemplateFile spec: %w", err)
	}
	path := strings.TrimSpace(decoded.Path)
	if path == "" {
		return errcode.Newf(errCodeInstallTemplatePathMiss, "TemplateFile requires path")
	}
	body := decoded.Template
	if body == "" {
		return errcode.Newf(errCodeInstallTemplateBodyMiss, "TemplateFile requires template")
	}
	return runWriteFile(map[string]any{
		"path":    path,
		"content": body,
		"mode":    decoded.Mode,
	})
}

func runRepoConfig(ctx context.Context, spec map[string]any) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	decoded, err := workflowexec.DecodeSpec[stepspec.ConfigureRepository](spec)
	if err != nil {
		return fmt.Errorf("decode ConfigureRepository spec: %w", err)
	}
	format, err := resolveRepoConfigFormat(decoded.Format)
	if err != nil {
		return err
	}

	path := strings.TrimSpace(decoded.Path)
	if path == "" {
		path = repoConfigDefaultPathFunc(format)
	}
	if path == "" {
		return errcode.Newf(errCodeInstallRepoConfigPath, "RepoConfig requires path")
	}

	if len(decoded.Repositories) == 0 {
		return fmt.Errorf("RepoConfig requires repositories")
	}

	replaceExisting := decoded.ReplaceExisting
	disableExisting := decoded.DisableExisting

	backupPatterns := append([]string{}, decoded.BackupPaths...)
	cleanupPatterns := append([]string{}, decoded.CleanupPaths...)

	if (replaceExisting || disableExisting) && len(backupPatterns) == 0 {
		backupPatterns = append(backupPatterns, defaultRepoConfigBackupPatterns(format)...)
	}
	if replaceExisting && len(cleanupPatterns) == 0 {
		cleanupPatterns = append(cleanupPatterns, defaultRepoConfigCleanupPatterns(format)...)
	}
	if format == "deb" && disableExisting && !replaceExisting && len(cleanupPatterns) == 0 {
		cleanupPatterns = append(cleanupPatterns, backupPatterns...)
	}

	if err := backupRepoConfigPaths(backupPatterns); err != nil {
		return err
	}
	if format == "rpm" && disableExisting && !replaceExisting {
		if err := disableYumRepoPaths(backupPatterns, path); err != nil {
			return err
		}
	}
	if err := cleanupRepoConfigPaths(cleanupPatterns); err != nil {
		return err
	}

	content, err := renderRepoConfigContent(format, decoded.Repositories)
	if err != nil {
		return err
	}

	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	hostPath, err := hostfs.NewHostPath(path)
	if err != nil {
		return err
	}
	if err := hostPath.EnsureParentDir(filemode.PublishedArtifact); err != nil {
		return err
	}
	if err := writeFileIfChanged(path, []byte(content), 0o644); err != nil {
		return err
	}
	if modeRaw := strings.TrimSpace(decoded.Mode); modeRaw != "" {
		modeVal, err := strconv.ParseUint(modeRaw, 8, 32)
		if err != nil {
			return fmt.Errorf("invalid mode: %w", err)
		}
		if err := os.Chmod(path, os.FileMode(modeVal)); err != nil {
			return err
		}
	}

	return nil
}

func resolveRepoConfigFormat(format string) (string, error) {
	format = strings.TrimSpace(format)
	if format == "" {
		format = "auto"
	}
	switch format {
	case "deb", "rpm":
		return format, nil
	case "auto":
		facts := repoConfigDetectHostFacts()
		osFacts, _ := facts["os"].(map[string]any)
		family := strings.ToLower(strings.TrimSpace(stringValue(osFacts, "family")))
		switch family {
		case "debian":
			return "deb", nil
		case "rhel":
			return "rpm", nil
		default:
			return "", fmt.Errorf("unable to resolve RepoConfig format from host family %q", family)
		}
	default:
		return "", fmt.Errorf("RepoConfig format must be one of auto, deb, rpm")
	}
}

func defaultRepoConfigPath(format string) string {
	if format == "deb" {
		return "/etc/apt/sources.list.d/deck-offline.list"
	}
	return "/etc/yum.repos.d/deck-offline.repo"
}

func defaultRepoConfigBackupPatterns(format string) []string {
	if format == "deb" {
		return []string{"/etc/apt/sources.list", "/etc/apt/sources.list.d/*.list", "/etc/apt/sources.list.d/*.sources"}
	}
	return defaultYumRepoPatterns()
}

func defaultRepoConfigCleanupPatterns(format string) []string {
	if format == "deb" {
		return []string{"/etc/apt/sources.list", "/etc/apt/sources.list.d/*.list", "/etc/apt/sources.list.d/*.sources"}
	}
	return defaultYumRepoPatterns()
}

func defaultYumRepoPatterns() []string {
	return []string{"/etc/yum.repos.d/*.repo"}
}

func renderRepoConfigContent(format string, repositories []stepspec.RepositoryEntry) (string, error) {
	if format == "deb" {
		return renderAptRepositoryList(repositories)
	}
	return renderYumRepositoryList(repositories)
}

func renderAptRepositoryList(repositories []stepspec.RepositoryEntry) (string, error) {
	lines := make([]string, 0, len(repositories))
	for _, repo := range repositories {
		baseURL := strings.TrimSpace(repo.BaseURL)
		if baseURL == "" {
			continue
		}
		repoType := strings.TrimSpace(repo.Type)
		if repoType == "" {
			repoType = "deb"
		}
		suite := strings.TrimSpace(repo.Suite)
		if suite == "" {
			suite = "./"
		}
		component := strings.TrimSpace(repo.Component)

		opts := make([]string, 0, 1)
		if repo.Trusted != nil && *repo.Trusted {
			opts = append(opts, "trusted=yes")
		}

		line := repoType + " "
		if len(opts) > 0 {
			line += "[" + strings.Join(opts, " ") + "] "
		}
		line += baseURL + " " + suite
		if component != "" {
			line += " " + component
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return "", fmt.Errorf("RepoConfig requires at least one deb repository with baseurl")
	}
	return strings.Join(lines, "\n"), nil
}

func renderYumRepositoryList(repositories []stepspec.RepositoryEntry) (string, error) {
	lines := make([]string, 0, len(repositories)*6)
	for _, repo := range repositories {
		id := strings.TrimSpace(repo.ID)
		if id == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("[%s]", id))
		if name := strings.TrimSpace(repo.Name); name != "" {
			lines = append(lines, fmt.Sprintf("name=%s", name))
		}
		if baseURL := strings.TrimSpace(repo.BaseURL); baseURL != "" {
			lines = append(lines, fmt.Sprintf("baseurl=%s", baseURL))
		}
		if repo.Enabled != nil {
			if *repo.Enabled {
				lines = append(lines, "enabled=1")
			} else {
				lines = append(lines, "enabled=0")
			}
		}
		if repo.GPGCheck != nil {
			if *repo.GPGCheck {
				lines = append(lines, "gpgcheck=1")
			} else {
				lines = append(lines, "gpgcheck=0")
			}
		}
		if gpgkey := strings.TrimSpace(repo.GPGKey); gpgkey != "" {
			lines = append(lines, fmt.Sprintf("gpgkey=%s", gpgkey))
		}

		extraKeys := make([]string, 0, len(repo.Extra))
		for k := range repo.Extra {
			extraKeys = append(extraKeys, k)
		}
		sort.Strings(extraKeys)
		for _, key := range extraKeys {
			switch v := repo.Extra[key].(type) {
			case string:
				if strings.TrimSpace(v) == "" {
					continue
				}
				lines = append(lines, fmt.Sprintf("%s=%s", key, strings.TrimSpace(v)))
			case bool:
				if v {
					lines = append(lines, fmt.Sprintf("%s=1", key))
				} else {
					lines = append(lines, fmt.Sprintf("%s=0", key))
				}
			case float64:
				lines = append(lines, fmt.Sprintf("%s=%v", key, v))
			case int, int64, uint64:
				lines = append(lines, fmt.Sprintf("%s=%v", key, v))
			}
		}

		lines = append(lines, "")
	}
	if len(lines) == 0 {
		return "", fmt.Errorf("RepoConfig requires at least one repository with id")
	}
	return strings.Join(lines, "\n"), nil
}

func backupRepoConfigPaths(patterns []string) error {
	paths, err := resolveRepoConfigPaths(patterns)
	if err != nil {
		return err
	}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if info.IsDir() {
			continue
		}
		content, err := fsutil.ReadFile(path)
		if err != nil {
			return err
		}
		backupPath := path + ".deck.bak"
		if err := os.WriteFile(backupPath, content, info.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

func cleanupRepoConfigPaths(patterns []string) error {
	paths, err := resolveRepoConfigPaths(patterns)
	if err != nil {
		return err
	}
	for _, path := range paths {
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	return nil
}

func resolveRepoConfigPaths(patterns []string) ([]string, error) {
	resolved := make([]string, 0)
	seen := map[string]bool{}
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		hasMeta := strings.ContainsAny(pattern, "*?[")
		if !hasMeta {
			if _, err := os.Stat(pattern); err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, err
			}
			if !seen[pattern] {
				resolved = append(resolved, pattern)
				seen[pattern] = true
			}
			continue
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		for _, match := range matches {
			if !seen[match] {
				resolved = append(resolved, match)
				seen[match] = true
			}
		}
	}
	sort.Strings(resolved)
	return resolved, nil
}

func disableYumRepoPaths(patterns []string, keepPath string) error {
	paths, err := resolveRepoConfigPaths(patterns)
	if err != nil {
		return err
	}
	for _, path := range paths {
		if strings.TrimSpace(keepPath) != "" && filepath.Clean(path) == filepath.Clean(keepPath) {
			continue
		}
		raw, err := fsutil.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		changed := false
		lines := strings.Split(string(raw), "\n")
		for i := range lines {
			if yumEnabledTruePattern.MatchString(lines[i]) {
				lines[i] = "enabled=0"
				changed = true
			}
		}
		if !changed {
			continue
		}
		content := strings.Join(lines, "\n")
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		if err := writeFileIfChanged(path, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}
