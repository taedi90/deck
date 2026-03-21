package main

import (
	"github.com/spf13/cobra"

	"github.com/taedi90/deck/internal/preparecli"
	"github.com/taedi90/deck/internal/workspacepaths"
)

type prepareOptions struct {
	preparedRoot string
	dryRun       bool
	refresh      bool
	clean        bool
	varOverrides map[string]string
}

func newPrepareCommand() *cobra.Command {
	vars := &varFlag{}
	cmd := &cobra.Command{
		Use:   "prepare",
		Short: "Prepare bundle contents under outputs/",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			preparedRoot, err := cmdFlagValue(cmd, "root")
			if err != nil {
				return err
			}
			dryRun, err := cmdFlagBoolValue(cmd, "dry-run")
			if err != nil {
				return err
			}
			refresh, err := cmdFlagBoolValue(cmd, "refresh")
			if err != nil {
				return err
			}
			clean, err := cmdFlagBoolValue(cmd, "clean")
			if err != nil {
				return err
			}
			return runPrepareWithOptions(cmd, prepareOptions{
				preparedRoot: preparedRoot,
				dryRun:       dryRun,
				refresh:      refresh,
				clean:        clean,
				varOverrides: vars.AsMap(),
			})
		},
	}
	cmd.Flags().String("root", workspacepaths.DefaultPreparedRoot("."), "prepared bundle output directory")
	cmd.Flags().Bool("dry-run", false, "print prepare plan without writing files")
	cmd.Flags().Bool("refresh", false, "re-download artifacts instead of reusing prepared files")
	cmd.Flags().Bool("clean", false, "remove the prepared directory before writing")
	cmd.Flags().Var(vars, "var", "set variable override (key=value), repeatable")
	return cmd
}

func runPrepareWithOptions(cmd *cobra.Command, opts prepareOptions) error {
	if err := verbosef(1, "deck: prepare root=%s dryRun=%t refresh=%t clean=%t\n", opts.preparedRoot, opts.dryRun, opts.refresh, opts.clean); err != nil {
		return err
	}
	return preparecli.Run(cmd.Context(), preparecli.Options{
		PreparedRoot: opts.preparedRoot,
		DryRun:       opts.dryRun,
		Refresh:      opts.refresh,
		Clean:        opts.clean,
		VarOverrides: varsAsAnyMap(opts.varOverrides),
		Stdout:       stdoutWriter(),
		Diagnosticf:  verbosef,
	})
}
