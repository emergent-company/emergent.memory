package graph

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeSameEntityHead builds a HEAD for the exact-key same-entity tests. Type,
// namespace and key are the natural object identity; props/name/labels drive
// the content comparison classifySameEntity performs.
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

// TestClassifySameEntity pins the merge-time classification of a source object
// that matches a target HEAD by its natural (type, namespace, key) identity but
// carries a different canonical ID — a re-extraction of an entity that already
// exists on the target.
//
// Unlike an ordinary canonical match, identical content is NOT "unchanged": the
// merge still has to record the cross-canonical mapping and collapse the source
// canonical into the target's. Everything that does not genuinely disagree on a
// shared property path is therefore a "fast_forward"; only overlapping property
// paths with different values are a "conflict".
func TestClassifySameEntity(t *testing.T) {
	key := "barry-farber"
	target := makeSameEntityHead(uuid.New(), "Person", key, "Barry Farber", nil,
		map[string]any{"name": "Barry Farber", "role": "radio host"}, nil)

	t.Run("identical content and name fast-forwards", func(t *testing.T) {
		src := makeSameEntityHead(uuid.New(), "Person", key, "Barry Farber", nil,
			map[string]any{"name": "Barry Farber", "role": "radio host"}, nil)

		status, conflicts := classifySameEntity(src, target)
		assert.Equal(t, "fast_forward", status)
		assert.Empty(t, conflicts)
	})

	t.Run("additive properties fast-forward without conflict", func(t *testing.T) {
		// No overlapping property path: the source adds a field the target does
		// not have. That is a clean fast-forward, never a conflict.
		src := makeSameEntityHead(uuid.New(), "Person", key, "Barry Farber", nil,
			map[string]any{"name": "Barry Farber", "role": "radio host", "born": 1927}, nil)

		status, conflicts := classifySameEntity(src, target)
		assert.Equal(t, "fast_forward", status)
		assert.Empty(t, conflicts)
	})

	t.Run("changed non-overlapping properties fast-forward", func(t *testing.T) {
		// The source omits shared keys and adds new ones: nothing disagrees.
		src := makeSameEntityHead(uuid.New(), "Person", key, "Barry Farber", nil,
			map[string]any{"occupation": "radio host"}, nil)

		status, conflicts := classifySameEntity(src, target)
		assert.Equal(t, "fast_forward", status)
		assert.Empty(t, conflicts)
	})

	t.Run("overlapping property path with different values conflicts", func(t *testing.T) {
		src := makeSameEntityHead(uuid.New(), "Person", key, "Barry Farber", nil,
			map[string]any{"name": "Barry Farber", "role": "author"}, nil)

		status, conflicts := classifySameEntity(src, target)
		assert.Equal(t, "conflict", status)
		assert.Equal(t, []string{"/role"}, conflicts)
	})

	t.Run("differing name alone does not conflict", func(t *testing.T) {
		// The title is not a property path: a rename fast-forwards and the write
		// path decides which spelling wins.
		src := makeSameEntityHead(uuid.New(), "Person", key, "Barry Farber (radio host)", nil,
			map[string]any{"name": "Barry Farber", "role": "radio host"}, nil)

		status, conflicts := classifySameEntity(src, target)
		assert.Equal(t, "fast_forward", status)
		assert.Empty(t, conflicts)
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

	// A source with no namespace must only match a target with no namespace: it
	// must not fall through to either namespaced head.
	unnamespaced := makeSameEntityHead(uuid.New(), "Service", key, "Payments", nil, nil, nil)
	assert.Nil(t, findSameEntityTargetInIndex(unnamespaced, index))

	// A nil namespace and an explicitly empty namespace are the same identity.
	empty := ""
	srcEmpty := makeSameEntityHead(uuid.New(), "Service", key, "Payments", &empty, nil, nil)
	unnamespacedTargets := map[uuid.UUID]*BranchObjectHead{
		unnamespaced.CanonicalID: unnamespaced,
	}
	got = findSameEntityTargetInIndex(srcEmpty, buildSameEntityTargetIndex(unnamespacedTargets))
	require.NotNil(t, got)
	assert.Equal(t, unnamespaced.CanonicalID, got.CanonicalID)
}

// TestSameEntityIndexKeysTypeAndNormalizedKey also pins the type dimension and
// key normalization: same key under a different type is a different entity, and
// key comparison is trimmed/case-insensitive.
func TestSameEntityIndexKeysTypeAndNormalizedKey(t *testing.T) {
	key := "payment-service"
	target := makeSameEntityHead(uuid.New(), "Service", key, "Payments", nil, nil, nil)
	index := buildSameEntityTargetIndex(map[uuid.UUID]*BranchObjectHead{
		target.CanonicalID: target,
	})

	sameKeyOtherType := makeSameEntityHead(uuid.New(), "Person", key, "Payments", nil, nil, nil)
	assert.Nil(t, findSameEntityTargetInIndex(sameKeyOtherType, index))

	spaced := "  Payment-Service  "
	normalized := makeSameEntityHead(uuid.New(), "Service", spaced, "Payments", nil, nil, nil)
	got := findSameEntityTargetInIndex(normalized, index)
	require.NotNil(t, got)
	assert.Equal(t, target.CanonicalID, got.CanonicalID)
}

func TestUnionLabels(t *testing.T) {
	assert.Equal(t, []string{"a"}, unionLabels(nil, []string{"a"}))
	assert.Equal(t, []string{"a", "b", "c"}, unionLabels([]string{"a", "b"}, []string{"b", "c"}))
	assert.Equal(t, []string{"a"}, unionLabels([]string{"a"}, nil))
	assert.Equal(t, []string{"a", "b"}, unionLabels(nil, []string{"a", "b"}))
	assert.Empty(t, unionLabels(nil, nil))
}

// TestSameEntityIdentityProperty pins the identity-protection boundary for the
// opt-in partial_newest drift resolution (#1222): properties["name"] mirrors the
// entity's natural identity, so a drift policy must refuse to overwrite it and
// leave the object for review. Every other property is drift.
func TestSameEntityIdentityProperty(t *testing.T) {
	assert.True(t, sameEntityIdentityProperty("/name"))
	assert.True(t, sameEntityIdentityProperty("name"), "a bare path is accepted too")
	assert.False(t, sameEntityIdentityProperty("/description"))
	assert.False(t, sameEntityIdentityProperty("/role"))
	assert.False(t, sameEntityIdentityProperty("/name_long"), "prefix matches are not identity")
}

func TestHasSameEntityIdentityConflict(t *testing.T) {
	assert.True(t, hasSameEntityIdentityConflict([]string{"/role", "/name"}),
		"one identity path must block auto-resolution of the whole object")
	assert.False(t, hasSameEntityIdentityConflict([]string{"/role", "/description"}))
	assert.False(t, hasSameEntityIdentityConflict(nil))
}

// TestPartialNewestPresetPinsSettings keeps the opt-in drift policy explicit:
// it is a distinct preset (partial keeps preserve_target), similarity stays
// suggest-only, and the new canonical-conflict action is "newest".
func TestPartialNewestPresetPinsSettings(t *testing.T) {
	got := resolveMergePolicy("partial_newest", "", 0)
	require.True(t, got.SimilarityEnabled)
	assert.Equal(t, "suggest", got.SimilarityAction, "similar duplicates still need a reviewer")
	assert.Equal(t, "newest", got.CanonicalConflictAction)
	assert.Equal(t, mergePolicyPresets["partial_newest"], got)

	partial := resolveMergePolicy("partial", "", 0)
	assert.Equal(t, "preserve_target", partial.CanonicalConflictAction,
		"the default partial policy must keep leaving same-entity drift for review")

	require.NoError(t, validateMergePolicy("partial_newest", ""))
	assert.Contains(t, MergePolicyNames(), "partial_newest")
}
