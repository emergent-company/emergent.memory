package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/labstack/echo/v4"
)

func TestResolveScopeRefs(t *testing.T) {
	tree := []OrgWithProjectsDto{
		{ID: "o1", Name: "Acme", Projects: []ProjectAccessDto{{ID: "p1", Name: "Home", OrgID: "o1"}}},
		{ID: "o2", Name: "Other", Projects: []ProjectAccessDto{{ID: "p2", Name: "Away", OrgID: "o2"}}},
	}
	tests := []struct {
		name       string
		orgRef     string
		projectRef string
		wantOrg    string
		wantProj   string
		wantOK     bool
	}{
		{"project in org", "o1", "p1", "o1", "p1", true},
		{"org only", "o2", "", "o2", "", true},
		{"unknown org", "nope", "p1", "", "", false},
		{"project not in stated org", "o2", "p1", "", "", false},
		{"org known project unknown", "o1", "nope", "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			org, proj, ok := resolveScopeRefs(tree, tc.orgRef, tc.projectRef)
			if ok != tc.wantOK || org != tc.wantOrg || proj != tc.wantProj {
				t.Fatalf("resolveScopeRefs(%q,%q) = (%q,%q,%v), want (%q,%q,%v)",
					tc.orgRef, tc.projectRef, org, proj, ok, tc.wantOrg, tc.wantProj, tc.wantOK)
			}
		})
	}
}

func TestScopeRewriteStripsPrefix(t *testing.T) {
	seen := struct{ org, project, path string }{}
	s := &Server{}
	e := echo.New()
	e.Pre(s.scopeRewrite)
	e.GET("/objects", func(c echo.Context) error {
		rs, _ := scopeFromContext(c.Request().Context())
		seen.org, seen.project, seen.path = rs.OrgRef, rs.ProjectRef, c.Request().URL.Path
		return c.NoContent(http.StatusOK)
	})
	// Org routes are rewritten onto the legacy /orgs/:id tree.
	e.GET("/orgs/:id/settings", func(c echo.Context) error {
		rs, _ := scopeFromContext(c.Request().Context())
		seen.org, seen.project, seen.path = rs.OrgRef, rs.ProjectRef, c.Request().URL.Path
		return c.NoContent(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/o/o1/p/p1/objects", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("project scope status = %d, want 200", rec.Code)
	}
	if seen.org != "o1" || seen.project != "p1" || seen.path != "/objects" {
		t.Fatalf("project scope = %+v, want org=o1 project=p1 path=/objects", seen)
	}

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/o/o1/settings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("org scope status = %d, want 200", rec.Code)
	}
	if seen.org != "o1" || seen.project != "" || seen.path != "/orgs/o1/settings" {
		t.Fatalf("org scope = %+v, want org=o1 project= path=/orgs/o1/settings", seen)
	}
}

func TestApplyScopeValidatesAgainstAccessTree(t *testing.T) {
	f := &fakeMemory{orgsAndProjects: []OrgWithProjectsDto{
		{ID: "o1", Name: "Acme", Projects: []ProjectAccessDto{{ID: "p1", Name: "Home", OrgID: "o1"}}},
	}}
	s := &Server{cfg: sessionCfg(), memory: f}

	newServer := func() *echo.Echo {
		e := echo.New()
		e.Pre(s.scopeRewrite)
		// Simulate authDispatch attaching a session context.
		e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				ctx := withSessionContext(c.Request().Context(), &sessionContext{Sub: "u1"})
				c.SetRequest(c.Request().WithContext(ctx))
				return next(c)
			}
		})
		e.Use(s.applyScope)
		e.GET("/objects", func(c echo.Context) error {
			sc, _ := sessionContextFrom(c.Request().Context())
			return c.String(http.StatusOK, sc.OrgID+"/"+sc.ProjectID)
		})
		return e
	}

	rec := httptest.NewRecorder()
	newServer().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/o/o1/p/p1/objects", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "o1/p1" {
		t.Fatalf("valid scope = %d %q, want 200 o1/p1", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	newServer().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/o/o1/p/nope/objects", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("invalid scope status = %d, want 404", rec.Code)
	}
}

