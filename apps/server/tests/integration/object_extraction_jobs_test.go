package integration

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/domain/extraction"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// ObjectExtractionJobsTestSuite tests the object extraction job queue functionality
type ObjectExtractionJobsTestSuite struct {
	suite.Suite
	testDB      *testutil.TestDB
	ctx         context.Context
	jobsService *extraction.ObjectExtractionJobsService
	log         *slog.Logger
	orgID       string
	projectID   string
	documentID  string // test document for FK tests
}

func TestObjectExtractionJobsSuite(t *testing.T) {
	suite.Run(t, new(ObjectExtractionJobsTestSuite))
}

func (s *ObjectExtractionJobsTestSuite) SetupSuite() {
	s.ctx = context.Background()
	s.log = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Create isolated test database with NestJS migrations
	testDB, err := testutil.SetupTestDB(s.ctx, "object_extraction_jobs")
	s.Require().NoError(err, "Failed to setup test database")
	s.testDB = testDB

	// Create the object extraction jobs service
	cfg := &extraction.ObjectExtractionConfig{
		DefaultMaxRetries:     3,
		WorkerIntervalMs:      1000,
		WorkerBatchSize:       5,
		StaleThresholdMinutes: 10,
	}
	s.jobsService = extraction.NewObjectExtractionJobsService(testDB.DB, s.log, cfg)
}

func (s *ObjectExtractionJobsTestSuite) TearDownSuite() {
	if s.testDB != nil {
		s.testDB.Close()
	}
}

func (s *ObjectExtractionJobsTestSuite) SetupTest() {
	// Truncate tables before each test
	err := testutil.TruncateTables(s.ctx, s.testDB.DB)
	s.Require().NoError(err)

	// Set up test fixtures (users)
	err = testutil.SetupTestFixtures(s.ctx, s.testDB.DB)
	s.Require().NoError(err)

	// Create an org and project for tests
	s.orgID = uuid.New().String()
	s.projectID = uuid.New().String()

	err = testutil.SetupFullTestProject(s.ctx, s.testDB.DB, s.orgID, s.projectID)
	s.Require().NoError(err)

	// Create a test document for FK tests
	s.documentID = uuid.New().String()
	s.createTestDocument()
}

// createTestDocument creates a test document for FK tests
func (s *ObjectExtractionJobsTestSuite) createTestDocument() {
	_, err := s.testDB.DB.NewRaw(`
		INSERT INTO kb.documents (id, project_id, source_type, filename, content, created_at, updated_at)
		VALUES (?, ?, 'upload', 'test-document.txt', 'Test content for extraction', now(), now())
	`, s.documentID, s.projectID).Exec(s.ctx)
	s.Require().NoError(err)
}

// =============================================================================
// Test: CreateJob
// =============================================================================

func (s *ObjectExtractionJobsTestSuite) TestCreateJob_CreatesJob() {
	job, err := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID:    s.projectID,
		DocumentID:   &s.documentID,
		JobType:      extraction.JobTypeFullExtraction,
		EnabledTypes: []string{"Person", "Organization", "Concept"},
		ExtractionConfig: extraction.JSON{
			"model": "gemini-1.5-flash",
		},
	})

	s.NoError(err)
	s.NotEmpty(job.ID)
	s.Equal(s.projectID, job.ProjectID)
	s.Equal(&s.documentID, job.DocumentID)
	s.Equal(extraction.JobTypeFullExtraction, job.JobType)
	s.Equal(extraction.JobStatusPending, job.Status)
	s.Equal([]string{"Person", "Organization", "Concept"}, job.EnabledTypes)
	s.Equal(0, job.RetryCount)
	s.Equal(3, job.MaxRetries)
}

func (s *ObjectExtractionJobsTestSuite) TestCreateJob_WithMinimalOptions() {
	job, err := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})

	s.NoError(err)
	s.NotEmpty(job.ID)
	s.Equal(s.projectID, job.ProjectID)
	s.Equal(extraction.JobTypeFullExtraction, job.JobType) // Default
	s.Equal(extraction.JobStatusPending, job.Status)
	s.Nil(job.DocumentID)
}

