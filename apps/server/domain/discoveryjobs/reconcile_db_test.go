package discoveryjobs_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/discoveryjobs"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// TestUpsertDiscoveredMemorySchemaReusesCurrentRow is the schema-row half of
// issue #497: a second discovery run over the same project must update the
// existing pending discovered pack instead of appending a new timestamped row.
func TestUpsertDiscoveredMemorySchemaReusesCurrentRow(t *testing.T) {
	ctx := context.Background()
	testDB, err := testutil.SetupTestDB(ctx, "discoveryjobs_reconcile")
	if err != nil {
		t.Skipf("test database unavailable: %v", err)
	}
	defer testDB.Close()
	require.NoError(t, testutil.TruncateTables(ctx, testDB.DB))
	require.NoError(t, testutil.SetupTestFixtures(ctx, testDB.DB))

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	require.NoError(t, testutil.SetupFullTestProject(ctx, testDB.DB, orgID, projectID))
	projectUUID := uuid.MustParse(projectID)

	repo := discoveryjobs.NewRepository(
		testDB.DB,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	params := discoveryjobs.CreateMemorySchemaParams{
		Name:          "Discovered Types - first",
		Version:       "1.0.0",
		Description:   "first run",
		Author:        "Auto-Discovery System",
		Source:        "discovered",
		PendingReview: true,
		ProjectID:     &projectUUID,
		ObjectTypeSchemas: discoveryjobs.JSONMap{
			"Organization": map[string]any{"type": "object"},
		},
		RelationshipTypeSchemas: discoveryjobs.JSONMap{},
		UIConfigs:               discoveryjobs.JSONMap{},
	}

	firstID, created, err := repo.UpsertDiscoveredMemorySchema(ctx, params)
	require.NoError(t, err)
	require.True(t, created)

	params.Name = "Discovered Types - second"
	secondID, created, err := repo.UpsertDiscoveredMemorySchema(ctx, params)
	require.NoError(t, err)
	require.False(t, created, "repeat discovery must reuse the current pack")
	require.Equal(t, firstID, secondID)

	var count int
	require.NoError(t, testDB.DB.NewRaw(
		`SELECT count(*) FROM kb.graph_schemas
		 WHERE project_id = ? AND source = 'discovered' AND pending_review = true AND superseded_by IS NULL`,
		projectUUID,
	).Scan(ctx, &count))
	require.Equal(t, 1, count, "exactly one current pending discovered schema may exist per project")
}
