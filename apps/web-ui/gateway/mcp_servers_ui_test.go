package main

import (
	"strings"
	"testing"
)

// Render tests for the MCP Servers pages (list / create / edit). Fixtures
// share mcpTestRegistryFixture from mcp_servers_handlers_test.go so the route
// tests and the render tests assert the same payload shapes.

// TestRenderMCPServersPage asserts the list renders every registered server
// (builtin + external) with its type badge, enabled state, cached tool count,
// the expandable tool rows, per-row actions for external servers — and no
// plaintext header values.
func TestRenderMCPServersPage(t *testing.T) {
	html := renderHTML(t, MCPServersPage(mcpServersPageData{Servers: mcpTestRegistryFixture()}))
	for _, want := range []string{
		"MCP Servers", "github-mcp", "files", "builtin",
		"3 servers",
		"2 tools", "1 tool",
		"New server", `href="/settings/mcp-servers/new"`,
		// The tool rows are deferred: every row carries a load-on-expand host
		// pointing at the fragment endpoint (#210).
		`data-mcp-tools-host="srv-http"`, `data-mcp-tools-host="srv-stdio"`,
		`data-mcp-tools-host="srv-builtin"`,
		`data-mcp-tools-url="/settings/mcp-servers/srv-http/tools"`,
		"Expand to load the cached tools.",
		// external row actions
		`href="/settings/mcp-servers/srv-http/edit"`,
		`href="/settings/mcp-servers/srv-stdio/edit"`,
		`action="/settings/mcp-servers/srv-http/delete"`,
		`action="/settings/mcp-servers/srv-stdio/delete"`,
		`data-mcp-action="sync"`, `data-mcp-action="inspect"`,
		"data-mcp-enabled-toggle",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("MCPServersPage missing %q", want)
		}
	}
	if strings.Contains(html, "secret-token") {
		t.Error("list page must never render header values (plaintext secrets)")
	}
	// builtin row: read-only
	if strings.Contains(html, `href="/settings/mcp-servers/srv-builtin/edit"`) ||
		strings.Contains(html, `action="/settings/mcp-servers/srv-builtin/delete"`) {
		t.Error("builtin row must not offer edit or delete actions")
	}
	if strings.Contains(html, `aria-label="Enable builtin"`) {
		t.Error("builtin row must not offer the enabled toggle")
	}
	if got := strings.Count(html, `data-mcp-action="sync"`); got != 2 {
		t.Errorf("sync action occurrences = %d, want 2 (external rows only)", got)
	}
	// The tool rows are not part of the page any more: rendering a 141-row
	// builtin catalogue up front was half the page's weight inside a collapsed
	// row (#210). They arrive from the fragment endpoint on first expand.
	for _, unwanted := range []string{
		"search_issues", "create_issue", "read_file", "memory_lookup", "graph_write",
	} {
		if strings.Contains(html, unwanted) {
			t.Errorf("list page still renders deferred tool-row markup %q", unwanted)
		}
	}
	for _, want := range []string{`aria-label="Enable github-mcp"`, `aria-label="Enable files"`} {
		if !strings.Contains(html, want) {
			t.Errorf("external row enabled toggle missing %q", want)
		}
	}
}

// TestRenderMCPServerToolsContainer covers the deferred half: the container the
// fragment endpoint returns on a row's first expand must render the full rows,
// including the per-tool toggles the page no longer ships.
func TestRenderMCPServerToolsContainer(t *testing.T) {
	fixture := mcpTestRegistryFixture()

	httpServer := renderHTML(t, mcpServerToolsContainer(fixture[0], fixture[0].Tools, nil))
	for _, want := range []string{
		"search_issues", "create_issue",
		`data-mcp-tools="srv-http"`, `data-mcp-tool="t1"`, `data-mcp-tool-toggle`,
	} {
		if !strings.Contains(httpServer, want) {
			t.Errorf("external tools container missing %q", want)
		}
	}
	if got := strings.Count(httpServer, `class="toggle toggle-sm shrink-0"`); got != 2 {
		t.Errorf("per-tool toggle occurrences = %d, want 2", got)
	}

	builtin := renderHTML(t, mcpServerToolsContainer(fixture[2], fixture[2].Tools, nil))
	if !strings.Contains(builtin, "memory_lookup") {
		t.Error("builtin tools container missing its rows")
	}
	if strings.Contains(builtin, `data-mcp-tool-toggle`) {
		t.Error("builtin rows are read-only: no per-tool toggles")
	}
}

