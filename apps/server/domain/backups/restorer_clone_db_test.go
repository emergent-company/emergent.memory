package backups

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// TestCloneRestoreSkipsUnresolvableSchemaLinks is the regression test for
// GitHub issue #592: clone-restore into a differently-bootstrapped deployment
// must skip kb.project_schemas and kb.project_edge_schema_registry rows whose
// schema_id points at the SOURCE deployment's global builtin graph_schemas UUID
// (which does not exist in the target) instead of failing the whole restore on
// the project_schemas_schema_id_fkey / project_edge_schema_registry_schema_id_fkey
// constraints.
func TestCloneRestoreSkipsUnresolvableSchemaLinks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	ctx := context.Background()

	// Two test databases stand in for two independent deployments.
	srcDB := testutil.SetupTestDBOrFail(t, ctx, "clone_src")
	defer srcDB.Close()

	dstDB := testutil.SetupTestDBOrFail(t, ctx, "clone_dst")
	defer dstDB.Close()

	mustExec := func(db *bun.DB, query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}
	seedSchema := func(db *bun.DB, id, name, version, source string, projectID *string) {
		t.Helper()
		mustExec(db, `
			INSERT INTO kb.graph_schemas
				(id, name, version, source, project_id, object_type_schemas, relationship_type_schemas)
			VALUES (?, ?, ?, ?, ?, ?::jsonb, ?::jsonb)`,
			id, name, version, source, projectID, "{}", "[]")
	}

	// --- Deployment A (source): its own global builtin UUID, a project-owned
	// schema, and links in both project_schemas and
	// project_edge_schema_registry. ---
	orgA := uuid.NewString()
	projectA := uuid.NewString()
	builtinA := uuid.NewString() // deployment A's global builtin schema UUID
	ownedA := uuid.NewString()   // project-owned schema UUID

	mustExec(srcDB.DB, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgA, "org-a")
	mustExec(srcDB.DB, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, projectA, orgA, "project-a")
	seedSchema(srcDB.DB, builtinA, "session-message-types", "1.0.0", "builtin", nil)
	seedSchema(srcDB.DB, ownedA, "owned-schema", "1.0.0", "manual", &projectA)
	mustExec(srcDB.DB, `INSERT INTO kb.project_schemas (id, project_id, schema_id) VALUES (?, ?, ?)`,
		uuid.NewString(), projectA, ownedA)
	mustExec(srcDB.DB, `INSERT INTO kb.project_schemas (id, project_id, schema_id) VALUES (?, ?, ?)`,
		uuid.NewString(), projectA, builtinA)
	mustExec(srcDB.DB, `INSERT INTO kb.project_edge_schema_registry (id, project_id, schema_id, type_name) VALUES (?, ?, ?, ?)`,
		uuid.NewString(), projectA, ownedA, "RELATES_TO")
	mustExec(srcDB.DB, `INSERT INTO kb.project_edge_schema_registry (id, project_id, schema_id, type_name) VALUES (?, ?, ?, ?)`,
		uuid.NewString(), projectA, builtinA, "OTHER_TYPE")

	// --- Deployment B (target): its own global builtin UUID (different value),
	// plus the target project the clone will populate. ---
	orgB := uuid.NewString()
	newProjectID := uuid.NewString()
	builtinB := uuid.NewString() // deployment B's global builtin schema UUID

	mustExec(dstDB.DB, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgB, "org-b")
	seedSchema(dstDB.DB, builtinB, "session-message-types", "1.0.0", "builtin", nil)
	mustExec(dstDB.DB, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, newProjectID, orgB, "cloned-project")
	// Simulate the target's builtin provisioning trigger (absent from the test
	// schema snapshot) linking the project to the TARGET's own builtin.
	mustExec(dstDB.DB, `INSERT INTO kb.project_schemas (id, project_id, schema_id) VALUES (?, ?, ?)`,
		uuid.NewString(), newProjectID, builtinB)

	// Build the archive as the exporter would: graph_schemas carries only the
	// project-owned row (global rows are filtered by project_id), while the link
	// tables carry both the owned and the builtin links (they are project-scoped).
	archive := &Archive{
		tableData: map[string][]byte{
			"graph_schemas": mustNDJSON(t, []map[string]any{{
				"id":                        ownedA,
				"name":                      "owned-schema",
				"version":                   "1.0.0",
				"source":                    "manual",
				"project_id":                projectA,
				"object_type_schemas":       "{}",
				"relationship_type_schemas": "[]",
			}}),
			"project_schemas": mustNDJSON(t, []map[string]any{
				{"id": uuid.NewString(), "project_id": projectA, "schema_id": ownedA},
				{"id": uuid.NewString(), "project_id": projectA, "schema_id": builtinA},
			}),
			"project_edge_schema_registry": mustNDJSON(t, []map[string]any{
				{"id": uuid.NewString(), "project_id": projectA, "schema_id": ownedA, "type_name": "RELATES_TO"},
				{"id": uuid.NewString(), "project_id": projectA, "schema_id": builtinA, "type_name": "OTHER_TYPE"},
			}),
		},
	}

	specs := map[string]restoreTableSpec{}
	for _, s := range restoreTableOrder() {
		specs[s.name] = s
	}

	remap := map[string]string{projectA: newProjectID}
	restorer := &Restorer{db: dstDB.DB, log: slog.Default()}

	tx, err := dstDB.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin clone transaction: %v", err)
	}
	defer tx.Rollback() //nolint:errcheck // rolled back on every error path

	// graph_schemas first so the project-owned schema registers in remap.
	gsRows, err := archive.Rows("graph_schemas")
	if err != nil {
		t.Fatalf("archive graph_schemas rows: %v", err)
	}
	if _, err := restorer.insertTableRows(ctx, tx, specs["graph_schemas"], gsRows, remap); err != nil {
		t.Fatalf("insert graph_schemas: %v", err)
	}

	psRows, err := archive.Rows("project_schemas")
	if err != nil {
		t.Fatalf("archive project_schemas rows: %v", err)
	}
	inserted, err := restorer.insertTableRows(ctx, tx, specs["project_schemas"], psRows, remap)
	if err != nil {
		t.Fatalf("insert project_schemas: %v", err)
	}
	if inserted != 1 {
		t.Fatalf("inserted %d project_schemas rows, want 1 (builtin link must be skipped)", inserted)
	}

	erRows, err := archive.Rows("project_edge_schema_registry")
	if err != nil {
		t.Fatalf("archive project_edge_schema_registry rows: %v", err)
	}
	inserted, err = restorer.insertTableRows(ctx, tx, specs["project_edge_schema_registry"], erRows, remap)
	if err != nil {
		t.Fatalf("insert project_edge_schema_registry: %v", err)
	}
	if inserted != 1 {
		t.Fatalf("inserted %d project_edge_schema_registry rows, want 1 (builtin link must be skipped)", inserted)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("commit clone transaction: %v", err)
	}

	countLinks := func(db *bun.DB, table, projectID, schemaID string) int {
		t.Helper()
		n, err := db.NewSelect().
			Table(table).
			Where("project_id = ?", projectID).
			Where("schema_id = ?", schemaID).
			Count(ctx)
		if err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}

	// The project-owned link is remapped (ownedA -> a fresh UUID).
	ownedNew, ok := remap[ownedA]
	if !ok {
		t.Fatalf("owned schema %s was not registered in remap", ownedA)
	}
	if got := countLinks(dstDB.DB, "kb.project_schemas", newProjectID, ownedNew); got != 1 {
		t.Errorf("project-owned project_schemas link remapped = %d rows, want 1", got)
	}
	if got := countLinks(dstDB.DB, "kb.project_edge_schema_registry", newProjectID, ownedNew); got != 1 {
		t.Errorf("project-owned project_edge_schema_registry link remapped = %d rows, want 1", got)
	}

	// No row references the source deployment's builtin UUID.
	if got := countLinks(dstDB.DB, "kb.project_schemas", newProjectID, builtinA); got != 0 {
		t.Errorf("source builtin project_schemas link = %d rows, want 0", got)
	}
	if got := countLinks(dstDB.DB, "kb.project_edge_schema_registry", newProjectID, builtinA); got != 0 {
		t.Errorf("source builtin project_edge_schema_registry link = %d rows, want 0", got)
	}

	// The target deployment's own builtin link remains present.
	if got := countLinks(dstDB.DB, "kb.project_schemas", newProjectID, builtinB); got != 1 {
		t.Errorf("target builtin link = %d rows, want 1", got)
	}
}

