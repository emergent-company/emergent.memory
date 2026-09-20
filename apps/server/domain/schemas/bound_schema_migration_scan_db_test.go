package schemas_test

import (
	"context"
	"log/slog"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/schemas"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// These tests pin the bounded-scan behaviour of the schema migrate/rollback
// paths: forward migration must still visit archive-free objects, rollback must
// scan only archive-carrying objects, and the synchronous request path must
// respect a configurable hard cap plus a per-request MaxObjects cap.
//
// Every test skips cleanly when Postgres is unavailable. Helpers
// (setupRollbackTest, newRollbackService, insertRollbackPack) live in
// restore_type_registry_db_test.go.

// seedArchivedObject inserts a "Doc" object at schema_version 2.0.0 whose
// migration_archive holds a single entry archiving the dropped "alpha" property
// (from_version 1.0.0 → to_version 2.0.0).
func seedArchivedObject(t *testing.T, ctx context.Context, db bun.IDB, projectID, alpha string) string {
	t.Helper()
	id := insertArchiveTestObject(t, ctx, db, projectID, "Doc", `{"title":"doc"}`, "2.0.0")
	if _, err := db.NewRaw(`
		UPDATE kb.graph_objects
		SET migration_archive = jsonb_build_array(
			jsonb_build_object('from_version','1.0.0','to_version','2.0.0',
				'dropped_data', jsonb_build_object('alpha', ?))
		)
		WHERE id = uuid(?)
	`, alpha, id).Exec(ctx); err != nil {
		t.Fatalf("set archive on %s: %v", id, err)
	}
	return id
}

// TestExecuteMigratesEmptyArchiveObjects is the regression guard for the
// correctness constraint: the archive predicate must NOT be applied to forward
// migration. Objects whose archive is empty (a first-ever migration, or objects
// created after a prior migration) must still be migrated and counted.
func TestExecuteMigratesEmptyArchiveObjects(t *testing.T) {
	ctx, repo, db, projectID, cfg := setupRollbackTest(t)
	svc := newRollbackService(t, db, repo, cfg)

	v1 := insertRollbackPack(t, ctx, db, "Empty Archive Pack", "1.0.0", `[
		{"name":"Doc","properties":{"title":{"type":"string"},"alpha":{"type":"string"}}}
	]`)
	v2 := insertRollbackPack(t, ctx, db, "Empty Archive Pack", "2.0.0", `[
		{"name":"Doc","properties":{"title":{"type":"string"}}}
	]`)

	const total = 4
	ids := make([]string, 0, total)
	for i := 0; i < total; i++ {
		ids = append(ids, insertArchiveTestObject(t, ctx, db, projectID, "Doc",
			`{"title":"doc","alpha":"keep"}`, "1.0.0"))
	}

	resp, err := svc.ExecuteSchemaMigration(ctx, projectID, &schemas.SchemaMigrationExecuteRequest{
		FromSchemaID: v1.ID,
		ToSchemaID:   v2.ID,
		Force:        true,
	})
	require.NoError(t, err)
	require.Equal(t, total, resp.ObjectsMigrated,
		"objects with EMPTY archives must still be migrated and counted")

	for _, id := range ids {
		row := readArchiveTestObject(t, ctx, db, id)
		assert.NotContains(t, decodeArchiveProps(t, row.Properties), "alpha")
		assert.Equal(t, []string{"2.0.0"}, archiveEntryVersions(t, row.MigrationArchive))
	}
}

// TestRollbackArchiveFreeProjectRestoresZero proves a rollback over a project
// whose objects carry no archive reports zero restores (and, via
// OnlyWithMigrationArchive, never scans the empty-archive rows).
func TestRollbackArchiveFreeProjectRestoresZero(t *testing.T) {
	ctx, repo, db, projectID, cfg := setupRollbackTest(t)
	svc := newRollbackService(t, db, repo, cfg)

	insertRollbackPack(t, ctx, db, "Archive Free Pack", "1.0.0", `[
		{"name":"Doc","properties":{"title":{"type":"string"},"alpha":{"type":"string"}}}
	]`)
	insertRollbackPack(t, ctx, db, "Archive Free Pack", "2.0.0", `[
		{"name":"Doc","properties":{"title":{"type":"string"}}}
	]`)

	for i := 0; i < 3; i++ {
		insertArchiveTestObject(t, ctx, db, projectID, "Doc", `{"title":"doc","alpha":"x"}`, "1.0.0")
	}

	resp, err := svc.RollbackSchemaMigration(ctx, projectID, &schemas.SchemaMigrationRollbackRequest{
		ToVersion: "2.0.0",
	})
	require.NoError(t, err)
	assert.Equal(t, 0, resp.ObjectsRestored, "archive-free project must restore zero objects")
	assert.Equal(t, 0, resp.ObjectsFailed)
}

// TestRollbackOnlyRestoresArchivedObjects proves rollback restores only the
// matching archive-carrying object and leaves an archive-free sibling untouched.
func TestRollbackOnlyRestoresArchivedObjects(t *testing.T) {
	ctx, repo, db, projectID, cfg := setupRollbackTest(t)
	svc := newRollbackService(t, db, repo, cfg)

	insertRollbackPack(t, ctx, db, "Bound Rollback Pack", "1.0.0", `[
		{"name":"Doc","properties":{"title":{"type":"string"},"alpha":{"type":"string"}}}
	]`)
	insertRollbackPack(t, ctx, db, "Bound Rollback Pack", "2.0.0", `[
		{"name":"Doc","properties":{"title":{"type":"string"}}}
	]`)

	archivedID := seedArchivedObject(t, ctx, db, projectID, "value")
	emptyID := insertArchiveTestObject(t, ctx, db, projectID, "Doc", `{"title":"b"}`, "2.0.0")

	resp, err := svc.RollbackSchemaMigration(ctx, projectID, &schemas.SchemaMigrationRollbackRequest{
		ToVersion: "2.0.0",
	})
	require.NoError(t, err)
	require.Equal(t, 1, resp.ObjectsRestored, "only the archive-carrying object must be restored")

	archived := readArchiveTestObject(t, ctx, db, archivedID)
	assert.Equal(t, "value", decodeArchiveProps(t, archived.Properties)["alpha"])
	assert.Empty(t, archiveEntryVersions(t, archived.MigrationArchive), "consumed archive must be emptied")

	empty := readArchiveTestObject(t, ctx, db, emptyID)
	assert.NotContains(t, decodeArchiveProps(t, empty.Properties), "alpha",
		"archive-free object must not be restored")
}

// TestMigrationScanHardCapAborts proves the configurable hard cap aborts the
// synchronous scan with a clear 4xx and (for rollback) writes nothing.
func TestMigrationScanHardCapAborts(t *testing.T) {
	ctx, repo, db, projectID, cfg := setupRollbackTest(t)
	cfg.Graph.MigrationScanMaxObjects = 2
	svc := newRollbackService(t, db, repo, cfg)

	insertRollbackPack(t, ctx, db, "Cap Pack", "1.0.0", `[
		{"name":"Doc","properties":{"title":{"type":"string"},"alpha":{"type":"string"}}}
	]`)
	insertRollbackPack(t, ctx, db, "Cap Pack", "2.0.0", `[
		{"name":"Doc","properties":{"title":{"type":"string"}}}
	]`)

	ids := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		ids = append(ids, seedArchivedObject(t, ctx, db, projectID, "x"))
	}

	_, err := svc.RollbackSchemaMigration(ctx, projectID, &schemas.SchemaMigrationRollbackRequest{
		ToVersion: "2.0.0",
	})
	require.Error(t, err)

	var apErr *apperror.Error
	require.ErrorAs(t, err, &apErr)
	assert.Equal(t, http.StatusBadRequest, apErr.HTTPStatus)
	assert.Contains(t, apErr.Message, "scan more than 2 objects")

	// The abort happens inside the tx, so nothing is written: every object must
	// still carry its archive entry.
	for _, id := range ids {
		row := readArchiveTestObject(t, ctx, db, id)
		assert.Equal(t, []string{"2.0.0"}, archiveEntryVersions(t, row.MigrationArchive),
			"hard-cap abort must roll back the transaction")
		assert.NotContains(t, decodeArchiveProps(t, row.Properties), "alpha")
	}
}

