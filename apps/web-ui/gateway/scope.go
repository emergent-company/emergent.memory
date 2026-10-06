package main

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/a-h/templ"
	"github.com/labstack/echo/v4"
)

// --- scoped URL paths: /o/{orgRef} and /o/{orgRef}/p/{projectRef} ---
//
// Org and project context is carried in the URL so each browser tab is
// independently scoped and every screen is linkable (see the scoped-web-urls
// OpenSpec change). The scope prefix is stripped before routing, so the
// existing flat route table and handlers are reused unchanged; the resolver
// middleware re-establishes the scope from the refs and validates it against
// the caller's org→project access tree. The session cookie is no longer
// authoritative for a scoped request.
//
// This is deliberately additive: while the URL migration is in flight, an
// unscoped app path still resolves through the legacy session cookie. The
// hard-cut (404 on unscoped app paths) lands once every internal link is
// scoped — see unscopedAppPathAllowed.

// scopePathRe matches a scoped request path and captures the org ref, the
// optional project ref, and the remainder.
var scopePathRe = regexp.MustCompile(`^/o/([^/]+)(?:/p/([^/]+))?(/.*)?$`)

// requestScope is the per-request scoped-path context, stashed before routing
// by scopeRewrite and consumed by applyScope and link generation.
type requestScope struct {
	OrgRef       string // {orgRef} segment as written in the URL
	ProjectRef   string // {projectRef} segment; empty for an org-only scope
	OriginalPath string // the pre-rewrite path (for sidebar active-state)

	// ResolvedOrgID/ResolvedProjectID are the concrete ids after access-tree
	// validation. Empty until applyScope runs.
	ResolvedOrgID     string
	ResolvedProjectID string
}

// scopeCtxKey is the request-context key carrying the requestScope.
type scopeCtxKey struct{}

// scopeFromContext returns the requestScope stashed for this request.
func scopeFromContext(ctx context.Context) (*requestScope, bool) {
	sc, ok := ctx.Value(scopeCtxKey{}).(*requestScope)
	return sc, ok
}

// globalScopePath reports paths that are never org/project-scoped and are
// therefore exempt from the hard cut: the landing, API/auth/share/asset
// surfaces, dev tooling, and the account/org-list pages. Legacy /orgs/{id}
// pages are NOT exempt — they are cut in favour of /o/{id}.
func globalScopePath(p string) bool {
	if p == "" || p == "/" || p == "/orgs" || p == "/orgs/new" {
		return true
	}
	for _, pre := range []string{
		"/o/", "/api/", "/auth/", "/share/", "/static/", "/assets/",
		"/webhooks/", "/internal/", "/dev/", "/members", "/invites",
		"/profile", "/partial/invite-user-search",
	} {
		if strings.HasPrefix(p, pre) {
			return true
		}
	}
	return false
}

// strictScopedPath reports whether an unscoped read must be rejected under the
// hard-cut rule: app page navigations require a scope prefix, so no legacy
// cookie-scoped URL survives. Mutations keep the cookie fallback because every
// form/htmx call is emitted scoped (D5) and some global endpoints share paths.
// The rule is enforced only in session mode (see scopeRewrite): dev mode has no
// session/URL authority, and its test harness navigates unscoped.
func strictScopedPath(method, path string) bool {
	if method != http.MethodGet && method != http.MethodHead {
		return false
	}
	return !globalScopePath(path)
}

