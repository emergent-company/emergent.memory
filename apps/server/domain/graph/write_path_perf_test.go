package graph

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// overlay applies a Patch-style delta to old: non-nil delta values overwrite
// (or add) the key, nil delta values delete it. This mirrors the merge in
// Patch so merged is always "old overlaid by delta".
func overlay(old map[string]any, delta map[string]any) map[string]any {
	merged := make(map[string]any, len(old)+len(delta))
	for k, v := range old {
		merged[k] = v
	}
	for k, v := range delta {
		if v == nil {
			delete(merged, k)
		} else {
			merged[k] = v
		}
	}
	return merged
}

// assertSummaryEqual compares a computeChangeSummaryFromDelta result to the
// reference computeChangeSummary result. `paths` and `removed` are unordered
// (built from map iteration), so they are compared element-wise; the rest are
// compared structurally.
func assertSummaryEqual(t *testing.T, full, delta map[string]any) {
	t.Helper()
	if full == nil {
		require.Nil(t, delta, "expected nil summary, got %v", delta)
		return
	}
	require.NotNil(t, delta, "expected non-nil summary")

	require.Equal(t, full["added"], delta["added"])
	require.Equal(t, full["updated"], delta["updated"])
	require.ElementsMatch(t, full["removed"].([]string), delta["removed"].([]string))
	require.ElementsMatch(t, full["paths"].([]string), delta["paths"].([]string))
	require.Equal(t, full["meta"], delta["meta"])
}

func TestComputeChangeSummaryFromDelta_MatchesFullDiff(t *testing.T) {
	cases := []struct {
		name  string
		old   map[string]any
		delta map[string]any
	}{
		{
			name:  "add",
			old:   map[string]any{},
			delta: map[string]any{"a": 1},
		},
		{
			name:  "update",
			old:   map[string]any{"a": 1},
			delta: map[string]any{"a": 2},
		},
		{
			name:  "remove",
			old:   map[string]any{"a": 1},
			delta: map[string]any{"a": nil},
		},
		{
			name:  "no-op",
			old:   map[string]any{"a": 1},
			delta: map[string]any{"a": 1},
		},
		{
			name:  "mixed",
			old:   map[string]any{"a": 1, "b": 2, "c": 3},
			delta: map[string]any{"a": 10, "b": nil, "d": 4},
		},
		{
			name:  "nil-delta",
			old:   map[string]any{"a": 1},
			delta: nil,
		},
		{
			name:  "delete-absent-key",
			old:   map[string]any{},
			delta: map[string]any{"a": nil},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			merged := overlay(tc.old, tc.delta)
			full := computeChangeSummary(tc.old, merged)
			delta := computeChangeSummaryFromDelta(tc.old, tc.delta, merged)
			assertSummaryEqual(t, full, delta)
		})
	}
}

func TestComputeChangeSummaryFromDelta_IgnoresNonDeltaKeys(t *testing.T) {
	old := map[string]any{
		"a": 1, "b": 2, "c": 3, "d": 4, "e": 5,
		"f": 6, "g": 7, "h": 8, "i": 9, "j": 10,
	}
	delta := map[string]any{"e": 99}
	merged := overlay(old, delta)

	summary := computeChangeSummaryFromDelta(old, delta, merged)
	require.NotNil(t, summary)
	require.Len(t, summary["paths"].([]string), 1, "only the touched key may appear")
	require.Equal(t, "/e", summary["paths"].([]string)[0])
}

func TestComputeContentHash_UnchangedByKeyOrder(t *testing.T) {
	m1 := map[string]any{"a": 1, "b": 2, "c": 3}
	m2 := map[string]any{"c": 3, "a": 1, "b": 2}
	m3 := map[string]any{"a": 1, "b": 2, "c": 4}

	require.Equal(t,
		computeContentHash(m1, nil, nil, nil),
		computeContentHash(m2, nil, nil, nil),
		"key insertion order must not change the hash")

	require.NotEqual(t,
		computeContentHash(m1, nil, nil, nil),
		computeContentHash(m3, nil, nil, nil),
		"a value change must change the hash")
}

// BenchmarkChangeSummaryFullVsDelta shows the per-write work removed from the
// Patch path: on a large object, a one-key no-op delta must not diff/marshal the
// whole property map (#1478).
func BenchmarkChangeSummaryFullVsDelta(b *testing.B) {
	const n = 5000
	old := make(map[string]any, n)
	for i := 0; i < n; i++ {
		old[fmt.Sprintf("k%04d", i)] = i
	}
	delta := map[string]any{"k0000": 0} // same value → no-op from a summary POV
	merged := overlay(old, delta)

	b.Run("full-diff", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = computeChangeSummary(old, merged)
		}
	})
	b.Run("delta-diff", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = computeChangeSummaryFromDelta(old, delta, merged)
		}
	})
}
