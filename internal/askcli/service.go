package askcli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/taedi90/deck/internal/askconfig"
	"github.com/taedi90/deck/internal/askcontract"
	"github.com/taedi90/deck/internal/askhooks"
	"github.com/taedi90/deck/internal/askintent"
	"github.com/taedi90/deck/internal/askprovider"
	"github.com/taedi90/deck/internal/askretrieve"
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
	Endpoint      string
	Stdout        io.Writer
	Stderr        io.Writer
}

type runResult struct {
	Route         askintent.Route
	Confidence    float64
	Reason        string
	Summary       string
	Answer        string
	ReviewLines   []string
	LintSummary   string
	LocalFindings []askreview.Finding
	Files         []askcontract.GeneratedFile
	WroteFiles    bool
	RetriesUsed   int
	LLMUsed       bool
	Termination   string
	Chunks        []askretrieve.Chunk
	DroppedChunks []string
	ConfigSource  askconfig.EffectiveSettings
}

func Execute(ctx context.Context, opts Options, client askprovider.Client) error {
	if client == nil {
		return fmt.Errorf("ask backend is not configured")
	}
	hooks := askhooks.Default()
	resolvedRoot, err := filepath.Abs(strings.TrimSpace(opts.Root))
	if err != nil {
		return fmt.Errorf("resolve workspace root: %w", err)
	}
	requestText, err := loadRequestText(strings.TrimSpace(opts.Prompt), strings.TrimSpace(opts.FromPath))
	if err != nil {
		return err
	}
	requestText = strings.TrimSpace(hooks.PreClassify(requestText))
	if requestText == "" && !opts.Review {
		return fmt.Errorf("ask request is required")
	}
	state, err := askstate.Load(resolvedRoot)
	if err != nil {
		return err
	}
	workspace, err := askretrieve.InspectWorkspace(resolvedRoot)
	if err != nil {
		return err
	}
	decision := hooks.PostClassify(askintent.Classify(askintent.Input{
		Prompt:          requestText,
		ReviewFlag:      opts.Review,
		HasWorkflowTree: workspace.HasWorkflowTree,
		HasPrepare:      workspace.HasPrepare,
		HasApply:        workspace.HasApply,
	}))
	effective, err := askconfig.ResolveEffective(askconfig.Settings{Provider: opts.Provider, Model: opts.Model, Endpoint: opts.Endpoint})
	if err != nil {
		return err
	}
	retrieval := askretrieve.Retrieve(decision.Route, requestText, workspace, state)
	result := runResult{
		Route:         decision.Route,
		Confidence:    decision.Confidence,
		Reason:        decision.Reason,
		RetriesUsed:   0,
		Chunks:        retrieval.Chunks,
		DroppedChunks: retrieval.Dropped,
		ConfigSource:  effective,
	}

	if decision.LLMPolicy == askintent.LLMRequired && askconfig.NeedsAPIKey(effective.Provider) && strings.TrimSpace(effective.APIKey) == "" {
		return fmt.Errorf("missing ask api key for provider %q; set %s or run `deck ask auth set --api-key ...`", effective.Provider, "DECK_ASK_API_KEY")
	}

	switch decision.Route {
	case askintent.RouteClarify:
		result.Summary, result.Answer, result.ReviewLines = localClarify(requestText), "", clarificationSuggestions()
		result.Termination = "clarified"
	case askintent.RouteExplain:
		result.Summary, result.Answer = localExplain(workspace, requestText)
		if shouldUseLLMForInfo(decision.Route, requestText, effective) {
			augmentWithLLM(ctx, client, effective, retrieval, requestText, decision.Route, &result)
		}
		result.Termination = "answered-locally"
	case askintent.RouteQuestion:
		if shouldUseLLMForInfo(decision.Route, requestText, effective) {
			augmentWithLLM(ctx, client, effective, retrieval, requestText, decision.Route, &result)
			result.Termination = "answered-with-llm"
		} else {
			result.Summary = "Question received"
			result.Answer = "I need either more context from your workspace or an API key for deeper question answering."
			result.ReviewLines = clarificationSuggestions()
			result.Termination = "answered-locally"
		}
	case askintent.RouteReview:
		result.Summary = "Workspace review"
		result.LocalFindings = askreview.Workspace(resolvedRoot)
		result.ReviewLines = findingsToLines(result.LocalFindings)
		if shouldUseLLMForInfo(decision.Route, requestText, effective) {
			augmentWithLLM(ctx, client, effective, retrieval, requestText, decision.Route, &result)
		}
		result.Termination = "reviewed"
	case askintent.RouteDraft, askintent.RouteRefine:
		attempts := generationAttempts(opts.MaxIterations, decision)
		gen, lintSummary, retriesUsed, err := generateWithValidation(ctx, client, askprovider.Request{
			Provider:     effective.Provider,
			Model:        effective.Model,
			APIKey:       effective.APIKey,
			Endpoint:     effective.Endpoint,
			SystemPrompt: generationSystemPrompt(decision.Route, retrieval),
			Prompt:       generationUserPrompt(workspace, state, requestText, strings.TrimSpace(opts.FromPath), decision.Route),
			MaxRetries:   attempts,
		}, resolvedRoot, attempts)
		if err != nil {
			return err
		}
		result.LLMUsed = true
		result.RetriesUsed = retriesUsed
		result.Files = gen.Files
		result.Summary = gen.Summary
		result.ReviewLines = append(result.ReviewLines, gen.Review...)
		result.LintSummary = lintSummary
		result.LocalFindings = localFindings(result.Files)
		if opts.Write {
			if err := writeFiles(resolvedRoot, result.Files); err != nil {
				return err
			}
			result.WroteFiles = true
		}
		if retriesUsed > 0 {
			result.Termination = "generated-after-repair"
		} else {
			result.Termination = "generated"
		}
	default:
		result.Summary = localClarify(requestText)
		result.ReviewLines = clarificationSuggestions()
		result.Termination = "clarified"
	}

	if err := askstate.Save(resolvedRoot, askstate.Context{
		LastMode:            string(result.Route),
		LastRoute:           string(result.Route),
		LastConfidence:      result.Confidence,
		LastReason:          result.Reason,
		LastPrompt:          strings.TrimSpace(requestText),
		LastFiles:           filePaths(result.Files),
		LastLint:            result.LintSummary,
		LastLLMUsed:         result.LLMUsed,
		LastChunkIDs:        chunkIDs(result.Chunks),
		LastDroppedChunkIDs: append([]string(nil), result.DroppedChunks...),
		LastRetries:         result.RetriesUsed,
		LastTermination:     result.Termination,
	}, requestText, resultToMarkdown(result)); err != nil {
		return err
	}

	return render(opts.Stdout, opts.Stderr, result)
}

