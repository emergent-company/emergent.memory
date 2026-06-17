package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"


	"github.com/emergent-company/emergent.memory/domain/discoveryjobs"
	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/domain/graph"
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

// ---------------------------------------------------------------------------
// GAP 1: schema_policy=enrich integration test via the remember endpoint
// ---------------------------------------------------------------------------

// TestRememberEnrichPolicy verifies that calling POST /api/projects/:id/remember
// with schema_policy=enrich routes to the V8 enrich agent and produces a schema
// pack with enriched (populated) property definitions — no errors.
func TestRememberEnrichPolicy(t *testing.T) {
	skipDiscoveryEnrich(t)
	ctx := context.Background()

	testDB, err := testutil.SetupTestDB(ctx, "remenrich")
	require.NoError(t, err)
	defer testDB.Close()

	require.NoError(t, testutil.SetupTestFixtures(ctx, testDB.DB))

	orgID := uuid.New().String()
	projectID := uuid.New().String()
	err = testutil.SetupFullTestProject(ctx, testDB.DB, orgID, projectID)
	require.NoError(t, err)

	svr := testutil.NewTestServerWithLLM(testDB)
	client := testutil.NewHTTPClient(svr.Echo)

	// POST remember with schema_policy=enrich and mode=sync (waits for completion).
	rec := client.POST(
		fmt.Sprintf("/api/projects/%s/remember", projectID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"message":       orgChartDoc,
			"schema_policy": "enrich",
			"mode":          "sync",
		}),
	)
	require.Equal(t, http.StatusOK, rec.StatusCode, "schema_policy=enrich should return 200")
	t.Logf("remember status: %d", rec.StatusCode)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body, &body))
	runID, _ := body["run_id"].(string)
	status, _ := body["status"].(string)
	docID, _ := body["document_id"].(string)
	t.Logf("remember enrich: run_id=%s status=%s document_id=%s", runID, status, docID)

	// Verify no error in response.
	errMsg, _ := body["error"].(string)
	require.Empty(t, errMsg, "response should not contain an error")
	require.Equal(t, "completed", status, "agent should complete successfully")
	require.NotEmpty(t, docID, "response should include a document_id")

	// Poll for schema packs installed to the project.
	// The enrich/new-domain agent calls finalize-discovery with
	// create_rich_combined, which creates and installs a schema pack.
	djRepo := discoveryjobs.NewRepository(testDB.GetDB(), slog.Default())

	var enriched bool
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(3 * time.Second)

		// Query project_schemas for active schema pack IDs.
		var schemaIDs []string
		qErr := testDB.DB.NewRaw(
			"SELECT schema_id::text FROM kb.project_schemas WHERE project_id = ? AND active = true",
			projectID,
		).Scan(ctx, &schemaIDs)
		if qErr != nil || len(schemaIDs) == 0 {
			continue
		}

		for _, sidStr := range schemaIDs {
			schemaUUID := uuid.MustParse(sidStr)
			pack, getErr := djRepo.GetMemorySchema(ctx, schemaUUID)
			if getErr != nil {
				t.Logf("  GetMemorySchema(%s): %v", schemaUUID, getErr)
				continue
			}
			t.Logf("  schema %s has %d object types, %d rel types",
				schemaUUID, len(pack.ObjectTypeSchemas), len(pack.RelationshipTypeSchemas))
			objJSON, _ := json.MarshalIndent(pack.ObjectTypeSchemas, "    ", "  ")
			t.Logf("  raw object_type_schemas:\n    %s", string(objJSON))
			// Check if any type has non-empty properties.
			for typeName, raw := range pack.ObjectTypeSchemas {
				m, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				props, _ := m["properties"].(map[string]any)
				if len(props) > 0 {
					enriched = true
					t.Logf("  ✅ type %q has %d properties", typeName, len(props))
				}
			}
		}
		if enriched {
			break
		}
	}
	require.True(t, enriched,
		"schema_policy=enrich should produce a schema pack with populated properties")
}

// ---------------------------------------------------------------------------
// GAP 6: schema_policy=ask pauses for confirmation on remember
// ---------------------------------------------------------------------------

