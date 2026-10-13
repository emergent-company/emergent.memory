package main

import (
	"strings"
	"testing"
)

// Render tests for the MCP Registry page. Assert the contract — data-testid /
// data-registry-name anchors, the install form action, badge labels, and
// omission where it matters (no server-side install action for package-only
// entries) — not class strings.

// mcpRegistryPageFixture returns two registry entries: a remote+package entry
// (server-side install) and a package-only entry (device install).
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
		// package-only entry — a device install trigger + dialog, and no
		// server-side install form of its own
		`data-registry-name="io.github.ChromeDevTools/chrome-devtools-mcp"`,
		"Chrome DevTools",
		`data-testid="mcp-registry-install-to-device"`,
		`data-testid="mcp-registry-device-dialog"`,
		// no device connected — setup instructions with the connector commands
		`data-testid="mcp-registry-device-setup"`,
		"memory-connector auth login",
		"memory-connector relay --accept-managed-config",
		// availability badges
		">remote<", ">package<",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("MCPServerRegistryPage missing %q", want)
		}
	}
}

// TestRenderMCPRegistryStdioOnlyNoInstall asserts the package-only entry
// renders exactly one server-side install form (the remote entry's) — its own
// call to action is a device install, never a shared-host install. The device
// install form/dialog are asserted in
// TestRenderMCPRegistryPackageOnlyDeviceInstall (a connected device is required
// for the form to render).
func TestRenderMCPRegistryStdioOnlyNoInstall(t *testing.T) {
	html := renderHTML(t, MCPServerRegistryPage(mcpRegistryPageData{Servers: mcpRegistryPageFixture()}))
	if got := strings.Count(html, `action="/settings/mcp-registry/install"`); got != 1 {
		t.Errorf("server-side install form count = %d, want 1 (remote entry only)", got)
	}
	if got := strings.Count(html, `data-testid="mcp-registry-install"`); got != 1 {
		t.Errorf("server-side install button count = %d, want 1 (remote entry only)", got)
	}
	// the package-only entry never renders a server-side install form with its
	// name — the single /settings/mcp-registry/install form belongs to the
	// remote entry (asserted above), and the package-only entry's device form
	// targets /settings/mcp-registry/install-to-device instead.
}

// TestRenderMCPRegistryPackageOnlyDeviceInstall asserts the package-only entry
// offers a device install (trigger + dialog + form) and no server-side install
// form, while the remote entry keeps its install form and no device dialog.
func TestRenderMCPRegistryPackageOnlyDeviceInstall(t *testing.T) {
	html := renderHTML(t, MCPServerRegistryPage(mcpRegistryPageData{
		Servers: mcpRegistryPageFixture(),
		Devices: []RelaySession{{InstanceID: "dev-1", Version: "1.2.3"}},
	}))
	if got := strings.Count(html, `action="/settings/mcp-registry/install"`); got != 1 {
		t.Errorf("remote install form count = %d, want 1 (remote entry only)", got)
	}
	if got := strings.Count(html, `data-testid="mcp-registry-install"`); got != 1 {
		t.Errorf("remote install button count = %d, want 1 (remote entry only)", got)
	}
	if got := strings.Count(html, `action="/settings/mcp-registry/install-to-device"`); got != 1 {
		t.Errorf("device install form count = %d, want 1 (package-only entry only)", got)
	}
	if got := strings.Count(html, `data-testid="mcp-registry-install-to-device"`); got != 1 {
		t.Errorf("device install trigger count = %d, want 1 (package-only entry only)", got)
	}
	if got := strings.Count(html, `data-testid="mcp-registry-device-dialog"`); got != 1 {
		t.Errorf("device dialog count = %d, want 1 (package-only entry only)", got)
	}
}

// TestRenderMCPRegistryDeviceDialogWithDevices asserts that, with a connected
// device, the dialog offers a connector picker instead of setup instructions.
func TestRenderMCPRegistryDeviceDialogWithDevices(t *testing.T) {
	html := renderHTML(t, MCPServerRegistryPage(mcpRegistryPageData{
		Servers: mcpRegistryPageFixture(),
		Devices: []RelaySession{{InstanceID: "dev-1", Version: "1.2.3", ToolCount: 3}},
	}))
	for _, want := range []string{
		`data-testid="mcp-registry-device-select"`,
		`name="targetInstanceId"`,
		`<option value="dev-1">`,
		"dev-1 (v1.2.3)",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("device dialog missing %q", want)
		}
	}
	if strings.Contains(html, `data-testid="mcp-registry-device-setup"`) {
		t.Error("setup instructions must not render when a device is connected")
	}
	if strings.Contains(html, `data-testid="mcp-registry-device-error"`) {
		t.Error("error state must not render on a successful device fetch")
	}
}

// TestRenderMCPRegistryDeviceDialogDevicesError asserts a failed session-list
// fetch surfaces an error in the dialog rather than falsely claiming there are
// no devices to install to.
func TestRenderMCPRegistryDeviceDialogDevicesError(t *testing.T) {
	html := renderHTML(t, MCPServerRegistryPage(mcpRegistryPageData{
		Servers:    mcpRegistryPageFixture(),
		DevicesErr: errTest,
	}))
	if !strings.Contains(html, `data-testid="mcp-registry-device-error"`) {
		t.Error("device dialog must surface a session-list failure")
	}
	if strings.Contains(html, `data-testid="mcp-registry-device-setup"`) {
		t.Error("setup instructions must not render when the device list failed")
	}
	if strings.Contains(html, `data-testid="mcp-registry-device-select"`) {
		t.Error("device picker must not render when the device list failed")
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
