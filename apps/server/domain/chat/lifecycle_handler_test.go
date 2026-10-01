package chat_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// ChatLifecycleSuite exercises the archive/unarchive/delete HTTP surface through
// the full in-process server (auth middleware + route table + handler) to prove
// the lifecycle operations are member-scoped (chat:use, not chat:admin), apply
// the owner-or-shared predicate, and that the list includeArchived query
// parameter is forwarded into the repository.
type ChatLifecycleSuite struct {
	testutil.BaseSuite

	// userBID is the user_profiles.id of a second org member who is NOT the
	// owner of the seeded conversations (the foreign caller).
	userBID string
}

func TestChatLifecycleSuite(t *testing.T) {
	suite.Run(t, new(ChatLifecycleSuite))
}

func (s *ChatLifecycleSuite) SetupSuite() {
	s.SetDBSuffix("chat_lifecycle")
	s.BaseSuite.SetupSuite()
}

func (s *ChatLifecycleSuite) SetupTest() {
	s.BaseSuite.SetupTest()

	s.userBID = "00000000-0000-0000-0000-00000000c0ff"
	s.Require().NoError(testutil.CreateTestUser(s.Ctx, s.DB(), testutil.TestUser{
		ID:            s.userBID,
		ZitadelUserID: "e2e-chat-lifecycle-b",
		Email:         "lifecycle-b@test.local",
		Scopes:        []string{"chat:use", "chat:admin"},
	}))
	s.Require().NoError(testutil.CreateTestOrgMembership(s.Ctx, s.DB(), s.OrgID, s.userBID, "member"))
}

// seedOwnedConversation inserts a private conversation owned by ownerID and
// returns its id string.
func (s *ChatLifecycleSuite) seedOwnedConversation(ownerID string) string {
	convID := uuid.New()
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.chat_conversations (id, title, project_id, is_private, owner_user_id, created_at, updated_at)
		VALUES (?, ?, ?, true, ?, NOW(), NOW())
	`, convID, "owner-secret", s.ProjectID, ownerID).Exec(s.Ctx)
	s.Require().NoError(err)
	return convID.String()
}

// TestArchiveUnarchiveHandler covers task 3.2: owner archives/unarchives (200),
// the archive state round-trips through the read path, and a foreign member
// archiving another user's private conversation receives 404.
func (s *ChatLifecycleSuite) TestArchiveUnarchiveHandler() {
	ownerToken := "e2e-test-user"
	foreignToken := "e2e-chat-lifecycle-b"

	convID := s.seedOwnedConversation(testutil.AdminUser.ID)

	resp := s.Client.POST("/api/chat/"+convID+"/archive",
		testutil.WithAuth(ownerToken), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"owner archive must be 200, got %d: %s", resp.StatusCode, resp.String())

	got := s.Client.GET("/api/chat/"+convID,
		testutil.WithAuth(ownerToken), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, got.StatusCode)
	s.Require().Contains(got.String(), `"isArchived":true`,
		"archived conversation must expose isArchived=true: %s", got.String())

	resp = s.Client.POST("/api/chat/"+convID+"/unarchive",
		testutil.WithAuth(ownerToken), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"owner unarchive must be 200, got %d: %s", resp.StatusCode, resp.String())

	got = s.Client.GET("/api/chat/"+convID,
		testutil.WithAuth(ownerToken), testutil.WithProjectID(s.ProjectID))
	s.Require().Contains(got.String(), `"isArchived":false`,
		"unarchived conversation must expose isArchived=false: %s", got.String())

	// Foreign member archiving another user's private conversation → 404.
	foreignConv := s.seedOwnedConversation(testutil.AdminUser.ID)
	resp = s.Client.POST("/api/chat/"+foreignConv+"/archive",
		testutil.WithAuth(foreignToken), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusNotFound, resp.StatusCode,
		"foreign member must get 404 archiving another user's private conversation, got %d: %s",
		resp.StatusCode, resp.String())
}

// TestListIncludeArchivedForwarding covers task 3.3: the includeArchived query
// parameter is forwarded into the list, defaults to false, and "false" matches
// the default-exclude behaviour.
func (s *ChatLifecycleSuite) TestListIncludeArchivedForwarding() {
	token := "e2e-test-user"

	active := s.seedOwnedConversation(testutil.AdminUser.ID)
	archived := s.seedOwnedConversation(testutil.AdminUser.ID)
	resp := s.Client.POST("/api/chat/"+archived+"/archive",
		testutil.WithAuth(token), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, resp.StatusCode)

	// Default list excludes archived.
	list := s.Client.GET("/api/chat/conversations",
		testutil.WithAuth(token), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, list.StatusCode)
	s.Require().Contains(list.String(), active, "active conversation must be listed by default")
	s.Require().NotContains(list.String(), archived, "archived conversation must be excluded by default")

	// includeArchived=true includes both.
	list = s.Client.GET("/api/chat/conversations?includeArchived=true",
		testutil.WithAuth(token), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, list.StatusCode)
	s.Require().Contains(list.String(), active)
	s.Require().Contains(list.String(), archived, "includeArchived=true must include archived conversations")

	// includeArchived=false matches default (excluded).
	list = s.Client.GET("/api/chat/conversations?includeArchived=false",
		testutil.WithAuth(token), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, list.StatusCode)
	s.Require().NotContains(list.String(), archived, "includeArchived=false must exclude archived conversations")
}

// TestNonAdminReachesLifecycleRoutes covers task 3.4: a project member with only
// chat:use (no chat:admin) reaches archive, unarchive, and delete — proving the
// DELETE route moved out of the chat:admin subgroup — and the delete handler
// still fails closed (404) for a foreign conversation.
func (s *ChatLifecycleSuite) TestNonAdminReachesLifecycleRoutes() {
	token := "emt_lifecycle_use_only"
	s.Require().NoError(testutil.CreateTestAPIToken(s.Ctx, s.DB(),
		testutil.AdminUser.ID, token, []string{"chat:use"}, s.ProjectID))

	convID := s.seedOwnedConversation(testutil.AdminUser.ID)

	resp := s.Client.POST("/api/chat/"+convID+"/archive",
		testutil.WithAuth(token), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"chat:use-only caller must reach archive (not the admin gate), got %d: %s",
		resp.StatusCode, resp.String())

	resp = s.Client.POST("/api/chat/"+convID+"/unarchive",
		testutil.WithAuth(token), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"chat:use-only caller must reach unarchive (not the admin gate), got %d: %s",
		resp.StatusCode, resp.String())

	resp = s.Client.DELETE("/api/chat/"+convID,
		testutil.WithAuth(token), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"chat:use-only caller must reach delete (not the admin gate), got %d: %s",
		resp.StatusCode, resp.String())

	// Deleted conversation is gone.
	got := s.Client.GET("/api/chat/"+convID,
		testutil.WithAuth(token), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusNotFound, got.StatusCode,
		"deleted conversation must be gone, got %d: %s", got.StatusCode, got.String())

	// Foreign conversation still 404 for a chat:use-only caller.
	foreignConv := s.seedOwnedConversation(s.userBID)
	resp = s.Client.DELETE("/api/chat/"+foreignConv,
		testutil.WithAuth(token), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusNotFound, resp.StatusCode,
		"delete of a foreign private conversation must be 404, got %d: %s",
		resp.StatusCode, resp.String())
}