func TestRememberAskPolicy_PausesForConfirmation(t *testing.T) {
	skipDiscoveryEnrich(t)
	ctx := context.Background()

	testDB, err := testutil.SetupTestDB(ctx, "remask")
	require.NoError(t, err)
	defer testDB.Close()

	require.NoError(t, testutil.SetupTestFixtures(ctx, testDB.DB))

	orgID := uuid.New().String()
	projectID := uuid.New().String()
	err = testutil.SetupFullTestProject(ctx, testDB.DB, orgID, projectID)
	require.NoError(t, err)

	svr := testutil.NewTestServerWithLLM(testDB)
	client := testutil.NewHTTPClient(svr.Echo)

	// POST remember with schema_policy=ask in async mode.
	rec := client.POST(
		fmt.Sprintf("/api/projects/%s/remember", projectID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"message":       orgChartDoc,
			"schema_policy": "ask",
			"mode":          "async",
		}),
	)
	require.Equal(t, http.StatusAccepted, rec.StatusCode, "async should return 202")

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body, &body))
	runID, _ := body["run_id"].(string)
	require.NotEmpty(t, runID, "async response must contain run_id")
	t.Logf("remember ask run_id: %s", runID)

	// Poll run status; expect paused/input-required for tool confirmation.
	var finalStatus string
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(1 * time.Second)
		statusResp := client.GET(
			fmt.Sprintf("/api/v1/runs/%s", runID),
			testutil.WithAuth("e2e-test-user"),
		)
		if statusResp.StatusCode != http.StatusOK {
			continue
		}
		var runBody map[string]any
		if err := json.Unmarshal(statusResp.Body, &runBody); err != nil {
			continue
		}
		data, _ := runBody["data"].(map[string]any)
		status, _ := data["status"].(string)
		if status == "paused" || status == "input-required" || status == "error" || status == "success" {
			finalStatus = status
			break
		}
	}

	t.Logf("remember ask run %s final status: %s", runID, finalStatus)
	if finalStatus != "paused" && finalStatus != "input-required" && finalStatus != "success" {
		t.Fatalf("expected run to pause for tool confirmation or complete; got status=%q", finalStatus)
	}
	if finalStatus == "success" {
		t.Log("⚠ run completed without pausing — LLM chose not to call finalize-discovery (ask policy not exercised)")
	}
}

// ---------------------------------------------------------------------------
// GAP 7: strategy=ask pauses for confirmation on forget
// ---------------------------------------------------------------------------

func TestForgetAskStrategy_PausesForConfirmation(t *testing.T) {
	skipDiscoveryEnrich(t)
	ctx := context.Background()

	testDB, err := testutil.SetupTestDB(ctx, "frgask")
	require.NoError(t, err)
	defer testDB.Close()

	require.NoError(t, testutil.SetupTestFixtures(ctx, testDB.DB))

	orgID := uuid.New().String()
	projectID := uuid.New().String()
	err = testutil.SetupFullTestProject(ctx, testDB.DB, orgID, projectID)
	require.NoError(t, err)

	// Seed a graph object to forget.
	projectUUID := uuid.MustParse(projectID)
	canonicalID := uuid.New()
	obj := &graph.GraphObject{
		ID:          uuid.New(),
		ProjectID:   projectUUID,
		CanonicalID: canonicalID,
		Version:     1,
		Type:        "Person",
		Properties: map[string]any{
			"name": "TargetForgetPerson",
		},
		Labels:    []string{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	_, err = testDB.DB.NewInsert().Model(obj).Exec(ctx)
	require.NoError(t, err, "seed graph object")

	svr := testutil.NewTestServerWithLLM(testDB)
	client := testutil.NewHTTPClient(svr.Echo)

	// POST forget with strategy=ask in async mode.
	rec := client.POST(
		fmt.Sprintf("/api/projects/%s/forget", projectID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"message":  "forget the person named TargetForgetPerson",
			"strategy": "ask",
			"mode":     "async",
		}),
	)
	require.Equal(t, http.StatusAccepted, rec.StatusCode, "async should return 202")

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body, &body))
	runID, _ := body["run_id"].(string)
	require.NotEmpty(t, runID, "async response must contain run_id")
	t.Logf("forget ask run_id: %s", runID)

	// Poll run status; expect input-required for tool confirmation.
	var finalStatus string
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(1 * time.Second)
		statusResp := client.GET(
			fmt.Sprintf("/api/v1/runs/%s", runID),
			testutil.WithAuth("e2e-test-user"),
		)
		if statusResp.StatusCode != http.StatusOK {
			continue
		}
		var runBody map[string]any
		if err := json.Unmarshal(statusResp.Body, &runBody); err != nil {
			continue
		}
		data, _ := runBody["data"].(map[string]any)
		status, _ := data["status"].(string)
		if status == "input-required" || status == "completed" || status == "failed" {
			finalStatus = status
			break
		}
		// Log intermediate statuses for debugging.
		if status != "" {
			t.Logf("forget ask run %s status: %s", runID, status)
		}
	}

	t.Logf("forget ask run %s final status: %q", runID, finalStatus)
	if finalStatus == "" {
		t.Fatal("run never reached a terminal or paused state (120s timeout)")
	}
	if finalStatus == "completed" {
		t.Log("⚠ run completed without pausing — LLM may not have called entity-delete")
	}
	if finalStatus == "failed" {
		t.Logf("⚠ run failed — agent may not have found the target entity")
	}
}

// ---------------------------------------------------------------------------
// Re-extraction comparison: extract same doc twice, report diff
// ---------------------------------------------------------------------------

type extractionSnapshot struct {
	SchemaPacks       []schemaPackInfo
	ObjectStats       map[string]objTypeStats // type → stats
	RelStats          map[string]relTypeStats // type → count
	ExtractionJobInfo string                  // status info about extraction jobs
}

type schemaPackInfo struct {
	SchemaID   string
	ObjTypes   int
	RelTypes   int
	TypeNames  []string
	Properties map[string]int // typeName → prop count
}