// TestRenderMCPServersBuiltinIdentity asserts the builtin server renders as
// "Memory tools" with the package mark + a distinct Built-in badge, and that
// external rows keep their transport type badge.
func TestRenderMCPServersBuiltinIdentity(t *testing.T) {
	html := renderHTML(t, MCPServersPage(mcpServersPageData{Servers: mcpTestRegistryFixture()}))
	for _, want := range []string{
		"Memory tools",
		"Built-in",
		"lucide--package",
		// external rows keep the transport badge
		">http<", ">stdio<",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("MCPServersPage missing %q", want)
		}
	}
	// The builtin row must not render the raw transport type badge (the old
	// duplicated "builtin" badge).
	if strings.Contains(html, ">builtin<") {
		t.Error("builtin row still renders the literal type badge")
	}
}

// TestRenderMCPServersBuiltinGrouped asserts the builtin server's cached tools
// render grouped (collapsible per-capability disclosures + the dashed Other
// fallback) when the taxonomy is present. The groups live in the deferred
// fragment now, so this renders the container the fragment endpoint returns.
func TestRenderMCPServersBuiltinGrouped(t *testing.T) {
	groups := []BuiltinToolGroup{
		{ID: "graph", Label: "Graph", Description: "Entity and relation tools", Enabled: true, Tools: []string{"memory_lookup"}},
	}
	fixture := mcpTestRegistryFixture()
	builtin := fixture[2]
	html := renderHTML(t, mcpServerToolsContainer(builtin, builtin.Tools, groups))
	for _, want := range []string{
		"data-mcp-builtin-groups",
		`data-testid="mcp-builtin-group"`,
		`data-mcp-group="graph"`,
		"Graph", "Entity and relation tools", "lucide--layers",
		// the uncovered tool stays reachable in the Other fallback
		`data-testid="mcp-builtin-group-other"`,
		"Other",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("grouped builtin tools container missing %q", want)
		}
	}
}

// TestMCPBuiltinToolGroupsJoin asserts the group join: a group carries the rows
// whose ToolName it lists, in the group's membership order; names with no
// cached row are dropped; uncovered tools fall through to other.
func TestMCPBuiltinToolGroupsJoin(t *testing.T) {
	tools := []MCPTool{{ToolName: "a"}, {ToolName: "b"}, {ToolName: "c"}}
	groups := []BuiltinToolGroup{
		{ID: "g1", Label: "One", Tools: []string{"c", "a", "missing"}},
		{ID: "empty", Label: "Empty", Tools: []string{"missing"}},
	}
	grouped, other := mcpBuiltinToolGroups(tools, groups)
	if len(grouped) != 1 || grouped[0].Group.ID != "g1" {
		t.Fatalf("grouped = %+v, want only g1 (empty groups dropped)", grouped)
	}
	if len(grouped[0].Rows) != 2 || grouped[0].Rows[0].ToolName != "c" || grouped[0].Rows[1].ToolName != "a" {
		t.Errorf("group rows = %+v, want [c a] in membership order", grouped[0].Rows)
	}
	if len(other) != 1 || other[0].ToolName != "b" {
		t.Errorf("other = %+v, want [b]", other)
	}
}

// TestRenderMCPServersInvokeAffordances asserts every tool row carries a Run
// button, and the page mounts the shared invoke dialog with its args input,
// run button, and result region.
func TestRenderMCPServersInvokeAffordances(t *testing.T) {
	// The dialog is page-level; the per-tool Run affordances live in the
	// deferred rows container, so both halves are asserted.
	page := renderHTML(t, MCPServersPage(mcpServersPageData{Servers: mcpTestRegistryFixture()}))
	for _, want := range []string{
		`data-testid="mcp-tool-invoke-dialog"`,
		`data-testid="mcp-tool-invoke-args"`,
		`data-testid="mcp-tool-invoke-run"`,
		`data-testid="mcp-tool-invoke-result"`,
		"data-mcp-invoke-open",
		"data-mcp-invoke-run",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("MCPServersPage missing invoke affordance %q", want)
		}
	}

	fixture := mcpTestRegistryFixture()
	rows := renderHTML(t, mcpServerToolsContainer(fixture[0], fixture[0].Tools, nil))
	for _, want := range []string{`data-mcp-tool="search_issues"`, `data-mcp-tool="create_issue"`} {
		if !strings.Contains(rows, want) {
			t.Errorf("tools container missing invoke affordance %q", want)
		}
	}
	builtin := renderHTML(t, mcpServerToolsContainer(fixture[2], fixture[2].Tools, nil))
	if !strings.Contains(builtin, `data-mcp-tool="memory_lookup"`) {
		t.Error("builtin tools container missing its invoke affordance")
	}
}

