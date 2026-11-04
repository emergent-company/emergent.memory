package graph

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// TestClassifyMergeObject exercises the pure merge classification, including
// the "merged" status driven by the merge ledger (MergedToCanonicalID).
func TestClassifyMergeObject(t *testing.T) {
	t.Run("merged_via_ledger", func(t *testing.T) {
		cid := uuid.New()
		mergedTo := uuid.New()
		src := makeBranchHead(cid, map[string]any{"name": "svc"}, nil, nil, nil)
		src.MergedToCanonicalID = &mergedTo

		status, conflicts := classifyMergeObject(src, nil)
		assert.Equal(t, "merged", status)
		assert.Empty(t, conflicts)
	})

	t.Run("added_source_only", func(t *testing.T) {
		cid := uuid.New()
		src := makeBranchHead(cid, map[string]any{"name": "svc"}, nil, nil, nil)

		status, _ := classifyMergeObject(src, nil)
		assert.Equal(t, "added", status)
	})

	t.Run("unchanged_target_only", func(t *testing.T) {
		cid := uuid.New()
		tgt := makeBranchHead(cid, map[string]any{"name": "svc"}, nil, nil, nil)

		status, _ := classifyMergeObject(nil, tgt)
		assert.Equal(t, "unchanged", status)
	})

	t.Run("unchanged_identical", func(t *testing.T) {
		cid := uuid.New()
		props := map[string]any{"name": "svc"}
		src := makeBranchHead(cid, props, nil, nil, nil)
		tgt := makeBranchHead(cid, props, nil, nil, nil)

		status, _ := classifyMergeObject(src, tgt)
		assert.Equal(t, "unchanged", status)
	})

	t.Run("fast_forward_additive", func(t *testing.T) {
		cid := uuid.New()
		src := makeBranchHead(cid, map[string]any{"name": "svc", "version": 2}, nil, nil, nil)
		tgt := makeBranchHead(cid, map[string]any{"name": "svc"}, nil, nil, nil)

		status, conflicts := classifyMergeObject(src, tgt)
		assert.Equal(t, "fast_forward", status)
		assert.Empty(t, conflicts)
	})

	t.Run("conflict", func(t *testing.T) {
		cid := uuid.New()
		src := makeBranchHead(cid, map[string]any{"status": "active"}, nil, nil, nil)
		tgt := makeBranchHead(cid, map[string]any{"status": "deprecated"}, nil, nil, nil)

		status, conflicts := classifyMergeObject(src, tgt)
		assert.Equal(t, "conflict", status)
		assert.Equal(t, []string{"/status"}, conflicts)
	})

	t.Run("deleted_on_source", func(t *testing.T) {
		cid := uuid.New()
		src := makeBranchHead(cid, map[string]any{"name": "svc"}, nil, nil, nil)
		ts := time.Now()
		src.DeletedAt = &ts
		tgt := makeBranchHead(cid, map[string]any{"name": "svc"}, nil, nil, nil)

		status, _ := classifyMergeObject(src, tgt)
		assert.Equal(t, "deleted", status)
	})

	t.Run("deleted_on_source_not_on_target", func(t *testing.T) {
		cid := uuid.New()
		src := makeBranchHead(cid, map[string]any{"name": "svc"}, nil, nil, nil)
		ts := time.Now()
		src.DeletedAt = &ts

		status, _ := classifyMergeObject(src, nil)
		assert.Equal(t, "unchanged", status)
	})
}
