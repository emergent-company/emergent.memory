package schemas_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/schemas"
)

// This file is the regression test for the silent no-op rollback: the service
// listed objects through graph.Repository.List, whose explicit projection omits
// the migration_archive column. Every object therefore carried an empty archive
// and RollbackSchemaMigration restored zero objects while still returning
// success. The test drives a real forward migration (which archives/drops a
// property), then the rollback, and asserts the property is restored on the
// object row.

// insertRollbackGraphObject inserts a HEAD graph_object row for the data-restore
// test. branch_id/supersedes_id are NULL so the object is visible to the
// project-wide and type-filtered listings used by migration and rollback.
func insertRollbackGraphObject(t *testing.T, ctx context.Context, db bun.IDB, projectID uuid.UUID, objType, schemaVersion string, properties map[string]any) uuid.UUID {
	t.Helper()
	id := uuid.New()
	props, err := json.Marshal(properties)
	if err != nil {
		t.Fatalf("marshal properties: %v", err)
	}
	_, err = db.NewRaw(`
		INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, status,
			 properties, labels, content_hash, created_at, updated_at, schema_version, migration_archive)
		VALUES
			(?, ?, NULL, ?, NULL, 1, ?, 'active',
			 ?, '{}'::text[], ?, NOW(), NOW(), ?, '[]'::jsonb)
	`, id, projectID, id, objType, string(props), fmt.Sprintf("hash-%s", id.String()), schemaVersion).Exec(ctx)
	if err != nil {
		t.Fatalf("insert graph object: %v", err)
	}
	return id
}

type rollbackObjectState struct {
	Properties       map[string]any   `bun:"properties"`
	SchemaVersion    *string          `bun:"schema_version"`
	MigrationArchive []map[string]any `bun:"migration_archive"`
}

func readRollbackObjectState(t *testing.T, ctx context.Context, db bun.IDB, id uuid.UUID) rollbackObjectState {
	t.Helper()
	var state rollbackObjectState
	if err := db.NewRaw(`
		SELECT properties, schema_version, migration_archive
		FROM kb.graph_objects
		WHERE id = ?
	`, id).Scan(ctx, &state); err != nil {
		t.Fatalf("read graph object state: %v", err)
	}
	return state
}

func TestRollbackSchemaMigrationRestoresArchivedProperty(t *testing.T) {
	ctx, repo, db, projectID, cfg := setupRollbackTest(t)
	svc := newRollbackService(t, db, repo, cfg)

	fromPack := insertRollbackPack(t, ctx, db, "Data Restore Pack", "1.0.0", `[
		{"name":"Person","properties":{"name":{"type":"string"},"old_field":{"type":"string"}}}
	]`)
	toPack := insertRollbackPack(t, ctx, db, "Data Restore Pack", "2.0.0", `[
		{"name":"Person","properties":{"name":{"type":"string"}}}
	]`)

	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		t.Fatalf("parse project id: %v", err)
	}
	objID := insertRollbackGraphObject(t, ctx, db, projectUUID, "Person", fromPack.Version, map[string]any{
		"name":      "John Doe",
		"old_field": "deprecated value",
	})

	// Forward migration: old_field exists in the from-pack but not the to-pack,
	// so it is dropped from the object and archived.
	if _, err := svc.ExecuteSchemaMigration(ctx, projectID, &schemas.SchemaMigrationExecuteRequest{
		FromSchemaID: fromPack.ID,
		ToSchemaID:   toPack.ID,
		Force:        true,
	}); err != nil {
		t.Fatalf("execute migration: %v", err)
	}

	afterMigration := readRollbackObjectState(t, ctx, db, objID)
	if _, present := afterMigration.Properties["old_field"]; present {
		t.Fatalf("migration should have dropped old_field, got %v", afterMigration.Properties)
	}
	if afterMigration.SchemaVersion == nil || *afterMigration.SchemaVersion != toPack.Version {
		t.Fatalf("schema_version after migration = %v, want %q", afterMigration.SchemaVersion, toPack.Version)
	}
	if len(afterMigration.MigrationArchive) == 0 {
		t.Fatal("migration should have archived the dropped property")
	}

	// Rollback must read migration_archive from the database and restore the
	// dropped property. Before the fix the archive column was never selected,
	// so the archive appeared empty and nothing was restored.
	resp, err := svc.RollbackSchemaMigration(ctx, projectID, &schemas.SchemaMigrationRollbackRequest{
		ToVersion: toPack.Version,
	})
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if resp.ObjectsRestored != 1 {
		t.Fatalf("ObjectsRestored = %d, want 1 (rollback silently restored nothing)", resp.ObjectsRestored)
	}

	afterRollback := readRollbackObjectState(t, ctx, db, objID)
	if got, want := afterRollback.Properties["old_field"], "deprecated value"; got != want {
		t.Fatalf("old_field after rollback = %v, want %q (properties=%v)", got, want, afterRollback.Properties)
	}
	if afterRollback.SchemaVersion == nil || *afterRollback.SchemaVersion != fromPack.Version {
		t.Fatalf("schema_version after rollback = %v, want %q", afterRollback.SchemaVersion, fromPack.Version)
	}
	if len(afterRollback.MigrationArchive) != 0 {
		t.Fatalf("archive entry should be consumed by rollback, got %v", afterRollback.MigrationArchive)
	}
}

// TestRollbackSchemaMigrationIgnoresArchiveForOtherVersion pins the precision of
// the archive match: an object whose only archive entry targets a different
// to_version must not be restored or counted, even though the rollback can now
// see its archive.
func TestRollbackSchemaMigrationIgnoresArchiveForOtherVersion(t *testing.T) {
	ctx, repo, db, projectID, cfg := setupRollbackTest(t)
	svc := newRollbackService(t, db, repo, cfg)

	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		t.Fatalf("parse project id: %v", err)
	}
	// Archive entry targets 3.0.0, not the 2.0.0 being rolled back.
	objID := insertRollbackGraphObject(t, ctx, db, projectUUID, "Person", "3.0.0", map[string]any{
		"name": "Jane Doe",
	})
	if _, err := db.NewRaw(`
		UPDATE kb.graph_objects
		SET migration_archive = '[{"from_version":"2.0.0","to_version":"3.0.0","dropped_data":{"old_field":"keep me"}}]'::jsonb
		WHERE id = ?
	`, objID).Exec(ctx); err != nil {
		t.Fatalf("seed migration archive: %v", err)
	}

	resp, err := svc.RollbackSchemaMigration(ctx, projectID, &schemas.SchemaMigrationRollbackRequest{
		ToVersion: "2.0.0",
	})
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if resp.ObjectsRestored != 0 {
		t.Fatalf("ObjectsRestored = %d, want 0 for a non-matching to_version", resp.ObjectsRestored)
	}

	after := readRollbackObjectState(t, ctx, db, objID)
	if _, present := after.Properties["old_field"]; present {
		t.Fatalf("old_field must not be restored from a non-matching archive, got %v", after.Properties)
	}
	if len(after.MigrationArchive) != 1 {
		t.Fatalf("non-matching archive entry must be left intact, got %v", after.MigrationArchive)
	}
}
