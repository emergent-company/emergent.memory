package backups

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// TestRestoreRejectsMalformedScopeKeyDeclaration pins the write-path fix for
// issue #1177: backup restore inserts json_schema verbatim, so it must reject a
// snapshot row carrying a malformed scopeKey declaration fail-closed rather than
// persist it into the registry.
func TestRestoreRejectsMalformedScopeKeyDeclaration(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	db := testutil.SetupTestDBOrFail(t, ctx, "restore_scope_key")
	defer db.Close()

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "restore-org")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`,
		projectID, orgID, "restore-project")
	require.NoError(t, err)

	specs := map[string]restoreTableSpec{}
	for _, s := range restoreTableOrder() {
		specs[s.name] = s
	}

	rows := []map[string]any{{
		"id":          uuid.NewString(),
		"project_id":  projectID,
		"type":        "LegalParagraph",
		"version":     1,
		"json_schema": `{"properties":{"chapter_id":{"type":"string"}},"scopeKey":{"property":"law_ref_id"}}`,
	}}

	restorer := &Restorer{db: db.DB, log: slog.Default()}
	tx, err := db.DB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback() //nolint:errcheck

	_, err = restorer.insertTableRows(ctx, tx, specs["object_type_schemas"], rows, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `scopeKey.property "law_ref_id" is not a declared property`)

	// Fail-closed: validation happens before any row is written.
	var n int
	require.NoError(t, db.DB.NewSelect().
		Table("kb.object_type_schemas").
		Where("type = ?", "LegalParagraph").
		ColumnExpr("count(*)").
		Scan(ctx, &n))
	assert.Zero(t, n, "rejected restore must not insert a schema row")
}
