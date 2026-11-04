package chat_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/chat"
)

// insertChild inserts a conversation with a parent link, bypassing the service
// so the repository/list/cascade behaviour can be tested without the loopback
// path that normally sets it.
func (e *repoEnv) insertChild(parentID uuid.UUID, ownerID string, isPrivate bool, title string, updatedAt time.Time) uuid.UUID {
	e.t.Helper()
	id := uuid.New()
	_, err := e.db.DB.NewRaw(`
		INSERT INTO kb.chat_conversations
			(id, title, project_id, is_private, owner_user_id, parent_conversation_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, id, title, e.projectID, isPrivate, ownerID, parentID, updatedAt, updatedAt).Exec(e.ctx)
	require.NoError(e.t, err)
	return id
}

func (e *repoEnv) conversationIDs(result *chat.ListConversationsResult) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(result.Conversations))
	for _, c := range result.Conversations {
		ids = append(ids, c.ID)
	}
	return ids
}

// TestListConversationsIncludeAncestors covers the opt-in ancestor expansion the
// session rail uses: a child that outran its parent must still bring the parent
// along, but only when the caller opts in and the ancestor is visible.
func TestListConversationsIncludeAncestors(t *testing.T) {
	e := newRepoEnv(t)
	base := time.Now().UTC()

	parent := e.insertConversation(e.ownerID, true, "parent", base.Add(-3*time.Hour))
	child := e.insertChild(parent, e.ownerID, true, "child", base)
	other := e.insertConversation(e.ownerID, true, "other", base.Add(-1*time.Hour))

	// The page limit drops the parent: the child (newest) and other fill it.
	res, err := e.repo.ListConversations(e.ctx, chat.ListConversationsParams{
		ProjectID:   e.projectID,
		OwnerUserID: &e.ownerID,
		Limit:       2,
	})
	require.NoError(t, err)
	require.Equal(t, 3, res.Total, "Total is the filtered count, not the page length")
	require.Equal(t, []uuid.UUID{child, other}, e.conversationIDs(res))

	// Opting in adds the parent so the page is groupable; order stays updated_at DESC.
	res, err = e.repo.ListConversations(e.ctx, chat.ListConversationsParams{
		ProjectID:        e.projectID,
		OwnerUserID:      &e.ownerID,
		Limit:            2,
		IncludeAncestors: true,
	})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{child, other, parent}, e.conversationIDs(res))

	// An archived parent is filtered like any other row: the child is promoted
	// to a root by the rail instead of revealing a hidden session.
	_, err = e.repo.SetArchived(e.ctx, e.projectID, e.ownerID, parent, true)
	require.NoError(t, err)

	res, err = e.repo.ListConversations(e.ctx, chat.ListConversationsParams{
		ProjectID:        e.projectID,
		OwnerUserID:      &e.ownerID,
		Limit:            2,
		IncludeAncestors: true,
	})
	require.NoError(t, err)
	require.NotContains(t, e.conversationIDs(res), parent, "archived ancestor must not be revealed")

	// includeArchived turns the same ancestor back on.
	res, err = e.repo.ListConversations(e.ctx, chat.ListConversationsParams{
		ProjectID:        e.projectID,
		OwnerUserID:      &e.ownerID,
		Limit:            2,
		IncludeArchived:  true,
		IncludeAncestors: true,
	})
	require.NoError(t, err)
	require.Contains(t, e.conversationIDs(res), parent)
}

// TestCascadeArchiveAndDelete covers the lifecycle contract: a group is one
// session in the rail, so archive/unarchive/delete follow the subtree, while a
// foreign private sub-chat is never swept up by a shared parent's owner action.
func TestCascadeArchiveAndDelete(t *testing.T) {
	e := newRepoEnv(t)
	base := time.Now().UTC()

	parent := e.insertConversation(e.ownerID, true, "parent", base)
	child := e.insertChild(parent, e.ownerID, true, "child", base)
	grandchild := e.insertChild(child, e.ownerID, true, "grandchild", base)
	unrelated := e.insertConversation(e.ownerID, true, "unrelated", base)
	e.insertMessage(child, "child message")

	// Archive follows the whole subtree.
	require.NoError(t, e.svc.ArchiveConversation(e.ctx, e.projectID, e.ownerID, parent))
	for _, id := range []uuid.UUID{parent, child, grandchild} {
		require.True(t, e.getByID(id).IsArchived, "descendant %s must be archived", id)
	}
	require.False(t, e.getByID(unrelated).IsArchived, "unrelated conversation must stay active")

	// Unarchive restores it symmetrically.
	require.NoError(t, e.svc.UnarchiveConversation(e.ctx, e.projectID, e.ownerID, parent))
	for _, id := range []uuid.UUID{parent, child, grandchild} {
		require.False(t, e.getByID(id).IsArchived, "descendant %s must be unarchived", id)
	}

	// Delete removes the subtree and its messages, leaving unrelated rows alone.
	require.NoError(t, e.svc.DeleteConversation(e.ctx, e.projectID, e.ownerID, parent))
	for _, id := range []uuid.UUID{parent, child, grandchild} {
		conv, err := e.repo.GetByID(e.ctx, e.projectID, e.ownerID, id)
		require.NoError(t, err)
		require.Nil(t, conv, "descendant %s must be deleted", id)
	}
	require.Equal(t, 0, e.countMessages(child))
	require.NotNil(t, e.getByID(unrelated))
}

// TestCascadeRespectsAccess ensures a shared parent cannot archive or delete
// another user's private sub-chat: the recursive predicate is re-applied at
// every level, not only at the root.
func TestCascadeRespectsAccess(t *testing.T) {
	e := newRepoEnv(t)
	base := time.Now().UTC()

	// A project-shared parent owner can see; a private child owned by another
	// user that the owner cannot see.
	shared := e.insertConversation(e.ownerID, false, "shared-parent", base)
	foreignChild := e.insertChild(shared, e.foreignID, true, "foreign-child", base)

	require.NoError(t, e.svc.ArchiveConversation(e.ctx, e.projectID, e.ownerID, shared))
	foreignConv, err := e.repo.GetByID(e.ctx, e.projectID, e.foreignID, foreignChild)
	require.NoError(t, err)
	require.NotNil(t, foreignConv)
	require.False(t, foreignConv.IsArchived, "foreign private sub-chat must not be archived")

	require.NoError(t, e.svc.UnarchiveConversation(e.ctx, e.projectID, e.ownerID, shared))
	require.NoError(t, e.svc.DeleteConversation(e.ctx, e.projectID, e.ownerID, shared))

	foreignConv, err = e.repo.GetByID(e.ctx, e.projectID, e.foreignID, foreignChild)
	require.NoError(t, err)
	require.NotNil(t, foreignConv, "foreign private sub-chat must survive")
	require.Nil(t, foreignConv.ParentConversationID, "the deleted parent link is cleared by the FK")
}

// TestParentLinkGateRejectsInaccessibleConversation covers the security half of
// QueryStream's parent resolution: ConversationIDForRun is not project-scoped,
// so the visible-conversation check is the only thing stopping a caller from
// grafting a sub-chat onto another user's private chat.
func TestParentLinkGateRejectsInaccessibleConversation(t *testing.T) {
	e := newRepoEnv(t)
	base := time.Now().UTC()

	foreignPrivate := e.insertConversation(e.foreignID, true, "foreign-private", base)
	require.False(t, e.svc.CanAccessConversation(e.ctx, e.projectID, e.ownerID, foreignPrivate),
		"a foreign private conversation must not be linkable as a parent")
	require.True(t, e.svc.CanAccessConversation(e.ctx, e.projectID, e.foreignID, foreignPrivate),
		"the owner can link its own private conversation")

	foreignShared := e.insertConversation(e.foreignID, false, "foreign-shared", base)
	require.True(t, e.svc.CanAccessConversation(e.ctx, e.projectID, e.ownerID, foreignShared),
		"a project-shared conversation is visible to any project member")

	require.False(t, e.svc.CanAccessConversation(e.ctx, e.projectID, e.ownerID, uuid.New()),
		"an unknown conversation must not be linkable")
}

// TestCreateConversationPersistsParent covers the write path QueryStream uses:
// the service stores the parent link and spawn source on creation.
func TestCreateConversationPersistsParent(t *testing.T) {
	e := newRepoEnv(t)
	parent := e.insertConversation(e.ownerID, true, "parent", time.Now().UTC())
	parentID := parent.String()
	spawn := "search-knowledge"

	conv, err := e.svc.CreateConversation(e.ctx, e.projectID, e.ownerID, chat.CreateConversationRequest{
		Title:                "what changed?",
		Message:              "what changed?",
		ParentConversationID: &parentID,
		SpawnSource:          &spawn,
	})
	require.NoError(t, err)
	require.NotNil(t, conv.ParentConversationID)
	require.Equal(t, parent, *conv.ParentConversationID)
	require.NotNil(t, conv.SpawnSource)
	require.Equal(t, "search-knowledge", *conv.SpawnSource)

	stored := e.getByID(conv.ID)
	require.NotNil(t, stored.ParentConversationID)
	require.Equal(t, parent, *stored.ParentConversationID)
	require.NotNil(t, stored.SpawnSource)
	require.Equal(t, "search-knowledge", *stored.SpawnSource)
}
