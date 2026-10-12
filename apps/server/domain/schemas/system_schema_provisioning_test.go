package schemas_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/schemas"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/migrations"
)

// TestMigration00235_00236_SystemSchemaFlag verifies the two provisioning-removal
// migrations are embedded and carry the key DDL: 00235 drops the auto-install
// trigger and adds/marks the is_system column; 00236 deletes leftover
// auto-provisioned assignments and orphaned blueprint-pack rows. This guards
// against a silent deletion of the migrations (they must be embedded) and
// against drift in the cleanup predicate.
func TestMigration00235_00236_SystemSchemaFlag(t *testing.T) {
	b235, err := migrations.FS.ReadFile("00235_drop_builtin_schema_provisioning.sql")
	if err != nil {
		t.Fatalf("00235 must be embedded: %v", err)
	}
	b236, err := migrations.FS.ReadFile("00236_cleanup_builtin_schema_provisioning.sql")
	if err != nil {
		t.Fatalf("00236 must be embedded: %v", err)
	}

	s235 := string(b235)
	s236 := string(b236)

	for _, want := range []string{
		"DROP TRIGGER IF EXISTS trg_projects_install_builtins ON kb.projects",
		"DROP FUNCTION IF EXISTS kb.trg_projects_install_builtins()",
		"ADD COLUMN IF NOT EXISTS is_system boolean NOT NULL DEFAULT false",
		"SET is_system = true",
		"source = 'builtin' AND name = 'session-message-types'",
		"CREATE INDEX IF NOT EXISTS graph_schemas_is_system_idx",
		"ON kb.graph_schemas (is_system) WHERE is_system = true",
		"DROP INDEX IF EXISTS kb.graph_schemas_is_system_idx",
	} {
		if !strings.Contains(s235, want) {
			t.Errorf("00235 missing %q", want)
		}
	}

	for _, want := range []string{
		"DELETE FROM kb.project_schemas",
		"DELETE FROM kb.project_object_schema_registry",
		"DELETE FROM kb.project_edge_schema_registry",
		"DELETE FROM kb.graph_schemas",
		"gs.is_system = false",
		"gs.is_system = true",
		"bpa.status = 'applied'",
		"UPDATE kb.project_schemas ps",
		"source_blueprint_id",
		"HAVING COUNT(DISTINCT bpc.blueprint_id) = 1",
		"array_agg(DISTINCT bpc.blueprint_id)",
		"ps.source_blueprint_id IS NULL",
	} {
		if !strings.Contains(s236, want) {
			t.Errorf("00236 missing %q", want)
		}
	}
	// Postgres has no MIN/MAX aggregate over uuid (SQLSTATE 42883); the backfill
	// must select the single claiming blueprint id with array_agg instead.
	if strings.Contains(s236, "MIN(bpc.blueprint_id)") || strings.Contains(s236, "MAX(bpc.blueprint_id)") {
		t.Errorf("00236 must not aggregate uuid with MIN/MAX (use array_agg)")
	}
}

// migrationUpStatements returns the individual SQL statements in a migration's
// "-- +goose Up" body, with goose markers and SQL line comments stripped. The
// provisioning-removal migrations contain no embedded semicolons in the Up path
// (no procedural bodies), so after removing full-line comments splitting on ";"
// is safe and each statement can be executed standalone against a test database.
func migrationUpStatements(t *testing.T, name string) []string {
	t.Helper()
	b, err := migrations.FS.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	s := string(b)
	const up = "-- +goose Up\n"
	i := strings.Index(s, up)
	if i < 0 {
		t.Fatalf("%s missing -- +goose Up marker", name)
	}
	s = s[i+len(up):]
	if j := strings.Index(s, "-- +goose Down"); j >= 0 {
		s = s[:j]
	}

	// Drop full-line SQL comments (including -- +goose StatementBegin/End) so a
	// semicolon inside a comment is not mistaken for a statement boundary.
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		lines = append(lines, line)
	}
	s = strings.Join(lines, "\n")

	var out []string
	for _, stmt := range strings.Split(s, ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		out = append(out, stmt)
	}
	return out
}

