package graph

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeSameEntityHead builds a target/source HEAD for the exact-key identity
// tests. Type and key are the natural object identity; props/name/labels drive
// the content comparison classifyMergeObject performs.
func makeSameEntityHead(canonicalID uuid.UUID, typ, key, name string, ns *string, props map[string]any, labels []string) *BranchObjectHead {
	if props == nil {
		props = map[string]any{}
	}
	if labels == nil {
		labels = []string{}
	}
	return &BranchObjectHead{
		CanonicalID: canonicalID,
		ID:          uuid.New(),
		Type:        typ,
		Name:        &name,
		Key:         &key,
		Namespace:   ns,
		Properties:  props,
		Labels:      labels,
		ContentHash: computeContentHash(props, nil, &key, labels),
	}
}

// TestApplySameEntityMatch pins the merge-time identity rule from #1186: a
// source object that would otherwise be "added" but shares the natural
// (type, namespace, key) identity with an existing target head is the same
// entity re-extracted onto a staging branch. It must be classified against that
// head rather than left as a "similar" duplicate for review.
func TestApplySameEntityMatch(t *testing.T) {
	key := "barry-farber"
	targetID := uuid.MustParse("11111111-0000-0000-0000-000000000000")
	target := makeSameEntityHead(targetID, "Person", key, "Barry Farber", nil, map[string]any{"name": "Barry Farber"}, nil)

	t.Run("identical_content_is_unchanged", func(t *testing.T) {
		src := makeSameEntityHead(uuid.New(), "Person", key, "Barry Farber", nil, map[string]any{"name": "Barry Farber"}, nil)
		summary := &BranchMergeObjectSummary{CanonicalID: src.CanonicalID, Status: "added"}

		require.True(t, applySameEntityMatch(summary, src, target))
		assert.Equal(t, "unchanged", summary.Status)
		require.NotNil(t, summary.SameEntityTargetID)
		assert.Equal(t, target.CanonicalID, *summary.SameEntityTargetID)
		require.NotNil(t, summary.TargetHeadID)
		assert.Equal(t, target.ID, *summary.TargetHeadID)
		assert.Empty(t, summary.Conflicts)
	})

	t.Run("additive_content_is_fast_forward", func(t *testing.T) {
		src := makeSameEntityHead(uuid.New(), "Person", key, "Barry Farber", nil,
			map[string]any{"name": "Barry Farber", "occupation": "radio host"}, nil)
		summary := &BranchMergeObjectSummary{CanonicalID: src.CanonicalID, Status: "added"}

		require.True(t, applySameEntityMatch(summary, src, target))
		assert.Equal(t, "fast_forward", summary.Status)
		require.NotNil(t, summary.SameEntityTargetID)
		assert.Equal(t, target.CanonicalID, *summary.SameEntityTargetID)
		assert.Empty(t, summary.Conflicts)
	})

	t.Run("differing_shared_value_is_conflict", func(t *testing.T) {
		src := makeSameEntityHead(uuid.New(), "Person", key, "Barry Farber", nil,
			map[string]any{"name": "Barry Farber", "status": "active"}, nil)
		// Target shares the "name" key with the same value but adds a status
		// the source disagrees with.
		conflictingTarget := makeSameEntityHead(targetID, "Person", key, "Barry Farber", nil,
			map[string]any{"name": "Barry Farber", "status": "retired"}, nil)
		summary := &BranchMergeObjectSummary{CanonicalID: src.CanonicalID, Status: "added"}

		require.True(t, applySameEntityMatch(summary, src, conflictingTarget))
		assert.Equal(t, "conflict", summary.Status)
		assert.Equal(t, []string{"/status"}, summary.Conflicts)
		require.NotNil(t, summary.SameEntityTargetID)
		assert.Equal(t, conflictingTarget.CanonicalID, *summary.SameEntityTargetID)
	})

	t.Run("non_added_status_is_left_alone", func(t *testing.T) {
		src := makeSameEntityHead(uuid.New(), "Person", key, "Barry Farber", nil, nil, nil)
		summary := &BranchMergeObjectSummary{CanonicalID: src.CanonicalID, Status: "similar"}

		assert.False(t, applySameEntityMatch(summary, src, target))
		assert.Equal(t, "similar", summary.Status)
		assert.Nil(t, summary.SameEntityTargetID)
	})

	t.Run("nil_match_is_noop", func(t *testing.T) {
		src := makeSameEntityHead(uuid.New(), "Person", key, "Barry Farber", nil, nil, nil)
		summary := &BranchMergeObjectSummary{CanonicalID: src.CanonicalID, Status: "added"}

		assert.False(t, applySameEntityMatch(summary, src, nil))
		assert.Equal(t, "added", summary.Status)
		assert.Nil(t, summary.SameEntityTargetID)
	})
}

// TestSameEntityIndexSeparatesNamespaces guards the identity dimensions: the
// repository resolves an object by (type, key, namespace), so two heads that
// share a type and key in different namespaces are different entities and must
// not be merged into each other.
func TestSameEntityIndexSeparatesNamespaces(t *testing.T) {
	key := "payment-service"
	billing, platform := "billing", "platform"
	inBilling := makeSameEntityHead(uuid.New(), "Service", key, "Payments", &billing, nil, nil)
	inPlatform := makeSameEntityHead(uuid.New(), "Service", key, "Payments", &platform, nil, nil)
	targets := map[uuid.UUID]*BranchObjectHead{
		inBilling.CanonicalID:  inBilling,
		inPlatform.CanonicalID: inPlatform,
	}
	index := buildSameEntityTargetIndex(targets)

	src := makeSameEntityHead(uuid.New(), "Service", key, "Payments", &billing, nil, nil)
	got := findSameEntityTargetInIndex(src, index)
	require.NotNil(t, got)
	assert.Equal(t, inBilling.CanonicalID, got.CanonicalID)

	srcPlatform := makeSameEntityHead(uuid.New(), "Service", key, "Payments", &platform, nil, nil)
	got = findSameEntityTargetInIndex(srcPlatform, index)
	require.NotNil(t, got)
	assert.Equal(t, inPlatform.CanonicalID, got.CanonicalID)

	// A source with no namespace must only match a target with no namespace.
	unnamespaced := makeSameEntityHead(uuid.New(), "Service", key, "Payments", nil, nil, nil)
	assert.Nil(t, findSameEntityTargetInIndex(unnamespaced, index))
}

func TestUnionLabels(t *testing.T) {
	assert.Equal(t, []string{"a"}, unionLabels(nil, []string{"a"}))
	assert.Equal(t, []string{"a", "b", "c"}, unionLabels([]string{"a", "b"}, []string{"b", "c"}))
	assert.Equal(t, []string{"a"}, unionLabels([]string{"a"}, nil))
	assert.Empty(t, unionLabels(nil, nil))
}
