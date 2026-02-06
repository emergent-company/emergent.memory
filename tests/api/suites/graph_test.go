package suites

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/emergent/api-tests/client"
	"github.com/google/uuid"
)

// GraphTestSuite tests the graph API endpoints.
// These tests create data via API calls (not SQL) for external testing.
type GraphTestSuite struct {
	BaseSuite
	createdObjectIDs       []string // Track created objects for cleanup
	createdRelationshipIDs []string // Track created relationships for cleanup
}

func TestGraphSuite(t *testing.T) {
	RunSuite(t, new(GraphTestSuite))
}

// SetupTest runs before each test.
func (s *GraphTestSuite) SetupTest() {
	s.BaseSuite.SetupTest()
	s.createdObjectIDs = nil
	s.createdRelationshipIDs = nil
}

// TearDownTest cleans up created resources after each test.
func (s *GraphTestSuite) TearDownTest() {
	// Clean up relationships first (they reference objects)
	for _, id := range s.createdRelationshipIDs {
		_, _ = s.Client.DELETE("/api/v2/graph/relationships/"+id,
			s.AdminAuth(),
			s.ProjectHeader(),
		)
	}
	// Then clean up objects
	for _, id := range s.createdObjectIDs {
		_, _ = s.Client.DELETE("/api/v2/graph/objects/"+id,
			s.AdminAuth(),
			s.ProjectHeader(),
		)
	}
}

// createObject is a helper that creates a graph object and tracks it for cleanup.
func (s *GraphTestSuite) createObject(objType string, properties map[string]any) (string, map[string]any) {
	body := map[string]any{
		"type": objType,
	}
	if properties != nil {
		body["properties"] = properties
	}

	resp, err := s.Client.POST("/api/v2/graph/objects", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)
	s.Require().NoError(err)
	s.Require().Equal(http.StatusCreated, resp.StatusCode,
		"Expected 201, got %d: %s", resp.StatusCode, resp.BodyString())

	var obj map[string]any
	err = resp.JSON(&obj)
	s.Require().NoError(err)

	id := obj["id"].(string)
	s.createdObjectIDs = append(s.createdObjectIDs, id)
	return id, obj
}

// createRelationship is a helper that creates a relationship and tracks it for cleanup.
func (s *GraphTestSuite) createRelationship(relType, srcID, dstID string, properties map[string]any) (string, map[string]any) {
	body := map[string]any{
		"type":   relType,
		"src_id": srcID,
		"dst_id": dstID,
	}
	if properties != nil {
		body["properties"] = properties
	}

	resp, err := s.Client.POST("/api/v2/graph/relationships", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)
	s.Require().NoError(err)
	s.Require().Equal(http.StatusCreated, resp.StatusCode,
		"Expected 201, got %d: %s", resp.StatusCode, resp.BodyString())

	var rel map[string]any
	err = resp.JSON(&rel)
	s.Require().NoError(err)

	id := rel["id"].(string)
	s.createdRelationshipIDs = append(s.createdRelationshipIDs, id)
	return id, rel
}

// =============================================================================
// Test: Create Object
// =============================================================================

func (s *GraphTestSuite) TestCreateObject_Success() {
	body := map[string]any{
		"type":   "Requirement",
		"status": "draft",
		"properties": map[string]any{
			"title":       "User Authentication",
			"description": "Implement user auth flow",
		},
		"labels": []string{"security", "mvp"},
	}

	resp, err := s.Client.POST("/api/v2/graph/objects", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusCreated, resp.StatusCode, "Response: %s", resp.BodyString())

	obj, err := resp.JSONMap()
	s.Require().NoError(err)

	// Track for cleanup
	s.createdObjectIDs = append(s.createdObjectIDs, obj["id"].(string))

	s.NotEmpty(obj["id"])
	s.Equal("Requirement", obj["type"])
	s.Equal("draft", obj["status"])
	s.Equal(float64(1), obj["version"])
	s.Equal(obj["id"], obj["canonical_id"])

	props := obj["properties"].(map[string]any)
	s.Equal("User Authentication", props["title"])
}

