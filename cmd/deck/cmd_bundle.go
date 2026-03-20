package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/taedi90/deck/internal/bundle"
	"github.com/taedi90/deck/internal/filemode"
	"github.com/taedi90/deck/internal/workspacepaths"
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
		if err := filemode.EnsureArtifactDir(dir); err != nil {
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
		if err := filemode.WriteArtifactFile(path, []byte(body)); err != nil {
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
	applyComponentContent := strings.Join([]string{
		"steps: []",
		"",
	}, "\n")
	prepareScenarioContent := strings.Join([]string{
		"version: v1alpha1",
		"phases:",
		"  - name: prepare",
		"    steps: []",
		"",
	}, "\n")
	applyScenarioContent := strings.Join([]string{
		"version: v1alpha1",
		"phases:",
		"  - name: install",
		"    imports:",
		"      - path: example-apply.yaml",
		"",
	}, "\n")

	return map[string]string{
		workspacepaths.CanonicalVarsPath(root):                                            "{}\n",
		workspacepaths.CanonicalPrepareWorkflowPath(root):                                 prepareScenarioContent,
		workspacepaths.CanonicalApplyWorkflowPath(root):                                   applyScenarioContent,
		filepath.Join(root, workflowRootDir, workflowComponentsDir, "example-apply.yaml"): applyComponentContent,
		filepath.Join(root, preparedDirRel, "files", ".keep"):                             "",
		filepath.Join(root, preparedDirRel, "images", ".keep"):                            "",
		filepath.Join(root, preparedDirRel, "packages", ".keep"):                          "",
	}
}

func ensureFileWithDefault(path string, content string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("init: stat target path %s: %w", path, err)
	}
	if err := filemode.WriteArtifactFile(path, []byte(content)); err != nil {
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

type bundleVerifyReport struct {
	Status string `json:"status"`
	Path   string `json:"path"`
}

type bundleManifestSummary struct {
	Entries  int
	Files    int
	Images   int
	Packages int
	Other    int
}

func executeBundleVerify(filePath string, positionalArgs []string, output string) error {
	resolvedOutput, err := resolveOutputFormat(output)
	if err != nil {
		return err
	}
	resolvedPath, err := resolveBundlePathArg(filePath, positionalArgs, "bundle verify accepts a single <path>")
	if err != nil {
		return err
	}
	if err := verbosef(1, "deck: bundle verify path=%s\n", resolvedPath); err != nil {
		return err
	}

	if err := bundle.VerifyManifest(resolvedPath); err != nil {
		_ = verbosef(2, "deck: bundle verify error=%v\n", err)
		return err
	}
	entries, err := bundle.InspectManifest(resolvedPath)
	if err != nil {
		return err
	}
	summary := summarizeBundleManifest(entries)
	if err := verbosef(2, "deck: bundle verify manifestEntries=%d files=%d images=%d packages=%d other=%d\n", summary.Entries, summary.Files, summary.Images, summary.Packages, summary.Other); err != nil {
		return err
	}
	report := bundleVerifyReport{Status: "ok", Path: resolvedPath}
	if resolvedOutput == "json" {
		enc := stdoutJSONEncoder()
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}

	return stdoutPrintf("bundle verify: ok (%s)\n", report.Path)
}

func executeBundleBuild(root string, out string) error {
	resolvedRoot := strings.TrimSpace(root)
	if resolvedRoot == "" {
		resolvedRoot = "."
	}
	if strings.TrimSpace(out) == "" {
		return errors.New("--out is required")
	}
	if err := verbosef(1, "deck: bundle build root=%s out=%s\n", resolvedRoot, strings.TrimSpace(out)); err != nil {
		return err
	}
	manifestPath := filepath.Join(resolvedRoot, ".deck", "manifest.json")
	entries, err := bundle.InspectManifest(resolvedRoot)
	if err != nil {
		if err := verbosef(2, "deck: bundle build manifestInspectError=%v\n", err); err != nil {
			return err
		}
	} else {
		summary := summarizeBundleManifest(entries)
		if err := verbosef(1, "deck: bundle build manifest=%s entries=%d\n", manifestPath, summary.Entries); err != nil {
			return err
		}
		if err := verbosef(2, "deck: bundle build manifest files=%d images=%d packages=%d other=%d\n", summary.Files, summary.Images, summary.Packages, summary.Other); err != nil {
			return err
		}
	}

	if err := bundle.CollectArchive(resolvedRoot, out); err != nil {
		return err
	}
	if info, err := os.Stat(out); err == nil {
		if err := verbosef(2, "deck: bundle build archiveSize=%d\n", info.Size()); err != nil {
			return err
		}
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

func summarizeBundleManifest(entries []bundle.ManifestEntry) bundleManifestSummary {
	summary := bundleManifestSummary{Entries: len(entries)}
	for _, entry := range entries {
		path := strings.TrimSpace(entry.Path)
		switch {
		case strings.HasPrefix(path, "outputs/files/") || strings.HasPrefix(path, "files/"):
			summary.Files++
		case strings.HasPrefix(path, "outputs/images/") || strings.HasPrefix(path, "images/"):
			summary.Images++
		case strings.HasPrefix(path, "outputs/packages/") || strings.HasPrefix(path, "packages/"):
			summary.Packages++
		default:
			summary.Other++
		}
	}
	return summary
}
