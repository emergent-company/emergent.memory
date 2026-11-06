package extraction

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/notifications"
)

// TestNotifyProcessingFailed_RetrySurfacesNewError is the regression guard for
// #13. DocumentParsingJobsService.RetryJob reuses the same job row/id, so the
// second failure arrives with the same group key. The producer must reopen the
// still-active failure row — replacing the old message/details with the new
// error and returning it to unread — rather than letting the coalescing insert
// swallow the episode.
func TestNotifyProcessingFailed_RetrySurfacesNewError(t *testing.T) {
	h := newReviewNotificationHarness(t)
	projectID, documentID := h.seedProject(t)
	jobID := uuid.NewString()
	groupKey := "document.processing_failed:" + jobID

	notifyProcessingFailed(h.ctx, h.w.notificationsSvc, quietLogger(),
		projectID, documentID, jobID, "document_parsing_job", stageParsing, "report.pdf",
		"first attempt: unsupported encoding")

	require.Len(t, h.notificationsByGroupKey(t, groupKey), 1)
	first := h.notificationsByGroupKey(t, groupKey)[0]
	require.Contains(t, first.Message, "unsupported encoding")
	require.False(t, first.Read)

	// The member read the first alert before pressing Retry.
	_, err := h.db.NewUpdate().
		TableExpr("kb.notifications").
		Set("read = true, read_at = now()").
		Where("group_key = ?", groupKey).
		Exec(h.ctx)
	require.NoError(t, err)

	// Retry runs the same job row and fails for a different reason.
	notifyProcessingFailed(h.ctx, h.w.notificationsSvc, quietLogger(),
		projectID, documentID, jobID, "document_parsing_job", stageParsing, "report.pdf",
		"second attempt: malformed PDF")

	rows := h.notificationsByGroupKey(t, groupKey)
	require.Len(t, rows, 1, "a retried job must refresh its existing row, not stack another")
	second := rows[0]
	require.Equal(t, first.ID, second.ID)
	require.Contains(t, second.Message, "malformed PDF")
	require.NotContains(t, second.Message, "unsupported encoding",
		"the inbox must describe the retry's failure, not the stale first error")
	require.False(t, second.Read, "a retry that failed must return the alert to unread")

	var details map[string]any
	require.NoError(t, json.Unmarshal(second.Details, &details))
	require.Equal(t, "second attempt: malformed PDF", details["error"])
	require.Equal(t, jobID, details["job_id"])
}

// notificationsByGroupKey returns the stored rows for a group key, oldest first.
func (h *reviewNotificationHarness) notificationsByGroupKey(t *testing.T, groupKey string) []notifications.Notification {
	t.Helper()
	var rows []notifications.Notification
	require.NoError(t, h.db.NewSelect().
		Model(&rows).
		Where("group_key = ?", groupKey).
		Order("created_at ASC").
		Scan(h.ctx))
	return rows
}
