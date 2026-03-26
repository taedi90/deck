package askretrieve

import (
	"strings"
	"testing"

	"github.com/Airgap-Castaways/deck/internal/askintent"
	"github.com/Airgap-Castaways/deck/internal/askstate"
)

func TestRetrieveUsesRouteAwareStepGuidance(t *testing.T) {
	prompt := "create an air-gapped rhel9 single-node kubeadm workflow using typed steps where possible"
	draft := Retrieve(askintent.RouteDraft, prompt, askintent.Target{}, WorkspaceSummary{}, askstate.Context{}, nil)
	review := Retrieve(askintent.RouteReview, prompt, askintent.Target{}, WorkspaceSummary{}, askstate.Context{}, nil)
	draftChunk := findChunk(draft, "typed-steps-draft")
	reviewChunk := findChunk(review, "typed-steps-review")
	if draftChunk == nil || reviewChunk == nil {
		t.Fatalf("expected route-specific typed-step chunks, got draft=%v review=%v", draft.Chunks, review.Chunks)
	}
	if !strings.Contains(draftChunk.Content, "example") || !strings.Contains(draftChunk.Content, "mistake:") {
		t.Fatalf("expected draft chunk to include examples and mistakes, got %q", draftChunk.Content)
	}
	if strings.Contains(reviewChunk.Content, "mistake:") || strings.Contains(reviewChunk.Content, "example") {
		t.Fatalf("expected review chunk to stay summary-focused, got %q", reviewChunk.Content)
	}
}

func TestRepairChunksStayFixFocused(t *testing.T) {
	chunks := RepairChunks("create an air-gapped rhel9 kubeadm workflow using typed steps where possible", "E_SCHEMA_INVALID: step check-rhel9-host (CheckHost): spec: checks is required; spec: Additional property os is not allowed")
	if len(chunks) != 1 {
		t.Fatalf("expected one repair chunk, got %d", len(chunks))
	}
	content := chunks[0].Content
	for _, want := range []string{"Relevant repair guidance:", "CheckHost", "spec.checks", "spec.os"} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %q in repair chunk, got %q", want, content)
		}
	}
	if strings.Contains(content, "LoadImage") || strings.Contains(content, "CheckCluster") {
		t.Fatalf("expected repair chunk to stay narrow and fix-focused, got %q", content)
	}
}

func TestRetrievePrefersTypedGuidanceOverCommandForKubeadmPrompt(t *testing.T) {
	result := Retrieve(askintent.RouteDraft, "create an air-gapped rhel9 single-node kubeadm workflow using typed steps where possible", askintent.Target{}, WorkspaceSummary{}, askstate.Context{}, nil)
	chunk := findChunk(result, "typed-steps-draft")
	if chunk == nil {
		t.Fatalf("expected typed-steps chunk, got %#v", result.Chunks)
	}
	for _, want := range []string{"CheckHost", "LoadImage", "CheckCluster"} {
		if !strings.Contains(chunk.Content, want) {
			t.Fatalf("expected %q in typed guidance chunk, got %q", want, chunk.Content)
		}
	}
	if strings.Contains(chunk.Content, "- Command:") && strings.Index(chunk.Content, "Command") < strings.Index(chunk.Content, "CheckHost") {
		t.Fatalf("expected typed steps to outrank Command, got %q", chunk.Content)
	}
}

func TestRetrieveIncludesStructuredMCPEvidenceChunk(t *testing.T) {
	result := Retrieve(askintent.RouteDraft, "create an air-gapped package workflow", askintent.Target{}, WorkspaceSummary{}, askstate.Context{}, []Chunk{{ID: "mcp-doc", Source: "mcp", Label: "context7:search", Topic: "mcp:context7:search", Content: "Typed MCP evidence JSON:\n{\n  \"artifactKinds\": [\"package\"],\n  \"offlineHints\": [\"Treat gathered installation artifacts as offline bundle inputs for prepare before apply.\"]\n}\n\nSource excerpt:\nDownload rpm packages before offline installation.", Score: 70, Evidence: &EvidenceSummary{ArtifactKinds: []string{"package"}, OfflineHints: []string{"Treat gathered installation artifacts as offline bundle inputs for prepare before apply."}}}})
	var found bool
	for _, chunk := range result.Chunks {
		if chunk.Source == "mcp" && strings.Contains(chunk.Content, "Typed MCP evidence JSON:") {
			if chunk.Evidence == nil || len(chunk.Evidence.ArtifactKinds) == 0 || chunk.Evidence.ArtifactKinds[0] != "package" {
				t.Fatalf("expected typed evidence summary, got %#v", chunk)
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected structured mcp evidence chunk, got %#v", result.Chunks)
	}
}

func findChunk(result RetrievalResult, id string) *Chunk {
	for i := range result.Chunks {
		if result.Chunks[i].ID == id {
			return &result.Chunks[i]
		}
	}
	return nil
}
