package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestInstallErrorHandlerBrandedStatuses(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		heading string
	}{
		{"forbidden", http.StatusForbidden, "Access denied"},
		{"not found", http.StatusNotFound, "Page not found"},
		{"server error", http.StatusInternalServerError, "Something went wrong"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()
			installErrorHandler(e)
			e.GET("/boom", func(c echo.Context) error {
				return echo.NewHTTPError(tc.status, "x")
			})

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/boom", nil)
			req.Header.Set("Accept", "text/html")
			e.ServeHTTP(rec, req)

			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
				t.Fatalf("Content-Type = %q, want text/html", ct)
			}
			body := rec.Body.String()
			if !strings.Contains(body, tc.heading) {
				t.Errorf("body missing heading %q", tc.heading)
			}
			if !strings.Contains(body, `data-testid="error-home"`) || !strings.Contains(body, `href="/"`) {
				t.Errorf("body missing home action (data-testid=error-home, href=/)")
			}
		})
	}
}

func TestInstallErrorHandlerUnknownRoute(t *testing.T) {
	e := echo.New()
	installErrorHandler(e)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/does-not-exist", nil)
	req.Header.Set("Accept", "text/html")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Errorf("Content-Type = %q, want text/html", rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(body, "Page not found") || !strings.Contains(body, `data-testid="error-page"`) {
		t.Errorf("unknown route not rendered as branded 404 page")
	}
}

func TestInstallErrorHandlerAPISurfaceStaysJSON(t *testing.T) {
	e := echo.New()
	installErrorHandler(e)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/does-not-exist", nil)
	req.Header.Set("Accept", "application/json")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"message"`) {
		t.Errorf("body missing Echo default message field: %s", body)
	}
	if strings.Contains(body, `data-testid="error-home"`) {
		t.Errorf("API surface got branded page: %s", body)
	}
}

// A boosted htmx GET navigation must not receive the branded document (the app
// opts 4xx/5xx out of swapping), but must be told to do a full-page load so the
// branded page is actually shown.
func TestInstallErrorHandlerBoostedNavigationRedirects(t *testing.T) {
	e := echo.New()
	installErrorHandler(e)
	e.GET("/boom", func(c echo.Context) error {
		return echo.NewHTTPError(http.StatusNotFound, "gone")
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("HX-Boosted", "true")
	req.Header.Set("HX-Request", "true")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if loc := rec.Header().Get("HX-Redirect"); loc != "/boom" {
		t.Fatalf("HX-Redirect = %q, want /boom", loc)
	}
	if strings.Contains(rec.Body.String(), `data-testid="error-page"`) {
		t.Errorf("boosted navigation got a branded body instead of a redirect: %s", rec.Body.String())
	}

	// The follow-up full-page navigation carries no HX-Boosted and renders the page.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/boom", nil)
	req2.Header.Set("Accept", "text/html")
	e.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNotFound || !strings.Contains(rec2.Body.String(), `data-testid="error-page"`) {
		t.Fatalf("follow-up navigation = %d, branded=%v; want branded 404", rec2.Code, strings.Contains(rec2.Body.String(), `data-testid="error-page"`))
	}
}

// HX-Redirect must carry the scoped path + query, otherwise a scoped 404 would
// bounce the user out of their org/project context.
func TestInstallErrorHandlerBoostedRedirectPreservesScopeAndQuery(t *testing.T) {
	e := echo.New()
	installErrorHandler(e)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/o/acme/p/widgets/missing?tab=activity", nil)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("HX-Boosted", "true")
	e.ServeHTTP(rec, req)

	if got, want := rec.Header().Get("HX-Redirect"), "/o/acme/p/widgets/missing?tab=activity"; got != want {
		t.Fatalf("HX-Redirect = %q, want %q", got, want)
	}
}

// A failed boosted POST must never be turned into a GET navigation (that would
// replay/obscure the mutation), and a non-boosted partial keeps its response.
func TestInstallErrorHandlerNonBoostedHTMXNotRedirected(t *testing.T) {
	e := echo.New()
	installErrorHandler(e)

	partial := httptest.NewRecorder()
	preq := httptest.NewRequest(http.MethodGet, "/partial/x", nil)
	preq.Header.Set("Accept", "text/html")
	preq.Header.Set("HX-Request", "true") // partial, not boosted
	e.ServeHTTP(partial, preq)
	if partial.Header().Get("HX-Redirect") != "" {
		t.Errorf("non-boosted partial got HX-Redirect = %q", partial.Header().Get("HX-Redirect"))
	}

	post := httptest.NewRecorder()
	poq := httptest.NewRequest(http.MethodPost, "/mutate", nil)
	poq.Header.Set("Accept", "text/html")
	poq.Header.Set("HX-Boosted", "true")
	e.ServeHTTP(post, poq)
	if post.Header().Get("HX-Redirect") != "" {
		t.Errorf("boosted POST got HX-Redirect = %q, want none", post.Header().Get("HX-Redirect"))
	}
}

// The branded 500 must never echo the underlying error detail.
func TestInstallErrorHandler500DoesNotLeakDetail(t *testing.T) {
	e := echo.New()
	installErrorHandler(e)
	e.GET("/boom", func(c echo.Context) error {
		return echo.NewHTTPError(http.StatusInternalServerError, "secret token hunter2 at /var/lib/db:5432")
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	req.Header.Set("Accept", "text/html")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	for _, leak := range []string{"hunter2", "/var/lib/db", "secret token", "5432"} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Errorf("500 body leaked internal detail %q: %s", leak, rec.Body.String())
		}
	}
}

func TestInstallErrorHandlerNonBrandedStatusDelegates(t *testing.T) {
	e := echo.New()
	installErrorHandler(e)
	e.GET("/bad", func(c echo.Context) error {
		return echo.NewHTTPError(http.StatusBadRequest, "bad")
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/bad", nil)
	req.Header.Set("Accept", "text/html")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"message"`) {
		t.Errorf("body missing Echo default message field: %s", body)
	}
	if strings.Contains(body, `data-testid="error-home"`) {
		t.Errorf("non-branded status got branded page: %s", body)
	}
}
