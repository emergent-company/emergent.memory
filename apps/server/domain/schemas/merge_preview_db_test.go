package schemas_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/schemas"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// setupMergePreviewTest wires a live-database repository and a project factory.
func setupMergePreviewTest(t *testing.T) (context.Context, *schemas.Repository, bun.IDB, func() string) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "mergepreview")
	t.Cleanup(testDB.Close)

	db := testDB.GetDB()
	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "Merge Preview Org"))

	newProject := func() string {
		projectID := uuid.NewString()
		require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
			ID:    projectID,
			OrgID: orgID,
			Name:  "Merge Preview Project " + projectID,
		}, testutil.AdminUser.ID))
		return projectID
	}
	return ctx, schemas.NewRepository(db, slog.Default()), db, newProject
}

func insertMergePreviewPack(t *testing.T, ctx context.Context, db bun.IDB, projectID, name, version string) string {
	t.Helper()
	pack := &schemas.GraphMemorySchema{
		ID:                      uuid.NewString(),
		Name:                    name,
		Version:                 version,
		ObjectTypeSchemas:       json.RawMessage(`{}`),
		RelationshipTypeSchemas: json.RawMessage(`[]`),
		UIConfigs:               json.RawMessage(`{}`),
		ExtractionPrompts:       json.RawMessage(`{}`),
		ProjectID:               &projectID,
	}
	_, err := db.NewInsert().Model(pack).Exec(ctx)
	require.NoError(t, err)
	return pack.ID
}

func insertMergePreviewRegistry(t *testing.T, ctx context.Context, db bun.IDB, projectID, typeName, schemaID, jsonSchema string) {
	t.Helper()
	_, err := db.NewRaw(`
		INSERT INTO kb.project_object_schema_registry
		(project_id, type_name, source, schema_id, json_schema, ui_config, extraction_config, enabled)
		VALUES (?, ?, 'template', ?::uuid, ?, '{}'::jsonb, '{}'::jsonb, true)
	`, projectID, typeName, schemaID, jsonSchema).Exec(ctx)
	require.NoError(t, err)
}

func insertMergePreviewManualRegistry(t *testing.T, ctx context.Context, db bun.IDB, projectID, typeName, jsonSchema string) {
	t.Helper()
	_, err := db.NewRaw(`
		INSERT INTO kb.project_object_schema_registry
		(project_id, type_name, source, schema_id, json_schema, ui_config, extraction_config, enabled)
		VALUES (?, ?, 'template', NULL, ?, '{}'::jsonb, '{}'::jsonb, true)
	`, projectID, typeName, jsonSchema).Exec(ctx)
	require.NoError(t, err)
}

func countMergePreviewRows(t *testing.T, ctx context.Context, db bun.IDB, table string) int {
	t.Helper()
	var n int
	require.NoError(t, db.NewRaw("SELECT COUNT(*) FROM "+table).Scan(ctx, &n))
	return n
}

