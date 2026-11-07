package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// --- MCP servers web UI + JSON echo handlers ---
//
// Page surface (/settings/mcp-servers): list (GET), standalone create
// (GET/POST /new), edit (GET/POST /:id/update), delete (POST /:id/delete), and
// a server-level enabled toggle (POST /:id/toggle, JSON — the JS fetch target
// for the row's Enabled switch). Create/update validation failures re-render
// the form with inline field errors and the user's draft intact (no partial
// save); memory-side duplicate-name errors map to the name field.
//
// JSON echo surface (/api/mcp-servers/...): sync / inspect / list-tools /
// tool-toggle proxies for the page JS (D5/D6).

// --- list page ---

// uiMCPServers renders the registry list (GET /settings/mcp-servers).
// ?created=1 / ?updated=1 / ?deleted=1 surface PRG flash feedback from the
// create/update/delete flows; a failed list fetch renders the whole-page error
// state.
func (s *Server) uiMCPServers(c echo.Context) error {
	ctx := c.Request().Context()
	data := mcpServersPageData{
		FlashMsg: flashFromQuery(c, []flashParam{
			{key: "created", msg: "MCP server created."},
			{key: "updated", msg: "MCP server updated."},
			{key: "deleted", msg: "MCP server deleted."},
			{key: "disconnected", msg: "MCP server disconnected."},
			{key: "oauthClient", msg: "OAuth client configured."},
			{key: "oauthClientCleared", msg: "OAuth client cleared."},
		}),
	}
	// A registry install redirects here with ?created=1&env=<names>; surface the
	// required environment variables memory reported (see mcp_registry.go).
	if env := c.QueryParam("env"); env != "" {
		data.FlashMsg = "MCP server created. Configure required environment variables before use: " + env + "."
	}
	// A registry install of an OAuth-required server redirects here with
	// ?created=1&requiresAuth=1; point the admin at the row's Connect action.
	if c.QueryParam("requiresAuth") != "" {
		data.FlashMsg = "MCP server created. This server requires authorization — connect it below to finish setup."
	}
	data.FlashErr = flashError(c)
	// The server's OAuth callback lands here with ?mcp_oauth=<status>; fold it
	// into the flash state (one-shot: the query param only exists on the
	// landing navigation).
	mcpOAuthLanding(c, &data)
	servers, err := s.memory.ListMCPServers(ctx)
	if err != nil {
		data.LoadErr = userFacingError(err)
	} else {
		data.Servers = servers
	}
	return s.page(c, pageTitle("MCP Servers"), MCPServersPage(data))
}

// uiMCPServerToolsFragment renders one server's cached-tool rows for the
// expand-on-demand rows host on the list page
// (GET /settings/mcp-servers/:id/tools). The list page ships no rows: a fully
// populated builtin catalogue is ~140 kB of markup — half the page's weight —
// inside a collapsed row the user has not opened (#210). This is the deferred
// half, fetched once per row on its first expand.
//
// It reads the server detail (which carries the cached tools) and, for the
// builtin server, the capability taxonomy; a failed taxonomy degrades to the
// flat list exactly as the page did before.
func (s *Server) uiMCPServerToolsFragment(c echo.Context) error {
	ctx := c.Request().Context()
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "server id is required")
	}
	// htmx only swaps 2xx responses, so a failure has to come back as markup the
	// user can read rather than as a status code the page silently drops.
	srv, err := s.memory.GetMCPServer(ctx, id)
	if err != nil {
		return mcpToolsErrorNote(c, id, userFacingError(err).Error())
	}
	if srv == nil {
		return mcpToolsErrorNote(c, id, "server not found")
	}
	var groups []BuiltinToolGroup
	if mcpServerIsBuiltin(*srv) {
		if g, gerr := s.memory.ListBuiltinToolGroups(ctx); gerr == nil {
			groups = g
		}
	}
	c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	return mcpServerToolsContainer(*srv, srv.Tools, groups).Render(ctx, c.Response())
}

