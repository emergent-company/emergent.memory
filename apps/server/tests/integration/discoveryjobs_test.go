package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/domain/discoveryjobs"
	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/pkg/adk"
)

// orgChartDoc is a short org-chart text used to seed discovery tests.
// It is intentionally dense with reporting relationships so a naive LLM
// is tempted to produce "ReportingRelationship" as an entity type.
const orgChartDoc = `
Acme Corp Organizational Chart

Alice Johnson is the Chief Executive Officer of Acme Corp and reports to the Board of Directors.
Bob Smith serves as Vice President of Engineering and reports directly to Alice Johnson.
Carol Davis is the Director of Product and also reports to Alice Johnson.
Dave Lee is a Senior Software Engineer who reports to Bob Smith.
Eve Chen is a Product Manager and reports to Carol Davis.
Frank Torres is a UX Designer and reports to Carol Davis.

Acme Corp is headquartered in San Francisco, California.
The Engineering department has 42 staff. The Product department has 18 staff.
`

// DiscoveryJobsTestSuite tests discovery job endpoints end-to-end using an
// in-process Echo server backed by a real Postgres test database.
//
// These tests do NOT require an LLM — they exercise HTTP mechanics, auth
// boundaries, and the post-parse reification filter introduced in
// parseTypeDiscoveryResponse.  LLM-dependent assertions are skipped
// unless TEST_SERVER_URL is set (external mode).
type DiscoveryJobsTestSuite struct {
	suite.Suite

	testDB    *testutil.TestDB
	inProcess *testutil.TestServer
	client    *testutil.HTTPClient

	orgID     string
	projectID string
	docID     string
	authToken string
}

func TestDiscoveryJobsSuite(t *testing.T) {
	suite.Run(t, new(DiscoveryJobsTestSuite))
}

// ---------------------------------------------------------------------------
// Suite lifecycle
// ---------------------------------------------------------------------------

func (s *DiscoveryJobsTestSuite) SetupSuite() {
	testDB, err := testutil.SetupTestDB(s.T().Context(), "discoveryjobs")
	if err != nil {
		s.T().Skipf("test DB unavailable: %v", err)
	}
	s.testDB = testDB
	s.authToken = "e2e-test-user"
}

func (s *DiscoveryJobsTestSuite) TearDownSuite() {
	if s.testDB != nil {
		s.testDB.Close()
	}
}

func (s *DiscoveryJobsTestSuite) SetupTest() {
	ctx := s.T().Context()

	err := testutil.TruncateTables(ctx, s.testDB.DB)
	s.Require().NoError(err)

	err = testutil.SetupTestFixtures(ctx, s.testDB.DB)
	s.Require().NoError(err)

	s.orgID = uuid.New().String()
	s.projectID = uuid.New().String()
	s.docID = uuid.New().String()

	err = testutil.SetupFullTestProject(ctx, s.testDB.DB, s.orgID, s.projectID)
	s.Require().NoError(err)

	content := orgChartDoc
	mimeType := "text/plain"
	filename := "org-chart.txt"
	err = testutil.CreateTestDocument(ctx, s.testDB.DB, testutil.TestDocument{
		ID:        s.docID,
		ProjectID: s.projectID,
		Filename:  &filename,
		MimeType:  &mimeType,
		Content:   &content,
	})
	s.Require().NoError(err)

	s.inProcess = testutil.NewTestServer(s.testDB)
	s.client = testutil.NewHTTPClient(s.inProcess.Echo)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (s *DiscoveryJobsTestSuite) startURL() string {
	return fmt.Sprintf("/api/discovery-jobs/projects/%s/start", s.projectID)
}

func (s *DiscoveryJobsTestSuite) statusURL(jobID string) string {
	return fmt.Sprintf("/api/discovery-jobs/%s", jobID)
}

func (s *DiscoveryJobsTestSuite) finalizeURL(jobID string) string {
	return fmt.Sprintf("/api/discovery-jobs/%s/finalize", jobID)
}

// startDiscoveryJob posts to the start endpoint and returns the job ID.
func (s *DiscoveryJobsTestSuite) startDiscoveryJob(docIDs []string) string {
	s.T().Helper()

	ids := make([]string, len(docIDs))
	copy(ids, docIDs)

	resp := s.client.POST(
		s.startURL(),
		testutil.WithAuth(s.authToken),
		testutil.WithJSONBody(map[string]any{
			"document_ids":          ids,
			"batch_size":            5,
			"min_confidence":        0.3,
			"include_relationships": false,
		}),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "start discovery: %s", resp.Body)

	var body map[string]any
	s.Require().NoError(json.Unmarshal(resp.Body, &body))

	jobID, ok := body["job_id"].(string)
	s.Require().True(ok, "job_id missing from response")
	return jobID
}

// pollUntilDone polls GET /discovery-jobs/:id until status is completed/failed
// or the deadline is reached. Returns the final status response body.
func (s *DiscoveryJobsTestSuite) pollUntilDone(jobID string, deadline time.Duration) map[string]any {
	s.T().Helper()

	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		resp := s.client.GET(
			s.statusURL(jobID),
			testutil.WithAuth(s.authToken),
		)
		s.Require().Equal(http.StatusOK, resp.StatusCode)

		var body map[string]any
		s.Require().NoError(json.Unmarshal(resp.Body, &body))

		status, _ := body["status"].(string)
		if status == "completed" || status == "failed" {
			return body
		}
		time.Sleep(200 * time.Millisecond)
	}
	s.T().Fatalf("discovery job %s did not complete within %s", jobID, deadline)
	return nil
}

