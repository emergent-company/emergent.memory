package mcpregistry

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// --- fakes ---

// fakeProxyRepo is an in-memory proxyRepo for CallTool/resolution tests.
type fakeProxyRepo struct {
	servers []*MCPServer
}

func (f *fakeProxyRepo) FindServerByName(_ context.Context, projectID, name string) (*MCPServer, error) {
	for _, s := range f.servers {
		if s.ProjectID == projectID && s.Name == name {
			return s, nil
		}
	}
	return nil, nil
}

func (f *fakeProxyRepo) FindAllServers(_ context.Context, projectID string) ([]*MCPServer, error) {
	var out []*MCPServer
	for _, s := range f.servers {
		if s.ProjectID == projectID {
			out = append(out, s)
		}
	}
	return out, nil
}

func server(id, projectID, name string, enabled bool) *MCPServer {
	return &MCPServer{ID: id, ProjectID: projectID, Name: name, Enabled: enabled}
}

// fakeMCPClient records the tool call it receives so tests can assert exactly
// which (raw server, unprefixed tool) pair the proxy dispatched.
type fakeMCPClient struct {
	calledTool string
	calledArgs any
	callErr    error
}

func (f *fakeMCPClient) CallTool(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	f.calledTool = req.Params.Name
	f.calledArgs = req.Params.Arguments
	if f.callErr != nil {
		return nil, f.callErr
	}
	return &mcpgo.CallToolResult{Content: []mcpgo.Content{mcpgo.NewTextContent("ok")}}, nil
}

func (f *fakeMCPClient) ListTools(context.Context, mcpgo.ListToolsRequest) (*mcpgo.ListToolsResult, error) {
	return &mcpgo.ListToolsResult{}, nil
}

func (f *fakeMCPClient) Close() error { return nil }

func newTestProxyManager(repo proxyRepo) *ProxyManager {
	return &ProxyManager{
		repo:  repo,
		log:   slog.Default(),
		conns: make(map[string]*mcpConnection),
	}
}

// --- resolveServerForTool unit tests ---

func TestResolveServerForTool_RawNameFound(t *testing.T) {
	repo := &fakeProxyRepo{servers: []*MCPServer{
		server("s1", "p1", "myserver", true),
	}}
	pm := newTestProxyManager(repo)

	srv, tool, err := pm.resolveServerForTool(context.Background(), "p1", "myserver_search")
	require.NoError(t, err)
	require.NotNil(t, srv)
	assert.Equal(t, "myserver", srv.Name)
	assert.Equal(t, "search", tool)
}

func TestResolveServerForTool_SpaceyServer_ResolvedViaSlug(t *testing.T) {
	// Pool key for server "E2E MCP 123" is "e2e_mcp_123_web_fetch_exa". Raw mode
	// splits "e2e"+"mcp_123_web_fetch_exa" → no server "e2e" → slug mode must win.
	repo := &fakeProxyRepo{servers: []*MCPServer{
		server("s1", "p1", "E2E MCP 123", true),
	}}
	pm := newTestProxyManager(repo)

	srv, tool, err := pm.resolveServerForTool(context.Background(), "p1", "e2e_mcp_123_web_fetch_exa")
	require.NoError(t, err)
	require.NotNil(t, srv)
	assert.Equal(t, "E2E MCP 123", srv.Name)
	assert.Equal(t, "web_fetch_exa", tool)
}

func TestResolveServerForTool_UnderscoreInServerName_ResolvedViaSlug(t *testing.T) {
	// Raw name "my_server" contains an underscore, so the legacy first-underscore
	// split ("my") misses; slug mode keeps the single underscore and must match.
	repo := &fakeProxyRepo{servers: []*MCPServer{
		server("s1", "p1", "my_server", true),
	}}
	pm := newTestProxyManager(repo)

	srv, tool, err := pm.resolveServerForTool(context.Background(), "p1", "my_server_do_thing")
	require.NoError(t, err)
	require.NotNil(t, srv)
	assert.Equal(t, "my_server", srv.Name)
	assert.Equal(t, "do_thing", tool)
}

func TestResolveServerForTool_LongestSlugWins(t *testing.T) {
	// Slugs "e2e_mcp" and "e2e_mcp_123" both prefix the key; no server is named
	// exactly "e2e", so raw mode misses and slug mode must pick the LONGEST
	// matching slug — the short one would swallow "123_web_fetch_exa" as the tool.
	repo := &fakeProxyRepo{servers: []*MCPServer{
		server("s1", "p1", "e2e mcp", true),
		server("s2", "p1", "e2e mcp 123", true),
	}}
	pm := newTestProxyManager(repo)

	srv, tool, err := pm.resolveServerForTool(context.Background(), "p1", "e2e_mcp_123_web_fetch_exa")
	require.NoError(t, err)
	require.NotNil(t, srv)
	assert.Equal(t, "e2e mcp 123", srv.Name)
	assert.Equal(t, "web_fetch_exa", tool)
}

