package mcpaugment

import (
	"context"
	"testing"

	"github.com/taedi90/deck/internal/askconfig"
	"github.com/taedi90/deck/internal/askintent"
)

func TestGatherDisabledReturnsNothing(t *testing.T) {
	chunks, events := Gather(context.Background(), askconfig.MCP{}, askintent.RouteExplain, "explain apply")
	if len(chunks) != 0 || len(events) != 0 {
		t.Fatalf("expected disabled mcp gather to return nothing, got chunks=%v events=%v", chunks, events)
	}
}

func TestRouteAllowedTools(t *testing.T) {
	if !routeAllowedTools(askintent.RouteExplain)["get-library-docs"] {
		t.Fatalf("expected explain route to allow docs tool")
	}
	if routeAllowedTools(askintent.RouteReview) != nil {
		t.Fatalf("expected review route to avoid external mcp tools by default")
	}
}
