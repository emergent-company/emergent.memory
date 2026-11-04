package chat_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/chat"
)

// An ownerless caller is a device credential: core.api_tokens.user_id is NULL,
// so the request carries an empty user id. The create paths used to bind that
// empty string against the uuid owner column, which Postgres rejects with
// SQLSTATE 22P02 and which the gateway relayed as a 500/502 to the device.
//
// These tests pin the semantics chosen for that caller: the conversation is
// created project-shared and unowned, so the creator (and every project member)
// can read it back. An unowned *private* conversation would be invisible to
// everyone, including the caller that just created it.
func TestOwnerlessCreateConversation(t *testing.T) {
	e := newRepoEnv(t)

	conv, err := e.svc.CreateConversation(e.ctx, e.projectID, "", chat.CreateConversationRequest{
		Title:   "device chat",
		Message: "hello from a device",
	})
	require.NoError(t, err, "an ownerless caller must be able to create a conversation")
	require.NotNil(t, conv)
	require.Nil(t, conv.OwnerUserID, "an ownerless caller's conversation has no owner")
	require.False(t, conv.IsPrivate, "an unowned private conversation would be invisible to its creator")

	// The creator reads it back through the ownerless scope.
	read, err := e.repo.GetByIDWithMessages(e.ctx, e.projectID, "", conv.ID)
	require.NoError(t, err)
	require.NotNil(t, read, "the creator must see the conversation it just made")
	require.Len(t, read.Messages, 1)

	// A project member reads it too, because it is project-shared.
	member, err := e.repo.GetByID(e.ctx, e.projectID, e.ownerID, conv.ID)
	require.NoError(t, err)
	require.NotNil(t, member, "a project-shared conversation is visible to members")

	// Nobody else's private conversation is affected by the unowned row.
	private := e.insertConversation(e.foreignID, true, "foreign-private", conv.CreatedAt)
	list, err := e.repo.ListConversations(e.ctx, chat.ListConversationsParams{
		ProjectID:   e.projectID,
		OwnerUserID: new(string),
		Limit:       50,
	})
	require.NoError(t, err)
	ids := make([]uuid.UUID, 0, len(list.Conversations))
	for _, c := range list.Conversations {
		ids = append(ids, c.ID)
	}
	require.Contains(t, ids, conv.ID)
	require.NotContains(t, ids, private)
}

func TestOwnerlessGetOrCreateConversation(t *testing.T) {
	e := newRepoEnv(t)
	canonical := uuid.New().String()

	conv, created, err := e.svc.GetOrCreateConversation(e.ctx, e.projectID, "", canonical, "Object chat")
	require.NoError(t, err, "an ownerless caller must be able to open a canonical conversation")
	require.True(t, created)
	require.NotNil(t, conv)
	require.Nil(t, conv.OwnerUserID)
	require.False(t, conv.IsPrivate, "canonical conversations are project-shared")

	// Re-opening resolves the same row instead of creating a second one.
	again, created, err := e.svc.GetOrCreateConversation(e.ctx, e.projectID, "", canonical, "Object chat")
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, conv.ID, again.ID)
}

// The owner-present path is unchanged: a member's ordinary conversation is
// still private and owned by them.
func TestOwnedCreateConversationStaysPrivate(t *testing.T) {
	e := newRepoEnv(t)

	conv, err := e.svc.CreateConversation(e.ctx, e.projectID, e.ownerID, chat.CreateConversationRequest{
		Title:   "member chat",
		Message: "hello",
	})
	require.NoError(t, err)
	require.NotNil(t, conv.OwnerUserID)
	require.Equal(t, e.ownerID, *conv.OwnerUserID)
	require.True(t, conv.IsPrivate)

	// A foreign member cannot read it.
	foreign, err := e.repo.GetByID(e.ctx, e.projectID, e.foreignID, conv.ID)
	require.NoError(t, err)
	require.Nil(t, foreign, "a private conversation stays private to its owner")
}
