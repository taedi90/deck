package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/taedi90/deck/internal/bundle"
	"github.com/taedi90/deck/internal/config"
	"github.com/taedi90/deck/internal/prepare"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func runPack(args []string) error {
	if wantsHelp(args) {
		return helpRequest{text: packHelpText()}
	}

	fs := newHelpFlagSet("pack")
	outPath := fs.String("out", "", "output tar archive path")
	dryRun := fs.Bool("dry-run", false, "print pack plan without writing files")
	cacheDir := fs.String("cache-dir", "", "artifact cache directory")
	noCache := fs.Bool("no-cache", false, "disable artifact cache reuse")
	vars := &varFlag{}
	fs.Var(vars, "var", "set variable override (key=value), repeatable")
	if err := parseFlags(fs, args, packHelpText()); err != nil {
		return err
	}
	resolvedOut := strings.TrimSpace(*outPath)
	if !*dryRun && resolvedOut == "" {
		return errors.New("--out is required")
	}

	ctx := context.Background()

	packWorkflowPath, err := discoverPackWorkflow(ctx)
	if err != nil {
		return err
	}
	workflowBaseDir := filepath.Dir(packWorkflowPath)
	applyWorkflowPath := filepath.Join(workflowBaseDir, "apply.yaml")
	varsWorkflowPath := filepath.Join(workflowBaseDir, "vars.yaml")
	for _, requiredPath := range []string{applyWorkflowPath, varsWorkflowPath} {
		info, statErr := os.Stat(requiredPath)
		if statErr != nil || info.IsDir() {
			return fmt.Errorf("required workflow file not found: %s", requiredPath)
		}
	}

	if *dryRun {
		fmt.Fprintf(os.Stdout, "PACK_WORKFLOW=%s\n", filepath.ToSlash(packWorkflowPath))
		fmt.Fprintf(os.Stdout, "WORKFLOW_INCLUDE=%s\n", filepath.ToSlash(packWorkflowPath))
		fmt.Fprintf(os.Stdout, "WORKFLOW_INCLUDE=%s\n", filepath.ToSlash(applyWorkflowPath))
		fmt.Fprintf(os.Stdout, "WORKFLOW_INCLUDE=%s\n", filepath.ToSlash(varsWorkflowPath))
		fmt.Fprintln(os.Stdout, "ARTIFACT_DEFAULT_DEST=packages->bundle/packages")
		fmt.Fprintln(os.Stdout, "ARTIFACT_DEFAULT_DEST=images->bundle/images")
		fmt.Fprintln(os.Stdout, "ARTIFACT_DEFAULT_DEST=files->bundle/files")
		fmt.Fprintln(os.Stdout, "WRITE=bundle/deck")
		fmt.Fprintln(os.Stdout, "WRITE=bundle/files/deck")
		fmt.Fprintln(os.Stdout, "WRITE=bundle/.deck/manifest.json")
		if resolvedOut != "" {
			fmt.Fprintf(os.Stdout, "WRITE_TAR=%s\n", resolvedOut)
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

	stagingRoot, err := os.MkdirTemp(filepath.Dir(resolvedOutAbs), "deck-pack-")
	if err != nil {
		return fmt.Errorf("create staging root: %w", err)
	}
	defer os.RemoveAll(stagingRoot)
	bundleRoot := filepath.Join(stagingRoot, "bundle")

	packWorkflow, err := config.LoadWithOptions(ctx, packWorkflowPath, config.LoadOptions{VarOverrides: varsAsAnyMap(vars.AsMap())})
	if err != nil {
		return err
	}
	if strings.TrimSpace(packWorkflow.Role) != "pack" {
		return fmt.Errorf("pack workflow role must be pack: %s", packWorkflowPath)
	}

	artifactRoot := bundleRoot
	if !*noCache {
		if strings.TrimSpace(*cacheDir) != "" {
			artifactRoot, err = filepath.Abs(strings.TrimSpace(*cacheDir))
			if err != nil {
				return fmt.Errorf("resolve --cache-dir: %w", err)
			}
		}
	}

	if err := prepare.Run(ctx, packWorkflow, prepare.RunOptions{BundleRoot: artifactRoot, ForceRedownload: *noCache}); err != nil {
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

	fmt.Fprintf(os.Stdout, "pack: ok (%s)\n", resolvedOutAbs)
	return nil
}

func discoverPackWorkflow(ctx context.Context) (string, error) {
	workflowDir := filepath.Join(".", "workflows")
	absWorkflowDir, err := filepath.Abs(workflowDir)
	if err != nil {
		return "", fmt.Errorf("resolve workflow directory: %w", err)
	}
	info, err := os.Stat(absWorkflowDir)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("workflow directory not found: %s", absWorkflowDir)
	}

	preferred := filepath.Join(absWorkflowDir, "pack.yaml")
	if preferredInfo, statErr := os.Stat(preferred); statErr == nil && !preferredInfo.IsDir() {
		wf, loadErr := config.Load(ctx, preferred)
		if loadErr != nil {
			return "", loadErr
		}
		if strings.TrimSpace(wf.Role) != "pack" {
			return "", fmt.Errorf("pack workflow role must be pack: %s", preferred)
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
		if strings.TrimSpace(wf.Role) == "pack" {
			matches = append(matches, candidate)
		}
	}
	sort.Strings(matches)
	if len(matches) == 0 {
		return "", fmt.Errorf("pack workflow not found under %s", absWorkflowDir)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("multiple pack workflows found under %s", absWorkflowDir)
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
		return fmt.Errorf("encode pack manifest: %w", err)
	}
	return writeBytes(path, raw, 0o644)
}