// TestRenderMCPServersAddDialog asserts the page header carries one "Add
// server" action on both the populated and empty list, and that the dialog it
// opens offers the two add paths with the right destinations: the manual create
// form and the registry browse page (whose per-entry install posts to the
// existing /settings/mcp-registry/install handler).
func TestRenderMCPServersAddDialog(t *testing.T) {
	for _, tc := range []struct {
		name string
		data mcpServersPageData
	}{
		{"populated", mcpServersPageData{Servers: mcpTestRegistryFixture()}},
		{"empty", mcpServersPageData{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			html := renderHTML(t, MCPServersPage(tc.data))
			for _, want := range []string{
				// header action (populated pages had no add affordance before)
				`data-testid="mcp-server-add"`,
				`data-dialog-open="mcp-server-add-dialog"`,
				"Add server",
				// dialog shell + title
				`id="mcp-server-add-dialog"`,
				`data-testid="mcp-server-add-dialog"`,
				"Add an MCP server",
				// path 1: the existing manual form
				`data-testid="mcp-server-add-new"`,
				`href="/settings/mcp-servers/new"`,
				"Register a server",
				"New server",
				// path 2: the existing registry browse page
				`data-testid="mcp-server-add-registry"`,
				`href="/settings/mcp-registry"`,
				"Browse the MCP registry",
				"Open registry",
			} {
				if !strings.Contains(html, want) {
					t.Errorf("%s MCPServersPage missing add-flow %q", tc.name, want)
				}
			}
		})
	}
}

// TestRenderMCPServersPageEmpty asserts the shared EmptyState with the CTA.
func TestRenderMCPServersPageEmpty(t *testing.T) {
	html := renderHTML(t, MCPServersPage(mcpServersPageData{}))
	for _, want := range []string{
		"No MCP servers yet", `href="/settings/mcp-servers/new"`, "Register a server",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("empty MCPServersPage missing %q", want)
		}
	}
	if strings.Contains(html, `<table`) {
		t.Error("empty list must not render a table")
	}
}

// TestRenderMCPServersPageLoadError asserts the whole-page error state.
func TestRenderMCPServersPageLoadError(t *testing.T) {
	html := renderHTML(t, MCPServersPage(mcpServersPageData{LoadErr: errTest}))
	if !strings.Contains(html, "MCP servers unavailable") || !strings.Contains(html, "backend unreachable") {
		t.Error("load-error state missing")
	}
}

// TestRenderMCPServerNewPage asserts the create page defaults to stdio (radio
// checked, stdio panel visible) and renders name, transport radios, the stdio
// command/args/env fields, and the create action.
func TestRenderMCPServerNewPage(t *testing.T) {
	data := mcpServerFormData{Type: "stdio", Enabled: true}
	html := renderHTML(t, MCPServerNewPage(data))
	for _, want := range []string{
		"New MCP server", `action="/settings/mcp-servers/new"`,
		`name="name"`, "Create server", `href="/settings/mcp-servers"`, "Cancel",
		// stdio selected by default
		`value="stdio" class="radio radio-sm mt-0.5" data-mcp-transport checked`,
		`value="sse" class="radio radio-sm mt-0.5" data-mcp-transport`,
		`value="http" class="radio radio-sm mt-0.5" data-mcp-transport`,
		// stdio panel fields
		`name="command"`, `name="args"`, `name="env.key"`, `name="env.value"`,
		// remote panel present but hidden
		`data-mcp-transport-panel="remote" hidden`,
		// kv add/remove affordances + plaintext note
		"data-mcp-kv-add", "data-mcp-kv-remove",
		"Values are stored in plaintext and shown only in this form.",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("create page missing %q", want)
		}
	}
	if strings.Contains(html, "Save changes") {
		t.Error("create page must not render the edit submit label")
	}
}