func shouldUseLLMForInfo(route askintent.Route, prompt string, cfg askconfig.EffectiveSettings) bool {
	if askconfig.NeedsAPIKey(cfg.Provider) && strings.TrimSpace(cfg.APIKey) == "" {
		return false
	}
	prompt = strings.TrimSpace(prompt)
	switch route {
	case askintent.RouteQuestion:
		return true
	case askintent.RouteReview:
		return strings.Contains(strings.ToLower(prompt), "detail") || strings.Contains(strings.ToLower(prompt), "style")
	case askintent.RouteExplain:
		return len(prompt) > 48
	default:
		return false
	}
}

func generationAttempts(requested int, decision askintent.Decision) int {
	if !decision.AllowRetry {
		return 1
	}
	if requested > 0 {
		return requested
	}
	return 1
}

func augmentWithLLM(ctx context.Context, client askprovider.Client, cfg askconfig.EffectiveSettings, retrieval askretrieve.RetrievalResult, prompt string, route askintent.Route, result *runResult) {
	resp, err := client.Generate(ctx, askprovider.Request{
		Provider:     cfg.Provider,
		Model:        cfg.Model,
		APIKey:       cfg.APIKey,
		Endpoint:     cfg.Endpoint,
		SystemPrompt: infoSystemPrompt(route, retrieval),
		Prompt:       infoUserPrompt(prompt, route),
		MaxRetries:   1,
	})
	if err != nil {
		result.ReviewLines = append(result.ReviewLines, "LLM augmentation skipped: "+err.Error())
		return
	}
	info := askcontract.ParseInfo(resp.Content)
	result.LLMUsed = true
	if strings.TrimSpace(info.Summary) != "" {
		result.Summary = info.Summary
	}
	if strings.TrimSpace(info.Answer) != "" {
		result.Answer = info.Answer
	}
	for _, line := range info.Suggestions {
		if strings.TrimSpace(line) != "" {
			result.ReviewLines = append(result.ReviewLines, line)
		}
	}
	for _, line := range info.Findings {
		if strings.TrimSpace(line) != "" {
			result.ReviewLines = append(result.ReviewLines, line)
		}
	}
	for _, line := range info.SuggestedChange {
		if strings.TrimSpace(line) != "" {
			result.ReviewLines = append(result.ReviewLines, line)
		}
	}
}

