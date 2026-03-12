package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func runWriteFile(spec map[string]any) error {
	path := stringValue(spec, "path")
	if path == "" {
		return fmt.Errorf("%s: WriteFile requires path", errCodeInstallWritePathMissing)
	}

	content := stringValue(spec, "content")
	if content == "" {
		if tmpl := stringValue(spec, "contentFromTemplate"); tmpl != "" {
			content = tmpl
			if !strings.HasSuffix(content, "\n") {
				content += "\n"
			}
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	return nil
}

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
	path := stringValue(spec, "path")
	if path == "" {
		return fmt.Errorf("%s: RepoConfig requires path", errCodeInstallRepoConfigPath)
	}
	repositories, ok := spec["repositories"].([]any)
	if !ok || len(repositories) == 0 {
		return fmt.Errorf("RepoConfig requires repositories")
	}

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
		lines = append(lines, "")
	}
	if len(lines) == 0 {
		return fmt.Errorf("RepoConfig requires at least one repository with id")
	}

	content := strings.Join(lines, "\n")
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
