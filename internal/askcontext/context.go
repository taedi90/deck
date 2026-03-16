package askcontext

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/taedi90/deck/internal/askstate"
	"github.com/taedi90/deck/internal/schemadoc"
	"github.com/taedi90/deck/internal/workspacepaths"
)

type Mode string

const (
	ModeDraft  Mode = "draft"
	ModeRefine Mode = "refine"
	ModeReview Mode = "review"
)

type BuildInput struct {
	Root      string
	Prompt    string
	Review    bool
	State     askstate.Context
	FromLabel string
}

type BuildResult struct {
	Mode         Mode
	SystemPrompt string
	Prompt       string
	Workspace    WorkspaceSummary
	TargetFiles  []string
}

type WorkspaceSummary struct {
	Root             string
	HasWorkflowTree  bool
	HasPrepare       bool
	HasApply         bool
	RelevantFiles    []ContextFile
	ComponentNames   []string
	SuggestedTargets []string
}

type ContextFile struct {
	Path    string
	Content string
}

func Build(input BuildInput) (BuildResult, error) {
	workspace, err := inspectWorkspace(input.Root)
	if err != nil {
		return BuildResult{}, err
	}
	mode := inferMode(input.Review, workspace)
	systemPrompt := buildSystemPrompt(workspace)
	userPrompt := buildUserPrompt(input, workspace, mode)
	targets := append([]string(nil), workspace.SuggestedTargets...)
	return BuildResult{
		Mode:         mode,
		SystemPrompt: systemPrompt,
		Prompt:       userPrompt,
		Workspace:    workspace,
		TargetFiles:  targets,
	}, nil
}

func inspectWorkspace(root string) (WorkspaceSummary, error) {
	resolvedRoot, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return WorkspaceSummary{}, fmt.Errorf("resolve workspace root: %w", err)
	}
	workflowRoot := filepath.Join(resolvedRoot, workspacepaths.WorkflowRootDir)
	preparePath := workspacepaths.CanonicalPrepareWorkflowPath(resolvedRoot)
	applyPath := workspacepaths.CanonicalApplyWorkflowPath(resolvedRoot)
	varsPath := workspacepaths.CanonicalVarsPath(resolvedRoot)
	workspace := WorkspaceSummary{Root: resolvedRoot}
	if info, err := os.Stat(workflowRoot); err == nil && info.IsDir() {
		workspace.HasWorkflowTree = true
	}
	workspace.HasPrepare = isFile(preparePath)
	workspace.HasApply = isFile(applyPath)
	for _, candidate := range []string{varsPath, preparePath, applyPath} {
		if !isFile(candidate) {
			continue
		}
		//nolint:gosec // Candidate paths are derived from the current workspace root.
		content, err := os.ReadFile(candidate)
		if err != nil {
			return WorkspaceSummary{}, fmt.Errorf("read workspace file %s: %w", candidate, err)
		}
		workspace.RelevantFiles = append(workspace.RelevantFiles, ContextFile{
			Path:    relativePath(resolvedRoot, candidate),
			Content: string(content),
		})
	}
	componentDir := filepath.Join(workflowRoot, workspacepaths.WorkflowComponentsDir)
	if entries, err := os.ReadDir(componentDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(strings.ToLower(name), ".yaml") && !strings.HasSuffix(strings.ToLower(name), ".yml") {
				continue
			}
			workspace.ComponentNames = append(workspace.ComponentNames, name)
			if len(workspace.RelevantFiles) >= 8 {
				continue
			}
			path := filepath.Join(componentDir, name)
			//nolint:gosec // Component paths are derived from the current workspace root.
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return WorkspaceSummary{}, fmt.Errorf("read component %s: %w", path, readErr)
			}
			workspace.RelevantFiles = append(workspace.RelevantFiles, ContextFile{
				Path:    relativePath(resolvedRoot, path),
				Content: string(content),
			})
		}
	}
	sort.Strings(workspace.ComponentNames)
	if workspace.HasWorkflowTree {
		if workspace.HasPrepare {
			workspace.SuggestedTargets = append(workspace.SuggestedTargets, relativePath(resolvedRoot, preparePath))
		}
		if workspace.HasApply {
			workspace.SuggestedTargets = append(workspace.SuggestedTargets, relativePath(resolvedRoot, applyPath))
		}
		if isFile(varsPath) {
			workspace.SuggestedTargets = append(workspace.SuggestedTargets, relativePath(resolvedRoot, varsPath))
		}
	} else {
		workspace.SuggestedTargets = []string{
			relFromSlash(workspacepaths.CanonicalVarsPath(resolvedRoot), resolvedRoot),
			relFromSlash(workspacepaths.CanonicalPrepareWorkflowPath(resolvedRoot), resolvedRoot),
			relFromSlash(workspacepaths.CanonicalApplyWorkflowPath(resolvedRoot), resolvedRoot),
		}
	}
	return workspace, nil
}

