package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/discoveryjobs"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// ---------------------------------------------------------------------------
// scope=project, the per-run cap, the overlap guard and retry
// ---------------------------------------------------------------------------

// createDocument seeds one more analysable document in the suite's project.
func (s *DiscoveryJobsTestSuite) createDocument(filename, content string) string {
	s.T().Helper()
	id := uuid.New().String()
	mimeType := "text/plain"
	s.Require().NoError(testutil.CreateTestDocument(s.T().Context(), s.testDB.DB, testutil.TestDocument{
		ID:        id,
		ProjectID: s.projectID,
		Filename:  &filename,
		MimeType:  &mimeType,
		Content:   &content,
	}))
	return id
}

// jobConfigRow reads a job's stored config straight from the database.
func (s *DiscoveryJobsTestSuite) jobConfigRow(jobID string) map[string]any {
	s.T().Helper()
	var raw []byte
	err := s.testDB.DB.NewSelect().
		Table("kb.discovery_jobs").
		Column("config").
		Where("id = ?", uuid.MustParse(jobID)).
		Scan(s.T().Context(), &raw)
	s.Require().NoError(err, "read job config")
	var cfg map[string]any
	s.Require().NoError(json.Unmarshal(raw, &cfg))
	return cfg
}

// jobStatusRow reads a job's status straight from the database.
func (s *DiscoveryJobsTestSuite) jobStatusRow(jobID string) string {
	s.T().Helper()
	var status string
	err := s.testDB.DB.NewSelect().
		Table("kb.discovery_jobs").
		Column("status").
		Where("id = ?", uuid.MustParse(jobID)).
		Scan(s.T().Context(), &status)
	s.Require().NoError(err, "read job status")
	return status
}

// insertJobWithConfig inserts a job directly in the given status with a stored
// config, without launching the background goroutine. Retry and overlap tests
// need a terminal/non-terminal job whose state cannot race the assertion.
func (s *DiscoveryJobsTestSuite) insertJobWithConfig(status string, cfg map[string]any) string {
	s.T().Helper()
	job := &discoveryjobs.DiscoveryJob{
		ID:                      uuid.New(),
		OrganizationID:          uuid.MustParse(s.orgID),
		ProjectID:               uuid.MustParse(s.projectID),
		Status:                  status,
		Progress:                discoveryjobs.JSONMap{},
		Config:                  discoveryjobs.JSONMap(cfg),
		KBPurpose:               "test",
		DiscoveredTypes:         discoveryjobs.JSONArray{},
		DiscoveredRelationships: discoveryjobs.JSONArray{},
		CreatedAt:               time.Now(),
		UpdatedAt:               time.Now(),
	}
	if status == discoveryjobs.StatusFailed {
		msg := "All 1 batches failed. Errors: Batch 1: failed to parse LLM response: unexpected EOF"
		job.ErrorMessage = &msg
	}
	_, err := s.testDB.DB.NewInsert().Model(job).Exec(s.T().Context())
	s.Require().NoError(err)
	return job.ID.String()
}

// TestDiscovery_ProjectScopeResolvesDocuments pins that scope=project makes the
// server resolve the document set itself: the caller names no ids, and the run
// covers the project's analysable documents.
func (s *DiscoveryJobsTestSuite) TestDiscovery_ProjectScopeResolvesDocuments() {
	s.createDocument("policy.txt", "The security policy is owned by Carol Davis.")

	resp := s.client.POST(
		s.startURL(),
		testutil.WithAuth(s.authToken),
		testutil.WithJSONBody(map[string]any{
			"scope":                 "project",
			"batch_size":            5,
			"min_confidence":        0.3,
			"include_relationships": false,
		}),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "project-scope start: %s", resp.Body)

	var body discoveryjobs.StartDiscoveryResponse
	s.Require().NoError(json.Unmarshal(resp.Body, &body))
	s.Require().NotEmpty(body.JobID.String())
	s.Equal(2, body.DocumentCount, "the run should cover both documents")
	s.Equal(2, body.TotalDocuments, "the project total should be reported")

	cfg := s.jobConfigRow(body.JobID.String())
	s.Equal("project", cfg["scope"])
	s.Equal("manual", cfg["trigger"])
	s.Len(cfg["document_ids"], 2)
	s.EqualValues(2, cfg["document_count"])
	s.EqualValues(2, cfg["total_documents"])
}

