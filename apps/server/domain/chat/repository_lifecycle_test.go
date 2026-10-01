package chat_test

import (
	"context"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/chat"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// repoEnv wires a chat Repository + Service against a throwaway test database
// with a project and two users (owner + foreign), so repository/service lifecycle
// behaviour can be asserted without the full HTTP server.
type repoEnv struct {
	t         *testing.T
	ctx       context.Context
	db        *testutil.TestDB
	repo      *chat.Repository
	svc       *chat.Service
	projectID string
	ownerID   string
	foreignID string
}

func newRepoEnv(t *testing.T) *repoEnv {
	t.Helper()
	ctx := context.Background()
	db := testutil.SetupTestDBOrFail(t, ctx, "chat_repo_lifecycle")
	t.Cleanup(func() { db.Close() })

	orgID := uuid.New().String()
	projectID := uuid.New().String()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db.DB, orgID, "lifecycle-org"))
	require.NoError(t, testutil.CreateTestProject(ctx, db.DB, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "lifecycle-project",
	}, testutil.AdminUser.ID))

	require.NoError(t, testutil.CreateTestUser(ctx, db.DB, testutil.AdminUser))

	foreignID := "00000000-0000-0000-0000-00000000beef"
	require.NoError(t, testutil.CreateTestUser(ctx, db.DB, testutil.TestUser{
		ID:            foreignID,
		ZitadelUserID: "lifecycle-foreign",
		Email:         "lifecycle-foreign@test.local",
	}))

	repo := chat.NewRepository(db.DB, slog.Default())
	return &repoEnv{
		t:         t,
		ctx:       ctx,
		db:        db,
		repo:      repo,
		svc:       chat.NewService(repo, slog.Default()),
		projectID: projectID,
		ownerID:   testutil.AdminUser.ID,
		foreignID: foreignID,
	}
}

