package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// This file pins two rendered-output structure changes that landed during the
// component-adoption refactor (PageHeading + ui.Disclosure). Neither change is
// covered by an existing test, so these regression tests lock the current
// markup so a future change cannot silently move it.

// --- Delta 1: agent dashboard header leading icon tile ---

// Class tokens used to locate the tile and the flex title/actions row in the
// parsed DOM. Shared between the subtest body and checkHeaderDOM.
const (
	tileMark     = `grid shrink-0 place-items-center rounded-lg` // unique within the header
	flexRowClass = `mt-2 flex flex-wrap`                         // flex title/actions row
)

// TestAgentDashboardHeaderLeadingTileStructure pins the structural position of
// the agent icon tile after it moved inline: the tile is now a descendant of
// the flex title/actions row, on the same flex line as (and ordered before) the
// h1, rather than a direct child of the mb-6 wrapper above the breadcrumbs.
// checkHeaderDOM asserts the tile is not a direct wrapper child and shares the
// h1's inline-flex parent with document order tile < h1. The test also locks the
// Bare guard (leading tile dropped) and the tile markup itself.
func TestAgentDashboardHeaderLeadingTileStructure(t *testing.T) {
	declared := &AgentDefinition{
		ID: "a1", Name: "diane", Description: "household assistant",
		UIConfig: json.RawMessage(`{"icon":"database","color":"#2563EB"}`),
	}

	const (
		neutralTile   = `class="grid shrink-0 place-items-center rounded-lg border bg-primary/10 text-primary border-primary/15 size-9"`
		accentTile    = `class="grid shrink-0 place-items-center rounded-lg border size-9"`
		accentStyle   = `style="color:#2563EB;background-color:color-mix(in oklch,#2563EB 10%,transparent);border-color:color-mix(in oklch,#2563EB 15%,transparent);"`
		neutralGlyph  = `class="iconify lucide--bot size-4.5" aria-hidden="true"`
		accentedGlyph = `class="iconify lucide--database size-4.5" aria-hidden="true"`
	)

	t.Run("declared appearance", func(t *testing.T) {
		rendered := renderHTML(t, agentDashboardHeader(declared, ""))
		checkHeaderDOM(t, parseDOM(t, rendered))

		// Tile markup unchanged (accented: colour via inline style, no tone classes).
		for _, want := range []string{accentTile, accentStyle, accentedGlyph} {
			if !strings.Contains(rendered, want) {
				t.Errorf("accented header tile missing %q", want)
			}
		}
	})

	t.Run("neutral appearance", func(t *testing.T) {
		rendered := renderHTML(t, agentDashboardHeader(&AgentDefinition{ID: "a1", Name: "diane"}, ""))
		checkHeaderDOM(t, parseDOM(t, rendered))

		// Tile markup unchanged (neutral: primary tone classes).
		for _, want := range []string{neutralTile, neutralGlyph} {
			if !strings.Contains(rendered, want) {
				t.Errorf("neutral header tile missing %q", want)
			}
		}
	})

	t.Run("bare drops leading tile", func(t *testing.T) {
		rendered := renderHTML(t, detailHeader(nil, "", "diane", "", detailHeaderOpts{
			Margin:       "mb-6",
			Dashboard:    true,
			Bare:         true,
			TitleLeading: agentIconTile("database", "#2563EB"),
		}))
		// The Bare guard ignores TitleLeading entirely — no tile icon/class survives.
		if findElementByClass(parseDOM(t, rendered), strings.Fields(tileMark)...) != nil {
			t.Error("bare header must drop the leading tile element")
		}
		for _, bad := range []string{tileMark, accentedGlyph, neutralGlyph} {
			if strings.Contains(rendered, bad) {
				t.Errorf("bare header must drop the leading tile, found %q", bad)
			}
		}
	})
}

