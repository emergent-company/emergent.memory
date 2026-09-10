package schemas_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/schemas"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// This file exercises the registry-restoring rollback end to end against a
// live database. Every test skips cleanly when Postgres is unavailable.
//
// The task's stated source table "kb.schema_migration_runs" does not carry
// from_schema_id/to_schema_id; it stores human version strings and is not
// written by the current migration path. kb.schema_migration_jobs is the table
// that persists from_schema_id/to_schema_id, so resolution uses it first.

func setupRollbackTest(t *testing.T) (context.Context, *schemas.Repository, bun.IDB, string, *config.Config) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	ctx := context.Background()
	testDB, err := testutil.SetupTestDB(ctx, "restorereg")
	if err != nil {
		t.Skipf("skipping: test database unavailable: %v", err)
	}
	t.Cleanup(testDB.Close)

	db := testDB.GetDB()
	orgID := uuid.NewString()
	if err := testutil.CreateTestOrganization(ctx, db, orgID, "Restore Registry Org"); err != nil {
		t.Fatalf("create org: %v", err)
	}
	projectID := uuid.NewString()
	if err := testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "Restore Registry Project",
	}, testutil.AdminUser.ID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	return ctx, schemas.NewRepository(db, slog.Default()), db, projectID, testDB.Config
}

func insertRollbackPack(t *testing.T, ctx context.Context, db bun.IDB, name, version, objectSchemas string) *schemas.GraphMemorySchema {
	t.Helper()
	now := time.Now()
	pack := &schemas.GraphMemorySchema{
		ID:                      uuid.NewString(),
		Name:                    name,
		Version:                 version,
		ObjectTypeSchemas:       json.RawMessage(objectSchemas),
		RelationshipTypeSchemas: json.RawMessage(`[]`),
		UIConfigs:               json.RawMessage(`{}`),
		ExtractionPrompts:       json.RawMessage(`{}`),
		PublishedAt:             &now,
		CreatedAt:               now,
		UpdatedAt:               now,
	}
	if _, err := db.NewInsert().Model(pack).Exec(ctx); err != nil {
		t.Fatalf("insert pack %s@%s: %v", name, version, err)
	}
	return pack
}

func insertRollbackPackWithConfig(t *testing.T, ctx context.Context, db bun.IDB, name, version, objectSchemas, uiConfigs, extractionPrompts string) *schemas.GraphMemorySchema {
	t.Helper()
	now := time.Now()
	pack := &schemas.GraphMemorySchema{
		ID:                      uuid.NewString(),
		Name:                    name,
		Version:                 version,
		ObjectTypeSchemas:       json.RawMessage(objectSchemas),
		RelationshipTypeSchemas: json.RawMessage(`[]`),
		UIConfigs:               json.RawMessage(uiConfigs),
		ExtractionPrompts:       json.RawMessage(extractionPrompts),
		PublishedAt:             &now,
		CreatedAt:               now,
		UpdatedAt:               now,
	}
	if _, err := db.NewInsert().Model(pack).Exec(ctx); err != nil {
		t.Fatalf("insert pack %s@%s: %v", name, version, err)
	}
	return pack
}

func insertRegistryRow(t *testing.T, ctx context.Context, db bun.IDB, projectID, typeName, schemaID, jsonSchema string) {
	t.Helper()
	_, err := db.NewRaw(`
		INSERT INTO kb.project_object_schema_registry
		(project_id, type_name, source, schema_id, json_schema, ui_config, extraction_config, enabled)
		VALUES (uuid(?), ?, 'template', uuid(?), ?, '{}'::jsonb, '{}'::jsonb, true)
	`, projectID, typeName, schemaID, jsonSchema).Exec(ctx)
	if err != nil {
		t.Fatalf("insert registry row %s: %v", typeName, err)
	}
}

type registryRow struct {
	ID       string          `bun:"id"`
	SchemaID *string         `bun:"schema_id"`
	JSON     json.RawMessage `bun:"json_schema"`
	Enabled  bool            `bun:"enabled"`
}

func registryRows(t *testing.T, ctx context.Context, db bun.IDB, projectID, typeName string) []registryRow {
	t.Helper()
	var rows []registryRow
	if err := db.NewRaw(`
		SELECT id, schema_id, json_schema, enabled
		FROM kb.project_object_schema_registry
		WHERE project_id = ? AND type_name = ?
	`, projectID, typeName).Scan(ctx, &rows); err != nil {
		t.Fatalf("read registry rows for %s: %v", typeName, err)
	}
	return rows
}

