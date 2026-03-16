//go:build ai

package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/taedi90/deck/internal/askcli"
	"github.com/taedi90/deck/internal/askconfig"
	"github.com/taedi90/deck/internal/askprovider"
	gollmprovider "github.com/taedi90/deck/internal/askprovider/gollm"
)

var newAskBackend = func() askprovider.Client {
	return gollmprovider.New()
}

func newAskCommand() *cobra.Command {
	var fromPath string
	var write bool
	var review bool
	var maxIterations int
	var provider string
	var model string

	cmd := &cobra.Command{
		Use:   "ask [request]",
		Short: "Draft, refine, or review workflows from the current workspace",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			request := strings.TrimSpace(strings.Join(args, " "))
			return askcli.Execute(cmd.Context(), askcli.Options{
				Root:          ".",
				Prompt:        request,
				FromPath:      fromPath,
				Write:         write,
				Review:        review,
				MaxIterations: maxIterations,
				Provider:      provider,
				Model:         model,
				Stdout:        cmd.OutOrStdout(),
				Stderr:        cmd.ErrOrStderr(),
			}, newAskBackend())
		},
	}
	cmd.Flags().StringVar(&fromPath, "from", "", "load additional request details from a text or markdown file")
	cmd.Flags().BoolVar(&write, "write", false, "write generated workflow changes into the current workspace")
	cmd.Flags().BoolVar(&review, "review", false, "review the current workspace without writing files")
	cmd.Flags().IntVar(&maxIterations, "max-iterations", 3, "maximum lint-repair attempts")
	cmd.Flags().StringVar(&provider, "provider", "", "override the configured ask provider for this run")
	cmd.Flags().StringVar(&model, "model", "", "override the configured ask model for this run")

	cmd.AddCommand(newAskAuthCommand())
	return cmd
}

func newAskAuthCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage global ask authentication and defaults",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newAskAuthSetCommand(), newAskAuthShowCommand(), newAskAuthUnsetCommand())
	return cmd
}

func newAskAuthSetCommand() *cobra.Command {
	var apiKey string
	var provider string
	var model string
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Save ask api key and default provider/model",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			settings, err := askconfig.LoadStored()
			if err != nil {
				return err
			}
			updated := settings
			if value := strings.TrimSpace(apiKey); value != "" {
				updated.APIKey = value
			}
			if value := strings.TrimSpace(provider); value != "" {
				updated.Provider = value
			}
			if value := strings.TrimSpace(model); value != "" {
				updated.Model = value
			}
			if updated == settings {
				return fmt.Errorf("ask auth set requires at least one of --api-key, --provider, or --model")
			}
			if err := askconfig.SaveStored(updated); err != nil {
				return err
			}
			return stdoutPrintln("ask auth saved")
		},
	}
	cmd.Flags().StringVar(&apiKey, "api-key", "", "save the ask api key in XDG config")
	cmd.Flags().StringVar(&provider, "provider", "", "save the default ask provider")
	cmd.Flags().StringVar(&model, "model", "", "save the default ask model")
	return cmd
}

func newAskAuthShowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show the effective ask provider, model, and masked key source",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			effective, err := askconfig.ResolveEffective(askconfig.Settings{})
			if err != nil {
				return err
			}
			if err := stdoutPrintf("provider=%s\n", effective.Provider); err != nil {
				return err
			}
			if err := stdoutPrintf("providerSource=%s\n", effective.ProviderSource); err != nil {
				return err
			}
			if err := stdoutPrintf("model=%s\n", effective.Model); err != nil {
				return err
			}
			if err := stdoutPrintf("modelSource=%s\n", effective.ModelSource); err != nil {
				return err
			}
			if err := stdoutPrintf("apiKey=%s\n", askconfig.MaskAPIKey(effective.APIKey)); err != nil {
				return err
			}
			return stdoutPrintf("apiKeySource=%s\n", effective.APIKeySource)
		},
	}
	return cmd
}

func newAskAuthUnsetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unset",
		Short: "Clear saved ask auth settings from XDG config",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := askconfig.ClearStored(); err != nil {
				return err
			}
			return stdoutPrintln("ask auth cleared")
		},
	}
	return cmd
}
