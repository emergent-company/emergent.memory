package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// --- route wiring + helpers ---

// mcpServerTestServer registers the MCP Servers UI page routes plus the JSON
// echo routes the page JS calls, mirroring main.go's wiring.
func mcpServerTestServer(s *Server) *echo.Echo {
	e := echo.New()
	e.GET("/settings/mcp-servers", s.uiMCPServers)
	e.GET("/settings/mcp-servers/new", s.uiMCPNewPage)
	e.POST("/settings/mcp-servers/new", s.uiMCPCreate)
	e.GET("/settings/mcp-servers/:id/edit", s.uiMCPEditPage)
	e.GET("/settings/mcp-servers/:id/tools", s.uiMCPServerToolsFragment)
	e.POST("/settings/mcp-servers/:id/update", s.uiMCPUpdate)
	e.POST("/settings/mcp-servers/:id/delete", s.uiMCPDelete)
	e.POST("/settings/mcp-servers/:id/toggle", s.uiMCPToggle)
	e.GET("/settings/mcp-servers/:id/oauth/start", s.uiMCPOAuthStart)
	e.POST("/settings/mcp-servers/:id/oauth/disconnect", s.uiMCPOAuthDisconnect)
	e.POST("/settings/mcp-servers/:id/oauth/client", s.uiMCPOAuthClient)
	e.POST("/settings/mcp-servers/:id/oauth/client/clear", s.uiMCPOAuthClientClear)
	e.POST("/api/mcp-servers/:id/sync", s.syncMCPServer)
	e.POST("/api/mcp-servers/:id/inspect", s.inspectMCPServer)
	e.GET("/api/mcp-servers/:id/tools", s.listMCPServerTools)
	e.PATCH("/api/mcp-servers/:id/tools/:toolId", s.setMCPServerToolEnabled)
	e.POST("/api/mcp-servers/:id/tools/:toolName/call", s.callMCPServerTool)
	return e
}

