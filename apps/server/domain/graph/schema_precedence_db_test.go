package graph_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/schemas"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// TestSchemaProviderLastInstallWins is the graph-validation half of issue #295:
// when two active packs declare the same object type, property validation must
// use the definition from the newest install, matching the compiled-types view
// and the type registry. The rows are inserted in reverse order so an unordered
// scan (the old behaviour) would pick the older pack.
func TestSchemaProviderLastInstallWins(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "Skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "graph_schema_precedence")
	t.Cleanup(tdb.Close)
	db := tdb.DB

	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "Schema Precedence Org"))
	projectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID: projectID, OrgID: orgID, Name: "Schema Precedence " + projectID,
	}, testutil.AdminUser.ID))

	now := time.Now()
	insert := func(packName, props string, installedAt time.Time) {
		pack := &schemas.GraphMemorySchema{
			ID:                      uuid.NewString(),
			Name:                    packName,
			Version:                 "1.0.0",
			ObjectTypeSchemas:       json.RawMessage(`[{"name":"Person","properties":` + props + `}]`),
			RelationshipTypeSchemas: json.RawMessage(`[]`),
			UIConfigs:               json.RawMessage(`{}`),
			ExtractionPrompts:       json.RawMessage(`{}`),
			ProjectID:               &projectID,
		}
		_, err := db.NewInsert().Model(pack).Exec(ctx)
		require.NoError(t, err)
		assignment := &schemas.ProjectMemorySchema{
			ID: uuid.NewString(), ProjectID: projectID, SchemaID: pack.ID,
			Active: true, InstalledAt: installedAt,
		}
		_, err = db.NewInsert().Model(assignment).Exec(ctx)
		require.NoError(t, err)
	}

	// Insert the newest pack first to make the assertion order-sensitive.
	insert("zz-newest", `{"legal_name":{"type":"string"}}`, now)
	insert("aa-oldest", `{"name":{"type":"string"}}`, now.Add(-time.Hour))

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	provider := graph.ProvideSchemaProvider(db, log)

	got, err := provider.GetProjectSchemas(ctx, projectID)
	require.NoError(t, err)
	person, ok := got.ObjectSchemas["Person"]
	require.True(t, ok, "Person must be registered")
	assert.Contains(t, person.Properties, "legal_name", "the newest install must define the type")
	assert.NotContains(t, person.Properties, "name")
}
