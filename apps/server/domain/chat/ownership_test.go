package chat_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// ChatOwnershipSuite proves the by-id chat conversation accessors enforce the
// intended access model: a conversation is owned by its owner_user_id and is
// readable/continuable/renamable/deletable only by that owner, OR by any
// project member when the conversation is non-private (is_private = false).
//
// This closes the surface→authority matrix finding (PR #994) that by-id paths
// (`GetByID`/`GetByIDWithMessages`/`Update`/`Delete`/`AddMessage`) filtered on
// id + project_id only, ignoring owner_user_id — letting any project member
// read and continue another member's private conversation by guessing its id.
type ChatOwnershipSuite struct {
	testutil.BaseSuite

	// userBID is the user_profiles.id of a second org member who is NOT the
	// owner of the seeded conversations (the attacker).
	userBID string
}

func TestChatOwnershipSuite(t *testing.T) {
	suite.Run(t, new(ChatOwnershipSuite))
}

func (s *ChatOwnershipSuite) SetupSuite() {
	s.SetDBSuffix("chat_ownership")
	s.BaseSuite.SetupSuite()
}

// SetupTest creates a second user (B) who is a member of the same org — and
// therefore passes RequireProjectMember for the same project as the owner (A,
// the e2e-test-user / AdminUser) — but owns no conversation.
func (s *ChatOwnershipSuite) SetupTest() {
	s.BaseSuite.SetupTest()

	s.userBID = "00000000-0000-0000-0000-00000000b00b"
	s.Require().NoError(testutil.CreateTestUser(s.Ctx, s.DB(), testutil.TestUser{
		ID:            s.userBID,
		ZitadelUserID: "e2e-chat-user-b",
		Email:         "userb@test.local",
		FirstName:     "User",
		LastName:      "B",
		Scopes:        []string{"chat:use", "chat:admin"},
	}))
	// Org membership is what RequireProjectMember checks for the header-scoped
	// /api/chat group, so B must belong to the same org as the project owner.
	s.Require().NoError(testutil.CreateTestOrgMembership(s.Ctx, s.DB(), s.OrgID, s.userBID, "member"))
}

