package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/taedi90/deck/internal/bundle"
	"github.com/taedi90/deck/internal/config"
	"github.com/taedi90/deck/internal/prepare"
)

type prepareOptions struct {
	outPath      string
	dryRun       bool
	cacheDir     string
	noCache      bool
	varOverrides map[string]string
}

func newPrepareCommand() *cobra.Command {
	vars := &varFlag{}
	cmd := &cobra.Command{
		Use:   "prepare",
		Short: "Build an offline bundle from local deck files",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPrepareWithOptions(prepareOptions{
				outPath:      cmdFlagValue(cmd, "out"),
				dryRun:       cmdFlagBoolValue(cmd, "dry-run"),
				cacheDir:     cmdFlagValue(cmd, "cache-dir"),
				noCache:      cmdFlagBoolValue(cmd, "no-cache"),
				varOverrides: vars.AsMap(),
			})
		},
	}
	cmd.Flags().SetInterspersed(false)
	cmd.Flags().String("out", "", "output tar archive path")
	cmd.Flags().Bool("dry-run", false, "print prepare plan without writing files")
	cmd.Flags().String("cache-dir", "", "artifact cache directory")
	cmd.Flags().Bool("no-cache", false, "disable artifact cache reuse")
	cmd.Flags().Var(vars, "var", "set variable override (key=value), repeatable")
	return cmd
}

func runPrepareWithOptions(opts prepareOptions) error {
	resolvedOut := strings.TrimSpace(opts.outPath)
	if !opts.dryRun && resolvedOut == "" {
		return errors.New("--out is required")
	}

	ctx := context.Background()

	prepareWorkflowPath, err := discoverPrepareWorkflow(ctx)
	if err != nil {
		return err
	}
	workflowBaseDir := filepath.Dir(prepareWorkflowPath)
	applyWorkflowPath := filepath.Join(workflowBaseDir, "apply.yaml")
	varsWorkflowPath := filepath.Join(workflowBaseDir, "vars.yaml")
	for _, requiredPath := range []string{applyWorkflowPath, varsWorkflowPath} {
		info, statErr := os.Stat(requiredPath)
		if statErr != nil || info.IsDir() {
			return fmt.Errorf("required workflow file not found: %s", requiredPath)
		}
	}

	if opts.dryRun {
		for _, line := range []string{
			fmt.Sprintf("PREPARE_WORKFLOW=%s", filepath.ToSlash(prepareWorkflowPath)),
			fmt.Sprintf("WORKFLOW_INCLUDE=%s", filepath.ToSlash(prepareWorkflowPath)),
			fmt.Sprintf("WORKFLOW_INCLUDE=%s", filepath.ToSlash(applyWorkflowPath)),
			fmt.Sprintf("WORKFLOW_INCLUDE=%s", filepath.ToSlash(varsWorkflowPath)),
			"ARTIFACT_DEFAULT_DEST=packages->bundle/packages",
			"ARTIFACT_DEFAULT_DEST=images->bundle/images",
			"ARTIFACT_DEFAULT_DEST=files->bundle/files",
			"WRITE=bundle/deck",
			"WRITE=bundle/files/deck",
			"WRITE=bundle/.deck/manifest.json",
		} {
			if err := stdoutPrintln(line); err != nil {
				return err
			}
		}
		if resolvedOut != "" {
			if err := stdoutPrintf("WRITE_TAR=%s\n", resolvedOut); err != nil {
				return err
			}
		}
		return nil
	}

	resolvedOutAbs, err := filepath.Abs(resolvedOut)
	if err != nil {
		return fmt.Errorf("resolve --out: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(resolvedOutAbs), 0o755); err != nil {
		return fmt.Errorf("create output parent: %w", err)
	}

	stagingRoot, err := os.MkdirTemp(filepath.Dir(resolvedOutAbs), "deck-prepare-")
	if err != nil {
		return fmt.Errorf("create staging root: %w", err)
	}
	defer func() { _ = os.RemoveAll(stagingRoot) }()
	bundleRoot := filepath.Join(stagingRoot, "bundle")

	prepareWorkflow, err := config.LoadWithOptions(ctx, prepareWorkflowPath, config.LoadOptions{VarOverrides: varsAsAnyMap(opts.varOverrides)})
	if err != nil {
		return err
	}
	if strings.TrimSpace(prepareWorkflow.Role) != "prepare" {
		return fmt.Errorf("prepare workflow role must be prepare: %s", prepareWorkflowPath)
	}

	artifactRoot := bundleRoot
	if !opts.noCache {
		if strings.TrimSpace(opts.cacheDir) != "" {
			artifactRoot, err = filepath.Abs(strings.TrimSpace(opts.cacheDir))
			if err != nil {
				return fmt.Errorf("resolve --cache-dir: %w", err)
			}
		}
	}

	if err := prepare.Run(ctx, prepareWorkflow, prepare.RunOptions{BundleRoot: artifactRoot, ForceRedownload: opts.noCache}); err != nil {
		return err
	}

	if artifactRoot != bundleRoot {
		for _, artifactDir := range []string{"packages", "images", "files"} {
			if err := copySubtreeIfExists(filepath.Join(artifactRoot, artifactDir), filepath.Join(bundleRoot, artifactDir)); err != nil {
				return err
			}
		}
	}

	workflowOutDir := filepath.Join(bundleRoot, "workflows")
	if err := os.MkdirAll(workflowOutDir, 0o755); err != nil {
		return fmt.Errorf("create workflow output dir: %w", err)
	}
	if err := copySubtreeIfExists(workflowBaseDir, workflowOutDir); err != nil {
		return err
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve deck binary path: %w", err)
	}
	binaryBytes, err := os.ReadFile(execPath)
	if err != nil {
		return fmt.Errorf("read deck binary: %w", err)
	}
	if err := writeBytes(filepath.Join(bundleRoot, "deck"), binaryBytes, 0o755); err != nil {
		return err
	}
	if err := writeBytes(filepath.Join(bundleRoot, "files", "deck"), binaryBytes, 0o755); err != nil {
		return err
	}

	manifest, err := buildPackManifest(bundleRoot)
	if err != nil {
		return err
	}
	if err := writePackManifest(filepath.Join(bundleRoot, ".deck", "manifest.json"), manifest); err != nil {
		return err
	}

	if err := bundle.CollectArchive(bundleRoot, resolvedOutAbs); err != nil {
		return err
	}

	return stdoutPrintf("prepare: ok (%s)\n", resolvedOutAbs)
}

func discoverPrepareWorkflow(ctx context.Context) (string, error) {
	workflowDir := filepath.Join(".", "workflows")
	absWorkflowDir, err := filepath.Abs(workflowDir)
	if err != nil {
		return "", fmt.Errorf("resolve workflow directory: %w", err)
	}
	info, err := os.Stat(absWorkflowDir)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("workflow directory not found: %s", absWorkflowDir)
	}

	preferred := filepath.Join(absWorkflowDir, "prepare.yaml")
	if preferredInfo, statErr := os.Stat(preferred); statErr == nil && !preferredInfo.IsDir() {
		wf, loadErr := config.Load(ctx, preferred)
		if loadErr != nil {
			return "", loadErr
		}
		if strings.TrimSpace(wf.Role) != "prepare" {
			return "", fmt.Errorf("prepare workflow role must be prepare: %s", preferred)
		}
		return preferred, nil
	}

	entries, err := os.ReadDir(absWorkflowDir)
	if err != nil {
		return "", fmt.Errorf("read workflow directory: %w", err)
	}
	matches := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		candidate := filepath.Join(absWorkflowDir, entry.Name())
		wf, loadErr := config.Load(ctx, candidate)
		if loadErr != nil {
			return "", loadErr
		}
		if strings.TrimSpace(wf.Role) == "prepare" {
			matches = append(matches, candidate)
		}
	}
	sort.Strings(matches)
	if len(matches) == 0 {
		return "", fmt.Errorf("prepare workflow not found under %s", absWorkflowDir)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("multiple prepare workflows found under %s", absWorkflowDir)
	}

	return matches[0], nil
}

