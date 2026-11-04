package agents

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
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// setupWorkActions returns a real agents Repository and graph Service wired to
// the same throwaway DB, plus the project ID, for the human-action tests.
func setupWorkActions(t *testing.T) (context.Context, *bun.DB, *Repository, *graph.Service, string) {
	return setupWorkActionsWithSchemas(t, nil)
}

// setupWorkActionsWithSchemas is setupWorkActions with an optional static schema
// provider, so set-status tests can exercise board-enabled types with a declared
// allowed-status set (otherwise the graph service's schema provider is nil and
// every type is unconfigured/unconstrained).
func setupWorkActionsWithSchemas(t *testing.T, objectSchemas map[string]agents.ObjectSchema) (context.Context, *bun.DB, *Repository, *graph.Service, string) {
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
	var sp graph.SchemaProvider
	if objectSchemas != nil {
		sp = &staticSchemaProvider{schemas: objectSchemas}
	}
	gSvc := graph.NewService(gRepo, log, sp, nil, nil, nil, nil, graph.NoopEventSink{}, nil, nil)

	return ctx, tdb.DB, NewRepository(tdb.DB), gSvc, projectID
}

// staticSchemaProvider is a minimal SchemaProvider returning a fixed set of
// object schemas, so the set-status tests can exercise per-type board-enabled
// config without a real schema pack.
type staticSchemaProvider struct {
	schemas map[string]agents.ObjectSchema
}

func (p *staticSchemaProvider) GetProjectSchemas(ctx context.Context, projectID string) (*graph.ExtractionSchemas, error) {
	return &graph.ExtractionSchemas{ObjectSchemas: p.schemas, RelationshipSchemas: map[string]agents.RelationshipSchema{}}, nil
}

func (p *staticSchemaProvider) InvalidateProjectCache(projectID string) {}

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

