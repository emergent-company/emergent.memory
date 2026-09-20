package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// This file pins two rendered-output structure changes that landed during the
// component-adoption refactor (PageHeading + ui.Disclosure). Neither change is
// covered by an existing test, so these regression tests lock the current
// markup so a future change cannot silently move it.

// --- Delta 1: agent dashboard header leading icon tile ---

// TestAgentDashboardHeaderLeadingTileStructure pins the structural position of
// the leading icon tile after the header migrated to nav.PageHeading's Leading
// slot: the tile now renders BEFORE the breadcrumbs and BEFORE the h1, on the
// outer wrapper, as a sibling preceding the flex title/actions row (not nested
// inside it). It also locks the Bare guard (leading tile dropped) and the tile
// markup itself.
func TestAgentDashboardHeaderLeadingTileStructure(t *testing.T) {
	declared := &AgentDefinition{
		ID: "a1", Name: "diane", Description: "household assistant",
		UIConfig: json.RawMessage(`{"icon":"database","color":"#2563EB"}`),
	}

	const (
		tileMark      = `grid shrink-0 place-items-center rounded-lg` // unique within the header
		crumbsMark    = `<div class="breadcrumbs text-sm"`
		h1Mark        = `<h1 class="text-2xl font-bold tracking-tight lg:text-3xl">`
		flexRowMark   = `<div class="mt-2 flex flex-wrap items-end justify-between gap-4">`
		neutralTile   = `class="grid shrink-0 place-items-center rounded-lg border bg-primary/10 text-primary border-primary/15 size-9"`
		accentTile    = `class="grid shrink-0 place-items-center rounded-lg border size-9"`
		accentStyle   = `style="color:#2563EB;background-color:color-mix(in oklch,#2563EB 10%,transparent);border-color:color-mix(in oklch,#2563EB 15%,transparent);"`
		neutralGlyph  = `class="iconify lucide--bot size-4.5" aria-hidden="true"`
		accentedGlyph = `class="iconify lucide--database size-4.5" aria-hidden="true"`
	)

	t.Run("declared appearance", func(t *testing.T) {
		html := renderHTML(t, agentDashboardHeader(declared, ""))

		tile := strings.Index(html, tileMark)
		crumbs := strings.Index(html, crumbsMark)
		h1 := strings.Index(html, h1Mark)
		flex := strings.Index(html, flexRowMark)
		if tile == -1 || crumbs == -1 || h1 == -1 || flex == -1 {
			t.Fatalf("header markers missing: tile=%d crumbs=%d h1=%d flex=%d", tile, crumbs, h1, flex)
		}

		// Tile leads the breadcrumbs and the h1 in document order.
		if tile > crumbs {
			t.Errorf("tile (%d) must precede breadcrumbs (%d)", tile, crumbs)
		}
		if tile > h1 {
			t.Errorf("tile (%d) must precede h1 (%d)", tile, h1)
		}
		// Tile is a sibling preceding the flex title/actions row, not nested
		// within it: it opens before the flex row's opening tag.
		if tile > flex {
			t.Errorf("tile (%d) must precede the flex title/actions row (%d)", tile, flex)
		}

		// Tile markup unchanged (accented: colour via inline style, no tone classes).
		for _, want := range []string{accentTile, accentStyle, accentedGlyph} {
			if !strings.Contains(html, want) {
				t.Errorf("accented header tile missing %q", want)
			}
		}
	})

	t.Run("neutral appearance", func(t *testing.T) {
		html := renderHTML(t, agentDashboardHeader(&AgentDefinition{ID: "a1", Name: "diane"}, ""))

		tile := strings.Index(html, tileMark)
		crumbs := strings.Index(html, crumbsMark)
		h1 := strings.Index(html, h1Mark)
		flex := strings.Index(html, flexRowMark)
		if tile == -1 || crumbs == -1 || h1 == -1 || flex == -1 {
			t.Fatalf("header markers missing: tile=%d crumbs=%d h1=%d flex=%d", tile, crumbs, h1, flex)
		}
		if tile > crumbs || tile > h1 || tile > flex {
			t.Errorf("neutral tile (%d) must lead crumbs (%d), h1 (%d), flex row (%d)", tile, crumbs, h1, flex)
		}

		// Tile markup unchanged (neutral: primary tone classes).
		for _, want := range []string{neutralTile, neutralGlyph} {
			if !strings.Contains(html, want) {
				t.Errorf("neutral header tile missing %q", want)
			}
		}
	})

	t.Run("bare drops leading tile", func(t *testing.T) {
		html := renderHTML(t, detailHeader(nil, "", "diane", "", detailHeaderOpts{
			Margin:    "mb-6",
			Dashboard: true,
			Bare:      true,
			Leading:   agentIconTile("database", "#2563EB"),
		}))
		// The Bare guard ignores Leading entirely — no tile icon/class survives.
		for _, bad := range []string{tileMark, accentedGlyph, neutralGlyph} {
			if strings.Contains(html, bad) {
				t.Errorf("bare header must drop the leading tile, found %q", bad)
			}
		}
	})
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
		html := renderHTML(t, agentToolCapabilityGroup(gv))

		// Exact summary class: alignment token items-start trails the base list.
		wantClass := base + ` items-start `
		if !strings.Contains(html, `class="`+wantClass+`"`) {
			t.Errorf("capability summary class missing %q", wantClass)
		}
		// The trailing alignment token must come after list-none (end of list),
		// never before it.
		if strings.Index(html, "items-start") < strings.Index(html, "list-none") {
			t.Error("items-start must trail list-none in the summary class")
		}

		// Summary + details attributes unchanged.
		for _, want := range []string{
			`data-testid="tool-group-header-cap"`,            // summary
			`data-testid="tool-group" data-tool-group="cap"`, // details
		} {
			if !strings.Contains(html, want) {
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
		html := renderHTML(t, agentToolGroup(p))

		wantClass := base + ` items-center `
		if !strings.Contains(html, `class="`+wantClass+`"`) {
			t.Errorf("source-group summary class missing %q", wantClass)
		}
		if strings.Index(html, "items-center") < strings.Index(html, "list-none") {
			t.Error("items-center must trail list-none in the summary class")
		}
	})
}
