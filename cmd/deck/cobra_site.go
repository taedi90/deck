package main

import (
	"github.com/spf13/cobra"
)

func newSiteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "site",
		Short: "Manage site releases, sessions, and assignments",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	siteReleaseCommand := &cobra.Command{
		Use:   "release",
		Short: "Import or list site releases",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	siteReleaseCommand.AddCommand(
		newSiteReleaseImportCommand(),
		newSiteReleaseListCommand(),
	)

	siteSessionCommand := &cobra.Command{
		Use:   "session",
		Short: "Create or close sessions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	siteSessionCommand.AddCommand(
		newSiteSessionCreateCommand(),
		newSiteSessionCloseCommand(),
	)

	siteAssignCommand := &cobra.Command{
		Use:   "assign",
		Short: "Assign workflows by role or node",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	siteAssignCommand.AddCommand(
		newSiteAssignRoleCommand(),
		newSiteAssignNodeCommand(),
	)

	cmd.AddCommand(
		siteReleaseCommand,
		siteSessionCommand,
		siteAssignCommand,
		newSiteStatusCommand(),
	)
	return cmd
}

func newSiteReleaseImportCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import a bundle archive as a release",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeSiteReleaseImport(
				cmdFlagValue(cmd, "root"),
				cmdFlagValue(cmd, "id"),
				cmdFlagValue(cmd, "bundle"),
				cmdFlagValue(cmd, "created-at"),
			)
		},
	}
	cmd.Flags().String("root", ".", "site server root")
	cmd.Flags().String("id", "", "release id")
	cmd.Flags().String("bundle", "", "local bundle archive path")
	cmd.Flags().String("created-at", "", "release timestamp (RFC3339, optional)")
	return cmd
}

func newSiteReleaseListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List stored releases",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeSiteReleaseList(
				cmdFlagValue(cmd, "root"),
				cmdFlagValue(cmd, "output"),
			)
		},
	}
	cmd.Flags().String("root", ".", "site server root")
	cmd.Flags().StringP("output", "o", "text", "output format (text|json)")
	return cmd
}

func newSiteSessionCreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new session for a release",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeSiteSessionCreate(
				cmdFlagValue(cmd, "root"),
				cmdFlagValue(cmd, "id"),
				cmdFlagValue(cmd, "release"),
				cmdFlagValue(cmd, "started-at"),
			)
		},
	}
	cmd.Flags().String("root", ".", "site server root")
	cmd.Flags().String("id", "", "session id")
	cmd.Flags().String("release", "", "release id")
	cmd.Flags().String("started-at", "", "session start timestamp (RFC3339, optional)")
	return cmd
}

func newSiteSessionCloseCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "close",
		Short: "Close an existing session",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeSiteSessionClose(
				cmdFlagValue(cmd, "root"),
				cmdFlagValue(cmd, "id"),
				cmdFlagValue(cmd, "closed-at"),
			)
		},
	}
	cmd.Flags().String("root", ".", "site server root")
	cmd.Flags().String("id", "", "session id")
	cmd.Flags().String("closed-at", "", "session close timestamp (RFC3339, optional)")
	return cmd
}

func newSiteAssignRoleCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "role",
		Short: "Assign a workflow to a role for a session",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeSiteAssignRole(
				cmdFlagValue(cmd, "root"),
				cmdFlagValue(cmd, "session"),
				cmdFlagValue(cmd, "assignment"),
				cmdFlagValue(cmd, "role"),
				cmdFlagValue(cmd, "workflow"),
			)
		},
	}
	cmd.Flags().String("root", ".", "site server root")
	cmd.Flags().String("session", "", "session id")
	cmd.Flags().String("assignment", "", "assignment id")
	cmd.Flags().String("role", "", "role")
	cmd.Flags().String("workflow", "", "workflow path inside release bundle")
	return cmd
}

func newSiteAssignNodeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "node",
		Short: "Override assignment for a specific node",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeSiteAssignNode(
				cmdFlagValue(cmd, "root"),
				cmdFlagValue(cmd, "session"),
				cmdFlagValue(cmd, "assignment"),
				cmdFlagValue(cmd, "node"),
				cmdFlagValue(cmd, "role"),
				cmdFlagValue(cmd, "workflow"),
			)
		},
	}
	cmd.Flags().String("root", ".", "site server root")
	cmd.Flags().String("session", "", "session id")
	cmd.Flags().String("assignment", "", "assignment id")
	cmd.Flags().String("node", "", "node id")
	cmd.Flags().String("role", "", "role (optional)")
	cmd.Flags().String("workflow", "", "workflow path inside release bundle")
	return cmd
}

func newSiteStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show release and session status summaries",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeSiteStatus(
				cmdFlagValue(cmd, "root"),
				cmdFlagValue(cmd, "output"),
			)
		},
	}
	cmd.Flags().String("root", ".", "site server root")
	cmd.Flags().StringP("output", "o", "text", "output format (text|json)")
	return cmd
}
