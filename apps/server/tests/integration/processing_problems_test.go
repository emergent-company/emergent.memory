package integration

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/domain/processingproblems"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// ProcessingProblemsTestSuite covers the read model's counting contract: the
// report caps how many rows it carries per queue, but it must never present
// that cap as the size of the backlog.
type ProcessingProblemsTestSuite struct {
	suite.Suite
	testDB    *testutil.TestDB
	ctx       context.Context
	svc       *processingproblems.Service
	log       *slog.Logger
	orgID     string
	projectID string
}

func TestProcessingProblemsSuite(t *testing.T) {
	suite.Run(t, new(ProcessingProblemsTestSuite))
}

func (s *ProcessingProblemsTestSuite) SetupSuite() {
	s.ctx = context.Background()
	s.log = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

	testDB, err := testutil.SetupTestDB(s.ctx, "processing_problems")
	s.Require().NoError(err, "Failed to setup test database")
	s.testDB = testDB

	s.svc = processingproblems.NewService(processingproblems.NewRepository(testDB.DB), s.log)
}

func (s *ProcessingProblemsTestSuite) TearDownSuite() {
	if s.testDB != nil {
		s.testDB.Close()
	}
}

func (s *ProcessingProblemsTestSuite) SetupTest() {
	s.Require().NoError(testutil.TruncateTables(s.ctx, s.testDB.DB))
	s.Require().NoError(testutil.SetupTestFixtures(s.ctx, s.testDB.DB))

	s.orgID = uuid.New().String()
	s.projectID = uuid.New().String()
	s.Require().NoError(testutil.SetupFullTestProject(s.ctx, s.testDB.DB, s.orgID, s.projectID))
}

// insertFailedConversions creates n terminal conversion failures, one unit each.
// document_id stays NULL so COALESCE(document_id, id) makes every row its own
// unit, which is the shape of a backlog of unparsed uploads.
func (s *ProcessingProblemsTestSuite) insertFailedConversions(n int) {
	_, err := s.testDB.DB.NewRaw(`
		INSERT INTO kb.document_parsing_jobs
			(id, organization_id, project_id, status, source_type, source_filename, error_message,
			 retry_count, max_retries, created_at, updated_at)
		SELECT gen_random_uuid(), ?, ?, 'failed', 'upload', 'scan-' || g || '.pdf',
		       'provider timeout', 3, 3,
		       now() - make_interval(secs => g), now() - make_interval(secs => g)
		FROM generate_series(1, ?) g
	`, s.orgID, s.projectID, n).Exec(s.ctx)
	s.Require().NoError(err)
}

// TestReportCountsBeyondRowCap is the regression test for the cap being
// reported as the truth: 264 failed conversions must read as 264, not 200.
func (s *ProcessingProblemsTestSuite) TestReportCountsBeyondRowCap() {
	const failed = 264
	s.insertFailedConversions(failed)

	// A second queue in the same area proves the area count is a sum of honest
	// per-queue counts, not a count of returned rows.
	_, err := s.testDB.DB.NewRaw(`
		INSERT INTO kb.object_extraction_jobs (project_id, status, error_message)
		VALUES (?, 'failed', 'invalid json'), (?, 'failed', 'invalid json')
	`, s.projectID, s.projectID).Exec(s.ctx)
	s.Require().NoError(err)

	report, err := s.svc.List(s.ctx, s.projectID)
	s.Require().NoError(err)

	s.Require().Len(report.Areas, 1, "both queues belong to the documents area")
	documents := report.Areas[0]
	s.Equal(processingproblems.AreaDocuments, documents.Area)
	s.Equal(failed+2, documents.Count, "the area count is the real backlog")
	s.Equal(200+2, len(documents.Problems), "the report still caps how many rows it carries")
	s.True(documents.Truncated, "a capped area must advertise the cap")
	s.Equal(failed+2, report.Total, "the report total is not capped either")

	// The sum of the visible class breakdown must not be mistaken for the
	// total: it describes the sample, which is what Truncated is for.
	visible := 0
	for _, c := range documents.ByClass {
		visible += c.Count
	}
	s.Equal(len(documents.Problems), visible)
}

// TestReportExactUnderCap keeps the small case honest: nothing is truncated, so
// the count and the rows agree and the UI can print the number unqualified.
func (s *ProcessingProblemsTestSuite) TestReportExactUnderCap() {
	s.insertFailedConversions(3)

	report, err := s.svc.List(s.ctx, s.projectID)
	s.Require().NoError(err)
	s.Require().Len(report.Areas, 1)

	documents := report.Areas[0]
	s.Equal(3, documents.Count)
	s.Equal(3, len(documents.Problems))
	s.False(documents.Truncated)
	s.Equal(3, report.Total)
}
