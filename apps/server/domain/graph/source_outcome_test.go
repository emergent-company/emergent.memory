package graph

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSourceOutcomeTransitions pins the legal edges of the outcome state
// machine. The table is the review surface for "what may a later decision say
// about a record": a state added to the constants without an entry here would
// make every write a no-op, so the pairs are asserted exhaustively.
func TestSourceOutcomeTransitions(t *testing.T) {
	all := []string{
		SourceOutcomeMaterialized,
		SourceOutcomeMerged,
		SourceOutcomeAdopted,
		SourceOutcomeWithdrawn,
		SourceOutcomeSkipped,
	}

	allowed := map[string]map[string]bool{
		SourceOutcomeMaterialized: {
			SourceOutcomeMerged:    true,
			SourceOutcomeAdopted:   true,
			SourceOutcomeWithdrawn: true,
			SourceOutcomeSkipped:   true,
		},
		SourceOutcomeSkipped: {
			SourceOutcomeMaterialized: true,
			SourceOutcomeMerged:       true,
			SourceOutcomeAdopted:      true,
			SourceOutcomeWithdrawn:    true,
		},
		SourceOutcomeWithdrawn: {
			SourceOutcomeMaterialized: true,
		},
		SourceOutcomeAdopted: {
			// A re-push that adopts the row back materializes the record again
			// (changeset.go writes materialized on the adopt-back path), so the
			// table must allow it rather than silently keep a stale adopter.
			SourceOutcomeMaterialized: true,
			SourceOutcomeMerged:       true,
			SourceOutcomeWithdrawn:    true,
		},
		SourceOutcomeMerged: {
			SourceOutcomeMaterialized: true,
			SourceOutcomeMerged:       true,
		},
	}

	for _, from := range all {
		for _, to := range all {
			want := allowed[from][to]
			if from == to {
				// Staying in the same state is always allowed: a repeated
				// decision updates the facts without breaking the machine.
				want = true
			}
			assert.Equalf(t, want, SourceOutcomeTransitionAllowed(from, to),
				"transition %s -> %s", from, to)
		}
	}

	// An empty from means "no row yet": any first state is legal.
	for _, to := range all {
		assert.True(t, SourceOutcomeTransitionAllowed("", to), "first write as %s", to)
	}
}

// TestSourceOutcomeCursorIsOpaqueAndReversible: the cursor is a versioned,
// opaque encoding of the sequence value. Garbage is a bad request rather than a
// silent reset to the beginning, so a typo cannot replay the table.
func TestSourceOutcomeCursorIsOpaqueAndReversible(t *testing.T) {
	for _, seq := range []int64{0, 1, 42, 1 << 40} {
		cursor := encodeSourceOutcomeCursor(seq)
		assert.NotContains(t, cursor, "so1", "the cursor must not expose its encoding")
		got, err := decodeSourceOutcomeCursor(cursor)
		require.NoError(t, err)
		assert.Equal(t, seq, got)
	}

	assert.NotEqual(t, encodeSourceOutcomeCursor(1), encodeSourceOutcomeCursor(2))

	_, err := decodeSourceOutcomeCursor("not-a-cursor")
	require.Error(t, err)

	// A base64 payload without the version prefix is rejected, not treated as
	// the first page.
	_, err = decodeSourceOutcomeCursor(encodeSourceOutcomeCursor(1)[1:])
	require.Error(t, err)
}

// TestSourceOutcomeUnchanged covers the no-op rule: a repeated decision that
// states the same facts must not consume a sequence value (and so must not make
// a source re-read an outcome that says the same thing).
func TestSourceOutcomeUnchanged(t *testing.T) {
	canonical := uuid.New()
	adopter := uuid.New()
	existing := &SourceOutcome{
		State:               SourceOutcomeAdopted,
		CanonicalID:         &canonical,
		ExternalRevision:    "r2",
		ExternalRevisionSeq: 2,
		Generation:          7,
		AdoptedBySourceID:   &adopter,
		Reason:              "why",
	}

	same := *existing
	assert.True(t, sourceOutcomeUnchanged(existing, &same))

	cases := map[string]func(o *SourceOutcome){
		"state":     func(o *SourceOutcome) { o.State = SourceOutcomeMerged },
		"canonical": func(o *SourceOutcome) { other := uuid.New(); o.CanonicalID = &other },
		"revision":  func(o *SourceOutcome) { o.ExternalRevision = "r3" },
		"seq":       func(o *SourceOutcome) { o.ExternalRevisionSeq = 3 },
		"generation": func(o *SourceOutcome) {
			o.Generation = 8
		},
		"adopter": func(o *SourceOutcome) { other := uuid.New(); o.AdoptedBySourceID = &other },
		"reason":  func(o *SourceOutcome) { o.Reason = "other" },
		"nil-canonical": func(o *SourceOutcome) {
			o.CanonicalID = nil
		},
	}
	for name, mutate := range cases {
		next := *existing
		mutate(&next)
		assert.Falsef(t, sourceOutcomeUnchanged(existing, &next), "%s must count as a change", name)
	}
}