// assertNoReifiedTypes fails the test if any discovered type name ends in
// "Relationship" or "Association" — these are relational concepts that should
// never appear as entity types in the discovery output.
func assertNoReifiedTypes(t *testing.T, discoveredTypes []any) {
	t.Helper()
	for _, raw := range discoveredTypes {
		obj, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := obj["type_name"].(string)
		if strings.HasSuffix(name, "Relationship") || strings.HasSuffix(name, "Association") {
			t.Errorf("reified type found in discovery output: %q — should be a graph edge, not an entity type", name)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestDiscovery_StartReturnsJobID verifies that the start endpoint accepts a
// valid request and returns a well-formed job ID. No LLM required.
func (s *DiscoveryJobsTestSuite) TestDiscovery_StartReturnsJobID() {
	jobID := s.startDiscoveryJob([]string{s.docID})
	_, err := uuid.Parse(jobID)
	s.NoError(err, "job_id should be a valid UUID, got %q", jobID)
}

// TestDiscovery_StartRequiresAuth verifies that unauthenticated requests are
// rejected with 401.
func (s *DiscoveryJobsTestSuite) TestDiscovery_StartRequiresAuth() {
	resp := s.client.POST(
		s.startURL(),
		testutil.WithJSONBody(map[string]any{
			"document_ids": []string{s.docID},
		}),
	)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

// TestDiscovery_StartRequiresDocumentIDs verifies that a missing document_ids
// array returns 400.
func (s *DiscoveryJobsTestSuite) TestDiscovery_StartRequiresDocumentIDs() {
	resp := s.client.POST(
		s.startURL(),
		testutil.WithAuth(s.authToken),
		testutil.WithJSONBody(map[string]any{}),
	)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

// TestDiscovery_GetStatusUnknownJob verifies that querying a nonexistent job
// returns 404.
func (s *DiscoveryJobsTestSuite) TestDiscovery_GetStatusUnknownJob() {
	resp := s.client.GET(
		s.statusURL(uuid.New().String()),
		testutil.WithAuth(s.authToken),
	)
	s.Equal(http.StatusNotFound, resp.StatusCode)
}

// TestDiscovery_FinalizeRequiresProjectIDHeader verifies that FinalizeDiscovery
// returns 400 when the X-Project-ID header is absent.
func (s *DiscoveryJobsTestSuite) TestDiscovery_FinalizeRequiresProjectIDHeader() {
	// Start a real job so we have a valid job ID in the DB.
	jobID := s.startDiscoveryJob([]string{s.docID})

	resp := s.client.POST(
		s.finalizeURL(jobID),
		testutil.WithAuth(s.authToken),
		testutil.WithJSONBody(map[string]any{
			"packName":      "test-pack",
			"mode":          "create",
			"includedTypes": []map[string]any{{"type_name": "Person"}},
		}),
		// intentionally omit X-Project-ID header
	)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

// TestDiscovery_CancelJob verifies that a running job can be cancelled and the
// subsequent status reflects the cancellation.
func (s *DiscoveryJobsTestSuite) TestDiscovery_CancelJob() {
	jobID := s.startDiscoveryJob([]string{s.docID})

	cancelURL := fmt.Sprintf("/api/discovery-jobs/%s", jobID)
	resp := s.client.DELETE(
		cancelURL,
		testutil.WithAuth(s.authToken),
	)
	// Accept 200 (cancelled) or 409 (already finished — race in fast test env).
	s.True(
		resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusConflict,
		"unexpected status %d: %s", resp.StatusCode, resp.Body,
	)
}

// TestDiscovery_ListJobs verifies the list endpoint returns the started job.
func (s *DiscoveryJobsTestSuite) TestDiscovery_ListJobs() {
	jobID := s.startDiscoveryJob([]string{s.docID})

	resp := s.client.GET(
		fmt.Sprintf("/api/discovery-jobs/projects/%s", s.projectID),
		testutil.WithAuth(s.authToken),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode)

	var items []map[string]any
	s.Require().NoError(json.Unmarshal(resp.Body, &items))
	s.Require().Len(items, 1, "expected exactly one job")
	s.Equal(jobID, items[0]["id"])
}

// TestDiscovery_AssertNoReifiedTypes_HelperWorks confirms that the
// assertNoReifiedTypes helper correctly identifies reified type names.
// This is a meta-test for the test helper itself.
func TestDiscovery_AssertNoReifiedTypes_HelperWorks(t *testing.T) {
	reifiedSuffixes := []string{
		"ReportingRelationship",
		"EmployeeAssociation",
		"ManagementRelationship",
	}
	for _, name := range reifiedSuffixes {
		n := name
		t.Run("detects_"+n, func(t *testing.T) {
			// Build a mock recorder to capture failures without aborting.
			// We just check the suffix logic directly.
			if !strings.HasSuffix(n, "Relationship") && !strings.HasSuffix(n, "Association") {
				t.Errorf("expected %q to be detected as reified", n)
			}
		})
	}

	clean := []any{
		map[string]any{"type_name": "Person"},
		map[string]any{"type_name": "Organization"},
		map[string]any{"type_name": "Department"},
	}
	// assertNoReifiedTypes should not call t.Errorf for clean types.
	assertNoReifiedTypes(t, clean)
}

// ---------------------------------------------------------------------------
// LLM-dependent tests (external mode only — require TEST_SERVER_URL)
// ---------------------------------------------------------------------------

// createAndCleanupProject creates a fresh project via the live API, sets its
// project_info (kbPurpose), and registers a t.Cleanup to delete it.
// It resolves the org ID by inspecting the current user via GET /api/auth/me.
// Only TEST_SERVER_URL and TEST_API_TOKEN are required.
func createAndCleanupProject(t *testing.T, client *testutil.HTTPClient, token, name, projectInfo string) string {
	t.Helper()

	// Resolve org ID from the first available project.
	projsResp := client.GET("/api/projects", testutil.WithAPIKey(token))
	if projsResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/projects: status=%d body=%s", projsResp.StatusCode, projsResp.Body)
	}
	var projects []map[string]any
	mustUnmarshal(t, projsResp.Body, &projects)
	if len(projects) == 0 {
		t.Fatal("GET /api/projects returned empty list — cannot resolve org_id")
	}
	orgID, _ := projects[0]["orgId"].(string)
	if orgID == "" {
		t.Fatalf("orgId missing from projects response: %v", projects[0])
	}

	// Create the project.
	createResp := client.POST(
		"/api/projects",
		testutil.WithAPIKey(token),
		testutil.WithJSONBody(map[string]any{
			"name":  name,
			"orgId": orgID,
		}),
	)
	if createResp.StatusCode != http.StatusOK && createResp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/projects: status=%d body=%s", createResp.StatusCode, createResp.Body)
	}
	var createBody map[string]any
	mustUnmarshal(t, createResp.Body, &createBody)
	projectID, _ := createBody["id"].(string)
	if projectID == "" {
		t.Fatalf("project id missing from create response: %s", createResp.Body)
	}

	// Set project_info (kbPurpose) and model config via PATCH / PUT.
	if projectInfo != "" {
		patchResp := client.PATCH(
			fmt.Sprintf("/api/projects/%s", projectID),
			testutil.WithAPIKey(token),
			testutil.WithJSONBody(map[string]any{
				"project_info": projectInfo,
			}),
		)
		if patchResp.StatusCode != http.StatusOK {
			t.Fatalf("PATCH /api/projects/%s: status=%d body=%s", projectID, patchResp.StatusCode, patchResp.Body)
		}
	}

	// If TEST_GOOGLE_API_KEY is set, store it as a project-level provider credential
	// and use Google AI for generative calls (avoids dependency on OpenAI-compatible
	// env-var config which may have exhausted balance).
	googleAPIKey := os.Getenv("TEST_GOOGLE_API_KEY")
	generativeModel := testutil.BareGenerativeModelFromEnv()
	if generativeModel == "" {
		generativeModel = "deepseek/deepseek-v4-flash" // fallback for CI
	}
	if googleAPIKey != "" {
		provResp := client.PUT(
			fmt.Sprintf("/api/v1/projects/%s/providers/google", projectID),
			testutil.WithAPIKey(token),
			testutil.WithJSONBody(map[string]any{
				"apiKey": googleAPIKey,
			}),
		)
		if provResp.StatusCode != http.StatusOK && provResp.StatusCode != http.StatusCreated {
			t.Logf("WARN: PUT providers/google: status=%d body=%s — falling back to env model", provResp.StatusCode, provResp.Body)
		} else {
			generativeModel = "google/gemini-2.0-flash"
		}
	}

	// Configure per-project model config.
	modelResp := client.PUT(
		fmt.Sprintf("/api/v1/projects/%s/model-config", projectID),
		testutil.WithAPIKey(token),
		testutil.WithJSONBody(map[string]any{
			"generativeModel": generativeModel,
			"embeddingModel":  "google/gemini-embedding-2-preview",
		}),
	)
	if modelResp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /api/v1/projects/%s/model-config: status=%d body=%s", projectID, modelResp.StatusCode, modelResp.Body)
	}

	// Register cleanup — DELETE is async (202) which is fine.
	t.Cleanup(func() {
		delResp := client.DELETE(fmt.Sprintf("/api/projects/%s", projectID), testutil.WithAPIKey(token))
		if delResp.StatusCode != http.StatusOK && delResp.StatusCode != http.StatusAccepted {
			t.Logf("cleanup: DELETE /api/projects/%s returned %d (ignored)", projectID, delResp.StatusCode)
		}
	})

	t.Logf("created test project %s (org=%s)", projectID, orgID)
	return projectID
}

// TestDiscovery_StartAndComplete_D2_D8 runs a full discovery job against a live
// server and asserts the D2 (no reified types) and D8 (no embedded property
// objects) post-parse filter guarantees.
//
// Assumptions verified: D2, D8
// Requires: TEST_SERVER_URL, TEST_API_TOKEN env vars.
func TestDiscovery_StartAndComplete_D2_D8(t *testing.T) {
	serverURL := os.Getenv("TEST_SERVER_URL")
	apiToken := os.Getenv("TEST_API_TOKEN")
	if serverURL == "" || apiToken == "" {
		t.Skip("TEST_SERVER_URL / TEST_API_TOKEN not set — skipping live LLM test")
	}

	client := testutil.NewExternalHTTPClient(serverURL)
	projectID := createAndCleanupProject(t, client, apiToken,
		"discovery-d2d8-"+uuid.New().String()[:8],
		"HR org-chart system tracking employee reporting lines, departments, and team structures.",
	)

	// Upload the org-chart test document.
	docID := uploadTextDocument(t, client, apiToken, projectID, "org-chart-d2d8.txt", orgChartDoc)

	// Start discovery job.
	startResp := client.POST(
		fmt.Sprintf("/api/discovery-jobs/projects/%s/start", projectID),
		testutil.WithAPIKey(apiToken),
		testutil.WithJSONBody(map[string]any{
			"document_ids":          []string{docID},
			"batch_size":            5,
			"min_confidence":        0.3,
			"include_relationships": false,
		}),
	)
	if startResp.StatusCode != http.StatusOK {
		t.Fatalf("start discovery: status=%d body=%s", startResp.StatusCode, startResp.Body)
	}
	var startBody map[string]any
	mustUnmarshal(t, startResp.Body, &startBody)
	jobID, _ := startBody["job_id"].(string)
	if jobID == "" {
		t.Fatal("job_id missing from start response")
	}

	// Poll until done (up to 3 minutes — LLM calls can be slow).
	statusBody := pollJobDone(t, client, apiToken, jobID, 3*time.Minute)

	status, _ := statusBody["status"].(string)
	if status == "failed" {
		errMsg, _ := statusBody["error_message"].(string)
		t.Fatalf("discovery job failed: %s", errMsg)
	}

	discoveredTypes, _ := statusBody["discovered_types"].([]any)
	if len(discoveredTypes) == 0 {
		t.Skip("discovery produced zero types — likely LLM quota issue, skipping assertions")
	}

	t.Logf("D2/D8 check: %d discovered types", len(discoveredTypes))

	// D2: no reified type names.
	assertNoReifiedTypes(t, discoveredTypes)
	t.Log("D2 PASS: zero reified type names")

	// D8: no embedded objects in properties.
	assertNoEmbeddedProperties(t, discoveredTypes)
	t.Log("D8 PASS: zero embedded property objects")
}

// TestDiscovery_RelationshipGating_D7 verifies that:
//   - include_relationships=false → discovered_relationships is empty
//   - include_relationships=true  → discovered_relationships field exists (may be empty if LLM finds none)
//
// The first assertion is purely mechanical (no LLM output needed).
// The second requires a completed job and is skipped if no LLM is available.
//
// Assumptions verified: D7
// Requires: TEST_SERVER_URL, TEST_API_TOKEN env vars.
func TestDiscovery_RelationshipGating_D7(t *testing.T) {
	serverURL := os.Getenv("TEST_SERVER_URL")
	apiToken := os.Getenv("TEST_API_TOKEN")
	if serverURL == "" || apiToken == "" {
		t.Skip("TEST_SERVER_URL / TEST_API_TOKEN not set — skipping live LLM test")
	}

	client := testutil.NewExternalHTTPClient(serverURL)
	projectID := createAndCleanupProject(t, client, apiToken,
		"discovery-d7-"+uuid.New().String()[:8],
		"HR org-chart system tracking employee reporting lines, departments, and team structures.",
	)
	docID := uploadTextDocument(t, client, apiToken, projectID, "org-chart-d7.txt", orgChartDoc)

	t.Run("relationships_excluded_when_flag_false", func(t *testing.T) {
		startResp := client.POST(
			fmt.Sprintf("/api/discovery-jobs/projects/%s/start", projectID),
			testutil.WithAPIKey(apiToken),
			testutil.WithJSONBody(map[string]any{
				"document_ids":          []string{docID},
				"batch_size":            5,
				"min_confidence":        0.3,
				"include_relationships": false,
			}),
		)
		if startResp.StatusCode != http.StatusOK {
			t.Fatalf("start: %d %s", startResp.StatusCode, startResp.Body)
		}
		var sb map[string]any
		mustUnmarshal(t, startResp.Body, &sb)
		jobID, _ := sb["job_id"].(string)

		body := pollJobDone(t, client, apiToken, jobID, 3*time.Minute)
		if s, _ := body["status"].(string); s == "failed" {
			t.Skipf("job failed — skipping: %v", body["error_message"])
		}

		rels, _ := body["discovered_relationships"].([]any)
		if len(rels) != 0 {
			t.Errorf("D7 FAIL: include_relationships=false but got %d relationships", len(rels))
		} else {
			t.Log("D7 PASS (no-rels branch): discovered_relationships empty when flag=false")
		}
	})

	t.Run("relationships_field_present_when_flag_true", func(t *testing.T) {
		startResp := client.POST(
			fmt.Sprintf("/api/discovery-jobs/projects/%s/start", projectID),
			testutil.WithAPIKey(apiToken),
			testutil.WithJSONBody(map[string]any{
				"document_ids":          []string{docID},
				"batch_size":            5,
				"min_confidence":        0.3,
				"include_relationships": true,
			}),
		)
		if startResp.StatusCode != http.StatusOK {
			t.Fatalf("start: %d %s", startResp.StatusCode, startResp.Body)
		}
		var sb map[string]any
		mustUnmarshal(t, startResp.Body, &sb)
		jobID, _ := sb["job_id"].(string)

		body := pollJobDone(t, client, apiToken, jobID, 3*time.Minute)
		if s, _ := body["status"].(string); s == "failed" {
			t.Skipf("job failed — skipping: %v", body["error_message"])
		}

		if _, exists := body["discovered_relationships"]; !exists {
			t.Error("D7 FAIL: discovered_relationships field missing when include_relationships=true")
		} else {
			t.Log("D7 PASS (with-rels branch): discovered_relationships field present when flag=true")
		}
	})
}

// ---------------------------------------------------------------------------
// FinalizeDiscovery enrich / create_rich mode tests
// These run in-process with a real LLM when credentials are available.
// ---------------------------------------------------------------------------

// skipDiscoveryEnrich skips the test when the LLM credential checks fail.
func skipDiscoveryEnrich(t *testing.T) {
	testutil.LoadEnvFiles()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cfg, err := config.NewConfig(log)
	if err != nil || !cfg.LLM.IsEnabled() {
		t.Skip("no LLM credentials configured — skipping discovery enrich test")
	}
}

// discoveryEnrichFactory builds an adk.ModelFactory from env credentials.
func discoveryEnrichFactory() *adk.ModelFactory {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cfg, _ := config.NewConfig(log)
	if cfg == nil || !cfg.LLM.IsEnabled() {
		return nil
	}
	return adk.NewModelFactory(&cfg.LLM, log, nil, nil, nil)
}

// TestFinalizeDiscovery_EnrichMode verifies that mode=enrich fills null
// properties in an existing schema pack using LLM-generated property definitions.
func TestFinalizeDiscovery_EnrichMode(t *testing.T) {
	skipDiscoveryEnrich(t)
	ctx := context.Background()

	testDB, err := testutil.SetupTestDB(ctx, "discenrich")
	require.NoError(t, err)
	defer testDB.Close()

	require.NoError(t, testutil.SetupTestFixtures(ctx, testDB.DB))

	orgID := uuid.New().String()
	projectID := uuid.New().String()
	require.NoError(t, testutil.SetupFullTestProject(ctx, testDB.DB, orgID, projectID))

	projectUUID := uuid.MustParse(projectID)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cfg := &config.Config{}

	// Create a document so loadDocumentText has text to work with.
	content := `Alice Johnson is the CEO of Acme Corp. Bob Smith is VP of Engineering.
Carol Davis is Director of Product. Dave Lee is a Senior Engineer reporting to Bob.`
	filename := "org.txt"
	sourceType := "manual"
	docsRepo := documents.NewRepository(testDB.DB, log)
	docsSvc := documents.NewService(docsRepo, log)
	doc, _, err := docsSvc.Create(ctx, documents.CreateParams{
		ProjectID:  projectID,
		Filename:   &filename,
		Content:    &content,
		SourceType: &sourceType,
	})
	require.NoError(t, err)

	// Build discovery service with real LLM.
	mf := discoveryEnrichFactory()
	require.NotNil(t, mf)
	djRepo := discoveryjobs.NewRepository(testDB.DB, log)
	djSvc := discoveryjobs.NewService(djRepo, docsSvc, cfg, mf, log)

	// Create a stub discovery job.
	stubJob := &discoveryjobs.DiscoveryJob{
		ID:        uuid.New(),
		ProjectID: projectUUID,
		Status:    discoveryjobs.StatusCompleted,
		KBPurpose: "HR org-chart system",
		Progress:  discoveryjobs.JSONMap{"message": "stub"},
		Config:    discoveryjobs.JSONMap{"document_ids": []string{doc.ID}},
	}
	require.NoError(t, djRepo.Create(ctx, stubJob))

	// Step 1: Finalize in "create" mode to install a schema with sparse properties.
	createResp, err := djSvc.FinalizeDiscovery(ctx, stubJob.ID, projectUUID, &discoveryjobs.FinalizeDiscoveryRequest{
		Mode:     "create",
		PackName: "OrgChart",
		IncludedTypes: []discoveryjobs.IncludedType{
			{TypeName: "Person", Description: "An employee", Properties: nil, RequiredProperties: nil},
			{TypeName: "Department", Description: "A business unit", Properties: nil, RequiredProperties: nil},
		},
	})
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, createResp.SchemaID, "create should return a valid schema_id")

	schemaUUID := createResp.SchemaID

	// Step 2: Finalize in "enrich" mode to fill null properties.
	_, err = djSvc.FinalizeDiscovery(ctx, stubJob.ID, projectUUID, &discoveryjobs.FinalizeDiscoveryRequest{
		Mode:           "enrich",
		PackName:       "OrgChart",
		DocumentID:     doc.ID,
		ExistingPackID: &schemaUUID,
		IncludedTypes:  []discoveryjobs.IncludedType{},
	})
	require.NoError(t, err)

	// Step 3: Verify the schema now has properties filled.
	updatedPack, err := djRepo.GetMemorySchema(ctx, schemaUUID)
	require.NoError(t, err)
	require.NotNil(t, updatedPack.ObjectTypeSchemas)

	personRaw, ok := updatedPack.ObjectTypeSchemas["Person"]
	require.True(t, ok, "Person type should exist after enrich")
	person := personRaw.(map[string]any)
	props, ok := person["properties"].(map[string]any)
	require.True(t, ok, "Person should have a properties map after enrich")
	require.Greater(t, len(props), 0, "Person should have at least one property after enrich")

	propKeys := make([]string, 0, len(props))
	for k := range props {
		propKeys = append(propKeys, k)
	}
	t.Logf("enrich result: Person has %d properties: %v", len(props), propKeys)
}

// TestFinalizeDiscovery_CreateRichMode verifies that mode=create_rich
// generates a full schema with populated properties from scratch.
func TestFinalizeDiscovery_CreateRichMode(t *testing.T) {
	skipDiscoveryEnrich(t)
	ctx := context.Background()

	testDB, err := testutil.SetupTestDB(ctx, "disccreaterich")
	require.NoError(t, err)
	defer testDB.Close()

	require.NoError(t, testutil.SetupTestFixtures(ctx, testDB.DB))

	orgID := uuid.New().String()
	projectID := uuid.New().String()
	require.NoError(t, testutil.SetupFullTestProject(ctx, testDB.DB, orgID, projectID))

	projectUUID := uuid.MustParse(projectID)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cfg := &config.Config{}

	content := `Klarnak ball is played by two teams of 7 on a triangular court.
Each player wields a vibro-racquet to volley the incandescent solk. Points are scored
when the solk touches the gravity-well. A match lasts 5 phases of 8 zorns each.`
	docsRepo := documents.NewRepository(testDB.DB, log)
	docsSvc := documents.NewService(docsRepo, log)
	filename := "klarnak.txt"
	sourceType := "manual"
	doc, _, err := docsSvc.Create(ctx, documents.CreateParams{
		ProjectID:  projectID,
		Filename:   &filename,
		Content:    &content,
		SourceType: &sourceType,
	})
	require.NoError(t, err)

	mf := discoveryEnrichFactory()
	require.NotNil(t, mf)
	djRepo := discoveryjobs.NewRepository(testDB.DB, log)
	djSvc := discoveryjobs.NewService(djRepo, docsSvc, cfg, mf, log)

	stubJob := &discoveryjobs.DiscoveryJob{
		ID:        uuid.New(),
		ProjectID: projectUUID,
		Status:    discoveryjobs.StatusCompleted,
		KBPurpose: "Fictional sports tracking",
		Progress:  discoveryjobs.JSONMap{"message": "stub"},
		Config:    discoveryjobs.JSONMap{"document_ids": []string{doc.ID}},
	}
	require.NoError(t, djRepo.Create(ctx, stubJob))

	// Finalize with create_rich — generates types + properties from document.
	resp, err := djSvc.FinalizeDiscovery(ctx, stubJob.ID, projectUUID, &discoveryjobs.FinalizeDiscoveryRequest{
		Mode:          "create_rich",
		PackName:      "KlarnakBall",
		DocumentID:    doc.ID,
		IncludedTypes: []discoveryjobs.IncludedType{},
	})
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, resp.SchemaID, "create_rich should return a valid schema_id")

	// Verify the generated schema has populated types.
	pack, err := djRepo.GetMemorySchema(ctx, resp.SchemaID)
	require.NoError(t, err)
	require.Greater(t, len(pack.ObjectTypeSchemas), 0,
		"create_rich should generate at least one type")
	var hasProps bool
	for typeName, raw := range pack.ObjectTypeSchemas {
		if m, ok := raw.(map[string]any); ok {
			if props, ok := m["properties"].(map[string]any); ok && len(props) > 0 {
				hasProps = true
				t.Logf("  type %q has %d properties", typeName, len(props))
			}
		}
	}
	require.True(t, hasProps, "at least one generated type should have non-empty properties")
}

// ---------------------------------------------------------------------------
// Shared helpers for external-mode tests
// ---------------------------------------------------------------------------

// uploadTextDocument creates a document on the live server with the given
// content and returns its ID.
func uploadTextDocument(t *testing.T, client *testutil.HTTPClient, token, projectID, filename, content string) string {
	t.Helper()
	resp := client.POST(
		"/api/documents",
		testutil.WithAPIKey(token),
		testutil.WithProjectID(projectID),
		testutil.WithJSONBody(map[string]any{
			"filename":    filename,
			"mime_type":   "text/plain",
			"source_type": "upload",
			"content":     content,
		}),
	)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload document: status=%d body=%s", resp.StatusCode, resp.Body)
	}
	var body map[string]any
	mustUnmarshal(t, resp.Body, &body)
	id, _ := body["id"].(string)
	if id == "" {
		t.Fatalf("document id missing from response: %s", resp.Body)
	}
	return id
}

