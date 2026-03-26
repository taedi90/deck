package askretrieve

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/askcontext"
	"github.com/Airgap-Castaways/deck/internal/askintent"
	"github.com/Airgap-Castaways/deck/internal/askknowledge"
	"github.com/Airgap-Castaways/deck/internal/askstate"
	"github.com/Airgap-Castaways/deck/internal/workspacepaths"
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
	ID       string
	Source   string
	Label    string
	Topic    askcontext.Topic
	Content  string
	Score    int
	Evidence *EvidenceSummary
}

type EvidenceSummary struct {
	ArtifactKinds []string
	InstallHints  []string
	OfflineHints  []string
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
	for _, dir := range []string{
		filepath.Join(workflowRoot, workspacepaths.WorkflowScenariosDir),
		filepath.Join(workflowRoot, workspacepaths.WorkflowComponentsDir),
	} {
		if err := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			lower := strings.ToLower(d.Name())
			if !strings.HasSuffix(lower, ".yaml") && !strings.HasSuffix(lower, ".yml") {
				return nil
			}
			content, readErr := os.ReadFile(path) //nolint:gosec // Workspace-derived files only.
			if readErr != nil {
				return fmt.Errorf("read workspace file %s: %w", path, readErr)
			}
			rel := toRel(resolvedRoot, path)
			if !containsWorkspacePath(out.Files, rel) {
				out.Files = append(out.Files, WorkspaceFile{Path: rel, Content: string(content)})
			}
			return nil
		}); err != nil && !os.IsNotExist(err) {
			return WorkspaceSummary{}, fmt.Errorf("walk workflow directory %s: %w", dir, err)
		}
	}
	sort.Slice(out.Files, func(i, j int) bool {
		return out.Files[i].Path < out.Files[j].Path
	})
	return out, nil
}

func Retrieve(route askintent.Route, prompt string, target askintent.Target, workspace WorkspaceSummary, state askstate.Context, external []Chunk) RetrievalResult {
	budget, maxChunks := routeBudget(route)
	lowerPrompt := strings.ToLower(strings.TrimSpace(prompt))
	related := relatedWorkspaceTargets(workspace, target)
	chunks := make([]Chunk, 0, 32)
	bundle := askknowledge.Current()
	chunks = append(chunks, Chunk{
		ID:      "workflow-meta",
		Source:  "askcontext",
		Label:   "workflow-summary",
		Topic:   askcontext.TopicWorkflowInvariants,
		Content: bundle.WorkflowPromptBlock(),
		Score:   50,
	})
	chunks = append(chunks, Chunk{
		ID:      "philosophy",
		Source:  "askcontext",
		Label:   "authoring-rules",
		Topic:   askcontext.TopicPolicy,
		Content: bundle.PolicyPromptBlock(),
		Score:   45,
	})
	chunks = append(chunks,
		Chunk{ID: "topology", Source: "askcontext", Label: "workspace-topology", Topic: askcontext.TopicWorkspaceTopology, Content: askcontext.WorkspaceTopologyBlock(), Score: 52},
		Chunk{ID: "role-guidance", Source: "askcontext", Label: "prepare-apply-guidance", Topic: askcontext.TopicPrepareApplyGuidance, Content: askcontext.RoleGuidanceBlock(), Score: roleGuidanceScore(prompt)},
		Chunk{ID: "component-guidance", Source: "askcontext", Label: "components-imports", Topic: askcontext.TopicComponentsImports, Content: bundle.ComponentPromptBlock(), Score: 52},
		Chunk{ID: "vars-guidance", Source: "askcontext", Label: "vars-guidance", Topic: askcontext.TopicVarsGuidance, Content: bundle.VarsPromptBlock(), Score: 52},
		Chunk{ID: "cli-guidance", Source: "askcontext", Label: "cli-hints", Topic: askcontext.TopicCLIHints, Content: askcontext.CLIHintsBlock(), Score: 25},
	)
	if typedSteps := askcontext.StepGuidanceBlock(route, prompt); strings.TrimSpace(typedSteps) != "" {
		chunks = append(chunks, Chunk{
			ID:      "typed-steps-" + string(route),
			Source:  "askcontext",
			Label:   "typed-steps",
			Topic:   askcontext.TopicTypedSteps,
			Content: typedSteps,
			Score:   typedStepsScore(route, lowerPrompt),
		})
	}
	for _, file := range workspace.Files {
		if !workspaceFileAllowed(file.Path) {
			continue
		}
		score := 30
		if target.Path != "" && filepath.ToSlash(file.Path) == filepath.ToSlash(target.Path) {
			score += 50
		}
		if related[filepath.ToSlash(file.Path)] {
			score += 35
		}
		if target.Name != "" && strings.Contains(strings.ToLower(filepath.Base(file.Path)), strings.ToLower(target.Name)) {
			score += 25
		}
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
			Topic:   askcontext.Topic("workspace:" + filepath.ToSlash(file.Path)),
			Content: file.Content,
			Score:   score,
		})
	}
	if state.LastLint != "" {
		chunks = append(chunks, Chunk{
			ID:      "state-last-lint",
			Source:  "state",
			Label:   "last-lint",
			Topic:   askcontext.Topic("state:last-lint"),
			Content: state.LastLint,
			Score:   20,
		})
	}
	for _, chunk := range external {
		if strings.TrimSpace(chunk.Content) == "" {
			continue
		}
		chunks = append(chunks, chunk)
	}

	chunks = dedupeChunksByTopic(chunks)
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

