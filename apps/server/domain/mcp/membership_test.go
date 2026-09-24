package mcp_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// MCPMembershipSuite exercises the /api/mcp group through the full in-process
// server. The group is session-bound with a header fallback (issue #868, the
// #864 class): the SSE sub-routes address the project by :projectId, while the
// unified/rpc endpoints resolve it from the X-Project-ID header with an
// initialize-claimed session fallback. Before the fix, all of these trusted a
// client-supplied project with no membership check.
type MCPMembershipSuite struct {
	testutil.BaseSuite
}

func TestMCPMembershipSuite(t *testing.T) {
	suite.Run(t, new(MCPMembershipSuite))
}

func (s *MCPMembershipSuite) SetupSuite() {
	s.SetDBSuffix("mcp_membership")
	s.BaseSuite.SetupSuite()
}

func (s *MCPMembershipSuite) newForeignProject() string {
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

func initBody(projectParam string) map[string]any {
	params := map[string]any{
		"protocolVersion": "2025-06-18",
		"clientInfo":      map[string]any{"name": "mcp-membership-test", "version": "1.0.0"},
	}
	if projectParam != "" {
		params["project_id"] = projectParam
	}
	return map[string]any{
		"jsonrpc": "2.0",
		"method":  "initialize",
		"id":      1,
		"params":  params,
	}
}

// TestCrossProjectHeaderForbidden proves the header-scoped path is closed: a
// member of org A sending org B's project id via X-Project-ID is rejected 403
// before the session is created.
func (s *MCPMembershipSuite) TestCrossProjectHeaderForbidden() {
	projectB := s.newForeignProject()
	resp := s.Client.POST("/api/mcp/rpc",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(projectB),
		testutil.WithJSONBody(initBody("")))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project MCP initialize via header must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

// TestSSEPathParamForbidden proves the :projectId-scoped SSE route rejects a
// non-member's project via the URL path.
func (s *MCPMembershipSuite) TestSSEPathParamForbidden() {
	projectB := s.newForeignProject()
	resp := s.Client.GET("/api/mcp/sse/"+projectB,
		testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"cross-project MCP SSE connect must be forbidden, got %d: %s", resp.StatusCode, resp.String())
}

// TestOwnProjectInitializeOK proves the membership derivation still admits the
// caller's own project on the session-bound endpoint.
func (s *MCPMembershipSuite) TestOwnProjectInitializeOK() {
	resp := s.Client.POST("/api/mcp/rpc",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(initBody(s.ProjectID)))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"own-project MCP initialize must succeed, got %d: %s", resp.StatusCode, resp.String())
}

// TestNoUserUnauthorized proves an unauthenticated request is rejected 401.
func (s *MCPMembershipSuite) TestNoUserUnauthorized() {
	resp := s.Client.POST("/api/mcp/rpc",
		testutil.WithProjectID(s.ProjectID), testutil.WithJSONBody(initBody(s.ProjectID)))
	s.Require().Equal(http.StatusUnauthorized, resp.StatusCode,
		"unauthenticated MCP initialize must be 401, got %d: %s", resp.StatusCode, resp.String())
}

// TestTokenProjectBindingForbidden proves a project-bound emt_* token
// presenting a different project via X-Project-ID is rejected 403.
func (s *MCPMembershipSuite) TestTokenProjectBindingForbidden() {
	projectB := s.newForeignProject()
	token := "emt_868_mcp_binding"
	s.Require().NoError(testutil.CreateTestAPIToken(s.Ctx, s.DB(),
		testutil.AdminUser.ID, token, []string{"mcp:admin"}, s.ProjectID))

	resp := s.Client.POST("/api/mcp/rpc",
		testutil.WithAuth(token), testutil.WithProjectID(projectB),
		testutil.WithJSONBody(initBody("")))
	s.Require().Equal(http.StatusForbidden, resp.StatusCode,
		"project token addressing a different project must be 403, got %d: %s", resp.StatusCode, resp.String())
}

// TestInitializeClaimReconciled proves the session-bound decision: a session
// caller who passes the header check (own project) cannot then claim a foreign
// project via the initialize params. The handler reconciles the claim and
// returns a JSON-RPC forbidden error rather than a bound session.
func (s *MCPMembershipSuite) TestInitializeClaimReconciled() {
	projectB := s.newForeignProject()
	resp := s.Client.POST("/api/mcp/rpc",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(initBody(projectB)))
	s.Require().Equal(http.StatusOK, resp.StatusCode,
		"reconcile denial surfaces as a JSON-RPC error (HTTP 200), got %d: %s", resp.StatusCode, resp.String())

	var body struct {
		JSONRPC string `json:"jsonrpc"`
		ID      any    `json:"id"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	s.Require().NoError(json.Unmarshal(resp.Body, &body))
	s.Require().NotNil(body.Error, "expected a JSON-RPC error, got %s", resp.String())
	s.Require().Equal(-32002, body.Error.Code,
		"initialize claim for a foreign project must be forbidden (-32002), got %d", body.Error.Code)
}