// checkHeaderDOM asserts the DOM-structure invariants shared by the declared
// and neutral header variants: the breadcrumbs and the flex title/actions row
// are direct children of the mb-6 wrapper, the icon tile now lives INSIDE the
// flex row on the same inline flex line as the h1, and the tile is ordered
// before the h1 (inline leading position, not above the breadcrumbs).
func checkHeaderDOM(t *testing.T, doc *html.Node) {
	t.Helper()

	wrapper := findElementByClass(doc, "mb-6")
	tile := findElementByClass(doc, strings.Fields(tileMark)...)
	crumbs := findElementByClass(doc, "breadcrumbs")
	flex := findElementByClass(doc, strings.Fields(flexRowClass)...)
	h1 := findElementByTag(doc, "h1")

	if wrapper == nil || tile == nil || crumbs == nil || flex == nil || h1 == nil {
		t.Fatalf("header DOM missing elements: wrapper=%v tile=%v crumbs=%v flex=%v h1=%v",
			wrapper != nil, tile != nil, crumbs != nil, flex != nil, h1 != nil)
	}

	// Breadcrumbs and the flex title/actions row stay direct children of the
	// outer wrapper, in that order.
	if crumbs.Parent != wrapper {
		t.Errorf("breadcrumbs parent = %s, want outer wrapper", nodeDesc(crumbs.Parent))
	}
	if flex.Parent != wrapper {
		t.Errorf("flex title/actions row parent = %s, want outer wrapper", nodeDesc(flex.Parent))
	}
	if ci, fi := childIndexOf(wrapper, crumbs), childIndexOf(wrapper, flex); ci < 0 || ci >= fi {
		t.Errorf("wrapper child order wrong: crumbs=%d flex=%d, want crumbs < flex", ci, fi)
	}

	// Core fix: the tile moved from a direct wrapper child (above the
	// breadcrumbs) to an inline member of the flex title/actions row.
	if tile.Parent == wrapper {
		t.Error("icon tile must not be a direct child of the outer wrapper (it belongs inline in the flex row)")
	}
	if !isDescendantOf(tile, flex) {
		t.Error("icon tile must be a descendant of the flex title/actions row")
	}
	if !isDescendantOf(h1, flex) {
		t.Error("h1 must be a descendant of the flex title/actions row")
	}

	// Inline placement: the tile and the h1 share the title's inline flex line,
	// with the tile first in document order.
	if tile.Parent != h1.Parent {
		t.Errorf("icon tile and h1 must share the inline flex line parent: tile parent=%s h1 parent=%s",
			nodeDesc(tile.Parent), nodeDesc(h1.Parent))
	}
	if ti, hi := childIndexOf(tile.Parent, tile), childIndexOf(h1.Parent, h1); ti < 0 || ti >= hi {
		t.Errorf("inline order wrong: tile=%d h1=%d, want tile < h1", ti, hi)
	}
}

// --- Delta 2: agent tool-group disclosure summary class ordering ---