func TestRestoreTypeRegistryTx(t *testing.T) {
	ctx, repo, db, projectID, _ := setupRollbackTest(t)

	fromPack := insertRollbackPack(t, ctx, db, "Test Pack", "1.0.0", `[
		{"name":"Person","properties":{"name":{"type":"string"}}},
		{"name":"Belief","properties":{"text":{"type":"string"}}}
	]`)
	toPack := insertRollbackPack(t, ctx, db, "Test Pack", "2.0.0", `[
		{"name":"Person","properties":{"name":{"type":"string"},"age":{"type":"number"}}},
		{"name":"Organization","properties":{"title":{"type":"string"}}}
	]`)
	otherPack := insertRollbackPack(t, ctx, db, "Other Pack", "9.9.9", `[]`)

	// Shared type owned by the to-pack, from-only type owned by the from-pack,
	// to-only type owned by the to-pack, and an unrelated pack's type.
	insertRegistryRow(t, ctx, db, projectID, "Person", toPack.ID, `{"properties":{"name":{"type":"string"},"age":{"type":"number"}}}`)
	insertRegistryRow(t, ctx, db, projectID, "Belief", fromPack.ID, `{"properties":{"text":{"type":"string"}}}`)
	insertRegistryRow(t, ctx, db, projectID, "Organization", toPack.ID, `{"properties":{"title":{"type":"string"}}}`)
	insertRegistryRow(t, ctx, db, projectID, "Foreign", otherPack.ID, `{"properties":{"x":{"type":"string"}}}`)
	// Same type name as a restored type but owned by another pack — must not be
	// touched.
	insertRegistryRow(t, ctx, db, projectID, "Person", otherPack.ID, `{"properties":{"other":{"type":"string"}}}`)

	err := db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		return repo.RestoreTypeRegistryTx(ctx, tx, projectID, "", fromPack, toPack)
	})
	if err != nil {
		t.Fatalf("RestoreTypeRegistryTx: %v", err)
	}

	personRows := registryRows(t, ctx, db, projectID, "Person")
	if len(personRows) != 2 {
		t.Fatalf("expected 2 Person rows (from-packed + other pack), got %d", len(personRows))
	}
	for _, row := range personRows {
		if row.SchemaID == nil {
			t.Fatalf("Person row %s has NULL schema_id", row.ID)
		}
		switch *row.SchemaID {
		case fromPack.ID:
			if strings.Contains(string(row.JSON), `"age"`) {
				t.Errorf("restored Person must use from-pack definition, got %s", row.JSON)
			}
			if !strings.Contains(string(row.JSON), `"name"`) {
				t.Errorf("restored Person missing from-pack property, got %s", row.JSON)
			}
			if !row.Enabled {
				t.Error("restored Person must be enabled")
			}
		case otherPack.ID:
			if !strings.Contains(string(row.JSON), `"other"`) {
				t.Errorf("other pack's Person row was modified, got %s", row.JSON)
			}
		default:
			t.Fatalf("unexpected Person row owner %s", *row.SchemaID)
		}
	}

	// From-only type restored to from-pack.
	beliefRows := registryRows(t, ctx, db, projectID, "Belief")
	if len(beliefRows) != 1 || beliefRows[0].SchemaID == nil || *beliefRows[0].SchemaID != fromPack.ID {
		t.Fatalf("Belief should be owned by from-pack, got %+v", beliefRows)
	}

	// To-only type removed.
	if orgRows := registryRows(t, ctx, db, projectID, "Organization"); len(orgRows) != 0 {
		t.Fatalf("expected Organization rows removed, got %d", len(orgRows))
	}

	// Unrelated pack's type untouched.
	foreignRows := registryRows(t, ctx, db, projectID, "Foreign")
	if len(foreignRows) != 1 || foreignRows[0].SchemaID == nil || *foreignRows[0].SchemaID != otherPack.ID {
		t.Fatalf("Foreign row should be untouched, got %+v", foreignRows)
	}
}

func newRollbackService(t *testing.T, db bun.IDB, repo *schemas.Repository, cfg *config.Config) *schemas.Service {
	t.Helper()
	// graph.NewService only needs the repository for object listing; other
	// dependencies are unused by RollbackSchemaMigration.
	log := slog.Default()
	gRepo := graph.NewRepository(db, log, cfg)
	gSvc := graph.NewService(gRepo, log, nil, nil, nil, nil, nil, nil, nil, nil)
	return schemas.NewService(repo, gSvc, log)
}