func TestStrictScopedPath(t *testing.T) {
	cases := []struct {
		method, path string
		want         bool
	}{
		{http.MethodGet, "/objects", true},
		{http.MethodGet, "/agents/7", true},
		{http.MethodGet, "/settings/tokens", true},
		{http.MethodGet, "/inbox", true},
		{http.MethodGet, "/", false},
		{http.MethodGet, "/orgs", false},
		{http.MethodGet, "/orgs/new", false},
		{http.MethodGet, "/orgs/o1", true}, // legacy org page is cut in favour of /o/o1
		{http.MethodGet, "/profile", false},
		{http.MethodGet, "/api/notifications/counts", false},
		{http.MethodGet, "/auth/login", false},
		{http.MethodGet, "/share/agent", false},
		{http.MethodGet, "/static/js/app.js", false},
		{http.MethodGet, "/assets/x.css", false},
		{http.MethodGet, "/webhooks/github", false},
		{http.MethodGet, "/dev/components", false},
		{http.MethodGet, "/partial/invite-user-search", false},
		{http.MethodGet, "/data-sources", false},                // OAuth landing re-scoped by dataSourcesScopeRedirect
		{http.MethodGet, "/data-sources/oauth/callback", false}, // OAuth callback lands unscoped
		{http.MethodGet, "/data-sources/new", false},            // OAuth landing re-scoped by dataSourcesScopeRedirect
		{http.MethodPost, "/objects", false},                    // mutations keep the cookie fallback
		{http.MethodGet, "/o/o1/p/p1/objects", false},           // scoped paths are handled earlier
	}
	for _, tc := range cases {
		if got := strictScopedPath(tc.method, tc.path); got != tc.want {
			t.Errorf("strictScopedPath(%s %s) = %v, want %v", tc.method, tc.path, got, tc.want)
		}
	}
}

func TestScopeRewriteHardCut(t *testing.T) {
	newServer := func(mode string) *echo.Echo {
		s := &Server{cfg: Config{AuthMode: mode}}
		e := echo.New()
		e.Pre(s.scopeRewrite)
		e.GET("/objects", func(c echo.Context) error { return c.NoContent(http.StatusOK) })
		return e
	}

	// Session mode: an unscoped app page is cut.
	rec := httptest.NewRecorder()
	newServer("session").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("session unscoped /objects = %d, want 404", rec.Code)
	}

	// Dev mode: no session/URL authority, so unscoped navigation still works
	// (the e2e harness runs in dev mode).
	rec = httptest.NewRecorder()
	newServer("dev").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("dev unscoped /objects = %d, want 200", rec.Code)
	}

	// A scoped path is always served, regardless of mode.
	rec = httptest.NewRecorder()
	newServer("session").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/o/o1/p/p1/objects", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("session scoped /objects = %d, want 200", rec.Code)
	}
}

