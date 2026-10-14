package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/labstack/echo/v4"
)

// uiObjectPreviewPartial renders the read-only object summary swapped into the
// global preview drawer (#object-preview-body) when a chat object reference is
// clicked. It reuses the same data seam as the object detail page: GetGraphObject
// supplies the object (including its stored Properties) and GetCompiledTypes
// supplies the declared per-type ui accent so the header icon matches the rest
// of the UI. Failures degrade to a small "unavailable" state rather than
// leaving the drawer blank. The optional ?rel= fragment carries the
// relationship id a `/objects/<src>#relationship-<rel>` reference pointed at,
// which is humanised into a subtle "Referenced via …" line.
func (s *Server) uiObjectPreviewPartial(c echo.Context) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), optionalFetchTimeout)
	defer cancel()

	id := c.Param("id")
	relID := c.QueryParam("rel")

	obj, err := s.memory.GetGraphObject(ctx, id)
	if err != nil {
		captureError(err)
		renderPartial(c.Response().Writer, c.Request(), objectPreviewNotFound())
		return nil
	}

	var typeUIByType map[string]typeUI
	compiled, compiledErr := s.memory.GetCompiledTypes(ctx)
	captureError(compiledErr)
	if compiled != nil {
		typeUIByType = objectTypeUIMap(compiled.ObjectTypes)
	}

	// ?actions=board renders the board's status-gated work-item actions into the
	// preview's action slot so the drawer is a usable surface, not just a
	// summary. Chat previews carry no actions param and stay read-only. A failed
	// work-item fetch leaves the slot empty — intentionally not captured, so a
	// missing work item never degrades the object summary.
	var actions templ.Component
	if c.QueryParam("actions") == "board" {
		if detail, werr := s.memory.GetWorkItem(ctx, id); werr == nil && detail != nil && detail.Item != nil {
			actions = boardDrawerActions(detail.Item, s.boardStatusMap(ctx))
		}
	}

	renderPartial(c.Response().Writer, c.Request(),
		objectPreviewContent(obj, typeUIByType, s.objectPreviewRelationshipLabel(ctx, id, relID), actions))
	return nil
}

// uiObjectHistoryPartial renders the lazy-loaded version timeline for the
// preview drawer's History section. GetObjectHistory returns versions newest
// first (the server orders by version DESC), so the timeline is rendered in the
// order received. A failed fetch degrades to a small "history unavailable"
// state rather than leaving the section blank.
func (s *Server) uiObjectHistoryPartial(c echo.Context) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), optionalFetchTimeout)
	defer cancel()

	id := c.Param("id")
	versions, err := s.memory.GetObjectHistory(ctx, id)
	if err != nil {
		captureError(err)
		renderPartial(c.Response().Writer, c.Request(), objectPreviewHistoryError())
		return nil
	}
	renderPartial(c.Response().Writer, c.Request(), objectPreviewHistory(versions))
	return nil
}

// objectPreviewRelationshipLabel resolves a relationship id carried by a chat
// reference to its humanised relationship type, or "" when there is no
// relationship context or it cannot be resolved. Best-effort: a failed edge
// fetch just omits the context line rather than degrading the whole preview.
func (s *Server) objectPreviewRelationshipLabel(ctx context.Context, objectID, relID string) string {
	if relID == "" {
		return ""
	}
	edges, err := s.memory.GetObjectEdges(ctx, objectID)
	if err != nil {
		captureError(err)
		return ""
	}
	for _, e := range edges {
		if e.ID == relID {
			return relationshipTypeLabel(e.Type)
		}
	}
	return ""
}

// objectPreviewProperties lists an object's display properties in key order.
// Unlike graphObjectProperties (which drops empties for the compact list
// render), every non-internal property is returned — including empty ones — so
// the preview shows the full shape of the object. Internal (underscore) names
// are dropped, and so is the mirrored "status" key: the server writes
// properties["status"] alongside the status column, but status is already
// rendered once in the header badge, so repeating it as a property row would
// duplicate it. The deferred "hide empties" refinement can filter rows by their
// data-empty marker without changing this.
func objectPreviewProperties(o GraphObject) []propertyKV {
	keys := make([]string, 0, len(o.Properties))
	for k := range o.Properties {
		if strings.HasPrefix(k, "_") || k == "status" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]propertyKV, 0, len(keys))
	for _, k := range keys {
		out = append(out, propertyKV{Key: k, Value: objectPreviewValue(o.Properties[k])})
	}
	return out
}

