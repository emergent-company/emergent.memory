package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The board must use the full-bleed page container and a width-sharing lane
// grid (go-daisy `ContainerFull` = `max-w-none`). The width is applied from a
// runtime value (`string(size)`), so the compiled-stylesheet side of this
// contract is covered by the `@source inline("max-w-none")` safelist in
// webui/css/app.css; here we assert the rendered page asks for it.
func TestBoardPageUsesFullBleedContainer(t *testing.T) {
	f := &fakeMemory{workItems: []WorkItem{
		{CanonicalID: "w1", Type: "BoardTask", Key: "ship-1", Status: "ready"},
	}}
	_, e := newBoardEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	if !strings.Contains(body, "max-w-none") {
		t.Errorf("board page does not render the full-bleed container width (max-w-none):\n%s", body)
	}
	if strings.Contains(body, "max-w-6xl") {
		t.Errorf("board page still renders the capped container width (max-w-6xl)")
	}
}

// The status lanes must form a single row that divides the available width,
// instead of a wrapping auto-fill grid (which either leaves a trailing empty
// column or strands a lone lane on a second row) or fixed-width scroll
// columns: a column-flow grid whose lanes grow to an equal share of the row,
// with a 9rem desktop floor so the canonical six lanes fit at a 1280px
// viewport and horizontal scroll is reserved for genuinely narrow ones.
func TestBoardLanesShareWidth(t *testing.T) {
	f := &fakeMemory{workItems: []WorkItem{
		{CanonicalID: "w1", Type: "BoardTask", Key: "ship-1", Status: "ready"},
		{CanonicalID: "w2", Type: "BoardTask", Key: "ship-2", Status: "in_progress"},
	}}
	_, e := newBoardEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board", nil))
	body := rec.Body.String()

	for _, want := range []string{
		"grid-flow-col",
		"auto-cols-[minmax(15rem,1fr)]",
		"lg:auto-cols-[minmax(9rem,1fr)]",
		"min-w-0 flex-col rounded-box",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("board lanes missing width-sharing grid member %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "grid-cols-[repeat(auto-fill,minmax(12rem,1fr))]") {
		t.Errorf("board lanes still use the wrapping auto-fill grid:\n%s", body)
	}
	if strings.Contains(body, "min-w-64") {
		t.Errorf("board lanes still carry the fixed lane width (min-w-64)")
	}
}

// The board page must load the live-refresh client (SSE-driven #board swap).
func TestBoardPageLoadsLiveRefreshClient(t *testing.T) {
	f := &fakeMemory{workItems: []WorkItem{
		{CanonicalID: "w1", Type: "BoardTask", Key: "ship-1", Status: "ready"},
	}}
	_, e := newBoardEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board", nil))
	body := rec.Body.String()

	if !strings.Contains(body, "/js/board.js") {
		t.Errorf("board page does not load the live-refresh client (/js/board.js):\n%s", body)
	}
}
