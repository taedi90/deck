package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/taedi90/deck/internal/config"
	"github.com/taedi90/deck/internal/prepare"
)

type prepareOptions struct {
	preparedRoot string
	dryRun       bool
	refresh      bool
	clean        bool
	varOverrides map[string]string
}

func newPrepareCommand() *cobra.Command {
	vars := &varFlag{}
	cmd := &cobra.Command{
		Use:   "prepare",
		Short: "Prepare bundle contents under outputs/",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPrepareWithOptions(prepareOptions{
				preparedRoot: cmdFlagValue(cmd, "root"),
				dryRun:       cmdFlagBoolValue(cmd, "dry-run"),
				refresh:      cmdFlagBoolValue(cmd, "refresh"),
				clean:        cmdFlagBoolValue(cmd, "clean"),
				varOverrides: vars.AsMap(),
			})
		},
	}
	cmd.Flags().String("root", defaultPreparedRoot("."), "prepared bundle output directory")
	cmd.Flags().Bool("dry-run", false, "print prepare plan without writing files")
	cmd.Flags().Bool("refresh", false, "re-download artifacts instead of reusing prepared files")
	cmd.Flags().Bool("clean", false, "remove the prepared directory before writing")
	cmd.Flags().Var(vars, "var", "set variable override (key=value), repeatable")
	return cmd
}

func runPrepareWithOptions(opts prepareOptions) error {
	ctx := context.Background()

	prepareWorkflowPath, err := discoverPrepareWorkflow(ctx)
	if err != nil {
		return err
	}
	workflowRootDirPath, err := locateWorkflowTreeRoot(prepareWorkflowPath)
	if err != nil {
		return err
	}
	varsWorkflowPath, err := resolveRequiredVarsWorkflowPath(workflowRootDirPath)
	if err != nil {
		return err
	}
	applyWorkflowPath, err := resolveOptionalApplyWorkflowPath(workflowRootDirPath)
	if err != nil {
		return err
	}
	for _, requiredPath := range []string{varsWorkflowPath} {
		info, statErr := os.Stat(requiredPath)
		if statErr != nil || info.IsDir() {
			return fmt.Errorf("required workflow file not found: %s", requiredPath)
		}
	}

	resolvedPreparedRoot := strings.TrimSpace(opts.preparedRoot)
	if resolvedPreparedRoot == "" {
		resolvedPreparedRoot = defaultPreparedRoot(".")
	}
	resolvedPreparedRootAbs, err := filepath.Abs(resolvedPreparedRoot)
	if err != nil {
		return fmt.Errorf("resolve --root: %w", err)
	}

	if opts.dryRun {
		for _, line := range []string{
			fmt.Sprintf("PREPARE_WORKFLOW=%s", filepath.ToSlash(prepareWorkflowPath)),
			fmt.Sprintf("WORKFLOW_INCLUDE=%s", filepath.ToSlash(prepareWorkflowPath)),
			fmt.Sprintf("WORKFLOW_INCLUDE=%s", filepath.ToSlash(varsWorkflowPath)),
			fmt.Sprintf("PREPARED_ROOT=%s", filepath.ToSlash(resolvedPreparedRootAbs)),
			fmt.Sprintf("WRITE=%s", filepath.ToSlash(filepath.Join(resolvedPreparedRootAbs, "packages"))),
			fmt.Sprintf("WRITE=%s", filepath.ToSlash(filepath.Join(resolvedPreparedRootAbs, "images"))),
			fmt.Sprintf("WRITE=%s", filepath.ToSlash(filepath.Join(resolvedPreparedRootAbs, "files"))),
			fmt.Sprintf("WRITE=%s", filepath.ToSlash(filepath.Join(filepath.Dir(resolvedPreparedRootAbs), "deck"))),
			fmt.Sprintf("WRITE=%s", filepath.ToSlash(filepath.Join(filepath.Dir(resolvedPreparedRootAbs), ".deck", "manifest.json"))),
		} {
			if err := stdoutPrintln(line); err != nil {
				return err
			}
		}
		if applyWorkflowPath != "" {
			if err := stdoutPrintf("WORKFLOW_INCLUDE=%s\n", filepath.ToSlash(applyWorkflowPath)); err != nil {
				return err
			}
		}
		return nil
	}

	if opts.clean {
		if err := os.RemoveAll(resolvedPreparedRootAbs); err != nil {
			return fmt.Errorf("reset prepared root: %w", err)
		}
	}
	if err := os.MkdirAll(resolvedPreparedRootAbs, 0o755); err != nil {
		return fmt.Errorf("create prepared root: %w", err)
	}

	prepareWorkflow, err := config.LoadWithOptions(ctx, prepareWorkflowPath, config.LoadOptions{VarOverrides: varsAsAnyMap(opts.varOverrides)})
	if err != nil {
		return err
	}
	if strings.TrimSpace(prepareWorkflow.Role) != "prepare" {
		return fmt.Errorf("prepare workflow role must be prepare: %s", prepareWorkflowPath)
	}

	if err := prepare.Run(ctx, prepareWorkflow, prepare.RunOptions{BundleRoot: resolvedPreparedRootAbs, ForceRedownload: opts.refresh}); err != nil {
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
	workspaceRoot := filepath.Dir(resolvedPreparedRootAbs)
	if err := writeBytes(filepath.Join(workspaceRoot, "deck"), binaryBytes, 0o755); err != nil {
		return err
	}

	manifest, err := buildPreparedManifest(resolvedPreparedRootAbs)
	if err != nil {
		return err
	}
	if err := writePreparedManifest(filepath.Join(workspaceRoot, ".deck", "manifest.json"), manifest); err != nil {
		return err
	}

	return stdoutPrintf("prepare: ok (%s)\n", resolvedPreparedRootAbs)
}

func discoverPrepareWorkflow(ctx context.Context) (string, error) {
	workflowDir := filepath.Join(".", workflowRootDir)
	absWorkflowDir, err := filepath.Abs(workflowDir)
	if err != nil {
		return "", fmt.Errorf("resolve workflow directory: %w", err)
	}
	info, err := os.Stat(absWorkflowDir)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("workflow directory not found: %s", absWorkflowDir)
	}

	preferred := canonicalPrepareWorkflowPath(filepath.Dir(absWorkflowDir))
	preferredInfo, statErr := os.Stat(preferred)
	if statErr != nil || preferredInfo.IsDir() {
		return "", fmt.Errorf("prepare workflow not found: %s", preferred)
	}
	wf, loadErr := config.Load(ctx, preferred)
	if loadErr != nil {
		return "", loadErr
	}
	if strings.TrimSpace(wf.Role) != "prepare" {
		return "", fmt.Errorf("prepare workflow role must be prepare: %s", preferred)
	}
	return preferred, nil
}

