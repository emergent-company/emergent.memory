package agents_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// A2AProjectAuthzSuite is the end-to-end half of the #660 fix: it runs the real
// A2A route set against real rows (projects, org memberships, API tokens) so the
// header-addressed project is authorized the way production authorizes it.
//
// Fixtures from BaseSuite: org A with the admin user as org_admin member, and
// project A inside it. A foreign org B with project B is created on demand; the
// admin user is a member of neither.
type A2AProjectAuthzSuite struct {
	testutil.BaseSuite
}

func TestA2AProjectAuthzSuite(t *testing.T) {
	suite.Run(t, new(A2AProjectAuthzSuite))
}

func (s *A2AProjectAuthzSuite) SetupSuite() {
	s.SetDBSuffix("a2a_project_authz")
	s.BaseSuite.SetupSuite()
}

// foreignProject creates org B and project B, plus an external agent definition
// that only project B can see. It returns the org and project ids.
func (s *A2AProjectAuthzSuite) foreignProject() (orgID, projectID string) {
	orgB := uuid.New().String()
	s.Require().NoError(testutil.CreateTestOrganization(s.Ctx, s.DB(), orgB, "Org B"))

	projectB := uuid.New().String()
	s.Require().NoError(testutil.CreateTestProject(s.Ctx, s.DB(), testutil.TestProject{
		ID:    projectB,
		OrgID: orgB,
		Name:  "Project B",
	}, testutil.AdminUser.ID))

	// An external agent definition in B. It is the observable that proves which
	// project a request actually addressed: only a request scoped to B returns it.
	repo := agents.NewRepository(s.DB())
	s.Require().NoError(repo.CreateDefinition(s.Ctx, &agents.AgentDefinition{
		ProjectID:  projectB,
		Name:       "b-only-agent",
		Visibility: agents.VisibilityExternal,
		Enabled:    true,
		Tools:      []string{},
		Skills:     []string{},
	}))

	return orgB, projectB
}

const a2aSendBody = `{"message":{"messageId":"m1","role":"ROLE_USER","parts":[{"text":"hi"}]}}`

// a2aGuardedRequest is one A2A operation that addresses a project.
// `okStatuses` is what a correctly authorized caller sees on its own project:
// the handler's own outcome (validation error, missing task), never a 403.
type a2aGuardedRequest struct {
	name       string
	method     string
	path       func(taskID string) string
	body       string
	okStatuses []int
}

var a2aGuardedRequests = []a2aGuardedRequest{
	{
		name:       "message:send",
		method:     http.MethodPost,
		path:       func(string) string { return "/message:send" },
		body:       `{}`, // decoded, then rejected by the message validator
		okStatuses: []int{http.StatusBadRequest},
	},
	{
		name:       "message:stream",
		method:     http.MethodPost,
		path:       func(string) string { return "/message:stream" },
		body:       `{}`,
		okStatuses: []int{http.StatusBadRequest},
	},
	{
		name:       "tasks list",
		method:     http.MethodGet,
		path:       func(string) string { return "/tasks" },
		okStatuses: []int{http.StatusOK},
	},
	{
		name:       "task get",
		method:     http.MethodGet,
		path:       func(id string) string { return "/tasks/" + id },
		okStatuses: []int{http.StatusNotFound},
	},
	{
		name:       "task cancel",
		method:     http.MethodPost,
		path:       func(id string) string { return "/tasks/" + id + ":cancel" },
		okStatuses: []int{http.StatusNotFound},
	},
	{
		name:       "task subscribe",
		method:     http.MethodPost,
		path:       func(id string) string { return "/tasks/" + id + ":subscribe" },
		okStatuses: []int{http.StatusNotFound},
	},
	{
		name:       "extended agent card",
		method:     http.MethodGet,
		path:       func(string) string { return "/extendedAgentCard" },
		okStatuses: []int{http.StatusOK},
	},
}

func (s *A2AProjectAuthzSuite) do(req a2aGuardedRequest, taskID string, opts ...testutil.RequestOption) *testutil.HTTPResponse {
	if req.body != "" {
		opts = append(opts, testutil.WithBody(req.body), testutil.WithJSON())
	}
	return s.Client.Request(req.method, req.path(taskID), opts...)
}

// TestNonMemberCannotAddressForeignProject is the #660 regression guard, at the
// full-server level: the caller is an authenticated session (the e2e test token
// maps to a real user, authenticated without an API-token binding, exactly like
// an OIDC session), is a member of project A only, and sends project B's id in
// X-Project-ID. Every guarded operation must answer 403 PERMISSION_DENIED.
func (s *A2AProjectAuthzSuite) TestNonMemberCannotAddressForeignProject() {
	_, projectB := s.foreignProject()
	taskID := uuid.New().String()

	for _, req := range a2aGuardedRequests {
		s.Run(req.name, func() {
			resp := s.do(req, taskID,
				testutil.WithAuth("e2e-test-user"),
				testutil.WithProjectID(projectB),
			)

			s.Require().Equal(http.StatusForbidden, resp.StatusCode,
				"%s must be refused for a non-member addressing project B, got %d: %s",
				req.name, resp.StatusCode, resp.String())
			s.Require().Contains(resp.String(), "PERMISSION_DENIED",
				"%s must answer in the A2A envelope: %s", req.name, resp.String())
		})
	}
}

