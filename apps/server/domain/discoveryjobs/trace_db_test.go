package discoveryjobs_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/discoveryjobs"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// TestSetTraceIDPersists pins that the discovery job trace id round-trips
// through the repository and that an empty id is a no-op (NULL stays NULL).
func TestSetTraceIDPersists(t *testing.T) {
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "discoveryjobs_trace")
	t.Cleanup(testDB.Close)
	require.NoError(t, testutil.TruncateTables(ctx, testDB.DB))
	require.NoError(t, testutil.SetupTestFixtures(ctx, testDB.DB))

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	require.NoError(t, testutil.SetupFullTestProject(ctx, testDB.DB, orgID, projectID))

	repo := discoveryjobs.NewRepository(testDB.DB, slog.New(slog.NewTextHandler(io.Discard, nil)))

	job := &discoveryjobs.DiscoveryJob{
		ID:             uuid.New(),
		OrganizationID: uuid.MustParse(orgID),
		ProjectID:      uuid.MustParse(projectID),
		Status:         discoveryjobs.StatusPending,
	}
	require.NoError(t, repo.Create(ctx, job))

	// Empty is a no-op: the row stays with no trace id.
	require.NoError(t, repo.SetTraceID(ctx, job.ID, ""))
	got, err := repo.GetByID(ctx, job.ID)
	require.NoError(t, err)
	require.Nil(t, got.TraceID, "empty trace id must not write")

	// A real trace id round-trips.
	traceID := strings.Repeat("a", 32)
	require.NoError(t, repo.SetTraceID(ctx, job.ID, traceID))
	got, err = repo.GetByID(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, got.TraceID)
	require.Equal(t, traceID, *got.TraceID)
}
