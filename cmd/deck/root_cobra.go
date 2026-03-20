package main

import "github.com/spf13/cobra"

const (
	commandGroupCore       = "core"
	commandGroupAdditional = "additional"
)

func newRootRunCommand() *cobra.Command {
	cobra.EnableCommandSorting = false
	setCLIVerbosity(0)

	cmd := &cobra.Command{
		Use:                "deck",
		Short:              "deck",
		Long:               "Run deck workflows for offline preparation and local execution.",
		SilenceErrors:      true,
		SilenceUsage:       true,
		DisableSuggestions: true,
	}
	cmd.PersistentFlags().IntVar(&cliVerbosity, "v", 0, "diagnostic verbosity level")

	cmd.CompletionOptions.DisableDefaultCmd = true
	cmd.SetHelpCommandGroupID(commandGroupAdditional)
	cmd.AddGroup(
		&cobra.Group{ID: commandGroupCore, Title: "Core RunCommands:"},
		&cobra.Group{ID: commandGroupAdditional, Title: "Additional RunCommands:"},
	)

	for _, child := range []*cobra.Command{
		withGroup(newInitRunCommand(), commandGroupCore),
		withGroup(newLintRunCommand(), commandGroupCore),
		withGroup(newPrepareRunCommand(), commandGroupCore),
		withGroup(newBundleRunCommand(), commandGroupCore),
		withGroup(newPlanRunCommand(), commandGroupCore),
		withGroup(newApplyRunCommand(), commandGroupCore),
		withGroup(newListRunCommand(), commandGroupAdditional),
		withGroup(newServerRunCommand(), commandGroupAdditional),
		withGroup(newAskCommand(), commandGroupAdditional),
		withGroup(newVersionRunCommand(), commandGroupAdditional),
		withGroup(newCompletionRunCommand(), commandGroupAdditional),
		withGroup(newCacheRunCommand(), commandGroupAdditional),
	} {
		if child != nil {
			cmd.AddCommand(child)
		}
	}

	return cmd
}

func withGroup(cmd *cobra.Command, groupID string) *cobra.Command {
	if cmd == nil {
		return nil
	}
	cmd.GroupID = groupID
	return cmd
}