// TestDataSourcesScopeRedirect verifies the unscoped OAuth-landing re-scope
// middleware: an unscoped /data-sources GET with an active project redirects to
// the scoped URL (preserving the query), while one with no project keeps the
// hard cut's 404 and a scoped request passes through untouched.
func TestDataSourcesScopeRedirect(t *testing.T) {
	newServer := func(sess *sessionContext) *echo.Echo {
		s := &Server{cfg: sessionCfg()}
		e := echo.New()
		e.Pre(s.scopeRewrite)
		e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				if sess != nil {
					ctx := withSessionContext(c.Request().Context(), sess)
					c.SetRequest(c.Request().WithContext(ctx))
				}
				return next(c)
			}
		})
		e.Use(s.dataSourcesScopeRedirect)
		e.GET("/data-sources", func(c echo.Context) error { return c.NoContent(http.StatusOK) })
		return e
	}

	t.Run("unscoped with active project redirects scoped", func(t *testing.T) {
		rec := httptest.NewRecorder()
		newServer(&sessionContext{Sub: "u1", OrgID: "o1", ProjectID: "p1"}).
			ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources", nil))
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303", rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/o/o1/p/p1/data-sources" {
			t.Errorf("location = %q, want /o/o1/p/p1/data-sources", loc)
		}
	})

	t.Run("query string preserved", func(t *testing.T) {
		rec := httptest.NewRecorder()
		newServer(&sessionContext{Sub: "u1", OrgID: "o1", ProjectID: "p1"}).
			ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources?connected=s1", nil))
		if loc := rec.Header().Get("Location"); loc != "/o/o1/p/p1/data-sources?connected=s1" {
			t.Errorf("location = %q, want /o/o1/p/p1/data-sources?connected=s1", loc)
		}
	})

	t.Run("no project keeps 404", func(t *testing.T) {
		rec := httptest.NewRecorder()
		newServer(&sessionContext{Sub: "u1"}).
			ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("no session keeps 404", func(t *testing.T) {
		rec := httptest.NewRecorder()
		newServer(nil).
			ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("scoped request passes through", func(t *testing.T) {
		rec := httptest.NewRecorder()
		newServer(&sessionContext{Sub: "u1", OrgID: "o1", ProjectID: "p1"}).
			ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/o/o1/p/p1/data-sources", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})
}

// TestApplyScopeIsPerRequestPath is the multi-tab guarantee at the resolver
// level: the same session, resolving two different scoped paths, yields two
// different Memory scopes — one per request/tab.
func TestApplyScopeIsPerRequestPath(t *testing.T) {
	f := &fakeMemory{orgsAndProjects: []OrgWithProjectsDto{
		{ID: "o1", Name: "Acme", Projects: []ProjectAccessDto{{ID: "p1", Name: "Home", OrgID: "o1"}}},
		{ID: "o2", Name: "Globex", Projects: []ProjectAccessDto{{ID: "p2", Name: "Lab", OrgID: "o2"}}},
	}}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := echo.New()
	e.Pre(s.scopeRewrite)
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := withSessionContext(c.Request().Context(), &sessionContext{Sub: "u1"})
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	})
	e.Use(s.applyScope)
	e.GET("/objects", func(c echo.Context) error {
		sc, _ := sessionContextFrom(c.Request().Context())
		return c.String(http.StatusOK, sc.OrgID+"/"+sc.ProjectID)
	})

	for _, tc := range []struct{ path, want string }{
		{"/o/o1/p/p1/objects", "o1/p1"},
		{"/o/o2/p/p2/objects", "o2/p2"},
		{"/o/o1/p/p1/objects", "o1/p1"}, // back to the first tab's URL
	} {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != http.StatusOK || rec.Body.String() != tc.want {
			t.Fatalf("%s = %d %q, want 200 %q", tc.path, rec.Code, rec.Body.String(), tc.want)
		}
	}
}

func TestResolveClientScope(t *testing.T) {
	f := &fakeMemory{orgsAndProjects: []OrgWithProjectsDto{
		{ID: "o1", Name: "Acme", Projects: []ProjectAccessDto{{ID: "p1", Name: "Home", OrgID: "o1"}}},
	}}
	s := &Server{cfg: sessionCfg(), memory: f}

	newServer := func() *echo.Echo {
		e := echo.New()
		e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				ctx := withSessionContext(c.Request().Context(), &sessionContext{Sub: "u1"})
				c.SetRequest(c.Request().WithContext(ctx))
				return next(c)
			}
		})
		e.Use(s.resolveClientScope)
		e.GET("/api/conversations", func(c echo.Context) error {
			sc, _ := sessionContextFrom(c.Request().Context())
			return c.String(http.StatusOK, sc.OrgID+"/"+sc.ProjectID)
		})
		return e
	}

	// Header scope.
	req := httptest.NewRequest(http.MethodGet, "/api/conversations", nil)
	req.Header.Set("X-Project-ID", "p1")
	req.Header.Set("X-Org-ID", "o1")
	rec := httptest.NewRecorder()
	newServer().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "o1/p1" {
		t.Fatalf("header scope = %d %q, want 200 o1/p1", rec.Code, rec.Body.String())
	}

	// Query scope (EventSource path), project only.
	rec = httptest.NewRecorder()
	newServer().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/conversations?project_id=p1", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "o1/p1" {
		t.Fatalf("query scope = %d %q, want 200 o1/p1", rec.Code, rec.Body.String())
	}

	// Out-of-tree scope fails closed.
	rec = httptest.NewRecorder()
	newServer().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/conversations?project_id=nope", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("bad scope status = %d, want 403", rec.Code)
	}

	// No client scope: untouched.
	rec = httptest.NewRecorder()
	newServer().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/conversations", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "/" {
		t.Fatalf("no scope = %d %q, want 200 /", rec.Code, rec.Body.String())
	}
}

