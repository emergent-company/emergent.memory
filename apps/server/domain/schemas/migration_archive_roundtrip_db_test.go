package schemas_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/schemas"
)

// These tests exercise the schema migrate/rollback data path end to end against
// a live database: packs are inserted directly, an object is inserted directly,
// then the real Service methods the HTTP handlers call are invoked.
//
// They cover two defects in the same root cause:
//   - Repository.List omitted the migration_archive column, so every rollback
//     skipped every object and restored nothing.
//   - The forward-migrate path overwrote (rather than appended to) the archive
//     on each hop, so a second migration destroyed the first hop's entries.
//   - List was capped at one page, so objects beyond MaxListLimit were ignored.
//
// Every test skips cleanly when Postgres is unavailable. Helpers
// (setupRollbackTest, newRollbackService, insertRollbackPack) live in
// restore_type_registry_db_test.go.

type archiveObjectRow struct {
	Properties       json.RawMessage `bun:"properties"`
	SchemaVersion    *string         `bun:"schema_version"`
	MigrationArchive json.RawMessage `bun:"migration_archive"`
}

func insertArchiveTestObject(t *testing.T, ctx context.Context, db bun.IDB, projectID, typ, propsJSON, schemaVersion string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.NewRaw(`
		INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, status,
			 properties, labels, schema_version, created_at, updated_at, migration_archive)
		VALUES (uuid(?), uuid(?), NULL, uuid(?), NULL, 1, ?, 'active', ?::jsonb, '{}'::text[], ?, now(), now(), '[]'::jsonb)
	`, id, projectID, id, typ, propsJSON, schemaVersion).Exec(ctx)
	if err != nil {
		t.Fatalf("insert %s object: %v", typ, err)
	}
	return id
}

func readArchiveTestObject(t *testing.T, ctx context.Context, db bun.IDB, id string) archiveObjectRow {
	t.Helper()
	var row archiveObjectRow
	if err := db.NewRaw(`
		SELECT properties, schema_version, migration_archive
		FROM kb.graph_objects WHERE id = uuid(?)
	`, id).Scan(ctx, &row); err != nil {
		t.Fatalf("read object %s: %v", id, err)
	}
	return row
}

func archiveEntryVersions(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var entries []map[string]any
	require.NoError(t, json.Unmarshal(raw, &entries), "parse migration_archive %s", raw)
	versions := make([]string, 0, len(entries))
	for _, e := range entries {
		versions = append(versions, e["to_version"].(string))
	}
	return versions
}

func decodeArchiveProps(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var props map[string]any
	require.NoError(t, json.Unmarshal(raw, &props), "parse properties %s", raw)
	return props
}

