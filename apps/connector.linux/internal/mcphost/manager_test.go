package mcphost

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/emergent-company/memory.web-ui/connector/internal/toolreg"
)

// fakeClient is an in-memory mcpClient for unit tests.
type fakeClient struct {
	startErr  error
	initErr   error
	listErr   error
	callErr   error
	tools     []mcp.Tool
	result    *mcp.CallToolResult
	started   bool
	closed    bool
	callNames []string
}

func (f *fakeClient) Start(context.Context) error { f.started = true; return f.startErr }

func (f *fakeClient) Initialize(context.Context, mcp.InitializeRequest) (*mcp.InitializeResult, error) {
	return &mcp.InitializeResult{}, f.initErr
}

func (f *fakeClient) ListTools(context.Context, mcp.ListToolsRequest) (*mcp.ListToolsResult, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return &mcp.ListToolsResult{Tools: f.tools}, nil
}

func (f *fakeClient) CallTool(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	f.callNames = append(f.callNames, req.Params.Name)
	if f.callErr != nil {
		return nil, f.callErr
	}
	if f.result == nil {
		return mcp.NewToolResultText("ok"), nil
	}
	return f.result, nil
}

func (f *fakeClient) Close() error { f.closed = true; return nil }

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// mcpTool builds a minimal discovered tool with an object input schema.
func mcpTool(name, desc string) mcp.Tool {
	t := mcp.NewTool(name, mcp.WithDescription(desc))
	return t
}

// managerWithDialer builds a Manager whose connections are served by the
// supplied fake, keyed by server name. Unknown names use a fresh fake with no
// tools.
func managerWithDialer(servers []ServerConfig, byName map[string]*fakeClient) *Manager {
	m := NewManager(servers)
	m.dial = func(cfg ServerConfig) (mcpClient, error) {
		if fc, ok := byName[cfg.Name]; ok {
			return fc, nil
		}
		return &fakeClient{}, nil
	}
	return m
}

