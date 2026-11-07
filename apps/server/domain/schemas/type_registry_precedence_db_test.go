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
	assert.Empty(t, merged.ShadowedTypes, "a merge owns the type, so there is no registry/read-path divergence")

	owner, got := registryRowAndOwner(t, ctx, db, projectID, "Person")
	assert.Equal(t, "bb-companies", owner, "the newest install must define the type")
	props := propsOf(t, got)
	assert.Contains(t, props, "name")
	assert.Contains(t, props, "legal_name")
}

// TestAssignPackCrossPackSkipReportsShadowing is the #338 part-2 regression.
// A non-merge install of a pack that declares a type owned by another pack
// skips the registry write, but assigning the pack still makes it the newest
// assignment, so the last-wins read paths resolve the type from the incoming
// declaration. The result must report that divergence explicitly instead of
// leaving the registry owner and the compiled types naming different packs.
func TestAssignPackCrossPackSkipReportsShadowing(t *testing.T) {
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

	// The dry run predicts the same divergence without touching the database.
	preview, err := repo.AssignPackWithTypes(ctx, projectID, testutil.AdminUser.ID, &schemas.AssignPackRequest{SchemaID: b.ID, DryRun: true})
	require.NoError(t, err)
	require.Len(t, preview.ShadowedTypes, 1)

	skipped, err := repo.AssignPackWithTypes(ctx, projectID, testutil.AdminUser.ID, &schemas.AssignPackRequest{SchemaID: b.ID})
	require.NoError(t, err)
	assert.Equal(t, []string{"Person"}, skipped.SkippedTypes)
	require.Len(t, skipped.ShadowedTypes, 1, "a cross-pack skip must name the shadowed type")

	shadowed := skipped.ShadowedTypes[0]
	assert.Equal(t, "Person", shadowed.TypeName)
	assert.Equal(t, "aa-core", shadowed.RegistryOwnerPack)
	assert.Equal(t, a.ID, shadowed.RegistryOwnerSchemaID)
	assert.Equal(t, "bb-companies", shadowed.ShadowingPack)
	assert.Equal(t, b.ID, shadowed.ShadowingSchemaID)

	// The registry still names the old owner...
	owner, _ := registryRowAndOwner(t, ctx, db, projectID, "Person")
	assert.Equal(t, "aa-core", owner, "a skip must not re-point the registry row")

	// ...while the read paths resolve Person from the newer pack. This is the
	// divergence the result now reports.
	compiled, err := repo.GetCompiledTypesByProject(ctx, projectID)
	require.NoError(t, err)
	var effective, superseded *schemas.ObjectTypeSchema
	for i := range compiled.ObjectTypes {
		if compiled.ObjectTypes[i].Name != "Person" {
			continue
		}
		if compiled.ObjectTypes[i].Shadowed {
			superseded = &compiled.ObjectTypes[i]
		} else {
			effective = &compiled.ObjectTypes[i]
		}
	}
	require.NotNil(t, effective, "the newer pack must define Person in the compiled view")
	require.NotNil(t, superseded, "the older pack's Person must be marked shadowed")
	assert.Equal(t, "bb-companies", effective.SchemaName)
	assert.Equal(t, "aa-core", superseded.SchemaName)
}

// TestAssignPackUpgradePreservesProjectTypeCustomisation pins the config-column
// rule for the "replace" action: a refresh rewrites the definition, but a
// ui_config the incoming pack does not declare keeps the value the project set
// (via the type-registry editor), instead of being reset to '{}'.
func TestAssignPackUpgradePreservesProjectTypeCustomisation(t *testing.T) {
	ctx, repo, db, projectID := setupTypeRegistryTest(t)

	pack, err := repo.CreatePack(ctx, projectID, &schemas.CreatePackRequest{
		Name: "ot-core", Version: "0.1.0",
		ObjectTypeSchemasSnake: json.RawMessage(`[{"name":"Person","properties":{"name":{"type":"string"}}}]`),
	})
	require.NoError(t, err)
	_, err = repo.AssignPackWithTypes(ctx, projectID, testutil.AdminUser.ID, &schemas.AssignPackRequest{SchemaID: pack.ID})
	require.NoError(t, err)

	// The project customises the type's appearance in the registry.
	_, err = db.NewRaw(`
		UPDATE kb.project_object_schema_registry
		SET ui_config = '{"icon":"project-icon"}', extraction_config = '{"enabled":false}'
		WHERE project_id = ? AND type_name = 'Person'
	`, projectID).Exec(ctx)
	require.NoError(t, err)

	newVersion := "0.2.0"
	_, err = repo.UpdatePack(ctx, pack.ID, projectID, &schemas.UpdatePackRequest{
		Version: &newVersion,
		ObjectTypeSchemas: json.RawMessage(
			`[{"name":"Person","properties":{"name":{"type":"string"},"email":{"type":"string"}}}]`),
	})
	require.NoError(t, err)
	_, err = repo.AssignPackWithTypes(ctx, projectID, testutil.AdminUser.ID, &schemas.AssignPackRequest{SchemaID: pack.ID})
	require.NoError(t, err)

	var row struct {
		UIConfig         json.RawMessage `bun:"ui_config"`
		ExtractionConfig json.RawMessage `bun:"extraction_config"`
	}
	require.NoError(t, db.NewRaw(`
		SELECT ui_config, extraction_config FROM kb.project_object_schema_registry
		WHERE project_id = ? AND type_name = 'Person'
	`, projectID).Scan(ctx, &row))
	assert.JSONEq(t, `{"icon":"project-icon"}`, string(row.UIConfig),
		"an upgrade must not wipe a project's type appearance")
	assert.JSONEq(t, `{"enabled":false}`, string(row.ExtractionConfig),
		"an upgrade must not wipe a project's extraction config")
}

