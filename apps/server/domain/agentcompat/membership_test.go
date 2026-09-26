package agentcompat_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// AgentCompatMembershipSuite proves the OpenAI-compatible /v1 endpoints derive
// authorization from real organization membership, not from the client-supplied
// X-Project-ID header / token binding (issue #913).
type AgentCompatMembershipSuite struct {
	testutil.BaseSuite
}

func TestAgentCompatMembershipSuite(t *testing.T) {
	suite.Run(t, new(AgentCompatMembershipSuite))
}

func (s *AgentCompatMembershipSuite) SetupSuite() {
	s.SetDBSuffix("agentcompat_membership")
	s.BaseSuite.SetupSuite()
}

func (s *AgentCompatMembershipSuite) newForeignProject() string {
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

func (s *AgentCompatMembershipSuite) TestCrossProjectModelsForbidden() {
	projectB := s.newForeignProject()

	resp := s.Client.GET("/v1/models",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(projectB))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project models must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

func (s *AgentCompatMembershipSuite) TestCrossProjectChatForbidden() {
	projectB := s.newForeignProject()

	resp := s.Client.POST("/v1/chat/completions",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(projectB),
		testutil.WithJSONBody(map[string]any{
			"model":    "agent:graph-query-agent",
			"messages": []map[string]any{{"role": "user", "content": "hi"}},
		}))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project chat completion must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

func (s *AgentCompatMembershipSuite) TestOwnProjectModelsOK() {
	resp := s.Client.GET("/v1/models",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-project models must succeed, got %d: %s", resp.StatusCode, resp.String())
}