func TestResolveProjectRefAnyOrg(t *testing.T) {
	tree := []OrgWithProjectsDto{
		{ID: "o1", Projects: []ProjectAccessDto{{ID: "p1"}}},
		{ID: "o2", Projects: []ProjectAccessDto{{ID: "p2"}}},
	}
	if org, proj, ok := resolveProjectRefAnyOrg(tree, "p2"); !ok || org != "o2" || proj != "p2" {
		t.Fatalf("got (%q,%q,%v), want (o2,p2,true)", org, proj, ok)
	}
	if _, _, ok := resolveProjectRefAnyOrg(tree, "nope"); ok {
		t.Fatal("unknown project must not resolve")
	}
}

func TestLinkURLPrefixesAppPathsOnly(t *testing.T) {
	ctx := withLinkBase(context.Background(), "/o/o1/p/p1")
	cases := map[string]string{
		"/objects":              "/o/o1/p/p1/objects",
		"/agents/7":             "/o/o1/p/p1/agents/7",
		"/":                     "/o/o1/p/p1",
		"/api/chat":             "/api/chat",
		"/auth/login":           "/auth/login",
		"/profile":              "/profile",
		"/orgs":                 "/orgs",
		"/orgs/new":             "/orgs/new",
		"/orgs/o2/members":      "/o/o2/members",
		"/orgs/o2":              "/o/o2",
		"https://example.com/x": "https://example.com/x",
		"#frag":                 "#frag",
	}
	for in, want := range cases {
		if got := linkURL(ctx, in); got != want {
			t.Errorf("linkURL(%q) = %q, want %q", in, got, want)
		}
	}
	if got := linkURL(context.Background(), "/objects"); got != "/objects" {
		t.Errorf("unscoped linkURL(/objects) = %q, want /objects", got)
	}
	// The legacy org mapping applies in every context, including unscoped.
	if got := linkURL(context.Background(), "/orgs/o2/members"); got != "/o/o2/members" {
		t.Errorf("unscoped linkURL(/orgs/o2/members) = %q, want /o/o2/members", got)
	}
}

