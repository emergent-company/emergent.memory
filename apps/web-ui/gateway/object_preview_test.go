package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// TestObjectPreviewStoredProperties pins the stored-property contract: sorted
// by key, internal (underscore) names dropped, and — unlike
// graphObjectProperties — empty values retained so the preview shows the full
// shape of the object. The mirrored "status" key is kept here: whether the
// drawer renders it depends on whether the type declares a workflow, which is
// objectPreviewTypeFields' rule (a workflow type shows it as the built-in
// Workflow status row instead).
func TestObjectPreviewStoredProperties(t *testing.T) {
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
	got := objectPreviewStoredProperties(obj)
	want := []propertyKV{
		{Key: "alpha", Value: "first"},
		{Key: "count", Value: "3"},
		{Key: "empty", Value: ""},
		{Key: "status", Value: "in_progress"},
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

// TestObjectPreviewContentIdentity pins the summary contract: the identity
// heading id, label/value rows (with empty rows de-emphasised and tagged), and
// the editable workflow strip for a workflow type.
func TestObjectPreviewContentIdentity(t *testing.T) {
	obj := &GraphObject{
		ID:       "11111111-1111-1111-1111-111111111111",
		Type:     "note",
		Key:      "my-note",
		Status:   "active",
		Assignee: "sam",
		Labels:   []string{"alpha", "beta"},
		Properties: map[string]any{
			"summary": "A short note",
			"extra":   "",
		},
	}
	html := renderHTML(t, objectPreviewContent(obj, nil, "", objectPreviewSchema{
		TypeLabel:   "Note",
		HasWorkflow: true,
		Properties:  []objectPropertyDef{{Name: "summary", Description: "One-line summary."}},
	}, nil, objectPreviewChrome{
		Status:    &objectStatusControl{Show: true, Options: []string{"active", "done"}},
		Assignees: []boardAssigneeCandidate{{Value: "sam", Label: "sam"}},
		Surface:   objectWorkControlTarget{ObjectID: obj.ID, Preview: true, Actions: "board"},
	}))

	for _, want := range []string{
		`id="object-preview-heading"`,
		`id="object-preview-id"`,
		`data-copy-target="#object-preview-id"`,
		`data-testid="object-preview-copy-id"`,
		`data-preview-field`,
		`data-empty`,
		`my-note`,
		`A short note`,
		`data-testid="object-work-controls"`,
		`data-testid="object-status-control"`,
		`data-testid="object-assignee-control"`,
		`aria-label="Change status"`,
		`aria-label="Assignee"`,
		`data-tip="One-line summary."`,
		`alpha`,
		`beta`,
		`11111111-1111-1111-1111-111111111111`,
		`>Properties</h4>`,
		`>Note Properties</h4>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("preview content missing %q\n%s", want, html)
		}
	}
	// Status and assignee are edited in the top strip, not repeated as
	// read-only rows in the Properties panel.
	if strings.Contains(html, "Workflow status") {
		t.Errorf("preview must not render a read-only Workflow status row:\n%s", html)
	}
	if strings.Contains(html, `>Assignee</dt>`) {
		t.Errorf("preview must not render a read-only Assignee row:\n%s", html)
	}
	// The schema description is a tooltip, never a visible paragraph under the
	// label.
	if strings.Contains(html, "text-muted-faint mt-0.5 block") {
		t.Errorf("preview description must be a tooltip, not a visible block:\n%s", html)
	}
	// The removed Details section must not come back as a third panel.
	if strings.Contains(html, ">Details<") {
		t.Errorf("preview content must not render the removed Details section:\n%s", html)
	}
}

// TestObjectPreviewStatusPlacement pins where an object's status is read: a
// type that declares a workflow shows the editable top-strip status picker and
// does not repeat the mirrored status property in its type panel; a type with
// no workflow shows its own declared status property in the type panel and no
// workflow controls (the Scenario shape that prompted the fix).
func TestObjectPreviewStatusPlacement(t *testing.T) {
	t.Run("workflow type shows the status picker and hides the mirrored property", func(t *testing.T) {
		obj := &GraphObject{
			Type: "Task", Key: "T-1", Status: "in_progress",
			Properties: map[string]any{"status": "in_progress", "title": "Do it"},
		}
		html := renderHTML(t, objectPreviewContent(obj, nil, "", objectPreviewSchema{
			TypeLabel:   "Task",
			HasWorkflow: true,
			Properties:  []objectPropertyDef{{Name: "status"}, {Name: "title"}},
		}, nil, objectPreviewChrome{
			Status:  &objectStatusControl{Show: true, Options: []string{"ready", "in_progress", "review"}},
			Surface: objectWorkControlTarget{ObjectID: "t1", Preview: true},
		}))
		if !strings.Contains(html, `data-testid="object-status-control"`) {
			t.Errorf("workflow type should render the top status control:\n%s", html)
		}
		if !strings.Contains(html, "In progress") {
			t.Errorf("status trigger should show the current status:\n%s", html)
		}
		if strings.Contains(html, "Workflow status") {
			t.Errorf("preview must not render a read-only Workflow status row:\n%s", html)
		}
		if strings.Contains(html, `title="status"`) {
			t.Errorf("mirrored status property must not repeat in the type panel:\n%s", html)
		}
		if !strings.Contains(html, "Do it") {
			t.Errorf("type property missing:\n%s", html)
		}
	})

	t.Run("workflow type with an empty column keeps the stored property", func(t *testing.T) {
		// The server does not mirror properties.status into the column on every
		// write path; a value set on the property must not be hidden just because
		// the type declares a workflow.
		obj := &GraphObject{Type: "Task", Key: "T-2", Properties: map[string]any{"status": "review"}}
		html := renderHTML(t, objectPreviewContent(obj, nil, "", objectPreviewSchema{
			TypeLabel:   "Task",
			HasWorkflow: true,
			Properties:  []objectPropertyDef{{Name: "status"}},
		}, nil, objectPreviewChrome{
			Status:  &objectStatusControl{Show: true, Options: []string{"ready", "review"}},
			Surface: objectWorkControlTarget{ObjectID: "t2", Preview: true},
		}))
		if !strings.Contains(html, `title="status"`) {
			t.Errorf("stored status property must render when the column is empty:\n%s", html)
		}
		if !strings.Contains(html, "review") {
			t.Errorf("stored status value missing:\n%s", html)
		}
	})

	t.Run("no-workflow type shows the declared status property", func(t *testing.T) {
		obj := &GraphObject{
			Type: "Scenario", Key: "S-005",
			Properties: map[string]any{"status": "proposed"},
		}
		html := renderHTML(t, objectPreviewContent(obj, nil, "", objectPreviewSchema{
			TypeLabel:  "Scenario",
			Properties: []objectPropertyDef{{Name: "status", Description: "Lifecycle state."}},
		}, nil, objectPreviewChrome{}))
		if strings.Contains(html, `data-testid="object-work-controls"`) {
			t.Errorf("no-workflow type must not render the workflow strip:\n%s", html)
		}
		if strings.Contains(html, "Workflow status") {
			t.Errorf("no-workflow type must not render a Workflow status row:\n%s", html)
		}
		if !strings.Contains(html, "proposed") {
			t.Errorf("declared status property should be shown:\n%s", html)
		}
		if !strings.Contains(html, `title="status"`) {
			t.Errorf("status property should keep its raw name as tooltip:\n%s", html)
		}
		if !strings.Contains(html, ">Scenario Properties</h4>") {
			t.Errorf("type panel title missing:\n%s", html)
		}
		if !strings.Contains(html, `data-tip="Lifecycle state."`) {
			t.Errorf("schema description should render as an info tooltip:\n%s", html)
		}
	})
}

// TestObjectPreviewDeclaredLabel pins the title-precedence rule: a declared
// JSON-Schema title is the row label, otherwise the drawer humanises the key.
func TestObjectPreviewDeclaredLabel(t *testing.T) {
	cases := []struct {
		name string
		def  objectPropertyDef
		want string
	}{
		{"title wins", objectPropertyDef{Name: "entry_point", Title: "Entry point (URL)"}, "Entry point (URL)"},
		{"untitled humanises", objectPropertyDef{Name: "entry_point"}, "Entry point"},
		{"untitled single word", objectPropertyDef{Name: "email"}, "Email"},
	}
	for _, c := range cases {
		if got := objectPreviewDeclaredLabel(c.def); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// TestObjectPreviewFieldLabel pins the humanised field label used by the type
// panel, with the raw schema name kept separately as the row tooltip.
func TestObjectPreviewFieldLabel(t *testing.T) {
	cases := map[string]string{
		"status":      "Status",
		"entry_point": "Entry point",
		"first-name":  "First name",
		"a.b":         "A b",
		"état":        "État",
		"":            "",
	}
	for in, want := range cases {
		if got := objectPreviewFieldLabel(in); got != want {
			t.Errorf("objectPreviewFieldLabel(%q) = %q, want %q", in, got, want)
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
	html := renderHTML(t, objectPreviewContent(withCanonical, nil, "", objectPreviewSchema{}, nil))
	if got := srOnlySpanText(t, html); got != "canonical-123" {
		t.Errorf("sr-only span = %q, want canonical id", got)
	}
	if strings.Contains(srOnlySpanText(t, html), "version-row-id") {
		t.Errorf("sr-only span must not carry the version row id when a canonical id exists:\n%s", html)
	}

	withoutCanonical := &GraphObject{ID: "version-row-id", Type: "note", Key: "k"}
	html2 := renderHTML(t, objectPreviewContent(withoutCanonical, nil, "", objectPreviewSchema{}, nil))
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
	withRel := renderHTML(t, objectPreviewContent(obj, nil, "cites", objectPreviewSchema{}, nil))
	if !strings.Contains(withRel, "Referenced via") || !strings.Contains(withRel, "cites") {
		t.Errorf("relationship context missing:\n%s", withRel)
	}
	withoutRel := renderHTML(t, objectPreviewContent(obj, nil, "", objectPreviewSchema{}, nil))
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
	html := renderHTML(t, objectPreviewContent(obj, nil, "", objectPreviewSchema{}, nil))

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
	html := renderHTML(t, objectPreviewHistory(versions, nil))

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
	if html := renderHTML(t, objectPreviewHistory(nil, nil)); !strings.Contains(html, "No change history recorded.") {
		t.Errorf("empty history missing message:\n%s", html)
	}
	if html := renderHTML(t, objectPreviewHistoryError()); !strings.Contains(html, "History unavailable") {
		t.Errorf("history error missing message:\n%s", html)
	}
}

// TestObjectPreviewHistoryEntryExpandable pins the disclosure contract: every
// entry is a native <details>/<summary> (collapsed by default, keyboard
// operable with no JS) carrying the stable test ids, and the change list lives
// in the details body.
func TestObjectPreviewHistoryEntryExpandable(t *testing.T) {
	versions := []GraphObject{
		{ID: "2", Version: 2, Type: "note", Key: "k", Status: "ready",
			ChangeSummary: map[string]any{"added": map[string]any{"/summary": "hello"}}},
	}
	html := renderHTML(t, objectPreviewHistory(versions, nil))
	for _, want := range []string{
		`<details`,
		`<summary`,
		`data-testid="object-preview-history-entry"`,
		`data-testid="object-preview-history-changes"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("history entry missing %q:\n%s", want, html)
		}
	}
}

// TestObjectPreviewHistoryChangeSummary pins the recorded-summary rendering:
// added paths lead with "+", removed with "−", and updates render
// "<path>: <from> → <to>".
func TestObjectPreviewHistoryChangeSummary(t *testing.T) {
	versions := []GraphObject{
		{
			ID: "2", Version: 2, Type: "note", Key: "k", Status: "ready",
			ChangeSummary: map[string]any{
				"added":   map[string]any{"/summary": "hello", "/author": "me"},
				"removed": []any{"/old", "/legacy"},
				"updated": map[string]any{"/title": map[string]any{"from": "draft", "to": "final"}},
			},
		},
	}
	html := renderHTML(t, objectPreviewHistory(versions, nil))
	for _, want := range []string{
		"+ /author",
		"+ /summary",
		"− /legacy",
		"− /old",
		"/title: draft → final",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("change summary missing %q:\n%s", want, html)
		}
	}
}

