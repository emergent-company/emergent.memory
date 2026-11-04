package agents_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// VisibilityValidationSuite exercises the agent-definition write paths through
// the full in-process server. Before the fix (issue #889) an out-of-enum
// visibility like "public" was accepted and persisted; after the fix both
// create and update refuse it with a 400, and sloppy-but-valid input
// (mixed-case/whitespace) is canonicalized rather than stored verbatim.
type VisibilityValidationSuite struct {
	testutil.BaseSuite
}

func TestVisibilityValidationSuite(t *testing.T) {
	suite.Run(t, new(VisibilityValidationSuite))
}

func (s *VisibilityValidationSuite) SetupSuite() {
	s.SetDBSuffix("agent_visibility_validation")
	s.BaseSuite.SetupSuite()
}

func (s *VisibilityValidationSuite) defsPath() string {
	return "/api/projects/" + s.ProjectID + "/agent-definitions"
}

// createDefinition POSTs an agent definition and returns the raw response body.
func (s *VisibilityValidationSuite) createDefinition(body map[string]any) *testutil.HTTPResponse {
	return s.Client.POST(s.defsPath(),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(body),
	)
}

// definitionID extracts data.id from a create response.
func (s *VisibilityValidationSuite) definitionID(resp *testutil.HTTPResponse) string {
	var wrapper struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	s.Require().NoError(json.Unmarshal(resp.Body, &wrapper))
	return wrapper.Data.ID
}

// TestCreateInvalidVisibilityRefused is the fail-first test: an invalid
// visibility must be refused with 400 and never persisted.
func (s *VisibilityValidationSuite) TestCreateInvalidVisibilityRefused() {
	resp := s.createDefinition(map[string]any{"name": "bad-vis", "visibility": "public"})
	s.Require().Equal(http.StatusBadRequest, resp.StatusCode,
		"invalid visibility must be refused, got %d: %s", resp.StatusCode, resp.String())
}

// TestCreateMixedCaseVisibilityCanonicalized verifies sloppy-but-valid input is
// normalized to the canonical lowercase level rather than rejected or echoed.
func (s *VisibilityValidationSuite) TestCreateMixedCaseVisibilityCanonicalized() {
	resp := s.createDefinition(map[string]any{"name": "mixed-case-vis", "visibility": "  External "})
	s.Require().Equal(http.StatusCreated, resp.StatusCode,
		"valid visibility must be accepted, got %d: %s", resp.StatusCode, resp.String())
	var wrapper struct {
		Data struct {
			Visibility string `json:"visibility"`
		} `json:"data"`
	}
	s.Require().NoError(json.Unmarshal(resp.Body, &wrapper))
	s.Require().Equal("external", wrapper.Data.Visibility,
		"mixed-case/whitespace visibility must normalize to external")
}

// TestUpdateInvalidVisibilityRefused verifies the update path also rejects an
// out-of-enum visibility.
func (s *VisibilityValidationSuite) TestUpdateInvalidVisibilityRefused() {
	createResp := s.createDefinition(map[string]any{"name": "updatable", "visibility": "project"})
	s.Require().Equal(http.StatusCreated, createResp.StatusCode, "setup create failed: %s", createResp.String())
	id := s.definitionID(createResp)
	s.Require().NotEmpty(id)

	resp := s.Client.PATCH(s.defsPath()+"/"+id,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{"visibility": "public"}),
	)
	s.Require().Equal(http.StatusBadRequest, resp.StatusCode,
		"invalid visibility on update must be refused, got %d: %s", resp.StatusCode, resp.String())
}
