package main

import "fmt"

func renderNodeHelp(args []string) (string, error) {
	if len(args) == 0 {
		return nodeHelpText(), nil
	}
	switch args[0] {
	case "id":
		return renderNodeIDHelp(args[1:])
	case "assignment":
		return renderNodeAssignmentHelp(args[1:])
	default:
		return "", fmt.Errorf("unknown help topic %q", "node "+args[0])
	}
}

func renderNodeIDHelp(args []string) (string, error) {
	if len(args) == 0 {
		return nodeIDHelpText(), nil
	}
	switch args[0] {
	case "show":
		return nodeIDShowHelpText(), nil
	case "set":
		return nodeIDSetHelpText(), nil
	case "init":
		return nodeIDInitHelpText(), nil
	default:
		return "", fmt.Errorf("unknown help topic %q", "node id "+args[0])
	}
}

func renderNodeAssignmentHelp(args []string) (string, error) {
	if len(args) == 0 {
		return nodeAssignmentHelpText(), nil
	}
	switch args[0] {
	case "show":
		return nodeAssignmentHelpText(), nil
	default:
		return "", fmt.Errorf("unknown help topic %q", "node assignment "+args[0])
	}
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