// TestSystemSchemasResolvedWithoutAssignment verifies the read-time resolution
// behaviour: after the schemas builtin seeder runs, the Session/Message system
// types are returned by GetCompiledTypesByProject with IsSystem=true, and no
// kb.project_schemas row exists for the system pack.
func TestSystemSchemasResolvedWithoutAssignment(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "sysschemas")
	defer testDB.Close()

	db := testDB.GetDB()
	log := slog.Default()
	repo := schemas.NewRepository(db, log)

	// Seed the single is_system=true registry row.
	if err := schemas.NewBuiltinSeeder(repo, log).Seed(ctx); err != nil {
		t.Fatalf("seed system schemas: %v", err)
	}

	orgID := uuid.NewString()
	if err := testutil.CreateTestOrganization(ctx, db, orgID, "System Schema Org"); err != nil {
		t.Fatalf("create org: %v", err)
	}
	projectID := uuid.NewString()
	if err := testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "System Schema Project",
	}, testutil.AdminUser.ID); err != nil {
		t.Fatalf("create project: %v", err)
	}

	// The system pack must not be auto-assigned to the fresh project.
	var assigned int
	if err := db.NewRaw(`
		SELECT COUNT(*) FROM kb.project_schemas ps
		JOIN kb.graph_schemas gs ON gs.id = ps.schema_id
		WHERE ps.project_id = ? AND gs.name = 'session-message-types'
	`, projectID).Scan(ctx, &assigned); err != nil {
		t.Fatalf("count project_schemas: %v", err)
	}
	if assigned != 0 {
		t.Fatalf("expected no project_schemas row for session-message-types, got %d", assigned)
	}

	resp, err := repo.GetCompiledTypesByProject(ctx, projectID)
	if err != nil {
		t.Fatalf("GetCompiledTypesByProject: %v", err)
	}

	names := map[string]bool{}
	for _, ot := range resp.ObjectTypes {
		names[ot.Name] = true
		if (ot.Name == "Session" || ot.Name == "Message") && !ot.IsSystem {
			t.Errorf("type %s must carry IsSystem=true", ot.Name)
		}
	}
	if !names["Session"] {
		t.Errorf("expected Session type in compiled types, got %+v", names)
	}
	if !names["Message"] {
		t.Errorf("expected Message type in compiled types, got %+v", names)
	}
}

// TestAssignPackWithTypesStampsExistingAssignment verifies that a blueprint
// re-apply which hits the already-assigned + Merge early return still stamps
// source_blueprint_id on the pre-existing assignment, so blueprint unapply can
// later identify and remove it.
func TestAssignPackWithTypesStampsExistingAssignment(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "stampassign")
	defer testDB.Close()

	db := testDB.GetDB()
	log := slog.Default()
	repo := schemas.NewRepository(db, log)

	orgID := uuid.NewString()
	if err := testutil.CreateTestOrganization(ctx, db, orgID, "Stamp Org"); err != nil {
		t.Fatalf("create org: %v", err)
	}
	projectID := uuid.NewString()
	if err := testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "Stamp Project",
	}, testutil.AdminUser.ID); err != nil {
		t.Fatalf("create project: %v", err)
	}

	packID := uuid.NewString()
	if _, err := db.NewRaw(`
		INSERT INTO kb.graph_schemas (id, name, version, source, object_type_schemas, relationship_type_schemas)
		VALUES (?, 'stamp-pack', '1.0.0', 'builtin', '{}'::jsonb, '{}'::jsonb)
	`, packID).Exec(ctx); err != nil {
		t.Fatalf("insert graph schema: %v", err)
	}
	// Pre-existing assignment with no owning blueprint.
	if _, err := db.NewRaw(`
		INSERT INTO kb.project_schemas (project_id, schema_id, active, installed_at, source_blueprint_id)
		VALUES (?, ?, true, now(), NULL)
	`, projectID, packID).Exec(ctx); err != nil {
		t.Fatalf("insert assignment: %v", err)
	}

	blueprintID := uuid.NewString()
	userID := testutil.AdminUser.ID
	res, err := repo.AssignPackWithTypes(ctx, projectID, userID, &schemas.AssignPackRequest{
		SchemaID:          packID,
		Merge:             true,
		SourceBlueprintID: &blueprintID,
	})
	if err != nil {
		t.Fatalf("AssignPackWithTypes: %v", err)
	}
	if !res.AlreadyInstalled {
		t.Fatalf("expected AlreadyInstalled=true, got %+v", res)
	}

	var stamped *string
	if err := db.NewRaw(`
		SELECT source_blueprint_id FROM kb.project_schemas
		WHERE project_id = ? AND schema_id = ? AND removed_at IS NULL
	`, projectID, packID).Scan(ctx, &stamped); err != nil {
		t.Fatalf("read assignment: %v", err)
	}
	if stamped == nil || *stamped != blueprintID {
		t.Fatalf("expected source_blueprint_id=%s, got %v", blueprintID, stamped)
	}
}

