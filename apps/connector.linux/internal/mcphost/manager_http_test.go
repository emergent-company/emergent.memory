package mcphost

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/emergent-company/memory.web-ui/connector/internal/toolreg"
)

// TestStartHostsStreamableHTTPServer is a hermetic end-to-end test: an
// in-process mcp-go MCP server is exposed over httptest (streamable HTTP), an
// mcphost http ServerConfig points at it, and the discovered tool must be
// registered under <serverName>_<toolName> and forward calls to the server.
// No external binaries or network access are involved.
func TestStartHostsStreamableHTTPServer(t *testing.T) {
	mcpServer := server.NewMCPServer("dummy", "1.0.0", server.WithToolCapabilities(false))
	mcpServer.AddTool(
		mcp.NewTool("greet", mcp.WithDescription("greet someone"), mcp.WithString("name", mcp.Description("who to greet"))),
		func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			name := ""
			if args, ok := req.Params.Arguments.(map[string]any); ok {
				name, _ = args["name"].(string)
			}
			return mcp.NewToolResultText("hello " + name), nil
		},
	)

	httpServer := server.NewStreamableHTTPServer(mcpServer)
	ts := httptest.NewServer(httpServer)
	defer ts.Close()

	reg := toolreg.New()
	m := NewManager([]ServerConfig{{Name: "dummy", Transport: TransportHTTP, URL: ts.URL}})
	if err := m.Start(context.Background(), reg, quietLogger()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Close()

	tool, ok := reg.Lookup("dummy_greet")
	if !ok {
		t.Fatalf("dummy_greet not registered; registered = %v", reg.List())
	}
	if tool.Description != "greet someone" {
		t.Errorf("description = %q, want %q", tool.Description, "greet someone")
	}
	if tool.InputSchema == nil {
		t.Fatal("input schema must be populated")
	}

	out, err := tool.Handler(context.Background(), map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	content, ok := out["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("content = %v, want one block", out["content"])
	}
	block, _ := content[0].(map[string]any)
	if block["text"] != "hello Ada" {
		t.Errorf("forwarded result text = %v, want %q", block["text"], "hello Ada")
	}
}

// TestStartHostsStreamableHTTPWithDisabledTool verifies per-server selection
// over a real transport: a disabled tool is never registered even though the
// server exposes it.
func TestStartHostsStreamableHTTPWithDisabledTool(t *testing.T) {
	mcpServer := server.NewMCPServer("dummy", "1.0.0", server.WithToolCapabilities(false))
	for _, name := range []string{"keep", "drop"} {
		name := name
		mcpServer.AddTool(mcp.NewTool(name), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText(name), nil
		})
	}
	httpServer := server.NewStreamableHTTPServer(mcpServer)
	ts := httptest.NewServer(httpServer)
	defer ts.Close()

	reg := toolreg.New()
	m := NewManager([]ServerConfig{{
		Name:          "dummy",
		Transport:     TransportHTTP,
		URL:           ts.URL,
		DisabledTools: []string{"drop"},
	}})
	if err := m.Start(context.Background(), reg, quietLogger()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Close()

	if _, ok := reg.Lookup("dummy_keep"); !ok {
		t.Error("dummy_keep must be registered")
	}
	if _, ok := reg.Lookup("dummy_drop"); ok {
		t.Error("dummy_drop is disabled and must not be registered")
	}
}