// TestRenderMCPServerNewPageRemote asserts an http-prefilled create draft
// checks the http radio and shows the remote panel with the url + headers.
func TestRenderMCPServerNewPageRemote(t *testing.T) {
	data := mcpServerFormData{
		Type:    "http",
		Enabled: true,
		URL:     "https://example.com/mcp",
		Headers: []mcpKVField{{Key: "Authorization", Value: "Bearer xyz"}},
	}
	html := renderHTML(t, MCPServerNewPage(data))
	for _, want := range []string{
		`value="http" class="radio radio-sm mt-0.5" data-mcp-transport checked`,
		`value="https://example.com/mcp"`,
		`name="headers.key" value="Authorization"`,
		`name="headers.value" value="Bearer xyz"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("http-prefilled create page missing %q", want)
		}
	}
	if !strings.Contains(html, `data-mcp-transport-panel="stdio" hidden`) {
		t.Error("stdio panel must stay hidden when http is selected")
	}
}

// TestRenderMCPServerNewPageValidationErrors asserts inline field errors render
// under the offending groups with the draft values preserved.
func TestRenderMCPServerNewPageValidationErrors(t *testing.T) {
	data := mcpServerFormData{
		Type:    "http",
		Name:    "gh",
		Enabled: true,
		FieldErrs: mcpServerFieldErrs{
			Name: "Server name is required.",
			URL:  "A URL is required for http servers.",
		},
	}
	html := renderHTML(t, MCPServerNewPage(data))
	for _, want := range []string{
		"Server name is required.", "A URL is required for http servers.",
		`value="gh"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("create validation re-render missing %q", want)
		}
	}
}

// TestRenderMCPServerEditPagePrefills asserts the edit form is pre-filled with
// the server's connection details — including header values (the edit form is
// the one place plaintext config is shown).
func TestRenderMCPServerEditPagePrefills(t *testing.T) {
	servers := mcpTestRegistryFixture()
	data := mcpServerFormDataFromServer(servers[0])
	html := renderHTML(t, MCPServerEditPage(data))
	for _, want := range []string{
		"Edit server", "github-mcp",
		`action="/settings/mcp-servers/srv-http/update"`,
		"Save changes", `href="/settings/mcp-servers"`,
		`value="github-mcp"`, `value="https://github.example.com/mcp"`,
		`value="http" class="radio radio-sm mt-0.5" data-mcp-transport checked`,
		`name="headers.key" value="Authorization"`,
		`name="headers.value" value="Bearer secret-token"`,
		// remote panel visible, stdio hidden
		`data-mcp-transport-panel="remote"`,
		`data-mcp-transport-panel="stdio" hidden`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("edit page missing %q", want)
		}
	}
}

// TestRenderMCPServerEditPageStates asserts the not-found and load-error states.
func TestRenderMCPServerEditPageStates(t *testing.T) {
	nf := renderHTML(t, MCPServerEditPage(mcpServerFormData{ID: "nope", NotFound: true}))
	if !strings.Contains(nf, "MCP server not found") {
		t.Error("edit not-found state missing")
	}
	le := renderHTML(t, MCPServerEditPage(mcpServerFormData{ID: "x", LoadErr: errTest}))
	if !strings.Contains(le, "Server unavailable") || !strings.Contains(le, "backend unreachable") {
		t.Error("edit load-error state missing")
	}
}

