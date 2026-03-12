package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

type cliResult struct {
	stdout   string
	stderr   string
	err      error
	exitCode int
}

type helpRequest struct {
	text string
}

func (h helpRequest) Error() string {
	return h.text
}

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
			return helpRequest{text: helpText}
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

func renderHelp(args []string) (string, error) {
	if len(args) == 0 {
		return mainHelpText(), nil
	}

	switch args[0] {
	case "pack":
		return packHelpText(), nil
	case "apply":
		return applyHelpText(), nil
	case "diff":
		return diffHelpText(), nil
	case "doctor":
		return doctorHelpText(), nil
	case "serve":
		return serveHelpText(), nil
	case "list":
		return listHelpText(), nil
	case "validate":
		return validateHelpText(), nil
	case "health":
		return healthHelpText(), nil
	case "logs":
		return logsHelpText(), nil
	case "init":
		return initHelpText(), nil
	case "bundle":
		return renderBundleHelp(args[1:])
	case "cache":
		return renderCacheHelp(args[1:])
	case "node":
		return renderNodeHelp(args[1:])
	case "site":
		return renderSiteHelp(args[1:])
	default:
		return "", fmt.Errorf("unknown help topic %q", args[0])
	}
}

func helpResult(text string) cliResult {
	return cliResult{stdout: text + "\n", exitCode: 0}
}

func errorResult(err error) cliResult {
	if err == nil {
		return cliResult{}
	}
	if helpErr, ok := err.(helpRequest); ok {
		return helpResult(helpErr.text)
	}
	if code, ok := extractExitCode(err); ok {
		return cliResult{err: err, exitCode: code}
	}
	return cliResult{err: err, exitCode: 1}
}

func writeResult(res cliResult) error {
	if res.stdout != "" {
		if _, err := fmt.Fprint(os.Stdout, res.stdout); err != nil {
			return err
		}
	}
	if res.stderr != "" {
		if _, err := fmt.Fprint(os.Stderr, res.stderr); err != nil {
			return err
		}
	}
	return res.err
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
		helpSection{Title: "Input rules", Lines: []string{
			"Use --file to pass an explicit workflow path or URL.",
			"Without --file, the first positional argument is the workflow and the second is the bundle root.",
			"In assisted mode, both --server and --session are required and local workflow path discovery is skipped.",
		}},
		helpSection{Title: "Flags", Lines: []string{
			"--file, -f    Path or URL to the apply file",
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
		helpSection{Title: "Input rules", Lines: []string{
			"Use --file to point at a local or remote workflow file.",
			"In assisted mode, both --server and --session are required and the workflow comes from the assigned release.",
			"--out is always required because doctor writes a report file.",
		}},
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
