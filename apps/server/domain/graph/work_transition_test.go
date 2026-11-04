package graph_test

import (
	"context"
	"encoding/json"
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

// TestUpdateWorkObjectStatus verifies the exact-status write: an any→status
// transition that sets needs_review on the review status and clears it off
// review, keeping status and properties["status"] consistent.
func TestUpdateWorkObjectStatus(t *testing.T) {
	ctx, svc, projectID, db := setupWorkTransitionTest(t)
	obj := createAssignedWorkObject(t, ctx, svc, projectID, "k-set", "in_progress")

	ok, err := svc.UpdateWorkObjectStatus(ctx, projectID.String(), obj.CanonicalID.String(), "review", "review")
	require.NoError(t, err)
	require.True(t, ok)

	head, err := svc.GetHeadObject(ctx, projectID.String(), obj.CanonicalID.String())
	require.NoError(t, err)
	require.Equal(t, "review", head.Status)
	require.Equal(t, "review", statusProperty(t, ctx, db, obj.CanonicalID.String()))

	var needsReview bool
	require.NoError(t, db.NewRaw(
		`SELECT needs_review FROM kb.graph_objects WHERE canonical_id = ?::uuid AND supersedes_id IS NULL`,
		obj.CanonicalID.String(),
	).Scan(ctx, &needsReview))
	require.True(t, needsReview)

	// Off review clears needs_review.
	ok, err = svc.UpdateWorkObjectStatus(ctx, projectID.String(), obj.CanonicalID.String(), "done", "review")
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, db.NewRaw(
		`SELECT needs_review FROM kb.graph_objects WHERE canonical_id = ?::uuid AND supersedes_id IS NULL`,
		obj.CanonicalID.String(),
	).Scan(ctx, &needsReview))
	require.False(t, needsReview)
}

// TestUpdateWorkObjectStatus_AgentRejected verifies an agent-originated exact
// status write on a board-enabled object is rejected.
func TestUpdateWorkObjectStatus_AgentRejected(t *testing.T) {
	ctx, svc, projectID, _ := setupWorkTransitionTest(t)
	obj := createAssignedWorkObject(t, ctx, svc, projectID, "k-set-agent", "in_progress")

	agentID := uuid.New()
	agentCtx := auth.WithActor(ctx, graph.ActorAgent, &agentID)

	_, err := svc.UpdateWorkObjectStatus(agentCtx, projectID.String(), obj.CanonicalID.String(), "done", "review")
	require.Error(t, err)
}

// headChangeSummary reads and JSON-decodes the HEAD version's change_summary,
// mirroring the DB→API→gateway round trip ([]any/map[string]any shapes).
func headChangeSummary(t *testing.T, ctx context.Context, db bun.IDB, canonicalID string) map[string]any {
	t.Helper()
	var raw string
	require.NoError(t, db.NewRaw(
		`SELECT COALESCE(change_summary::text, '') FROM kb.graph_objects WHERE canonical_id = ?::uuid AND supersedes_id IS NULL`,
		canonicalID,
	).Scan(ctx, &raw))
	require.NotEmpty(t, raw, "HEAD change_summary is NULL")
	var cs map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &cs))
	return cs
}

// TestTransitionWorkObject_WritesChangeSummary is the #1850 regression: a
// work-path status transition must record an explicit change_summary (the status
// move as /status from→to), not rely on the gateway's derived fallback.
func TestTransitionWorkObject_WritesChangeSummary(t *testing.T) {
	ctx, svc, projectID, db := setupWorkTransitionTest(t)
	obj := createAssignedWorkObject(t, ctx, svc, projectID, "cs-1", "in_progress")

	ok, err := svc.CompleteWorkObject(ctx, projectID.String(), obj.CanonicalID.String(), "in_progress", "done", "review", false)
	require.NoError(t, err)
	require.True(t, ok)

	cs := headChangeSummary(t, ctx, db, obj.CanonicalID.String())
	updated, ok := cs["updated"].(map[string]any)
	require.True(t, ok, "change_summary.updated must be present, got %v", cs)
	status, ok := updated["/status"].(map[string]any)
	require.True(t, ok, "status move must be recorded as /status, got %v", updated)
	require.Equal(t, "in_progress", status["from"])
	require.Equal(t, "done", status["to"])
}

// TestReassignWorkObject_WritesAssigneeChangeSummary verifies the status-
// preserving reassign records the assignee move as a synthetic /assignee path
// and does not fabricate a /status change.
func TestReassignWorkObject_WritesAssigneeChangeSummary(t *testing.T) {
	ctx, svc, projectID, db := setupWorkTransitionTest(t)
	obj := createAssignedWorkObject(t, ctx, svc, projectID, "cs-2", "in_progress")

	next := "other-agent"
	ok, err := svc.ReassignWorkObject(ctx, projectID.String(), obj.CanonicalID.String(), &next)
	require.NoError(t, err)
	require.True(t, ok)

	cs := headChangeSummary(t, ctx, db, obj.CanonicalID.String())
	updated, ok := cs["updated"].(map[string]any)
	require.True(t, ok, "change_summary.updated must be present, got %v", cs)
	assignee, ok := updated["/assignee"].(map[string]any)
	require.True(t, ok, "assignee move must be recorded as /assignee, got %v", updated)
	require.Equal(t, "researcher", assignee["from"])
	require.Equal(t, "other-agent", assignee["to"])
	_, statusPresent := updated["/status"]
	require.False(t, statusPresent, "status-preserving reassign must not record a /status change")
}

// TestUnassignWorkObject_ChangeSummaryCoversStatusAndAssignee guards the
// conflict the issue warns about: a transition that moves both the status and
// the assignee must record both, so the recorded summary (which wins over the
// derived fallback) cannot hide the cleared assignee.
func TestUnassignWorkObject_ChangeSummaryCoversStatusAndAssignee(t *testing.T) {
	ctx, svc, projectID, db := setupWorkTransitionTest(t)
	obj := createAssignedWorkObject(t, ctx, svc, projectID, "cs-3", "in_progress")

	ok, err := svc.UnassignWorkObject(ctx, projectID.String(), obj.CanonicalID.String(), "in_progress", "ready")
	require.NoError(t, err)
	require.True(t, ok)

	cs := headChangeSummary(t, ctx, db, obj.CanonicalID.String())
	updated, ok := cs["updated"].(map[string]any)
	require.True(t, ok, "change_summary.updated must be present, got %v", cs)
	status, ok := updated["/status"].(map[string]any)
	require.True(t, ok, "status move must be recorded, got %v", updated)
	require.Equal(t, "ready", status["to"])
	assignee, ok := updated["/assignee"].(map[string]any)
	require.True(t, ok, "the summary must not hide the assignee clear, got %v", updated)
	require.Equal(t, "researcher", assignee["from"])
	require.Nil(t, assignee["to"])
}
