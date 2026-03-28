package askcontext

import "github.com/Airgap-Castaways/deck/internal/askcommandspec"

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
	spec := askcommandspec.Current()
	planFlags := make([]CLIFlag, 0, len(spec.Plan.Flags))
	for _, flag := range spec.Plan.Flags {
		planFlags = append(planFlags, CLIFlag{Name: flag.Name, Description: flag.Description})
	}
	rootFlags := make([]CLIFlag, 0, len(spec.Root.Flags))
	for _, flag := range spec.Root.Flags {
		rootFlags = append(rootFlags, CLIFlag{Name: flag.Name, Description: flag.Description})
	}
	return AskCommandMetadata{
		Short: spec.Root.Short,
		Plan: AskPlanCommandMetadata{
			Short: spec.Plan.Short,
			Long:  spec.Plan.Long,
			Flags: planFlags,
		},
		Config: AskConfigCommandMetadata{Short: spec.Config.Short},
		Flags:  rootFlags,
	}
}
