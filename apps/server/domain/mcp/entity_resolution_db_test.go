package mcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/branchscope"
)

// TestResolveEntityIDByKeyPrefersMain is the fail-first regression for issue
// #331: after a branch merge, a key that exists on both the merged main copy and
// the branch original must resolve to the main copy when no branch is named.
// The branch copy is seeded with a higher version so the pre-fix
// ORDER BY version DESC deterministically returns the branch copy.
func TestResolveEntityIDByKeyPrefersMain(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "mcpkeyresolve")
	t.Cleanup(tdb.Close)

	ctx := context.Background()
	db := tdb.DB
	svc := &Service{db: db}

	orgID := uuid.New()
	_, err := db.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "key-org")
	require.NoError(t, err)
	projectID := uuid.New()
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`,
		projectID, orgID, "key-project")
	require.NoError(t, err)

	branchID := uuid.New()
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.branches (id, project_id, name, origin) VALUES (?, ?, ?, 'manual')`,
		branchID, projectID, "migration/v4")
	require.NoError(t, err)

	const key = "veh-camping-trailer"
	mainCanonical := uuid.New()
	branchCanonical := uuid.New()

	insertObject := func(id, canonical uuid.UUID, version int, branch *uuid.UUID) {
		t.Helper()
		_, err := db.ExecContext(ctx, `
			INSERT INTO kb.graph_objects
				(id, project_id, branch_id, canonical_id, supersedes_id, version, type, key, status,
				 properties, labels, content_hash, created_at, updated_at)
			VALUES (?, ?, ?, ?, NULL, ?, 'Vehicle', ?, 'active', '{}'::jsonb, '{}'::text[], ?, NOW(), NOW())`,
			id, projectID, branch, canonical, version, key, []byte("key-resolve-hash"))
		require.NoError(t, err)
	}

	// Branch original: version 2 (an edit on the branch before merge). The
	// pre-fix query orders by version DESC, so this row is returned first.
	insertObject(branchCanonical, branchCanonical, 2, &branchID)
	// Merged main copy: version 1, its own canonical id.
	insertObject(uuid.New(), mainCanonical, 1, nil)

	// No branch context: key resolution must hit main, not the branch copy.
	got, err := svc.resolveEntityIDByKey(ctx, projectID, key)
	require.NoError(t, err)
	assert.Equal(t, mainCanonical, got, "key lookup without a branch must resolve to the main copy")

	// The main copy's physical id resolves to its own canonical id.
	var mainPhysical uuid.UUID
	require.NoError(t, db.NewRaw(
		`SELECT id FROM kb.graph_objects WHERE project_id = ? AND canonical_id = ? AND branch_id IS NULL`,
		projectID, mainCanonical).Scan(ctx, &mainPhysical))
	gotCanonical, err := svc.resolveCanonicalID(ctx, projectID, mainPhysical)
	require.NoError(t, err)
	assert.Equal(t, mainCanonical, gotCanonical, "canonical resolution of the main physical id must land on main")

	// A main-only run resolves the same way (the boundary is installed).
	mainOnlyCtx := branchscope.ContextWith(ctx, nil)
	got, err = svc.resolveEntityIDByKey(mainOnlyCtx, projectID, key)
	require.NoError(t, err)
	assert.Equal(t, mainCanonical, got, "main-only key lookup must resolve to the main copy")
}
