package blueprints

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// insertBuiltinBlueprintRow inserts a global (project_id NULL) published
// origin='builtin' blueprint, the shape the seeder writes on every boot.
func insertBuiltinBlueprintRow(t *testing.T, db bun.IDB, name, version string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.blueprints (id, name, version, status, manifest, checksum, project_id, origin, created_at, updated_at)
		VALUES (?, ?, ?, 'published', '{}'::jsonb, 'sum', NULL, 'builtin', now(), now())
	`, id, name, version)
	require.NoError(t, err)
	return id
}

func insertBlueprintApplication(t *testing.T, db bun.IDB, blueprintID, projectID, version, status string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.blueprint_applications (blueprint_id, project_id, version, checksum, status)
		VALUES (?, ?, ?, '', ?)
	`, blueprintID, projectID, version, status)
	require.NoError(t, err)
}

func countRowsForRetire(t *testing.T, db bun.IDB, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, db.NewRaw(query, args...).Scan(context.Background(), &n))
	return n
}

// TestBuiltinSeeder_RetiresStaleBuiltin covers the case the retirement exists
// for: a built-in that an earlier release shipped and a later release moved out
// to an external blueprint (personal-memory, #1513). Its row is unreachable —
// built-ins carry no Remove action and DeleteBlueprint refuses origin='builtin' —
// so without retirement every long-lived deployment keeps advertising a stale
// Built-in entry in the gallery for ever.
func TestBuiltinSeeder_RetiresStaleBuiltin(t *testing.T) {
	db := connectTestDB(t)
	repo := NewRepository(db, testLogger())
	seeder := NewBuiltinSeeder(repo, testLogger())
	ctx := context.Background()

	require.NoError(t, seeder.Seed(ctx))
	assertBlueprintCount(t, db, 5)

	_, projectID := seedProject(t, db)
	staleID := insertBuiltinBlueprintRow(t, db, "personal-memory", "2.0.0")
	schemaID := insertSchemaPack(t, db, projectID, "personal-memory", "2.0.0")
	insertPackClaim(t, db, staleID, projectID, schemaID)
	insertBlueprintApplication(t, db, staleID, projectID, "2.0.0", "superseded")

	// A project-scoped blueprint of the same name is the user's own install
	// history and must not be touched: retirement is scoped to global builtins.
	insertBlueprintRow(t, db, uuid.NewString(), "personal-memory", "2.1.0", &projectID)

	require.NoError(t, seeder.Seed(ctx))

	assertBlueprintCount(t, db, 5)
	assert.Equal(t, 0, countRowsForRetire(t, db, `SELECT COUNT(*) FROM kb.blueprints WHERE id = ?`, staleID))
	assertPackClaimCount(t, db, staleID, 0)
	assert.Equal(t, 0, countRowsForRetire(t, db, `SELECT COUNT(*) FROM kb.blueprint_applications WHERE blueprint_id = ?`, staleID))

	// The materialized schema pack is project data and outlives the gallery
	// entry: retiring the blueprint must never delete a project's schema.
	assert.Equal(t, 1, countRowsForRetire(t, db, `SELECT COUNT(*) FROM kb.graph_schemas WHERE id = ?`, schemaID))

	// The project's own personal-memory 2.1.0 survives.
	assert.Equal(t, 1, countRowsForRetire(t, db,
		`SELECT COUNT(*) FROM kb.blueprints WHERE name = 'personal-memory' AND version = '2.1.0' AND project_id = ?`, projectID))

	// Re-running is idempotent and the bundled packs are untouched.
	require.NoError(t, seeder.Seed(ctx))
	assertBlueprintCount(t, db, 5)
}

// TestBuiltinSeeder_KeepsAppliedStaleBuiltin: a project currently on the
// blueprint means the record is its install provenance, so retirement leaves it
// in place rather than silently detaching a live install.
func TestBuiltinSeeder_KeepsAppliedStaleBuiltin(t *testing.T) {
	db := connectTestDB(t)
	seeder := NewBuiltinSeeder(NewRepository(db, testLogger()), testLogger())
	ctx := context.Background()

	require.NoError(t, seeder.Seed(ctx))

	_, projectID := seedProject(t, db)
	staleID := insertBuiltinBlueprintRow(t, db, "retired-pack", "1.0.0")
	insertBlueprintApplication(t, db, staleID, projectID, "1.0.0", "applied")

	require.NoError(t, seeder.Seed(ctx))

	assert.Equal(t, 1, countRowsForRetire(t, db, `SELECT COUNT(*) FROM kb.blueprints WHERE id = ?`, staleID))
	assert.Equal(t, 1, countRowsForRetire(t, db, `SELECT COUNT(*) FROM kb.blueprint_applications WHERE blueprint_id = ?`, staleID))
}

// TestBuiltinSeeder_RetirementIgnoresUnappliedAndKeepsBundled guards the two
// other shapes: an 'unapplied' application is history (the project explicitly
// uninstalled it, so nothing is live) and a bundled pack must never be retired
// even when a same-named row exists at another version.
func TestBuiltinSeeder_RetirementIgnoresUnappliedAndKeepsBundled(t *testing.T) {
	db := connectTestDB(t)
	seeder := NewBuiltinSeeder(NewRepository(db, testLogger()), testLogger())
	ctx := context.Background()

	require.NoError(t, seeder.Seed(ctx))

	_, projectID := seedProject(t, db)
	goneID := insertBuiltinBlueprintRow(t, db, "uninstalled-pack", "1.0.0")
	insertBlueprintApplication(t, db, goneID, projectID, "1.0.0", "unapplied")

	require.NoError(t, seeder.Seed(ctx))

	assert.Equal(t, 0, countRowsForRetire(t, db, `SELECT COUNT(*) FROM kb.blueprints WHERE id = ?`, goneID))
	// All five bundled packs survive a retirement pass.
	assertBlueprintCount(t, db, 5)
}