// mcpToolsErrorNote answers a failed tool-rows fetch with a rendered note in
// the rows container.
func mcpToolsErrorNote(c echo.Context, serverID string, msg string) error {
	c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	return mcpServerToolsError(serverID, msg).Render(c.Request().Context(), c.Response())
}

// mcpOAuthLanding folds the OAuth callback status (?mcp_oauth=<status>) into the
// page's flash state: "connected" renders a success toast, "error"/"denied"
// render an error toast. The query param is consumed on this landing render, so
// later navigations never re-show it (the existing one-shot flash convention).
func mcpOAuthLanding(c echo.Context, data *mcpServersPageData) {
	switch c.QueryParam("mcp_oauth") {
	case "connected":
		data.FlashMsg = "MCP server connected via OAuth."
	case "denied":
		data.FlashErr = fmt.Errorf("OAuth authorization was denied")
	case "error":
		data.FlashErr = fmt.Errorf("OAuth connection failed — authorization could not be completed")
	}
}

// --- create page + create flow ---

// uiMCPNewPage renders the create form (GET /settings/mcp-servers/new): name +
// transport + transport-specific connection fields, posting to POST
// /settings/mcp-servers/new. ?err= surfaces PRG feedback; validation failures
// re-render this page directly (inline errors, draft preserved).
func (s *Server) uiMCPNewPage(c echo.Context) error {
	data := mcpServerFormData{
		Type:     "stdio",
		Enabled:  true,
		FlashErr: flashError(c),
	}
	return s.page(c, pageTitle("New MCP server"), MCPServerNewPage(data))
}

// renderMCPNewPage re-renders the create page after a validation failure or a
// rejected memory write, carrying the user's draft + inline errors.
func (s *Server) renderMCPNewPage(c echo.Context, data mcpServerFormData) error {
	if data.Type == "" {
		data.Type = "stdio"
	}
	return s.page(c, pageTitle("New MCP server"), MCPServerNewPage(data))
}

// uiMCPCreate handles the create form (POST /settings/mcp-servers/new). Local
// validation errors and memory rejections (duplicate name, transport rules)
// re-render the form inline with no partial save; success PRG-redirects to the
// list with ?created=1.
func (s *Server) uiMCPCreate(c echo.Context) error {
	ctx := c.Request().Context()
	draft := mcpServerFormDataFromRequest(c)
	fe := draft.validate()
	if mcpServerFieldErrsNonEmpty(fe) {
		draft.FieldErrs = fe
		return s.renderMCPNewPage(c, draft)
	}
	if _, err := s.memory.CreateMCPServer(ctx, draft.toServer()); err != nil {
		if mcpServerIsNameConflict(err) {
			fe.Name = err.Error()
		} else {
			draft.GeneralErr = userFacingError(err)
		}
		draft.FieldErrs = fe
		return s.renderMCPNewPage(c, draft)
	}
	return c.Redirect(http.StatusSeeOther, mcpServerSettingsBase+"?created=1")
}

// --- edit page + update flow ---

// uiMCPEditPage renders the edit form (GET /settings/mcp-servers/:id/edit)
// pre-filled from the server's current configuration. The server is resolved
// through the registry list (same pattern as the token edit page); an unknown
// id renders the not-found state. Builtin servers cannot be edited from the
// UI — the page redirects back to the list with the reason.
func (s *Server) uiMCPEditPage(c echo.Context) error {
	ctx := c.Request().Context()
	id := strings.TrimSpace(c.Param("id"))
	data := mcpServerFormData{ID: id, FlashErr: flashError(c)}
	if id == "" {
		return redirectWithError(c, mcpServerSettingsBase, errServerIDRequired())
	}
	servers, err := s.memory.ListMCPServers(ctx)
	if err != nil {
		data.LoadErr = userFacingError(err)
	} else if srv := mcpFindServerByID(servers, id); srv != nil {
		if mcpServerIsBuiltin(*srv) {
			return redirectWithError(c, mcpServerSettingsBase, builtinEditErr())
		}
		data = mcpServerFormDataFromServerConfigure(*srv, c.QueryParam("configure") == "1")
		data.FlashErr = flashError(c)
	} else {
		data.NotFound = true
	}
	return s.page(c, pageTitle("Edit server"), MCPServerEditPage(data))
}