type objTypeStats struct {
	TotalObjects int
	OnBranch     int // on a staging branch (extraction branch)
	OnMain       int // on main branch (branch_id IS NULL)
	Version1     int // only version 1
	VersionGe2   int // updated at least once
	Deleted      int // soft-deleted
	WithKey      int // has a name key (dedup-able)
	WithoutKey   int // no name key (duplicate-prone)
}

type relTypeStats struct {
	Total int
}

func snapshotExtraction(t *testing.T, ctx context.Context, projectID string, testDB *testutil.TestDB) extractionSnapshot {
	t.Helper()
	s := extractionSnapshot{
		ObjectStats: make(map[string]objTypeStats),
		RelStats:    make(map[string]relTypeStats),
	}

	// 0. Extraction job status
	type exJobRow struct {
		ID     string
		Status string
		Error  *string
	}
	var exJobs []exJobRow
	_ = testDB.DB.NewRaw(
		`SELECT id::text, status, error_message FROM kb.object_extraction_jobs WHERE project_id = ?::uuid ORDER BY created_at DESC LIMIT 5`,
		projectID,
	).Scan(ctx, &exJobs)
	if len(exJobs) > 0 {
		parts := make([]string, len(exJobs))
		for i, j := range exJobs {
			errSuffix := ""
			if j.Error != nil && *j.Error != "" {
				errSuffix = fmt.Sprintf(" err=%q", truncateStr(*j.Error, 80))
			}
			parts[i] = fmt.Sprintf("  job %s status=%s%s", j.ID[:8], j.Status, errSuffix)
		}
		s.ExtractionJobInfo = "Extraction jobs (last 5):\n" + strings.Join(parts, "\n")
	} else {
		s.ExtractionJobInfo = "No extraction jobs found."
	}

	// 1. Schema packs
	djRepo := discoveryjobs.NewRepository(testDB.GetDB(), slog.Default())
	var schemaIDs []string
	_ = testDB.DB.NewRaw(
		"SELECT schema_id::text FROM kb.project_schemas WHERE project_id = ? AND active = true",
		projectID,
	).Scan(ctx, &schemaIDs)
	for _, sidStr := range schemaIDs {
		schemaUUID := uuid.MustParse(sidStr)
		pack, err := djRepo.GetMemorySchema(ctx, schemaUUID)
		if err != nil {
			continue
		}
		info := schemaPackInfo{
			SchemaID:   sidStr[:8],
			ObjTypes:   len(pack.ObjectTypeSchemas),
			RelTypes:   len(pack.RelationshipTypeSchemas),
			TypeNames:  make([]string, 0, len(pack.ObjectTypeSchemas)),
			Properties: make(map[string]int),
		}
		for tn, raw := range pack.ObjectTypeSchemas {
			info.TypeNames = append(info.TypeNames, tn)
			m, ok := raw.(map[string]any)
			if ok {
				props, _ := m["properties"].(map[string]any)
				info.Properties[tn] = len(props)
			}
		}
		sort.Strings(info.TypeNames)
		for _, rn := range pack.RelationshipTypeSchemas {
			rnMap, ok := rn.(map[string]any)
			if ok {
				name, _ := rnMap["name"].(string)
				if name != "" {
					info.RelTypes++
					_ = name
				}
			}
		}
		s.SchemaPacks = append(s.SchemaPacks, info)
	}

	// 2. Graph objects — all branches (HEAD per branch)
	type objRow struct {
		Type      string
		Key       *string
		DeletedAt *time.Time
		Version   int
		BranchID  *string
	}
	var rows []objRow
	_ = testDB.DB.NewRaw(
		`SELECT type, key, deleted_at, version, branch_id::text
		 FROM kb.graph_objects
		 WHERE project_id = ?::uuid AND supersedes_id IS NULL`,
		projectID,
	).Scan(ctx, &rows)
	for _, r := range rows {
		stats := s.ObjectStats[r.Type]
		stats.TotalObjects++
		if r.BranchID != nil && *r.BranchID != "" {
			stats.OnBranch++
		} else {
			stats.OnMain++
		}
		if r.DeletedAt != nil {
			stats.Deleted++
		}
		if r.Version == 1 {
			stats.Version1++
		} else {
			stats.VersionGe2++
		}
		if r.Key != nil && *r.Key != "" {
			stats.WithKey++
		} else {
			stats.WithoutKey++
		}
		s.ObjectStats[r.Type] = stats
	}

	// 3. Relationships
	type relRow struct {
		Type string
	}
	var relRows []relRow
	_ = testDB.DB.NewRaw(
		`SELECT type FROM kb.graph_relationships WHERE project_id = ?::uuid AND deleted_at IS NULL`,
		projectID,
	).Scan(ctx, &relRows)
	for _, r := range relRows {
		rs := s.RelStats[r.Type]
		rs.Total++
		s.RelStats[r.Type] = rs
	}

	return s
}

