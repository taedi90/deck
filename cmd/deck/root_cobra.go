package main

import "github.com/spf13/cobra"

func newRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:                "deck",
		Short:              "deck",
		Long:               "Run deck bundle, apply, site, and server commands.",
		SilenceErrors:      true,
		SilenceUsage:       true,
		DisableSuggestions: true,
	}

	cmd.CompletionOptions.DisableDefaultCmd = true

	cmd.AddCommand(
		newPackCommand(),
		newApplyCommand(),
		newCompletionCommand(),
		newServeCommand(),
		newListCommand(),
		newValidateCommand(),
		newDiffCommand(),
		newDoctorCommand(),
		newHealthCommand(),
		newLogsCommand(),
		newInitCommand(),
		newBundleCommand(),
		newCacheCommand(),
		newNodeCommand(),
		newSiteCommand(),
	)

	return cmd
}
