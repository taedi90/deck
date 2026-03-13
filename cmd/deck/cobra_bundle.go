package main

import (
	"errors"

	"github.com/spf13/cobra"
)

func newBundleCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bundle",
		Short: "Verify, inspect, import, collect, or merge bundles",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		newBundleVerifyCommand(),
		newBundleInspectCommand(),
		newBundleImportCommand(),
		newBundleCollectCommand(),
		newBundleMergeCommand(),
	)

	return cmd
}

func newBundleVerifyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify [path]",
		Short: "Verify bundle manifest integrity",
		Args:  bundleSinglePathArgs("bundle verify accepts a single <path>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			return executeBundleVerify(cmdFlagValue(cmd, "file"), args)
		},
	}
	cmd.Flags().String("file", "", "bundle path (directory or bundle.tar)")
	return cmd
}

func newBundleInspectCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inspect [path]",
		Short: "List manifest entries in a bundle",
		Args:  bundleSinglePathArgs("bundle inspect accepts a single <path>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			return executeBundleInspect(cmdFlagValue(cmd, "file"), cmdFlagValue(cmd, "output"), args)
		},
	}
	cmd.Flags().String("file", "", "bundle path (directory or bundle.tar)")
	cmd.Flags().StringP("output", "o", "text", "output format (text|json)")
	return cmd
}

func newBundleImportCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Extract a bundle archive into a directory",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeBundleImport(cmdFlagValue(cmd, "file"), cmdFlagValue(cmd, "dest"))
		},
	}
	cmd.Flags().String("file", "", "bundle archive file path")
	cmd.Flags().String("dest", "", "destination directory")
	return cmd
}

func newBundleCollectCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "collect",
		Short: "Create a bundle archive from a directory",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeBundleCollect(cmdFlagValue(cmd, "root"), cmdFlagValue(cmd, "out"))
		},
	}
	cmd.Flags().String("root", "", "bundle directory")
	cmd.Flags().String("out", "", "output tar archive path")
	return cmd
}

func newBundleMergeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "merge <bundle.tar>",
		Short: "Merge a bundle archive into a destination directory",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return executeBundleMerge(cmdFlagValue(cmd, "to"), cmdFlagBoolValue(cmd, "dry-run"), args)
		},
	}
	cmd.Flags().SetInterspersed(false)
	cmd.Flags().String("to", "", "merge destination (local directory)")
	cmd.Flags().Bool("dry-run", false, "print merge plan without writing")
	return cmd
}

func bundleSinglePathArgs(message string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) > 1 {
			return errors.New(message)
		}
		return nil
	}
}
