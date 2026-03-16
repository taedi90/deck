package askcli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/taedi90/deck/internal/askconfig"
	"github.com/taedi90/deck/internal/askcontext"
	"github.com/taedi90/deck/internal/askprovider"
	"github.com/taedi90/deck/internal/askreview"
	"github.com/taedi90/deck/internal/askstate"
	"github.com/taedi90/deck/internal/fsutil"
	"github.com/taedi90/deck/internal/validate"
	"github.com/taedi90/deck/internal/workspacepaths"
)

type Options struct {
	Root          string
	Prompt        string
	FromPath      string
	Write         bool
	Review        bool
	MaxIterations int
	Provider      string
	Model         string
	Stdout        io.Writer
	Stderr        io.Writer
}

type generatedResponse struct {
	Summary string          `json:"summary"`
	Review  []string        `json:"review"`
	Files   []generatedFile `json:"files"`
}

type generatedFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func Execute(ctx context.Context, opts Options, client askprovider.Client) error {
	if client == nil {
		return fmt.Errorf("ask backend is not configured")
	}
	resolvedRoot, err := filepath.Abs(strings.TrimSpace(opts.Root))
	if err != nil {
		return fmt.Errorf("resolve workspace root: %w", err)
	}
	requestText, err := loadRequestText(strings.TrimSpace(opts.Prompt), strings.TrimSpace(opts.FromPath))
	if err != nil {
		return err
	}
	if strings.TrimSpace(requestText) == "" && !opts.Review {
		return fmt.Errorf("ask request is required")
	}
	state, err := askstate.Load(resolvedRoot)
	if err != nil {
		return err
	}
	effective, err := askconfig.ResolveEffective(askconfig.Settings{Provider: opts.Provider, Model: opts.Model})
	if err != nil {
		return err
	}
	if askconfig.NeedsAPIKey(effective.Provider) && strings.TrimSpace(effective.APIKey) == "" {
		return fmt.Errorf("missing ask api key for provider %q; set %s or run `deck ask auth set --api-key ...`", effective.Provider, "DECK_ASK_API_KEY")
	}
	maxIterations := opts.MaxIterations
	if maxIterations <= 0 {
		maxIterations = 3
	}
	built, err := askcontext.Build(askcontext.BuildInput{
		Root:      resolvedRoot,
		Prompt:    requestText,
		Review:    opts.Review,
		State:     state,
		FromLabel: strings.TrimSpace(opts.FromPath),
	})
	if err != nil {
		return err
	}
	candidate, lintSummary, lastRaw, err := generateCandidate(ctx, client, askprovider.Request{
		Provider:     effective.Provider,
		Model:        effective.Model,
		APIKey:       effective.APIKey,
		SystemPrompt: built.SystemPrompt,
		Prompt:       built.Prompt,
		MaxRetries:   maxIterations,
	}, resolvedRoot, built.Mode, maxIterations)
	if err != nil {
		return err
	}
	localFindings := localFindings(resolvedRoot, built.Mode, candidate)
	if err := askstate.Save(resolvedRoot, askstate.Context{
		LastMode:   string(built.Mode),
		LastPrompt: strings.TrimSpace(requestText),
		LastFiles:  filePaths(candidate.Files),
		LastLint:   lintSummary,
	}, requestText, lastRaw); err != nil {
		return err
	}
	if opts.Write && built.Mode != askcontext.ModeReview {
		if err := writeFiles(resolvedRoot, candidate.Files); err != nil {
			return err
		}
	}
	return render(opts.Stdout, opts.Stderr, renderInput{
		Mode:         built.Mode,
		Candidate:    candidate,
		LintSummary:  lintSummary,
		LocalReview:  localFindings,
		WroteFiles:   opts.Write && built.Mode != askcontext.ModeReview,
		ConfigSource: effective,
	})
}