func generateWithValidation(ctx context.Context, client askprovider.Client, req askprovider.Request, root string, attempts int) (askcontract.GenerationResponse, string, int, error) {
	var lastValidation string
	for attempt := 1; attempt <= attempts; attempt++ {
		currentPrompt := req.Prompt
		if attempt > 1 && lastValidation != "" {
			currentPrompt += "\n\nLocal validation failed. Fix the response and return full JSON again. Errors:\n" + lastValidation
		}
		resp, err := client.Generate(ctx, askprovider.Request{
			Provider:     req.Provider,
			Model:        req.Model,
			APIKey:       req.APIKey,
			Endpoint:     req.Endpoint,
			SystemPrompt: req.SystemPrompt,
			Prompt:       currentPrompt,
			MaxRetries:   1,
		})
		if err != nil {
			return askcontract.GenerationResponse{}, lastValidation, attempt - 1, err
		}
		gen, err := askcontract.ParseGeneration(resp.Content)
		if err != nil {
			lastValidation = err.Error()
			continue
		}
		lintSummary, err := validateGeneration(root, gen)
		if err == nil {
			return gen, lintSummary, attempt - 1, nil
		}
		lastValidation = err.Error()
	}
	if lastValidation == "" {
		lastValidation = "generation failed without a parseable response"
	}
	return askcontract.GenerationResponse{}, lastValidation, attempts - 1, fmt.Errorf("ask generation did not validate after %d attempts: %s", attempts, lastValidation)
}

func validateGeneration(root string, gen askcontract.GenerationResponse) (string, error) {
	if len(gen.Files) == 0 {
		return "", fmt.Errorf("response did not include any files")
	}
	staged, err := stageWorkspace(root, gen.Files)
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(staged) }()
	paths := make([]string, 0, len(gen.Files))
	for _, file := range gen.Files {
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
	validated = dedupe(validated)
	return fmt.Sprintf("lint ok (%d workflows)", len(validated)), nil
}

func generationSystemPrompt(route askintent.Route, retrieval askretrieve.RetrievalResult) string {
	b := &strings.Builder{}
	b.WriteString("You are deck ask, a workflow authoring assistant.\n")
	b.WriteString("Route: ")
	b.WriteString(string(route))
	b.WriteString("\n")
	b.WriteString("Rules:\n")
	b.WriteString("- Produce only strict JSON.\n")
	b.WriteString("- JSON shape: {\"summary\":string,\"review\":[]string,\"files\":[{\"path\":string,\"content\":string}]}.\n")
	b.WriteString("- Allowed paths: workflows/scenarios/*.yaml, workflows/components/*.yaml, workflows/vars.yaml.\n")
	b.WriteString("- Prefer typed steps over Command.\n")
	b.WriteString("- Do not invent unsupported fields.\n")
	b.WriteString("Retrieved context follows.\n")
	b.WriteString(askretrieve.BuildChunkText(retrieval))
	return b.String()
}

