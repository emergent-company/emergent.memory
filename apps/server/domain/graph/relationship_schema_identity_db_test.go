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

// TestSchemaProviderKeysRelationshipSchemasByEndpoints is the database half of
// issue #322: two active packs declare `owns` with different endpoint pairs and
// different property schemas. The provider must keep both definitions, keyed by
// (name, source type, target type), instead of collapsing them into one entry
// chosen by scan order.
//
// The packs are inserted newest-first so an unordered assignment scan (the old
// behaviour) would leave the older pack's definition in place.
func TestSchemaProviderKeysRelationshipSchemasByEndpoints(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "Skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "graph_relationship_identity")
	t.Cleanup(tdb.Close)
	db := tdb.DB

	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "Relationship Identity Org"))
	projectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID: projectID, OrgID: orgID, Name: "Relationship Identity " + projectID,
	}, testutil.AdminUser.ID))

	now := time.Now()
	insert := func(packName, relationshipTypes string, installedAt time.Time) {
		pack := &schemas.GraphMemorySchema{
			ID:                      uuid.NewString(),
			Name:                    packName,
			Version:                 "1.0.0",
			ObjectTypeSchemas:       json.RawMessage(`[]`),
			RelationshipTypeSchemas: json.RawMessage(relationshipTypes),
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

	insert("banking", `[{"name":"owns","sourceType":"Person","targetType":"Account",
		"properties":{"holder_role":{"type":"string"}},"required":["holder_role"]}]`, now)
	insert("asset-register", `[{"name":"owns","sourceType":"Person","targetType":"Vehicle",
		"properties":{"share_percent":{"type":"number"}},"required":["share_percent"]}]`, now.Add(-time.Hour))

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	provider := graph.ProvideSchemaProvider(db, log)

	got, err := provider.GetProjectSchemas(ctx, projectID)
	require.NoError(t, err)

	// Each endpoint-qualified entry is narrowed to the pair it was declared for,
	// so the pair identifies which property schema it carries. No entry may carry
	// both property schemas — that is the collapse this test guards against.
	var vehicle, account bool
	for _, schema := range got.RelationshipSchemas {
		_, hasShare := schema.Properties["share_percent"]
		_, hasHolder := schema.Properties["holder_role"]
		if hasShare && hasHolder {
			t.Fatalf("relationship schema mixes both packs' properties: %+v", schema)
		}
		if hasShare {
			vehicle = true
			assert.Equal(t, []string{"Vehicle"}, schema.TargetTypes,
				"the share_percent schema must be the Person→Vehicle declaration")
		}
		if hasHolder {
			account = true
		}
	}

	assert.True(t, vehicle, "owns Person→Vehicle must survive alongside owns Person→Account")
	assert.True(t, account, "owns Person→Account must survive alongside owns Person→Vehicle")

	// The name-wide default keeps the newest install's declaration, matching the
	// object-type precedence (#295, #336) rather than an arbitrary row order.
	fallback := got.RelationshipSchemas["owns"]
	assert.Contains(t, fallback.Properties, "holder_role")
}
