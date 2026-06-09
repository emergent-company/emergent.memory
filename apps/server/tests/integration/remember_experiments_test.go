package integration

// RememberExperimentsTestSuite runs A/B experiments on the remember pipeline.
//
// Each experiment:
//   1. Seeds a custom agent definition with a versioned system prompt.
//   2. Registers it as the project's remember agent via project_settings.
//   3. POSTs /remember with the shared novelTexts fixtures.
//   4. Measures QualityMetrics: type count, properties per type, extraction hints.
//   5. Writes a ComparisonDump JSON to logs/tests/experiments/.
//
// Baseline test runs with no custom agent (uses canonical domain-remember-agent).
//
// Adding a new experiment:
//   1. Add a new prompt constant in domain/agents/remember_experiments.go.
//   2. Add a TestExp_VN_* function here that calls s.runExperiment(...).
//   3. Run: task server:test:experiments -- -run TestRememberExperiments/TestExp_VN

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// ---------------------------------------------------------------------------
// Helpers (local)
// ---------------------------------------------------------------------------

func expStrPtr(s string) *string { return &s }

// ---------------------------------------------------------------------------
// Quality metrics
// ---------------------------------------------------------------------------

// QualityMetrics captures measurable schema-quality signals from one remember run.
type QualityMetrics struct {
	// Schema
	TypeCount            int      `json:"type_count"`
	TypesWithProperties  int      `json:"types_with_properties"`   // types where len(properties) > 0
	TotalProperties      int      `json:"total_properties"`        // sum across all types
	AvgPropertiesPerType float64  `json:"avg_properties_per_type"` // TotalProperties / TypeCount
	TypeNames            []string `json:"type_names"`
	HasExtractionHints   bool     `json:"has_extraction_hints"`    // extraction_prompts.typeHints non-empty
	RelationshipCount    int      `json:"relationship_count"`
	// SSE pipeline
	ToolsUsedList []string `json:"tools_used"`
	CalledFinalize bool    `json:"called_finalize"`
	CalledReextract bool   `json:"called_reextract"`
	// Cost / latency
	WallMillis   int64 `json:"wall_ms"`
	TokensInput  int   `json:"tokens_input"`
	TokensOutput int   `json:"tokens_output"`
	// IDs for cross-referencing
	SchemaID  string `json:"schema_id,omitempty"`
	RunID     string `json:"run_id,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
}

// ComparisonDump holds side-by-side results for two variants of the same fixture.
type ComparisonDump struct {
	Fixture   string         `json:"fixture"`
	Timestamp string         `json:"timestamp"`
	Baseline  *QualityMetrics `json:"baseline"`
	Improved  *QualityMetrics `json:"improved"`
	Diff      struct {
		TypeCountDelta         int     `json:"type_count_delta"`
		TypesWithPropertiesDelta int   `json:"types_with_properties_delta"`
		AvgPropsDelta          float64 `json:"avg_props_delta"`
		TokensInputDelta       int     `json:"tokens_input_delta"`
		WallMsDelta            int64   `json:"wall_ms_delta"`
	} `json:"diff"`
}

// ---------------------------------------------------------------------------
// Suite
// ---------------------------------------------------------------------------

type RememberExperimentsTestSuite struct {
	suite.Suite

	testDB    *testutil.TestDB
	inProcess *testutil.TestServer

	client    *testutil.HTTPClient
	ctx       context.Context
	projectID string
	orgID     string
	authToken string
}

func TestRememberExperiments(t *testing.T) {
	suite.Run(t, new(RememberExperimentsTestSuite))
}

func (s *RememberExperimentsTestSuite) SetupSuite() {
	s.ctx = context.Background()
	// Load .env / .env.local so POSTGRES_PORT and LLM keys are visible before SetupTestDB.
	testutil.LoadEnvFiles()
	testDB, err := testutil.SetupTestDB(s.ctx, "experiments")
	s.Require().NoError(err, "setup test db")
	s.testDB = testDB
	s.authToken = "e2e-test-user"
}

func (s *RememberExperimentsTestSuite) TearDownSuite() {
	if s.testDB != nil {
		s.testDB.Close()
	}
}

func (s *RememberExperimentsTestSuite) TearDownTest() {
	if s.inProcess != nil && s.inProcess.StopFn != nil {
		s.inProcess.StopFn()
	}
}

func (s *RememberExperimentsTestSuite) SetupTest() {
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

func (s *RememberExperimentsTestSuite) rememberURL() string {
	return fmt.Sprintf("/api/projects/%s/remember", s.projectID)
}

func (s *RememberExperimentsTestSuite) postRemember(body map[string]any) *testutil.SSEResponse {
	return s.client.PostSSE(
		s.rememberURL(),
		testutil.WithAuth(s.authToken),
		testutil.WithJSONBody(body),
	)
}

// newProject creates a fresh org+project for one experiment run, switches
// s.projectID/orgID to it, and returns a postRemember helper scoped to it.
// Every variant in a comparison gets its own project so there is zero state bleed.
func (s *RememberExperimentsTestSuite) newProject() {
	s.T().Helper()
	s.orgID = uuid.New().String()
	s.projectID = uuid.New().String()
	err := testutil.SetupFullTestProject(s.ctx, s.testDB.DB, s.orgID, s.projectID)
	s.Require().NoError(err, "create fresh project for experiment variant")
}

// skipIfNoLLM skips when no LLM provider is reachable.
func (s *RememberExperimentsTestSuite) skipIfNoLLM() {
	// Quick project to probe with — reuses current s.projectID.
	probe := s.postRemember(map[string]any{"message": "ping"})
	if probe.StatusCode == http.StatusServiceUnavailable || probe.StatusCode == http.StatusUnprocessableEntity {
		s.T().Skip("no LLM provider configured — skipping LLM-dependent experiment")
	}
	if probe.HasEvent("error") {
		s.T().Skip("LLM provider returned error on probe — skipping LLM-dependent experiment")
	}
}

// seedExperiment inserts a custom agent definition and sets it as the project's
// remember agent. The definition gets the provided system prompt and the same
// tool set as the canonical agent (finalize-discovery, queue-reextraction).
func (s *RememberExperimentsTestSuite) seedExperiment(name, systemPrompt string) {
	s.T().Helper()

	temp := float32(0.1)
	maxSteps := 30
	sp := systemPrompt

	def := &agents.AgentDefinition{
		ProjectID:    s.projectID,
		Name:         name,
		Description:  expStrPtr("Experiment agent: " + name),
		SystemPrompt: &sp,
		Model:        &agents.ModelConfig{Temperature: &temp},
		Tools:        []string{"finalize-discovery", "queue-reextraction"},
		Skills:       []string{},
		FlowType:     agents.FlowTypeSingle,
		IsDefault:    false,
		MaxSteps:     &maxSteps,
		Visibility:   agents.VisibilityProject,
		Config:       map[string]any{"experiment": name},
		ToolPolicies: map[string]agents.ToolPolicy{},
	}

	repo := agents.NewRepository(s.testDB.DB)
	err := repo.CreateDefinition(s.ctx, def)
	s.Require().NoError(err, "seed experiment agent definition")

	// Register as the project's remember agent.
	_, err = repo.UpsertProjectSetting(s.ctx, s.projectID,
		agents.SettingsCategoryRememberConfig,
		agents.SettingsKeyRememberAgentName,
		map[string]any{"name": name},
	)
	s.Require().NoError(err, "register experiment as remember agent")
}

// schemaIDFromFinalizeSSE extracts the schema UUID from the finalize-discovery
// mcp_tool completed SSE event. Returns "" if not found.
func schemaIDFromFinalizeSSE(rec *testutil.SSEResponse) string {
	for _, ev := range rec.GetEventsByType("mcp_tool") {
		var data map[string]any
		if err := ev.ParseSSEJSON(&data); err != nil {
			continue
		}
		tool, _ := data["tool"].(string)
		status, _ := data["status"].(string)
		if tool != "finalize-discovery" || status != "completed" {
			continue
		}
		// result.schemaId or result.schema_id
		if result, ok := data["result"].(map[string]any); ok {
			if id, ok := result["schemaId"].(string); ok && id != "" {
				return id
			}
			if id, ok := result["schema_id"].(string); ok && id != "" {
				return id
			}
			// Sometimes embedded in message string — skip, rely on DB query
		}
	}
	return ""
}

// querySchemaMetrics queries kb.graph_schemas for the most recently created
// schema in the project and extracts quality signals.
func (s *RememberExperimentsTestSuite) querySchemaMetrics(schemaID string) (typeCount, typesWithProps, totalProps int, typeNames []string, hasHints bool, relCount int) {
	if s.testDB == nil {
		return
	}

	// If schemaID not known from SSE, grab the latest one for the project.
	if schemaID == "" {
		err := s.testDB.DB.NewRaw(
			`SELECT gs.id FROM kb.graph_schemas gs
			 JOIN kb.project_schemas ps ON ps.schema_id = gs.id
			 WHERE ps.project_id = ? AND ps.removed_at IS NULL
			 ORDER BY ps.installed_at DESC LIMIT 1`,
			s.projectID,
		).Scan(s.ctx, &schemaID)
		if err != nil || schemaID == "" {
			return
		}
	}

	// Read object_type_schemas, relationship_type_schemas, extraction_prompts.
	// Cast to text in SQL so pgx delivers them as string (JSONB scan into []byte is unreliable).
	var rawTypes, rawRels, rawPrompts string
	err := s.testDB.DB.NewRaw(
		`SELECT object_type_schemas::text, relationship_type_schemas::text,
		        COALESCE(extraction_prompts::text, '{}')
		 FROM kb.graph_schemas WHERE id = ?`,
		schemaID,
	).Scan(s.ctx, &rawTypes, &rawRels, &rawPrompts)
	if err != nil {
		s.T().Logf("querySchemaMetrics: scan error: %v", err)
		return
	}

	// Count types and properties.
	var typeMap map[string]any
	if rawTypes != "" {
		_ = json.Unmarshal([]byte(rawTypes), &typeMap)
	}
	for typeName, v := range typeMap {
		typeCount++
		typeNames = append(typeNames, typeName)
		if schema, ok := v.(map[string]any); ok {
			if props, ok := schema["properties"].(map[string]any); ok && len(props) > 0 {
				typesWithProps++
				totalProps += len(props)
			}
		}
	}

	// Count relationships.
	var relMap map[string]any
	if rawRels != "" {
		_ = json.Unmarshal([]byte(rawRels), &relMap)
		relCount = len(relMap)
	}

	// Check extraction_prompts.
	if rawPrompts != "" && rawPrompts != "{}" {
		var prompts map[string]any
		if err := json.Unmarshal([]byte(rawPrompts), &prompts); err == nil {
			if hints, ok := prompts["typeHints"].(map[string]any); ok {
				hasHints = len(hints) > 0
			}
		}
	}

	return
}

// measureRun posts /remember and collects QualityMetrics.
func (s *RememberExperimentsTestSuite) measureRun(fixture, schemaPolicy string) *QualityMetrics {
	s.T().Helper()
	start := time.Now()

	rec := s.postRemember(map[string]any{
		"message":       novelTexts[fixture],
		"schema_policy": schemaPolicy,
	})

	wall := time.Since(start).Milliseconds()

	s.Require().Equal(http.StatusOK, rec.StatusCode,
		"remember HTTP status; body: %s", rec.RawBody)
	s.False(rec.HasEvent("error"), "unexpected error SSE: %s", rec.RawBody)
	s.True(rec.HasEvent("done"), "expected done SSE event")

	dumpSSE(s.T(), rec)

	m := &QualityMetrics{
		RunID:        runIDFromDone(rec),
		ProjectID:    s.projectID,
		WallMillis:   wall,
		ToolsUsedList: toolsUsed(rec),
		SchemaID:     schemaIDFromFinalizeSSE(rec),
	}

	for _, t := range m.ToolsUsedList {
		if t == "finalize-discovery" {
			m.CalledFinalize = true
		}
		if t == "queue-reextraction" {
			m.CalledReextract = true
		}
	}

	// Give extraction_prompts async LLM call time to complete (it's fire-and-forget).
	if m.CalledFinalize {
		time.Sleep(8 * time.Second)
	}

	tc, twp, tp, tnames, hints, rels := s.querySchemaMetrics(m.SchemaID)
	m.TypeCount = tc
	m.TypesWithProperties = twp
	m.TotalProperties = tp
	m.TypeNames = tnames
	m.HasExtractionHints = hints
	m.RelationshipCount = rels
	if tc > 0 {
		m.AvgPropertiesPerType = float64(tp) / float64(tc)
	}

	s.T().Logf("  ── quality metrics ────────────────────────────────")
	s.T().Logf("  types:              %d  (%d with properties)", m.TypeCount, m.TypesWithProperties)
	s.T().Logf("  total properties:   %d  (avg %.1f/type)", m.TotalProperties, m.AvgPropertiesPerType)
	s.T().Logf("  relationships:      %d", m.RelationshipCount)
	s.T().Logf("  extraction hints:   %v", m.HasExtractionHints)
	s.T().Logf("  type names:         %s", strings.Join(m.TypeNames, ", "))
	s.T().Logf("  wall time:          %dms", m.WallMillis)
	s.T().Logf("  tools used:         %s", strings.Join(m.ToolsUsedList, ", "))

	return m
}

// writeExperimentDump writes a ComparisonDump to logs/tests/experiments/.
func writeExperimentDump(t *testing.T, d *ComparisonDump) {
	t.Helper()

	// Repo root is 4 levels up from apps/server/tests/integration.
	repoRoot := filepath.Join("..", "..", "..", "..")
	dir := filepath.Join(repoRoot, "logs", "tests", "experiments")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Logf("writeExperimentDump: mkdir: %v", err)
		return
	}

	safeName := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == ' ' {
			return '_'
		}
		return r
	}, t.Name())

	filename := fmt.Sprintf("%s-%s.json", d.Timestamp, safeName)
	path := filepath.Join(dir, filename)

	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		t.Logf("writeExperimentDump: marshal: %v", err)
		return
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Logf("writeExperimentDump: write: %v", err)
		return
	}
	t.Logf("  experiment dump → %s", path)
}

// postRememberText posts /remember with raw text (not a novelTexts key).
func (s *RememberExperimentsTestSuite) postRememberText(text, schemaPolicy string) *testutil.SSEResponse {
	return s.client.PostSSE(
		s.rememberURL(),
		testutil.WithAuth(s.authToken),
		testutil.WithJSONBody(map[string]any{
			"message":       text,
			"schema_policy": schemaPolicy,
		}),
	)
}

// measureRunText is like measureRun but accepts raw text instead of a fixture key.
func (s *RememberExperimentsTestSuite) measureRunText(text, schemaPolicy string) *QualityMetrics {
	s.T().Helper()
	start := time.Now()
	rec := s.postRememberText(text, schemaPolicy)
	wall := time.Since(start).Milliseconds()

	s.Require().Equal(http.StatusOK, rec.StatusCode, "HTTP status; body: %s", rec.RawBody)
	s.False(rec.HasEvent("error"), "unexpected error SSE: %s", rec.RawBody)
	s.True(rec.HasEvent("done"), "expected done SSE event")

	dumpSSE(s.T(), rec)

	m := &QualityMetrics{
		RunID:         runIDFromDone(rec),
		ProjectID:     s.projectID,
		WallMillis:    wall,
		ToolsUsedList: toolsUsed(rec),
		SchemaID:      schemaIDFromFinalizeSSE(rec),
	}
	for _, t := range m.ToolsUsedList {
		if t == "finalize-discovery" {
			m.CalledFinalize = true
		}
		if t == "queue-reextraction" {
			m.CalledReextract = true
		}
	}
	if m.CalledFinalize {
		time.Sleep(8 * time.Second)
	}
	tc, twp, tp, tnames, hints, rels := s.querySchemaMetrics(m.SchemaID)
	m.TypeCount, m.TypesWithProperties, m.TotalProperties = tc, twp, tp
	m.TypeNames, m.HasExtractionHints, m.RelationshipCount = tnames, hints, rels
	if tc > 0 {
		m.AvgPropertiesPerType = float64(tp) / float64(tc)
	}
	s.T().Logf("  types=%d with_props=%d total_props=%d avg=%.1f rels=%d hints=%v wall=%dms",
		tc, twp, tp, m.AvgPropertiesPerType, rels, hints, wall)
	return m
}

// runExperimentText is like runExperiment but takes raw text instead of a novelTexts key.
// Each variant gets its own isolated project so there is zero state bleed.
func (s *RememberExperimentsTestSuite) runExperimentText(experimentName, promptConst, fixtureName, text string) (*QualityMetrics, *QualityMetrics) {
	s.T().Helper()
	s.skipIfNoLLM()

	// ── Baseline: fresh project, canonical agent ──
	s.T().Log("── BASELINE (fresh project) ──────────────────────────")
	s.newProject()
	baseline := s.measureRunText(text, "auto")

	// ── Improved: separate fresh project + experiment agent ──
	s.T().Logf("── IMPROVED (%s, fresh project) ──────────────────────", experimentName)
	s.newProject()
	s.seedExperiment(experimentName, promptConst)
	improved := s.measureRunText(text, "auto")

	cmp := &ComparisonDump{
		Fixture:   fixtureName,
		Timestamp: time.Now().Format("20060102-150405"),
		Baseline:  baseline,
		Improved:  improved,
	}
	if baseline != nil && improved != nil {
		cmp.Diff.TypeCountDelta = improved.TypeCount - baseline.TypeCount
		cmp.Diff.TypesWithPropertiesDelta = improved.TypesWithProperties - baseline.TypesWithProperties
		cmp.Diff.AvgPropsDelta = improved.AvgPropertiesPerType - baseline.AvgPropertiesPerType
		cmp.Diff.TokensInputDelta = improved.TokensInput - baseline.TokensInput
		cmp.Diff.WallMsDelta = improved.WallMillis - baseline.WallMillis
	}
	s.T().Logf("── DIFF ──────────────────────────────────────────────")
	s.T().Logf("  type_count:            baseline=%d  improved=%d  Δ=%+d", baseline.TypeCount, improved.TypeCount, cmp.Diff.TypeCountDelta)
	s.T().Logf("  types_with_properties: baseline=%d  improved=%d  Δ=%+d", baseline.TypesWithProperties, improved.TypesWithProperties, cmp.Diff.TypesWithPropertiesDelta)
	s.T().Logf("  avg_props/type:        baseline=%.1f  improved=%.1f  Δ=%+.1f", baseline.AvgPropertiesPerType, improved.AvgPropertiesPerType, cmp.Diff.AvgPropsDelta)
	s.T().Logf("  wall_ms:               baseline=%d  improved=%d  Δ=%+d", baseline.WallMillis, improved.WallMillis, cmp.Diff.WallMsDelta)
	writeExperimentDump(s.T(), cmp)
	return baseline, improved
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// runExperiment runs baseline then improved on the same fixture.
// Each variant gets its own isolated fresh project — no state bleed.
func (s *RememberExperimentsTestSuite) runExperiment(experimentName, promptConst, fixture string) (*QualityMetrics, *QualityMetrics) {
	s.T().Helper()
	s.skipIfNoLLM()

	_, ok := novelTexts[fixture]
	s.Require().True(ok, "unknown fixture %q — add to novelTexts map", fixture)

	// ── Baseline: fresh project, canonical agent ──────────────────────
	s.T().Log("── BASELINE (fresh project) ──────────────────────────")
	s.newProject()
	baseline := s.measureRun(fixture, "auto")

	// ── Improved: separate fresh project + experiment agent ───────────
	s.T().Logf("── IMPROVED (%s, fresh project) ──────────────────────", experimentName)
	s.newProject()
	s.seedExperiment(experimentName, promptConst)
	improved := s.measureRun(fixture, "auto")

	// ── Comparison dump ───────────────────────────────────────────────
	cmp := &ComparisonDump{
		Fixture:   fixture,
		Timestamp: time.Now().Format("20060102-150405"),
		Baseline:  baseline,
		Improved:  improved,
	}
	if baseline != nil && improved != nil {
		cmp.Diff.TypeCountDelta = improved.TypeCount - baseline.TypeCount
		cmp.Diff.TypesWithPropertiesDelta = improved.TypesWithProperties - baseline.TypesWithProperties
		cmp.Diff.AvgPropsDelta = improved.AvgPropertiesPerType - baseline.AvgPropertiesPerType
		cmp.Diff.TokensInputDelta = improved.TokensInput - baseline.TokensInput
		cmp.Diff.WallMsDelta = improved.WallMillis - baseline.WallMillis
	}

	s.T().Logf("── DIFF ──────────────────────────────────────────────")
	s.T().Logf("  type_count:            baseline=%d  improved=%d  Δ=%+d",
		baseline.TypeCount, improved.TypeCount, cmp.Diff.TypeCountDelta)
	s.T().Logf("  types_with_properties: baseline=%d  improved=%d  Δ=%+d",
		baseline.TypesWithProperties, improved.TypesWithProperties, cmp.Diff.TypesWithPropertiesDelta)
	s.T().Logf("  avg_props/type:        baseline=%.1f  improved=%.1f  Δ=%+.1f",
		baseline.AvgPropertiesPerType, improved.AvgPropertiesPerType, cmp.Diff.AvgPropsDelta)
	s.T().Logf("  wall_ms:               baseline=%d  improved=%d  Δ=%+d",
		baseline.WallMillis, improved.WallMillis, cmp.Diff.WallMsDelta)

	writeExperimentDump(s.T(), cmp)

	return baseline, improved
}

// ---------------------------------------------------------------------------
// Experiment tests
// ---------------------------------------------------------------------------

// TestExp_Baseline runs the canonical agent on all fixtures with no experiment
// seeded. Captures baseline QualityMetrics for reference.
func (s *RememberExperimentsTestSuite) TestExp_Baseline() {
	s.skipIfNoLLM()

	for fixture := range novelTexts {
		s.T().Run(fixture, func(t *testing.T) {
			m := s.measureRun(fixture, "auto")
			dump := &ComparisonDump{
				Fixture:   fixture,
				Timestamp: time.Now().Format("20060102-150405"),
				Baseline:  m,
			}
			writeExperimentDump(t, dump)

			// Baseline: schema must be created when agent calls finalize-discovery.
			// (It may call queue-reextraction instead if a schema already matched —
			// both are valid. We don't hard-assert here — this is observation only.)
			t.Logf("baseline fixture=%s types=%d props_per_type=%.1f",
				fixture, m.TypeCount, m.AvgPropertiesPerType)
		})
	}
}

// TestExp_V2Fields_Economy runs V2 (P0 field extraction) vs baseline on the
// economy fixture and asserts the improved schema has more properties.
func (s *RememberExperimentsTestSuite) TestExp_V2Fields_Economy() {
	baseline, improved := s.runExperiment(
		"domain-remember-agent-v2-fields",
		agents.RememberPromptV2Fields,
		"economy",
	)

	// Hard assertions: V2 must produce schemas with actual field definitions.
	if improved.CalledFinalize {
		s.GreaterOrEqual(improved.TypesWithProperties, 1,
			"V2: at least 1 type should have properties; got %d types, %d with props",
			improved.TypeCount, improved.TypesWithProperties)
		s.GreaterOrEqual(improved.TotalProperties, 3,
			"V2: at least 3 total properties expected; got %d", improved.TotalProperties)
	}

	// Soft comparison: log delta but don't fail if baseline also happened to get props.
	if baseline != nil {
		s.T().Logf("property improvement: baseline=%d props  improved=%d props",
			baseline.TotalProperties, improved.TotalProperties)
	}
}

// TestExp_V2Fields_AllFixtures runs V2 vs baseline across all four novel fixtures.
// Purely informational — no hard assertions beyond HTTP 200 + done event.
func (s *RememberExperimentsTestSuite) TestExp_V2Fields_AllFixtures() {
	s.skipIfNoLLM()

	for fixture := range novelTexts {
		fixture := fixture // capture
		s.T().Run(fixture, func(t *testing.T) {
			// Reset per-fixture: need clean project state each time.
			_, _ = s.testDB.DB.NewRaw(`DELETE FROM kb.project_schemas WHERE project_id = ?`, s.projectID).Exec(s.ctx)
			_, _ = s.testDB.DB.NewRaw(`DELETE FROM kb.graph_schemas WHERE project_id = ?`, s.projectID).Exec(s.ctx)
			_, _ = s.testDB.DB.NewRaw(`DELETE FROM kb.documents WHERE project_id = ?`, s.projectID).Exec(s.ctx)

			// Remove any previously seeded experiment so seedExperiment below is clean.
			_, _ = s.testDB.DB.NewRaw(
				`DELETE FROM kb.agent_definitions WHERE project_id = ? AND name != ?`,
				s.projectID, agents.CanonicalRememberAgentName,
			).Exec(s.ctx)
			_, _ = s.testDB.DB.NewRaw(
				`DELETE FROM kb.project_settings WHERE project_id = ? AND category = ?`,
				s.projectID, agents.SettingsCategoryRememberConfig,
			).Exec(s.ctx)

			_, improved := s.runExperiment(
				"domain-remember-agent-v2-fields",
				agents.RememberPromptV2Fields,
				fixture,
			)
			t.Logf("[%s] V2: types=%d props=%d avg=%.1f",
				fixture, improved.TypeCount, improved.TotalProperties, improved.AvgPropertiesPerType)
		})
	}
}

// ---------------------------------------------------------------------------
// Friends transcript helpers
// ---------------------------------------------------------------------------

var (
	friendsS01Once sync.Once
	friendsS01Raw  []byte
	friendsS01Err  error
)

// fetchFriendsS01 fetches the Friends season 1 JSON once per test binary run.
// Returns raw bytes or an error. All calls after the first reuse the cached result.
func fetchFriendsS01() ([]byte, error) {
	friendsS01Once.Do(func() {
		const url = "https://raw.githubusercontent.com/emorynlp/character-mining/master/json/friends_season_01.json"
		resp, err := http.Get(url) //nolint:noctx
		if err != nil {
			friendsS01Err = fmt.Errorf("fetch friends S01: %w", err)
			return
		}
		defer resp.Body.Close()
		friendsS01Raw, friendsS01Err = io.ReadAll(resp.Body)
	})
	return friendsS01Raw, friendsS01Err
}

// friendsTranscript returns up to maxLines of dialogue from season 1 episode
// episodeIdx (0-based), formatted as "Speaker: line\n". Scene directions and
// empty lines are skipped.
func friendsTranscript(episodeIdx, maxLines int) (string, error) {
	raw, err := fetchFriendsS01()
	if err != nil {
		return "", err
	}

	var data struct {
		Episodes []struct {
			Scenes []struct {
				Utterances []struct {
					Speakers            []string `json:"speakers"`
					Transcript          string   `json:"transcript"`
					TranscriptWithNote  string   `json:"transcript_with_note"`
				} `json:"utterances"`
			} `json:"scenes"`
		} `json:"episodes"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return "", fmt.Errorf("parse friends JSON: %w", err)
	}
	if episodeIdx >= len(data.Episodes) {
		return "", fmt.Errorf("episode index %d out of range (have %d)", episodeIdx, len(data.Episodes))
	}

	var sb strings.Builder
	count := 0
	for _, scene := range data.Episodes[episodeIdx].Scenes {
		for _, u := range scene.Utterances {
			text := strings.TrimSpace(u.Transcript)
			if text == "" {
				text = strings.TrimSpace(u.TranscriptWithNote)
			}
			if text == "" {
				continue
			}
			speaker := "?"
			if len(u.Speakers) > 0 && u.Speakers[0] != "" {
				speaker = u.Speakers[0]
			}
			// Skip pure scene directions (no real speaker)
			if speaker == "?" || speaker == "Scene Directions" {
				// Include bracketed stage directions as context but don't count them
				sb.WriteString("[" + text + "]\n")
				continue
			}
			sb.WriteString(speaker + ": " + text + "\n")
			count++
			if count >= maxLines {
				return sb.String(), nil
			}
		}
	}
	return sb.String(), nil
}

