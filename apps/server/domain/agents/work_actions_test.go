package agents

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// setupWorkActions returns a real agents Repository and graph Service wired to
// the same throwaway DB, plus the project ID, for the human-action tests.
func setupWorkActions(t *testing.T) (context.Context, *bun.DB, *Repository, *graph.Service, string) {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "integration test requires database")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "agents_work_actions")
	t.Cleanup(tdb.Close)
	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{}
	cfg.Graph.MaxListLimit = 100_000
	gRepo := graph.NewRepository(tdb.DB, log, cfg)
	gSvc := graph.NewService(gRepo, log, nil, nil, nil, nil, nil, graph.NoopEventSink{}, nil, nil)

	return ctx, tdb.DB, NewRepository(tdb.DB), gSvc, projectID
}

// newWorkActions builds the human-action service with a real graph store.
func newWorkActions(repo *Repository, gSvc *graph.Service) *WorkActionService {
	s := NewWorkActionService(repo, testAgentLogger())
	s.SetWorkObjectStore(gSvc)
	return s
}

// createWorkObject inserts a version-1 work object with a key and assignee and
// returns its canonical ID.
func createWorkObject(t *testing.T, ctx context.Context, gSvc *graph.Service, projectID, key, status, assignee string) string {
	t.Helper()
	obj, err := gSvc.Create(ctx, uuid.MustParse(projectID), &graph.CreateGraphObjectRequest{
		Type:       "ResearchRequest",
		Key:        &key,
		Status:     &status,
		Assignee:   &assignee,
		Properties: map[string]any{"topic": "new Gemini model"},
	}, nil)
	require.NoError(t, err)
	return obj.CanonicalID.String()
}

func workHeadStatus(t *testing.T, ctx context.Context, gSvc *graph.Service, projectID, canonicalID string) string {
	t.Helper()
	head, err := gSvc.GetHeadObject(ctx, projectID, canonicalID)
	require.NoError(t, err)
	require.NotNil(t, head)
	return head.Status
}

func countWorkFeedback(t *testing.T, ctx context.Context, repo *Repository, projectID, canonicalID string) int {
	t.Helper()
	fb, err := repo.ListWorkFeedback(ctx, projectID, canonicalID)
	require.NoError(t, err)
	return len(fb)
}

func countQueuedSubjectRuns(t *testing.T, ctx context.Context, repo *Repository, canonicalID string) int {
	t.Helper()
	var n int
	require.NoError(t, repo.db.NewRaw(
		`SELECT COUNT(*) FROM kb.agent_runs WHERE subject_object_id = ?::uuid AND status = ?`,
		canonicalID, string(RunStatusQueued),
	).Scan(ctx, &n))
	return n
}

// TestWorkActionsApprove verifies the approve write path: review→done,
// reviewed_by/at set, needs_review cleared.
func TestWorkActionsApprove(t *testing.T) {
	ctx, db, repo, gSvc, projectID := setupWorkActions(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "researcher", `{}`)
	insertWorkAgent(t, db, ctx, projectID, "researcher", defID)

	canonicalID := createWorkObject(t, ctx, gSvc, projectID, "k-1", "review", "researcher")
	require.Equal(t, "review", workHeadStatus(t, ctx, gSvc, projectID, canonicalID))

	reviewer := uuid.NewString()
	res, err := newWorkActions(repo, gSvc).Approve(ctx, projectID, canonicalID, reviewer)
	require.NoError(t, err)
	require.NotNil(t, res.Head)
	require.Equal(t, "done", res.Head.Status)

	var needsReview bool
	var reviewedBy string
	var reviewedAt any
	require.NoError(t, repo.db.NewRaw(
		`SELECT needs_review, reviewed_by::text, reviewed_at FROM kb.graph_objects WHERE canonical_id = ?::uuid AND supersedes_id IS NULL`,
		canonicalID,
	).Scan(ctx, &needsReview, &reviewedBy, &reviewedAt))
	require.False(t, needsReview)
	require.Equal(t, reviewer, reviewedBy)
	require.NotNil(t, reviewedAt)
}

