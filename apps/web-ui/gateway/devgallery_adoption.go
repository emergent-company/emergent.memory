package main

import "strings"

// devgallery_adoption.go holds the runtime display layer for the adoption-gap
// section of the component gallery. Like devgallery_ui.go it deliberately
// contains no templ components and no render call sites: the component graph is
// generated from render topology (cmd/componentgraph), so keeping the gallery's
// component structure here would churn the generated graph, which is build
// output (gitignored), not a checked-in artifact. Everything in this file is
// plain data shaping over the generated devgallery_adoption_gen.go data.

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
// regenerating the derived data file.
//
// Every entry names the upstream issue that unblocks it (ui-rule-upstream-first).
// All six currently point at the same go-daisy issue, which carries the three
// gaps they reduce to: an arbitrary-class hook on Badge and Alert, an Alert role
// that is not hardcoded to "alert", and a <form> element on CardRaw. Verified
// against the pinned go-daisy v0.20.0: none of the three props exists yet, so
// deleting an exemption today would break the page it covers. When the upstream
// props land, delete the entries they unblock — this list only ever shrinks.
//
// Enforcement ("an entry without an issue reference fails a check") is not
// implemented here: the ui-patterns pack still marks that clause "planned", so
// nothing can fail yet, and the issue tracking the enforcement lives in the
// rig's own repository rather than this one, which means there is no link to
// record in this file. Until that lands, the comment above each entry is the
// only record of why a page is exempt and what would unblock it.
var adoptionAllowlist = map[string][]string{
	// link-styled ghost badges: ui.Badge has Href and DirectLabel, but no Class prop, so the
	// hover:opacity-70 affordance cannot be expressed
	// upstream: https://github.com/emergent-company/go-daisy/issues/150
	"runs.templ": {"badge"},
	// chat.js reveals the banner by toggling the hidden class on #chat-model-warning; ui.Alert has
	// a Body slot but no Class prop, so a client-side hidden toggle cannot be expressed
	// upstream: https://github.com/emergent-company/go-daisy/issues/150
	"chat.templ": {"alert"},
	// the inline terminal is role=status, not role=alert, and needs its own
	// share-inline-terminal/items-start/gap-3/hidden classes; ui.Alert hardcodes role=alert and has
	// no Class prop
	// upstream: https://github.com/emergent-company/go-daisy/issues/150
	"share_page.templ": {"alert"},
	// <form class="card border border-line">; ui.CardRaw has no <form> element variant
	// (As: div/a/button/section/article/li), so the form element itself cannot be the card
	// upstream: https://github.com/emergent-company/go-daisy/issues/150
	"share_manage.templ": {"card"},
	// tab count badge needs tabular-nums so the digits do not jitter when a live refresh changes the
	// count; ui.Badge has DirectLabel for the direct-text count but no Class prop
	// upstream: https://github.com/emergent-company/go-daisy/issues/150
	"inbox.templ": {"badge"},
	// unread indicator badge: ui.Badge has Position, DirectLabel and Attrs for indicator-item
	// positioning and the data-notifications-badge hook, but no Class prop for tabular-nums
	// upstream: https://github.com/emergent-company/go-daisy/issues/150
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
