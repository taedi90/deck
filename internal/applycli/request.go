package applycli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/taedi90/deck/internal/bundle"
	"github.com/taedi90/deck/internal/config"
	"github.com/taedi90/deck/internal/filemode"
	"github.com/taedi90/deck/internal/fsutil"
	"github.com/taedi90/deck/internal/install"
	"github.com/taedi90/deck/internal/userdirs"
	"github.com/taedi90/deck/internal/validate"
	"github.com/taedi90/deck/internal/workspacepaths"
)

type ExecutionRequestOptions struct {
	CommandName                  string
	WorkflowPath                 string
	DiscoverWorkflow             func(context.Context) (string, error)
	AllowRemoteWorkflow          bool
	NormalizeLocalWorkflowPath   bool
	VarOverrides                 map[string]any
	Fresh                        bool
	SelectedPhase                string
	DefaultPhase                 string
	BuildExecutionWorkflow       bool
	ResolveStatePath             bool
	StatePathFromExecutionTarget bool
}

type ExecutionRequest struct {
	WorkflowPath      string
	Workflow          *config.Workflow
	Fresh             bool
	SelectedPhase     string
	ExecutionWorkflow *config.Workflow
	StatePath         string
}

func ResolveExecutionRequest(ctx context.Context, opts ExecutionRequestOptions) (ExecutionRequest, error) {
	if ctx == nil {
		return ExecutionRequest{}, fmt.Errorf("context is nil")
	}
	workflowPath := strings.TrimSpace(opts.WorkflowPath)
	if workflowPath == "" && opts.DiscoverWorkflow != nil {
		resolvedWorkflowPath, err := opts.DiscoverWorkflow(ctx)
		if err != nil {
			return ExecutionRequest{}, err
		}
		workflowPath = strings.TrimSpace(resolvedWorkflowPath)
	}

	isRemoteWorkflow := opts.AllowRemoteWorkflow && IsHTTPWorkflowPath(workflowPath)
	if isRemoteWorkflow {
		workflowBytes, err := FetchWorkflowForValidation(ctx, workflowPath)
		if err != nil {
			return ExecutionRequest{}, err
		}
		if err := validate.Bytes(workflowPath, workflowBytes); err != nil {
			return ExecutionRequest{}, err
		}
	} else {
		if opts.NormalizeLocalWorkflowPath {
			resolvedWorkflowPath, err := filepath.Abs(workflowPath)
			if err != nil {
				return ExecutionRequest{}, fmt.Errorf("resolve workflow path: %w", err)
			}
			workflowPath = resolvedWorkflowPath
		}
		if err := validate.FileWithContext(ctx, workflowPath); err != nil {
			return ExecutionRequest{}, err
		}
	}

	wf, err := config.LoadWithOptions(ctx, workflowPath, config.LoadOptions{VarOverrides: opts.VarOverrides})
	if err != nil {
		return ExecutionRequest{}, err
	}
	selectedPhase := strings.TrimSpace(opts.SelectedPhase)
	if selectedPhase == "" {
		selectedPhase = strings.TrimSpace(opts.DefaultPhase)
	}

	var executionWorkflow *config.Workflow
	if opts.BuildExecutionWorkflow {
		executionWorkflow, err = BuildExecutionWorkflow(wf, selectedPhase)
		if err != nil {
			return ExecutionRequest{}, err
		}
	}

	statePath := ""
	if opts.ResolveStatePath {
		stateWorkflow := wf
		if opts.StatePathFromExecutionTarget {
			stateWorkflow = executionWorkflow
		}
		resolvedStatePath, err := ResolveInstallStatePath(stateWorkflow)
		if err != nil {
			return ExecutionRequest{}, err
		}
		statePath = resolvedStatePath
	}

	return ExecutionRequest{
		WorkflowPath:      workflowPath,
		Workflow:          wf,
		Fresh:             opts.Fresh,
		SelectedPhase:     selectedPhase,
		ExecutionWorkflow: executionWorkflow,
		StatePath:         statePath,
	}, nil
}

func LoadInstallDryRunState(request ExecutionRequest) (*install.State, error) {
	if request.Fresh {
		return &install.State{CompletedPhases: []string{}, RuntimeVars: map[string]any{}}, nil
	}
	wf := request.Workflow
	if wf == nil {
		wf = request.ExecutionWorkflow
	}
	statePath, err := ResolveInstallStatePath(wf)
	if err != nil {
		return nil, err
	}
	statePath, err = install.ResolveStateReadPathForWorkflow(wf, statePath)
	if err != nil {
		return nil, err
	}
	return install.LoadState(statePath)
}

func ResolveInstallStatePath(wf *config.Workflow) (string, error) {
	return install.DefaultStatePath(wf)
}

func IsHTTPWorkflowPath(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	return strings.TrimSpace(parsed.Host) != ""
}

