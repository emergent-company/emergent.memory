package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"
)

// personalKBSchema is installed into the test project so the agent has typed
// entity definitions to choose from when saving information.
var personalKBSchema = map[string]any{
	"name":    "personal-kb",
	"version": "1.0.0",
	"objectTypeSchemas": []map[string]any{
		{
			"name":        "Person",
			"description": "A person the user knows — colleague, friend, or contact",
			"properties": map[string]any{
				"name":       map[string]any{"type": "string", "description": "Full name"},
				"occupation": map[string]any{"type": "string", "description": "Job title or role"},
				"employer":   map[string]any{"type": "string", "description": "Company or organisation"},
				"phone":      map[string]any{"type": "string", "description": "Phone number"},
				"notes":      map[string]any{"type": "string", "description": "Free-form notes"},
			},
		},
		{
			"name":        "Note",
			"description": "An idea, thought, reference, or general piece of information",
			"properties": map[string]any{
				"name":    map[string]any{"type": "string", "description": "Short title"},
				"content": map[string]any{"type": "string", "description": "Full note text"},
				"tags":    map[string]any{"type": "string", "description": "Comma-separated tags"},
			},
		},
		{
			"name":        "Event",
			"description": "Something that happened or is planned",
			"properties": map[string]any{
				"name":     map[string]any{"type": "string", "description": "Event title"},
				"date":     map[string]any{"type": "string", "description": "Date or time"},
				"location": map[string]any{"type": "string", "description": "Where it takes place"},
				"notes":    map[string]any{"type": "string", "description": "Additional context"},
			},
		},
		{
			"name":        "Place",
			"description": "A location, city, venue, or address",
			"properties": map[string]any{
				"name":    map[string]any{"type": "string", "description": "Place name"},
				"country": map[string]any{"type": "string", "description": "Country"},
				"notes":   map[string]any{"type": "string", "description": "Notes about this place"},
			},
		},
		{
			"name":        "Fact",
			"description": "A standalone fact that does not fit other categories",
			"properties": map[string]any{
				"name":      map[string]any{"type": "string", "description": "Short label"},
				"statement": map[string]any{"type": "string", "description": "The fact itself"},
			},
		},
	},
}

// PersonalKBAgentTestSuite tests the personal-kb-agent end-to-end via the
// OpenAI-compatible /v1/chat/completions endpoint.
//
// Design:
//   - Test project is configured as a "Personal AI Knowledge Base" via
//     PATCH /api/projects/{id} (project_info field).
//   - A typed schema (Person, Note, Event, Place, Fact) is installed via the
//     schemas HTTP API so the agent has rich type definitions.
//   - The agent definition is created via EnsurePersonalKBAgent.
//   - All graph seeding / assertion uses the REST API — no direct DB inserts.
//   - Real embeddings are active (via NewTestEmbeddingsService) so search-hybrid
//     can use semantic similarity.
//
// Modes:
//  1. In-process (default): requires DB on port 5436 + LLM provider key.
//  2. External (TEST_SERVER_URL set): sends real HTTP to a running server.
type PersonalKBAgentTestSuite struct {
	suite.Suite

	testDB    *testutil.TestDB
	inProcess *testutil.TestServer

	client    *testutil.HTTPClient
	ctx       context.Context
	projectID string
	orgID     string
	authToken string
	external  bool

	agentName string
	schemaID  string // schema installed in SetupTest
}

func TestPersonalKBAgentSuite(t *testing.T) {
	suite.Run(t, new(PersonalKBAgentTestSuite))
}

// ---------------------------------------------------------------------------
// Suite lifecycle
// ---------------------------------------------------------------------------

func (s *PersonalKBAgentTestSuite) SetupSuite() {
	s.ctx = context.Background()
	s.external = os.Getenv("TEST_SERVER_URL") != ""

	if s.external {
		s.authToken = os.Getenv("TEST_API_TOKEN")
		s.projectID = os.Getenv("TEST_PROJECT_ID")
		if s.authToken == "" || s.projectID == "" {
			s.T().Fatal("TEST_SERVER_URL set but TEST_API_TOKEN or TEST_PROJECT_ID missing")
		}
		s.client = testutil.NewExternalHTTPClient(os.Getenv("TEST_SERVER_URL"))
		s.agentName = "personal-kb-agent"
		return
	}

	testDB, err := testutil.SetupTestDB(s.ctx, "personalkb")
	s.Require().NoError(err, "setup test db")
	s.testDB = testDB
	s.authToken = "e2e-test-user"
}