func (s *ObjectExtractionJobsTestSuite) TestCreateJob_WithSourceMetadata() {
	sourceType := "document"
	job, err := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID:  s.projectID,
		SourceType: &sourceType,
		SourceMetadata: extraction.JSON{
			"trigger": "manual",
			"user":    "test-user",
		},
	})

	s.NoError(err)
	s.Equal(&sourceType, job.SourceType)
	s.Equal("manual", job.SourceMetadata["trigger"])
}

func (s *ObjectExtractionJobsTestSuite) TestCreateJob_WithCreatedBy() {
	createdBy := uuid.New().String()
	job, err := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
		CreatedBy: &createdBy,
	})

	s.NoError(err)
	s.Equal(&createdBy, job.CreatedBy)
}

func (s *ObjectExtractionJobsTestSuite) TestCreateJob_Reextraction() {
	// Create original job
	originalJob, _ := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
		JobType:   extraction.JobTypeFullExtraction,
	})

	// Create reextraction job
	job, err := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID:      s.projectID,
		JobType:        extraction.JobTypeReextraction,
		ReprocessingOf: &originalJob.ID,
	})

	s.NoError(err)
	s.Equal(extraction.JobTypeReextraction, job.JobType)
	s.Equal(&originalJob.ID, job.ReprocessingOf)
}

// =============================================================================
// Test: Dequeue
// =============================================================================

func (s *ObjectExtractionJobsTestSuite) TestDequeue_ClaimsJob() {
	_, err := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})
	s.NoError(err)

	job, err := s.jobsService.Dequeue(s.ctx)

	s.NoError(err)
	s.NotNil(job)
	s.Equal(extraction.JobStatusProcessing, job.Status)
	s.NotNil(job.StartedAt)
}

func (s *ObjectExtractionJobsTestSuite) TestDequeue_ReturnsNilWhenEmpty() {
	job, err := s.jobsService.Dequeue(s.ctx)

	s.NoError(err)
	s.Nil(job)
}

func (s *ObjectExtractionJobsTestSuite) TestDequeue_RespectsOrder() {
	// Create first job
	job1, err := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})
	s.Require().NoError(err)

	// Create second job
	job2, err := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})
	s.Require().NoError(err)

	// Dequeue one job - should get first (FIFO order)
	dequeuedJob, err := s.jobsService.Dequeue(s.ctx)
	s.Require().NoError(err)
	s.Require().NotNil(dequeuedJob, "dequeue must return a job while pending jobs remain")
	s.Equal(job1.ID, dequeuedJob.ID, "Should dequeue jobs in FIFO order")

	// Per-project serialisation is no longer enforced: DequeueBatch was
	// rewritten to support worker concurrency (object_extraction_worker claims
	// up to Concurrency jobs per poll and runs them in parallel), so a second
	// pending job in the same project IS dequeued even while the first is still
	// processing. Assert the current contract and fail cleanly on a nil result
	// rather than panicking on a nil dereference.
	dequeuedJob2, err := s.jobsService.Dequeue(s.ctx)
	s.Require().NoError(err)
	s.Require().NotNil(dequeuedJob2, "second same-project job must be dequeued while the first is processing")
	s.Equal(job2.ID, dequeuedJob2.ID, "Should dequeue the second job while the first is processing")
}

func (s *ObjectExtractionJobsTestSuite) TestDequeue_DifferentProjectsCanRunInParallel() {
	// Create another project
	otherProjectID := uuid.NewString()
	err := testutil.CreateTestProject(s.ctx, s.testDB.DB, testutil.TestProject{
		ID:    otherProjectID,
		OrgID: s.orgID,
		Name:  "Other Project",
	}, testutil.AdminUser.ID)
	s.Require().NoError(err)

	// Create jobs for different projects
	job1, _ := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})
	job2, _ := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: otherProjectID,
	})

	// Dequeue first job
	dequeuedJob1, err := s.jobsService.Dequeue(s.ctx)
	s.NoError(err)
	s.Equal(job1.ID, dequeuedJob1.ID)

	// Dequeue second job (different project) - should work
	dequeuedJob2, err := s.jobsService.Dequeue(s.ctx)
	s.NoError(err)
	s.NotNil(dequeuedJob2)
	s.Equal(job2.ID, dequeuedJob2.ID)
}

