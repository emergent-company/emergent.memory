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

// setupTypeRegistryTest wires a live-database repository plus a fresh project.
func setupTypeRegistryTest(t *testing.T) (context.Context, *schemas.Repository, bun.IDB, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "typeregistry")
	t.Cleanup(testDB.Close)

	db := testDB.GetDB()
	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "Type Registry Org"))
	projectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "Type Registry Project " + projectID,
	}, testutil.AdminUser.ID))
	return ctx, schemas.NewRepository(db, slog.Default()), db, projectID
}

// registryRowAndOwner returns the registered JSON schema and owning pack name for a type.
func registryRowAndOwner(t *testing.T, ctx context.Context, db bun.IDB, projectID, typeName string) (string, map[string]any) {
	t.Helper()
	var row struct {
		JSONSchema json.RawMessage `bun:"json_schema"`
		PackName   *string         `bun:"pack_name"`
	}
	require.NoError(t, db.NewRaw(`
		SELECT por.json_schema, gs.name AS pack_name
		FROM kb.project_object_schema_registry por
		LEFT JOIN kb.graph_schemas gs ON gs.id = por.schema_id
		WHERE por.project_id = ? AND por.type_name = ?
		LIMIT 1
	`, projectID, typeName).Scan(ctx, &row))

	var out map[string]any
	require.NoError(t, json.Unmarshal(row.JSONSchema, &out))
	owner := ""
	if row.PackName != nil {
		owner = *row.PackName
	}
	return owner, out
}

func propsOf(t *testing.T, schema map[string]any) map[string]any {
	t.Helper()
	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok, "schema has no properties: %v", schema)
	return props
}

// TestAssignPackInPlaceUpgradeReplacesOwnTypes is the #294 regression: the CLI
// `--upgrade` path updates the pack row in place and then re-assigns it. That
// re-assign must refresh the registry for the pack's own types, with or without
// a merge confirmation, because no other pack's definition is involved.
func TestAssignPackInPlaceUpgradeReplacesOwnTypes(t *testing.T) {
	ctx, repo, db, projectID := setupTypeRegistryTest(t)

	pack, err := repo.CreatePack(ctx, projectID, &schemas.CreatePackRequest{
		Name:    "ot-core",
		Version: "0.1.0",
		ObjectTypeSchemasSnake: json.RawMessage(
			`[{"name":"Person","properties":{"name":{"type":"string"},"legacy":{"type":"string"}}}]`),
	})
	require.NoError(t, err)
	_, err = repo.AssignPackWithTypes(ctx, projectID, testutil.AdminUser.ID, &schemas.AssignPackRequest{SchemaID: pack.ID})
	require.NoError(t, err)

	// In-place content update (same pack row) — exactly what `blueprints
	// install --upgrade` does via UpdatePack.
	newVersion := "0.2.0"
	_, err = repo.UpdatePack(ctx, pack.ID, projectID, &schemas.UpdatePackRequest{
		Version: &newVersion,
		ObjectTypeSchemas: json.RawMessage(
			`[{"name":"Person","properties":{"name":{"type":"string"},"email":{"type":"string"}}}]`),
	})
	require.NoError(t, err)

	// Merge=false: refreshing this pack's own types is not a cross-pack merge.
	result, err := repo.AssignPackWithTypes(ctx, projectID, testutil.AdminUser.ID, &schemas.AssignPackRequest{SchemaID: pack.ID})
	require.NoError(t, err)
	assert.Equal(t, []string{"Person"}, result.ReplacedTypes)
	assert.Empty(t, result.Conflicts)

	owner, got := registryRowAndOwner(t, ctx, db, projectID, "Person")
	assert.Equal(t, "ot-core", owner)
	props := propsOf(t, got)
	assert.Contains(t, props, "email", "upgraded property must reach the registry")
	assert.NotContains(t, props, "legacy", "a removed property must be removed on upgrade")
}

