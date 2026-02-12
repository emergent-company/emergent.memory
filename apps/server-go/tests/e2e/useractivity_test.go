package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent/emergent-core/domain/useractivity"
	"github.com/emergent/emergent-core/internal/testutil"
)

// UserActivityTestSuite tests the User Activity API endpoints
type UserActivityTestSuite struct {
	testutil.BaseSuite
}

func TestUserActivitySuite(t *testing.T) {
	suite.Run(t, new(UserActivityTestSuite))
}

func (s *UserActivityTestSuite) SetupSuite() {
	s.SetDBSuffix("useractivity")
	s.BaseSuite.SetupSuite()
}

// testUserID returns the user ID from the admin test user
func (s *UserActivityTestSuite) testUserID() string {
	return testutil.AdminUser.ID
}

// createActivityViaDB creates a user activity item directly in the database
// actionType must be 'viewed' or 'edited', resourceType must be 'document' or 'object'
func (s *UserActivityTestSuite) createActivityViaDB(resourceType, resourceID, actionType string) *useractivity.UserRecentItem {
	now := time.Now().UTC()
	item := &useractivity.UserRecentItem{
		ID:           uuid.New().String(),
		UserID:       s.testUserID(),
		ProjectID:    s.ProjectID,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		ActionType:   actionType,
		AccessedAt:   now,
		CreatedAt:    now,
	}
	_, err := s.DB().NewInsert().Model(item).Exec(s.Ctx)
	s.Require().NoError(err)
	return item
}

// createActivityWithNameViaDB creates a user activity item with a resource name
func (s *UserActivityTestSuite) createActivityWithNameViaDB(resourceType, resourceID, resourceName, actionType string) *useractivity.UserRecentItem {
	now := time.Now().UTC()
	item := &useractivity.UserRecentItem{
		ID:           uuid.New().String(),
		UserID:       s.testUserID(),
		ProjectID:    s.ProjectID,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		ResourceName: &resourceName,
		ActionType:   actionType,
		AccessedAt:   now,
		CreatedAt:    now,
	}
	_, err := s.DB().NewInsert().Model(item).Exec(s.Ctx)
	s.Require().NoError(err)
	return item
}

// === Record Activity Tests ===

func (s *UserActivityTestSuite) TestRecord_RequiresAuth() {
	resp := s.Client.POST(fmt.Sprintf("/api/user-activity/record?project_id=%s", s.ProjectID),
		testutil.WithJSONBody(map[string]string{
			"resourceType": "document",
			"resourceId":   uuid.New().String(),
			"actionType":   "viewed",
		}),
	)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *UserActivityTestSuite) TestRecord_RequiresProjectID() {
	resp := s.Client.POST("/api/user-activity/record",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]string{
			"resourceType": "document",
			"resourceId":   uuid.New().String(),
			"actionType":   "viewed",
		}),
	)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *UserActivityTestSuite) TestRecord_RequiresFields() {
	// Missing resourceType
	resp := s.Client.POST(fmt.Sprintf("/api/user-activity/record?project_id=%s", s.ProjectID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]string{
			"resourceId": uuid.New().String(),
			"actionType": "viewed",
		}),
	)
	s.Equal(http.StatusBadRequest, resp.StatusCode)

	// Missing resourceId
	resp = s.Client.POST(fmt.Sprintf("/api/user-activity/record?project_id=%s", s.ProjectID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]string{
			"resourceType": "document",
			"actionType":   "viewed",
		}),
	)
	s.Equal(http.StatusBadRequest, resp.StatusCode)

	// Missing actionType
	resp = s.Client.POST(fmt.Sprintf("/api/user-activity/record?project_id=%s", s.ProjectID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]string{
			"resourceType": "document",
			"resourceId":   uuid.New().String(),
		}),
	)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *UserActivityTestSuite) TestRecord_ValidatesResourceID() {
	resp := s.Client.POST(fmt.Sprintf("/api/user-activity/record?project_id=%s", s.ProjectID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]string{
			"resourceType": "document",
			"resourceId":   "not-a-uuid",
			"actionType":   "viewed",
		}),
	)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *UserActivityTestSuite) TestRecord_ValidatesProjectID() {
	resp := s.Client.POST("/api/user-activity/record?project_id=invalid-uuid",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]string{
			"resourceType": "document",
			"resourceId":   uuid.New().String(),
			"actionType":   "viewed",
		}),
	)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *UserActivityTestSuite) TestRecord_RecordsActivity() {
	resourceID := uuid.New().String()
	resp := s.Client.POST(fmt.Sprintf("/api/user-activity/record?project_id=%s", s.ProjectID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]string{
			"resourceType": "document",
			"resourceId":   resourceID,
			"actionType":   "viewed",
		}),
	)
	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]string
	err := json.Unmarshal(resp.Body, &result)
	s.Require().NoError(err)
	s.Equal("recorded", result["status"])

	// Verify it was recorded by fetching recent items
	getResp := s.Client.GET("/api/user-activity/recent",
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, getResp.StatusCode)

	var recentResult useractivity.RecentItemsResponse
	err = json.Unmarshal(getResp.Body, &recentResult)
	s.Require().NoError(err)
	s.GreaterOrEqual(len(recentResult.Data), 1)

	// Find our recorded item
	found := false
	for _, item := range recentResult.Data {
		if item.ResourceID == resourceID {
			s.Equal("document", item.ResourceType)
			s.Equal("viewed", item.ActionType)
			found = true
			break
		}
	}
	s.True(found, "Recorded activity should be in recent items")
}