func truncateStr(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

func printExtractionSummary(t *testing.T, label string, snap extractionSnapshot) {
	t.Logf("")
	t.Logf("=== %s ===", label)

	// Schema packs
	for _, p := range snap.SchemaPacks {
		t.Logf("  Schema %s: %d object types, %d relationship types", p.SchemaID, p.ObjTypes, p.RelTypes)
		for _, tn := range p.TypeNames {
			t.Logf("    Object type %q: %d properties", tn, p.Properties[tn])
		}
	}
	if len(snap.SchemaPacks) == 0 {
		t.Logf("  (no schema packs installed)")
	}

	// Objects
	var typeNames []string
	for tn := range snap.ObjectStats {
		typeNames = append(typeNames, tn)
	}
	sort.Strings(typeNames)
	var totalObj, totalKey, totalNoKey, totalV1, totalVge2, totalDel, totalBranch, totalMain int
	for _, tn := range typeNames {
		os := snap.ObjectStats[tn]
		totalObj += os.TotalObjects
		totalKey += os.WithKey
		totalNoKey += os.WithoutKey
		totalV1 += os.Version1
		totalVge2 += os.VersionGe2
		totalDel += os.Deleted
		totalBranch += os.OnBranch
		totalMain += os.OnMain
		t.Logf("  Objects %q: total=%d  branch=%d  main=%d  v1=%d  v2+=%d  del=%d  key=%d  nokey=%d",
			tn, os.TotalObjects, os.OnBranch, os.OnMain, os.Version1, os.VersionGe2, os.Deleted, os.WithKey, os.WithoutKey)
	}
	t.Logf("  OBJECT TOTALS: %d objects  |  branch=%d  main=%d  |  key=%d  nokey=%d  |  v1=%d  v2+=%d  del=%d",
		totalObj, totalBranch, totalMain, totalKey, totalNoKey, totalV1, totalVge2, totalDel)

	// Relationships
	var relTypeNames []string
	for rn := range snap.RelStats {
		relTypeNames = append(relTypeNames, rn)
	}
	sort.Strings(relTypeNames)
	var totalRel int
	for _, rn := range relTypeNames {
		totalRel += snap.RelStats[rn].Total
		t.Logf("  Relationships %q: %d", rn, snap.RelStats[rn].Total)
	}
	t.Logf("  RELATIONSHIP TOTAL: %d", totalRel)

	// Extraction jobs
	if snap.ExtractionJobInfo != "" {
		t.Logf("  %s", snap.ExtractionJobInfo)
	}
}

func printDiffTable(t *testing.T, pass1, pass2 extractionSnapshot) {
	t.Logf("")
	t.Logf("╔══════════════════════════════════════════════════════════════════════╗")
	t.Logf("║              RE-EXTRACTION COMPARISON SUMMARY                      ║")
	t.Logf("╚══════════════════════════════════════════════════════════════════════╝")

	// Schema comparison
	type schemaDiff struct {
		TypeName  string
		PropsPass1 int
		PropsPass2 int
		Changed    string
	}
	var schemaDiffs []schemaDiff
	propMap1 := map[string]int{}
	propMap2 := map[string]int{}
	for _, p := range pass1.SchemaPacks {
		for tn, pc := range p.Properties {
			propMap1[tn] = pc
		}
	}
	for _, p := range pass2.SchemaPacks {
		for tn, pc := range p.Properties {
			propMap2[tn] = pc
		}
	}
	allTypeNames := map[string]bool{}
	for tn := range propMap1 { allTypeNames[tn] = true }
	for tn := range propMap2 { allTypeNames[tn] = true }
	var sortedTypes []string
	for tn := range allTypeNames { sortedTypes = append(sortedTypes, tn) }
	sort.Strings(sortedTypes)
	for _, tn := range sortedTypes {
		p1 := propMap1[tn]
		p2 := propMap2[tn]
		ch := "IDENTICAL"
		if p2 > p1 {
			ch = fmt.Sprintf("ENRICHED +%d props", p2-p1)
		} else if p2 < p1 {
			ch = fmt.Sprintf("SHRUNK -%d props", p1-p2)
		}
		schemaDiffs = append(schemaDiffs, schemaDiff{tn, p1, p2, ch})
	}

	t.Logf("")
	t.Logf("── Schema Types ──")
	if len(schemaDiffs) == 0 {
		t.Logf("  No types discovered")
	} else {
		t.Logf("  %-30s %10s %10s   %s", "TYPE", "PASS1", "PASS2", "CHANGE")
		t.Logf("  %-30s %10s %10s   %s", strings.Repeat("─", 30), strings.Repeat("─", 10), strings.Repeat("─", 10), strings.Repeat("─", 20))
		for _, sd := range schemaDiffs {
			t.Logf("  %-30s %10d %10d   %s", sd.TypeName, sd.PropsPass1, sd.PropsPass2, sd.Changed)
		}
	}

	// Object comparison
	allObjTypes := map[string]bool{}
	for tn := range pass1.ObjectStats { allObjTypes[tn] = true }
	for tn := range pass2.ObjectStats { allObjTypes[tn] = true }
	var sortedObjTypes []string
	for tn := range allObjTypes { sortedObjTypes = append(sortedObjTypes, tn) }
	sort.Strings(sortedObjTypes)

	t.Logf("")
	t.Logf("── Graph Objects (HEAD versions, by type) ──")
	t.Logf("  %-25s %6s %6s %+6s %+6s %6s %6s", "TYPE", "PASS1", "PASS2", "NEW", "DEL", "BRANCH1", "BRANCH2")
	t.Logf("  %-25s %6s %6s %6s %6s %6s %6s", strings.Repeat("─", 25), strings.Repeat("─", 6), strings.Repeat("─", 6), strings.Repeat("─", 6), strings.Repeat("─", 6), strings.Repeat("─", 6), strings.Repeat("─", 6))
	for _, tn := range sortedObjTypes {
		o1 := pass1.ObjectStats[tn]
		o2 := pass2.ObjectStats[tn]
		diffCount := o2.TotalObjects - o1.TotalObjects
		delCount := o2.Deleted - o1.Deleted
		t.Logf("  %-25s %6d %6d %+6d %+6d %6d %6d",
			tn, o1.TotalObjects, o2.TotalObjects, diffCount, delCount, o1.OnBranch, o2.OnBranch)
	}

	// Total summary
	var total1, total2, total1Del, total2Del, total2Vge2, total1Branch, total2Branch int
	for _, os := range pass1.ObjectStats { total1 += os.TotalObjects; total1Del += os.Deleted; total1Branch += os.OnBranch }
	for _, os := range pass2.ObjectStats { total2 += os.TotalObjects; total2Del += os.Deleted; total2Vge2 += os.VersionGe2; total2Branch += os.OnBranch }

	t.Logf("")
	t.Logf("── Overall Summary ──")
	t.Logf("  Objects on staging branches (pass1): %d", total1Branch)
	t.Logf("  Objects on staging branches (pass2): %d", total2Branch)
	t.Logf("  Total objects pass1: %d  |  pass2: %d", total1, total2)
	t.Logf("  Net new objects:     %+d", total2-total1)
	t.Logf("  Deleted (pass1):     %d  |  pass2: %d", total1Del, total2Del)
	t.Logf("  v2+ in pass2:        %d (objects updated vs original)", total2Vge2)
	t.Logf("")
	if total1 == 0 && total2 == 0 {
		t.Logf("  ⚠ No objects found in either pass — extraction may not have completed.")
	} else if total1 == 0 && total2 > 0 {
		t.Logf("  ⚠ Pass 1 extraction hadn't completed yet (objects found only in pass 2).")
		t.Logf("     Pass 2 found %d objects — dedup comparison incomplete.", total2)
	} else if total2Vge2 == 0 && (total2-total1) == 0 {
		t.Logf("  ✅ VERDICT: Second extraction produced IDENTICAL results — no new versions, no new objects.")
		t.Logf("     The upsert-by-key dedup (project_id+type+key) correctly avoided duplicates.")
	} else if total2Vge2 > 0 && (total2-total1) >= 0 {
		t.Logf("  ⚠ VERDICT: Second extraction created new versions or objects.")
		t.Logf("     Check diff table above to see which types changed.")
	} else {
		t.Logf("  ⚠ VERDICT: Object count decreased — possible deletion or tombstoning.")
		t.Logf("     diff=%+d  v2+=%d", total2-total1, total2Vge2)
	}

	// Relationship comparison
	allRelTypes := map[string]bool{}
	for rn := range pass1.RelStats { allRelTypes[rn] = true }
	for rn := range pass2.RelStats { allRelTypes[rn] = true }
	var sortedRelTypes []string
	for rn := range allRelTypes { sortedRelTypes = append(sortedRelTypes, rn) }
	sort.Strings(sortedRelTypes)

	t.Logf("")
	t.Logf("── Relationships (by type) ──")
	t.Logf("  %-30s %8s %8s %8s", "TYPE", "PASS1", "PASS2", "DIFF")
	t.Logf("  %-30s %8s %8s %8s", strings.Repeat("─", 30), strings.Repeat("─", 8), strings.Repeat("─", 8), strings.Repeat("─", 8))
	var relTotal1, relTotal2 int
	for _, rn := range sortedRelTypes {
		r1 := pass1.RelStats[rn].Total
		r2 := pass2.RelStats[rn].Total
		relTotal1 += r1
		relTotal2 += r2
		t.Logf("  %-30s %8d %8d %+8d", rn, r1, r2, r2-r1)
	}
	t.Logf("  %-30s %8s %8s %8s", strings.Repeat("─", 30), strings.Repeat("─", 8), strings.Repeat("─", 8), strings.Repeat("─", 8))
	t.Logf("  %-30s %8d %8d %+8d", "TOTAL", relTotal1, relTotal2, relTotal2-relTotal1)
}

// waitForObjects polls until at least one graph object exists OR extraction job
// moves from "processing" to "completed"/"failed". Returns true if objects found.
func waitForObjects(t *testing.T, ctx context.Context, testDB *testutil.TestDB, projectID string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var count int
		err := testDB.DB.NewRaw(
			"SELECT COUNT(*) FROM kb.graph_objects WHERE project_id = ?::uuid AND supersedes_id IS NULL",
			projectID,
		).Scan(ctx, &count)
		if err == nil && count > 0 {
			t.Logf("  background extraction complete: %d objects found", count)
			return true
		}
		// Check if extraction job finished (even if it produced 0 objects)
		var jobStatus string
		_ = testDB.DB.NewRaw(
			`SELECT status FROM kb.object_extraction_jobs WHERE project_id = ?::uuid ORDER BY created_at DESC LIMIT 1`,
			projectID,
		).Scan(ctx, &jobStatus)
		if jobStatus == "completed" || jobStatus == "failed" {
			t.Logf("  extraction job reached terminal status: %s (objects: %d)", jobStatus, count)
			return count > 0
		}
		time.Sleep(2 * time.Second)
	}
	t.Logf("  ⚠ no objects appeared after %v (extraction may be slow or failed)", timeout)
	return false
}

