package preparecli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/taedi90/deck/internal/config"
	"github.com/taedi90/deck/internal/filemode"
	"github.com/taedi90/deck/internal/fsutil"
	"github.com/taedi90/deck/internal/hostfs"
	"github.com/taedi90/deck/internal/prepare"
	"github.com/taedi90/deck/internal/workspacepaths"
)

type Options struct {
	PreparedRoot string
	DryRun       bool
	Refresh      bool
	Clean        bool
	VarOverrides map[string]any
	Stdout       io.Writer
	Diagnosticf  func(level int, format string, args ...any) error
}

type preparedManifest struct {
	Entries []preparedManifestEntry `json:"entries"`
}

type preparedManifestEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func Run(ctx context.Context, opts Options) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	prepareWorkflowPath, err := discoverPrepareWorkflow(ctx)
	if err != nil {
		return err
	}
	if err := emitDiagnostic(opts, 1, "deck: prepare workflow=%s\n", filepath.ToSlash(prepareWorkflowPath)); err != nil {
		return err
	}
	workflowRootDirPath, err := workspacepaths.LocateWorkflowTreeRoot(prepareWorkflowPath)
	if err != nil {
		return err
	}
	varsWorkflowPath, err := resolveOptionalVarsWorkflowPath(workflowRootDirPath)
	if err != nil {
		return err
	}
	if varsWorkflowPath != "" {
		if err := emitDiagnostic(opts, 1, "deck: prepare vars=%s\n", filepath.ToSlash(varsWorkflowPath)); err != nil {
			return err
		}
	}
	applyWorkflowPath, err := resolveOptionalApplyWorkflowPath(workflowRootDirPath)
	if err != nil {
		return err
	}
	if applyWorkflowPath != "" {
		if err := emitDiagnostic(opts, 1, "deck: prepare apply=%s\n", filepath.ToSlash(applyWorkflowPath)); err != nil {
			return err
		}
	}
	resolvedPreparedRoot := strings.TrimSpace(opts.PreparedRoot)
	if resolvedPreparedRoot == "" {
		resolvedPreparedRoot = workspacepaths.DefaultPreparedRoot(".")
	}
	resolvedPreparedRootAbs, err := filepath.Abs(resolvedPreparedRoot)
	if err != nil {
		return fmt.Errorf("resolve --root: %w", err)
	}
	if err := emitDiagnostic(opts, 1, "deck: prepare preparedRoot=%s\n", filepath.ToSlash(resolvedPreparedRootAbs)); err != nil {
		return err
	}
	preparedRoot, err := fsutil.NewPreparedRoot(resolvedPreparedRootAbs)
	if err != nil {
		return err
	}
	preparedHostPath, err := hostfs.NewHostPath(preparedRoot.Abs())
	if err != nil {
		return err
	}
	prepareWorkflow, err := config.LoadWithOptions(ctx, prepareWorkflowPath, config.LoadOptions{VarOverrides: opts.VarOverrides})
	if err != nil {
		return err
	}
	if strings.TrimSpace(prepareWorkflow.Role) != "prepare" {
		return fmt.Errorf("prepare workflow role must be prepare: %s", prepareWorkflowPath)
	}
	if err := emitDiagnostic(opts, 1, "deck: prepare role=%s refresh=%t clean=%t\n", strings.TrimSpace(prepareWorkflow.Role), opts.Refresh, opts.Clean); err != nil {
		return err
	}
	planDiagnostics, err := prepare.InspectPlan(prepareWorkflow, preparedRoot.Abs(), prepare.RunOptions{BundleRoot: preparedRoot.Abs(), ForceRedownload: opts.Refresh})
	if err != nil {
		return err
	}
	artifactGroups := summarizeArtifactGroups(prepareWorkflow)
	if len(artifactGroups) == 0 {
		fallbackGroups, err := summarizeArtifactGroupsFromFile(prepareWorkflowPath)
		if err != nil {
			return err
		}
		artifactGroups = fallbackGroups
	}
	if len(artifactGroups) > 0 {
		if err := emitDiagnostic(opts, 2, "deck: prepare artifactGroups=%d\n", len(artifactGroups)); err != nil {
			return err
		}
		for _, group := range artifactGroups {
			if err := emitDiagnostic(opts, 2, "deck: prepare artifactGroup kind=%s name=%s jobs=%d parallelism=%d retry=%d\n", group.Kind, group.Name, group.Jobs, group.Parallelism, group.Retry); err != nil {
				return err
			}
		}
	}
	if len(planDiagnostics.CachePlan.Artifacts) > 0 {
		fetchCount := 0
		reuseCount := 0
		for _, artifact := range planDiagnostics.CachePlan.Artifacts {
			switch strings.TrimSpace(artifact.Action) {
			case "REUSE":
				reuseCount++
			default:
				fetchCount++
			}
			if err := emitDiagnostic(opts, 2, "deck: prepare cacheArtifact step=%s type=%s action=%s\n", artifact.StepID, artifact.Type, artifact.Action); err != nil {
				return err
			}
		}
		if err := emitDiagnostic(opts, 2, "deck: prepare cachePlan fetch=%d reuse=%d\n", fetchCount, reuseCount); err != nil {
			return err
		}
	}

	if opts.DryRun {
		if err := emitDiagnostic(opts, 1, "deck: prepare dry-run outputsRoot=%s\n", filepath.ToSlash(preparedRoot.Abs())); err != nil {
			return err
		}
		if err := emitDiagnostic(opts, 2, "deck: prepare workflowIncludes=%d\n", workflowIncludeCount(prepareWorkflowPath, varsWorkflowPath, applyWorkflowPath)); err != nil {
			return err
		}
		for _, line := range []string{
			fmt.Sprintf("PREPARE_WORKFLOW=%s", filepath.ToSlash(prepareWorkflowPath)),
			fmt.Sprintf("WORKFLOW_INCLUDE=%s", filepath.ToSlash(prepareWorkflowPath)),
			fmt.Sprintf("PREPARED_ROOT=%s", filepath.ToSlash(preparedRoot.Abs())),
			fmt.Sprintf("WRITE=%s", filepath.ToSlash(filepath.Join(preparedRoot.Abs(), "packages"))),
			fmt.Sprintf("WRITE=%s", filepath.ToSlash(filepath.Join(preparedRoot.Abs(), "images"))),
			fmt.Sprintf("WRITE=%s", filepath.ToSlash(filepath.Join(preparedRoot.Abs(), "files"))),
			fmt.Sprintf("WRITE=%s", filepath.ToSlash(filepath.Join(filepath.Dir(preparedRoot.Abs()), "deck"))),
			fmt.Sprintf("WRITE=%s", filepath.ToSlash(filepath.Join(filepath.Dir(preparedRoot.Abs()), ".deck", "manifest.json"))),
		} {
			if err := printLine(opts.Stdout, line); err != nil {
				return err
			}
		}
		if varsWorkflowPath != "" {
			if err := printLine(opts.Stdout, fmt.Sprintf("WORKFLOW_INCLUDE=%s", filepath.ToSlash(varsWorkflowPath))); err != nil {
				return err
			}
		}
		if applyWorkflowPath != "" {
			if err := printLine(opts.Stdout, fmt.Sprintf("WORKFLOW_INCLUDE=%s", filepath.ToSlash(applyWorkflowPath))); err != nil {
				return err
			}
		}
		return nil
	}

	if opts.Clean {
		if err := emitDiagnostic(opts, 1, "deck: prepare cleaning preparedRoot=%s\n", filepath.ToSlash(preparedRoot.Abs())); err != nil {
			return err
		}
		if err := preparedHostPath.RemoveAll(); err != nil {
			return fmt.Errorf("reset prepared root: %w", err)
		}
	}
	if err := preparedHostPath.EnsureDir(filemode.PublishedArtifact); err != nil {
		return fmt.Errorf("create prepared root: %w", err)
	}

	if err := prepare.Run(ctx, prepareWorkflow, prepare.RunOptions{BundleRoot: preparedRoot.Abs(), ForceRedownload: opts.Refresh}); err != nil {
		return err
	}
	if err := emitDiagnostic(opts, 2, "deck: prepare bundleRoot=%s\n", filepath.ToSlash(preparedRoot.Abs())); err != nil {
		return err
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve deck binary path: %w", err)
	}
	if err := emitDiagnostic(opts, 2, "deck: prepare binary=%s\n", filepath.ToSlash(execPath)); err != nil {
		return err
	}
	binaryBytes, err := fsutil.ReadFile(execPath)
	if err != nil {
		return fmt.Errorf("read deck binary: %w", err)
	}
	workspaceRoot := filepath.Dir(preparedRoot.Abs())
	if err := writeBytes(filepath.Join(workspaceRoot, "deck"), binaryBytes, 0o755); err != nil {
		return err
	}

	manifest, err := buildPreparedManifest(preparedRoot)
	if err != nil {
		return err
	}
	if err := writePreparedManifest(filepath.Join(workspaceRoot, ".deck", "manifest.json"), manifest); err != nil {
		return err
	}
	if err := emitDiagnostic(opts, 1, "deck: prepare manifestEntries=%d workspaceRoot=%s\n", len(manifest.Entries), filepath.ToSlash(workspaceRoot)); err != nil {
		return err
	}
	if err := emitDiagnostic(opts, 2, "deck: prepare manifestPath=%s\n", filepath.ToSlash(filepath.Join(workspaceRoot, ".deck", "manifest.json"))); err != nil {
		return err
	}

	return printLine(opts.Stdout, fmt.Sprintf("prepare: ok (%s)", preparedRoot.Abs()))
}

