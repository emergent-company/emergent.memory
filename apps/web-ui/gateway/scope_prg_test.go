package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

// prgContext builds an echo context whose request carries ctx, so a handler's
// redirect target can be inspected without a real session middleware.
func prgContext(ctx context.Context, method, target string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(method, target, nil)
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}

func scopedCtx(org, project string) context.Context {
	return context.WithValue(context.Background(), scopeCtxKey{}, &requestScope{
		OrgRef:     org,
		ProjectRef: project,
	})
}

// A scoped request already names its project, so the PRG target must carry the
// same scope even if the session cookie points at another project.
func TestPrgScopedUsesURLScope(t *testing.T) {
	s := &Server{cfg: Config{AuthMode: "session"}}
	c, _ := prgContext(scopedCtx("org-1", "proj-1"), http.MethodPost, "/o/org-1/p/proj-1/documents")

	if got, want := s.prgScoped(c, "/documents?extractions_retried=2"), "/o/org-1/p/proj-1/documents?extractions_retried=2"; got != want {
		t.Fatalf("prgScoped = %q, want %q", got, want)
	}
}

// An org-only scope has no project, and neither adds one: the target stays on
// the org, exactly as scopeLocation's base would.
func TestPrgScopedKeepsOrgOnlyScope(t *testing.T) {
	s := &Server{cfg: Config{AuthMode: "session"}}
	ctx := withSessionContext(context.Background(), &sessionContext{OrgID: "org-1", ProjectID: "proj-1"})
	c, _ := prgContext(context.WithValue(ctx, scopeCtxKey{}, &requestScope{OrgRef: "org-1"}), http.MethodPost, "/o/org-1/documents")

	if got, want := s.prgScoped(c, "/documents"), "/o/org-1/documents"; got != want {
		t.Fatalf("prgScoped = %q, want %q", got, want)
	}
}

// An unscoped mutation still has to redirect somewhere valid: the session's
// active project supplies the base, the same way scopeLocation would have.
func TestPrgScopedFallsBackToSessionProject(t *testing.T) {
	s := &Server{cfg: Config{AuthMode: "session"}}
	ctx := withSessionContext(context.Background(), &sessionContext{OrgID: "org-1", ProjectID: "proj-1"})
	c, _ := prgContext(ctx, http.MethodPost, "/documents/retry-extraction-failures")

	if got, want := s.prgScoped(c, "/documents?extractions_retried=2"), "/o/org-1/p/proj-1/documents?extractions_retried=2"; got != want {
		t.Fatalf("prgScoped = %q, want %q", got, want)
	}
}

// A device credential has no page scope at all (its project comes from token
// introspection), so there is nothing to prefix.
func TestPrgScopedLeavesDeviceSessionAlone(t *testing.T) {
	s := &Server{cfg: Config{AuthMode: "session"}}
	ctx := withSessionContext(context.Background(), &sessionContext{OrgID: "org-1", ProjectID: "proj-1", Device: true})
	c, _ := prgContext(ctx, http.MethodPost, "/documents/retry-extraction-failures")

	if got := s.prgScoped(c, "/documents?extractions_retried=2"); got != "/documents?extractions_retried=2" {
		t.Fatalf("prgScoped = %q, want unchanged target", got)
	}
}