// renderMCPEditPage re-renders the edit page after a validation failure or a
// rejected memory write, carrying the user's draft + inline errors.
func (s *Server) renderMCPEditPage(c echo.Context, data mcpServerFormData) error {
	return s.page(c, pageTitle("Edit server"), MCPServerEditPage(data))
}

// uiMCPUpdate handles the edit form (POST /settings/mcp-servers/:id/update).
// Validation failures re-render the form inline (no partial save); a transport
// change is now persisted (memory switches the server's transport and clears
// the previous transport's connection fields). Success PRG-redirects to the
// list with ?updated=1.
func (s *Server) uiMCPUpdate(c echo.Context) error {
	ctx := c.Request().Context()
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return redirectWithError(c, mcpServerSettingsBase, errServerIDRequired())
	}
	current, err := s.memory.GetMCPServer(ctx, id)
	if err != nil {
		return redirectWithError(c, mcpServerSettingsBase, err)
	}
	if current == nil || mcpServerIsBuiltin(*current) {
		return redirectWithError(c, mcpServerSettingsBase, builtinEditErr())
	}

	draft := mcpServerFormDataFromRequest(c)
	draft.ID = id
	fe := draft.validate()
	if mcpServerFieldErrsNonEmpty(fe) {
		draft.FieldErrs = fe
		return s.renderMCPEditPage(c, draft)
	}
	if _, err := s.memory.UpdateMCPServer(ctx, id, draft.toServer()); err != nil {
		if mcpServerIsNameConflict(err) {
			fe.Name = err.Error()
			draft.FieldErrs = fe
		} else {
			draft.GeneralErr = userFacingError(err)
		}
		return s.renderMCPEditPage(c, draft)
	}
	return c.Redirect(http.StatusSeeOther, mcpServerSettingsBase+"?updated=1")
}

// --- delete flow ---

// uiMCPDelete handles the delete-confirm form (POST /settings/mcp-servers/
// :id/delete, PRG). Builtin servers cannot be deleted through the UI (server-
// side guard, in addition to hiding the action). Success redirects to the list
// with ?deleted=1; deleting strips the server's tools from agents that
// reference them (memory side).
func (s *Server) uiMCPDelete(c echo.Context) error {
	ctx := c.Request().Context()
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return redirectWithError(c, mcpServerSettingsBase, errServerIDRequired())
	}
	if srv, err := s.memory.GetMCPServer(ctx, id); err == nil && srv != nil && mcpServerIsBuiltin(*srv) {
		return redirectWithError(c, mcpServerSettingsBase, builtinDeleteErr())
	}
	if err := s.memory.DeleteMCPServer(ctx, id); err != nil {
		return redirectWithError(c, mcpServerSettingsBase, err)
	}
	return c.Redirect(http.StatusSeeOther, mcpServerSettingsBase+"?deleted=1")
}

// --- server-level enabled toggle (JSON; the list row's Enabled switch) ---