func RepairChunks(prompt string, validationError string) []Chunk {
	content := askcontext.RepairGuidanceBlock(prompt, validationError)
	if strings.TrimSpace(content) == "" {
		return nil
	}
	return []Chunk{{
		ID:      "step-repair",
		Source:  "askcontext",
		Label:   "step-repair",
		Topic:   askcontext.TopicStepRepair,
		Content: content,
		Score:   80,
	}}
}

func dedupeChunksByTopic(chunks []Chunk) []Chunk {
	best := make(map[askcontext.Topic]Chunk, len(chunks))
	ordered := make([]askcontext.Topic, 0, len(chunks))
	for _, chunk := range chunks {
		topic := chunk.Topic
		if topic == "" {
			topic = askcontext.Topic("id:" + chunk.ID)
			chunk.Topic = topic
		}
		current, ok := best[topic]
		if !ok {
			best[topic] = chunk
			ordered = append(ordered, topic)
			continue
		}
		if chunk.Score > current.Score || (chunk.Score == current.Score && chunk.ID < current.ID) {
			best[topic] = chunk
		}
	}
	out := make([]Chunk, 0, len(best))
	for _, topic := range ordered {
		out = append(out, best[topic])
	}
	return out
}

func typedStepsScore(route askintent.Route, prompt string) int {
	score := 30
	if route == askintent.RouteDraft || route == askintent.RouteRefine {
		score += 15
	}
	if strings.Contains(prompt, "docker") || strings.Contains(prompt, "package") || strings.Contains(prompt, "install") || strings.Contains(prompt, "kubeadm") || strings.Contains(prompt, "air-gapped") {
		score += 30
	}
	return score
}

func roleGuidanceScore(prompt string) int {
	score := 40
	if strings.Contains(strings.ToLower(prompt), "prepare") || strings.Contains(strings.ToLower(prompt), "apply") {
		score += 20
	}
	return score
}

func containsWorkspacePath(files []WorkspaceFile, path string) bool {
	for _, file := range files {
		if filepath.ToSlash(file.Path) == filepath.ToSlash(path) {
			return true
		}
	}
	return false
}

func relatedWorkspaceTargets(workspace WorkspaceSummary, target askintent.Target) map[string]bool {
	if target.Path == "" {
		return nil
	}
	targetPath := filepath.ToSlash(target.Path)
	fileByPath := make(map[string]WorkspaceFile, len(workspace.Files))
	for _, file := range workspace.Files {
		fileByPath[filepath.ToSlash(file.Path)] = file
	}
	current, ok := fileByPath[targetPath]
	if !ok {
		return nil
	}
	related := map[string]bool{targetPath: true}
	if !strings.HasPrefix(targetPath, "workflows/scenarios/") {
		return related
	}
	for _, importPath := range importPaths(current.Content) {
		resolved := filepath.ToSlash(filepath.Join("workflows/components", importPath))
		if _, exists := fileByPath[resolved]; exists {
			related[resolved] = true
		}
	}
	return related
}

func importPaths(content string) []string {
	paths := make([]string, 0)
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- path:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, "- path:"))
		value = strings.Trim(value, `"'`)
		if value != "" {
			paths = append(paths, filepath.ToSlash(value))
		}
	}
	return paths
}

func BuildChunkText(retrieval RetrievalResult) string {
	return BuildChunkTextWithoutTopics(retrieval)
}

func BuildChunkTextWithoutTopics(retrieval RetrievalResult, excluded ...askcontext.Topic) string {
	excludedSet := map[askcontext.Topic]bool{}
	for _, topic := range excluded {
		excludedSet[topic] = true
	}
	b := &strings.Builder{}
	for _, chunk := range retrieval.Chunks {
		if excludedSet[chunk.Topic] {
			continue
		}
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
	if askcontext.AllowedGeneratedPath(clean) {
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