// objectPreviewValue renders a stored property value for read-only display.
// Lists join with commas; non-scalars fall back to their JSON form (reusing
// propValues, the same conversion the edit form uses).
func objectPreviewValue(v any) string {
	return strings.Join(propValues(v), ", ")
}

// objectPreviewCanonicalID resolves the identifier the copy control yields: the
// canonical id when present, else the version row id. The version row id is
// NOT the canonical id — a copy must yield the stable canonical identifier, not
// the specific version that happened to be previewed.
func objectPreviewCanonicalID(o GraphObject) string {
	return cmp.Or(o.CanonicalID, o.ID)
}

// objectPreviewCreated formats an object's creation timestamp for the preview's
// history timeline: relative time when parseable, otherwise the raw value, else "".
func objectPreviewCreated(iso string) string {
	if iso == "" {
		return ""
	}
	return relTime(iso)
}

// objectPreviewVersionLabel formats a version number for the history timeline.
func objectPreviewVersionLabel(version int) string {
	return "v" + strconv.Itoa(version)
}

// objectChange is one line in a history entry's expandable change list. Kind
// drives the glyph/format:
//   - "add":     "+ <label>" — a property path was added
//   - "remove":  "− <label>" — a property path was removed
//   - "update":  "<label>: <from> → <to>"
//   - "changed": "<label>: changed" — nested/complex value, no from/to
//   - "note":    muted informational line (merge provenance, or the no-changes note)
type objectChange struct {
	Kind  string
	Label string
	From  string
	To    string
}

// objectPreviewPrevious returns the immediately-preceding version for index i in
// a version-DESC slice (the next element), or nil when i is the last entry.
func objectPreviewPrevious(versions []GraphObject, i int) *GraphObject {
	if i < 0 || i >= len(versions)-1 {
		return nil
	}
	return &versions[i+1]
}

// objectPreviewChanges assembles the change list for one history entry. A
// recorded change_summary wins (added/removed/updated, plus merge provenance);
// when it is absent the list is derived from the preceding version (status,
// assignee, and top-level property diffs); when nothing is derivable a muted
// "no recorded changes" note is returned so the expanded row is never blank.
func objectPreviewChanges(v GraphObject, prev *GraphObject) []objectChange {
	if cs := objectPreviewChangesFromSummary(v.ChangeSummary); len(cs) > 0 {
		return cs
	}
	if prev != nil {
		if d := objectPreviewChangesDerived(v, *prev); len(d) > 0 {
			return d
		}
	}
	return []objectChange{{Kind: "note", Label: "No recorded changes for this version."}}
}

