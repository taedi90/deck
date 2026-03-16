package main

import (
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/taedi90/deck/internal/nodeid"
)

func executeNodeIDShow() error {
	result, err := nodeid.Resolve(resolveNodeIDPathsFromEnv())
	if err != nil {
		return err
	}
	return printNodeIDResult(result)
}

func executeNodeIDSet(id string) error {
	result, err := nodeid.SetOperator(resolveNodeIDPathsFromEnv(), strings.TrimSpace(id))
	if err != nil {
		return err
	}
	if err := stdoutPrintf("node id set: %s\n", result.ID); err != nil {
		return err
	}
	return printNodeIDResult(result)
}

func executeNodeIDInit() error {
	result, err := nodeid.Init(resolveNodeIDPathsFromEnv())
	if err != nil {
		return err
	}
	if result.GeneratedIDNew {
		if err := stdoutPrintln("node id init: created generated node-id"); err != nil {
			return err
		}
	} else {
		if err := stdoutPrintln("node id init: generated node-id already exists"); err != nil {
			return err
		}
	}
	return printNodeIDResult(result)
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

func printNodeIDResult(result nodeid.Result) error {
	if err := stdoutPrintf("node-id=%s\n", result.ID); err != nil {
		return err
	}
	if err := stdoutPrintf("source=%s\n", result.Source); err != nil {
		return err
	}
	if err := stdoutPrintf("hostname=%s\n", result.Hostname); err != nil {
		return err
	}
	if result.Mismatch {
		if err := stdoutPrintln("mismatch=true"); err != nil {
			return err
		}
		if err := stdoutPrintf("operator-node-id=%s\n", result.OperatorID); err != nil {
			return err
		}
		if err := stdoutPrintf("generated-node-id=%s\n", result.GeneratedID); err != nil {
			return err
		}
	}
	return nil
}

func executeNodeAssignmentShow(root string, sessionID string, action string, output string) error {
	resolvedSessionID := strings.TrimSpace(sessionID)
	if resolvedSessionID == "" {
		return errors.New("--session is required")
	}
	resolvedAction := strings.TrimSpace(action)
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
	st, _, err := newSiteStore(strings.TrimSpace(root))
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
	if err := stdoutPrintf("session=%s\n", assignment.SessionID); err != nil {
		return err
	}
	if err := stdoutPrintf("node-id=%s\n", assignment.NodeID); err != nil {
		return err
	}
	if err := stdoutPrintf("assignment=%s\n", assignment.ID); err != nil {
		return err
	}
	if err := stdoutPrintf("role=%s\n", assignment.Role); err != nil {
		return err
	}
	return stdoutPrintf("workflow=%s\n", assignment.Workflow)
}
