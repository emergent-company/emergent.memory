package agents

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// enableBoardType installs a project schema pack declaring the given object
// type board-enabled, so the board projection (ListWorkItems / GetWorkItemDetail)
// returns objects of that type. Mirrors the graph package's own board-projection
// fixture setup.
func enableBoardType(t *testing.T, ctx context.Context, db *bun.DB, projectID, typeName string) {
	t.Helper()
	schemaID := uuid.NewString()
	_, err := db.NewRaw(`
		INSERT INTO kb.graph_schemas (id, name, version, object_type_schemas, relationship_type_schemas, created_at, updated_at)
		VALUES (?::uuid, 'board-pack', '1.0.0', ?::jsonb, '{}'::jsonb, NOW(), NOW())
	`, schemaID, `[{"name":"`+typeName+`","boardEnabled":true}]`).Exec(ctx)
	require.NoError(t, err)

	assignmentID := uuid.NewString()
	_, err = db.NewRaw(`
		INSERT INTO kb.project_schemas (id, project_id, schema_id, active, installed_at, created_at, updated_at)
		VALUES (?::uuid, ?::uuid, ?::uuid, true, NOW(), NOW(), NOW())
	`, assignmentID, projectID, schemaID).Exec(ctx)
	require.NoError(t, err)
}

// --- store: append + list ordering ---

// TestWorkCommentsAppendAndList verifies the append-only store round-trip: two
// comments land with their author/actor_type/run_id columns and are listed in
// ascending created_at order (with nil authors tolerated).
func TestWorkCommentsAppendAndList(t *testing.T) {
	ctx, _, repo, projectID := setupWorkObjectDispatch(t)
	canonicalID := uuid.NewString()
	runID := uuid.NewString()

	first, err := repo.AppendWorkComment(ctx, projectID, canonicalID, "alice", "first note", "user", nil)
	require.NoError(t, err)
	require.NotEmpty(t, first.ID)
	require.NotNil(t, first.Author)
	require.Equal(t, "alice", *first.Author)
	require.NotNil(t, first.ActorType)
	require.Equal(t, "user", *first.ActorType)
	require.Nil(t, first.RunID)

	// Sleep a hair so the default now() ordering is deterministic.
	time.Sleep(5 * time.Millisecond)
	_, err = repo.AppendWorkComment(ctx, projectID, canonicalID, "researcher", "second note", "agent", &runID)
	require.NoError(t, err)

	out, err := repo.ListWorkComments(ctx, projectID, canonicalID)
	require.NoError(t, err)
	require.Len(t, out, 2)
	require.Equal(t, "first note", out[0].Body)
	require.Equal(t, "second note", out[1].Body)
	require.Equal(t, "agent", *out[1].ActorType)
	require.NotNil(t, out[1].RunID)
	require.Equal(t, runID, *out[1].RunID)
}

// TestWorkCommentsList_ScopedByItem verifies comments are keyed by
// (project_id, canonical_id): a different item — and, critically, an object with
// the same canonical id in a *different project* — sees no comments.
func TestWorkCommentsList_ScopedByItem(t *testing.T) {
	ctx, db, repo, projectID := setupWorkObjectDispatch(t)
	canonicalID := uuid.NewString()

	_, err := repo.AppendWorkComment(ctx, projectID, canonicalID, "alice", "note", "user", nil)
	require.NoError(t, err)

	out, err := repo.ListWorkComments(ctx, projectID, uuid.NewString())
	require.NoError(t, err)
	require.Empty(t, out)

	// A second project carrying an object with the SAME canonical id must not see
	// the first project's comments: the store scopes by (project_id, canonical_id),
	// not by canonical_id alone.
	_, otherProjectID := insertOrgAndProject(t, db, ctx)
	crossProject, err := repo.ListWorkComments(ctx, otherProjectID, canonicalID)
	require.NoError(t, err)
	require.Empty(t, crossProject, "an object with the same canonical id in another project must not see these comments")
}

// --- service: human comment path ---

// TestWorkActionsAddComment verifies the human comment path records a comment
// attributed to the authoring user (actor_type='user', no run id) without
// transitioning the item.
func TestWorkActionsAddComment(t *testing.T) {
	ctx, _, repo, gSvc, projectID := setupWorkActions(t)
	canonicalID := createWorkObject(t, ctx, gSvc, projectID, "k-comment-1", "in_progress", "researcher")

	svc := newWorkActions(repo, gSvc)
	res, err := svc.AddComment(ctx, projectID, canonicalID, "user-123", "a reviewer note")
	require.NoError(t, err)
	require.NotNil(t, res.Head)
	require.Equal(t, "in_progress", res.Head.Status)

	comments, err := repo.ListWorkComments(ctx, projectID, canonicalID)
	require.NoError(t, err)
	require.Len(t, comments, 1)
	require.Equal(t, "a reviewer note", comments[0].Body)
	require.NotNil(t, comments[0].Author)
	require.Equal(t, "user-123", *comments[0].Author)
	require.NotNil(t, comments[0].ActorType)
	require.Equal(t, "user", *comments[0].ActorType)
	require.Nil(t, comments[0].RunID)
}

