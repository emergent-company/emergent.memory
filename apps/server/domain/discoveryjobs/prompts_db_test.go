package discoveryjobs_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/discoveryjobs"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// TestSchemaExtractionPromptsRoundTrip pins that the stored extraction prompts
// (and their embedded fingerprint) round-trip through the repository, which is
// what the discovery run's reuse check relies on.
func TestSchemaExtractionPromptsRoundTrip(t *testing.T) {
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "discoveryjobs_prompts")
	t.Cleanup(testDB.Close)
	require.NoError(t, testutil.TruncateTables(ctx, testDB.DB))
	require.NoError(t, testutil.SetupTestFixtures(ctx, testDB.DB))

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	require.NoError(t, testutil.SetupFullTestProject(ctx, testDB.DB, orgID, projectID))
	projectUUID := uuid.MustParse(projectID)

	repo := discoveryjobs.NewRepository(testDB.DB, slog.New(slog.NewTextHandler(io.Discard, nil)))

	schemaID, err := repo.CreateMemorySchema(ctx, discoveryjobs.CreateMemorySchemaParams{
		Name:              "Discovered Types - t",
		Version:           "1.0.0",
		Description:       "test",
		Author:            "t",
		ObjectTypeSchemas: discoveryjobs.JSONMap{},
		UIConfigs:         discoveryjobs.JSONMap{},
		Source:            "discovered",
		ProjectID:         &projectUUID,
	})
	require.NoError(t, err)

	// A fresh schema stores the jsonb default `{}` — no fingerprint yet.
	raw, err := repo.GetSchemaExtractionPrompts(ctx, schemaID)
	require.NoError(t, err)
	var before map[string]string
	require.NoError(t, json.Unmarshal(raw, &before))
	require.Equal(t, "", before["__fingerprint"], "no fingerprint before prompts are stored")

	prompts := map[string]any{"domainContext": "d", "__fingerprint": "abc123"}
	body, err := json.Marshal(prompts)
	require.NoError(t, err)
	require.NoError(t, repo.UpdateSchemaExtractionPrompts(ctx, schemaID, body))

	raw, err = repo.GetSchemaExtractionPrompts(ctx, schemaID)
	require.NoError(t, err)
	var got map[string]string
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Equal(t, "abc123", got["__fingerprint"])
}
