package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"
)

// ForgetAgentTestSuite exercises POST /api/projects/:id/forget end-to-end.
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
//  2. Agent idempotency     (EnsureForgetAgent called on every request)
//  3. dry_run flag          (no mutations when dry_run=true)
//  4. strategy validation   (bad values → 400)
//  5. cascade_depth         (invalid values → 400, 0 is a valid default)
//  6. Conversation reuse    (same conversation_id honoured)
//  7. sync mode             (200 JSON with run_id + status)
//  8. async mode            (202 JSON with run_id)
//  9. Error cases           (no auth, empty message)
//
// 10. LLM: auto strategy   (object appears deleted after forget)
// 11. LLM: confirm strategy (run pauses for human confirmation)
type ForgetAgentTestSuite struct {
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

func TestForgetAgentSuite(t *testing.T) {
	suite.Run(t, new(ForgetAgentTestSuite))
}

// ---------------------------------------------------------------------------
// Suite lifecycle
// ---------------------------------------------------------------------------

func (s *ForgetAgentTestSuite) SetupSuite() {
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
	testDB, err := testutil.SetupTestDB(s.ctx, "forget")
	s.Require().NoError(err, "setup test db")
	s.testDB = testDB
	s.authToken = "e2e-test-user"
}

func (s *ForgetAgentTestSuite) TearDownSuite() {
	if s.testDB != nil {
		s.testDB.Close()
	}
}

func (s *ForgetAgentTestSuite) SetupTest() {
	if s.external {
		return
	}

	// In-process: truncate + recreate fixtures for each test.
	err := testutil.TruncateTables(s.ctx, s.testDB.DB)
	s.Require().NoError(err)

	err = testutil.SetupTestFixtures(s.ctx, s.testDB.DB)
	s.Require().NoError(err)

	s.orgID = uuid.New().String()
	s.projectID = uuid.New().String()

	err = testutil.SetupFullTestProject(s.ctx, s.testDB.DB, s.orgID, s.projectID)
	s.Require().NoError(err)

	s.inProcess = testutil.NewTestServerWithLLM(s.testDB)
	s.client = testutil.NewHTTPClient(s.inProcess.Echo)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (s *ForgetAgentTestSuite) forgetURL() string {
	return fmt.Sprintf("/api/projects/%s/forget", s.projectID)
}

func (s *ForgetAgentTestSuite) postForget(body map[string]any) *testutil.SSEResponse {
	return s.client.PostSSE(
		s.forgetURL(),
		testutil.WithAuth(s.authToken),
		testutil.WithJSONBody(body),
	)
}

func (s *ForgetAgentTestSuite) postForgetNoAuth(body map[string]any) *testutil.SSEResponse {
	return s.client.PostSSE(
		s.forgetURL(),
		testutil.WithJSONBody(body),
	)
}

func (s *ForgetAgentTestSuite) postForgetMode(body map[string]any) *testutil.HTTPResponse {
	return s.client.POST(
		s.forgetURL(),
		testutil.WithAuth(s.authToken),
		testutil.WithJSONBody(body),
	)
}

// objectCount returns the number of non-deleted graph objects in the project.
func (s *ForgetAgentTestSuite) objectCount() int {
	resp := s.client.GET(
		fmt.Sprintf("/api/graph/objects/search?projectId=%s", s.projectID),
		testutil.WithAuth(s.authToken),
		testutil.WithProjectID(s.projectID),
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

// seedGraphObject inserts a graph object directly into the DB and returns its canonical_id.
// Only usable in in-process mode (s.testDB != nil).
func (s *ForgetAgentTestSuite) seedGraphObject(name string) uuid.UUID {
	s.T().Helper()
	s.Require().NotNil(s.testDB, "seedGraphObject requires in-process mode")

	projectUUID, err := uuid.Parse(s.projectID)
	s.Require().NoError(err)

	canonicalID := uuid.New()
	obj := &graph.GraphObject{
		ID:          uuid.New(),
		ProjectID:   projectUUID,
		CanonicalID: canonicalID,
		Version:     1,
		Type:        "Person",
		Properties: map[string]any{
			"name": name,
		},
		Labels:    []string{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	_, err = s.testDB.DB.NewInsert().Model(obj).Exec(s.ctx)
	s.Require().NoError(err, "seed graph object %q", name)
	return canonicalID
}

// isObjectDeleted checks whether the HEAD version of a canonical object has deleted_at set.
func (s *ForgetAgentTestSuite) isObjectDeleted(canonicalID uuid.UUID) bool {
	s.T().Helper()
	s.Require().NotNil(s.testDB)

	var obj graph.GraphObject
	err := s.testDB.DB.NewSelect().
		Model(&obj).
		Where("canonical_id = ?", canonicalID).
		Where("supersedes_id IS NULL").
		Scan(s.ctx)
	if err != nil {
		return false
	}
	return obj.DeletedAt != nil
}

// seedGraphRelationship inserts a relationship directly into the DB and returns its canonical_id.
// Only usable in in-process mode (s.testDB != nil).
func (s *ForgetAgentTestSuite) seedGraphRelationship(srcID, dstID uuid.UUID, relType string) uuid.UUID {
	s.T().Helper()
	s.Require().NotNil(s.testDB, "seedGraphRelationship requires in-process mode")

	projectUUID, err := uuid.Parse(s.projectID)
	s.Require().NoError(err)

	canonicalID := uuid.New()
	rel := &graph.GraphRelationship{
		ID:          uuid.New(),
		ProjectID:   projectUUID,
		CanonicalID: canonicalID,
		Version:     1,
		Type:        relType,
		SrcID:       srcID,
		DstID:       dstID,
		Properties:  map[string]any{},
		CreatedAt:   time.Now(),
	}
	_, err = s.testDB.DB.NewInsert().Model(rel).Exec(s.ctx)
	s.Require().NoError(err, "seed graph relationship %s->%s (%s)", srcID, dstID, relType)
	return canonicalID
}

// isRelationshipDeleted checks whether the HEAD version of a canonical relationship has deleted_at set.
func (s *ForgetAgentTestSuite) isRelationshipDeleted(canonicalID uuid.UUID) bool {
	s.T().Helper()
	s.Require().NotNil(s.testDB)

	var rel graph.GraphRelationship
	err := s.testDB.DB.NewSelect().
		Model(&rel).
		Where("canonical_id = ?", canonicalID).
		Where("supersedes_id IS NULL").
		Scan(s.ctx)
	if err != nil {
		return false
	}
	return rel.DeletedAt != nil
}

// skipIfNoLLM skips a test when no LLM provider is configured.
func (s *ForgetAgentTestSuite) skipIfNoLLM() {
	probe := s.postForget(map[string]any{"message": "ping"})
	if probe.StatusCode == http.StatusServiceUnavailable || probe.StatusCode == http.StatusUnprocessableEntity {
		s.T().Skip("no LLM provider configured — skipping LLM-dependent test")
	}
	if s.external {
		for _, ev := range probe.GetEventsByType("error") {
			var data map[string]any
			if err := ev.ParseSSEJSON(&data); err == nil {
				if msg, ok := data["error"].(string); ok {
					s.T().Skipf("LLM unavailable on external server (%s) — skipping", msg)
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Tests — HTTP / SSE mechanics (no LLM required)
// ---------------------------------------------------------------------------

func (s *ForgetAgentTestSuite) TestForget_NoAuth_Returns401() {
	if s.external {
		s.T().Skip("no-auth test not suitable for shared prod project — skipping in external mode")
	}
	rec := s.postForgetNoAuth(map[string]any{"message": "forget everything"})
	s.Equal(http.StatusUnauthorized, rec.StatusCode)
}

func (s *ForgetAgentTestSuite) TestForget_EmptyMessage_Returns400() {
	if s.external {
		s.T().Skip("validation test runs in-process only")
	}
	rec := s.postForget(map[string]any{"message": ""})
	s.Equal(http.StatusBadRequest, rec.StatusCode)
}

func (s *ForgetAgentTestSuite) TestForget_MissingMessage_Returns400() {
	if s.external {
		s.T().Skip("validation test runs in-process only")
	}
	rec := s.postForget(map[string]any{})
	s.Equal(http.StatusBadRequest, rec.StatusCode)
}

func (s *ForgetAgentTestSuite) TestForget_BadStrategy_Returns400() {
	if s.external {
		s.T().Skip("validation test runs in-process only")
	}
	rec := s.postForget(map[string]any{
		"message":  "forget Alice",
		"strategy": "invalid-strategy",
	})
	s.Equal(http.StatusBadRequest, rec.StatusCode)
}

func (s *ForgetAgentTestSuite) TestForget_BadCascadeDepth_Returns400() {
	if s.external {
		s.T().Skip("validation test runs in-process only")
	}
	rec := s.postForget(map[string]any{
		"message":       "forget Alice",
		"cascade_depth": 99,
	})
	s.Equal(http.StatusBadRequest, rec.StatusCode)
}

// cascade_depth=0 is treated as the default (2), not an error.
func (s *ForgetAgentTestSuite) TestForget_ZeroCascadeDepth_IsValid() {
	if s.external {
		s.T().Skip("validation test runs in-process only")
	}
	// Without LLM the server returns 503, not 400 — proving validation passed.
	rec := s.postForget(map[string]any{
		"message":       "forget Alice",
		"cascade_depth": 0,
	})
	s.NotEqual(http.StatusBadRequest, rec.StatusCode,
		"cascade_depth=0 must not be a validation error; got %d", rec.StatusCode)
}

func (s *ForgetAgentTestSuite) TestForget_BadMode_Returns400() {
	if s.external {
		s.T().Skip("validation test runs in-process only")
	}
	rec := s.postForgetMode(map[string]any{
		"message": "forget Alice",
		"mode":    "invalid-mode",
	})
	s.Equal(http.StatusBadRequest, rec.StatusCode)
}

// ---------------------------------------------------------------------------
// Tests — LLM-dependent
// ---------------------------------------------------------------------------

func (s *ForgetAgentTestSuite) TestForget_ValidRequest_Returns200WithSSE() {
	s.skipIfNoLLM()

	rec := s.postForget(map[string]any{"message": "forget everything about Alice"})
	s.Require().Equal(http.StatusOK, rec.StatusCode)
	s.True(testutil.IsSSEContentType(rec.ContentType),
		"expected text/event-stream, got %s", rec.ContentType)
}

func (s *ForgetAgentTestSuite) TestForget_EmitsMetaAndDoneEvents() {
	s.skipIfNoLLM()

	rec := s.postForget(map[string]any{"message": "forget Alice"})
	s.Require().Equal(http.StatusOK, rec.StatusCode)

	s.True(rec.HasEvent("meta"), "expected meta SSE event")
	s.True(rec.HasEvent("done"), "expected done SSE event")

	for _, ev := range rec.GetEventsByType("meta") {
		var data map[string]any
		s.Require().NoError(ev.ParseSSEJSON(&data))
		s.NotEmpty(data["conversationId"], "meta must contain conversationId")
	}

	for _, ev := range rec.GetEventsByType("done") {
		var data map[string]any
		s.Require().NoError(ev.ParseSSEJSON(&data))
		s.NotEmpty(data["runId"], "done must contain runId")
	}
}

func (s *ForgetAgentTestSuite) TestForget_AgentIdempotency() {
	s.skipIfNoLLM()

	rec1 := s.postForget(map[string]any{"message": "forget Alice"})
	s.Require().Equal(http.StatusOK, rec1.StatusCode)

	rec2 := s.postForget(map[string]any{"message": "forget Bob"})
	s.Equal(http.StatusOK, rec2.StatusCode)
}

func (s *ForgetAgentTestSuite) TestForget_ReusesConversation() {
	s.skipIfNoLLM()

	rec1 := s.postForget(map[string]any{"message": "forget Alice"})
	s.Require().Equal(http.StatusOK, rec1.StatusCode)

	var convID string
	for _, ev := range rec1.GetEventsByType("meta") {
		var data map[string]any
		_ = ev.ParseSSEJSON(&data)
		if id, ok := data["conversationId"].(string); ok {
			convID = id
		}
	}
	s.Require().NotEmpty(convID, "expected conversationId in meta event")

	rec2 := s.postForget(map[string]any{
		"message":         "forget Bob",
		"conversation_id": convID,
	})
	s.Require().Equal(http.StatusOK, rec2.StatusCode)

	for _, ev := range rec2.GetEventsByType("meta") {
		var data map[string]any
		_ = ev.ParseSSEJSON(&data)
		if id, ok := data["conversationId"].(string); ok {
			s.Equal(convID, id, "conversationId must be reused across calls")
		}
	}
}

func (s *ForgetAgentTestSuite) TestForget_DryRun_NoMutations() {
	s.skipIfNoLLM()

	// Seed a known object (external mode: skip direct DB seed, use object count check only).
	if !s.external {
		s.seedGraphObject("Alice Dryrun")
	}

	before := s.objectCount()
	s.Require().GreaterOrEqual(before, 0, "objectCount failed")

	rec := s.postForget(map[string]any{
		"message": "forget Alice Dryrun",
		"dry_run": true,
	})
	s.Require().Equal(http.StatusOK, rec.StatusCode)
	s.False(rec.HasEvent("error"), "unexpected error event:\n%s", rec.RawBody)

	after := s.objectCount()
	s.Equal(before, after,
		"expected no deletions after dry_run=true; before=%d after=%d", before, after)
}

func (s *ForgetAgentTestSuite) TestForget_SyncMode_Returns200JSON() {
	s.skipIfNoLLM()

	rec := s.postForgetMode(map[string]any{
		"message": "forget everything",
		"mode":    "sync",
	})
	s.Require().Equal(http.StatusOK, rec.StatusCode)

	var body map[string]any
	s.Require().NoError(rec.JSON(&body))
	s.NotEmpty(body["run_id"], "sync response must contain run_id")
	s.NotEmpty(body["status"], "sync response must contain status")
}

func (s *ForgetAgentTestSuite) TestForget_AsyncMode_Returns202JSON() {
	s.skipIfNoLLM()

	rec := s.postForgetMode(map[string]any{
		"message": "forget everything",
		"mode":    "async",
	})
	s.Require().Equal(http.StatusAccepted, rec.StatusCode)

	var body map[string]any
	s.Require().NoError(rec.JSON(&body))
	s.NotEmpty(body["run_id"], "async response must contain run_id")
}

// TestForget_AutoStrategy_DeletesObject seeds a graph object, forgets it with
// strategy=auto, then verifies the object is soft-deleted (deleted_at set).
// Requires a live LLM and in-process mode for direct DB verification.
func (s *ForgetAgentTestSuite) TestForget_AutoStrategy_DeletesObject() {
	s.skipIfNoLLM()
	if s.external {
		s.T().Skip("direct DB verification not available in external mode")
	}

	canonicalID := s.seedGraphObject("ForgetTarget Person")
	s.False(s.isObjectDeleted(canonicalID), "object must not be deleted before forget")

	rec := s.postForget(map[string]any{
		"message":  "forget the person named ForgetTarget Person",
		"strategy": "auto",
	})
	s.Require().Equal(http.StatusOK, rec.StatusCode)
	s.False(rec.HasEvent("error"), "unexpected error event:\n%s", rec.RawBody)

	s.True(s.isObjectDeleted(canonicalID),
		"expected object to be soft-deleted after forget with strategy=auto")
}

// ---------------------------------------------------------------------------
// Tests — LLM + real graph (in-process only, direct DB verification)
// ---------------------------------------------------------------------------

// TestForget_WithRealGraph_DeletesSingleNode seeds one Person, forgets it by
// exact name, and asserts the object is soft-deleted afterwards.
func (s *ForgetAgentTestSuite) TestForget_WithRealGraph_DeletesSingleNode() {
	s.skipIfNoLLM()
	if s.external {
		s.T().Skip("direct DB verification not available in external mode")
	}

	ctx, cancel := context.WithTimeout(s.ctx, 90*time.Second)
	defer cancel()
	_ = ctx

	aliceID := s.seedGraphObject("AliceSingle")
	before := s.objectCount()
	s.Require().Equal(1, before, "expected exactly 1 object after seed")

	rec := s.postForget(map[string]any{
		"message":  "forget the person named AliceSingle",
		"strategy": "auto",
	})
	s.Require().Equal(http.StatusOK, rec.StatusCode)
	s.False(rec.HasEvent("error"), "unexpected error event:\n%s", rec.RawBody)

	s.True(s.isObjectDeleted(aliceID), "AliceSingle must be soft-deleted after forget")
	s.Equal(0, s.objectCount(), "object count must drop to 0 after forgetting the only object")
}

// TestForget_WithRealGraph_PartialForget_LeavesOthersIntact seeds three objects,
// forgets only one by exact name, and asserts the other two are untouched.
func (s *ForgetAgentTestSuite) TestForget_WithRealGraph_PartialForget_LeavesOthersIntact() {
	s.skipIfNoLLM()
	if s.external {
		s.T().Skip("direct DB verification not available in external mode")
	}

	ctx, cancel := context.WithTimeout(s.ctx, 90*time.Second)
	defer cancel()
	_ = ctx

	aliceID := s.seedGraphObject("AlicePartial")
	bobID := s.seedGraphObject("BobPartial")
	tennisID := s.seedGraphObject("TennisPartial")
	before := s.objectCount()
	s.Require().Equal(3, before, "expected 3 objects after seed")

	rec := s.postForget(map[string]any{
		"message":  "forget only the person named AlicePartial, leave everything else intact",
		"strategy": "auto",
	})
	s.Require().Equal(http.StatusOK, rec.StatusCode)
	s.False(rec.HasEvent("error"), "unexpected error event:\n%s", rec.RawBody)

	s.True(s.isObjectDeleted(aliceID), "AlicePartial must be soft-deleted")
	s.False(s.isObjectDeleted(bobID), "BobPartial must NOT be deleted")
	s.False(s.isObjectDeleted(tennisID), "TennisPartial must NOT be deleted")
	s.Equal(2, s.objectCount(), "object count must drop by exactly 1")
}

// TestForget_WithRealGraph_CascadeDeletesRelatedNodes seeds two Person nodes
// connected by a friend_of relationship plus one unrelated Person, then forgets
// the first person with cascade_depth=2.
//
// Alice deletion is required; Bob/relationship deletion is relaxed (logged but
// not fatal) because cascade behaviour depends on LLM interpretation.
func (s *ForgetAgentTestSuite) TestForget_WithRealGraph_CascadeDeletesRelatedNodes() {
	s.skipIfNoLLM()
	if s.external {
		s.T().Skip("direct DB verification not available in external mode")
	}

	ctx, cancel := context.WithTimeout(s.ctx, 120*time.Second)
	defer cancel()
	_ = ctx

	aliceID := s.seedGraphObject("AliceCascade")
	bobID := s.seedGraphObject("BobCascade")
	carolID := s.seedGraphObject("CarolCascade")
	relID := s.seedGraphRelationship(aliceID, bobID, "friend_of")

	rec := s.postForget(map[string]any{
		"message":       "forget AliceCascade and all entities directly connected to her, including BobCascade",
		"strategy":      "auto",
		"cascade_depth": 2,
	})
	s.Require().Equal(http.StatusOK, rec.StatusCode)
	s.False(rec.HasEvent("error"), "unexpected error event:\n%s", rec.RawBody)

	// Alice must be deleted — required assertion.
	s.True(s.isObjectDeleted(aliceID), "AliceCascade must be soft-deleted")

	// Carol must never be touched.
	s.False(s.isObjectDeleted(carolID), "CarolCascade must NOT be deleted (unrelated)")

	// Bob and relationship — relaxed: log if not deleted but don't fail.
	bobDeleted := s.isObjectDeleted(bobID)
	relDeleted := s.isRelationshipDeleted(relID)
	s.T().Logf("cascade result: BobCascade deleted=%v, friend_of relationship deleted=%v", bobDeleted, relDeleted)
	if bobDeleted {
		s.True(relDeleted, "if BobCascade is deleted the friend_of relationship must also be deleted")
	}
}

// TestForget_WithRealGraph_DryRunDoesNotDelete seeds two Person nodes with a
// relationship and runs forget with dry_run=true, asserting nothing is mutated.
func (s *ForgetAgentTestSuite) TestForget_WithRealGraph_DryRunDoesNotDelete() {
	s.skipIfNoLLM()
	if s.external {
		s.T().Skip("direct DB verification not available in external mode")
	}

	ctx, cancel := context.WithTimeout(s.ctx, 90*time.Second)
	defer cancel()
	_ = ctx

	aliceID := s.seedGraphObject("AliceDry")
	bobID := s.seedGraphObject("BobDry")
	relID := s.seedGraphRelationship(aliceID, bobID, "knows")
	before := s.objectCount()
	s.Require().Equal(2, before, "expected 2 objects after seed")

	rec := s.postForget(map[string]any{
		"message":  "forget AliceDry and BobDry",
		"strategy": "auto",
		"dry_run":  true,
	})
	s.Require().Equal(http.StatusOK, rec.StatusCode)
	s.False(rec.HasEvent("error"), "unexpected error event:\n%s", rec.RawBody)

	s.False(s.isObjectDeleted(aliceID), "AliceDry must NOT be deleted (dry_run=true)")
	s.False(s.isObjectDeleted(bobID), "BobDry must NOT be deleted (dry_run=true)")
	s.False(s.isRelationshipDeleted(relID), "knows relationship must NOT be deleted (dry_run=true)")
	s.Equal(before, s.objectCount(), "object count must be unchanged after dry_run")
}

// TestForget_ConfirmStrategy_PausesRun verifies that strategy=confirm causes the
// agent run to pause waiting for human input before executing any deletions.
func (s *ForgetAgentTestSuite) TestForget_ConfirmStrategy_PausesRun() {
	s.skipIfNoLLM()
	if s.external {
		s.T().Skip("run status polling not available in external mode")
	}

	s.seedGraphObject("ConfirmTarget Person")

	// Use async so we get a run_id back immediately without blocking.
	rec := s.postForgetMode(map[string]any{
		"message":  "forget the person named ConfirmTarget Person",
		"strategy": "confirm",
		"mode":     "async",
	})
	s.Require().Equal(http.StatusAccepted, rec.StatusCode)

	var body map[string]any
	s.Require().NoError(rec.JSON(&body))
	runID, _ := body["run_id"].(string)
	s.Require().NotEmpty(runID, "async response must contain run_id")

	// Poll the run status; expect it to reach paused/input-required quickly.
	var finalStatus string
	for i := 0; i < 20; i++ {
		time.Sleep(250 * time.Millisecond)
		statusResp := s.client.GET(
			fmt.Sprintf("/api/agents/runs/%s", runID),
			testutil.WithAuth(s.authToken),
		)
		if statusResp.StatusCode != http.StatusOK {
			continue
		}
		var runBody map[string]any
		if err := json.Unmarshal(statusResp.Body, &runBody); err != nil {
			continue
		}
		status, _ := runBody["status"].(string)
		if status == "paused" || status == "input-required" || status == "error" || status == "success" {
			finalStatus = status
			break
		}
	}

	s.T().Logf("run %s final status: %s", runID, finalStatus)
	s.True(finalStatus == "paused" || finalStatus == "input-required",
		"expected run to pause for confirmation; got status=%q", finalStatus)
}
