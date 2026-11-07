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

func insertSchemaAssignment(t *testing.T, db bun.IDB, projectID, schemaID, sourceBlueprintID string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.project_schemas (id, project_id, schema_id, source_blueprint_id)
		VALUES (?, ?, ?, ?)
	`, uuid.NewString(), projectID, schemaID, sourceBlueprintID)
	require.NoError(t, err)
}

func insertAgentDefinition(t *testing.T, db bun.IDB, projectID, sourceBlueprintID string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.agent_definitions (id, project_id, name, source_blueprint_id)
		VALUES (?, ?, ?, ?)
	`, id, projectID, "retired-agent-"+id[:8], sourceBlueprintID)
	require.NoError(t, err)
	return id
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
	insertSchemaAssignment(t, db, projectID, schemaID, staleID)
	agentID := insertAgentDefinition(t, db, projectID, staleID)
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

	// Its attribution is cleared, though, so the pointer cannot dangle and block
	// a later same-name blueprint from stamping the assignment.
	assert.Equal(t, 0, countRowsForRetire(t, db,
		`SELECT COUNT(*) FROM kb.project_schemas WHERE source_blueprint_id = ?`, staleID))
	assert.Equal(t, 1, countRowsForRetire(t, db,
		`SELECT COUNT(*) FROM kb.project_schemas WHERE schema_id = ? AND source_blueprint_id IS NULL`, schemaID))

	// Same for the agent definitions the retired blueprint owned: the agent
	// survives, only its attribution is cleared.
	assert.Equal(t, 1, countRowsForRetire(t, db,
		`SELECT COUNT(*) FROM kb.agent_definitions WHERE id = ? AND source_blueprint_id IS NULL`, agentID))

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

// TestBuiltinSeeder_RetirementIgnoresUnappliedAndKeepsBundled covers the
// 'unapplied' shape: the project explicitly uninstalled the blueprint, so
// nothing is live and the row is history rather than an install.
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

// TestBuiltinSeeder_RetirementIsKeyedOnExactVersion guards the rollback case:
// reconcile compares (name, version) pairs, so a row for a version the binary
// does not embed is retired even while another version of the same name is
// embedded — and the embedded version must survive.
func TestBuiltinSeeder_RetirementIsKeyedOnExactVersion(t *testing.T) {
	db := connectTestDB(t)
	seeder := NewBuiltinSeeder(NewRepository(db, testLogger()), testLogger())
	ctx := context.Background()

	require.NoError(t, seeder.Seed(ctx))

	var bundledVersion string
	require.NoError(t, db.NewRaw(`
		SELECT version FROM kb.blueprints
		WHERE name = 'task-board' AND project_id IS NULL
	`).Scan(ctx, &bundledVersion))
	require.NotEmpty(t, bundledVersion)

	_, projectID := seedProject(t, db)
	newerID := insertBuiltinBlueprintRow(t, db, "task-board", "9.9.9")
	insertBlueprintApplication(t, db, newerID, projectID, "9.9.9", "superseded")

	require.NoError(t, seeder.Seed(ctx))

	assert.Equal(t, 0, countRowsForRetire(t, db, `SELECT COUNT(*) FROM kb.blueprints WHERE id = ?`, newerID))
	assert.Equal(t, 1, countRowsForRetire(t, db,
		`SELECT COUNT(*) FROM kb.blueprints WHERE name = 'task-board' AND version = ? AND project_id IS NULL`, bundledVersion))
}

// TestBuiltinSeeder_LeavesGlobalRowsTheSeederDidNotWrite: migration 00228
// backfilled every pre-existing global row to origin='builtin', so a global row
// can exist that this binary never wrote. Seed always writes published + a
// checksum, so anything else is not attributable to it and must not be
// hard-deleted as stale — prod carries exactly such a row (alfred-assistant
// 1.0.0, draft, empty checksum) and the origin backfill never touched its
// content.
func TestBuiltinSeeder_LeavesGlobalRowsTheSeederDidNotWrite(t *testing.T) {
	db := connectTestDB(t)
	seeder := NewBuiltinSeeder(NewRepository(db, testLogger()), testLogger())
	ctx := context.Background()

	require.NoError(t, seeder.Seed(ctx))

	legacyID := uuid.NewString()
	_, err := db.ExecContext(ctx, `
		INSERT INTO kb.blueprints (id, name, version, status, manifest, checksum, project_id, origin, created_at, updated_at)
		VALUES (?, 'alfred-assistant', '1.0.0', 'draft', '{}'::jsonb, '', NULL, 'builtin', now(), now())
	`, legacyID)
	require.NoError(t, err)

	require.NoError(t, seeder.Seed(ctx))

	assert.Equal(t, 1, countRowsForRetire(t, db, `SELECT COUNT(*) FROM kb.blueprints WHERE id = ?`, legacyID))
	assertBlueprintCount(t, db, 6)
}
