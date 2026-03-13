package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available deck files from a local bundle or server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeList(cmdFlagValue(cmd, "server"), cmdFlagValue(cmd, "output"))
		},
	}
	cmd.Flags().SetInterspersed(false)
	cmd.Flags().String("server", "", "server URL for index (optional; defaults to local workflows/)")
	cmd.Flags().StringP("output", "o", "text", "output format (text|json)")
	return cmd
}

func newValidateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate a deck file",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeValidate(cmdFlagValue(cmd, "file"))
		},
	}
	cmd.Flags().StringP("file", "f", "", "path or URL to workflow file")
	return cmd
}

func newHealthCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Probe a running deck server",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeHealth(cmdFlagValue(cmd, "server"))
		},
	}
	cmd.Flags().String("server", "", "server base URL (required)")
	return cmd
}

func newServeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the bundle server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeServe(
				cmdFlagValue(cmd, "root"),
				cmdFlagValue(cmd, "addr"),
				cmdFlagValue(cmd, "api-token"),
				cmdFlagIntValue(cmd, "report-max"),
				cmdFlagIntValue(cmd, "audit-max-size-mb"),
				cmdFlagIntValue(cmd, "audit-max-files"),
				cmdFlagValue(cmd, "tls-cert"),
				cmdFlagValue(cmd, "tls-key"),
				cmdFlagBoolValue(cmd, "tls-self-signed"),
			)
		},
	}
	cmd.Flags().SetInterspersed(false)
	cmd.Flags().String("root", "./bundle", "server content root")
	cmd.Flags().String("addr", ":8080", "server listen address")
	cmd.Flags().String("api-token", "deck-site-v1", "bearer token required for /api/site/v1 endpoints")
	cmd.Flags().Int("report-max", 200, "max retained in-memory reports")
	cmd.Flags().Int("audit-max-size-mb", 50, "max audit log size in MB before rotation")
	cmd.Flags().Int("audit-max-files", 10, "max retained rotated audit files")
	cmd.Flags().String("tls-cert", "", "TLS certificate path")
	cmd.Flags().String("tls-key", "", "TLS private key path")
	cmd.Flags().Bool("tls-self-signed", false, "auto-generate and use self-signed TLS cert")
	return cmd
}

func newLogsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Read server audit logs from file or journal",
		Args:  cobra.ExactArgs(0),
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
	cmd.Flags().String("unit", "", "systemd unit for journal logs")
	cmd.Flags().StringP("output", "o", "text", "output format (text|json)")
	return cmd
}

func newInitCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold starter deck files",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeInit(cmdFlagValue(cmd, "out"))
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
			return executeCacheList(cmdFlagValue(cmd, "output"))
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
			return executeCacheClean(cmdFlagValue(cmd, "older-than"), cmdFlagBoolValue(cmd, "dry-run"))
		},
	}
	cmd.Flags().SetInterspersed(false)
	cmd.Flags().String("older-than", "", "delete entries not modified within this duration (e.g. 30d, 24h)")
	cmd.Flags().Bool("dry-run", false, "print deletion plan without deleting")
	return cmd
}

func newNodeIDShowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Print the resolved node id and source",
		Args:  cobra.ExactArgs(0),
		RunE: func(_ *cobra.Command, _ []string) error {
			return executeNodeIDShow()
		},
	}
	return cmd
}

func newNodeIDSetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <node-id>",
		Short: "Write the operator node id",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return executeNodeIDSet(args[0])
		},
	}
	return cmd
}

func newNodeIDInitCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Generate a node id if one is missing",
		Args:  cobra.ExactArgs(0),
		RunE: func(_ *cobra.Command, _ []string) error {
			return executeNodeIDInit()
		},
	}
	return cmd
}

func newNodeAssignmentShowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show the resolved site assignment for the current node id",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeNodeAssignmentShow(
				cmdFlagValue(cmd, "root"),
				cmdFlagValue(cmd, "session"),
				cmdFlagValue(cmd, "action"),
				cmdFlagValue(cmd, "output"),
			)
		},
	}
	cmd.Flags().String("root", ".", "site server root")
	cmd.Flags().String("session", "", "session id")
	cmd.Flags().String("action", "apply", "assignment action (diff|doctor|apply)")
	cmd.Flags().StringP("output", "o", "text", "output format (text|json)")
	return cmd
}

func cmdFlagValue(cmd *cobra.Command, name string) string {
	value, err := cmd.Flags().GetString(name)
	if err != nil {
		panic(fmt.Sprintf("internal CLI wiring error: string flag %q not registered on %q: %v", name, cmd.CommandPath(), err))
	}
	return value
}

func cmdFlagIntValue(cmd *cobra.Command, name string) int {
	value, err := cmd.Flags().GetInt(name)
	if err != nil {
		panic(fmt.Sprintf("internal CLI wiring error: int flag %q not registered on %q: %v", name, cmd.CommandPath(), err))
	}
	return value
}

func cmdFlagBoolValue(cmd *cobra.Command, name string) bool {
	value, err := cmd.Flags().GetBool(name)
	if err != nil {
		panic(fmt.Sprintf("internal CLI wiring error: bool flag %q not registered on %q: %v", name, cmd.CommandPath(), err))
	}
	return value
}
