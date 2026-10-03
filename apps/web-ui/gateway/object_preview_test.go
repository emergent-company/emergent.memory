package main

import (
	"os"
	"strings"
	"testing"
)

// TestObjectPreviewProperties pins the property-list contract: sorted by key,
// internal (underscore) names dropped, and — unlike graphObjectProperties —
// empty values retained so the preview shows the object's full shape.
func TestObjectPreviewProperties(t *testing.T) {
	obj := GraphObject{
		Properties: map[string]any{
			"_internal": "hidden",
			"zeta":      []any{"a", "b"},
			"alpha":     "first",
			"empty":     "",
			"count":     float64(3),
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
		`data-preview-field`,
		`data-empty`,
		`my-note`,
		`A short note`,
		`active`,
		`alpha`,
		`beta`,
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
