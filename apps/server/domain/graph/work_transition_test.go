package graph_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// setupWorkTransitionTest returns a graph service plus a raw DB handle so tests
// can assert properties["status"] consistency directly.
func setupWorkTransitionTest(t *testing.T) (context.Context, *graph.Service, uuid.UUID, bun.IDB) {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "integration test requires database")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "graph_work_transition")
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
	// ResearchRequest is board-enabled (no allowed-status set → status values
	// unconstrained) so the single-status-writer guard applies via the real P4
	// per-type flag rather than the removed assignee heuristic.
	svc := graph.NewService(repo, log, &fakeSchemaProvider{objectSchemas: map[string]agents.ObjectSchema{
		"ResearchRequest": {ObjectTypeWorkConfig: agents.ObjectTypeWorkConfig{BoardEnabled: true}},
	}}, nil, nil, nil, nil, graph.NoopEventSink{}, nil, nil)
	return ctx, svc, uuid.MustParse(projectID), db
}

func createAssignedWorkObject(t *testing.T, ctx context.Context, svc *graph.Service, projectID uuid.UUID, key, status string) *graph.GraphObjectResponse {
	t.Helper()
	assignee := "researcher"
	obj, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:       "ResearchRequest",
		Key:        &key,
		Status:     &status,
		Assignee:   &assignee,
		Properties: map[string]any{"topic": "new Gemini model"},
	}, nil)
	require.NoError(t, err)
	return obj
}

func statusProperty(t *testing.T, ctx context.Context, db bun.IDB, canonicalID string) string {
	t.Helper()
	var s string
	require.NoError(t, db.NewRaw(
		`SELECT properties->>'status' FROM kb.graph_objects WHERE canonical_id = ?::uuid AND supersedes_id IS NULL`,
		canonicalID,
	).Scan(ctx, &s))
	return s
}

func TestCompleteWorkObject_Done(t *testing.T) {
	ctx, svc, projectID, db := setupWorkTransitionTest(t)
	obj := createAssignedWorkObject(t, ctx, svc, projectID, "k-1", "in_progress")

	ok, err := svc.CompleteWorkObject(ctx, projectID.String(), obj.CanonicalID.String(), "in_progress", "done", "review", false)
	require.NoError(t, err)
	require.True(t, ok)

	head, err := svc.GetHeadObject(ctx, projectID.String(), obj.CanonicalID.String())
	require.NoError(t, err)
	require.Equal(t, "done", head.Status)
	require.Equal(t, "done", statusProperty(t, ctx, db, obj.CanonicalID.String()))
}

func TestCompleteWorkObject_Review(t *testing.T) {
	ctx, svc, projectID, db := setupWorkTransitionTest(t)
	obj := createAssignedWorkObject(t, ctx, svc, projectID, "k-2", "in_progress")

	ok, err := svc.CompleteWorkObject(ctx, projectID.String(), obj.CanonicalID.String(), "in_progress", "done", "review", true)
	require.NoError(t, err)
	require.True(t, ok)

	head, err := svc.GetHeadObject(ctx, projectID.String(), obj.CanonicalID.String())
	require.NoError(t, err)
	require.Equal(t, "review", head.Status)

	var needsReview bool
	require.NoError(t, db.NewRaw(
		`SELECT needs_review FROM kb.graph_objects WHERE canonical_id = ?::uuid AND supersedes_id IS NULL`,
		obj.CanonicalID.String(),
	).Scan(ctx, &needsReview))
	require.True(t, needsReview)
}

func TestBlockWorkObject(t *testing.T) {
	ctx, svc, projectID, db := setupWorkTransitionTest(t)
	obj := createAssignedWorkObject(t, ctx, svc, projectID, "k-3", "in_progress")

	ok, err := svc.BlockWorkObject(ctx, projectID.String(), obj.CanonicalID.String(), "in_progress", "blocked")
	require.NoError(t, err)
	require.True(t, ok)

	head, err := svc.GetHeadObject(ctx, projectID.String(), obj.CanonicalID.String())
	require.NoError(t, err)
	require.Equal(t, "blocked", head.Status)
	require.Equal(t, "blocked", statusProperty(t, ctx, db, obj.CanonicalID.String()))
}

func TestUnassignWorkObject(t *testing.T) {
	ctx, svc, projectID, _ := setupWorkTransitionTest(t)
	obj := createAssignedWorkObject(t, ctx, svc, projectID, "k-4", "in_progress")

	ok, err := svc.UnassignWorkObject(ctx, projectID.String(), obj.CanonicalID.String(), "in_progress", "ready")
	require.NoError(t, err)
	require.True(t, ok)

	head, err := svc.GetHeadObject(ctx, projectID.String(), obj.CanonicalID.String())
	require.NoError(t, err)
	require.Equal(t, "ready", head.Status)
	require.Equal(t, "", head.Assignee)
}

func TestTransitionWorkObject_KeepsPropertiesStatusConsistent(t *testing.T) {
	ctx, svc, projectID, db := setupWorkTransitionTest(t)
	obj := createAssignedWorkObject(t, ctx, svc, projectID, "k-5", "ready")

	ok, err := svc.TransitionWorkObject(ctx, projectID.String(), obj.CanonicalID.String(), graph.WorkObjectTransition{
		FromStatus: "ready",
		ToStatus:   "in_progress",
	})
	require.NoError(t, err)
	require.True(t, ok)

	head, err := svc.GetHeadObject(ctx, projectID.String(), obj.CanonicalID.String())
	require.NoError(t, err)
	require.Equal(t, "in_progress", head.Status)
	require.Equal(t, "in_progress", statusProperty(t, ctx, db, obj.CanonicalID.String()))
}

// TestAgentDirectStatusWriteRejected verifies an agent-originated create that
// sets status on a board-enabled (assignee) object is rejected.
func TestAgentDirectStatusWriteRejected(t *testing.T) {
	ctx, svc, projectID, _ := setupWorkTransitionTest(t)

	agentID := uuid.New()
	ctx = auth.WithActor(ctx, graph.ActorAgent, &agentID)

	assignee := "researcher"
	status := "done"
	key := "agent-tries-to-set-done"
	_, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:     "ResearchRequest",
		Key:      &key,
		Status:   &status,
		Assignee: &assignee,
	}, nil)
	require.Error(t, err)
}

// TestAgentPropertiesStatusWriteRejected verifies an agent cannot smuggle status
// through properties["status"] on a board-enabled object.
func TestAgentPropertiesStatusWriteRejected(t *testing.T) {
	ctx, svc, projectID, _ := setupWorkTransitionTest(t)

	agentID := uuid.New()
	ctx = auth.WithActor(ctx, graph.ActorAgent, &agentID)

	assignee := "researcher"
	key := "agent-smuggles-status"
	_, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:       "ResearchRequest",
		Key:        &key,
		Assignee:   &assignee,
		Properties: map[string]any{"status": "done"},
	}, nil)
	require.Error(t, err)
}

// TestNonBoardAgentStatusWriteAllowed verifies an agent writing status on a
// non-board (no assignee) object is allowed — the guard only applies to
// board-enabled types.
func TestNonBoardAgentStatusWriteAllowed(t *testing.T) {
	ctx, svc, projectID, _ := setupWorkTransitionTest(t)

	agentID := uuid.New()
	ctx = auth.WithActor(ctx, graph.ActorAgent, &agentID)

	status := "done"
	key := "non-board-object"
	obj, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:   "Document",
		Key:    &key,
		Status: &status,
	}, nil)
	require.NoError(t, err)
	require.NotNil(t, obj)
}