// TestObjectPreviewHistoryMergeProvenance pins the merge-provenance keys: a
// merge version's change_summary carries source/strategy/enriched_keys (no
// added/removed/updated), which render as muted informational lines rather than
// an empty list or a panic.
func TestObjectPreviewHistoryMergeProvenance(t *testing.T) {
	versions := []GraphObject{
		{
			ID: "2", Version: 2, Type: "note", Key: "k",
			ChangeSummary: map[string]any{
				"source":        "merge",
				"strategy":      "three-way",
				"resolution":    "src",
				"enriched_keys": []any{"alpha", "beta"},
			},
		},
	}
	html := renderHTML(t, objectPreviewHistory(versions, nil))
	for _, want := range []string{
		"source: merge",
		"strategy: three-way",
		"resolution: src",
		"enriched keys: alpha, beta",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("merge provenance missing %q:\n%s", want, html)
		}
	}
}

// TestObjectPreviewHistoryDerivedFallback pins the derivation for a version
// with no recorded change_summary: status and assignee diff as from→to, and the
// top-level property paths diff (scalar from→to, added/removed paths, and
// nested/complex values as "changed").
func TestObjectPreviewHistoryDerivedFallback(t *testing.T) {
	versions := []GraphObject{
		{
			ID: "2", Version: 2, Type: "BoardTask", Key: "k", Status: "in_progress", Assignee: "bob",
			Properties: map[string]any{
				"count":  float64(2),
				"title":  "new title",
				"nested": map[string]any{"a": 2},
			},
		},
		{
			ID: "1", Version: 1, Type: "BoardTask", Key: "k", Status: "ready", Assignee: "alice",
			Properties: map[string]any{
				"count":        float64(1),
				"title":        "old title",
				"nested":       map[string]any{"a": 1},
				"removed_prop": "x",
			},
		},
	}
	html := renderHTML(t, objectPreviewHistory(versions, nil))
	for _, want := range []string{
		"status: ready → in_progress",
		"assignee: alice → bob",
		"/count: 1 → 2",
		"/title: old title → new title",
		"/nested: changed",
		"− /removed_prop",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("derived fallback missing %q:\n%s", want, html)
		}
	}
}

