package chat_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// ChatMembershipSuite exercises the header-scoped /api/chat group through the
// full in-process server (auth middleware + route table + handler) to prove
// that a caller's authorization is derived from real organization membership,
// not from the client-supplied X-Project-ID header (issue #864).
//
// The admin user (e2e-test-user token) is set up by BaseSuite.SetupTest as an
// org_admin member of s.OrgID (org A). A second org B is created on demand with
// no membership for the admin user, and the caller sends org B's project id via
// the X-Project-ID header to reach B's conversations.
type ChatMembershipSuite struct {
	testutil.BaseSuite
}

func TestChatMembershipSuite(t *testing.T) {
	suite.Run(t, new(ChatMembershipSuite))
}

func (s *ChatMembershipSuite) SetupSuite() {
	s.SetDBSuffix("chat_membership")
	s.BaseSuite.SetupSuite()
}

// newForeignProject creates an organization the admin user is NOT a member of,
// plus a project owned by it, and returns the foreign project ID.
func (s *ChatMembershipSuite) newForeignProject() string {
	orgB := uuid.New().String()
	s.Require().NoError(testutil.CreateTestOrganization(s.Ctx, s.DB(), orgB, "Org B"))
	projectB := uuid.New().String()
	s.Require().NoError(testutil.CreateTestProject(s.Ctx, s.DB(), testutil.TestProject{
		ID:    projectB,
		OrgID: orgB,
		Name:  "Project B",
	}, testutil.AdminUser.ID))
	return projectB
}

// seedConversation inserts a conversation owned by projectID directly in the DB
// (the admin user cannot create one via the API once the membership guard lands).
func (s *ChatMembershipSuite) seedConversation(projectID string) string {
	convID := uuid.New()
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.chat_conversations (id, title, project_id, is_private, owner_user_id, created_at, updated_at)
		VALUES (?, ?, ?, true, ?, NOW(), NOW())
	`, convID, "foreign-secret", projectID, testutil.AdminUser.ID).Exec(s.Ctx)
	s.Require().NoError(err)
	return convID.String()
}

// TestCrossProjectConversationReadForbidden is the read reproducer: a member of
// org A who sends org B's project id via X-Project-ID must not read or list
// org B's conversations. Before the fix these returned 200 because the handlers
// scoped by the header-derived project ID without any membership check.
func (s *ChatMembershipSuite) TestCrossProjectConversationReadForbidden() {
	projectB := s.newForeignProject()
	convB := s.seedConversation(projectB)

	resp := s.Client.GET("/api/chat/"+convB,
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(projectB))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project conversation read must be forbidden, got %d: %s", resp.StatusCode, resp.String())

	resp = s.Client.GET("/api/chat/conversations",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(projectB))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project conversation list must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

// TestCrossProjectConversationWriteForbidden is the higher-severity reproducer:
// a member of org A must not create a conversation (or message) in org B's
// project via X-Project-ID.
func (s *ChatMembershipSuite) TestCrossProjectConversationWriteForbidden() {
	projectB := s.newForeignProject()
	convB := s.seedConversation(projectB)

	resp := s.Client.POST("/api/chat/conversations",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(projectB),
		testutil.WithJSONBody(map[string]any{"title": "stolen", "message": "hi"}))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project conversation create must be forbidden, got %d: %s", resp.StatusCode, resp.String())

	resp = s.Client.POST("/api/chat/"+convB+"/messages",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(projectB),
		testutil.WithJSONBody(map[string]any{"role": "user", "content": "hi"}))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project message add must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

// TestOwnProjectChatAccessOK proves the membership derivation still admits the
// caller's own project: the admin user is a member of s.OrgID, so chat against
// their own project must succeed.
func (s *ChatMembershipSuite) TestOwnProjectChatAccessOK() {
	resp := s.Client.GET("/api/chat/conversations",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-project conversation list must succeed, got %d: %s", resp.StatusCode, resp.String())
}

// TestChatNoUserUnauthorized proves an unauthenticated request is rejected with
// 401 before any membership resolution.
func (s *ChatMembershipSuite) TestChatNoUserUnauthorized() {
	resp := s.Client.GET("/api/chat/conversations", testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusUnauthorized, resp.StatusCode,
		"unauthenticated chat list must be 401, got %d: %s", resp.StatusCode, resp.String())
}

// TestChatUnknownProjectNotFound proves a session caller addressing a
// non-existent project receives 404 (no existence oracle) rather than an empty
// result set.
func (s *ChatMembershipSuite) TestChatUnknownProjectNotFound() {
	resp := s.Client.GET("/api/chat/conversations",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(uuid.New().String()))
	s.Require().Equal(http.StatusNotFound, resp.StatusCode,
		"unknown-project chat list must be 404, got %d: %s", resp.StatusCode, resp.String())
}