// TestAssignPackWithTypesOwnershipStampFamily verifies the ownership stamp
// mirrors applyAgents' family logic: it never overwrites an assignment owned by
// a different blueprint name, and it adopts an assignment owned by the same
// blueprint name (a newer version).
func TestAssignPackWithTypesOwnershipStampFamily(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "stampfamily")
	defer testDB.Close()

	db := testDB.GetDB()
	log := slog.Default()
	repo := schemas.NewRepository(db, log)

	orgID := uuid.NewString()
	if err := testutil.CreateTestOrganization(ctx, db, orgID, "Family Org"); err != nil {
		t.Fatalf("create org: %v", err)
	}
	projectID := uuid.NewString()
	if err := testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "Family Project",
	}, testutil.AdminUser.ID); err != nil {
		t.Fatalf("create project: %v", err)
	}

	insertBlueprint := func(id, name, version string) {
		t.Helper()
		if _, err := db.NewRaw(`
			INSERT INTO kb.blueprints (id, name, version) VALUES (?, ?, ?)
		`, id, name, version).Exec(ctx); err != nil {
			t.Fatalf("insert blueprint %s: %v", name, err)
		}
	}
	insertPack := func(id, name string) {
		t.Helper()
		if _, err := db.NewRaw(`
			INSERT INTO kb.graph_schemas (id, name, version, source, object_type_schemas, relationship_type_schemas)
			VALUES (?, ?, '1.0.0', 'builtin', '{}'::jsonb, '{}'::jsonb)
		`, id, name).Exec(ctx); err != nil {
			t.Fatalf("insert pack %s: %v", name, err)
		}
	}
	readOwner := func(packID string) string {
		t.Helper()
		var owner *string
		if err := db.NewRaw(`
			SELECT source_blueprint_id FROM kb.project_schemas
			WHERE project_id = ? AND schema_id = ? AND removed_at IS NULL
		`, projectID, packID).Scan(ctx, &owner); err != nil {
			t.Fatalf("read assignment %s: %v", packID, err)
		}
		if owner == nil {
			return ""
		}
		return *owner
	}

	// Case 1: different blueprint name — must NOT overwrite.
	ownerID := uuid.NewString()
	incomingID := uuid.NewString()
	insertBlueprint(ownerID, "owner-blueprint", "1.0.0")
	insertBlueprint(incomingID, "incoming-blueprint", "1.0.0")

	pack1 := uuid.NewString()
	insertPack(pack1, "family-pack-1")
	if _, err := db.NewRaw(`
		INSERT INTO kb.project_schemas (project_id, schema_id, active, installed_at, source_blueprint_id)
		VALUES (?, ?, true, now(), ?)
	`, projectID, pack1, ownerID).Exec(ctx); err != nil {
		t.Fatalf("insert assignment pack1: %v", err)
	}
	if _, err := repo.AssignPackWithTypes(ctx, projectID, testutil.AdminUser.ID, &schemas.AssignPackRequest{
		SchemaID:          pack1,
		Merge:             true,
		SourceBlueprintID: &incomingID,
	}); err != nil {
		t.Fatalf("AssignPackWithTypes pack1: %v", err)
	}
	if got := readOwner(pack1); got != ownerID {
		t.Errorf("different-family owner must be preserved, got %q want %q", got, ownerID)
	}

	// Case 2: same blueprint name, newer version — must adopt (re-stamp).
	oldID := uuid.NewString()
	newID := uuid.NewString()
	insertBlueprint(oldID, "upgraded-blueprint", "1.0.0")
	insertBlueprint(newID, "upgraded-blueprint", "2.0.0")

	pack2 := uuid.NewString()
	insertPack(pack2, "family-pack-2")
	if _, err := db.NewRaw(`
		INSERT INTO kb.project_schemas (project_id, schema_id, active, installed_at, source_blueprint_id)
		VALUES (?, ?, true, now(), ?)
	`, projectID, pack2, oldID).Exec(ctx); err != nil {
		t.Fatalf("insert assignment pack2: %v", err)
	}
	if _, err := repo.AssignPackWithTypes(ctx, projectID, testutil.AdminUser.ID, &schemas.AssignPackRequest{
		SchemaID:          pack2,
		Merge:             true,
		SourceBlueprintID: &newID,
	}); err != nil {
		t.Fatalf("AssignPackWithTypes pack2: %v", err)
	}
	if got := readOwner(pack2); got != newID {
		t.Errorf("same-family owner must adopt newer version, got %q want %q", got, newID)
	}
}

