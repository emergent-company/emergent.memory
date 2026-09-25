package superadmin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEmailJobSelectScansAgainstMigratedSchema is the regression test for
// issue #951. The superadmin EmailJob entity had drifted from kb.email_jobs in
// both directions: it declared a phantom `updated_at` (absent from the table)
// and lacked `mailgun_message_id`, `started_at`, `next_retry_at` and
// `delivery_status_synced_at`. ListEmailJobs / GetEmailJob use
// `NewSelect().Model((*EmailJob)(nil))` with no explicit Column() list, so the
// query selected the phantom column and failed with
// "ERROR: column ej.updated_at does not exist".
//
// The test inserts a real row into a head-migrated database and re-runs the
// exact select the endpoint performs, asserting it scans cleanly and that the
// formerly-missing columns round-trip.
func TestEmailJobSelectScansAgainstMigratedSchema(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		INSERT INTO kb.email_jobs
			(template_name, to_email, subject, mailgun_message_id, started_at, next_retry_at, delivery_status_synced_at)
		VALUES ('welcome', 'ops@example.com', 'Welcome', 'msg-1', NOW(), NOW(), NOW())`)
	require.NoError(t, err)

	var jobs []EmailJob
	err = db.NewSelect().Model((*EmailJob)(nil)).Order("created_at DESC").Scan(ctx, &jobs)
	require.NoError(t, err, "select against kb.email_jobs must scan every migrated column")
	require.Len(t, jobs, 1)

	job := jobs[0]
	require.Equal(t, "welcome", job.TemplateName)
	require.NotNil(t, job.MailgunMessageID)
	require.Equal(t, "msg-1", *job.MailgunMessageID)
	require.NotNil(t, job.StartedAt)
	require.NotNil(t, job.NextRetryAt)
	require.NotNil(t, job.DeliveryStatusSyncedAt)
}