func resolveOptionalApplyWorkflowPath(workflowRootPath string) (string, error) {
	path := canonicalApplyWorkflowPath(filepath.Dir(workflowRootPath))
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("apply workflow path is a directory: %s", path)
	}
	return path, nil
}

func resolveRequiredVarsWorkflowPath(workflowRootPath string) (string, error) {
	varsPath := canonicalVarsPath(filepath.Dir(workflowRootPath))
	if info, err := os.Stat(varsPath); err == nil && !info.IsDir() {
		return varsPath, nil
	}
	return "", fmt.Errorf("required workflow file not found: %s", varsPath)
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

type preparedManifest struct {
	Entries []preparedManifestEntry `json:"entries"`
}

type preparedManifestEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func buildPreparedManifest(bundleRoot string) (preparedManifest, error) {
	entries := make([]preparedManifestEntry, 0)
	workspaceRoot := filepath.Dir(bundleRoot)
	for _, root := range []string{"packages", "images", "files"} {
		rootPath := filepath.Join(bundleRoot, root)
		if _, err := os.Stat(rootPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return preparedManifest{}, err
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
			rel, err := filepath.Rel(workspaceRoot, path)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(raw)
			entries = append(entries, preparedManifestEntry{
				Path:   filepath.ToSlash(rel),
				SHA256: hex.EncodeToString(sum[:]),
				Size:   info.Size(),
			})
			return nil
		}); err != nil {
			return preparedManifest{}, err
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return preparedManifest{Entries: entries}, nil
}

func writePreparedManifest(path string, manifest preparedManifest) error {
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode prepare manifest: %w", err)
	}
	return writeBytes(path, raw, 0o644)
}
