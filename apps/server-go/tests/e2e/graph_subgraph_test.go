package e2e

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent/domain/graph"
	"github.com/emergent-company/emergent/internal/testutil"
)

type GraphSubgraphSuite struct {
	testutil.BaseSuite
}

func (s *GraphSubgraphSuite) SetupSuite() {
	s.SetDBSuffix("graph_subgraph")
	s.BaseSuite.SetupSuite()
}

// TestCreateSubgraph_Success creates a simple subgraph with objects and relationships.
func (s *GraphSubgraphSuite) TestCreateSubgraph_Success() {
	rec := s.Client.POST(
		"/api/graph/subgraph",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"objects": []map[string]any{
				{"_ref": "spec1", "type": "Spec", "key": stringPtr("auth-spec"), "properties": map[string]any{"title": "Auth Spec"}},
				{"_ref": "req1", "type": "Requirement", "key": stringPtr("req-login"), "properties": map[string]any{"title": "Login"}},
				{"_ref": "req2", "type": "Requirement", "key": stringPtr("req-logout"), "properties": map[string]any{"title": "Logout"}},
			},
			"relationships": []map[string]any{
				{"type": "has_requirement", "src_ref": "spec1", "dst_ref": "req1"},
				{"type": "has_requirement", "src_ref": "spec1", "dst_ref": "req2"},
			},
		}),
	)

	s.Equal(http.StatusCreated, rec.StatusCode, "Response: %s", rec.String())

	var response graph.CreateSubgraphResponse
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	// Verify objects
	s.Len(response.Objects, 3)
	s.Equal("Spec", response.Objects[0].Type)
	s.Equal("Requirement", response.Objects[1].Type)
	s.Equal("Requirement", response.Objects[2].Type)

	// Verify relationships
	s.Len(response.Relationships, 2)
	s.Equal("has_requirement", response.Relationships[0].Type)
	s.Equal("has_requirement", response.Relationships[1].Type)

	// Verify ref_map
	s.Len(response.RefMap, 3)
	s.Contains(response.RefMap, "spec1")
	s.Contains(response.RefMap, "req1")
	s.Contains(response.RefMap, "req2")

	// Verify that relationship endpoints match the ref_map
	spec1ID := response.RefMap["spec1"]
	req1ID := response.RefMap["req1"]
	req2ID := response.RefMap["req2"]

	s.Equal(spec1ID, response.Relationships[0].SrcID)
	s.Equal(req1ID, response.Relationships[0].DstID)
	s.Equal(spec1ID, response.Relationships[1].SrcID)
	s.Equal(req2ID, response.Relationships[1].DstID)
}

// TestCreateSubgraph_ObjectsOnly creates a subgraph with objects but no relationships.
func (s *GraphSubgraphSuite) TestCreateSubgraph_ObjectsOnly() {
	rec := s.Client.POST(
		"/api/graph/subgraph",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"objects": []map[string]any{
				{"_ref": "a", "type": "Task", "properties": map[string]any{"title": "Task A"}},
				{"_ref": "b", "type": "Task", "properties": map[string]any{"title": "Task B"}},
			},
		}),
	)

	s.Equal(http.StatusCreated, rec.StatusCode, "Response: %s", rec.String())

	var response graph.CreateSubgraphResponse
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.Len(response.Objects, 2)
	s.Len(response.Relationships, 0)
	s.Len(response.RefMap, 2)
}

// TestCreateSubgraph_EmptyObjects returns 400 when objects is empty.
func (s *GraphSubgraphSuite) TestCreateSubgraph_EmptyObjects() {
	rec := s.Client.POST(
		"/api/graph/subgraph",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"objects": []map[string]any{},
		}),
	)

	s.Equal(http.StatusBadRequest, rec.StatusCode)
}

// TestCreateSubgraph_DuplicateRef returns 400 when refs are not unique.
func (s *GraphSubgraphSuite) TestCreateSubgraph_DuplicateRef() {
	rec := s.Client.POST(
		"/api/graph/subgraph",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"objects": []map[string]any{
				{"_ref": "dup", "type": "Task"},
				{"_ref": "dup", "type": "Task"},
			},
		}),
	)

	s.Equal(http.StatusBadRequest, rec.StatusCode)
}

// TestCreateSubgraph_InvalidRef returns 400 when a relationship references an undefined ref.
func (s *GraphSubgraphSuite) TestCreateSubgraph_InvalidRef() {
	rec := s.Client.POST(
		"/api/graph/subgraph",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"objects": []map[string]any{
				{"_ref": "obj1", "type": "Task"},
			},
			"relationships": []map[string]any{
				{"type": "depends_on", "src_ref": "obj1", "dst_ref": "nonexistent"},
			},
		}),
	)

	s.Equal(http.StatusBadRequest, rec.StatusCode)
}

