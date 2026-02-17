package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent/internal/testutil"
)

// NotificationsTestSuite tests the notifications API endpoints
type NotificationsTestSuite struct {
	testutil.BaseSuite
}

func TestNotificationsSuite(t *testing.T) {
	suite.Run(t, new(NotificationsTestSuite))
}

func (s *NotificationsTestSuite) SetupSuite() {
	s.SetDBSuffix("notifications")
	s.BaseSuite.SetupSuite()
}

// testUserID returns the admin user ID (created by SetupTest fixtures)
func (s *NotificationsTestSuite) testUserID() string {
	return testutil.AdminUser.ID
}

// createNotificationViaDB creates a notification directly in the database for testing
func (s *NotificationsTestSuite) createNotificationViaDB(userID, title, message, importance string, read bool) string {
	s.Require().NotNil(s.DB(), "Database connection required for notification creation")

	var notificationID string
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.notifications (user_id, title, message, importance, read, severity)
		VALUES (?, ?, ?, ?, ?, 'info')
		RETURNING id
	`, userID, title, message, importance, read).Exec(context.Background(), &notificationID)
	s.Require().NoError(err)

	return notificationID
}

// createDismissedNotificationViaDB creates a dismissed notification
func (s *NotificationsTestSuite) createDismissedNotificationViaDB(userID, title, message string) string {
	s.Require().NotNil(s.DB(), "Database connection required for notification creation")

	var notificationID string
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.notifications (user_id, title, message, importance, dismissed, dismissed_at, cleared_at, severity)
		VALUES (?, ?, ?, 'other', true, NOW(), NOW(), 'info')
		RETURNING id
	`, userID, title, message).Exec(context.Background(), &notificationID)
	s.Require().NoError(err)

	return notificationID
}

