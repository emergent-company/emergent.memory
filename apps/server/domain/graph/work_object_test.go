package graph_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// setupWorkObjectTest opens a throwaway test database and returns a graph
// service wired with no journal/embedding/schema deps, plus the project id.
func setupWorkObjectTest(t *testing.T) (context.Context, *graph.Service, uuid.UUID) {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "integration test requires database")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "graph_work_object")
	t.Cleanup(tdb.Close)
	db := tdb.GetDB()

	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "Work Org"))
	projectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "Work Project",
	}, testutil.AdminUser.ID))

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{}
	cfg.Graph.MaxListLimit = 100_000
	repo := graph.NewRepository(db, log, cfg)
	svc := graph.NewService(repo, log, nil, nil, nil, nil, nil, graph.NoopEventSink{}, nil, nil)
	return ctx, svc, uuid.MustParse(projectID)
}

// TestClaimWorkObject_TransitionsReadyToInProgress verifies the claim
// transitions ready→in_progress under the versioned write model, and that a
// second claim loses the race (claimed=false) without error.
func TestClaimWorkObject_TransitionsReadyToInProgress(t *testing.T) {
	ctx, svc, projectID := setupWorkObjectTest(t)

	key := "gemini-model-2026"
	ready := "ready"
	obj, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:       "ResearchRequest",
		Key:        &key,
		Status:     &ready,
		Properties: map[string]any{"topic": "new Gemini model"},
	}, nil)
	require.NoError(t, err)
	require.NotNil(t, obj)

	claimed, err := svc.ClaimWorkObject(ctx, projectID.String(), obj.CanonicalID.String(), "ready", "in_progress")
	require.NoError(t, err)
	require.True(t, claimed)

	head, err := svc.GetHeadObject(ctx, projectID.String(), obj.CanonicalID.String())
	require.NoError(t, err)
	require.NotNil(t, head)
	require.Equal(t, "in_progress", head.Status)
	require.Equal(t, 2, head.Version)
	require.Equal(t, key, head.Key)
	require.Equal(t, "ResearchRequest", head.Type)

	// A second claim for the same object loses the race: the object is no longer
	// "ready", so claimed=false with no error and no further version bump.
	claimed2, err := svc.ClaimWorkObject(ctx, projectID.String(), obj.CanonicalID.String(), "ready", "in_progress")
	require.NoError(t, err)
	require.False(t, claimed2)

	headAfter, err := svc.GetHeadObject(ctx, projectID.String(), obj.CanonicalID.String())
	require.NoError(t, err)
	require.Equal(t, "in_progress", headAfter.Status)
	require.Equal(t, 2, headAfter.Version)
}

// TestClaimWorkObject_MissingKeyNotClaimed verifies a work object without a key
// is not claimable (board-enabled work requires a stable identity).
func TestClaimWorkObject_MissingKeyNotClaimed(t *testing.T) {
	ctx, svc, projectID := setupWorkObjectTest(t)

	ready := "ready"
	obj, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:   "ResearchRequest",
		Status: &ready,
	}, nil)
	require.NoError(t, err)

	claimed, err := svc.ClaimWorkObject(ctx, projectID.String(), obj.CanonicalID.String(), "ready", "in_progress")
	require.NoError(t, err)
	require.False(t, claimed)
}

// TestClaimWorkObject_NotReadyNotClaimed verifies a claim only succeeds from
// the ready status.
func TestClaimWorkObject_NotReadyNotClaimed(t *testing.T) {
	ctx, svc, projectID := setupWorkObjectTest(t)

	key := "blocked-item"
	blocked := "blocked"
	obj, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:   "ResearchRequest",
		Key:    &key,
		Status: &blocked,
	}, nil)
	require.NoError(t, err)

	claimed, err := svc.ClaimWorkObject(ctx, projectID.String(), obj.CanonicalID.String(), "ready", "in_progress")
	require.NoError(t, err)
	require.False(t, claimed)

	head, err := svc.GetHeadObject(ctx, projectID.String(), obj.CanonicalID.String())
	require.NoError(t, err)
	require.Equal(t, "blocked", head.Status)
}