func (s *UserActivityTestSuite) TestRecord_WithOptionalFields() {
	resourceID := uuid.New().String()
	resourceName := "Test Document"
	resourceSubtype := "pdf"

	resp := s.Client.POST(fmt.Sprintf("/api/user-activity/record?project_id=%s", s.ProjectID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]string{
			"resourceType":    "document",
			"resourceId":      resourceID,
			"resourceName":    resourceName,
			"resourceSubtype": resourceSubtype,
			"actionType":      "edited",
		}),
	)
	s.Equal(http.StatusOK, resp.StatusCode)

	// Verify optional fields were stored
	getResp := s.Client.GET("/api/user-activity/recent",
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, getResp.StatusCode)

	var result useractivity.RecentItemsResponse
	err := json.Unmarshal(getResp.Body, &result)
	s.Require().NoError(err)

	found := false
	for _, item := range result.Data {
		if item.ResourceID == resourceID {
			s.Equal("edited", item.ActionType)
			s.Require().NotNil(item.ResourceName)
			s.Equal(resourceName, *item.ResourceName)
			s.Require().NotNil(item.ResourceSubtype)
			s.Equal(resourceSubtype, *item.ResourceSubtype)
			found = true
			break
		}
	}
	s.True(found, "Recorded activity with optional fields should be found")
}

// === GetRecent Tests ===

func (s *UserActivityTestSuite) TestGetRecent_RequiresAuth() {
	resp := s.Client.GET("/api/user-activity/recent")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *UserActivityTestSuite) TestGetRecent_ReturnsEmptyArrayWhenNoActivity() {
	// Clean up any existing activity for this user
	_, err := s.DB().NewDelete().
		Model((*useractivity.UserRecentItem)(nil)).
		Where("user_id = ?", s.testUserID()).
		Exec(s.Ctx)
	s.Require().NoError(err)

	resp := s.Client.GET("/api/user-activity/recent",
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, resp.StatusCode)

	var result useractivity.RecentItemsResponse
	err = json.Unmarshal(resp.Body, &result)
	s.Require().NoError(err)
	s.NotNil(result.Data)
	s.Equal(0, len(result.Data))
}

func (s *UserActivityTestSuite) TestGetRecent_ReturnsActivityItems() {
	// Clean up first
	_, err := s.DB().NewDelete().
		Model((*useractivity.UserRecentItem)(nil)).
		Where("user_id = ?", s.testUserID()).
		Exec(s.Ctx)
	s.Require().NoError(err)

	// Create some activity items
	resourceID1 := uuid.New().String()
	resourceID2 := uuid.New().String()
	s.createActivityViaDB("document", resourceID1, "viewed")
	s.createActivityViaDB("object", resourceID2, "edited")

	resp := s.Client.GET("/api/user-activity/recent",
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, resp.StatusCode)

	var result useractivity.RecentItemsResponse
	err = json.Unmarshal(resp.Body, &result)
	s.Require().NoError(err)
	s.Equal(2, len(result.Data))
}