// TestWorkActionsApprove_NotReviewRejected verifies a non-review item cannot be
// approved.
func TestWorkActionsApprove_NotReviewRejected(t *testing.T) {
	ctx, _, repo, gSvc, projectID := setupWorkActions(t)
	canonicalID := createWorkObject(t, ctx, gSvc, projectID, "k-2", "in_progress", "researcher")

	_, err := newWorkActions(repo, gSvc).Approve(ctx, projectID, canonicalID, uuid.NewString())
	require.Error(t, err)
	require.Equal(t, "in_progress", workHeadStatus(t, ctx, gSvc, projectID, canonicalID))
}

// TestWorkActionsRequestChanges verifies request-changes records feedback,
// transitions review→revision, and enqueues a rework run carrying the feedback.
func TestWorkActionsRequestChanges(t *testing.T) {
	ctx, db, repo, gSvc, projectID := setupWorkActions(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "researcher", `{"requiresReview":true}`)
	insertWorkAgent(t, db, ctx, projectID, "researcher", defID)

	canonicalID := createWorkObject(t, ctx, gSvc, projectID, "k-3", "review", "researcher")

	res, err := newWorkActions(repo, gSvc).RequestChanges(ctx, projectID, canonicalID, "reviewer-1", "please fix the summary", nil)
	require.NoError(t, err)
	require.Equal(t, "revision", res.Head.Status)
	require.Equal(t, 1, res.Round)
	require.False(t, res.Escalated)
	require.NotEmpty(t, res.RunID)

	require.Equal(t, 1, countWorkFeedback(t, ctx, repo, projectID, canonicalID))
	require.Equal(t, 1, countQueuedSubjectRuns(t, ctx, repo, canonicalID))

	// The rework run carries the feedback history in its trigger metadata.
	var meta string
	require.NoError(t, repo.db.NewRaw(
		`SELECT trigger_metadata->>'work_feedback' FROM kb.agent_runs WHERE subject_object_id = ?::uuid AND status = ? LIMIT 1`,
		canonicalID, string(RunStatusQueued),
	).Scan(ctx, &meta))
	require.Contains(t, meta, "please fix the summary")
}

// TestWorkActionsRequestChanges_EmptyFeedbackRejected verifies empty feedback is
// rejected without transitioning or recording.
func TestWorkActionsRequestChanges_EmptyFeedbackRejected(t *testing.T) {
	ctx, db, repo, gSvc, projectID := setupWorkActions(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "researcher", `{}`)
	insertWorkAgent(t, db, ctx, projectID, "researcher", defID)

	canonicalID := createWorkObject(t, ctx, gSvc, projectID, "k-4", "review", "researcher")

	_, err := newWorkActions(repo, gSvc).RequestChanges(ctx, projectID, canonicalID, "reviewer-1", "", nil)
	require.Error(t, err)
	require.Equal(t, "review", workHeadStatus(t, ctx, gSvc, projectID, canonicalID))
	require.Equal(t, 0, countWorkFeedback(t, ctx, repo, projectID, canonicalID))
}

// TestWorkActionsRequestChanges_RevisionCapEscalates verifies the revision cap
// escalates (task, no rework re-enqueue) instead of re-running.
func TestWorkActionsRequestChanges_RevisionCapEscalates(t *testing.T) {
	ctx, db, repo, gSvc, projectID := setupWorkActions(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "researcher", `{"revisionLimit":1}`)
	insertWorkAgent(t, db, ctx, projectID, "researcher", defID)

	canonicalID := createWorkObject(t, ctx, gSvc, projectID, "k-5", "review", "researcher")

	res, err := newWorkActions(repo, gSvc).RequestChanges(ctx, projectID, canonicalID, "reviewer-1", "still wrong", nil)
	require.NoError(t, err)
	require.Equal(t, "revision", res.Head.Status)
	require.Equal(t, 1, res.Round)
	require.True(t, res.Escalated)
	require.Empty(t, res.RunID)

	require.Equal(t, 0, countQueuedSubjectRuns(t, ctx, repo, canonicalID))

	var taskCount int
	require.NoError(t, repo.db.NewRaw(
		`SELECT COUNT(*) FROM kb.tasks WHERE type = 'work-escalation' AND source_id = ?`,
		canonicalID,
	).Scan(ctx, &taskCount))
	require.Equal(t, 1, taskCount)
}

