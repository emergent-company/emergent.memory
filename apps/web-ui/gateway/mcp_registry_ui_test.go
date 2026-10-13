package main

import (
	"strings"
	"testing"
)

// Render tests for the MCP Registry page. Assert the contract — data-testid /
// data-registry-name anchors, the install form action, badge labels, and
// omission where it matters (no install action for stdio-only entries) — not
// class strings.

// mcpRegistryPageFixture returns two registry entries: a remote+package entry
// (installable) and a stdio-only entry (connector guidance).
func mcpRegistryPageFixture() []MCPRegistryServer {
	return []MCPRegistryServer{
		{
			Name:        "io.github.github/github-mcp-server",
			Title:       "GitHub",
			Description: "Official GitHub MCP server",
			Version:     "1.0.0",
			HasRemotes:  true,
			HasPackages: true,
			Remotes:     []MCPRegistryRemote{{Type: "streamable-http", URL: "https://api.githubcopilot.com/mcp/"}},
			EnvVars:     []MCPRegistryEnvVar{{Name: "GITHUB_PERSONAL_ACCESS_TOKEN", IsRequired: true, IsSecret: true}},
		},
		{
			Name:        "io.github.ChromeDevTools/chrome-devtools-mcp",
			Title:       "Chrome DevTools",
			Description: "Chrome DevTools automation",
			Version:     "0.11.0",
			HasRemotes:  false,
			HasPackages: true,
		},
	}
}

func TestRenderMCPRegistryPage(t *testing.T) {
	html := renderHTML(t, MCPServerRegistryPage(mcpRegistryPageData{
		Servers: mcpRegistryPageFixture(),
		Count:   2,
	}))
	for _, want := range []string{
		"MCP Registry",
		"2 results",
		// search box
		`data-testid="mcp-registry-search"`,
		`data-testid="mcp-registry-search-submit"`,
		`action="/settings/mcp-registry"`,
		// remote entry — install action present
		`data-testid="mcp-registry-entry"`,
		`data-registry-name="io.github.github/github-mcp-server"`,
		"GitHub", "Official GitHub MCP server",
		`action="/settings/mcp-registry/install"`,
		`name="registryName" value="io.github.github/github-mcp-server"`,
		`data-testid="mcp-registry-install"`,
		// install action sits in the entry's right-hand action column
		`data-testid="mcp-registry-entry-actions"`,
		// stdio-only entry — connector guidance, no install form for it
		`data-registry-name="io.github.ChromeDevTools/chrome-devtools-mcp"`,
		"Chrome DevTools",
		`data-testid="mcp-registry-connector-guidance"`,
		"macOS connector",
		// availability badges
		">remote<", ">package<",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("MCPServerRegistryPage missing %q", want)
		}
	}
}

// TestRenderMCPRegistryStdioOnlyNoInstall asserts the stdio-only entry renders
// exactly one install form (the remote entry's) — the stdio-only entry offers
// no server-side install action.
func TestRenderMCPRegistryStdioOnlyNoInstall(t *testing.T) {
	html := renderHTML(t, MCPServerRegistryPage(mcpRegistryPageData{Servers: mcpRegistryPageFixture()}))
	if got := strings.Count(html, `action="/settings/mcp-registry/install"`); got != 1 {
		t.Errorf("install form count = %d, want 1 (remote entry only)", got)
	}
	if got := strings.Count(html, `data-testid="mcp-registry-install"`); got != 1 {
		t.Errorf("install button count = %d, want 1 (remote entry only)", got)
	}
	// the stdio-only entry name never appears inside an install form's hidden
	// registryName field.
	if strings.Contains(html, `name="registryName" value="io.github.ChromeDevTools/chrome-devtools-mcp"`) {
		t.Error("stdio-only entry must not render an install form with its name")
	}
	if got := strings.Count(html, `data-testid="mcp-registry-connector-guidance"`); got != 1 {
		t.Errorf("connector guidance count = %d, want 1 (stdio-only entry only)", got)
	}
}

// TestRenderMCPRegistryLoadError asserts the whole-page error state.
func TestRenderMCPRegistryLoadError(t *testing.T) {
	html := renderHTML(t, MCPServerRegistryPage(mcpRegistryPageData{LoadErr: errTest}))
	if !strings.Contains(html, "MCP registry unavailable") || !strings.Contains(html, "backend unreachable") {
		t.Error("load-error state missing")
	}
}

// TestRenderMCPRegistryEmptyStates asserts the pre-search hint and the
// no-results state, keyed on the query.
func TestRenderMCPRegistryEmptyStates(t *testing.T) {
	hint := renderHTML(t, MCPServerRegistryPage(mcpRegistryPageData{}))
	for _, want := range []string{"Search recommended servers", "Find MCP servers by name"} {
		if !strings.Contains(hint, want) {
			t.Errorf("pre-search empty state missing %q", want)
		}
	}
	none := renderHTML(t, MCPServerRegistryPage(mcpRegistryPageData{Query: "zzz"}))
	for _, want := range []string{"No matching servers", "zzz"} {
		if !strings.Contains(none, want) {
			t.Errorf("no-results state missing %q", want)
		}
	}
}

// TestRenderMCPRegistryNextLink asserts the next-page affordance renders only
// when a cursor is present and preserves the query.
func TestRenderMCPRegistryNextLink(t *testing.T) {
	withCursor := renderHTML(t, MCPServerRegistryPage(mcpRegistryPageData{
		Query:      "github",
		Servers:    mcpRegistryPageFixture(),
		NextCursor: "cursor-1",
	}))
	for _, want := range []string{
		`data-testid="mcp-registry-next"`,
		`cursor=cursor-1`,
		`q=github`,
	} {
		if !strings.Contains(withCursor, want) {
			t.Errorf("next link missing %q", want)
		}
	}
	noCursor := renderHTML(t, MCPServerRegistryPage(mcpRegistryPageData{Servers: mcpRegistryPageFixture()}))
	if strings.Contains(noCursor, `data-testid="mcp-registry-next"`) {
		t.Error("next link must not render without a next cursor")
	}
}

// TestMCPRegistryDisplayTitle asserts the title-preferred label.
func TestMCPRegistryDisplayTitle(t *testing.T) {
	if got := mcpRegistryDisplayTitle(MCPRegistryServer{Name: "io.github.x/y", Title: "X Y"}); got != "X Y" {
		t.Errorf("display title = %q, want title", got)
	}
	if got := mcpRegistryDisplayTitle(MCPRegistryServer{Name: "io.github.x/y"}); got != "io.github.x/y" {
		t.Errorf("display title = %q, want name fallback", got)
	}
}

// TestMCPRegistryRequiredEnvNames asserts the env-name join.
func TestMCPRegistryRequiredEnvNames(t *testing.T) {
	res := &MCPRegistryInstallResult{
		RequiredEnvVars: []MCPRegistryEnvVar{
			{Name: "A"},
			{Name: "B"},
		},
	}
	if got := mcpRegistryRequiredEnvNames(res); got != "A, B" {
		t.Errorf("env names = %q, want A, B", got)
	}
	if got := mcpRegistryRequiredEnvNames(&MCPRegistryInstallResult{}); got != "" {
		t.Errorf("env names (empty) = %q, want empty", got)
	}
	if got := mcpRegistryRequiredEnvNames(nil); got != "" {
		t.Errorf("env names (nil) = %q, want empty", got)
	}
}
