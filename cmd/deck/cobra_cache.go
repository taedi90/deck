package main

import (
	"github.com/spf13/cobra"
)

func newCacheRunCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Inspect or clean deck cache data",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		newCacheListRunCommand(),
		newCacheCleanRunCommand(),
	)

	return cmd
}
