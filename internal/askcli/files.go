package askcli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/taedi90/deck/internal/askcontext"
	"github.com/taedi90/deck/internal/askcontract"
	"github.com/taedi90/deck/internal/askintent"
	"github.com/taedi90/deck/internal/askretrieve"
	"github.com/taedi90/deck/internal/askreview"
	"github.com/taedi90/deck/internal/fsutil"
	"github.com/taedi90/deck/internal/validate"
	"github.com/taedi90/deck/internal/workspacepaths"
)

const maxPlanSlugLength = 48

func validateGeneratedPath(path string) error { /* split helper below */
	clean := filepath.ToSlash(strings.TrimSpace(path))
	if clean == "" {
		return fmt.Errorf("generated file path is empty")
	}
	if !askcontext.AllowedGeneratedPath(clean) {
		return fmt.Errorf("generated file path is not allowed: %s", clean)
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
		raw, err := os.ReadFile(path) //nolint:gosec
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}
		return os.WriteFile(target, raw, 0o600) //nolint:gosec
	})
}

func loadRequestText(root string, prompt string, fromPath string) (string, string, error) {
	prompt = strings.TrimSpace(prompt)
	fromPath = strings.TrimSpace(fromPath)
	if fromPath == "" {
		return prompt, "", nil
	}
	candidate := fromPath
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return "", "", fmt.Errorf("resolve ask request file: %w", err)
	}
	resolved, err := fsutil.ResolveUnder(root, strings.Split(filepath.ToSlash(rel), "/")...)
	if err != nil {
		return "", "", fmt.Errorf("resolve ask request file: %w", err)
	}
	raw, err := os.ReadFile(resolved) //nolint:gosec
	if err != nil {
		return "", "", fmt.Errorf("read ask request file: %w", err)
	}
	fromText := strings.TrimSpace(string(raw))
	source := "file"
	if strings.HasPrefix(filepath.ToSlash(rel), ".deck/plan/") && strings.HasSuffix(strings.ToLower(resolved), ".md") {
		jsonPath := strings.TrimSuffix(resolved, filepath.Ext(resolved)) + ".json"
		jsonRaw, jsonErr := os.ReadFile(jsonPath) //nolint:gosec
		if jsonErr == nil {
			var plan askcontract.PlanResponse
			if err := json.Unmarshal(jsonRaw, &plan); err == nil {
				fromText = buildPlanSourceText(plan)
				source = "plan-json"
			}
		} else {
			source = "plan-markdown"
		}
	}
	if prompt == "" {
		return fromText, source, nil
	}
	return prompt + "\n\nAttached request details:\n" + fromText, source, nil
}

func buildPlanSourceText(plan askcontract.PlanResponse) string {
	b := &strings.Builder{}
	b.WriteString("Plan request:\n")
	b.WriteString(strings.TrimSpace(plan.Request))
	b.WriteString("\nIntent: ")
	b.WriteString(strings.TrimSpace(plan.Intent))
	b.WriteString("\nTarget outcome: ")
	b.WriteString(strings.TrimSpace(plan.TargetOutcome))
	b.WriteString("\nPlanned files:\n")
	for _, file := range plan.Files {
		b.WriteString("- ")
		b.WriteString(file.Path)
		if strings.TrimSpace(file.Action) != "" {
			b.WriteString(" (")
			b.WriteString(strings.TrimSpace(file.Action))
			b.WriteString(")")
		}
		b.WriteString("\n")
	}
	if len(plan.ValidationChecklist) > 0 {
		b.WriteString("Validation checklist:\n")
		for _, line := range plan.ValidationChecklist {
			b.WriteString("- ")
			b.WriteString(strings.TrimSpace(line))
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func resolvePlanDir(root string, requested string) (string, error) {
	if strings.TrimSpace(requested) == "" {
		return filepath.Join(root, ".deck", "plan"), nil
	}
	candidate := requested
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return "", fmt.Errorf("resolve plan directory: %w", err)
	}
	resolved, err := fsutil.ResolveUnder(root, strings.Split(filepath.ToSlash(rel), "/")...)
	if err != nil {
		return "", fmt.Errorf("resolve plan directory: %w", err)
	}
	return resolved, nil
}

func planSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "plan"
	}
	var out strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			out.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			out.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && out.Len() > 0 {
				out.WriteRune('-')
				lastDash = true
			}
		}
	}
	slug := strings.Trim(out.String(), "-")
	if slug == "" {
		return "plan"
	}
	if len(slug) > maxPlanSlugLength {
		slug = strings.Trim(slug[:maxPlanSlugLength], "-")
	}
	if slug == "" {
		return "plan"
	}
	return slug
}