// TestAssignPackSameFamilyNewVersionReplaces covers the version-bump variant:
// a new pack row with the same name owns the same type, so it replaces.
func TestAssignPackSameFamilyNewVersionReplaces(t *testing.T) {
	ctx, repo, db, projectID := setupTypeRegistryTest(t)

	v1, err := repo.CreatePack(ctx, projectID, &schemas.CreatePackRequest{
		Name: "ot-core", Version: "0.1.0",
		ObjectTypeSchemasSnake: json.RawMessage(`[{"name":"Person","properties":{"name":{"type":"string"}}}]`),
	})
	require.NoError(t, err)
	_, err = repo.AssignPackWithTypes(ctx, projectID, testutil.AdminUser.ID, &schemas.AssignPackRequest{SchemaID: v1.ID})
	require.NoError(t, err)

	v2, err := repo.CreatePack(ctx, projectID, &schemas.CreatePackRequest{
		Name: "ot-core", Version: "0.2.0",
		ObjectTypeSchemasSnake: json.RawMessage(`[{"name":"Person","properties":{"name":{"type":"string"},"email":{"type":"string"}}}]`),
	})
	require.NoError(t, err)
	result, err := repo.AssignPackWithTypes(ctx, projectID, testutil.AdminUser.ID, &schemas.AssignPackRequest{SchemaID: v2.ID})
	require.NoError(t, err)
	assert.Equal(t, []string{"Person"}, result.ReplacedTypes)
	assert.Empty(t, result.Conflicts)

	owner, got := registryRowAndOwner(t, ctx, db, projectID, "Person")
	assert.Equal(t, "ot-core", owner)
	assert.Contains(t, propsOf(t, got), "email")
}

// TestAssignPackCrossPackMergeMovesOwnership pins the precedence rule shared by
// every read path (issue #295): the newest install defines the type. A
// merge-confirmed install from another pack therefore becomes the owner, and a
// later non-confirmed install of the older pack no longer clobbers it.
func TestAssignPackCrossPackMergeMovesOwnership(t *testing.T) {
	ctx, repo, db, projectID := setupTypeRegistryTest(t)

	a, err := repo.CreatePack(ctx, projectID, &schemas.CreatePackRequest{
		Name: "aa-core", Version: "0.1.0",
		ObjectTypeSchemasSnake: json.RawMessage(`[{"name":"Person","properties":{"name":{"type":"string"}}}]`),
	})
	require.NoError(t, err)
	_, err = repo.AssignPackWithTypes(ctx, projectID, testutil.AdminUser.ID, &schemas.AssignPackRequest{SchemaID: a.ID})
	require.NoError(t, err)

	b, err := repo.CreatePack(ctx, projectID, &schemas.CreatePackRequest{
		Name: "bb-companies", Version: "0.1.0",
		ObjectTypeSchemasSnake: json.RawMessage(`[{"name":"Person","properties":{"legal_name":{"type":"string"}}}]`),
	})
	require.NoError(t, err)

	// No merge confirmation: skipped, and the existing owner keeps the type.
	skipped, err := repo.AssignPackWithTypes(ctx, projectID, testutil.AdminUser.ID, &schemas.AssignPackRequest{SchemaID: b.ID})
	require.NoError(t, err)
	assert.Equal(t, []string{"Person"}, skipped.SkippedTypes)
	require.Len(t, skipped.Conflicts, 1)

	owner, _ := registryRowAndOwner(t, ctx, db, projectID, "Person")
	assert.Equal(t, "aa-core", owner)

	// Merge confirmed: additive merge, and the newest install owns the type.
	merged, err := repo.AssignPackWithTypes(ctx, projectID, testutil.AdminUser.ID, &schemas.AssignPackRequest{SchemaID: b.ID, Merge: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"Person"}, merged.MergedTypes)

	owner, got := registryRowAndOwner(t, ctx, db, projectID, "Person")
	assert.Equal(t, "bb-companies", owner, "the newest install must define the type")
	props := propsOf(t, got)
	assert.Contains(t, props, "name")
	assert.Contains(t, props, "legal_name")
}

// TestPreviewMergeDetectsSameRunCollision is the #293 regression: two packs in
// the same install declaring the same type must be reported before anything is
// materialized, even when the type is not registered yet.
func TestPreviewMergeDetectsSameRunCollision(t *testing.T) {
	ctx, repo, _, projectID := setupTypeRegistryTest(t)

	preview, err := repo.PreviewMerge(ctx, projectID, []schemas.MergePreviewPack{
		{Name: "aa-core", ObjectTypes: json.RawMessage(`[{"name":"Person","properties":{"name":{"type":"string"}}}]`)},
		{Name: "bb-companies", ObjectTypes: json.RawMessage(`[{"name":"Person","properties":{"legal_name":{"type":"string"}}}]`)},
	})
	require.NoError(t, err)
	require.True(t, preview.RequiresMerge)
	require.Len(t, preview.Conflicts, 1)
	c := preview.Conflicts[0]
	assert.Equal(t, "Person", c.TypeName)
	assert.Equal(t, "bb-companies", c.IncomingPackName)
	assert.Equal(t, "aa-core", c.ExistingSchemaName)
	assert.Equal(t, []string{"legal_name"}, c.AddedProperties)
}
