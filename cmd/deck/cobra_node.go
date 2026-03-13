package main

import (
	"github.com/spf13/cobra"
)

func newNodeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "node",
		Short: "Resolve or manage node identity data",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	nodeIDCommand := &cobra.Command{
		Use:   "id",
		Short: "Show, set, or initialize the local node id",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	nodeIDCommand.AddCommand(
		newNodeIDShowCommand(),
		newNodeIDSetCommand(),
		newNodeIDInitCommand(),
	)

	nodeAssignmentCommand := &cobra.Command{
		Use:   "assignment",
		Short: "Show the resolved site assignment for this node",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	nodeAssignmentCommand.AddCommand(
		newNodeAssignmentShowCommand(),
	)

	cmd.AddCommand(nodeIDCommand, nodeAssignmentCommand)

	return cmd
}