// friendsEpisodeInfoText returns a structured episode info document for the
// first count episodes of season 1: title, air date, director, writer, IMDB rating.
func friendsEpisodeInfoText(count int) (string, error) {
	raw, err := fetchFriendsS01()
	if err != nil {
		return "", err
	}

	// The episode info is in a separate dataset; parse what we can from the season file.
	// The season JSON has episode_id but not full metadata. Use hardcoded S01 info
	// which is public domain factual data (episode titles, air dates, directors, ratings).
	type episodeInfo struct {
		EpisodeNum int
		Title      string
		AirDate    string
		DirectedBy string
		WrittenBy  string
		IMDBRating float64
		Views      float64
	}
	// Friends S01 episode metadata — public factual records.
	episodes := []episodeInfo{
		{1, "The Pilot", "1994-09-22", "James Burrows", "David Crane & Marta Kauffman", 8.3, 21.5},
		{2, "The One with the Sonogram at the End", "1994-09-29", "James Burrows", "David Crane & Marta Kauffman", 8.1, 20.2},
		{3, "The One with the Thumb", "1994-10-06", "James Burrows", "Jeffrey Astrof & Mike Sikowitz", 8.0, 19.5},
		{4, "The One with George Stephanopoulos", "1994-10-13", "James Burrows", "Alexa Junge", 8.1, 19.7},
		{5, "The One with the East German Laundry Detergent", "1994-10-20", "Pamela Fryman", "Jeff Greenstein & Jeff Strauss", 8.0, 18.6},
		{6, "The One with the Butt", "1994-10-27", "Arlene Sanford", "Adam Chase & Ira Ungerleider", 7.8, 18.2},
		{7, "The One with the Blackout", "1994-11-03", "James Burrows", "Jeffrey Astrof & Mike Sikowitz", 8.5, 23.5},
	}

	// Validate we have enough — raw JSON parse just to check episode count
	var check struct {
		Episodes []struct{ EpisodeID string `json:"episode_id"` } `json:"episodes"`
	}
	_ = json.Unmarshal(raw, &check) // ignore parse error; episodes slice already populated

	if count > len(episodes) {
		count = len(episodes)
	}

	var sb strings.Builder
	sb.WriteString("Friends (TV Series) — Season 1 Episode Guide\n\n")
	for _, ep := range episodes[:count] {
		sb.WriteString(fmt.Sprintf("Episode %d: %s\n", ep.EpisodeNum, ep.Title))
		sb.WriteString(fmt.Sprintf("  Air date:   %s\n", ep.AirDate))
		sb.WriteString(fmt.Sprintf("  Directed by: %s\n", ep.DirectedBy))
		sb.WriteString(fmt.Sprintf("  Written by:  %s\n", ep.WrittenBy))
		sb.WriteString(fmt.Sprintf("  IMDB rating: %.1f/10\n", ep.IMDBRating))
		sb.WriteString(fmt.Sprintf("  US viewers:  %.1f million\n\n", ep.Views))
	}
	return sb.String(), nil
}