func copySubtreeIfExists(srcRoot, dstRoot string) error {
	info, err := os.Stat(srcRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}

	return filepath.WalkDir(srcRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dstRoot, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target, 0o644)
	})
}

func copyFile(srcPath, dstPath string, mode os.FileMode) error {
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", srcPath, err)
	}
	return writeBytes(dstPath, raw, mode)
}

func writeBytes(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent directory for %s: %w", path, err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

type packManifest struct {
	Entries []packManifestEntry `json:"entries"`
}

type packManifestEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func buildPackManifest(bundleRoot string) (packManifest, error) {
	entries := make([]packManifestEntry, 0)
	for _, root := range []string{"packages", "images", "files"} {
		rootPath := filepath.Join(bundleRoot, root)
		if _, err := os.Stat(rootPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return packManifest{}, err
		}
		if err := filepath.WalkDir(rootPath, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			info, err := os.Stat(path)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(bundleRoot, path)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(raw)
			entries = append(entries, packManifestEntry{
				Path:   filepath.ToSlash(rel),
				SHA256: hex.EncodeToString(sum[:]),
				Size:   info.Size(),
			})
			return nil
		}); err != nil {
			return packManifest{}, err
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return packManifest{Entries: entries}, nil
}

func writePackManifest(path string, manifest packManifest) error {
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode prepare manifest: %w", err)
	}
	return writeBytes(path, raw, 0o644)
}