func (s *UserActivityTestSuite) TestGetRecent_RespectsLimit() {
	// Clean up first
	_, err := s.DB().NewDelete().
		Model((*useractivity.UserRecentItem)(nil)).
		Where("user_id = ?", s.testUserID()).
		Exec(s.Ctx)
	s.Require().NoError(err)

	// Create multiple activity items
	for i := 0; i < 5; i++ {
		s.createActivityViaDB("document", uuid.New().String(), "viewed")
	}

	resp := s.Client.GET("/api/user-activity/recent?limit=2",
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, resp.StatusCode)

	var result useractivity.RecentItemsResponse
	err = json.Unmarshal(resp.Body, &result)
	s.Require().NoError(err)
	s.Equal(2, len(result.Data))
}

// === GetRecentByType Tests ===

func (s *UserActivityTestSuite) TestGetRecentByType_RequiresAuth() {
	resp := s.Client.GET("/api/user-activity/recent/document")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *UserActivityTestSuite) TestGetRecentByType_ReturnsEmptyArrayWhenNoMatches() {
	// Clean up first
	_, err := s.DB().NewDelete().
		Model((*useractivity.UserRecentItem)(nil)).
		Where("user_id = ?", s.testUserID()).
		Exec(s.Ctx)
	s.Require().NoError(err)

	resp := s.Client.GET("/api/user-activity/recent/document",
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, resp.StatusCode)

	var result useractivity.RecentItemsResponse
	err = json.Unmarshal(resp.Body, &result)
	s.Require().NoError(err)
	s.NotNil(result.Data)
	s.Equal(0, len(result.Data))
}

func (s *UserActivityTestSuite) TestGetRecentByType_FiltersCorrectly() {
	// Clean up first
	_, err := s.DB().NewDelete().
		Model((*useractivity.UserRecentItem)(nil)).
		Where("user_id = ?", s.testUserID()).
		Exec(s.Ctx)
	s.Require().NoError(err)

	// Create mixed activity items
	docID := uuid.New().String()
	objID := uuid.New().String()
	s.createActivityViaDB("document", docID, "viewed")
	s.createActivityViaDB("object", objID, "viewed")
	s.createActivityViaDB("document", uuid.New().String(), "edited")

	// Get only document activity
	resp := s.Client.GET("/api/user-activity/recent/document",
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, resp.StatusCode)

	var result useractivity.RecentItemsResponse
	err = json.Unmarshal(resp.Body, &result)
	s.Require().NoError(err)
	s.Equal(2, len(result.Data))

	for _, item := range result.Data {
		s.Equal("document", item.ResourceType)
	}
}

func (s *UserActivityTestSuite) TestGetRecentByType_RespectsLimit() {
	// Clean up first
	_, err := s.DB().NewDelete().
		Model((*useractivity.UserRecentItem)(nil)).
		Where("user_id = ?", s.testUserID()).
		Exec(s.Ctx)
	s.Require().NoError(err)

	// Create multiple document activities
	for i := 0; i < 5; i++ {
		s.createActivityViaDB("document", uuid.New().String(), "viewed")
	}

	resp := s.Client.GET("/api/user-activity/recent/document?limit=3",
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, resp.StatusCode)

	var result useractivity.RecentItemsResponse
	err = json.Unmarshal(resp.Body, &result)
	s.Require().NoError(err)
	s.Equal(3, len(result.Data))
}

// === DeleteAll Tests ===