// =============================================================================
// Test: MarkCompleted
// =============================================================================

func (s *ObjectExtractionJobsTestSuite) TestMarkCompleted_UpdatesJob() {
	job, _ := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})

	// Dequeue first
	_, _ = s.jobsService.Dequeue(s.ctx)

	err := s.jobsService.MarkCompleted(s.ctx, job.ID, extraction.ObjectExtractionResults{
		ObjectsCreated:       10,
		RelationshipsCreated: 5,
		SuggestionsCreated:   3,
		TotalItems:           20,
		ProcessedItems:       20,
		SuccessfulItems:      18,
		FailedItems:          2,
		DiscoveredTypes:      extraction.JSONArray{"Person", "Organization"},
		CreatedObjects:       extraction.JSONArray{"obj-1", "obj-2"},
	})
	s.NoError(err)

	// Verify job is completed
	updatedJob, err := s.jobsService.FindByID(s.ctx, job.ID)
	s.NoError(err)
	s.Equal(extraction.JobStatusCompleted, updatedJob.Status)
	s.NotNil(updatedJob.CompletedAt)
	s.Equal(10, updatedJob.ObjectsCreated)
	s.Equal(5, updatedJob.RelationshipsCreated)
	s.Equal(18, updatedJob.SuccessfulItems)
	s.Equal(2, updatedJob.FailedItems)
}

// TestMarkCompleted_ZeroObjects verifies a job that completes with zero created
// objects persists its terminal state. created_object_ids is NOT NULL, so an
// empty result must be written as an empty array, not SQL NULL (issue #894).
func (s *ObjectExtractionJobsTestSuite) TestMarkCompleted_ZeroObjects() {
	job, _ := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})

	_, _ = s.jobsService.Dequeue(s.ctx)

	// Empty result: no objects created, and CreatedObjectIDs left nil.
	err := s.jobsService.MarkCompleted(s.ctx, job.ID, extraction.ObjectExtractionResults{})
	s.Require().NoError(err)

	updatedJob, err := s.jobsService.FindByID(s.ctx, job.ID)
	s.Require().NoError(err)
	s.Equal(extraction.JobStatusCompleted, updatedJob.Status)
	s.NotNil(updatedJob.CompletedAt)

	var isNull bool
	err = s.testDB.DB.NewRaw(
		"SELECT (created_object_ids IS NULL) FROM kb.object_extraction_jobs WHERE id = ?",
		job.ID).Scan(s.ctx, &isNull)
	s.Require().NoError(err)
	s.False(isNull, "created_object_ids must not be NULL for a zero-object completion")

	var cardinality int
	err = s.testDB.DB.NewRaw(
		"SELECT cardinality(created_object_ids) FROM kb.object_extraction_jobs WHERE id = ?",
		job.ID).Scan(s.ctx, &cardinality)
	s.Require().NoError(err)
	s.Equal(0, cardinality, "created_object_ids must be an empty array for a zero-object completion")
}

// =============================================================================
// Test: MarkFailed
// =============================================================================

func (s *ObjectExtractionJobsTestSuite) TestMarkFailed_SchedulesRetry() {
	job, _ := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})

	// Dequeue to set to processing
	_, _ = s.jobsService.Dequeue(s.ctx)

	err := s.jobsService.MarkFailed(s.ctx, job.ID, "LLM API error: rate limited", extraction.JSON{
		"code": "RATE_LIMITED",
	})
	s.NoError(err)

	// Verify job is back to pending (scheduled for retry)
	updatedJob, err := s.jobsService.FindByID(s.ctx, job.ID)
	s.NoError(err)
	s.Equal(extraction.JobStatusPending, updatedJob.Status)
	s.NotNil(updatedJob.ErrorMessage)
	s.Contains(*updatedJob.ErrorMessage, "rate limited")
	s.Equal(1, updatedJob.RetryCount)
}