func mcpServerGet(e *echo.Echo, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func mcpServerPost(e *echo.Echo, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	return rec
}

// mcpServerJSON issues a method with a JSON body (the tool-toggle PATCH path).
func mcpServerJSON(e *echo.Echo, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	e.ServeHTTP(rec, req)
	return rec
}

// mcpTestRegistryFixture returns three registry rows: an http server with
// headers (whose secret must never render on the list page), a stdio server,
// and the read-only builtin server.
func mcpTestRegistryFixture() []MCPServer {
	return []MCPServer{
		{
			ID: "srv-http", Name: "github-mcp", Type: "http", Enabled: true,
			URL:       "https://github.example.com/mcp",
			Headers:   map[string]string{"Authorization": "Bearer secret-token"},
			ToolCount: 2,
			Tools: []MCPTool{
				{ID: "t1", ServerID: "srv-http", ToolName: "search_issues", Description: "Search issues", Enabled: true},
				{ID: "t2", ServerID: "srv-http", ToolName: "create_issue", Enabled: false},
			},
		},
		{
			ID: "srv-stdio", Name: "files", Type: "stdio", Enabled: false,
			Command:   "npx -y files-mcp",
			ToolCount: 1,
			Tools: []MCPTool{
				{ID: "t3", ServerID: "srv-stdio", ToolName: "read_file", Enabled: true},
			},
		},
		{
			ID: "srv-builtin", Name: "builtin", Type: "builtin", Enabled: true,
			ToolCount: 2,
			Tools: []MCPTool{
				{ID: "b1", ServerID: "srv-builtin", ToolName: "memory_lookup", Enabled: true},
				{ID: "b2", ServerID: "srv-builtin", ToolName: "graph_write", Enabled: true},
			},
		},
	}
}

// mcpTestBackend is the MCP-management test backend: an embedded fakeMemory
// (so every unrelated interface method still exists) with real create/update/
// delete/toggle/list semantics over the servers slice plus error injection.
type mcpTestBackend struct {
	*fakeMemory
	listErr         error // ListMCPServers / GetMCPServer failures
	createErr       error
	updateErr       error
	deleteErr       error
	syncErr         error
	inspect         *MCPInspectResult
	inspectErr      error
	toolsErr        error
	toggleErr       error
	syncTo          []MCPTool // when set, SyncMCPServer replaces the server's tools
	lastToolID      string
	lastToolEnabled *bool

	// call* drive the per-tool invoke proxy (POST /tools/:toolName/call).
	callResult   *MCPToolCallResult
	callErr      error
	lastCallID   string
	lastCallTool string
	lastCallArgs map[string]any

	// builtinGroups is served by ListBuiltinToolGroups; builtinGroupsErr lets a
	// test assert the page degrades to the flat builtin list on failure.
	builtinGroups    []BuiltinToolGroup
	builtinGroupsErr error

	// authorize* / disconnect* drive the OAuth connect routes
	// (uiMCPOAuthStart / uiMCPOAuthDisconnect).
	authorizeResult *MCPOAuthAuthorize
	authorizeErr    error
	disconnectErr   error
	lastOAuthID     string

	// oauthClient* / clearClient* drive the pre-registered OAuth client routes
	// (uiMCPOAuthClient / uiMCPOAuthClientClear).
	oauthClientResult *MCPOAuthStatus
	oauthClientErr    error
	clearClientErr    error
	lastClientServer  string
	lastClientID      string
	lastClientSecret  string
	lastClientScope   string
}

func newMCPTestBackend(servers []MCPServer) *mcpTestBackend {
	return &mcpTestBackend{fakeMemory: &fakeMemory{servers: servers}}
}

func (b *mcpTestBackend) find(id string) *MCPServer { return mcpFindServerByID(b.servers, id) }

func (b *mcpTestBackend) ListMCPServers(ctx context.Context) ([]MCPServer, error) {
	if b.listErr != nil {
		return nil, b.listErr
	}
	return b.servers, nil
}

func (b *mcpTestBackend) GetMCPServer(ctx context.Context, id string) (*MCPServer, error) {
	if b.listErr != nil {
		return nil, b.listErr
	}
	srv := b.find(id)
	if srv == nil {
		return nil, fmt.Errorf("memory 404 mcp_server not found")
	}
	cp := *srv
	return &cp, nil
}

func (b *mcpTestBackend) CreateMCPServer(ctx context.Context, in *MCPServer) (*MCPServer, error) {
	if b.createErr != nil {
		return nil, b.createErr
	}
	if b.nameExists(in.Name, "") {
		return nil, fmt.Errorf("memory 400 bad_request: server with name %q already exists in this project", in.Name)
	}
	srv := *in
	srv.ID = "srv-new"
	b.servers = append(b.servers, srv)
	return &srv, nil
}

func (b *mcpTestBackend) UpdateMCPServer(ctx context.Context, id string, in *MCPServer) (*MCPServer, error) {
	if b.updateErr != nil {
		return nil, b.updateErr
	}
	if b.nameExists(in.Name, id) {
		return nil, fmt.Errorf("memory 400 bad_request: server with name %q already exists in this project", in.Name)
	}
	idx := -1
	for i := range b.servers {
		if b.servers[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, fmt.Errorf("memory 404 mcp_server not found")
	}
	srv := *in
	srv.ID = id
	b.servers[idx] = srv
	return &srv, nil
}

// nameExists reports whether another registry row already holds name.
func (b *mcpTestBackend) nameExists(name, exceptID string) bool {
	for _, s := range b.servers {
		if s.Name == name && s.ID != exceptID {
			return true
		}
	}
	return false
}

func (b *mcpTestBackend) DeleteMCPServer(ctx context.Context, id string) error {
	if b.deleteErr != nil {
		return b.deleteErr
	}
	out := b.servers[:0]
	found := false
	for _, s := range b.servers {
		if s.ID == id {
			found = true
			continue
		}
		out = append(out, s)
	}
	b.servers = out
	if !found {
		return fmt.Errorf("memory 404 mcp_server not found")
	}
	return nil
}

func (b *mcpTestBackend) SyncMCPServer(ctx context.Context, id string) error {
	if b.syncErr != nil {
		return b.syncErr
	}
	if b.syncTo != nil {
		srv := b.find(id)
		if srv == nil {
			return fmt.Errorf("memory 404 mcp_server not found")
		}
		srv.Tools = append([]MCPTool(nil), b.syncTo...)
		srv.ToolCount = len(b.syncTo)
	}
	return nil
}

func (b *mcpTestBackend) InspectMCPServer(ctx context.Context, id string) (*MCPInspectResult, error) {
	if b.inspectErr != nil {
		return nil, b.inspectErr
	}
	if b.inspect != nil {
		return b.inspect, nil
	}
	srv := b.find(id)
	name := "probe"
	if srv != nil {
		name = srv.Name
	}
	return &MCPInspectResult{ServerID: id, ServerName: name, Status: "ok"}, nil
}

func (b *mcpTestBackend) ListMCPServerTools(ctx context.Context, id string) ([]MCPTool, error) {
	if b.toolsErr != nil {
		return nil, b.toolsErr
	}
	srv := b.find(id)
	if srv == nil {
		return nil, fmt.Errorf("memory 404 mcp_server not found")
	}
	return append([]MCPTool(nil), srv.Tools...), nil
}

func (b *mcpTestBackend) SetMCPServerToolEnabled(ctx context.Context, id, toolID string, enabled bool) error {
	if b.toggleErr != nil {
		return b.toggleErr
	}
	b.lastToolID = toolID
	en := enabled
	b.lastToolEnabled = &en
	srv := b.find(id)
	if srv == nil {
		return fmt.Errorf("memory 404 mcp_server not found")
	}
	for i := range srv.Tools {
		if srv.Tools[i].ID == toolID {
			srv.Tools[i].Enabled = enabled
			return nil
		}
	}
	return fmt.Errorf("memory 404 mcp_server_tool not found")
}

func (b *mcpTestBackend) CallMCPServerTool(ctx context.Context, id, toolName string, args map[string]any) (*MCPToolCallResult, error) {
	if b.callErr != nil {
		return nil, b.callErr
	}
	b.lastCallID, b.lastCallTool, b.lastCallArgs = id, toolName, args
	if b.callResult != nil {
		return b.callResult, nil
	}
	return &MCPToolCallResult{Content: []MCPToolCallContent{{Type: "text", Text: `{"ok":true}`}}}, nil
}

func (b *mcpTestBackend) ListBuiltinToolGroups(ctx context.Context) ([]BuiltinToolGroup, error) {
	if b.builtinGroupsErr != nil {
		return nil, b.builtinGroupsErr
	}
	return b.builtinGroups, nil
}

func (b *mcpTestBackend) AuthorizeMCPServer(ctx context.Context, id string) (*MCPOAuthAuthorize, error) {
	b.lastOAuthID = id
	if b.authorizeErr != nil {
		return nil, b.authorizeErr
	}
	if b.authorizeResult != nil {
		return b.authorizeResult, nil
	}
	return &MCPOAuthAuthorize{AuthorizeURL: "https://auth.example.com/authorize?state=fake"}, nil
}

func (b *mcpTestBackend) DisconnectMCPServer(ctx context.Context, id string) error {
	b.lastOAuthID = id
	return b.disconnectErr
}

func (b *mcpTestBackend) SetMCPServerOAuthClient(ctx context.Context, id, clientID, clientSecret, scope string) (*MCPOAuthStatus, error) {
	b.lastClientServer = id
	b.lastClientID = clientID
	b.lastClientSecret = clientSecret
	b.lastClientScope = scope
	if b.oauthClientErr != nil {
		return nil, b.oauthClientErr
	}
	if b.oauthClientResult != nil {
		return b.oauthClientResult, nil
	}
	return &MCPOAuthStatus{ServerID: id, ClientConfigured: true}, nil
}

func (b *mcpTestBackend) ClearMCPServerOAuthClient(ctx context.Context, id string) error {
	b.lastClientServer = id
	return b.clearClientErr
}

// --- list page ---

func TestUIMCPServersListRoute(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerGet(e, "/settings/mcp-servers")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /settings/mcp-servers = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"MCP Servers", "github-mcp", "files", "builtin",
		"3 servers", "http", "stdio",
		"2 tools",
		`href="/settings/mcp-servers/new"`, "New server",
		// rows are deferred to the first expand (#210)
		`data-mcp-tools-host="srv-http"`, `data-mcp-tools-host="srv-builtin"`,
		`data-mcp-tools-url="/settings/mcp-servers/srv-http/tools"`,
		// external row actions + delete confirm target
		`href="/settings/mcp-servers/srv-http/edit"`,
		`action="/settings/mcp-servers/srv-http/delete"`,
		`action="/settings/mcp-servers/srv-stdio/delete"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /settings/mcp-servers missing %q", want)
		}
	}
	// header secrets must never render on the list page
	if strings.Contains(body, "secret-token") {
		t.Error("list page must never render header values (plaintext secrets)")
	}
	// builtin rows are read-only: no edit link, no delete confirm, no toggles
	if strings.Contains(body, `href="/settings/mcp-servers/srv-builtin/edit"`) ||
		strings.Contains(body, `action="/settings/mcp-servers/srv-builtin/delete"`) {
		t.Error("builtin row must not offer edit or delete actions")
	}
	if strings.Contains(body, `aria-label="Enable builtin"`) || strings.Contains(body, `aria-label="Sync builtin"`) {
		t.Error("builtin row must not offer the enabled toggle or sync")
	}
	// sync/inspect buttons exist only on the two external rows
	if got := strings.Count(body, `data-mcp-action="sync"`); got != 2 {
		t.Errorf("sync action occurrences = %d, want 2 (external rows only)", got)
	}
	if got := strings.Count(body, `data-mcp-action="inspect"`); got != 2 {
		t.Errorf("inspect action occurrences = %d, want 2 (external rows only)", got)
	}
	// The per-tool toggles belong to the deferred rows fragment, not the page
	// (#210): the page must ship none of them, whatever the row count.
	if got := strings.Count(body, `class="toggle toggle-sm shrink-0"`); got != 0 {
		t.Errorf("per-tool toggle occurrences on the page = %d, want 0 (rows are deferred)", got)
	}
	for _, want := range []string{`aria-label="Enable github-mcp"`, `aria-label="Enable files"`} {
		if !strings.Contains(body, want) {
			t.Errorf("external row enabled toggle missing %q", want)
		}
	}
}

// TestUIMCPServersListDeviceRow verifies that a device-targeted server renders
// as a device row (device badge + bound instance id + device note, no outbound
// sync/inspect/connect affordances) while an outbound server renders the normal
// sync/inspect actions — the two must not look alike.
func TestUIMCPServersListDeviceRow(t *testing.T) {
	f := newMCPTestBackend([]MCPServer{
		{
			ID: "srv-http", Name: "github-mcp", Type: "http", Enabled: true,
			URL:       "https://github.example.com/mcp",
			ToolCount: 1,
			Tools: []MCPTool{
				{ID: "t1", ServerID: "srv-http", ToolName: "search_issues", Enabled: true},
			},
		},
		{
			ID: "srv-device", Name: "local-files", Type: "stdio", Enabled: true,
			Command:          "npx -y files-mcp",
			Target:           "device",
			TargetInstanceID: "inst-abc123",
		},
		{
			ID: "srv-device-broadcast", Name: "local-notes", Type: "stdio", Enabled: false,
			Command: "npx -y notes-mcp",
			Target:  "device",
		},
	})
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerGet(e, "/settings/mcp-servers")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /settings/mcp-servers = %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	// device rows carry the device badge; exactly the two device rows
	if got := strings.Count(body, `data-testid="mcp-device-badge"`); got != 2 {
		t.Errorf("device badge occurrences = %d, want 2 (device rows only)", got)
	}
	// only the bound row renders its instance id; the broadcast row does not
	if got := strings.Count(body, `data-testid="mcp-device-instance"`); got != 1 {
		t.Errorf("device instance id occurrences = %d, want 1 (bound row only)", got)
	}
	if !strings.Contains(body, "inst-abc123") {
		t.Error("device row must render its bound target instance id")
	}

	// outbound-only affordances are dropped on device rows: sync/inspect appear
	// exactly once (the outbound row), never on the device rows.
	if got := strings.Count(body, `data-mcp-action="sync"`); got != 1 {
		t.Errorf("sync action occurrences = %d, want 1 (outbound row only)", got)
	}
	if got := strings.Count(body, `data-mcp-action="inspect"`); got != 1 {
		t.Errorf("inspect action occurrences = %d, want 1 (outbound row only)", got)
	}
	for _, name := range []string{"local-files", "local-notes"} {
		if strings.Contains(body, `aria-label="Sync `+name+`"`) || strings.Contains(body, `aria-label="Inspect `+name+`"`) {
			t.Errorf("device row %q must not offer sync or inspect", name)
		}
	}

	// device rows explain where their tools come from instead of pointing at sync
	if got := strings.Count(body, `data-testid="mcp-device-note"`); got != 2 {
		t.Errorf("device note occurrences = %d, want 2 (device rows only)", got)
	}
	if !strings.Contains(body, "Expand to load the tools discovered on the device.") {
		t.Error("device row must say its tools are discovered on the device, not synced from the host")
	}
	// The "no tools cached" copy moved into the deferred rows fragment (#210);
	// it must still tell a device user not to run a host sync.
	deviceRows := renderHTML(t, mcpServerToolsContainer(
		MCPServer{ID: "srv-device", Name: "local-files", Type: "stdio", Target: "device"}, nil, nil))
	if !strings.Contains(deviceRows, "tools are discovered on the device") {
		t.Error("device rows with no cached tools must not suggest a host sync")
	}
	// the outbound row keeps its sync hint
	if !strings.Contains(body, "sync refreshes this list") {
		t.Error("outbound row must keep the sync hint")
	}
	// a device stdio row must not claim it runs on the memory host
	if strings.Contains(body, "on the memory host") {
		t.Error("device stdio row must not claim it runs on the memory host")
	}
}

func TestUIMCPServersListEmpty(t *testing.T) {
	f := newMCPTestBackend(nil)
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerGet(e, "/settings/mcp-servers")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"No MCP servers yet", `href="/settings/mcp-servers/new"`, "Register a server"} {
		if !strings.Contains(body, want) {
			t.Errorf("empty list missing %q", want)
		}
	}
}

func TestUIMCPServersListLoadError(t *testing.T) {
	f := newMCPTestBackend(nil)
	f.listErr = errTest
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerGet(e, "/settings/mcp-servers")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "MCP servers unavailable") {
		t.Error("load-error state missing")
	}
}

// --- create page + create flow ---

func TestUIMCPServersNewPageRoute(t *testing.T) {
	s := &Server{cfg: Config{}, memory: newMCPTestBackend(nil)}
	e := mcpServerTestServer(s)

	rec := mcpServerGet(e, "/settings/mcp-servers/new")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /settings/mcp-servers/new = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"New MCP server", `action="/settings/mcp-servers/new"`, `name="name"`,
		`value="stdio" class="radio radio-sm mt-0.5" data-mcp-transport checked`,
		`value="sse" class="radio radio-sm mt-0.5" data-mcp-transport`,
		`value="http" class="radio radio-sm mt-0.5" data-mcp-transport`,
		`name="command"`, `name="env.key"`, `Create server`, `href="/settings/mcp-servers"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /settings/mcp-servers/new missing %q", want)
		}
	}
	if strings.Contains(body, "Save changes") {
		t.Error("new page must not render the edit submit label")
	}
}

