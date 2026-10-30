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
	"unicode/utf8"

	"github.com/a-h/templ"
	"github.com/labstack/echo/v4"
)

// uiObjectPreviewPartial renders the read-only object summary swapped into the
// global preview drawer (#object-preview-body) when a chat object reference is
// clicked. It reuses the same data seam as the object detail page: GetGraphObject
// supplies the object (including its stored Properties) and GetCompiledTypes
// supplies the declared per-type ui accent (so the header icon matches the rest
// of the UI) plus the schema properties the drawer's type panel lists. Failures
// degrade to a small "unavailable" state rather than leaving the drawer blank.
// The optional ?rel= fragment carries the relationship id a
// `/objects/<src>#relationship-<rel>` reference pointed at, which is humanised
// into a subtle "Referenced via …" line.
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
	schema := objectPreviewSchema{TypeLabel: obj.Type}
	compiled, compiledErr := s.memory.GetCompiledTypes(ctx)
	captureError(compiledErr)
	if compiled != nil {
		typeUIByType = objectTypeUIMap(compiled.ObjectTypes)
		schema = objectPreviewSchema{
			TypeLabel:   objectPreviewTypeLabel(compiled.ObjectTypes, obj.Type),
			Properties:  compiledTypePropertyDefs(compiled.ObjectTypes, obj.Type),
			HasWorkflow: objectStatusControlFor(compiled.ObjectTypes, obj.Type).Show,
		}
	}

	// ?actions=board renders the board's work-item surface into the preview's
	// action slot — the status-gated actions followed by the merged comments
	// thread — so the drawer is a usable surface, not just a summary. Chat
	// previews carry no actions param and stay read-only. A failed work-item
	// fetch leaves the slot empty — intentionally not captured, so a missing
	// work item never degrades the object summary. The assignee picker's
	// candidate list is fetched only here (never on the read-only path).
	var actions templ.Component
	if c.QueryParam("actions") == "board" {
		if detail, werr := s.memory.GetWorkItem(ctx, id); werr == nil && detail != nil && detail.Item != nil {
			actions = boardPreviewBoardSlot(detail.Item, s.boardStatusMap(ctx), s.boardAssigneeCandidates(ctx), detail.Comments, detail.Feedback)
		}
	}

	renderPartial(c.Response().Writer, c.Request(),
		objectPreviewContent(obj, typeUIByType, s.objectPreviewRelationshipLabel(ctx, id, relID), schema, actions))
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

// objectPreviewField is one read-only row of the preview drawer: the label the
// row is titled with, the schema description shown beneath it (empty for
// built-in fields, which carry no schema prose), and the value. Name, when set,
// is the raw schema property name — exposed as the label's tooltip because the
// label itself is humanised. Mono renders identifier values monospaced, and
// Empty de-emphasises a row that has no value.
type objectPreviewField struct {
	Label       string
	Description string
	Value       string
	Name        string
	Empty       bool
	Mono        bool
}

// objectPreviewSchema is the compiled-schema context the drawer renders with:
// the type's display label (the second panel's title, e.g. "Scenario
// Properties") and its declared properties. HasWorkflow mirrors the create/edit
// form rule — a type declares a workflow when it is board-enabled or lists
// allowedStatuses — so the drawer shows a Workflow status row, and drops the
// mirrored `status` property, exactly when the forms would render the
// workflow-status field.
type objectPreviewSchema struct {
	TypeLabel   string
	Properties  []objectPropertyDef
	HasWorkflow bool
}

// typePanelTitle is the second panel's heading: the type's display label plus
// "Properties", so a Scenario's schema fields read as "Scenario Properties"
// rather than merging into the platform fields above them.
func (s objectPreviewSchema) typePanelTitle() string {
	return typePropertiesPanelTitle(s.TypeLabel)
}

// typePropertiesPanelTitle is the schema panel's heading on every surface that
// splits platform fields from schema fields (the preview drawer and the object
// detail page), so the two can never drift. An unresolved type label falls back
// to a neutral title rather than a bare " Properties".
func typePropertiesPanelTitle(typeLabel string) string {
	if typeLabel == "" {
		return "Type Properties"
	}
	return typeLabel + " Properties"
}

// objectPreviewTypeLabel resolves the object's type display label from the
// compiled types, falling back to the raw type name when the schema does not
// know it (the drawer still titles its type-property panel).
func objectPreviewTypeLabel(objectTypes []CompiledType, typeName string) string {
	for _, t := range visibleCompiledTypes(objectTypes) {
		if t.Name == typeName {
			return compiledTypeLabel(t)
		}
	}
	return typeName
}