func emitDiagnostic(opts Options, level int, format string, args ...any) error {
	if opts.Diagnosticf == nil {
		return nil
	}
	return opts.Diagnosticf(level, format, args...)
}

func workflowIncludeCount(prepareWorkflowPath, varsWorkflowPath, applyWorkflowPath string) int {
	count := 1
	if strings.TrimSpace(varsWorkflowPath) != "" {
		count++
	}
	if strings.TrimSpace(applyWorkflowPath) != "" {
		count++
	}
	return count
}

func summarizeArtifactGroups(wf *config.Workflow) []prepare.ArtifactGroupDiagnostic {
	if wf == nil || wf.Artifacts == nil {
		return nil
	}
	summary := make([]prepare.ArtifactGroupDiagnostic, 0, len(wf.Artifacts.Files)+len(wf.Artifacts.Images)+len(wf.Artifacts.Packages))
	for _, group := range wf.Artifacts.Files {
		summary = append(summary, prepare.ArtifactGroupDiagnostic{
			Kind:        "file",
			Name:        strings.TrimSpace(group.Group),
			Jobs:        len(expandPrepareTargets(group.Targets)) * len(group.Items),
			Parallelism: normalizeParallelism(group.Execution),
			Retry:       normalizeRetry(group.Execution),
		})
	}
	for _, group := range wf.Artifacts.Images {
		summary = append(summary, prepare.ArtifactGroupDiagnostic{
			Kind:        "image",
			Name:        strings.TrimSpace(group.Group),
			Jobs:        len(expandPrepareTargets(group.Targets)) * len(group.Items),
			Parallelism: normalizeParallelism(group.Execution),
			Retry:       normalizeRetry(group.Execution),
		})
	}
	for _, group := range wf.Artifacts.Packages {
		summary = append(summary, prepare.ArtifactGroupDiagnostic{
			Kind:        "package",
			Name:        strings.TrimSpace(group.Group),
			Jobs:        len(expandPrepareTargets(group.Targets)),
			Parallelism: normalizeParallelism(group.Execution),
			Retry:       normalizeRetry(group.Execution),
		})
	}
	return summary
}

