package schemas_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/schemas"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// TestUpdatePackValidatesScopeKey is the missing dedicated regression test for
// the update path: UpdatePack must validate supplied schema definitions the same
// way CreatePack does, including scope-key declarations. Before the UpdatePack
// validation was added, an invalid declaration was persisted silently (the
// CreatePack-equivalent check only ran on create).
func TestUpdatePackValidatesScopeKey(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "updpk")
	t.Cleanup(testDB.Close)
	db := testDB.GetDB()

	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "UpdatePack Org"))
	projectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "UpdatePack Project",
	}, testutil.AdminUser.ID))

	repo := schemas.NewRepository(db, slog.Default())
	svc := schemas.NewService(repo, nil, slog.Default(), &config.Config{})

	pack, err := repo.CreatePack(ctx, projectID, &schemas.CreatePackRequest{
		Name:    "law",
		Version: "1.0.0",
		ObjectTypeSchemasSnake: json.RawMessage(`[
			{"name":"Law","properties":{"ref_id":{"type":"string"}}},
			{"name":"LegalParagraph","properties":{"law_ref_id":{"type":"string"},"chapter_id":{"type":"string"}}}
		]`),
		RelationshipTypeSchemasSnake: json.RawMessage(`[]`),
	})
	require.NoError(t, err)

	// Invalid: scopeKey names a property the type does not declare. The update
	// must be rejected with the same actionable message as CreatePack.
	_, err = svc.UpdatePack(ctx, pack.ID, projectID, &schemas.UpdatePackRequest{
		ObjectTypeSchemas: json.RawMessage(`[
			{"name":"Law","properties":{"ref_id":{"type":"string"}}},
			{"name":"LegalParagraph","properties":{"chapter_id":{"type":"string"}},
			 "scopeKey":{"property":"law_ref_id","referencesType":"Law","referencesProperty":"ref_id"}}
		]`),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `scopeKey.property "law_ref_id" is not a declared property`)

	// The invalid update must not have been persisted.
	after, err := repo.GetPack(ctx, pack.ID, projectID)
	require.NoError(t, err)
	assert.NotContains(t, string(after.ObjectTypeSchemas), "scopeKey", "rejected update must not persist")

	// A valid declaration updates and persists through the same path.
	updated, err := svc.UpdatePack(ctx, pack.ID, projectID, &schemas.UpdatePackRequest{
		ObjectTypeSchemas: json.RawMessage(`[
			{"name":"Law","properties":{"ref_id":{"type":"string"}}},
			{"name":"LegalParagraph","properties":{"law_ref_id":{"type":"string"},"chapter_id":{"type":"string"}},
			 "scopeKey":{"property":"law_ref_id","referencesType":"Law","referencesProperty":"ref_id"}}
		]`),
	})
	require.NoError(t, err)
	assert.Contains(t, string(updated.ObjectTypeSchemas), `"scopeKey"`)
}
