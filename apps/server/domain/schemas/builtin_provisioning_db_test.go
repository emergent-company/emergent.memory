package schemas_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/schemas"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// TestProvisionBuiltinSchemasToAllProjects verifies that
// Repository.ProvisionBuiltinSchemasToAllProjects installs builtin schemas into
// projects idempotently and never resurrects an explicitly-uninstalled row.
func TestProvisionBuiltinSchemasToAllProjects(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	ctx := context.Background()
	testDB, err := testutil.SetupTestDB(ctx, "builtinprov")
	if err != nil {
		t.Skipf("skipping: test database unavailable: %v", err)
	}
	defer testDB.Close()
	db := testDB.GetDB()

	repo := schemas.NewRepository(db, slog.Default())

	// Insert a real org + project: project_schemas.project_id and
	// graph_schemas.project_id reference kb.projects, which references kb.orgs.
	orgID := uuid.NewString()
	projectID := uuid.NewString()
	_, err = db.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "test-org-"+orgID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`,
		projectID, orgID, "test-project-"+projectID)
	require.NoError(t, err)

	// Insert the builtin schema AFTER the project so the projects trigger (if
	// present) cannot have provisioned it — the repository method under test
	// must do the install.
	builtin := "builtin"
	now := time.Now().UTC()
	schema := &schemas.GraphMemorySchema{
		ID:                      uuid.NewString(),
		Name:                    "builtin-prov-test-" + uuid.NewString(),
		Version:                 "1.0.0",
		ObjectTypeSchemas:       json.RawMessage(`{}`),
		RelationshipTypeSchemas: json.RawMessage(`[]`),
		UIConfigs:               json.RawMessage(`{}`),
		ExtractionPrompts:       json.RawMessage(`{}`),
		Source:                  &builtin,
		CreatedAt:               now,
		UpdatedAt:               now,
	}
	_, err = db.NewInsert().Model(schema).Exec(ctx)
	require.NoError(t, err, "insert builtin graph schema")

	countRows := func() int {
		t.Helper()
		n, err := db.NewSelect().
			Table("kb.project_schemas").
			Where("project_id = ?", projectID).
			Where("schema_id = ?", schema.ID).
			Count(ctx)
		require.NoError(t, err)
		return n
	}
	loadRow := func() (active bool, removedAt *time.Time) {
		t.Helper()
		var row struct {
			Active    bool       `bun:"active"`
			RemovedAt *time.Time `bun:"removed_at"`
		}
		err := db.NewSelect().
			Table("kb.project_schemas").
			Column("active", "removed_at").
			Where("project_id = ?", projectID).
			Where("schema_id = ?", schema.ID).
			Scan(ctx, &row)
		require.NoError(t, err)
		return row.Active, row.RemovedAt
	}

	t.Run("installs builtin into project", func(t *testing.T) {
		require.Equal(t, 0, countRows(), "precondition: no assignment yet")

		require.NoError(t, repo.ProvisionBuiltinSchemasToAllProjects(ctx))

		require.Equal(t, 1, countRows(), "exactly one assignment expected")
		active, removedAt := loadRow()
		assert.True(t, active, "assignment must be active")
		assert.Nil(t, removedAt, "assignment must not be removed")
	})

	t.Run("second run is idempotent (no duplicate)", func(t *testing.T) {
		require.NoError(t, repo.ProvisionBuiltinSchemasToAllProjects(ctx))
		assert.Equal(t, 1, countRows(), "second run must not duplicate the assignment")
	})

	t.Run("respects explicit uninstall", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `
			UPDATE kb.project_schemas
			SET removed_at = now(), active = false, updated_at = now()
			WHERE project_id = ? AND schema_id = ?`,
			projectID, schema.ID)
		require.NoError(t, err)

		require.NoError(t, repo.ProvisionBuiltinSchemasToAllProjects(ctx))

		assert.Equal(t, 1, countRows(), "removed assignment must not be re-created")
		active, removedAt := loadRow()
		assert.False(t, active, "removed assignment must not be re-activated")
		require.NotNil(t, removedAt, "removed_at must be preserved")
	})
}