// TestDiscovery_ProjectScopeLimitReportsSubset pins the cap contract: a run
// over a project larger than the requested limit analyses the newest documents
// and reports the project total, so the caller can say it covered a subset
// instead of silently skipping documents.
func (s *DiscoveryJobsTestSuite) TestDiscovery_ProjectScopeLimitReportsSubset() {
	s.createDocument("policy.txt", "The security policy is owned by Carol Davis.")

	resp := s.client.POST(
		s.startURL(),
		testutil.WithAuth(s.authToken),
		testutil.WithJSONBody(map[string]any{
			"scope": "project",
			"limit": 1,
		}),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "limited project-scope start: %s", resp.Body)

	var body discoveryjobs.StartDiscoveryResponse
	s.Require().NoError(json.Unmarshal(resp.Body, &body))
	s.Equal(1, body.DocumentCount, "limit should bound the run")
	s.Equal(2, body.TotalDocuments, "the project total stays visible")

	cfg := s.jobConfigRow(body.JobID.String())
	s.Len(cfg["document_ids"], 1)
	s.EqualValues(2, cfg["total_documents"])
}

// TestDiscovery_ProjectScopeEmptyProject verifies the honest failure for a
// project with nothing to analyse, rather than a job that fails later with an
// unhelpful message.
func (s *DiscoveryJobsTestSuite) TestDiscovery_ProjectScopeEmptyProject() {
	emptyProjectID := uuid.New().String()
	s.Require().NoError(testutil.CreateTestProject(s.T().Context(), s.testDB.DB, testutil.TestProject{
		ID:    emptyProjectID,
		OrgID: s.orgID,
		Name:  "empty-project-" + emptyProjectID[:8],
	}, testutil.AdminUser.ID))
	s.Require().NoError(testutil.CreateTestProjectMembership(s.T().Context(), s.testDB.DB, emptyProjectID, testutil.AdminUser.ID, "project_admin"))

	resp := s.client.POST(
		fmt.Sprintf("/api/discovery-jobs/projects/%s/start", emptyProjectID),
		testutil.WithAuth(s.authToken),
		testutil.WithJSONBody(map[string]any{"scope": "project"}),
	)
	s.Equal(http.StatusBadRequest, resp.StatusCode, "empty project: %s", resp.Body)
}

// TestDiscovery_OverlapGuardRejectsSecondRun pins one run per project: a
// second start while a job is in flight returns 409 and runs nothing, so two
// jobs cannot double the spend and race to finalise packs over the same
// documents.
func (s *DiscoveryJobsTestSuite) TestDiscovery_OverlapGuardRejectsSecondRun() {
	running := s.insertPendingJob()

	resp := s.client.POST(
		s.startURL(),
		testutil.WithAuth(s.authToken),
		testutil.WithJSONBody(map[string]any{"document_ids": []string{s.docID}}),
	)
	s.Equal(http.StatusConflict, resp.StatusCode, "second start while running: %s", resp.Body)

	// Still exactly one job: the refused start created nothing.
	listResp := s.client.GET(
		fmt.Sprintf("/api/discovery-jobs/projects/%s", s.projectID),
		testutil.WithAuth(s.authToken),
	)
	s.Require().Equal(http.StatusOK, listResp.StatusCode)
	var items []map[string]any
	s.Require().NoError(json.Unmarshal(listResp.Body, &items))
	s.Require().Len(items, 1)
	s.Equal(running, items[0]["id"])
}

// TestDiscovery_RetryClonesTerminalJob pins the retry contract: the new job
// reuses the stored document set (never a caller-supplied one), records
// trigger=manual, and leaves the failed job in place for history.
func (s *DiscoveryJobsTestSuite) TestDiscovery_RetryClonesTerminalJob() {
	failed := s.insertJobWithConfig(discoveryjobs.StatusFailed, map[string]any{
		"document_ids":          []string{s.docID},
		"batch_size":            5,
		"min_confidence":        0.3,
		"include_relationships": false,
		"max_iterations":        3,
		"scope":                 "documents",
		"trigger":               "source_sync",
		"document_count":        1,
		"total_documents":       1,
	})

	resp := s.client.POST(
		fmt.Sprintf("/api/discovery-jobs/%s/retry", failed),
		testutil.WithAuth(s.authToken),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "retry: %s", resp.Body)

	var body discoveryjobs.StartDiscoveryResponse
	s.Require().NoError(json.Unmarshal(resp.Body, &body))
	s.Require().NotEqual(failed, body.JobID.String(), "retry must create a new job")
	s.Equal(1, body.DocumentCount)

	cfg := s.jobConfigRow(body.JobID.String())
	s.Equal("documents", cfg["scope"], "a retry re-runs the stored document set")
	s.Equal("manual", cfg["trigger"], "an explicit retry is a manual run")
	s.Require().Len(cfg["document_ids"], 1)
	s.Equal(s.docID, cfg["document_ids"].([]any)[0])

	// The original stays failed and untouched.
	s.Equal(discoveryjobs.StatusFailed, s.jobStatusRow(failed))
}

