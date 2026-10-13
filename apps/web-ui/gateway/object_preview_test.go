package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// TestObjectPreviewProperties pins the property-list contract: sorted by key,
// internal (underscore) names dropped, the mirrored "status" key dropped (it is
// already rendered once in the header badge), and — unlike graphObjectProperties
// — empty values retained so the preview shows the object's full shape.
func TestObjectPreviewProperties(t *testing.T) {
	obj := GraphObject{
		Properties: map[string]any{
			"_internal": "hidden",
			"zeta":      []any{"a", "b"},
			"alpha":     "first",
			"empty":     "",
			"count":     float64(3),
			"status":    "in_progress",
		},
	}
	got := objectPreviewProperties(obj)
	want := []propertyKV{
		{Key: "alpha", Value: "first"},
		{Key: "count", Value: "3"},
		{Key: "empty", Value: ""},
		{Key: "zeta", Value: "a, b"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d properties, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("property %d = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestObjectPreviewValue(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"nil", nil, ""},
		{"string", "hello", "hello"},
		{"number", float64(2.5), "2.5"},
		{"bool", true, "true"},
		{"slice", []any{"x", "y"}, "x, y"},
		{"string slice", []string{"p", "q"}, "p, q"},
	}
	for _, c := range cases {
		if got := objectPreviewValue(c.in); got != c.want {
			t.Errorf("%s: objectPreviewValue(%#v) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// TestObjectPreviewContentIsReadOnly pins the summary contract: the identity
// heading id, label/value rows (with empty rows de-emphasised and tagged), and
// — critically — that no form controls leak into the preview.
func TestObjectPreviewContentIsReadOnly(t *testing.T) {
	obj := &GraphObject{
		ID:     "11111111-1111-1111-1111-111111111111",
		Type:   "note",
		Key:    "my-note",
		Status: "active",
		Labels: []string{"alpha", "beta"},
		Properties: map[string]any{
			"summary": "A short note",
			"extra":   "",
		},
	}
	html := renderHTML(t, objectPreviewContent(obj, nil, "", nil))

	for _, want := range []string{
		`id="object-preview-heading"`,
		`id="object-preview-id"`,
		`data-copy-target="#object-preview-id"`,
		`data-testid="object-preview-copy-id"`,
		`data-preview-field`,
		`data-empty`,
		`my-note`,
		`A short note`,
		`active`,
		`alpha`,
		`beta`,
		`11111111-1111-1111-1111-111111111111`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("preview content missing %q\n%s", want, html)
		}
	}
	for _, bad := range []string{"<input", "<textarea", "<select"} {
		if strings.Contains(html, bad) {
			t.Errorf("preview content must be read-only, found %q", bad)
		}
	}
	// The Details section (type/key/status/created/id) is gone — identity
	// metadata lives in the header, not as duplicated property rows.
	for _, gone := range []string{">Details<", ">Created<"} {
		if strings.Contains(html, gone) {
			t.Errorf("preview content must not render the removed Details rows, found %q", gone)
		}
	}
}

// srOnlySpanText extracts the rendered text of the visually-hidden canonical-id
// span (#object-preview-id) from a preview-content render, so tests can assert
// exactly what the copy control yields.
func srOnlySpanText(t *testing.T, html string) string {
	t.Helper()
	const marker = `id="object-preview-id"`
	i := strings.Index(html, marker)
	if i < 0 {
		t.Fatalf("object-preview-id span missing:\n%s", html)
	}
	tail := html[i:]
	open := strings.Index(tail, ">")
	if open < 0 {
		t.Fatalf("object-preview-id span has no closing bracket:\n%s", html)
	}
	content := tail[open+1:]
	close := strings.Index(content, "</span>")
	if close < 0 {
		t.Fatalf("object-preview-id span unterminated:\n%s", html)
	}
	return strings.TrimSpace(content[:close])
}

// TestObjectPreviewCanonicalIDCopySource pins the copy contract: the sr-only
// span carries the canonical id when present (never the version row id), and
// falls back to the version row id only when no canonical id exists.
func TestObjectPreviewCanonicalIDCopySource(t *testing.T) {
	withCanonical := &GraphObject{
		ID:          "version-row-id",
		CanonicalID: "canonical-123",
		Type:        "note",
		Key:         "k",
	}
	html := renderHTML(t, objectPreviewContent(withCanonical, nil, "", nil))
	if got := srOnlySpanText(t, html); got != "canonical-123" {
		t.Errorf("sr-only span = %q, want canonical id", got)
	}
	if strings.Contains(srOnlySpanText(t, html), "version-row-id") {
		t.Errorf("sr-only span must not carry the version row id when a canonical id exists:\n%s", html)
	}

	withoutCanonical := &GraphObject{ID: "version-row-id", Type: "note", Key: "k"}
	html2 := renderHTML(t, objectPreviewContent(withoutCanonical, nil, "", nil))
	if got := srOnlySpanText(t, html2); got != "version-row-id" {
		t.Errorf("sr-only span fallback = %q, want version row id", got)
	}

	if got := objectPreviewCanonicalID(GraphObject{ID: "a", CanonicalID: "b"}); got != "b" {
		t.Errorf("objectPreviewCanonicalID with canonical = %q, want %q", got, "b")
	}
	if got := objectPreviewCanonicalID(GraphObject{ID: "a"}); got != "a" {
		t.Errorf("objectPreviewCanonicalID without canonical = %q, want %q", got, "a")
	}
}

// TestObjectPreviewContentRelationshipLabel guards the optional relationship
// context line carried through from a /objects/<src>#relationship-<rel> ref.
func TestObjectPreviewContentRelationshipLabel(t *testing.T) {
	obj := &GraphObject{ID: "abc", Type: "note", Key: "k"}
	withRel := renderHTML(t, objectPreviewContent(obj, nil, "cites", nil))
	if !strings.Contains(withRel, "Referenced via") || !strings.Contains(withRel, "cites") {
		t.Errorf("relationship context missing:\n%s", withRel)
	}
	withoutRel := renderHTML(t, objectPreviewContent(obj, nil, "", nil))
	if strings.Contains(withoutRel, "Referenced via") {
		t.Errorf("relationship context should be omitted when absent:\n%s", withoutRel)
	}
}

// TestObjectPreviewDrawerContract pins the drawer shell's accessibility and
// integration contract: dialog role, hidden-until-opened state, the htmx body
// target, and the edit/close actions object-preview.js drives.
func TestObjectPreviewDrawerContract(t *testing.T) {
	html := renderHTML(t, objectPreviewDrawer())
	for _, want := range []string{
		`id="object-preview-root"`,
		`id="object-preview-panel"`,
		`id="object-preview-backdrop"`,
		`id="object-preview-body"`,
		`id="object-preview-edit"`,
		`id="object-preview-close"`,
		`role="dialog"`,
		`aria-modal="true"`,
		`aria-hidden="true"`,
		`inert`,
		`data-testid="object-preview-edit"`,
		`data-testid="object-preview-close"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("drawer missing %q\n%s", want, html)
		}
	}
}

// TestObjectPreviewResizeAndModalContract pins the assistant-panel pattern
// (#1389) ported onto the object preview: the resizable separator, the
// double-width toggle, the modal-open action, and the modal box the aside is
// moved into. These ids/markers are what object-preview.js wires against.
func TestObjectPreviewResizeAndModalContract(t *testing.T) {
	html := renderHTML(t, objectPreviewDrawer())
	for _, want := range []string{
		`id="object-preview-resize"`,
		`role="separator"`,
		`aria-orientation="vertical"`,
		`aria-label="Resize object preview"`,
		`tabindex="0"`,
		`id="object-preview-width-toggle"`,
		`data-action="object-preview-toggle-width"`,
		`aria-pressed="false"`,
		`id="object-preview-open-modal"`,
		`data-action="object-preview-open-modal"`,
		`id="object-preview-modal"`,
		`id="object-preview-modal-box"`,
		`object-preview-drawer-only`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("resize/modal contract missing %q\n%s", want, html)
		}
	}
}

// TestObjectPreviewClientWiring pins the client half of #1389: the shared
// resize grip is reused (no parallel implementation), the modal move uses the
// object-preview-in-modal marker, and the separator is keyboard operable.
func TestObjectPreviewClientWiring(t *testing.T) {
	src, err := os.ReadFile("webui/static/js/object-preview.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(src)
	for _, want := range []string{
		`MemoryChatHost.createResizeGrip`,
		`memory.objectpreview.width.v1`,
		`"object-preview-resize"`,
		`"object-preview-width-toggle"`,
		`"object-preview-open-modal"`,
		`"object-preview-modal"`,
		`object-preview-in-modal`,
		`ArrowLeft`,
		`ArrowRight`,
		`[data-copy-target]`,
		`data-copy-target`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("object-preview.js missing %q", want)
		}
	}
}

func TestObjectPreviewNotFound(t *testing.T) {
	html := renderHTML(t, objectPreviewNotFound())
	if !strings.Contains(html, "Object unavailable") {
		t.Errorf("not-found state missing message:\n%s", html)
	}
}

// TestObjectPreviewStatusHumanised pins the status-label contract: the preview
// renders the humanised status (not the raw key) in the header badge, while
// preserving the raw value for machine fidelity.
func TestObjectPreviewStatusHumanised(t *testing.T) {
	obj := &GraphObject{
		ID:     "11111111-1111-1111-1111-111111111111",
		Type:   "note",
		Key:    "my-note",
		Status: "in_progress",
	}
	html := renderHTML(t, objectPreviewContent(obj, nil, "", nil))

	if !strings.Contains(html, "In progress") {
		t.Errorf("status should be humanised to 'In progress':\n%s", html)
	}
	if !strings.Contains(html, `data-status="in_progress"`) {
		t.Errorf("raw status must be preserved in data-status:\n%s", html)
	}
	if strings.Contains(html, ">in_progress<") {
		t.Errorf("raw status key must not be rendered as a text label:\n%s", html)
	}
}

// TestObjectPreviewHistoryNewestFirst pins the timeline contract: the versions
// render newest-first (the server returns version-DESC order), with the
// humanised status and version labels.
func TestObjectPreviewHistoryNewestFirst(t *testing.T) {
	versions := []GraphObject{
		{ID: "2", Version: 2, Type: "note", Key: "k", Status: "in_progress", CreatedAt: "2026-01-02T00:00:00Z"},
		{ID: "1", Version: 1, Type: "note", Key: "k", Status: "ready", CreatedAt: "2026-01-01T00:00:00Z"},
	}
	html := renderHTML(t, objectPreviewHistory(versions))

	for _, want := range []string{"v2", "v1", "In progress", `data-testid="object-preview-history"`, "k"} {
		if !strings.Contains(html, want) {
			t.Errorf("history missing %q:\n%s", want, html)
		}
	}
	if strings.Index(html, "v2") > strings.Index(html, "v1") {
		t.Errorf("newest version (v2) must render before v1:\n%s", html)
	}
}

// TestObjectPreviewHistoryEmptyAndError pins the timeline's degenerate states.
func TestObjectPreviewHistoryEmptyAndError(t *testing.T) {
	if html := renderHTML(t, objectPreviewHistory(nil)); !strings.Contains(html, "No change history recorded.") {
		t.Errorf("empty history missing message:\n%s", html)
	}
	if html := renderHTML(t, objectPreviewHistoryError()); !strings.Contains(html, "History unavailable") {
		t.Errorf("history error missing message:\n%s", html)
	}
}

// TestObjectPreviewHistoryLazySection pins the shared lazy-load contract: the
// History section renders an htmx load-triggered slot (spinner until swapped)
// scoped to /objects/:id/history.
func TestObjectPreviewHistoryLazySection(t *testing.T) {
	obj := &GraphObject{ID: "abc", Type: "note", Key: "k", Status: "ready"}
	html := renderHTML(t, objectPreviewContent(obj, nil, "", nil))
	for _, want := range []string{
		`data-testid="object-preview-history-slot"`,
		`hx-get="/objects/abc/history"`,
		`hx-trigger="load"`,
		`loading loading-spinner`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("history lazy section missing %q:\n%s", want, html)
		}
	}
}

// TestObjectPreviewHistoryPartial pins the handler: GET /objects/:id/history
// renders the timeline from the backend's version history.
func TestObjectPreviewHistoryPartial(t *testing.T) {
	f := &fakeMemory{
		objectHistory: []GraphObject{
			{ID: "w1", Version: 2, Type: "BoardTask", Key: "k", Status: "in_progress"},
			{ID: "w0", Version: 1, Type: "BoardTask", Key: "k", Status: "ready"},
		},
	}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.GET("/objects/:id/history", s.uiObjectHistoryPartial)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/w1/history", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "v2") || !strings.Contains(body, "v1") {
		t.Errorf("history partial missing versions:\n%s", body)
	}
	if strings.Index(body, "v2") > strings.Index(body, "v1") {
		t.Errorf("newest version must render first:\n%s", body)
	}
	if !strings.Contains(body, "In progress") {
		t.Errorf("history partial should humanise status:\n%s", body)
	}
}

// TestGetObjectHistoryClient pins the client decode: GET
// /api/graph/objects/:id/history returns a `{versions:[...]}` object (not the
// successEnvelope), decoded into []GraphObject with per-version numbers.
func TestGetObjectHistoryClient(t *testing.T) {
	var gotMethod, gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"versions":[
			{"id":"22222222-2222-2222-2222-222222222222","version":2,"type":"note","key":"k","status":"in_progress","created_at":"2026-01-02T00:00:00Z"},
			{"id":"11111111-1111-1111-1111-111111111111","version":1,"type":"note","key":"k","status":"ready","created_at":"2026-01-01T00:00:00Z"}
		]}`))
	}))
	defer ts.Close()

	m := NewMemoryClient(ts.URL, "proj")
	versions, err := m.GetObjectHistory(t.Context(), "abc")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodGet || gotPath != "/api/graph/objects/abc/history" {
		t.Errorf("request = %s %s, want GET /api/graph/objects/abc/history", gotMethod, gotPath)
	}
	if len(versions) != 2 {
		t.Fatalf("got %d versions, want 2", len(versions))
	}
	if versions[0].Version != 2 || versions[0].Status != "in_progress" {
		t.Errorf("versions[0] = %+v, want v2 in_progress", versions[0])
	}
	if versions[1].Version != 1 || versions[1].Status != "ready" {
		t.Errorf("versions[1] = %+v, want v1 ready", versions[1])
	}
}

// TestObjectPreviewWiring pins the integration points: the preview partial route
// is registered, and the app shell mounts the drawer + its script.
func TestObjectPreviewWiring(t *testing.T) {
	mainSrc, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mainSrc), `"/objects/:id/preview"`) ||
		!strings.Contains(string(mainSrc), `s.uiObjectPreviewPartial`) {
		t.Error("main.go must register GET /objects/:id/preview -> uiObjectPreviewPartial")
	}

	shellSrc, err := os.ReadFile("ui.templ")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"@objectPreviewDrawer()", "/js/object-preview.js"} {
		if !strings.Contains(string(shellSrc), want) {
			t.Errorf("appShell (ui.templ) missing %q", want)
		}
	}
}

// TestObjectPreviewInterceptionScope pins the client scoping contract: the
// drawer opens only for references inside a chat message list (or a sources
// citation link) — never for the .memory-md knowledge answer on the object page
// — and both htmx error events are handled.
func TestObjectPreviewInterceptionScope(t *testing.T) {
	src, err := os.ReadFile("webui/static/js/object-preview.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(src)
	for _, want := range []string{
		`#chat-messages, #sidepanel-messages`,
		`data-testid") === "citation-link"`,
		`htmx:response:error`,
		`htmx:error`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("object-preview.js missing %q", want)
		}
	}
	if strings.Contains(js, `closest(".memory-md")`) {
		t.Error("object-preview.js must not scope interception to .memory-md (matches non-chat surfaces too)")
	}
}
