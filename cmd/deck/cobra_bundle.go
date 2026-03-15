package main

import (
	"errors"

	"github.com/spf13/cobra"
)

func newBundleCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bundle",
		Short: "Build or verify bundles",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		newBundleBuildCommand(),
		newBundleVerifyCommand(),
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

func newBundleBuildCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Create a bundle archive from a prepared directory",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeBundleBuild(cmdFlagValue(cmd, "root"), cmdFlagValue(cmd, "out"))
		},
	}
	cmd.Flags().String("root", ".", "workspace root to archive")
	cmd.Flags().String("out", "", "output tar archive path")
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
