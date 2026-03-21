package main

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

func newLintCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lint [scenario]",
		Short: "Lint the workflow tree or a single workflow file",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			scenario := ""
			if len(args) == 1 {
				scenario = args[0]
			}
			root, err := cmdFlagValue(cmd, "root")
			if err != nil {
				return err
			}
			file, err := cmdFlagValue(cmd, "file")
			if err != nil {
				return err
			}
			output, err := cmdFlagValue(cmd, "output")
			if err != nil {
				return err
			}
			return executeLint(cmd.Context(), root, file, scenario, output)
		},
	}
	cmd.Flags().String("root", ".", "workspace root containing workflows/")
	cmd.Flags().StringP("file", "f", "", "path or URL to workflow file")
	cmd.Flags().StringP("output", "o", "text", "output format (text|json)")
	return cmd
}

func newInitCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold starter deck files",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, err := cmdFlagValue(cmd, "out")
			if err != nil {
				return err
			}
			return executeInit(out)
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
			output, err := cmdFlagValue(cmd, "output")
			if err != nil {
				return err
			}
			return executeCacheList(output)
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
			olderThan, err := cmdFlagValue(cmd, "older-than")
			if err != nil {
				return err
			}
			dryRun, err := cmdFlagBoolValue(cmd, "dry-run")
			if err != nil {
				return err
			}
			return executeCacheClean(olderThan, dryRun)
		},
	}
	cmd.Flags().SetInterspersed(false)
	cmd.Flags().String("older-than", "", "delete entries not modified within this duration (e.g. 30d, 24h)")
	cmd.Flags().Bool("dry-run", false, "print deletion plan without deleting")
	return cmd
}

func cmdFlagValue(cmd *cobra.Command, name string) (string, error) {
	flag := cmd.Flags().Lookup(name)
	if flag == nil {
		return "", flagWiringError(cmd, "string", name, fmt.Errorf("flag not registered"))
	}
	return flag.Value.String(), nil
}

func cmdFlagIntValue(cmd *cobra.Command, name string) (int, error) {
	flag := cmd.Flags().Lookup(name)
	if flag == nil {
		return 0, flagWiringError(cmd, "int", name, fmt.Errorf("flag not registered"))
	}
	value, err := strconv.Atoi(flag.Value.String())
	if err != nil {
		return 0, flagWiringError(cmd, "int", name, err)
	}
	return value, nil
}

func cmdFlagBoolValue(cmd *cobra.Command, name string) (bool, error) {
	flag := cmd.Flags().Lookup(name)
	if flag == nil {
		return false, flagWiringError(cmd, "bool", name, fmt.Errorf("flag not registered"))
	}
	value, err := strconv.ParseBool(flag.Value.String())
	if err != nil {
		return false, flagWiringError(cmd, "bool", name, err)
	}
	return value, nil
}

func flagWiringError(cmd *cobra.Command, kind, name string, err error) error {
	return fmt.Errorf("internal CLI wiring error: %s flag %q on %q: %w", kind, name, cmd.CommandPath(), err)
}
