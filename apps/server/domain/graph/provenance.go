package graph

import (
	"context"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Provenance property keys stored on a graph object's Properties map.
//
// `_sources` is the canonical multi-source provenance array; `_source_id` /
// `_extraction_job_id` are the legacy scalar keys kept in sync with the newest
// `_sources` entry for backward compatibility. `_document_version` is the
// legacy per-object document-version stamp consulted only during backfill.
const (
	provenanceSourcesKey    = "_sources"
	provenanceSourceIDKey   = "_source_id"
	provenanceJobIDKey      = "_extraction_job_id"
	provenanceDocVersionKey = "_document_version"
)

// provenanceMaxSources caps the number of `_sources` entries retained on a
// single object. Provenance is bounded so long-lived, heavily-extracted objects
// do not accumulate unbounded JSONB.
const provenanceMaxSources = 25

// provenanceEntry is the normalized in-memory form of one `_sources` element.
type provenanceEntry struct {
	DocumentID      string
	DocumentVersion int
	JobID           string
	ExtractedAt     time.Time
}

// BranchNotificationResolver resolves (marks as actioned) notifications that
// reference a staging branch once that branch is merged. Implemented by the
// notifications service; wired via fx into the graph service. Call sites treat
// a nil resolver as a no-op.
type BranchNotificationResolver interface {
	ResolveByBranch(ctx context.Context, projectID, branchID string) error
}

// mergeProvenanceProps returns a copy of `incoming` whose `_sources` is the
// union of `existing._sources` and `incoming._sources`, keeping the
// `_source_id` / `_extraction_job_id` scalars pointed at the newest entry for
// backward compatibility.
//
// Provenance is exempt from property policy, so every write path funnels
// through this helper rather than letting a scalar-overwrite (CreateOrUpdate) or
// a policy merge (mergeProps / applyPropsPolicy) clobber prior sources.
//
// When `_sources` is absent on a side but the legacy `_source_id` scalar is
// present, that side is seeded from `_source_id` + `_extraction_job_id`
// (+ `_document_version` when present), so existing objects backfill on their
// next write.
func mergeProvenanceProps(existing, incoming map[string]any) map[string]any {
	out := make(map[string]any, len(incoming)+3)
	for k, v := range incoming {
		out[k] = v
	}

	existingSources := sourcesFromProps(existing)
	incomingSources := sourcesFromProps(incoming)
	if len(existingSources) == 0 && len(incomingSources) == 0 {
		return out
	}

	merged := mergeSources(existingSources, incomingSources)
	if len(merged) == 0 {
		return out
	}

	out[provenanceSourcesKey] = serializeSources(merged)
	// Newest entry is last after the oldest-first stable sort.
	newest := merged[len(merged)-1]
	out[provenanceSourceIDKey] = newest.DocumentID
	out[provenanceJobIDKey] = newest.JobID
	return out
}

// provenanceKeysChanged reports whether applying the provenance union of
// `existing` and `incoming` onto `current` changes the provenance keys relative
// to `current`. It is used by the similarity-absorb path to force a version
// write even when the property policy produced no `changedKeys`.
func provenanceKeysChanged(current, existing, incoming map[string]any) bool {
	union := mergeProvenanceProps(existing, incoming)
	for _, k := range []string{provenanceSourcesKey, provenanceSourceIDKey, provenanceJobIDKey} {
		nv, hasNew := union[k]
		cv, hasCur := current[k]
		if hasNew != hasCur {
			return true
		}
		if hasNew && !jsonEqual(cv, nv) {
			return true
		}
	}
	return false
}

// applyProvenanceUnion overlays the provenance union of `existing` and
// `incoming` onto `merged` (a property-policy result) and returns the resulting
// map. Provenance is exempt from policy, so the union always wins.
func applyProvenanceUnion(merged, existing, incoming map[string]any) map[string]any {
	union := mergeProvenanceProps(existing, incoming)
	for _, k := range []string{provenanceSourcesKey, provenanceSourceIDKey, provenanceJobIDKey} {
		if v, ok := union[k]; ok {
			merged[k] = v
		}
	}
	return merged
}

// sourcesFromProps extracts the normalized provenance entries from a properties
// map, seeding a single entry from the legacy scalar keys when `_sources` is
// absent.
func sourcesFromProps(props map[string]any) []provenanceEntry {
	if props == nil {
		return nil
	}
	if raw, ok := props[provenanceSourcesKey]; ok {
		return parseSources(raw)
	}
	sourceID := provenanceString(props[provenanceSourceIDKey])
	if sourceID == "" {
		return nil
	}
	return []provenanceEntry{{
		DocumentID:      sourceID,
		DocumentVersion: provenanceInt(props[provenanceDocVersionKey]),
		JobID:           provenanceString(props[provenanceJobIDKey]),
	}}
}

// parseSources decodes a `_sources` array value into normalized entries. A
// malformed value yields nil; an empty array yields an empty (non-nil) slice.
func parseSources(raw any) []provenanceEntry {
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]provenanceEntry, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		e := provenanceEntry{
			DocumentID:      provenanceString(m["document_id"]),
			DocumentVersion: provenanceInt(m["document_version"]),
			JobID:           provenanceString(m["job_id"]),
		}
		if ts := provenanceString(m["extracted_at"]); ts != "" {
			if t, err := time.Parse(time.RFC3339, ts); err == nil {
				e.ExtractedAt = t
			}
		}
		if e.DocumentID == "" {
			continue
		}
		out = append(out, e)
	}
	return out
}