func TestRewriteScopedHTML(t *testing.T) {
	in := `<a href="/objects">o</a>` +
		`<form action="/agents/7/settings/general" hx-post="/settings/tokens/new">x</form>` +
		`<button data-reveal-url="/agents/7/share-links/l1/reveal">copy</button>` +
		`<a href="/orgs/o2/members">members</a>` +
		`<a href="/api/chat">api</a><a href="#x">f</a><a href="https://e.com">e</a>` +
		`<a href="/o/other/p/x/objects">already</a>`
	got := rewriteScopedHTML(in, "/o/o1/p/p1")
	for _, want := range []string{
		`href="/o/o1/p/p1/objects"`,
		`action="/o/o1/p/p1/agents/7/settings/general"`,
		`hx-post="/o/o1/p/p1/settings/tokens/new"`,
		`data-reveal-url="/o/o1/p/p1/agents/7/share-links/l1/reveal"`,
		`href="/o/o2/members"`,
		`href="/api/chat"`,
		`href="#x"`,
		`href="https://e.com"`,
		`href="/o/other/p/x/objects"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rewriteScopedHTML missing %q in %s", want, got)
		}
	}
	// With no scope base, app paths are untouched but org links still map.
	global := rewriteScopedHTML(`<a href="/objects">o</a><button data-reveal-url="/agents/7/share-links/l1/reveal">c</button><a href="/orgs/o2">g</a>`, "")
	if !strings.Contains(global, `href="/objects"`) {
		t.Errorf("global rewrite must not touch app paths: %s", global)
	}
	if !strings.Contains(global, `data-reveal-url="/agents/7/share-links/l1/reveal"`) {
		t.Errorf("global rewrite must not touch data-reveal-url: %s", global)
	}
	if !strings.Contains(global, `href="/o/o2"`) {
		t.Errorf("global rewrite must map /orgs/{id} to /o/{id}: %s", global)
	}
}

// TestRewriteScopedHTMLSrc pins the sub-resource URL rule: src/data-src on
// app-internal paths (the document preview file proxy) carry the scope base,
// while global prefixes (/assets), scheme-relative/external URLs, and data URIs
// pass through unchanged. Dev gallery previews stay global via /dev/.
func TestRewriteScopedHTMLSrc(t *testing.T) {
	in := `<embed data-testid="document-preview-pdf" src="/documents/d1/file">` +
		`<img src="/assets/js/app.js?v=1">` +
		`<img data-src="/documents/d2/file">` +
		`<script src="https://x/y.js"></script>` +
		`<img src="data:image/png;base64,AAAA">` +
		`<iframe src="/dev/components/preview/x?variant=a"></iframe>`
	got := rewriteScopedHTML(in, "/o/o1/p/p1")
	for _, want := range []string{
		`src="/o/o1/p/p1/documents/d1/file"`,
		`data-src="/o/o1/p/p1/documents/d2/file"`,
		`src="/assets/js/app.js?v=1"`,
		`src="https://x/y.js"`,
		`src="data:image/png;base64,AAAA"`,
		`src="/dev/components/preview/x?variant=a"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rewriteScopedHTML missing %q in %s", want, got)
		}
	}
	if got := rewriteScopedHTML(in, ""); got != in {
		t.Errorf("unscoped rewrite changed src:\n got %s\nwant %s", got, in)
	}
}

func TestScopeLocationRewritesRedirects(t *testing.T) {
	s := &Server{}
	e := echo.New()
	e.Pre(s.scopeRewrite)
	e.Use(s.scopeLocation)
	e.POST("/objects", func(c echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/objects?created=1")
	})
	e.POST("/agents", func(c echo.Context) error {
		c.Response().Header().Set("HX-Redirect", "/orgs/o2")
		return c.NoContent(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/o/o1/p/p1/objects", nil))
	if loc := rec.Header().Get("Location"); loc != "/o/o1/p/p1/objects?created=1" {
		t.Fatalf("Location = %q, want scoped", loc)
	}

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/o/o1/p/p1/agents", nil))
	if loc := rec.Header().Get("HX-Redirect"); loc != "/o/o2" {
		t.Fatalf("HX-Redirect = %q, want /o/o2", loc)
	}
}

// TestUnscopedMutationRedirectUsesSessionProject pins the #2034 rule: an
// unscoped mutation is still accepted (only reads are hard-cut), so its
// context-free PRG target must not answer 404. The session's active project
// supplies the scope — but only for targets the hard cut would reject, so global
// pages, external URLs and already-scoped targets pass through untouched.
func TestUnscopedMutationRedirectUsesSessionProject(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := withSessionContext(c.Request().Context(), &sessionContext{Sub: "u1", OrgID: "o1", ProjectID: "p1"})
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	})
	e.Use(s.scopeLocation)
	e.POST("/settings/providers/config", func(c echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/settings/providers?updated=1")
	})
	e.POST("/settings/devices/sync", func(c echo.Context) error {
		c.Response().Header().Set("HX-Redirect", "/settings/devices?mcp_installed=1")
		return c.NoContent(http.StatusOK)
	})
	e.POST("/profile/update", func(c echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/profile?updated=1")
	})
	e.POST("/orgs/new", func(c echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/orgs")
	})
	e.POST("/external", func(c echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "https://example.com/done")
	})
	e.POST("/already-scoped", func(c echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/o/o9/p/p9/objects?created=1")
	})

	post := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
		return rec
	}

	// The reported defect: a successful write answered with a 404 URL.
	rec := post("/settings/providers/config")
	if loc := rec.Header().Get("Location"); loc != "/o/o1/p/p1/settings/providers?updated=1" {
		t.Fatalf("unscoped PRG Location = %q, want /o/o1/p/p1/settings/providers?updated=1", loc)
	}
	// htmx redirects take the same treatment.
	rec = post("/settings/devices/sync")
	if loc := rec.Header().Get("HX-Redirect"); loc != "/o/o1/p/p1/settings/devices?mcp_installed=1" {
		t.Fatalf("unscoped HX-Redirect = %q, want scoped", loc)
	}
	// Global pages stay global: scoping them would invent a route.
	for path, want := range map[string]string{
		"/profile/update": "/profile?updated=1",
		"/orgs/new":       "/orgs",
		"/external":       "https://example.com/done",
		// A handler that already emits a scoped target must not be re-scoped.
		"/already-scoped": "/o/o9/p/p9/objects?created=1",
	} {
		if got := post(path).Header().Get("Location"); got != want {
			t.Errorf("%s Location = %q, want %q", path, got, want)
		}
	}
}