func generationUserPrompt(workspace askretrieve.WorkspaceSummary, state askstate.Context, prompt string, fromLabel string, route askintent.Route) string {
	b := &strings.Builder{}
	b.WriteString("Workspace root: ")
	b.WriteString(workspace.Root)
	b.WriteString("\n")
	_, _ = fmt.Fprintf(b, "Has workflow tree: %t\n", workspace.HasWorkflowTree)
	_, _ = fmt.Fprintf(b, "Has prepare scenario: %t\n", workspace.HasPrepare)
	_, _ = fmt.Fprintf(b, "Has apply scenario: %t\n", workspace.HasApply)
	b.WriteString("Route: ")
	b.WriteString(string(route))
	b.WriteString("\n")
	if state.LastLint != "" {
		b.WriteString("Last lint summary: ")
		b.WriteString(state.LastLint)
		b.WriteString("\n")
	}
	if fromLabel != "" {
		b.WriteString("Attached request source: ")
		b.WriteString(fromLabel)
		b.WriteString("\n")
	}
	b.WriteString("User request:\n")
	b.WriteString(strings.TrimSpace(prompt))
	b.WriteString("\n")
	b.WriteString("Return the minimum complete file set needed for this request.\n")
	return b.String()
}

func infoSystemPrompt(route askintent.Route, retrieval askretrieve.RetrievalResult) string {
	b := &strings.Builder{}
	b.WriteString("You are deck ask.\n")
	b.WriteString("Route: ")
	b.WriteString(string(route))
	b.WriteString("\n")
	b.WriteString("Return strict JSON with shape {\"summary\":string,\"answer\":string,\"suggestions\":[]string}.\n")
	b.WriteString("Do not return file content for this route.\n")
	b.WriteString(askretrieve.BuildChunkText(retrieval))
	return b.String()
}

func infoUserPrompt(prompt string, route askintent.Route) string {
	return "Route: " + string(route) + "\nUser request:\n" + strings.TrimSpace(prompt)
}

func localClarify(prompt string) string {
	if strings.TrimSpace(prompt) == "" {
		return "Your request is empty. Please describe what you want to do."
	}
	return "Your request is too ambiguous to start workflow generation."
}

func clarificationSuggestions() []string {
	return []string{
		"Try: deck ask \"rhel9 single-node kubeadm cluster scenario\"",
		"Try: deck ask --review",
		"Try: deck ask \"explain what workflows/scenarios/apply.yaml does\"",
	}
}

func localExplain(workspace askretrieve.WorkspaceSummary, prompt string) (string, string) {
	b := &strings.Builder{}
	b.WriteString("Workspace summary:\n")
	_, _ = fmt.Fprintf(b, "- workflow tree: %t\n", workspace.HasWorkflowTree)
	_, _ = fmt.Fprintf(b, "- prepare scenario: %t\n", workspace.HasPrepare)
	_, _ = fmt.Fprintf(b, "- apply scenario: %t\n", workspace.HasApply)
	_, _ = fmt.Fprintf(b, "- relevant files: %d\n", len(workspace.Files))
	if strings.TrimSpace(prompt) != "" {
		b.WriteString("Prompt interpreted as explain request for current workspace.\n")
	}
	return "Workspace explanation", strings.TrimSpace(b.String())
}

func findingsToLines(findings []askreview.Finding) []string {
	if len(findings) == 0 {
		return []string{"No local style findings detected."}
	}
	out := make([]string, 0, len(findings))
	for _, finding := range findings {
		out = append(out, fmt.Sprintf("[%s] %s", finding.Severity, finding.Message))
	}
	return out
}

func resultToMarkdown(result runResult) string {
	b := &strings.Builder{}
	b.WriteString("# ask result\n\n")
	b.WriteString("- route: ")
	b.WriteString(string(result.Route))
	b.WriteString("\n")
	_, _ = fmt.Fprintf(b, "- confidence: %.2f\n", result.Confidence)
	b.WriteString("- reason: ")
	b.WriteString(result.Reason)
	b.WriteString("\n")
	b.WriteString("- termination: ")
	b.WriteString(result.Termination)
	b.WriteString("\n")
	b.WriteString("\n")
	b.WriteString(result.Answer)
	b.WriteString("\n")
	return b.String()
}