// TestSchemaMigrationRollbackRoundTripRestoresArchive is the primary
// regression test: a dropped property must be archived by the forward
// migration, then restored (and its archive entry consumed) by rollback.
// It runs a two-hop migration so the second hop also proves the archive is
// appended to, not clobbered.
func TestSchemaMigrationRollbackRoundTripRestoresArchive(t *testing.T) {
	ctx, repo, db, projectID, cfg := setupRollbackTest(t)
	svc := newRollbackService(t, db, repo, cfg)

	v1 := insertRollbackPack(t, ctx, db, "Archive Roundtrip Pack", "1.0.0", `[
		{"name":"Doc","properties":{"title":{"type":"string"},"alpha":{"type":"string"},"beta":{"type":"string"}}}
	]`)
	v2 := insertRollbackPack(t, ctx, db, "Archive Roundtrip Pack", "2.0.0", `[
		{"name":"Doc","properties":{"title":{"type":"string"},"beta":{"type":"string"}}}
	]`)
	v3 := insertRollbackPack(t, ctx, db, "Archive Roundtrip Pack", "3.0.0", `[
		{"name":"Doc","properties":{"title":{"type":"string"}}}
	]`)

	objID := insertArchiveTestObject(t, ctx, db, projectID, "Doc",
		`{"title":"doc","alpha":"first-hop","beta":"second-hop"}`, "1.0.0")

	// Hop 1 (1.0.0 -> 2.0.0) drops alpha.
	resp1, err := svc.ExecuteSchemaMigration(ctx, projectID, &schemas.SchemaMigrationExecuteRequest{
		FromSchemaID: v1.ID,
		ToSchemaID:   v2.ID,
		Force:        true,
	})
	require.NoError(t, err)
	require.Equal(t, 1, resp1.ObjectsMigrated)

	row := readArchiveTestObject(t, ctx, db, objID)
	props := decodeArchiveProps(t, row.Properties)
	assert.NotContains(t, props, "alpha", "dropped property must leave properties")
	assert.Equal(t, "second-hop", props["beta"])
	assert.Equal(t, "doc", props["title"])
	require.NotNil(t, row.SchemaVersion)
	assert.Equal(t, "2.0.0", *row.SchemaVersion)
	require.Equal(t, []string{"2.0.0"}, archiveEntryVersions(t, row.MigrationArchive))

	// Hop 2 (2.0.0 -> 3.0.0) drops beta. It must APPEND to the archive, keeping
	// hop 1's entry — the old code overwrote the whole column from an
	// always-empty slice.
	resp2, err := svc.ExecuteSchemaMigration(ctx, projectID, &schemas.SchemaMigrationExecuteRequest{
		FromSchemaID: v2.ID,
		ToSchemaID:   v3.ID,
		Force:        true,
	})
	require.NoError(t, err)
	require.Equal(t, 1, resp2.ObjectsMigrated)

	row = readArchiveTestObject(t, ctx, db, objID)
	props = decodeArchiveProps(t, row.Properties)
	assert.NotContains(t, props, "beta", "dropped property must leave properties")
	require.Equal(t, []string{"2.0.0", "3.0.0"}, archiveEntryVersions(t, row.MigrationArchive),
		"second hop must retain the first hop's archive entry")

	// Roll back hop 2: beta restored, its archive entry consumed.
	rb1, err := svc.RollbackSchemaMigration(ctx, projectID, &schemas.SchemaMigrationRollbackRequest{
		ToVersion: "3.0.0",
	})
	require.NoError(t, err)
	require.Equal(t, 1, rb1.ObjectsRestored, "rollback must restore the archived object")

	row = readArchiveTestObject(t, ctx, db, objID)
	props = decodeArchiveProps(t, row.Properties)
	assert.Equal(t, "second-hop", props["beta"])
	assert.NotContains(t, props, "alpha", "hop-1 data must stay archived until hop-1 rollback")
	require.Equal(t, []string{"2.0.0"}, archiveEntryVersions(t, row.MigrationArchive))

	// Roll back hop 1: alpha restored, archive fully consumed.
	rb2, err := svc.RollbackSchemaMigration(ctx, projectID, &schemas.SchemaMigrationRollbackRequest{
		ToVersion: "2.0.0",
	})
	require.NoError(t, err)
	require.Equal(t, 1, rb2.ObjectsRestored)

	row = readArchiveTestObject(t, ctx, db, objID)
	props = decodeArchiveProps(t, row.Properties)
	assert.Equal(t, "first-hop", props["alpha"])
	assert.Equal(t, "second-hop", props["beta"])
	assert.Empty(t, archiveEntryVersions(t, row.MigrationArchive), "consumed archive must be emptied")
}

// TestSchemaMigrationProcessesEveryPage proves migrate and rollback visit every
// object when the project holds more objects than one MaxListLimit-sized page.
func TestSchemaMigrationProcessesEveryPage(t *testing.T) {
	ctx, repo, db, projectID, cfg := setupRollbackTest(t)
	// Force a small page so the multi-page path is exercised without seeding
	// thousands of rows.
	cfg.Graph.MaxListLimit = 2
	svc := newRollbackService(t, db, repo, cfg)

	v1 := insertRollbackPack(t, ctx, db, "Archive Paging Pack", "1.0.0", `[
		{"name":"PagedDoc","properties":{"title":{"type":"string"},"alpha":{"type":"string"}}}
	]`)
	v2 := insertRollbackPack(t, ctx, db, "Archive Paging Pack", "2.0.0", `[
		{"name":"PagedDoc","properties":{"title":{"type":"string"}}}
	]`)

	const total = 5
	ids := make([]string, 0, total)
	for i := 0; i < total; i++ {
		ids = append(ids, insertArchiveTestObject(t, ctx, db, projectID, "PagedDoc",
			`{"title":"paged","alpha":"value"}`, "1.0.0"))
	}

	resp, err := svc.ExecuteSchemaMigration(ctx, projectID, &schemas.SchemaMigrationExecuteRequest{
		FromSchemaID: v1.ID,
		ToSchemaID:   v2.ID,
		Force:        true,
	})
	require.NoError(t, err)
	require.Equal(t, total, resp.ObjectsMigrated, "migrate must cover objects beyond the first page")

	for _, id := range ids {
		row := readArchiveTestObject(t, ctx, db, id)
		assert.NotContains(t, decodeArchiveProps(t, row.Properties), "alpha")
		require.Equal(t, []string{"2.0.0"}, archiveEntryVersions(t, row.MigrationArchive))
	}

	rb, err := svc.RollbackSchemaMigration(ctx, projectID, &schemas.SchemaMigrationRollbackRequest{
		ToVersion: "2.0.0",
	})
	require.NoError(t, err)
	require.Equal(t, total, rb.ObjectsRestored, "rollback must cover objects beyond the first page")

	for _, id := range ids {
		row := readArchiveTestObject(t, ctx, db, id)
		assert.Equal(t, "value", decodeArchiveProps(t, row.Properties)["alpha"])
		assert.Empty(t, archiveEntryVersions(t, row.MigrationArchive))
	}
}
