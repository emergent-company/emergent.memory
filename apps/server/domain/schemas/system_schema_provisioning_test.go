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

// TestMigration00235_00236_00242_SystemSchemaFlag verifies the three
// provisioning-removal migrations are embedded and carry the key DDL: 00235
// drops the auto-install trigger and adds/marks the is_system column; 00236
// deletes leftover auto-provisioned assignments and orphaned blueprint-pack
// rows; 00242 converts the system pack into a normal built-in blueprint pack and
// drops the is_system column. This guards against a silent deletion of the
// migrations (they must be embedded) and against drift in the cleanup/backfill
// predicates.
func TestMigration00235_00236_00242_SystemSchemaFlag(t *testing.T) {
	b235, err := migrations.FS.ReadFile("00235_drop_builtin_schema_provisioning.sql")
	if err != nil {
		t.Fatalf("00235 must be embedded: %v", err)
	}
	b236, err := migrations.FS.ReadFile("00236_cleanup_builtin_schema_provisioning.sql")
	if err != nil {
		t.Fatalf("00236 must be embedded: %v", err)
	}
	b237, err := migrations.FS.ReadFile("00242_session_message_types_blueprint.sql")
	if err != nil {
		t.Fatalf("00242 must be embedded: %v", err)
	}

	s235 := string(b235)
	s236 := string(b236)
	s237 := string(b237)

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

	for _, want := range []string{
		// Backfill: assignments + registry rows for projects owning Session/Message.
		"INSERT INTO kb.project_schemas",
		"INSERT INTO kb.project_object_schema_registry",
		"INSERT INTO kb.project_edge_schema_registry",
		"jsonb_array_elements(gs.object_type_schemas)",
		"jsonb_array_elements(gs.relationship_type_schemas)",
		"type IN ('Session','Message') AND deleted_at IS NULL",
		"ON CONFLICT (project_id, schema_id) DO NOTHING",
		"source_blueprint_id",
		// Cleanup + column/index removal.
		"DELETE FROM kb.graph_schemas gs",
		"DROP INDEX IF EXISTS kb.graph_schemas_is_system_idx",
		"DROP COLUMN IF EXISTS is_system",
		// Down restores the column + index.
		"ADD COLUMN IF NOT EXISTS is_system boolean NOT NULL DEFAULT false",
		"CREATE INDEX IF NOT EXISTS graph_schemas_is_system_idx",
	} {
		if !strings.Contains(s237, want) {
			t.Errorf("00242 missing %q", want)
		}
	}
	// The Up body must remain splitter-safe: no procedural bodies (StatementBegin),
	// since the tests and the migration runner split the Up body on ';'.
	if strings.Contains(s237, "StatementBegin") || strings.Contains(s237, "StatementEnd") {
		t.Errorf("00242 Up must not contain procedural bodies (StatementBegin/End)")
	}
	// The Up body must split into the six expected standalone statements
	// (assignment insert, object-registry insert, edge-registry insert, pack
	// delete, index drop, column drop).
	if got := len(migrationUpStatements(t, "00242_session_message_types_blueprint.sql")); got != 6 {
		t.Errorf("00242 Up split into %d statements, want 6", got)
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

// TestMigration00242BackfillsOwningProjects verifies 00242's Up backfill: a
// project that owns Session/Message graph objects gains an active
// kb.project_schemas assignment plus Session/Message object-registry rows and a
// has_message edge-registry row (materialized from the global pack's JSON),
// while a project with no such objects gains nothing. The test DB is already at
// HEAD (00242 applied, is_system dropped), so the Up statements are re-run
// idempotently against a freshly prepared dataset.
func TestMigration00242BackfillsOwningProjects(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "mig237backfill")
	defer testDB.Close()

	db := testDB.GetDB()

	orgID := uuid.NewString()
	if err := testutil.CreateTestOrganization(ctx, db, orgID, "Backfill Org"); err != nil {
		t.Fatalf("create org: %v", err)
	}
	owningID := uuid.NewString()
	if err := testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    owningID,
		OrgID: orgID,
		Name:  "Owning Project",
	}, testutil.AdminUser.ID); err != nil {
		t.Fatalf("create owning project: %v", err)
	}
	emptyID := uuid.NewString()
	if err := testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    emptyID,
		OrgID: orgID,
		Name:  "Empty Project",
	}, testutil.AdminUser.ID); err != nil {
		t.Fatalf("create empty project: %v", err)
	}

	// Insert the global session-message-types pack row exactly as the schemas
	// builtin seeder used to store it (array-shaped object_type_schemas /
	// relationship_type_schemas), with Session/Message and has_message.
	packID := uuid.NewString()
	if _, err := db.NewRaw(`
		INSERT INTO kb.graph_schemas (id, name, version, source, object_type_schemas, relationship_type_schemas)
		VALUES (?, 'session-message-types', '1.0.0', 'builtin',
			'[{"name":"Session","label":"Session","description":"A conversation session","properties":{"title":{"type":"string"}},"required":["title"]},{"name":"Message","label":"Message","description":"A message","properties":{"role":{"type":"string"},"content":{"type":"string","x-embedding-target":true}},"required":["role","content"]}]'::jsonb,
			'[{"name":"has_message","description":"Links a Session to its Messages","sourceType":"Session","targetType":"Message"}]'::jsonb)
	`, packID).Exec(ctx); err != nil {
		t.Fatalf("insert system pack: %v", err)
	}

	// The owning project has a Session and a Message object; the empty project
	// has none.
	insertObject := func(projectID, typ string) {
		t.Helper()
		if _, err := db.NewRaw(`
			INSERT INTO kb.graph_objects (id, project_id, type, canonical_id)
			VALUES (?, ?, ?, ?)
		`, uuid.NewString(), projectID, typ, uuid.NewString()).Exec(ctx); err != nil {
			t.Fatalf("insert graph object %s: %v", typ, err)
		}
	}
	insertObject(owningID, "Session")
	insertObject(owningID, "Message")

	// Re-run 00242's Up statements (idempotent at HEAD: the DROP INDEX/COLUMN
	// steps are IF EXISTS/IF NOT EXISTS no-ops; the backfill INSERTs run fresh).
	for _, stmt := range migrationUpStatements(t, "00242_session_message_types_blueprint.sql") {
		if _, err := db.NewRaw(stmt).Exec(ctx); err != nil {
			t.Fatalf("execute 00242 statement %q: %v", stmt, err)
		}
	}

	// Owning project: one active assignment with no owning blueprint yet.
	var owningAssignments int
	if err := db.NewRaw(`
		SELECT COUNT(*) FROM kb.project_schemas ps
		WHERE ps.project_id = ? AND ps.schema_id = ? AND ps.removed_at IS NULL AND ps.active
	`, owningID, packID).Scan(ctx, &owningAssignments); err != nil {
		t.Fatalf("count owning assignments: %v", err)
	}
	if owningAssignments != 1 {
		t.Errorf("owning project: want 1 active assignment, got %d", owningAssignments)
	}

	var sourceBlueprintID *string
	if err := db.NewRaw(`
		SELECT source_blueprint_id FROM kb.project_schemas
		WHERE project_id = ? AND schema_id = ? AND removed_at IS NULL
	`, owningID, packID).Scan(ctx, &sourceBlueprintID); err != nil {
		t.Fatalf("read assignment source_blueprint_id: %v", err)
	}
	if sourceBlueprintID != nil {
		t.Errorf("backfilled assignment must have NULL source_blueprint_id, got %v", *sourceBlueprintID)
	}

	// Owning project: Session + Message object-registry rows (source='template').
	for _, typ := range []string{"Session", "Message"} {
		var n int
		if err := db.NewRaw(`
			SELECT COUNT(*) FROM kb.project_object_schema_registry
			WHERE project_id = ? AND type_name = ? AND schema_id = ? AND source = 'template'
		`, owningID, typ, packID).Scan(ctx, &n); err != nil {
			t.Fatalf("count object registry %s: %v", typ, err)
		}
		if n != 1 {
			t.Errorf("owning project: want 1 %s object-registry row, got %d", typ, n)
		}
	}

	// Owning project: has_message edge-registry row.
	var edgeCount int
	if err := db.NewRaw(`
		SELECT COUNT(*) FROM kb.project_edge_schema_registry
		WHERE project_id = ? AND schema_id = ? AND type_name = 'has_message'
	`, owningID, packID).Scan(ctx, &edgeCount); err != nil {
		t.Fatalf("count edge registry: %v", err)
	}
	if edgeCount != 1 {
		t.Errorf("owning project: want 1 has_message edge-registry row, got %d", edgeCount)
	}

	// Empty project: no assignment, no registry rows.
	for _, q := range []struct{ table, idCol string }{
		{"kb.project_schemas", "schema_id"},
		{"kb.project_object_schema_registry", "schema_id"},
		{"kb.project_edge_schema_registry", "schema_id"},
	} {
		var n int
		if err := db.NewRaw(`SELECT COUNT(*) FROM `+q.table+` WHERE project_id = ? AND `+q.idCol+` = ?`, emptyID, packID).Scan(ctx, &n); err != nil {
			t.Fatalf("count empty-project %s: %v", q.table, err)
		}
		if n != 0 {
			t.Errorf("empty project: want no %s rows, got %d", q.table, n)
		}
	}

	// The global pack row survives (referenced by the owning assignment).
	var packCount int
	if err := db.NewRaw(`SELECT COUNT(*) FROM kb.graph_schemas WHERE id = ?`, packID).Scan(ctx, &packCount); err != nil {
		t.Fatalf("count system pack: %v", err)
	}
	if packCount != 1 {
		t.Errorf("expected system pack graph_schemas row to survive, got %d", packCount)
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
