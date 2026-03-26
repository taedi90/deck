package mcpaugment

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/Airgap-Castaways/deck/internal/askconfig"
	"github.com/Airgap-Castaways/deck/internal/askcontext"
	"github.com/Airgap-Castaways/deck/internal/askintent"
	"github.com/Airgap-Castaways/deck/internal/askretrieve"
)

func Gather(ctx context.Context, cfg askconfig.MCP, route askintent.Route, prompt string) ([]askretrieve.Chunk, []string) {
	if !cfg.Enabled || len(cfg.Servers) == 0 {
		return nil, nil
	}
	chunks := make([]askretrieve.Chunk, 0)
	events := make([]string, 0)
	for _, server := range cfg.Servers {
		if strings.TrimSpace(server.RunCommand) == "" {
			continue
		}
		chunk, event := queryServer(ctx, server, route, prompt)
		if event != "" {
			events = append(events, event)
		}
		if chunk != nil {
			chunks = append(chunks, *chunk)
		}
	}
	return chunks, events
}

func queryServer(parent context.Context, server askconfig.MCPServer, route askintent.Route, prompt string) (*askretrieve.Chunk, string) {
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	tr := transport.NewStdio(server.RunCommand, nil, server.Args...)
	c := client.NewClient(tr)
	if err := c.Start(ctx); err != nil {
		return nil, fmt.Sprintf("mcp:%s start failed: %v", server.Name, err)
	}
	defer func() { _ = c.Close() }()
	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "deck-ask", Version: "1.0.0"}
	initReq.Params.Capabilities = mcp.ClientCapabilities{}
	if _, err := c.Initialize(ctx, initReq); err != nil {
		return nil, fmt.Sprintf("mcp:%s initialize failed: %v", server.Name, err)
	}
	tools, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return nil, fmt.Sprintf("mcp:%s list tools failed: %v", server.Name, err)
	}
	toolName := pickToolName(server.Name, route, prompt, tools)
	if toolName == "" {
		return nil, fmt.Sprintf("mcp:%s no known tool for route %s", server.Name, route)
	}
	args := callArgsForTool(toolName, prompt)
	result, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: toolName, Arguments: args}})
	if err != nil {
		return nil, fmt.Sprintf("mcp:%s call %s failed: %v", server.Name, toolName, err)
	}
	if result != nil && result.IsError {
		return nil, fmt.Sprintf("mcp:%s call %s returned tool error", server.Name, toolName)
	}
	if result == nil || len(result.Content) == 0 {
		return nil, fmt.Sprintf("mcp:%s call %s returned empty", server.Name, toolName)
	}
	b := &strings.Builder{}
	for _, content := range result.Content {
		text := strings.TrimSpace(mcp.GetTextFromContent(content))
		if text == "" {
			continue
		}
		b.WriteString(text)
		b.WriteString("\n")
	}
	content := strings.TrimSpace(b.String())
	if content == "" {
		return nil, fmt.Sprintf("mcp:%s call %s returned no text", server.Name, toolName)
	}
	return evidenceChunk(server.Name, toolName, content, prompt), fmt.Sprintf("mcp:%s call %s ok", server.Name, toolName)
}

func evidenceChunk(serverName string, toolName string, content string, prompt string) *askretrieve.Chunk {
	evidence := summarizeEvidence(content, prompt)
	if evidence != nil {
		content = renderEvidence(*evidence) + "\n\nSource excerpt:\n" + content
	}
	return &askretrieve.Chunk{
		ID:       "mcp-" + sanitize(serverName) + "-" + sanitize(toolName),
		Source:   "mcp",
		Label:    serverName + ":" + toolName,
		Topic:    askcontext.Topic("mcp:" + sanitize(serverName) + ":" + sanitize(toolName)),
		Content:  content,
		Score:    70,
		Evidence: evidence,
	}
}

