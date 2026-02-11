package e2e

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/emergent/emergent-core/domain/graph"
	"github.com/emergent/emergent-core/internal/testutil"
)

type GraphAnalyticsSuite struct {
	testutil.BaseSuite
}

func (s *GraphAnalyticsSuite) SetupSuite() {
	s.SetDBSuffix("graph_analytics")
	s.BaseSuite.SetupSuite()
}

func (s *GraphAnalyticsSuite) SetupTest() {
	s.BaseSuite.SetupTest()
	s.CreateTestObjects()
}

func (s *GraphAnalyticsSuite) CreateTestObjects() {
	for i := 0; i < 5; i++ {
		rec := s.Client.POST(
			"/api/v2/graph/objects",
			testutil.WithAuth("e2e-test-user"),
			testutil.WithProjectID(s.ProjectID),
			testutil.WithJSONBody(map[string]any{
				"type": "TestObject",
				"properties": map[string]any{
					"name": "Test Object " + string(rune('A'+i)),
				},
			}),
		)
		s.Require().Equal(http.StatusCreated, rec.StatusCode)
	}
}

func (s *GraphAnalyticsSuite) TestGetMostAccessed_Success() {
	rec := s.Client.POST(
		"/api/v2/graph/search",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"query": "Test",
			"limit": 5,
		}),
	)
	s.Require().Equal(http.StatusOK, rec.StatusCode)

	time.Sleep(100 * time.Millisecond)

	rec = s.Client.GET(
		"/api/v2/graph/analytics/most-accessed",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, rec.StatusCode, "Response: %s", rec.String())

	var response graph.MostAccessedResponse
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.GreaterOrEqual(len(response.Items), 1, "Should have at least one accessed object")
	s.GreaterOrEqual(response.Total, 1)

	if len(response.Items) > 0 {
		s.NotEmpty(response.Items[0].ID)
		s.NotNil(response.Items[0].LastAccessedAt)
		s.Equal("TestObject", response.Items[0].Type)
	}

	s.NotNil(response.Meta)
	s.NotNil(response.Meta["limit"])
}

func (s *GraphAnalyticsSuite) TestGetMostAccessed_WithLimit() {
	rec := s.Client.POST(
		"/api/v2/graph/search",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"query": "Test",
			"limit": 5,
		}),
	)
	s.Require().Equal(http.StatusOK, rec.StatusCode)

	time.Sleep(100 * time.Millisecond)

	rec = s.Client.GET(
		"/api/v2/graph/analytics/most-accessed?limit=2",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, rec.StatusCode)

	var response graph.MostAccessedResponse
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.LessOrEqual(len(response.Items), 2, "Should respect limit parameter")
	s.Equal(2, response.Meta["limit"])
}

func (s *GraphAnalyticsSuite) TestGetMostAccessed_RequiresAuth() {
	rec := s.Client.GET(
		"/api/v2/graph/analytics/most-accessed",
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusUnauthorized, rec.StatusCode)
}

func (s *GraphAnalyticsSuite) TestGetMostAccessed_RequiresProjectID() {
	rec := s.Client.GET(
		"/api/v2/graph/analytics/most-accessed",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusBadRequest, rec.StatusCode)
}

func (s *GraphAnalyticsSuite) TestGetUnused_Success() {
	rec := s.Client.GET(
		"/api/v2/graph/analytics/unused",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, rec.StatusCode, "Response: %s", rec.String())

	var response graph.UnusedObjectsResponse
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.GreaterOrEqual(len(response.Items), 5, "All test objects should be unused (never accessed)")
	s.GreaterOrEqual(response.Total, 5)

	for _, item := range response.Items {
		s.NotEmpty(item.ID)
		s.Equal("TestObject", item.Type)
		s.Nil(item.LastAccessedAt, "Unused objects should have NULL last_accessed_at")
	}

	s.NotNil(response.Meta)
	s.Equal(50, response.Meta["limit"])
	s.Equal(30, response.Meta["daysThreshold"])
}

func (s *GraphAnalyticsSuite) TestGetUnused_WithDaysThreshold() {
	rec := s.Client.GET(
		"/api/v2/graph/analytics/unused?days=7",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, rec.StatusCode)

	var response graph.UnusedObjectsResponse
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.Equal(7, response.Meta["daysThreshold"])
}

func (s *GraphAnalyticsSuite) TestGetUnused_ExcludesRecentlyAccessed() {
	rec := s.Client.POST(
		"/api/v2/graph/search",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"query": "Test Object A",
			"limit": 1,
		}),
	)
	s.Require().Equal(http.StatusOK, rec.StatusCode)

	time.Sleep(100 * time.Millisecond)

	rec = s.Client.GET(
		"/api/v2/graph/analytics/unused?days=1",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, rec.StatusCode)

	var response graph.UnusedObjectsResponse
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	for _, item := range response.Items {
		if item.Properties["name"] == "Test Object A" {
			s.NotNil(item.LastAccessedAt, "Recently accessed object should have timestamp")
			s.NotNil(item.DaysSinceAccess, "Should calculate days since access")
		}
	}
}

func (s *GraphAnalyticsSuite) TestGetUnused_RequiresAuth() {
	rec := s.Client.GET(
		"/api/v2/graph/analytics/unused",
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusUnauthorized, rec.StatusCode)
}

func (s *GraphAnalyticsSuite) TestGetUnused_RequiresProjectID() {
	rec := s.Client.GET(
		"/api/v2/graph/analytics/unused",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusBadRequest, rec.StatusCode)
}

func TestGraphAnalyticsSuite(t *testing.T) {
	suite.Run(t, new(GraphAnalyticsSuite))
}
