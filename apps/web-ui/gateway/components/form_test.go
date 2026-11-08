package components

import (
	"testing"

	"github.com/a-h/templ"
)

// InlineFormActions is the in-form footer row (#486): the main actions sit on
// the right of the row by default, the same place the go-daisy form footer puts
// them and the same place StickyFormActions puts a page-terminal form's actions.
func TestInlineFormActionsAlignsMainActionsRight(t *testing.T) {
	html := renderHTML(t, InlineFormActions(InlineFormActionsProps{}))
	assertContains(t, html, "flex", "flex-wrap", "items-center", "gap-2", "justify-end")
	assertNotContains(t, html, "justify-start", "justify-between")
}

func TestInlineFormActionsAlignStart(t *testing.T) {
	html := renderHTML(t, InlineFormActions(InlineFormActionsProps{Align: "start"}))
	assertContains(t, html, "justify-start")
	assertNotContains(t, html, "justify-end")
}

func TestInlineFormActionsAlignBetween(t *testing.T) {
	html := renderHTML(t, InlineFormActions(InlineFormActionsProps{Align: "between"}))
	assertContains(t, html, "justify-between")
	assertNotContains(t, html, "justify-end")
}

func TestInlineFormActionsPassesClassAndAttrs(t *testing.T) {
	html := renderHTML(t, InlineFormActions(InlineFormActionsProps{
		Class: "mt-4",
		Attrs: templ.Attributes{"data-testid": "form-actions"},
	}))
	assertContains(t, html, "mt-4", `data-testid="form-actions"`)
}

// StickyFormActions' contract is its pin position and the three-level shape:
// the root carries the .sticky-form-actions class (whose CSS sets position:
// sticky / bottom: 0), the surface child carries the pinned chrome, and the
// innermost wrapper carries the content gutter (max-w-6xl + padding) with the
// actions row aligned by Align / offset by Rail. The chrome itself (colours,
// blur, border) is deliberately not pinned here — a theme tweak must not fail.
func TestStickyFormActionsShape(t *testing.T) {
	html := renderHTML(t, StickyFormActions(StickyFormActionsProps{}))
	assertContains(t, html, "sticky-form-actions", "sfa__surface", "max-w-6xl")
	// Default alignment right-aligns the action row.
	assertContains(t, html, "justify-end")
	// The inset variant never bleeds into the container gutter.
	assertNotContains(t, html, "-mx-6")
}

func TestStickyFormActionsAlignStart(t *testing.T) {
	html := renderHTML(t, StickyFormActions(StickyFormActionsProps{Align: "start"}))
	assertContains(t, html, "justify-start")
	assertNotContains(t, html, "justify-end")
}

func TestStickyFormActionsRail(t *testing.T) {
	html := renderHTML(t, StickyFormActions(StickyFormActionsProps{Rail: true}))
	// The rail variant mirrors layout.Rail's grid so the actions line up with
	// the rail's content column on lg.
	assertContains(t, html, "lg:grid-cols-[11rem_minmax(0,1fr)]")
}

func TestStickyFormActionsPassesAttrs(t *testing.T) {
	html := renderHTML(t, StickyFormActions(StickyFormActionsProps{
		Attrs: templ.Attributes{"data-testid": "sticky-bar"},
	}))
	assertContains(t, html, `data-testid="sticky-bar"`)
}
