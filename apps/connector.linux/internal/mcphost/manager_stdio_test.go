package mcphost

import (
	"context"
	"os"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/emergent-company/memory.web-ui/connector/internal/toolreg"
)

// stdioHelperEnv marks the re-executed test binary as the MCP stdio server.
const stdioHelperEnv = "MCPHOST_STDIO_HELPER"

// TestStdioHelperProcess is not a real test: when the manager launches this
// test binary as a stdio MCP server (env set) it serves one tool and exits. In
// the normal `go test` run the guard skips it.
func TestStdioHelperProcess(t *testing.T) {
	if os.Getenv(stdioHelperEnv) != "1" {
		t.Skip("stdio helper process: only runs as a subprocess")
	}
	mcpServer := server.NewMCPServer("helper", "1.0.0", server.WithToolCapabilities(false))
	mcpServer.AddTool(
		mcp.NewTool("hello", mcp.WithDescription("say hi")),
		func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("hi from stdio"), nil
		},
	)
	// ServeStdio returns on stdin EOF; exit directly so the test framework
	// never writes "PASS" to the MCP stdout stream.
	if err := server.ServeStdio(mcpServer); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

// TestStartHostsStdioServer launches this test binary as a subprocess MCP
// server over stdio and verifies tool discovery and call forwarding. Hermetic:
// no external binary is needed.
func TestStartHostsStdioServer(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	reg := toolreg.New()
	m := NewManager([]ServerConfig{{
		Name:      "helper",
		Transport: TransportStdio,
		Command:   exe,
		Args:      []string{"-test.run=TestStdioHelperProcess"},
		Env:       map[string]string{stdioHelperEnv: "1"},
	}})
	if err := m.Start(context.Background(), reg, quietLogger()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Close()

	tool, ok := reg.Lookup("helper_hello")
	if !ok {
		t.Fatalf("helper_hello not registered; registered = %v", reg.List())
	}
	out, err := tool.Handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	content, ok := out["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("content = %v, want one block", out["content"])
	}
	block, _ := content[0].(map[string]any)
	if block["text"] != "hi from stdio" {
		t.Errorf("forwarded result text = %v, want %q", block["text"], "hi from stdio")
	}
}