func TestStartNamespacesAndFiltersDisabledTools(t *testing.T) {
	fc := &fakeClient{tools: []mcp.Tool{mcpTool("search", "search notes"), mcpTool("create", "create note")}}
	srv := ServerConfig{Name: "notes", Transport: TransportStdio, Command: "notes-mcp", DisabledTools: []string{"search"}}
	reg := toolreg.New()

	m := managerWithDialer([]ServerConfig{srv}, map[string]*fakeClient{"notes": fc})
	if err := m.Start(context.Background(), reg, quietLogger()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if _, ok := reg.Lookup("notes_search"); ok {
		t.Error("disabled tool notes_search must not be registered")
	}
	tool, ok := reg.Lookup("notes_create")
	if !ok {
		t.Fatal("namespaced tool notes_create must be registered")
	}
	if tool.Description != "create note" {
		t.Errorf("description = %q, want %q", tool.Description, "create note")
	}
	if tool.InputSchema == nil {
		t.Error("input schema must be populated")
	}

	// Handler forwards to the server using its own (un-namespaced) tool name.
	if _, err := tool.Handler(context.Background(), map[string]any{"title": "x"}); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if len(fc.callNames) != 1 || fc.callNames[0] != "create" {
		t.Errorf("forwarded tool names = %v, want [create]", fc.callNames)
	}
	if !fc.started {
		t.Error("client Start must be called")
	}
	m.Close()
	if !fc.closed {
		t.Error("Close must close the hosted client")
	}
}

func TestStartForwardsResultMap(t *testing.T) {
	fc := &fakeClient{
		tools:  []mcp.Tool{mcpTool("echo", "echo")},
		result: mcp.NewToolResultText("hello world"),
	}
	reg := toolreg.New()
	m := managerWithDialer([]ServerConfig{{Name: "srv", Transport: TransportHTTP, URL: "http://x"}}, map[string]*fakeClient{"srv": fc})
	if err := m.Start(context.Background(), reg, quietLogger()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Close()

	tool, ok := reg.Lookup("srv_echo")
	if !ok {
		t.Fatal("srv_echo not registered")
	}
	out, err := tool.Handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	content, ok := out["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("result content = %v, want one block", out["content"])
	}
	block, _ := content[0].(map[string]any)
	if block["text"] != "hello world" {
		t.Errorf("content[0].text = %v, want %q", block["text"], "hello world")
	}
}

func TestStartRejectsDuplicateServerNames(t *testing.T) {
	servers := []ServerConfig{
		{Name: "dup", Transport: TransportStdio, Command: "a"},
		{Name: "dup", Transport: TransportStdio, Command: "b"},
	}
	reg := toolreg.New()
	m := managerWithDialer(servers, nil)
	err := m.Start(context.Background(), reg, quietLogger())
	if err == nil || !strings.Contains(err.Error(), "duplicate server name") {
		t.Fatalf("Start error = %v, want duplicate server name error", err)
	}
}

func TestStartRejectsServerCollidingWithExistingTool(t *testing.T) {
	reg := toolreg.New()
	if err := reg.Register(toolreg.Tool{
		Name:        "srv_echo",
		Description: "apple tool",
		InputSchema: map[string]any{"type": "object"},
		Handler:     func(context.Context, map[string]any) (map[string]any, error) { return nil, nil },
	}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}

	fc := &fakeClient{tools: []mcp.Tool{mcpTool("echo", "echo")}}
	m := managerWithDialer([]ServerConfig{{Name: "srv", Transport: TransportHTTP, URL: "http://x"}}, map[string]*fakeClient{"srv": fc})
	err := m.Start(context.Background(), reg, quietLogger())
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("Start error = %v, want already-registered error", err)
	}
	if !fc.closed {
		t.Error("client must be closed when registration conflicts")
	}
}

func TestStartRejectsDuplicateToolNamesWithinServer(t *testing.T) {
	fc := &fakeClient{tools: []mcp.Tool{mcpTool("echo", "one"), mcpTool("echo", "two")}}
	reg := toolreg.New()
	m := managerWithDialer([]ServerConfig{{Name: "srv", Transport: TransportHTTP, URL: "http://x"}}, map[string]*fakeClient{"srv": fc})
	err := m.Start(context.Background(), reg, quietLogger())
	if err == nil || !strings.Contains(err.Error(), "duplicate tool name") {
		t.Fatalf("Start error = %v, want duplicate tool name error", err)
	}
}

func TestStartSkipsFailedServerButKeepsOthers(t *testing.T) {
	good := &fakeClient{tools: []mcp.Tool{mcpTool("ping", "ping")}}
	servers := []ServerConfig{
		{Name: "bad", Transport: TransportHTTP, URL: "http://bad"},
		{Name: "good", Transport: TransportHTTP, URL: "http://good"},
	}
	m := NewManager(servers)
	m.dial = func(cfg ServerConfig) (mcpClient, error) {
		if cfg.Name == "bad" {
			return nil, errors.New("connection refused")
		}
		return good, nil
	}

	reg := toolreg.New()
	if err := m.Start(context.Background(), reg, quietLogger()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Close()

	if _, ok := reg.Lookup("good_ping"); !ok {
		t.Error("good server's tool must still be registered")
	}
	if _, ok := reg.Lookup("bad_ping"); ok {
		t.Error("bad server registered a tool")
	}
}

func TestStartSkipsServerThatFailsInitialize(t *testing.T) {
	bad := &fakeClient{initErr: errors.New("boom"), tools: []mcp.Tool{mcpTool("x", "x")}}
	good := &fakeClient{tools: []mcp.Tool{mcpTool("y", "y")}}
	servers := []ServerConfig{
		{Name: "bad", Transport: TransportHTTP, URL: "http://bad"},
		{Name: "good", Transport: TransportHTTP, URL: "http://good"},
	}
	m := managerWithDialer(servers, map[string]*fakeClient{"bad": bad, "good": good})
	reg := toolreg.New()
	if err := m.Start(context.Background(), reg, quietLogger()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Close()

	if _, ok := reg.Lookup("bad_x"); ok {
		t.Error("server failing initialize must not register tools")
	}
	if _, ok := reg.Lookup("good_y"); !ok {
		t.Error("healthy server must still register tools")
	}
	if !bad.closed {
		t.Error("failed server client must be closed")
	}
}

func TestStartSkipsDisabledServer(t *testing.T) {
	disabled := false
	servers := []ServerConfig{{Name: "off", Transport: TransportStdio, Command: "x", Enabled: &disabled}}
	m := managerWithDialer(servers, nil)
	reg := toolreg.New()
	if err := m.Start(context.Background(), reg, quietLogger()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if len(reg.List()) != 0 {
		t.Errorf("disabled server registered tools: %v", reg.List())
	}
}

func TestStartValidatesConfig(t *testing.T) {
	cases := []struct {
		name    string
		servers []ServerConfig
		want    string
	}{
		{
			name:    "unknown transport",
			servers: []ServerConfig{{Name: "x", Transport: "carrier-pigeon"}},
			want:    "unknown transport",
		},
		{
			name:    "stdio missing command",
			servers: []ServerConfig{{Name: "x", Transport: TransportStdio}},
			want:    "command is required",
		},
		{
			name:    "http missing url",
			servers: []ServerConfig{{Name: "x", Transport: TransportHTTP}},
			want:    "url is required",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := managerWithDialer(tc.servers, nil)
			err := m.Start(context.Background(), toolreg.New(), quietLogger())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Start error = %v, want %q", err, tc.want)
			}
		})
	}
}