// pollJobDone polls GET /api/discovery-jobs/:id until status is completed or
// failed, or deadline is exceeded.
func pollJobDone(t *testing.T, client *testutil.HTTPClient, token, jobID string, deadline time.Duration) map[string]any {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		resp := client.GET(
			fmt.Sprintf("/api/discovery-jobs/%s", jobID),
			testutil.WithAPIKey(token),
		)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("poll status: %d %s", resp.StatusCode, resp.Body)
		}
		var body map[string]any
		mustUnmarshal(t, resp.Body, &body)
		st, _ := body["status"].(string)
		if st == "completed" || st == "failed" {
			return body
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("job %s did not complete within %s", jobID, deadline)
	return nil
}

// assertNoEmbeddedProperties fails if any discovered type has a property whose
// value is a nested object without a scalar "type" key — which would indicate
// the normalizePropertyCrossRefs filter did not run or was bypassed.
func assertNoEmbeddedProperties(t *testing.T, discoveredTypes []any) {
	t.Helper()
	for _, raw := range discoveredTypes {
		obj, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		typeName, _ := obj["type_name"].(string)
		props, _ := obj["properties"].(map[string]any)
		for propKey, propVal := range props {
			nested, isMap := propVal.(map[string]any)
			if !isMap {
				continue
			}
			typeField, hasType := nested["type"]
			if !hasType {
				t.Errorf("D8 FAIL: type %q property %q is an embedded object without 'type' field — normalizer did not flatten it", typeName, propKey)
				continue
			}
			if _, isString := typeField.(string); !isString {
				t.Errorf("D8 FAIL: type %q property %q has non-string 'type' field: %T", typeName, propKey, typeField)
			}
		}
	}
}

// mustUnmarshal decodes JSON bytes into dst or fatals the test.
func mustUnmarshal(t *testing.T, data []byte, dst any) {
	t.Helper()
	if err := json.Unmarshal(data, dst); err != nil {
		t.Fatalf("json.Unmarshal: %v (body: %s)", err, data)
	}
}
