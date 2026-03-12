package bundle

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

type MergeAction struct {
	Path   string
	Action string
	Reason string
}

type MergeReport struct {
	Destination string
	DryRun      bool
	Actions     []MergeAction
}

type stagedBundle struct {
	manifestEntries []ManifestEntry
	artifacts       map[string]stagedFile
	workflows       map[string]stagedFile
}

type stagedFile struct {
	tempPath string
	digest   string
	size     int64
}

func MergeArchive(archivePath, to string, dryRun bool) (MergeReport, error) {
	bundleData, cleanup, err := stageBundleForMerge(archivePath)
	if err != nil {
		return MergeReport{}, err
	}
	defer cleanup()

	if strings.HasPrefix(to, "http://") || strings.HasPrefix(to, "https://") {
		return MergeReport{}, fmt.Errorf("http merge destinations are not supported; use a local directory")
	}

	return mergeArchiveToLocal(bundleData, to, dryRun)
}

func stageBundleForMerge(archivePath string) (stagedBundle, func(), error) {
	src, err := os.Open(archivePath)
	if err != nil {
		return stagedBundle{}, nil, fmt.Errorf("open bundle archive: %w", err)
	}
	defer func() { _ = src.Close() }()

	stageDir, err := os.MkdirTemp("", "deck-bundle-merge-")
	if err != nil {
		return stagedBundle{}, nil, fmt.Errorf("create merge staging dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(stageDir) }

	bundleData := stagedBundle{
		artifacts: map[string]stagedFile{},
		workflows: map[string]stagedFile{},
	}

	tr := tar.NewReader(src)
	var manifestRaw []byte
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			cleanup()
			return stagedBundle{}, nil, fmt.Errorf("read bundle archive: %w", err)
		}

		rel, err := normalizeTarBundleEntry(hdr.Name)
		if err != nil {
			cleanup()
			return stagedBundle{}, nil, err
		}
		if rel == "" {
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		if rel == manifestRelativePath {
			raw, readErr := io.ReadAll(tr)
			if readErr != nil {
				cleanup()
				return stagedBundle{}, nil, fmt.Errorf("read manifest from archive: %w", readErr)
			}
			manifestRaw = raw
			continue
		}

		if !shouldStageForMerge(rel) {
			continue
		}

		staged, stageErr := stageTarFile(stageDir, rel, tr)
		if stageErr != nil {
			cleanup()
			return stagedBundle{}, nil, stageErr
		}

		if strings.HasPrefix(rel, "workflows/") && strings.HasSuffix(rel, ".yaml") {
			bundleData.workflows[rel] = staged
			continue
		}
		bundleData.artifacts[rel] = staged
	}

	if len(manifestRaw) == 0 {
		cleanup()
		return stagedBundle{}, nil, fmt.Errorf("E_MANIFEST_MISSING: %s", tarManifestPath())
	}

	entries, _, err := normalizeManifestEntriesFromBytes(manifestRaw, tarManifestPath())
	if err != nil {
		cleanup()
		return stagedBundle{}, nil, err
	}
	bundleData.manifestEntries = entries

	for _, entry := range entries {
		if _, ok := bundleData.artifacts[entry.Path]; !ok {
			cleanup()
			return stagedBundle{}, nil, fmt.Errorf("E_BUNDLE_INTEGRITY: missing artifact %s", entry.Path)
		}
	}
	if _, ok := bundleData.artifacts["files/deck"]; !ok {
		cleanup()
		return stagedBundle{}, nil, fmt.Errorf("E_BUNDLE_INTEGRITY: missing artifact %s", "files/deck")
	}

	return bundleData, cleanup, nil
}

func shouldStageForMerge(rel string) bool {
	if rel == "files/deck" {
		return true
	}
	if strings.HasPrefix(rel, "workflows/") && strings.HasSuffix(rel, ".yaml") {
		return true
	}
	return isManifestTrackedPath(rel)
}

func stageTarFile(stageDir, rel string, reader io.Reader) (stagedFile, error) {
	targetPath := filepath.Join(stageDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return stagedFile{}, fmt.Errorf("create merge staging parent: %w", err)
	}

	out, err := os.Create(targetPath)
	if err != nil {
		return stagedFile{}, fmt.Errorf("create merge staging file: %w", err)
	}

	h := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(out, h), reader)
	closeErr := out.Close()
	if copyErr != nil {
		return stagedFile{}, fmt.Errorf("copy merge staging file: %w", copyErr)
	}
	if closeErr != nil {
		return stagedFile{}, fmt.Errorf("close merge staging file: %w", closeErr)
	}

	return stagedFile{
		tempPath: targetPath,
		digest:   hex.EncodeToString(h.Sum(nil)),
		size:     size,
	}, nil
}

