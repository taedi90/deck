package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

const (
	scenarioSourceLocal  = "local"
	scenarioSourceServer = "server"
	scenarioSourceAll    = "all"
)

type scenarioEntry struct {
	Name     string `json:"name"`
	Source   string `json:"source"`
	Workflow string `json:"workflow"`
}

func newListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available scenarios from local workflows or the saved remote server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			source, err := cmdFlagValue(cmd, "source")
			if err != nil {
				return err
			}
			output, err := cmdFlagValue(cmd, "output")
			if err != nil {
				return err
			}
			return executeList(cmd.Context(), strings.TrimSpace(source), strings.TrimSpace(output))
		},
	}
	cmd.Flags().String("source", scenarioSourceAll, "scenario source (local|server|all)")
	cmd.Flags().StringP("output", "o", "text", "output format (text|json)")
	registerScenarioSourceCompletion(cmd, "source", true)
	return cmd
}

func executeList(ctx context.Context, source, output string) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	resolvedSource, err := normalizeScenarioSource(source, true)
	if err != nil {
		return err
	}
	resolvedOutput, err := resolveOutputFormat(output)
	if err != nil {
		return err
	}
	if err := verbosef(1, "deck: list source=%s output=%s\n", resolvedSource, strings.TrimSpace(output)); err != nil {
		return err
	}

	entries, err := discoverScenarioEntries(ctx, resolvedSource)
	if err != nil {
		return err
	}
	if err := verbosef(1, "deck: list entries=%d\n", len(entries)); err != nil {
		return err
	}

	if resolvedOutput == "json" {
		enc := stdoutJSONEncoder()
		enc.SetIndent("", "  ")
		return enc.Encode(entries)
	}

	for _, entry := range entries {
		if err := stdoutPrintf("%s\t%s\t%s\n", entry.Source, entry.Name, entry.Workflow); err != nil {
			return err
		}
	}
	return nil
}

func discoverScenarioEntries(ctx context.Context, source string) ([]scenarioEntry, error) {
	entries := make([]scenarioEntry, 0)

	if source == scenarioSourceLocal || source == scenarioSourceAll {
		localEntries, err := discoverLocalScenarioEntries(".")
		if err != nil {
			if source != scenarioSourceAll {
				return nil, err
			}
			_ = verbosef(2, "deck: list local skipped error=%v\n", err)
		} else {
			entries = append(entries, localEntries...)
		}
	}

	if source == scenarioSourceServer || source == scenarioSourceAll {
		serverURL, _, err := resolveSourceURL("")
		switch {
		case err != nil:
			if source != scenarioSourceAll {
				return nil, err
			}
			_ = verbosef(2, "deck: list server skipped error=%v\n", err)
		case strings.TrimSpace(serverURL) != "":
			serverEntries, err := discoverServerScenarioEntries(ctx, serverURL)
			if err != nil {
				if source != scenarioSourceAll {
					return nil, err
				}
				_ = verbosef(2, "deck: list server lookup=%s error=%v\n", serverURL, err)
			} else {
				entries = append(entries, serverEntries...)
			}
		case source == scenarioSourceAll:
			_ = verbosef(2, "deck: list server skipped reason=no-remote\n")
		case source == scenarioSourceServer:
			return nil, errors.New("saved remote server URL is required; set one with \"deck server remote set <url>\"")
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Source != entries[j].Source {
			return entries[i].Source < entries[j].Source
		}
		if entries[i].Name != entries[j].Name {
			return entries[i].Name < entries[j].Name
		}
		return entries[i].Workflow < entries[j].Workflow
	})
	return entries, nil
}

func discoverLocalScenarioEntries(root string) ([]scenarioEntry, error) {
	resolvedRoot, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return nil, fmt.Errorf("resolve local workflow root: %w", err)
	}
	scenarioDir := filepath.Join(resolvedRoot, workflowRootDir, workflowScenariosDir)
	items := make([]scenarioEntry, 0)
	err = filepath.WalkDir(scenarioDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		name := strings.ToLower(d.Name())
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			return nil
		}
		rel, err := filepath.Rel(scenarioDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		items = append(items, scenarioEntry{
			Name:     strings.TrimSuffix(rel, filepath.Ext(rel)),
			Source:   scenarioSourceLocal,
			Workflow: path,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read local scenarios: %w", err)
	}
	return items, nil
}

func discoverServerScenarioEntries(ctx context.Context, serverURL string) ([]scenarioEntry, error) {
	items, err := fetchScenarioIndexFromServer(ctx, serverURL)
	if err != nil {
		return nil, err
	}
	entries := make([]scenarioEntry, 0, len(items))
	for _, item := range items {
		name, err := normalizeScenarioName(item)
		if err != nil {
			continue
		}
		entries = append(entries, scenarioEntry{
			Name:     name,
			Source:   scenarioSourceServer,
			Workflow: buildServerScenarioWorkflowURL(serverURL, name),
		})
	}
	return entries, nil
}

func normalizeScenarioSource(raw string, allowAll bool) (string, error) {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	if trimmed == "" {
		if allowAll {
			return scenarioSourceAll, nil
		}
		return scenarioSourceLocal, nil
	}
	valid := map[string]bool{scenarioSourceLocal: true, scenarioSourceServer: true}
	if allowAll {
		valid[scenarioSourceAll] = true
	}
	if !valid[trimmed] {
		if allowAll {
			return "", errors.New("--source must be local, server, or all")
		}
		return "", errors.New("--source must be local or server")
	}
	return trimmed, nil
}

func normalizeScenarioName(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimSuffix(trimmed, ".yaml")
	trimmed = strings.TrimSuffix(trimmed, ".yml")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		return "", errors.New("scenario name is required")
	}
	if strings.Contains(trimmed, "\\") || strings.Contains(trimmed, "..") {
		return "", fmt.Errorf("invalid scenario name: %s", raw)
	}
	return filepath.ToSlash(trimmed), nil
}