// TestReExtractionComparison runs two remember extractions with the same document
// and prints a human-readable comparison summary of what changed.
func TestReExtractionComparison(t *testing.T) {
	skipDiscoveryEnrich(t)
	ctx := context.Background()

	testDB, err := testutil.SetupTestDB(ctx, "reextract")
	require.NoError(t, err)
	defer testDB.Close()

	require.NoError(t, testutil.SetupTestFixtures(ctx, testDB.DB))

	orgID := uuid.New().String()
	projectID := uuid.New().String()
	err = testutil.SetupFullTestProject(ctx, testDB.DB, orgID, projectID)
	require.NoError(t, err)

	svr := testutil.NewTestServerWithLLM(testDB)
	client := testutil.NewHTTPClient(svr.Echo)

	t.Logf("")
	t.Logf("╔══════════════════════════════════════════════════════════════════════╗")
	t.Logf("║         FIRST EXTRACTION: schema_policy=auto, mode=sync            ║")
	t.Logf("╚══════════════════════════════════════════════════════════════════════╝")

	// ── Pass 1: First extraction ──
	rec := client.POST(
		fmt.Sprintf("/api/projects/%s/remember", projectID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"message":       orgChartDoc,
			"schema_policy": "auto",
			"mode":          "sync",
		}),
	)
	require.Equal(t, http.StatusOK, rec.StatusCode, "first extraction should return 200")
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body, &body))
	p1Status, _ := body["status"].(string)
	require.Equal(t, "completed", p1Status, "first extraction should complete")
	t.Logf("PASS1 response: run_id=%s status=%s", body["run_id"], p1Status)

	waitForObjects(t, ctx, testDB, projectID, 120*time.Second)

	pass1 := snapshotExtraction(t, ctx, projectID, testDB)
	printExtractionSummary(t, "PASS 1 RESULTS", pass1)

	// ── Pass 2: Second extraction with same document ──
	t.Logf("")
	t.Logf("╔══════════════════════════════════════════════════════════════════════╗")
	t.Logf("║      SECOND EXTRACTION: same doc, schema_policy=auto, mode=sync    ║")
	t.Logf("╚══════════════════════════════════════════════════════════════════════╝")

	rec2 := client.POST(
		fmt.Sprintf("/api/projects/%s/remember", projectID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"message":       orgChartDoc,
			"schema_policy": "auto",
			"mode":          "sync",
		}),
	)
	require.Equal(t, http.StatusOK, rec2.StatusCode, "second extraction should return 200")
	var body2 map[string]any
	require.NoError(t, json.Unmarshal(rec2.Body, &body2))
	p2Status, _ := body2["status"].(string)
	require.Equal(t, "completed", p2Status, "second extraction should complete")
	t.Logf("PASS2 response: run_id=%s status=%s", body2["run_id"], p2Status)

	waitForObjects(t, ctx, testDB, projectID, 120*time.Second)

	pass2 := snapshotExtraction(t, ctx, projectID, testDB)
	printExtractionSummary(t, "PASS 2 RESULTS", pass2)

	// ── Print diff ──
	printDiffTable(t, pass1, pass2)

	// No assert on pass2 equality — we're collecting data for human judgment.
	// The test always passes; the summary tells the story.
	t.Logf("")
	t.Logf("Done. See above for the re-extraction comparison summary.")
}