func (s *ObjectExtractionJobsTestSuite) TestMarkFailed_PermanentlyFailsAfterMaxRetries() {
	job, _ := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})

	// Simulate max retries reached by setting retry_count
	_, _ = s.testDB.DB.NewRaw(
		"UPDATE kb.object_extraction_jobs SET retry_count = 2 WHERE id = ?",
		job.ID).Exec(s.ctx)

	// Dequeue to set to processing
	_, _ = s.jobsService.Dequeue(s.ctx)

	err := s.jobsService.MarkFailed(s.ctx, job.ID, "Final failure", nil)
	s.NoError(err)

	// Verify job is moved to dead letter queue (permanently failed)
	updatedJob, err := s.jobsService.FindByID(s.ctx, job.ID)
	s.NoError(err)
	s.Equal(extraction.JobStatusDeadLetter, updatedJob.Status, "Job should be moved to dead_letter status after max retries")
	s.NotNil(updatedJob.CompletedAt)
	s.Equal(3, updatedJob.RetryCount)
}

// =============================================================================
// Test: UpdateProgress
// =============================================================================

func (s *ObjectExtractionJobsTestSuite) TestUpdateProgress_UpdatesProgressFields() {
	job, _ := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})

	_, _ = s.jobsService.Dequeue(s.ctx)

	err := s.jobsService.UpdateProgress(s.ctx, job.ID, 50, 100)
	s.NoError(err)

	updatedJob, _ := s.jobsService.FindByID(s.ctx, job.ID)
	s.Equal(50, updatedJob.ProcessedItems)
	s.Equal(100, updatedJob.TotalItems)
}

// =============================================================================
// Test: CancelJob
// =============================================================================

func (s *ObjectExtractionJobsTestSuite) TestCancelJob_CancelsPendingJob() {
	job, _ := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})

	err := s.jobsService.CancelJob(s.ctx, job.ID)
	s.NoError(err)

	updatedJob, _ := s.jobsService.FindByID(s.ctx, job.ID)
	s.Equal(extraction.JobStatusCancelled, updatedJob.Status)
	s.NotNil(updatedJob.CompletedAt)
}

func (s *ObjectExtractionJobsTestSuite) TestCancelJob_CancelsProcessingJob() {
	job, _ := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})

	_, _ = s.jobsService.Dequeue(s.ctx)

	err := s.jobsService.CancelJob(s.ctx, job.ID)
	s.NoError(err)

	updatedJob, _ := s.jobsService.FindByID(s.ctx, job.ID)
	s.Equal(extraction.JobStatusCancelled, updatedJob.Status)
}

func (s *ObjectExtractionJobsTestSuite) TestCancelJob_FailsForCompletedJob() {
	job, _ := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})

	_, _ = s.jobsService.Dequeue(s.ctx)
	_ = s.jobsService.MarkCompleted(s.ctx, job.ID, extraction.ObjectExtractionResults{})

	err := s.jobsService.CancelJob(s.ctx, job.ID)
	s.Error(err) // Should fail - can't cancel completed job
}

// =============================================================================
// Test: RecoverStaleJobs
// =============================================================================

func (s *ObjectExtractionJobsTestSuite) TestRecoverStaleJobs_RecoversProcessingJobs() {
	job, _ := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})

	// Dequeue to set to processing
	_, _ = s.jobsService.Dequeue(s.ctx)

	// Manually set started_at to 15 minutes ago to simulate stale job
	_, err := s.testDB.DB.NewRaw(
		"UPDATE kb.object_extraction_jobs SET started_at = now() - interval '15 minutes' WHERE id = ?",
		job.ID).Exec(s.ctx)
	s.NoError(err)

	// Recover with default 10 minute threshold
	count, err := s.jobsService.RecoverStaleJobs(s.ctx)
	s.NoError(err)
	s.Equal(1, count)

	// Verify job is back to pending
	updatedJob, _ := s.jobsService.FindByID(s.ctx, job.ID)
	s.Equal(extraction.JobStatusPending, updatedJob.Status)
	s.Nil(updatedJob.StartedAt)
}

func (s *ObjectExtractionJobsTestSuite) TestRecoverStaleJobs_IgnoresRecentProcessingJobs() {
	_, _ = s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})

	// Dequeue to set to processing (started just now)
	_, _ = s.jobsService.Dequeue(s.ctx)

	// Recover with 10 minute threshold - should not recover
	count, err := s.jobsService.RecoverStaleJobs(s.ctx)
	s.NoError(err)
	s.Equal(0, count)
}