// ---------------------------------------------------------------------------
// Project info helper
// ---------------------------------------------------------------------------

// setProjectInfo sets kb.projects.project_info for the current test project.
// This is picked up by GetProjectInfo → generateExtractionPrompts kbPurpose.
func (s *RememberExperimentsTestSuite) setProjectInfo(info string) {
	s.T().Helper()
	_, err := s.testDB.DB.NewRaw(
		`UPDATE kb.projects SET project_info = ? WHERE id = ?`,
		info, s.projectID,
	).Exec(s.ctx)
	s.Require().NoError(err, "set project_info")
}

// avgHintLength queries extraction_prompts.typeHints and returns the average
// character length of all hint values. Returns 0 when no hints exist.
func (s *RememberExperimentsTestSuite) avgHintLength(schemaID string) float64 {
	if s.testDB == nil || schemaID == "" {
		return 0
	}
	var rawPrompts string
	err := s.testDB.DB.NewRaw(
		`SELECT COALESCE(extraction_prompts::text, '{}') FROM kb.graph_schemas WHERE id = ?`,
		schemaID,
	).Scan(s.ctx, &rawPrompts)
	if err != nil || rawPrompts == "" || rawPrompts == "{}" {
		return 0
	}
	var prompts struct {
		TypeHints map[string]string `json:"typeHints"`
	}
	if err := json.Unmarshal([]byte(rawPrompts), &prompts); err != nil || len(prompts.TypeHints) == 0 {
		return 0
	}
	total := 0
	for _, v := range prompts.TypeHints {
		total += len(v)
	}
	return float64(total) / float64(len(prompts.TypeHints))
}

