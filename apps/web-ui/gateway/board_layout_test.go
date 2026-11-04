package main

import (
	"net/http"
	"net/http/httptest"
	"os"
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

// The board page must wire the live refresh: the htmx hx-sse extension plus a
// connection anchor. The old client-side poller (/js/board.js) is gone — it
// listened for entity.created/updated/deleted and therefore never saw the
// entity.status_changed/entity.assigned events the work path emits.
func TestBoardPageWiresLiveStream(t *testing.T) {
	f := &fakeMemory{workItems: []WorkItem{
		{CanonicalID: "w1", Type: "BoardTask", Key: "ship-1", Status: "ready"},
	}}
	_, e := newBoardEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board", nil))
	body := rec.Body.String()

	for _, want := range []string{
		"/js/hx-sse.js",
		`hx-sse:connect="/board/stream"`,
		`id="board-header"`,
		`id="board-health"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("board page is missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "/js/board.js") {
		t.Errorf("board page still loads the retired client-side poller (/js/board.js)")
	}
}

// The drag highlight must be drawn INSIDE the lane. Lanes live in an
// overflow-x-auto row with no top padding, so the previous external outline
// (offset +2px) had its top edge clipped by the scroll container.
func TestBoardDropRingIsDrawnInsideTheLane(t *testing.T) {
	css, err := os.ReadFile("webui/css/app.css")
	if err != nil {
		t.Fatalf("read app.css: %v", err)
	}
	s := string(css)
	for _, selector := range []string{".memory-board__lane[data-drop-eligible]", ".memory-board__lane--drop"} {
		block := cssRuleBlock(t, s, selector)
		if !strings.Contains(block, "outline-offset: -2px") {
			t.Errorf("%s must draw its ring inside the lane (outline-offset: -2px); got:\n%s", selector, block)
		}
	}
	// The active target has to outrank the eligibility ring, which is a
	// class+attribute selector (0,2,0): with a single class (0,1,0) the hovered
	// lane kept the dashed ring and only the background tint distinguished it,
	// so the solid "drop here" ring never appeared.
	if !strings.Contains(s, ".memory-board__lane.memory-board__lane--drop") {
		t.Errorf("the active drop-target rule must be two-class specific (.memory-board__lane.memory-board__lane--drop) to override the eligibility ring")
	}
	// ...and it has to come after the eligibility rule. Both selectors are
	// (0,2,0), so specificity cannot decide between them: only source order can.
	// Pinning the shape alone left a later reorder free to put the dashed ring
	// back on the hovered lane with the suite still green (#2464).
	eligibilityAt := strings.Index(s, ".memory-board__lane[data-drop-eligible] {")
	activeAt := strings.Index(s, ".memory-board__lane.memory-board__lane--drop {")
	if eligibilityAt < 0 || activeAt < 0 {
		t.Fatalf("both drop-ring rules must be present in app.css: eligibility at %d, active at %d", eligibilityAt, activeAt)
	}
	if activeAt < eligibilityAt {
		t.Errorf("the active drop-target rule (.memory-board__lane.memory-board__lane--drop) must come after the eligibility rule: both are (0,2,0), so only source order decides which ring the hovered lane gets")
	}
}

// The eligibility ring is a class+attribute selector, so the lane element must
// carry the class for it to paint. Nothing else in the page uses
// .memory-board__lane, so a rename on either side of this pair leaves the drag
// affordance invisible with no error anywhere — pin both halves here.
func TestBoardLaneCarriesTheDropAffordanceHook(t *testing.T) {
	f := &fakeMemory{workItems: []WorkItem{
		{CanonicalID: "w1", Type: "BoardTask", Key: "ship-1", Status: "ready"},
	}}
	_, e := newBoardEcho(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board", nil))
	body := rec.Body.String()

	lane := elementEndingAt(t, body, `<section`, `data-board-column="ready"`)
	if !strings.Contains(lane, "memory-board__lane") {
		t.Errorf("board lane element does not carry the memory-board__lane hook, so .memory-board__lane[data-drop-eligible] never matches:\n%s", lane)
	}

	css, err := os.ReadFile("webui/css/app.css")
	if err != nil {
		t.Fatalf("read app.css: %v", err)
	}
	if !strings.Contains(string(css), ".memory-board__lane[data-drop-eligible]") {
		t.Errorf("app.css no longer styles the eligibility ring through .memory-board__lane[data-drop-eligible]")
	}
}

// elementEndingAt isolates the opening tag that carries attribute: it starts at
// the last tagBefore before that attribute, so an assertion cannot pass on a
// class that belongs to a different element earlier in the page.
func elementEndingAt(t *testing.T, s, tagBefore, attribute string) string {
	t.Helper()
	end := strings.Index(s, attribute)
	if end < 0 {
		t.Fatalf("attribute %q not found in rendered board", attribute)
	}
	start := strings.LastIndex(s[:end], tagBefore)
	if start < 0 {
		t.Fatalf("no %q before %q", tagBefore, attribute)
	}
	return s[start : end+len(attribute)]
}

// cssRuleBlock returns the declaration block for selector, for assertions that
// must not accidentally match a different rule elsewhere in the sheet.
func cssRuleBlock(t *testing.T, css, selector string) string {
	t.Helper()
	start := strings.Index(css, selector+" {")
	if start < 0 {
		t.Fatalf("selector %q not found in app.css", selector)
	}
	rest := css[start:]
	end := strings.Index(rest, "}")
	if end < 0 {
		t.Fatalf("unterminated block for %q", selector)
	}
	return rest[:end]
}
