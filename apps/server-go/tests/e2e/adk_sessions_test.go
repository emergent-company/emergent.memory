package e2e

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/emergent-company/emergent/internal/testutil"
)

type ADKSessionsSuite struct {
	testutil.BaseSuite
}

func TestADKSessionsSuite(t *testing.T) {
	suite.Run(t, new(ADKSessionsSuite))
}

func (s *ADKSessionsSuite) SetupSuite() {
	s.SetDBSuffix("adk_sessions")
	s.BaseSuite.SetupSuite()
}

func (s *ADKSessionsSuite) TestADKSessionsEndpoints_NotFound() {
	// Without actual ADK events being created in the DB yet by an agent, 
	// we test the basic API surface boundary responses.
	
	// List should return empty for empty project
	rec := s.Client.GET(
		fmt.Sprintf("/api/projects/%s/adk-sessions", s.ProjectID),
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, rec.StatusCode)
	
	// Get should return 404 for invalid session
	rec2 := s.Client.GET(
		fmt.Sprintf("/api/projects/%s/adk-sessions/invalid-id", s.ProjectID),
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusNotFound, rec2.StatusCode)
}