func TestUIMCPServersCreateRedirectsToList(t *testing.T) {
	f := newMCPTestBackend(nil)
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	body := "name=server-a&type=http&url=https%3A%2F%2Fmcp.example.com&headers.key=Authorization&headers.value=Bearer+abc&enabled=on"
	rec := mcpServerPost(e, "/settings/mcp-servers/new", body)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/settings/mcp-servers?created=1" {
		t.Fatalf("create = %d %q, want 303 /settings/mcp-servers?created=1", rec.Code, rec.Header().Get("Location"))
	}
	if len(f.servers) != 1 || f.servers[0].Name != "server-a" {
		t.Fatalf("store after create = %+v", f.servers)
	}
	if f.servers[0].URL != "https://mcp.example.com" || f.servers[0].Headers["Authorization"] != "Bearer abc" || !f.servers[0].Enabled {
		t.Errorf("created server config = %+v, want url + Authorization header + enabled", f.servers[0])
	}
	// the list re-render shows the new server
	rec = mcpServerGet(e, "/settings/mcp-servers")
	if !strings.Contains(rec.Body.String(), "server-a") {
		t.Error("created server missing from the list after create")
	}
}

func TestUIMCPServersCreateValidationErrorsInline(t *testing.T) {
	f := newMCPTestBackend(nil)
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	// empty name re-renders the form with an inline error and no partial save
	rec := mcpServerPost(e, "/settings/mcp-servers/new", "name=&type=http&url=https%3A%2F%2Fmcp.example.com")
	if rec.Code != http.StatusOK {
		t.Fatalf("empty-name create = %d, want 200 inline re-render", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Server name is required.") {
		t.Error("empty-name inline error missing")
	}
	if strings.Contains(body, `href="/settings/mcp-servers?created=1"`) || len(f.servers) != 0 {
		t.Error("empty-name create must not persist a server")
	}

	// http without a URL
	rec = mcpServerPost(e, "/settings/mcp-servers/new", "name=server-b&type=http&url=")
	if rec.Code != http.StatusOK {
		t.Fatalf("missing-url create = %d, want 200 inline re-render", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "A URL is required for http servers.") {
		t.Error("missing-url inline error missing")
	}
	if len(f.servers) != 0 {
		t.Error("missing-url create must not persist a server")
	}

	// stdio without a command
	rec = mcpServerPost(e, "/settings/mcp-servers/new", "name=server-c&type=stdio&command=")
	if !strings.Contains(rec.Body.String(), "Command is required for stdio servers.") {
		t.Error("missing-command inline error missing")
	}

	// blank header name with a value
	rec = mcpServerPost(e, "/settings/mcp-servers/new", "name=server-d&type=http&url=https%3A%2F%2Fmcp.example.com&headers.key=&headers.value=abc")
	if !strings.Contains(rec.Body.String(), "blank header names are rejected") {
		t.Error("blank-header-name inline error missing")
	}

	// invalid URL scheme
	rec = mcpServerPost(e, "/settings/mcp-servers/new", "name=server-e&type=sse&url=ftp%3A%2F%2Fexample.com")
	if !strings.Contains(rec.Body.String(), "http:// or https://") {
		t.Error("invalid-scheme inline error missing")
	}
	if len(f.servers) != 0 {
		t.Error("invalid submissions must not persist servers")
	}
}

func TestUIMCPServersCreateDuplicateNameInline(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture()) // already holds "github-mcp"
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerPost(e, "/settings/mcp-servers/new", "name=github-mcp&type=http&url=https%3A%2F%2Fmcp.example.com")
	if rec.Code != http.StatusOK {
		t.Fatalf("duplicate create = %d, want 200 inline re-render", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "already exists") {
		t.Error("duplicate-name memory error missing from the inline name error")
	}
	if len(f.servers) != 3 {
		t.Error("duplicate create must not add a server (no partial save)")
	}
}

func TestUIMCPServersCreateRejectedInline(t *testing.T) {
	f := newMCPTestBackend(nil)
	f.createErr = fmt.Errorf("memory 400 bad_request: command is required for stdio-type servers")
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerPost(e, "/settings/mcp-servers/new", "name=server-z&type=stdio&command=ignored")
	if rec.Code != http.StatusOK {
		t.Fatalf("rejected create = %d, want 200 inline re-render", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), genericErrorMessage) {
		t.Error("memory rejection must surface (sanitized) on the re-rendered form")
	}
}

// --- edit page + update flow ---

func TestUIMCPServersEditPagePrefills(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerGet(e, "/settings/mcp-servers/srv-http/edit")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /settings/mcp-servers/srv-http/edit = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Edit server", `action="/settings/mcp-servers/srv-http/update"`,
		`value="github-mcp"`, `value="https://github.example.com/mcp"`,
		`value="http" class="radio radio-sm mt-0.5" data-mcp-transport checked`,
		"Save changes", `href="/settings/mcp-servers"`,
		// header rows pre-fill (edit form is the one place secrets are shown)
		`name="headers.key" value="Authorization"`, `name="headers.value" value="Bearer secret-token"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("edit page missing %q", want)
		}
	}
}

func TestUIMCPServersEditPageNotFound(t *testing.T) {
	f := newMCPTestBackend(nil)
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerGet(e, "/settings/mcp-servers/nope/edit")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "MCP server not found") {
		t.Errorf("unknown-server edit GET = %d, want the not-found state", rec.Code)
	}
}

func TestUIMCPServersEditPageBuiltinRedirects(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerGet(e, "/settings/mcp-servers/srv-builtin/edit")
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "built-in") {
		t.Errorf("builtin edit GET = %d %q, want redirect back to the list with a built-in reason", rec.Code, rec.Header().Get("Location"))
	}
}

func TestUIMCPServersUpdatePersists(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	body := "name=github-mcp&type=http&url=https%3A%2F%2Fupdated.example.com%2Fmcp&headers.key=X-Api-Key&headers.value=k123&enabled=on"
	rec := mcpServerPost(e, "/settings/mcp-servers/srv-http/update", body)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/settings/mcp-servers?updated=1" {
		t.Fatalf("update = %d %q, want 303 /settings/mcp-servers?updated=1", rec.Code, rec.Header().Get("Location"))
	}
	srv := f.find("srv-http")
	if srv == nil || srv.URL != "https://updated.example.com/mcp" || srv.Headers["X-Api-Key"] != "k123" {
		t.Errorf("updated server = %+v, want new URL + header persisted", srv)
	}
	if len(srv.Headers) != 1 {
		t.Errorf("updated headers = %v, want the old Authorization header replaced", srv.Headers)
	}
}

func TestUIMCPServersUpdateTransportChangeAccepted(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	// github-mcp is http; submitting it as stdio must now succeed and persist
	// the transport switch (memory clears the previous transport's fields).
	body := "name=github-mcp&type=stdio&command=npx+-y+files-mcp&enabled=on"
	rec := mcpServerPost(e, "/settings/mcp-servers/srv-http/update", body)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/settings/mcp-servers?updated=1" {
		t.Fatalf("transport-change update = %d %q, want 303 /settings/mcp-servers?updated=1", rec.Code, rec.Header().Get("Location"))
	}
	srv := f.find("srv-http")
	if srv == nil {
		t.Fatal("updated server missing from the backend")
	}
	if srv.Type != "stdio" {
		t.Errorf("updated server type = %q, want stdio", srv.Type)
	}
	if srv.Command != "npx -y files-mcp" {
		t.Errorf("updated server command = %q, want the submitted stdio command", srv.Command)
	}
	if srv.URL != "" {
		t.Errorf("updated server url = %q, want empty (remote fields cleared on switch to stdio)", srv.URL)
	}
}

func TestUIMCPServersUpdateBuiltinRejected(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	body := "name=builtin&type=builtin&enabled=on"
	rec := mcpServerPost(e, "/settings/mcp-servers/srv-builtin/update", body)
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "built-in") {
		t.Errorf("builtin update = %d %q, want redirect with a built-in reason", rec.Code, rec.Header().Get("Location"))
	}
}

func TestUIMCPServersUpdateNameConflictInline(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	// rename srv-stdio ("files") to the taken "github-mcp"
	body := "name=github-mcp&type=stdio&command=npx+y&enabled=on"
	rec := mcpServerPost(e, "/settings/mcp-servers/srv-stdio/update", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("dup-name update = %d, want 200 inline re-render", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "already exists") {
		t.Error("duplicate-name error must surface on the name field inline")
	}
	if srv := f.find("srv-stdio"); srv == nil || srv.Name != "files" {
		t.Error("rejected rename must not partially persist")
	}
}

// --- delete flow ---

func TestUIMCPServersDeleteRemovesServer(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerPost(e, "/settings/mcp-servers/srv-http/delete", "")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/settings/mcp-servers?deleted=1" {
		t.Fatalf("delete = %d %q, want 303 /settings/mcp-servers?deleted=1", rec.Code, rec.Header().Get("Location"))
	}
	if f.find("srv-http") != nil || len(f.servers) != 2 {
		t.Error("delete must remove the server from the registry")
	}
}

func TestUIMCPServersDeleteBuiltinRejected(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerPost(e, "/settings/mcp-servers/srv-builtin/delete", "")
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "built-in") {
		t.Errorf("builtin delete = %d %q, want redirect with a built-in reason", rec.Code, rec.Header().Get("Location"))
	}
	if f.find("srv-builtin") == nil {
		t.Error("builtin server must survive the delete attempt")
	}
}

// --- enabled toggle (JSON) ---

func TestUIMCPServersToggleEnabled(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerPost(e, "/settings/mcp-servers/srv-stdio/toggle", "enabled=true")
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle = %d, want 200", rec.Code)
	}
	var out struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || !out.Enabled {
		t.Errorf("toggle response = %s, want enabled:true", rec.Body.String())
	}
	srv := f.find("srv-stdio")
	if srv == nil || !srv.Enabled {
		t.Error("toggle must persist the enabled flip on the full server config")
	}
	// flipping back
	rec = mcpServerPost(e, "/settings/mcp-servers/srv-stdio/toggle", "enabled=false")
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle off = %d, want 200", rec.Code)
	}
	if srv := f.find("srv-stdio"); srv == nil || srv.Enabled {
		t.Error("toggle off must persist")
	}
}

func TestUIMCPServersToggleBuiltinRejected(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerPost(e, "/settings/mcp-servers/srv-builtin/toggle", "enabled=false")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Built-in servers cannot be disabled") {
		t.Errorf("builtin toggle = %d %s, want 400 with a built-in message", rec.Code, rec.Body.String())
	}
	if srv := f.find("srv-builtin"); srv == nil || !srv.Enabled {
		t.Error("builtin server must stay enabled")
	}
}

func TestUIMCPServersToggleFailureIsJSON(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	f.updateErr = fmt.Errorf("memory 500 internal: boom")
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerPost(e, "/settings/mcp-servers/srv-http/toggle", "enabled=false")
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "boom") {
		t.Errorf("toggle failure = %d %s, want 502 with the memory error", rec.Code, rec.Body.String())
	}
}

// --- JSON sync / inspect / tools / tool-toggle ---

func TestUIMCPServersSyncRefreshesAndReportsPruned(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	// sync prunes search_issues and discovers two new tools
	f.syncTo = []MCPTool{
		{ID: "n1", ServerID: "srv-http", ToolName: "create_issue", Enabled: false},
		{ID: "n2", ServerID: "srv-http", ToolName: "list_repos", Enabled: true},
	}
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerPost(e, "/api/mcp-servers/srv-http/sync", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("sync = %d, want 200", rec.Code)
	}
	var out mcpSyncResult
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("sync body: %v (%s)", err, rec.Body.String())
	}
	if len(out.Tools) != 2 {
		t.Errorf("synced tools = %d, want 2", len(out.Tools))
	}
	if len(out.Pruned) != 1 || out.Pruned[0] != "search_issues" {
		t.Errorf("pruned = %v, want [search_issues]", out.Pruned)
	}
	// the html fragment re-renders the tool rows (fresh list only)
	if !strings.Contains(out.HTML, "list_repos") || strings.Contains(out.HTML, "search_issues") {
		t.Error("sync html fragment must carry the refreshed tool rows only")
	}
	// the fragment carries the row container for the outerHTML swap
	if !strings.Contains(out.HTML, `data-mcp-tools="srv-http"`) {
		t.Error("sync html fragment must carry the data-mcp-tools container")
	}
}

func TestUIMCPServersSyncFailureLeavesCache(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	f.syncErr = fmt.Errorf("memory 400 mcp_connection_error: failed to connect to server")
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerPost(e, "/api/mcp-servers/srv-http/sync", "")
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "failed to connect") {
		t.Errorf("sync failure = %d %s, want 502 with the memory error text", rec.Code, rec.Body.String())
	}
	srv := f.find("srv-http")
	if srv == nil || len(srv.Tools) != 2 {
		t.Error("failed sync must leave the cached tools untouched")
	}
}

func TestUIMCPServersSyncBuiltinRejected(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerPost(e, "/api/mcp-servers/srv-builtin/sync", "")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Built-in servers have no syncable tool cache") {
		t.Errorf("builtin sync = %d %s", rec.Code, rec.Body.String())
	}
}

func TestUIMCPServersInspectOK(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	f.inspect = &MCPInspectResult{
		ServerID: "srv-http", ServerName: "github-mcp", ServerType: "http", Status: "ok", LatencyMs: 12,
		ServerInfo: &MCPInspectServerInfo{Name: "github-mcp-server", Version: "1.2.3"},
		Tools:      []MCPInspectTool{{Name: "search_issues"}},
		Resources:  []MCPInspectResource{{URI: "repo://acme/README", Name: "readme"}},
	}
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerPost(e, "/api/mcp-servers/srv-http/inspect", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("inspect = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`"status":"ok"`, `"github-mcp-server"`, `"1.2.3"`, `"search_issues"`, `"readme"`} {
		if !strings.Contains(body, want) {
			t.Errorf("inspect body missing %q", want)
		}
	}
}

func TestUIMCPServersInspectConnectionErrorInBody(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	msg := "connection refused"
	f.inspect = &MCPInspectResult{ServerID: "srv-http", Status: "error", Error: &msg}
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerPost(e, "/api/mcp-servers/srv-http/inspect", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("inspect (connection error) = %d, want 200 (errors live in the body)", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"status":"error"`) || !strings.Contains(body, "connection refused") {
		t.Errorf("inspect error body = %s, want status error + message", body)
	}
}

func TestUIMCPServersInspectInternalError(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	f.inspectErr = fmt.Errorf("memory 500 internal: inspect failed")
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerPost(e, "/api/mcp-servers/srv-http/inspect", "")
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "inspect failed") {
		t.Errorf("inspect internal = %d %s", rec.Code, rec.Body.String())
	}
}

