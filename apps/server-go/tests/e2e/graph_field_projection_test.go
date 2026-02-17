package e2e

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/emergent/emergent-core/domain/graph"
	"github.com/emergent/emergent-core/internal/testutil"
)

type GraphFieldProjectionSuite struct {
	testutil.BaseSuite
}

func (s *GraphFieldProjectionSuite) SetupSuite() {
	s.SetDBSuffix("graph_field_projection")
	s.BaseSuite.SetupSuite()
}

// TestListObjects_FieldProjection verifies that the fields parameter filters properties.
func (s *GraphFieldProjectionSuite) TestListObjects_FieldProjection() {
	// Create an object with multiple properties
	rec := s.Client.POST(
		"/api/graph/objects",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"type": "Task",
			"properties": map[string]any{
				"title":       "Build Feature",
				"description": "Build the new feature for the product",
				"priority":    "high",
				"estimate":    5,
			},
		}),
	)
	s.Require().Equal(http.StatusCreated, rec.StatusCode, "Response: %s", rec.String())

	// List with field projection - only request title and priority
	rec = s.Client.GET(
		"/api/graph/objects/search?type=Task&fields=title,priority",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, rec.StatusCode, "Response: %s", rec.String())

	var response graph.SearchGraphObjectsResponse
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.Require().GreaterOrEqual(len(response.Items), 1)
	item := response.Items[0]

	// Should include requested fields
	s.Contains(item.Properties, "title")
	s.Contains(item.Properties, "priority")

	// Should NOT include non-requested fields
	s.NotContains(item.Properties, "description")
	s.NotContains(item.Properties, "estimate")

	// Metadata fields should still be present (they're on the response struct, not in properties)
	s.NotEmpty(item.ID)
	s.Equal("Task", item.Type)
}

// TestListObjects_NoFieldProjection returns all properties when fields is omitted.
func (s *GraphFieldProjectionSuite) TestListObjects_NoFieldProjection() {
	// Create an object with multiple properties
	rec := s.Client.POST(
		"/api/graph/objects",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"type": "Requirement",
			"properties": map[string]any{
				"title":       "Auth Requirement",
				"description": "Users must authenticate",
			},
		}),
	)
	s.Require().Equal(http.StatusCreated, rec.StatusCode, "Response: %s", rec.String())

	// List without field projection
	rec = s.Client.GET(
		"/api/graph/objects/search?type=Requirement",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, rec.StatusCode, "Response: %s", rec.String())

	var response graph.SearchGraphObjectsResponse
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.Require().GreaterOrEqual(len(response.Items), 1)
	item := response.Items[0]

	// All properties should be present
	s.Contains(item.Properties, "title")
	s.Contains(item.Properties, "description")
}

func TestGraphFieldProjectionSuite(t *testing.T) {
	suite.Run(t, new(GraphFieldProjectionSuite))
}
