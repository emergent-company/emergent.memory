package agentcompat_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// InternalVisibilitySuite proves the OpenAI-compatible surface cannot resolve or
// invoke an internal-visible agent (issue #939). An internal agent is refused at
// the resolution boundary of /v1/chat/completions, while external and project
// agents resolve normally (the nil test executor surfaces its own 502 past the
// guard, proving only internal is blocked).
type InternalVisibilitySuite struct {
	testutil.BaseSuite
}

func TestInternalVisibilitySuite(t *testing.T) {
	suite.Run(t, new(InternalVisibilitySuite))
}

func (s *InternalVisibilitySuite) SetupSuite() {
	s.SetDBSuffix("agentcompat_internal_visibility")
	s.BaseSuite.SetupSuite()
}

func (s *InternalVisibilitySuite) defsPath() string {
	return "/api/projects/" + s.ProjectID + "/agent-definitions"
}

func (s *InternalVisibilitySuite) createDefinition(name, visibility string) {
	resp := s.Client.POST(s.defsPath(),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{"name": name, "visibility": visibility}),
	)
	s.Require().Equal(http.StatusCreated, resp.StatusCode,
		"create definition %q: %s", name, resp.String())
}

func (s *InternalVisibilitySuite) chat(model string) *testutil.HTTPResponse {
	return s.Client.POST("/v1/chat/completions",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"model":    model,
			"messages": []map[string]any{{"role": "user", "content": "hi"}},
		}),
	)
}

// TestInternalAgentNotResolvableViaChatCompletions is the fail-first test for
// the agentcompat resolution hole: an internal agent must not be reachable via
// the OpenAI-compatible surface.
func (s *InternalVisibilitySuite) TestInternalAgentNotResolvableViaChatCompletions() {
	s.createDefinition("internal-helper", "internal")
	s.createDefinition("public-agent", "external")

	// Internal agent: refused at resolution (400), never reaching the executor.
	internalResp := s.chat("agent:internal-helper")
	s.Require().Equal(http.StatusBadRequest, internalResp.StatusCode,
		"internal agent must be unresolvable, got %d: %s", internalResp.StatusCode, internalResp.String())

	// External agent: resolves past the guard (nil executor → 502), proving the
	// guard blocks only internal agents.
	externalResp := s.chat("agent:public-agent")
	s.Require().Equal(http.StatusBadGateway, externalResp.StatusCode,
		"external agent must resolve (nil executor yields 502), got %d: %s", externalResp.StatusCode, externalResp.String())
}