func TestUIMCPServersListToolsJSON(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerGet(e, "/api/mcp-servers/srv-http/tools")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET tools = %d, want 200", rec.Code)
	}
	var out []MCPTool
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out) != 2 {
		t.Fatalf("tools body = %s", rec.Body.String())
	}
	if out[0].ToolName != "search_issues" || !out[0].Enabled {
		t.Errorf("tools[0] = %+v, want search_issues enabled", out[0])
	}
	if out[1].ToolName != "create_issue" || out[1].Enabled {
		t.Errorf("tools[1] = %+v, want create_issue disabled", out[1])
	}
}

func TestUIMCPServersSetToolEnabled(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerJSON(e, http.MethodPatch, "/api/mcp-servers/srv-http/tools/t1", `{"enabled":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("tool toggle = %d, want 200", rec.Code)
	}
	if f.lastToolID != "t1" || f.lastToolEnabled == nil || *f.lastToolEnabled != false {
		t.Errorf("tool toggle record = %s %v", f.lastToolID, f.lastToolEnabled)
	}
	if srv := f.find("srv-http"); srv == nil || srv.Tools[0].Enabled {
		t.Error("tool toggle must persist the tool's enabled state")
	}
}

func TestUIMCPServersSetToolEnabledBadBody(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerJSON(e, http.MethodPatch, "/api/mcp-servers/srv-http/tools/t1", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty toggle body = %d, want 400", rec.Code)
	}
	if f.lastToolID != "" {
		t.Error("invalid body must not reach the backend")
	}
}

func TestUIMCPServersToolToggleFailureIsJSON(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	f.toggleErr = fmt.Errorf("memory 400 bad_request: tool not found")
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerJSON(e, http.MethodPatch, "/api/mcp-servers/srv-http/tools/nope", `{"enabled":true}`)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "tool not found") {
		t.Errorf("tool toggle failure = %d %s, want 502 with the memory error", rec.Code, rec.Body.String())
	}
}

func TestUIMCPServersCallToolProxies(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	f.callResult = &MCPToolCallResult{
		Content:           []MCPToolCallContent{{Type: "text", Text: `{"ok":true,"data":42}`}},
		StructuredContent: json.RawMessage(`{"ok":true}`),
	}
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerJSON(e, http.MethodPost, "/api/mcp-servers/srv-http/tools/search_issues/call", `{"arguments":{"query":"hello"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("call = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if f.lastCallID != "srv-http" || f.lastCallTool != "search_issues" {
		t.Errorf("backend call target = %s %s", f.lastCallID, f.lastCallTool)
	}
	if f.lastCallArgs["query"] != "hello" {
		t.Errorf("backend args = %v, want query=hello", f.lastCallArgs)
	}
	body := rec.Body.String()
	for _, want := range []string{`"type":"text"`, `"isError":false`, `"structuredContent":{"ok":true}`} {
		if !strings.Contains(body, want) {
			t.Errorf("call body missing %q (%s)", want, body)
		}
	}
}

func TestUIMCPServersCallToolBuiltinAllowed(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerJSON(e, http.MethodPost, "/api/mcp-servers/srv-builtin/tools/memory_lookup/call", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("builtin call = %d, want 200 (builtin tools are runnable)", rec.Code)
	}
	if f.lastCallTool != "memory_lookup" {
		t.Errorf("builtin call tool = %q", f.lastCallTool)
	}
}

func TestUIMCPServersCallToolBadBody(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerJSON(e, http.MethodPost, "/api/mcp-servers/srv-http/tools/search_issues/call", `{"arguments":[1,2]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("non-object arguments = %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
	if f.lastCallTool != "" {
		t.Error("invalid body must not reach the backend")
	}
}

func TestUIMCPServersCallToolFailureIsJSON(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	f.callErr = fmt.Errorf("memory 502 mcp_connection_error: server unreachable")
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerJSON(e, http.MethodPost, "/api/mcp-servers/srv-http/tools/search_issues/call", `{}`)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "server unreachable") {
		t.Errorf("call failure = %d %s, want 502 with the memory error", rec.Code, rec.Body.String())
	}
}

func TestUIMCPServersCallToolPreservesForbiddenStatus(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	f.callErr = &memoryHTTPError{Status: http.StatusForbidden, Code: "mcp_tool_forbidden", Message: "not authorized to invoke this tool"}
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerJSON(e, http.MethodPost, "/api/mcp-servers/srv-builtin/tools/web-fetch/call", `{}`)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "not authorized") {
		t.Errorf("forbidden call = %d %s, want 403 with the memory message", rec.Code, rec.Body.String())
	}
}

// TestUIMCPServersListDoesNotFetchGroups pins the deferred half of #210: the
// list page must not carry the builtin capability taxonomy (or any tool rows) —
// a row fetches them when it is first expanded.
func TestUIMCPServersListDoesNotFetchGroups(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	f.builtinGroups = []BuiltinToolGroup{
		{ID: "graph", Label: "Graph", Enabled: true, Tools: []string{"memory_lookup"}},
	}
	e := mcpServerTestServer(&Server{cfg: Config{}, memory: f})

	rec := mcpServerGet(e, "/settings/mcp-servers")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, `data-mcp-group="graph"`) || strings.Contains(body, "data-mcp-builtin-groups") {
		t.Error("list page must not render the capability groups — they belong to the deferred rows fragment")
	}
	if !strings.Contains(body, `data-mcp-tools-host="srv-builtin"`) {
		t.Error("builtin row must still carry its load-on-expand host")
	}
}

// TestUIMCPServerToolsFragmentGroupsBuiltin covers the fragment the expanded
// row fetches: the builtin server's rows render grouped by capability.
func TestUIMCPServerToolsFragmentGroupsBuiltin(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	f.builtinGroups = []BuiltinToolGroup{
		{ID: "graph", Label: "Graph", Enabled: true, Tools: []string{"memory_lookup"}},
	}
	e := mcpServerTestServer(&Server{cfg: Config{}, memory: f})

	rec := mcpServerGet(e, "/settings/mcp-servers/srv-builtin/tools")
	if rec.Code != http.StatusOK {
		t.Fatalf("tools fragment = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get(echo.HeaderContentType); !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want an HTML fragment", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-mcp-tools="srv-builtin"`) {
		t.Error("fragment must return the data-mcp-tools container the row host swaps in")
	}
	if !strings.Contains(body, `data-mcp-group="graph"`) || !strings.Contains(body, "memory_lookup") {
		t.Error("fragment must render the builtin rows grouped by capability")
	}
}

// TestUIMCPServerToolsFragmentGroupsFailureDegrades keeps the additive-endpoint
// contract: a missing/failing taxonomy falls back to the flat builtin rows
// rather than failing the row.
func TestUIMCPServerToolsFragmentGroupsFailureDegrades(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	f.builtinGroupsErr = fmt.Errorf("memory 404 not_found")
	e := mcpServerTestServer(&Server{cfg: Config{}, memory: f})

	rec := mcpServerGet(e, "/settings/mcp-servers/srv-builtin/tools")
	if rec.Code != http.StatusOK {
		t.Fatalf("fragment with groups failure = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "data-mcp-builtin-groups") {
		t.Error("groups failure must fall back to the flat builtin list")
	}
	if !strings.Contains(body, "memory_lookup") {
		t.Error("builtin tool rows must stay reachable when the groups fetch fails")
	}
}

// TestUIMCPServerToolsFragmentErrorRendersNote covers the failure path htmx
// cannot show by itself: a non-2xx response is never swapped, so the row's
// fetch failure has to arrive as a 200 with a readable note.
func TestUIMCPServerToolsFragmentErrorRendersNote(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	f.listErr = fmt.Errorf("backend unreachable")
	e := mcpServerTestServer(&Server{cfg: Config{}, memory: f})

	rec := mcpServerGet(e, "/settings/mcp-servers/srv-http/tools")
	if rec.Code != http.StatusOK {
		t.Fatalf("failed fragment = %d, want 200 with a rendered note", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Could not load cached tools") {
		t.Errorf("expected a readable failure note, got %s", body)
	}
	if !strings.Contains(body, `data-mcp-tools="srv-http"`) {
		t.Error("failure note must stay inside the rows container")
	}
}

// --- OAuth connect / disconnect routes ---

func TestUIMCPOAuthStartRedirectsToAuthorizeURL(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	f.authorizeResult = &MCPOAuthAuthorize{AuthorizeURL: "https://auth.example.com/authorize?client_id=abc", State: "opaque"}
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerGet(e, "/settings/mcp-servers/srv-http/oauth/start")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("oauth/start = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "https://auth.example.com/authorize?client_id=abc" {
		t.Errorf("Location = %q, want the authorize URL", loc)
	}
	if f.lastOAuthID != "srv-http" {
		t.Errorf("authorize target = %q, want srv-http", f.lastOAuthID)
	}
}

// TestUIMCPOAuthStartHTMXRedirectsViaHXRedirect covers the boosted caller: the
// Connect link sits inside the boosted <main>, so htmx hits this route with
// HX-Request. htmx fetches with mode "same-origin" and cannot follow the
// cross-origin 303 the plain path emits, so the route answers HX-Redirect (a
// top-level navigation to the absolute authorize URL) instead.
func TestUIMCPOAuthStartHTMXRedirectsViaHXRedirect(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	f.authorizeResult = &MCPOAuthAuthorize{AuthorizeURL: "https://auth.example.com/authorize?client_id=abc", State: "opaque"}
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/settings/mcp-servers/srv-http/oauth/start", nil)
	req.Header.Set("HX-Request", "true")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("htmx oauth/start = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("HX-Redirect"); got != "https://auth.example.com/authorize?client_id=abc" {
		t.Errorf("HX-Redirect = %q, want the authorize URL", got)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("Location = %q, want empty for an htmx caller", loc)
	}
}

func TestUIMCPOAuthStartErrorRedirectsToList(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	f.authorizeErr = fmt.Errorf("memory 400 bad_request: server does not support OAuth")
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerGet(e, "/settings/mcp-servers/srv-stdio/oauth/start")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/settings/mcp-servers?err=") {
		t.Fatalf("oauth/start error = %d %q, want 303 back to the list with an error", rec.Code, rec.Header().Get("Location"))
	}
}

func TestUIMCPOAuthStartEmptyID(t *testing.T) {
	f := newMCPTestBackend(nil)
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerGet(e, "/settings/mcp-servers//oauth/start")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/settings/mcp-servers?err=") {
		t.Fatalf("empty-id oauth/start = %d %q, want error redirect", rec.Code, rec.Header().Get("Location"))
	}
	if f.lastOAuthID != "" {
		t.Error("empty id must not reach the backend")
	}
}

func TestUIMCPOAuthDisconnectRedirectsWithFlash(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerPost(e, "/settings/mcp-servers/srv-http/oauth/disconnect", "")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/settings/mcp-servers?disconnected=1" {
		t.Fatalf("disconnect = %d %q, want 303 /settings/mcp-servers?disconnected=1", rec.Code, rec.Header().Get("Location"))
	}
	if f.lastOAuthID != "srv-http" {
		t.Errorf("disconnect target = %q, want srv-http", f.lastOAuthID)
	}
}

func TestUIMCPOAuthDisconnectErrorRedirectsToList(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	f.disconnectErr = fmt.Errorf("memory 500 internal: failed to clear tokens")
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerPost(e, "/settings/mcp-servers/srv-http/oauth/disconnect", "")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/settings/mcp-servers?err=") {
		t.Fatalf("disconnect error = %d %q, want 303 back to the list with an error", rec.Code, rec.Header().Get("Location"))
	}
}

// --- pre-registered OAuth client routes ---

func TestUIMCPOAuthClientRedirectsWithFlash(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	body := "client_id=client-abc&client_secret=secret-xyz&scope=repo+read%3Aorg"
	rec := mcpServerPost(e, "/settings/mcp-servers/srv-http/oauth/client", body)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/settings/mcp-servers?oauthClient=1" {
		t.Fatalf("oauth/client = %d %q, want 303 /settings/mcp-servers?oauthClient=1", rec.Code, rec.Header().Get("Location"))
	}
	if f.lastClientServer != "srv-http" || f.lastClientID != "client-abc" || f.lastClientSecret != "secret-xyz" {
		t.Errorf("backend record = %s %q %q, want srv-http client-abc secret-xyz", f.lastClientServer, f.lastClientID, f.lastClientSecret)
	}
	if f.lastClientScope != "repo read:org" {
		t.Errorf("backend scope = %q, want the submitted scope set", f.lastClientScope)
	}
}

