package chat_test

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// seedPrivateCanonicalConversation inserts a PRIVATE conversation owned by
// ownerID with a fresh canonical_id (the legacy shape the live CreateConversation
// path used to produce before refinement chats became shared), plus one message.
// It returns the canonical id string.
func (s *ChatOwnershipSuite) seedPrivateCanonicalConversation(ownerID string) string {
	canonicalID := uuid.New()
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.chat_conversations (id, title, project_id, is_private, owner_user_id, canonical_id, created_at, updated_at)
		VALUES (?, ?, ?, true, ?, ?, NOW(), NOW())
	`, uuid.New(), "a-private-canonical", s.ProjectID, ownerID, canonicalID).Exec(s.Ctx)
	s.Require().NoError(err)

	_, err = s.DB().NewRaw(`
		INSERT INTO kb.chat_messages (id, conversation_id, role, content, created_at)
		SELECT ?, id, 'user', ?, NOW() FROM kb.chat_conversations WHERE canonical_id = ?
	`, uuid.New(), "a-private-canonical-secret", canonicalID).Exec(s.Ctx)
	s.Require().NoError(err)

	return canonicalID.String()
}

// TestCanonicalIDPrivateForeignBlocked is the canonical-id leak reproducer: a
// foreign project member addressing another user's PRIVATE canonical id must
// not receive that conversation (nor its messages). Because canonical_id is
// globally unique, the caller cannot mint a second row for the same canonical
// id, so the correct behaviour is a 409 conflict, never A's row.
func (s *ChatOwnershipSuite) TestCanonicalIDPrivateForeignBlocked() {
	canonicalID := s.seedPrivateCanonicalConversation(testutil.AdminUser.ID)

	resp := s.Client.POST("/api/chat/conversations",
		testutil.WithAuth("e2e-chat-user-b"), testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"title":       "stolen",
			"message":     "hi",
			"canonicalId": canonicalID,
		}))
	s.Require().Equal(http.StatusConflict, resp.StatusCode,
		"foreign member must not receive another user's private canonical conversation, got %d: %s",
		resp.StatusCode, resp.String())
	s.Require().NotContains(resp.String(), "a-private-canonical",
		"response must not leak the owner's conversation title")
}

// TestCanonicalIDSharedGetOrCreate proves the legitimate refinement flow: a
// canonical (refinement) conversation is project-shared, so the first creator
// owns it and every other project member (and the creator again) resolves the
// SAME conversation by canonical id — the get-or-create the gateway's
// CreateObjectConversation relies on.
func (s *ChatOwnershipSuite) TestCanonicalIDSharedGetOrCreate() {
	canonicalID := uuid.New().String()

	create := func(token string) (id string, isPrivate bool) {
		resp := s.Client.POST("/api/chat/conversations",
			testutil.WithAuth(token), testutil.WithProjectID(s.ProjectID),
			testutil.WithJSONBody(map[string]any{
				"title":       "obj-refinement",
				"message":     "refine this",
				"canonicalId": canonicalID,
			}))
		s.Require().Equal(http.StatusCreated, resp.StatusCode,
			"refinement get-or-create must succeed, got %d: %s", resp.StatusCode, resp.String())
		var out struct {
			ID        string `json:"id"`
			IsPrivate bool   `json:"isPrivate"`
		}
		s.Require().NoError(json.Unmarshal([]byte(resp.String()), &out))
		return out.ID, out.IsPrivate
	}

	ownerID, ownerPrivate := create("e2e-test-user")
	s.Require().False(ownerPrivate, "canonical/refinement conversation must be shared (is_private=false)")

	// Owner reaches their own conversation by canonical id again (dedup).
	againID, _ := create("e2e-test-user")
	s.Require().Equal(ownerID, againID, "owner must resolve the same conversation by canonical id")

	// A second project member resolves the same shared conversation.
	memberID, _ := create("e2e-chat-user-b")
	s.Require().Equal(ownerID, memberID, "project member must resolve the shared refinement conversation")
}

// TestCanonicalStreamForeignBlocked is the stream-path variant of the
// canonical-id hole: a foreign member streaming with A's private canonical id
// must not start a stream against A's conversation (and thereby exfiltrate its
// history through the LLM).
func (s *ChatOwnershipSuite) TestCanonicalStreamForeignBlocked() {
	canonicalID := s.seedPrivateCanonicalConversation(testutil.AdminUser.ID)

	resp := s.Client.POST("/api/chat/stream",
		testutil.WithAuth("e2e-chat-user-b"), testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"message":     "steal",
			"canonicalId": canonicalID,
		}))
	s.Require().Equal(http.StatusConflict, resp.StatusCode,
		"foreign member must not stream against another user's private canonical conversation, got %d: %s",
		resp.StatusCode, resp.String())
}

// TestStreamConversationIDForeignBlocked is the stream-path variant of the
// by-id hole: a foreign member streaming with A's private conversation id must
// be rejected before any stream starts.
func (s *ChatOwnershipSuite) TestStreamConversationIDForeignBlocked() {
	convID := s.seedOwnedConversation(testutil.AdminUser.ID)

	resp := s.Client.POST("/api/chat/stream",
		testutil.WithAuth("e2e-chat-user-b"), testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"message":        "steal",
			"conversationId": convID,
		}))
	s.Require().Equal(http.StatusNotFound, resp.StatusCode,
		"foreign member must not stream against another user's private conversation, got %d: %s",
		resp.StatusCode, resp.String())
}