func FetchWorkflowForValidation(ctx context.Context, rawURL string) ([]byte, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("get workflow url: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get workflow url: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get workflow url: unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read workflow url: %w", err)
	}
	return body, nil
}

func ResolveBundleRoot(positionalBundle string) (string, error) {
	if strings.TrimSpace(positionalBundle) != "" {
		return resolveBundleCandidate(positionalBundle, true)
	}
	for _, candidate := range []string{"./bundle.tar", "."} {
		resolved, err := resolveBundleCandidate(candidate, false)
		if err != nil {
			return "", err
		}
		if resolved != "" {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("bundle not found: expected positional bundle path, ./bundle.tar, ./bundle, or current directory with workflows/")
}

func BundleSHA256Hex(path string) (string, error) {
	return sha256FileHex(path)
}

func HasWorkflowDir(root string) bool {
	return hasWorkflowDir(root)
}

func resolveBundleCandidate(candidate string, strict bool) (string, error) {
	resolved, err := filepath.Abs(strings.TrimSpace(candidate))
	if err != nil {
		return "", fmt.Errorf("resolve bundle path: %w", err)
	}
	bundleRoot, err := fsutil.NewBundleRoot(resolved)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(bundleRoot.Abs())
	if err != nil {
		if os.IsNotExist(err) && !strict {
			return "", nil
		}
		if os.IsNotExist(err) {
			return "", fmt.Errorf("bundle path not found: %s", resolved)
		}
		return "", fmt.Errorf("stat bundle path: %w", err)
	}
	if info.IsDir() {
		if hasWorkflowDir(bundleRoot.Abs()) {
			return bundleRoot.Abs(), nil
		}
		if strict {
			return "", fmt.Errorf("bundle directory must contain workflows/: %s", bundleRoot.Abs())
		}
		return "", nil
	}
	if strings.ToLower(filepath.Ext(resolved)) != ".tar" {
		if strict {
			return "", fmt.Errorf("bundle path must be a directory or .tar archive: %s", resolved)
		}
		return "", nil
	}
	return extractBundleArchive(bundleRoot.Abs())
}

func extractBundleArchive(archivePath string) (string, error) {
	sum, err := sha256FileHex(archivePath)
	if err != nil {
		return "", fmt.Errorf("hash bundle archive: %w", err)
	}
	cacheRoot, err := userdirs.CacheRoot()
	if err != nil {
		return "", err
	}
	extractRoot := filepath.Join(cacheRoot, "extract", sum)
	bundleRoot, err := fsutil.NewBundleRoot(filepath.Join(extractRoot, "bundle"))
	if err != nil {
		return "", err
	}
	if hasWorkflowDir(bundleRoot.Abs()) {
		return bundleRoot.Abs(), nil
	}
	if err := os.RemoveAll(extractRoot); err != nil {
		return "", fmt.Errorf("reset extract cache: %w", err)
	}
	if err := filemode.EnsureDir(extractRoot, filemode.PrivateState); err != nil {
		return "", fmt.Errorf("create extract cache directory: %w", err)
	}
	if err := bundle.ImportArchive(archivePath, extractRoot); err != nil {
		return "", err
	}
	if !hasWorkflowDir(bundleRoot.Abs()) {
		return "", fmt.Errorf("extracted bundle missing workflows/: %s", bundleRoot.Abs())
	}
	return bundleRoot.Abs(), nil
}

func sha256FileHex(path string) (string, error) {
	f, err := fsutil.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hasWorkflowDir(root string) bool {
	workflowDir := workspacepaths.WorkflowRootPath(root)
	info, err := os.Stat(workflowDir)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func DiscoverApplyWorkflow(ctx context.Context, bundleRoot string) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("context is nil")
	}
	root, err := fsutil.NewBundleRoot(bundleRoot)
	if err != nil {
		return "", err
	}
	workflowDir, err := root.Resolve(workspacepaths.WorkflowRootDir)
	if err != nil {
		return "", err
	}
	if !hasWorkflowDir(root.Abs()) {
		return "", fmt.Errorf("workflow directory not found: %s", workflowDir)
	}
	preferred, err := root.Resolve(workspacepaths.WorkflowRootDir, workspacepaths.WorkflowScenariosDir, "apply.yaml")
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(preferred); err == nil && !info.IsDir() {
		return preferred, nil
	}
	entries, err := os.ReadDir(workflowDir)
	if err != nil {
		return "", fmt.Errorf("read workflow directory: %w", err)
	}
	matches := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		lower := strings.ToLower(entry.Name())
		if !strings.HasSuffix(lower, ".yaml") && !strings.HasSuffix(lower, ".yml") {
			continue
		}
		candidate := filepath.Join(workflowDir, entry.Name())
		matches = append(matches, candidate)
	}
	sort.Strings(matches)
	if len(matches) == 0 {
		return "", fmt.Errorf("apply workflow not found under %s", workflowDir)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("multiple apply workflows found under %s", workflowDir)
	}
	return matches[0], nil
}
