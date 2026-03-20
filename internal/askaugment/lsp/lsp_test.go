package lspaugment

import (
	"context"
	"testing"

	"github.com/taedi90/deck/internal/askconfig"
	"github.com/taedi90/deck/internal/askintent"
	"github.com/taedi90/deck/internal/askretrieve"
)

func TestGatherDisabledReturnsNothing(t *testing.T) {
	chunks, events := Gather(context.Background(), askconfig.LSP{}, askintent.Target{}, askretrieve.WorkspaceSummary{})
	if len(chunks) != 0 || len(events) != 0 {
		t.Fatalf("expected disabled lsp gather to return nothing, got chunks=%v events=%v", chunks, events)
	}
}

func TestCollectYAMLDiagnosticsTargetsSelectedFile(t *testing.T) {
	workspace := askretrieve.WorkspaceSummary{Files: []askretrieve.WorkspaceFile{
		{Path: "workflows/scenarios/apply.yaml", Content: "version: ["},
		{Path: "workflows/components/valid.yaml", Content: "steps: []\n"},
	}}
	diag := collectYAMLDiagnostics(askintent.Target{Path: "workflows/scenarios/apply.yaml"}, workspace)
	if diag == "" {
		t.Fatalf("expected yaml diagnostics for targeted invalid file")
	}
	if collectYAMLDiagnostics(askintent.Target{Path: "workflows/components/valid.yaml"}, workspace) != "" {
		t.Fatalf("expected no diagnostics for valid targeted file")
	}
}