func generateCandidate(ctx context.Context, client askprovider.Client, req askprovider.Request, root string, mode askcontext.Mode, maxIterations int) (generatedResponse, string, string, error) {
	var lastValidation string
	var lastRaw string
	for attempt := 1; attempt <= maxIterations; attempt++ {
		currentPrompt := req.Prompt
		if attempt > 1 && lastValidation != "" {
			currentPrompt = currentPrompt + "\n\nLocal validation failed. Fix the response and return full JSON again. Errors:\n" + lastValidation
		}
		resp, err := client.Generate(ctx, askprovider.Request{
			Provider:     req.Provider,
			Model:        req.Model,
			APIKey:       req.APIKey,
			SystemPrompt: req.SystemPrompt,
			Prompt:       currentPrompt,
			MaxRetries:   req.MaxRetries,
		})
		if err != nil {
			return generatedResponse{}, lastValidation, lastRaw, err
		}
		lastRaw = resp.Content
		candidate, err := parseGeneratedResponse(resp.Content)
		if err != nil {
			lastValidation = err.Error()
			continue
		}
		lintSummary, err := validateCandidate(root, mode, candidate)
		if err == nil {
			return candidate, lintSummary, lastRaw, nil
		}
		lastValidation = err.Error()
	}
	if lastValidation == "" {
		lastValidation = "ask generation failed without a parseable response"
	}
	return generatedResponse{}, lastValidation, lastRaw, fmt.Errorf("ask generation did not validate after %d attempts: %s", maxIterations, lastValidation)
}

func parseGeneratedResponse(raw string) (generatedResponse, error) {
	cleaned := strings.TrimSpace(cleanResponse(raw))
	if cleaned == "" {
		return generatedResponse{}, fmt.Errorf("model returned empty response")
	}
	var candidate generatedResponse
	if err := json.Unmarshal([]byte(cleaned), &candidate); err != nil {
		return generatedResponse{}, fmt.Errorf("parse ask response json: %w", err)
	}
	if strings.TrimSpace(candidate.Summary) == "" {
		candidate.Summary = "No summary provided."
	}
	return candidate, nil
}

func validateCandidate(root string, mode askcontext.Mode, candidate generatedResponse) (string, error) {
	if mode != askcontext.ModeReview && len(candidate.Files) == 0 {
		return "", fmt.Errorf("response did not include any files")
	}
	staged, err := stageWorkspace(root, candidate.Files)
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(staged) }()
	if mode == askcontext.ModeReview {
		return "review only", nil
	}
	paths := make([]string, 0, len(candidate.Files))
	for _, file := range candidate.Files {
		if err := validateGeneratedFile(staged, file); err != nil {
			return "", err
		}
		paths = append(paths, file.Path)
	}
	entrypoints := scenarioPaths(staged, paths)
	validated := make([]string, 0, len(entrypoints))
	for _, path := range entrypoints {
		files, err := validate.Entrypoint(path)
		if err != nil {
			return "", err
		}
		validated = append(validated, files...)
	}
	if len(entrypoints) == 0 {
		return "file validation only", nil
	}
	validated = dedupe(validated)
	return fmt.Sprintf("lint ok (%d workflows)", len(validated)), nil
}

func validateGeneratedFile(root string, file generatedFile) error {
	if err := validateGeneratedPath(file.Path); err != nil {
		return err
	}
	target, err := fsutil.ResolveUnder(root, strings.Split(filepath.ToSlash(file.Path), "/")...)
	if err != nil {
		return err
	}
	if strings.HasSuffix(file.Path, ".yaml") || strings.HasSuffix(file.Path, ".yml") {
		if isVarsPath(file.Path) {
			var vars map[string]any
			if err := yaml.Unmarshal([]byte(file.Content), &vars); err != nil {
				return fmt.Errorf("%s: parse vars yaml: %w", file.Path, err)
			}
			return nil
		}
		if err := validate.Bytes(target, []byte(file.Content)); err != nil {
			return err
		}
	}
	return nil
}

func writeFiles(root string, files []generatedFile) error {
	if err := ensureScaffold(root); err != nil {
		return err
	}
	for _, file := range files {
		if err := validateGeneratedPath(file.Path); err != nil {
			return err
		}
		target, err := fsutil.ResolveUnder(root, strings.Split(filepath.ToSlash(file.Path), "/")...)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return fmt.Errorf("create ask target directory: %w", err)
		}
		if err := os.WriteFile(target, []byte(file.Content), 0o600); err != nil {
			return fmt.Errorf("write %s: %w", file.Path, err)
		}
	}
	return nil
}