// TestObjectPreviewHistoryNoChangesNote pins the muted note when a version has
// neither a recorded summary nor any derivable difference from its predecessor
// (or has no predecessor at all).
func TestObjectPreviewHistoryNoChangesNote(t *testing.T) {
	const note = "No recorded changes for this version."

	same := []GraphObject{
		{ID: "2", Version: 2, Type: "BoardTask", Key: "k", Status: "ready"},
		{ID: "1", Version: 1, Type: "BoardTask", Key: "k", Status: "ready"},
	}
	if html := renderHTML(t, objectPreviewHistory(same, nil)); !strings.Contains(html, note) {
		t.Errorf("identical versions missing no-changes note:\n%s", html)
	}

	single := []GraphObject{
		{ID: "1", Version: 1, Type: "BoardTask", Key: "k", Status: "ready"},
	}
	if html := renderHTML(t, objectPreviewHistory(single, nil)); !strings.Contains(html, note) {
		t.Errorf("single version missing no-changes note:\n%s", html)
	}
}

// TestObjectPreviewChangesToleratesUnexpectedShape pins the defensive decode:
// a change_summary whose keys have unexpected types must not panic or blank the
// entry — it falls back to the derived diff from the preceding version.
func TestObjectPreviewChangesToleratesUnexpectedShape(t *testing.T) {
	versions := []GraphObject{
		{
			ID: "2", Version: 2, Type: "note", Key: "k", Status: "in_progress",
			ChangeSummary: map[string]any{
				"added":   "not-a-map",
				"removed": map[string]any{"unexpected": true},
				"updated": []any{"not-a-map"},
			},
		},
		{ID: "1", Version: 1, Type: "note", Key: "k", Status: "ready"},
	}
	html := renderHTML(t, objectPreviewHistory(versions, nil))
	if !strings.Contains(html, "status: ready → in_progress") {
		t.Errorf("unexpected change_summary shape should fall back to derivation:\n%s", html)
	}
}