func (s *UserActivityTestSuite) TestDeleteAll_RequiresAuth() {
	resp := s.Client.DELETE("/api/user-activity/recent")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *UserActivityTestSuite) TestDeleteAll_DeletesAllActivity() {
	// Create some activity first
	s.createActivityViaDB("document", uuid.New().String(), "viewed")
	s.createActivityViaDB("object", uuid.New().String(), "edited")

	// Verify items exist
	getResp := s.Client.GET("/api/user-activity/recent",
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, getResp.StatusCode)
	var result useractivity.RecentItemsResponse
	_ = json.Unmarshal(getResp.Body, &result)
	s.GreaterOrEqual(len(result.Data), 2)

	// Delete all
	resp := s.Client.DELETE("/api/user-activity/recent",
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, resp.StatusCode)

	var deleteResult map[string]string
	err := json.Unmarshal(resp.Body, &deleteResult)
	s.Require().NoError(err)
	s.Equal("deleted", deleteResult["status"])

	// Verify all deleted
	getResp = s.Client.GET("/api/user-activity/recent",
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, getResp.StatusCode)
	_ = json.Unmarshal(getResp.Body, &result)
	s.Equal(0, len(result.Data))
}

func (s *UserActivityTestSuite) TestDeleteAll_SucceedsWhenNoActivity() {
	// Clean up first
	_, err := s.DB().NewDelete().
		Model((*useractivity.UserRecentItem)(nil)).
		Where("user_id = ?", s.testUserID()).
		Exec(s.Ctx)
	s.Require().NoError(err)

	resp := s.Client.DELETE("/api/user-activity/recent",
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, resp.StatusCode)
}

// === DeleteByResource Tests ===

func (s *UserActivityTestSuite) TestDeleteByResource_RequiresAuth() {
	resp := s.Client.DELETE("/api/user-activity/recent/document/" + uuid.New().String())
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *UserActivityTestSuite) TestDeleteByResource_ValidatesResourceID() {
	resp := s.Client.DELETE("/api/user-activity/recent/document/not-a-uuid",
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *UserActivityTestSuite) TestDeleteByResource_DeletesSpecificResource() {
	// Clean up first
	_, err := s.DB().NewDelete().
		Model((*useractivity.UserRecentItem)(nil)).
		Where("user_id = ?", s.testUserID()).
		Exec(s.Ctx)
	s.Require().NoError(err)

	// Create activity items
	resourceToDelete := uuid.New().String()
	resourceToKeep := uuid.New().String()
	s.createActivityViaDB("document", resourceToDelete, "viewed")
	s.createActivityViaDB("document", resourceToKeep, "viewed")

	// Delete specific resource
	resp := s.Client.DELETE(fmt.Sprintf("/api/user-activity/recent/document/%s", resourceToDelete),
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, resp.StatusCode)

	// Verify only the specific resource was deleted
	getResp := s.Client.GET("/api/user-activity/recent",
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, getResp.StatusCode)

	var result useractivity.RecentItemsResponse
	err = json.Unmarshal(getResp.Body, &result)
	s.Require().NoError(err)
	s.Equal(1, len(result.Data))
	s.Equal(resourceToKeep, result.Data[0].ResourceID)
}

func (s *UserActivityTestSuite) TestDeleteByResource_SucceedsWhenResourceNotFound() {
	resp := s.Client.DELETE(fmt.Sprintf("/api/user-activity/recent/document/%s", uuid.New().String()),
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, resp.StatusCode)
}

func (s *UserActivityTestSuite) TestDeleteByResource_OnlyDeletesMatchingType() {
	// Clean up first
	_, err := s.DB().NewDelete().
		Model((*useractivity.UserRecentItem)(nil)).
		Where("user_id = ?", s.testUserID()).
		Exec(s.Ctx)
	s.Require().NoError(err)

	// Create activity items with same resource ID but different types
	sharedResourceID := uuid.New().String()
	s.createActivityViaDB("document", sharedResourceID, "viewed")
	s.createActivityViaDB("object", sharedResourceID, "viewed")

	// Delete document type
	resp := s.Client.DELETE(fmt.Sprintf("/api/user-activity/recent/document/%s", sharedResourceID),
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, resp.StatusCode)

	// Verify only document type was deleted, object type remains
	getResp := s.Client.GET("/api/user-activity/recent",
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusOK, getResp.StatusCode)

	var result useractivity.RecentItemsResponse
	err = json.Unmarshal(getResp.Body, &result)
	s.Require().NoError(err)
	s.Equal(1, len(result.Data))
	s.Equal("object", result.Data[0].ResourceType)
}
