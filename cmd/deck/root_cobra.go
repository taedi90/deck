package main

import "github.com/spf13/cobra"

const (
	commandGroupCore       = "core"
	commandGroupAdditional = "additional"
)

func newRootCommand() *cobra.Command {
	cobra.EnableCommandSorting = false

	cmd := &cobra.Command{
		Use:                "deck",
		Short:              "deck",
		Long:               "Run deck workflows for offline preparation and local execution.",
		SilenceErrors:      true,
		SilenceUsage:       true,
		DisableSuggestions: true,
	}

	cmd.CompletionOptions.DisableDefaultCmd = true
	cmd.SetHelpCommandGroupID(commandGroupAdditional)
	cmd.AddGroup(
		&cobra.Group{ID: commandGroupCore, Title: "Core Commands:"},
		&cobra.Group{ID: commandGroupAdditional, Title: "Additional Commands:"},
	)

	for _, child := range []*cobra.Command{
		withGroup(newInitCommand(), commandGroupCore),
		withGroup(newListCommand(), commandGroupCore),
		withGroup(newLintCommand(), commandGroupCore),
		withGroup(newPrepareCommand(), commandGroupCore),
		withGroup(newBundleCommand(), commandGroupCore),
		withGroup(newPlanCommand(), commandGroupCore),
		withGroup(newApplyCommand(), commandGroupCore),
		withGroup(newSourceCommand(), commandGroupAdditional),
		withGroup(newServerCommand(), commandGroupAdditional),
		withGroup(newAskCommand(), commandGroupAdditional),
		withGroup(newCompletionCommand(), commandGroupAdditional),
		withGroup(newCacheCommand(), commandGroupAdditional),
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