func TestRollbackSchemaMigrationRestoreTypeRegistryFlag(t *testing.T) {
	ctx, repo, db, projectID, cfg := setupRollbackTest(t)
	svc := newRollbackService(t, db, repo, cfg)

	fromPack := insertRollbackPack(t, ctx, db, "Flag Pack", "1.0.0", `[
		{"name":"Person","properties":{"name":{"type":"string"}}}
	]`)
	toPack := insertRollbackPack(t, ctx, db, "Flag Pack", "2.0.0", `[
		{"name":"Person","properties":{"name":{"type":"string"},"age":{"type":"number"}}},
		{"name":"Organization","properties":{"title":{"type":"string"}}}
	]`)
	insertRegistryRow(t, ctx, db, projectID, "Person", toPack.ID, `{"properties":{"name":{"type":"string"},"age":{"type":"number"}}}`)
	insertRegistryRow(t, ctx, db, projectID, "Organization", toPack.ID, `{"properties":{"title":{"type":"string"}}}`)

	// Record the migration so pack resolution can find from/to.
	if err := repo.CreateMigrationJob(ctx, &schemas.SchemaMigrationJob{
		ProjectID:    projectID,
		FromSchemaID: fromPack.ID,
		ToSchemaID:   toPack.ID,
		Chain:        []schemas.MigrationHop{{FromSchemaID: fromPack.ID, ToSchemaID: toPack.ID}},
		Status:       "completed",
		CreatedAt:    time.Now(),
	}); err != nil {
		t.Fatalf("create migration job: %v", err)
	}

	// flag=false: data-only rollback must leave the registry untouched.
	if _, err := svc.RollbackSchemaMigration(ctx, projectID, &schemas.SchemaMigrationRollbackRequest{
		ToVersion:           "2.0.0",
		RestoreTypeRegistry: false,
	}); err != nil {
		t.Fatalf("rollback flag=false: %v", err)
	}
	personAfterFalse := registryRows(t, ctx, db, projectID, "Person")
	if len(personAfterFalse) != 1 || personAfterFalse[0].SchemaID == nil || *personAfterFalse[0].SchemaID != toPack.ID {
		t.Fatalf("flag=false must not touch registry, got %+v", personAfterFalse)
	}
	if orgRows := registryRows(t, ctx, db, projectID, "Organization"); len(orgRows) != 1 {
		t.Fatalf("flag=false must leave to-only rows, got %d", len(orgRows))
	}

	// flag=true: full rollback restores the registry.
	if _, err := svc.RollbackSchemaMigration(ctx, projectID, &schemas.SchemaMigrationRollbackRequest{
		ToVersion:           "2.0.0",
		RestoreTypeRegistry: true,
	}); err != nil {
		t.Fatalf("rollback flag=true: %v", err)
	}
	personAfterTrue := registryRows(t, ctx, db, projectID, "Person")
	if len(personAfterTrue) != 1 || personAfterTrue[0].SchemaID == nil || *personAfterTrue[0].SchemaID != fromPack.ID {
		t.Fatalf("flag=true must restore Person to from-pack, got %+v", personAfterTrue)
	}
	if strings.Contains(string(personAfterTrue[0].JSON), `"age"`) {
		t.Errorf("flag=true Person must use from-pack definition, got %s", personAfterTrue[0].JSON)
	}
	if orgRows := registryRows(t, ctx, db, projectID, "Organization"); len(orgRows) != 0 {
		t.Fatalf("flag=true must remove to-only rows, got %d", len(orgRows))
	}
}

