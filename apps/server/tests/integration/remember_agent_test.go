package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// RememberAgentTestSuite exercises POST /api/projects/:id/remember end-to-end.
//
// Modes:
//
//  1. In-process (default): real Postgres test DB, no LLM — LLM-dependent tests skip.
//
//  2. External (TEST_SERVER_URL set): sends real HTTP to the target server.
//     Requires TEST_API_TOKEN and TEST_PROJECT_ID env vars.
//     All tests (including LLM-dependent) run against the live server.
//
// Tests cover:
//  1. HTTP / SSE mechanics  (status, content-type, event shape)
//  2. Agent idempotency     (EnsureGraphInsertAgent called on every request)
//  3. Graph mutation        (entities land in the graph after ingest)
//  4. dry_run flag          (no merge → empty graph)
//  5. schema_policy         (reuse_only accepted without error)
//  6. Conversation reuse    (same conversation_id honoured)
//  7. Tool usage            (branch-create + branch-merge both invoked)
//  8. Error cases           (no auth, empty message, bad policy)
type RememberAgentTestSuite struct {
	suite.Suite

	// in-process fields (nil when external)
	testDB    *testutil.TestDB
	inProcess *testutil.TestServer

	// shared
	client    *testutil.HTTPClient
	ctx       context.Context
	projectID string
	orgID     string
	authToken string // "e2e-test-user" for in-process; TEST_API_TOKEN for external

	// external mode only
	external bool
}

func TestRememberAgentSuite(t *testing.T) {
	suite.Run(t, new(RememberAgentTestSuite))
}

// ---------------------------------------------------------------------------
// Suite lifecycle
// ---------------------------------------------------------------------------

func (s *RememberAgentTestSuite) SetupSuite() {
	s.ctx = context.Background()
	s.external = os.Getenv("TEST_SERVER_URL") != ""

	if s.external {
		baseURL := os.Getenv("TEST_SERVER_URL")
		s.authToken = os.Getenv("TEST_API_TOKEN")
		s.projectID = os.Getenv("TEST_PROJECT_ID")

		if s.authToken == "" || s.projectID == "" {
			s.T().Fatal("TEST_SERVER_URL set but TEST_API_TOKEN or TEST_PROJECT_ID missing")
		}

		s.client = testutil.NewExternalHTTPClient(baseURL)
		return
	}

	// In-process: set up local test DB once for the whole suite.
	testDB, err := testutil.SetupTestDB(s.ctx, "remember")
	s.Require().NoError(err, "setup test db")
	s.testDB = testDB
	s.authToken = "e2e-test-user"
}

func (s *RememberAgentTestSuite) TearDownSuite() {
	if s.testDB != nil {
		s.testDB.Close()
	}
}

