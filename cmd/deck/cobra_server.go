package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func newServerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Manage default server settings and local server runtime",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		newServerSetCommand(),
		newServerShowCommand(),
		newServerUnsetCommand(),
		newServerWorkflowsCommand(),
		newServerUpCommand(),
		newServerDownCommand(),
		newServerHealthCommand(),
		newServerLogsCommand(),
	)

	return cmd
}

func newServerSetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <url>",
		Short: "Save the default server profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return executeServerSet(args[0], cmdFlagValue(cmd, "api-token"))
		},
	}
	cmd.Flags().String("api-token", "", "default API token for assisted site APIs")
	return cmd
}

func newServerShowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show the effective default server URL",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return executeServerShow()
		},
	}
	return cmd
}

func newServerUnsetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unset",
		Short: "Clear the saved default server URL",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return executeServerUnset()
		},
	}
	return cmd
}

func newServerUpCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "up",
		Short: "Start the local bundle server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeServerUp(serverUpOptions{
				root:          cmdFlagValue(cmd, "root"),
				addr:          cmdFlagValue(cmd, "addr"),
				apiToken:      cmdFlagValue(cmd, "api-token"),
				reportMax:     cmdFlagIntValue(cmd, "report-max"),
				auditMaxSize:  cmdFlagIntValue(cmd, "audit-max-size-mb"),
				auditMaxFiles: cmdFlagIntValue(cmd, "audit-max-files"),
				tlsCert:       cmdFlagValue(cmd, "tls-cert"),
				tlsKey:        cmdFlagValue(cmd, "tls-key"),
				tlsSelfSigned: cmdFlagBoolValue(cmd, "tls-self-signed"),
				daemon:        cmdFlagBoolValue(cmd, "daemon"),
				unit:          cmdFlagValue(cmd, "unit"),
			})
		},
	}
	cmd.Flags().String("root", "./bundle", "server content root")
	cmd.Flags().String("addr", ":8080", "server listen address")
	cmd.Flags().String("api-token", "deck-site-v1", "bearer token required for /api/site/v1 endpoints")
	cmd.Flags().Int("report-max", 200, "max retained in-memory reports")
	cmd.Flags().Int("audit-max-size-mb", 50, "max audit log size in MB before rotation")
	cmd.Flags().Int("audit-max-files", 10, "max retained rotated audit files")
	cmd.Flags().String("tls-cert", "", "TLS certificate path")
	cmd.Flags().String("tls-key", "", "TLS private key path")
	cmd.Flags().Bool("tls-self-signed", false, "auto-generate and use self-signed TLS cert")
	cmd.Flags().BoolP("daemon", "d", false, "run as a transient systemd service")
	cmd.Flags().String("unit", "deck-server", "systemd unit name for daemon mode")
	return cmd
}

func newServerWorkflowsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workflows",
		Short: "List available workflows from a server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeList(cmdFlagValue(cmd, "server"), cmdFlagValue(cmd, "output"))
		},
	}
	cmd.Flags().SetInterspersed(false)
	cmd.Flags().String("server", "", "server URL for index (defaults to saved server)")
	cmd.Flags().StringP("output", "o", "text", "output format (text|json)")
	return cmd
}

func newServerDownCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "down",
		Short: "Stop the local server daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeServerDown(cmdFlagValue(cmd, "unit"))
		},
	}
	cmd.Flags().String("unit", "deck-server", "systemd unit name to stop")
	return cmd
}

func newServerHealthCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Probe the configured or explicit server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeHealth(cmdFlagValue(cmd, "server"))
		},
	}
	cmd.Flags().String("server", "", "server base URL (defaults to saved server)")
	return cmd
}

func newServerLogsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Read local server audit logs from file or journal",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeLogs(
				cmdFlagValue(cmd, "root"),
				cmdFlagValue(cmd, "source"),
				cmdFlagValue(cmd, "path"),
				cmdFlagValue(cmd, "unit"),
				cmdFlagValue(cmd, "output"),
			)
		},
	}
	cmd.Flags().String("root", ".", "serve root directory")
	cmd.Flags().String("source", "file", "log source (file|journal|both)")
	cmd.Flags().String("path", "", "explicit audit log file path")
	cmd.Flags().String("unit", "deck-server.service", "systemd unit for journal logs")
	cmd.Flags().StringP("output", "o", "text", "output format (text|json)")
	return cmd
}