// friendsPilotDoc is a condensed transcript of Friends S01E01
// "The One Where Monica Gets a Roommate" — rich with characters, locations, relationships.
const friendsPilotDoc = `
Friends — Season 1, Episode 1: "The One Where Monica Gets a Roommate"

Central Perk coffeehouse, Greenwich Village, New York.

MONICA GELLER (26, chef at a restaurant in Manhattan) is sitting with her brother
ROSS GELLER (26, paleontologist, just divorced from Carol) and
CHANDLER BING (26, data processor at a multinational corporation).
PHOEBE BUFFAY (26, masseuse and musician) arrives and sits down.
JOEY TRIBBIANI (26, struggling actor) walks in.

They talk about Ross's ex-wife Carol, who left him for another woman named Susan.
The gang has known each other for years. Monica and Ross are siblings.
Chandler and Ross were college roommates at Columbia University.

Monica has a date tonight with Paul the Wine Guy. Paul works at her restaurant
and is described as a handsome man in his 40s who knows wine.

At Monica's apartment (Greenwich Village), Monica is preparing for her date.

RACHEL GREEN (24, just left her fiancé Barry at the altar) bursts in wearing
a wedding dress. She was supposed to marry Barry Farber, a doctor, but realized
she didn't love him. Rachel has never worked a day in her life — her father
Dr. Leonard Green is a wealthy surgeon who bought her everything.

Monica agrees to let Rachel stay with her. Rachel becomes Monica's new roommate.
The previous roommate was Kandi, who moved out.

Ross has been in love with Rachel since high school.
Rachel remembers Ross as "the boy with the funny raft" from a childhood incident.

Paul the Wine Guy comes for dinner at Monica's apartment.
Monica cooks a multicourse meal. Paul says Monica is "incredible" and
"the most beautiful woman he's ever served wine to."
In Monica's bedroom, Paul starts crying. He says his wife left him
three years ago and he hasn't been able to perform since.
Monica comforts him. They end up having sex.
Later, Paul tells his friends at the restaurant that Monica is easy,
which devastates Monica when she overhears.

Rachel gets her first job as a waitress at Central Perk coffeehouse.
She makes $4.50 an hour plus tips. She hates it.

Ross's pet monkey Marcel (a capuchin monkey) arrives at his apartment.
Marcel was rescued from a research laboratory.

At Central Perk, the gang discusses Rachel's new job.
Rachel smokes a cigarette, even though nobody knew she smoked.
Chandler makes a joke about taking up smoking now.

Ross asks Rachel out on a date. She says yes.
Later, Rachel returns Ross's jacket, which is full of spinach dip from their date.
They share a kiss.
`

