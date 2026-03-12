package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/taedi90/deck/internal/bundle"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func runWorkflowInit(args []string) error {
	fs := newHelpFlagSet("workflow init")
	output := fs.String("out", ".", "output directory")
	if err := parseFlags(fs, args, initHelpText()); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return helpRequest{text: initHelpText()}
	}
	resolvedOutput := strings.TrimSpace(*output)
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
	fmt.Fprintf(os.Stdout, "init: wrote %s\n", strings.Join(created, ", "))
	return nil
}

func initTemplateFiles() map[string]string {
	packContent := strings.Join([]string{
		"role: pack",
		"version: v1alpha1",
		"steps: []",
		"",
	}, "\n")
	applyContent := strings.Join([]string{
		"role: apply",
		"version: v1alpha1",
		"steps: []",
		"",
	}, "\n")

	return map[string]string{
		"vars.yaml":  "{}\n",
		"pack.yaml":  packContent,
		"apply.yaml": applyContent,
	}
}
func runWorkflowBundle(args []string) error {
	if len(args) == 0 {
		return helpRequest{text: bundleHelpText()}
	}
	if wantsHelp(args) {
		text, err := renderBundleHelp(args[1:])
		if err != nil {
			return err
		}
		return helpRequest{text: text}
	}

	switch args[0] {
	case "verify":
		fs := newHelpFlagSet("workflow bundle verify")
		bundlePath := fs.String("file", "", "bundle path (directory or bundle.tar)")
		parseArgs := append([]string{}, args[1:]...)
		positionalPath := ""
		if len(parseArgs) > 0 && !strings.HasPrefix(parseArgs[0], "-") {
			positionalPath = strings.TrimSpace(parseArgs[0])
			parseArgs = parseArgs[1:]
		}
		if err := parseFlags(fs, parseArgs, bundleVerifyHelpText()); err != nil {
			return err
		}
		if fs.NArg() > 0 {
			return errors.New("bundle verify accepts a single <path>")
		}
		resolvedPath := strings.TrimSpace(*bundlePath)
		if resolvedPath == "" {
			resolvedPath = positionalPath
		}
		if resolvedPath == "" {
			return errors.New("bundle path is required")
		}

		if err := bundle.VerifyManifest(resolvedPath); err != nil {
			return err
		}

		fmt.Fprintf(os.Stdout, "bundle verify: ok (%s)\n", resolvedPath)
		return nil

	case "inspect":
		fs := newHelpFlagSet("workflow bundle inspect")
		bundlePath := fs.String("file", "", "bundle path (directory or bundle.tar)")
		output := ""
		registerOutputFormatFlags(fs, &output, "text")
		parseArgs := append([]string{}, args[1:]...)
		positionalPath := ""
		if len(parseArgs) > 0 && !strings.HasPrefix(parseArgs[0], "-") {
			positionalPath = strings.TrimSpace(parseArgs[0])
			parseArgs = parseArgs[1:]
		}
		if err := parseFlags(fs, parseArgs, bundleInspectHelpText()); err != nil {
			return err
		}
		if fs.NArg() > 0 {
			return errors.New("bundle inspect accepts a single <path>")
		}
		resolvedPath := strings.TrimSpace(*bundlePath)
		if resolvedPath == "" {
			resolvedPath = positionalPath
		}
		if resolvedPath == "" {
			return errors.New("bundle path is required")
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

	case "import":
		fs := newHelpFlagSet("workflow bundle import")
		archiveFile := fs.String("file", "", "bundle archive file path")
		destDir := fs.String("dest", "", "destination directory")
		if err := parseFlags(fs, args[1:], bundleImportHelpText()); err != nil {
			return err
		}
		if *archiveFile == "" {
			return errors.New("--file is required")
		}
		if *destDir == "" {
			return errors.New("--dest is required")
		}

		if err := bundle.ImportArchive(*archiveFile, *destDir); err != nil {
			return err
		}

		fmt.Fprintf(os.Stdout, "bundle import: ok (%s -> %s)\n", *archiveFile, *destDir)
		return nil

	case "collect":
		fs := newHelpFlagSet("workflow bundle collect")
		bundleDir := fs.String("root", "", "bundle directory")
		outputFile := fs.String("out", "", "output tar archive path")
		if err := parseFlags(fs, args[1:], bundleCollectHelpText()); err != nil {
			return err
		}
		if *bundleDir == "" {
			return errors.New("--root is required")
		}
		if *outputFile == "" {
			return errors.New("--out is required")
		}

		if err := bundle.CollectArchive(*bundleDir, *outputFile); err != nil {
			return err
		}

		fmt.Fprintf(os.Stdout, "bundle collect: ok (%s -> %s)\n", *bundleDir, *outputFile)
		return nil

	case "merge":
		fs := newHelpFlagSet("workflow bundle merge")
		to := fs.String("to", "", "merge destination (local directory)")
		dryRun := fs.Bool("dry-run", false, "print merge plan without writing")
		parseArgs := append([]string{}, args[1:]...)
		archivePath := ""
		if len(parseArgs) > 0 && !strings.HasPrefix(parseArgs[0], "-") {
			archivePath = strings.TrimSpace(parseArgs[0])
			parseArgs = parseArgs[1:]
		}
		if err := parseFlags(fs, parseArgs, bundleMergeHelpText()); err != nil {
			return err
		}
		if archivePath == "" {
			return errors.New("bundle merge requires <bundle.tar>")
		}
		if strings.TrimSpace(*to) == "" {
			return errors.New("--to is required")
		}

		report, err := bundle.MergeArchive(archivePath, strings.TrimSpace(*to), *dryRun)
		if err != nil {
			return err
		}

		if *dryRun {
			fmt.Fprintf(os.Stdout, "bundle merge: dry-run (%s -> %s)\n", archivePath, report.Destination)
			for _, action := range report.Actions {
				fmt.Fprintf(os.Stdout, "PLAN %s %s (%s)\n", action.Action, action.Path, action.Reason)
			}
			return nil
		}

		fmt.Fprintf(os.Stdout, "bundle merge: ok (%s -> %s)\n", archivePath, report.Destination)
		return nil

	default:
		return fmt.Errorf("unknown bundle command %q", args[0])
	}
}
