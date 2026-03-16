//go:build !ai

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newAskCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "ask",
		Short: "Experimental AI helper (not included in this build)",
		Args:  cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("ask is not available in this build; rebuild with -tags ai or use the AI-ready binary")
		},
	}
}