func (s *GraphTestSuite) TestCreateObject_RequiresAuth() {
	body := map[string]any{
		"type": "Requirement",
	}

	resp, err := s.Client.POST("/api/v2/graph/objects", body,
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *GraphTestSuite) TestCreateObject_RequiresProjectID() {
	body := map[string]any{
		"type": "Requirement",
	}

	resp, err := s.Client.POST("/api/v2/graph/objects", body,
		s.AdminAuth(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *GraphTestSuite) TestCreateObject_MissingType() {
	body := map[string]any{
		"status": "draft",
	}

	resp, err := s.Client.POST("/api/v2/graph/objects", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *GraphTestSuite) TestCreateObject_MinimalFields() {
	body := map[string]any{
		"type": "Task",
	}

	resp, err := s.Client.POST("/api/v2/graph/objects", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusCreated, resp.StatusCode)

	obj, err := resp.JSONMap()
	s.Require().NoError(err)

	// Track for cleanup
	s.createdObjectIDs = append(s.createdObjectIDs, obj["id"].(string))

	s.Equal("Task", obj["type"])
	s.NotNil(obj["properties"])
	s.NotNil(obj["labels"])
}

// =============================================================================
// Test: Get Object
// =============================================================================

func (s *GraphTestSuite) TestGetObject_Success() {
	// Create an object first
	id, created := s.createObject("Decision", map[string]any{
		"title": "Test Decision",
	})

	// Get the object
	resp, err := s.Client.GET("/api/v2/graph/objects/"+id,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	obj, err := resp.JSONMap()
	s.Require().NoError(err)

	s.Equal(created["id"], obj["id"])
	s.Equal("Decision", obj["type"])
}

func (s *GraphTestSuite) TestGetObject_NotFound() {
	resp, err := s.Client.GET("/api/v2/graph/objects/"+uuid.New().String(),
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusNotFound, resp.StatusCode)
}

// =============================================================================
// Test: List Objects (Search)
// =============================================================================

func (s *GraphTestSuite) TestListObjects_ReturnsObjects() {
	// Create test objects
	s.createObject("Requirement", map[string]any{"title": "List Test 1"})
	s.createObject("Requirement", map[string]any{"title": "List Test 2"})
	s.createObject("Decision", map[string]any{"title": "List Test 3"})

	resp, err := s.Client.GET("/api/v2/graph/objects/search",
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	body, err := resp.JSONMap()
	s.Require().NoError(err)

	s.Contains(body, "items")
	s.Contains(body, "total")

	items := body["items"].([]any)
	s.GreaterOrEqual(len(items), 3)
}

func (s *GraphTestSuite) TestListObjects_FilterByType() {
	// Create objects of different types
	s.createObject("Requirement", nil)
	s.createObject("Decision", nil)
	s.createObject("Requirement", nil)

	resp, err := s.Client.GET("/api/v2/graph/objects/search",
		s.AdminAuth(),
		s.ProjectHeader(),
		client.WithQuery("types", "Decision"),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	body, err := resp.JSONMap()
	s.Require().NoError(err)

	items := body["items"].([]any)
	for _, item := range items {
		obj := item.(map[string]any)
		s.Equal("Decision", obj["type"])
	}
}

func (s *GraphTestSuite) TestListObjects_Pagination_Limit() {
	// Create 5 objects
	for i := 0; i < 5; i++ {
		s.createObject("Requirement", map[string]any{
			"index": i,
		})
	}

	// Request with limit=2
	resp, err := s.Client.GET("/api/v2/graph/objects/search",
		s.AdminAuth(),
		s.ProjectHeader(),
		client.WithQuery("limit", "2"),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	body, err := resp.JSONMap()
	s.Require().NoError(err)

	items := body["items"].([]any)
	s.Len(items, 2)
	s.NotNil(body["next_cursor"])
}

func (s *GraphTestSuite) TestListObjects_Pagination_Cursor() {
	// Create 5 objects
	for i := 0; i < 5; i++ {
		s.createObject("Requirement", map[string]any{
			"index": i,
		})
	}

	// First page
	resp1, err := s.Client.GET("/api/v2/graph/objects/search",
		s.AdminAuth(),
		s.ProjectHeader(),
		client.WithQuery("limit", "2"),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp1.StatusCode)

	page1, err := resp1.JSONMap()
	s.Require().NoError(err)

	items1 := page1["items"].([]any)
	s.Len(items1, 2)
	cursor := page1["next_cursor"].(string)

	// Second page using cursor
	resp2, err := s.Client.GET("/api/v2/graph/objects/search",
		s.AdminAuth(),
		s.ProjectHeader(),
		client.WithQuery("limit", "2"),
		client.WithQuery("cursor", cursor),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp2.StatusCode)

	page2, err := resp2.JSONMap()
	s.Require().NoError(err)

	items2 := page2["items"].([]any)
	s.Len(items2, 2)

	// Verify no duplicates
	allIDs := make(map[string]bool)
	for _, item := range items1 {
		obj := item.(map[string]any)
		id := obj["id"].(string)
		s.False(allIDs[id], "Duplicate ID found")
		allIDs[id] = true
	}
	for _, item := range items2 {
		obj := item.(map[string]any)
		id := obj["id"].(string)
		s.False(allIDs[id], "Duplicate ID found")
		allIDs[id] = true
	}
}

func (s *GraphTestSuite) TestListObjects_InvalidCursor() {
	resp, err := s.Client.GET("/api/v2/graph/objects/search",
		s.AdminAuth(),
		s.ProjectHeader(),
		client.WithQuery("cursor", "invalid-cursor-format"),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

// =============================================================================
// Test: Patch Object
// =============================================================================

func (s *GraphTestSuite) TestPatchObject_Success() {
	// Create an object
	id, _ := s.createObject("Requirement", map[string]any{
		"title": "Original Title",
	})

	// Patch the object
	body := map[string]any{
		"status": "approved",
		"properties": map[string]any{
			"title": "Updated Title",
		},
	}

	resp, err := s.Client.PATCH("/api/v2/graph/objects/"+id, body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode, "Response: %s", resp.BodyString())

	obj, err := resp.JSONMap()
	s.Require().NoError(err)

	// New version should be created
	s.NotEqual(id, obj["id"])
	s.Equal(float64(2), obj["version"])
	s.Equal("approved", obj["status"])

	// Track the new version for cleanup
	s.createdObjectIDs = append(s.createdObjectIDs, obj["id"].(string))

	props := obj["properties"].(map[string]any)
	s.Equal("Updated Title", props["title"])
}

func (s *GraphTestSuite) TestPatchObject_MergesProperties() {
	// Create an object with multiple properties
	id, _ := s.createObject("Requirement", map[string]any{
		"title":       "Original Title",
		"description": "Original Description",
		"priority":    "high",
	})

	// Patch only the title
	body := map[string]any{
		"properties": map[string]any{
			"title": "Updated Title",
		},
	}

	resp, err := s.Client.PATCH("/api/v2/graph/objects/"+id, body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	obj, err := resp.JSONMap()
	s.Require().NoError(err)

	// Track the new version for cleanup
	s.createdObjectIDs = append(s.createdObjectIDs, obj["id"].(string))

	// Original properties should be preserved
	props := obj["properties"].(map[string]any)
	s.Equal("Updated Title", props["title"])
	s.Equal("Original Description", props["description"])
	s.Equal("high", props["priority"])
}

// =============================================================================
// Test: Delete Object
// =============================================================================

func (s *GraphTestSuite) TestDeleteObject_Success() {
	// Create an object
	id, _ := s.createObject("Requirement", nil)

	// Delete the object
	resp, err := s.Client.DELETE("/api/v2/graph/objects/"+id,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	// Remove from cleanup list since it's already deleted
	for i, oid := range s.createdObjectIDs {
		if oid == id {
			s.createdObjectIDs = append(s.createdObjectIDs[:i], s.createdObjectIDs[i+1:]...)
			break
		}
	}

	// Verify object doesn't appear in normal list
	listResp, err := s.Client.GET("/api/v2/graph/objects/search",
		s.AdminAuth(),
		s.ProjectHeader(),
	)
	s.Require().NoError(err)

	body, _ := listResp.JSONMap()
	items := body["items"].([]any)

	for _, item := range items {
		obj := item.(map[string]any)
		// The canonical_id of deleted items should not appear (unless include_deleted)
		s.NotEqual(id, obj["canonical_id"])
	}
}

func (s *GraphTestSuite) TestDeleteObject_NotFound() {
	resp, err := s.Client.DELETE("/api/v2/graph/objects/"+uuid.New().String(),
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusNotFound, resp.StatusCode)
}

// =============================================================================
// Test: Restore Object
// =============================================================================

func (s *GraphTestSuite) TestRestoreObject_Success() {
	// Create and delete an object
	id, _ := s.createObject("Requirement", nil)

	deleteResp, err := s.Client.DELETE("/api/v2/graph/objects/"+id,
		s.AdminAuth(),
		s.ProjectHeader(),
	)
	s.Require().NoError(err)
	s.Require().Equal(http.StatusOK, deleteResp.StatusCode)

	// Get the deleted object to find its tombstone ID
	listResp, err := s.Client.GET("/api/v2/graph/objects/search",
		s.AdminAuth(),
		s.ProjectHeader(),
		client.WithQuery("include_deleted", "true"),
	)
	s.Require().NoError(err)

	body, _ := listResp.JSONMap()
	items := body["items"].([]any)

	// Find the deleted object by canonical_id
	var deletedID string
	for _, item := range items {
		obj := item.(map[string]any)
		if obj["canonical_id"] == id && obj["deleted_at"] != nil {
			deletedID = obj["id"].(string)
			break
		}
	}
	s.Require().NotEmpty(deletedID, "Could not find deleted object")

	// Restore the object
	resp, err := s.Client.POST("/api/v2/graph/objects/"+deletedID+"/restore", nil,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode, "Response: %s", resp.BodyString())

	obj, err := resp.JSONMap()
	s.Require().NoError(err)

	s.Nil(obj["deleted_at"])
	s.createdObjectIDs = append(s.createdObjectIDs, obj["id"].(string))
}

// =============================================================================
// Test: Object History
// =============================================================================

func (s *GraphTestSuite) TestGetObjectHistory_Success() {
	// Create an object
	id, _ := s.createObject("Requirement", map[string]any{
		"title": "Version 1",
	})

	// Update the object to create version 2
	body := map[string]any{
		"properties": map[string]any{
			"title": "Version 2",
		},
	}

	patchResp, err := s.Client.PATCH("/api/v2/graph/objects/"+id, body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)
	s.Require().NoError(err)
	s.Require().Equal(http.StatusOK, patchResp.StatusCode)

	patchObj, _ := patchResp.JSONMap()
	s.createdObjectIDs = append(s.createdObjectIDs, patchObj["id"].(string))

	// Get history
	resp, err := s.Client.GET("/api/v2/graph/objects/"+id+"/history",
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	history, err := resp.JSONMap()
	s.Require().NoError(err)

	versions := history["versions"].([]any)
	s.Len(versions, 2)

	// Versions should be in descending order
	v1 := versions[0].(map[string]any)
	v2 := versions[1].(map[string]any)
	s.Equal(float64(2), v1["version"])
	s.Equal(float64(1), v2["version"])
}

// =============================================================================
// Test: Object Edges
// =============================================================================

func (s *GraphTestSuite) TestGetObjectEdges_Empty() {
	// Create an object
	id, _ := s.createObject("Requirement", nil)

	// Get edges
	resp, err := s.Client.GET("/api/v2/graph/objects/"+id+"/edges",
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	body, err := resp.JSONMap()
	s.Require().NoError(err)

	incoming := body["incoming"].([]any)
	outgoing := body["outgoing"].([]any)
	s.Empty(incoming)
	s.Empty(outgoing)
}

func (s *GraphTestSuite) TestGetObjectEdges_WithRelationships() {
	// Create two objects
	srcID, _ := s.createObject("Requirement", nil)
	dstID, _ := s.createObject("Decision", nil)

	// Create a relationship
	s.createRelationship("DEPENDS_ON", srcID, dstID, nil)

	// Get edges for source object
	resp, err := s.Client.GET("/api/v2/graph/objects/"+srcID+"/edges",
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	body, err := resp.JSONMap()
	s.Require().NoError(err)

	outgoing := body["outgoing"].([]any)
	incoming := body["incoming"].([]any)
	s.Len(outgoing, 1)
	s.Empty(incoming)

	// Get edges for destination object
	resp2, err := s.Client.GET("/api/v2/graph/objects/"+dstID+"/edges",
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp2.StatusCode)

	body2, err := resp2.JSONMap()
	s.Require().NoError(err)

	outgoing2 := body2["outgoing"].([]any)
	incoming2 := body2["incoming"].([]any)
	s.Empty(outgoing2)
	s.Len(incoming2, 1)
}

// =============================================================================
// Test: Create Relationship
// =============================================================================

func (s *GraphTestSuite) TestCreateRelationship_Success() {
	// Create two objects
	srcID, _ := s.createObject("Requirement", nil)
	dstID, _ := s.createObject("Decision", nil)

	// Create a relationship
	body := map[string]any{
		"type":   "DEPENDS_ON",
		"src_id": srcID,
		"dst_id": dstID,
		"properties": map[string]any{
			"reason": "Decision informs requirement",
		},
		"weight": 0.8,
	}

	resp, err := s.Client.POST("/api/v2/graph/relationships", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusCreated, resp.StatusCode, "Response: %s", resp.BodyString())

	rel, err := resp.JSONMap()
	s.Require().NoError(err)

	s.createdRelationshipIDs = append(s.createdRelationshipIDs, rel["id"].(string))

	s.NotEmpty(rel["id"])
	s.Equal("DEPENDS_ON", rel["type"])
	s.Equal(srcID, rel["src_id"])
	s.Equal(dstID, rel["dst_id"])
	s.Equal(float64(1), rel["version"])
	s.Equal(rel["id"], rel["canonical_id"])

	props := rel["properties"].(map[string]any)
	s.Equal("Decision informs requirement", props["reason"])
}

func (s *GraphTestSuite) TestCreateRelationship_RequiresAuth() {
	srcID, _ := s.createObject("Requirement", nil)
	dstID, _ := s.createObject("Decision", nil)

	body := map[string]any{
		"type":   "DEPENDS_ON",
		"src_id": srcID,
		"dst_id": dstID,
	}

	resp, err := s.Client.POST("/api/v2/graph/relationships", body,
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *GraphTestSuite) TestCreateRelationship_MissingType() {
	srcID, _ := s.createObject("Requirement", nil)
	dstID, _ := s.createObject("Decision", nil)

	body := map[string]any{
		"src_id": srcID,
		"dst_id": dstID,
	}

	resp, err := s.Client.POST("/api/v2/graph/relationships", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *GraphTestSuite) TestCreateRelationship_SelfLoopNotAllowed() {
	objID, _ := s.createObject("Requirement", nil)

	body := map[string]any{
		"type":   "DEPENDS_ON",
		"src_id": objID,
		"dst_id": objID, // Same as src
	}

	resp, err := s.Client.POST("/api/v2/graph/relationships", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *GraphTestSuite) TestCreateRelationship_EndpointNotFound() {
	srcID, _ := s.createObject("Requirement", nil)

	body := map[string]any{
		"type":   "DEPENDS_ON",
		"src_id": srcID,
		"dst_id": uuid.New().String(), // Non-existent
	}

	resp, err := s.Client.POST("/api/v2/graph/relationships", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusNotFound, resp.StatusCode)
}

func (s *GraphTestSuite) TestCreateRelationship_Idempotent() {
	// Creating the same relationship twice should return the same relationship
	srcID, _ := s.createObject("Requirement", nil)
	dstID, _ := s.createObject("Decision", nil)

	body := map[string]any{
		"type":   "DEPENDS_ON",
		"src_id": srcID,
		"dst_id": dstID,
		"properties": map[string]any{
			"reason": "test",
		},
	}

	// First creation
	resp1, err := s.Client.POST("/api/v2/graph/relationships", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)
	s.Require().NoError(err)
	s.Require().Equal(http.StatusCreated, resp1.StatusCode)

	rel1, _ := resp1.JSONMap()
	s.createdRelationshipIDs = append(s.createdRelationshipIDs, rel1["id"].(string))

	// Second creation with same properties
	resp2, err := s.Client.POST("/api/v2/graph/relationships", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)
	s.Require().NoError(err)
	s.Require().Equal(http.StatusCreated, resp2.StatusCode)

	rel2, _ := resp2.JSONMap()

	// Should return the same relationship (no new version)
	s.Equal(rel1["id"], rel2["id"])
	s.Equal(rel1["version"], rel2["version"])
}

// =============================================================================
// Test: Get Relationship
// =============================================================================

func (s *GraphTestSuite) TestGetRelationship_Success() {
	srcID, _ := s.createObject("Requirement", nil)
	dstID, _ := s.createObject("Decision", nil)

	relID, created := s.createRelationship("DEPENDS_ON", srcID, dstID, nil)

	// Get the relationship
	resp, err := s.Client.GET("/api/v2/graph/relationships/"+relID,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	rel, err := resp.JSONMap()
	s.Require().NoError(err)

	s.Equal(created["id"], rel["id"])
	s.Equal("DEPENDS_ON", rel["type"])
	s.Equal(srcID, rel["src_id"])
	s.Equal(dstID, rel["dst_id"])
}

func (s *GraphTestSuite) TestGetRelationship_NotFound() {
	resp, err := s.Client.GET("/api/v2/graph/relationships/"+uuid.New().String(),
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusNotFound, resp.StatusCode)
}

// =============================================================================
// Test: List Relationships (Search)
// =============================================================================

func (s *GraphTestSuite) TestListRelationships_FilterByType() {
	srcID, _ := s.createObject("Requirement", nil)
	dstID, _ := s.createObject("Decision", nil)
	dst2ID, _ := s.createObject("Task", nil)

	// Create relationships of different types
	s.createRelationship("DEPENDS_ON", srcID, dstID, nil)
	s.createRelationship("IMPLEMENTS", srcID, dst2ID, nil)

	// Filter by type
	resp, err := s.Client.GET("/api/v2/graph/relationships/search",
		s.AdminAuth(),
		s.ProjectHeader(),
		client.WithQuery("type", "DEPENDS_ON"),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	body, err := resp.JSONMap()
	s.Require().NoError(err)

	data := body["data"].([]any)
	for _, item := range data {
		rel := item.(map[string]any)
		s.Equal("DEPENDS_ON", rel["type"])
	}
}

func (s *GraphTestSuite) TestListRelationships_FilterBySrcID() {
	src1ID, _ := s.createObject("Requirement", nil)
	src2ID, _ := s.createObject("Requirement", nil)
	dstID, _ := s.createObject("Decision", nil)

	// Create relationships from different sources
	s.createRelationship("DEPENDS_ON", src1ID, dstID, nil)
	s.createRelationship("DEPENDS_ON", src2ID, dstID, nil)

	// Filter by src_id
	resp, err := s.Client.GET("/api/v2/graph/relationships/search",
		s.AdminAuth(),
		s.ProjectHeader(),
		client.WithQuery("src_id", src1ID),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	body, err := resp.JSONMap()
	s.Require().NoError(err)

	data := body["data"].([]any)
	s.Len(data, 1)
	rel := data[0].(map[string]any)
	s.Equal(src1ID, rel["src_id"])
}

// =============================================================================
// Test: Patch Relationship
// =============================================================================

func (s *GraphTestSuite) TestPatchRelationship_Success() {
	srcID, _ := s.createObject("Requirement", nil)
	dstID, _ := s.createObject("Decision", nil)

	relID, _ := s.createRelationship("DEPENDS_ON", srcID, dstID, map[string]any{
		"reason": "Initial reason",
	})

	// Patch the relationship
	body := map[string]any{
		"properties": map[string]any{
			"reason": "Updated reason",
		},
		"weight": 0.9,
	}

	resp, err := s.Client.PATCH("/api/v2/graph/relationships/"+relID, body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode, "Response: %s", resp.BodyString())

	rel, err := resp.JSONMap()
	s.Require().NoError(err)

	// New version should be created
	s.NotEqual(relID, rel["id"])
	s.Equal(float64(2), rel["version"])

	// Track new version for cleanup
	s.createdRelationshipIDs = append(s.createdRelationshipIDs, rel["id"].(string))

	props := rel["properties"].(map[string]any)
	s.Equal("Updated reason", props["reason"])
}

func (s *GraphTestSuite) TestPatchRelationship_MergesProperties() {
	srcID, _ := s.createObject("Requirement", nil)
	dstID, _ := s.createObject("Decision", nil)

	relID, _ := s.createRelationship("DEPENDS_ON", srcID, dstID, map[string]any{
		"reason":   "Initial reason",
		"priority": "high",
	})

	// Patch only the reason
	body := map[string]any{
		"properties": map[string]any{
			"reason": "Updated reason",
		},
	}

	resp, err := s.Client.PATCH("/api/v2/graph/relationships/"+relID, body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	rel, err := resp.JSONMap()
	s.Require().NoError(err)

	// Track new version for cleanup
	s.createdRelationshipIDs = append(s.createdRelationshipIDs, rel["id"].(string))

	// Both properties should exist
	props := rel["properties"].(map[string]any)
	s.Equal("Updated reason", props["reason"])
	s.Equal("high", props["priority"])
}

// =============================================================================
// Test: Delete Relationship
// =============================================================================

func (s *GraphTestSuite) TestDeleteRelationship_Success() {
	srcID, _ := s.createObject("Requirement", nil)
	dstID, _ := s.createObject("Decision", nil)

	relID, _ := s.createRelationship("DEPENDS_ON", srcID, dstID, nil)

	// Delete the relationship
	resp, err := s.Client.DELETE("/api/v2/graph/relationships/"+relID,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	rel, err := resp.JSONMap()
	s.Require().NoError(err)

	// Should return tombstone
	s.NotNil(rel["deleted_at"])

	// Remove from cleanup list since it's deleted
	for i, rid := range s.createdRelationshipIDs {
		if rid == relID {
			s.createdRelationshipIDs = append(s.createdRelationshipIDs[:i], s.createdRelationshipIDs[i+1:]...)
			break
		}
	}
}

// =============================================================================
// Test: Restore Relationship
// =============================================================================

func (s *GraphTestSuite) TestRestoreRelationship_Success() {
	srcID, _ := s.createObject("Requirement", nil)
	dstID, _ := s.createObject("Decision", nil)

	relID, created := s.createRelationship("DEPENDS_ON", srcID, dstID, nil)

	// Delete
	deleteResp, err := s.Client.DELETE("/api/v2/graph/relationships/"+relID,
		s.AdminAuth(),
		s.ProjectHeader(),
	)
	s.Require().NoError(err)
	s.Require().Equal(http.StatusOK, deleteResp.StatusCode)

	deleted, _ := deleteResp.JSONMap()
	deletedID := deleted["id"].(string)

	// Restore
	resp, err := s.Client.POST("/api/v2/graph/relationships/"+deletedID+"/restore", nil,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusCreated, resp.StatusCode, "Response: %s", resp.BodyString())

	rel, err := resp.JSONMap()
	s.Require().NoError(err)

	s.Nil(rel["deleted_at"])
	s.Equal(created["canonical_id"], rel["canonical_id"])

	// Track restored version for cleanup
	s.createdRelationshipIDs = append(s.createdRelationshipIDs, rel["id"].(string))
}

// =============================================================================
// Test: Relationship History
// =============================================================================

func (s *GraphTestSuite) TestGetRelationshipHistory_Success() {
	srcID, _ := s.createObject("Requirement", nil)
	dstID, _ := s.createObject("Decision", nil)

	relID, _ := s.createRelationship("DEPENDS_ON", srcID, dstID, map[string]any{
		"reason": "Version 1",
	})

	// Update to create version 2
	patchBody := map[string]any{
		"properties": map[string]any{
			"reason": "Version 2",
		},
	}

	patchResp, err := s.Client.PATCH("/api/v2/graph/relationships/"+relID, patchBody,
		s.AdminAuth(),
		s.ProjectHeader(),
	)
	s.Require().NoError(err)
	s.Require().Equal(http.StatusOK, patchResp.StatusCode)

	patchRel, _ := patchResp.JSONMap()
	s.createdRelationshipIDs = append(s.createdRelationshipIDs, patchRel["id"].(string))

	// Get history
	resp, err := s.Client.GET("/api/v2/graph/relationships/"+relID+"/history",
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	var history []map[string]any
	err = resp.JSON(&history)
	s.Require().NoError(err)

	s.Len(history, 2)

	// Versions should be in descending order
	s.Equal(float64(2), history[0]["version"])
	s.Equal(float64(1), history[1]["version"])
}

// =============================================================================
// Test: FTS Search
// =============================================================================

func (s *GraphTestSuite) TestFTSSearch_RequiresAuth() {
	resp, err := s.Client.GET("/api/v2/graph/objects/fts",
		s.ProjectHeader(),
		client.WithQuery("q", "test"),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *GraphTestSuite) TestFTSSearch_RequiresQuery() {
	resp, err := s.Client.GET("/api/v2/graph/objects/fts",
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *GraphTestSuite) TestFTSSearch_EmptyResults() {
	resp, err := s.Client.GET("/api/v2/graph/objects/fts",
		s.AdminAuth(),
		s.ProjectHeader(),
		client.WithQuery("q", fmt.Sprintf("nonexistent_%s", uuid.New().String())),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	body, err := resp.JSONMap()
	s.Require().NoError(err)

	s.Contains(body, "data")
	s.Equal(float64(0), body["total"])
}

func (s *GraphTestSuite) TestFTSSearch_WithFilters() {
	// Create an object
	s.createObject("Requirement", map[string]any{
		"title": "Authentication Feature",
	})

	// Search with type filter
	resp, err := s.Client.GET("/api/v2/graph/objects/fts",
		s.AdminAuth(),
		s.ProjectHeader(),
		client.WithQuery("q", "authentication"),
		client.WithQuery("types", "Requirement"),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	body, err := resp.JSONMap()
	s.Require().NoError(err)

	// Results may be empty if FTS trigger doesn't index
	// The main point is the API accepts the parameters
	s.Contains(body, "data")
}

// =============================================================================
// Test: Vector Search
// =============================================================================

func (s *GraphTestSuite) TestVectorSearch_RequiresAuth() {
	vector := make([]float32, 768)
	for i := range vector {
		vector[i] = float32(i) * 0.001
	}

	body := map[string]any{
		"vector": vector,
	}

	resp, err := s.Client.POST("/api/v2/graph/objects/vector-search", body,
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *GraphTestSuite) TestVectorSearch_RequiresVector() {
	resp, err := s.Client.POST("/api/v2/graph/objects/vector-search", map[string]any{},
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *GraphTestSuite) TestVectorSearch_EmptyResults() {
	// Generate a 768-dim vector (matching embedding_v2 dimensions)
	vector := make([]float32, 768)
	for i := range vector {
		vector[i] = float32(i) * 0.001
	}

	body := map[string]any{
		"vector": vector,
	}

	resp, err := s.Client.POST("/api/v2/graph/objects/vector-search", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	result, err := resp.JSONMap()
	s.Require().NoError(err)

	s.Contains(result, "data")
	s.Equal(float64(0), result["total"])
}

func (s *GraphTestSuite) TestVectorSearch_WithFilters() {
	// Generate a 768-dim vector
	vector := make([]float32, 768)
	for i := range vector {
		vector[i] = float32(i) * 0.001
	}

	body := map[string]any{
		"vector":      vector,
		"types":       []string{"Requirement"},
		"limit":       10,
		"maxDistance": 0.5,
	}

	resp, err := s.Client.POST("/api/v2/graph/objects/vector-search", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	result, err := resp.JSONMap()
	s.Require().NoError(err)

	// Results empty but API accepts the parameters
	s.Contains(result, "data")
}

// =============================================================================
// Test: Hybrid Search
// =============================================================================

func (s *GraphTestSuite) TestHybridSearch_RequiresAuth() {
	body := map[string]any{
		"query": "test",
	}

	resp, err := s.Client.POST("/api/v2/graph/search", body,
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *GraphTestSuite) TestHybridSearch_RequiresQueryOrVector() {
	resp, err := s.Client.POST("/api/v2/graph/search", map[string]any{},
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *GraphTestSuite) TestHybridSearch_QueryOnly() {
	body := map[string]any{
		"query": "authentication",
	}

	resp, err := s.Client.POST("/api/v2/graph/search", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	result, err := resp.JSONMap()
	s.Require().NoError(err)

	// Results may be empty, but response structure is correct
	s.Contains(result, "data")
}

func (s *GraphTestSuite) TestHybridSearch_VectorOnly() {
	// Generate a 768-dim vector
	vector := make([]float32, 768)
	for i := range vector {
		vector[i] = float32(i) * 0.001
	}

	body := map[string]any{
		"vector": vector,
	}

	resp, err := s.Client.POST("/api/v2/graph/search", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	result, err := resp.JSONMap()
	s.Require().NoError(err)

	s.Contains(result, "data")
}

func (s *GraphTestSuite) TestHybridSearch_QueryAndVector() {
	// Generate a 768-dim vector
	vector := make([]float32, 768)
	for i := range vector {
		vector[i] = float32(i) * 0.001
	}

	body := map[string]any{
		"query":         "authentication",
		"vector":        vector,
		"lexicalWeight": 0.7,
		"vectorWeight":  0.3,
	}

	resp, err := s.Client.POST("/api/v2/graph/search", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	result, err := resp.JSONMap()
	s.Require().NoError(err)

	s.Contains(result, "data")
}

func (s *GraphTestSuite) TestHybridSearch_WithFilters() {
	body := map[string]any{
		"query":  "authentication",
		"types":  []string{"Requirement", "Decision"},
		"labels": []string{"security"},
		"limit":  10,
	}

	resp, err := s.Client.POST("/api/v2/graph/search", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	result, err := resp.JSONMap()
	s.Require().NoError(err)

	s.Contains(result, "data")
}
