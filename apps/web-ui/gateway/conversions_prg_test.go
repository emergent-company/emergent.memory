package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// conversions_prg_test.go pins the error path of uiRetryAllConversions
// (POST /documents/retry-conversion-failures), the one redirectScoped call site
// that had no test at all (#854), plus the route registration itself. The
// boosted/plain PRG wire-shape matrix for this handler lives in
// scope_prg_test.go, next to the same matrix for its three siblings
// (uiRetryAllExtractions, uiRetryExtraction, uiRetryConversion); the gate behind
// all of it is ui-rule-prg-contract:
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
	if want := "/o/org-1/p/proj-1/documents?err="; !strings.HasPrefix(loc, want) {
		t.Fatalf("location = %q, want prefix %q", loc, want)
	}
}

// The error path has the same two wire shapes as the success path: a boosted
// form gets 200 + HX-Redirect carrying ?err= (a 303 would be followed by fetch
// and the flash would never reach htmx), while the test above pins the plain
// form's 303 + Location.
func TestRetryAllConversionsBoostedErrorRedirect(t *testing.T) {
	f := &fakeMemory{bulkParsingErr: errConversionRetryUnavailable}
	s, _ := newClientEcho(f, Config{AuthMode: "session"})
	e := mountScopedConversionsRetry(s)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/o/org-1/p/proj-1"+conversionsRetryRoute, nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Boosted", "true")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a boosted form; body %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get(echo.HeaderLocation); got != "" {
		t.Fatalf("location = %q, want empty: a boosted form navigates via HX-Redirect, not Location", got)
	}
	loc := rec.Header().Get("HX-Redirect")
	if want := "/o/org-1/p/proj-1/documents?err="; !strings.HasPrefix(loc, want) {
		t.Fatalf("HX-Redirect = %q, want prefix %q", loc, want)
	}
}

// TestRetryAllConversionsRoute pins that the handler is reachable at the
// production path and that main.go registers that path on it. The router is
// assembled inside func main, which a test cannot call, so the production half
// is a source check of the registration line rather than a request through the
// real router.
func TestRetryAllConversionsRoute(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if want := `e.POST("` + conversionsRetryRoute + `", s.uiRetryAllConversions)`; !strings.Contains(string(src), want) {
		t.Fatalf("main.go does not register %s on uiRetryAllConversions: want %q", conversionsRetryRoute, want)
	}

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