// The retry must land on a reachable page even when nothing rewrites the
// response. This mounts the handler on the scoped path WITHOUT scopeLocation —
// the shape production produced when a boosted POST answered with a bare
// /documents?extractions_retried=5 and the browser followed it to a 404. The
// handler now names the target in HX-Redirect itself, so htmx navigates scoped.
func TestRetryAllExtractionsRedirectIsScopedWithoutMiddleware(t *testing.T) {
	f := &fakeMemory{bulkRetried: 5}
	s, _ := newClientEcho(f, Config{AuthMode: "session"})
	e := echo.New()
	// Mirrors scopeRewrite's Pre step: stash the URL refs, then dispatch.
	e.POST("/o/:org/p/:project/documents/retry-extraction-failures", func(c echo.Context) error {
		ctx := context.WithValue(c.Request().Context(), scopeCtxKey{}, &requestScope{
			OrgRef: c.Param("org"), ProjectRef: c.Param("project"),
		})
		c.SetRequest(c.Request().WithContext(ctx))
		return s.uiRetryAllExtractions(c)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/o/org-1/p/proj-1/documents/retry-extraction-failures", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Boosted", "true")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	want := "/o/org-1/p/proj-1/documents?extractions_retried=5"
	if got := rec.Header().Get(echo.HeaderLocation); got != want {
		t.Fatalf("location = %q, want %q", got, want)
	}
	if got := rec.Header().Get("HX-Redirect"); got != want {
		t.Fatalf("HX-Redirect = %q, want %q", got, want)
	}
}

// A plain form post (no htmx) gets no HX-Redirect: the browser follows Location,
// which is already scoped.
func TestRetryAllExtractionsPlainFormRedirect(t *testing.T) {
	f := &fakeMemory{bulkRetried: 0}
	s, _ := newClientEcho(f, Config{AuthMode: "session"})
	e := echo.New()
	e.POST("/o/:org/p/:project/documents/retry-extraction-failures", func(c echo.Context) error {
		ctx := context.WithValue(c.Request().Context(), scopeCtxKey{}, &requestScope{
			OrgRef: c.Param("org"), ProjectRef: c.Param("project"),
		})
		c.SetRequest(c.Request().WithContext(ctx))
		return s.uiRetryAllExtractions(c)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/o/org-1/p/proj-1/documents/retry-extraction-failures", nil)
	e.ServeHTTP(rec, req)

	if got, want := rec.Header().Get(echo.HeaderLocation), "/o/org-1/p/proj-1/documents?extractions_none=1"; got != want {
		t.Fatalf("location = %q, want %q", got, want)
	}
	if got := rec.Header().Get("HX-Redirect"); got != "" {
		t.Fatalf("HX-Redirect = %q, want empty for a plain form post", got)
	}
}

// Error paths keep the ?err= flash and the scope.
func TestRetryExtractionErrorRedirectCarriesScope(t *testing.T) {
	f := &fakeMemory{retryJobErr: errDocumentIDRequired}
	s, _ := newClientEcho(f, Config{AuthMode: "session"})
	e := echo.New()
	e.POST("/o/:org/p/:project/documents/:id/extractions/:jobId/retry", func(c echo.Context) error {
		ctx := context.WithValue(c.Request().Context(), scopeCtxKey{}, &requestScope{
			OrgRef: c.Param("org"), ProjectRef: c.Param("project"),
		})
		c.SetRequest(c.Request().WithContext(ctx))
		return s.uiRetryExtraction(c)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/o/org-1/p/proj-1/documents/doc-1/extractions/job-1/retry", nil)
	e.ServeHTTP(rec, req)

	loc := rec.Header().Get(echo.HeaderLocation)
	if want := "/o/org-1/p/proj-1/documents/doc-1/extractions?"; len(loc) <= len(want) || loc[:len(want)] != want {
		t.Fatalf("location = %q, want prefix %q", loc, want)
	}
}

// The wire, not the header map, is what the browser sees. Scoping a redirect
// after the response is committed is invisible to the client: the Go server
// serialises the status line and headers at WriteHeader, so a header mutated
// afterwards is never sent. This path names its target before WriteHeader
// (redirectScoped), so its outcome never depended on that rewrite — which is
// why production still answered a scoped, boosted POST with
// 'location: /documents?extractions_retried=5' while it was broken for
// everything else. The middleware's rewrite now runs inside Response.Before, so
// a handler that emits a context-free target is scoped on the wire whenever the
// request carries a base (#2479).
//
// This mounts the real chain (scopeRewrite + scopeLocation + the handler) on a
// real server and asserts what a client receives.
func TestRetryAllExtractionsScopesTheWireLocation(t *testing.T) {
	f := &fakeMemory{bulkRetried: 5}
	s, _ := newClientEcho(f, Config{AuthMode: "session"})
	e := echo.New()
	e.Pre(s.scopeRewrite)
	e.Use(s.scopeLocation)
	e.POST("/documents/retry-extraction-failures", s.uiRetryAllExtractions)

	srv := httptest.NewServer(e)
	defer srv.Close()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/o/org-1/p/proj-1/documents/retry-extraction-failures", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Boosted", "true")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	want := "/o/org-1/p/proj-1/documents?extractions_retried=5"
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", res.StatusCode)
	}
	if got := res.Header.Get(echo.HeaderLocation); got != want {
		t.Fatalf("wire location = %q, want %q", got, want)
	}
	if got := res.Header.Get("HX-Redirect"); got != want {
		t.Fatalf("wire HX-Redirect = %q, want %q", got, want)
	}
}

// A committed redirect must already carry its scope when it reaches the wire.
// scopeLocation used to rewrite Location/HX-Redirect after next(c) returned,
// which is invisible: c.Redirect calls WriteHeader, and Go serialises the status
// line and headers at that moment, so the browser saw the handler's context-free
// target and landed on a 404 after its mutation had succeeded (#2479). The
// recorder-based tests cannot catch that class — an httptest.ResponseRecorder's
// header map stays writable after WriteHeader — so this mounts the real chain
// (scopeRewrite + scopeLocation + the handler) on a real server and asserts what
// a client receives.
func TestScopeLocationRewritesACommittedRedirectOnTheWire(t *testing.T) {
	f := &fakeMemory{}
	s, _ := newClientEcho(f, Config{AuthMode: "session"})

	e := echo.New()
	e.Pre(s.scopeRewrite)
	e.Use(s.scopeLocation)
	// A plain c.Redirect: the response is committed by the handler itself.
	e.POST("/documents/save", func(c echo.Context) error {
		return c.Redirect(http.StatusSeeOther, "/documents?saved=1")
	})
	// The htmx shape: no Location at all, the target rides in HX-Redirect and
	// the response commits with a 204.
	e.POST("/documents/boost", func(c echo.Context) error {
		c.Response().Header().Set("HX-Redirect", "/documents?saved=2")
		return c.NoContent(http.StatusNoContent)
	})

	srv := httptest.NewServer(e)
	defer srv.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	t.Run("Location on a 303", func(t *testing.T) {
		res, err := client.Post(srv.URL+"/o/org-1/p/proj-1/documents/save", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		if res.StatusCode != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303", res.StatusCode)
		}
		if got, want := res.Header.Get(echo.HeaderLocation), "/o/org-1/p/proj-1/documents?saved=1"; got != want {
			t.Fatalf("wire location = %q, want %q", got, want)
		}
	})

	t.Run("HX-Redirect on a 204", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/o/org-1/p/proj-1/documents/boost", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("HX-Request", "true")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		if res.StatusCode != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", res.StatusCode)
		}
		if got, want := res.Header.Get("HX-Redirect"), "/o/org-1/p/proj-1/documents?saved=2"; got != want {
			t.Fatalf("wire HX-Redirect = %q, want %q", got, want)
		}
	})
}

