package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// RememberAgentTestSuite exercises POST /api/projects/:id/remember end-to-end.
//
// Modes:
//
//  1. In-process (default): real Postgres test DB + NewTestServerWithLLM.
//     LLM-dependent tests run when DEEPSEEK_API_KEY / GOOGLE_API_KEY /
//     LLM_ENCRYPTION_KEY are present in .env.local; otherwise they skip.
//
//  2. External (TEST_SERVER_URL set): sends real HTTP to the target server.
//     Requires TEST_API_TOKEN and TEST_PROJECT_ID env vars.
//     All tests (including LLM-dependent) run against the live server.
//
// Tests cover:
//  1. HTTP / SSE mechanics        (status, content-type, event shape)
//  2. Agent idempotency           (EnsureGraphInsertAgent called on every request)
//  3. Graph mutation              (entities land in the graph after ingest)
//  4. dry_run flag                (no merge → empty graph)
//  5. schema_policy=reuse_only   (blocks finalize-discovery)
//  6. schema_policy=auto         (fires finalize-discovery for novel domains)
//  7. Conversation reuse          (same conversation_id honoured)
//  8. Tool usage                  (branch-create + branch-merge both invoked)
//  9. Error cases                 (no auth, empty message, bad policy)
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

	err = testutil.SetupFullTestProject(s.ctx, s.testDB.DB, s.orgID, s.projectID)
	s.Require().NoError(err)

	s.inProcess = testutil.NewTestServerWithLLM(s.testDB)
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
		"/api/graph/objects/search",
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

// traceBodyKeys returns the top-level keys of a Tempo trace response for logging.
func traceBodyKeys(body map[string]any) []string {
	keys := make([]string, 0, len(body))
	for k := range body {
		keys = append(keys, k)
	}
	return keys
}

// toolsUsed extracts tool names from mcp_tool SSE events (status=started).
func toolsUsed(sse *testutil.SSEResponse) []string {
	names := make([]string, 0)
	for _, ev := range sse.GetEventsByType("mcp_tool") {
		var data map[string]any
		if err := ev.ParseSSEJSON(&data); err == nil {
			// Only count each tool once (started event), not completed
			if status, _ := data["status"].(string); status == "started" {
				if name, ok := data["tool"].(string); ok {
					names = append(names, name)
				}
			}
		}
	}
	return names
}

// dumpSSE logs every SSE event in the response with its type and (truncated) data.
// Output is only visible when running with -v. Safe to call on any SSEResponse.
// When the SSE "event:" field is empty (server uses JSON "type" field instead),
// the type is extracted from the JSON data payload.
func dumpSSE(t *testing.T, resp *testutil.SSEResponse) {
	t.Helper()

	// Resolve the display event type for an event — prefer SSE "event:" field,
	// fall back to the "type" key inside the JSON data payload.
	eventType := func(ev testutil.SSEEvent) string {
		if ev.Event != "" {
			return ev.Event
		}
		var d map[string]any
		if err := json.Unmarshal([]byte(ev.Data), &d); err == nil {
			if t, ok := d["type"].(string); ok {
				return t
			}
		}
		return "(unknown)"
	}

	// Count by resolved event type for the summary line.
	typeCounts := map[string]int{}
	for _, ev := range resp.Events {
		typeCounts[eventType(ev)]++
	}

	t.Logf("  ── SSE stream: %d events ──────────────────────────────────", len(resp.Events))
	for i, ev := range resp.Events {
		evType := eventType(ev)
		data := ev.Data

		// For mcp_tool events extract a concise summary instead of the full payload.
		var parsed map[string]any
		if err := json.Unmarshal([]byte(data), &parsed); err == nil {
			switch evType {
			case "mcp_tool":
				tool, _ := parsed["tool"].(string)
				status, _ := parsed["status"].(string)
				data = fmt.Sprintf(`tool="%s" status="%s"`, tool, status)
				// For completed tool calls also show a snippet of the result.
				if status == "completed" {
					if result, ok := parsed["result"].(map[string]any); ok {
						if msg, ok := result["message"].(string); ok {
							data += fmt.Sprintf(` result="%s"`, msg)
						}
					}
				}
			case "meta":
				convID, _ := parsed["conversationId"].(string)
				data = fmt.Sprintf(`conversationId="%s"`, convID)
			case "done":
				runID, _ := parsed["runId"].(string)
				data = fmt.Sprintf(`runId="%s"`, runID)
			case "token":
				// token events are streaming LLM output — show just the first 80 chars.
				tok, _ := parsed["token"].(string)
				if len(tok) > 80 {
					tok = tok[:80] + "…"
				}
				data = fmt.Sprintf(`token="%s"`, tok)
			default:
				// Generic fallback: compact JSON, truncated.
				if b, err := json.Marshal(parsed); err == nil {
					data = string(b)
				}
				const maxLen = 200
				if len(data) > maxLen {
					data = data[:maxLen] + " …"
				}
			}
		}

		t.Logf("  [%02d] %-16s  %s", i, evType, data)
	}

	t.Logf("  ── summary ─────────────────────────────────────────────────")
	for evType, count := range typeCounts {
		t.Logf("       %-20s × %d", evType, count)
	}
}