// resetSchemas truncates project schema state so successive runs start clean.
func (s *RememberExperimentsTestSuite) resetSchemas() {
	_, _ = s.testDB.DB.NewRaw(`DELETE FROM kb.project_schemas WHERE project_id = ?`, s.projectID).Exec(s.ctx)
	_, _ = s.testDB.DB.NewRaw(`DELETE FROM kb.graph_schemas WHERE project_id = ?`, s.projectID).Exec(s.ctx)
	_, _ = s.testDB.DB.NewRaw(`DELETE FROM kb.documents WHERE project_id = ?`, s.projectID).Exec(s.ctx)
}

// clearExperiment removes a previously seeded experiment agent + setting.
func (s *RememberExperimentsTestSuite) clearExperiment() {
	_, _ = s.testDB.DB.NewRaw(
		`DELETE FROM kb.agent_definitions WHERE project_id = ? AND name != ?`,
		s.projectID, agents.CanonicalRememberAgentName,
	).Exec(s.ctx)
	_, _ = s.testDB.DB.NewRaw(
		`DELETE FROM kb.project_settings WHERE project_id = ? AND category = ?`,
		s.projectID, agents.SettingsCategoryRememberConfig,
	).Exec(s.ctx)
}

// ---------------------------------------------------------------------------
// V4 — kbPurpose grounding via project_info (server-side fix)
// ---------------------------------------------------------------------------