func loadRequestText(prompt string, fromPath string) (string, error) {
	prompt = strings.TrimSpace(prompt)
	fromPath = strings.TrimSpace(fromPath)
	if fromPath == "" {
		return prompt, nil
	}
	//nolint:gosec // The user explicitly chose the request file path.
	raw, err := os.ReadFile(fromPath)
	if err != nil {
		return "", fmt.Errorf("read ask request file: %w", err)
	}
	fromText := strings.TrimSpace(string(raw))
	if prompt == "" {
		return fromText, nil
	}
	return prompt + "\n\nAttached request details:\n" + fromText, nil
}

func validateGeneratedPath(path string) error {
	clean := filepath.ToSlash(strings.TrimSpace(path))
	if clean == "" {
		return fmt.Errorf("generated file path is empty")
	}
	allowed := strings.HasPrefix(clean, "workflows/scenarios/") || strings.HasPrefix(clean, "workflows/components/") || clean == "workflows/vars.yaml"
	if !allowed {
		return fmt.Errorf("generated file path is not allowed: %s", clean)
	}
	if strings.Contains(clean, "..") {
		return fmt.Errorf("generated file path escapes workspace: %s", clean)
	}
	return nil
}

func stageWorkspace(root string, files []generatedFile) (string, error) {
	tempRoot, err := os.MkdirTemp("", "deck-ask-workspace-")
	if err != nil {
		return "", fmt.Errorf("create ask staging workspace: %w", err)
	}
	workflowRoot := filepath.Join(root, workspacepaths.WorkflowRootDir)
	if info, err := os.Stat(workflowRoot); err == nil && info.IsDir() {
		if err := copyTree(workflowRoot, filepath.Join(tempRoot, workspacepaths.WorkflowRootDir)); err != nil {
			return "", err
		}
	}
	for _, file := range files {
		if err := validateGeneratedPath(file.Path); err != nil {
			return "", err
		}
		target, err := fsutil.ResolveUnder(tempRoot, strings.Split(filepath.ToSlash(file.Path), "/")...)
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return "", fmt.Errorf("create ask staging directory: %w", err)
		}
		if err := os.WriteFile(target, []byte(file.Content), 0o600); err != nil {
			return "", fmt.Errorf("write ask staging file: %w", err)
		}
	}
	return tempRoot, nil
}

func scenarioPaths(root string, candidatePaths []string) []string {
	paths := make([]string, 0)
	seen := map[string]bool{}
	for _, rel := range candidatePaths {
		clean := filepath.ToSlash(strings.TrimSpace(rel))
		if !strings.HasPrefix(clean, "workflows/scenarios/") {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(clean))
		if !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	if len(paths) > 0 {
		sort.Strings(paths)
		return paths
	}
	scenarioDir := filepath.Join(root, workspacepaths.WorkflowRootDir, workspacepaths.WorkflowScenariosDir)
	entries, err := os.ReadDir(scenarioDir)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
			paths = append(paths, filepath.Join(scenarioDir, entry.Name()))
		}
	}
	sort.Strings(paths)
	return paths
}

func localFindings(root string, mode askcontext.Mode, candidate generatedResponse) []askreview.Finding {
	if mode == askcontext.ModeReview {
		return askreview.Workspace(root)
	}
	files := make(map[string]string, len(candidate.Files))
	for _, file := range candidate.Files {
		files[file.Path] = file.Content
	}
	return askreview.Candidate(files)
}

type renderInput struct {
	Mode         askcontext.Mode
	Candidate    generatedResponse
	LintSummary  string
	LocalReview  []askreview.Finding
	WroteFiles   bool
	ConfigSource askconfig.EffectiveSettings
}