// =============================================================================
// Test: FindByProject
// =============================================================================

func (s *ObjectExtractionJobsTestSuite) TestFindByProject_ReturnsProjectJobs() {
	// Create jobs for our project
	_, _ = s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})
	_, _ = s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})

	// Create job for different project
	otherProjectID := uuid.NewString()
	err := testutil.CreateTestProject(s.ctx, s.testDB.DB, testutil.TestProject{
		ID:    otherProjectID,
		OrgID: s.orgID,
		Name:  "Other Project",
	}, testutil.AdminUser.ID)
	s.Require().NoError(err)

	_, _ = s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: otherProjectID,
	})

	// Find jobs for our project
	jobs, err := s.jobsService.FindByProject(s.ctx, s.projectID, nil, 20)
	s.NoError(err)
	s.Len(jobs, 2)
}

func (s *ObjectExtractionJobsTestSuite) TestFindByProject_WithStatusFilter() {
	// Create pending job
	_, _ = s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})

	// Create and complete another job. A job only reaches 'completed' from
	// 'processing', which is what a worker's claim does, so claim it first.
	job2, _ := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})
	_, err := s.testDB.DB.NewRaw(
		"UPDATE kb.object_extraction_jobs SET status = 'processing', started_at = now() WHERE id = ?",
		job2.ID).Exec(s.ctx)
	s.Require().NoError(err)
	s.Require().NoError(s.jobsService.MarkCompleted(s.ctx, job2.ID, extraction.ObjectExtractionResults{}))

	// Find pending jobs only
	pendingStatus := extraction.JobStatusPending
	jobs, err := s.jobsService.FindByProject(s.ctx, s.projectID, &pendingStatus, 20)
	s.NoError(err)
	s.Len(jobs, 1)
}

// =============================================================================
// Test: FindByDocument
// =============================================================================

func (s *ObjectExtractionJobsTestSuite) TestFindByDocument_ReturnsMatchingJobs() {
	// Create job with document ID
	job, _ := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID:  s.projectID,
		DocumentID: &s.documentID,
	})

	// Create job without document ID
	_, _ = s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})

	// Find by document ID
	jobs, err := s.jobsService.FindByDocument(s.ctx, s.documentID)
	s.NoError(err)
	s.Len(jobs, 1)
	s.Equal(job.ID, jobs[0].ID)
}

// =============================================================================
// Test: Stats
// =============================================================================

func (s *ObjectExtractionJobsTestSuite) TestStats_ReturnsCorrectCounts() {
	// Create pending job
	_, _ = s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})

	// Create a second pending job (same project — per-project serialisation is
	// no longer enforced, so both stay pending until explicitly dequeued).
	_, _ = s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})

	// Create and complete job
	job3, _ := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})
	_, _ = s.testDB.DB.NewRaw(
		"UPDATE kb.object_extraction_jobs SET status = 'completed' WHERE id = ?",
		job3.ID).Exec(s.ctx)

	// Create failed job
	job4, _ := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})
	_, _ = s.testDB.DB.NewRaw(
		"UPDATE kb.object_extraction_jobs SET status = 'failed' WHERE id = ?",
		job4.ID).Exec(s.ctx)

	stats, err := s.jobsService.Stats(s.ctx, &s.projectID)
	s.NoError(err)
	s.Equal(int64(2), stats.Pending)
	s.Equal(int64(0), stats.Processing)
	s.Equal(int64(1), stats.Completed)
	s.Equal(int64(1), stats.Failed)
	s.Equal(int64(4), stats.Total)
}

func (s *ObjectExtractionJobsTestSuite) TestStats_AllProjects() {
	// Create jobs in different projects
	_, _ = s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})

	otherProjectID := uuid.NewString()
	err := testutil.CreateTestProject(s.ctx, s.testDB.DB, testutil.TestProject{
		ID:    otherProjectID,
		OrgID: s.orgID,
		Name:  "Other Project",
	}, testutil.AdminUser.ID)
	s.Require().NoError(err)

	_, _ = s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: otherProjectID,
	})

	// Get stats for all projects (nil projectID)
	stats, err := s.jobsService.Stats(s.ctx, nil)
	s.NoError(err)
	s.Equal(int64(2), stats.Pending)
	s.Equal(int64(2), stats.Total)
}