func executeServerSet(rawURL string, apiToken string) error {
	resolved := strings.TrimRight(strings.TrimSpace(rawURL), "/")
	if err := validateServerURL(resolved); err != nil {
		return err
	}
	if err := saveServerDefaults(serverDefaults{URL: resolved, APIToken: strings.TrimSpace(apiToken)}); err != nil {
		return err
	}
	if strings.TrimSpace(apiToken) == "" {
		return stdoutPrintf("server default set: %s\n", resolved)
	}
	return stdoutPrintf("server default set: %s (api-token saved)\n", resolved)
}

func executeServerShow() error {
	resolved, source, err := resolveServerURL("")
	if err != nil {
		return err
	}
	apiToken, apiTokenSource, err := resolveServerAPIToken("")
	if err != nil {
		return err
	}
	if err := stdoutPrintf("server=%s\n", resolved); err != nil {
		return err
	}
	if err := stdoutPrintf("api-token-set=%t\n", strings.TrimSpace(apiToken) != ""); err != nil {
		return err
	}
	if strings.TrimSpace(apiTokenSource) != "" {
		if err := stdoutPrintf("api-token-source=%s\n", apiTokenSource); err != nil {
			return err
		}
	}
	if resolved == "" {
		source = "none"
	}
	if err := stdoutPrintf("source=%s\n", source); err != nil {
		return err
	}
	return nil
}

func executeServerUnset() error {
	if err := clearServerDefaults(); err != nil {
		return err
	}
	return stdoutPrintln("server default cleared")
}

type serverUpOptions struct {
	root          string
	addr          string
	apiToken      string
	reportMax     int
	auditMaxSize  int
	auditMaxFiles int
	tlsCert       string
	tlsKey        string
	tlsSelfSigned bool
	daemon        bool
	unit          string
}

func executeServerUp(opts serverUpOptions) error {
	if err := validateServerUpDaemonMode(opts); err != nil {
		return err
	}
	if !opts.daemon {
		return executeServe(opts.root, opts.addr, opts.apiToken, opts.reportMax, opts.auditMaxSize, opts.auditMaxFiles, opts.tlsCert, opts.tlsKey, opts.tlsSelfSigned)
	}
	return runServerDaemon(opts)
}

func executeServerDown(unit string) error {
	resolvedUnit := normalizeServerUnitName(unit)
	cmd := exec.Command("systemctl", "stop", resolvedUnit)
	raw, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("server down: %s", msg)
	}
	return stdoutPrintf("server down: ok (%s)\n", resolvedUnit)
}

func runServerDaemon(opts serverUpOptions) error {
	resolvedUnit := normalizeServerUnitBaseName(opts.unit)
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("server up: resolve executable: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("server up: resolve working directory: %w", err)
	}
	args := []string{
		"--unit", resolvedUnit,
		"--property", "WorkingDirectory=" + cwd,
		"--service-type=simple",
		execPath,
		"server", "up",
		"--root", opts.root,
		"--addr", opts.addr,
		"--api-token", opts.apiToken,
		"--report-max", fmt.Sprintf("%d", opts.reportMax),
		"--audit-max-size-mb", fmt.Sprintf("%d", opts.auditMaxSize),
		"--audit-max-files", fmt.Sprintf("%d", opts.auditMaxFiles),
	}
	if strings.TrimSpace(opts.tlsCert) != "" {
		args = append(args, "--tls-cert", opts.tlsCert)
	}
	if strings.TrimSpace(opts.tlsKey) != "" {
		args = append(args, "--tls-key", opts.tlsKey)
	}
	if opts.tlsSelfSigned {
		args = append(args, "--tls-self-signed")
	}
	cmd := exec.Command("systemd-run", args...)
	raw, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("server up: %s", msg)
	}
	if err := stdoutPrintf("server up: ok (%s.service)\n", resolvedUnit); err != nil {
		return err
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil
	}
	return stdoutPrintf("%s\n", trimmed)
}

func normalizeServerUnitBaseName(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "deck-server"
	}
	return strings.TrimSuffix(trimmed, ".service")
}

func normalizeServerUnitName(raw string) string {
	base := normalizeServerUnitBaseName(raw)
	if strings.HasSuffix(base, ".service") {
		return base
	}
	return base + ".service"
}

func validateServerUpDaemonMode(opts serverUpOptions) error {
	if !opts.daemon {
		return nil
	}
	if _, err := exec.LookPath("systemd-run"); err != nil {
		return errors.New("server up: systemd-run not found")
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return errors.New("server up: systemctl not found")
	}
	if strings.TrimSpace(opts.unit) == "" {
		return errors.New("server up: --unit must not be empty")
	}
	if strings.ContainsRune(opts.unit, filepath.Separator) {
		return errors.New("server up: --unit must be a unit name, not a path")
	}
	return nil
}
