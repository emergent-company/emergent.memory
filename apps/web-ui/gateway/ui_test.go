package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/labstack/echo/v4"
)

func TestPageTitle(t *testing.T) {
	cases := []struct {
		name     string
		segments []string
		want     string
	}{
		{"single segment", []string{"Agents"}, "Agents — Memory"},
		{"entity and section", []string{"Ada", "Settings"}, "Ada — Settings — Memory"},
		{"blank segment dropped", []string{"Agents", ""}, "Agents — Memory"},
		{"whitespace segment dropped", []string{"Agents", "  "}, "Agents — Memory"},
		{"all blank still yields brand", []string{"", "  "}, "Memory"},
		{"order preserved", []string{"Ada", "Memories"}, "Ada — Memories — Memory"},
		{"trims surrounding whitespace", []string{"  Agents  "}, "Agents — Memory"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pageTitle(tc.segments...); got != tc.want {
				t.Errorf("pageTitle(%q) = %q, want %q", tc.segments, got, tc.want)
			}
		})
	}
}

// TestPageHistoryRestoreFullShell guards the browser back/forward fix. htmx v4
// restores history by selecting [hx-history-elt] out of the response and
// outerSync-swapping it into #main-content, so a history-restore request must
// return the full shell (which carries the marker), never a bare fragment.
// Regression: fragments made htmx fall back to swapping document.body, wiping
// the stylesheet <link> and scripts that live in <body> (no CSS, no JS).
func TestPageHistoryRestoreFullShell(t *testing.T) {
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: &fakeMemory{}}
	e := echo.New()
	e.GET("/agents", func(c echo.Context) error {
		return s.page(c, pageTitle("Agents"), templ.Raw("<p>agent body</p>"))
	})

	// Browser back/forward: full shell so htmx can select the history target.
	req := httptest.NewRequest(http.MethodGet, "/agents", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-History-Restore-Request", "true")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "<html") {
		t.Fatalf("history restore must return the full document, got %q", body)
	}
	if !strings.Contains(body, `hx-history-elt`) {
		t.Fatal("full shell must mark its history restore target (hx-history-elt)")
	}
	if !strings.Contains(body, "agent body") {
		t.Fatal("history restore shell must include the page content")
	}

	// Boosted/partial navigation still gets just the fragment.
	req = httptest.NewRequest(http.MethodGet, "/agents", nil)
	req.Header.Set("HX-Request", "true")
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	body = rec.Body.String()
	if strings.Contains(body, "<html") {
		t.Errorf("partial navigation must not include the shell, got %q", body)
	}
	if !strings.Contains(body, "agent body") {
		t.Errorf("partial navigation must include the content, got %q", body)
	}
}
