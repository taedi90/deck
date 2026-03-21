package askcontext

import "strings"

const (
	BeginCLIDocMarker       = "<!-- BEGIN GENERATED:ASK_CLI_CONTEXT -->"
	EndCLIDocMarker         = "<!-- END GENERATED:ASK_CLI_CONTEXT -->"
	BeginAuthoringDocMarker = "<!-- BEGIN GENERATED:ASK_AUTHORING_CONTEXT -->"
	EndAuthoringDocMarker   = "<!-- END GENERATED:ASK_AUTHORING_CONTEXT -->"
)

func AuthoringDocBlock() string {
	b := &strings.Builder{}
	b.WriteString("## Ask authoring context\n\n")
	b.WriteString("- ")
	b.WriteString(Current().Workflow.Summary)
	b.WriteString("\n- ")
	b.WriteString(Current().Components.ImportRule)
	b.WriteString("\n- ")
	b.WriteString(Current().Vars.Summary)
	b.WriteString("\n- Prefer typed steps over `Command` when a typed step exists.\n")
	return b.String()
}

func CLIDocBlock() string {
	b := &strings.Builder{}
	b.WriteString("## Ask CLI context\n\n")
	b.WriteString("- `")
	b.WriteString(Current().CLI.Command)
	b.WriteString("` previews by default; add `--write` to write workflow files.\n")
	b.WriteString("- `")
	b.WriteString(Current().CLI.PlanSubcommand)
	b.WriteString("` saves reusable plan artifacts under `./.deck/plan/`.\n")
	return b.String()
}

func SyncedCLIDocBlock() string {
	return BeginCLIDocMarker + "\n" + strings.TrimRight(CLIDocBlock(), "\n") + "\n" + EndCLIDocMarker
}

func SyncedAuthoringDocBlock() string {
	return BeginAuthoringDocMarker + "\n" + strings.TrimRight(AuthoringDocBlock(), "\n") + "\n" + EndAuthoringDocMarker
}

func SyncManagedBlocks(content string) string {
	updated := replaceManagedBlock(content, BeginCLIDocMarker, EndCLIDocMarker, CLIDocBlock())
	updated = replaceManagedBlock(updated, BeginAuthoringDocMarker, EndAuthoringDocMarker, AuthoringDocBlock())
	return updated
}

func replaceManagedBlock(content string, begin string, end string, block string) string {
	start := strings.Index(content, begin)
	finish := strings.Index(content, end)
	if start == -1 || finish == -1 || finish < start {
		return content
	}
	finish += len(end)
	replacement := begin + "\n" + strings.TrimRight(block, "\n") + "\n" + end
	return content[:start] + replacement + content[finish:]
}