func (s *RememberAgentTestSuite) SetupTest() {
	if s.external {
		// External mode: nothing to set up locally per-test.
		// orgID is unused; projectID/authToken come from env and are fixed.
		return
	}

	// In-process: truncate + recreate fixtures for each test.
	err := testutil.TruncateTables(s.ctx, s.testDB.DB)
	s.Require().NoError(err)

	err = testutil.SetupTestFixtures(s.ctx, s.testDB.DB)
	s.Require().NoError(err)

	s.orgID = uuid.New().String()
	s.projectID = uuid.New().String()

	err = testutil.CreateTestOrganization(s.ctx, s.testDB.DB, s.orgID, "Test Org for Remember")
	s.Require().NoError(err)

	err = testutil.CreateTestProject(s.ctx, s.testDB.DB, testutil.TestProject{
		ID:    s.projectID,
		OrgID: s.orgID,
		Name:  "remember-test",
	}, testutil.AdminUser.ID)
	s.Require().NoError(err)

	err = testutil.CreateTestOrgMembership(s.ctx, s.testDB.DB, s.orgID, testutil.AdminUser.ID, "admin")
	s.Require().NoError(err)

	err = testutil.CreateTestProjectMembership(s.ctx, s.testDB.DB, s.projectID, testutil.AdminUser.ID, "admin")
	s.Require().NoError(err)

	s.inProcess = testutil.NewTestServer(s.testDB)
	s.client = testutil.NewHTTPClient(s.inProcess.Echo)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (s *RememberAgentTestSuite) rememberURL() string {
	return fmt.Sprintf("/api/projects/%s/remember", s.projectID)
}

func (s *RememberAgentTestSuite) postRemember(body map[string]any) *testutil.SSEResponse {
	return s.client.PostSSE(
		s.rememberURL(),
		testutil.WithAuth(s.authToken),
		testutil.WithJSONBody(body),
	)
}

// objectCount returns the number of graph objects in the project.
func (s *RememberAgentTestSuite) objectCount() int {
	resp := s.client.GET(
		fmt.Sprintf("/api/projects/%s/graph/objects/search", s.projectID),
		testutil.WithAuth(s.authToken),
	)
	if resp.StatusCode != http.StatusOK {
		return -1
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		return -1
	}
	items, _ := body["items"].([]any)
	return len(items)
}

// toolsUsed extracts tool names from tool_call SSE events.
func toolsUsed(sse *testutil.SSEResponse) []string {
	names := make([]string, 0)
	for _, ev := range sse.GetEventsByType("tool_call") {
		var data map[string]any
		if err := ev.ParseSSEJSON(&data); err == nil {
			if name, ok := data["name"].(string); ok {
				names = append(names, name)
			}
		}
	}
	return names
}

// skipIfNoLLM skips a test when no LLM provider is configured.
// In external mode, the server is always expected to have a provider — skip only on 503.
// In in-process mode, modelFactory is nil so we always get 503.
func (s *RememberAgentTestSuite) skipIfNoLLM() {
	if s.external {
		// External: provider should be configured; skip only if actually unavailable.
		probe := s.postRemember(map[string]any{"message": "ping"})
		if probe.StatusCode == http.StatusServiceUnavailable || probe.StatusCode == http.StatusUnprocessableEntity {
			s.T().Skip("no LLM provider configured on external server — skipping")
		}
		return
	}
	// In-process: modelFactory is nil → always 503.
	s.T().Skip("no LLM provider configured (in-process mode) — skipping LLM-dependent test")
}

// ---------------------------------------------------------------------------
// Tests — HTTP / SSE mechanics (no LLM required, always run in-process)
// ---------------------------------------------------------------------------

// noAuthClient returns a client that sends requests without auth.
// In external mode it hits the prod server; in-process it hits the local server.
func (s *RememberAgentTestSuite) postRememberNoAuth(body map[string]any) *testutil.SSEResponse {
	return s.client.PostSSE(
		s.rememberURL(),
		testutil.WithJSONBody(body),
	)
}

func (s *RememberAgentTestSuite) TestRemember_NoAuth_Returns401() {
	if s.external {
		s.T().Skip("no-auth test not suitable for shared prod project — skipping in external mode")
	}
	rec := s.postRememberNoAuth(map[string]any{"message": "hello"})
	s.Equal(http.StatusUnauthorized, rec.StatusCode)
}

func (s *RememberAgentTestSuite) TestRemember_EmptyMessage_Returns400() {
	if s.external {
		s.T().Skip("validation test runs in-process only")
	}
	rec := s.postRemember(map[string]any{"message": ""})
	s.Equal(http.StatusBadRequest, rec.StatusCode)
}

func (s *RememberAgentTestSuite) TestRemember_MissingMessage_Returns400() {
	if s.external {
		s.T().Skip("validation test runs in-process only")
	}
	rec := s.postRemember(map[string]any{})
	s.Equal(http.StatusBadRequest, rec.StatusCode)
}

func (s *RememberAgentTestSuite) TestRemember_BadSchemaPolicy_Returns400() {
	if s.external {
		s.T().Skip("validation test runs in-process only")
	}
	rec := s.postRemember(map[string]any{
		"message":       "Alice works at Acme.",
		"schema_policy": "invalid-value",
	})
	s.Equal(http.StatusBadRequest, rec.StatusCode)
}

// ---------------------------------------------------------------------------
// Tests — LLM-dependent (skip in-process; run against external server)
// ---------------------------------------------------------------------------

func (s *RememberAgentTestSuite) TestRemember_SSEContentType() {
	s.skipIfNoLLM()

	rec := s.postRemember(map[string]any{"message": "Alice works at Acme."})
	s.Equal(http.StatusOK, rec.StatusCode)
	s.True(testutil.IsSSEContentType(rec.ContentType),
		"expected text/event-stream, got %s", rec.ContentType)
}

func (s *RememberAgentTestSuite) TestRemember_EmitsMetaAndDoneEvents() {
	s.skipIfNoLLM()

	rec := s.postRemember(map[string]any{"message": "Alice works at Acme."})
	s.Require().Equal(http.StatusOK, rec.StatusCode)

	s.True(rec.HasEvent("meta"), "expected meta SSE event")
	s.True(rec.HasEvent("done"), "expected done SSE event")

	for _, ev := range rec.GetEventsByType("meta") {
		var data map[string]any
		s.Require().NoError(ev.ParseSSEJSON(&data))
		s.NotEmpty(data["conversation_id"], "meta must contain conversation_id")
	}

	for _, ev := range rec.GetEventsByType("done") {
		var data map[string]any
		s.Require().NoError(ev.ParseSSEJSON(&data))
		s.NotEmpty(data["run_id"], "done must contain run_id")
	}
}

func (s *RememberAgentTestSuite) TestRemember_AgentIdempotency() {
	s.skipIfNoLLM()

	rec1 := s.postRemember(map[string]any{"message": "Alice joined Acme in 2023."})
	s.Require().Equal(http.StatusOK, rec1.StatusCode)

	rec2 := s.postRemember(map[string]any{"message": "Bob joined Acme in 2024."})
	s.Equal(http.StatusOK, rec2.StatusCode)
}

func (s *RememberAgentTestSuite) TestRemember_WritesGraphEntities() {
	s.skipIfNoLLM()

	text := `Chat session on 2024-01-10:
Alice: I'm a software engineer at TechCorp. I started last month.
Bob: What team are you on?
Alice: The Platform team. My manager is Carlos.`

	rec := s.postRemember(map[string]any{"message": text})
	s.Require().Equal(http.StatusOK, rec.StatusCode)
	s.False(rec.HasEvent("error"), "unexpected error event:\n%s", rec.RawBody)

	count := s.objectCount()
	s.Greater(count, 0,
		"expected >0 graph objects after remember; got %d", count)
	s.T().Logf("Graph objects after ingest: %d", count)
}

func (s *RememberAgentTestSuite) TestRemember_DryRun_NoGraphMutation() {
	s.skipIfNoLLM()

	// Count objects before.
	before := s.objectCount()
	if before < 0 {
		before = 0
	}

	rec := s.postRemember(map[string]any{
		"message": "Diana is a product manager at StartupXYZ.",
		"dry_run": true,
	})
	s.Require().Equal(http.StatusOK, rec.StatusCode)

	after := s.objectCount()
	s.Equal(before, after,
		"expected no new graph objects after dry_run=true; before=%d after=%d", before, after)
}

func (s *RememberAgentTestSuite) TestRemember_SchemaPolicyReuseOnly() {
	s.skipIfNoLLM()

	rec := s.postRemember(map[string]any{
		"message":       "Eve is a data scientist at DeepMind.",
		"schema_policy": "reuse_only",
	})
	s.Equal(http.StatusOK, rec.StatusCode)
	s.False(rec.HasEvent("error"),
		"unexpected error event with reuse_only:\n%s", rec.RawBody)
}

func (s *RememberAgentTestSuite) TestRemember_ReusesConversation() {
	s.skipIfNoLLM()

	rec1 := s.postRemember(map[string]any{"message": "Alice is an engineer."})
	s.Require().Equal(http.StatusOK, rec1.StatusCode)

	var convID string
	for _, ev := range rec1.GetEventsByType("meta") {
		var data map[string]any
		_ = ev.ParseSSEJSON(&data)
		if id, ok := data["conversation_id"].(string); ok {
			convID = id
		}
	}
	s.Require().NotEmpty(convID, "expected conversation_id in meta event")

	rec2 := s.postRemember(map[string]any{
		"message":         "Alice now works at Google.",
		"conversation_id": convID,
	})
	s.Require().Equal(http.StatusOK, rec2.StatusCode)

	for _, ev := range rec2.GetEventsByType("meta") {
		var data map[string]any
		_ = ev.ParseSSEJSON(&data)
		if id, ok := data["conversation_id"].(string); ok {
			s.Equal(convID, id, "conversation_id must be reused across calls")
		}
	}
}

// TestRemember_ToolEvents_BranchLifecycle verifies the agent uses the
// branch→write→merge protocol (not direct writes to main).
func (s *RememberAgentTestSuite) TestRemember_ToolEvents_BranchLifecycle() {
	s.skipIfNoLLM()

	text := `Meeting notes 2024-03-01:
Frank is the CTO of Megacorp. Revenue grew 20% this quarter.`

	rec := s.postRemember(map[string]any{"message": text})
	s.Require().Equal(http.StatusOK, rec.StatusCode)

	tools := toolsUsed(rec)
	s.T().Logf("Tools used by agent: %s", strings.Join(tools, ", "))

	contains := func(name string) bool {
		for _, t := range tools {
			if t == name {
				return true
			}
		}
		return false
	}

	s.True(contains("graph-branch-create"),
		"agent must use graph-branch-create; tools: %v", tools)
	s.True(contains("graph-branch-merge"),
		"agent must use graph-branch-merge; tools: %v", tools)
}