// objectPreviewChangesFromSummary decodes a server change_summary into change
// lines. It reads only the keys it understands and ignores the rest (meta,
// paths, and anything an unexpected writer added), so an unexpected shape can
// never panic or blank the entry.
func objectPreviewChangesFromSummary(cs map[string]any) []objectChange {
	if len(cs) == 0 {
		return nil
	}
	var out []objectChange

	if added, ok := cs["added"].(map[string]any); ok && len(added) > 0 {
		paths := sortedKeys(added)
		for _, path := range paths {
			out = append(out, objectChange{Kind: "add", Label: path})
		}
	}
	if removed, ok := cs["removed"].([]any); ok && len(removed) > 0 {
		paths := make([]string, 0, len(removed))
		for _, p := range removed {
			if path, ok := p.(string); ok {
				paths = append(paths, path)
			}
		}
		slices.Sort(paths)
		for _, path := range paths {
			out = append(out, objectChange{Kind: "remove", Label: path})
		}
	}
	if updated, ok := cs["updated"].(map[string]any); ok && len(updated) > 0 {
		paths := sortedKeys(updated)
		for _, path := range paths {
			from, to := "", ""
			if m, ok := updated[path].(map[string]any); ok {
				from = objectPreviewChangeValue(m["from"])
				to = objectPreviewChangeValue(m["to"])
			}
			out = append(out, objectChange{Kind: "update", Label: path, From: from, To: to})
		}
	}

	// Merge provenance (source/strategy/resolution/enriched_keys) rides on the
	// same summary but carries no added/removed/updated — surface it as muted
	// notes so a merge version still reads as a change, not "nothing".
	for _, kv := range []struct {
		key, prefix string
	}{
		{"source", "source: "},
		{"strategy", "strategy: "},
		{"resolution", "resolution: "},
	} {
		if v, ok := cs[kv.key].(string); ok && v != "" {
			out = append(out, objectChange{Kind: "note", Label: kv.prefix + v})
		}
	}
	if enriched, ok := cs["enriched_keys"].([]any); ok && len(enriched) > 0 {
		keys := make([]string, 0, len(enriched))
		for _, e := range enriched {
			if s, ok := e.(string); ok {
				keys = append(keys, s)
			}
		}
		out = append(out, objectChange{Kind: "note", Label: "enriched keys: " + strings.Join(keys, ", ")})
	}

	return out
}

// objectPreviewChangesDerived derives a change list for a version with no
// recorded change_summary by diffing it against its preceding version: the
// status, the assignee, and each top-level property path (from→to for scalars,
// "changed" for nested/complex values).
func objectPreviewChangesDerived(cur, prev GraphObject) []objectChange {
	var out []objectChange
	if cur.Status != prev.Status {
		out = append(out, objectChange{Kind: "update", Label: "status", From: prev.Status, To: cur.Status})
	}
	if cur.Assignee != prev.Assignee {
		out = append(out, objectChange{Kind: "update", Label: "assignee", From: prev.Assignee, To: cur.Assignee})
	}

	keys := make(map[string]struct{}, len(cur.Properties)+len(prev.Properties))
	for k := range cur.Properties {
		keys[k] = struct{}{}
	}
	for k := range prev.Properties {
		keys[k] = struct{}{}
	}

	for _, k := range slices.Sorted(maps.Keys(keys)) {
		if strings.HasPrefix(k, "_") || k == "status" {
			continue
		}
		cv, cok := cur.Properties[k]
		pv, pok := prev.Properties[k]
		switch {
		case cok && !pok:
			out = append(out, objectChange{Kind: "add", Label: "/" + k})
		case !cok && pok:
			out = append(out, objectChange{Kind: "remove", Label: "/" + k})
		case objectPreviewValueEqual(cv, pv):
			continue
		case objectPreviewScalar(cv) && objectPreviewScalar(pv):
			out = append(out, objectChange{Kind: "update", Label: "/" + k, From: objectPreviewChangeValue(pv), To: objectPreviewChangeValue(cv)})
		default:
			out = append(out, objectChange{Kind: "changed", Label: "/" + k})
		}
	}
	return out
}

// objectPreviewScalar reports whether a decoded property value is a scalar
// (string, number, bool, null) rather than a nested map/slice.
func objectPreviewScalar(v any) bool {
	switch v.(type) {
	case string, float64, bool, nil, int, int64:
		return true
	default:
		return false
	}
}

// objectPreviewValueEqual compares two decoded property values for equality.
// Both come from JSON decode, so numbers are float64 and reflect.DeepEqual is
// a safe, order-insensitive-for-maps comparison.
func objectPreviewValueEqual(a, b any) bool {
	return reflect.DeepEqual(a, b)
}

// objectPreviewChangeValue renders a change-list value compactly: strings pass
// through (truncated), everything else is JSON-marshalled and truncated.
func objectPreviewChangeValue(v any) string {
	if v == nil {
		return "null"
	}
	if s, ok := v.(string); ok {
		return truncateRunes(s, 60)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return truncateRunes(fmt.Sprint(v), 60)
	}
	return truncateRunes(string(b), 60)
}

// sortedKeys returns the sorted keys of a map[string]any.
func sortedKeys(m map[string]any) []string {
	return slices.Sorted(maps.Keys(m))
}
