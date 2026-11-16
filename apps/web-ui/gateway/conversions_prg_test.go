package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

// conversions_prg_test.go pins the PRG wire shape of uiRetryAllConversions
// (POST /documents/retry-conversion-failures), the one redirectScoped call site
// that had no test at all (#854). It is the same shape as its three siblings in
// scope_prg_test.go — uiRetryAllExtractions, uiRetryExtraction and
// uiRetryConversion — and the gate behind it is ui-rule-prg-contract:
//
//   - a boosted (htmx) form gets 200 + HX-Redirect and NO Location, because
//     htmx issues its request through fetch, which follows a 3xx transparently
//     and discards the header on the intermediate response (#828, #838);
//   - a plain form post gets 303 + Location and NO HX-Redirect;
//   - the error path keeps the ?err= flash and the scope.
//
// Each test fails if the handler regresses to the other half of that contract.

// errConversionRetryUnavailable is the upstream failure the error-path test
// injects through the fake.
var errConversionRetryUnavailable = errors.New("conversion retry unavailable")

// conversionsRetryRoute is the path main.go registers for the handler.
const conversionsRetryRoute = "/documents/retry-conversion-failures"

// mountScopedConversionsRetry mounts the handler the way scopeRewrite's Pre step
// dispatches it: the URL's org/project are stashed in the request context, with
// no scopeLocation middleware — the production shape that once answered a
// boosted POST with an unscoped target.
func mountScopedConversionsRetry(s *Server) *echo.Echo {
	e := echo.New()
	e.POST("/o/:org/p/:project"+conversionsRetryRoute, func(c echo.Context) error {
		ctx := context.WithValue(c.Request().Context(), scopeCtxKey{}, &requestScope{
			OrgRef: c.Param("org"), ProjectRef: c.Param("project"),
		})
		c.SetRequest(c.Request().WithContext(ctx))
		return s.uiRetryAllConversions(c)
	})
	return e
}

// A boosted form's redirect must be scoped and must ride HX-Redirect on a 200,
// for both outcomes the handler produces.
func TestRetryAllConversionsRedirectIsScopedWithoutMiddleware(t *testing.T) {
	for _, tc := range []struct {
		name     string
		retried  int
		wantTail string
	}{
		{name: "conversions retried", retried: 5, wantTail: "/documents?conversions_retried=5"},
		{name: "nothing to retry", retried: 0, wantTail: "/documents?conversions_none=1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeMemory{bulkRetried: tc.retried}
			s, _ := newClientEcho(f, Config{AuthMode: "session"})
			e := mountScopedConversionsRetry(s)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/o/org-1/p/proj-1"+conversionsRetryRoute, nil)
			req.Header.Set("HX-Request", "true")
			req.Header.Set("HX-Boosted", "true")
			e.ServeHTTP(rec, req)

			// 200, not 303: fetch follows a 3xx and the HX-Redirect on the
			// intermediate response never reaches htmx (#828).
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 for a boosted form; body %s", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get(echo.HeaderLocation); got != "" {
				t.Fatalf("location = %q, want empty: a boosted form navigates via HX-Redirect, not Location", got)
			}
			want := "/o/org-1/p/proj-1" + tc.wantTail
			if got := rec.Header().Get("HX-Redirect"); got != want {
				t.Fatalf("HX-Redirect = %q, want %q", got, want)
			}
		})
	}
}

// A plain form post gets no HX-Redirect: the browser follows Location, which is
// already scoped.
func TestRetryAllConversionsPlainFormRedirect(t *testing.T) {
	f := &fakeMemory{bulkRetried: 0}
	s, _ := newClientEcho(f, Config{AuthMode: "session"})
	e := mountScopedConversionsRetry(s)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/o/org-1/p/proj-1"+conversionsRetryRoute, nil)
	e.ServeHTTP(rec, req)

	// 303, not 302: ui-rule-prg-contract names 303 for the plain-form half.
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 for a plain form post; body %s", rec.Code, rec.Body.String())
	}
	if got, want := rec.Header().Get(echo.HeaderLocation), "/o/org-1/p/proj-1/documents?conversions_none=1"; got != want {
		t.Fatalf("location = %q, want %q", got, want)
	}
	if got := rec.Header().Get("HX-Redirect"); got != "" {
		t.Fatalf("HX-Redirect = %q, want empty for a plain form post", got)
	}
}

// The wire, not the header map, is what the browser receives: this mounts the
// real chain (scopeRewrite + scopeLocation + the handler) on a real server, so a
// scope rewrite that ran after the response committed would fail here rather
// than pass in a recorder.
func TestRetryAllConversionsScopesTheWireRedirect(t *testing.T) {
	f := &fakeMemory{bulkRetried: 5}
	s, _ := newClientEcho(f, Config{AuthMode: "session"})

	e := echo.New()
	e.Pre(s.scopeRewrite)
	e.Use(s.scopeLocation)
	e.POST(conversionsRetryRoute, s.uiRetryAllConversions)

	srv := httptest.NewServer(e)
	defer srv.Close()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/o/org-1/p/proj-1"+conversionsRetryRoute, nil)
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

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a boosted form", res.StatusCode)
	}
	if got := res.Header.Get(echo.HeaderLocation); got != "" {
		t.Fatalf("wire location = %q, want empty for a boosted form", got)
	}
	if got, want := res.Header.Get("HX-Redirect"), "/o/org-1/p/proj-1/documents?conversions_retried=5"; got != want {
		t.Fatalf("wire HX-Redirect = %q, want %q", got, want)
	}
}

// The error path keeps the ?err= flash and the scope: a mutation that failed
// must land the user back on the page that explains it, not on an unscoped 404.
func TestRetryAllConversionsErrorRedirectCarriesScope(t *testing.T) {
	f := &fakeMemory{bulkParsingErr: errConversionRetryUnavailable}
	s, _ := newClientEcho(f, Config{AuthMode: "session"})
	e := mountScopedConversionsRetry(s)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/o/org-1/p/proj-1"+conversionsRetryRoute, nil)
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 for a plain form post", rec.Code)
	}
	loc := rec.Header().Get(echo.HeaderLocation)
	if want := "/o/org-1/p/proj-1/documents?err="; len(loc) <= len(want) || loc[:len(want)] != want {
		t.Fatalf("location = %q, want prefix %q", loc, want)
	}
}

// TestRetryAllConversionsRoute pins that the handler is reachable at the path
// main.go registers and that it drives the bulk conversion retry — the route is
// otherwise only asserted by the handler-level tests above.
func TestRetryAllConversionsRoute(t *testing.T) {
	f := &fakeMemory{bulkRetried: 2}
	s, e := newClientEcho(f, Config{})
	e.POST(conversionsRetryRoute, s.uiRetryAllConversions)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, conversionsRetryRoute, nil)
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body %s", rec.Code, rec.Body.String())
	}
	if got, want := rec.Header().Get(echo.HeaderLocation), "/documents?conversions_retried=2"; got != want {
		t.Fatalf("location = %q, want %q", got, want)
	}
	if len(f.bulkIncludeNonRetryable) != 1 {
		t.Fatalf("bulk retry calls = %d, want 1: the route did not reach the handler", len(f.bulkIncludeNonRetryable))
	}
}