// TestExp_V4_KBPurpose_Friends tests whether setting project_info improves
// extraction hint quality. Uses the same agent prompt as V2 (P0 fields) but
// sets a rich project description so generateExtractionPrompts gets useful
// kbPurpose context instead of the pack name string.
//
// Variable isolated: project_info in kb.projects (nothing else changes).
// Control: V4 with no project_info set (gets hardcoded fallback).
// Treatment: V4 with Friends-specific project_info.
func (s *RememberExperimentsTestSuite) TestExp_V4_KBPurpose_Friends() {
	s.skipIfNoLLM()

	transcript, err := friendsTranscript(0, 30)
	if err != nil {
		s.T().Skipf("could not fetch Friends transcript: %v", err)
	}
	s.T().Logf("fixture length: %d chars, %d lines", len(transcript), strings.Count(transcript, "\n"))

	friendsProjectInfo := "Knowledge base for the Friends TV sitcom (1994–2004, NBC). " +
		"Tracks characters (Monica, Rachel, Ross, Chandler, Joey, Phoebe), their relationships, " +
		"dialogue, locations (Central Perk cafe, apartments), episodes, and storylines across 10 seasons."

	// ── Control: fresh project, V2 prompt, no project_info ──
	s.T().Log("── CONTROL (fresh project, generic kbPurpose) ────────")
	s.newProject()
	s.seedExperiment("domain-remember-agent-v4-control", agents.RememberPromptV4KBPurpose)
	controlRec := s.postRememberText(transcript, "auto")
	s.Require().Equal(http.StatusOK, controlRec.StatusCode)
	s.False(controlRec.HasEvent("error"))
	s.True(controlRec.HasEvent("done"))
	dumpSSE(s.T(), controlRec)

	time.Sleep(8 * time.Second) // wait for async extraction prompts

	controlSchemaID := schemaIDFromFinalizeSSE(controlRec)
	controlHintLen := s.avgHintLength(controlSchemaID)
	_, _, controlProps, _, controlHints, _ := s.querySchemaMetrics(controlSchemaID)
	s.T().Logf("control: schemaID=%s total_props=%d has_hints=%v avg_hint_len=%.0f",
		controlSchemaID, controlProps, controlHints, controlHintLen)

	// ── Treatment: fresh project, V2 prompt + Friends project_info ──
	s.T().Log("── TREATMENT (fresh project + Friends project_info) ──")
	s.newProject()
	s.setProjectInfo(friendsProjectInfo)
	s.seedExperiment("domain-remember-agent-v4-treatment", agents.RememberPromptV4KBPurpose)
	treatRec := s.postRememberText(transcript, "auto")
	s.Require().Equal(http.StatusOK, treatRec.StatusCode)
	s.False(treatRec.HasEvent("error"))
	s.True(treatRec.HasEvent("done"))
	dumpSSE(s.T(), treatRec)

	time.Sleep(8 * time.Second)

	treatSchemaID := schemaIDFromFinalizeSSE(treatRec)
	treatHintLen := s.avgHintLength(treatSchemaID)
	_, _, treatProps, _, treatHints, _ := s.querySchemaMetrics(treatSchemaID)
	s.T().Logf("treatment: schemaID=%s total_props=%d has_hints=%v avg_hint_len=%.0f",
		treatSchemaID, treatProps, treatHints, treatHintLen)

	s.T().Logf("── DIFF ──")
	s.T().Logf("  avg_hint_length: control=%.0f  treatment=%.0f  Δ=%.0f",
		controlHintLen, treatHintLen, treatHintLen-controlHintLen)
	s.T().Logf("  total_props:     control=%d  treatment=%d", controlProps, treatProps)

	cmp := &ComparisonDump{
		Fixture:   "friends_pilot_scene1",
		Timestamp: time.Now().Format("20060102-150405"),
		Baseline: &QualityMetrics{
			TotalProperties:    controlProps,
			HasExtractionHints: controlHints,
			SchemaID:           controlSchemaID,
		},
		Improved: &QualityMetrics{
			TotalProperties:    treatProps,
			HasExtractionHints: treatHints,
			SchemaID:           treatSchemaID,
		},
	}
	writeExperimentDump(s.T(), cmp)
}