// =============================================================================
// Test: BulkCancelByProject
// =============================================================================

func (s *ObjectExtractionJobsTestSuite) TestBulkCancelByProject_CancelsAllPendingJobs() {
	// Create multiple jobs
	_, _ = s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})
	_, _ = s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})
	job3, _ := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})
	// Complete one
	_, _ = s.testDB.DB.NewRaw(
		"UPDATE kb.object_extraction_jobs SET status = 'completed' WHERE id = ?",
		job3.ID).Exec(s.ctx)

	count, err := s.jobsService.BulkCancelByProject(s.ctx, s.projectID)
	s.NoError(err)
	s.Equal(2, count) // Only pending jobs cancelled

	// Verify stats
	stats, _ := s.jobsService.Stats(s.ctx, &s.projectID)
	s.Equal(int64(0), stats.Pending)
	s.Equal(int64(2), stats.Cancelled)
	s.Equal(int64(1), stats.Completed)
}

// =============================================================================
// Test: BulkRetryFailed
// =============================================================================

func (s *ObjectExtractionJobsTestSuite) TestBulkRetryFailed_CreatesLinkedRuns() {
	// Two units, two terminal failures. A retry must start a new run for each
	// unit and leave the failed attempt and its error untouched.
	doc1 := s.documentID
	doc2 := uuid.New().String()
	_, err := s.testDB.DB.NewRaw(`
		INSERT INTO kb.documents (id, project_id, source_type, filename, content, created_at, updated_at)
		VALUES (?, ?, 'upload', 'second-document.txt', 'Second document', now(), now())
	`, doc2, s.projectID).Exec(s.ctx)
	s.Require().NoError(err)

	job1, err := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID:  s.projectID,
		DocumentID: &doc1,
	})
	s.Require().NoError(err)
	job2, err := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID:  s.projectID,
		DocumentID: &doc2,
	})
	s.Require().NoError(err)

	_, err = s.testDB.DB.NewRaw(
		"UPDATE kb.object_extraction_jobs SET status = 'dead_letter', error_message = 'test error', retry_count = 3 WHERE id IN (?, ?)",
		job1.ID, job2.ID).Exec(s.ctx)
	s.Require().NoError(err)

	count, err := s.jobsService.BulkRetryFailed(s.ctx, s.projectID)
	s.NoError(err)
	s.Equal(2, count)

	for _, old := range []*extraction.ObjectExtractionJob{job1, job2} {
		preserved, err := s.jobsService.FindByID(s.ctx, old.ID)
		s.NoError(err)
		s.Equal(extraction.JobStatusDeadLetter, preserved.Status, "failed attempt must stay in history")
		s.NotNil(preserved.ErrorMessage)

		next, err := s.jobsService.FindByID(s.ctx, s.linkedJobID(old.ID))
		s.Require().NoError(err)
		s.Require().NotNil(next)
		s.Require().NotNil(next.ReprocessingOf)
		s.Equal(extraction.JobStatusPending, next.Status)
		s.Equal(old.ID, *next.ReprocessingOf)
		s.Equal(0, next.RetryCount)
		s.Equal(3, next.MaxRetries, "retry gets a full budget")
	}
}