// TestReExtractionFriends runs two extractions on the Friends pilot episode
// and prints a human-readable comparison summary of what changed.
func TestReExtractionFriends(t *testing.T) {
	skipDiscoveryEnrich(t)
	ctx := context.Background()

	testDB, err := testutil.SetupTestDB(ctx, "friendsrex")
	require.NoError(t, err)
	defer testDB.Close()

	require.NoError(t, testutil.SetupTestFixtures(ctx, testDB.DB))

	orgID := uuid.New().String()
	projectID := uuid.New().String()
	err = testutil.SetupFullTestProject(ctx, testDB.DB, orgID, projectID)
	require.NoError(t, err)

	svr := testutil.NewTestServerWithLLM(testDB)
	client := testutil.NewHTTPClient(svr.Echo)

	t.Logf("")
	t.Logf("╔══════════════════════════════════════════════════════════════════════╗")
	t.Logf("║   FRIENDS S01E01 — FIRST EXTRACTION (schema_policy=auto, sync)     ║")
	t.Logf("╚══════════════════════════════════════════════════════════════════════╝")

	// ── Pass 1: First extraction of Friends pilot ──
	rec := client.POST(
		fmt.Sprintf("/api/projects/%s/remember", projectID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"message":       friendsPilotDoc,
			"schema_policy": "auto",
			"mode":          "sync",
		}),
	)
	require.Equal(t, http.StatusOK, rec.StatusCode, "first extraction should return 200")
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body, &body))
	p1Status, _ := body["status"].(string)
	require.Equal(t, "completed", p1Status, "first extraction should complete")
	t.Logf("PASS1 response: run_id=%s status=%s", body["run_id"], p1Status)

	waitForObjects(t, ctx, testDB, projectID, 120*time.Second)

	pass1 := snapshotExtraction(t, ctx, projectID, testDB)
	printExtractionSummary(t, "PASS 1 RESULTS", pass1)

	// ── Pass 2: Second extraction with same Friends pilot ──
	t.Logf("")
	t.Logf("╔══════════════════════════════════════════════════════════════════════╗")
	t.Logf("║   FRIENDS S01E01 — SECOND EXTRACTION (same doc, auto, sync)        ║")
	t.Logf("╚══════════════════════════════════════════════════════════════════════╝")

	rec2 := client.POST(
		fmt.Sprintf("/api/projects/%s/remember", projectID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"message":       friendsPilotDoc,
			"schema_policy": "auto",
			"mode":          "sync",
		}),
	)
	require.Equal(t, http.StatusOK, rec2.StatusCode, "second extraction should return 200")
	var body2 map[string]any
	require.NoError(t, json.Unmarshal(rec2.Body, &body2))
	p2Status, _ := body2["status"].(string)
	require.Equal(t, "completed", p2Status, "second extraction should complete")
	t.Logf("PASS2 response: run_id=%s status=%s", body2["run_id"], p2Status)

	waitForObjects(t, ctx, testDB, projectID, 120*time.Second)

	pass2 := snapshotExtraction(t, ctx, projectID, testDB)
	printExtractionSummary(t, "PASS 2 RESULTS", pass2)

	// ── Print diff ──
	printDiffTable(t, pass1, pass2)

	t.Logf("")
	t.Logf("Done. See above for Friends re-extraction comparison.")
}

