package main

import "strings"

// devgallery_adoption.go holds the runtime display layer for the adoption-gap
// section of the component gallery. Like devgallery_ui.go it deliberately
// contains no templ components and no render call sites: the component graph is
// generated from render topology (cmd/componentgraph), so keeping the gallery's
// component structure here would churn the generated graph. Everything in this
// file is plain data shaping over the committed devgallery_adoption_gen.go data.

// AdoptionViewSummary aggregates the adoption findings for one authored .templ
// source file.
type AdoptionViewSummary struct {
	File     string
	Count    int
	Findings []AdoptionFinding
}

// adoptionAllowlist is the manual escape hatch for the adoption detector,
// mirroring GalleryEntry.AdditionalUses: keyed by .templ basename, it lists
// markers (bare class names or element tags, e.g. "badge" or "dialog") whose
// findings should be suppressed for that file. It is applied at render time, so
// an author can exempt a file's known, acceptable ad-hoc markup without
// regenerating the committed data file.
var adoptionAllowlist = map[string][]string{
	// link-styled badges (<a class="badge" href>); ui.Badge renders a <span> with no href
	"runs.templ": {"badge"},
	// rich children (icon + text + button); ui.Alert has no children slot
	"chat.templ": {"alert"},
	// share-inline-terminal rich children; ui.Alert has no children slot
	"share_page.templ": {"alert"},
	// <form class="card card-border">; ui.CardRaw as= supports only "a"
	"share_manage.templ": {"card", "card-border"},
	// border-only row (no card/card-body); ui.CardRaw always emits card bg-base-100 + card-body
	"documents.templ": {"card-border"},
	// tab count badge renders the count as direct text (pinned by TestInboxPageTabsAndCounts);
	// ui.Badge wraps the label in a nested <span>, which changes that markup
	"inbox.templ": {"badge"},
	// unread indicator badge: indicator-item positioning classes + direct-text count that
	// ui.Badge cannot reproduce (it always emits gap-1 and nests the label in a <span>)
	"notifications_bell.templ": {"badge"},
}

// adoptionGaps aggregates the generated adoptionFindings by file, in the
// findings' first-seen (File, Line) order, dropping findings the allowlist
// exempts. The result is stable: findings are already sorted by
// (File, Line, Marker) in the generated data file.
func adoptionGaps() []AdoptionViewSummary {
	allow := map[string]map[string]bool{}
	for file, markers := range adoptionAllowlist {
		set := make(map[string]bool, len(markers))
		for _, m := range markers {
			set[m] = true
		}
		allow[file] = set
	}

	var order []string
	byFile := map[string][]AdoptionFinding{}
	for _, f := range adoptionFindings {
		if markers := allow[f.File]; markers[markerName(f.Marker)] || markers[f.Marker] {
			continue
		}
		if _, seen := byFile[f.File]; !seen {
			order = append(order, f.File)
		}
		byFile[f.File] = append(byFile[f.File], f)
	}

	gaps := make([]AdoptionViewSummary, 0, len(order))
	for _, file := range order {
		gaps = append(gaps, AdoptionViewSummary{File: file, Count: len(byFile[file]), Findings: byFile[file]})
	}
	return gaps
}

// adoptionOwnerLabel renders a finding's owner components as a human-readable
// list.
func adoptionOwnerLabel(owners []string) string {
	return strings.Join(owners, ", ")
}

// adoptionFindingCount returns the total number of findings across the gap
// summaries.
func adoptionFindingCount(gaps []AdoptionViewSummary) int {
	total := 0
	for _, g := range gaps {
		total += g.Count
	}
	return total
}

// adoptionTrackedComponents returns the number of shared components whose
// markers the detector tracks (the keys of the generated signature map).
func adoptionTrackedComponents() int {
	return len(adoptionSignatures)
}

// markerName returns a marker's bare name: the class or tag name without its
// "class:"/"tag:" prefix. It is the runtime counterpart of the scanner's helper.
func markerName(marker string) string {
	if n, ok := strings.CutPrefix(marker, "class:"); ok {
		return n
	}
	if n, ok := strings.CutPrefix(marker, "tag:"); ok {
		return n
	}
	return marker
}
