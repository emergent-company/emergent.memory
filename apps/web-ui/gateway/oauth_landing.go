package main

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
)

// --- OAuth callback landing ---
//
// The API server issues the post-callback browser redirect, but it has no
// notion of the gateway's /o/{org}/p/{project} scope (see scoped-web-urls). A
// deployment-wide landing URL therefore cannot name a project: hardcoding one
// pinned every connect to a single project and 404'd every caller outside it
// (applyScope fails closed). These global routes are the single place that maps
// a callback back into the caller's scope:
//
//   - the API forwards the flow's project as an untrusted scope_project hint;
//   - the gateway validates the hint against the caller's access tree;
//   - an unusable hint falls back to the caller's active project, then recent
//     projects, then the configured default project when there is one — a
//     landing never 404s.
//
// The scoped page it lands on owns the outcome: uiDataSources delegates to
// uiDataSourceOAuthCallback (?connected/?oauth_error), and the MCP settings page
// folds ?mcp_oauth into its flash (see mcpOAuthLanding).
const (
	oauthLandingDataSourcesPath = "/oauth/landing/data-sources"
	oauthLandingMCPServersPath  = "/oauth/landing/mcp-servers"

	// oauthScopeProjectParam duplicates the API server's hint parameter name
	// (apps/server is a separate Go module) — keep them in sync.
	oauthScopeProjectParam = "scope_project"
)

// uiOAuthLandingDataSources maps a data-source connect callback into the
// caller's project scope, preserving the one-shot outcome parameters that
// /data-sources already knows how to consume. `reconnected` is the repair of an
// existing source's grant (same landing, different message).
func (s *Server) uiOAuthLandingDataSources(c echo.Context) error {
	return s.oauthLanding(c, "/data-sources", "connected", "reconnected", "oauth_error")
}

// uiOAuthLandingMCPServers maps an MCP registry connect callback into the
// caller's project scope, preserving ?mcp_oauth for the settings page.
func (s *Server) uiOAuthLandingMCPServers(c echo.Context) error {
	return s.oauthLanding(c, mcpServerSettingsBase, "mcp_oauth")
}

// oauthLanding redirects to path inside the resolved scope, carrying only the
// named outcome params. Anything else on the query string is dropped: the
// landing is free to accept arbitrary input (it is the cross-domain hop), and
// the target is always built from this server's own scope base, so nothing
// here can be turned into an open redirect.
func (s *Server) oauthLanding(c echo.Context, path string, outcomeParams ...string) error {
	orgID, projectID, ok := s.oauthLandingScope(c)
	if !ok {
		// No project resolved (e.g. the state expired and the session carries no
		// project): the global root picks a landing from the live account and
		// can never 404. The outcome is dropped rather than shown on a page the
		// user did not come from.
		return c.Redirect(http.StatusSeeOther, "/")
	}
	// A landing places the user in project X: persist X as the session's active
	// project so the follow-up GET/POST from the scoped page resolves to X, not
	// the previously active project. Without this, an unscoped form action on
	// the landed page (e.g. the resource-step save) is silently retargeted to
	// the cookie's still-active project and the just-connected source 404s.
	// activateProjectInSession re-issues the cookie preserving tokens/expiry,
	// records the project in recent-projects, and no-ops when there is no
	// session (dev/API-key) or it has expired.
	s.activateProjectInSession(c, projectID, orgID)
	base := projectScopeURL(orgID, projectID)
	q := url.Values{}
	for _, p := range outcomeParams {
		if v := c.QueryParam(p); v != "" {
			q.Set(p, v)
		}
	}
	target := base + path
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	return c.Redirect(http.StatusSeeOther, target)
}

// oauthLandingScope resolves the scope a callback should land in, returning the
// concrete org/project ids. Preference order:
//
//  1. the scope_project hint, when it resolves within the caller's tree;
//  2. the session's active project;
//  3. the caller's recent-project list (most recent first);
//  4. the configured static project (dev / standalone wiring — a deployment
//     default, tried whether or not the caller has a session).
//
// Every candidate is validated against the access tree; an inaccessible or
// stale value is skipped, never surfaced as an error.
func (s *Server) oauthLandingScope(c echo.Context) (orgID, projectID string, ok bool) {
	tree, err := s.orgAccessTree(c)
	if err != nil {
		captureError(err)
		return "", "", false
	}
	if ref := strings.TrimSpace(c.QueryParam(oauthScopeProjectParam)); ref != "" {
		if orgID, projectID, ok := resolveProjectRefAnyOrg(tree, ref); ok {
			return orgID, projectID, true
		}
	}
	for _, id := range s.oauthLandingFallbackProjects(c) {
		if id == "" {
			continue
		}
		if orgID, projectID, ok := resolveProjectRefAnyOrg(tree, id); ok {
			return orgID, projectID, true
		}
	}
	return "", "", false
}

// oauthLandingFallbackProjects lists the project ids to try when the callback
// carries no usable hint, in preference order. The static MemoryProjectID is
// last, and it is used whenever it is configured — not only in API-key mode:
// dev/standalone wiring sets MEMORY_PROJECT_ID alongside AUTH_MODE=session, so
// a session caller with no active or recent project still lands in the
// deployment's static project. That is a deployment default, not a per-caller
// preference; dropping the value would also change other default-project paths.
func (s *Server) oauthLandingFallbackProjects(c echo.Context) []string {
	ids := make([]string, 0, 4)
	sc, hasSession := sessionContextFrom(c.Request().Context())
	if hasSession {
		if sc.ProjectID != "" {
			ids = append(ids, sc.ProjectID)
		}
		if recent, ok := s.readRecentProjects(c); ok {
			ids = append(ids, recent.Recent[sc.Sub]...)
		}
	}
	if s.cfg.MemoryProjectID != "" {
		ids = append(ids, s.cfg.MemoryProjectID)
	}
	return ids
}