// scopeRewrite is a Pre middleware (runs before routing): it recognises a
// scoped path, stashes the refs on the request, and rewrites the path the
// router sees to the legacy flat path. Org-only scopes map onto the existing
// /orgs/:id route tree; project scopes strip the prefix entirely.
func (s *Server) scopeRewrite(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		m := scopePathRe.FindStringSubmatch(c.Request().URL.Path)
		if m == nil {
			// Hard cut (session mode only): an unscoped app page is not valid.
			if s.cfg.AuthMode == "session" && strictScopedPath(c.Request().Method, c.Request().URL.Path) {
				return echo.NewHTTPError(http.StatusNotFound, "not found")
			}
			return next(c)
		}
		orgRef, projectRef, rest := m[1], m[2], m[3]
		if rest == "" {
			rest = "/"
		}
		reqScope := &requestScope{OrgRef: orgRef, ProjectRef: projectRef, OriginalPath: c.Request().URL.Path}

		var rewritten string
		switch {
		case projectRef == "":
			// Org context reuses the existing /orgs/:id routes.
			rewritten = "/orgs/" + orgRef + strings.TrimSuffix(rest, "/")
		case rest == "/":
			// Bare project scope (/o/{org}/p/{project}): land on the project's
			// default surface. Routing it to "/" would hand control to uiRoot,
			// which follows the recent/cookie project and can leave the stated
			// project — breaking "the path is authoritative".
			rewritten = "/agents"
		default:
			rewritten = rest
		}
		c.Request().URL.Path = rewritten
		ctx := context.WithValue(c.Request().Context(), scopeCtxKey{}, reqScope)
		c.SetRequest(c.Request().WithContext(ctx))
		return next(c)
	}
}

// applyScope runs after authDispatch: it resolves the stashed refs against the
// caller's access tree and makes them the request's Memory scope. It fails
// closed (404) when the refs do not resolve within the caller's access. With
// no session (dev/API-key mode) it leaves the resolved ids empty and lets the
// legacy fallback stand.
func (s *Server) applyScope(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		reqScope, ok := scopeFromContext(c.Request().Context())
		if !ok {
			return next(c)
		}
		sess, hasSession := sessionContextFrom(c.Request().Context())
		if !hasSession || sess.Device {
			// A device credential binds org/project from token introspection,
			// never from the URL path (see attachDeviceContext in auth.go).
			return next(c)
		}
		tree, err := s.orgAccessTree(c)
		if err != nil {
			captureError(err)
			return s.scopeNotFound(c)
		}
		orgID, projectID, ok := resolveScopeRefs(tree, reqScope.OrgRef, reqScope.ProjectRef)
		if !ok {
			return s.scopeNotFound(c)
		}
		reqScope.ResolvedOrgID = orgID
		reqScope.ResolvedProjectID = projectID
		sess.OrgID = orgID
		sess.ProjectID = projectID
		return next(c)
	}
}

// resolveScopeRefs maps the URL refs onto concrete ids within the caller's
// org→project access tree. A ref matches an id (today) or a slug (once the
// backend exposes one). It returns ok=false when the org is unknown, the
// project is absent, or the project does not belong to the stated org.
func resolveScopeRefs(tree []OrgWithProjectsDto, orgRef, projectRef string) (orgID, projectID string, ok bool) {
	for _, o := range tree {
		if o.ID != orgRef {
			continue
		}
		if projectRef == "" {
			return o.ID, "", true
		}
		for _, p := range o.Projects {
			if p.ID == projectRef {
				return o.ID, p.ID, true
			}
		}
		return "", "", false // org known, project not in it
	}
	return "", "", false
}

// scopeBase returns the link base for a scoped request, or "" for an unscoped
// one. It is built from the refs as written in the URL so it round-trips.
func (reqScope *requestScope) base() string {
	if reqScope == nil {
		return ""
	}
	if reqScope.ProjectRef == "" {
		return "/o/" + reqScope.OrgRef
	}
	return "/o/" + reqScope.OrgRef + "/p/" + reqScope.ProjectRef
}

// scopeNotFound renders a 404 for a scoped request whose refs do not resolve.
// It is a plain error (not the app shell) so it also works for partial/htmx
// requests before the shell data is loaded.
func (s *Server) scopeNotFound(c echo.Context) error {
	return echo.NewHTTPError(http.StatusNotFound, "organization or project not found")
}

