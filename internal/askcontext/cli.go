package askcontext

type AskCommandMetadata struct {
	Short  string
	Plan   AskPlanCommandMetadata
	Config AskConfigCommandMetadata
	Flags  []CLIFlag
}

type AskPlanCommandMetadata struct {
	Short string
	Long  string
	Flags []CLIFlag
}

type AskConfigCommandMetadata struct {
	Short string
}

func AskCommandMeta() AskCommandMetadata {
	return AskCommandMetadata{
		Short: "(Experimental) AI helper for drafting and reviewing workflows",
		Plan: AskPlanCommandMetadata{
			Short: "Generate an ask plan artifact without writing workflow files",
			Long:  "Generate a reusable planning artifact under .deck/plan without writing workflow files. This mode is intended for draft/refine style authoring requests.",
			Flags: []CLIFlag{
				{Name: "--from", Description: "Load additional request details from a text or markdown file."},
				{Name: "--plan-name", Description: "Optional plan artifact name."},
				{Name: "--plan-dir", Description: "EnsureDirectory for ask plan artifacts."},
			},
		},
		Config: AskConfigCommandMetadata{Short: "Manage global ask config defaults and api key"},
		Flags: []CLIFlag{
			{Name: "--write", Description: "Write generated workflow files into the current workspace."},
			{Name: "--from", Description: "Load additional request details from a text or markdown file."},
			{Name: "--plan-name", Description: "Optional plan artifact name used by ask plan."},
			{Name: "--plan-dir", Description: "EnsureDirectory for ask plan artifacts."},
		},
	}
}