func inferMode(review bool, workspace WorkspaceSummary) Mode {
	if review {
		return ModeReview
	}
	if workspace.HasWorkflowTree {
		return ModeRefine
	}
	return ModeDraft
}

func buildSystemPrompt(workspace WorkspaceSummary) string {
	workflowMeta := schemadoc.WorkflowMeta()
	toolKinds := schemadoc.ToolKinds()
	b := &strings.Builder{}
	b.WriteString("You are deck ask, a workflow authoring assistant for deck.\n")
	b.WriteString("Follow these rules exactly:\n")
	b.WriteString("- Generate deck workflow files only.\n")
	b.WriteString("- Prefer typed steps over Command.\n")
	b.WriteString("- Keep workflows explicit, reviewable, and local-first.\n")
	b.WriteString("- Do not invent unsupported fields, kinds, or APIs.\n")
	b.WriteString("- Component imports must resolve from workflows/components/.\n")
	b.WriteString("- For a new workspace, include workflows/scenarios/prepare.yaml, workflows/scenarios/apply.yaml, and workflows/vars.yaml unless the user clearly asked for review only.\n")
	b.WriteString("- Return strict JSON only, no markdown fences.\n")
	b.WriteString("- JSON shape: {\"summary\":string,\"review\":[]string,\"files\":[{\"path\":string,\"content\":string}]}.\n")
	b.WriteString("- Files may only target workflows/scenarios/*.yaml, workflows/components/*.yaml, or workflows/vars.yaml.\n")
	b.WriteString("Workflow schema summary: ")
	b.WriteString(workflowMeta.Summary)
	b.WriteString("\nWorkflow notes:\n")
	for _, note := range workflowMeta.Notes {
		b.WriteString("- ")
		b.WriteString(note)
		b.WriteString("\n")
	}
	b.WriteString("Supported typed step kinds:\n")
	for _, kind := range toolKinds {
		meta := schemadoc.ToolMeta(kind)
		b.WriteString("- ")
		b.WriteString(meta.Kind)
		b.WriteString(": ")
		b.WriteString(meta.Summary)
		if meta.WhenToUse != "" {
			b.WriteString(" Use when: ")
			b.WriteString(meta.WhenToUse)
		}
		b.WriteString("\n")
	}
	if len(workspace.ComponentNames) > 0 {
		b.WriteString("Existing components in this workspace:\n")
		for _, name := range workspace.ComponentNames {
			b.WriteString("- ")
			b.WriteString(name)
			b.WriteString("\n")
		}
	}
	return b.String()
}

func buildUserPrompt(input BuildInput, workspace WorkspaceSummary, mode Mode) string {
	b := &strings.Builder{}
	b.WriteString("Workspace root: ")
	b.WriteString(workspace.Root)
	b.WriteString("\n")
	b.WriteString("Mode: ")
	b.WriteString(string(mode))
	b.WriteString("\n")
	b.WriteString("Workspace status:\n")
	_, _ = fmt.Fprintf(b, "- has workflow tree: %t\n", workspace.HasWorkflowTree)
	_, _ = fmt.Fprintf(b, "- has prepare scenario: %t\n", workspace.HasPrepare)
	_, _ = fmt.Fprintf(b, "- has apply scenario: %t\n", workspace.HasApply)
	if len(workspace.SuggestedTargets) > 0 {
		b.WriteString("Suggested target files:\n")
		for _, path := range workspace.SuggestedTargets {
			b.WriteString("- ")
			b.WriteString(path)
			b.WriteString("\n")
		}
	}
	if len(workspace.RelevantFiles) > 0 {
		b.WriteString("Existing workspace files:\n")
		for _, file := range workspace.RelevantFiles {
			b.WriteString("--- FILE: ")
			b.WriteString(file.Path)
			b.WriteString("\n")
			b.WriteString(file.Content)
			if !strings.HasSuffix(file.Content, "\n") {
				b.WriteString("\n")
			}
		}
	}
	if input.State.LastPrompt != "" || input.State.LastLint != "" {
		b.WriteString("Recent ask context:\n")
		if input.State.LastPrompt != "" {
			b.WriteString("- Last prompt: ")
			b.WriteString(input.State.LastPrompt)
			b.WriteString("\n")
		}
		if input.State.LastLint != "" {
			b.WriteString("- Last lint summary: ")
			b.WriteString(input.State.LastLint)
			b.WriteString("\n")
		}
	}
	b.WriteString("User request:\n")
	b.WriteString(strings.TrimSpace(input.Prompt))
	b.WriteString("\n")
	if input.FromLabel != "" {
		b.WriteString("Attached request source: ")
		b.WriteString(input.FromLabel)
		b.WriteString("\n")
	}
	if mode == ModeReview {
		b.WriteString("Return review findings in review[]. Do not return files unless a small targeted fix is absolutely necessary.\n")
	} else {
		b.WriteString("Return the minimum complete file set needed to satisfy the request.\n")
	}
	return b.String()
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func relativePath(root string, path string) string {
	return relFromSlash(path, root)
}

func relFromSlash(path string, root string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}