func savePlanArtifact(root string, opts Options, plan askcontract.PlanResponse, markdown string) (string, string, error) {
	planDir, err := resolvePlanDir(root, opts.PlanDir)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(planDir, 0o750); err != nil {
		return "", "", fmt.Errorf("create plan directory: %w", err)
	}
	name := strings.TrimSpace(opts.PlanName)
	if name == "" {
		name = plan.Request
	}
	timestamp := time.Now().UTC().Format("2006-01-02-150405")
	base := timestamp + "-" + planSlug(name)
	mdPath := filepath.Join(planDir, base+".md")
	jsonPath := filepath.Join(planDir, base+".json")
	jsonRaw, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return "", "", fmt.Errorf("marshal plan json: %w", err)
	}
	if err := os.WriteFile(mdPath, []byte(markdown+"\n"), 0o600); err != nil {
		return "", "", fmt.Errorf("write plan markdown: %w", err)
	}
	if err := os.WriteFile(jsonPath, append(jsonRaw, '\n'), 0o600); err != nil {
		return "", "", fmt.Errorf("write plan json: %w", err)
	}
	if err := os.WriteFile(filepath.Join(planDir, "latest.md"), []byte(markdown+"\n"), 0o600); err != nil {
		return "", "", fmt.Errorf("write latest plan markdown: %w", err)
	}
	if err := os.WriteFile(filepath.Join(planDir, "latest.json"), append(jsonRaw, '\n'), 0o600); err != nil {
		return "", "", fmt.Errorf("write latest plan json: %w", err)
	}
	relMD, err := filepath.Rel(root, mdPath)
	if err != nil {
		relMD = mdPath
	}
	relJSON, err := filepath.Rel(root, jsonPath)
	if err != nil {
		relJSON = jsonPath
	}
	return filepath.ToSlash(relMD), filepath.ToSlash(relJSON), nil
}

