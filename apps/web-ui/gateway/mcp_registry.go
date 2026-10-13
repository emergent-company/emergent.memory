package main

import (
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/emergent-company/go-daisy/render"
	"github.com/labstack/echo/v4"
)

// --- MCP registry browse/install page (/settings/mcp-registry) ---
//
// Lists Memory's curated set of recommended MCP servers and installs them into
// the project. An entry with a remote transport (http/sse) installs an outbound
// server on the shared host via POST /settings/mcp-registry/install. A
// package-only entry (no remote) instead offers a device install: the entry's
// local npm/pypi package runs on a connected connector, chosen in a dialog and
// pushed over the relay via POST /settings/mcp-registry/install-to-device. When
// no device is connected the dialog explains how to connect one. Both flows are
// PRG: POST → 303 → the configure form (when credentials are still required) or
// the success target. No credential preview is shown on the listing — credential
// guidance lives on the configure form after install.

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
	// Devices are the currently connected connector sessions a package-only
	// entry can be installed to, most recently connected first. DevicesErr
	// surfaces a failed session-list fetch so the dialog can say so instead of
	// claiming there are no devices.
	Devices    []RelaySession
	DevicesErr error
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
		data.LoadErr = userFacingError(err)
	} else if res != nil {
		data.Servers = res.Servers
		data.NextCursor = res.NextCursor
		data.Count = res.Count
	}
	// Connected devices feed the package-only entries' install dialog. A failed
	// fetch degrades that dialog only — the listing still renders.
	devices, derr := s.memory.ListRelaySessions(ctx)
	data.DevicesErr = derr
	if derr == nil {
		sortRelaySessions(devices)
		data.Devices = devices
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
	// A server installed with required configuration is created disabled and
	// must be configured before use: send the user straight to its edit form
	// (configure mode marks the required rows and names the missing keys).
	if len(result.RequiredConfig) > 0 && result.Server != nil && result.Server.ID != "" {
		render.RedirectAfterMutation(c.Response().Writer, c.Request(),
			mcpServerSettingsBase+"/"+url.PathEscape(result.Server.ID)+"/edit?configure=1")
		return nil
	}
	target := mcpServerSettingsBase + "?created=1"
	if names := mcpRegistryRequiredEnvNames(result); names != "" {
		target += "&env=" + url.QueryEscape(names)
	}
	render.RedirectAfterMutation(c.Response().Writer, c.Request(), target)
	return nil
}

// uiMCPRegistryInstallToDevice handles the device-install form (POST
// /settings/mcp-registry/install-to-device, PRG). The form names the registry
// entry and the connector instance to install to; memory translates the entry's
// local package into a stdio command and pushes it over the relay. A failure
// (an entry with no locally-runnable package, or a device the caller may not
// target) redirects back to the registry page — memory rejects before any
// write, so no partially-created server survives. A server created with
// required configuration is disabled and the user is sent to its configure
// form; otherwise the user lands on the devices page, where the pushed server's
// node is visible.
func (s *Server) uiMCPRegistryInstallToDevice(c echo.Context) error {
	ctx := c.Request().Context()
	registryName := strings.TrimSpace(c.FormValue("registryName"))
	version := strings.TrimSpace(c.FormValue("version"))
	name := strings.TrimSpace(c.FormValue("name"))
	instanceID := strings.TrimSpace(c.FormValue("targetInstanceId"))
	if registryName == "" {
		return redirectWithError(c, mcpRegistrySettingsBase, errRegistryNameRequired())
	}
	if instanceID == "" {
		return redirectWithError(c, mcpRegistrySettingsBase, errRegistryDeviceRequired())
	}
	result, err := s.memory.InstallRegistryServerToDevice(ctx, registryName, version, name, instanceID)
	if err != nil {
		return redirectWithError(c, mcpRegistrySettingsBase, err)
	}
	if len(result.RequiredConfig) > 0 && result.Server != nil && result.Server.ID != "" {
		render.RedirectAfterMutation(c.Response().Writer, c.Request(),
			mcpServerSettingsBase+"/"+url.PathEscape(result.Server.ID)+"/edit?configure=1")
		return nil
	}
	render.RedirectAfterMutation(c.Response().Writer, c.Request(), "/settings/devices?mcp_installed=1")
	return nil
}

// mcpRegistryRequiredEnvNames joins the required environment variable names
// (plus header names from RequiredConfig) from an install result into a single
// ", "-separated string for the success flash (empty when the result has none).
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
	// RequiredConfig carries the selected transport's header names too (the
	// legacy RequiredEnvVars covers env vars only); include them so the flash
	// names every key the admin still needs to set.
	for _, k := range result.RequiredConfig {
		if k.Kind != "header" {
			continue
		}
		if name := strings.TrimSpace(k.Name); name != "" && !slices.Contains(names, name) {
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

// errRegistryDeviceRequired rejects a device install without a target instance.
// The dialog always submits one; this guards a hand-crafted POST.
func errRegistryDeviceRequired() error {
	return fmt.Errorf("a device is required for a device install")
}

// mcpRegistryDeviceLabel names one connected device in the install dialog: the
// relay instance id, with the connector version when the session reports one.
func mcpRegistryDeviceLabel(sess RelaySession) string {
	label := sess.InstanceID
	if sess.Version != "" {
		label += " (v" + sess.Version + ")"
	}
	return label
}

// mcpRegistryDeviceDialogID derives the per-entry modal id from a registry name.
// Registry names contain "/" and ".", which an element id may not, so any rune
// outside [A-Za-z0-9_-] becomes "-".
func mcpRegistryDeviceDialogID(registryName string) string {
	var b strings.Builder
	b.WriteString("mcp-registry-device-")
	for _, r := range registryName {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}
