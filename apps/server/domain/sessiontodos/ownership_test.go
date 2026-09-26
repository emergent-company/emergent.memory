package sessiontodos_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// SessionTodosOwnershipSuite proves the session-todo accessors enforce the
// conversation ownership model settled in #1010: a session's todos are reachable
// only when the session is in the caller's project AND, when the session is
// linked to a chat conversation (kb.chat_conversations.acp_session_id), that
// conversation is owned by the caller or non-private. A foreign member must get
// 404 (no existence oracle) on every read/write; the owner still succeeds; and a
// non-private (project-shared) conversation's session todos remain reachable.
type SessionTodosOwnershipSuite struct {
	testutil.BaseSuite

	// userBID is the user_profiles.id of a second org member who is NOT the
	// owner of the seeded conversation (the attacker).
	userBID string
}

func TestSessionTodosOwnershipSuite(t *testing.T) {
	suite.Run(t, new(SessionTodosOwnershipSuite))
}

func (s *SessionTodosOwnershipSuite) SetupSuite() {
	s.SetDBSuffix("sessiontodos_ownership")
	s.BaseSuite.SetupSuite()
}

// SetupTest creates a second user (B) who is a member of the same org — and
// therefore a legitimate project member — but owns no conversation.
func (s *SessionTodosOwnershipSuite) SetupTest() {
	s.BaseSuite.SetupTest()

	s.userBID = "00000000-0000-0000-0000-00000000b004"
	s.Require().NoError(testutil.CreateTestUser(s.Ctx, s.DB(), testutil.TestUser{
		ID:            s.userBID,
		ZitadelUserID: "e2e-sessiontodos-user-b",
		Email:         "sessiontodos-b@test.local",
		FirstName:     "User",
		LastName:      "B",
	}))
	s.Require().NoError(testutil.CreateTestOrgMembership(s.Ctx, s.DB(), s.OrgID, s.userBID, "member"))
}

// seedSession inserts an ACP session, a chat conversation owned by ownerID
// (private when isPrivate), and one todo in that session, returning the session
// id and the todo id.
func (s *SessionTodosOwnershipSuite) seedSession(ownerID string, isPrivate bool) (string, string) {
	sessionID := uuid.New()
	todoID := uuid.New()
	convID := uuid.New()

	_, err := s.DB().NewRaw(`
		INSERT INTO kb.acp_sessions (id, project_id, created_at, updated_at)
		VALUES (?, ?, NOW(), NOW())
	`, sessionID, s.ProjectID).Exec(s.Ctx)
	s.Require().NoError(err)

	_, err = s.DB().NewRaw(`
		INSERT INTO kb.chat_conversations (id, title, project_id, is_private, owner_user_id, acp_session_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, NOW(), NOW())
	`, convID, "owner-secret", s.ProjectID, isPrivate, ownerID, sessionID).Exec(s.Ctx)
	s.Require().NoError(err)

	_, err = s.DB().NewRaw(`
		INSERT INTO kb.session_todos (id, session_id, content, status, "order", created_at, updated_at)
		VALUES (?, ?, ?, 'draft', 0, NOW(), NOW())
	`, todoID, sessionID, "secret-todo-owned-by-A").Exec(s.Ctx)
	s.Require().NoError(err)

	return sessionID.String(), todoID.String()
}