// TestMemberOfAddressedProjectSucceeds is the positive control: the same session
// addressing its own project gets past authorization and reaches each handler.
func (s *A2AProjectAuthzSuite) TestMemberOfAddressedProjectSucceeds() {
	taskID := uuid.New().String()

	for _, req := range a2aGuardedRequests {
		s.Run(req.name, func() {
			resp := s.do(req, taskID,
				testutil.WithAuth("e2e-test-user"),
				testutil.WithProjectID(s.ProjectID),
			)

			s.Require().Contains(req.okStatuses, resp.StatusCode,
				"%s must reach its handler for a member of the addressed project, got %d: %s",
				req.name, resp.StatusCode, resp.String())
		})
	}
}

// TestGlobalAgentCardStaysUnauthenticated: discovery remains open. It is the one
// route the issue explicitly exempts.
func (s *A2AProjectAuthzSuite) TestGlobalAgentCardStaysUnauthenticated() {
	resp := s.Client.GET("/.well-known/agent-card.json")
	s.Require().Equal(http.StatusOK, resp.StatusCode, resp.String())

	var card agents.AgentCard
	s.Require().NoError(json.Unmarshal([]byte(resp.String()), &card))
	s.Require().Equal(agents.A2APlatformName, card.Name)
}

// TestProjectBoundTokenCannotBeRedirected: a project-bound emt_* token keeps
// working exactly as before — the token's project wins over the header, so a
// header naming another project cannot move the request. The foreign project
// holds an external agent definition, which the card would list if the header
// were honoured.
func (s *A2AProjectAuthzSuite) TestProjectBoundTokenCannotBeRedirected() {
	orgB, projectB := s.foreignProject()

	// Control: once the admin user is a member of B, addressing B as a session
	// advertises B's external agent, so the fixture is observable and the
	// negative below is meaningful.
	s.Require().NoError(testutil.CreateTestOrgMembership(s.Ctx, s.DB(), orgB, testutil.AdminUser.ID, "org_admin"))

	control := s.Client.GET("/extendedAgentCard",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(projectB))
	s.Require().Equal(http.StatusOK, control.StatusCode, control.String())
	s.Require().Contains(control.String(), "b-only-agent",
		"project B's external agent must be advertised when the request addresses B: %s", control.String())

	// The token bound to A names B in the header: the request still addresses A,
	// so B's agent must not appear.
	tokenA := "emt_660_bound_project_a"
	s.Require().NoError(testutil.CreateTestAPIToken(s.Ctx, s.DB(),
		testutil.AdminUser.ID, tokenA, []string{"agents:read"}, s.ProjectID))

	resp := s.Client.GET("/extendedAgentCard",
		testutil.WithAuth(tokenA), testutil.WithProjectID(projectB))
	s.Require().Equal(http.StatusOK, resp.StatusCode, resp.String())
	s.Require().NotContains(resp.String(), "b-only-agent",
		"a project-bound token must not be redirected by X-Project-ID: %s", resp.String())
}

// TestProjectBoundTokenStillWorks: the token keeps its existing read/write
// split; the guard did not turn project tokens into session tokens.
func (s *A2AProjectAuthzSuite) TestProjectBoundTokenStillWorks() {
	tokenRead := "emt_660_read_only"
	s.Require().NoError(testutil.CreateTestAPIToken(s.Ctx, s.DB(),
		testutil.AdminUser.ID, tokenRead, []string{"agents:read"}, s.ProjectID))

	resp := s.Client.GET("/tasks", testutil.WithAuth(tokenRead), testutil.WithProjectID(s.ProjectID))
	s.Require().Equal(http.StatusOK, resp.StatusCode, resp.String())

	// agents:read is not agents:write: the write route stays closed for it.
	resp = s.Client.POST("/message:send",
		testutil.WithAuth(tokenRead), testutil.WithProjectID(s.ProjectID),
		testutil.WithBody(a2aSendBody), testutil.WithJSON())
	s.Require().Equal(http.StatusForbidden, resp.StatusCode, resp.String())
	s.Require().Contains(resp.String(), "PERMISSION_DENIED", resp.String())
}

// TestSessionWithoutAgentsScopeIsRefused is the second half of #660: before the
// fix the scope gate was a no-op for every non-emt_ credential. "read-only" is a
// development test token authenticated as a plain session (no APITokenID) whose
// scope set carries no agents:* scope, so both the read and the write route must
// now refuse it.
func (s *A2AProjectAuthzSuite) TestSessionWithoutAgentsScopeIsRefused() {
	for _, req := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "tasks list", method: http.MethodGet, path: "/tasks"},
		{name: "message:send", method: http.MethodPost, path: "/message:send", body: a2aSendBody},
	} {
		s.Run(req.name, func() {
			opts := []testutil.RequestOption{
				testutil.WithAuth("read-only"),
				testutil.WithProjectID(s.ProjectID),
			}
			if req.body != "" {
				opts = append(opts, testutil.WithBody(req.body), testutil.WithJSON())
			}
			resp := s.Client.Request(req.method, req.path, opts...)

			s.Require().Equal(http.StatusForbidden, resp.StatusCode,
				"%s must refuse a session without an agents:* scope, got %d: %s",
				req.name, resp.StatusCode, resp.String())
			s.Require().Contains(resp.String(), "PERMISSION_DENIED", resp.String())
		})
	}
}

// TestSessionWithoutProjectSelectorIsProjectRequired keeps the pre-existing
// contract: an authenticated request with no selector is a client error.
func (s *A2AProjectAuthzSuite) TestSessionWithoutProjectSelectorIsProjectRequired() {
	resp := s.Client.GET("/tasks", testutil.WithAuth("e2e-test-user"))
	s.Require().Equal(http.StatusBadRequest, resp.StatusCode, resp.String())
	s.Require().Contains(resp.String(), "PROJECT_REQUIRED", resp.String())
}