func resolveScenarioWorkflowReference(source, scenario string, localRoot string) (string, error) {
	resolvedScenario, err := normalizeScenarioName(scenario)
	if err != nil {
		return "", err
	}
	resolvedSource, err := normalizeScenarioSource(source, false)
	if err != nil {
		return "", err
	}
	if resolvedSource == scenarioSourceServer {
		serverURL, _, err := resolveRequiredSourceURL("")
		if err != nil {
			return "", err
		}
		return buildServerScenarioWorkflowURL(serverURL, resolvedScenario), nil
	}
	return resolveLocalScenarioPath(localRoot, resolvedScenario)
}

func resolveLocalScenarioPath(root, scenario string) (string, error) {
	resolvedRoot := strings.TrimSpace(root)
	if resolvedRoot == "" {
		resolvedRoot = "."
	}
	rootAbs, err := filepath.Abs(resolvedRoot)
	if err != nil {
		return "", fmt.Errorf("resolve local scenario root: %w", err)
	}
	scenarioDir := filepath.Join(rootAbs, workflowRootDir, workflowScenariosDir)
	for _, suffix := range []string{".yaml", ".yml"} {
		candidate := filepath.Join(scenarioDir, filepath.FromSlash(scenario)+suffix)
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("scenario not found under %s: %s", scenarioDir, scenario)
}

func buildServerScenarioWorkflowURL(serverURL, scenario string) string {
	return strings.TrimRight(strings.TrimSpace(serverURL), "/") + "/workflows/scenarios/" + strings.TrimLeft(scenario, "/") + ".yaml"
}

func fetchScenarioIndexFromServer(ctx context.Context, server string) ([]string, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}
	trimmed := strings.TrimRight(server, "/")
	indexURL := trimmed + "/workflows/index.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, indexURL, nil)
	if err != nil {
		return nil, fmt.Errorf("scenario list: build request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("scenario list: request failed: %w", err)
	}
	defer closeSilently(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return []string{}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scenario list: unexpected status %d", resp.StatusCode)
	}

	var items []string
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("scenario list: decode response: %w", err)
	}
	return items, nil
}

func registerScenarioSourceCompletion(cmd *cobra.Command, flagName string, allowAll bool) {
	_ = cmd.RegisterFlagCompletionFunc(flagName, func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		items := []string{scenarioSourceLocal, scenarioSourceServer}
		if allowAll {
			items = append(items, scenarioSourceAll)
		}
		return items, cobra.ShellCompDirectiveNoFileComp
	})
}

func registerScenarioNameCompletion(cmd *cobra.Command, flagName, sourceFlagName, localRootFlagName string, allowAll bool) {
	_ = cmd.RegisterFlagCompletionFunc(flagName, func(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		source := scenarioSourceLocal
		if sourceFlagName != "" {
			value := cmd.Flags().Lookup(sourceFlagName)
			if value != nil {
				source = value.Value.String()
			}
		}
		resolvedSource, err := normalizeScenarioSource(source, allowAll)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		localRoot := "."
		if localRootFlagName != "" {
			flag := cmd.Flags().Lookup(localRootFlagName)
			if flag != nil && strings.TrimSpace(flag.Value.String()) != "" {
				localRoot = strings.TrimSpace(flag.Value.String())
			}
		}

		items := completeScenarioNames(cmd.Context(), resolvedSource, localRoot, toComplete)
		return items, cobra.ShellCompDirectiveNoFileComp
	})
}

func completeScenarioNames(ctx context.Context, source, localRoot, toComplete string) []string {
	candidates := map[string]bool{}
	if source == scenarioSourceLocal || source == scenarioSourceAll {
		if entries, err := discoverLocalScenarioEntries(localRoot); err == nil {
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name, toComplete) {
					candidates[entry.Name] = true
				}
			}
		}
	}
	if source == scenarioSourceServer || source == scenarioSourceAll {
		if serverURL, _, err := resolveSourceURL(""); err == nil && strings.TrimSpace(serverURL) != "" {
			if entries, err := discoverServerScenarioEntries(ctx, serverURL); err == nil {
				for _, entry := range entries {
					if strings.HasPrefix(entry.Name, toComplete) {
						candidates[entry.Name] = true
					}
				}
			}
		}
	}
	items := make([]string, 0, len(candidates))
	for item := range candidates {
		items = append(items, item)
	}
	sort.Strings(items)
	return items
}