func renderUserCommand(opts Options) string {
	parts := []string{"deck", "ask"}
	if opts.PlanOnly {
		parts = append(parts, "plan")
	}
	if opts.Write {
		parts = append(parts, "--write")
	}
	if opts.Review {
		parts = append(parts, "--review")
	}
	if opts.MaxIterations > 0 {
		parts = append(parts, "--max-iterations", fmt.Sprintf("%d", opts.MaxIterations))
	}
	if strings.TrimSpace(opts.FromPath) != "" {
		parts = append(parts, "--from", strings.TrimSpace(opts.FromPath))
	}
	if strings.TrimSpace(opts.PlanName) != "" {
		parts = append(parts, "--plan-name", strings.TrimSpace(opts.PlanName))
	}
	if strings.TrimSpace(opts.PlanDir) != "" {
		parts = append(parts, "--plan-dir", strings.TrimSpace(opts.PlanDir))
	}
	if strings.TrimSpace(opts.Provider) != "" {
		parts = append(parts, "--provider", strings.TrimSpace(opts.Provider))
	}
	if strings.TrimSpace(opts.Model) != "" {
		parts = append(parts, "--model", strings.TrimSpace(opts.Model))
	}
	if strings.TrimSpace(opts.Endpoint) != "" {
		parts = append(parts, "--endpoint", strings.TrimSpace(opts.Endpoint))
	}
	if strings.TrimSpace(opts.Prompt) != "" {
		parts = append(parts, strconv.Quote(strings.TrimSpace(opts.Prompt)))
	}
	return strings.Join(parts, " ")
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

func filePathsFromPlan(plan askcontract.PlanResponse) []string {
	paths := make([]string, 0, len(plan.Files))
	for _, file := range plan.Files {
		if strings.TrimSpace(file.Path) == "" {
			continue
		}
		paths = append(paths, strings.TrimSpace(file.Path))
	}
	sort.Strings(paths)
	return paths
}

func validateSemanticGeneration(gen askcontract.GenerationResponse, decision askintent.Decision, plan askcontract.PlanResponse) error {
	critic := semanticCritic(gen, decision, plan)
	if len(critic.Blocking) > 0 {
		return fmt.Errorf("semantic validation failed: %s", strings.Join(critic.Blocking, "; "))
	}
	return nil
}

func semanticCritic(gen askcontract.GenerationResponse, decision askintent.Decision, plan askcontract.PlanResponse) askcontract.CriticResponse {
	critic := askcontract.CriticResponse{}
	generated := map[string]askcontract.GeneratedFile{}
	for _, file := range gen.Files {
		generated[filepath.ToSlash(strings.TrimSpace(file.Path))] = file
	}
	for _, file := range gen.Files {
		if err := validateGeneratedPath(file.Path); err != nil {
			critic.Blocking = append(critic.Blocking, err.Error())
			critic.RequiredFixes = append(critic.RequiredFixes, "Keep generated files under allowed workflow paths only")
		}
		if strings.HasPrefix(filepath.ToSlash(strings.TrimSpace(file.Path)), "workflows/scenarios/") {
			for _, importPath := range localImportPaths(file.Content) {
				resolved := filepath.ToSlash(filepath.Join("workflows/components", importPath))
				if _, ok := generated[resolved]; !ok {
					critic.Blocking = append(critic.Blocking, fmt.Sprintf("scenario imports missing component: %s -> %s", file.Path, resolved))
					critic.InvalidImports = append(critic.InvalidImports, resolved)
				}
			}
		}
	}
	if len(plan.Files) > 0 {
		planned := map[string]string{}
		for _, file := range plan.Files {
			planned[filepath.ToSlash(strings.TrimSpace(file.Path))] = strings.ToLower(strings.TrimSpace(file.Action))
		}
		for path := range planned {
			if _, ok := generated[path]; !ok {
				critic.Blocking = append(critic.Blocking, fmt.Sprintf("planned file missing from generation: %s", path))
				critic.MissingFiles = append(critic.MissingFiles, path)
			}
		}
		checklistText := strings.ToLower(strings.Join(plan.ValidationChecklist, "\n"))
		if strings.Contains(checklistText, "vars") {
			if _, ok := generated["workflows/vars.yaml"]; !ok {
				critic.Blocking = append(critic.Blocking, "validation checklist requires vars but workflows/vars.yaml was not generated")
				critic.CoverageGaps = append(critic.CoverageGaps, "validation checklist references vars but workflows/vars.yaml was not generated")
			}
		}
		if decision.Route == askintent.RouteRefine {
			for _, file := range gen.Files {
				clean := filepath.ToSlash(strings.TrimSpace(file.Path))
				action, ok := planned[clean]
				if !ok {
					critic.Blocking = append(critic.Blocking, fmt.Sprintf("refine generated unplanned file: %s", clean))
					critic.RequiredFixes = append(critic.RequiredFixes, "Only update or create files declared in the plan during refine")
				}
				if action != "" && action != "update" && action != "create" {
					critic.Blocking = append(critic.Blocking, fmt.Sprintf("invalid planned action for %s", clean))
				}
				if action == "update" && strings.HasPrefix(clean, "workflows/scenarios/") && strings.Contains(strings.ToLower(clean), "apply") {
					critic.Advisory = append(critic.Advisory, fmt.Sprintf("refine updates existing entry scenario: %s", clean))
				}
			}
		}
	}
	if entry := filepath.ToSlash(strings.TrimSpace(plan.EntryScenario)); entry != "" {
		if _, ok := generated[entry]; !ok {
			critic.Blocking = append(critic.Blocking, fmt.Sprintf("planned entry scenario missing from generation: %s", entry))
			critic.MissingFiles = append(critic.MissingFiles, entry)
		}
	}
	generatedScenarioRefs := map[string]bool{}
	for _, file := range gen.Files {
		for _, importPath := range localImportPaths(file.Content) {
			generatedScenarioRefs[filepath.ToSlash(filepath.Join("workflows/components", importPath))] = true
		}
	}
	for _, file := range gen.Files {
		clean := filepath.ToSlash(strings.TrimSpace(file.Path))
		if strings.HasPrefix(clean, "workflows/components/") && !generatedScenarioRefs[clean] {
			critic.Advisory = append(critic.Advisory, fmt.Sprintf("generated component has no scenario import: %s", clean))
		}
	}
	critic.Blocking = dedupe(critic.Blocking)
	critic.Advisory = dedupe(critic.Advisory)
	critic.MissingFiles = dedupe(critic.MissingFiles)
	critic.InvalidImports = dedupe(critic.InvalidImports)
	critic.CoverageGaps = dedupe(critic.CoverageGaps)
	critic.RequiredFixes = dedupe(critic.RequiredFixes)
	return critic
}

func criticJSON(critic askcontract.CriticResponse) string {
	raw, err := json.MarshalIndent(critic, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(raw)
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