// ---------------------------------------------------------------------------
// V5 — few-shot example in agent prompt
// ---------------------------------------------------------------------------

// TestExp_V5_FewShot_Friends tests whether a concrete worked example in the
// agent prompt raises property density vs V2 on a Friends transcript.
func (s *RememberExperimentsTestSuite) TestExp_V5_FewShot_Friends() {
	s.skipIfNoLLM()

	// Use episode 0 scenes 2+ (Rachel's entrance) — different slice from V4
	transcript, err := friendsTranscript(0, 50)
	if err != nil {
		s.T().Skipf("could not fetch Friends transcript: %v", err)
	}

	baseline, improved := s.runExperimentText(
		"domain-remember-agent-v5-fewshot",
		agents.RememberPromptV5FewShot,
		"friends_pilot_ep1",
		transcript,
	)

	if improved.CalledFinalize {
		s.GreaterOrEqual(improved.TypesWithProperties, 2,
			"V5 few-shot: at least 2 types should have properties; got %d/%d",
			improved.TypesWithProperties, improved.TypeCount)
		s.GreaterOrEqual(improved.AvgPropertiesPerType, 2.0,
			"V5 few-shot: avg properties/type should be ≥2; got %.1f", improved.AvgPropertiesPerType)
	}

	if baseline != nil {
		s.T().Logf("V5 vs V2: types_with_props baseline=%d improved=%d, avg_props baseline=%.1f improved=%.1f, rels baseline=%d improved=%d",
			baseline.TypesWithProperties, improved.TypesWithProperties,
			baseline.AvgPropertiesPerType, improved.AvgPropertiesPerType,
			baseline.RelationshipCount, improved.RelationshipCount)
	}
}

// ---------------------------------------------------------------------------
// V6 — two-step create+extend
// ---------------------------------------------------------------------------