// TestSystemSchemaIndexExists verifies 00235 created the partial is_system
// index that backs the read-time system lookup.
func TestSystemSchemaIndexExists(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "sysidx")
	defer testDB.Close()

	db := testDB.GetDB()
	var count int
	if err := db.NewRaw(`
		SELECT COUNT(*) FROM pg_indexes WHERE indexname = 'graph_schemas_is_system_idx'
	`).Scan(ctx, &count); err != nil {
		t.Fatalf("query pg_indexes: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected graph_schemas_is_system_idx to exist, found %d", count)
	}
}

// TestMigration00236SystemPackCleanup verifies 00236 removes the leftover
// auto-provisioned assignment and registry rows for the read-time system pack
// while keeping its graph_schemas row.
func TestMigration00236SystemPackCleanup(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "syscleanup")
	defer testDB.Close()

	db := testDB.GetDB()

	orgID := uuid.NewString()
	if err := testutil.CreateTestOrganization(ctx, db, orgID, "Cleanup Org"); err != nil {
		t.Fatalf("create org: %v", err)
	}
	projectID := uuid.NewString()
	if err := testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "Cleanup Project",
	}, testutil.AdminUser.ID); err != nil {
		t.Fatalf("create project: %v", err)
	}

	// Seed the read-time system pack plus the leftover assignment/registry rows
	// that the old 00146 trigger + ProvisionBuiltinSchemasToAllProjects would
	// have left behind.
	systemPackID := uuid.NewString()
	if _, err := db.NewRaw(`
		INSERT INTO kb.graph_schemas (id, name, version, source, is_system, object_type_schemas, relationship_type_schemas)
		VALUES (?, 'session-message-types', '1.0.0', 'builtin', true, '{}'::jsonb, '{}'::jsonb)
	`, systemPackID).Exec(ctx); err != nil {
		t.Fatalf("insert system pack: %v", err)
	}
	if _, err := db.NewRaw(`
		INSERT INTO kb.project_schemas (project_id, schema_id, active, installed_at)
		VALUES (?, ?, true, now())
	`, projectID, systemPackID).Exec(ctx); err != nil {
		t.Fatalf("insert assignment: %v", err)
	}
	if _, err := db.NewRaw(`
		INSERT INTO kb.project_object_schema_registry (project_id, schema_id, type_name, source, json_schema)
		VALUES (?, ?, 'Session', 'builtin', '{}'::jsonb)
	`, projectID, systemPackID).Exec(ctx); err != nil {
		t.Fatalf("insert object registry: %v", err)
	}
	if _, err := db.NewRaw(`
		INSERT INTO kb.project_edge_schema_registry (project_id, schema_id, type_name, json_schema)
		VALUES (?, ?, 'SessionEdge', '{}'::jsonb)
	`, projectID, systemPackID).Exec(ctx); err != nil {
		t.Fatalf("insert edge registry: %v", err)
	}

	// Re-run 00236's Up statements (idempotent DELETEs).
	for _, stmt := range migrationUpStatements(t, "00236_cleanup_builtin_schema_provisioning.sql") {
		if _, err := db.NewRaw(stmt).Exec(ctx); err != nil {
			t.Fatalf("execute 00236 statement %q: %v", stmt, err)
		}
	}

	var sysPackCount int
	if err := db.NewRaw(`SELECT COUNT(*) FROM kb.graph_schemas WHERE id = ?`, systemPackID).Scan(ctx, &sysPackCount); err != nil {
		t.Fatalf("count system pack: %v", err)
	}
	if sysPackCount != 1 {
		t.Errorf("expected system pack graph_schemas row to survive, got %d", sysPackCount)
	}

	for _, q := range []struct{ table, idCol string }{
		{"kb.project_schemas", "schema_id"},
		{"kb.project_object_schema_registry", "schema_id"},
		{"kb.project_edge_schema_registry", "schema_id"},
	} {
		var n int
		if err := db.NewRaw(`SELECT COUNT(*) FROM `+q.table+` WHERE `+q.idCol+` = ?`, systemPackID).Scan(ctx, &n); err != nil {
			t.Fatalf("count %s: %v", q.table, err)
		}
		if n != 0 {
			t.Errorf("expected no %s rows for system pack, got %d", q.table, n)
		}
	}
}

