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

func queueRepo(t *testing.T, ctx context.Context) (*discoveryjobs.Repository, string) {
	testDB := testutil.SetupTestDBOrFail(t, ctx, "discoveryjobs_queue")
	t.Cleanup(testDB.Close)
	require.NoError(t, testutil.TruncateTables(ctx, testDB.DB))
	require.NoError(t, testutil.SetupTestFixtures(ctx, testDB.DB))

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	require.NoError(t, testutil.SetupFullTestProject(ctx, testDB.DB, orgID, projectID))

	repo := discoveryjobs.NewRepository(testDB.DB, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return repo, projectID
}

func TestEnqueueCoalescesIntoOneActiveRow(t *testing.T) {
	ctx := context.Background()
	repo, projectID := queueRepo(t, ctx)
	p := uuid.MustParse(projectID)
	docA, docB, docC := uuid.New(), uuid.New(), uuid.New()

	require.NoError(t, repo.Enqueue(ctx, p, []uuid.UUID{docA, docB}))
	require.NoError(t, repo.Enqueue(ctx, p, []uuid.UUID{docC}))

	rows, err := repo.ListQueued(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1, "enqueue must coalesce into a single active row")
	require.ElementsMatch(t, []uuid.UUID{docA, docB, docC}, rows[0].DocumentIDs)
}

func TestClaimAndFinishLifecycle(t *testing.T) {
	ctx := context.Background()
	repo, projectID := queueRepo(t, ctx)
	p := uuid.MustParse(projectID)
	docA, docB := uuid.New(), uuid.New()
	require.NoError(t, repo.Enqueue(ctx, p, []uuid.UUID{docA, docB}))

	rows, _ := repo.ListQueued(ctx)
	require.Len(t, rows, 1)
	reqID := rows[0].ID

	ids, attempts, ok, err := repo.Claim(ctx, reqID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 1, attempts)
	require.ElementsMatch(t, []uuid.UUID{docA, docB}, ids)

	// Second claim is refused.
	_, _, ok2, err := repo.Claim(ctx, reqID)
	require.NoError(t, err)
	require.False(t, ok2)

	// Finish with a remainder requeues it.
	require.NoError(t, repo.Finish(ctx, reqID, []uuid.UUID{docB}))
	rows, err = repo.ListQueued(ctx)
	require.Len(t, rows, 1)
	require.ElementsMatch(t, []uuid.UUID{docB}, rows[0].DocumentIDs)

	// Finish with no remainder removes the row.
	_, _, ok3, _ := repo.Claim(ctx, rows[0].ID)
	require.True(t, ok3)
	require.NoError(t, repo.Finish(ctx, rows[0].ID, nil))
	rows, _ = repo.ListQueued(ctx)
	require.Len(t, rows, 0)
}

func TestEnqueueRequeuesARunningRow(t *testing.T) {
	ctx := context.Background()
	repo, projectID := queueRepo(t, ctx)
	p := uuid.MustParse(projectID)
	docA, docB := uuid.New(), uuid.New()
	require.NoError(t, repo.Enqueue(ctx, p, []uuid.UUID{docA}))

	rows, _ := repo.ListQueued(ctx)
	reqID := rows[0].ID
	claimed, _, ok, _ := repo.Claim(ctx, reqID)
	require.True(t, ok)
	require.ElementsMatch(t, []uuid.UUID{docA}, claimed, "claim must atomically own the queued ids")

	// A new enqueue during the run window appends to the now-empty running row
	// and re-marks it queued; the claimed ids are owned by the runner and not
	// re-queued.
	require.NoError(t, repo.Enqueue(ctx, p, []uuid.UUID{docB}))
	rows, _ = repo.ListQueued(ctx)
	require.Len(t, rows, 1)
	require.ElementsMatch(t, []uuid.UUID{docB}, rows[0].DocumentIDs)
}
