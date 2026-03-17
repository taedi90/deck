package askcli

import (
	"fmt"
	"strings"

	"github.com/taedi90/deck/internal/askcontext"
	"github.com/taedi90/deck/internal/askintent"
	"github.com/taedi90/deck/internal/askretrieve"
	"github.com/taedi90/deck/internal/askstate"
)

func classifierSystemPrompt() string {
	return strings.Join([]string{
		"You are a classifier for deck ask.",
		"Return strict JSON only.",
		"Valid route values: clarify, question, explain, review, refine, draft.",
		"Only choose draft/refine when user clearly asks to create or modify workflow files.",
		"When user asks analyze/explain/summarize existing scenario, choose explain or review.",
		"Include target.kind (workspace|scenario|component|vars|unknown) and optional target.path/name when inferable.",
		"JSON shape: {\"route\":string,\"confidence\":number,\"reason\":string,\"target\":{\"kind\":string,\"path\":string,\"name\":string},\"generationAllowed\":boolean}",
	}, "\n")
}

func classifierUserPrompt(prompt string, reviewFlag bool, workspace askretrieve.WorkspaceSummary) string {
	b := &strings.Builder{}
	b.WriteString("User prompt:\n")
	b.WriteString(strings.TrimSpace(prompt))
	b.WriteString("\n")
	_, _ = fmt.Fprintf(b, "review flag: %t\n", reviewFlag)
	_, _ = fmt.Fprintf(b, "has workflow tree: %t\n", workspace.HasWorkflowTree)
	_, _ = fmt.Fprintf(b, "has prepare scenario: %t\n", workspace.HasPrepare)
	_, _ = fmt.Fprintf(b, "has apply scenario: %t\n", workspace.HasApply)
	b.WriteString("workspace files:\n")
	for _, file := range workspace.Files {
		b.WriteString("- ")
		b.WriteString(file.Path)
		b.WriteString("\n")
	}
	return b.String()
}

func generationSystemPrompt(route askintent.Route, target askintent.Target, retrieval askretrieve.RetrievalResult) string {
	b := &strings.Builder{}
	b.WriteString("You are deck ask, a workflow authoring assistant.\n")
	b.WriteString("Route: ")
	b.WriteString(string(route))
	b.WriteString("\n")
	b.WriteString("Target kind: ")
	b.WriteString(target.Kind)
	b.WriteString("\n")
	if target.Path != "" {
		b.WriteString("Target path: ")
		b.WriteString(target.Path)
		b.WriteString("\n")
	}
	b.WriteString("Rules:\n")
	b.WriteString("- Produce only strict JSON.\n")
	b.WriteString("- JSON shape: {\"summary\":string,\"review\":[]string,\"files\":[{\"path\":string,\"content\":string}]}.\n")
	b.WriteString(askcontext.InvariantPromptBlock().Content)
	b.WriteString("\n")
	b.WriteString(askcontext.PolicyPromptBlock().Content)
	b.WriteString("\n")
	b.WriteString("- Never place summary, description, or review fields inside workflow YAML content.\n")
	b.WriteString("- For a new workspace draft, prefer creating workflows/scenarios/apply.yaml and workflows/vars.yaml only when needed.\n")
	b.WriteString("- If the request is simply to print text in the terminal, a minimal valid apply scenario with one Command step is acceptable.\n")
	b.WriteString("- Do not use whole-value template expressions such as `{{ .vars.dockerPackages }}` for typed fields that expect arrays or objects; inline those YAML arrays or objects directly.\n")
	b.WriteString("- Detailed topology, component/import guidance, vars guidance, and typed-step references are provided through retrieved context.\n")
	b.WriteString("- Example valid minimal scenario YAML:\n")
	b.WriteString("  role: apply\n")
	b.WriteString("  version: v1alpha1\n")
	b.WriteString("  steps:\n")
	b.WriteString("    - id: print-hello\n")
	b.WriteString("      kind: Command\n")
	b.WriteString("      spec:\n")
	b.WriteString("        command:\n")
	b.WriteString("          - echo\n")
	b.WriteString("          - hello\n")
	b.WriteString("- Do not use Kubernetes-style fields such as apiVersion, kind, metadata, or spec wrappers at the workflow top level.\n")
	b.WriteString("- Do not invent unsupported fields.\n")
	b.WriteString("Retrieved context follows.\n")
	b.WriteString(askretrieve.BuildChunkTextWithoutTopics(retrieval, askcontext.TopicWorkflowInvariants, askcontext.TopicPolicy))
	return b.String()
}

func infoPrompts(route askintent.Route, target askintent.Target, retrieval askretrieve.RetrievalResult, prompt string) (string, string) {
	switch route {
	case askintent.RouteExplain:
		return explainSystemPrompt(target, retrieval), explainUserPrompt(prompt, target)
	case askintent.RouteReview:
		return reviewSystemPrompt(target, retrieval), reviewUserPrompt(prompt, target)
	case askintent.RouteQuestion:
		return questionSystemPrompt(target, retrieval), questionUserPrompt(prompt, target)
	default:
		return infoSystemPrompt(route, target, retrieval), infoUserPrompt(prompt, route, target)
	}
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
	if !workspace.HasWorkflowTree && route == askintent.RouteDraft {
		b.WriteString("This is an empty workspace. Return the minimum valid starter workflow files needed to satisfy the request.\n")
		b.WriteString("At minimum, the result should usually include a valid workflows/scenarios/apply.yaml file.\n")
	}
	b.WriteString("Return the minimum complete file set needed for this request.\n")
	return b.String()
}