func (s *PersonalKBAgentTestSuite) TearDownSuite() {
	if s.testDB != nil {
		s.testDB.Close()
	}
}

func (s *PersonalKBAgentTestSuite) SetupTest() {
	if s.external {
		s.agentName = "personal-kb-agent"
		return
	}

	// 1. Truncate + base fixtures.
	s.Require().NoError(testutil.TruncateTables(s.ctx, s.testDB.DB))
	s.Require().NoError(testutil.SetupTestFixtures(s.ctx, s.testDB.DB))

	s.orgID = uuid.New().String()
	s.projectID = uuid.New().String()

	// 2. Full test project — org, project, memberships, provider creds, model config.
	s.Require().NoError(testutil.SetupFullTestProject(s.ctx, s.testDB.DB, s.orgID, s.projectID))

	// 3. Ensure the personal-kb-agent definition.
	agentRepo := agents.NewRepository(s.testDB.DB)
	_, err := agentRepo.EnsurePersonalKBAgent(s.ctx, s.projectID)
	s.Require().NoError(err, "ensure personal-kb-agent")
	s.agentName = "personal-kb-agent"

	// 4. Build the in-process server (real embeddings + embedding worker wired).
	s.inProcess = testutil.NewTestServerWithLLM(s.testDB)
	s.client = testutil.NewHTTPClient(s.inProcess.Echo)

	// 5. Set project_info via PATCH /api/projects/{id} — no direct DB insert.
	s.Require().NoError(s.client.PatchProject(s.projectID, s.authToken, map[string]any{
		"project_info": "Personal AI Knowledge Base — stores notes, contacts, ideas, events, and facts about my life and work.",
	}), "set project_info")

	// 6. Install the personal-kb schema via HTTP API.
	s.installSchema()
}

func (s *PersonalKBAgentTestSuite) TearDownTest() {
	if s.inProcess != nil && s.inProcess.StopFn != nil {
		s.inProcess.StopFn()
		s.inProcess = nil
	}
}

// ---------------------------------------------------------------------------
// Schema helpers
// ---------------------------------------------------------------------------

// installSchema creates the personal-kb schema in the global registry and
// installs it to the test project, both via the HTTP schemas API.
func (s *PersonalKBAgentTestSuite) installSchema() {
	s.T().Helper()

	// Create schema in the global registry.
	createResp := s.client.POST("/api/schemas",
		testutil.WithAuth(s.authToken),
		testutil.WithProjectID(s.projectID),
		testutil.WithOrgID(s.orgID),
		testutil.WithJSONBody(personalKBSchema),
	)
	s.Require().Equal(http.StatusCreated, createResp.StatusCode,
		"create schema: %s", createResp.Body)

	var schemaBody map[string]any
	s.Require().NoError(json.Unmarshal(createResp.Body, &schemaBody))
	s.schemaID, _ = schemaBody["id"].(string)
	s.Require().NotEmpty(s.schemaID, "schema id must be set")
	s.T().Logf("created schema id=%s", s.schemaID)

	// Install schema to the test project.
	assignResp := s.client.POST(
		fmt.Sprintf("/api/schemas/projects/%s/assign", s.projectID),
		testutil.WithAuth(s.authToken),
		testutil.WithProjectID(s.projectID),
		testutil.WithOrgID(s.orgID),
		testutil.WithJSONBody(map[string]any{
			"schema_id": s.schemaID,
			"dry_run":   false,
			"merge":     false,
		}),
	)
	s.Require().Equal(http.StatusCreated, assignResp.StatusCode,
		"install schema: %s", assignResp.Body)
	s.T().Logf("installed schema to project %s", s.projectID)
}

// ---------------------------------------------------------------------------
// Chat helpers
// ---------------------------------------------------------------------------

func (s *PersonalKBAgentTestSuite) postChat(messages []map[string]any) *testutil.HTTPResponse {
	return s.client.POST(
		"/v1/chat/completions",
		testutil.WithAuth(s.authToken),
		testutil.WithProjectID(s.projectID),
		testutil.WithJSONBody(map[string]any{
			"model":    s.agentName,
			"messages": messages,
		}),
	)
}

