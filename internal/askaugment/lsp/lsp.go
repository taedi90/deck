package lspaugment

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/taedi90/deck/internal/askconfig"
	"github.com/taedi90/deck/internal/askcontext"
	"github.com/taedi90/deck/internal/askintent"
	"github.com/taedi90/deck/internal/askretrieve"
)

func Gather(ctx context.Context, cfg askconfig.LSP, target askintent.Target, workspace askretrieve.WorkspaceSummary) ([]askretrieve.Chunk, []string) {
	if !cfg.Enabled {
		return nil, nil
	}
	events := make([]string, 0)
	if cmd := strings.TrimSpace(cfg.YAML.RunCommand); cmd != "" {
		//nolint:gosec // LSP command is explicitly configured by the local user.
		probe := exec.CommandContext(ctx, cmd, "--version")
		if err := probe.Run(); err != nil {
			events = append(events, fmt.Sprintf("lsp yaml command probe failed: %v", err))
		} else {
			events = append(events, "lsp yaml command available")
		}
	}
	diag := collectYAMLDiagnostics(target, workspace)
	if diag == "" {
		return nil, events
	}
	chunk := askretrieve.Chunk{
		ID:      "lsp-yaml-diagnostics",
		Source:  "lsp",
		Label:   "yaml-diagnostics",
		Topic:   askcontext.Topic("lsp:yaml-diagnostics"),
		Content: diag,
		Score:   65,
	}
	return []askretrieve.Chunk{chunk}, events
}

func collectYAMLDiagnostics(target askintent.Target, workspace askretrieve.WorkspaceSummary) string {
	b := &strings.Builder{}
	for _, file := range workspace.Files {
		if !strings.HasSuffix(strings.ToLower(file.Path), ".yaml") && !strings.HasSuffix(strings.ToLower(file.Path), ".yml") {
			continue
		}
		if target.Path != "" {
			if filepath.ToSlash(file.Path) != filepath.ToSlash(target.Path) {
				continue
			}
		}
		var node yaml.Node
		if err := yaml.Unmarshal([]byte(file.Content), &node); err != nil {
			b.WriteString(file.Path)
			b.WriteString(": ")
			b.WriteString(err.Error())
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}
