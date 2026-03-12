package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/taedi90/deck/internal/nodeid"
	"os"
	"strings"
)

func runNode(args []string) error {
	if len(args) == 0 {
		return helpRequest{text: nodeHelpText()}
	}
	if wantsHelp(args) {
		text, err := renderNodeHelp(args[1:])
		if err != nil {
			return err
		}
		return helpRequest{text: text}
	}
	switch args[0] {
	case "id":
		return runNodeID(args[1:])
	case "assignment":
		return runNodeAssignment(args[1:])
	default:
		return fmt.Errorf("unknown node command %q", args[0])
	}
}

func runNodeID(args []string) error {
	if len(args) == 0 {
		return helpRequest{text: nodeIDHelpText()}
	}
	if wantsHelp(args) {
		text, err := renderNodeIDHelp(args[1:])
		if err != nil {
			return err
		}
		return helpRequest{text: text}
	}
	switch args[0] {
	case "show":
		return runNodeIDShow(args[1:])
	case "set":
		return runNodeIDSet(args[1:])
	case "init":
		return runNodeIDInit(args[1:])
	default:
		return fmt.Errorf("unknown node id command %q", args[0])
	}
}

func runNodeIDShow(args []string) error {
	fs := newHelpFlagSet("node id show")
	if err := parseFlags(fs, args, nodeIDShowHelpText()); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return helpRequest{text: nodeIDShowHelpText()}
	}

	result, err := nodeid.Resolve(resolveNodeIDPathsFromEnv())
	if err != nil {
		return err
	}
	printNodeIDResult(result)
	return nil
}

func runNodeIDSet(args []string) error {
	fs := newHelpFlagSet("node id set")
	if err := parseFlags(fs, args, nodeIDSetHelpText()); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return helpRequest{text: nodeIDSetHelpText()}
	}

	result, err := nodeid.SetOperator(resolveNodeIDPathsFromEnv(), strings.TrimSpace(fs.Arg(0)))
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "node id set: %s\n", result.ID)
	printNodeIDResult(result)
	return nil
}

func runNodeIDInit(args []string) error {
	fs := newHelpFlagSet("node id init")
	if err := parseFlags(fs, args, nodeIDInitHelpText()); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return helpRequest{text: nodeIDInitHelpText()}
	}

	result, err := nodeid.Init(resolveNodeIDPathsFromEnv())
	if err != nil {
		return err
	}
	if result.GeneratedCreated {
		fmt.Fprintln(os.Stdout, "node id init: created generated node-id")
	} else {
		fmt.Fprintln(os.Stdout, "node id init: generated node-id already exists")
	}
	printNodeIDResult(result)
	return nil
}

func resolveNodeIDPathsFromEnv() nodeid.Paths {
	paths := nodeid.DefaultPaths()
	if operatorPath := strings.TrimSpace(os.Getenv("DECK_NODE_ID_OPERATOR_PATH")); operatorPath != "" {
		paths.OperatorPath = operatorPath
	}
	if generatedPath := strings.TrimSpace(os.Getenv("DECK_NODE_ID_GENERATED_PATH")); generatedPath != "" {
		paths.GeneratedPath = generatedPath
	}
	return paths
}

func printNodeIDResult(result nodeid.Result) {
	fmt.Fprintf(os.Stdout, "node-id=%s\n", result.ID)
	fmt.Fprintf(os.Stdout, "source=%s\n", result.Source)
	fmt.Fprintf(os.Stdout, "hostname=%s\n", result.Hostname)
	if result.Mismatch {
		fmt.Fprintln(os.Stdout, "mismatch=true")
		fmt.Fprintf(os.Stdout, "operator-node-id=%s\n", result.OperatorID)
		fmt.Fprintf(os.Stdout, "generated-node-id=%s\n", result.GeneratedID)
	}
}

func runNodeAssignment(args []string) error {
	if len(args) == 0 {
		return helpRequest{text: nodeAssignmentHelpText()}
	}
	if wantsHelp(args) {
		text, err := renderNodeAssignmentHelp(args[1:])
		if err != nil {
			return err
		}
		return helpRequest{text: text}
	}
	switch args[0] {
	case "show":
		return runNodeAssignmentShow(args[1:])
	default:
		return fmt.Errorf("unknown node assignment command %q", args[0])
	}
}

func runNodeAssignmentShow(args []string) error {
	fs := newHelpFlagSet("node assignment show")
	root := fs.String("root", ".", "site server root")
	sessionID := fs.String("session", "", "session id")
	action := fs.String("action", "apply", "assignment action (diff|doctor|apply)")
	output := ""
	registerOutputFormatFlags(fs, &output, "text")
	if err := parseFlags(fs, args, nodeAssignmentHelpText()); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return helpRequest{text: nodeAssignmentHelpText()}
	}
	resolvedSessionID := strings.TrimSpace(*sessionID)
	if resolvedSessionID == "" {
		return errors.New("--session is required")
	}
	resolvedAction := strings.TrimSpace(*action)
	if resolvedAction != "diff" && resolvedAction != "doctor" && resolvedAction != "apply" {
		return errors.New("--action must be one of diff|doctor|apply")
	}
	if output != "text" && output != "json" {
		return errors.New("--output must be text or json")
	}

	result, err := nodeid.Resolve(resolveNodeIDPathsFromEnv())
	if err != nil {
		return err
	}
	st, _, err := newSiteStore(strings.TrimSpace(*root))
	if err != nil {
		return err
	}
	assignment, err := st.ResolveAssignment(resolvedSessionID, result.ID, resolvedAction)
	if err != nil {
		return err
	}
	if output == "json" {
		return json.NewEncoder(os.Stdout).Encode(assignment)
	}
	fmt.Fprintf(os.Stdout, "session=%s\n", assignment.SessionID)
	fmt.Fprintf(os.Stdout, "node-id=%s\n", assignment.NodeID)
	fmt.Fprintf(os.Stdout, "assignment=%s\n", assignment.ID)
	fmt.Fprintf(os.Stdout, "role=%s\n", assignment.Role)
	fmt.Fprintf(os.Stdout, "workflow=%s\n", assignment.Workflow)
	return nil
}