func TestPreviewMerge(t *testing.T) {
	ctx, repo, db, newProject := setupMergePreviewTest(t)

	t.Run("no existing types yields empty conflicts", func(t *testing.T) {
		projectID := newProject()
		res, err := repo.PreviewMerge(ctx, projectID, []schemas.MergePreviewPack{{
			Name:        "Fresh Pack",
			ObjectTypes: json.RawMessage(`[{"name":"Person","properties":{"name":{"type":"string"}}}]`),
		}})
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.False(t, res.RequiresMerge)
		assert.Empty(t, res.Conflicts)
	})

	t.Run("one collision reports added and conflicting properties", func(t *testing.T) {
		projectID := newProject()
		existingPackID := insertMergePreviewPack(t, ctx, db, projectID, "Existing Pack", "1.0.0")
		insertMergePreviewRegistry(t, ctx, db, projectID, "Person", existingPackID,
			`{"properties":{"name":{"type":"string"},"age":{"type":"integer"}}}`)

		res, err := repo.PreviewMerge(ctx, projectID, []schemas.MergePreviewPack{{
			Name: "Incoming Pack",
			ObjectTypes: json.RawMessage(`[
				{"name":"Person","properties":{"name":{"type":"string"},"email":{"type":"string"}}}
			]`),
		}})
		require.NoError(t, err)
		require.True(t, res.RequiresMerge)
		require.Len(t, res.Conflicts, 1)

		c := res.Conflicts[0]
		assert.Equal(t, "Person", c.TypeName)
		assert.Equal(t, "Incoming Pack", c.IncomingPackName)
		assert.Equal(t, existingPackID, c.ExistingSchemaID)
		assert.Equal(t, "Existing Pack", c.ExistingSchemaName)
		assert.Equal(t, "1.0.0", c.ExistingSchemaVersion)
		assert.Equal(t, []string{"email"}, c.AddedProperties)
		require.Len(t, c.ConflictingProperties, 1)
		assert.Equal(t, "name", c.ConflictingProperties[0].Property)
		assert.Equal(t, "existing_wins", c.ConflictingProperties[0].Resolution)
		assert.NotEmpty(t, c.MergedSchema)
	})

	t.Run("same pack name is ignored", func(t *testing.T) {
		projectID := newProject()
		packID := insertMergePreviewPack(t, ctx, db, projectID, "Same Pack", "1.0.0")
		insertMergePreviewRegistry(t, ctx, db, projectID, "Person", packID,
			`{"properties":{"name":{"type":"string"}}}`)

		res, err := repo.PreviewMerge(ctx, projectID, []schemas.MergePreviewPack{{
			Name:        "Same Pack",
			ObjectTypes: json.RawMessage(`[{"name":"Person","properties":{"name":{"type":"string"}}}]`),
		}})
		require.NoError(t, err)
		assert.False(t, res.RequiresMerge)
		assert.Empty(t, res.Conflicts)
	})

	t.Run("multiple packs and types report every conflict", func(t *testing.T) {
		projectID := newProject()
		packA := insertMergePreviewPack(t, ctx, db, projectID, "Pack A", "1.0.0")
		packB := insertMergePreviewPack(t, ctx, db, projectID, "Pack B", "1.0.0")
		insertMergePreviewRegistry(t, ctx, db, projectID, "Alpha", packA, `{"properties":{"a":{"type":"string"}}}`)
		insertMergePreviewRegistry(t, ctx, db, projectID, "Beta", packB, `{"properties":{"b":{"type":"string"}}}`)

		res, err := repo.PreviewMerge(ctx, projectID, []schemas.MergePreviewPack{
			{Name: "Incoming One", ObjectTypes: json.RawMessage(`[{"name":"Beta"},{"name":"Alpha"}]`)},
			{Name: "Incoming Two", ObjectTypes: json.RawMessage(`{"Gamma":{"properties":{}}}`)},
		})
		require.NoError(t, err)
		require.True(t, res.RequiresMerge)
		require.Len(t, res.Conflicts, 2)
		// Deterministic sort: TypeName then IncomingPackName.
		assert.Equal(t, "Alpha", res.Conflicts[0].TypeName)
		assert.Equal(t, "Incoming One", res.Conflicts[0].IncomingPackName)
		assert.Equal(t, "Beta", res.Conflicts[1].TypeName)
		assert.Equal(t, "Incoming One", res.Conflicts[1].IncomingPackName)
	})

	t.Run("manual registry row with null schema_id still conflicts", func(t *testing.T) {
		projectID := newProject()
		insertMergePreviewManualRegistry(t, ctx, db, projectID, "ManualType",
			`{"properties":{"name":{"type":"string"}}}`)

		res, err := repo.PreviewMerge(ctx, projectID, []schemas.MergePreviewPack{{
			Name:        "Incoming Pack",
			ObjectTypes: json.RawMessage(`[{"name":"ManualType","properties":{"name":{"type":"string"}}}]`),
		}})
		require.NoError(t, err)
		require.True(t, res.RequiresMerge)
		require.Len(t, res.Conflicts, 1)

		c := res.Conflicts[0]
		assert.Equal(t, "ManualType", c.TypeName)
		assert.Empty(t, c.ExistingSchemaID)
		assert.Empty(t, c.ExistingSchemaName)
		assert.Empty(t, c.ExistingSchemaVersion)
		assert.NotEmpty(t, c.ExistingSchema)
	})

	t.Run("preview is read-only", func(t *testing.T) {
		projectID := newProject()
		packID := insertMergePreviewPack(t, ctx, db, projectID, "Existing Pack", "1.0.0")
		insertMergePreviewRegistry(t, ctx, db, projectID, "Person", packID,
			`{"properties":{"name":{"type":"string"}}}`)

		registryBefore := countMergePreviewRows(t, ctx, db, "kb.project_object_schema_registry")
		assignmentsBefore := countMergePreviewRows(t, ctx, db, "kb.project_schemas")
		var jsonBefore string
		require.NoError(t, db.NewRaw(`
			SELECT json_schema::text AS json_schema FROM kb.project_object_schema_registry
			WHERE project_id = ? AND type_name = 'Person'
		`, projectID).Scan(ctx, &jsonBefore))

		_, err := repo.PreviewMerge(ctx, projectID, []schemas.MergePreviewPack{{
			Name:        "Incoming Pack",
			ObjectTypes: json.RawMessage(`[{"name":"Person","properties":{"email":{"type":"string"}}},{"name":"NewType"}]`),
		}})
		require.NoError(t, err)

		assert.Equal(t, registryBefore, countMergePreviewRows(t, ctx, db, "kb.project_object_schema_registry"))
		assert.Equal(t, assignmentsBefore, countMergePreviewRows(t, ctx, db, "kb.project_schemas"))
		var jsonAfter string
		require.NoError(t, db.NewRaw(`
			SELECT json_schema::text AS json_schema FROM kb.project_object_schema_registry
			WHERE project_id = ? AND type_name = 'Person'
		`, projectID).Scan(ctx, &jsonAfter))
		assert.JSONEq(t, jsonBefore, jsonAfter)
	})
}