// TestRollbackMaxObjectsCapsRestoredObjects proves the per-request MaxObjects
// cap bounds how many objects a rollback restores.
func TestRollbackMaxObjectsCapsRestoredObjects(t *testing.T) {
	ctx, repo, db, projectID, cfg := setupRollbackTest(t)
	svc := newRollbackService(t, db, repo, cfg)

	insertRollbackPack(t, ctx, db, "Cap Rollback Pack", "1.0.0", `[
		{"name":"Doc","properties":{"title":{"type":"string"},"alpha":{"type":"string"}}}
	]`)
	insertRollbackPack(t, ctx, db, "Cap Rollback Pack", "2.0.0", `[
		{"name":"Doc","properties":{"title":{"type":"string"}}}
	]`)

	ids := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		ids = append(ids, seedArchivedObject(t, ctx, db, projectID, "v"))
	}

	resp, err := svc.RollbackSchemaMigration(ctx, projectID, &schemas.SchemaMigrationRollbackRequest{
		ToVersion:  "2.0.0",
		MaxObjects: 2,
	})
	require.NoError(t, err)
	require.Equal(t, 2, resp.ObjectsRestored, "MaxObjects must cap restored objects")

	restored := 0
	stillArchived := 0
	for _, id := range ids {
		row := readArchiveTestObject(t, ctx, db, id)
		if decodeArchiveProps(t, row.Properties)["alpha"] == "v" {
			restored++
		} else {
			stillArchived++
		}
	}
	assert.Equal(t, 2, restored)
	assert.Equal(t, 1, stillArchived, "one object must remain un-restored beyond the cap")
}

// TestRollbackRejectsMaxObjectsWithRestoreTypeRegistry is a pure unit test: the
// guard runs immediately after the projectID parse and before any
// repository/graph/DB access, so a Service with nil dependencies is sufficient
// (no Postgres required — this test does NOT skip).
func TestRollbackRejectsMaxObjectsWithRestoreTypeRegistry(t *testing.T) {
	svc := schemas.NewService(nil, nil, slog.Default(), &config.Config{})

	_, err := svc.RollbackSchemaMigration(context.Background(), uuid.NewString(), &schemas.SchemaMigrationRollbackRequest{
		ToVersion:           "2.0.0",
		RestoreTypeRegistry: true,
		MaxObjects:          1,
	})
	require.Error(t, err)

	var apErr *apperror.Error
	require.ErrorAs(t, err, &apErr)
	assert.Equal(t, http.StatusBadRequest, apErr.HTTPStatus)
	assert.Contains(t, apErr.Message, "max_objects cannot be combined with restore_type_registry")
}