func expandPrepareTargets(targets []config.ArtifactTarget) []config.ArtifactTarget {
	if len(targets) == 0 {
		return []config.ArtifactTarget{{}}
	}
	return targets
}

func normalizeParallelism(spec *config.ArtifactExecutionSpec) int {
	if spec == nil || spec.Parallelism < 1 {
		return 1
	}
	return spec.Parallelism
}

func normalizeRetry(spec *config.ArtifactExecutionSpec) int {
	if spec == nil || spec.Retry < 0 {
		return 0
	}
	return spec.Retry
}

func summarizeArtifactGroupsFromFile(path string) ([]prepare.ArtifactGroupDiagnostic, error) {
	raw, err := fsutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read workflow file: %w", err)
	}
	var partial struct {
		Artifacts *config.ArtifactsSpec `yaml:"artifacts"`
	}
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	if err := dec.Decode(&partial); err != nil {
		return nil, fmt.Errorf("parse workflow artifacts: %w", err)
	}
	return summarizeArtifactGroups(&config.Workflow{Artifacts: partial.Artifacts}), nil
}

func printLine(w io.Writer, line string) error {
	if w == nil {
		w = os.Stdout
	}
	_, err := fmt.Fprintln(w, line)
	return err
}

func discoverPrepareWorkflow(ctx context.Context) (string, error) {
	workflowDir := filepath.Join(".", workspacepaths.WorkflowRootDir)
	absWorkflowDir, err := filepath.Abs(workflowDir)
	if err != nil {
		return "", fmt.Errorf("resolve workflow directory: %w", err)
	}
	info, err := os.Stat(absWorkflowDir)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("workflow directory not found: %s", absWorkflowDir)
	}

	preferred := workspacepaths.CanonicalPrepareWorkflowPath(filepath.Dir(absWorkflowDir))
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
	path := workspacepaths.CanonicalApplyWorkflowPath(filepath.Dir(workflowRootPath))
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

func resolveOptionalVarsWorkflowPath(workflowRootPath string) (string, error) {
	varsPath := workspacepaths.CanonicalVarsPath(filepath.Dir(workflowRootPath))
	if info, err := os.Stat(varsPath); err == nil && !info.IsDir() {
		return varsPath, nil
	}
	return "", nil
}

func writeBytes(path string, data []byte, mode os.FileMode) error {
	hostPath, err := hostfs.NewHostPath(path)
	if err != nil {
		return err
	}
	if err := hostPath.WriteFileMode(data, mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func buildPreparedManifest(bundleRoot fsutil.PreparedRoot) (preparedManifest, error) {
	entries := make([]preparedManifestEntry, 0)
	workspaceRoot := filepath.Dir(bundleRoot.Abs())
	for _, root := range []string{"packages", "images", "files"} {
		if _, _, err := bundleRoot.Stat(root); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return preparedManifest{}, err
		}
		if err := bundleRoot.WalkFiles(func(path string, d os.DirEntry) error {
			if d.IsDir() {
				return nil
			}
			raw, err := fsutil.ReadFile(path)
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
			entries = append(entries, preparedManifestEntry{Path: filepath.ToSlash(rel), SHA256: hex.EncodeToString(sum[:]), Size: info.Size()})
			return nil
		}, root); err != nil {
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
