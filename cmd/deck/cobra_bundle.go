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
			file, err := cmdFlagValue(cmd, "file")
			if err != nil {
				return err
			}
			output, err := cmdFlagValue(cmd, "output")
			if err != nil {
				return err
			}
			return executeBundleVerify(file, args, output)
		},
	}
	cmd.Flags().String("file", "", "bundle path (directory or bundle.tar)")
	cmd.Flags().StringP("output", "o", "text", "output format (text|json)")
	return cmd
}

func newBundleBuildCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Archive deck, workflows, outputs, and manifest",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := cmdFlagValue(cmd, "root")
			if err != nil {
				return err
			}
			out, err := cmdFlagValue(cmd, "out")
			if err != nil {
				return err
			}
			return executeBundleBuild(root, out)
		},
	}
	cmd.Flags().String("root", ".", "workspace root containing deck, workflows, outputs, and .deck/manifest.json")
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