// TestExp_V6_TwoStep_Friends tests whether splitting creation from enrichment
// (two finalize-discovery calls) increases property density vs a single call.
// Run 1: transcript → creates schema.
// Run 2: episode info (different structure, same show) → extends schema.
func (s *RememberExperimentsTestSuite) TestExp_V6_TwoStep_Friends() {
	s.skipIfNoLLM()

	transcript, err := friendsTranscript(0, 35)
	if err != nil {
		s.T().Skipf("could not fetch Friends transcript: %v", err)
	}
	epInfo, err := friendsEpisodeInfoText(5)
	if err != nil {
		s.T().Skipf("could not build episode info: %v", err)
	}

	s.seedExperiment("domain-remember-agent-v6-twostep", agents.RememberPromptV6TwoStep)

	// ── Run 1: transcript → should create schema (two calls: create then extend) ──
	s.T().Log("── RUN 1 (transcript, two-step create+extend) ────────")
	rec1 := s.postRememberText(transcript, "auto")
	s.Require().Equal(http.StatusOK, rec1.StatusCode)
	s.False(rec1.HasEvent("error"), "run1 error: %s", rec1.RawBody)
	s.True(rec1.HasEvent("done"))
	dumpSSE(s.T(), rec1)

	// Count finalize-discovery start events — V6 should call it twice.
	finalizeCount := 0
	for _, ev := range rec1.GetEventsByType("mcp_tool") {
		var d map[string]any
		if err := ev.ParseSSEJSON(&d); err == nil {
			if d["tool"] == "finalize-discovery" && d["status"] == "started" {
				finalizeCount++
			}
		}
	}
	s.T().Logf("run1 finalize-discovery calls: %d (expect 2 for two-step)", finalizeCount)

	time.Sleep(8 * time.Second)

	schemaID1 := schemaIDFromFinalizeSSE(rec1)
	tc1, twp1, tp1, tnames1, hints1, rels1 := s.querySchemaMetrics(schemaID1)
	s.T().Logf("after run1: types=%d with_props=%d total_props=%d rels=%d hints=%v names=%s",
		tc1, twp1, tp1, rels1, hints1, strings.Join(tnames1, ", "))

	// ── Run 2: episode info → should extend existing schema ──
	s.T().Log("── RUN 2 (episode info, expect extend) ───────────────")
	rec2 := s.postRememberText(epInfo, "auto")
	s.Require().Equal(http.StatusOK, rec2.StatusCode)
	s.False(rec2.HasEvent("error"), "run2 error: %s", rec2.RawBody)
	s.True(rec2.HasEvent("done"))
	dumpSSE(s.T(), rec2)

	tools2 := toolsUsed(rec2)
	s.T().Logf("run2 tools: %s", strings.Join(tools2, ", "))

	time.Sleep(5 * time.Second)

	tc2, twp2, tp2, tnames2, _, _ := s.querySchemaMetrics("")
	s.T().Logf("after run2: types=%d with_props=%d total_props=%d names=%s",
		tc2, twp2, tp2, strings.Join(tnames2, ", "))

	s.T().Logf("── DIFF run1→run2 ──")
	s.T().Logf("  type_count:    %d → %d  (Δ%+d)", tc1, tc2, tc2-tc1)
	s.T().Logf("  total_props:   %d → %d  (Δ%+d)", tp1, tp2, tp2-tp1)
	s.T().Logf("  finalize calls run1: %d", finalizeCount)

	// Comparison dump
	cmp := &ComparisonDump{
		Fixture:   "friends_v6_twostep",
		Timestamp: time.Now().Format("20060102-150405"),
		Baseline: &QualityMetrics{
			TypeCount: tc1, TypesWithProperties: twp1,
			TotalProperties: tp1, RelationshipCount: rels1,
			HasExtractionHints: hints1,
		},
		Improved: &QualityMetrics{
			TypeCount: tc2, TypesWithProperties: twp2,
			TotalProperties: tp2,
		},
	}
	cmp.Diff.TypeCountDelta = tc2 - tc1
	cmp.Diff.TypesWithPropertiesDelta = twp2 - twp1
	cmp.Diff.AvgPropsDelta = float64(tp2-tp1) / float64(max(tc2, 1))
	writeExperimentDump(s.T(), cmp)
}

// TestExp_V3Extend_Economy runs V3 (multi-document schema refinement) on the
// economy fixture twice: first creates schema, second should extend it.
func (s *RememberExperimentsTestSuite) TestExp_V3Extend_Economy() {
	s.skipIfNoLLM()

	s.seedExperiment("domain-remember-agent-v3-extend", agents.RememberPromptV3Extend)

	// First remember: should create schema.
	s.T().Log("── RUN 1 (create) ────────────────────────────────────")
	m1 := s.measureRun("economy", "auto")
	typeCountAfterRun1 := m1.TypeCount
	propsAfterRun1 := m1.TotalProperties
	s.T().Logf("after run1: types=%d props=%d", typeCountAfterRun1, propsAfterRun1)

	// Second remember with slightly different but related text (geography of same world).
	// Agent should see existing schema and either extend or reextract.
	s.T().Log("── RUN 2 (extend or reextract) ───────────────────────")
	rec2 := s.postRemember(map[string]any{
		"message": novelTexts["economy"] + "\n\nAdditional context: The Glint Exchange Board regulates all " +
			"OrbitalExchange trades and assigns GEB scores to brokers based on transaction volume and " +
			"dispute rate. Brokers with GEB scores above 750 qualify for the Platinum Tier, granting " +
			"access to the Dark Mesh trading network.",
		"schema_policy": "auto",
	})
	s.Require().Equal(http.StatusOK, rec2.StatusCode)
	s.False(rec2.HasEvent("error"))
	s.True(rec2.HasEvent("done"))
	dumpSSE(s.T(), rec2)

	tools2 := toolsUsed(rec2)
	s.T().Logf("run2 tools: %s", strings.Join(tools2, ", "))

	// Wait for any async extraction prompts.
	time.Sleep(5 * time.Second)

	tc2, twp2, tp2, tnames2, _, _ := s.querySchemaMetrics("")
	s.T().Logf("after run2: types=%d props=%d type_names=%s", tc2, tp2, strings.Join(tnames2, ", "))

	// Soft assertion: second run must not produce an error; other outcomes depend on LLM.
	_ = twp2
}

// ---------------------------------------------------------------------------
// V7 — best-of-both: few-shot + reason passthrough + project_info (server fix)
// ---------------------------------------------------------------------------

