package main

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/emergent-company/go-daisy/render"
	"github.com/labstack/echo/v4"
)

// --- MCP registry browse/install page (/settings/mcp-registry) ---
//
// Browses the official MCP registry through memory and installs remote
// (http/sse) entries into the project. Stdio-only entries render connector
// guidance instead of an install action (memory's registry install is
// remote-only and rejects stdio packages). The install flow is PRG: POST →
// 303 → /settings/mcp-servers?created=1 with the required env var names
// carried in the ?env= param.

// mcpRegistrySettingsBase is the registry page path.
const mcpRegistrySettingsBase = "/settings/mcp-registry"

// mcpRegistryPageData is the payload for the registry browse page.
type mcpRegistryPageData struct {
	Query      string
	Servers    []MCPRegistryServer
	NextCursor string
	Count      int
	LoadErr    error
	FlashMsg   string
	FlashErr   error
}

// uiMCPRegistry renders the registry browse page (GET /settings/mcp-registry).
// ?q= / ?cursor= drive the search; a failed search renders the whole-page error
// state. ?installed=1 surfaces the PRG success flash (used when an install
// redirects back here rather than to the mcp-servers list).
func (s *Server) uiMCPRegistry(c echo.Context) error {
	ctx := c.Request().Context()
	data := mcpRegistryPageData{
		Query:    strings.TrimSpace(c.QueryParam("q")),
		FlashMsg: flashFromQuery(c, []flashParam{{key: "installed", msg: "MCP server installed."}}),
		FlashErr: flashError(c),
	}
	res, err := s.memory.SearchRegistry(ctx, data.Query, 0, strings.TrimSpace(c.QueryParam("cursor")))
	if err != nil {
		data.LoadErr = err
	} else if res != nil {
		data.Servers = res.Servers
		data.NextCursor = res.NextCursor
		data.Count = res.Count
	}
	return s.page(c, pageTitle("MCP Registry"), MCPServerRegistryPage(data))
}

// uiMCPRegistryInstall handles the install form (POST
// /settings/mcp-registry/install, PRG). It forwards the registry name to
// memory's remote-only install; a failure (including a stdio-only rejection)
// redirects back to the registry page with the error — memory rejects the
// install before any write, so no partially-created server survives. Success
// redirects to /settings/mcp-servers?created=1 and names the required env vars
// in the ?env= param so the list surfaces them.
func (s *Server) uiMCPRegistryInstall(c echo.Context) error {
	ctx := c.Request().Context()
	registryName := strings.TrimSpace(c.FormValue("registryName"))
	version := strings.TrimSpace(c.FormValue("version"))
	name := strings.TrimSpace(c.FormValue("name"))
	if registryName == "" {
		return redirectWithError(c, mcpRegistrySettingsBase, errRegistryNameRequired())
	}
	result, err := s.memory.InstallRegistryServer(ctx, registryName, version, name)
	if err != nil {
		return redirectWithError(c, mcpRegistrySettingsBase, err)
	}
	target := mcpServerSettingsBase + "?created=1"
	if names := mcpRegistryRequiredEnvNames(result); names != "" {
		target += "&env=" + url.QueryEscape(names)
	}
	render.RedirectAfterMutation(c.Response().Writer, c.Request(), target)
	return nil
}

// mcpRegistryRequiredEnvNames joins the required environment variable names
// from an install result into a single ", "-separated string for the success
// flash (empty when the result has none).
func mcpRegistryRequiredEnvNames(result *MCPRegistryInstallResult) string {
	if result == nil {
		return ""
	}
	var names []string
	for _, ev := range result.RequiredEnvVars {
		if name := strings.TrimSpace(ev.Name); name != "" {
			names = append(names, name)
		}
	}
	return strings.Join(names, ", ")
}

// mcpRegistryDisplayTitle returns the human-readable label for a registry
// entry, preferring the title over the raw registry name.
func mcpRegistryDisplayTitle(e MCPRegistryServer) string {
	if e.Title != "" {
		return e.Title
	}
	return e.Name
}

// mcpRegistryNextURL builds the "next page" link that preserves the query and
// advances the cursor.
func mcpRegistryNextURL(query, cursor string) string {
	q := url.Values{}
	if query != "" {
		q.Set("q", query)
	}
	q.Set("cursor", cursor)
	return mcpRegistrySettingsBase + "?" + q.Encode()
}

func errRegistryNameRequired() error { return fmt.Errorf("registry name is required") }
