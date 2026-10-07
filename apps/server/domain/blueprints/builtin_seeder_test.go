package blueprints

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/migrations"
)

// TestMigration00227_BlueprintOrigin verifies the origin-column migration's
// semantics against the (already-migrated) test schema: default 'user', the
// CHECK constraint, the global→builtin backfill, and the origin index. It also
// guards that the migration is embedded (it must not be silently deleted).
func TestMigration00227_BlueprintOrigin(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()

	b, err := migrations.FS.ReadFile("00227_blueprint_origin.sql")
	require.NoError(t, err, "00227_blueprint_origin.sql must be embedded")
	sql := string(b)
	assert.Contains(t, sql, "blueprints_origin_check")
	assert.Contains(t, sql, "blueprints_origin_idx")
	assert.Contains(t, sql, "SET origin = 'builtin' WHERE project_id IS NULL")

	_, projectID := seedProject(t, db)

	globalID := uuid.NewString()
	projectID2 := uuid.NewString()
	insertBlueprintRow(t, db, globalID, uniqueName("mig-global"), "1.0.0", nil)
	insertBlueprintRow(t, db, projectID2, uniqueName("mig-proj"), "1.0.0", &projectID)

	// Default is 'user' for both scopes before any backfill.
	assertOrigin(t, db, globalID, OriginUser)
	assertOrigin(t, db, projectID2, OriginUser)

	// Backfill: global scope becomes builtin.
	_, err = db.ExecContext(ctx, `UPDATE kb.blueprints SET origin = 'builtin' WHERE project_id IS NULL`)
	require.NoError(t, err)
	assertOrigin(t, db, globalID, OriginBuiltin)
	assertOrigin(t, db, projectID2, OriginUser)

	// CHECK constraint rejects an invalid origin value.
	_, err = db.ExecContext(ctx, `UPDATE kb.blueprints SET origin = 'bogus' WHERE id = ?`, projectID2)
	require.Error(t, err, "CHECK constraint must reject an origin outside builtin/user/import")

	// Index exists.
	var idxCount int
	err = db.NewRaw(`SELECT COUNT(*) FROM pg_indexes WHERE schemaname = 'kb' AND tablename = 'blueprints' AND indexname = 'blueprints_origin_idx'`).
		Scan(ctx, &idxCount)
	require.NoError(t, err)
	assert.Equal(t, 1, idxCount)
}

func insertBlueprintRow(t *testing.T, db bun.IDB, id, name, version string, projectID *string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.blueprints (id, name, version, status, manifest, checksum, project_id, created_at, updated_at)
		VALUES (?, ?, ?, 'draft', '{}'::jsonb, '', ?, now(), now())
	`, id, name, version, projectID)
	require.NoError(t, err)
}

func assertOrigin(t *testing.T, db bun.IDB, id, want string) {
	t.Helper()
	var got string
	err := db.NewRaw(`SELECT origin FROM kb.blueprints WHERE id = ?`, id).Scan(context.Background(), &got)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// TestBuiltinSeeder_CreatesGlobals_Idempotent verifies the seeder upserts every
// bundled pack as a global published blueprint (project_id NULL, origin=builtin)
// plus a global source='builtin' schema pack, and that a re-run is idempotent.
func TestBuiltinSeeder_CreatesGlobals_Idempotent(t *testing.T) {
	db := connectTestDB(t)
	repo := NewRepository(db, testLogger())
	seeder := NewBuiltinSeeder(repo, testLogger())
	ctx := context.Background()

	require.NoError(t, seeder.Seed(ctx))

	// 4 bundled packs; 3 carry a schema (operator is agent-only).
	assertBlueprintCount(t, db, 4)
	assertSchemaPackCount(t, db, 3)

	// Every seeded blueprint is a global published builtin with a checksum.
	var rows []struct {
		Name      string  `bun:"name"`
		Version   string  `bun:"version"`
		Status    string  `bun:"status"`
		Origin    string  `bun:"origin"`
		ProjectID *string `bun:"project_id"`
		Checksum  string  `bun:"checksum"`
	}
	err := db.NewRaw(`
		SELECT name, version, status, origin, project_id, checksum
		FROM kb.blueprints
		WHERE origin = 'builtin'
		ORDER BY name
	`).Scan(ctx, &rows)
	require.NoError(t, err)
	assert.Len(t, rows, 4)
	for _, r := range rows {
		assert.Equal(t, StatusPublished, r.Status, "builtin blueprint %s must be published", r.Name)
		assert.Equal(t, OriginBuiltin, r.Origin)
		assert.Nil(t, r.ProjectID, "builtin blueprint %s must be global", r.Name)
		assert.NotEmpty(t, r.Checksum, "builtin blueprint %s must carry a checksum", r.Name)
	}

	// operator's version comes from project.yaml metadata (1.0.1), not the
	// layout default of 1.0.0.
	assertBlueprintVersion(t, db, "operator", "1.0.1")
	assertBlueprintVersion(t, db, "agent-notes", "2.0.0")

	// Re-run: idempotent.
	require.NoError(t, seeder.Seed(ctx))
	assertBlueprintCount(t, db, 4)
	assertSchemaPackCount(t, db, 3)
}

func assertBlueprintCount(t *testing.T, db bun.IDB, want int) {
	t.Helper()
	var n int
	err := db.NewRaw(`SELECT COUNT(*) FROM kb.blueprints WHERE origin = 'builtin'`).Scan(context.Background(), &n)
	require.NoError(t, err)
	assert.Equal(t, want, n)
}

func assertSchemaPackCount(t *testing.T, db bun.IDB, want int) {
	t.Helper()
	var n int
	err := db.NewRaw(`SELECT COUNT(*) FROM kb.graph_schemas WHERE source = 'builtin' AND project_id IS NULL`).Scan(context.Background(), &n)
	require.NoError(t, err)
	assert.Equal(t, want, n)
}

func assertBlueprintVersion(t *testing.T, db bun.IDB, name, want string) {
	t.Helper()
	var got string
	err := db.NewRaw(`SELECT version FROM kb.blueprints WHERE name = ? AND origin = 'builtin' LIMIT 1`, name).
		Scan(context.Background(), &got)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}