// TestUnscopedMutationRedirectLeavesNonSessionRequestsAlone pins the boundaries
// of the #2034 fallback: no session, a device credential, a session with no
// active project, and dev mode all keep the handler's target verbatim (dev mode
// has no session/URL authority and its harness navigates unscoped by design).
func TestUnscopedMutationRedirectLeavesNonSessionRequestsAlone(t *testing.T) {
	cases := map[string]struct {
		cfg     Config
		session *sessionContext
	}{
		"no session":        {cfg: sessionCfg()},
		"device credential": {cfg: sessionCfg(), session: &sessionContext{OrgID: "o1", ProjectID: "p1", Device: true}},
		"no active project": {cfg: sessionCfg(), session: &sessionContext{OrgID: "o1"}},
		"dev mode":          {cfg: Config{AuthMode: "dev"}, session: &sessionContext{OrgID: "o1", ProjectID: "p1"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := &Server{cfg: tc.cfg, memory: &fakeMemory{}}
			e := echo.New()
			if tc.session != nil {
				e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
					return func(c echo.Context) error {
						ctx := withSessionContext(c.Request().Context(), tc.session)
						c.SetRequest(c.Request().WithContext(ctx))
						return next(c)
					}
				})
			}
			e.Use(s.scopeLocation)
			e.POST("/settings/providers/config", func(c echo.Context) error {
				return c.Redirect(http.StatusSeeOther, "/settings/providers?updated=1")
			})
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/settings/providers/config", nil))
			if loc := rec.Header().Get("Location"); loc != "/settings/providers?updated=1" {
				t.Fatalf("Location = %q, want the handler's target unchanged", loc)
			}
		})
	}
}

