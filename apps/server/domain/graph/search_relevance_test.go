package graph

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// Tests for the relevance-floor and lexical-first-ranking helpers used by
// object search (#244): the VectorSearch semantic path applies a default max
// distance from the similarity floor, the HybridSearch vector leg drops below-
// floor matches, and hybrid fusion sorts lexical term matches ahead of
// vector-only matches.

func TestResolveMaxDistance(t *testing.T) {
	explicit := float32(0.3)
	half := float32(0.5)

	tests := []struct {
		name          string
		explicit      *float32
		minSimilarity float32
		want          *float32
	}{
		{name: "explicit max distance wins", explicit: &explicit, minSimilarity: 0.5, want: &explicit},
		{name: "floor translates to distance", explicit: nil, minSimilarity: 0.5, want: &half},
		{name: "floor disabled leaves no bound", explicit: nil, minSimilarity: 0, want: nil},
		{name: "negative floor leaves no bound", explicit: nil, minSimilarity: -1, want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveMaxDistance(tt.explicit, tt.minSimilarity)
			if tt.want == nil {
				assert.Nil(t, got)
				return
			}
			requireNonNilFloat(t, got)
			assert.InDelta(t, *tt.want, *got, 1e-6)
		})
	}
}

func requireNonNilFloat(t *testing.T, v *float32) {
	t.Helper()
	if v == nil {
		t.Fatal("expected non-nil *float32")
	}
}

func TestAboveSimilarityFloor(t *testing.T) {
	tests := []struct {
		name     string
		distance float32
		floor    float32
		want     bool
	}{
		{name: "above floor", distance: 0.2, floor: 0.5, want: true},  // similarity 0.8
		{name: "below floor", distance: 0.6, floor: 0.5, want: false}, // similarity 0.4
		{name: "at boundary", distance: 0.5, floor: 0.5, want: true},  // similarity 0.5
		{name: "floor zero disables", distance: 0.99, floor: 0, want: true},
		{name: "floor negative disables", distance: 0.99, floor: -1, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, aboveSimilarityFloor(tt.distance, tt.floor))
		})
	}
}

func TestFusedLessLexicalFirst(t *testing.T) {
	idLow := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	idHigh := uuid.MustParse("00000000-0000-0000-0000-000000000002")

	tests := []struct {
		name       string
		aLex, bLex bool
		aScore     float32
		bScore     float32
		aID, bID   uuid.UUID
		want       bool
	}{
		{
			name: "lexical match outranks vector-only despite lower score",
			aLex: true, bLex: false,
			aScore: 0.1, bScore: 0.9,
			aID: idLow, bID: idHigh,
			want: true,
		},
		{
			name: "vector-only never outranks lexical",
			aLex: false, bLex: true,
			aScore: 0.9, bScore: 0.1,
			aID: idLow, bID: idHigh,
			want: false,
		},
		{
			name: "both lexical prefer higher score",
			aLex: true, bLex: true,
			aScore: 0.9, bScore: 0.1,
			aID: idLow, bID: idHigh,
			want: true,
		},
		{
			name: "both vector-only prefer higher score",
			aLex: false, bLex: false,
			aScore: 0.1, bScore: 0.9,
			aID: idLow, bID: idHigh,
			want: false,
		},
		{
			name: "equal score and lexical breaks by ascending id",
			aLex: true, bLex: true,
			aScore: 0.5, bScore: 0.5,
			aID: idLow, bID: idHigh,
			want: true,
		},
		{
			name: "equal score and lexical descending id is not less",
			aLex: true, bLex: true,
			aScore: 0.5, bScore: 0.5,
			aID: idHigh, bID: idLow,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, fusedLess(tt.aLex, tt.bLex, tt.aScore, tt.bScore, tt.aID, tt.bID))
		})
	}
}
