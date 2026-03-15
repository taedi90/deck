package install

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	repoConfigDetectHostFacts = detectHostFacts
	repoConfigRunTimedCommand = runTimedCommand
	repoConfigDefaultPathFunc = defaultRepoConfigPath
)

var yumEnabledTruePattern = regexp.MustCompile(`(?i)^\s*enabled\s*=\s*(1|yes|true)\s*$`)

func runEditFile(spec map[string]any) error {
	path := stringValue(spec, "path")
	if path == "" {
		return fmt.Errorf("%s: EditFile requires path", errCodeInstallEditPathMissing)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if editFileBackupEnabled(spec) {
		backupPath, err := createEditFileBackup(path, content)
		if err != nil {
			return fmt.Errorf("create backup %s: %w", backupPath, err)
		}
		if err := trimEditFileBackups(path, 10); err != nil {
			return fmt.Errorf("trim backups after %s: %w", backupPath, err)
		}
	}
	updated := string(content)

	edits, ok := spec["edits"].([]any)
	if !ok || len(edits) == 0 {
		return fmt.Errorf("%s: EditFile requires edits", errCodeInstallEditsMissing)
	}

	for _, e := range edits {
		em, ok := e.(map[string]any)
		if !ok {
			continue
		}
		match := stringValue(em, "match")
		with := stringValue(em, "with")
		if match == "" {
			continue
		}
		updated = strings.Replace(updated, match, with, 1)
	}

	return os.WriteFile(path, []byte(updated), 0o644)
}

func runCopyFile(spec map[string]any) error {
	src := stringValue(spec, "src")
	dest := stringValue(spec, "dest")
	if src == "" || dest == "" {
		return fmt.Errorf("%s: CopyFile requires src and dest", errCodeInstallCopyPathMissing)
	}

	content, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dest, content, 0o644)
}