func TestUIMCPOAuthClientBlankIDRedirectsWithError(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerPost(e, "/settings/mcp-servers/srv-http/oauth/client", "client_id=&client_secret=secret-xyz")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/settings/mcp-servers?err=") {
		t.Fatalf("blank-id oauth/client = %d %q, want 303 back to the list with an error", rec.Code, rec.Header().Get("Location"))
	}
	if f.lastClientServer != "" {
		t.Error("blank client id must not reach the backend")
	}
}

func TestUIMCPOAuthClientScopeOnlyUpdate(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	// A blank client id with a scope is the DCR-provider remedy: keep the
	// stored client, change only what the next connect requests.
	rec := mcpServerPost(e, "/settings/mcp-servers/srv-http/oauth/client", "client_id=&scope=repo+read%3Aorg")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/settings/mcp-servers?oauthClient=1" {
		t.Fatalf("scope-only oauth/client = %d %q, want 303 success", rec.Code, rec.Header().Get("Location"))
	}
	if f.lastClientServer != "srv-http" || f.lastClientID != "" || f.lastClientScope != "repo read:org" {
		t.Errorf("backend record = %s %q %q, want srv-http blank-client repo read:org", f.lastClientServer, f.lastClientID, f.lastClientScope)
	}
}

func TestUIMCPOAuthClientBackendErrorRedirectsToList(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	f.oauthClientErr = fmt.Errorf("memory 400 bad_request: server is not a remote http/sse server")
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerPost(e, "/settings/mcp-servers/srv-http/oauth/client", "client_id=client-abc&client_secret=secret-xyz")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/settings/mcp-servers?err=") {
		t.Fatalf("oauth/client error = %d %q, want 303 back to the list with an error", rec.Code, rec.Header().Get("Location"))
	}
}