func (e *repoEnv) insertConversation(ownerID string, isPrivate bool, title string, updatedAt time.Time) uuid.UUID {
	e.t.Helper()
	id := uuid.New()
	_, err := e.db.DB.NewRaw(`
		INSERT INTO kb.chat_conversations (id, title, project_id, is_private, owner_user_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, id, title, e.projectID, isPrivate, ownerID, updatedAt, updatedAt).Exec(e.ctx)
	require.NoError(e.t, err)
	return id
}

func (e *repoEnv) insertMessage(convID uuid.UUID, content string) {
	e.t.Helper()
	_, err := e.db.DB.NewRaw(`
		INSERT INTO kb.chat_messages (id, conversation_id, role, content, created_at)
		VALUES (?, ?, 'user', ?, NOW())
	`, uuid.New(), convID, content).Exec(e.ctx)
	require.NoError(e.t, err)
}

func (e *repoEnv) getByID(convID uuid.UUID) *chat.Conversation {
	e.t.Helper()
	conv, err := e.repo.GetByID(e.ctx, e.projectID, e.ownerID, convID)
	require.NoError(e.t, err)
	return conv
}

func (e *repoEnv) countMessages(convID uuid.UUID) int {
	e.t.Helper()
	var n int
	err := e.db.DB.NewRaw(`SELECT COUNT(*) FROM kb.chat_messages WHERE conversation_id = ?`, convID).Scan(e.ctx, &n)
	require.NoError(e.t, err)
	return n
}

func mustNotFound(t *testing.T, err error) {
	t.Helper()
	appErr, ok := err.(*apperror.Error)
	require.Truef(t, ok, "expected *apperror.Error, got %T: %v", err, err)
	require.Equalf(t, http.StatusNotFound, appErr.HTTPStatus, "expected 404, got %d: %v", appErr.HTTPStatus, err)
}

// TestSetArchivedLifecycle covers the repository archive accessor (task 2.1):
// owner archive/unarchive succeed, both are idempotent, and foreign/unknown ids
// return no match.
func TestSetArchivedLifecycle(t *testing.T) {
	e := newRepoEnv(t)

	convID := e.insertConversation(e.ownerID, true, "owner-private", time.Now())

	matched, err := e.repo.SetArchived(e.ctx, e.projectID, e.ownerID, convID, true)
	require.NoError(t, err)
	require.True(t, matched, "owner archive must match a row")

	conv := e.getByID(convID)
	require.True(t, conv.IsArchived, "archived state must be set")
	require.NotNil(t, conv.ArchivedAt, "archived_at must be set on archive")

	// Archive again — idempotent, still matches.
	matched, err = e.repo.SetArchived(e.ctx, e.projectID, e.ownerID, convID, true)
	require.NoError(t, err)
	require.True(t, matched, "re-archiving must still match (idempotent)")

	// Unarchive clears both columns.
	matched, err = e.repo.SetArchived(e.ctx, e.projectID, e.ownerID, convID, false)
	require.NoError(t, err)
	require.True(t, matched)

	conv = e.getByID(convID)
	require.False(t, conv.IsArchived)
	require.Nil(t, conv.ArchivedAt)

	// Unarchive again — idempotent.
	matched, err = e.repo.SetArchived(e.ctx, e.projectID, e.ownerID, convID, false)
	require.NoError(t, err)
	require.True(t, matched, "re-unarchiving must still match (idempotent)")

	// Foreign private id → no match.
	foreignConv := e.insertConversation(e.foreignID, true, "foreign-private", time.Now())
	matched, err = e.repo.SetArchived(e.ctx, e.projectID, e.ownerID, foreignConv, true)
	require.NoError(t, err)
	require.False(t, matched, "foreign private conversation must not match")

	// Unknown id → no match.
	matched, err = e.repo.SetArchived(e.ctx, e.projectID, e.ownerID, uuid.New(), true)
	require.NoError(t, err)
	require.False(t, matched, "unknown id must not match")
}

// TestServiceArchiveUnarchive covers task 3.1: service methods succeed, are
// idempotent, and propagate not-found for inaccessible/unknown ids.
func TestServiceArchiveUnarchive(t *testing.T) {
	e := newRepoEnv(t)

	convID := e.insertConversation(e.ownerID, true, "svc-owner", time.Now())

	require.NoError(t, e.svc.ArchiveConversation(e.ctx, e.projectID, e.ownerID, convID))
	require.NoError(t, e.svc.ArchiveConversation(e.ctx, e.projectID, e.ownerID, convID), "archive must be idempotent")

	conv := e.getByID(convID)
	require.True(t, conv.IsArchived)

	require.NoError(t, e.svc.UnarchiveConversation(e.ctx, e.projectID, e.ownerID, convID))
	require.NoError(t, e.svc.UnarchiveConversation(e.ctx, e.projectID, e.ownerID, convID), "unarchive must be idempotent")

	conv = e.getByID(convID)
	require.False(t, conv.IsArchived)

	// Foreign private id → not-found.
	foreignConv := e.insertConversation(e.foreignID, true, "svc-foreign", time.Now())
	mustNotFound(t, e.svc.ArchiveConversation(e.ctx, e.projectID, e.ownerID, foreignConv))
	mustNotFound(t, e.svc.UnarchiveConversation(e.ctx, e.projectID, e.ownerID, foreignConv))

	// Unknown id → not-found.
	mustNotFound(t, e.svc.ArchiveConversation(e.ctx, e.projectID, e.ownerID, uuid.New()))
}

// TestListConversationsArchiveFilter covers task 2.2: default list excludes
// archived, include-archived returns both, ordering is updated_at DESC in both
// cases, and Total reflects the filtered set.
func TestListConversationsArchiveFilter(t *testing.T) {
	e := newRepoEnv(t)

	base := time.Now().UTC()
	activeOld := e.insertConversation(e.ownerID, true, "active-old", base.Add(-2*time.Hour))
	activeNew := e.insertConversation(e.ownerID, true, "active-new", base.Add(-1*time.Hour))

	archivedMid := e.insertConversation(e.ownerID, true, "archived-mid", base.Add(-30*time.Minute))
	_, err := e.repo.SetArchived(e.ctx, e.projectID, e.ownerID, archivedMid, true)
	require.NoError(t, err)
	archivedNewest := e.insertConversation(e.ownerID, true, "archived-newest", base)
	_, err = e.repo.SetArchived(e.ctx, e.projectID, e.ownerID, archivedNewest, true)
	require.NoError(t, err)

	// Default list: active only, updated_at DESC, filtered Total.
	res, err := e.repo.ListConversations(e.ctx, chat.ListConversationsParams{
		ProjectID:   e.projectID,
		OwnerUserID: &e.ownerID,
		Limit:       100,
	})
	require.NoError(t, err)
	require.Equal(t, 2, res.Total, "Total must be the filtered (active-only) count")
	require.Len(t, res.Conversations, 2)
	require.Equal(t, activeNew, res.Conversations[0].ID, "most-recently-updated active first")
	require.Equal(t, activeOld, res.Conversations[1].ID)

	// Include archived: all four, updated_at DESC across both sets.
	res, err = e.repo.ListConversations(e.ctx, chat.ListConversationsParams{
		ProjectID:       e.projectID,
		OwnerUserID:     &e.ownerID,
		Limit:           100,
		IncludeArchived: true,
	})
	require.NoError(t, err)
	require.Equal(t, 4, res.Total, "Total must be the full count when including archived")
	require.Len(t, res.Conversations, 4)
	require.Equal(t, archivedNewest, res.Conversations[0].ID)
	require.Equal(t, archivedMid, res.Conversations[1].ID)
	require.Equal(t, activeNew, res.Conversations[2].ID)
	require.Equal(t, activeOld, res.Conversations[3].ID)
}

// TestArchivedConversationStillReadable covers task 2.3: an archived
// conversation remains fully retrievable by id and its history is intact (D5).
func TestArchivedConversationStillReadable(t *testing.T) {
	e := newRepoEnv(t)

	convID := e.insertConversation(e.ownerID, true, "archived-readable", time.Now())
	e.insertMessage(convID, "hello archive")

	_, err := e.repo.SetArchived(e.ctx, e.projectID, e.ownerID, convID, true)
	require.NoError(t, err)

	conv := e.getByID(convID)
	require.NotNil(t, conv, "archived conversation must still be returned by id")
	require.True(t, conv.IsArchived)

	convWith, err := e.repo.GetByIDWithMessages(e.ctx, e.projectID, e.ownerID, convID)
	require.NoError(t, err)
	require.NotNil(t, convWith)
	require.Len(t, convWith.Messages, 1, "archived conversation must keep its messages")

	hist, err := e.repo.GetConversationHistory(e.ctx, e.projectID, e.ownerID, convID, 10)
	require.NoError(t, err)
	require.Len(t, hist, 1, "archived conversation history must still be readable")
	require.Equal(t, "hello archive", hist[0].Content)
}

// TestDeletePredicateAndCascade covers task 2.4: owner deletes their own, a
// non-owner member deletes a non-private conversation, a foreign private id is
// not deleted, and messages are removed with the conversation.
func TestDeletePredicateAndCascade(t *testing.T) {
	e := newRepoEnv(t)

	// Owner deletes their private conversation; messages removed.
	ownConv := e.insertConversation(e.ownerID, true, "own-delete", time.Now())
	e.insertMessage(ownConv, "own msg")
	deleted, err := e.repo.Delete(e.ctx, e.projectID, e.ownerID, ownConv)
	require.NoError(t, err)
	require.True(t, deleted)
	require.Nil(t, e.getByID(ownConv), "deleted conversation must be gone")
	require.Equal(t, 0, e.countMessages(ownConv), "messages must be removed with the conversation")

	// Non-owner member deletes a non-private (shared) conversation.
	sharedConv := e.insertConversation(e.ownerID, false, "shared-delete", time.Now())
	e.insertMessage(sharedConv, "shared msg")
	deleted, err = e.repo.Delete(e.ctx, e.projectID, e.foreignID, sharedConv)
	require.NoError(t, err)
	require.True(t, deleted, "non-owner member must be able to delete a non-private conversation")
	require.Nil(t, e.getByID(sharedConv))
	require.Equal(t, 0, e.countMessages(sharedConv))

	// Foreign private id → not deleted.
	privateConv := e.insertConversation(e.ownerID, true, "private-keep", time.Now())
	deleted, err = e.repo.Delete(e.ctx, e.projectID, e.foreignID, privateConv)
	require.NoError(t, err)
	require.False(t, deleted, "foreign private conversation must not be deletable")
	require.NotNil(t, e.getByID(privateConv))

	// Unknown id → no match.
	deleted, err = e.repo.Delete(e.ctx, e.projectID, e.ownerID, uuid.New())
	require.NoError(t, err)
	require.False(t, deleted)
}
