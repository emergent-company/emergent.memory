package graph_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
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

// TestPatchRelationshipValidatesAgainstEndpointSchema is the regression for
// #361 (follow-up to #322/#357). Relationship create already resolves the
// property schema by (name, source type, target type); the patch path still
// used the name-wide default, which is the newest install's declaration for the
// name. With two packs declaring `owns` for different endpoint pairs, patching
// a valid Person→Vehicle edge was validated against the Person→Account schema
// and failed with `missing required field: holder_role`.
func TestPatchRelationshipValidatesAgainstEndpointSchema(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "Skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "graph_relationship_patch_identity")
	t.Cleanup(tdb.Close)
	db := tdb.GetDB()

	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "Relationship Patch Identity Org"))
	projectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID: projectID, OrgID: orgID, Name: "Relationship Patch Identity " + projectID,
	}, testutil.AdminUser.ID))

	now := time.Now()
	insertPack := func(packName, relationshipTypes string, installedAt time.Time) {
		t.Helper()
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

	// Newest pack last installed, so the name-wide `owns` default is the
	// Person→Account schema — exactly the shape a bare-name patch lookup picks.
	insertPack("asset-register", `[{"name":"owns","sourceType":"Person","targetType":"Vehicle",
		"properties":{"share_percent":{"type":"number"}},"required":["share_percent"]}]`, now.Add(-time.Hour))
	insertPack("banking", `[{"name":"owns","sourceType":"Person","targetType":"Account",
		"properties":{"holder_role":{"type":"string"}},"required":["holder_role"]}]`, now)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	provider := graph.ProvideSchemaProvider(db, log)
	repo := graph.NewRepository(db, log, tdb.Config)
	svc := graph.NewService(repo, log, provider, nil, nil, nil, nil, graph.NoopEventSink{}, nil, nil)

	newObject := func(objType, key string) *graph.GraphObjectResponse {
		t.Helper()
		obj, err := svc.Create(ctx, uuid.MustParse(projectID), &graph.CreateGraphObjectRequest{
			Type: objType, Key: strPtr(key),
		}, nil)
		require.NoError(t, err)
		return obj
	}
	person := newObject("Person", "patch-identity-person")
	vehicle := newObject("Vehicle", "patch-identity-vehicle")
	account := newObject("Account", "patch-identity-account")
	projectUUID := uuid.MustParse(projectID)

	// create vehicle edge, valid against the Person→Vehicle schema
	vehicleEdge, err := svc.CreateRelationship(ctx, projectUUID, &graph.CreateGraphRelationshipRequest{
		Type:       "owns",
		SrcID:      person.CanonicalID,
		DstID:      vehicle.CanonicalID,
		Properties: map[string]any{"share_percent": 40},
	}, nil)
	require.NoError(t, err, "creating a Person→Vehicle owns edge must validate against share_percent")

	// Pre-#361 this patch was validated against the name-wide default (the
	// Person→Account schema) and failed with `missing required field: holder_role`
	// even though the edge's stored properties are complete for its own schema.
	patched, err := svc.PatchRelationship(ctx, projectUUID, vehicleEdge.CanonicalID, &graph.PatchGraphRelationshipRequest{
		Properties: map[string]any{"share_percent": 55},
	}, nil)
	require.NoError(t, err, "patching a Person→Vehicle owns edge must validate against its own endpoint schema")
	assert.EqualValues(t, 55, patched.Properties["share_percent"])

	// Dropping the Vehicle schema's required property must still be rejected.
	_, err = svc.PatchRelationship(ctx, projectUUID, vehicleEdge.CanonicalID, &graph.PatchGraphRelationshipRequest{
		Properties: map[string]any{"share_percent": nil},
	}, nil)
	require.Error(t, err, "a Vehicle edge may not drop its required share_percent")
	assert.Contains(t, err.Error(), "share_percent")

	// And the Account endpoint keeps its own schema: a patch that leaves only
	// the Vehicle-only property behind is rejected for missing holder_role.
	accountEdge, err := svc.CreateRelationship(ctx, projectUUID, &graph.CreateGraphRelationshipRequest{
		Type:       "owns",
		SrcID:      person.CanonicalID,
		DstID:      account.CanonicalID,
		Properties: map[string]any{"holder_role": "primary"},
	}, nil)
	require.NoError(t, err, "creating a Person→Account owns edge must validate against holder_role")

	_, err = svc.PatchRelationship(ctx, projectUUID, accountEdge.CanonicalID, &graph.PatchGraphRelationshipRequest{
		Properties: map[string]any{"holder_role": nil, "share_percent": 40},
	}, nil)
	require.Error(t, err, "a Person→Account owns edge may not be reduced to Vehicle-only properties")
	assert.Contains(t, err.Error(), "holder_role")
}
