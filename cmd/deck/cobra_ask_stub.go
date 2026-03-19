//go:build !ai

package main

import "github.com/spf13/cobra"

// Non-AI builds intentionally omit the ask command from the CLI surface.
func newAskCommand() *cobra.Command {
	return nil
}
