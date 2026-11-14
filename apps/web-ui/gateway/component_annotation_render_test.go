package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// component_annotation_render_test.go covers what the source scan in
// component_annotation_coverage_test.go cannot: that the middleware switch
// actually reaches the rendered HTML, and that a component which delegates to
// another component reports the boundary that owns the element.
//
// The scan proves a `devmode.Attrs(ctx, "gateway/X")` call exists in the source.
// It cannot prove the attribute is emitted on the response: the call is a no-op
// when the context is not flagged dev mode, and it is silent when a component
// renders a different branch than the one it annotated. These tests render
// through the same middleware the server installs (main.go) and assert on the
// bytes a client would receive.

// TestPageHeadAnnotationReachesTheResponse pins the contract the element-level
// feedback overlay depends on: a page header rendered by a real request carries
// data-component="gateway/pageHead". Without this, an overlay report on the page
// title falls back to the nearest annotated layout container and names the
// wrong component.
func TestPageHeadAnnotationReachesTheResponse(t *testing.T) {
	const want = `data-component="gateway/pageHead"`

	render := func(t *testing.T, mw echo.MiddlewareFunc) string {
		t.Helper()
		e := echo.New()
		if mw != nil {
			e.Use(mw)
		}
		e.GET("/", func(c echo.Context) error {
			return pageHead(headerSpec{Group: "Objects", Title: "Objects"}).
				Render(c.Request().Context(), c.Response())
		})
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}

	t.Run("annotated under the server middleware", func(t *testing.T) {
		body := render(t, componentAnnotations())
		if !strings.Contains(body, want) {
			t.Fatalf("pageHead response is missing %s:\n%s", want, body)
		}
	})

	// The gate proves the annotation is one switch, not a hardcoded attribute:
	// with the middleware absent nothing is annotated, and the component is back
	// to plain markup. A literal data-component in a templ file would survive
	// this and fragment the contract (see component_annotation_coverage_test.go).
	t.Run("silent when the middleware is absent", func(t *testing.T) {
		body := render(t, nil)
		if strings.Contains(body, "data-component=") {
			t.Fatalf("expected no data-component without componentAnnotations(), got:\n%s", body)
		}
	})
}

// TestTypeIconTileAnnotationFollowsTheVariant pins the delegation boundary.
// typeIconTile renders its own annotated element at lead scale, but at row and
// compact scale it delegates to go-daisy's ui.IconTile, which annotates itself
// as ui/IconTile. Asserting only "some data-component is present" would pass
// while the overlay reported the wrong component, so the lead case asserts the
// gateway name is present and the row case asserts it is absent.
func TestTypeIconTileAnnotationFollowsTheVariant(t *testing.T) {
	t.Run("lead scale is the gateway component", func(t *testing.T) {
		body := renderHTMLDev(t, typeIconTile("", "", typeIconTileLead))
		if !strings.Contains(body, `data-component="gateway/TypeIconTile"`) {
			t.Fatalf("lead tile is missing gateway/TypeIconTile:\n%s", body)
		}
	})

	t.Run("row scale delegates to ui.IconTile", func(t *testing.T) {
		body := renderHTMLDev(t, typeIconTile("", "", typeIconTileRow))
		if strings.Contains(body, `data-component="gateway/TypeIconTile"`) {
			t.Fatalf("row tile claims gateway/TypeIconTile but renders ui.IconTile:\n%s", body)
		}
		if !strings.Contains(body, `data-component="ui/IconTile"`) {
			t.Fatalf("row tile is not annotated as ui/IconTile:\n%s", body)
		}
	})
}

// TestObjectHeaderAnnotationCoversBothVariants pins that the page-head scale and
// the list scale both report the same boundary. objectHeader is what a wiki page,
// a detail page, a browse card and a relationship row all render, so a variant
// that lost the annotation would make the overlay name a layout container on that
// surface only — the kind of gap a single-fixture test misses.
func TestObjectHeaderAnnotationCoversBothVariants(t *testing.T) {
	const want = `data-component="gateway/ObjectHeader"`

	cases := []struct {
		name  string
		props objectHeaderProps
	}{
		{
			name:  "lead",
			props: objectHeaderProps{Variant: objectHeaderLead, Type: "Object", Label: "Ada Lovelace", Ref: "abc123"},
		},
		{
			name:  "row",
			props: objectHeaderProps{Variant: objectHeaderRow, Type: "Object", Label: "Ada Lovelace", Ref: "abc123", Href: "/objects/abc123"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := renderHTMLDev(t, objectHeader(tc.props))
			if !strings.Contains(body, want) {
				t.Fatalf("%s objectHeader is missing %s:\n%s", tc.name, want, body)
			}
		})
	}
}