func TestResolveServerForTool_NoMatch_ErrorListsTriedSlugs(t *testing.T) {
	repo := &fakeProxyRepo{servers: []*MCPServer{
		server("s1", "p1", "E2E MCP 123", true),
		server("s2", "p1", "other", true),
	}}
	pm := newTestProxyManager(repo)

	srv, tool, err := pm.resolveServerForTool(context.Background(), "p1", "ghost_tool")
	require.Error(t, err)
	assert.Nil(t, srv)
	assert.Empty(t, tool)
	assert.Contains(t, err.Error(), "ghost_tool")
	assert.Contains(t, err.Error(), "e2e_mcp_123", "error must list the candidate slug tried")
	assert.Contains(t, err.Error(), "other")
}

func TestResolveServerForTool_EmptyToolRemainder_NoMatch(t *testing.T) {
	// "e2e_" has an empty tool remainder — must not resolve to server "e2e".
	repo := &fakeProxyRepo{servers: []*MCPServer{
		server("s1", "p1", "e2e", true),
	}}
	pm := newTestProxyManager(repo)

	srv, tool, err := pm.resolveServerForTool(context.Background(), "p1", "e2e_")
	require.Error(t, err)
	assert.Nil(t, srv)
	assert.Empty(t, tool)
}

// --- CallTool tests (fake repo + fake connection) ---

func TestCallTool_SluggedKey_RoutesToRawServerWithUnprefixedTool(t *testing.T) {
	repo := &fakeProxyRepo{servers: []*MCPServer{
		server("s1", "p1", "E2E MCP 123", true),
	}}
	pm := newTestProxyManager(repo)
	fake := &fakeMCPClient{}
	pm.conns["s1"] = &mcpConnection{client: fake, serverID: "s1", serverName: "E2E MCP 123"}

	args := map[string]any{"url": "https://example.com"}
	result, err := pm.CallTool(context.Background(), "p1", "e2e_mcp_123_web_fetch_exa", args)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "web_fetch_exa", fake.calledTool,
		"the raw server must receive the unprefixed tool name")
	assert.Equal(t, args, fake.calledArgs)
	require.Len(t, result.Content, 1)
	assert.Equal(t, "ok", result.Content[0].Text)
}

func TestCallTool_RawKey_StillRoutes(t *testing.T) {
	repo := &fakeProxyRepo{servers: []*MCPServer{
		server("s1", "p1", "myserver", true),
	}}
	pm := newTestProxyManager(repo)
	fake := &fakeMCPClient{}
	pm.conns["s1"] = &mcpConnection{client: fake, serverID: "s1", serverName: "myserver"}

	result, err := pm.CallTool(context.Background(), "p1", "myserver_search", map[string]any{"q": "x"})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "search", fake.calledTool, "legacy raw-format keys must keep routing")
}

func TestCallTool_DisabledServer_RejectedBeforeConnect(t *testing.T) {
	repo := &fakeProxyRepo{servers: []*MCPServer{
		server("s1", "p1", "E2E MCP 123", false),
	}}
	pm := newTestProxyManager(repo)
	fake := &fakeMCPClient{}
	pm.conns["s1"] = &mcpConnection{client: fake, serverID: "s1", serverName: "E2E MCP 123"}

	_, err := pm.CallTool(context.Background(), "p1", "e2e_mcp_123_web_fetch_exa", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
	assert.Empty(t, fake.calledTool, "no tool call may reach a disabled server")
}

// TestCallTool_ClientError_Propagates sanity-checks error surfacing end to end.
func TestCallTool_ClientError_Propagates(t *testing.T) {
	repo := &fakeProxyRepo{servers: []*MCPServer{
		server("s1", "p1", "myserver", true),
	}}
	pm := newTestProxyManager(repo)
	fake := &fakeMCPClient{callErr: errors.New("tool boom")}
	pm.conns["s1"] = &mcpConnection{client: fake, serverID: "s1", serverName: "myserver"}

	_, err := pm.CallTool(context.Background(), "p1", "myserver_search", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
	assert.Contains(t, err.Error(), "search")
	assert.Contains(t, err.Error(), "myserver")
}