// TestCreateSubgraph_SelfLoop returns 400 when a relationship src_ref == dst_ref.
func (s *GraphSubgraphSuite) TestCreateSubgraph_SelfLoop() {
	rec := s.Client.POST(
		"/api/graph/subgraph",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"objects": []map[string]any{
				{"_ref": "obj1", "type": "Task"},
			},
			"relationships": []map[string]any{
				{"type": "depends_on", "src_ref": "obj1", "dst_ref": "obj1"},
			},
		}),
	)

	s.Equal(http.StatusBadRequest, rec.StatusCode)
}

// TestCreateSubgraph_Unauthorized returns 401 without auth.
func (s *GraphSubgraphSuite) TestCreateSubgraph_Unauthorized() {
	rec := s.Client.POST(
		"/api/graph/subgraph",
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"objects": []map[string]any{
				{"_ref": "obj1", "type": "Task"},
			},
		}),
	)

	s.Equal(http.StatusUnauthorized, rec.StatusCode)
}

// TestCreateSubgraph_ObjectsRetrievable verifies that subgraph objects can be retrieved individually.
func (s *GraphSubgraphSuite) TestCreateSubgraph_ObjectsRetrievable() {
	rec := s.Client.POST(
		"/api/graph/subgraph",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"objects": []map[string]any{
				{"_ref": "r1", "type": "Requirement", "properties": map[string]any{"title": "Test Requirement"}},
			},
		}),
	)

	s.Require().Equal(http.StatusCreated, rec.StatusCode, "Response: %s", rec.String())

	var response graph.CreateSubgraphResponse
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	// Retrieve the created object
	objID := response.RefMap["r1"].String()
	getRec := s.Client.GET(
		"/api/graph/objects/"+objID,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)

	s.Equal(http.StatusOK, getRec.StatusCode, "Response: %s", getRec.String())

	var obj graph.GraphObjectResponse
	err = json.Unmarshal(getRec.Body, &obj)
	s.Require().NoError(err)
	s.Equal("Requirement", obj.Type)
	s.Equal("Test Requirement", obj.Properties["title"])
}

// TestCreateSubgraph_SameBranch creates a subgraph with objects on the same branch (should succeed).
func (s *GraphSubgraphSuite) TestCreateSubgraph_SameBranch() {
	branchID := "00000000-0000-0000-0000-000000000099"
	rec := s.Client.POST(
		"/api/graph/subgraph",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"objects": []map[string]any{
				{"_ref": "a", "type": "Task", "branch_id": branchID, "properties": map[string]any{"title": "Task A"}},
				{"_ref": "b", "type": "Task", "branch_id": branchID, "properties": map[string]any{"title": "Task B"}},
			},
			"relationships": []map[string]any{
				{"type": "depends_on", "src_ref": "a", "dst_ref": "b"},
			},
		}),
	)

	s.Equal(http.StatusCreated, rec.StatusCode, "Response: %s", rec.String())

	var response graph.CreateSubgraphResponse
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.Len(response.Objects, 2)
	s.Len(response.Relationships, 1)
}

// TestCreateSubgraph_DifferentBranches returns 400 when objects are on different branches.
func (s *GraphSubgraphSuite) TestCreateSubgraph_DifferentBranches() {
	branchA := "00000000-0000-0000-0000-000000000001"
	branchB := "00000000-0000-0000-0000-000000000002"
	rec := s.Client.POST(
		"/api/graph/subgraph",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"objects": []map[string]any{
				{"_ref": "a", "type": "Task", "branch_id": branchA, "properties": map[string]any{"title": "Task A"}},
				{"_ref": "b", "type": "Task", "branch_id": branchB, "properties": map[string]any{"title": "Task B"}},
			},
			"relationships": []map[string]any{
				{"type": "depends_on", "src_ref": "a", "dst_ref": "b"},
			},
		}),
	)

	s.Equal(http.StatusBadRequest, rec.StatusCode)
}

// TestCreateSubgraph_NilBranches succeeds when all objects have nil branch_id (main branch).
func (s *GraphSubgraphSuite) TestCreateSubgraph_NilBranches() {
	rec := s.Client.POST(
		"/api/graph/subgraph",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"objects": []map[string]any{
				{"_ref": "a", "type": "Task", "properties": map[string]any{"title": "Task A"}},
				{"_ref": "b", "type": "Task", "properties": map[string]any{"title": "Task B"}},
			},
			"relationships": []map[string]any{
				{"type": "depends_on", "src_ref": "a", "dst_ref": "b"},
			},
		}),
	)

	s.Equal(http.StatusCreated, rec.StatusCode, "Response: %s", rec.String())

	var response graph.CreateSubgraphResponse
	err := json.Unmarshal(rec.Body, &response)
	s.Require().NoError(err)

	s.Len(response.Objects, 2)
	s.Len(response.Relationships, 1)
}

func stringPtr(s string) *string {
	return &s
}

func TestGraphSubgraphSuite(t *testing.T) {
	suite.Run(t, new(GraphSubgraphSuite))
}
