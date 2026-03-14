package main

import (
	"encoding/json"
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
	workflowsDir := filepath.Join(resolvedOutput, "workflows")

	if info, err := os.Stat(workflowsDir); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("init: workflows path exists and is not a directory: %s", workflowsDir)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("init: stat workflows directory %s: %w", workflowsDir, err)
	}

	templates := initTemplateFiles()
	overwriteTargets := make([]string, 0, len(templates))
	for fileName := range templates {
		targetPath := filepath.Join(workflowsDir, fileName)
		if _, err := os.Stat(targetPath); err == nil {
			overwriteTargets = append(overwriteTargets, targetPath)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("init: stat target file %s: %w", targetPath, err)
		}
	}
	if len(overwriteTargets) > 0 {
		sort.Strings(overwriteTargets)
		return fmt.Errorf("init: workflows already contains target files; refusing to overwrite: %s (choose another --out or remove these files)", strings.Join(overwriteTargets, ", "))
	}

	if err := os.MkdirAll(workflowsDir, 0o755); err != nil {
		return fmt.Errorf("init: create workflows directory %s: %w", workflowsDir, err)
	}
	for fileName, body := range templates {
		targetPath := filepath.Join(workflowsDir, fileName)
		if err := os.WriteFile(targetPath, []byte(body), 0o644); err != nil {
			return fmt.Errorf("init: write %s: %w", targetPath, err)
		}
	}
	created := make([]string, 0, len(templates))
	for fileName := range templates {
		created = append(created, filepath.Join(workflowsDir, fileName))
	}
	sort.Strings(created)
	return stdoutPrintf("init: wrote %s\n", strings.Join(created, ", "))
}

func initTemplateFiles() map[string]string {
	prepareContent := strings.Join([]string{
		"role: prepare",
		"version: v1alpha1",
		"artifacts:",
		"  files: []",
		"",
	}, "\n")
	applyContent := strings.Join([]string{
		"role: apply",
		"version: v1alpha1",
		"steps: []",
		"",
	}, "\n")

	return map[string]string{
		"vars.yaml":    "{}\n",
		"prepare.yaml": prepareContent,
		"apply.yaml":   applyContent,
	}
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

func executeBundleInspect(filePath string, output string, positionalArgs []string) error {
	resolvedPath, err := resolveBundlePathArg(filePath, positionalArgs, "bundle inspect accepts a single <path>")
	if err != nil {
		return err
	}
	if output != "text" && output != "json" {
		return errors.New("--output must be text or json")
	}

	entries, err := bundle.InspectManifest(resolvedPath)
	if err != nil {
		return err
	}

	if output == "json" {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"entries": entries})
	}
	for _, entry := range entries {
		if _, err := fmt.Fprintln(os.Stdout, entry.Path); err != nil {
			return fmt.Errorf("bundle inspect: write output: %w", err)
		}
	}
	return nil
}

func executeBundleImport(filePath string, destDir string) error {
	if strings.TrimSpace(filePath) == "" {
		return errors.New("--file is required")
	}
	if strings.TrimSpace(destDir) == "" {
		return errors.New("--dest is required")
	}

	if err := bundle.ImportArchive(filePath, destDir); err != nil {
		return err
	}

	return stdoutPrintf("bundle import: ok (%s -> %s)\n", filePath, destDir)
}

func executeBundleCollect(root string, out string) error {
	if strings.TrimSpace(root) == "" {
		return errors.New("--root is required")
	}
	if strings.TrimSpace(out) == "" {
		return errors.New("--out is required")
	}

	if err := bundle.CollectArchive(root, out); err != nil {
		return err
	}

	return stdoutPrintf("bundle collect: ok (%s -> %s)\n", root, out)
}

func executeBundleMerge(to string, dryRun bool, positionalArgs []string) error {
	if len(positionalArgs) == 0 {
		return errors.New("bundle merge requires <bundle.tar>")
	}
	archivePath := strings.TrimSpace(positionalArgs[0])
	if archivePath == "" {
		return errors.New("bundle merge requires <bundle.tar>")
	}
	if strings.TrimSpace(to) == "" {
		return errors.New("--to is required")
	}

	report, err := bundle.MergeArchive(archivePath, strings.TrimSpace(to), dryRun)
	if err != nil {
		return err
	}

	if dryRun {
		if err := stdoutPrintf("bundle merge: dry-run (%s -> %s)\n", archivePath, report.Destination); err != nil {
			return err
		}
		for _, action := range report.Actions {
			if err := stdoutPrintf("PLAN %s %s (%s)\n", action.Action, action.Path, action.Reason); err != nil {
				return err
			}
		}
		return nil
	}

	return stdoutPrintf("bundle merge: ok (%s -> %s)\n", archivePath, report.Destination)
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
