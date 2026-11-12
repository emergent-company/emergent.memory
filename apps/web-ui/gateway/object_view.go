package main

import (
	"sort"
	"strings"
)

// Rendering an object page from its type's declared view.
//
// The resolver turns an object, its edges and the schema into the page's
// relationship groups: which edges belong to each declared section, in what
// order, how many rows, and which four facts a depth-1 preview of each
// neighbour shows. It is pure — every input is already fetched — so the same
// function backs the web page today and the JSON card (and any future surface)
// without a second rendering path.
//
// Two rules keep it honest against real data:
//
//   - Edges are matched on either endpoint, because most of them point AT the
//     object (a property is "concerned" by its documents, "served" by its
//     utilities). A renderer that only walked outgoing edges would show one row
//     where a real object has sixteen.
//   - Every group is bounded. A view is authored data, and a project with a
//     187-edge hub must not render a 187-row page just because an author wrote
//     no limit.

// viewFactMaxRunes bounds one preview fact, so a long `notes` value cannot
// turn a one-line preview into a paragraph.
const viewFactMaxRunes = 60

// viewMaxFactsPerRow is the preview depth-1 budget: a row shows at most four
// facts and never previews its neighbour's neighbours.
const viewMaxFactsPerRow = 4

// viewDefaultFactProps are property names that make a poor preview fact because
// the row already shows them (the label) or they are prose/links (too long to
// scan). Everything else is a candidate.
var viewDefaultFactProps = map[string]bool{
	"name":        true,
	"title":       true,
	"label":       true,
	"notes":       true,
	"description": true,
	"summary":     true,
	"url":         true,
}

// objectViewFact is one label/value pair of a preview.
type objectViewFact struct {
	Label string
	Value string
}

// objectViewItem is one neighbour of the object inside a section, with the
// preview facts the row renders.
type objectViewItem struct {
	// RelID and RelType identify the edge, so a client can deep-link or act on
	// it without re-deriving which edge a row came from.
	RelID   string
	RelType string
	// Neighbour is the object on the other end of the edge. Resolved reports
	// whether it was fetched: an unresolved neighbour renders as a plain
	// reference rather than a link to a page that may not exist.
	Neighbour GraphObject
	Resolved  bool
	// Facts is the depth-1 preview: at most four short label/value pairs.
	Facts []objectViewFact
	// incoming records which endpoint of the edge the object is, so a declared
	// section can filter on direction.
	incoming bool
}

// direction is the row's edge direction relative to the object: "in" when the
// edge points at it, "out" when it leaves.
func (r objectViewItem) direction() string {
	if r.incoming {
		return "in"
	}
	return "out"
}

// objectViewSection is one rendered group. Total is the row count before
// truncation, so the page can say how many it is not showing.
type objectViewSection struct {
	Title string
	Rows  []objectViewItem
	Total int
}

// resolveObjectViewSections builds the page's relationship groups from the
// type's declared view. It returns nil when the type declares no view, which is
// the caller's signal to keep the generic list.
func resolveObjectViewSections(view *typeUIView, obj *GraphObject, edges []GraphRelationship, related []GraphObject, typeUIByType map[string]typeUI) []objectViewSection {
	if view == nil || obj == nil || len(edges) == 0 {
		return nil
	}

	// used records every edge a declared section rendered, so the trailing
	// "more" group only offers what no section claimed.
	used := make(map[string]bool, len(edges))
	out := make([]objectViewSection, 0, len(view.Sections)+1)

	for _, section := range view.Sections {
		rows := make([]objectViewItem, 0, len(edges))
		for _, edge := range edges {
			if !section.matches(edge.Type) {
				continue
			}
			row, ok := objectViewItemFor(edge, obj, related, typeUIByType)
			if !ok {
				continue
			}
			// A section declares the direction it reads, because an edge
			// pointing at the object and one leaving it mean different things.
			if dir := section.direction(); dir != "both" && dir != row.direction() {
				continue
			}
			used[edge.ID] = true
			rows = append(rows, row)
		}
		if len(rows) == 0 {
			// A declared section with no rows stays hidden: an empty "Utilities"
			// card is noise on a page that has nineteen other things to say.
			continue
		}
		out = append(out, objectViewSection{
			Title: section.Title,
			Rows:  sortViewRows(rows, section.rowLimit()),
			Total: len(rows),
		})
	}

	if view.More {
		rest := make([]objectViewItem, 0, len(edges))
		for _, edge := range edges {
			if used[edge.ID] {
				continue
			}
			row, ok := objectViewItemFor(edge, obj, related, typeUIByType)
			if !ok {
				continue
			}
			rest = append(rest, row)
		}
		if len(rest) > 0 {
			out = append(out, objectViewSection{
				Title: "More connections",
				Rows:  sortViewRows(rest, viewDefaultLimit),
				Total: len(rest),
			})
		}
	}

	return out
}

