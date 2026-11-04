package mcp

import "github.com/emergent-company/emergent.memory/domain/journal"

// filterJournalEntriesByScope drops or scrubs journal entries so no excluded
// object or relationship type name survives in any field — top-level columns or
// nested metadata. A nil or unrestricted scope returns entries unchanged (no
// allocation).
//
// Per entry:
//   - graph_object: object_type present -> keep iff AllowsObject; object_type
//     nil -> fail closed (drop when the object axis is restricted) to cover bulk
//     batch rows.
//   - graph_relationship: resolve the relationship type from object_type or
//     metadata.rel_type; empty -> fail closed (drop when the relationship axis
//     is restricted); otherwise keep iff AllowsRelationship.
//   - nil/unknown entity_type (merge, fork, notes): keep, then scrub metadata.
//
// Every kept entry has its metadata scrubbed recursively.
//
// Note: this runs after the journal service's pagination, so NextCursor/HasMore
// still reflect the unfiltered page — a page can come back shorter than
// requested under a scope. That trade-off keeps excluded types fully hidden
// without a second cursor-aware query.
func filterJournalEntriesByScope(scope *TypeScope, entries []*journal.JournalEntry) []*journal.JournalEntry {
	if !scope.HasObjectRestriction() && !scope.HasRelationshipRestriction() {
		return entries
	}
	out := make([]*journal.JournalEntry, 0, len(entries))
	for _, e := range entries {
		if e == nil {
			out = append(out, e)
			continue
		}
		switch {
		case e.EntityType != nil && *e.EntityType == journal.EntityObject:
			if e.ObjectType != nil {
				if !scope.AllowsObject(*e.ObjectType) {
					continue
				}
			} else if scope.HasObjectRestriction() {
				// Bulk batch rows carry EntityType=graph_object, ObjectType=nil.
				continue
			}
		case e.EntityType != nil && *e.EntityType == journal.EntityRelationship:
			relType := ""
			if e.ObjectType != nil {
				relType = *e.ObjectType
			} else if rv, ok := e.Metadata["rel_type"].(string); ok {
				relType = rv
			}
			if relType == "" {
				if scope.HasRelationshipRestriction() {
					continue
				}
			} else if !scope.AllowsRelationship(relType) {
				continue
			}
		default:
			// nil/unknown entity_type (merge, fork, notes): keep, scrub below.
		}
		scrubJournalMetadataByScope(scope, e.Metadata)
		out = append(out, e)
	}
	return out
}

// scrubJournalMetadataByScope removes excluded type names from a journal entry's
// metadata map, recursively, mutating the map in place.
func scrubJournalMetadataByScope(scope *TypeScope, m map[string]any) {
	if m == nil {
		return
	}
	// left_for_review_objects is a []map[string]any of candidate objects; drop
	// whole items whose object type is excluded rather than leaving a typeless
	// husk behind.
	if v, ok := m["left_for_review_objects"]; ok {
		m["left_for_review_objects"] = scrubLeftForReviewObjects(scope, v)
	}
	scrubMap(scope, m)
}

func scrubLeftForReviewObjects(scope *TypeScope, v any) any {
	switch t := v.(type) {
	case []map[string]any:
		kept := make([]map[string]any, 0, len(t))
		for _, item := range t {
			if !journalMapTypeAllowed(scope, item) {
				continue
			}
			scrubMap(scope, item)
			kept = append(kept, item)
		}
		return kept
	case []any:
		kept := make([]any, 0, len(t))
		for _, item := range t {
			im, ok := item.(map[string]any)
			if !ok {
				kept = append(kept, item)
				continue
			}
			if !journalMapTypeAllowed(scope, im) {
				continue
			}
			scrubMap(scope, im)
			kept = append(kept, im)
		}
		return kept
	default:
		return v
	}
}

// journalMapTypeAllowed reports whether a candidate-object map may be surfaced:
// any present type/object_type/type_name string must be allowed on the object
// axis.
func journalMapTypeAllowed(scope *TypeScope, m map[string]any) bool {
	for _, k := range []string{"type", "object_type", "type_name"} {
		if s, ok := m[k].(string); ok && !scope.AllowsObject(s) {
			return false
		}
	}
	return true
}

// scrubMap walks a metadata map in place, deleting keys whose value names an
// excluded type and recursing into nested maps/slices.
func scrubMap(scope *TypeScope, m map[string]any) {
	for k, val := range m {
		switch k {
		case "rel_type":
			if s, ok := val.(string); ok && !scope.AllowsRelationship(s) {
				delete(m, k)
				continue
			}
		case "type", "object_type", "type_name":
			if s, ok := val.(string); ok && !scope.AllowsObject(s) {
				delete(m, k)
				continue
			}
		case "types":
			m[k] = filterTypesValue(scope, val)
			continue
		}
		scrubValue(scope, val)
	}
}

// filterTypesValue narrows a "types" list ([]string or []any of strings) to the
// allowed object types, preserving non-string elements.
func filterTypesValue(scope *TypeScope, val any) any {
	switch t := val.(type) {
	case []string:
		out := make([]string, 0, len(t))
		for _, s := range t {
			if scope.AllowsObject(s) {
				out = append(out, s)
			}
		}
		return out
	case []any:
		out := make([]any, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				if scope.AllowsObject(s) {
					out = append(out, item)
				}
			} else {
				out = append(out, item)
			}
		}
		return out
	default:
		return val
	}
}

// scrubValue recurses into nested maps and slices.
func scrubValue(scope *TypeScope, v any) {
	switch t := v.(type) {
	case map[string]any:
		scrubMap(scope, t)
	case []any:
		for _, item := range t {
			scrubValue(scope, item)
		}
	case []map[string]any:
		for _, item := range t {
			scrubMap(scope, item)
		}
	}
}