// requireRunCarriesFeedback asserts the given run's trigger metadata carries the
// work_feedback key (the marker that makes it a rework run via isReworkRun).
func requireRunCarriesFeedback(t *testing.T, ctx context.Context, repo *Repository, runID string) {
	t.Helper()
	var n int
	require.NoError(t, repo.db.NewRaw(
		`SELECT COUNT(*) FROM kb.agent_runs WHERE id = ?::uuid AND jsonb_exists(trigger_metadata, 'work_feedback')`,
		runID,
	).Scan(ctx, &n))
	require.Equal(t, 1, n)
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

// TestWorkActionsRequestChanges verifies request-changes records feedback and
// transitions review→revision, and enqueues exactly one rework run carrying the
// feedback history (so the worker recognizes it as a rework run and sees why it
// is re-running). The review→revision transition still emits a status_changed
// event for other consumers; the explicit rework enqueue is the source of truth
// for the rework run.
func TestWorkActionsRequestChanges(t *testing.T) {
	ctx, db, repo, gSvc, projectID := setupWorkActions(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "researcher", `{"requiresReview":true}`)
	insertWorkAgent(t, db, ctx, projectID, "researcher", defID)

	canonicalID := createWorkObject(t, ctx, gSvc, projectID, "k-3", "review", "researcher")

	res, err := newWorkActions(repo, gSvc).RequestChanges(ctx, projectID, canonicalID, "reviewer-1", "please fix the summary")
	require.NoError(t, err)
	require.Equal(t, "revision", res.Head.Status)
	require.Equal(t, 1, res.Round)
	require.False(t, res.Escalated)
	require.NotEmpty(t, res.RunID)

	// Exactly one rework run is enqueued and carries the feedback history.
	require.Equal(t, 1, countQueuedSubjectRuns(t, ctx, repo, canonicalID))
	requireRunCarriesFeedback(t, ctx, repo, res.RunID)

	// The feedback round persists the id of the rework run it produced.
	fb, err := repo.ListWorkFeedback(ctx, projectID, canonicalID)
	require.NoError(t, err)
	require.Len(t, fb, 1)
	require.NotNil(t, fb[0].RunID)
	require.Equal(t, res.RunID, *fb[0].RunID)
}

// TestWorkActionsRequestChanges_EmptyFeedbackRejected verifies empty feedback is
// rejected without transitioning or recording.
func TestWorkActionsRequestChanges_EmptyFeedbackRejected(t *testing.T) {
	ctx, db, repo, gSvc, projectID := setupWorkActions(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "researcher", `{}`)
	insertWorkAgent(t, db, ctx, projectID, "researcher", defID)

	canonicalID := createWorkObject(t, ctx, gSvc, projectID, "k-4", "review", "researcher")

	_, err := newWorkActions(repo, gSvc).RequestChanges(ctx, projectID, canonicalID, "reviewer-1", "")
	require.Error(t, err)
	require.Equal(t, "review", workHeadStatus(t, ctx, gSvc, projectID, canonicalID))
	require.Equal(t, 0, countWorkFeedback(t, ctx, repo, projectID, canonicalID))
}

// TestHasActiveRunForAgentObject_FindsQueuedWorkRun verifies the skip-concurrency
// checker matches object-driven queued runs: those record the subject object on
// the subject_object_id column (not on trigger_metadata, which only the inline
// reaction path stamps), so ConcurrencyStrategy=skip must find them there.
func TestHasActiveRunForAgentObject_FindsQueuedWorkRun(t *testing.T) {
	ctx, db, repo, gSvc, projectID := setupWorkActions(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "researcher", `{}`)
	insertWorkAgent(t, db, ctx, projectID, "researcher", defID)

	canonicalID := createWorkObject(t, ctx, gSvc, projectID, "k-hasactive", "ready", "researcher")
	svc := newWorkActions(repo, gSvc)
	head, agent, def, err := svc.resolveWorkContext(ctx, projectID, canonicalID)
	require.NoError(t, err)
	require.NotNil(t, agent)

	runID, err := svc.enqueueWorkRun(ctx, head, agent, def, nil)
	require.NoError(t, err)
	require.NotEmpty(t, runID)

	active, err := repo.HasActiveRunForAgentObject(ctx, agent.ID, canonicalID, head.Type)
	require.NoError(t, err)
	require.True(t, active)

	// A different object is not active for the same agent.
	active, err = repo.HasActiveRunForAgentObject(ctx, agent.ID, uuid.NewString(), head.Type)
	require.NoError(t, err)
	require.False(t, active)
}

// TestWorkActionsRequestChanges_RevisionCapEscalates verifies the revision cap
// escalates (task, no rework re-enqueue) instead of re-running.
func TestWorkActionsRequestChanges_RevisionCapEscalates(t *testing.T) {
	ctx, db, repo, gSvc, projectID := setupWorkActions(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "researcher", `{"revisionLimit":1}`)
	insertWorkAgent(t, db, ctx, projectID, "researcher", defID)

	canonicalID := createWorkObject(t, ctx, gSvc, projectID, "k-5", "review", "researcher")

	res, err := newWorkActions(repo, gSvc).RequestChanges(ctx, projectID, canonicalID, "reviewer-1", "still wrong")
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

// boardAllowedStatuses is the allowed-status set used by the board-enabled
// set-status tests (matches the flagship task-board blueprint).
func boardAllowedStatuses() []string {
	return []string{"ready", "in_progress", "review", "revision", "blocked", "done"}
}

// boardSchemas returns a static schema provider map marking ResearchRequest as
// board-enabled with a declared allowed-status set.
func boardSchemas() map[string]agents.ObjectSchema {
	return map[string]agents.ObjectSchema{
		"ResearchRequest": {ObjectTypeWorkConfig: agents.ObjectTypeWorkConfig{
			BoardEnabled:    true,
			AllowedStatuses: boardAllowedStatuses(),
		}},
	}
}

// TestWorkActionsSetStatus verifies the set-status action moves an item to an
// exact status without enqueuing a run.
func TestWorkActionsSetStatus(t *testing.T) {
	ctx, _, repo, gSvc, projectID := setupWorkActions(t)

	canonicalID := createWorkObject(t, ctx, gSvc, projectID, "k-setstatus", "in_progress", "researcher")

	res, err := newWorkActions(repo, gSvc).SetStatus(ctx, projectID, canonicalID, "review")
	require.NoError(t, err)
	require.Equal(t, "review", res.Head.Status)

	// No run is enqueued by a plain status move.
	require.Equal(t, 0, countQueuedSubjectRuns(t, ctx, repo, canonicalID))
}

// TestWorkActionsSetStatus_EmptyRejected verifies an empty status is rejected.
func TestWorkActionsSetStatus_EmptyRejected(t *testing.T) {
	ctx, _, repo, gSvc, projectID := setupWorkActions(t)

	canonicalID := createWorkObject(t, ctx, gSvc, projectID, "k-setstatus-empty", "in_progress", "researcher")

	_, err := newWorkActions(repo, gSvc).SetStatus(ctx, projectID, canonicalID, "")
	require.Error(t, err)
	require.Equal(t, "in_progress", workHeadStatus(t, ctx, gSvc, projectID, canonicalID))
}

// TestWorkActionsSetStatus_DisallowedRejected verifies a status outside a
// board-enabled type's allowed set is rejected and leaves the item unchanged.
func TestWorkActionsSetStatus_DisallowedRejected(t *testing.T) {
	ctx, _, repo, gSvc, projectID := setupWorkActionsWithSchemas(t, boardSchemas())

	canonicalID := createWorkObject(t, ctx, gSvc, projectID, "k-setstatus-disallowed", "in_progress", "researcher")

	_, err := newWorkActions(repo, gSvc).SetStatus(ctx, projectID, canonicalID, "shipped")
	require.Error(t, err)
	require.Equal(t, "in_progress", workHeadStatus(t, ctx, gSvc, projectID, canonicalID))
}

// TestWorkActionsSetStatus_AgentActorRejected verifies an agent-originated
// status write on a board-enabled item is rejected by the graph guard.
func TestWorkActionsSetStatus_AgentActorRejected(t *testing.T) {
	ctx, _, repo, gSvc, projectID := setupWorkActionsWithSchemas(t, boardSchemas())

	canonicalID := createWorkObject(t, ctx, gSvc, projectID, "k-setstatus-agent", "in_progress", "researcher")

	agentID := uuid.New()
	agentCtx := auth.WithActor(ctx, graph.ActorAgent, &agentID)

	_, err := newWorkActions(repo, gSvc).SetStatus(agentCtx, projectID, canonicalID, "review")
	require.Error(t, err)
	require.Equal(t, "in_progress", workHeadStatus(t, ctx, gSvc, projectID, canonicalID))
}

// TestWorkActionsSetStatus_NeedsReviewOnReview verifies setting the review
// status sets needs_review, and a non-review status clears it.
func TestWorkActionsSetStatus_NeedsReviewOnReview(t *testing.T) {
	ctx, _, repo, gSvc, projectID := setupWorkActions(t)

	canonicalID := createWorkObject(t, ctx, gSvc, projectID, "k-setstatus-review", "in_progress", "researcher")

	_, err := newWorkActions(repo, gSvc).SetStatus(ctx, projectID, canonicalID, "review")
	require.NoError(t, err)

	var needsReview bool
	require.NoError(t, repo.db.NewRaw(
		`SELECT needs_review FROM kb.graph_objects WHERE canonical_id = ?::uuid AND supersedes_id IS NULL`,
		canonicalID,
	).Scan(ctx, &needsReview))
	require.True(t, needsReview)

	// Moving it off review clears needs_review.
	_, err = newWorkActions(repo, gSvc).SetStatus(ctx, projectID, canonicalID, "done")
	require.NoError(t, err)
	require.NoError(t, repo.db.NewRaw(
		`SELECT needs_review FROM kb.graph_objects WHERE canonical_id = ?::uuid AND supersedes_id IS NULL`,
		canonicalID,
	).Scan(ctx, &needsReview))
	require.False(t, needsReview)
}