func render(stdout io.Writer, stderr io.Writer, result runResult) error {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if _, err := fmt.Fprintf(stdout, "ask: %s\n", result.Summary); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "route: %s (confidence %.2f)\n", result.Route, result.Confidence); err != nil {
		return err
	}
	if result.Answer != "" {
		if _, err := fmt.Fprintf(stdout, "answer: %s\n", result.Answer); err != nil {
			return err
		}
	}
	if result.LintSummary != "" {
		if _, err := fmt.Fprintf(stdout, "lint: %s\n", result.LintSummary); err != nil {
			return err
		}
	}
	if len(result.ReviewLines) > 0 {
		if _, err := io.WriteString(stdout, "notes:\n"); err != nil {
			return err
		}
		for _, line := range result.ReviewLines {
			if _, err := fmt.Fprintf(stdout, "- %s\n", line); err != nil {
				return err
			}
		}
	}
	if len(result.LocalFindings) > 0 {
		if _, err := io.WriteString(stdout, "local-findings:\n"); err != nil {
			return err
		}
		for _, finding := range result.LocalFindings {
			if _, err := fmt.Fprintf(stdout, "- [%s] %s\n", finding.Severity, finding.Message); err != nil {
				return err
			}
		}
	}
	if len(result.Files) > 0 {
		label := "preview"
		if result.WroteFiles {
			label = "wrote"
		}
		if _, err := fmt.Fprintf(stdout, "%s:\n", label); err != nil {
			return err
		}
		for _, file := range result.Files {
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
	if result.WroteFiles {
		if _, err := io.WriteString(stdout, "ask write: ok\n"); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(stderr, "deck ask route=%s reason=%s llmUsed=%t retries=%d termination=%s\n", result.Route, result.Reason, result.LLMUsed, result.RetriesUsed, result.Termination); err != nil {
		return err
	}
	if result.ConfigSource.APIKeySource != "unset" {
		if _, err := fmt.Fprintf(stderr, "deck ask using provider=%s model=%s endpoint=%s apiKeySource=%s\n", result.ConfigSource.Provider, result.ConfigSource.Model, result.ConfigSource.Endpoint, result.ConfigSource.APIKeySource); err != nil {
			return err
		}
	}
	return nil
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

func validateGeneratedFile(root string, file askcontract.GeneratedFile) error {
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

func writeFiles(root string, files []askcontract.GeneratedFile) error {
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

func stageWorkspace(root string, files []askcontract.GeneratedFile) (string, error) {
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

func localFindings(files []askcontract.GeneratedFile) []askreview.Finding {
	content := make(map[string]string, len(files))
	for _, file := range files {
		content[file.Path] = file.Content
	}
	return askreview.Candidate(content)
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
		raw, err := os.ReadFile(path) //nolint:gosec // Paths come from walking selected workspace tree.
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}
		return os.WriteFile(target, raw, 0o600) //nolint:gosec // Target stays inside staging root.
	})
}

func loadRequestText(prompt string, fromPath string) (string, error) {
	prompt = strings.TrimSpace(prompt)
	fromPath = strings.TrimSpace(fromPath)
	if fromPath == "" {
		return prompt, nil
	}
	raw, err := os.ReadFile(fromPath) //nolint:gosec // User-selected request file path.
	if err != nil {
		return "", fmt.Errorf("read ask request file: %w", err)
	}
	fromText := strings.TrimSpace(string(raw))
	if prompt == "" {
		return fromText, nil
	}
	return prompt + "\n\nAttached request details:\n" + fromText, nil
}

func isVarsPath(path string) bool {
	return filepath.ToSlash(strings.TrimSpace(path)) == "workflows/vars.yaml"
}

func filePaths(files []askcontract.GeneratedFile) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
	}
	sort.Strings(paths)
	return paths
}

func chunkIDs(chunks []askretrieve.Chunk) []string {
	ids := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		ids = append(ids, chunk.ID)
	}
	return ids
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