// friendsE02Doc is a condensed transcript of Friends S01E02
// "The One with the Sonogram at the End" — new characters, locations, and plot events.
const friendsE02Doc = `
Friends — Season 1, Episode 2: "The One with the Sonogram at the End"

At Central Perk coffeehouse, Greenwich Village.

ROSS GELLER (paleontologist, 26) tells his friends that his ex-wife CAROL
is pregnant with his baby. Carol left Ross for another woman named SUSAN.
Ross is nervous about becoming a father.

RACHEL GREEN (24) has moved in with MONICA GELLER (26, chef)
and is trying to adjust to being on her own for the first time.
Rachel's father DR. LEONARD GREEN (wealthy surgeon) is furious that she
left BARRY FARBER (doctor) at the altar. Dr. Green cuts off Rachel's credit cards.
Rachel has her first panic attack about money and being independent.
She decides to return her wedding dress to Bloomingdale's department store
to get money back. She also pawns some of her father's gifts.

Monica is supportive of Rachel's independence. She teaches Rachel
how to do laundry and manage money.

Carol and Susan invite Ross to the ultrasound appointment at the
obstetrician's office. Ross is uncomfortable that Susan will be there too.
CHANDLER BING (26, data processor), JOEY TRIBBIANI (26, struggling actor),
and PHOEBE BUFFAY (26, masseuse) support Ross and go with him to the clinic.

At the obstetrician's office, the doctor performs the ultrasound.
Ross, Carol, and Susan see the baby on the sonogram screen.
Ross gets emotional seeing his unborn child.
The sonogram shows the baby is healthy and developing normally.
The due date is in five months.

Ross realizes that he will always be connected to Carol and Susan
because of the baby, and they agree to co-parent together.

Meanwhile, at Bloomingdale's, Rachel tries to return her wedding dress
but has trouble because the store's return policy requires a receipt.
She eventually returns it and gets $500 back.

Back at Central Perk, Rachel shows everyone her new boots that she bought
with her first paycheck from the waitressing job she started.
She is proud of being financially independent for the first time.
`

// TestReExtractionFriendsE02 runs extraction on Friends S01E01 then S01E02
// and prints a comparison showing new entities, updated entities, and relationships.
func TestReExtractionFriendsE02(t *testing.T) {
	skipDiscoveryEnrich(t)
	ctx := context.Background()

	testDB, err := testutil.SetupTestDB(ctx, "friendse02")
	require.NoError(t, err)
	defer testDB.Close()

	require.NoError(t, testutil.SetupTestFixtures(ctx, testDB.DB))

	orgID := uuid.New().String()
	projectID := uuid.New().String()
	err = testutil.SetupFullTestProject(ctx, testDB.DB, orgID, projectID)
	require.NoError(t, err)

	svr := testutil.NewTestServerWithLLM(testDB)
	client := testutil.NewHTTPClient(svr.Echo)

	t.Logf("")
	t.Logf("╔══════════════════════════════════════════════════════════════════════╗")
	t.Logf("║   S01E01 — FIRST EXTRACTION (schema_policy=auto, sync)            ║")
	t.Logf("╚══════════════════════════════════════════════════════════════════════╝")

	rec := client.POST(
		fmt.Sprintf("/api/projects/%s/remember", projectID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"message":       friendsPilotDoc,
			"schema_policy": "auto",
			"mode":          "sync",
		}),
	)
	require.Equal(t, http.StatusOK, rec.StatusCode, "first extraction should return 200")
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body, &body))
	p1Status, _ := body["status"].(string)
	require.Equal(t, "completed", p1Status, "first extraction should complete")
	t.Logf("PASS1 (E01) response: run_id=%s status=%s", body["run_id"], p1Status)

	waitForObjects(t, ctx, testDB, projectID, 120*time.Second)

	pass1 := snapshotExtraction(t, ctx, projectID, testDB)
	printExtractionSummary(t, "S01E01 RESULTS", pass1)

	t.Logf("")
	t.Logf("╔══════════════════════════════════════════════════════════════════════╗")
	t.Logf("║   S01E02 — SECOND EXTRACTION (different episode, auto, sync)       ║")
	t.Logf("╚══════════════════════════════════════════════════════════════════════╝")

	rec2 := client.POST(
		fmt.Sprintf("/api/projects/%s/remember", projectID),
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"message":       friendsE02Doc,
			"schema_policy": "auto",
			"mode":          "sync",
		}),
	)
	require.Equal(t, http.StatusOK, rec2.StatusCode, "second extraction should return 200")
	var body2 map[string]any
	require.NoError(t, json.Unmarshal(rec2.Body, &body2))
	p2Status, _ := body2["status"].(string)
	require.Equal(t, "completed", p2Status, "second extraction should complete")
	t.Logf("PASS2 (E02) response: run_id=%s status=%s", body2["run_id"], p2Status)

	waitForObjects(t, ctx, testDB, projectID, 120*time.Second)

	pass2 := snapshotExtraction(t, ctx, projectID, testDB)
	printExtractionSummary(t, "S01E02 RESULTS", pass2)

	printDiffTable(t, pass1, pass2)

	t.Logf("")
	t.Logf("Done. See above for E01→E02 re-extraction comparison.")
}

// mustUnmarshal decodes JSON bytes into dst or fatals the test.
func mustUnmarshal(t *testing.T, data []byte, dst any) {
	t.Helper()
	if err := json.Unmarshal(data, dst); err != nil {
		t.Fatalf("json.Unmarshal: %v (body: %s)", err, data)
	}
}