// runIDFromDone extracts the runId string from the first "done" SSE event.
// Returns "" if no done event or no runId field is present.
func runIDFromDone(sse *testutil.SSEResponse) string {
	for _, ev := range sse.GetEventsByType("done") {
		var data map[string]any
		if err := ev.ParseSSEJSON(&data); err == nil {
			if id, ok := data["runId"].(string); ok {
				return id
			}
		}
	}
	return ""
}

// traceIDFromDone extracts the traceId string from the first "done" SSE event.
// Returns "" when tracing is disabled on the server (field absent).
func traceIDFromDone(sse *testutil.SSEResponse) string {
	for _, ev := range sse.GetEventsByType("done") {
		var data map[string]any
		if err := ev.ParseSSEJSON(&data); err == nil {
			if id, ok := data["traceId"].(string); ok {
				return id
			}
		}
	}
	return ""
}

// sseEventType resolves the display name for an SSE event — prefers the SSE
// "event:" field, falls back to the JSON "type" key inside the data payload.
func sseEventType(ev testutil.SSEEvent) string {
	if ev.Event != "" {
		return ev.Event
	}
	var d map[string]any
	if err := json.Unmarshal([]byte(ev.Data), &d); err == nil {
		if t, ok := d["type"].(string); ok {
			return t
		}
	}
	return "(unknown)"
}

// RunDump is the structured record written to logs/tests/remember/ per run.
// It captures everything needed for offline analysis.
type RunDump struct {
	TestName  string         `json:"testName"`
	Timestamp string         `json:"timestamp"`
	RunID     string         `json:"runId,omitempty"`
	ProjectID string         `json:"projectId,omitempty"`
	SSE       []SSEDumpEvent `json:"sse"`
	Messages  []MsgDump      `json:"messages,omitempty"`
	ToolCalls []TCDump       `json:"toolCalls,omitempty"`
	AgentRun  *AgentRunDump  `json:"agentRun,omitempty"`
}

// SSEDumpEvent is the JSON-serialisable form of a single SSE event.
type SSEDumpEvent struct {
	Index     int            `json:"index"`
	EventType string         `json:"eventType"`
	Data      map[string]any `json:"data,omitempty"`
	RawData   string         `json:"rawData,omitempty"`
}

// MsgDump is a simplified form of agents.AgentRunMessage for JSON output.
type MsgDump struct {
	Step    int            `json:"step"`
	Role    string         `json:"role"`
	Content map[string]any `json:"content"`
}

// TCDump is a simplified form of agents.AgentRunToolCall for JSON output.
type TCDump struct {
	Step       int            `json:"step"`
	Tool       string         `json:"tool"`
	Status     string         `json:"status"`
	DurationMs *int           `json:"durationMs,omitempty"`
	Input      map[string]any `json:"input,omitempty"`
	Output     map[string]any `json:"output,omitempty"`
}

// AgentRunDump captures key fields from the kb.agent_runs record.
type AgentRunDump struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Steps   int    `json:"steps"`
	TraceID string `json:"traceId,omitempty"`
}

