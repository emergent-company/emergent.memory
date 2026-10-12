package schemas_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/schemas"
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
		"bpa.status = 'applied'",
	} {
		if !strings.Contains(s236, want) {
			t.Errorf("00236 missing %q", want)
		}
	}
}

// TestSystemSchemasResolvedWithoutAssignment verifies the read-time resolution
// behaviour: after the schemas builtin seeder runs, the Session/Message system
// types are returned by GetCompiledTypesByProject with IsSystem=true, and no
// kb.project_schemas row exists for the system pack.
func TestSystemSchemasResolvedWithoutAssignment(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
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
		t.Skip("skipping database integration test in short mode")
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
