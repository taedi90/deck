package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newValidateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate [scenario]",
		Short: "Validate a deck file",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			scenario := ""
			if len(args) == 1 {
				scenario = args[0]
			}
			return executeValidate(cmdFlagValue(cmd, "file"), scenario)
		},
	}
	cmd.Flags().StringP("file", "f", "", "path or URL to workflow file")
	return cmd
}

func newInitCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold starter deck files",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeInit(cmdFlagValue(cmd, "out"))
		},
	}
	cmd.Flags().String("out", ".", "output directory")

	return cmd
}

func newCacheListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Show cached files",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeCacheList(cmdFlagValue(cmd, "output"))
		},
	}
	cmd.Flags().StringP("output", "o", "text", "output format (text|json)")
	return cmd
}

func newCacheCleanCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Delete cached entries, optionally by age",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeCacheClean(cmdFlagValue(cmd, "older-than"), cmdFlagBoolValue(cmd, "dry-run"))
		},
	}
	cmd.Flags().SetInterspersed(false)
	cmd.Flags().String("older-than", "", "delete entries not modified within this duration (e.g. 30d, 24h)")
	cmd.Flags().Bool("dry-run", false, "print deletion plan without deleting")
	return cmd
}

func newNodeIDShowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Print the resolved node id and source",
		Args:  cobra.ExactArgs(0),
		RunE: func(_ *cobra.Command, _ []string) error {
			return executeNodeIDShow()
		},
	}
	return cmd
}

func newNodeIDSetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <node-id>",
		Short: "Write the operator node id",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return executeNodeIDSet(args[0])
		},
	}
	return cmd
}

func newNodeIDInitCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Generate a node id if one is missing",
		Args:  cobra.ExactArgs(0),
		RunE: func(_ *cobra.Command, _ []string) error {
			return executeNodeIDInit()
		},
	}
	return cmd
}

func newNodeAssignmentShowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show the resolved site assignment for the current node id",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeNodeAssignmentShow(
				cmdFlagValue(cmd, "root"),
				cmdFlagValue(cmd, "session"),
				cmdFlagValue(cmd, "action"),
				cmdFlagValue(cmd, "output"),
			)
		},
	}
	cmd.Flags().String("root", ".", "site server root")
	cmd.Flags().String("session", "", "session id")
	cmd.Flags().String("action", "apply", "assignment action (diff|doctor|apply)")
	cmd.Flags().StringP("output", "o", "text", "output format (text|json)")
	return cmd
}

func cmdFlagValue(cmd *cobra.Command, name string) string {
	value, err := cmd.Flags().GetString(name)
	if err != nil {
		panic(fmt.Sprintf("internal CLI wiring error: string flag %q not registered on %q: %v", name, cmd.CommandPath(), err))
	}
	return value
}

func cmdFlagIntValue(cmd *cobra.Command, name string) int {
	value, err := cmd.Flags().GetInt(name)
	if err != nil {
		panic(fmt.Sprintf("internal CLI wiring error: int flag %q not registered on %q: %v", name, cmd.CommandPath(), err))
	}
	return value
}

func cmdFlagBoolValue(cmd *cobra.Command, name string) bool {
	value, err := cmd.Flags().GetBool(name)
	if err != nil {
		panic(fmt.Sprintf("internal CLI wiring error: bool flag %q not registered on %q: %v", name, cmd.CommandPath(), err))
	}
	return value
}