// objectPreviewBuiltinFields renders the platform fields an object carries
// outside its schema — Key, the workflow status (only for a type that declares
// a workflow), Assignee (only when set), Version, Created, and the canonical ID
// — as one panel. The type itself stays in the header chip and Labels keeps its
// own section, so neither is repeated here.
func objectPreviewBuiltinFields(o *GraphObject, hasWorkflow bool) []objectPreviewField {
	fields := []objectPreviewField{{Label: "Key", Value: o.Key, Empty: o.Key == ""}}
	if hasWorkflow {
		status := ""
		if o.Status != "" {
			status = boardStatusLabel(o.Status)
		}
		fields = append(fields, objectPreviewField{Label: "Workflow status", Value: status, Empty: status == ""})
	}
	if o.Assignee != "" {
		fields = append(fields, objectPreviewField{Label: "Assignee", Value: o.Assignee})
	}
	created := objectPreviewCreated(o.CreatedAt)
	id := objectPreviewCanonicalID(*o)
	return append(fields,
		objectPreviewField{Label: "Version", Value: objectPreviewVersionLabel(o.Version)},
		objectPreviewField{Label: "Created", Value: created, Empty: created == ""},
		objectPreviewField{Label: "ID", Value: id, Empty: id == "", Mono: true},
	)
}

// objectPreviewTypeFields renders the object's schema properties as labelled
// rows carrying the schema's own description, in key order (a compiled type's
// property map has no declaration order of its own), followed
// by any stored property the schema does not declare (agents may write keys the
// type did not anticipate) so nothing is silently hidden. The mirrored `status`
// property is dropped when the Workflow status row already shows it; for a type
// with no workflow it stays here, which is where a Scenario's "proposed" status
// is read.
func objectPreviewTypeFields(o GraphObject, schema objectPreviewSchema) []objectPreviewField {
	declared := make(map[string]bool, len(schema.Properties))
	out := make([]objectPreviewField, 0, len(schema.Properties)+len(o.Properties))
	// The mirrored `status` property is folded into the Workflow status row only
	// when that row actually carries a value: a workflow type whose column is
	// empty but whose property is set (a batch/upsert write path does not mirror
	// it) still shows the stored value here rather than hiding it.
	foldStatus := schema.HasWorkflow && o.Status != ""
	for _, d := range schema.Properties {
		declared[d.Name] = true
		if d.Name == "status" && foldStatus {
			continue
		}
		value := objectPreviewValue(o.Properties[d.Name])
		out = append(out, objectPreviewField{
			Label:       objectPreviewDeclaredLabel(d),
			Name:        d.Name,
			Description: d.Description,
			Value:       value,
			Empty:       value == "",
		})
	}
	for _, p := range objectPreviewStoredProperties(o) {
		if declared[p.Key] || (p.Key == "status" && foldStatus) {
			continue
		}
		out = append(out, objectPreviewField{
			Label: objectPreviewFieldLabel(p.Key),
			Name:  p.Key,
			Value: p.Value,
			Empty: p.Value == "",
		})
	}
	return out
}

// objectPreviewDeclaredLabel is the row label for a property the schema declares:
// the authored display label (JSON-Schema `title`) when the property has one,
// otherwise this drawer's humanised key. Titles take precedence so an authored
// label is authoritative wherever the property is named; the humanised fallback
// is the read-only preview's own convention for properties that declare none.
func objectPreviewDeclaredLabel(d objectPropertyDef) string {
	if d.Title != "" {
		return d.Title
	}
	return objectPreviewFieldLabel(d.Name)
}

// objectPreviewFieldLabel renders a schema property name as a readable field
// label ("entry_point" → "Entry point"), keeping the raw name on the row's
// tooltip so the API shape stays discoverable.
func objectPreviewFieldLabel(name string) string {
	words := strings.Fields(strings.NewReplacer("_", " ", "-", " ", ".", " ").Replace(name))
	if len(words) == 0 {
		return name
	}
	label := strings.Join(words, " ")
	// Capitalise the first rune, not the first byte: a property name may start
	// with a multi-byte character ("état").
	first, size := utf8.DecodeRuneInString(label)
	return strings.ToUpper(string(first)) + label[size:]
}

// objectPreviewStoredProperties lists the object's stored properties in key
// order, dropping only internal (underscore-prefixed) names. Unlike
// graphObjectProperties (which drops empties for the compact list render),
// every value is returned — including empty ones — so the preview shows the
// full shape of the object. Deduplicating the mirrored `status` key is the
// caller's job: it depends on whether the type declares a workflow.
func objectPreviewStoredProperties(o GraphObject) []propertyKV {
	keys := make([]string, 0, len(o.Properties))
	for k := range o.Properties {
		if strings.HasPrefix(k, "_") {
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