func mergeArchiveToLocal(bundleData stagedBundle, root string, dryRun bool) (MergeReport, error) {
	if strings.TrimSpace(root) == "" {
		return MergeReport{}, fmt.Errorf("merge destination is required")
	}

	resolvedRoot, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return MergeReport{}, fmt.Errorf("resolve merge destination: %w", err)
	}

	report := MergeReport{Destination: resolvedRoot, DryRun: dryRun}
	planned := map[string]struct{}{}

	for _, entry := range bundleData.manifestEntries {
		staged := bundleData.artifacts[entry.Path]
		action, reason, planErr := planLocalPath(resolvedRoot, entry.Path, staged.digest)
		if planErr != nil {
			return MergeReport{}, planErr
		}
		report.Actions = append(report.Actions, MergeAction{Path: entry.Path, Action: action, Reason: reason})
		planned[entry.Path] = struct{}{}
		if dryRun || action == "skip" {
			continue
		}
		if err := copyStagedFile(filepath.Join(resolvedRoot, filepath.FromSlash(entry.Path)), staged); err != nil {
			return MergeReport{}, err
		}
	}

	if _, ok := planned["files/deck"]; !ok {
		staged := bundleData.artifacts["files/deck"]
		action, reason, planErr := planLocalPath(resolvedRoot, "files/deck", staged.digest)
		if planErr != nil {
			return MergeReport{}, planErr
		}
		report.Actions = append(report.Actions, MergeAction{Path: "files/deck", Action: action, Reason: reason})
		if !dryRun && action != "skip" {
			if err := copyStagedFile(filepath.Join(resolvedRoot, "files", "deck"), staged); err != nil {
				return MergeReport{}, err
			}
		}
	}

	workflowPaths := sortedMapKeys(bundleData.workflows)
	for _, workflowPath := range workflowPaths {
		report.Actions = append(report.Actions, MergeAction{Path: workflowPath, Action: "overwrite", Reason: "workflow sync"})
		if dryRun {
			continue
		}
		if err := copyStagedFile(filepath.Join(resolvedRoot, filepath.FromSlash(workflowPath)), bundleData.workflows[workflowPath]); err != nil {
			return MergeReport{}, err
		}
	}

	existingIndex, exists, err := readLocalWorkflowIndex(filepath.Join(resolvedRoot, "workflows", "index.json"))
	if err != nil {
		return MergeReport{}, err
	}
	mergedIndex := mergeWorkflowIndex(existingIndex, workflowPaths)
	indexAction := "overwrite"
	if !exists {
		indexAction = "upload"
	}
	report.Actions = append(report.Actions, MergeAction{Path: "workflows/index.json", Action: indexAction, Reason: "workflow index sync"})
	if !dryRun {
		if err := writeLocalWorkflowIndex(filepath.Join(resolvedRoot, "workflows", "index.json"), mergedIndex); err != nil {
			return MergeReport{}, err
		}
	}

	return report, nil
}

func planLocalPath(root, relPath, digest string) (string, string, error) {
	targetPath := filepath.Join(root, filepath.FromSlash(relPath))
	_, actualDigest, err := fileDigest(targetPath)
	if err == nil {
		if strings.EqualFold(actualDigest, digest) {
			return "skip", "sha256 matched", nil
		}
		return "overwrite", "sha256 mismatched", nil
	}
	if os.IsNotExist(err) {
		return "upload", "destination missing", nil
	}
	return "", "", fmt.Errorf("read destination %s: %w", relPath, err)
}

func copyStagedFile(targetPath string, staged stagedFile) error {
	in, err := os.Open(staged.tempPath)
	if err != nil {
		return fmt.Errorf("open staged file %s: %w", staged.tempPath, err)
	}
	defer func() { _ = in.Close() }()

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("create destination parent for %s: %w", targetPath, err)
	}

	out, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open destination %s: %w", targetPath, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("write destination %s: %w", targetPath, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close destination %s: %w", targetPath, err)
	}
	return nil
}

func readLocalWorkflowIndex(indexPath string) ([]string, bool, error) {
	raw, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read workflows/index.json: %w", err)
	}
	var items []string
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, true, nil
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, false, fmt.Errorf("parse workflows/index.json: %w", err)
	}
	return items, true, nil
}

func writeLocalWorkflowIndex(indexPath string, items []string) error {
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return fmt.Errorf("encode workflows/index.json: %w", err)
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		return fmt.Errorf("create workflows index directory: %w", err)
	}
	if err := os.WriteFile(indexPath, raw, 0o644); err != nil {
		return fmt.Errorf("write workflows/index.json: %w", err)
	}
	return nil
}

func mergeWorkflowIndex(existing, incoming []string) []string {
	set := map[string]struct{}{}
	for _, item := range existing {
		cleaned := normalizeWorkflowIndexPath(item)
		if cleaned == "" {
			continue
		}
		set[cleaned] = struct{}{}
	}
	for _, item := range incoming {
		cleaned := normalizeWorkflowIndexPath(item)
		if cleaned == "" {
			continue
		}
		set[cleaned] = struct{}{}
	}

	merged := make([]string, 0, len(set))
	for item := range set {
		merged = append(merged, item)
	}
	sort.Strings(merged)
	return merged
}

func normalizeWorkflowIndexPath(raw string) string {
	cleaned := path.Clean(strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/")))
	if cleaned == "" || cleaned == "." {
		return ""
	}
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "workflows" || !strings.HasPrefix(cleaned, "workflows/") || !strings.HasSuffix(cleaned, ".yaml") {
		return ""
	}
	return cleaned
}

func sortedMapKeys(values map[string]stagedFile) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
