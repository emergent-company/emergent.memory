package search

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReciprocalRankFusion_TieDeterminism proves that equally-scored RRF entries
// are returned in a stable, deterministic order (ascending by id) across repeated
// calls, eliminating the nondeterminism introduced by iterating a Go map.
func TestReciprocalRankFusion_TieDeterminism(t *testing.T) {
	const entryCount = 200
	const iterations = 50

	// All entries share rank 0 so every id receives the identical RRF score
	// (1/(k+1)); the only ordering signal left is the id tie-break.
	set := rrfResultSet{results: make([]rrfResult, entryCount)}
	for i := 0; i < entryCount; i++ {
		set.results[i] = rrfResult{id: fmt.Sprintf("id-%04d", i), rank: 0}
	}

	var expected []string
	for i := 0; i < entryCount; i++ {
		expected = append(expected, fmt.Sprintf("id-%04d", i))
	}

	for i := 0; i < iterations; i++ {
		merged := reciprocalRankFusion([]rrfResultSet{set}, 60)
		require.Len(t, merged, entryCount, "iteration %d: unexpected result count", i)

		got := make([]string, entryCount)
		for j, scored := range merged {
			got[j] = scored.id
		}
		assert.Equal(t, expected, got, "iteration %d: id order must be deterministic and ascending", i)
	}
}