// TestScopeRewriteBareProjectRootLanding pins that /o/{org}/p/{project} (no
// trailing page) lands on the stated project's default page, not on uiRoot —
// the global "/" resolver follows the recent/cookie project and could silently
// switch the tab to a different project.
func TestScopeRewriteBareProjectRootLanding(t *testing.T) {
	s := &Server{}
	e := echo.New()
	e.Pre(s.scopeRewrite)
	seenPath := ""
	e.GET("/agents", func(c echo.Context) error {
		seenPath = c.Request().URL.Path
		return c.NoContent(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/o/o1/p/p1", nil))
	if rec.Code != http.StatusOK || seenPath != "/agents" {
		t.Fatalf("bare project root = %d path=%q, want 200 /agents", rec.Code, seenPath)
	}
}

// TestResolveClientScopeOverridesCookie pins that a scoped page's client scope
// is authoritative over a stale cookie project: the same browser may have
// activated another project, but the tab's fetch/SSE calls must follow the URL.
func TestResolveClientScopeOverridesCookie(t *testing.T) {
	f := &fakeMemory{orgsAndProjects: []OrgWithProjectsDto{
		{ID: "o1", Name: "Acme", Projects: []ProjectAccessDto{{ID: "p1", Name: "Home", OrgID: "o1"}}},
		{ID: "o2", Name: "Globex", Projects: []ProjectAccessDto{{ID: "p2", Name: "Lab", OrgID: "o2"}}},
	}}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := withSessionContext(c.Request().Context(), &sessionContext{Sub: "u1", OrgID: "o1", ProjectID: "p1"})
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	})
	e.Use(s.resolveClientScope)
	e.GET("/api/conversations", func(c echo.Context) error {
		sc, _ := sessionContextFrom(c.Request().Context())
		return c.String(http.StatusOK, sc.OrgID+"/"+sc.ProjectID)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/conversations", nil)
	req.Header.Set("X-Project-ID", "p2")
	req.Header.Set("X-Org-ID", "o2")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "o2/p2" {
		t.Fatalf("client scope over stale cookie = %d %q, want 200 o2/p2", rec.Code, rec.Body.String())
	}
}

// TestPageFullRenderScopesInternalLinks pins that a full (non-partial) scoped
// page load rewrites internal href/action attributes to the scope base — not
// just htmx partials. Without this, the first navigation after a full load hits
// an unscoped app path and 404s under the session-mode hard cut.
func TestPageFullRenderScopesInternalLinks(t *testing.T) {
	f := &fakeMemory{
		orgs: []Org{{ID: "o1", Name: "Acme"}},
		projects: []ProjectRef{
			{ID: "p1", Name: "Home", OrgID: "o1"},
		},
		orgsAndProjects: []OrgWithProjectsDto{
			{ID: "o1", Name: "Acme", Role: "org_admin", Projects: []ProjectAccessDto{{ID: "p1", Name: "Home", OrgID: "o1"}}},
		},
	}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := echo.New()
	e.Pre(s.scopeRewrite)
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := withSessionContext(c.Request().Context(), &sessionContext{Sub: "u1"})
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	})
	e.Use(s.applyScope)
	e.GET("/objects", func(c echo.Context) error {
		return s.page(c, "Objects", templ.Raw(`<a href="/objects/1">one</a><form action="/schema/add"></form>`))
	})

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/o/o1/p/p1/objects", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `href="/o/o1/p/p1/objects/1"`) {
		t.Errorf("full page: object link not scoped")
	}
	if !strings.Contains(body, `action="/o/o1/p/p1/schema/add"`) {
		t.Errorf("full page: form action not scoped")
	}
}

// TestScopeMiddlewaresSkipDeviceContexts pins that a device credential's
// introspection-bound scope is never overridden by the URL path or by raw
// X-Project-ID / X-Org-ID headers (the invariant documented on
// attachDeviceContext in auth.go).
func TestScopeMiddlewaresSkipDeviceContexts(t *testing.T) {
	f := &fakeMemory{orgsAndProjects: []OrgWithProjectsDto{
		{ID: "o1", Name: "Acme", Projects: []ProjectAccessDto{{ID: "p1", Name: "Home", OrgID: "o1"}}},
		{ID: "o2", Name: "Globex", Projects: []ProjectAccessDto{{ID: "p2", Name: "Lab", OrgID: "o2"}}},
	}}
	s := &Server{cfg: sessionCfg(), memory: f}
	newServer := func() *echo.Echo {
		e := echo.New()
		e.Pre(s.scopeRewrite)
		e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				ctx := withSessionContext(c.Request().Context(), &sessionContext{OrgID: "o1", ProjectID: "p1", Device: true})
				c.SetRequest(c.Request().WithContext(ctx))
				return next(c)
			}
		})
		e.Use(s.applyScope)
		e.Use(s.resolveClientScope)
		e.GET("/objects", func(c echo.Context) error {
			sc, _ := sessionContextFrom(c.Request().Context())
			return c.String(http.StatusOK, sc.OrgID+"/"+sc.ProjectID)
		})
		e.GET("/api/conversations", func(c echo.Context) error {
			sc, _ := sessionContextFrom(c.Request().Context())
			return c.String(http.StatusOK, sc.OrgID+"/"+sc.ProjectID)
		})
		return e
	}

	// Scoped path must not re-scope a device context.
	rec := httptest.NewRecorder()
	newServer().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/o/o2/p/p2/objects", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "o1/p1" {
		t.Fatalf("device scoped path = %d %q, want 200 o1/p1 (path must not override device scope)", rec.Code, rec.Body.String())
	}

	// Client headers must not re-scope a device context.
	req := httptest.NewRequest(http.MethodGet, "/api/conversations", nil)
	req.Header.Set("X-Project-ID", "p2")
	req.Header.Set("X-Org-ID", "o2")
	rec = httptest.NewRecorder()
	newServer().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "o1/p1" {
		t.Fatalf("device headers = %d %q, want 200 o1/p1 (headers must not override device scope)", rec.Code, rec.Body.String())
	}
}

