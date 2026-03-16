package askretrieve

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/taedi90/deck/internal/askintent"
	"github.com/taedi90/deck/internal/askstate"
	"github.com/taedi90/deck/internal/schemadoc"
	"github.com/taedi90/deck/internal/workspacepaths"
)

type WorkspaceSummary struct {
	Root            string
	HasWorkflowTree bool
	HasPrepare      bool
	HasApply        bool
	Files           []WorkspaceFile
}

type WorkspaceFile struct {
	Path    string
	Content string
}

type Chunk struct {
	ID      string
	Source  string
	Label   string
	Content string
	Score   int
}

type RetrievalResult struct {
	Chunks   []Chunk
	Dropped  []string
	MaxBytes int
}

func InspectWorkspace(root string) (WorkspaceSummary, error) {
	resolvedRoot, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return WorkspaceSummary{}, fmt.Errorf("resolve workspace root: %w", err)
	}
	workflowRoot := filepath.Join(resolvedRoot, workspacepaths.WorkflowRootDir)
	preparePath := workspacepaths.CanonicalPrepareWorkflowPath(resolvedRoot)
	applyPath := workspacepaths.CanonicalApplyWorkflowPath(resolvedRoot)
	varsPath := workspacepaths.CanonicalVarsPath(resolvedRoot)
	out := WorkspaceSummary{Root: resolvedRoot}
	if info, err := os.Stat(workflowRoot); err == nil && info.IsDir() {
		out.HasWorkflowTree = true
	}
	out.HasPrepare = isFile(preparePath)
	out.HasApply = isFile(applyPath)

	for _, path := range []string{varsPath, preparePath, applyPath} {
		if !isFile(path) {
			continue
		}
		content, err := os.ReadFile(path) //nolint:gosec // Workspace-derived files only.
		if err != nil {
			return WorkspaceSummary{}, fmt.Errorf("read workspace file %s: %w", path, err)
		}
		out.Files = append(out.Files, WorkspaceFile{Path: toRel(resolvedRoot, path), Content: string(content)})
	}
	componentDir := filepath.Join(workflowRoot, workspacepaths.WorkflowComponentsDir)
	entries, err := os.ReadDir(componentDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			lower := strings.ToLower(name)
			if !strings.HasSuffix(lower, ".yaml") && !strings.HasSuffix(lower, ".yml") {
				continue
			}
			path := filepath.Join(componentDir, name)
			content, readErr := os.ReadFile(path) //nolint:gosec // Workspace-derived files only.
			if readErr != nil {
				return WorkspaceSummary{}, fmt.Errorf("read component file %s: %w", path, readErr)
			}
			out.Files = append(out.Files, WorkspaceFile{Path: toRel(resolvedRoot, path), Content: string(content)})
		}
	}
	sort.Slice(out.Files, func(i, j int) bool {
		return out.Files[i].Path < out.Files[j].Path
	})
	return out, nil
}

