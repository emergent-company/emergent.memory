package schemas_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/schemas"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// TestReconcileProjectPackUpdatesInPlace pins the provider-pack contract: a
// code-owned pack is reconciled on its single project row — version, source
// token, type definitions (including the ui block) and checksum — while its
// assignment survives and no second row is created. EnsureProjectPack's
// return-untouched behaviour is why this path exists.
func TestReconcileProjectPackUpdatesInPlace(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "reconcile")
	t.Cleanup(testDB.Close)
	db := testDB.GetDB()

	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "Reconcile Org"))
	projectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "Reconcile Project",
	}, testutil.AdminUser.ID))

	cfg := &config.Config{}
	repo := schemas.NewRepository(db, slog.Default())
	// AssignPack invalidates the graph schema cache, so the service needs a real
	// graph service here; a nil one panics on the first assign (the fast CI lane
	// skips DB tests, so only the DB-backed job would catch it).
	gRepo := graph.NewRepository(db, slog.Default(), cfg)
	// AssignPack reaches the graph's schema-cache provider (not just the graph
	// repository), so pass the no-op provider this package already uses for the
	// assign path: assignNoopSchemaProvider implements exactly the one
	// method the assign path calls.
	gSvc := graph.NewService(gRepo, slog.Default(), assignNoopSchemaProvider{}, nil, nil, nil, nil, nil, nil, nil)
	svc := schemas.NewService(repo, gSvc, slog.Default(), cfg)

	created, err := svc.CreatePack(ctx, projectID, &schemas.CreatePackRequest{
		Name:        "github-app-workflow",
		Version:     "1.2.0",
		Description: stringPtr("old description"),
		Author:      stringPtr("Memory"),
		Source:      "manual",
		ObjectTypeSchemasSnake: json.RawMessage(`[
			{"name":"GitHubIssue","properties":{"title":{"type":"string"}}}
		]`),
		RelationshipTypeSchemasSnake: json.RawMessage(`[]`),
	})
	require.NoError(t, err)
	require.NotNil(t, created.Checksum)
	originalChecksum := *created.Checksum

	_, err = svc.AssignPack(ctx, projectID, "", &schemas.AssignPackRequest{
		SchemaID: created.ID,
		Merge:    true,
	})
	require.NoError(t, err)

	reconciled, err := svc.ReconcileProjectPack(ctx, projectID, &schemas.CreatePackRequest{
		Name:        "github-app-workflow",
		Version:     "1.3.0",
		Description: stringPtr("new description"),
		Author:      stringPtr("Memory"),
		Source:      "provider:github_app",
		ObjectTypeSchemasSnake: json.RawMessage(`[
			{"name":"GitHubIssue","properties":{"title":{"type":"string"}},"ui":{"icon":"circle","color":"#10B981"}}
		]`),
		RelationshipTypeSchemasSnake: json.RawMessage(`[]`),
	})
	require.NoError(t, err)

	assert.Equal(t, created.ID, reconciled.ID, "reconcile must update the existing row")
	assert.Equal(t, "1.3.0", reconciled.Version)
	require.NotNil(t, reconciled.Source)
	assert.Equal(t, "provider:github_app", *reconciled.Source)
	// object_type_schemas is jsonb: Postgres re-reads it with spaces and
	// reordered keys, so decode rather than substring-match the raw bytes.
	assertTypeUI(t, reconciled.ObjectTypeSchemas, "circle", "#10B981")
	require.NotNil(t, reconciled.Checksum)
	assert.NotEqual(t, originalChecksum, *reconciled.Checksum, "checksum must follow the content")

	stored, err := repo.GetPack(ctx, created.ID, projectID)
	require.NoError(t, err)
	assertTypeUI(t, stored.ObjectTypeSchemas, "circle", "#10B981")

	count, err := db.NewSelect().Model((*schemas.GraphMemorySchema)(nil)).
		Where("project_id = ?", projectID).
		Where("name = ?", "github-app-workflow").
		Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "reconcile must not create a second version row")

	// The assignment is untouched, and a second reconcile is idempotent.
	compiled, err := svc.GetCompiledTypes(ctx, projectID)
	require.NoError(t, err)
	assert.Contains(t, typeNames(compiled.ObjectTypes), "GitHubIssue")

	again, err := svc.ReconcileProjectPack(ctx, projectID, &schemas.CreatePackRequest{
		Name:    "github-app-workflow",
		Version: "1.3.0",
		Source:  "provider:github_app",
		ObjectTypeSchemasSnake: json.RawMessage(`[
			{"name":"GitHubIssue","properties":{"title":{"type":"string"}},"ui":{"icon":"circle","color":"#10B981"}}
		]`),
		RelationshipTypeSchemasSnake: json.RawMessage(`[]`),
	})
	require.NoError(t, err)
	assert.Equal(t, created.ID, again.ID)
}

func stringPtr(s string) *string { return &s }

// assertTypeUI decodes a stored object-type array and asserts the first type's
// presentation block.
func assertTypeUI(t *testing.T, raw json.RawMessage, icon, color string) {
	t.Helper()
	var defs []struct {
		Name string         `json:"name"`
		UI   map[string]any `json:"ui"`
	}
	require.NoError(t, json.Unmarshal(raw, &defs))
	require.Len(t, defs, 1)
	require.NotNil(t, defs[0].UI)
	assert.Equal(t, icon, defs[0].UI["icon"])
	assert.Equal(t, color, defs[0].UI["color"])
}

func typeNames(types []schemas.ObjectTypeSchema) []string {
	out := make([]string, 0, len(types))
	for _, t := range types {
		out = append(out, t.Name)
	}
	return out
}