// TestExp_V7_BestOfBoth_Friends runs V7 vs baseline on Friends transcript.
// V7 = few-shot example + reason passthrough + Friends project_info.
// Each variant gets its own isolated fresh project.
func (s *RememberExperimentsTestSuite) TestExp_V7_BestOfBoth_Friends() {
	s.skipIfNoLLM()

	transcript, err := friendsTranscript(0, 40)
	if err != nil {
		s.T().Skipf("could not fetch Friends transcript: %v", err)
	}

	friendsInfo := "Knowledge base for the Friends TV sitcom (1994–2004, NBC). " +
		"Tracks characters (Monica, Rachel, Ross, Chandler, Joey, Phoebe), " +
		"their relationships, dialogue, locations, episodes, and storylines."

	// ── Baseline: fresh project, canonical agent, no project_info ──
	s.T().Log("── BASELINE (fresh project, canonical agent) ─────────")
	s.newProject()
	baseline := s.measureRunText(transcript, "auto")

	// ── Improved: fresh project, V7 prompt, Friends project_info ──
	s.T().Log("── IMPROVED (fresh project, V7 prompt + project_info) ─")
	s.newProject()
	s.setProjectInfo(friendsInfo)
	s.seedExperiment("domain-remember-agent-v7-bestofboth", agents.RememberPromptV7BestOfBoth)
	improved := s.measureRunText(transcript, "auto")

	// ── Diff ──
	cmp := &ComparisonDump{
		Fixture:   "friends_pilot_v7",
		Timestamp: time.Now().Format("20060102-150405"),
		Baseline:  baseline,
		Improved:  improved,
	}
	cmp.Diff.TypeCountDelta = improved.TypeCount - baseline.TypeCount
	cmp.Diff.TypesWithPropertiesDelta = improved.TypesWithProperties - baseline.TypesWithProperties
	cmp.Diff.AvgPropsDelta = improved.AvgPropertiesPerType - baseline.AvgPropertiesPerType
	cmp.Diff.WallMsDelta = improved.WallMillis - baseline.WallMillis

	s.T().Logf("── DIFF ──────────────────────────────────────────────")
	s.T().Logf("  type_count:    baseline=%d  improved=%d  Δ=%+d", baseline.TypeCount, improved.TypeCount, cmp.Diff.TypeCountDelta)
	s.T().Logf("  types_w_props: baseline=%d  improved=%d  Δ=%+d", baseline.TypesWithProperties, improved.TypesWithProperties, cmp.Diff.TypesWithPropertiesDelta)
	s.T().Logf("  avg_props:     baseline=%.1f  improved=%.1f  Δ=%+.1f", baseline.AvgPropertiesPerType, improved.AvgPropertiesPerType, cmp.Diff.AvgPropsDelta)
	s.T().Logf("  rels:          baseline=%d  improved=%d", baseline.RelationshipCount, improved.RelationshipCount)
	s.T().Logf("  hints:         baseline=%v  improved=%v", baseline.HasExtractionHints, improved.HasExtractionHints)
	writeExperimentDump(s.T(), cmp)

	if improved.CalledFinalize {
		s.GreaterOrEqual(improved.TypesWithProperties, 2,
			"V7: at least 2 types with properties; got %d/%d", improved.TypesWithProperties, improved.TypeCount)
		s.GreaterOrEqual(improved.AvgPropertiesPerType, 2.0,
			"V7: avg props/type ≥2; got %.1f", improved.AvgPropertiesPerType)
	}
}

// ---------------------------------------------------------------------------
// V6 improved — two-step with explicit schema tracking
// ---------------------------------------------------------------------------

// TestExp_V6_TwoStep_Friends_Tracked is an improved version of V6 that
// explicitly tracks the schema created in run1 and queries cumulative metrics
// (both schemas) after run2, giving a true picture of schema growth.
func (s *RememberExperimentsTestSuite) TestExp_V6_TwoStep_Friends_Tracked() {
	s.skipIfNoLLM()

	transcript, err := friendsTranscript(0, 35)
	if err != nil {
		s.T().Skipf("could not fetch Friends transcript: %v", err)
	}
	epInfo, err := friendsEpisodeInfoText(5)
	if err != nil {
		s.T().Skipf("could not build episode info: %v", err)
	}

	s.seedExperiment("domain-remember-agent-v6-tracked", agents.RememberPromptV6TwoStep)

	// ── Run 1: transcript ──
	s.T().Log("── RUN 1 (transcript) ────────────────────────────────")
	rec1 := s.postRememberText(transcript, "auto")
	s.Require().Equal(http.StatusOK, rec1.StatusCode)
	s.False(rec1.HasEvent("error"), "run1 error: %s", rec1.RawBody)
	s.True(rec1.HasEvent("done"))
	dumpSSE(s.T(), rec1)

	finCalls1 := countToolCalls(rec1, "finalize-discovery")
	s.T().Logf("run1 finalize-discovery calls: %d", finCalls1)

	time.Sleep(8 * time.Second)

	schemaID1 := schemaIDFromFinalizeSSE(rec1)
	tc1, twp1, tp1, tnames1, hints1, rels1 := s.querySchemaMetrics(schemaID1)
	s.T().Logf("run1: schema=%s types=%d with_props=%d total_props=%d rels=%d hints=%v names=%v",
		schemaID1, tc1, twp1, tp1, rels1, hints1, tnames1)

	// ── Run 2: episode info ──
	s.T().Log("── RUN 2 (episode info) ──────────────────────────────")
	rec2 := s.postRememberText(epInfo, "auto")
	s.Require().Equal(http.StatusOK, rec2.StatusCode)
	s.False(rec2.HasEvent("error"), "run2 error: %s", rec2.RawBody)
	s.True(rec2.HasEvent("done"))
	dumpSSE(s.T(), rec2)

	finCalls2 := countToolCalls(rec2, "finalize-discovery")
	s.T().Logf("run2 finalize-discovery calls: %d", finCalls2)

	time.Sleep(5 * time.Second)

	// Query run2 schema specifically (latest installed).
	schemaID2 := schemaIDFromFinalizeSSE(rec2)
	tc2, twp2, tp2, tnames2, hints2, rels2 := s.querySchemaMetrics(schemaID2)
	s.T().Logf("run2: schema=%s types=%d with_props=%d total_props=%d rels=%d hints=%v names=%v",
		schemaID2, tc2, twp2, tp2, rels2, hints2, tnames2)

	// Cumulative: count all installed schemas for this project.
	var totalSchemas int
	_ = s.testDB.DB.NewRaw(
		`SELECT COUNT(*) FROM kb.project_schemas WHERE project_id = ? AND removed_at IS NULL`,
		s.projectID,
	).Scan(s.ctx, &totalSchemas)

	s.T().Logf("── CUMULATIVE after 2 runs ───────────────────────────")
	s.T().Logf("  schemas installed:  %d", totalSchemas)
	s.T().Logf("  run1 types=%d props=%d  run2 types=%d props=%d", tc1, tp1, tc2, tp2)
	s.T().Logf("  finalize calls:    run1=%d run2=%d", finCalls1, finCalls2)

	cmp := &ComparisonDump{
		Fixture:   "friends_v6_tracked",
		Timestamp: time.Now().Format("20060102-150405"),
		Baseline: &QualityMetrics{
			TypeCount: tc1, TypesWithProperties: twp1,
			TotalProperties: tp1, RelationshipCount: rels1,
			HasExtractionHints: hints1,
		},
		Improved: &QualityMetrics{
			TypeCount: tc2, TypesWithProperties: twp2,
			TotalProperties: tp2, RelationshipCount: rels2,
			HasExtractionHints: hints2,
		},
	}
	cmp.Diff.TypeCountDelta = tc2 - tc1
	cmp.Diff.TypesWithPropertiesDelta = twp2 - twp1
	writeExperimentDump(s.T(), cmp)
}

// countToolCalls counts SSE mcp_tool "started" events for a given tool name.
func countToolCalls(rec *testutil.SSEResponse, toolName string) int {
	n := 0
	for _, ev := range rec.GetEventsByType("mcp_tool") {
		var d map[string]any
		if err := ev.ParseSSEJSON(&d); err == nil {
			if d["tool"] == toolName && d["status"] == "started" {
				n++
			}
		}
	}
	return n
}