func Retrieve(route askintent.Route, prompt string, workspace WorkspaceSummary, state askstate.Context) RetrievalResult {
	budget, maxChunks := routeBudget(route)
	lowerPrompt := strings.ToLower(strings.TrimSpace(prompt))
	chunks := make([]Chunk, 0, 32)
	workflowMeta := schemadoc.WorkflowMeta()
	chunks = append(chunks, Chunk{
		ID:      "workflow-meta",
		Source:  "schemadoc",
		Label:   "workflow-summary",
		Content: workflowMeta.Summary + "\n" + strings.Join(workflowMeta.Notes, "\n"),
		Score:   50,
	})
	chunks = append(chunks, Chunk{
		ID:      "philosophy",
		Source:  "built-in",
		Label:   "authoring-rules",
		Content: "Prefer typed steps over Command. Keep workflows explicit and reviewable. Use workflows/components imports for reusable blocks. Do not invent unsupported fields.",
		Score:   45,
	})
	for _, kind := range schemadoc.ToolKinds() {
		meta := schemadoc.ToolMeta(kind)
		score := 10
		if strings.Contains(lowerPrompt, strings.ToLower(kind)) {
			score += 80
		}
		if strings.Contains(lowerPrompt, strings.ToLower(meta.Category)) {
			score += 5
		}
		if strings.Contains(lowerPrompt, strings.ToLower(meta.Summary)) {
			score += 8
		}
		chunks = append(chunks, Chunk{
			ID:      "tool-meta-" + strings.ToLower(kind),
			Source:  "schemadoc",
			Label:   kind,
			Content: meta.Kind + ": " + meta.Summary + "\nWhen to use: " + meta.WhenToUse,
			Score:   score,
		})
	}
	for _, file := range workspace.Files {
		if !workspaceFileAllowed(file.Path) {
			continue
		}
		score := 30
		if strings.Contains(lowerPrompt, strings.ToLower(filepath.Base(file.Path))) {
			score += 30
		}
		if strings.Contains(file.Path, "workflows/scenarios/") {
			score += 10
		}
		chunks = append(chunks, Chunk{
			ID:      "workspace-" + strings.ReplaceAll(file.Path, "/", "_"),
			Source:  "workspace",
			Label:   file.Path,
			Content: file.Content,
			Score:   score,
		})
	}
	if state.LastLint != "" {
		chunks = append(chunks, Chunk{
			ID:      "state-last-lint",
			Source:  "state",
			Label:   "last-lint",
			Content: state.LastLint,
			Score:   20,
		})
	}

	sort.Slice(chunks, func(i, j int) bool {
		if chunks[i].Score == chunks[j].Score {
			return chunks[i].ID < chunks[j].ID
		}
		return chunks[i].Score > chunks[j].Score
	})

	selected := make([]Chunk, 0, maxChunks)
	dropped := make([]string, 0)
	remaining := budget
	for _, chunk := range chunks {
		if len(selected) >= maxChunks {
			dropped = append(dropped, chunk.ID)
			continue
		}
		size := len(chunk.Content)
		if size > remaining {
			dropped = append(dropped, chunk.ID)
			continue
		}
		selected = append(selected, chunk)
		remaining -= size
	}

	return RetrievalResult{Chunks: selected, Dropped: dropped, MaxBytes: budget}
}

func BuildChunkText(retrieval RetrievalResult) string {
	b := &strings.Builder{}
	for _, chunk := range retrieval.Chunks {
		b.WriteString("[chunk:")
		b.WriteString(chunk.ID)
		b.WriteString(",source:")
		b.WriteString(chunk.Source)
		b.WriteString(",label:")
		b.WriteString(chunk.Label)
		b.WriteString("]\n")
		b.WriteString(chunk.Content)
		if !strings.HasSuffix(chunk.Content, "\n") {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func routeBudget(route askintent.Route) (maxBytes int, maxChunks int) {
	switch route {
	case askintent.RouteDraft, askintent.RouteRefine:
		return 12000, 10
	case askintent.RouteReview, askintent.RouteExplain:
		return 8000, 8
	default:
		return 4000, 6
	}
}

func workspaceFileAllowed(path string) bool {
	clean := filepath.ToSlash(strings.ToLower(strings.TrimSpace(path)))
	if clean == "" {
		return false
	}
	if strings.Contains(clean, "..") {
		return false
	}
	if strings.HasSuffix(clean, ".env") || strings.Contains(clean, "/.env") {
		return false
	}
	if strings.HasPrefix(clean, "outputs/") || strings.HasPrefix(clean, ".git/") || strings.HasPrefix(clean, "bin/") || strings.HasPrefix(clean, "test/artifacts/") {
		return false
	}
	if strings.HasPrefix(clean, "workflows/scenarios/") || strings.HasPrefix(clean, "workflows/components/") || clean == "workflows/vars.yaml" {
		return true
	}
	return false
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func toRel(root string, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}