// TestAssignPackReassignReactivatesAssignment pins the companion of the
// in-place upgrade: re-assigning a pack whose assignment was deactivated must
// bring it back, or the registry refresh stays invisible to the compiled-types
// and validation reads that filter on active.
func TestAssignPackReassignReactivatesAssignment(t *testing.T) {
	ctx, repo, db, projectID := setupTypeRegistryTest(t)

	pack, err := repo.CreatePack(ctx, projectID, &schemas.CreatePackRequest{
		Name: "ot-core", Version: "0.1.0",
		ObjectTypeSchemasSnake: json.RawMessage(`[{"name":"Person","properties":{"name":{"type":"string"}}}]`),
	})
	require.NoError(t, err)
	first, err := repo.AssignPackWithTypes(ctx, projectID, testutil.AdminUser.ID, &schemas.AssignPackRequest{SchemaID: pack.ID})
	require.NoError(t, err)
	require.NotEmpty(t, first.AssignmentID)

	inactive := false
	require.NoError(t, repo.UpdateAssignment(ctx, projectID, first.AssignmentID, &schemas.UpdateAssignmentRequest{Active: &inactive}))

	_, err = repo.AssignPackWithTypes(ctx, projectID, testutil.AdminUser.ID, &schemas.AssignPackRequest{SchemaID: pack.ID})
	require.NoError(t, err)

	var active bool
	require.NoError(t, db.NewRaw(
		`SELECT active FROM kb.project_schemas WHERE id = ?`, first.AssignmentID,
	).Scan(ctx, &active))
	assert.True(t, active, "re-assigning a pack must reactivate its assignment")
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

// TestAssignPackCrossScopeSameNameRequiresMerge is the #338 part-1 regression.
// A pack is "same family" (replace, no merge) only when it matches the registry
// owner on name AND project/source scope. A different pack that reuses the name
// across a scope boundary — e.g. a project-scoped copy of a builtin — must not
// silently capture the type: it requires Merge=true.
func TestAssignPackCrossScopeSameNameRequiresMerge(t *testing.T) {
	ctx, repo, db, projectID := setupTypeRegistryTest(t)

	// A global builtin owns Person (project_id NULL, source 'builtin').
	builtinID := uuid.NewString()
	_, err := db.NewRaw(`
		INSERT INTO kb.graph_schemas (id, name, version, source, project_id, object_type_schemas)
		VALUES (?, 'ot-core', '1.0.0', 'builtin', NULL,
		        '[{"name":"Person","properties":{"name":{"type":"string"}}}]'::jsonb)
	`, builtinID).Exec(ctx)
	require.NoError(t, err)
	_, err = repo.AssignPackWithTypes(ctx, projectID, testutil.AdminUser.ID, &schemas.AssignPackRequest{SchemaID: builtinID})
	require.NoError(t, err)

	// A project-scoped pack reuses the builtin's name and declares the same type.
	fork, err := repo.CreatePack(ctx, projectID, &schemas.CreatePackRequest{
		Name: "ot-core", Version: "0.1.0",
		ObjectTypeSchemasSnake: json.RawMessage(`[{"name":"Person","properties":{"name":{"type":"string"},"legal_name":{"type":"string"}}}]`),
	})
	require.NoError(t, err)

	// No merge: the same-name cross-scope pack must not capture the type.
	skipped, err := repo.AssignPackWithTypes(ctx, projectID, testutil.AdminUser.ID, &schemas.AssignPackRequest{SchemaID: fork.ID})
	require.NoError(t, err)
	assert.Equal(t, []string{"Person"}, skipped.SkippedTypes)
	assert.Empty(t, skipped.ReplacedTypes, "a cross-scope name reuse is not a same-pack upgrade")
	require.Len(t, skipped.ShadowedTypes, 1)
	assert.Equal(t, "Person", skipped.ShadowedTypes[0].TypeName)
	assert.Equal(t, builtinID, skipped.ShadowedTypes[0].RegistryOwnerSchemaID)
	assert.Equal(t, fork.ID, skipped.ShadowedTypes[0].ShadowingSchemaID)

	// The builtin still owns the type in the registry.
	var ownerSchemaID string
	require.NoError(t, db.NewRaw(`
		SELECT schema_id FROM kb.project_object_schema_registry
		WHERE project_id = ? AND type_name = 'Person'
	`, projectID).Scan(ctx, &ownerSchemaID))
	assert.Equal(t, builtinID, ownerSchemaID, "an unconfirmed cross-scope install must not re-point the registry row")

	// Merge confirmed: additive merge moves ownership to the project pack.
	merged, err := repo.AssignPackWithTypes(ctx, projectID, testutil.AdminUser.ID, &schemas.AssignPackRequest{SchemaID: fork.ID, Merge: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"Person"}, merged.MergedTypes)

	owner, got := registryRowAndOwner(t, ctx, db, projectID, "Person")
	assert.Equal(t, "ot-core", owner)
	props := propsOf(t, got)
	assert.Contains(t, props, "name")
	assert.Contains(t, props, "legal_name")
}
