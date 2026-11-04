// Package api_test — email_jobs_test.go
//
// Tests for the email job queue functionality.
// Ported from emergent.memory/apps/server/tests/e2e/email_jobs_test.go
//
// All tests in this file require direct DB access and are skipped in
// external-server (runlog) mode. The email.JobsService operates directly
// against the database — there is no HTTP API surface to test without it.
package api_test

import (
	"testing"
)

func TestEmailJobs_Enqueue_CreatesJob(t *testing.T) {
	t.Skip("requires direct DB access")
}

func TestEmailJobs_Enqueue_WithOptionalFields(t *testing.T) {
	t.Skip("requires direct DB access")
}

func TestEmailJobs_Dequeue_ClaimsJobs(t *testing.T) {
	t.Skip("requires direct DB access")
}

func TestEmailJobs_Dequeue_RespectsScheduledAt(t *testing.T) {
	t.Skip("requires direct DB access")
}

func TestEmailJobs_Dequeue_FIFO(t *testing.T) {
	t.Skip("requires direct DB access")
}

func TestEmailJobs_MarkSent_UpdatesJob(t *testing.T) {
	t.Skip("requires direct DB access")
}

func TestEmailJobs_MarkFailed_RequeuesForRetry(t *testing.T) {
	t.Skip("requires direct DB access")
}

func TestEmailJobs_MarkFailed_PermanentlyFailsAfterMaxAttempts(t *testing.T) {
	t.Skip("requires direct DB access")
}

func TestEmailJobs_RecoverStaleJobs_RecoversProcessingJobs(t *testing.T) {
	t.Skip("requires direct DB access")
}

func TestEmailJobs_RecoverStaleJobs_IgnoresRecentProcessingJobs(t *testing.T) {
	t.Skip("requires direct DB access")
}

func TestEmailJobs_Stats_ReturnsCorrectCounts(t *testing.T) {
	t.Skip("requires direct DB access")
}

func TestEmailJobs_GetJob_ReturnsJob(t *testing.T) {
	t.Skip("requires direct DB access")
}

func TestEmailJobs_GetJob_ReturnsNilForNonexistent(t *testing.T) {
	t.Skip("requires direct DB access")
}

func TestEmailJobs_GetJobsBySource_ReturnsMatchingJobs(t *testing.T) {
	t.Skip("requires direct DB access")
}
