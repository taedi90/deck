//go:build !ai

package main

import "github.com/spf13/cobra"

func newAskCommand() *cobra.Command {
	return nil
}