func TestOrgScopeURL(t *testing.T) {
	if got := orgScopeURL("o1"); got != "/o/o1" {
		t.Errorf("orgScopeURL(o1) = %q, want /o/o1", got)
	}
	if got := orgScopeURL(""); got != "/orgs" {
		t.Errorf("orgScopeURL() = %q, want /orgs", got)
	}
	// Org ids are path-escaped so an id with reserved characters stays a single
	// URL segment.
	if got := orgScopeURL("a/b"); got != "/o/a%2Fb" {
		t.Errorf("orgScopeURL(a/b) = %q, want /o/a%%2Fb", got)
	}
}

// TestInvitesActionScopesOnSettingsSurface pins the /invites link rule: the
// accept/decline forms live on the global /profile/invitations page (base ""),
// while the revoke/resend forms also render on the scoped Project Settings
// members surface (/settings/members). /invites must therefore take a scope
// prefix on a scoped render (so scopeLocation can rewrite the PRG redirect back
// onto the scoped members page) while staying global on global renders.
func TestInvitesActionScopesOnSettingsSurface(t *testing.T) {
	scoped := withLinkBase(context.Background(), "/o/o1/p/p1")
	if got := linkURL(scoped, "/invites/inv-1/revoke"); got != "/o/o1/p/p1/invites/inv-1/revoke" {
		t.Errorf("scoped linkURL(/invites/.../revoke) = %q, want /o/o1/p/p1/invites/inv-1/revoke", got)
	}
	if got := linkURL(scoped, "/profile/invitations"); got != "/profile/invitations" {
		t.Errorf("scoped linkURL(/profile/invitations) = %q, want /profile/invitations (global)", got)
	}
	if got := linkURL(context.Background(), "/invites/inv-1/revoke"); got != "/invites/inv-1/revoke" {
		t.Errorf("global linkURL(/invites/.../revoke) = %q, want unchanged", got)
	}
}

// TestMembersSettingsRedirectStaysScoped pins the settings-surface revoke PRG:
// the form POSTs to the scoped /o/{org}/p/{proj}/invites/:id/revoke route, so
// scopeLocation rewrites the /settings/members target back onto the scope
// instead of leaving the now-cut unscoped app path.
func TestMembersSettingsRedirectStaysScoped(t *testing.T) {
	f := &fakeMemory{}
	s := &Server{cfg: sessionCfg(), memory: f}
	e := echo.New()
	e.Pre(s.scopeRewrite)
	e.Use(s.scopeLocation)
	e.POST("/invites/:id/revoke", s.uiRevokeInvite)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/o/o1/p/p1/invites/inv-1/revoke",
		strings.NewReader(url.Values{"surface": {"/settings/members"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/o/o1/p/p1/settings/members?revoked=1" {
		t.Fatalf("settings revoke = %d %q, want 303 /o/o1/p/p1/settings/members?revoked=1", rec.Code, rec.Header().Get("Location"))
	}
	if f.canceledInvite != "inv-1" {
		t.Errorf("CancelInvite not forwarded: %q", f.canceledInvite)
	}
}
