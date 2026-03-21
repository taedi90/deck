//go:build ai

package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/taedi90/deck/internal/askcli"
	"github.com/taedi90/deck/internal/askconfig"
	"github.com/taedi90/deck/internal/askcontext"
	"github.com/taedi90/deck/internal/askprovider"
	openaiprovider "github.com/taedi90/deck/internal/askprovider/openai"
)

var newAskBackend = func() askprovider.Client {
	return openaiprovider.New()
}

func newAskCommand() *cobra.Command {
	var fromPath string
	var write bool
	var review bool
	var planName string
	var planDir string
	var maxIterations int
	var provider string
	var model string
	var endpoint string
	meta := askcontext.AskCommandMeta()

	cmd := &cobra.Command{
		Use:   "ask [request]",
		Short: meta.Short,
		Example: strings.Join([]string{
			`  deck ask "explain what workflows/scenarios/apply.yaml does"`,
			`  deck ask --write "create an air-gapped rhel9 kubeadm cluster workflow"`,
			`  deck ask plan "create an air-gapped rhel9 kubeadm cluster workflow"`,
		}, "\n"),
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			request := strings.TrimSpace(strings.Join(args, " "))
			return askcli.Execute(cmd.Context(), askcli.Options{
				Root:          ".",
				Prompt:        request,
				FromPath:      fromPath,
				PlanName:      planName,
				PlanDir:       planDir,
				Write:         write,
				Review:        review,
				MaxIterations: maxIterations,
				Provider:      provider,
				Model:         model,
				Endpoint:      endpoint,
				Stdout:        cmd.OutOrStdout(),
				Stderr:        cmd.ErrOrStderr(),
			}, newAskBackend())
		},
	}
	cmd.Flags().StringVar(&fromPath, "from", "", "load additional request details from a text or markdown file")
	cmd.Flags().BoolVar(&write, "write", false, "write generated workflow changes into the current workspace")
	cmd.Flags().BoolVar(&review, "review", false, "review the current workspace without writing files")
	cmd.Flags().IntVar(&maxIterations, "max-iterations", 0, "max repair attempts for draft/refine routes (0 uses route default)")
	cmd.Flags().StringVar(&provider, "provider", "", "override the configured ask provider for this run")
	cmd.Flags().StringVar(&model, "model", "", "override the configured ask model for this run")
	cmd.Flags().StringVar(&endpoint, "endpoint", "", "override the configured ask provider endpoint for this run")
	cmd.Flags().StringVar(&planName, "plan-name", "", "optional plan artifact name used by ask plan")
	cmd.Flags().StringVar(&planDir, "plan-dir", ".deck/plan", "directory for ask plan artifacts")

	cmd.AddCommand(newAskPlanCommand())
	cmd.AddCommand(newAskConfigCommand())
	return cmd
}

func newAskPlanCommand() *cobra.Command {
	var fromPath string
	var planName string
	var planDir string
	var provider string
	var model string
	var endpoint string
	meta := askcontext.AskCommandMeta()
	cmd := &cobra.Command{
		Use:   "plan [request]",
		Short: meta.Plan.Short,
		Long:  meta.Plan.Long,
		Example: strings.Join([]string{
			`  deck ask plan "create an air-gapped rhel9 kubeadm cluster workflow"`,
			`  deck ask plan --plan-name kubeadm-ha "create a 3-node kubeadm workflow"`,
		}, "\n"),
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			request := strings.TrimSpace(strings.Join(args, " "))
			return askcli.Execute(cmd.Context(), askcli.Options{
				Root:     ".",
				Prompt:   request,
				FromPath: fromPath,
				PlanOnly: true,
				PlanName: planName,
				PlanDir:  planDir,
				Provider: provider,
				Model:    model,
				Endpoint: endpoint,
				Stdout:   cmd.OutOrStdout(),
				Stderr:   cmd.ErrOrStderr(),
			}, newAskBackend())
		},
	}
	cmd.Flags().StringVar(&fromPath, "from", "", "load additional request details from a text or markdown file")
	cmd.Flags().StringVar(&planName, "plan-name", "", "optional plan artifact name")
	cmd.Flags().StringVar(&planDir, "plan-dir", ".deck/plan", "directory for ask plan artifacts")
	cmd.Flags().StringVar(&provider, "provider", "", "override the configured ask provider for this run")
	cmd.Flags().StringVar(&model, "model", "", "override the configured ask model for this run")
	cmd.Flags().StringVar(&endpoint, "endpoint", "", "override the configured ask provider endpoint for this run")
	return cmd
}

func newAskConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: askcontext.AskCommandMeta().Config.Short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newAskConfigSetCommand(), newAskConfigShowCommand(), newAskConfigUnsetCommand())
	return cmd
}

func newAskConfigSetCommand() *cobra.Command {
	var apiKey string
	var provider string
	var model string
	var endpoint string
	var logLevel string
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Save ask config defaults and api key",
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
			if value := strings.TrimSpace(endpoint); value != "" {
				updated.Endpoint = value
			}
			if value := strings.TrimSpace(logLevel); value != "" {
				updated.LogLevel = value
			}
			changed := settings.Provider != updated.Provider ||
				settings.Model != updated.Model ||
				settings.APIKey != updated.APIKey ||
				settings.Endpoint != updated.Endpoint ||
				settings.LogLevel != updated.LogLevel
			if !changed {
				return fmt.Errorf("ask config set requires at least one of --api-key, --provider, --model, --endpoint, or --log-level")
			}
			if err := askconfig.SaveStored(updated); err != nil {
				return err
			}
			return stdoutPrintln("ask config saved")
		},
	}
	cmd.Flags().StringVar(&apiKey, "api-key", "", "save the ask api key in XDG config")
	cmd.Flags().StringVar(&provider, "provider", "", "save the default ask provider")
	cmd.Flags().StringVar(&model, "model", "", "save the default ask model")
	cmd.Flags().StringVar(&endpoint, "endpoint", "", "save the default ask provider endpoint")
	cmd.Flags().StringVar(&logLevel, "log-level", "", "save the ask terminal log level (basic, debug, trace)")
	return cmd
}

func newAskConfigShowCommand() *cobra.Command {
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
			if err := stdoutPrintf("endpoint=%s\n", effective.Endpoint); err != nil {
				return err
			}
			if err := stdoutPrintf("endpointSource=%s\n", effective.EndpointSource); err != nil {
				return err
			}
			if err := stdoutPrintf("logLevel=%s\n", effective.LogLevel); err != nil {
				return err
			}
			if err := stdoutPrintf("mcpEnabled=%t\n", effective.MCP.Enabled); err != nil {
				return err
			}
			if err := stdoutPrintf("lspEnabled=%t\n", effective.LSP.Enabled); err != nil {
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

func newAskConfigUnsetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unset",
		Short: "Clear saved ask config settings from XDG config",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := askconfig.ClearStored(); err != nil {
				return err
			}
			return stdoutPrintln("ask config cleared")
		},
	}
	return cmd
}