// TestRenderMCPServerEditPageStdioPrefills asserts a stdio server's edit form
// shows command/args/env pre-fill and the stdio panel.
func TestRenderMCPServerEditPageStdioPrefills(t *testing.T) {
	servers := mcpTestRegistryFixture()
	data := mcpServerFormDataFromServer(servers[1])
	html := renderHTML(t, MCPServerEditPage(data))
	for _, want := range []string{
		`value="stdio" class="radio radio-sm mt-0.5" data-mcp-transport checked`,
		`value="npx -y files-mcp"`,
		`data-mcp-transport-panel="stdio"`,
		`data-mcp-transport-panel="remote" hidden`,
		`action="/settings/mcp-servers/srv-stdio/update"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("stdio edit page missing %q", want)
		}
	}
}

// --- OAuth connect controls ---

// mcpOAuthServerFixture builds one remote server carrying an OAuth summary so
// the OAuth-control render tests don't disturb the shared registry fixture.
func mcpOAuthServerFixture(connected bool) MCPServer {
	as := "https://auth.example.com"
	return MCPServer{
		ID: "srv-oauth", Name: "github-mcp", Type: "http", Enabled: true,
		URL:   "https://github.example.com/mcp",
		OAuth: &MCPServerOAuth{Connected: connected, AuthorizationServer: &as},
	}
}

// TestRenderMCPServersOAuthConnect asserts a remote server with an unconnected
// OAuth summary renders the Connect action (link to /oauth/start, opted out of
// hx-boost: the handler redirects to the provider's cross-origin authorization
// server, which a boosted fetch cannot follow) and the "Not connected" chip.
func TestRenderMCPServersOAuthConnect(t *testing.T) {
	html := renderHTML(t, MCPServersPage(mcpServersPageData{Servers: []MCPServer{mcpOAuthServerFixture(false)}}))
	for _, want := range []string{
		`data-testid="mcp-oauth-connect"`,
		`href="/settings/mcp-servers/srv-oauth/oauth/start"`,
		`hx-boost="false"`,
		"Connect",
		"Not connected",
		"lucide--plug-zap",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("oauth connect controls missing %q", want)
		}
	}
	if strings.Contains(html, "mcp-oauth-disconnect") {
		t.Error("unconnected server must not render a Disconnect action")
	}
}

// TestRenderMCPServersOAuthConnected asserts a connected server renders the
// Connected chip (with the authorization server) plus the Disconnect form.
func TestRenderMCPServersOAuthConnected(t *testing.T) {
	html := renderHTML(t, MCPServersPage(mcpServersPageData{Servers: []MCPServer{mcpOAuthServerFixture(true)}}))
	for _, want := range []string{
		`data-testid="mcp-oauth-status"`,
		"Connected",
		`data-testid="mcp-oauth-disconnect"`,
		`action="/settings/mcp-servers/srv-oauth/oauth/disconnect"`,
		"Disconnect",
		"lucide--unplug",
		"https://auth.example.com",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("oauth connected controls missing %q", want)
		}
	}
	if strings.Contains(html, `data-testid="mcp-oauth-connect"`) {
		t.Error("connected server must not render a Connect action")
	}
}

// TestRenderMCPServersOAuthHidden asserts builtin and stdio rows (and any
// server without an oauth object) render no OAuth controls.
func TestRenderMCPServersOAuthHidden(t *testing.T) {
	html := renderHTML(t, MCPServersPage(mcpServersPageData{Servers: mcpTestRegistryFixture()}))
	for _, want := range []string{
		`data-testid="mcp-oauth-connect"`,
		`data-testid="mcp-oauth-disconnect"`,
		`data-testid="mcp-oauth-status"`,
	} {
		if strings.Contains(html, want) {
			t.Errorf("non-OAuth rows must not render %q", want)
		}
	}
}

// TestRenderMCPServersOAuthClientDisclosure asserts a remote OAuth server
// renders the pre-registered-client disclosure with the client id/secret form
// and no "Client configured" chip.
func TestRenderMCPServersOAuthClientDisclosure(t *testing.T) {
	html := renderHTML(t, MCPServersPage(mcpServersPageData{Servers: []MCPServer{mcpOAuthServerFixture(false)}}))
	for _, want := range []string{
		`data-testid="mcp-oauth-client-disclosure"`,
		`data-testid="mcp-oauth-client-form"`,
		`action="/settings/mcp-servers/srv-oauth/oauth/client"`,
		`name="client_id"`,
		`name="client_secret"`,
		"Use a pre-registered OAuth client",
		`data-testid="mcp-oauth-client-save"`,
		"Save client",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("oauth client disclosure missing %q", want)
		}
	}
	if strings.Contains(html, `data-testid="mcp-oauth-client-status"`) {
		t.Error("unconfigured server must not render the Client configured chip")
	}
	// the secret input is a password field, never a pre-filled plaintext value
	if !strings.Contains(html, `type="password"`) {
		t.Error("client secret input must be a password field")
	}
}

// TestRenderMCPServersOAuthClientConfigured asserts a server with a stored
// pre-registered client renders the "Client configured" chip plus a Clear
// action.
func TestRenderMCPServersOAuthClientConfigured(t *testing.T) {
	srv := mcpOAuthServerFixture(false)
	srv.OAuth.ClientConfigured = true
	html := renderHTML(t, MCPServersPage(mcpServersPageData{Servers: []MCPServer{srv}}))
	for _, want := range []string{
		`data-testid="mcp-oauth-client-status"`,
		"Client configured",
		`data-testid="mcp-oauth-client-clear"`,
		`action="/settings/mcp-servers/srv-oauth/oauth/client/clear"`,
		"Clear",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("configured chip missing %q", want)
		}
	}
}