// uiMCPToggle handles POST /settings/mcp-servers/:id/toggle. It round-trips
// the server's full current configuration with only Enabled flipped (a bare
// {"enabled":...} PATCH would re-send empty name/type fields through the
// shared MCPServer wire struct and clobber the record). Secret keys keep their
// stored values: the placeholder fill names each secret key in the transport
// map with a blank value ("keep the stored secret"). Builtin servers are
// read-only. Responds JSON for the page's fetch; errors carry {"error":...}.
func (s *Server) uiMCPToggle(c echo.Context) error {
	ctx := c.Request().Context()
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": errServerIDRequired().Error()})
	}
	srv, err := s.memory.GetMCPServer(ctx, id)
	if err != nil {
		return mcpServerJSONErr(c, err)
	}
	if srv == nil || mcpServerIsBuiltin(*srv) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Built-in servers cannot be disabled."})
	}
	srv.Enabled = c.FormValue("enabled") == "true"
	mcpEnsureSecretKeyPlaceholders(srv)
	if _, err := s.memory.UpdateMCPServer(ctx, id, srv); err != nil {
		return mcpServerJSONErr(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"enabled": srv.Enabled})
}

// --- OAuth connect / disconnect ---

// uiMCPOAuthStart handles GET /settings/mcp-servers/:id/oauth/start. It asks
// memory to start the OAuth flow and sends the browser to the provider's
// authorization URL; any failure (unconfigured redirect origin, non-remote or
// no-auth server, 404, or metadata fetch failure) redirects back to the list
// with an error flash.
//
// The authorize URL is cross-origin, and the Connect control sits inside the
// boosted <main> (ui.templ). htmx fetches with mode "same-origin", so a
// redirect it cannot follow is dropped as "Failed to fetch" (the browser logs
// "Unsafe attempt to load URL … from frame with URL …") and the provider page
// never loads. Answer a boosted caller with HX-Redirect — a top-level
// navigation to an absolute URL — and a plain one with a 303
// (docs/ui-patterns.md §2).
func (s *Server) uiMCPOAuthStart(c echo.Context) error {
	ctx := c.Request().Context()
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return redirectWithError(c, mcpServerSettingsBase, errServerIDRequired())
	}
	res, err := s.memory.AuthorizeMCPServer(ctx, id)
	if err != nil {
		return redirectWithError(c, mcpServerSettingsBase, err)
	}
	redirectAfterMutation(c, res.AuthorizeURL)
	return nil
}

// uiMCPOAuthDisconnect handles POST /settings/mcp-servers/:id/oauth/disconnect
// (PRG). It clears the server's stored OAuth tokens and redirects back to the
// list with a success flash; a failure redirects with the mapped error.
func (s *Server) uiMCPOAuthDisconnect(c echo.Context) error {
	ctx := c.Request().Context()
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return redirectWithError(c, mcpServerSettingsBase, errServerIDRequired())
	}
	if err := s.memory.DisconnectMCPServer(ctx, id); err != nil {
		return redirectWithError(c, mcpServerSettingsBase, err)
	}
	return c.Redirect(http.StatusSeeOther, mcpServerSettingsBase+"?disconnected=1")
}

// uiMCPOAuthClient handles POST /settings/mcp-servers/:id/oauth/client (PRG).
// It stores a pre-registered OAuth client (id + optional secret) and the
// optional scope set to request for a server whose authorization provider
// rejects dynamic registration. A blank client id with a scope updates only the
// stored client's requested scope set (the remedy for a provider that offers
// dynamic registration but names no scope in its challenge). A request with
// neither a client id nor a scope, and a backend failure, both redirect back to
// the list with an error flash; success redirects with ?oauthClient=1. The
// client secret never round-trips into any page: failures PRG with a fixed
// error message that never carries it.
func (s *Server) uiMCPOAuthClient(c echo.Context) error {
	ctx := c.Request().Context()
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return redirectWithError(c, mcpServerSettingsBase, errServerIDRequired())
	}
	clientID := strings.TrimSpace(c.FormValue("client_id"))
	scope := strings.Join(strings.Fields(c.FormValue("scope")), " ")
	if clientID == "" && scope == "" {
		return redirectWithError(c, mcpServerSettingsBase, errMCPOAuthClientIDRequired())
	}
	clientSecret := c.FormValue("client_secret")
	if _, err := s.memory.SetMCPServerOAuthClient(ctx, id, clientID, clientSecret, scope); err != nil {
		return redirectWithError(c, mcpServerSettingsBase, err)
	}
	return c.Redirect(http.StatusSeeOther, mcpServerSettingsBase+"?oauthClient=1")
}

