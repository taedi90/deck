package main

import (
	"github.com/spf13/cobra"

	"github.com/taedi90/deck/internal/buildinfo"
)

func newVersionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show deck build version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			output, err := cmdFlagValue(cmd, "output")
			if err != nil {
				return err
			}
			resolvedOutput, err := resolveOutputFormat(output)
			if err != nil {
				return err
			}
			if resolvedOutput == "text" {
				return stdoutPrintf("%s\n", buildinfo.Summary())
			}

			enc := stdoutJSONEncoder()
			enc.SetIndent("", "  ")
			return enc.Encode(buildinfo.Current())
		},
	}
	cmd.Flags().StringP("output", "o", "text", "output format (text|json)")
	return cmd
}