// TestAgentToolGroupDisclosureSummaryClass pins the <summary> class string for
// the two agent tool-group disclosure variants after the move onto
// ui.Disclosure. The alignment token (items-start / items-center) is now
// emitted at the END of the class list, after the base list-none/… token.
func TestAgentToolGroupDisclosureSummaryClass(t *testing.T) {
	const (
		// base summary classes, with the webkit-details-marker selector
		// HTML-escaped exactly as templ emits it
		base = `flex cursor-pointer list-none justify-between gap-3 px-3 py-2.5 select-none [&amp;::-webkit-details-marker]:hidden`
	)

	t.Run("capability group uses items-start", func(t *testing.T) {
		gv := toolGroupView{
			Group:   ToolGroup{ID: "cap", Label: "Capability", Description: "desc here"},
			Enabled: true,
			Open:    true,
			Count:   1,
			Rows:    []agentToolRow{{Name: "native-tool", Checked: true}},
		}
		rendered := renderHTML(t, agentToolCapabilityGroup(gv))

		summary := findElementByClass(parseDOM(t, rendered), "list-none")
		if summary == nil || summary.Data != "summary" {
			t.Fatalf("summary element with list-none class not found (got %v)", summary)
		}
		tokens := strings.Fields(classAttr(summary))
		if len(tokens) == 0 || tokens[len(tokens)-1] != "items-start" {
			t.Errorf("items-start must be the last summary class token, got %q", classAttr(summary))
		}

		// Exact summary class: alignment token items-start trails the base list.
		wantClass := base + ` items-start `
		if !strings.Contains(rendered, `class="`+wantClass+`"`) {
			t.Errorf("capability summary class missing %q", wantClass)
		}

		// Summary + details attributes unchanged.
		for _, want := range []string{
			`data-testid="tool-group-header-cap"`,            // summary
			`data-testid="tool-group" data-tool-group="cap"`, // details
		} {
			if !strings.Contains(rendered, want) {
				t.Errorf("capability group missing %q", want)
			}
		}
	})

	t.Run("source group uses items-center", func(t *testing.T) {
		p := agentToolGroupProps{
			BorderClass:     "border-base-content/10 bg-base-100/60",
			BodyBorderClass: "border-base-content/10",
			Icon:            "lucide--server",
			IconClass:       "text-base-content/40",
			Title:           "builtin",
			Count:           1,
			Open:            true,
			Tools:           []agentToolRow{{Name: "web_search", Checked: true}},
		}
		rendered := renderHTML(t, agentToolGroup(p))

		summary := findElementByClass(parseDOM(t, rendered), "list-none")
		if summary == nil || summary.Data != "summary" {
			t.Fatalf("summary element with list-none class not found (got %v)", summary)
		}
		tokens := strings.Fields(classAttr(summary))
		if len(tokens) == 0 || tokens[len(tokens)-1] != "items-center" {
			t.Errorf("items-center must be the last summary class token, got %q", classAttr(summary))
		}

		wantClass := base + ` items-center `
		if !strings.Contains(rendered, `class="`+wantClass+`"`) {
			t.Errorf("source-group summary class missing %q", wantClass)
		}
	})
}

// --- DOM helpers (golang.org/x/net/html node walking) ---

// parseDOM parses an HTML fragment into a *html.Node document tree.
func parseDOM(t *testing.T, s string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(s))
	if err != nil {
		t.Fatalf("parse html: %v", err)
	}
	return doc
}

// classAttr returns the value of the node's class attribute, or "".
func classAttr(n *html.Node) string {
	for _, a := range n.Attr {
		if a.Key == "class" {
			return a.Val
		}
	}
	return ""
}

// hasClasses reports whether n is an element whose class attribute contains
// every one of the given tokens.
func hasClasses(n *html.Node, classes ...string) bool {
	if n.Type != html.ElementNode {
		return false
	}
	tokens := strings.Fields(classAttr(n))
	for _, want := range classes {
		if !slices.Contains(tokens, want) {
			return false
		}
	}
	return true
}

// findElementByClass returns the first element (depth-first) whose class
// attribute contains every given token, or nil.
func findElementByClass(n *html.Node, classes ...string) *html.Node {
	if hasClasses(n, classes...) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findElementByClass(c, classes...); found != nil {
			return found
		}
	}
	return nil
}

// findElementByTag returns the first element (depth-first) with the given tag
// name, or nil.
func findElementByTag(n *html.Node, tag string) *html.Node {
	if n.Type == html.ElementNode && n.Data == tag {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findElementByTag(c, tag); found != nil {
			return found
		}
	}
	return nil
}

// childIndexOf returns the index of child among parent's element children, or
// -1 if child is not an element child of parent.
func childIndexOf(parent, child *html.Node) int {
	idx := 0
	for c := parent.FirstChild; c != nil; c = c.NextSibling {
		if c == child {
			return idx
		}
		if c.Type == html.ElementNode {
			idx++
		}
	}
	return -1
}

// isDescendantOf reports whether node is a descendant of ancestor (node is
// reachable from ancestor via Parent links, exclusive of ancestor itself).
func isDescendantOf(node, ancestor *html.Node) bool {
	for n := node.Parent; n != nil; n = n.Parent {
		if n == ancestor {
			return true
		}
	}
	return false
}

// nodeDesc renders a compact, human-readable description of a node for error
// messages.
func nodeDesc(n *html.Node) string {
	if n == nil {
		return "nil"
	}
	if n.Type == html.ElementNode {
		if cls := classAttr(n); cls != "" {
			return "<" + n.Data + ` class="` + cls + `">`
		}
		return "<" + n.Data + ">"
	}
	return n.Data
}