func (s *PersonalKBAgentTestSuite) requireLLM() {
	s.T().Helper()
	testutil.LoadEnvFiles()
	for _, k := range []string{"DEEPSEEK_API_KEY", "OPENAI_API_KEY", "GOOGLE_API_KEY"} {
		if os.Getenv(k) != "" {
			return
		}
	}
	s.T().Skip("no LLM provider key — skipping LLM-dependent test")
}

// userMsg builds a simple user turn.
func userMsg(content string) map[string]any {
	return map[string]any{"role": "user", "content": content}
}

// assistantMsg builds an assistant turn for history injection.
func assistantMsg(content string) map[string]any {
	return map[string]any{"role": "assistant", "content": content}
}

// agentReply sends a single-turn chat and returns the assistant reply text.
func (s *PersonalKBAgentTestSuite) agentReply(prompt string) string {
	s.T().Helper()
	resp := s.postChat([]map[string]any{userMsg(prompt)})
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body: %s", resp.Body)
	cr := parseChatResponse(s.T(), resp.Body)
	reply := cr.Choices[0].Message.Content
	s.T().Logf("  → %q", reply)
	return reply
}

// waitForObjects polls GET /api/graph/objects/search until at least minCount
// objects of the given type exist in the project, or timeout expires.
func (s *PersonalKBAgentTestSuite) waitForObjects(typ string, minCount int, timeout time.Duration) []map[string]any {
	s.T().Helper()
	var found []map[string]any
	s.Assert().Eventually(func() bool {
		params := map[string]string{"limit": "50"}
		if typ != "" {
			params["type"] = typ
		}
		items, err := s.client.SearchGraphObjects(s.projectID, s.orgID, s.authToken, params)
		if err != nil {
			return false
		}
		found = items
		return len(items) >= minCount
	}, timeout, 500*time.Millisecond,
		"expected ≥%d objects of type %q within %s", minCount, typ, timeout)
	return found
}

// waitForEmbedding waits for the graph embedding worker to process the given object.
// embedding_updated_at is excluded from the API response (json:"-"), so we poll
// the graph_embedding_jobs table directly via the test DB.
// Falls back to a fixed sleep when testDB is nil (external mode).
func (s *PersonalKBAgentTestSuite) waitForEmbedding(objectID string, timeout time.Duration) {
	s.T().Helper()
	if s.testDB == nil {
		time.Sleep(timeout / 2)
		return
	}
	s.Assert().Eventually(func() bool {
		var count int
		err := s.testDB.DB.NewRaw(`
			SELECT COUNT(*) FROM kb.graph_embedding_jobs
			WHERE object_id = ? AND status IN ('pending','processing')`, objectID).
			Scan(s.ctx, &count)
		if err != nil {
			return false
		}
		return count == 0
	}, timeout, 1*time.Second,
		"embedding job for object %s should complete within %s", objectID, timeout)
	// Give the DB update (embedding_v2) a moment to commit after job completion.
	time.Sleep(200 * time.Millisecond)
}

// ---------------------------------------------------------------------------
// Test 1 — Save a fact and recall it
// ---------------------------------------------------------------------------

func (s *PersonalKBAgentTestSuite) TestSaveAndRecallFact() {
	s.requireLLM()

	// Ask agent to save a fact.
	saveReply := s.agentReply("Remember this: Paris is the capital of France.")
	s.T().Logf("save reply: %s", saveReply)
	s.NotEmpty(saveReply)

	// Give the agent a moment to complete the entity-create tool call.
	objects := s.waitForObjects("", 1, 15*time.Second)
	s.T().Logf("objects in graph: %d", len(objects))

	// Ask the agent to recall the fact.
	messages := []map[string]any{
		userMsg("Remember this: Paris is the capital of France."),
		assistantMsg(saveReply),
		userMsg("What is the capital of France?"),
	}
	resp := s.postChat(messages)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body: %s", resp.Body)
	cr := parseChatResponse(s.T(), resp.Body)
	reply := strings.ToLower(cr.Choices[0].Message.Content)
	s.T().Logf("recall reply: %q", reply)
	s.Contains(reply, "paris", "agent should recall Paris as the capital")
}

// ---------------------------------------------------------------------------
// Test 2 — Save a person and recall by name
// ---------------------------------------------------------------------------