func pickToolName(serverName string, route askintent.Route, prompt string, tools *mcp.ListToolsResult) string {
	if tools == nil {
		return ""
	}
	ordered := []string{"search", "web-search", "web_search", "resolve-library-id", "get-library-docs"}
	if strings.Contains(strings.ToLower(serverName), "context7") {
		ordered = context7ToolOrder(prompt)
	}
	allowed := routeAllowedTools(route)
	for _, candidate := range ordered {
		if len(allowed) > 0 && !allowed[strings.ToLower(candidate)] {
			continue
		}
		for _, tool := range tools.Tools {
			if strings.EqualFold(tool.Name, candidate) {
				return tool.Name
			}
		}
	}
	return ""
}

func context7ToolOrder(prompt string) []string {
	prompt = strings.ToLower(strings.TrimSpace(prompt))
	if prompt == "" {
		return []string{"search", "web-search", "web_search"}
	}
	libraryHints := []string{"library", "package", "module", "sdk", "api", "golang.org/", "github.com/", "npm", "pip", "crate"}
	for _, hint := range libraryHints {
		if strings.Contains(prompt, hint) {
			return []string{"resolve-library-id", "get-library-docs", "search", "web-search", "web_search"}
		}
	}
	return []string{"search", "web-search", "web_search", "get-library-docs"}
}

func routeAllowedTools(route askintent.Route) map[string]bool {
	switch route {
	case askintent.RouteQuestion, askintent.RouteExplain:
		return map[string]bool{"search": true, "web-search": true, "web_search": true, "resolve-library-id": true, "get-library-docs": true}
	case askintent.RouteDraft:
		return map[string]bool{"search": true, "web-search": true, "web_search": true, "resolve-library-id": true, "get-library-docs": true}
	default:
		return nil
	}
}

func callArgsForTool(name string, prompt string) map[string]any {
	switch strings.ToLower(name) {
	case "search", "web-search", "web_search":
		return map[string]any{"query": prompt, "limit": 3}
	case "resolve-library-id":
		return map[string]any{"libraryName": prompt}
	case "get-library-docs":
		return map[string]any{"topic": prompt, "tokens": 1800}
	default:
		return map[string]any{"query": prompt}
	}
}

func sanitize(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "-")
	value = strings.ReplaceAll(value, "/", "-")
	value = strings.ReplaceAll(value, "_", "-")
	return value
}

func summarizeEvidence(content string, prompt string) *askretrieve.EvidenceSummary {
	lower := strings.ToLower(strings.TrimSpace(content + "\n" + prompt))
	artifactKinds := []string{}
	addArtifact := func(kind string) {
		for _, existing := range artifactKinds {
			if existing == kind {
				return
			}
		}
		artifactKinds = append(artifactKinds, kind)
	}
	for _, token := range []string{"rpm", "package", "packages", "dnf", "apt"} {
		if strings.Contains(lower, token) {
			addArtifact("package")
			break
		}
	}
	for _, token := range []string{"image", "images", "registry", "container image"} {
		if strings.Contains(lower, token) {
			addArtifact("image")
			break
		}
	}
	for _, token := range []string{"binary", "tarball", "archive", "bundle"} {
		if strings.Contains(lower, token) {
			addArtifact("binary")
			break
		}
	}
	hints := []string{}
	if strings.Contains(lower, "air-gapped") || strings.Contains(lower, "offline") {
		hints = append(hints, "Treat gathered installation artifacts as offline bundle inputs for prepare before apply.")
	}
	if len(artifactKinds) == 0 && len(hints) == 0 {
		return nil
	}
	return &askretrieve.EvidenceSummary{ArtifactKinds: artifactKinds, OfflineHints: hints}
}

func renderEvidence(evidence askretrieve.EvidenceSummary) string {
	raw, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return "Typed MCP evidence JSON:\n{}"
	}
	return "Typed MCP evidence JSON:\n" + string(raw)
}
