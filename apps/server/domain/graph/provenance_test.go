package graph

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// srcEntry builds one canonical `_sources` array element.
func srcEntry(t *testing.T, docID, jobID string, version int, at time.Time) map[string]any {
	t.Helper()
	m := map[string]any{
		"document_id":  docID,
		"job_id":       jobID,
		"extracted_at": at.UTC().Format(time.RFC3339),
	}
	if version > 0 {
		m["document_version"] = version
	}
	return m
}

func TestMergeProvenanceProps_Union(t *testing.T) {
	docA, docB := uuid.NewString(), uuid.NewString()
	jobA, jobB := uuid.NewString(), uuid.NewString()
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)

	existing := map[string]any{
		"_sources":           []any{srcEntry(t, docA, jobA, 1, t0)},
		"_source_id":         docA,
		"_extraction_job_id": jobA,
	}
	incoming := map[string]any{
		"name":               "svc",
		"_sources":           []any{srcEntry(t, docB, jobB, 2, t1)},
		"_source_id":         docB,
		"_extraction_job_id": jobB,
	}

	out := mergeProvenanceProps(existing, incoming)

	// Non-provenance keys survive.
	assert.Equal(t, "svc", out["name"])

	sources, ok := out["_sources"].([]any)
	require.True(t, ok, "_sources must be an array")
	assert.Len(t, sources, 2)

	// Newest entry (docB, later extracted_at) becomes the legacy scalar.
	assert.Equal(t, docB, out["_source_id"])
	assert.Equal(t, jobB, out["_extraction_job_id"])
}

func TestMergeProvenanceProps_BackfillsLegacyScalars(t *testing.T) {
	docA, docB := uuid.NewString(), uuid.NewString()
	jobA, jobB := uuid.NewString(), uuid.NewString()
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)

	// Existing object predates `_sources`: only legacy scalars.
	existing := map[string]any{
		"_source_id":         docA,
		"_extraction_job_id": jobA,
		"_document_version":  3,
	}
	incoming := map[string]any{
		"_sources":           []any{srcEntry(t, docB, jobB, 2, t1)},
		"_source_id":         docB,
		"_extraction_job_id": jobB,
	}

	out := mergeProvenanceProps(existing, incoming)
	sources, ok := out["_sources"].([]any)
	require.True(t, ok)
	assert.Len(t, sources, 2, "legacy scalars must backfill into _sources")

	// The legacy entry carries its document version through the backfill.
	found := false
	for _, s := range sources {
		m := s.(map[string]any)
		if m["document_id"] == docA {
			found = true
			assert.Equal(t, 3, m["document_version"])
		}
	}
	assert.True(t, found, "backfilled docA entry must be present")
}

func TestMergeProvenanceProps_NoProvenanceIsIdentity(t *testing.T) {
	incoming := map[string]any{"name": "svc", "description": "x"}
	out := mergeProvenanceProps(nil, incoming)
	assert.Equal(t, incoming, out)
}

func TestMergeSources_DedupKeepsHighestVersion(t *testing.T) {
	doc := uuid.NewString()
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	job1, job2 := uuid.NewString(), uuid.NewString()

	existing := []provenanceEntry{{DocumentID: doc, DocumentVersion: 1, JobID: job1, ExtractedAt: t0}}
	incoming := []provenanceEntry{{DocumentID: doc, DocumentVersion: 2, JobID: job2, ExtractedAt: t0.Add(time.Hour)}}

	merged := mergeSources(existing, incoming)
	require.Len(t, merged, 1)
	assert.Equal(t, 2, merged[0].DocumentVersion)
	assert.Equal(t, job2, merged[0].JobID)
}

func TestMergeSources_CapsAtMax(t *testing.T) {
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	entries := make([]provenanceEntry, 0, provenanceMaxSources+5)
	for i := 0; i < provenanceMaxSources+5; i++ {
		entries = append(entries, provenanceEntry{
			DocumentID:  uuid.NewString(),
			ExtractedAt: t0.Add(time.Duration(i) * time.Minute),
		})
	}

	merged := mergeSources(entries, nil)
	assert.Len(t, merged, provenanceMaxSources)
	// The cap keeps the newest entries (oldest-first sort → slice the tail).
	assert.Equal(t, t0.Add(5*time.Minute), merged[0].ExtractedAt)
}

func TestMergeSources_SortsOldestFirst(t *testing.T) {
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	late := t0.Add(2 * time.Hour)
	mid := t0.Add(time.Hour)

	merged := mergeSources(nil, []provenanceEntry{
		{DocumentID: uuid.NewString(), ExtractedAt: late},
		{DocumentID: uuid.NewString(), ExtractedAt: mid},
		{DocumentID: uuid.NewString(), ExtractedAt: t0},
	})

	require.Len(t, merged, 3)
	assert.True(t, merged[0].ExtractedAt.Equal(t0))
	assert.True(t, merged[1].ExtractedAt.Equal(mid))
	assert.True(t, merged[2].ExtractedAt.Equal(late))
}

func TestSerializeParseRoundTrip(t *testing.T) {
	doc := uuid.NewString()
	job := uuid.NewString()
	at := time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)

	entries := []provenanceEntry{{DocumentID: doc, DocumentVersion: 4, JobID: job, ExtractedAt: at}}
	serialized := serializeSources(entries)

	parsed := parseSources(serialized)
	require.Len(t, parsed, 1)
	assert.Equal(t, doc, parsed[0].DocumentID)
	assert.Equal(t, 4, parsed[0].DocumentVersion)
	assert.Equal(t, job, parsed[0].JobID)
	assert.True(t, parsed[0].ExtractedAt.Equal(at))
}