// TestBulkRetryFailed_SkipsUnitsWithNewerSuccess pins the latest-attempt rule:
// a unit whose newest job completed must not be retried because an older
// attempt failed.
func (s *ObjectExtractionJobsTestSuite) TestBulkRetryFailed_SkipsUnitsWithNewerSuccess() {
	doc := s.documentID
	failed, err := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID:  s.projectID,
		DocumentID: &doc,
	})
	s.Require().NoError(err)
	_, err = s.testDB.DB.NewRaw(
		"UPDATE kb.object_extraction_jobs SET status = 'failed', error_message = 'old error', created_at = now() - interval '1 hour' WHERE id = ?",
		failed.ID).Exec(s.ctx)
	s.Require().NoError(err)

	newer, err := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID:  s.projectID,
		DocumentID: &doc,
	})
	s.Require().NoError(err)
	_, err = s.testDB.DB.NewRaw(
		"UPDATE kb.object_extraction_jobs SET status = 'completed', completed_at = now() WHERE id = ?",
		newer.ID).Exec(s.ctx)
	s.Require().NoError(err)

	count, err := s.jobsService.BulkRetryFailed(s.ctx, s.projectID)
	s.NoError(err)
	s.Equal(0, count)

	var retried int
	err = s.testDB.DB.NewRaw(
		"SELECT COUNT(*) FROM kb.object_extraction_jobs WHERE reprocessing_of = ?", failed.ID).Scan(s.ctx, &retried)
	s.NoError(err)
	s.Equal(0, retried)
}

// linkedJobID returns the id of the run BulkRetryFailed made for oldID.
func (s *ObjectExtractionJobsTestSuite) linkedJobID(oldID string) string {
	var id string
	err := s.testDB.DB.NewRaw(
		"SELECT id FROM kb.object_extraction_jobs WHERE reprocessing_of = ?", oldID).Scan(s.ctx, &id)
	s.Require().NoError(err)
	s.Require().NotEmpty(id)
	return id
}

// =============================================================================
// Test: DeleteCompleted
// =============================================================================

func (s *ObjectExtractionJobsTestSuite) TestDeleteCompleted_DeletesFinishedJobs() {
	// Create jobs in various states
	job1, _ := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})
	job2, _ := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})
	job3, _ := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})
	_, _ = s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	}) // job4 stays pending

	// Set various statuses
	_, _ = s.testDB.DB.NewRaw(
		"UPDATE kb.object_extraction_jobs SET status = 'completed' WHERE id = ?",
		job1.ID).Exec(s.ctx)
	_, _ = s.testDB.DB.NewRaw(
		"UPDATE kb.object_extraction_jobs SET status = 'failed' WHERE id = ?",
		job2.ID).Exec(s.ctx)
	_, _ = s.testDB.DB.NewRaw(
		"UPDATE kb.object_extraction_jobs SET status = 'cancelled' WHERE id = ?",
		job3.ID).Exec(s.ctx)
	// job4 stays pending

	count, err := s.jobsService.DeleteCompleted(s.ctx, s.projectID)
	s.NoError(err)
	s.Equal(3, count) // completed, failed, cancelled

	// Verify only pending job remains
	stats, _ := s.jobsService.Stats(s.ctx, &s.projectID)
	s.Equal(int64(1), stats.Total)
	s.Equal(int64(1), stats.Pending)
}

// =============================================================================
// Test: RetryJob staleness guard
// =============================================================================

// TestRetryJob_RejectsLiveProcessingJob pins the guard that keeps a retry from
// resetting a run whose worker is still alive. A 'processing' job younger than
// the stale threshold must be refused with ErrJobStillRunning and left
// processing: resetting it would process the same unit twice and let the older
// run overwrite the newer attempt's terminal state.
func (s *ObjectExtractionJobsTestSuite) TestRetryJob_RejectsLiveProcessingJob() {
	job, err := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})
	s.Require().NoError(err)

	// Dequeue claims the job: status processing, started_at = now.
	_, err = s.jobsService.Dequeue(s.ctx)
	s.Require().NoError(err)

	running, err := s.jobsService.FindByID(s.ctx, job.ID)
	s.Require().NoError(err)
	s.Require().Equal(extraction.JobStatusProcessing, running.Status)
	s.Require().NotNil(running.StartedAt)

	_, err = s.jobsService.RetryJob(s.ctx, job.ID, s.projectID)
	s.Require().Error(err)
	s.ErrorIs(err, extraction.ErrJobStillRunning, "a live run must be refused, got %v", err)

	still, err := s.jobsService.FindByID(s.ctx, job.ID)
	s.Require().NoError(err)
	s.Equal(extraction.JobStatusProcessing, still.Status, "a rejected retry must not touch the live job")
	s.Require().NotNil(still.StartedAt)
	s.Equal(*running.StartedAt, *still.StartedAt, "the live run keeps its own start time")
}