// seedOwnedConversation inserts a private conversation owned by ownerID (with a
// distinctive message) directly into the DB, and returns its id string.
func (s *ChatOwnershipSuite) seedOwnedConversation(ownerID string) string {
	convID := uuid.New()
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.chat_conversations (id, title, project_id, is_private, owner_user_id, created_at, updated_at)
		VALUES (?, ?, ?, true, ?, NOW(), NOW())
	`, convID, "owner-secret", s.ProjectID, ownerID).Exec(s.Ctx)
	s.Require().NoError(err)

	_, err = s.DB().NewRaw(`
		INSERT INTO kb.chat_messages (id, conversation_id, role, content, created_at)
		VALUES (?, ?, 'user', ?, NOW())
	`, uuid.New(), convID, "secret-content-owned-by-A").Exec(s.Ctx)
	s.Require().NoError(err)

	return convID.String()
}

// TestByIDOwnershipEnforced covers every by-id accessor: GET /:id,
// GET /:id/history, POST /:id/messages, PATCH /:id, DELETE /:id. For each, a
// foreign member (B) must receive 404 (no existence oracle), while the owner
// (A) must still succeed.
func (s *ChatOwnershipSuite) TestByIDOwnershipEnforced() {
	ownerToken := "e2e-test-user" // maps to AdminUser (owner A)
	foreignToken := "e2e-chat-user-b"

	endpoints := []struct {
		name          string
		issueForeign  func(convID string) *testutil.HTTPResponse
		issueOwner    func(convID string) *testutil.HTTPResponse
		ownerWantCode int
		ownerWantBody string
	}{
		{
			name: "get",
			issueForeign: func(id string) *testutil.HTTPResponse {
				return s.Client.GET("/api/chat/"+id,
					testutil.WithAuth(foreignToken), testutil.WithProjectID(s.ProjectID))
			},
			issueOwner: func(id string) *testutil.HTTPResponse {
				return s.Client.GET("/api/chat/"+id,
					testutil.WithAuth(ownerToken), testutil.WithProjectID(s.ProjectID))
			},
			ownerWantCode: http.StatusOK,
			ownerWantBody: "secret-content-owned-by-A",
		},
		{
			name: "history",
			issueForeign: func(id string) *testutil.HTTPResponse {
				return s.Client.GET("/api/chat/"+id+"/history",
					testutil.WithAuth(foreignToken), testutil.WithProjectID(s.ProjectID))
			},
			issueOwner: func(id string) *testutil.HTTPResponse {
				return s.Client.GET("/api/chat/"+id+"/history",
					testutil.WithAuth(ownerToken), testutil.WithProjectID(s.ProjectID))
			},
			ownerWantCode: http.StatusOK,
		},
		{
			name: "add-message",
			issueForeign: func(id string) *testutil.HTTPResponse {
				return s.Client.POST("/api/chat/"+id+"/messages",
					testutil.WithAuth(foreignToken), testutil.WithProjectID(s.ProjectID),
					testutil.WithJSONBody(map[string]any{"role": "user", "content": "stolen continuation"}))
			},
			issueOwner: func(id string) *testutil.HTTPResponse {
				return s.Client.POST("/api/chat/"+id+"/messages",
					testutil.WithAuth(ownerToken), testutil.WithProjectID(s.ProjectID),
					testutil.WithJSONBody(map[string]any{"role": "user", "content": "legit continuation"}))
			},
			ownerWantCode: http.StatusCreated,
		},
		{
			name: "update",
			issueForeign: func(id string) *testutil.HTTPResponse {
				return s.Client.PATCH("/api/chat/"+id,
					testutil.WithAuth(foreignToken), testutil.WithProjectID(s.ProjectID),
					testutil.WithJSONBody(map[string]any{"title": "renamed by attacker"}))
			},
			issueOwner: func(id string) *testutil.HTTPResponse {
				return s.Client.PATCH("/api/chat/"+id,
					testutil.WithAuth(ownerToken), testutil.WithProjectID(s.ProjectID),
					testutil.WithJSONBody(map[string]any{"title": "renamed by owner"}))
			},
			ownerWantCode: http.StatusOK,
		},
		{
			name: "delete",
			issueForeign: func(id string) *testutil.HTTPResponse {
				return s.Client.DELETE("/api/chat/"+id,
					testutil.WithAuth(foreignToken), testutil.WithProjectID(s.ProjectID))
			},
			issueOwner: func(id string) *testutil.HTTPResponse {
				return s.Client.DELETE("/api/chat/"+id,
					testutil.WithAuth(ownerToken), testutil.WithProjectID(s.ProjectID))
			},
			ownerWantCode: http.StatusOK,
		},
	}

	for _, ep := range endpoints {
		ep := ep
		s.Run(ep.name, func() {
			// Fresh conversation per endpoint so the destructive cases (delete,
			// rename, add-message) don't pollute each other.
			convID := s.seedOwnedConversation(testutil.AdminUser.ID)

			foreign := ep.issueForeign(convID)
			s.Require().Equal(http.StatusNotFound, foreign.StatusCode,
				"%s: foreign member must get 404, got %d: %s",
				ep.name, foreign.StatusCode, foreign.String())

			owner := ep.issueOwner(convID)
			s.Require().Equal(ep.ownerWantCode, owner.StatusCode,
				"%s: owner must get %d, got %d: %s",
				ep.name, ep.ownerWantCode, owner.StatusCode, owner.String())
			if ep.ownerWantBody != "" && !strings.Contains(owner.String(), ep.ownerWantBody) {
				s.Require().Contains(owner.String(), ep.ownerWantBody,
					"%s: owner response missing expected content", ep.name)
			}
		})
	}
}

// TestNonPrivateConversationSharedByProjectMember proves the is_private=false
// carve-out: a non-owner project member may still read a non-private (shared)
// conversation, matching the ListConversations predicate (owner OR is_private
// = false).
func (s *ChatOwnershipSuite) TestNonPrivateConversationSharedByProjectMember() {
	convID := uuid.New()
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.chat_conversations (id, title, project_id, is_private, owner_user_id, created_at, updated_at)
		VALUES (?, ?, ?, false, ?, NOW(), NOW())
	`, convID, "shared-conv", s.ProjectID, testutil.AdminUser.ID).Exec(s.Ctx)
	s.Require().NoError(err)

	resp := s.Client.GET("/api/chat/"+convID.String(),
		testutil.WithAuth("e2e-chat-user-b"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"non-private conversation must be readable by any project member, got %d: %s",
		resp.StatusCode, resp.String())
}