// TestDiscovery_RetryRefusesRunningJob pins that retry is a terminal-state
// action: a job still in flight returns 409 instead of starting a second run.
func (s *DiscoveryJobsTestSuite) TestDiscovery_RetryRefusesRunningJob() {
	pending := s.insertPendingJob()

	resp := s.client.POST(
		fmt.Sprintf("/api/discovery-jobs/%s/retry", pending),
		testutil.WithAuth(s.authToken),
	)
	s.Equal(http.StatusConflict, resp.StatusCode, "retry a running job: %s", resp.Body)
}

// TestDiscovery_RetryRefusesJobWithoutDocuments pins the guard for a job whose
// stored config has no document list: it cannot be re-run, so the caller is
// told to start a new run instead of receiving an opaque failure.
func (s *DiscoveryJobsTestSuite) TestDiscovery_RetryRefusesJobWithoutDocuments() {
	legacy := s.insertJobWithConfig(discoveryjobs.StatusFailed, map[string]any{})

	resp := s.client.POST(
		fmt.Sprintf("/api/discovery-jobs/%s/retry", legacy),
		testutil.WithAuth(s.authToken),
	)
	s.Equal(http.StatusBadRequest, resp.StatusCode, "retry without documents: %s", resp.Body)
}

// TestDiscovery_RetryUnknownJob verifies a retry of a nonexistent job is a 404,
// not a 500 from an unparsable stored config.
func (s *DiscoveryJobsTestSuite) TestDiscovery_RetryUnknownJob() {
	resp := s.client.POST(
		fmt.Sprintf("/api/discovery-jobs/%s/retry", uuid.New().String()),
		testutil.WithAuth(s.authToken),
	)
	s.Equal(http.StatusNotFound, resp.StatusCode)
}

// TestDiscovery_ListReportsTriggerAndCoverage pins the list fields the UI card
// relies on: trigger and the document coverage that reveals a capped run.
func (s *DiscoveryJobsTestSuite) TestDiscovery_ListReportsTriggerAndCoverage() {
	s.insertJobWithConfig(discoveryjobs.StatusFailed, map[string]any{
		"document_ids":    []string{s.docID},
		"scope":           "documents",
		"trigger":         "source_sync",
		"document_count":  500,
		"total_documents": 934,
	})

	resp := s.client.GET(
		fmt.Sprintf("/api/discovery-jobs/projects/%s", s.projectID),
		testutil.WithAuth(s.authToken),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode)

	var items []map[string]any
	s.Require().NoError(json.Unmarshal(resp.Body, &items))
	s.Require().Len(items, 1)
	s.Equal("source_sync", items[0]["trigger"])
	s.EqualValues(500, items[0]["document_count"])
	s.EqualValues(934, items[0]["total_documents"])
	s.Contains(items[0]["error_message"], "unexpected EOF")
}

// TestProject_AutoDiscoveryDefaultsOnAndRoundTrips pins the setting end to end:
// a project defaults to automatic discovery on, and PATCH persists a change so
// the sync worker's gate reads what the settings toggle wrote.
func (s *DiscoveryJobsTestSuite) TestProject_AutoDiscoveryDefaultsOnAndRoundTrips() {
	// A project created by the fixture defaults to on.
	var enabled bool
	err := s.testDB.DB.NewSelect().
		Table("kb.projects").
		Column("auto_discovery_enabled").
		Where("id = ?", uuid.MustParse(s.projectID)).
		Scan(s.T().Context(), &enabled)
	s.Require().NoError(err, "read default")
	s.True(enabled, "auto_discovery_enabled should default to true")

	resp := s.client.PATCH(
		"/api/projects/"+s.projectID,
		testutil.WithAuth(s.authToken),
		testutil.WithJSONBody(map[string]any{"auto_discovery_enabled": false}),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "patch auto_discovery_enabled: %s", resp.Body)

	var got bool
	err = s.testDB.DB.NewSelect().
		Table("kb.projects").
		Column("auto_discovery_enabled").
		Where("id = ?", uuid.MustParse(s.projectID)).
		Scan(s.T().Context(), &got)
	s.Require().NoError(err, "read after patch")
	s.False(got, "auto_discovery_enabled should be false after patching false")
}