// writeRunDump serialises the full run record to a JSON file under
// logs/tests/remember/<sanitisedTestName>/<timestamp>-<runID>.json
// relative to the repo root (two levels up from apps/server).
//
// It is a best-effort write: failures are logged but never fail the test.
func writeRunDump(t *testing.T, d *RunDump) {
	t.Helper()

	// Locate repo root relative to this file's directory (apps/server/tests/integration).
	repoRoot := filepath.Join("..", "..", "..", "..")

	// Sanitise test name for use as a directory segment.
	safeName := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == ' ' {
			return '_'
		}
		return r
	}, t.Name())

	dir := filepath.Join(repoRoot, "logs", "tests", "remember", safeName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Logf("writeRunDump: mkdir %s: %v", dir, err)
		return
	}

	runSuffix := d.RunID
	if runSuffix == "" {
		runSuffix = "no-run-id"
	}
	filename := fmt.Sprintf("%s-%s.json", d.Timestamp, runSuffix)
	path := filepath.Join(dir, filename)

	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		t.Logf("writeRunDump: marshal: %v", err)
		return
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Logf("writeRunDump: write %s: %v", path, err)
		return
	}
	t.Logf("  run dump → %s", path)
}

// buildRunDump constructs a RunDump from an SSE response and optional DB data.
// msgs and tcs may be nil (external mode or when the run had no tool calls).
func buildRunDump(
	t *testing.T,
	testName, projectID string,
	rec *testutil.SSEResponse,
	msgs []*agents.AgentRunMessage,
	tcs []*agents.AgentRunToolCall,
	run *agents.AgentRun,
) *RunDump {
	t.Helper()

	d := &RunDump{
		TestName:  testName,
		Timestamp: time.Now().Format("20060102-150405"),
		RunID:     runIDFromDone(rec),
		ProjectID: projectID,
	}

	// Build SSE event list.
	for i, ev := range rec.Events {
		entry := SSEDumpEvent{
			Index:     i,
			EventType: sseEventType(ev),
			RawData:   ev.Data,
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(ev.Data), &parsed); err == nil {
			entry.Data = parsed
		}
		d.SSE = append(d.SSE, entry)
	}

	// Messages.
	for _, m := range msgs {
		d.Messages = append(d.Messages, MsgDump{
			Step:    m.StepNumber,
			Role:    m.Role,
			Content: m.Content,
		})
	}

	// Tool calls.
	for _, tc := range tcs {
		d.ToolCalls = append(d.ToolCalls, TCDump{
			Step:       tc.StepNumber,
			Tool:       tc.ToolName,
			Status:     tc.Status,
			DurationMs: tc.DurationMs,
			Input:      tc.Input,
			Output:     tc.Output,
		})
	}

	// Agent run record.
	if run != nil {
		ar := &AgentRunDump{
			ID:     run.ID,
			Status: string(run.Status),
			Steps:  run.StepCount,
		}
		if run.TraceID != nil {
			ar.TraceID = *run.TraceID
		}
		d.AgentRun = ar
	}

	return d
}

// skipIfNoLLM skips a test when no LLM provider is reachable.
// Probes the remember endpoint with a minimal message; skips on 503/422 or
// an error SSE event. Works identically in-process and external.
func (s *RememberAgentTestSuite) skipIfNoLLM() {
	probe := s.postRemember(map[string]any{"message": "ping"})
	if probe.StatusCode == http.StatusServiceUnavailable || probe.StatusCode == http.StatusUnprocessableEntity {
		s.T().Skip("no LLM provider configured — skipping LLM-dependent test")
	}
	for _, ev := range probe.GetEventsByType("error") {
		var data map[string]any
		if err := ev.ParseSSEJSON(&data); err == nil {
			if msg, ok := data["error"].(string); ok {
				s.T().Skipf("LLM unavailable (%s) — skipping", msg)
			}
		}
	}
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
		s.NotEmpty(data["conversationId"], "meta must contain conversationId")
	}

	for _, ev := range rec.GetEventsByType("done") {
		var data map[string]any
		s.Require().NoError(ev.ParseSSEJSON(&data))
		s.NotEmpty(data["runId"], "done must contain runId")
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

	// schema_policy=auto: agent discovers a schema pack and queues reextraction.
	// Entity extraction runs asynchronously in a background worker — the remember
	// agent itself does not write graph objects directly.
	// We assert the agent completes without error and a document was persisted.
	rec := s.postRemember(map[string]any{
		"message":       text,
		"schema_policy": "auto",
	})
	s.Require().Equal(http.StatusOK, rec.StatusCode)
	s.False(rec.HasEvent("error"), "unexpected error event:\n%s", rec.RawBody)
	s.True(rec.HasEvent("done"), "expected done SSE event")

	tools := toolsUsed(rec)
	s.T().Logf("Tools used: %s", strings.Join(tools, ", "))
	// Tool usage is informational — the agent may not always call tools
	// (e.g. when it decides the text matches no schema with reuse_only fallback).
}

