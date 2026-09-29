package graph

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// head_resolution_test.go pins the deterministic resolution contract for
// GetByID / GetByIDIncludeDeleted / GetRelationshipByID on a *forked* object:
// a main HEAD and a fork-copy HEAD share one canonical_id, so a lookup that
// carries no branch context must resolve to main, never to the fork copy.
//
// The repository used to pick "the first HEAD" ordered by
// `supersedes_id ASC NULLS FIRST, id ASC`, so the winner was decided by the
// UUID tie-break and flipped when the fork copy's id happened to sort before
// the main HEAD's id. Both orderings are exercised here.

// insertForkHeadObject inserts a HEAD graph object with explicit ids and a
// nullable branch_id (nil = main), so a fixture can control the id ordering of
// a main HEAD and a fork-copy HEAD that share one canonical_id.
func insertForkHeadObject(t *testing.T, db *bun.DB, projectID, id, canonicalID uuid.UUID, branchID *uuid.UUID, typ, key string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, key, status,
			 properties, labels, content_hash, created_at, updated_at)
		VALUES (?, ?, ?, ?, NULL, 1, ?, ?, 'active',
		        '{}'::jsonb, '{}'::text[], ?, NOW(), NOW())
	`, id, projectID, branchID, canonicalID, typ, key, []byte("fork-head-object-hash"))
	require.NoError(t, err)
}

// insertForkHeadRelationship inserts a HEAD relationship with explicit ids and a
// nullable branch_id, mirroring insertForkHeadObject.
func insertForkHeadRelationship(t *testing.T, db *bun.DB, projectID, id, canonicalID, srcID, dstID uuid.UUID, branchID *uuid.UUID, typ string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.graph_relationships
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, src_id, dst_id,
			 properties, content_hash, created_at)
		VALUES (?, ?, ?, ?, NULL, 1, ?, ?, ?, '{}'::jsonb, ?, NOW())
	`, id, projectID, branchID, canonicalID, typ, srcID, dstID, []byte("fork-head-rel-hash"))
	require.NoError(t, err)
}

// orderedPair returns two distinct UUIDs, smaller first, so a caller can assign
// either one to the main HEAD and the other to the fork copy.
func orderedPair() (lo, hi uuid.UUID) {
	a, b := uuid.New(), uuid.New()
	if lessUUID(b, a) {
		a, b = b, a
	}
	return a, b
}

func TestGetByID_ForkedObjectResolvesMainNotBranch(t *testing.T) {
	db := openBulkTestDB(t)
	repo := newBulkTestRepo(t, db)
	ctx := context.Background()

	cases := []struct {
		name        string
		branchFirst bool
	}{
		{name: "branch_id_sorts_first", branchFirst: true},
		{name: "main_id_sorts_first", branchFirst: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projectID := uuid.New()
			seedProject(t, db, projectID)
			t.Cleanup(func() {
				_, _ = db.ExecContext(ctx, "DELETE FROM kb.graph_objects WHERE project_id = ?", projectID)
				_, _ = db.ExecContext(ctx, "DELETE FROM kb.projects WHERE id = ?", projectID)
			})

			canonicalID := uuid.New()
			branchID := uuid.New()
			lo, hi := orderedPair()

			// Default (main_id_sorts_first): main gets the smaller id.
			// branch_id_sorts_first: the fork copy gets the smaller id.
			mainID, forkID := lo, hi
			if tc.branchFirst {
				mainID, forkID = hi, lo
			}
			if tc.branchFirst {
				require.True(t, lessUUID(forkID, mainID), "fixture: fork copy must sort first under id ASC")
			} else {
				require.True(t, lessUUID(mainID, forkID), "fixture: main HEAD must sort first under id ASC")
			}

			insertForkHeadObject(t, db, projectID, mainID, canonicalID, nil, "Forked", "fork-key")
			insertForkHeadObject(t, db, projectID, forkID, canonicalID, &branchID, "Forked", "fork-key")

			// Canonical lookup with no branch context must resolve the main HEAD
			// regardless of which UUID sorts first.
			got, err := repo.GetByID(ctx, projectID, canonicalID)
			require.NoError(t, err)
			assert.Equal(t, mainID, got.ID, "canonical lookup must resolve the main HEAD")
			assert.Nil(t, got.BranchID, "resolved HEAD must be on main")

			// An explicit main physical id still resolves to main.
			got, err = repo.GetByID(ctx, projectID, mainID)
			require.NoError(t, err)
			assert.Equal(t, mainID, got.ID)

			// An explicit fork physical id still wins over the main default.
			got, err = repo.GetByID(ctx, projectID, forkID)
			require.NoError(t, err)
			assert.Equal(t, forkID, got.ID, "explicit fork physical id must win")
			require.NotNil(t, got.BranchID)
			assert.Equal(t, branchID, *got.BranchID)

			// GetByIDIncludeDeleted shares the contract.
			gotDel, err := repo.GetByIDIncludeDeleted(ctx, projectID, canonicalID)
			require.NoError(t, err)
			assert.Equal(t, mainID, gotDel.ID, "include-deleted canonical lookup must resolve the main HEAD")
		})
	}
}

func TestGetRelationshipByID_ForkedRelationshipResolvesMainNotBranch(t *testing.T) {
	db := openBulkTestDB(t)
	repo := newBulkTestRepo(t, db)
	ctx := context.Background()

	cases := []struct {
		name        string
		branchFirst bool
	}{
		{name: "branch_id_sorts_first", branchFirst: true},
		{name: "main_id_sorts_first", branchFirst: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projectID := uuid.New()
			seedProject(t, db, projectID)
			t.Cleanup(func() {
				_, _ = db.ExecContext(ctx, "DELETE FROM kb.graph_relationships WHERE project_id = ?", projectID)
				_, _ = db.ExecContext(ctx, "DELETE FROM kb.graph_objects WHERE project_id = ?", projectID)
				_, _ = db.ExecContext(ctx, "DELETE FROM kb.projects WHERE id = ?", projectID)
			})

			canonicalID := uuid.New()
			branchID := uuid.New()
			srcID := uuid.New()
			dstID := uuid.New()
			lo, hi := orderedPair()

			mainID, forkID := lo, hi
			if tc.branchFirst {
				mainID, forkID = hi, lo
			}

			insertForkHeadRelationship(t, db, projectID, mainID, canonicalID, srcID, dstID, nil, "fork_rel")
			insertForkHeadRelationship(t, db, projectID, forkID, canonicalID, srcID, dstID, &branchID, "fork_rel")

			got, err := repo.GetRelationshipByID(ctx, projectID, canonicalID)
			require.NoError(t, err)
			assert.Equal(t, mainID, got.ID, "canonical lookup must resolve the main HEAD relationship")
			assert.Nil(t, got.BranchID, "resolved relationship HEAD must be on main")

			got, err = repo.GetRelationshipByID(ctx, projectID, forkID)
			require.NoError(t, err)
			assert.Equal(t, forkID, got.ID, "explicit fork physical id must win")
			require.NotNil(t, got.BranchID)
			assert.Equal(t, branchID, *got.BranchID)

			// The canonical-HEAD sibling resolver (no branch context) must also
			// deterministically prefer main.
			head, err := repo.GetRelationshipHeadByCanonicalID(ctx, projectID, canonicalID)
			require.NoError(t, err)
			assert.Equal(t, mainID, head.ID, "GetRelationshipHeadByCanonicalID must prefer the main HEAD")
			assert.Nil(t, head.BranchID)
		})
	}
}
