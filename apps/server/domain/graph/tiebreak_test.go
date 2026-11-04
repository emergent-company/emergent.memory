package graph

import (
	"math/rand"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lessUUID reports whether a < b using the same unsigned byte ordering that
// PostgreSQL applies to uuid values (and therefore to `ORDER BY id ASC`).
func lessUUID(a, b uuid.UUID) bool {
	return slices.Compare(a[:], b[:]) < 0
}

// isUUIDsSorted reports whether ids is strictly ascending.
func isUUIDsSorted(ids []uuid.UUID) bool {
	for i := 1; i < len(ids); i++ {
		if !lessUUID(ids[i-1], ids[i]) {
			return false
		}
	}
	return true
}

func TestSortMergeObjectSummaries_TieDeterminism(t *testing.T) {
	const n = 50
	const iterations = 100

	// All summaries share the same status ("unchanged") with distinct CanonicalIDs,
	// so the secondary CanonicalID tie-break is the only thing deciding order.
	base := make([]*BranchMergeObjectSummary, n)
	for i := range base {
		base[i] = &BranchMergeObjectSummary{CanonicalID: uuid.New(), Status: "unchanged"}
	}

	var reference []uuid.UUID
	for iter := 0; iter < iterations; iter++ {
		shuffled := make([]*BranchMergeObjectSummary, n)
		copy(shuffled, base)
		rng := rand.New(rand.NewSource(int64(iter)))
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

		sortMergeObjectSummaries(shuffled)

		ids := make([]uuid.UUID, n)
		for i, s := range shuffled {
			ids[i] = s.CanonicalID
		}

		if iter == 0 {
			reference = append([]uuid.UUID(nil), ids...)
		}

		require.Equal(t, reference, ids, "iter %d: sorted CanonicalID order diverged from reference", iter)
		assert.True(t, isUUIDsSorted(ids), "iter %d: CanonicalID sequence must be strictly ascending", iter)
	}
}

func TestSortMergeObjectSummaries_PrimaryStatusKey(t *testing.T) {
	statuses := []string{"unchanged", "conflict", "unchanged", "conflict", "unchanged", "conflict"}
	summaries := make([]*BranchMergeObjectSummary, 0, len(statuses))
	for _, st := range statuses {
		summaries = append(summaries, &BranchMergeObjectSummary{CanonicalID: uuid.New(), Status: st})
	}

	sortMergeObjectSummaries(summaries)

	lastConflict := -1
	firstUnchanged := -1
	for i, s := range summaries {
		if s.Status == "conflict" {
			lastConflict = i
		} else if s.Status == "unchanged" && firstUnchanged == -1 {
			firstUnchanged = i
		}
	}

	require.NotEqual(t, -1, lastConflict, "fixture should contain conflict entries")
	require.NotEqual(t, -1, firstUnchanged, "fixture should contain unchanged entries")
	assert.Less(t, lastConflict, firstUnchanged, "all conflict entries must precede all unchanged entries")
}

func TestSortMergeRelationshipSummaries_TieDeterminism(t *testing.T) {
	const n = 50
	const iterations = 100

	base := make([]*BranchMergeRelationshipSummary, n)
	for i := range base {
		base[i] = &BranchMergeRelationshipSummary{CanonicalID: uuid.New(), Status: "unchanged"}
	}

	var reference []uuid.UUID
	for iter := 0; iter < iterations; iter++ {
		shuffled := make([]*BranchMergeRelationshipSummary, n)
		copy(shuffled, base)
		rng := rand.New(rand.NewSource(int64(iter)))
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

		sortMergeRelationshipSummaries(shuffled)

		ids := make([]uuid.UUID, n)
		for i, s := range shuffled {
			ids[i] = s.CanonicalID
		}

		if iter == 0 {
			reference = append([]uuid.UUID(nil), ids...)
		}

		require.Equal(t, reference, ids, "iter %d: sorted CanonicalID order diverged from reference", iter)
		assert.True(t, isUUIDsSorted(ids), "iter %d: CanonicalID sequence must be strictly ascending", iter)
	}
}

func TestSortMergeRelationshipSummaries_PrimaryStatusKey(t *testing.T) {
	statuses := []string{"unchanged", "conflict", "unchanged", "conflict", "unchanged", "conflict"}
	summaries := make([]*BranchMergeRelationshipSummary, 0, len(statuses))
	for _, st := range statuses {
		summaries = append(summaries, &BranchMergeRelationshipSummary{CanonicalID: uuid.New(), Status: st})
	}

	sortMergeRelationshipSummaries(summaries)

	lastConflict := -1
	firstUnchanged := -1
	for i, s := range summaries {
		if s.Status == "conflict" {
			lastConflict = i
		} else if s.Status == "unchanged" && firstUnchanged == -1 {
			firstUnchanged = i
		}
	}

	require.NotEqual(t, -1, lastConflict, "fixture should contain conflict entries")
	require.NotEqual(t, -1, firstUnchanged, "fixture should contain unchanged entries")
	assert.Less(t, lastConflict, firstUnchanged, "all conflict entries must precede all unchanged entries")
}
