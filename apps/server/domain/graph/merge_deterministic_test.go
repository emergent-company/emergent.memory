package graph

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFindConflictingPathsExcludesProvenanceAndDescription pins L0/L3: a
// leading-underscore key (`_sources`, `_extraction_job_id`, `_source_id`, …) or
// the top-level `description` never counts as a conflict, even when the values
// differ on both sides. A genuine value disagreement on any other shared key
// still conflicts.
func TestFindConflictingPathsExcludesProvenanceAndDescription(t *testing.T) {
	src := map[string]any{
		"name":               "Barry Farber",
		"role":               "radio host",
		"_sources":           []any{map[string]any{"document_id": "doc-b", "extracted_at": "2024-02-01T00:00:00Z"}},
		"_extraction_job_id": "job-b",
		"_source_id":         "doc-b",
		"description":        "Radio host",
	}
	tgt := map[string]any{
		"name":               "Barry Farber",
		"role":               "radio host",
		"_sources":           []any{map[string]any{"document_id": "doc-a", "extracted_at": "2024-01-01T00:00:00Z"}},
		"_extraction_job_id": "job-a",
		"_source_id":         "doc-a",
		"description":        "American talk radio host",
	}

	assert.Empty(t, findConflictingPaths(src, tgt),
		"provenance and description drift must not be conflicts")

	src["role"] = "author"
	assert.Equal(t, []string{"/role"}, findConflictingPaths(src, tgt),
		"a genuine value disagreement must still conflict")
}

// TestClassifySameEntityProvenanceOnlyFastForwards pins L0 end-to-end at the
// classification boundary: a same-entity re-extraction whose content differs
// only in provenance keys is a clean fast-forward, never a conflict.
func TestClassifySameEntityProvenanceOnlyFastForwards(t *testing.T) {
	key := "barry-farber"
	target := makeSameEntityHead(uuid.New(), "Person", key, "Barry Farber", nil,
		map[string]any{
			"name":     "Barry Farber",
			"role":     "radio host",
			"_sources": []any{map[string]any{"document_id": "doc-a", "extracted_at": "2024-01-01T00:00:00Z"}},
		}, nil)

	src := makeSameEntityHead(uuid.New(), "Person", key, "Barry Farber", nil,
		map[string]any{
			"name":     "Barry Farber",
			"role":     "radio host",
			"_sources": []any{map[string]any{"document_id": "doc-b", "extracted_at": "2024-02-01T00:00:00Z"}},
		}, nil)

	status, conflicts := classifySameEntity(src, target)
	assert.Equal(t, "fast_forward", status)
	assert.Empty(t, conflicts)
}

// TestClassifySameEntityDescriptionOnlyFastForwards pins L3 at the
// classification boundary: a description-only difference fast-forwards (the
// merge resolves the description newest-wins rather than parking the branch).
func TestClassifySameEntityDescriptionOnlyFastForwards(t *testing.T) {
	key := "barry-farber"
	target := makeSameEntityHead(uuid.New(), "Person", key, "Barry Farber", nil,
		map[string]any{"name": "Barry Farber", "role": "radio host", "description": "Radio host"}, nil)

	src := makeSameEntityHead(uuid.New(), "Person", key, "Barry Farber", nil,
		map[string]any{"name": "Barry Farber", "role": "radio host", "description": "American talk radio host"}, nil)

	status, conflicts := classifySameEntity(src, target)
	assert.Equal(t, "fast_forward", status)
	assert.Empty(t, conflicts)
}

func TestNewestExtractedAt(t *testing.T) {
	props := map[string]any{
		"_sources": []any{
			map[string]any{"document_id": "doc-a", "extracted_at": "2024-01-01T00:00:00Z"},
			map[string]any{"document_id": "doc-b", "extracted_at": "2024-03-01T00:00:00Z"},
		},
	}
	got, ok := newestExtractedAt(props)
	require.True(t, ok)
	assert.Equal(t, "2024-03-01T00:00:00Z", got.UTC().Format(time.RFC3339))

	_, ok = newestExtractedAt(map[string]any{"name": "x"})
	assert.False(t, ok)
}

// TestNewestPresetAndStrategy pins the new deterministic "newest" preset and
// legacy conflict strategy: it is accepted, advertised, resolves without the
// similarity probe, and sets CanonicalConflictAction to "newest".
func TestNewestPresetAndStrategy(t *testing.T) {
	require.NoError(t, validateMergePolicy("newest", ""))
	require.Contains(t, MergeConflictStrategyNames(), "newest")
	require.Contains(t, MergePolicyNames(), "newest")

	got := resolveMergePolicy("newest", "", 0)
	assert.False(t, got.SimilarityEnabled)
	assert.Equal(t, "newest", got.CanonicalConflictAction)
	assert.Equal(t, mergePolicyPresets["newest"], got)

	// The legacy conflict strategy "newest" resolves through the same action.
	got = resolveMergePolicy("", "newest", 0)
	assert.Equal(t, "newest", got.CanonicalConflictAction)
}

// TestFindConflictingPathsNormalizesStringCaseAndWhitespace pins the case-fold +
// trim + internal-whitespace-collapse equality: a case/whitespace-only drift
// (e.g. "Data processor" vs "data processor") is not a conflict, but a genuine
// value difference (e.g. "Masseuse" vs "Masseuse and musician") still is.
func TestFindConflictingPathsNormalizesStringCaseAndWhitespace(t *testing.T) {
	src := map[string]any{"job_title": "Data processor", "occupation": "Masseuse"}
	tgt := map[string]any{"job_title": "data processor", "occupation": "Masseuse and musician"}

	assert.Equal(t, []string{"/occupation"}, findConflictingPaths(src, tgt),
		"case/whitespace-only drift must not conflict; a genuine value difference must")
}

// TestValuesEqual pins the conflict-detection value comparison.
func TestValuesEqual(t *testing.T) {
	assert.True(t, valuesEqual("Data processor", "data processor"))
	assert.True(t, valuesEqual("  data   processor  ", "DATA PROCESSOR"))
	assert.False(t, valuesEqual("Masseuse", "Masseuse and musician"))
	assert.True(t, valuesEqual([]any{"a", "b"}, []any{"a", "b"}))
	assert.False(t, valuesEqual([]any{"a"}, []any{"b"}))
	assert.True(t, valuesEqual(map[string]any{"a": 1}, map[string]any{"a": 1}))
	assert.False(t, valuesEqual("x", 1), "a string and a number are never equal")
}

// TestReconcilePresetAndStrategy pins the new opt-in "reconcile" preset and
// legacy conflict strategy: accepted, advertised, no similarity probe, action
// "reconcile", and the per-run cap default.
func TestReconcilePresetAndStrategy(t *testing.T) {
	require.NoError(t, validateMergePolicy("reconcile", ""))
	require.Contains(t, MergeConflictStrategyNames(), "reconcile")
	require.Contains(t, MergePolicyNames(), "reconcile")

	got := resolveMergePolicy("reconcile", "", 0)
	assert.False(t, got.SimilarityEnabled)
	assert.Equal(t, "reconcile", got.CanonicalConflictAction)
	assert.Equal(t, defaultReconcileMaxPerRun, got.ReconcileMaxPerRun)
	assert.Equal(t, mergePolicyPresets["reconcile"], got)

	// The legacy conflict strategy "reconcile" resolves through the same action
	// and defaults the cap.
	got = resolveMergePolicy("", "reconcile", 0)
	assert.Equal(t, "reconcile", got.CanonicalConflictAction)
	assert.Equal(t, defaultReconcileMaxPerRun, got.ReconcileMaxPerRun)
}