func TestAssignPackWithTypesRegistryWrites(t *testing.T) {
	ctx, repo, db, projectID, _ := setupRollbackTest(t)

	pack := insertRollbackPackWithConfig(t, ctx, db, "Assign Pack", "1.0.0",
		`[{"name":"Person","properties":{"name":{"type":"string"}}},{"name":"Belief","properties":{"text":{"type":"string"}}}]`,
		`{"Person":{"icon":"lucide--user"}}`,
		`{"Belief":{"prompt":"extract belief"}}`)

	res, err := repo.AssignPackWithTypes(ctx, projectID, testutil.AdminUser.ID, &schemas.AssignPackRequest{SchemaID: pack.ID})
	if err != nil {
		t.Fatalf("assign pack: %v", err)
	}
	if len(res.InstalledTypes) != 2 {
		t.Fatalf("installed types = %v, want 2", res.InstalledTypes)
	}

	person := registryRows(t, ctx, db, projectID, "Person")
	if len(person) != 1 || person[0].SchemaID == nil || *person[0].SchemaID != pack.ID {
		t.Fatalf("Person row not installed from pack: %+v", person)
	}
	var personRow struct {
		UIConfig string `bun:"ui_config"`
	}
	if err := db.NewRaw(`SELECT ui_config FROM kb.project_object_schema_registry WHERE project_id = ? AND type_name = 'Person'`, projectID).Scan(ctx, &personRow); err != nil {
		t.Fatalf("read ui_config: %v", err)
	}
	if !strings.Contains(personRow.UIConfig, "lucide--user") {
		t.Errorf("Person ui_config not applied: %s", personRow.UIConfig)
	}

	// Merge a second pack: add a new property to the existing Person type.
	pack2 := insertRollbackPack(t, ctx, db, "Assign Pack Merge", "1.0.0",
		`[{"name":"Person","properties":{"name":{"type":"string"},"nickname":{"type":"string"}}}]`)
	res2, err := repo.AssignPackWithTypes(ctx, projectID, testutil.AdminUser.ID, &schemas.AssignPackRequest{
		SchemaID: pack2.ID,
		Merge:    true,
	})
	if err != nil {
		t.Fatalf("merge assign: %v", err)
	}
	if len(res2.MergedTypes) != 1 || res2.MergedTypes[0] != "Person" {
		t.Fatalf("merged types = %v, want [Person]", res2.MergedTypes)
	}
	person = registryRows(t, ctx, db, projectID, "Person")
	if len(person) != 1 || !strings.Contains(string(person[0].JSON), `"nickname"`) {
		t.Fatalf("merge did not extend Person schema: %+v", person)
	}

	// Dry-run must not write anything.
	pack3 := insertRollbackPack(t, ctx, db, "Assign Pack Dry", "1.0.0",
		`[{"name":"DryType","properties":{"x":{"type":"string"}}}]`)
	if _, err := repo.AssignPackWithTypes(ctx, projectID, testutil.AdminUser.ID, &schemas.AssignPackRequest{
		SchemaID: pack3.ID,
		DryRun:   true,
	}); err != nil {
		t.Fatalf("dry-run assign: %v", err)
	}
	if rows := registryRows(t, ctx, db, projectID, "DryType"); len(rows) != 0 {
		t.Fatalf("dry-run must not write registry rows, got %d", len(rows))
	}
}

func TestRollbackSchemaMigrationUnresolvablePackFails(t *testing.T) {
	ctx, repo, db, projectID, cfg := setupRollbackTest(t)
	svc := newRollbackService(t, db, repo, cfg)

	// No migration job and no run record: the explicit flag must error, not
	// silently no-op.
	if _, err := svc.RollbackSchemaMigration(ctx, projectID, &schemas.SchemaMigrationRollbackRequest{
		ToVersion:           "9.9.9",
		RestoreTypeRegistry: true,
	}); err == nil {
		t.Fatal("expected error when from/to packs cannot be resolved")
	}

	// Job exists but its from-pack row is gone: still a loud error.
	fromPack := insertRollbackPack(t, ctx, db, "Gone Pack", "1.0.0", `[]`)
	toPack := insertRollbackPack(t, ctx, db, "Gone Pack", "2.0.0", `[]`)
	if err := repo.CreateMigrationJob(ctx, &schemas.SchemaMigrationJob{
		ProjectID:    projectID,
		FromSchemaID: fromPack.ID,
		ToSchemaID:   toPack.ID,
		Chain:        []schemas.MigrationHop{{FromSchemaID: fromPack.ID, ToSchemaID: toPack.ID}},
		Status:       "completed",
		CreatedAt:    time.Now(),
	}); err != nil {
		t.Fatalf("create migration job: %v", err)
	}
	if _, err := db.NewRaw(`DELETE FROM kb.graph_schemas WHERE id = uuid(?)`, fromPack.ID).Exec(ctx); err != nil {
		t.Fatalf("delete from-pack: %v", err)
	}
	if _, err := svc.RollbackSchemaMigration(ctx, projectID, &schemas.SchemaMigrationRollbackRequest{
		ToVersion:           "2.0.0",
		RestoreTypeRegistry: true,
	}); err == nil {
		t.Fatal("expected error when the from-pack cannot be resolved")
	}
}