// objectViewItemFor resolves the far endpoint of one edge and its preview facts.
// It reports false when the edge does not actually touch obj, which is how a
// caller-supplied edge list that mentions another object is ignored rather than
// rendered as a self-reference.
func objectViewItemFor(edge GraphRelationship, obj *GraphObject, related []GraphObject, typeUIByType map[string]typeUI) (objectViewItem, bool) {
	neighbourID, incoming, ok := objectViewNeighbour(edge, obj)
	if !ok {
		return objectViewItem{}, false
	}
	neighbour, resolved := relationshipEndpointObject(obj, related, neighbourID)
	row := objectViewItem{
		RelID:     edge.ID,
		RelType:   edge.Type,
		Neighbour: neighbour,
		Resolved:  resolved,
		incoming:  incoming,
	}
	if resolved {
		row.Facts = objectViewFacts(neighbour, typeUIByType)
	}
	return row, true
}

// objectViewNeighbour returns the id on the far side of an edge and whether the
// edge points at obj. Relationships are stored against canonical ids, so a
// version row (any object re-written after creation) matches on CanonicalID.
func objectViewNeighbour(edge GraphRelationship, obj *GraphObject) (string, bool, bool) {
	switch {
	case edge.SrcID == obj.ID || (obj.CanonicalID != "" && edge.SrcID == obj.CanonicalID):
		if edge.DstID == "" {
			return "", false, false
		}
		return edge.DstID, false, true
	case edge.DstID == obj.ID || (obj.CanonicalID != "" && edge.DstID == obj.CanonicalID):
		if edge.SrcID == "" {
			return "", false, false
		}
		return edge.SrcID, true, true
	default:
		return "", false, false
	}
}

// sortViewRows orders rows by neighbour label so the same page renders the same
// way twice, then truncates to limit. An unresolved neighbour keeps its id as
// the sort key, which is stable.
func sortViewRows(rows []objectViewItem, limit int) []objectViewItem {
	sort.SliceStable(rows, func(i, j int) bool {
		li, lj := strings.ToLower(graphObjectLabel(rows[i].Neighbour)), strings.ToLower(graphObjectLabel(rows[j].Neighbour))
		if li != lj {
			return li < lj
		}
		return rows[i].Neighbour.ID < rows[j].Neighbour.ID
	})
	if limit > 0 && len(rows) > limit {
		return rows[:limit]
	}
	return rows
}

// objectViewFacts selects the depth-1 preview facts for a neighbour. A type
// that declares `ui.view.inline` chooses its own; everything else falls back to
// status plus the first scalar properties in key order, which is deterministic
// and never shows prose or a URL truncated mid-token.
func objectViewFacts(o GraphObject, typeUIByType map[string]typeUI) []objectViewFact {
	out := make([]objectViewFact, 0, viewMaxFactsPerRow)
	if view := typeUIOf(typeUIByType, o.Type).View; view != nil && len(view.Inline) > 0 {
		for _, name := range view.Inline {
			if len(out) == viewMaxFactsPerRow {
				break
			}
			value := objectViewFactValue(o.Properties[name])
			if value == "" {
				continue
			}
			out = append(out, objectViewFact{Label: objectPreviewFieldLabel(name), Value: value})
		}
		return out
	}

	if status := strings.TrimSpace(o.Status); status != "" {
		out = append(out, objectViewFact{Label: "Status", Value: truncateRunes(status, viewFactMaxRunes)})
	}
	// The status column is shown first when it is set; the same value often
	// also sits in properties (it is what the editor writes), and rendering it
	// twice wastes one of the four fact slots.
	statusShown := strings.TrimSpace(o.Status) != ""
	keys := make([]string, 0, len(o.Properties))
	for key := range o.Properties {
		if strings.HasPrefix(key, "_") || viewDefaultFactProps[key] {
			continue
		}
		if key == "status" && statusShown {
			continue
		}
		if !objectPreviewScalar(o.Properties[key]) {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if len(out) == viewMaxFactsPerRow {
			break
		}
		value := objectViewFactValue(o.Properties[key])
		if value == "" {
			continue
		}
		out = append(out, objectViewFact{Label: objectPreviewFieldLabel(key), Value: value})
	}
	return out
}

// objectViewFactValue renders one property value for a preview row, truncated
// so a long string cannot break the row's one-line rhythm.
func objectViewFactValue(v any) string {
	return truncateRunes(strings.TrimSpace(objectPreviewValue(v)), viewFactMaxRunes)
}
