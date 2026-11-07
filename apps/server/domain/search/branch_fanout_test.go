package search

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func graphResult(objectID, canonicalID string, score float32) *UnifiedSearchGraphResult {
	return &UnifiedSearchGraphResult{
		Type:        ItemTypeGraph,
		ObjectID:    objectID,
		CanonicalID: canonicalID,
		Score:       score,
	}
}

func TestFuseScopedGraphResultsPrefersMain(t *testing.T) {
	main := []*UnifiedSearchGraphResult{
		graphResult("main-1", "c-merged", 0.9),
		graphResult("main-2", "c-main", 0.8),
	}
	branch := []*UnifiedSearchGraphResult{
		graphResult("branch-1", "c-merged", 0.95), // same entity as main-1
		graphResult("branch-2", "c-pending", 0.7),
	}

	got := fuseScopedGraphResults([][]*UnifiedSearchGraphResult{main, branch}, 60, 10)

	require.Len(t, got, 3, "the canonical id present on both sides must appear once")
	assert.Equal(t, "main-1", got[0].ObjectID, "merged fact wins the collision and the main source ranks first")
	assert.Equal(t, "main-2", got[1].ObjectID)
	assert.Equal(t, "branch-2", got[2].ObjectID)
	for i, r := range got {
		assert.Equal(t, i+1, r.Rank, "rank must be rewritten to the fused order")
	}
	// Native scores are preserved so a min-score cut-off stays meaningful.
	assert.Equal(t, float32(0.9), got[0].Score)
	assert.Equal(t, float32(0.7), got[2].Score)
}

func TestFuseScopedGraphResultsInterleavesByRank(t *testing.T) {
	main := []*UnifiedSearchGraphResult{
		graphResult("m0", "cm0", 0.9),
		graphResult("m1", "cm1", 0.8),
	}
	branchA := []*UnifiedSearchGraphResult{
		graphResult("a0", "ca0", 0.99),
		graphResult("a1", "ca1", 0.98),
	}
	branchB := []*UnifiedSearchGraphResult{
		graphResult("b0", "cb0", 0.5),
	}

	got := fuseScopedGraphResults([][]*UnifiedSearchGraphResult{main, branchA, branchB}, 60, 10)

	require.Len(t, got, 5)
	assert.Equal(t, []string{"m0", "a0", "b0", "m1", "a1"}, []string{
		got[0].ObjectID, got[1].ObjectID, got[2].ObjectID, got[3].ObjectID, got[4].ObjectID,
	}, "rank 0 across sources ties and main breaks the tie; a branch's native score does not promote it")
}

func TestFuseScopedGraphResultsAppliesLimit(t *testing.T) {
	main := []*UnifiedSearchGraphResult{graphResult("m0", "cm0", 0.9), graphResult("m1", "cm1", 0.8)}
	branch := []*UnifiedSearchGraphResult{graphResult("b0", "cb0", 0.7)}

	got := fuseScopedGraphResults([][]*UnifiedSearchGraphResult{main, branch}, 60, 2)
	require.Len(t, got, 2)
	assert.Equal(t, 1, got[0].Rank)
	assert.Equal(t, 2, got[1].Rank)
}

func TestFuseScopedGraphResultsMainOnlyIsUnchanged(t *testing.T) {
	main := []*UnifiedSearchGraphResult{
		graphResult("m0", "cm0", 0.9),
		graphResult("m1", "cm1", 0.8),
		graphResult("m2", "cm2", 0.7),
	}
	got := fuseScopedGraphResults([][]*UnifiedSearchGraphResult{main}, 60, 32)
	require.Len(t, got, 3)
	for i := range main {
		assert.Same(t, main[i], got[i], "a one-source fusion must preserve order, not re-sort")
		assert.Equal(t, i+1, got[i].Rank)
	}
}

func TestAttachBranchProvenance(t *testing.T) {
	label := "Invoice.pdf"
	target := &BranchTarget{Name: "extraction/abc/def", Origin: "extraction", OriginLabel: &label}
	_ = target.ID // uuid.Nil is fine here; the assertion below checks the rendered string
	results := []*UnifiedSearchGraphResult{graphResult("o", "c", 0.5)}

	attachBranchProvenance(results, target)

	require.NotNil(t, results[0].BranchID)
	assert.Equal(t, "00000000-0000-0000-0000-000000000000", *results[0].BranchID)
	assert.Equal(t, "extraction/abc/def", *results[0].BranchName)
	assert.Equal(t, "extraction", *results[0].BranchOrigin)
	require.NotNil(t, results[0].BranchOriginLabel)
	assert.Equal(t, "Invoice.pdf", *results[0].BranchOriginLabel)
}

func TestAttachBranchProvenanceWithoutLabel(t *testing.T) {
	target := &BranchTarget{Name: "plan/q3", Origin: "manual"}
	results := []*UnifiedSearchGraphResult{graphResult("o", "c", 0.5)}

	attachBranchProvenance(results, target)

	assert.Nil(t, results[0].BranchOriginLabel)
}

func TestGraphObjectIDsSkipsMalformed(t *testing.T) {
	results := []*UnifiedSearchGraphResult{
		graphResult("11111111-1111-1111-1111-111111111111", "c1", 0.5),
		graphResult("not-a-uuid", "c2", 0.4),
	}
	assert.Len(t, graphObjectIDs(results), 1)
}