// assignPackToProject makes a pack discoverable by GetProjectPackByVersion.
func assignPackToProject(t *testing.T, ctx context.Context, db bun.IDB, projectID, schemaID string) {
	t.Helper()
	_, err := db.NewRaw(`
		INSERT INTO kb.project_schemas
		(id, project_id, schema_id, active, installed_at, created_at, updated_at)
		VALUES (uuid(?), uuid(?), uuid(?), true, now(), now(), now())
	`, uuid.NewString(), projectID, schemaID).Exec(ctx)
	if err != nil {
		t.Fatalf("assign pack to project: %v", err)
	}
}

// TestRollbackSchemaMigrationVersionOnlyFallback covers the fallback resolution
// path used when no migration job exists (a synchronous execute): the migration
// is identified from the version-only kb.schema_migration_runs record.
func TestRollbackSchemaMigrationVersionOnlyFallback(t *testing.T) {
	ctx, repo, db, projectID, cfg := setupRollbackTest(t)
	svc := newRollbackService(t, db, repo, cfg)

	fromPack := insertRollbackPack(t, ctx, db, "Fallback Pack", "1.0.0", `[
		{"name":"Person","properties":{"name":{"type":"string"}}}
	]`)
	toPack := insertRollbackPack(t, ctx, db, "Fallback Pack", "2.0.0", `[
		{"name":"Person","properties":{"name":{"type":"string"},"age":{"type":"number"}}},
		{"name":"Organization","properties":{"title":{"type":"string"}}}
	]`)
	assignPackToProject(t, ctx, db, projectID, toPack.ID)
	insertRegistryRow(t, ctx, db, projectID, "Person", toPack.ID, `{"properties":{"name":{"type":"string"},"age":{"type":"number"}}}`)
	insertRegistryRow(t, ctx, db, projectID, "Organization", toPack.ID, `{"properties":{"title":{"type":"string"}}}`)

	if _, err := db.NewRaw(`
		INSERT INTO kb.schema_migration_runs
		(project_id, from_version, to_version, status, total_objects, successful, failed)
		VALUES (uuid(?), ?, ?, 'completed', 0, 0, 0)
	`, projectID, fromPack.Version, toPack.Version).Exec(ctx); err != nil {
		t.Fatalf("insert migration run: %v", err)
	}

	if _, err := svc.RollbackSchemaMigration(ctx, projectID, &schemas.SchemaMigrationRollbackRequest{
		ToVersion:           "2.0.0",
		RestoreTypeRegistry: true,
	}); err != nil {
		t.Fatalf("rollback via version-only fallback: %v", err)
	}

	person := registryRows(t, ctx, db, projectID, "Person")
	if len(person) != 1 || person[0].SchemaID == nil || *person[0].SchemaID != fromPack.ID {
		t.Fatalf("fallback must restore Person to from-pack, got %+v", person)
	}
	if orgRows := registryRows(t, ctx, db, projectID, "Organization"); len(orgRows) != 0 {
		t.Fatalf("fallback must remove to-only rows, got %d", len(orgRows))
	}
}

// TestExecuteSchemaMigrationRecordsRun verifies the previously-broken INSERT
// now writes a run row with the human version keys the rollback path reads.
func TestExecuteSchemaMigrationRecordsRun(t *testing.T) {
	ctx, repo, db, projectID, cfg := setupRollbackTest(t)
	svc := newRollbackService(t, db, repo, cfg)

	fromPack := insertRollbackPack(t, ctx, db, "Run Pack", "1.0.0", `[
		{"name":"Person","properties":{"name":{"type":"string"}}}
	]`)
	toPack := insertRollbackPack(t, ctx, db, "Run Pack", "2.0.0", `[
		{"name":"Person","properties":{"name":{"type":"string"}}}
	]`)

	if _, err := svc.ExecuteSchemaMigration(ctx, projectID, &schemas.SchemaMigrationExecuteRequest{
		FromSchemaID: fromPack.ID,
		ToSchemaID:   toPack.ID,
	}); err != nil {
		t.Fatalf("execute migration: %v", err)
	}

	var runs []struct {
		FromVersion string `bun:"from_version"`
		ToVersion   string `bun:"to_version"`
	}
	if err := db.NewRaw(`SELECT from_version, to_version FROM kb.schema_migration_runs WHERE project_id = ?`, projectID).Scan(ctx, &runs); err != nil {
		t.Fatalf("read runs: %v", err)
	}
	if len(runs) != 1 || runs[0].FromVersion != "1.0.0" || runs[0].ToVersion != "2.0.0" {
		t.Fatalf("run record = %+v, want one 1.0.0 -> 2.0.0", runs)
	}
}
