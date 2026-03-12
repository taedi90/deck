package main

import "fmt"

func renderSiteHelp(args []string) (string, error) {
	if len(args) == 0 {
		return siteHelpText(), nil
	}
	switch args[0] {
	case "release":
		return renderSiteReleaseHelp(args[1:])
	case "session":
		return renderSiteSessionHelp(args[1:])
	case "assign":
		return renderSiteAssignHelp(args[1:])
	case "status":
		return siteStatusHelpText(), nil
	default:
		return "", fmt.Errorf("unknown help topic %q", "site "+args[0])
	}
}

func renderSiteReleaseHelp(args []string) (string, error) {
	if len(args) == 0 {
		return siteReleaseHelpText(), nil
	}
	switch args[0] {
	case "import":
		return siteReleaseImportHelpText(), nil
	case "list":
		return siteReleaseListHelpText(), nil
	default:
		return "", fmt.Errorf("unknown help topic %q", "site release "+args[0])
	}
}

func renderSiteSessionHelp(args []string) (string, error) {
	if len(args) == 0 {
		return siteSessionHelpText(), nil
	}
	switch args[0] {
	case "create":
		return siteSessionCreateHelpText(), nil
	case "close":
		return siteSessionCloseHelpText(), nil
	default:
		return "", fmt.Errorf("unknown help topic %q", "site session "+args[0])
	}
}

func renderSiteAssignHelp(args []string) (string, error) {
	if len(args) == 0 {
		return siteAssignHelpText(), nil
	}
	switch args[0] {
	case "role":
		return siteAssignRoleHelpText(), nil
	case "node":
		return siteAssignNodeHelpText(), nil
	default:
		return "", fmt.Errorf("unknown help topic %q", "site assign "+args[0])
	}
}

func siteHelpText() string {
	return formatHelp(
		"deck site <release|session|assign|status> [flags]",
		"Manage site releases, sessions, assignments, and status summaries.",
		helpSection{Title: "Commands", Lines: []string{
			"release      Import or list site releases",
			"session      Create or close sessions",
			"assign       Assign workflows by role or node",
			"status       Show release and session status summaries",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck site release import --id rel-001 --bundle ./bundle.tar",
			"deck site session create --id sess-001 --release rel-001",
			"deck site status --output json",
		}},
	)
}

func siteReleaseHelpText() string {
	return formatHelp(
		"deck site release <import|list> [flags]",
		"Import release bundles into the site store or list available releases.",
		helpSection{Title: "Commands", Lines: []string{
			"import      Import a bundle archive as a release",
			"list        List stored releases",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck site release import --id rel-001 --bundle ./bundle.tar",
			"deck site release list --output json",
		}},
	)
}

func siteReleaseImportHelpText() string {
	return formatHelp(
		"deck site release import --id <release-id> --bundle <bundle.tar> [--root <dir>] [--created-at <rfc3339>]",
		"Import a bundle archive into the site release store.",
		helpSection{Title: "Flags", Lines: []string{
			"--id          Release id to create",
			"--bundle      Local bundle archive path",
			"--root        Site server root directory",
			"--created-at  Release timestamp in RFC3339 format",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck site release import --id rel-001 --bundle ./bundle.tar",
			"deck site release import --root ./site --id rel-002 --bundle ./bundle.tar",
		}},
	)
}

func siteReleaseListHelpText() string {
	return formatHelp(
		"deck site release list [--root <dir>] [--output text|json]",
		"List site releases stored under the configured site root.",
		helpSection{Title: "Flags", Lines: []string{
			"--root        Site server root directory",
			"--output, -o  Output format: text or json",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck site release list",
			"deck site release list --root ./site --output json",
		}},
	)
}

func siteSessionHelpText() string {
	return formatHelp(
		"deck site session <create|close> [flags]",
		"Open or close site sessions for a release rollout.",
		helpSection{Title: "Commands", Lines: []string{
			"create      Create a new session for a release",
			"close       Close an existing session",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck site session create --id sess-001 --release rel-001",
			"deck site session close --id sess-001",
		}},
	)
}

func siteSessionCreateHelpText() string {
	return formatHelp(
		"deck site session create --id <session-id> --release <release-id> [--root <dir>] [--started-at <rfc3339>]",
		"Create an open session bound to an existing release.",
		helpSection{Title: "Flags", Lines: []string{
			"--id          Session id to create",
			"--release     Existing release id",
			"--root        Site server root directory",
			"--started-at  Session start timestamp in RFC3339 format",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck site session create --id sess-001 --release rel-001",
			"deck site session create --root ./site --id sess-002 --release rel-001",
		}},
	)
}

func siteSessionCloseHelpText() string {
	return formatHelp(
		"deck site session close --id <session-id> [--root <dir>] [--closed-at <rfc3339>]",
		"Close an existing site session.",
		helpSection{Title: "Flags", Lines: []string{
			"--id          Session id to close",
			"--root        Site server root directory",
			"--closed-at   Session close timestamp in RFC3339 format",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck site session close --id sess-001",
			"deck site session close --root ./site --id sess-001",
		}},
	)
}

func siteAssignHelpText() string {
	return formatHelp(
		"deck site assign <role|node> [flags]",
		"Create workflow assignments by role or by specific node id.",
		helpSection{Title: "Commands", Lines: []string{
			"role        Assign a workflow to a role for a session",
			"node        Override assignment for a specific node",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck site assign role --session sess-001 --assignment cp --role control-plane --workflow workflows/apply.yaml",
			"deck site assign node --session sess-001 --assignment node-01 --node rack-a-01 --workflow workflows/apply.yaml",
		}},
	)
}

func siteAssignRoleHelpText() string {
	return formatHelp(
		"deck site assign role --session <session-id> --assignment <assignment-id> --role <role> --workflow <path> [--root <dir>]",
		"Assign a workflow to all nodes matching a role within a session.",
		helpSection{Title: "Flags", Lines: []string{
			"--session     Session id",
			"--assignment  Assignment id",
			"--role        Role name",
			"--workflow    Relative workflow path inside the release bundle",
			"--root        Site server root directory",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck site assign role --session sess-001 --assignment cp --role control-plane --workflow workflows/apply.yaml",
			"deck site assign role --root ./site --session sess-001 --assignment worker --role worker --workflow workflows/apply.yaml",
		}},
	)
}

func siteAssignNodeHelpText() string {
	return formatHelp(
		"deck site assign node --session <session-id> --assignment <assignment-id> --node <node-id> --workflow <path> [--role <role>] [--root <dir>]",
		"Assign or override a workflow for a specific node in a session.",
		helpSection{Title: "Flags", Lines: []string{
			"--session     Session id",
			"--assignment  Assignment id",
			"--node        Node id",
			"--workflow    Relative workflow path inside the release bundle",
			"--role        Optional role label stored with the assignment",
			"--root        Site server root directory",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck site assign node --session sess-001 --assignment node-01 --node rack-a-01 --workflow workflows/apply.yaml",
			"deck site assign node --root ./site --session sess-001 --assignment node-02 --node rack-a-02 --role worker --workflow workflows/apply.yaml",
		}},
	)
}

func siteStatusHelpText() string {
	return formatHelp(
		"deck site status [--root <dir>] [--output text|json]",
		"Summarize releases, sessions, and per-node action status for a site store.",
		helpSection{Title: "Flags", Lines: []string{
			"--root        Site server root directory",
			"--output, -o  Output format: text or json",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck site status",
			"deck site status --root ./site --output json",
		}},
	)
}
