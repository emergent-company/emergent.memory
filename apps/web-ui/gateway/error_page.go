package main

import (
	"bytes"
	"errors"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// brandedErrorStatuses are the statuses that render the friendly full-page
// error document for browser navigations.
func brandedErrorStatus(status int) bool {
	return status == http.StatusForbidden || status == http.StatusNotFound || status == http.StatusInternalServerError
}

// wantsHTML reports whether the request is a browser/htmx navigation that
// should receive an HTML error document rather than the JSON default.
func wantsHTML(c echo.Context) bool {
	return strings.Contains(c.Request().Header.Get("Accept"), "text/html")
}

// isAppSurface excludes non-HTML/API surfaces from branded error rendering so
// JSON/asset/webhook callers keep Echo's default response.
func isAppSurface(path string) bool {
	for _, p := range []string{"/api/", "/share/", "/internal/", "/static/", "/assets/", "/webhooks/", "/auth/"} {
		if strings.HasPrefix(path, p) {
			return false
		}
	}
	return true
}

// installErrorHandler wraps Echo's HTTP error handler: 403/404/500 requests
// that want HTML and target an app surface get the branded error page; every
// other error is delegated to Echo's default handler unchanged. Catches both
// unmatched routes and handler-returned echo.NewHTTPError.
func installErrorHandler(e *echo.Echo) {
	def := e.HTTPErrorHandler
	if def == nil {
		def = e.DefaultHTTPErrorHandler
	}
	e.HTTPErrorHandler = func(err error, c echo.Context) {
		status := http.StatusInternalServerError
		var he *echo.HTTPError
		if errors.As(err, &he) {
			status = he.Code
		}
		if !c.Response().Committed && brandedErrorStatus(status) && wantsHTML(c) && isAppSurface(c.Request().URL.Path) {
			// Boosted htmx navigation: the app opts 4xx/5xx out of swapping
			// (htmx.config.noSwap in ui.templ), so the branded document would be
			// fetched and discarded while the browser stays on the previous page.
			// Force a full-page load of the same URL instead; the follow-up
			// navigation is not boosted, so it renders the branded page. GET only
			// — a failed mutation must never be turned into a GET navigation.
			// RequestURI (not URL.Path) preserves org/project scope + query.
			if c.Request().Method == http.MethodGet && c.Request().Header.Get("HX-Boosted") == "true" {
				c.Response().Header().Set("HX-Redirect", c.Request().RequestURI)
				_ = c.NoContent(status)
				return
			}
			var buf bytes.Buffer
			if rerr := errorPage(errorPageForStatus(status)).Render(c.Request().Context(), &buf); rerr == nil {
				_ = c.HTMLBlob(status, buf.Bytes())
				return
			}
		}
		def(err, c)
	}
}