// uiMCPOAuthClientClear handles POST /settings/mcp-servers/:id/oauth/client/
// clear (PRG). It removes the server's pre-registered OAuth client and
// redirects back to the list with a success flash; a failure redirects with
// the mapped error.
func (s *Server) uiMCPOAuthClientClear(c echo.Context) error {
	ctx := c.Request().Context()
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return redirectWithError(c, mcpServerSettingsBase, errServerIDRequired())
	}
	if err := s.memory.ClearMCPServerOAuthClient(ctx, id); err != nil {
		return redirectWithError(c, mcpServerSettingsBase, err)
	}
	return c.Redirect(http.StatusSeeOther, mcpServerSettingsBase+"?oauthClientCleared=1")
}

// --- JSON echo surface (page JS; routes added next to the mcp-servers block) ---

// mcpSyncResult is the sync endpoint's payload: the refreshed cached-tool
// list, the names pruned since the previous sync, and the rendered
// data-mcp-tools HTML fragment the page swaps back into the row.
type mcpSyncResult struct {
	Tools  []MCPTool `json:"tools"`
	Pruned []string  `json:"pruned"`
	HTML   string    `json:"html"`
}

// syncMCPServer handles POST /api/mcp-servers/:id/sync (design D5). It lists
// the cached tools before and after the memory discovery call so it can report
// which tools the sync pruned, and returns the freshly rendered tool rows.
// A failed sync (unreachable server) surfaces memory's error text and leaves
// the cached tools untouched.
func (s *Server) syncMCPServer(c echo.Context) error {
	ctx := c.Request().Context()
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": errServerIDRequired().Error()})
	}
	srv, err := s.memory.GetMCPServer(ctx, id)
	if err != nil {
		return mcpServerJSONErr(c, err)
	}
	if srv == nil || mcpServerIsBuiltin(*srv) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Built-in servers have no syncable tool cache."})
	}
	before, err := s.memory.ListMCPServerTools(ctx, id)
	if err != nil {
		return mcpServerJSONErr(c, err)
	}
	if err := s.memory.SyncMCPServer(ctx, id); err != nil {
		return mcpServerJSONErr(c, err)
	}
	after, err := s.memory.ListMCPServerTools(ctx, id)
	if err != nil {
		return mcpServerJSONErr(c, err)
	}
	return c.JSON(http.StatusOK, mcpSyncResult{
		Tools:  after,
		Pruned: mcpPrunedToolNames(before, after),
		HTML:   mcpToolsFragmentOrEmpty(*srv, after),
	})
}

// inspectMCPServer handles POST /api/mcp-servers/:id/inspect. Memory always
// answers 200 — a failed connection comes back inside the result (Status
// "error", Error set), which the page renders into the row's inspect area.
func (s *Server) inspectMCPServer(c echo.Context) error {
	ctx := c.Request().Context()
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": errServerIDRequired().Error()})
	}
	res, err := s.memory.InspectMCPServer(ctx, id)
	if err != nil {
		return mcpServerJSONErr(c, err)
	}
	return c.JSON(http.StatusOK, res)
}

// testMCPServer handles POST /api/mcp-servers/test: a save-less connection
// probe of the currently-entered form configuration (including unsaved kv
// rows). The form parses through the shared mcpServerFormDataFromRequest; an
// optional serverId backfills stored secrets left blank in edit mode. Memory
// always answers 200 with the inspect envelope; a transport failure surfaces
// as 502 via mcpServerJSONErr.
func (s *Server) testMCPServer(c echo.Context) error {
	ctx := c.Request().Context()
	draft := mcpServerFormDataFromRequest(c)
	serverID := strings.TrimSpace(c.FormValue("serverId"))
	res, err := s.memory.TestMCPServer(ctx, draft.toServer(), serverID)
	if err != nil {
		return mcpServerJSONErr(c, err)
	}
	return c.JSON(http.StatusOK, res)
}

