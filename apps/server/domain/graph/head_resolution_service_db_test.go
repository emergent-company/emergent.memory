package graph_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// head_resolution_service_db_test.go is the service-level fail-first regression
// for issue #1245: a BranchID-less Patch on an object that has been forked to a
// branch must deterministically mutate the *main* HEAD, not the fork copy.
//
// The fixture below seeds a main HEAD and a fork-copy HEAD that share one
// canonical_id, with explicit UUIDs so the caller can force the fork copy to
// sort first under the repository's old `id ASC` tie-break — the exact ordering
// that made the resolution non-deterministic.

// insertForkedObjectFixture seeds one main HEAD and one fork-copy HEAD sharing a
// canonical_id. Callers choose mainID/forkID explicitly to control the id order.
func insertForkedObjectFixture(t *testing.T, ctx context.Context, db bun.IDB, projectID, canonicalID, mainID, forkID, branchID uuid.UUID) {
	t.Helper()
	for _, row := range []struct {
		id       uuid.UUID
		branchID *uuid.UUID
	}{
		{id: mainID, branchID: nil},
		{id: forkID, branchID: &branchID},
	} {
		_, err := db.ExecContext(ctx, `
			INSERT INTO kb.graph_objects
				(id, project_id, branch_id, canonical_id, supersedes_id, version, type, key, status,
				 properties, labels, content_hash, created_at, updated_at)
			VALUES (?, ?, ?, ?, NULL, 1, 'Forked', 'fork-key', 'active',
			        '{}'::jsonb, '{}'::text[], ?, NOW(), NOW())
		`, row.id, projectID, row.branchID, canonicalID, []byte("fork-fixture-hash"))
		require.NoError(t, err)
	}
}

func headVersionAndID(t *testing.T, ctx context.Context, db bun.IDB, projectID, canonicalID uuid.UUID, branchID *uuid.UUID) (int, uuid.UUID) {
	t.Helper()
	q := db.NewRaw(`
		SELECT version, id FROM kb.graph_objects
		WHERE project_id = ? AND canonical_id = ? AND supersedes_id IS NULL
		  AND branch_id IS NULL`, projectID, canonicalID)
	if branchID != nil {
		q = db.NewRaw(`
			SELECT version, id FROM kb.graph_objects
			WHERE project_id = ? AND canonical_id = ? AND supersedes_id IS NULL
			  AND branch_id = ?`, projectID, canonicalID, *branchID)
	}
	var row struct {
		Version int       `bun:"version"`
		ID      uuid.UUID `bun:"id"`
	}
	require.NoError(t, q.Scan(ctx, &row.Version, &row.ID))
	return row.Version, row.ID
}

// TestPatch_BranchIDLessOnForkedObjectTargetsMain is the primary #1245
// regression: no BranchID => main, independent of UUID order.
func TestPatch_BranchIDLessOnForkedObjectTargetsMain(t *testing.T) {
	cases := []struct {
		name        string
		branchFirst bool
	}{
		{name: "branch_id_sorts_first", branchFirst: true},
		{name: "main_id_sorts_first", branchFirst: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, db, _, svc, projectID := setupProvenanceTest(t)

			canonicalID := uuid.New()
			branchID := uuid.New()
			a, b := uuid.New(), uuid.New()
			if lessUUIDBytes(b, a) {
				a, b = b, a
			}
			// Default (main_id_sorts_first): main gets the smaller id.
			// branch_id_sorts_first: the fork copy gets the smaller id.
			mainID, forkID := a, b
			if tc.branchFirst {
				mainID, forkID = b, a
			}
			if tc.branchFirst {
				require.True(t, lessUUIDBytes(forkID, mainID), "fixture: fork copy must sort first")
			} else {
				require.True(t, lessUUIDBytes(mainID, forkID), "fixture: main HEAD must sort first")
			}

			insertForkedObjectFixture(t, ctx, db, projectID, canonicalID, mainID, forkID, branchID)

			_, err := svc.Patch(ctx, projectID, canonicalID, &graph.PatchGraphObjectRequest{
				Properties: map[string]any{"patched": true},
			}, nil)
			require.NoError(t, err)

			mainVersion, _ := headVersionAndID(t, ctx, db, projectID, canonicalID, nil)
			assert.Equal(t, 2, mainVersion, "BranchID-less patch must land on the main HEAD")

			branchVersion, branchHeadID := headVersionAndID(t, ctx, db, projectID, canonicalID, &branchID)
			assert.Equal(t, 1, branchVersion, "fork copy must be untouched")
			assert.Equal(t, forkID, branchHeadID, "fork copy HEAD id must be unchanged")
		})
	}
}

// TestPatch_ExplicitBranchIDTargetsBranch guards the branch-scoped contract: an
// explicit BranchID still targets the fork copy.
func TestPatch_ExplicitBranchIDTargetsBranch(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)

	canonicalID := uuid.New()
	branchID := uuid.New()
	mainID, forkID := uuid.New(), uuid.New()
	insertForkedObjectFixture(t, ctx, db, projectID, canonicalID, mainID, forkID, branchID)

	_, err := svc.Patch(ctx, projectID, canonicalID, &graph.PatchGraphObjectRequest{
		BranchID:   &branchID,
		Properties: map[string]any{"patched": true},
	}, nil)
	require.NoError(t, err)

	mainVersion, _ := headVersionAndID(t, ctx, db, projectID, canonicalID, nil)
	assert.Equal(t, 1, mainVersion, "main must be untouched")

	branchVersion, _ := headVersionAndID(t, ctx, db, projectID, canonicalID, &branchID)
	assert.Equal(t, 2, branchVersion, "explicit BranchID patch must land on the fork copy")
}

// TestPatch_ExplicitForkPhysicalIDTargetsBranch guards that an explicit fork
// physical id still wins over the new main-by-default preference.
func TestPatch_ExplicitForkPhysicalIDTargetsBranch(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)

	canonicalID := uuid.New()
	branchID := uuid.New()
	mainID, forkID := uuid.New(), uuid.New()
	insertForkedObjectFixture(t, ctx, db, projectID, canonicalID, mainID, forkID, branchID)

	_, err := svc.Patch(ctx, projectID, forkID, &graph.PatchGraphObjectRequest{
		Properties: map[string]any{"patched": true},
	}, nil)
	require.NoError(t, err)

	mainVersion, _ := headVersionAndID(t, ctx, db, projectID, canonicalID, nil)
	assert.Equal(t, 1, mainVersion, "main must be untouched")

	branchVersion, _ := headVersionAndID(t, ctx, db, projectID, canonicalID, &branchID)
	assert.Equal(t, 2, branchVersion, "explicit fork physical id must land on the fork copy")
}

// lessUUIDBytes mirrors the bytewise UUID ordering PostgreSQL uses for `id ASC`.
func lessUUIDBytes(a, b uuid.UUID) bool {
	for i := 0; i < len(a); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}
