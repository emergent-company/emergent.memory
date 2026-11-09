//go:build integration

package database_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/database"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

const rlsSentinel = "00000000-0000-0000-0000-000000000000"

// TestSetRLSContext_AlwaysSetsValueAndIsTransactionScoped covers the two
// properties issue #32 calls out: an empty project must not be a no-op, and
// the value must be transaction-scoped (SET LOCAL), so it cannot survive on a
// pooled connection.
func TestSetRLSContext_AlwaysSetsValueAndIsTransactionScoped(t *testing.T) {
	ctx := context.Background()
	testDB, err := testutil.SetupTestDB(ctx, "rls_set")
	require.NoError(t, err)
	defer testDB.Close()

	// Pin one connection so the session- and transaction-scoped effects are
	// observable on the same backend.
	conn, err := testDB.DB.Conn(ctx)
	require.NoError(t, err)
	defer func() {
		_, _ = conn.ExecContext(context.Background(), "RESET app.current_project_id")
		_ = conn.Close()
	}()

	// Simulate a value left on the session by older code (plain SET).
	_, err = conn.ExecContext(ctx, "SET app.current_project_id = 'stale-project'")
	require.NoError(t, err)

	_, err = conn.ExecContext(ctx, "BEGIN")
	require.NoError(t, err)

	// An empty project id must still issue a value, and that value must be the
	// sentinel that matches no row.
	require.NoError(t, database.SetRLSContext(ctx, &conn, ""))
	var got string
	require.NoError(t, conn.NewRaw("SELECT current_setting('app.current_project_id', true)").Scan(ctx, &got))
	assert.Equal(t, rlsSentinel, got, "empty project id must set the fail-closed sentinel")

	projectID := uuid.NewString()
	require.NoError(t, database.SetRLSContext(ctx, &conn, projectID))
	require.NoError(t, conn.NewRaw("SELECT current_setting('app.current_project_id', true)").Scan(ctx, &got))
	assert.Equal(t, projectID, got)

	_, err = conn.ExecContext(ctx, "COMMIT")
	require.NoError(t, err)

	// SET LOCAL was discarded at COMMIT: the stale session value is visible
	// again. A plain SET would have leaked projectID onto the connection.
	require.NoError(t, conn.NewRaw("SELECT current_setting('app.current_project_id', true)").Scan(ctx, &got))
	assert.Equal(t, "stale-project", got, "RLS context must not outlive its transaction")
}

// TestRLSPolicies_FailClosedForGraphTables proves the 00281 policies behave as
// intended when RLS is actually enforced: as a non-superuser role, an unset or
// empty context sees nothing, and a project context sees only that project.
func TestRLSPolicies_FailClosedForGraphTables(t *testing.T) {
	ctx := context.Background()
	testDB, err := testutil.SetupTestDB(ctx, "rls_policies")
	require.NoError(t, err)
	defer testDB.Close()

	require.NoError(t, testDB.BeginTestTx(ctx))
	defer func() { _ = testDB.RollbackTestTx() }()
	db := testDB.GetDB()

	orgID := uuid.NewString()
	projectA := uuid.NewString()
	projectB := uuid.NewString()
	objectA := uuid.NewString()
	objectB := uuid.NewString()
	relationshipA := uuid.NewString()
	relationshipB := uuid.NewString()

	// Insert as the (superuser) test role: superusers bypass RLS even on FORCE
	// tables, which is also why the policies remain inert in the deployment.
	_, err = db.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, 'RLS Test Org')`, orgID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO kb.projects (id, organization_id, name)
		VALUES (?, ?, 'Project A'), (?, ?, 'Project B')
	`, projectA, orgID, projectB, orgID)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		INSERT INTO kb.graph_objects (id, project_id, type, canonical_id, key)
		VALUES (?, ?, 'Test', ?, 'a'), (?, ?, 'Test', ?, 'b')
	`, objectA, projectA, objectA, objectB, projectB, objectB)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		INSERT INTO kb.graph_relationships (id, project_id, type, src_id, dst_id, canonical_id)
		VALUES (?, ?, 'rel', ?, ?, ?), (?, ?, 'rel', ?, ?, ?)
	`, relationshipA, projectA, objectA, objectA, relationshipA,
		relationshipB, projectB, objectB, objectB, relationshipB)
	require.NoError(t, err)

	// A non-owner, non-BYPASSRLS role with only SELECT: RLS is enforced.
	role := "rls_probe_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = db.ExecContext(ctx, fmt.Sprintf("CREATE ROLE %s NOLOGIN", role))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, fmt.Sprintf(
		"GRANT USAGE ON SCHEMA kb TO %s; GRANT SELECT ON kb.graph_objects, kb.graph_relationships TO %s",
		role, role,
	))
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, fmt.Sprintf("SET ROLE %s", role))
	require.NoError(t, err)
	defer func() { _, _ = db.ExecContext(context.Background(), "RESET ROLE") }()

	count := func(table string) int {
		t.Helper()
		var n int
		require.NoError(t, db.NewRaw("SELECT count(*) FROM "+table).Scan(ctx, &n))
		return n
	}

	// Unset context: fail closed, no rows from either project.
	assert.Equal(t, 0, count("kb.graph_objects"), "unset context must not see graph objects")
	assert.Equal(t, 0, count("kb.graph_relationships"), "unset context must not see graph relationships")

	// Project A context: only A.
	require.NoError(t, database.SetRLSContext(ctx, db, projectA))
	assert.Equal(t, 1, count("kb.graph_objects"), "project A context must see exactly A's object")
	assert.Equal(t, 1, count("kb.graph_relationships"), "project A context must see exactly A's relationship")

	// Project B context: only B.
	require.NoError(t, database.SetRLSContext(ctx, db, projectB))
	assert.Equal(t, 1, count("kb.graph_objects"), "project B context must see exactly B's object")
	assert.Equal(t, 1, count("kb.graph_relationships"), "project B context must see exactly B's relationship")

	// Empty context: the sentinel matches no project, so fail closed rather
	// than reverting to "all rows" (the old fail-open policy).
	require.NoError(t, database.SetRLSContext(ctx, db, ""))
	assert.Equal(t, 0, count("kb.graph_objects"), "empty context must not see graph objects")
	assert.Equal(t, 0, count("kb.graph_relationships"), "empty context must not see graph relationships")

	_, err = db.ExecContext(ctx, "RESET ROLE")
	require.NoError(t, err)
}