func infoSystemPrompt(route askintent.Route, target askintent.Target, retrieval askretrieve.RetrievalResult) string {
	b := &strings.Builder{}
	b.WriteString("You are deck ask.\n")
	b.WriteString("Route: ")
	b.WriteString(string(route))
	b.WriteString("\n")
	b.WriteString("Target kind: ")
	b.WriteString(target.Kind)
	b.WriteString("\n")
	if target.Path != "" {
		b.WriteString("Target path: ")
		b.WriteString(target.Path)
		b.WriteString("\n")
	}
	b.WriteString("Return strict JSON with shape {\"summary\":string,\"answer\":string,\"suggestions\":[]string,\"findings\":[]string,\"suggestedChanges\":[]string}.\n")
	b.WriteString("Do not return file content for this route.\n")
	b.WriteString(askretrieve.BuildChunkText(retrieval))
	return b.String()
}

func questionSystemPrompt(target askintent.Target, retrieval askretrieve.RetrievalResult) string {
	b := &strings.Builder{}
	b.WriteString("You are deck ask answering a workflow question.\n")
	b.WriteString("Answer the user's question directly and use retrieved evidence.\n")
	b.WriteString("Keep the answer concise but specific.\n")
	b.WriteString("Return strict JSON with shape {\"summary\":string,\"answer\":string,\"suggestions\":[]string}.\n")
	b.WriteString("If evidence is incomplete, say what is known from the workspace and avoid speculation.\n")
	if target.Path != "" {
		b.WriteString("Target path: ")
		b.WriteString(target.Path)
		b.WriteString("\n")
	}
	b.WriteString(askretrieve.BuildChunkText(retrieval))
	return b.String()
}

func explainSystemPrompt(target askintent.Target, retrieval askretrieve.RetrievalResult) string {
	b := &strings.Builder{}
	b.WriteString("You are deck ask explaining an existing deck workspace file or workflow.\n")
	b.WriteString("Explain what the target does, how it fits into the workflow, and call out imports, phases, major step kinds, and Command usage when present.\n")
	b.WriteString("Do not give a shallow file count summary.\n")
	b.WriteString("Return strict JSON with shape {\"summary\":string,\"answer\":string,\"suggestions\":[]string}.\n")
	if target.Path != "" {
		b.WriteString("Target path: ")
		b.WriteString(target.Path)
		b.WriteString("\n")
	}
	b.WriteString(askretrieve.BuildChunkText(retrieval))
	return b.String()
}

func reviewSystemPrompt(target askintent.Target, retrieval askretrieve.RetrievalResult) string {
	b := &strings.Builder{}
	b.WriteString("You are deck ask reviewing an existing deck workspace.\n")
	b.WriteString("Use the retrieved evidence and any local findings to produce a scoped review with practical concerns and suggested changes.\n")
	b.WriteString("Narrate the findings instead of only repeating raw warnings.\n")
	b.WriteString("Return strict JSON with shape {\"summary\":string,\"answer\":string,\"findings\":[]string,\"suggestedChanges\":[]string}.\n")
	if target.Path != "" {
		b.WriteString("Target path: ")
		b.WriteString(target.Path)
		b.WriteString("\n")
	}
	b.WriteString(askretrieve.BuildChunkText(retrieval))
	return b.String()
}

func infoUserPrompt(prompt string, route askintent.Route, target askintent.Target) string {
	b := &strings.Builder{}
	b.WriteString("Route: ")
	b.WriteString(string(route))
	b.WriteString("\n")
	if target.Path != "" {
		b.WriteString("Target path: ")
		b.WriteString(target.Path)
		b.WriteString("\n")
	}
	b.WriteString("User request:\n")
	b.WriteString(strings.TrimSpace(prompt))
	return b.String()
}

func questionUserPrompt(prompt string, target askintent.Target) string {
	b := &strings.Builder{}
	if target.Path != "" {
		b.WriteString("Target path: ")
		b.WriteString(target.Path)
		b.WriteString("\n")
	}
	b.WriteString("User question:\n")
	b.WriteString(strings.TrimSpace(prompt))
	return b.String()
}

func explainUserPrompt(prompt string, target askintent.Target) string {
	b := &strings.Builder{}
	if target.Path != "" {
		b.WriteString("Explain target: ")
		b.WriteString(target.Path)
		b.WriteString("\n")
	}
	b.WriteString("User request:\n")
	b.WriteString(strings.TrimSpace(prompt))
	return b.String()
}

func reviewUserPrompt(prompt string, target askintent.Target) string {
	b := &strings.Builder{}
	if target.Path != "" {
		b.WriteString("Review target: ")
		b.WriteString(target.Path)
		b.WriteString("\n")
	}
	b.WriteString("User request:\n")
	b.WriteString(strings.TrimSpace(prompt))
	b.WriteString("\nProvide a scoped review with concrete suggested changes.")
	return b.String()
}