// TestObjectPreviewHistoryLazySection pins the shared lazy-load contract: the
// inline History section renders an htmx load-triggered slot (spinner until
// swapped) scoped to /objects/:id/history/partial, plus a link to the full
// History subpage.
func TestObjectPreviewHistoryLazySection(t *testing.T) {
	obj := &GraphObject{ID: "abc", Type: "note", Key: "k", Status: "ready"}
	html := renderHTML(t, objectPreviewContent(obj, nil, "", objectPreviewSchema{}, nil))
	for _, want := range []string{
		`data-testid="object-preview-history-slot"`,
		`hx-get="/objects/abc/history/partial"`,
		`hx-trigger="load"`,
		`data-testid="object-preview-history-link"`,
		`href="/objects/abc/history"`,
		`loading loading-spinner`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("history lazy section missing %q:\n%s", want, html)
		}
	}
}

// TestObjectPreviewHistoryPartial pins the handler: GET
// /objects/:id/history/partial renders the timeline from the backend's version
// history.
func TestObjectPreviewHistoryPartial(t *testing.T) {
	f := &fakeMemory{
		objectHistory: []GraphObject{
			{ID: "w1", Version: 2, Type: "BoardTask", Key: "k", Status: "in_progress"},
			{ID: "w0", Version: 1, Type: "BoardTask", Key: "k", Status: "ready"},
		},
	}
	s := &Server{cfg: Config{}, memory: f}
	e := echo.New()
	e.GET("/objects/:id/history/partial", s.uiObjectHistoryPartial)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/objects/w1/history/partial", nil))
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
// successEnvelope), decoded into []GraphObject with per-version numbers, the
// assignee, and the per-version change_summary (which must survive decoding so
// the gateway can render it rather than silently dropping it).
func TestGetObjectHistoryClient(t *testing.T) {
	var gotMethod, gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"versions":[
			{"id":"22222222-2222-2222-2222-222222222222","version":2,"type":"note","key":"k","status":"in_progress","assignee":"bob","created_at":"2026-01-02T00:00:00Z","change_summary":{"added":{"/summary":"hello"},"removed":["/old"],"updated":{"/title":{"from":"a","to":"b"}}}},
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
	if versions[0].Assignee != "bob" {
		t.Errorf("versions[0].Assignee = %q, want %q", versions[0].Assignee, "bob")
	}
	if versions[1].Version != 1 || versions[1].Status != "ready" {
		t.Errorf("versions[1] = %+v, want v1 ready", versions[1])
	}

	// The per-version change_summary must survive decoding (it was silently
	// dropped client-side before GraphObject gained the field).
	cs := versions[0].ChangeSummary
	if cs == nil {
		t.Fatalf("versions[0].ChangeSummary = nil, want the server's change_summary")
	}
	if added, ok := cs["added"].(map[string]any); !ok || added["/summary"] != "hello" {
		t.Errorf("change_summary.added = %#v, want {/summary: hello}", cs["added"])
	}
	if removed, ok := cs["removed"].([]any); !ok || len(removed) != 1 || removed[0] != "/old" {
		t.Errorf("change_summary.removed = %#v, want [/old]", cs["removed"])
	}
	if updated, ok := cs["updated"].(map[string]any); !ok {
		t.Errorf("change_summary.updated = %#v, want a map", cs["updated"])
	} else if m, ok := updated["/title"].(map[string]any); !ok || m["from"] != "a" || m["to"] != "b" {
		t.Errorf("change_summary.updated[/title] = %#v, want {from:a to:b}", updated["/title"])
	}
	// The older version has no summary — the field is nil, not an empty map.
	if versions[1].ChangeSummary != nil {
		t.Errorf("versions[1].ChangeSummary = %#v, want nil", versions[1].ChangeSummary)
	}
}

// TestObjectPreviewWiring pins the integration points: the preview partial route
// is registered, and the app shell mounts the drawer + its script.
func TestObjectPreviewWiring(t *testing.T) {
	mainSrc, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"/objects/:id/preview"`, `s.uiObjectPreviewPartial`,
		`"/objects/:id/history"`, `s.uiObjectHistoryPage`,
		`"/objects/:id/history/partial"`, `s.uiObjectHistoryPartial`,
		`"/objects/:id/similar"`, `s.uiObjectSimilarPage`,
		`"/objects/:id/similar/partial"`, `s.uiObjectSimilarPartial`,
		`"/objects/:id/status"`, `s.uiObjectStatus`,
		`"/objects/:id/assignee"`, `s.uiObjectAssignee`,
	} {
		if !strings.Contains(string(mainSrc), want) {
			t.Errorf("main.go must register %q", want)
		}
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
// drawer opens only for references inside a chat message list (the inline
// assistant markdown and the in-bubble Sources footer both live there) — never
// for the .memory-md knowledge answer on the object page — and both htmx error
// events are handled. It also pins that no testid-keyed escape hatch exists:
// `citation-link` was one until #2251, but the sources block stopped emitting it
// in #1228, leaving a branch that could never run.
func TestObjectPreviewInterceptionScope(t *testing.T) {
	src, err := os.ReadFile("webui/static/js/object-preview.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(src)
	for _, want := range []string{
		`#chat-messages, #sidepanel-messages`,
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
	if strings.Contains(js, `getAttribute("data-testid")`) {
		t.Error("object-preview.js must not key interception on a data-testid: the sources block stopped emitting citation-link in #1228, so such a branch is unreachable (#2251)")
	}
}
