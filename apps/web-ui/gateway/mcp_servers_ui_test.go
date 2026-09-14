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
		"search_issues", "create_issue", "read_file", "memory_lookup",
		"New server", `href="/settings/mcp-servers/new"`,
		`data-mcp-tools="srv-http"`, `data-mcp-tools="srv-stdio"`,
		// external row actions
		`href="/settings/mcp-servers/srv-http/edit"`,
		`href="/settings/mcp-servers/srv-stdio/edit"`,
		`action="/settings/mcp-servers/srv-http/delete"`,
		`action="/settings/mcp-servers/srv-stdio/delete"`,
		`data-mcp-action="sync"`, `data-mcp-action="inspect"`,
		"data-mcp-enabled-toggle", "data-mcp-tool-toggle",
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
	// per-tool toggles render only for the three external tools (builtin rows
	// are read-only); the enabled toggle only on the two external rows
	if got := strings.Count(html, `class="toggle toggle-sm shrink-0"`); got != 3 {
		t.Errorf("per-tool toggle occurrences = %d, want 3 (builtin tools read-only)", got)
	}
	for _, want := range []string{`aria-label="Enable github-mcp"`, `aria-label="Enable files"`} {
		if !strings.Contains(html, want) {
			t.Errorf("external row enabled toggle missing %q", want)
		}
	}
}

// TestRenderMCPServersPageEmpty asserts the shared EmptyState with the CTA.
func TestRenderMCPServersPageEmpty(t *testing.T) {
	html := renderHTML(t, MCPServersPage(mcpServersPageData{}))
	for _, want := range []string{
		"No MCP servers yet", `href="/settings/mcp-servers/new"`, "Register a server",
		"0 servers",
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