func render(stdout io.Writer, stderr io.Writer, input renderInput) error {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if _, err := fmt.Fprintf(stdout, "ask: %s\n", input.Candidate.Summary); err != nil {
		return err
	}
	if input.LintSummary != "" {
		if _, err := fmt.Fprintf(stdout, "lint: %s\n", input.LintSummary); err != nil {
			return err
		}
	}
	if len(input.Candidate.Review) > 0 {
		if _, err := io.WriteString(stdout, "review:\n"); err != nil {
			return err
		}
		for _, line := range input.Candidate.Review {
			if _, err := fmt.Fprintf(stdout, "- %s\n", line); err != nil {
				return err
			}
		}
	}
	if len(input.LocalReview) > 0 {
		if _, err := io.WriteString(stdout, "local-findings:\n"); err != nil {
			return err
		}
		for _, finding := range input.LocalReview {
			if _, err := fmt.Fprintf(stdout, "- [%s] %s\n", finding.Severity, finding.Message); err != nil {
				return err
			}
		}
	}
	if len(input.Candidate.Files) > 0 {
		label := "preview"
		if input.WroteFiles {
			label = "wrote"
		}
		if _, err := fmt.Fprintf(stdout, "%s:\n", label); err != nil {
			return err
		}
		for _, file := range input.Candidate.Files {
			if _, err := fmt.Fprintf(stdout, "--- %s\n%s", file.Path, file.Content); err != nil {
				return err
			}
			if !strings.HasSuffix(file.Content, "\n") {
				if _, err := io.WriteString(stdout, "\n"); err != nil {
					return err
				}
			}
		}
	}
	if input.WroteFiles {
		if _, err := io.WriteString(stdout, "ask write: ok\n"); err != nil {
			return err
		}
	}
	if input.ConfigSource.APIKeySource != "unset" {
		if _, err := fmt.Fprintf(stderr, "deck ask using provider=%s model=%s apiKeySource=%s\n", input.ConfigSource.Provider, input.ConfigSource.Model, input.ConfigSource.APIKeySource); err != nil {
			return err
		}
	}
	return nil
}

func ensureScaffold(root string) error {
	for _, dir := range []string{
		filepath.Join(root, ".deck"),
		filepath.Join(root, workspacepaths.WorkflowRootDir, workspacepaths.WorkflowScenariosDir),
		filepath.Join(root, workspacepaths.WorkflowRootDir, workspacepaths.WorkflowComponentsDir),
		filepath.Join(root, workspacepaths.PreparedDirRel, "files"),
		filepath.Join(root, workspacepaths.PreparedDirRel, "images"),
		filepath.Join(root, workspacepaths.PreparedDirRel, "packages"),
	} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create workspace scaffold: %w", err)
		}
	}
	defaults := map[string]string{
		filepath.Join(root, ".gitignore"):                                       strings.Join([]string{"/.deck/", "/deck", "/outputs/", "*.tar", ""}, "\n"),
		filepath.Join(root, ".deckignore"):                                      strings.Join([]string{".git/", ".gitignore", ".deckignore", "/*.tar", ""}, "\n"),
		filepath.Join(root, workspacepaths.PreparedDirRel, "files", ".keep"):    "",
		filepath.Join(root, workspacepaths.PreparedDirRel, "images", ".keep"):   "",
		filepath.Join(root, workspacepaths.PreparedDirRel, "packages", ".keep"): "",
	}
	for path, content := range defaults {
		if _, err := os.Stat(path); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat scaffold file %s: %w", path, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return fmt.Errorf("write scaffold file %s: %w", path, err)
		}
	}
	return nil
}

func copyTree(src string, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		//nolint:gosec // Paths come from walking the already-selected workspace tree.
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}
		//nolint:gosec // Target remains under the temporary staging workspace.
		return os.WriteFile(target, raw, 0o600)
	})
}

func isVarsPath(path string) bool {
	return filepath.ToSlash(strings.TrimSpace(path)) == "workflows/vars.yaml"
}

func filePaths(files []generatedFile) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
	}
	return paths
}

func dedupe(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func cleanResponse(response string) string {
	response = strings.TrimSpace(response)
	response = strings.TrimPrefix(response, "```json")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	response = strings.TrimSpace(response)
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")
	if start >= 0 && end > start {
		response = response[start : end+1]
	}
	return strings.TrimSpace(response)
}
