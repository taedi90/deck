package main

import (
	"errors"
	"flag"
	"io"
	"strings"
)

type helpSection struct {
	Title string
	Lines []string
}

func wantsHelp(args []string) bool {
	if len(args) == 0 {
		return false
	}
	return args[0] == "-h" || args[0] == "--help" || args[0] == "help"
}

func newHelpFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	return fs
}

func parseFlags(fs *flag.FlagSet, args []string, helpText string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return errors.New(helpText)
		}
		return err
	}
	return nil
}

func formatHelp(usage string, what string, sections ...helpSection) string {
	var b strings.Builder
	b.WriteString("Usage:\n  ")
	b.WriteString(usage)
	b.WriteString("\n\n")
	b.WriteString(what)
	for _, section := range sections {
		if len(section.Lines) == 0 {
			continue
		}
		b.WriteString("\n\n")
		b.WriteString(section.Title)
		b.WriteString(":\n")
		for _, line := range section.Lines {
			b.WriteString("  ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func mainHelpText() string {
	return formatHelp(
		"deck <command> [flags]",
		"Run deck bundle, apply, site, and server commands.",
		helpSection{Title: "Commands", Lines: []string{
			"pack       Build an offline bundle from local deck files",
			"apply      Execute an apply file against a bundle",
			"serve      Start the bundle server",
			"bundle     Verify, inspect, import, collect, or merge bundles",
			"list       List available deck files from a local bundle or server",
			"validate   Validate a deck file",
			"diff       Show the planned install step execution",
			"doctor     Check referenced artifact inputs before apply",
			"health     Probe a running deck server",
			"logs       Read server audit logs from file or journal",
			"cache      Inspect or clean deck cache data",
			"node       Resolve or manage node identity data",
			"site       Manage site releases, sessions, and assignments",
			"init       Scaffold starter deck files",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck pack --out ./bundle.tar",
			"deck apply ./apply.yaml ./bundle",
			"deck serve --root ./bundle --addr :8080",
		}},
	)
}

func packHelpText() string {
	return formatHelp(
		"deck pack --out <bundle.tar> [--var key=value] [--cache-dir <dir>] [--no-cache]",
		"Build an offline bundle from workflows in ./workflows.",
		helpSection{Title: "Required files", Lines: []string{
			"workflows/pack.yaml",
			"workflows/apply.yaml",
			"workflows/vars.yaml",
		}},
		helpSection{Title: "Flags", Lines: []string{
			"--out         Output bundle tar path",
			"--dry-run     Print the planned bundle contents without writing files",
			"--cache-dir   Reuse downloaded artifacts from this directory",
			"--no-cache    Force redownload of artifacts",
			"--var         Set a workflow variable override (repeatable)",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck pack --out ./bundle.tar",
			"deck pack --out ./bundle.tar --var kubeVersion=v1.31.0",
			"deck pack --out ./bundle.tar --cache-dir ~/.deck/cache/artifacts",
		}},
	)
}

func applyHelpText() string {
	return formatHelp(
		"deck apply [workflow] [bundle] [--phase <name>] [--prefetch] [--dry-run] [--var key=value]",
		"Run an apply workflow, optionally prefetching DownloadFile steps first.",
		helpSection{Title: "Flags", Lines: []string{
			"--file, -f    Path or URL to the workflow file",
			"--phase       Phase name to execute (default: install)",
			"--prefetch    Run DownloadFile steps before the selected phase",
			"--dry-run     Print the planned install step execution without applying",
			"--var         Set a workflow variable override (repeatable)",
			"--server      Run in assisted mode against a site server",
			"--session     Session id for assisted mode",
			"--api-token   Bearer token for assisted mode APIs",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck apply ./workflows/apply.yaml ./bundle",
			"deck apply --file https://example.invalid/workflows/apply.yaml --dry-run",
			"deck apply ./bundle --prefetch --var joinFile=/tmp/join.txt",
		}},
	)
}

func diffHelpText() string {
	return formatHelp(
		"deck diff --file <workflow> [--phase <name>] [--output text|json] [--var key=value]",
		"Show which install steps would run, skip, or reuse from saved state.",
		helpSection{Title: "Flags", Lines: []string{
			"--file, -f    Path to the workflow file",
			"--phase       Phase name to diff (default: install)",
			"--output      Output format: text or json",
			"--var         Set a workflow variable override (repeatable)",
			"--server      Run in assisted mode against a site server",
			"--session     Session id for assisted mode",
			"--api-token   Bearer token for assisted mode APIs",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck diff --file ./workflows/apply.yaml",
			"deck diff --file ./workflows/apply.yaml --output json",
			"deck diff --file ./workflows/apply.yaml --var region=lab",
		}},
	)
}

func doctorHelpText() string {
	return formatHelp(
		"deck doctor --file <workflow> --out <report.json> [--var key=value]",
		"Check workflow artifact references and write a doctor report before apply.",
		helpSection{Title: "Flags", Lines: []string{
			"--file, -f    Path or URL to the workflow file",
			"--out         Output report path",
			"--var         Set a workflow variable override (repeatable)",
			"--server      Run in assisted mode against a site server",
			"--session     Session id for assisted mode",
			"--api-token   Bearer token for assisted mode APIs",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck doctor --file ./workflows/apply.yaml --out ./doctor.json",
			"deck doctor --file ./workflows/apply.yaml --out ./doctor.json --var registry=repo.local",
		}},
	)
}

func serveHelpText() string {
	return formatHelp(
		"deck serve --root <dir> --addr <host:port> [--api-token <token>] [--tls-cert <crt> --tls-key <key> | --tls-self-signed]",
		"Start the deck content server for workflows, artifacts, registry access, and site APIs.",
		helpSection{Title: "Flags", Lines: []string{
			"--root                Server content root (default: ./bundle)",
			"--addr                Listen address (default: :8080)",
			"--api-token           Bearer token required for /api/site/v1 endpoints",
			"--report-max          Max retained in-memory reports",
			"--audit-max-size-mb   Max audit log size before rotation",
			"--audit-max-files     Max retained rotated audit files",
			"--tls-cert            TLS certificate path",
			"--tls-key             TLS private key path",
			"--tls-self-signed     Generate and use a self-signed certificate",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck serve --root ./bundle --addr :8080",
			"deck serve --root ./bundle --addr :8443 --tls-self-signed",
			"deck serve --root ./bundle --addr :8443 --tls-cert server.crt --tls-key server.key",
		}},
	)
}

func listHelpText() string {
	return formatHelp(
		"deck list [--server <url>] [--output text|json]",
		"List workflows from the local ./workflows directory or from a running server index.",
		helpSection{Title: "Flags", Lines: []string{
			"--server      Server URL for workflow index lookup",
			"--output, -o  Output format: text or json",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck list",
			"deck list --server http://127.0.0.1:8080",
			"deck list --server http://127.0.0.1:8080 --output json",
		}},
	)
}

func validateHelpText() string {
	return formatHelp(
		"deck validate --file <workflow> [--var key=value]",
		"Validate a workflow file against the deck schema and step schemas.",
		helpSection{Title: "Flags", Lines: []string{
			"--file, -f    Path or URL to the workflow file",
			"--var         Set a workflow variable override (repeatable)",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck validate --file ./workflows/apply.yaml",
			"deck validate --file https://example.invalid/workflows/apply.yaml",
		}},
	)
}

func initHelpText() string {
	return formatHelp(
		"deck init [--out <dir>]",
		"Create starter workflow files under a workflows directory.",
		helpSection{Title: "Flags", Lines: []string{
			"--out         Output directory root (default: .)",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck init",
			"deck init --out ./demo",
		}},
	)
}

func healthHelpText() string {
	return formatHelp(
		"deck health --server <url>",
		"Probe the /healthz endpoint of a running deck server.",
		helpSection{Title: "Flags", Lines: []string{
			"--server      Server base URL to probe",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck health --server http://127.0.0.1:8080",
			"deck health --server https://site.example",
		}},
	)
}

func logsHelpText() string {
	return formatHelp(
		"deck logs [--root <dir>] [--source file|journal|both] [--path <file>] [--unit <service>] [--output text|json]",
		"Read deck server audit logs from the log file, the journal, or both.",
		helpSection{Title: "Flags", Lines: []string{
			"--root        Serve root directory used to resolve the default log path",
			"--source      Log source: file, journal, or both",
			"--path        Explicit audit log file path",
			"--unit        Systemd unit name for journal logs",
			"--output, -o  Output format: text or json",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck logs --source file --root ./bundle",
			"deck logs --source journal --unit deck-server.service",
			"deck logs --source both --root ./bundle --unit deck-server.service --output json",
		}},
	)
}

func cacheHelpText() string {
	return formatHelp(
		"deck cache <list|clean> [flags]",
		"Inspect or delete cached deck artifacts under the local deck cache root.",
		helpSection{Title: "Commands", Lines: []string{
			"list        Show cached files",
			"clean       Delete cached entries, optionally by age",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck cache list",
			"deck cache clean --older-than 30d --dry-run",
		}},
	)
}

func nodeHelpText() string {
	return formatHelp(
		"deck node <id|assignment> [flags]",
		"Resolve node identity data or inspect the site assignment chosen for this node.",
		helpSection{Title: "Commands", Lines: []string{
			"id           Show, set, or initialize the local node id",
			"assignment   Show the resolved site assignment for this node",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck node id show",
			"deck node id set rack-a-01",
			"deck node assignment show --session bootstrap-001",
		}},
	)
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

func bundleHelpText() string {
	return formatHelp(
		"deck bundle <verify|inspect|import|collect|merge> [flags]",
		"Inspect or move deck bundles between directories and tar archives.",
		helpSection{Title: "Commands", Lines: []string{
			"verify       Verify bundle manifest integrity",
			"inspect      List manifest entries in a bundle",
			"import       Extract a bundle archive into a directory",
			"collect      Create a bundle archive from a directory",
			"merge        Merge a bundle archive into a destination directory",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck bundle verify ./bundle.tar",
			"deck bundle inspect ./bundle --output json",
			"deck bundle merge ./bundle.tar --to ./dest --dry-run",
		}},
	)
}

func cacheListHelpText() string {
	return formatHelp(
		"deck cache list [--output text|json]",
		"List cached files under the default deck cache root.",
		helpSection{Title: "Flags", Lines: []string{
			"--output, -o  Output format: text or json",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck cache list",
			"deck cache list --output json",
		}},
	)
}

func cacheCleanHelpText() string {
	return formatHelp(
		"deck cache clean [--older-than <duration>] [--dry-run]",
		"Delete cached entries, optionally filtering by last modification age.",
		helpSection{Title: "Flags", Lines: []string{
			"--older-than  Delete entries older than a duration such as 30d or 24h",
			"--dry-run     Print the deletion plan without removing files",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck cache clean --dry-run",
			"deck cache clean --older-than 30d",
		}},
	)
}

func nodeIDHelpText() string {
	return formatHelp(
		"deck node id <show|set|init>",
		"Show, set, or initialize the node id files used by deck.",
		helpSection{Title: "Commands", Lines: []string{
			"show        Print the resolved node id and source",
			"set         Write the operator node id",
			"init        Generate a node id if one is missing",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck node id show",
			"deck node id set rack-a-01",
			"deck node id init",
		}},
	)
}

func nodeIDShowHelpText() string {
	return formatHelp(
		"deck node id show",
		"Print the resolved node id, source, and hostname.",
		helpSection{Title: "Flags", Lines: []string{"(none)"}},
		helpSection{Title: "Examples", Lines: []string{"deck node id show"}},
	)
}

func nodeIDSetHelpText() string {
	return formatHelp(
		"deck node id set <node-id>",
		"Write the operator-managed node id value.",
		helpSection{Title: "Flags", Lines: []string{"(none)"}},
		helpSection{Title: "Examples", Lines: []string{"deck node id set rack-a-01"}},
	)
}

func nodeIDInitHelpText() string {
	return formatHelp(
		"deck node id init",
		"Generate a node id when the generated node-id file is missing.",
		helpSection{Title: "Flags", Lines: []string{"(none)"}},
		helpSection{Title: "Examples", Lines: []string{"deck node id init"}},
	)
}

func nodeAssignmentHelpText() string {
	return formatHelp(
		"deck node assignment show --session <session-id> [--action diff|doctor|apply] [--root <dir>] [--output text|json]",
		"Show the site assignment selected for the current node id.",
		helpSection{Title: "Flags", Lines: []string{
			"--session     Session id to resolve against",
			"--action      Assignment action: diff, doctor, or apply",
			"--root        Site server root directory",
			"--output, -o  Output format: text or json",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck node assignment show --session sess-001",
			"deck node assignment show --session sess-001 --action doctor --output json",
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

func bundleVerifyHelpText() string {
	return formatHelp(
		"deck bundle verify <path>",
		"Verify manifest integrity for a bundle directory or bundle tar archive.",
		helpSection{Title: "Flags", Lines: []string{"--file        Bundle path as an alternative to the positional argument"}},
		helpSection{Title: "Examples", Lines: []string{"deck bundle verify ./bundle.tar", "deck bundle verify --file ./bundle"}},
	)
}

func bundleInspectHelpText() string {
	return formatHelp(
		"deck bundle inspect <path> [--output text|json]",
		"List manifest entries for a bundle directory or bundle archive.",
		helpSection{Title: "Flags", Lines: []string{
			"--file        Bundle path as an alternative to the positional argument",
			"--output, -o  Output format: text or json",
		}},
		helpSection{Title: "Examples", Lines: []string{"deck bundle inspect ./bundle", "deck bundle inspect ./bundle.tar --output json"}},
	)
}

func bundleImportHelpText() string {
	return formatHelp(
		"deck bundle import --file <bundle.tar> --dest <dir>",
		"Extract a bundle tar archive into a destination directory.",
		helpSection{Title: "Flags", Lines: []string{
			"--file        Bundle archive path",
			"--dest        Destination directory",
		}},
		helpSection{Title: "Examples", Lines: []string{"deck bundle import --file ./bundle.tar --dest ./bundle"}},
	)
}

func bundleCollectHelpText() string {
	return formatHelp(
		"deck bundle collect --root <dir> --out <bundle.tar>",
		"Create a bundle tar archive from an unpacked bundle directory.",
		helpSection{Title: "Flags", Lines: []string{
			"--root        Bundle directory",
			"--out         Output archive path",
		}},
		helpSection{Title: "Examples", Lines: []string{"deck bundle collect --root ./bundle --out ./bundle.tar"}},
	)
}

func bundleMergeHelpText() string {
	return formatHelp(
		"deck bundle merge <bundle.tar> --to <dir> [--dry-run]",
		"Merge the contents of a bundle archive into a destination directory.",
		helpSection{Title: "Flags", Lines: []string{
			"--to          Merge destination directory",
			"--dry-run     Print the merge plan without writing files",
		}},
		helpSection{Title: "Examples", Lines: []string{"deck bundle merge ./bundle.tar --to ./dest", "deck bundle merge ./bundle.tar --to ./dest --dry-run"}},
	)
}