// TestWorkActionsRetry verifies retry moves blocked→ready, clears failure state,
// and enqueues.
func TestWorkActionsRetry(t *testing.T) {
	ctx, db, repo, gSvc, projectID := setupWorkActions(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "researcher", `{}`)
	insertWorkAgent(t, db, ctx, projectID, "researcher", defID)

	canonicalID := createWorkObject(t, ctx, gSvc, projectID, "k-6", "blocked", "researcher")
	// Seed a failure ledger so we can assert retry clears it.
	_, err := repo.IncrementWorkItemFailure(ctx, projectID, canonicalID, "deterministic", nil)
	require.NoError(t, err)

	res, err := newWorkActions(repo, gSvc).Retry(ctx, projectID, canonicalID)
	require.NoError(t, err)
	require.Equal(t, "ready", res.Head.Status)
	require.NotEmpty(t, res.RunID)

	st, err := repo.GetWorkItemState(ctx, projectID, canonicalID)
	require.NoError(t, err)
	require.Nil(t, st)
	require.Equal(t, 1, countQueuedSubjectRuns(t, ctx, repo, canonicalID))
}

// TestWorkActionsRetry_NotBlockedRejected verifies retry only applies to blocked
// items.
func TestWorkActionsRetry_NotBlockedRejected(t *testing.T) {
	ctx, _, repo, gSvc, projectID := setupWorkActions(t)
	canonicalID := createWorkObject(t, ctx, gSvc, projectID, "k-7", "ready", "researcher")

	_, err := newWorkActions(repo, gSvc).Retry(ctx, projectID, canonicalID)
	require.Error(t, err)
}

// TestWorkActionsReassign verifies reassign sets and clears the assignee without
// a status move.
func TestWorkActionsReassign(t *testing.T) {
	ctx, _, repo, gSvc, projectID := setupWorkActions(t)
	canonicalID := createWorkObject(t, ctx, gSvc, projectID, "k-8", "review", "researcher")

	svc := newWorkActions(repo, gSvc)
	newAssignee := "reviewer"
	res, err := svc.Reassign(ctx, projectID, canonicalID, &newAssignee)
	require.NoError(t, err)
	require.Equal(t, "review", res.Head.Status)
	require.Equal(t, "reviewer", res.Head.Assignee)

	empty := ""
	res, err = svc.Reassign(ctx, projectID, canonicalID, &empty)
	require.NoError(t, err)
	require.Equal(t, "", res.Head.Assignee)
}

// TestWorkActionsCancel verifies cancel closes the item (blocked) and cancels an
// in-flight run.
func TestWorkActionsCancel(t *testing.T) {
	ctx, db, repo, gSvc, projectID := setupWorkActions(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "researcher", `{}`)
	agentID := insertWorkAgent(t, db, ctx, projectID, "researcher", defID)

	canonicalID := createWorkObject(t, ctx, gSvc, projectID, "k-9", "in_progress", "researcher")

	objType := "ResearchRequest"
	run, err := repo.CreateRunQueued(ctx, agentID, 1, CreateRunQueuedOptions{
		SubjectObjectID:   &canonicalID,
		SubjectObjectType: &objType,
	})
	require.NoError(t, err)

	res, err := newWorkActions(repo, gSvc).Cancel(ctx, projectID, canonicalID)
	require.NoError(t, err)
	require.Equal(t, "blocked", res.Head.Status)

	var runStatus string
	require.NoError(t, repo.db.NewRaw(`SELECT status FROM kb.agent_runs WHERE id = ?`, run.ID).Scan(ctx, &runStatus))
	require.Equal(t, string(RunStatusCancelling), runStatus)
}