// TestCloneRestoreOverwriteInsertsUnchanged proves overwrite mode (remap == nil)
// does not apply any ref policy: rows are inserted with their original IDs.
func TestCloneRestoreOverwriteInsertsUnchanged(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	ctx := context.Background()

	db := testutil.SetupTestDBOrFail(t, ctx, "clone_overwrite")
	defer db.Close()

	mustExec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.DB.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}

	org := uuid.NewString()
	builtin := uuid.NewString()
	owned := uuid.NewString()
	project := uuid.NewString()

	mustExec(`INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, org, "org")
	mustExec(`INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, project, org, "project")
	mustExec(`INSERT INTO kb.graph_schemas (id, name, version, source, project_id, object_type_schemas, relationship_type_schemas)
		VALUES (?, ?, ?, ?, ?, ?::jsonb, ?::jsonb)`, builtin, "session-message-types", "1.0.0", "builtin", nil, "{}", "[]")
	mustExec(`INSERT INTO kb.graph_schemas (id, name, version, source, project_id, object_type_schemas, relationship_type_schemas)
		VALUES (?, ?, ?, ?, ?, ?::jsonb, ?::jsonb)`, owned, "owned-schema", "1.0.0", "manual", &project, "{}", "[]")

	linkID1 := uuid.NewString()
	linkID2 := uuid.NewString()
	rows := []map[string]any{
		{"id": linkID1, "project_id": project, "schema_id": owned},
		{"id": linkID2, "project_id": project, "schema_id": builtin},
	}

	specs := map[string]restoreTableSpec{}
	for _, s := range restoreTableOrder() {
		specs[s.name] = s
	}

	restorer := &Restorer{db: db.DB, log: slog.Default()}
	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin overwrite transaction: %v", err)
	}
	defer tx.Rollback() //nolint:errcheck

	inserted, err := restorer.insertTableRows(ctx, tx, specs["project_schemas"], rows, nil)
	if err != nil {
		t.Fatalf("insert project_schemas (overwrite): %v", err)
	}
	if inserted != 2 {
		t.Fatalf("inserted %d rows, want 2 (overwrite must not skip)", inserted)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit overwrite transaction: %v", err)
	}

	// Both rows must exist with their original (unchanged) IDs and schema IDs.
	var got []struct {
		ID       string `bun:"id"`
		SchemaID string `bun:"schema_id"`
	}
	if err := db.DB.NewSelect().
		Table("kb.project_schemas").
		Column("id", "schema_id").
		Where("project_id = ?", project).
		Order("schema_id ASC").
		Scan(ctx, &got); err != nil {
		t.Fatalf("load project_schemas: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("found %d project_schemas rows, want 2", len(got))
	}
	want := map[string]string{builtin: linkID2, owned: linkID1}
	for _, r := range got {
		if id, ok := want[r.SchemaID]; !ok || id != r.ID {
			t.Errorf("row schema_id=%s id=%s does not match original (want %s)", r.SchemaID, r.ID, id)
		}
	}
}

// mustNDJSON renders rows as newline-delimited JSON, mirroring the archive
// database/*.ndjson format consumed by Archive.Rows.
func mustNDJSON(t *testing.T, rows []map[string]any) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, row := range rows {
		if err := enc.Encode(row); err != nil {
			t.Fatalf("encode NDJSON row: %v", err)
		}
	}
	return buf.Bytes()
}