func (s *PersonalKBAgentTestSuite) TestSavePerson() {
	s.requireLLM()

	saveReply := s.agentReply(
		"Save this contact: Alice Smith is a software engineer at Acme Corp. " +
			"Her notes: very reliable, good at Go and Kubernetes.")
	s.T().Logf("save reply: %s", saveReply)

	// Wait for the Person entity to appear in the graph.
	objects := s.waitForObjects("Person", 1, 15*time.Second)
	s.Require().NotEmpty(objects, "at least one Person entity must exist after saving Alice")

	// Verify via direct API that the entity has the right type.
	props, _ := objects[0]["properties"].(map[string]any)
	name, _ := props["name"].(string)
	s.T().Logf("graph object name=%q type=%v", name, objects[0]["type"])

	// Ask agent to recall.
	messages := []map[string]any{
		userMsg("Save this contact: Alice Smith is a software engineer at Acme Corp. Her notes: very reliable, good at Go and Kubernetes."),
		assistantMsg(saveReply),
		userMsg("Who is Alice Smith and where does she work?"),
	}
	resp := s.postChat(messages)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body: %s", resp.Body)
	cr := parseChatResponse(s.T(), resp.Body)
	reply := strings.ToLower(cr.Choices[0].Message.Content)
	s.T().Logf("recall reply: %q", reply)
	s.Contains(reply, "acme", "agent should mention Acme Corp")
}

// ---------------------------------------------------------------------------
// Test 3 — Save a note and query by type
// ---------------------------------------------------------------------------

func (s *PersonalKBAgentTestSuite) TestSaveNoteAndQueryByType() {
	s.requireLLM()

	saveReply := s.agentReply("Note to self: I want to learn Rust programming in 2026 — start with the Rust book.")
	s.T().Logf("save reply: %s", saveReply)

	// Wait for note entity.
	_ = s.waitForObjects("Note", 1, 15*time.Second)

	// Ask agent what notes it has.
	messages := []map[string]any{
		userMsg("Note to self: I want to learn Rust programming in 2026 — start with the Rust book."),
		assistantMsg(saveReply),
		userMsg("What notes do I have saved?"),
	}
	resp := s.postChat(messages)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body: %s", resp.Body)
	cr := parseChatResponse(s.T(), resp.Body)
	reply := strings.ToLower(cr.Choices[0].Message.Content)
	s.T().Logf("recall reply: %q", reply)
	s.True(
		strings.Contains(reply, "rust") || strings.Contains(reply, "note"),
		"agent should mention the Rust note",
	)
}

// ---------------------------------------------------------------------------
// Test 4 — Save a relationship and recall connections
// ---------------------------------------------------------------------------

func (s *PersonalKBAgentTestSuite) TestSaveRelationshipAndRecall() {
	s.requireLLM()

	saveReply := s.agentReply(
		"Save two contacts: Alice Smith (engineer) and Bob Jones (designer). " +
			"They work together at the same company.")
	s.T().Logf("save reply: %s", saveReply)

	// Wait for at least 2 Person entities.
	objects := s.waitForObjects("Person", 2, 20*time.Second)
	s.T().Logf("Person entities in graph: %d", len(objects))

	// Ask about Alice's connections.
	messages := []map[string]any{
		userMsg("Save two contacts: Alice Smith (engineer) and Bob Jones (designer). They work together at the same company."),
		assistantMsg(saveReply),
		userMsg("Who does Alice work with?"),
	}
	resp := s.postChat(messages)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body: %s", resp.Body)
	cr := parseChatResponse(s.T(), resp.Body)
	reply := strings.ToLower(cr.Choices[0].Message.Content)
	s.T().Logf("recall reply: %q", reply)
	s.True(
		strings.Contains(reply, "bob") || strings.Contains(reply, "jones"),
		"agent should mention Bob Jones as Alice's colleague",
	)
}

// ---------------------------------------------------------------------------
// Test 5 — Agent uses project-get to describe KB purpose
// ---------------------------------------------------------------------------

func (s *PersonalKBAgentTestSuite) TestProjectGet() {
	s.requireLLM()

	reply := s.agentReply("What kind of information can I store in this knowledge base?")
	s.T().Logf("reply: %q", reply)

	replyLower := strings.ToLower(reply)
	// The agent should call project-get and reference the KB purpose we set.
	s.True(
		strings.Contains(replyLower, "note") ||
			strings.Contains(replyLower, "contact") ||
			strings.Contains(replyLower, "fact") ||
			strings.Contains(replyLower, "event") ||
			strings.Contains(replyLower, "personal"),
		"agent should describe the KB purpose (notes, contacts, facts, events)",
	)
}

// ---------------------------------------------------------------------------
// Test 6 — Semantic search: retrieve by meaning, not exact words
// ---------------------------------------------------------------------------