// createSnoozedNotificationViaDB creates a snoozed notification (snoozed until future)
func (s *NotificationsTestSuite) createSnoozedNotificationViaDB(userID, title, message string) string {
	s.Require().NotNil(s.DB(), "Database connection required for notification creation")

	var notificationID string
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.notifications (user_id, title, message, importance, snoozed_until, severity)
		VALUES (?, ?, ?, 'other', NOW() + INTERVAL '1 day', 'info')
		RETURNING id
	`, userID, title, message).Exec(context.Background(), &notificationID)
	s.Require().NoError(err)

	return notificationID
}

// =============================================================================
// Test: Get Stats (GET /api/notifications/stats)
// =============================================================================

func (s *NotificationsTestSuite) TestGetStats_RequiresAuth() {
	resp := s.Client.GET("/api/notifications/stats")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *NotificationsTestSuite) TestGetStats_ReturnsZeroStats() {
	resp := s.Client.GET("/api/notifications/stats",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	s.Contains(result, "unread")
	s.Contains(result, "dismissed")
	s.Contains(result, "total")
}

func (s *NotificationsTestSuite) TestGetStats_ReturnsCorrectStats() {
	// Create unread notification
	s.createNotificationViaDB(s.testUserID(), "Unread 1", "Message 1", "other", false)
	// Create read notification
	s.createNotificationViaDB(s.testUserID(), "Read 1", "Message 2", "other", true)
	// Create dismissed notification
	s.createDismissedNotificationViaDB(s.testUserID(), "Dismissed 1", "Message 3")

	resp := s.Client.GET("/api/notifications/stats",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	s.GreaterOrEqual(result["unread"].(float64), float64(1))
	s.GreaterOrEqual(result["dismissed"].(float64), float64(1))
	s.GreaterOrEqual(result["total"].(float64), float64(2)) // Total excludes cleared
}

// =============================================================================
// Test: Get Counts (GET /api/notifications/counts)
// =============================================================================

func (s *NotificationsTestSuite) TestGetCounts_RequiresAuth() {
	resp := s.Client.GET("/api/notifications/counts")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *NotificationsTestSuite) TestGetCounts_ReturnsCountsByTab() {
	// Create important notification
	s.createNotificationViaDB(s.testUserID(), "Important 1", "Message 1", "important", false)
	// Create other notification
	s.createNotificationViaDB(s.testUserID(), "Other 1", "Message 2", "other", false)
	// Create snoozed notification
	s.createSnoozedNotificationViaDB(s.testUserID(), "Snoozed 1", "Message 3")
	// Create cleared notification
	s.createDismissedNotificationViaDB(s.testUserID(), "Cleared 1", "Message 4")

	resp := s.Client.GET("/api/notifications/counts",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	data := result["data"].(map[string]any)
	s.GreaterOrEqual(data["all"].(float64), float64(2)) // Important + Other
	s.GreaterOrEqual(data["important"].(float64), float64(1))
	s.GreaterOrEqual(data["other"].(float64), float64(1))
	s.GreaterOrEqual(data["snoozed"].(float64), float64(1))
	s.GreaterOrEqual(data["cleared"].(float64), float64(1))
}

// =============================================================================
// Test: List Notifications (GET /api/notifications)
// =============================================================================

func (s *NotificationsTestSuite) TestList_RequiresAuth() {
	resp := s.Client.GET("/api/notifications")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *NotificationsTestSuite) TestList_ReturnsEmptyArrayWhenNoNotifications() {
	resp := s.Client.GET("/api/notifications",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	s.Contains(result, "data")
	// Data should be an array (possibly empty)
	data := result["data"].([]any)
	s.NotNil(data) // Should be array, not nil
}

func (s *NotificationsTestSuite) TestList_ReturnsNotifications() {
	// Create test notifications
	s.createNotificationViaDB(s.testUserID(), "Test Notification 1", "Message 1", "other", false)
	s.createNotificationViaDB(s.testUserID(), "Test Notification 2", "Message 2", "other", false)

	resp := s.Client.GET("/api/notifications",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	data := result["data"].([]any)
	s.GreaterOrEqual(len(data), 2)
}

func (s *NotificationsTestSuite) TestList_FiltersByTab_Important() {
	// Create notifications with different importance
	s.createNotificationViaDB(s.testUserID(), "Important Tab Test", "Message", "important", false)
	s.createNotificationViaDB(s.testUserID(), "Other Tab Test", "Message", "other", false)

	resp := s.Client.GET("/api/notifications?tab=important",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	data := result["data"].([]any)
	for _, n := range data {
		notification := n.(map[string]any)
		s.Equal("important", notification["importance"])
	}
}

func (s *NotificationsTestSuite) TestList_FiltersByTab_Cleared() {
	// Create cleared notification
	s.createDismissedNotificationViaDB(s.testUserID(), "Cleared Tab Test", "Message")

	resp := s.Client.GET("/api/notifications?tab=cleared",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	data := result["data"].([]any)
	s.GreaterOrEqual(len(data), 1)
	// All notifications in cleared tab should have clearedAt set
	for _, n := range data {
		notification := n.(map[string]any)
		s.NotNil(notification["clearedAt"])
	}
}

func (s *NotificationsTestSuite) TestList_FiltersByUnreadOnly() {
	// Create read and unread notifications
	s.createNotificationViaDB(s.testUserID(), "Unread Notification", "Message", "other", false)
	s.createNotificationViaDB(s.testUserID(), "Read Notification", "Message", "other", true)

	resp := s.Client.GET("/api/notifications?unread_only=true",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	data := result["data"].([]any)
	for _, n := range data {
		notification := n.(map[string]any)
		s.Equal(false, notification["read"])
	}
}

func (s *NotificationsTestSuite) TestList_FiltersBySearch() {
	// Create notifications with different titles
	s.createNotificationViaDB(s.testUserID(), "Important Alert XYZ123", "Message", "other", false)
	s.createNotificationViaDB(s.testUserID(), "Regular Update", "Message", "other", false)

	resp := s.Client.GET("/api/notifications?search=XYZ123",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)

	data := result["data"].([]any)
	s.GreaterOrEqual(len(data), 1)

	// Should find the notification with XYZ123 in title
	found := false
	for _, n := range data {
		notification := n.(map[string]any)
		if notification["title"] == "Important Alert XYZ123" {
			found = true
			break
		}
	}
	s.True(found, "Should find notification matching search")
}

// =============================================================================
// Test: Mark Read (PATCH /api/notifications/:id/read)
// =============================================================================

func (s *NotificationsTestSuite) TestMarkRead_RequiresAuth() {
	resp := s.Client.PATCH("/api/notifications/some-id/read")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *NotificationsTestSuite) TestMarkRead_ReturnsNotFoundForInvalidID() {
	resp := s.Client.PATCH("/api/notifications/00000000-0000-0000-0000-000000000000/read",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusNotFound, resp.StatusCode)
}

func (s *NotificationsTestSuite) TestMarkRead_MarksNotificationAsRead() {
	// Create unread notification
	notificationID := s.createNotificationViaDB(s.testUserID(), "Mark Read Test", "Message", "other", false)

	resp := s.Client.PATCH("/api/notifications/"+notificationID+"/read",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.Equal("read", result["status"])

	// Verify in database
	var read bool
	err = s.DB().NewRaw(`SELECT read FROM kb.notifications WHERE id = ?`, notificationID).Scan(context.Background(), &read)
	s.NoError(err)
	s.True(read)
}

func (s *NotificationsTestSuite) TestMarkRead_DoesNotAffectOtherUsersNotifications() {
	// Create notification for a different user
	var differentUserID string
	_, err := s.DB().NewRaw(`
		INSERT INTO core.user_profiles (zitadel_user_id, first_name, last_name, created_at, updated_at)
		VALUES ('other-user-zitadel-id', 'Other', 'User', NOW(), NOW())
		RETURNING id
	`).Exec(context.Background(), &differentUserID)
	s.Require().NoError(err)

	otherNotificationID := s.createNotificationViaDB(differentUserID, "Other User Notification", "Message", "other", false)

	// Try to mark it as read as the test user
	resp := s.Client.PATCH("/api/notifications/"+otherNotificationID+"/read",
		testutil.WithAuth("e2e-test-user"),
	)

	// Should not find it (filtered by user_id)
	s.Equal(http.StatusNotFound, resp.StatusCode)

	// Verify notification is still unread
	var read bool
	err = s.DB().NewRaw(`SELECT read FROM kb.notifications WHERE id = ?`, otherNotificationID).Scan(context.Background(), &read)
	s.NoError(err)
	s.False(read, "Other user's notification should still be unread")
}

// =============================================================================
// Test: Dismiss (DELETE /api/notifications/:id/dismiss)
// =============================================================================

func (s *NotificationsTestSuite) TestDismiss_RequiresAuth() {
	resp := s.Client.DELETE("/api/notifications/some-id/dismiss")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *NotificationsTestSuite) TestDismiss_ReturnsNotFoundForInvalidID() {
	resp := s.Client.DELETE("/api/notifications/00000000-0000-0000-0000-000000000000/dismiss",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusNotFound, resp.StatusCode)
}

func (s *NotificationsTestSuite) TestDismiss_DismissesNotification() {
	// Create notification
	notificationID := s.createNotificationViaDB(s.testUserID(), "Dismiss Test", "Message", "other", false)

	resp := s.Client.DELETE("/api/notifications/"+notificationID+"/dismiss",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.Equal("dismissed", result["status"])

	// Verify in database
	var dismissed bool
	err = s.DB().NewRaw(`SELECT dismissed FROM kb.notifications WHERE id = ?`, notificationID).Scan(context.Background(), &dismissed)
	s.NoError(err)
	s.True(dismissed)

	// Verify it's also cleared
	var clearedAt any
	err = s.DB().NewRaw(`SELECT cleared_at FROM kb.notifications WHERE id = ?`, notificationID).Scan(context.Background(), &clearedAt)
	s.NoError(err)
	s.NotNil(clearedAt)
}

// =============================================================================
// Test: Mark All Read (POST /api/notifications/mark-all-read)
// =============================================================================

func (s *NotificationsTestSuite) TestMarkAllRead_RequiresAuth() {
	resp := s.Client.POST("/api/notifications/mark-all-read")
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *NotificationsTestSuite) TestMarkAllRead_ReturnsZeroWhenNoUnread() {
	// Create only read notifications
	s.createNotificationViaDB(s.testUserID(), "Already Read 1", "Message", "other", true)
	s.createNotificationViaDB(s.testUserID(), "Already Read 2", "Message", "other", true)

	resp := s.Client.POST("/api/notifications/mark-all-read",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.Equal("marked_all_read", result["status"])
	// count can be 0 if no unread notifications
}

func (s *NotificationsTestSuite) TestMarkAllRead_MarksAllUnreadAsRead() {
	// Create unread notifications
	n1 := s.createNotificationViaDB(s.testUserID(), "Unread Mark All 1", "Message", "other", false)
	n2 := s.createNotificationViaDB(s.testUserID(), "Unread Mark All 2", "Message", "other", false)
	n3 := s.createNotificationViaDB(s.testUserID(), "Unread Mark All 3", "Message", "other", false)

	resp := s.Client.POST("/api/notifications/mark-all-read",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.NoError(err)
	s.Equal("marked_all_read", result["status"])
	s.GreaterOrEqual(result["count"].(float64), float64(3))

	// Verify all are now read
	for _, id := range []string{n1, n2, n3} {
		var read bool
		err = s.DB().NewRaw(`SELECT read FROM kb.notifications WHERE id = ?`, id).Scan(context.Background(), &read)
		s.NoError(err)
		s.True(read, "Notification %s should be read", id)
	}
}

func (s *NotificationsTestSuite) TestMarkAllRead_DoesNotAffectClearedNotifications() {
	// Create a cleared unread notification (edge case)
	var clearedNotificationID string
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.notifications (user_id, title, message, importance, read, cleared_at, severity)
		VALUES (?, 'Cleared Unread', 'Message', 'other', false, NOW(), 'info')
		RETURNING id
	`, s.testUserID()).Exec(context.Background(), &clearedNotificationID)
	s.Require().NoError(err)

	// Create regular unread notification
	s.createNotificationViaDB(s.testUserID(), "Regular Unread", "Message", "other", false)

	resp := s.Client.POST("/api/notifications/mark-all-read",
		testutil.WithAuth("e2e-test-user"),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	// The cleared notification should still be unread (mark-all-read excludes cleared)
	var read bool
	err = s.DB().NewRaw(`SELECT read FROM kb.notifications WHERE id = ?`, clearedNotificationID).Scan(context.Background(), &read)
	s.NoError(err)
	s.False(read, "Cleared notification should not be marked as read")
}