// listMCPServerTools handles GET /api/mcp-servers/:id/tools: the tools cached
// for one server (used by clients to refresh a row's tool list).
func (s *Server) listMCPServerTools(c echo.Context) error {
	ctx := c.Request().Context()
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": errServerIDRequired().Error()})
	}
	tools, err := s.memory.ListMCPServerTools(ctx, id)
	if err != nil {
		return mcpServerJSONErr(c, err)
	}
	return c.JSON(http.StatusOK, tools)
}

// setMCPServerToolEnabled handles PATCH /api/mcp-servers/:id/tools/:toolId
// with body {"enabled": bool} (design D6). Disabled tools are no longer
// offered to agents.
func (s *Server) setMCPServerToolEnabled(c echo.Context) error {
	ctx := c.Request().Context()
	id := strings.TrimSpace(c.Param("id"))
	toolID := strings.TrimSpace(c.Param("toolId"))
	if id == "" || toolID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": errServerIDRequired().Error()})
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.Bind(&body); err != nil || body.Enabled == nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body: {\"enabled\": bool} is required"})
	}
	if err := s.memory.SetMCPServerToolEnabled(ctx, id, toolID, *body.Enabled); err != nil {
		return mcpServerJSONErr(c, err)
	}
	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}

// callMCPServerTool handles POST /api/mcp-servers/:id/tools/:toolName/call:
// the page's per-tool "Run" affordance. The body is {"arguments": <JSON
// object>} (absent or empty = no arguments); it proxies to memory's
// same-shaped admin call route and relays the result. A bad body (non-object
// arguments) is rejected with 400; an upstream failure is surfaced as 502.
func (s *Server) callMCPServerTool(c echo.Context) error {
	ctx := c.Request().Context()
	id := strings.TrimSpace(c.Param("id"))
	toolName := strings.TrimSpace(c.Param("toolName"))
	if id == "" || toolName == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "server id and tool name are required"})
	}
	var body struct {
		Arguments map[string]any `json:"arguments"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body: {\"arguments\": {}} is required"})
	}
	res, err := s.memory.CallMCPServerTool(ctx, id, toolName, body.Arguments)
	if err != nil {
		return mcpToolCallJSONErr(c, err)
	}
	return c.JSON(http.StatusOK, res)
}

// --- shared helpers ---

// mcpServerFieldErrsNonEmpty reports whether any inline field error is set.
func mcpServerFieldErrsNonEmpty(fe mcpServerFieldErrs) bool {
	return fe.Name != "" || fe.Type != "" || fe.URL != "" || fe.Command != "" || fe.Headers != "" || fe.Env != ""
}

// mcpServerJSONErr writes a {"error": msg} JSON response for a relayed memory
// failure, using a 502-style upstream status (the page JS surfaces err.text).
func mcpServerJSONErr(c echo.Context, err error) error {
	if err == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "unknown error"})
	}
	return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
}

// mcpToolCallJSONErr relays a tool-invoke failure to the page, preserving the
// upstream memory status (e.g. 403 forbidden, 409 disabled) so the dialog can
// present the right state; a non-memory error or a status outside the HTTP
// error range falls back to 502.
func mcpToolCallJSONErr(c echo.Context, err error) error {
	status := memoryStatus(err)
	if status < 400 || status > 599 {
		status = http.StatusBadGateway
	}
	return c.JSON(status, map[string]string{"error": err.Error()})
}

func errServerIDRequired() error { return fmt.Errorf("server id is required") }

func errMCPOAuthClientIDRequired() error { return fmt.Errorf("OAuth client ID is required") }

func builtinEditErr() error {
	return fmt.Errorf("built-in servers cannot be edited — their configuration is managed by Memory")
}

func builtinDeleteErr() error { return fmt.Errorf("built-in servers cannot be deleted") }