// TestSemanticSearch verifies that search-hybrid can find entities via
// semantic similarity even when the query uses different words than the stored content.
// Requires real embeddings (skipped without Google AI key).
func (s *PersonalKBAgentTestSuite) TestSemanticSearch() {
	s.requireLLM()

	// Save via agent (uses entity-create → enqueues embedding job).
	saveReply := s.agentReply(
		"Save this event: quarterly business performance review meeting scheduled for June 15.")
	s.T().Logf("save reply: %s", saveReply)

	// Wait for the Event entity to appear.
	objects := s.waitForObjects("Event", 1, 15*time.Second)
	s.Require().NotEmpty(objects, "Event entity must exist")

	// Find the canonical_id and wait for embedding to be generated.
	var canonicalID string
	for _, obj := range objects {
		if id, ok := obj["canonical_id"].(string); ok && id != "" {
			canonicalID = id
			break
		}
		if id, ok := obj["entity_id"].(string); ok && id != "" {
			canonicalID = id
			break
		}
	}
	if canonicalID != "" {
		s.waitForEmbedding(canonicalID, 30*time.Second)
	}

	// Ask with semantically similar but lexically different query.
	messages := []map[string]any{
		userMsg("Save this event: quarterly business performance review meeting scheduled for June 15."),
		assistantMsg(saveReply),
		userMsg("What upcoming meetings or appointments do I have?"),
	}
	resp := s.postChat(messages)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "body: %s", resp.Body)
	cr := parseChatResponse(s.T(), resp.Body)
	reply := strings.ToLower(cr.Choices[0].Message.Content)
	s.T().Logf("semantic recall reply: %q", reply)
	s.True(
		strings.Contains(reply, "quarterly") ||
			strings.Contains(reply, "review") ||
			strings.Contains(reply, "performance") ||
			strings.Contains(reply, "june") ||
			strings.Contains(reply, "meeting"),
		"agent should find the event via semantic search",
	)
}

// ---------------------------------------------------------------------------
// Test 7 — Multi-turn: save in turn 1, add details in turn 2, recall in turn 3
// ---------------------------------------------------------------------------

func (s *PersonalKBAgentTestSuite) TestMultiTurnSaveAndRecall() {
	s.requireLLM()

	// Turn 1: plant the initial fact.
	turn1Reply := s.agentReply("Remember that Bob's phone number is 555-1234.")
	s.T().Logf("turn 1 reply: %s", turn1Reply)

	// Wait for entity to be saved before proceeding.
	_ = s.waitForObjects("", 1, 15*time.Second)

	// Turn 2: add more detail about Bob.
	turn2Messages := []map[string]any{
		userMsg("Remember that Bob's phone number is 555-1234."),
		assistantMsg(turn1Reply),
		userMsg("Also remember that Bob Jones works at TechCorp as a senior designer."),
	}
	turn2Resp := s.postChat(turn2Messages)
	s.Require().Equal(http.StatusOK, turn2Resp.StatusCode, "turn2: %s", turn2Resp.Body)
	turn2CR := parseChatResponse(s.T(), turn2Resp.Body)
	turn2Reply := turn2CR.Choices[0].Message.Content
	s.T().Logf("turn 2 reply: %s", turn2Reply)

	// Wait for any new entities/updates.
	_ = s.waitForObjects("", 1, 10*time.Second)

	// Turn 3: ask a question that requires both pieces of information.
	turn3Messages := []map[string]any{
		userMsg("Remember that Bob's phone number is 555-1234."),
		assistantMsg(turn1Reply),
		userMsg("Also remember that Bob Jones works at TechCorp as a senior designer."),
		assistantMsg(turn2Reply),
		userMsg("How can I contact Bob and what company does he work for?"),
	}
	turn3Resp := s.postChat(turn3Messages)
	s.Require().Equal(http.StatusOK, turn3Resp.StatusCode, "turn3: %s", turn3Resp.Body)
	turn3CR := parseChatResponse(s.T(), turn3Resp.Body)
	reply := strings.ToLower(turn3CR.Choices[0].Message.Content)
	s.T().Logf("turn 3 recall reply: %q", reply)

	s.Contains(reply, "555", "agent must recall Bob's phone number from turn 1")
	s.True(
		strings.Contains(reply, "techcorp") || strings.Contains(reply, "tech corp"),
		"agent must recall Bob's employer from turn 2",
	)
}