// scopeLocation rewrites redirect targets (Location and htmx's HX-Redirect) on
// a scoped request so handler-level PRG redirects stay in the active context.
// Handlers keep emitting context-free paths; only this layer adds the scope.
func (s *Server) scopeLocation(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if err := next(c); err != nil {
			return err
		}
		reqScope, ok := scopeFromContext(c.Request().Context())
		if !ok {
			return nil
		}
		base := reqScope.base()
		if base == "" {
			return nil
		}
		h := c.Response().Header()
		for _, key := range []string{echo.HeaderLocation, "HX-Redirect"} {
			if loc := h.Get(key); loc != "" {
				h.Set(key, scopedPathFor(base, loc))
			}
		}
		return nil
	}
}

// --- scoped link generation ---

// linkBaseCtxKey is the request-context key carrying the link base injected by
// page() for the render.
type linkBaseCtxKey struct{}

// withLinkBase returns ctx carrying the scoped link base.
func withLinkBase(ctx context.Context, base string) context.Context {
	return context.WithValue(ctx, linkBaseCtxKey{}, base)
}

// linkBaseFromContext returns the scoped link base ("", or "/o/{org}" /
// "/o/{org}/p/{project}") for the current render.
func linkBaseFromContext(ctx context.Context) string {
	base, _ := ctx.Value(linkBaseCtxKey{}).(string)
	return base
}

// globalLinkPrefixes are paths that never take a scope prefix: API/auth/asset
// surfaces plus the account and org-list pages that stay top-level. linkURL
// passes them through unchanged so a global link can be routed through the
// same helper without double-prefixing.
var globalLinkPrefixes = []string{
	"/api", "/auth/", "/share/", "/static/", "/assets/",
	"/webhooks/", "/internal/", "/o/", "/profile", "/invites", "/projects",
}

// resolveProjectRefAnyOrg finds a project by ref across every org in the
// access tree, returning its owning org. Used when a client supplies only a
// project ref (no org).
func resolveProjectRefAnyOrg(tree []OrgWithProjectsDto, projectRef string) (orgID, projectID string, ok bool) {
	if projectRef == "" {
		return "", "", false
	}
	for _, o := range tree {
		for _, p := range o.Projects {
			if p.ID == projectRef {
				return o.ID, p.ID, true
			}
		}
	}
	return "", "", false
}

// resolveClientScope applies a client-supplied scope (X-Org-ID/X-Project-ID
// headers, or org_id/project_id query params, which the shell's fetch/SSE shim
// attaches) to an unscoped request. The values are validated against the
// caller's access tree; a value outside it fails closed with 403. A request
// that is already path-scoped, has no session, or supplies no scope is left
// untouched.
func (s *Server) resolveClientScope(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, ok := scopeFromContext(c.Request().Context()); ok {
			return next(c)
		}
		sess, ok := sessionContextFrom(c.Request().Context())
		if !ok || sess.Device {
			// Device credentials bind scope from token introspection; raw
			// client headers must never re-scope them (attachDeviceContext).
			return next(c)
		}
		// A client-supplied scope always overrides the cookie's project: the
		// scoped page's base is authoritative per request (a stale cookie
		// project must not win over the tab the user is actually looking at).
		// It is validated against the access tree below, so an out-of-tree
		// value still fails closed. A request with no scope hint is untouched.
		projectID := strings.TrimSpace(c.Request().Header.Get("X-Project-ID"))
		if projectID == "" {
			projectID = strings.TrimSpace(c.QueryParam("project_id"))
		}
		orgID := strings.TrimSpace(c.Request().Header.Get("X-Org-ID"))
		if orgID == "" {
			orgID = strings.TrimSpace(c.QueryParam("org_id"))
		}
		if projectID == "" && orgID == "" {
			return next(c)
		}
		tree, err := s.orgAccessTree(c)
		if err != nil {
			captureError(err)
			return echo.NewHTTPError(http.StatusForbidden, "scope unavailable")
		}
		resolvedOrg, resolvedProject, ok := resolveScopeRefs(tree, orgID, projectID)
		if !ok && orgID == "" {
			// A project-only hint (no org) still resolves within the tree.
			resolvedOrg, resolvedProject, ok = resolveProjectRefAnyOrg(tree, projectID)
		}
		if !ok {
			return echo.NewHTTPError(http.StatusForbidden, "scope not permitted")
		}
		sess.OrgID = resolvedOrg
		sess.ProjectID = resolvedProject
		return next(c)
	}
}