// Every mutation handler used to emit its PRG through
// render.RedirectAfterMutation(c.Response().Writer, …). That writes through the
// raw writer, so echo never runs scopeLocation's Response.Before hook: the
// commit happens before the scope rewrite can touch the header map and the
// context-free target reaches the client unchanged. redirectAfterMutation routes
// the same helper through echo's Response, which does run Before. This pins both
// wire shapes a mutation produces (boosted htmx → 200 + HX-Redirect, plain form
// → 303 + Location) with a context-free handler target, the way the providers
// settings save emits it.
func TestRedirectAfterMutationScopesOnTheWire(t *testing.T) {
	s, _ := newClientEcho(&fakeMemory{}, Config{AuthMode: "session"})

	e := echo.New()
	e.Pre(s.scopeRewrite)
	e.Use(s.scopeLocation)
	e.POST("/settings/providers/config", func(c echo.Context) error {
		redirectAfterMutation(c, "/settings/providers?updated=1")
		return nil
	})

	srv := httptest.NewServer(e)
	defer srv.Close()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	want := "/o/org-1/p/proj-1/settings/providers?updated=1"

	t.Run("boosted htmx HX-Redirect", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/o/org-1/p/proj-1/settings/providers/config", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("HX-Request", "true")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", res.StatusCode)
		}
		if got := res.Header.Get("HX-Redirect"); got != want {
			t.Fatalf("wire HX-Redirect = %q, want %q", got, want)
		}
	})

	t.Run("plain form Location", func(t *testing.T) {
		res, err := client.Post(srv.URL+"/o/org-1/p/proj-1/settings/providers/config", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		if res.StatusCode != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303", res.StatusCode)
		}
		if got := res.Header.Get(echo.HeaderLocation); got != want {
			t.Fatalf("wire location = %q, want %q", got, want)
		}
	})
}
