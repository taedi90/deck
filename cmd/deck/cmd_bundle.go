package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/taedi90/deck/internal/bundle"
)

func executeInit(output string) error {
	resolvedOutput := strings.TrimSpace(output)
	if resolvedOutput == "" {
		resolvedOutput = "."
	}
	gitignorePath := filepath.Join(resolvedOutput, ".gitignore")
	deckignorePath := filepath.Join(resolvedOutput, ".deckignore")
	templates := initTemplateFiles(resolvedOutput)
	overwriteTargets := make([]string, 0, len(templates))
	for path := range templates {
		if _, err := os.Stat(path); err == nil {
			overwriteTargets = append(overwriteTargets, path)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("init: stat target path %s: %w", path, err)
		}
	}
	if len(overwriteTargets) > 0 {
		sort.Strings(overwriteTargets)
		return fmt.Errorf("init: starter layout already contains target paths; refusing to overwrite: %s (choose another --out or remove these files)", strings.Join(overwriteTargets, ", "))
	}

	dirs := initTemplateDirs(resolvedOutput)
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("init: create directory %s: %w", dir, err)
		}
	}
	if err := ensureFileWithDefault(gitignorePath, defaultGitignoreContent()); err != nil {
		return err
	}
	if err := ensureFileWithDefault(deckignorePath, defaultDeckignoreContent()); err != nil {
		return err
	}
	for path, body := range templates {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return fmt.Errorf("init: write %s: %w", path, err)
		}
	}
	created := make([]string, 0, len(dirs)+len(templates))
	created = append(created, dirs...)
	created = append(created, gitignorePath, deckignorePath)
	for path := range templates {
		created = append(created, path)
	}
	sort.Strings(created)
	return stdoutPrintf("init: wrote %s\n", strings.Join(created, ", "))
}

func initTemplateDirs(root string) []string {
	return []string{
		filepath.Join(root, deckWorkDirName),
		filepath.Join(root, workflowRootDir),
		filepath.Join(root, workflowRootDir, workflowScenariosDir),
		filepath.Join(root, workflowRootDir, workflowComponentsDir),
		filepath.Join(root, preparedDirRel),
		filepath.Join(root, preparedDirRel, "files"),
		filepath.Join(root, preparedDirRel, "images"),
		filepath.Join(root, preparedDirRel, "packages"),
	}
}

func initTemplateFiles(root string) map[string]string {
	prepareComponentContent := strings.Join([]string{
		"role: prepare",
		"version: v1alpha1",
		"steps: []",
		"",
	}, "\n")
	applyComponentContent := strings.Join([]string{
		"role: apply",
		"version: v1alpha1",
		"steps: []",
		"",
	}, "\n")
	prepareScenarioContent := strings.Join([]string{
		"role: prepare",
		"version: v1alpha1",
		"varImports:",
		"  - ../vars.yaml",
		"phases:",
		"  - name: prepare",
		"    imports:",
		"      - path: ../components/example-prepare.yaml",
		"",
	}, "\n")
	applyScenarioContent := strings.Join([]string{
		"role: apply",
		"version: v1alpha1",
		"varImports:",
		"  - ../vars.yaml",
		"phases:",
		"  - name: install",
		"    imports:",
		"      - path: ../components/example-apply.yaml",
		"",
	}, "\n")

	return map[string]string{
		canonicalVarsPath(root):            "{}\n",
		canonicalPrepareWorkflowPath(root): prepareScenarioContent,
		canonicalApplyWorkflowPath(root):   applyScenarioContent,
		filepath.Join(root, workflowRootDir, workflowComponentsDir, "example-prepare.yaml"): prepareComponentContent,
		filepath.Join(root, workflowRootDir, workflowComponentsDir, "example-apply.yaml"):   applyComponentContent,
		filepath.Join(root, preparedDirRel, "files", ".keep"):                               "",
		filepath.Join(root, preparedDirRel, "images", ".keep"):                              "",
		filepath.Join(root, preparedDirRel, "packages", ".keep"):                            "",
	}
}

func ensureFileWithDefault(path string, content string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("init: stat target path %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("init: write %s: %w", path, err)
	}
	return nil
}

func defaultGitignoreContent() string {
	return strings.Join([]string{
		"/.deck/",
		"/deck",
		"/outputs/",
		"*.tar",
		"",
	}, "\n")
}

func defaultDeckignoreContent() string {
	return strings.Join([]string{
		".git/",
		".gitignore",
		".deckignore",
		"/*.tar",
		"",
	}, "\n")
}

func executeBundleVerify(filePath string, positionalArgs []string) error {
	resolvedPath, err := resolveBundlePathArg(filePath, positionalArgs, "bundle verify accepts a single <path>")
	if err != nil {
		return err
	}

	if err := bundle.VerifyManifest(resolvedPath); err != nil {
		return err
	}

	return stdoutPrintf("bundle verify: ok (%s)\n", resolvedPath)
}

func executeBundleBuild(root string, out string) error {
	resolvedRoot := strings.TrimSpace(root)
	if resolvedRoot == "" {
		resolvedRoot = "."
	}
	if strings.TrimSpace(out) == "" {
		return errors.New("--out is required")
	}

	if err := bundle.CollectArchive(resolvedRoot, out); err != nil {
		return err
	}

	return stdoutPrintf("bundle build: ok (%s -> %s)\n", resolvedRoot, out)
}

func resolveBundlePathArg(filePath string, positionalArgs []string, tooManyArgsErr string) (string, error) {
	if len(positionalArgs) > 1 {
		return "", errors.New(tooManyArgsErr)
	}
	resolvedPath := strings.TrimSpace(filePath)
	if resolvedPath == "" && len(positionalArgs) == 1 {
		resolvedPath = strings.TrimSpace(positionalArgs[0])
	}
	if resolvedPath == "" {
		return "", errors.New("bundle path is required")
	}
	return resolvedPath, nil
}