// serializeSources encodes normalized entries back into the `_sources` array
// shape. Entries with an unknown version or timestamp omit those fields.
func serializeSources(entries []provenanceEntry) []any {
	out := make([]any, 0, len(entries))
	for _, e := range entries {
		m := map[string]any{
			"document_id": e.DocumentID,
			"job_id":      e.JobID,
		}
		if e.DocumentVersion > 0 {
			m["document_version"] = e.DocumentVersion
		}
		if !e.ExtractedAt.IsZero() {
			m["extracted_at"] = e.ExtractedAt.UTC().Format(time.RFC3339)
		}
		out = append(out, m)
	}
	return out
}

// mergeSources unions two entry sets: dedup by document_id keeping the highest
// document_version (earliest extracted_at as tiebreak), then sorts oldest
// extracted_at first and caps at provenanceMaxSources (retaining the newest).
func mergeSources(existing, incoming []provenanceEntry) []provenanceEntry {
	byDoc := make(map[string]provenanceEntry, len(existing)+len(incoming))
	for _, e := range existing {
		mergeProvenanceEntry(byDoc, e)
	}
	for _, e := range incoming {
		mergeProvenanceEntry(byDoc, e)
	}

	entries := make([]provenanceEntry, 0, len(byDoc))
	for _, e := range byDoc {
		entries = append(entries, e)
	}

	// Stable order: oldest extracted_at first, with deterministic tiebreaks.
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if !a.ExtractedAt.Equal(b.ExtractedAt) {
			return a.ExtractedAt.Before(b.ExtractedAt)
		}
		if a.DocumentVersion != b.DocumentVersion {
			return a.DocumentVersion < b.DocumentVersion
		}
		return a.DocumentID < b.DocumentID
	})

	if len(entries) > provenanceMaxSources {
		entries = entries[len(entries)-provenanceMaxSources:]
	}
	return entries
}

func mergeProvenanceEntry(byDoc map[string]provenanceEntry, e provenanceEntry) {
	if e.DocumentID == "" {
		return
	}
	cur, ok := byDoc[e.DocumentID]
	if !ok {
		byDoc[e.DocumentID] = e
		return
	}
	if e.DocumentVersion > cur.DocumentVersion {
		byDoc[e.DocumentID] = e
		return
	}
	if e.DocumentVersion == cur.DocumentVersion && e.ExtractedAt.Before(cur.ExtractedAt) {
		byDoc[e.DocumentID] = e
	}
}

func provenanceString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func provenanceInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
			return i
		}
	}
	return 0
}

// normalizeObjectKey normalizes an object key for deterministic same-entity
// detection at merge: trimmed and case-insensitive. An empty or nil key returns
// "" so objects without a key never match.
func normalizeObjectKey(key *string) string {
	if key == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(*key))
}

// sameEntityIndexKey builds the lookup key for the exact-key same-entity index.
// A NUL separator keeps a type that ends with the key's first character from
// colliding with a different type/key split.
func sameEntityIndexKey(typ, normalizedKey string) string {
	return typ + "\x00" + normalizedKey
}

// buildSameEntityTargetIndex builds a normalized `type|key → head` index from
// the target-branch HEADs. Soft-deleted heads (DeletedAt != nil) and heads
// without a key are excluded so they can never win an exact-key match. When
// several targets share a type+key, the one with the smallest canonical ID
// wins so the result is deterministic regardless of map iteration order.
//
// Built once per merge so keys are normalized once rather than once per source
// pair.
func buildSameEntityTargetIndex(targetObjects map[uuid.UUID]*BranchObjectHead) map[string]*BranchObjectHead {
	index := make(map[string]*BranchObjectHead, len(targetObjects))
	for _, tgt := range targetObjects {
		if tgt == nil || tgt.DeletedAt != nil {
			continue
		}
		key := normalizeObjectKey(tgt.Key)
		if key == "" {
			continue
		}
		idxKey := sameEntityIndexKey(tgt.Type, key)
		if existing, ok := index[idxKey]; !ok || slices.Compare(tgt.CanonicalID[:], existing.CanonicalID[:]) < 0 {
			index[idxKey] = tgt
		}
	}
	return index
}

// findSameEntityTargetInIndex returns the target-branch HEAD whose `type` and
// normalized key match the source object's, or nil when none matches. It looks
// up a pre-built index so the caller can amortize key normalization across all
// source objects in a merge.
func findSameEntityTargetInIndex(src *BranchObjectHead, index map[string]*BranchObjectHead) *BranchObjectHead {
	if src == nil {
		return nil
	}
	want := normalizeObjectKey(src.Key)
	if want == "" {
		return nil
	}
	return index[sameEntityIndexKey(src.Type, want)]
}

// findSameEntityTarget returns the target-branch HEAD whose `type` and
// normalized key match the source object's, or nil when none matches.
//
// This is the exact-key path of same-entity detection: it runs before (and
// independently of) the embedding-driven similarity probe, so an object
// extracted from document B that shares a key with an object already merged
// from document A is recognized as the same entity without embeddings.
func findSameEntityTarget(src *BranchObjectHead, targetObjects map[uuid.UUID]*BranchObjectHead) *BranchObjectHead {
	return findSameEntityTargetInIndex(src, buildSameEntityTargetIndex(targetObjects))
}