// TestMigration00236BackfillsSourceBlueprintID verifies 00236 backfills
// source_blueprint_id on kept assignments claimed by exactly one applied
// blueprint, and skips rows claimed by more than one.
func TestMigration00236BackfillsSourceBlueprintID(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "sysbackfill")
	defer testDB.Close()

	db := testDB.GetDB()

	orgID := uuid.NewString()
	if err := testutil.CreateTestOrganization(ctx, db, orgID, "Backfill Org"); err != nil {
		t.Fatalf("create org: %v", err)
	}
	projectID := uuid.NewString()
	if err := testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "Backfill Project",
	}, testutil.AdminUser.ID); err != nil {
		t.Fatalf("create project: %v", err)
	}

	insertBlueprint := func(id, name, version string) {
		t.Helper()
		if _, err := db.NewRaw(`
			INSERT INTO kb.blueprints (id, name, version) VALUES (?, ?, ?)
		`, id, name, version).Exec(ctx); err != nil {
			t.Fatalf("insert blueprint %s: %v", name, err)
		}
	}
	apply := func(blueprintID, version string) {
		t.Helper()
		if _, err := db.NewRaw(`
			INSERT INTO kb.blueprint_applications (blueprint_id, project_id, version)
			VALUES (?, ?, ?)
		`, blueprintID, projectID, version).Exec(ctx); err != nil {
			t.Fatalf("insert application %s: %v", blueprintID, err)
		}
	}
	claim := func(blueprintID, schemaID string) {
		t.Helper()
		if _, err := db.NewRaw(`
			INSERT INTO kb.blueprint_pack_claims (blueprint_id, project_id, schema_id)
			VALUES (?, ?, ?)
		`, blueprintID, projectID, schemaID).Exec(ctx); err != nil {
			t.Fatalf("insert claim %s: %v", blueprintID, err)
		}
	}
	insertPackAndAssignment := func(packID, name string) {
		t.Helper()
		if _, err := db.NewRaw(`
			INSERT INTO kb.graph_schemas (id, name, version, source, object_type_schemas, relationship_type_schemas)
			VALUES (?, ?, '1.0.0', 'builtin', '{}'::jsonb, '{}'::jsonb)
		`, packID, name).Exec(ctx); err != nil {
			t.Fatalf("insert pack %s: %v", name, err)
		}
		if _, err := db.NewRaw(`
			INSERT INTO kb.project_schemas (project_id, schema_id, active, installed_at, source_blueprint_id)
			VALUES (?, ?, true, now(), NULL)
		`, projectID, packID).Exec(ctx); err != nil {
			t.Fatalf("insert assignment %s: %v", name, err)
		}
	}

	// Single applied claim → backfill.
	singleBP := uuid.NewString()
	insertBlueprint(singleBP, "single-blueprint", "1.0.0")
	apply(singleBP, "1.0.0")
	singlePack := uuid.NewString()
	insertPackAndAssignment(singlePack, "single-pack")
	claim(singleBP, singlePack)

	// Two applied blueprints claim the same pack → skip (ambiguous ownership).
	multiBP1 := uuid.NewString()
	multiBP2 := uuid.NewString()
	insertBlueprint(multiBP1, "multi-blueprint-1", "1.0.0")
	insertBlueprint(multiBP2, "multi-blueprint-2", "1.0.0")
	apply(multiBP1, "1.0.0")
	apply(multiBP2, "1.0.0")
	multiPack := uuid.NewString()
	insertPackAndAssignment(multiPack, "multi-pack")
	claim(multiBP1, multiPack)
	claim(multiBP2, multiPack)

	for _, stmt := range migrationUpStatements(t, "00236_cleanup_builtin_schema_provisioning.sql") {
		if _, err := db.NewRaw(stmt).Exec(ctx); err != nil {
			t.Fatalf("execute 00236 statement %q: %v", stmt, err)
		}
	}

	readOwner := func(packID string) string {
		t.Helper()
		var owner *string
		if err := db.NewRaw(`
			SELECT source_blueprint_id FROM kb.project_schemas
			WHERE project_id = ? AND schema_id = ? AND removed_at IS NULL
		`, projectID, packID).Scan(ctx, &owner); err != nil {
			t.Fatalf("read assignment %s: %v", packID, err)
		}
		if owner == nil {
			return ""
		}
		return *owner
	}

	if got := readOwner(singlePack); got != singleBP {
		t.Errorf("single-claim assignment should be backfilled, got %q want %q", got, singleBP)
	}
	if got := readOwner(multiPack); got != "" {
		t.Errorf("multi-claim assignment should be skipped, got %q want empty", got)
	}
}