// projectScopeURL builds the canonical scoped URL for a project landing
// (/o/{org}/p/{project}). It is used by the picker and the post-create/switch
// redirects, which need a specific target project rather than the current
// render's base.
func projectScopeURL(orgID, projectID string) string {
	if orgID == "" || projectID == "" {
		return ""
	}
	return "/o/" + url.PathEscape(orgID) + "/p/" + url.PathEscape(projectID)
}

// linkURL builds a link for the current render: absolute app paths get the
// scoped base; legacy /orgs/{id} org links move to /o/{id}; global paths,
// fragments, and external/scheme-relative URLs are returned unchanged. It is
// the single place templates construct internal links, so no template embeds
// the scope prefix itself.
func linkURL(ctx context.Context, path string) string {
	return scopedPathFor(linkBaseFromContext(ctx), path)
}

// scopedPathFor applies the scope base to an internal path. It also maps the
// legacy /orgs/{id} org form onto the canonical /o/{id} in every context. With
// an empty base (a global page), app paths are returned unchanged, so the
// rewriter can run on every page.
func scopedPathFor(base, path string) string {
	if path == "" || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return path
	}
	// Org-context pages are canonical under /o/{orgRef}; the legacy /orgs/{id}
	// form maps over. The org list (/orgs) and create page (/orgs/new) stay
	// global.
	if strings.HasPrefix(path, "/orgs/") && path != "/orgs/new" {
		return "/o/" + strings.TrimPrefix(path, "/orgs/")
	}
	if path == "/orgs" || path == "/orgs/new" {
		return path
	}
	if base == "" {
		return path
	}
	if path == "/" {
		return base
	}
	for _, p := range globalLinkPrefixes {
		if strings.HasPrefix(path, p) {
			return path
		}
	}
	return base + path
}

// scopeAttrRe matches URL-bearing HTML attributes whose value is an absolute
// internal path. src/data-* are deliberately excluded (assets and share
// endpoints are global); the global prefixes are filtered in scopedPathFor.
var scopeAttrRe = regexp.MustCompile(`(?i)\b(href|action|hx-get|hx-post|hx-put|hx-delete|hx-patch|data-href|data-autosave-url|data-activate-url)="(/[^"]*)"`)

// rewriteScopedHTML rewrites the URL-bearing attributes of a rendered page or
// fragment so links and htmx/form actions stay in the page's context. Doing it
// once at render time keeps the ~400 internal link sites (literal hrefs,
// templ.URL(...), and concatenated expressions) scope-agnostic. It also runs
// for global pages (base == "") to map legacy /orgs/{id} links onto /o/{id}.
func rewriteScopedHTML(html, base string) string {
	return scopeAttrRe.ReplaceAllStringFunc(html, func(m string) string {
		sub := scopeAttrRe.FindStringSubmatch(m)
		return sub[1] + `="` + scopedPathFor(base, sub[2]) + `"`
	})
}

// renderPartial mirrors render.RenderPartial but rewrites the fragment's
// URL-bearing attributes to the request's scope base. Use it (instead of
// render.RenderPartial) for htmx fragments rendered directly, so links/actions
// inside swapped-in content stay in context — that coverage is what allows the
// hard cut to be enabled.
func renderPartial(w http.ResponseWriter, r *http.Request, content templ.Component) {
	var buf bytes.Buffer
	if err := content.Render(r.Context(), &buf); err != nil {
		captureError(err)
		http.Error(w, "render error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Add("Vary", "HX-Request-Type, HX-Request")
	base := ""
	if reqScope, ok := scopeFromContext(r.Context()); ok {
		base = reqScope.base()
	}
	_, _ = w.Write([]byte(rewriteScopedHTML(buf.String(), base)))
}