// TestOwnershipEnforced covers every accessor: GET/POST/PATCH/DELETE. For each,
// a foreign member (B) must receive 404 while the owner (A) still succeeds.
func (s *SessionTodosOwnershipSuite) TestOwnershipEnforced() {
	ownerToken := "e2e-test-user"
	foreignToken := "e2e-sessiontodos-user-b"

	endpoints := []struct {
		name         string
		issueForeign func(sessionID, todoID string) *testutil.HTTPResponse
		issueOwner   func(sessionID, todoID string) *testutil.HTTPResponse
		ownerWant    int
	}{
		{
			name: "list",
			issueForeign: func(sessionID, _ string) *testutil.HTTPResponse {
				return s.Client.GET("/api/v1/agent/sessions/"+sessionID+"/todos",
					testutil.WithAuth(foreignToken), testutil.WithProjectID(s.ProjectID))
			},
			issueOwner: func(sessionID, _ string) *testutil.HTTPResponse {
				return s.Client.GET("/api/v1/agent/sessions/"+sessionID+"/todos",
					testutil.WithAuth(ownerToken), testutil.WithProjectID(s.ProjectID))
			},
			ownerWant: http.StatusOK,
		},
		{
			name: "create",
			issueForeign: func(sessionID, _ string) *testutil.HTTPResponse {
				return s.Client.POST("/api/v1/agent/sessions/"+sessionID+"/todos",
					testutil.WithAuth(foreignToken), testutil.WithProjectID(s.ProjectID),
					testutil.WithJSONBody(map[string]any{"content": "stolen todo"}))
			},
			issueOwner: func(sessionID, _ string) *testutil.HTTPResponse {
				return s.Client.POST("/api/v1/agent/sessions/"+sessionID+"/todos",
					testutil.WithAuth(ownerToken), testutil.WithProjectID(s.ProjectID),
					testutil.WithJSONBody(map[string]any{"content": "legit todo"}))
			},
			ownerWant: http.StatusCreated,
		},
		{
			name: "update",
			issueForeign: func(sessionID, todoID string) *testutil.HTTPResponse {
				return s.Client.PATCH("/api/v1/agent/sessions/"+sessionID+"/todos/"+todoID,
					testutil.WithAuth(foreignToken), testutil.WithProjectID(s.ProjectID),
					testutil.WithJSONBody(map[string]any{"status": "completed"}))
			},
			issueOwner: func(sessionID, todoID string) *testutil.HTTPResponse {
				return s.Client.PATCH("/api/v1/agent/sessions/"+sessionID+"/todos/"+todoID,
					testutil.WithAuth(ownerToken), testutil.WithProjectID(s.ProjectID),
					testutil.WithJSONBody(map[string]any{"status": "completed"}))
			},
			ownerWant: http.StatusOK,
		},
		{
			name: "delete",
			issueForeign: func(sessionID, todoID string) *testutil.HTTPResponse {
				return s.Client.DELETE("/api/v1/agent/sessions/"+sessionID+"/todos/"+todoID,
					testutil.WithAuth(foreignToken), testutil.WithProjectID(s.ProjectID))
			},
			issueOwner: func(sessionID, todoID string) *testutil.HTTPResponse {
				return s.Client.DELETE("/api/v1/agent/sessions/"+sessionID+"/todos/"+todoID,
					testutil.WithAuth(ownerToken), testutil.WithProjectID(s.ProjectID))
			},
			ownerWant: http.StatusNoContent,
		},
	}

	for _, ep := range endpoints {
		ep := ep
		s.Run(ep.name, func() {
			sessionID, todoID := s.seedSession(testutil.AdminUser.ID, true)

			foreign := ep.issueForeign(sessionID, todoID)
			s.Require().Equal(http.StatusNotFound, foreign.StatusCode,
				"%s: foreign member must get 404, got %d: %s",
				ep.name, foreign.StatusCode, foreign.String())

			owner := ep.issueOwner(sessionID, todoID)
			s.Require().Equal(ep.ownerWant, owner.StatusCode,
				"%s: owner must get %d, got %d: %s",
				ep.name, ep.ownerWant, owner.StatusCode, owner.String())
		})
	}
}

// TestNonPrivateSessionSharedByProjectMember proves the is_private=false
// carve-out: a non-owner project member may still read a non-private (shared)
// conversation's session todos, matching the #1010 predicate.
func (s *SessionTodosOwnershipSuite) TestNonPrivateSessionSharedByProjectMember() {
	sessionID, _ := s.seedSession(testutil.AdminUser.ID, false)

	resp := s.Client.GET("/api/v1/agent/sessions/"+sessionID+"/todos",
		testutil.WithAuth("e2e-sessiontodos-user-b"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"non-private session todos must be readable by any project member, got %d: %s",
		resp.StatusCode, resp.String())
}