func TestUIMCPOAuthClientDoesNotLeakSecret(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	f.oauthClientErr = fmt.Errorf("memory 400 bad_request: client id is required")
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	const secret = "super-secret-value-123"
	rec := mcpServerPost(e, "/settings/mcp-servers/srv-http/oauth/client", "client_id=&client_secret="+secret)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("failed submit = %d, want a PRG redirect", rec.Code)
	}
	if strings.Contains(rec.Body.String(), secret) || strings.Contains(rec.Header().Get("Location"), secret) {
		t.Error("a failed submit must not leak the client secret into the response body or redirect URL")
	}
}

func TestUIMCPOAuthClientClearRedirectsWithFlash(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerPost(e, "/settings/mcp-servers/srv-http/oauth/client/clear", "")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/settings/mcp-servers?oauthClientCleared=1" {
		t.Fatalf("oauth/client/clear = %d %q, want 303 /settings/mcp-servers?oauthClientCleared=1", rec.Code, rec.Header().Get("Location"))
	}
	if f.lastClientServer != "srv-http" {
		t.Errorf("clear target = %q, want srv-http", f.lastClientServer)
	}
}

func TestUIMCPOAuthClientClearErrorRedirectsToList(t *testing.T) {
	f := newMCPTestBackend(mcpTestRegistryFixture())
	f.clearClientErr = fmt.Errorf("memory 500 internal: failed to clear client")
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerPost(e, "/settings/mcp-servers/srv-http/oauth/client/clear", "")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/settings/mcp-servers?err=") {
		t.Fatalf("oauth/client/clear error = %d %q, want 303 back to the list with an error", rec.Code, rec.Header().Get("Location"))
	}
}

