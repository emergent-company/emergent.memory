package components

import (
	"strings"
	"testing"

	"github.com/a-h/templ"
)

// StickyFormActions' contract is its pin position: the root must be sticky to
// the bottom of the page scroll area, so those classes (and the edge variant's
// bleed) ARE the component's API and are asserted. The bar's chrome (colours,
// blur, border) is deliberately not pinned — a theme tweak must not fail here.
func TestStickyFormActionsPinsToBottom(t *testing.T) {
	html := renderHTML(t, StickyFormActions(StickyFormActionsProps{}))
	assertContains(t, html, "sticky", "bottom-0")
}

func TestStickyFormActionsEdge(t *testing.T) {
	t.Run("inset does not bleed", func(t *testing.T) {
		html := renderHTML(t, StickyFormActions(StickyFormActionsProps{}))
		if strings.Contains(html, "-mx-6") {
			t.Fatalf("inset bar must not bleed to the container gutter: %s", html)
		}
	})

	t.Run("container bleeds to the container gutter", func(t *testing.T) {
		html := renderHTML(t, StickyFormActions(StickyFormActionsProps{Edge: "container"}))
		assertContains(t, html, "-mx-6", "-mb-6", "lg:-mx-8", "lg:-mb-8")
	})
}

func TestStickyFormActionsPassesAttrs(t *testing.T) {
	html := renderHTML(t, StickyFormActions(StickyFormActionsProps{
		Attrs: templ.Attributes{"data-testid": "sticky-bar"},
	}))
	assertContains(t, html, `data-testid="sticky-bar"`)
}