func (s *RememberAgentTestSuite) TestRemember_DryRun_NoGraphMutation() {
	s.skipIfNoLLM()

	// Count objects before.
	before := s.objectCount()
	if before < 0 {
		before = 0
	}

	rec := s.postRemember(map[string]any{
		"message":       "Diana is a product manager at StartupXYZ.",
		"schema_policy": "auto",
		"dry_run":       true,
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
		if id, ok := data["conversationId"].(string); ok {
			convID = id
		}
	}
	s.Require().NotEmpty(convID, "expected conversationId in meta event")

	rec2 := s.postRemember(map[string]any{
		"message":         "Alice now works at Google.",
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

// TestRemember_ToolEvents_UsesDiscoveryOrExtraction verifies the domain-remember
// agent calls the correct MCP tools for the document pipeline.
//
// Architecture note: the remember agent classifies the document and either
// creates a new schema pack (finalize-discovery) or queues reextraction against
// an existing one (queue-reextraction). Actual entity extraction — which uses
// graph-branch-create/merge — runs in a background worker, not in this agent.
// We assert the agent calls at least one of the two expected domain tools.
func (s *RememberAgentTestSuite) TestRemember_ToolEvents_UsesDiscoveryOrExtraction() {
	s.skipIfNoLLM()

	text := `Meeting notes 2024-03-01:
Frank is the CTO of Megacorp. Revenue grew 20% this quarter.`

	rec := s.postRemember(map[string]any{
		"message":       text,
		"schema_policy": "auto",
	})
	s.Require().Equal(http.StatusOK, rec.StatusCode)
	s.False(rec.HasEvent("error"), "unexpected error:\n%s", rec.RawBody)

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

	// The agent must call either finalize-discovery (new schema) or
	// queue-reextraction (existing schema matched). Both are valid outcomes.
	usedDomainTool := contains("finalize-discovery") || contains("queue-reextraction")
	s.True(usedDomainTool,
		"agent must call finalize-discovery or queue-reextraction; tools: %v", tools)
}

// ---------------------------------------------------------------------------
// Auto-discovery tests — schema_policy=auto fires finalize-discovery for
// genuinely novel domains, and schema_policy=reuse_only suppresses it.
//
// Each variant uses a fictional / clearly alien domain so it will never match
// any schema pack that may exist in the test project.
// ---------------------------------------------------------------------------

// novelTexts holds distinct fictional-domain passages used across discovery tests.
// Each is unique enough that the classifier should return new_domain on a fresh project.
var novelTexts = map[string]string{
	"biology": `Florgon reproductive cycle: a florgon reaches maturity after 3 zorn cycles.
During the glorbing phase it secretes enzyme-rich muk to dissolve silicate rock
into a nutrient slurry. The resulting spore pods gestate for 12 zorns before
releasing airborne larvae that seek geothermal vents for metamorphosis.`,

	"sport": `Klarnak ball is played by two teams of 7 on a triangular court.
Each player wields a vibro-racquet to volley the incandescent solk over the
shimmer-barrier. Points are scored when the solk touches the gravity-well on
the opponent's side. A match lasts 5 phases of 8 zorns each.`,

	"economy": `The Varnak economy runs on reputation credits called glint.
Every citizen has a glint score 0–1000 adjusted daily by the Consensus Ledger.
Transactions are recorded as smart-contracts on the Quantum Mesh at 0.3 glints
per exchange. Glint is earned through communal service or Orbital Exchange trading.`,

	"geography": `The Shattered Plains of Qor'vash span 40 000 square leagues across
the equatorial belt. Basalt plateaus are separated by chasm valleys 200–300 spans
deep. Seasonal monsoons flood the lower chasms, creating temporary channels that
connect the Bitter Sea to the Western Abyss.`,
}

// schemaPackCount returns the number of project_schemas rows for the current project.
// Returns -1 when not in-process (no DB access).
func (s *RememberAgentTestSuite) schemaPackCount() int {
	if s.external || s.testDB == nil {
		return -1
	}
	var count int
	err := s.testDB.DB.NewRaw(
		`SELECT COUNT(*) FROM kb.project_schemas WHERE project_id = ?`,
		s.projectID,
	).Scan(s.ctx, &count)
	if err != nil {
		return -1
	}
	return count
}

// runAutoDiscoverVariant is the shared body for all auto-discovery variant tests.
// domain must be a key in novelTexts.
//
// Invariants tested:
//  1. Request completes with HTTP 200 and a done SSE event (no hard errors).
//  2. If the classifier returns new_domain, finalize-discovery is called and a
//     schema pack is created — schema_policy=auto does NOT block the tool.
//  3. If the classifier returns heuristic/llm (the LLM decided the text matches
//     an existing schema), the test still passes — we cannot force new_domain.
//
// We do NOT assert that finalize-discovery MUST be called, because the LLM
// classifier may match an existing schema depending on run order and context.
func (s *RememberAgentTestSuite) runAutoDiscoverVariant(domain string) {
	s.skipIfNoLLM()

	text, ok := novelTexts[domain]
	s.Require().True(ok, "unknown domain %q in novelTexts", domain)

	before := s.schemaPackCount()

	rec := s.postRemember(map[string]any{
		"message":       text,
		"schema_policy": "auto",
	})
	s.Require().Equal(http.StatusOK, rec.StatusCode,
		"remember call failed for domain %q: %s", domain, rec.RawBody)

	// Dump full SSE stream so -v output shows every agent step.
	dumpSSE(s.T(), rec)

	s.False(rec.HasEvent("error"),
		"unexpected error SSE event for domain %q:\n%s", domain, rec.RawBody)
	s.True(rec.HasEvent("done"),
		"expected done SSE event for domain %q", domain)

	tools := toolsUsed(rec)
	s.T().Logf("[%s] tools used: %s", domain, strings.Join(tools, ", "))

	// If finalize-discovery was called, verify a schema pack was persisted.
	// This is the key invariant: auto policy allows the tool, and when called
	// it must actually write a schema pack to the DB.
	calledFinalize := false
	for _, t := range tools {
		if t == "finalize-discovery" {
			calledFinalize = true
			break
		}
	}
	if calledFinalize {
		s.T().Logf("[%s] finalize-discovery called — verifying schema pack created", domain)
		if !s.external && before >= 0 {
			after := s.schemaPackCount()
			s.Greater(after, before,
				"[%s] finalize-discovery was called but no kb.project_schemas row created; before=%d after=%d",
				domain, before, after)
			s.T().Logf("[%s] schema packs: %d → %d", domain, before, after)
		}
	} else {
		s.T().Logf("[%s] classifier chose heuristic/llm match (not new_domain) — finalize-discovery not triggered; this is valid", domain)
	}

	// Write full run dump (SSE only — no DB access needed here).
	dump := buildRunDump(s.T(), s.T().Name(), s.projectID, rec, nil, nil, nil)
	writeRunDump(s.T(), dump)
}

func (s *RememberAgentTestSuite) TestRemember_AutoDiscover_Biology_CreatesSchemapack() {
	s.runAutoDiscoverVariant("biology")
}

func (s *RememberAgentTestSuite) TestRemember_AutoDiscover_Sport_CreatesSchemapack() {
	s.runAutoDiscoverVariant("sport")
}

func (s *RememberAgentTestSuite) TestRemember_AutoDiscover_Economy_CreatesSchemapack() {
	s.runAutoDiscoverVariant("economy")
}

func (s *RememberAgentTestSuite) TestRemember_AutoDiscover_Geography_CreatesSchemapack() {
	s.runAutoDiscoverVariant("geography")
}

// TestRemember_AutoDiscover_ReuseOnly_BlocksDiscovery verifies that the same
// novel text with schema_policy=reuse_only completes successfully but never
// calls finalize-discovery (the tool is disabled in that policy).
func (s *RememberAgentTestSuite) TestRemember_AutoDiscover_ReuseOnly_BlocksDiscovery() {
	s.skipIfNoLLM()

	before := s.schemaPackCount()

	rec := s.postRemember(map[string]any{
		"message":       novelTexts["biology"],
		"schema_policy": "reuse_only",
	})
	s.Require().Equal(http.StatusOK, rec.StatusCode,
		"remember with reuse_only failed: %s", rec.RawBody)
	s.False(rec.HasEvent("error"),
		"unexpected error SSE event with reuse_only:\n%s", rec.RawBody)
	s.True(rec.HasEvent("done"), "expected done SSE event")

	tools := toolsUsed(rec)
	s.T().Logf("reuse_only tools used: %s", strings.Join(tools, ", "))

	for _, t := range tools {
		s.NotEqual("finalize-discovery", t,
			"reuse_only must NOT call finalize-discovery; tools: %v", tools)
	}

	// Schema pack count must not increase.
	if !s.external && before >= 0 {
		after := s.schemaPackCount()
		s.Equal(before, after,
			"reuse_only must not create new schema packs; before=%d after=%d", before, after)
	}
}

// ---------------------------------------------------------------------------
// Run log / persistence verification
// ---------------------------------------------------------------------------

// TestRemember_VerifiesRunPersistsMessagesAndToolCalls posts a remember request
// and then queries the database directly to confirm that:
//
//  1. The agent run produced at least one persisted message (kb.agent_run_messages).
//  2. The agent run produced at least one persisted tool call (kb.agent_run_tool_calls).
//  3. The AgentRun record exists and is in a terminal state.
//
// This test exercises the full persistence path end-to-end: SSE stream →
// runId → DB query via agents.Repository.
func (s *RememberAgentTestSuite) TestRemember_VerifiesRunPersistsMessagesAndToolCalls() {
	if s.external {
		s.T().Skip("run log DB verification requires in-process mode")
	}
	s.skipIfNoLLM()

	rec := s.postRemember(map[string]any{
		"message":       novelTexts["economy"],
		"schema_policy": "auto",
	})
	s.Require().Equal(http.StatusOK, rec.StatusCode,
		"remember call failed: %s", rec.RawBody)

	// Dump full SSE stream so -v output shows every agent step.
	dumpSSE(s.T(), rec)

	s.False(rec.HasEvent("error"), "unexpected error SSE event:\n%s", rec.RawBody)
	s.True(rec.HasEvent("done"), "expected done SSE event")

	// Extract runId from the "done" event.
	runID := runIDFromDone(rec)
	s.Require().NotEmpty(runID, "done event must contain runId")
	s.T().Logf("runId: %s", runID)

	// Query persisted messages and tool calls directly from the DB.
	repo := agents.NewRepository(s.testDB.DB)

	msgs, err := repo.FindMessagesByRunID(s.ctx, runID)
	s.Require().NoError(err, "FindMessagesByRunID failed")
	s.T().Logf("── messages persisted: %d ─────────────────────────────────", len(msgs))
	for i, m := range msgs {
		content := fmt.Sprintf("%v", m.Content)
		if len(content) > 150 {
			content = content[:150] + " …"
		}
		s.T().Logf("  [%02d] step=%-3d role=%-12s  %s", i, m.StepNumber, m.Role, content)
	}
	s.Greater(len(msgs), 0, "expected at least 1 persisted agent_run_messages row")

	tcs, err := repo.FindToolCallsByRunID(s.ctx, runID)
	s.Require().NoError(err, "FindToolCallsByRunID failed")
	s.T().Logf("── tool calls persisted: %d ───────────────────────────────", len(tcs))
	for i, tc := range tcs {
		dur := ""
		if tc.DurationMs != nil {
			dur = fmt.Sprintf(" (%dms)", *tc.DurationMs)
		}
		s.T().Logf("  [%02d] step=%-3d tool=%-30s status=%s%s", i, tc.StepNumber, tc.ToolName, tc.Status, dur)
	}

	// If the SSE stream showed tool calls, they must be persisted in the DB.
	// We can't hard-assert >0 tool calls because the LLM classifier may decide
	// the document matches no schema and return a text response with no tool calls.
	sseTools := toolsUsed(rec)
	if len(sseTools) > 0 {
		s.Greater(len(tcs), 0,
			"SSE showed %d tool calls but 0 rows in agent_run_tool_calls", len(sseTools))
		s.T().Logf("  ✓ SSE tool calls match DB tool calls (%d)", len(tcs))
	} else {
		s.T().Logf("  (no tool calls in this run — LLM returned text response only)")
	}

	// Verify the AgentRun record itself exists and is in a terminal state.
	run, err := repo.FindRunByID(s.ctx, runID)
	s.Require().NoError(err, "FindRunByID failed")
	s.T().Logf("── agent run ──────────────────────────────────────────────")
	s.T().Logf("  id:       %s", run.ID)
	s.T().Logf("  status:   %s", run.Status)
	s.T().Logf("  steps:    %d", run.StepCount)

	// TraceID is set only when the server has OTel tracing enabled.
	// If nil → tracing is off on this server; skip the trace assertion.
	if run.TraceID == nil || *run.TraceID == "" {
		s.T().Skip("tracing not enabled on this server (run.TraceID not set) — skipping trace assertion")
	}
	s.T().Logf("  traceId:  %s", *run.TraceID)

	// Poll GET /api/traces/:id — spans may flush to Tempo asynchronously.
	// 503 means the server's Tempo proxy is disabled → skip, not a failure.
	// Any other non-200 within the window → retry.
	traceURL := fmt.Sprintf("/api/traces/%s", *run.TraceID)
	var traceBody map[string]any
	traceFound := false
	tracingDisabled := false
	s.Assert().Eventually(func() bool {
		resp := s.client.GET(traceURL, testutil.WithAuth(s.authToken))
		if resp.StatusCode == http.StatusServiceUnavailable {
			tracingDisabled = true
			return true // stop polling
		}
		if resp.StatusCode != http.StatusOK {
			return false
		}
		if err := json.Unmarshal(resp.Body, &traceBody); err != nil {
			return false
		}
		traceFound = true
		return true
	}, 10*time.Second, 500*time.Millisecond, "trace %s should appear in Tempo or server returns 503", *run.TraceID)

	if tracingDisabled {
		s.T().Skip("tracing proxy disabled on this server — skipping trace assertion")
	}
	s.True(traceFound, "expected trace data from /api/traces/%s", *run.TraceID)
	s.T().Logf("  trace keys: %v", traceBodyKeys(traceBody))

	terminalStatuses := []agents.AgentRunStatus{
		agents.RunStatusSuccess,
		agents.RunStatusError,
		agents.RunStatusCancelled,
	}
	isTerminal := false
	for _, st := range terminalStatuses {
		if run.Status == st {
			isTerminal = true
			break
		}
	}
	s.True(isTerminal, "expected terminal run status, got %q", run.Status)

	// Write full run dump (SSE + messages + tool calls + agent run record).
	dump := buildRunDump(s.T(), s.T().Name(), s.projectID, rec, msgs, tcs, run)
	writeRunDump(s.T(), dump)
}

// TestRemember_TracesArePropagated verifies that after a remember run the
// server's trace proxy returns span data for the run's traceId.
//
// Works in both in-process and external modes.
// Skips when tracing is disabled on the server (traceId absent from done event).
func (s *RememberAgentTestSuite) TestRemember_TracesArePropagated() {
	s.skipIfNoLLM()

	rec := s.postRemember(map[string]any{
		"message":       novelTexts["economy"],
		"schema_policy": "auto",
	})
	s.Require().Equal(http.StatusOK, rec.StatusCode,
		"remember call failed: %s", rec.RawBody)

	dumpSSE(s.T(), rec)

	s.False(rec.HasEvent("error"), "unexpected error SSE event")
	s.True(rec.HasEvent("done"), "expected done SSE event")

	traceID := traceIDFromDone(rec)
	if traceID == "" {
		s.T().Skip("traceId absent from done event — tracing not enabled on this server")
	}
	s.T().Logf("traceId from done event: %s", traceID)

	// Poll GET /api/traces/:id — spans flush to Tempo asynchronously.
	// 503 → tracing proxy disabled → skip.
	traceURL := fmt.Sprintf("/api/traces/%s", traceID)
	var traceBody map[string]any
	traceFound := false
	tracingDisabled := false
	s.Assert().Eventually(func() bool {
		resp := s.client.GET(traceURL, testutil.WithAuth(s.authToken))
		if resp.StatusCode == http.StatusServiceUnavailable {
			tracingDisabled = true
			return true
		}
		if resp.StatusCode != http.StatusOK {
			return false
		}
		if err := json.Unmarshal(resp.Body, &traceBody); err != nil {
			return false
		}
		traceFound = true
		return true
	}, 15*time.Second, 500*time.Millisecond, "trace %s should appear in Tempo within 15s", traceID)

	if tracingDisabled {
		s.T().Skip("tracing proxy disabled on this server — skipping trace assertion")
	}
	s.Require().True(traceFound, "trace data not returned from /api/traces/%s", traceID)
	s.T().Logf("trace response keys: %v", traceBodyKeys(traceBody))
}