func TestUIMCPOAuthClientEmptyID(t *testing.T) {
	f := newMCPTestBackend(nil)
	s := &Server{cfg: Config{}, memory: f}
	e := mcpServerTestServer(s)

	rec := mcpServerPost(e, "/settings/mcp-servers//oauth/client", "client_id=client-abc")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/settings/mcp-servers?err=") {
		t.Fatalf("empty-id oauth/client = %d %q, want error redirect", rec.Code, rec.Header().Get("Location"))
	}
	if f.lastClientServer != "" {
		t.Error("empty id must not reach the backend")
	}
}

// --- OAuth landing / flash states on the list page ---

func TestUIMCPServersOAuthLandingFlash(t *testing.T) {
	cases := []struct {
		query string
		want  string
	}{
		{"?mcp_oauth=connected", "MCP server connected via OAuth."},
		{"?mcp_oauth=denied", "OAuth authorization was denied"},
		{"?mcp_oauth=error", "OAuth connection failed"},
		{"?disconnected=1", "MCP server disconnected."},
		{"?created=1&requiresAuth=1", "This server requires authorization — connect it below to finish setup."},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			f := newMCPTestBackend(mcpTestRegistryFixture())
			s := &Server{cfg: Config{}, memory: f}
			e := mcpServerTestServer(s)

			rec := mcpServerGet(e, "/settings/mcp-servers"+tc.query)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET = %d, want 200", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Errorf("flash for %q missing %q", tc.query, tc.want)
			}
		})
	}
}
