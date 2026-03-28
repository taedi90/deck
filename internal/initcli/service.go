package initcli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/filemode"
	"github.com/Airgap-Castaways/deck/internal/workspacepaths"
)

type Options struct {
	Output       string
	DeckWorkDir  string
	StdoutPrintf func(format string, args ...any) error
}

func Run(opts Options) error {
	resolvedOutput := strings.TrimSpace(opts.Output)
	if resolvedOutput == "" {
		resolvedOutput = "."
	}
	deckWorkDir := strings.TrimSpace(opts.DeckWorkDir)
	if deckWorkDir == "" {
		deckWorkDir = ".deck"
	}
	gitignorePath := filepath.Join(resolvedOutput, ".gitignore")
	deckignorePath := filepath.Join(resolvedOutput, ".deckignore")
	templates := templateFiles(resolvedOutput)
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

	for _, dir := range templateDirs(resolvedOutput, deckWorkDir) {
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
	created := make([]string, 0, len(templates)+2)
	created = append(created, templateDirs(resolvedOutput, deckWorkDir)...)
	created = append(created, gitignorePath, deckignorePath)
	for path := range templates {
		created = append(created, path)
	}
	sort.Strings(created)
	if opts.StdoutPrintf == nil {
		return nil
	}
	return opts.StdoutPrintf("init: wrote %s\n", strings.Join(created, ", "))
}

func templateDirs(root string, deckWorkDir string) []string {
	return []string{
		filepath.Join(root, deckWorkDir),
		filepath.Join(root, workspacepaths.WorkflowRootDir),
		filepath.Join(root, workspacepaths.WorkflowRootDir, workspacepaths.WorkflowScenariosDir),
		filepath.Join(root, workspacepaths.WorkflowRootDir, workspacepaths.WorkflowComponentsDir),
		filepath.Join(root, workspacepaths.PreparedDirRel),
		filepath.Join(root, workspacepaths.PreparedDirRel, workspacepaths.PreparedFilesRoot),
		filepath.Join(root, workspacepaths.PreparedDirRel, workspacepaths.PreparedImagesRoot),
		filepath.Join(root, workspacepaths.PreparedDirRel, workspacepaths.PreparedPackagesRoot),
	}
}

func templateFiles(root string) map[string]string {
	applyComponentContent := strings.Join([]string{"steps: []", ""}, "\n")
	prepareScenarioContent := strings.Join([]string{"version: v1alpha1", "phases:", "  - name: prepare", "    steps: []", ""}, "\n")
	applyScenarioContent := strings.Join([]string{"version: v1alpha1", "phases:", "  - name: install", "    imports:", "      - path: example-apply.yaml", ""}, "\n")

	return map[string]string{
		workspacepaths.CanonicalVarsPath(root):            "{}\n",
		workspacepaths.CanonicalPrepareWorkflowPath(root): prepareScenarioContent,
		workspacepaths.CanonicalApplyWorkflowPath(root):   applyScenarioContent,
		filepath.Join(root, workspacepaths.WorkflowRootDir, workspacepaths.WorkflowComponentsDir, "example-apply.yaml"): applyComponentContent,
		filepath.Join(root, workspacepaths.PreparedDirRel, workspacepaths.PreparedFilesRoot, ".keep"):                   "",
		filepath.Join(root, workspacepaths.PreparedDirRel, workspacepaths.PreparedImagesRoot, ".keep"):                  "",
		filepath.Join(root, workspacepaths.PreparedDirRel, workspacepaths.PreparedPackagesRoot, ".keep"):                "",
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
	return strings.Join([]string{"/.deck/", "/deck", "/outputs/", "*.tar", ""}, "\n")
}

func defaultDeckignoreContent() string {
	return strings.Join([]string{".git/", ".gitignore", ".deckignore", "/*.tar", ""}, "\n")
}