func TestParseSources_MalformedYieldsNil(t *testing.T) {
	assert.Nil(t, parseSources("not-an-array"))
	assert.Nil(t, parseSources(nil))

	// Entries without document_id are dropped.
	out := parseSources([]any{map[string]any{"job_id": uuid.NewString()}})
	assert.Empty(t, out)
}

func TestNormalizeObjectKey(t *testing.T) {
	s := "  Alice  "
	assert.Equal(t, "alice", normalizeObjectKey(&s))

	empty := ""
	assert.Equal(t, "", normalizeObjectKey(&empty))
	assert.Equal(t, "", normalizeObjectKey(nil))
}

func TestFindSameEntityTarget(t *testing.T) {
	key := "payment-service"
	tgtA := &BranchObjectHead{CanonicalID: uuid.MustParse("aaaaaaaa-0000-0000-0000-000000000000"), Type: "Service", Key: &key}
	tgtB := &BranchObjectHead{CanonicalID: uuid.MustParse("bbbbbbbb-0000-0000-0000-000000000000"), Type: "Service", Key: &key}
	tgtOtherType := &BranchObjectHead{CanonicalID: uuid.New(), Type: "Person", Key: &key}
	tgtOtherKey := &BranchObjectHead{CanonicalID: uuid.New(), Type: "Service", Key: ptr("other")}

	targets := map[uuid.UUID]*BranchObjectHead{
		tgtA.CanonicalID:         tgtA,
		tgtB.CanonicalID:         tgtB,
		tgtOtherType.CanonicalID: tgtOtherType,
		tgtOtherKey.CanonicalID:  tgtOtherKey,
	}

	// Same type + normalized key (case/space-insensitive) → match, smallest id wins.
	spaced := "  Payment-Service  "
	src := &BranchObjectHead{CanonicalID: uuid.New(), Type: "Service", Key: &spaced}
	got := findSameEntityTarget(src, targets)
	require.NotNil(t, got)
	assert.Equal(t, tgtA.CanonicalID, got.CanonicalID, "smallest canonical id must win deterministically")

	// A soft-deleted target with a smaller canonical id must be skipped: the
	// exact-key path must never re-classify onto a head that was deleted.
	now := time.Now()
	deletedWinner := &BranchObjectHead{
		CanonicalID: uuid.MustParse("00000000-0000-0000-0000-000000000000"),
		Type:        "Service",
		Key:         &key,
		DeletedAt:   &now,
	}
	targets[deletedWinner.CanonicalID] = deletedWinner
	got = findSameEntityTarget(src, targets)
	require.NotNil(t, got)
	assert.Equal(t, tgtA.CanonicalID, got.CanonicalID, "soft-deleted target must not win exact-key match")

	// A soft-deleted target alone must never match.
	assert.Nil(t, findSameEntityTarget(src, map[uuid.UUID]*BranchObjectHead{
		deletedWinner.CanonicalID: deletedWinner,
	}))

	// Nil key → no match.
	assert.Nil(t, findSameEntityTarget(&BranchObjectHead{CanonicalID: uuid.New(), Type: "Service"}, targets))

	// No type/key match → nil.
	nomatch := &BranchObjectHead{CanonicalID: uuid.New(), Type: "Widget", Key: ptr("widget-x")}
	assert.Nil(t, findSameEntityTarget(nomatch, targets))
}

func TestProvenanceKeysChanged(t *testing.T) {
	docA, docB := uuid.NewString(), uuid.NewString()
	jobA, jobB := uuid.NewString(), uuid.NewString()
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	existing := map[string]any{
		"_sources":           []any{srcEntry(t, docA, jobA, 1, t0)},
		"_source_id":         docA,
		"_extraction_job_id": jobA,
	}
	incoming := map[string]any{
		"_sources":           []any{srcEntry(t, docB, jobB, 2, t0.Add(time.Hour))},
		"_source_id":         docB,
		"_extraction_job_id": jobB,
	}

	// incoming adds a new source → provenance changes.
	assert.True(t, provenanceKeysChanged(existing, existing, incoming))

	// incoming identical to current → no provenance change.
	assert.False(t, provenanceKeysChanged(existing, existing, existing))

	// current missing provenance entirely → change when union introduces it.
	assert.True(t, provenanceKeysChanged(map[string]any{}, existing, incoming))
}

func TestApplyProvenanceUnion(t *testing.T) {
	docA, docB := uuid.NewString(), uuid.NewString()
	jobA, jobB := uuid.NewString(), uuid.NewString()
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	existing := map[string]any{"_sources": []any{srcEntry(t, docA, jobA, 1, t0)}}
	incoming := map[string]any{"_sources": []any{srcEntry(t, docB, jobB, 2, t0.Add(time.Hour))}}

	// A policy merge produced only non-provenance keys.
	merged := map[string]any{"name": "svc"}
	out := applyProvenanceUnion(merged, existing, incoming)

	assert.Equal(t, "svc", out["name"])
	sources, ok := out["_sources"].([]any)
	require.True(t, ok)
	assert.Len(t, sources, 2)
	assert.Equal(t, docB, out["_source_id"])
}

func ptr(s string) *string { return &s }