// TestWorkActionsAddComment_EmptyRejected verifies an empty comment is rejected
// without recording anything.
func TestWorkActionsAddComment_EmptyRejected(t *testing.T) {
	ctx, _, repo, gSvc, projectID := setupWorkActions(t)
	canonicalID := createWorkObject(t, ctx, gSvc, projectID, "k-comment-2", "in_progress", "researcher")

	_, err := newWorkActions(repo, gSvc).AddComment(ctx, projectID, canonicalID, "user-123", "")
	require.Error(t, err)

	comments, lerr := repo.ListWorkComments(ctx, projectID, canonicalID)
	require.NoError(t, lerr)
	require.Empty(t, comments)
}

// TestWorkActionsAddComment_MissingItemNotFound verifies a comment on a missing
// item 404s without recording.
func TestWorkActionsAddComment_MissingItemNotFound(t *testing.T) {
	ctx, _, repo, gSvc, projectID := setupWorkActions(t)

	_, err := newWorkActions(repo, gSvc).AddComment(ctx, projectID, uuid.NewString(), "user-123", "note")
	require.Error(t, err)

	comments, lerr := repo.ListWorkComments(ctx, projectID, uuid.NewString())
	require.NoError(t, lerr)
	require.Empty(t, comments)
}

// TestWorkActionsGetWorkItemDetail_Comments verifies the drawer projection
// carries durable comments alongside feedback and runs.
func TestWorkActionsGetWorkItemDetail_Comments(t *testing.T) {
	ctx, db, repo, gSvc, projectID := setupWorkActions(t)
	// GetWorkItemDetail resolves the item through the board projection
	// (ListWorkItems), which only returns board-enabled types — install a schema
	// pack declaring ResearchRequest board-enabled so the created object is
	// visible.
	enableBoardType(t, ctx, db, projectID, "ResearchRequest")
	canonicalID := createWorkObject(t, ctx, gSvc, projectID, "k-comment-3", "in_progress", "researcher")

	svc := newWorkActions(repo, gSvc)
	_, err := svc.AddComment(ctx, projectID, canonicalID, "user-123", "a reviewer note")
	require.NoError(t, err)

	detail, err := svc.GetWorkItemDetail(ctx, projectID, canonicalID)
	require.NoError(t, err)
	require.NotNil(t, detail.Item)
	require.Len(t, detail.Comments, 1)
	require.Equal(t, "a reviewer note", detail.Comments[0].Body)
}

// --- tool: construction + handler ---

func TestBuildWorkCommentTool(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	deps := WorkToolDeps{
		Logger:      log,
		ProjectID:   "p-1",
		CanonicalID: uuid.NewString(),
	}
	tool, err := BuildWorkCommentTool(deps)
	require.NoError(t, err)
	require.NotNil(t, tool)
	require.Equal(t, ToolNameWorkComment, tool.Name())
}

// TestCommentWork_AppendsAgentComment verifies the comment tool body appends a
// durable comment attributed to the agent (actor_type='agent', run id) without
// touching the terminator.
func TestCommentWork_AppendsAgentComment(t *testing.T) {
	ctx, _, repo, projectID := setupWorkObjectDispatch(t)
	canonicalID := uuid.NewString()
	runID := uuid.NewString()

	deps := WorkToolDeps{
		Repo:        repo,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		ProjectID:   projectID,
		CanonicalID: canonicalID,
		ActorName:   "researcher",
		RunID:       runID,
		Terminator:  &WorkTerminatorState{},
	}

	res, err := commentWork(ctx, deps, map[string]any{"body": "agent context note"})
	require.NoError(t, err)
	require.Equal(t, "recorded", res["status"])
	require.False(t, deps.Terminator.ShouldFinalize())

	comments, err := repo.ListWorkComments(ctx, projectID, canonicalID)
	require.NoError(t, err)
	require.Len(t, comments, 1)
	require.Equal(t, "agent context note", comments[0].Body)
	require.NotNil(t, comments[0].Author)
	require.Equal(t, "researcher", *comments[0].Author)
	require.NotNil(t, comments[0].ActorType)
	require.Equal(t, "agent", *comments[0].ActorType)
	require.NotNil(t, comments[0].RunID)
	require.Equal(t, runID, *comments[0].RunID)
}

// TestCommentWork_EmptyBodyRejected verifies an empty body is rejected without a
// repository write.
func TestCommentWork_EmptyBodyRejected(t *testing.T) {
	deps := WorkToolDeps{
		Repo:        &Repository{}, // non-nil; must not be reached for empty body
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		CanonicalID: uuid.NewString(),
	}
	res, err := commentWork(context.Background(), deps, map[string]any{"body": ""})
	require.NoError(t, err)
	require.Equal(t, "body is required", res["error"])
}

// TestCommentWork_NoSubject verifies a missing subject is reported without a
// repository write.
func TestCommentWork_NoSubject(t *testing.T) {
	deps := WorkToolDeps{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	res, err := commentWork(context.Background(), deps, map[string]any{"body": "note"})
	require.NoError(t, err)
	require.Equal(t, "comment: no subject work object to comment on", res["error"])
}

// TestCommentWork_NoRepo verifies a nil repository is reported.
func TestCommentWork_NoRepo(t *testing.T) {
	deps := WorkToolDeps{
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		CanonicalID: uuid.NewString(),
	}
	res, err := commentWork(context.Background(), deps, map[string]any{"body": "note"})
	require.NoError(t, err)
	require.Equal(t, "comment: repository unavailable", res["error"])
}