// TestRetryJob_RequeuesStaleProcessingJob is the other half of the same guard:
// once the run is older than the threshold, the retry path still requeues it in
// place, which is how a genuinely stuck unit recovers.
func (s *ObjectExtractionJobsTestSuite) TestRetryJob_RequeuesStaleProcessingJob() {
	job, err := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})
	s.Require().NoError(err)
	_, err = s.jobsService.Dequeue(s.ctx)
	s.Require().NoError(err)

	// Past the suite's 10-minute threshold.
	_, err = s.testDB.DB.NewRaw(
		"UPDATE kb.object_extraction_jobs SET started_at = now() - interval '2 hours', retry_count = 2 WHERE id = ?",
		job.ID).Exec(s.ctx)
	s.Require().NoError(err)

	retried, err := s.jobsService.RetryJob(s.ctx, job.ID, s.projectID)
	s.Require().NoError(err)
	s.Require().NotNil(retried)
	s.Equal(job.ID, retried.ID, "a stuck job is requeued in place, not replaced")
	s.Equal(extraction.JobStatusPending, retried.Status)
	s.Equal(0, retried.RetryCount, "the requeue hands back a fresh retry budget")
	s.Nil(retried.StartedAt)
}

// TestRetryJob_UsesWiredStaleThreshold proves the guard reads the threshold the
// stale-job sweep uses (via StaleMinutesResolver) instead of a second, local
// definition: the same 5-minute-old job is refused under a 30-minute threshold
// and requeued under a 2-minute one.
func (s *ObjectExtractionJobsTestSuite) TestRetryJob_UsesWiredStaleThreshold() {
	makeService := func(minutes int) *extraction.ObjectExtractionJobsService {
		return extraction.NewObjectExtractionJobsService(s.testDB.DB, s.log, extraction.DefaultObjectExtractionConfig()).
			WithStaleMinutesResolver(func() int { return minutes })
	}

	job, err := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})
	s.Require().NoError(err)
	_, err = s.jobsService.Dequeue(s.ctx)
	s.Require().NoError(err)
	_, err = s.testDB.DB.NewRaw(
		"UPDATE kb.object_extraction_jobs SET started_at = now() - interval '5 minutes' WHERE id = ?",
		job.ID).Exec(s.ctx)
	s.Require().NoError(err)

	_, err = makeService(30).RetryJob(s.ctx, job.ID, s.projectID)
	s.Require().Error(err)
	s.ErrorIs(err, extraction.ErrJobStillRunning, "5 minutes old is not stuck under a 30-minute threshold")

	retried, err := makeService(2).RetryJob(s.ctx, job.ID, s.projectID)
	s.Require().NoError(err)
	s.Equal(extraction.JobStatusPending, retried.Status, "past the threshold the job is requeued")
}

// TestMarkCompleted_DoesNotOverwriteRequeuedJob covers the second half of the
// double-processing hazard: the worker of the old attempt must not write its
// terminal state onto a job that has since been requeued or cancelled.
func (s *ObjectExtractionJobsTestSuite) TestMarkCompleted_DoesNotOverwriteRequeuedJob() {
	job, err := s.jobsService.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID: s.projectID,
	})
	s.Require().NoError(err)
	_, err = s.jobsService.Dequeue(s.ctx)
	s.Require().NoError(err)

	// Simulate requeueing the stuck row while the old worker is still running.
	_, err = s.testDB.DB.NewRaw(
		"UPDATE kb.object_extraction_jobs SET status = 'pending', started_at = NULL WHERE id = ?",
		job.ID).Exec(s.ctx)
	s.Require().NoError(err)

	err = s.jobsService.MarkCompleted(s.ctx, job.ID, extraction.ObjectExtractionResults{ObjectsCreated: 99})
	s.Require().NoError(err, "an ignored write is not an error for the caller")

	after, err := s.jobsService.FindByID(s.ctx, job.ID)
	s.Require().NoError(err)
	s.Equal(extraction.JobStatusPending, after.Status, "the requeued row keeps its state")
	s.Equal(0, after.ObjectsCreated, "the stale run's results must not be written")
}