func runEnsureDir(spec map[string]any) error {
	path := stringValue(spec, "path")
	if path == "" {
		return fmt.Errorf("%s: EnsureDir requires path", errCodeInstallEnsureDirPathMis)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	if modeRaw := stringValue(spec, "mode"); modeRaw != "" {
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

func runSymlink(spec map[string]any) error {
	path := stringValue(spec, "path")
	if path == "" {
		return fmt.Errorf("%s: Symlink requires path", errCodeInstallSymlinkPathMiss)
	}
	target := stringValue(spec, "target")
	if target == "" {
		return fmt.Errorf("%s: Symlink requires target", errCodeInstallSymlinkTargetMis)
	}

	if boolValue(spec, "createParent") {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
	}

	if boolValue(spec, "requireTarget") {
		if _, err := os.Lstat(target); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("symlink target does not exist: %s", target)
			}
			return err
		}
	}

	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			currentTarget, readErr := os.Readlink(path)
			if readErr != nil {
				return readErr
			}
			if currentTarget == target {
				return nil
			}
		}

		if !boolValue(spec, "force") {
			return fmt.Errorf("destination already exists: %s", path)
		}
		if info.IsDir() {
			return fmt.Errorf("destination is a directory and cannot be replaced: %s", path)
		}

		if removeErr := os.Remove(path); removeErr != nil {
			return removeErr
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	return os.Symlink(target, path)
}

func runInstallFile(spec map[string]any) error {
	path := stringValue(spec, "path")
	if path == "" {
		return fmt.Errorf("%s: InstallFile requires path", errCodeInstallInstallFilePath)
	}
	content := stringValue(spec, "content")
	if content == "" {
		if from := stringValue(spec, "contentFromTemplate"); from != "" {
			content = from
		}
	}
	if content == "" {
		return fmt.Errorf("%s: InstallFile requires content", errCodeInstallInstallFileInput)
	}
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := writeFileIfChanged(path, []byte(content), 0o644); err != nil {
		return err
	}
	if modeRaw := stringValue(spec, "mode"); modeRaw != "" {
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

func runTemplateFile(spec map[string]any) error {
	path := stringValue(spec, "path")
	if path == "" {
		return fmt.Errorf("%s: TemplateFile requires path", errCodeInstallTemplatePathMiss)
	}
	body := stringValue(spec, "template")
	if body == "" {
		return fmt.Errorf("%s: TemplateFile requires template", errCodeInstallTemplateBodyMiss)
	}
	return runInstallFile(map[string]any{
		"path":    path,
		"content": body,
		"mode":    stringValue(spec, "mode"),
	})
}

func runRepoConfig(spec map[string]any) error {
	format, err := resolveRepoConfigFormat(spec)
	if err != nil {
		return err
	}

	path := stringValue(spec, "path")
	if path == "" {
		path = repoConfigDefaultPathFunc(format)
	}
	if path == "" {
		return fmt.Errorf("%s: RepoConfig requires path", errCodeInstallRepoConfigPath)
	}

	repositories, ok := spec["repositories"].([]any)
	if !ok || len(repositories) == 0 {
		return fmt.Errorf("RepoConfig requires repositories")
	}

	replaceExisting := boolValue(spec, "replaceExisting")
	disableExisting := boolValue(spec, "disableExisting")

	backupPatterns := append([]string{}, stringSlice(spec["backupPaths"])...)
	cleanupPatterns := append([]string{}, stringSlice(spec["cleanupPaths"])...)

	if (replaceExisting || disableExisting) && len(backupPatterns) == 0 {
		backupPatterns = append(backupPatterns, defaultRepoConfigBackupPatterns(format)...)
	}
	if replaceExisting && len(cleanupPatterns) == 0 {
		cleanupPatterns = append(cleanupPatterns, defaultRepoConfigCleanupPatterns(format)...)
	}
	if format == "apt" && disableExisting && !replaceExisting && len(cleanupPatterns) == 0 {
		cleanupPatterns = append(cleanupPatterns, backupPatterns...)
	}

	if err := backupRepoConfigPaths(backupPatterns); err != nil {
		return err
	}
	if format == "yum" && disableExisting && !replaceExisting {
		if err := disableYumRepoPaths(backupPatterns, path); err != nil {
			return err
		}
	}
	if err := cleanupRepoConfigPaths(cleanupPatterns); err != nil {
		return err
	}

	content, err := renderRepoConfigContent(format, repositories)
	if err != nil {
		return err
	}

	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := writeFileIfChanged(path, []byte(content), 0o644); err != nil {
		return err
	}
	if modeRaw := stringValue(spec, "mode"); modeRaw != "" {
		modeVal, err := strconv.ParseUint(modeRaw, 8, 32)
		if err != nil {
			return fmt.Errorf("invalid mode: %w", err)
		}
		if err := os.Chmod(path, os.FileMode(modeVal)); err != nil {
			return err
		}
	}
	if err := refreshRepoMetadata(spec, format); err != nil {
		return err
	}

	return nil
}

func resolveRepoConfigFormat(spec map[string]any) (string, error) {
	format := stringValue(spec, "format")
	if format == "" {
		format = "auto"
	}
	switch format {
	case "apt", "yum":
		return format, nil
	case "auto":
		facts := repoConfigDetectHostFacts()
		osFacts, _ := facts["os"].(map[string]any)
		family := strings.ToLower(strings.TrimSpace(stringValue(osFacts, "family")))
		switch family {
		case "debian":
			return "apt", nil
		case "rhel":
			return "yum", nil
		default:
			return "", fmt.Errorf("unable to resolve RepoConfig format from host family %q", family)
		}
	default:
		return "", fmt.Errorf("RepoConfig format must be one of auto, apt, yum")
	}
}

func defaultRepoConfigPath(format string) string {
	if format == "apt" {
		return "/etc/apt/sources.list.d/deck-offline.list"
	}
	return "/etc/yum.repos.d/deck-offline.repo"
}

func defaultRepoConfigBackupPatterns(format string) []string {
	if format == "apt" {
		return []string{"/etc/apt/sources.list", "/etc/apt/sources.list.d/*.list", "/etc/apt/sources.list.d/*.sources"}
	}
	return defaultYumRepoPatterns()
}

func defaultRepoConfigCleanupPatterns(format string) []string {
	if format == "apt" {
		return []string{"/etc/apt/sources.list", "/etc/apt/sources.list.d/*.list", "/etc/apt/sources.list.d/*.sources"}
	}
	return defaultYumRepoPatterns()
}

func defaultYumRepoPatterns() []string {
	return []string{"/etc/yum.repos.d/*.repo"}
}

func renderRepoConfigContent(format string, repositories []any) (string, error) {
	if format == "apt" {
		return renderAptRepositoryList(repositories)
	}
	return renderYumRepositoryList(repositories)
}

func renderAptRepositoryList(repositories []any) (string, error) {
	lines := make([]string, 0, len(repositories))
	for _, raw := range repositories {
		repo, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		baseURL := stringValue(repo, "baseurl")
		if baseURL == "" {
			continue
		}
		repoType := stringValue(repo, "type")
		if repoType == "" {
			repoType = "deb"
		}
		suite := stringValue(repo, "suite")
		if suite == "" {
			suite = "./"
		}
		component := stringValue(repo, "component")

		opts := make([]string, 0, 1)
		if trusted, ok := repo["trusted"].(bool); ok && trusted {
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
		return "", fmt.Errorf("RepoConfig requires at least one apt repository with baseurl")
	}
	return strings.Join(lines, "\n"), nil
}

func renderYumRepositoryList(repositories []any) (string, error) {
	lines := make([]string, 0, len(repositories)*6)
	for _, raw := range repositories {
		repo, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id := stringValue(repo, "id")
		if id == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("[%s]", id))
		if name := stringValue(repo, "name"); name != "" {
			lines = append(lines, fmt.Sprintf("name=%s", name))
		}
		if baseURL := stringValue(repo, "baseurl"); baseURL != "" {
			lines = append(lines, fmt.Sprintf("baseurl=%s", baseURL))
		}
		if enabled, ok := repo["enabled"].(bool); ok {
			if enabled {
				lines = append(lines, "enabled=1")
			} else {
				lines = append(lines, "enabled=0")
			}
		}
		if gpgcheck, ok := repo["gpgcheck"].(bool); ok {
			if gpgcheck {
				lines = append(lines, "gpgcheck=1")
			} else {
				lines = append(lines, "gpgcheck=0")
			}
		}
		if gpgkey := stringValue(repo, "gpgkey"); gpgkey != "" {
			lines = append(lines, fmt.Sprintf("gpgkey=%s", gpgkey))
		}

		extraKeys := make([]string, 0)
		for k := range repo {
			switch k {
			case "id", "name", "baseurl", "enabled", "gpgcheck", "gpgkey", "trusted", "suite", "component", "type":
				continue
			default:
				extraKeys = append(extraKeys, k)
			}
		}
		sort.Strings(extraKeys)
		for _, key := range extraKeys {
			switch v := repo[key].(type) {
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
		content, err := os.ReadFile(path)
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
		raw, err := os.ReadFile(path)
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

func refreshRepoMetadata(spec map[string]any, format string) error {
	refresh, ok := spec["refreshCache"].(map[string]any)
	if !ok {
		return nil
	}
	if enabled, exists := refresh["enabled"].(bool); !exists || !enabled {
		return nil
	}
	clean, _ := refresh["clean"].(bool)
	return runPackageCacheCommands(
		repoConfigFormatToPackageManager(format),
		clean,
		true,
		packageRepoPolicy{},
		commandTimeoutWithDefault(spec, defaultPackageCacheTimeout),
		repoConfigRunTimedCommand,
		"repo metadata refresh",
	)
}
