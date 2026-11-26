package extraction

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestBranchSweepReapsStaleBranches pins that the retention sweep soft-deletes a
// stale unmerged extraction branch's objects and relationships and removes the
// branch, while leaving a fresh branch and non-extraction branches alone.
func TestBranchSweepReapsStaleBranches(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)

	branch := func(origin string, createdAgo time.Duration) string {
		id := uuid.NewString()
		_, err := db.NewRaw(`INSERT INTO kb.branches (id, project_id, name, origin, created_at)
			VALUES (?, ?, ?, ?, now() - (? * interval '1 second'))`, id, projectID, "b-"+id[:8], origin, createdAgo.Seconds()).Exec(ctx)
		require.NoError(t, err)
		return id
	}
	object := func(branchID string) string {
		id := uuid.NewString()
		_, err := db.NewRaw(`INSERT INTO kb.graph_objects
			(id, project_id, canonical_id, version, type, name, status, properties, labels, branch_id, created_at, updated_at)
			VALUES (?, ?, ?, 1, 'Thing', ?, 'active', '{}'::jsonb, '{}'::text[], ?, now(), now())`,
			id, projectID, id, "obj-"+id[:8], branchID).Exec(ctx)
		require.NoError(t, err)
		return id
	}
	relationship := func(branchID, srcID, dstID string) string {
		id := uuid.NewString()
		_, err := db.NewRaw(`INSERT INTO kb.graph_relationships
			(id, project_id, canonical_id, version, type, src_id, dst_id, properties, branch_id, created_at)
			VALUES (?, ?, ?, 1, 'RELATES_TO', ?, ?, '{}'::jsonb, ?, now())`,
			id, projectID, id, srcID, dstID, branchID).Exec(ctx)
		require.NoError(t, err)
		return id
	}

	stale := branch("extraction", 2*time.Hour)
	staleObj := object(stale)
	staleRel := relationship(stale, staleObj, staleObj)

	fresh := branch("extraction", time.Minute)
	_ = object(fresh)

	manual := branch("manual", 2*time.Hour)
	_ = object(manual)

	worker := NewBranchSweepWorker(db, quietLogger(), &BranchSweepConfig{
		Interval:  time.Minute,
		Retention: time.Hour,
	})

	res, err := worker.Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, res.BranchesReaped, "only the stale extraction branch is reaped")
	require.Equal(t, 1, res.ObjectsDeleted)
	require.Equal(t, 1, res.RelationshipsDeleted)

	// Stale branch row is gone and its object soft-deleted.
	var branchCount int
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.branches WHERE id = ?`, stale).Scan(ctx, &branchCount))
	require.Equal(t, 0, branchCount)

	var deletedAt *time.Time
	require.NoError(t, db.NewRaw(`SELECT deleted_at FROM kb.graph_objects WHERE id = ?`, staleObj).Scan(ctx, &deletedAt))
	require.NotNil(t, deletedAt, "stale branch object must be soft-deleted")

	var relDeletedAt *time.Time
	require.NoError(t, db.NewRaw(`SELECT deleted_at FROM kb.graph_relationships WHERE id = ?`, staleRel).Scan(ctx, &relDeletedAt))
	require.NotNil(t, relDeletedAt, "stale branch relationship must be soft-deleted")

	// Fresh extraction branch and the manual branch are untouched.
	var kept int
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.branches WHERE id = ? OR id = ?`, fresh, manual).Scan(ctx, &kept))
	require.Equal(t, 2, kept)
}

// TestBranchSweepNonPositiveRetentionDisabled pins that a non-positive retention
// disables the sweep (nothing is reaped).
func TestBranchSweepNonPositiveRetentionDisabled(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)

	id := uuid.NewString()
	_, err := db.NewRaw(`INSERT INTO kb.branches (id, project_id, name, origin, created_at)
		VALUES (?, ?, ?, 'extraction', now() - interval '2 hours')`, id, projectID, "stale").Exec(ctx)
	require.NoError(t, err)

	worker := NewBranchSweepWorker(db, quietLogger(), &BranchSweepConfig{Interval: time.Minute, Retention: 0})
	res, err := worker.Sweep(ctx)
	require.NoError(t, err)
	require.Zero(t, res.BranchesReaped)

	var count int
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.branches WHERE id = ?`, id).Scan(ctx, &count))
	require.Equal(t, 1, count, "non-positive retention must reap nothing")
}
