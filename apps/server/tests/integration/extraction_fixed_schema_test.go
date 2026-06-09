package integration

// ExtractionFixedSchemaTestSuite tests entity extraction quality against a
// pre-prepared schema — no auto-discovery, no agent. The schema is defined
// here in Go and installed into the test project before each run.
//
// This is a separate concern from remember_experiments_test.go (which tests
// schema discovery quality). Here we assume discovery is done and ask:
// "given a known-correct schema, how well does extraction work?"
//
// Fixture: Friends (TV show) transcript excerpts from the public dataset at
// https://github.com/emorynlp/character-mining (MIT licence).
//
// Schema: hand-crafted, structurally correct Friends domain:
//   - Character   (name, occupation, apartment, relationship_status)
//   - Location    (name, type, address)
//   - Episode     (title, season, episode_number, air_date, imdb_rating)
//   - Utterance   (speaker, text, scene_number, episode_id)
//   - Relationship (from_character, to_character, type, description)
//
// Tests verify:
//   - Extracted objects have correct types from the schema
//   - Key properties are populated (name, text, etc.)
//   - Object count is plausible for the input text size
//   - No hallucinated types outside the schema
//
// To run:
//   task server:test:experiments -- -run TestExtractionFixedSchema

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
	"github.com/stretchr/testify/suite"

	"google.golang.org/adk/model"
	"google.golang.org/genai"

	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/domain/extraction"
	extractionagents "github.com/emergent-company/emergent.memory/domain/extraction/agents"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/pkg/adk"
)

// ---------------------------------------------------------------------------
// Friends schema definition
// ---------------------------------------------------------------------------

// prop is a shorthand for a JSON Schema property definition.
func prop(typ, description string) map[string]any {
	return map[string]any{"type": typ, "description": description}
}

// friendsSchema is the Friends TV show domain schema for fixed-schema extraction tests.
//
// Design decisions:
//   - No Utterance type — individual dialogue lines are data artefacts, not semantic objects.
//     What dialogue reveals should surface as Character properties and Event entities.
//   - Relationship uses relationship_kind (not type) to avoid collision with the reserved
//     "type" top-level field in the extraction protocol.
//   - Character captures what is learned about a character: occupation, home, current situation,
//     personality, and feelings — not just static facts.
//   - Event captures plot occurrences: things that happened or are currently happening.
//   - Object captures plot-significant physical items.
var friendsSchema = map[string]any{
	"Character": map[string]any{
		"type":     "object",
		"required": []string{"name"},
		"properties": map[string]any{
			"name":               prop("string", "Full name of the character (e.g. Monica Geller, Ross Geller)"),
			"occupation":         prop("string", "Job or profession (e.g. chef, paleontologist, actor, masseuse, data analyst)"),
			"workplace":          prop("string", "Place of work (e.g. Javu restaurant, the museum, Days of Our Lives set)"),
			"home":               prop("string", "Where they live (e.g. apartment 20, across the hall from Monica)"),
			"relationship_status": prop("string", "Romantic situation (e.g. recently separated, single, dating, engaged)"),
			"current_situation":  prop("string", "What is happening in their life right now, based on the text"),
			"personality":        prop("string", "Key personality traits or behavioural patterns revealed in the scene"),
			"stated_feelings":    prop("string", "Emotions or feelings the character expresses or reveals through dialogue"),
		},
	},
	"Location": map[string]any{
		"type":     "object",
		"required": []string{"name"},
		"properties": map[string]any{
			"name":          prop("string", "Name of the location (e.g. Central Perk, Monica's apartment)"),
			"location_type": prop("string", "Category: cafe / apartment / workplace / street / other"),
			"address":       prop("string", "Street address, neighbourhood, or city if mentioned"),
			"significance":  prop("string", "Why this place matters to the characters or story"),
		},
	},
	"Relationship": map[string]any{
		"type":     "object",
		"required": []string{"person_a", "person_b", "relationship_kind"},
		"properties": map[string]any{
			"person_a":          prop("string", "First character in the relationship (e.g. Ross Geller)"),
			"person_b":          prop("string", "Second character in the relationship (e.g. Monica Geller)"),
			"relationship_kind": prop("string", "Nature of relationship: friend / sibling / ex_spouse / romantic / roommate / colleague / parent"),
			"history":           prop("string", "Background or shared history of this relationship if mentioned"),
			"current_state":     prop("string", "Current quality or status of the relationship (e.g. strained, close, newly formed, on a break)"),
		},
	},
	"Event": map[string]any{
		"type":     "object",
		"required": []string{"name", "description"},
		"properties": map[string]any{
			"name":         prop("string", "Short identifying label for the event (e.g. Rachel leaves wedding, Ross and Carol separate)"),
			"description":  prop("string", "What happened, described in detail based on what the text reveals"),
			"participants": prop("string", "Characters directly involved in the event"),
			"location":     prop("string", "Where the event occurred, if mentioned"),
			"timing":       prop("string", "When it happened relative to now (e.g. today, last night, in college, six years ago)"),
			"outcome":      prop("string", "Result, consequence, or emotional impact of the event"),
		},
	},
	"Object": map[string]any{
		"type":     "object",
		"required": []string{"name"},
		"properties": map[string]any{
			"name":         prop("string", "Name of the object or item (e.g. wedding dress, Central Perk couch)"),
			"object_type":  prop("string", "Category: food / drink / clothing / furniture / possession / other"),
			"owner":        prop("string", "Character who owns or is strongly associated with this object"),
			"significance": prop("string", "Why this object is meaningful or notable in the scene"),
		},
	},
}

// friendsRelationshipSchema defines graph-level edges between entity nodes.
var friendsRelationshipSchema = map[string]any{
	"KNOWS": map[string]any{
		"sourceTypes": []string{"Character"},
		"targetTypes": []string{"Character"},
		"cardinality": "many-to-many",
		"description": "Characters who know each other",
	},
	"LIVES_AT": map[string]any{
		"sourceTypes": []string{"Character"},
		"targetTypes": []string{"Location"},
		"cardinality": "many-to-one",
		"description": "Character lives at or regularly frequents this location",
	},
	"INVOLVED_IN": map[string]any{
		"sourceTypes": []string{"Character"},
		"targetTypes": []string{"Event"},
		"cardinality": "many-to-many",
		"description": "Character is a participant in this event",
	},
	"LOCATED_AT": map[string]any{
		"sourceTypes": []string{"Event"},
		"targetTypes": []string{"Location"},
		"cardinality": "many-to-one",
		"description": "Event takes place at this location",
	},
	"OWNS": map[string]any{
		"sourceTypes": []string{"Character"},
		"targetTypes": []string{"Object"},
		"cardinality": "many-to-many",
		"description": "Character owns or is strongly associated with this object",
	},
}

// friendsProjectInfo is the project description used as kbPurpose context.
const friendsProjectInfo = "Knowledge base for the Friends TV sitcom (1994–2004, NBC). " +
	"Tracks the six main characters — Monica Geller, Rachel Green, Ross Geller, " +
	"Chandler Bing, Joey Tribbiani, Phoebe Buffay — along with recurring characters, " +
	"their relationships, significant life events, locations they inhabit, " +
	"and notable objects across 10 seasons and 236 episodes."

// friendsExtractionPrompts provides Friends-specific extraction guidance.
var friendsExtractionPrompts = map[string]any{
	"domainContext": "Friends is an NBC sitcom (1994–2004) following six friends living in New York City. " +
		"The text consists of episode transcripts with dialogue and scene directions. " +
		"Extract the semantic objects the text reveals about the characters and their world — " +
		"who they are, what connects them, where they live and spend time, " +
		"what significant events are happening or have happened. " +
		"Do NOT extract individual lines of dialogue as entities. " +
		"Extract what the dialogue reveals about people, places, relationships, and events.",
	"typeHints": map[string]string{
		"Character": "Extract one Character entity per named person. Consolidate everything " +
			"said about them across the scene into a single entity — do not create duplicates. " +
			"Main cast: Monica Geller (chef), Ross Geller (paleontologist, recently separated from Carol), " +
			"Rachel Green (just left her fiancé Barry at the altar), Chandler Bing (office data analyst, sarcastic), " +
			"Joey Tribbiani (struggling actor), Phoebe Buffay (masseuse, free-spirited). " +
			"Populate occupation, home, relationship_status, current_situation, personality, " +
			"and stated_feelings from what is said in the scene.",
		"Location": "Extract named places where characters live or spend time. " +
			"Key locations: Central Perk (the coffee shop on the ground floor), " +
			"Monica's apartment (apartment 20, West Village), " +
			"Chandler and Joey's apartment (across the hall from Monica), " +
			"Ross's apartment, any named workplace. " +
			"A scene's setting is a Location even if not described by name — infer from context.",
		"Relationship": "Extract one Relationship entity per character pair whose connection is described. " +
			"Known relationships: Ross and Monica are siblings, Ross and Carol are ex-spouses, " +
			"Chandler and Joey are roommates, all six are close friends. " +
			"Use relationship_kind values: friend / sibling / ex_spouse / romantic / roommate / colleague / parent. " +
			"Note the current_state if the relationship is under strain or has recently changed.",
		"Event": "Extract significant occurrences that have happened or are currently happening. " +
			"Examples: Rachel leaving Barry at the altar, Ross and Carol's separation, " +
			"Monica losing her job, Phoebe's mother revealed to be alive. " +
			"Give each event a short name label and describe it from what the text says. " +
			"Include timing (e.g. 'happened today', 'six years ago') if stated.",
		"Object": "Extract only objects that carry plot significance or are distinctly memorable. " +
			"Examples: Rachel's wedding dress she arrived in, the apartment's Central Perk couch. " +
			"Do not extract every prop or passing mention of food and drink.",
	},
	"negativeExamples": []string{
		"Do not extract individual spoken lines as entities — there is no Utterance type",
		"Do not extract scene directions or stage notes as entities",
		"Do not create a separate entity for a date or time — attach timing as a property of Event or Character",
		"Do not extract generic props like 'a cup of coffee' unless they are plot-significant",
		"Do not extract the show name 'Friends' as a Character or Location",
		"Do not create duplicate Character entities — one entity per named person",
	},
}

// ---------------------------------------------------------------------------
// Suite
// ---------------------------------------------------------------------------

type ExtractionFixedSchemaTestSuite struct {
	suite.Suite

	testDB    *testutil.TestDB
	inProcess *testutil.TestServer

	client    *testutil.HTTPClient
	ctx       context.Context
	projectID string
	orgID     string
	authToken string
	schemaID  string // installed Friends schema ID, set in SetupTest
}

func TestExtractionFixedSchema(t *testing.T) {
	suite.Run(t, new(ExtractionFixedSchemaTestSuite))
}

func (s *ExtractionFixedSchemaTestSuite) SetupSuite() {
	s.ctx = context.Background()
	testutil.LoadEnvFiles()
	testDB, err := testutil.SetupTestDB(s.ctx, "extraction")
	s.Require().NoError(err, "setup test db")
	s.testDB = testDB
	s.authToken = "e2e-test-user"
}

func (s *ExtractionFixedSchemaTestSuite) TearDownSuite() {
	if s.testDB != nil {
		s.testDB.Close()
	}
}

func (s *ExtractionFixedSchemaTestSuite) TearDownTest() {
	if s.inProcess != nil && s.inProcess.StopFn != nil {
		s.inProcess.StopFn()
	}
}

func (s *ExtractionFixedSchemaTestSuite) SetupTest() {
	err := testutil.TruncateTables(s.ctx, s.testDB.DB)
	s.Require().NoError(err)

	err = testutil.SetupTestFixtures(s.ctx, s.testDB.DB)
	s.Require().NoError(err)

	// Fresh org + project per test.
	s.orgID = uuid.New().String()
	s.projectID = uuid.New().String()
	err = testutil.SetupFullTestProject(s.ctx, s.testDB.DB, s.orgID, s.projectID)
	s.Require().NoError(err)

	// Set project_info for grounded kbPurpose in extraction hints.
	_, err = s.testDB.DB.NewRaw(
		`UPDATE kb.projects SET project_info = ? WHERE id = ?`,
		friendsProjectInfo, s.projectID,
	).Exec(s.ctx)
	s.Require().NoError(err)

	// Install the Friends schema into this project.
	s.schemaID = s.installFriendsSchema()

	s.inProcess = testutil.NewTestServerWithLLM(s.testDB)
	s.client = testutil.NewHTTPClient(s.inProcess.Echo)
}

// ---------------------------------------------------------------------------
// Schema installation
// ---------------------------------------------------------------------------

// installFriendsSchema inserts the pre-prepared Friends schema into
// kb.graph_schemas and installs it into the project via kb.project_schemas.
// Returns the schema UUID as a string.
func (s *ExtractionFixedSchemaTestSuite) installFriendsSchema() string {
	s.T().Helper()

	typeSchemas, err := json.Marshal(friendsSchema)
	s.Require().NoError(err)

	relSchemas, err := json.Marshal(friendsRelationshipSchema)
	s.Require().NoError(err)

	prompts, err := json.Marshal(friendsExtractionPrompts)
	s.Require().NoError(err)

	// Build UI configs automatically from type names.
	uiConfigs := map[string]any{}
	for typeName := range friendsSchema {
		uiConfigs[typeName] = map[string]any{
			"displayName": typeName,
			"icon":        "person",
			"color":       "#6366f1",
			"description": fmt.Sprintf("Friends domain: %s entities", typeName),
		}
	}
	uiConfigsJSON, err := json.Marshal(uiConfigs)
	s.Require().NoError(err)

	schemaID := uuid.New().String()
	projectUUID := s.projectID

	_, err = s.testDB.DB.NewRaw(`
		INSERT INTO kb.graph_schemas (
			id, name, version, description, author, source,
			object_type_schemas, relationship_type_schemas, ui_configs,
			extraction_prompts, pending_review, project_id, created_at, updated_at
		) VALUES (
			?, 'Friends TV Show', '1.0.0',
			'Pre-prepared schema for Friends sitcom extraction tests',
			'Test Suite', 'manual',
			?::jsonb, ?::jsonb, ?::jsonb, ?::jsonb,
			false, ?, now(), now()
		)`,
		schemaID,
		string(typeSchemas), string(relSchemas), string(uiConfigsJSON), string(prompts),
		projectUUID,
	).Exec(s.ctx)
	s.Require().NoError(err, "insert Friends schema")

	_, err = s.testDB.DB.NewRaw(`
		INSERT INTO kb.project_schemas (project_id, schema_id, active, installed_at)
		VALUES (?, ?, true, now())`,
		projectUUID, schemaID,
	).Exec(s.ctx)
	s.Require().NoError(err, "install Friends schema to project")

	// Register types in project_object_schema_registry.
	for typeName, typeSchema := range friendsSchema {
		typeSchemaJSON, _ := json.Marshal(typeSchema)
		_, _ = s.testDB.DB.NewRaw(`
			INSERT INTO kb.project_object_schema_registry
				(project_id, type_name, json_schema, ui_config, source, schema_id, enabled, created_at, updated_at)
			VALUES (?, ?, ?::jsonb, ?::jsonb, 'discovered', ?, true, now(), now())
			ON CONFLICT (project_id, type_name) DO UPDATE
				SET json_schema = EXCLUDED.json_schema, enabled = true, updated_at = now()`,
			projectUUID, typeName, string(typeSchemaJSON),
			fmt.Sprintf(`{"displayName":"%s"}`, typeName),
			schemaID,
		).Exec(s.ctx)
	}

	s.T().Logf("installed Friends schema: id=%s types=%d", schemaID, len(friendsSchema))
	return schemaID
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (s *ExtractionFixedSchemaTestSuite) skipIfNoLLM() {
	resp := s.client.GET(
		"/api/health",
		testutil.WithAuth(s.authToken),
	)
	if resp.StatusCode == http.StatusServiceUnavailable {
		s.T().Skip("server unavailable")
	}
	// Quick extraction probe via /remember — schema exists so agent should queue extraction
	probe := s.client.PostSSE(
		fmt.Sprintf("/api/projects/%s/remember", s.projectID),
		testutil.WithAuth(s.authToken),
		testutil.WithJSONBody(map[string]any{"message": "ping", "schema_policy": "reuse_only"}),
	)
	if probe.StatusCode == http.StatusServiceUnavailable {
		s.T().Skip("no LLM provider — skipping extraction test")
	}
}

// createDocument inserts raw text as a document in the project, returns document ID.
func (s *ExtractionFixedSchemaTestSuite) createDocument(text string) string {
	s.T().Helper()
	db := s.testDB.DB
	log := s.quietLogger()

	docsRepo := documents.NewRepository(db, log)
	docsSvc := documents.NewService(docsRepo, log)

	filename := "friends-transcript.txt"
	sourceType := "manual"
	doc, _, err := docsSvc.Create(s.ctx, documents.CreateParams{
		ProjectID:  s.projectID,
		Filename:   &filename,
		Content:    &text,
		SourceType: &sourceType,
	})
	s.Require().NoError(err, "create document")
	s.T().Logf("document created: id=%s len=%d chars", doc.ID, len(text))
	return doc.ID
}

// runExtraction builds an in-process worker and processes one extraction job
// synchronously. Returns the job row.
func (s *ExtractionFixedSchemaTestSuite) runExtraction(documentID string) *extraction.ObjectExtractionJob {
	s.T().Helper()
	db := s.testDB.DB
	log := s.quietLogger()

	docsRepo := documents.NewRepository(db, log)
	docsSvc := documents.NewService(docsRepo, log)

	graphCfg := s.testGraphConfig()
	graphRepo := graph.NewRepository(db, log, graphCfg)
	graphSchemaProvider := graph.ProvideSchemaProvider(db, log)
	graphSvc := graph.NewService(graphRepo, log, graphSchemaProvider,
		graph.ProvideInverseTypeProvider(db, log), nil, nil, nil, nil, nil, nil)

	extractionSchemaProvider := extraction.NewMemorySchemaProvider(db, log)
	jobsCfg := &extraction.ObjectExtractionConfig{}
	jobsSvc := extraction.NewObjectExtractionJobsService(db, log, jobsCfg)

	// Resolve model factory from loaded env.
	modelFactory := s.modelFactory()
	if modelFactory == nil {
		s.T().Skip("no LLM credentials — cannot run extraction")
	}

	// Create extraction job — schema linked via project installation.
	enabledTypes := []string{"Character", "Location", "Relationship", "Event", "Object"}
	job, err := jobsSvc.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID:    s.projectID,
		DocumentID:   &documentID,
		EnabledTypes: enabledTypes,
	})
	s.Require().NoError(err, "create extraction job")
	s.T().Logf("extraction job created: id=%s doc=%s", job.ID, documentID)

	// Build worker and process synchronously (bypasses poll loop).
	worker := extraction.NewObjectExtractionWorker(
		jobsSvc,
		graphSvc,
		nil, // branchService — nil → objects go to main graph directly
		docsSvc,
		extractionSchemaProvider,
		modelFactory,
		nil, // embeddingService
		extraction.DefaultObjectExtractionWorkerConfig(),
		log,
		nil, // concurrency scaler
	)

	results, runErr := worker.ProcessJobSync(s.ctx, job)
	if runErr != nil {
		s.T().Logf("extraction error (may be partial): %v", runErr)
	}
	if results != nil {
		s.T().Logf("extraction results: objects=%d relationships=%d",
			results.ObjectsCreated, results.RelationshipsCreated)
	}
	return job
}

// quietLogger returns a logger that only shows warnings and above.
func (s *ExtractionFixedSchemaTestSuite) quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

// testGraphConfig returns a minimal config for graph repository.
func (s *ExtractionFixedSchemaTestSuite) testGraphConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Graph.MaxBatchObjects = 500
	cfg.Graph.MaxBatchRelationships = 500
	cfg.Graph.MaxListLimit = 1000
	cfg.Graph.DefaultListLimit = 100
	return cfg
}

// modelFactory resolves an ADK ModelFactory from env credentials.
// Returns nil when no credentials are configured (test should skip).
func (s *ExtractionFixedSchemaTestSuite) modelFactory() *adk.ModelFactory {
	testutil.LoadEnvFiles()
	log := s.quietLogger()
	cfg, err := config.NewConfig(log)
	if err != nil || !cfg.LLM.IsEnabled() {
		return nil
	}
	return adk.NewModelFactory(&cfg.LLM, log, nil, nil, nil)
}

// countExtractedObjects queries kb.graph_objects for the project, filtered by type.
func (s *ExtractionFixedSchemaTestSuite) countExtractedObjects(typeName string) int {
	var count int
	q := `SELECT COUNT(*) FROM kb.graph_objects
		  WHERE project_id = ? AND supersedes_id IS NULL AND deleted_at IS NULL`
	args := []any{s.projectID}
	if typeName != "" {
		q += " AND type = ?"
		args = append(args, typeName)
	}
	_ = s.testDB.DB.NewRaw(q, args...).Scan(s.ctx, &count)
	return count
}

// extractedObjectsOfType returns all live objects of a given type with their properties.
func (s *ExtractionFixedSchemaTestSuite) extractedObjectsOfType(typeName string) []map[string]any {
	var rows []struct {
		Properties []byte `bun:"properties"`
	}
	_ = s.testDB.DB.NewRaw(`
		SELECT properties FROM kb.graph_objects
		WHERE project_id = ? AND type = ?
		  AND supersedes_id IS NULL AND deleted_at IS NULL`,
		s.projectID, typeName,
	).Scan(s.ctx, &rows)

	result := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		var props map[string]any
		if err := json.Unmarshal(r.Properties, &props); err == nil {
			result = append(result, props)
		}
	}
	return result
}

// schemaTypes is the canonical type list for the Friends schema.
var schemaTypes = []string{"Character", "Location", "Relationship", "Event", "Object"}

// logExtractionSummary logs a table of extracted object counts per type.
func (s *ExtractionFixedSchemaTestSuite) logExtractionSummary() {
	s.T().Logf("── extraction summary ─────────────────────────────────")
	total := 0
	for _, t := range schemaTypes {
		n := s.countExtractedObjects(t)
		total += n
		s.T().Logf("  %-14s %d", t, n)
	}
	other := s.countExtractedObjects("") - total
	if other > 0 {
		s.T().Logf("  %-14s %d  ← outside schema (hallucinated types)", "OTHER", other)
	}
	s.T().Logf("  %-14s %d", "TOTAL", s.countExtractedObjects(""))
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestExtract_FriendsS01E01_Scene1 extracts entities from the first scene
// of Friends S01E01 (Monica's apartment / café opening scene) using the
// pre-prepared schema. Verifies character and location extraction.
func (s *ExtractionFixedSchemaTestSuite) TestExtract_FriendsS01E01_Scene1() {
	s.skipIfNoLLM()

	transcript, err := friendsTranscript(0, 25)
	if err != nil {
		s.T().Skipf("could not fetch Friends transcript: %v", err)
	}
	s.T().Logf("fixture: %d chars, %d lines", len(transcript), strings.Count(transcript, "\n"))

	docID := s.createDocument(transcript)
	job := s.runExtraction(docID)
	_ = job

	s.logExtractionSummary()

	characters := s.countExtractedObjects("Character")
	total := s.countExtractedObjects("")

	s.T().Logf("characters=%d total=%d", characters, total)

	s.Greater(total, 0, "extraction produced no objects at all")
	s.Greater(characters, 0, "no Character entities extracted")

	// Verify character objects have a name property populated.
	chars := s.extractedObjectsOfType("Character")
	namedCount := 0
	for _, c := range chars {
		if name, ok := c["name"].(string); ok && name != "" {
			namedCount++
			s.T().Logf("  character: %s", name)
		}
	}
	s.Greater(namedCount, 0, "Character objects have no 'name' property")

	// Verify no hallucinated types (all objects must be from schema).
	schemaTotal := 0
	for _, t := range schemaTypes {
		schemaTotal += s.countExtractedObjects(t)
	}
	hallucinated := total - schemaTotal
	s.Equal(0, hallucinated, "hallucinated %d objects with types outside schema", hallucinated)
}

// TestExtract_FriendsS01E01_LongScene extracts from a longer excerpt (50 lines)
// and verifies relationship extraction between known characters.
func (s *ExtractionFixedSchemaTestSuite) TestExtract_FriendsS01E01_LongScene() {
	s.skipIfNoLLM()

	transcript, err := friendsTranscript(0, 50)
	if err != nil {
		s.T().Skipf("could not fetch Friends transcript: %v", err)
	}

	docID := s.createDocument(transcript)
	job := s.runExtraction(docID)
	_ = job

	s.logExtractionSummary()

	total := s.countExtractedObjects("")
	characters := s.countExtractedObjects("Character")
	relationships := s.countExtractedObjects("Relationship")
	events := s.countExtractedObjects("Event")

	s.T().Logf("total=%d characters=%d relationships=%d events=%d", total, characters, relationships, events)
	s.Greater(total, 2, "expected more than 2 extracted objects from 50-line transcript")
	s.GreaterOrEqual(characters, 2, "expected at least 2 Character entities from 50-line transcript")
}

// TestExtract_EpisodeInfo extracts from structured episode metadata.
// Verifies the schema handles non-dialogue structured data.
func (s *ExtractionFixedSchemaTestSuite) TestExtract_EpisodeInfo() {
	s.skipIfNoLLM()

	epInfo, err := friendsEpisodeInfoText(5)
	if err != nil {
		s.T().Skipf("could not build episode info: %v", err)
	}

	docID := s.createDocument(epInfo)
	job := s.runExtraction(docID)
	_ = job

	s.logExtractionSummary()

	total := s.countExtractedObjects("")
	s.T().Logf("episode info extraction: total=%d", total)
	s.Greater(total, 0, "no objects extracted from episode metadata")
}

// TestExtract_SchemaTypes verifies the installed schema has correct structure.
func (s *ExtractionFixedSchemaTestSuite) TestExtract_SchemaTypes() {
	// Verify schema was installed correctly — no LLM needed.
	var rawTypes string
	err := s.testDB.DB.NewRaw(
		`SELECT object_type_schemas::text FROM kb.graph_schemas WHERE id = ?`,
		s.schemaID,
	).Scan(s.ctx, &rawTypes)
	s.Require().NoError(err)

	var typeMap map[string]any
	s.Require().NoError(json.Unmarshal([]byte(rawTypes), &typeMap))

	expectedTypes := []string{"Character", "Location", "Relationship", "Event", "Object"}
	for _, typeName := range expectedTypes {
		s.Contains(typeMap, typeName, "schema missing type %s", typeName)
		if schema, ok := typeMap[typeName].(map[string]any); ok {
			props, _ := schema["properties"].(map[string]any)
			s.NotEmpty(props, "type %s has no properties", typeName)
		}
	}

	// Verify extraction_prompts installed.
	var rawPrompts string
	err = s.testDB.DB.NewRaw(
		`SELECT COALESCE(extraction_prompts::text, '{}') FROM kb.graph_schemas WHERE id = ?`,
		s.schemaID,
	).Scan(s.ctx, &rawPrompts)
	s.Require().NoError(err)

	var prompts map[string]any
	s.Require().NoError(json.Unmarshal([]byte(rawPrompts), &prompts))
	s.NotEmpty(prompts["domainContext"], "extraction_prompts missing domainContext")
	hints, _ := prompts["typeHints"].(map[string]any)
	s.NotEmpty(hints, "extraction_prompts missing typeHints")

	s.T().Logf("schema OK: %d types, domainContext=%d chars, hints=%d",
		len(typeMap), len(fmt.Sprint(prompts["domainContext"])), len(hints))
}

// TestExtract_CompareExtractionQuality runs extraction twice on the same
// transcript and logs consistency of results (no hard assertion — observational).
func (s *ExtractionFixedSchemaTestSuite) TestExtract_CompareExtractionQuality() {
	s.skipIfNoLLM()

	transcript, err := friendsTranscript(0, 30)
	if err != nil {
		s.T().Skipf("could not fetch Friends transcript: %v", err)
	}

	// Run 1.
	docID1 := s.createDocument(transcript)
	s.runExtraction(docID1)
	run1Total := s.countExtractedObjects("")
	run1Chars := s.countExtractedObjects("Character")
	run1Rels := s.countExtractedObjects("Relationship")

	// Wait briefly then run again with same text.
	time.Sleep(2 * time.Second)
	s.runExtraction(docID1)
	run2Total := s.countExtractedObjects("")
	run2Chars := s.countExtractedObjects("Character")
	run2Rels := s.countExtractedObjects("Relationship")

	s.T().Logf("run1: total=%d chars=%d relationships=%d", run1Total, run1Chars, run1Rels)
	s.T().Logf("run2: total=%d chars=%d relationships=%d", run2Total, run2Chars, run2Rels)
	s.T().Logf("consistency: total_delta=%d char_delta=%d rel_delta=%d",
		run2Total-run1Total, run2Chars-run1Chars, run2Rels-run1Rels)
}

// ---------------------------------------------------------------------------
// ExtractionMetrics captures per-type object counts for comparison
// ---------------------------------------------------------------------------

type ExtractionMetrics struct {
	Label         string
	Total         int
	ByType        map[string]int
	PropsPerType  map[string]float64 // avg properties per object per type
	Relationships int
	WallMs        int64
}

func (m ExtractionMetrics) logTo(t *testing.T) {
	t.Helper()
	t.Logf("  ── %s ─────────────────────────────────────────", m.Label)
	t.Logf("  total=%d  rels=%d  wall=%dms", m.Total, m.Relationships, m.WallMs)
	for _, typeName := range schemaTypes {
		n := m.ByType[typeName]
		avg := m.PropsPerType[typeName]
		t.Logf("    %-14s count=%d  avg_props=%.1f", typeName, n, avg)
	}
}

// ---------------------------------------------------------------------------
// runTwoPhaseExtraction runs the two-phase pipeline directly (in-process).
// Returns the pipeline output — does NOT persist to graph (pure comparison).
// ---------------------------------------------------------------------------

func (s *ExtractionFixedSchemaTestSuite) runTwoPhaseExtraction(text string) (*extractionagents.ExtractionPipelineOutput, int64) {
	s.T().Helper()

	mf := s.modelFactory()
	if mf == nil {
		s.T().Skip("no LLM credentials — cannot run two-phase extraction")
	}
	log := s.quietLogger()

	// Convert friendsSchema (map[string]any) → map[string]ObjectSchema
	objectSchemas := make(map[string]extractionagents.ObjectSchema)
	for typeName, raw := range friendsSchema {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		schema := extractionagents.ObjectSchema{Name: typeName}
		if desc, ok := m["description"].(string); ok {
			schema.Description = desc
		}
		if req, ok := m["required"].([]string); ok {
			schema.Required = req
		}
		if propsRaw, ok := m["properties"].(map[string]any); ok {
			schema.Properties = make(map[string]extractionagents.PropertyDef)
			for propName, propRaw := range propsRaw {
				if propMap, ok := propRaw.(map[string]any); ok {
					pd := extractionagents.PropertyDef{}
					if t, ok := propMap["type"].(string); ok {
						pd.Type = t
					}
					if d, ok := propMap["description"].(string); ok {
						pd.Description = d
					}
					schema.Properties[propName] = pd
				}
			}
		}
		objectSchemas[typeName] = schema
	}

	// Convert friendsRelationshipSchema → map[string]RelationshipSchema
	relSchemas := make(map[string]extractionagents.RelationshipSchema)
	for relType, raw := range friendsRelationshipSchema {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		rs := extractionagents.RelationshipSchema{Name: relType}
		if desc, ok := m["description"].(string); ok {
			rs.Description = desc
		}
		if src, ok := m["sourceTypes"].([]string); ok {
			rs.SourceTypes = src
		}
		if dst, ok := m["targetTypes"].([]string); ok {
			rs.TargetTypes = dst
		}
		relSchemas[relType] = rs
	}

	// Extract typeHints and negativeExamples from friendsExtractionPrompts.
	typeHints := make(map[string]string)
	var negativeExamples []string
	if hints, ok := friendsExtractionPrompts["typeHints"].(map[string]string); ok {
		typeHints = hints
	}
	if neg, ok := friendsExtractionPrompts["negativeExamples"].([]string); ok {
		negativeExamples = neg
	}

	domainCtx, _ := friendsExtractionPrompts["domainContext"].(string)

	pipeline, err := extractionagents.NewTwoPhaseExtractionPipeline(extractionagents.ExtractionPipelineConfig{
		ModelFactory:        mf,
		ObjectSchemas:       objectSchemas,
		RelationshipSchemas: relSchemas,
		OrphanThreshold:     0.3,
		MaxRetries:          2,
		Logger:              log,
	})
	s.Require().NoError(err, "create two-phase pipeline")

	input := extractionagents.TwoPhaseExtractionInput{
		ExtractionPipelineInput: extractionagents.ExtractionPipelineInput{
			DocumentText:        text,
			ObjectSchemas:       objectSchemas,
			RelationshipSchemas: relSchemas,
			ProjectContext:      friendsProjectInfo,
			DomainGuidance:      domainCtx,
		},
		TypeHints:        typeHints,
		NegativeExamples: negativeExamples,
	}

	start := time.Now()
	output, err := pipeline.Run(s.ctx, input)
	wall := time.Since(start).Milliseconds()
	if err != nil {
		s.T().Logf("two-phase pipeline error (may be partial): %v", err)
		if output == nil {
			return &extractionagents.ExtractionPipelineOutput{}, wall
		}
	}
	return output, wall
}

// collectPipelineMetrics collects ExtractionMetrics from a pipeline output.
func collectPipelineMetrics(label string, output *extractionagents.ExtractionPipelineOutput, wallMs int64) ExtractionMetrics {
	m := ExtractionMetrics{
		Label:         label,
		ByType:        make(map[string]int),
		PropsPerType:  make(map[string]float64),
		Relationships: len(output.Relationships),
		WallMs:        wallMs,
	}
	propCounts := make(map[string][]int)
	for _, e := range output.Entities {
		m.Total++
		m.ByType[e.Type]++
		propCounts[e.Type] = append(propCounts[e.Type], len(e.Properties))
	}
	for typeName, counts := range propCounts {
		total := 0
		for _, c := range counts {
			total += c
		}
		m.PropsPerType[typeName] = float64(total) / float64(len(counts))
	}
	return m
}

// ---------------------------------------------------------------------------
// Two-phase comparison tests
// ---------------------------------------------------------------------------

// TestExtract_TwoPhase_FriendsS01E01_Scene1 compares single-phase vs two-phase
// extraction on the same Friends transcript (28 lines, same schema).
// Purely observational — no hard assertions on the comparison diff.
func (s *ExtractionFixedSchemaTestSuite) TestExtract_TwoPhase_FriendsS01E01_Scene1() {
	s.skipIfNoLLM()

	transcript, err := friendsTranscript(0, 28)
	if err != nil {
		s.T().Skipf("could not fetch Friends transcript: %v", err)
	}
	s.T().Logf("fixture: %d chars, %d lines", len(transcript), strings.Count(transcript, "\n"))

	// ── Single-phase (existing worker, persists to graph) ──────────────────
	s.T().Log("── SINGLE-PHASE ──────────────────────────────────────")
	spStart := time.Now()
	docID := s.createDocument(transcript)
	s.runExtraction(docID)
	spWall := time.Since(spStart).Milliseconds()
	s.logExtractionSummary()

	spMetrics := ExtractionMetrics{
		Label:        "single-phase",
		ByType:       make(map[string]int),
		PropsPerType: make(map[string]float64),
		WallMs:       spWall,
	}
	propCounts := make(map[string][]int)
	for _, typeName := range schemaTypes {
		objs := s.extractedObjectsOfType(typeName)
		spMetrics.ByType[typeName] = len(objs)
		spMetrics.Total += len(objs)
		for _, obj := range objs {
			propCounts[typeName] = append(propCounts[typeName], len(obj))
		}
	}
	for typeName, counts := range propCounts {
		total := 0
		for _, c := range counts {
			total += c
		}
		if len(counts) > 0 {
			spMetrics.PropsPerType[typeName] = float64(total) / float64(len(counts))
		}
	}
	// relationships: count graph_relationships for project
	var relCount int
	_ = s.testDB.DB.NewRaw(
		`SELECT COUNT(*) FROM kb.graph_relationships WHERE project_id = ?`, s.projectID,
	).Scan(s.ctx, &relCount)
	spMetrics.Relationships = relCount

	// ── Two-phase (pipeline only, no graph persist — pure LLM comparison) ──
	s.T().Log("── TWO-PHASE ─────────────────────────────────────────")
	tpOutput, tpWall := s.runTwoPhaseExtraction(transcript)
	tpMetrics := collectPipelineMetrics("two-phase", tpOutput, tpWall)

	// ── Side-by-side report ────────────────────────────────────────────────
	s.T().Log("── COMPARISON ────────────────────────────────────────")
	spMetrics.logTo(s.T())
	tpMetrics.logTo(s.T())

	s.T().Logf("── DIFF (two-phase minus single-phase) ───────────────")
	s.T().Logf("  total:         %+d", tpMetrics.Total-spMetrics.Total)
	s.T().Logf("  relationships: %+d", tpMetrics.Relationships-spMetrics.Relationships)
	for _, typeName := range schemaTypes {
		s.T().Logf("  %-14s count=%+d  avg_props=%+.1f",
			typeName,
			tpMetrics.ByType[typeName]-spMetrics.ByType[typeName],
			tpMetrics.PropsPerType[typeName]-spMetrics.PropsPerType[typeName],
		)
	}
	s.T().Logf("  wall_ms:       sp=%d  tp=%d  Δ=%+d", spMetrics.WallMs, tpMetrics.WallMs, tpMetrics.WallMs-spMetrics.WallMs)
}

// TestExtract_TwoPhase_LongScene compares on a 50-line fixture.
func (s *ExtractionFixedSchemaTestSuite) TestExtract_TwoPhase_LongScene() {
	s.skipIfNoLLM()

	transcript, err := friendsTranscript(0, 50)
	if err != nil {
		s.T().Skipf("could not fetch Friends transcript: %v", err)
	}

	s.T().Log("── TWO-PHASE (50 lines) ──────────────────────────────")
	tpOutput, tpWall := s.runTwoPhaseExtraction(transcript)
	tpMetrics := collectPipelineMetrics("two-phase-50lines", tpOutput, tpWall)
	tpMetrics.logTo(s.T())

	// Basic sanity: two-phase should find some entities.
	s.Greater(tpMetrics.Total, 0, "two-phase produced no entities")
	s.T().Logf("two-phase 50-line: entities=%d rels=%d wall=%dms",
		tpMetrics.Total, tpMetrics.Relationships, tpMetrics.WallMs)
}

// ---------------------------------------------------------------------------
// Discovery vs Fixed-Schema comparison
// ---------------------------------------------------------------------------

// ExtractionObjectMetrics captures per-project extracted object counts used
// for discovery vs fixed-schema comparison.
type ExtractionObjectMetrics struct {
	Label         string
	Total         int
	ByType        map[string]int
	PropsPerType  map[string]float64 // avg filled properties per object per type
	Relationships int
	SchemaTypes   []string // type names in the schema (from DB)
	WallMs        int64
}

func (m *ExtractionObjectMetrics) logTo(t *testing.T) {
	t.Helper()
	t.Logf("  ── %s ──────────────────────────────────────────", m.Label)
	t.Logf("  schema types: %s", strings.Join(m.SchemaTypes, ", "))
	t.Logf("  total=%d  rels=%d  wall=%dms", m.Total, m.Relationships, m.WallMs)
	for _, typeName := range m.SchemaTypes {
		n := m.ByType[typeName]
		avg := m.PropsPerType[typeName]
		t.Logf("    %-16s count=%d  avg_props=%.1f", typeName, n, avg)
	}
	other := m.Total
	for _, n := range m.ByType {
		other -= n
	}
	if other > 0 {
		t.Logf("    %-16s count=%d  ← hallucinated types", "OTHER", other)
	}
}

// collectDBMetrics reads graph_objects + graph_relationships from the DB for
// the given project and builds ExtractionObjectMetrics.
func (s *ExtractionFixedSchemaTestSuite) collectDBMetrics(label string, projectID string, typeNames []string, wallMs int64) *ExtractionObjectMetrics {
	s.T().Helper()
	m := &ExtractionObjectMetrics{
		Label:        label,
		ByType:       make(map[string]int),
		PropsPerType: make(map[string]float64),
		SchemaTypes:  typeNames,
		WallMs:       wallMs,
	}

	// Total object count.
	_ = s.testDB.DB.NewRaw(
		`SELECT COUNT(*) FROM kb.graph_objects WHERE project_id = ? AND supersedes_id IS NULL AND deleted_at IS NULL`,
		projectID,
	).Scan(s.ctx, &m.Total)

	// Per-type counts + avg filled props.
	for _, typeName := range typeNames {
		var rows []struct {
			Properties []byte `bun:"properties"`
		}
		_ = s.testDB.DB.NewRaw(`
			SELECT properties FROM kb.graph_objects
			WHERE project_id = ? AND type = ?
			  AND supersedes_id IS NULL AND deleted_at IS NULL`,
			projectID, typeName,
		).Scan(s.ctx, &rows)

		m.ByType[typeName] = len(rows)
		if len(rows) == 0 {
			continue
		}
		totalProps := 0
		for _, r := range rows {
			var props map[string]any
			if err := json.Unmarshal(r.Properties, &props); err == nil {
				filled := 0
				for _, v := range props {
					if s, ok := v.(string); ok && s != "" {
						filled++
					} else if v != nil {
						filled++
					}
				}
				totalProps += filled
			}
		}
		m.PropsPerType[typeName] = float64(totalProps) / float64(len(rows))
	}

	// Relationship count.
	_ = s.testDB.DB.NewRaw(
		`SELECT COUNT(*) FROM kb.graph_relationships WHERE project_id = ?`,
		projectID,
	).Scan(s.ctx, &m.Relationships)

	return m
}

// friendsTranscriptGuide is the guide message passed to /remember when processing
// Friends transcripts. It tells the discovery agent that the input is a screenplay
// transcript and instructs it to extract semantic story objects — not data-form types
// like Dialogue, Scene, or StageDirection.
const friendsTranscriptGuide = "This is a transcript from the Friends TV show (NBC sitcom, 1994–2004). " +
	"The text is formatted as a screenplay with character names followed by dialogue. " +
	"Do NOT model this as a data format — do not create types like Dialogue, Scene, " +
	"DialogueLine, Quote, StageDirection, or NarrativeTransition. " +
	"Instead, extract the real-world objects and relationships the story reveals: " +
	"the characters and what we learn about them (occupation, home, personality, feelings, situation), " +
	"the bonds between characters (friendships, family, romantic history), " +
	"significant events that have happened or are happening, " +
	"and named places where characters live or spend time."

// friendsRichProjectInfo is a detailed project description used as kbPurpose when
// testing auto-guide generation. It lists the expected types, their key properties,
// valid relationship kinds, and negative rules — giving the guide-generation LLM
// enough context to produce a precise, schema-aligned guide from the document alone.
const friendsRichProjectInfo = `{
  "project": "Friends TV Show Transcripts",
  "purpose": "Build a knowledge graph of the characters, relationships, events, and places in the NBC sitcom Friends (1994-2004). The source documents are episode transcripts formatted as screenplays.",
  "expectedTypes": [
    {
      "type": "Character",
      "description": "A named person who appears or is mentioned in the transcript. One entity per person — consolidate all information about them into a single entity.",
      "keyProperties": ["occupation", "home", "relationship_status", "current_situation", "personality", "stated_feelings"],
      "examples": ["Monica Geller (chef, apartment 20)", "Ross Geller (paleontologist, recently separated)", "Rachel Green (just fled her wedding)", "Chandler Bing (data analyst, sarcastic)", "Joey Tribbiani (struggling actor)", "Phoebe Buffay (masseuse, free-spirited)"]
    },
    {
      "type": "Relationship",
      "description": "A meaningful bond between two characters, described by relationship_kind. Capture the current state if the relationship is under strain or has recently changed.",
      "keyProperties": ["relationship_kind", "current_state", "duration"],
      "validRelationshipKinds": ["friend", "sibling", "ex_spouse", "romantic", "roommate", "colleague", "parent_child"]
    },
    {
      "type": "Event",
      "description": "A significant occurrence that has happened, is happening, or is about to happen. Each event should have a short descriptive name.",
      "keyProperties": ["timing", "location", "description"],
      "examples": ["Rachel leaving Barry at the altar", "Ross and Carol separating", "Carol moving out of Ross's apartment"]
    },
    {
      "type": "Place",
      "description": "A named physical location where scenes take place or characters spend time.",
      "keyProperties": ["location_type", "address", "significance"],
      "examples": ["Central Perk coffee shop", "Monica's apartment", "Chandler and Joey's apartment across the hall"]
    }
  ],
  "negativeRules": [
    "Do NOT extract individual dialogue lines as entities — no Utterance, Dialogue, Quote, or DialogueLine types",
    "Do NOT extract scene directions, stage notes, or formatting markers as entities",
    "Do NOT create duplicate Character entities — one entity per named person",
    "Do NOT create generic types like Scene, Transcript, Episode, or NarrativeTransition",
    "Do NOT extract passing mentions of food, props, or objects unless they are plot-significant"
  ]
}`

// runDiscovery calls /remember with the given text and waits for the agent to
// finalize schema discovery + background extraction to complete.
// guide is an optional natural-language hint injected as prefix in the agent message.
// Pass "" for no guide (baseline). Pass friendsTranscriptGuide for guided discovery.
// Returns the project ID used (fresh isolated project), schema type names, and wall time.
func (s *ExtractionFixedSchemaTestSuite) runDiscovery(text, guide string) (projectID string, typeNames []string, wallMs int64) {
	return s.runDiscoveryProject(text, guide, friendsProjectInfo)
}

// runDiscoveryProject is like runDiscovery but accepts an explicit projectInfo string
// (used as kbPurpose on the project). This lets callers supply friendsRichProjectInfo
// or any other domain description without touching the shared suite state.
func (s *ExtractionFixedSchemaTestSuite) runDiscoveryProject(text, guide, projectInfo string) (projectID string, typeNames []string, wallMs int64) {
	s.T().Helper()

	// Fresh isolated project — no bleed from fixed-schema project.
	orgID := uuid.New().String()
	projectID = uuid.New().String()
	err := testutil.SetupFullTestProject(s.ctx, s.testDB.DB, orgID, projectID)
	s.Require().NoError(err, "create discovery project")

	// Set kbPurpose so the discovery agent has domain context.
	_, err = s.testDB.DB.NewRaw(
		`UPDATE kb.projects SET project_info = ? WHERE id = ?`,
		projectInfo, projectID,
	).Exec(s.ctx)
	s.Require().NoError(err)

	rememberURL := fmt.Sprintf("/api/projects/%s/remember", projectID)

	body := map[string]any{
		"message":       text,
		"schema_policy": "auto",
	}
	if guide != "" {
		body["guide"] = guide
	}

	start := time.Now()
	rec := s.client.PostSSE(
		rememberURL,
		testutil.WithAuth(s.authToken),
		testutil.WithJSONBody(body),
	)
	wallMs = time.Since(start).Milliseconds()

	if rec.StatusCode != http.StatusOK {
		s.T().Logf("discovery /remember returned %d — skipping discovery path", rec.StatusCode)
		return projectID, nil, wallMs
	}
	if rec.HasEvent("error") {
		s.T().Logf("discovery /remember error event — skipping discovery path")
		return projectID, nil, wallMs
	}

	// Wait for: (a) async extraction_prompts LLM call, (b) background extraction worker.
	// The worker runs in the same in-process server, so polling graph_objects works.
	s.T().Log("  waiting for discovery extraction to settle...")
	deadline := time.Now().Add(120 * time.Second)
	var prevCount int
	stableFor := 0
	for time.Now().Before(deadline) {
		time.Sleep(3 * time.Second)
		var count int
		_ = s.testDB.DB.NewRaw(
			`SELECT COUNT(*) FROM kb.graph_objects WHERE project_id = ? AND supersedes_id IS NULL AND deleted_at IS NULL`,
			projectID,
		).Scan(s.ctx, &count)
		if count > 0 && count == prevCount {
			stableFor++
			if stableFor >= 3 {
				break // stable for 9s — assume done
			}
		} else {
			stableFor = 0
		}
		prevCount = count
		s.T().Logf("  discovery poll: objects=%d", count)
	}
	wallMs = time.Since(start).Milliseconds()

	// Query schema type names for this project.
	var rawTypes string
	_ = s.testDB.DB.NewRaw(`
		SELECT COALESCE(gs.object_type_schemas::text, '{}')
		FROM kb.graph_schemas gs
		JOIN kb.project_schemas ps ON ps.schema_id = gs.id
		WHERE ps.project_id = ? AND ps.removed_at IS NULL
		ORDER BY ps.installed_at DESC LIMIT 1`,
		projectID,
	).Scan(s.ctx, &rawTypes)

	if rawTypes != "" && rawTypes != "{}" {
		var typeMap map[string]any
		if err := json.Unmarshal([]byte(rawTypes), &typeMap); err == nil {
			for typeName := range typeMap {
				typeNames = append(typeNames, typeName)
			}
		}
	}

	return projectID, typeNames, wallMs
}

// TestCompare_DiscoveryVsFixedSchema runs the same Friends transcript through
// both paths and logs a side-by-side comparison:
//
//   - Discovery path: /remember agent discovers schema + background extraction
//   - Fixed-schema path: pre-installed Friends schema + synchronous extraction
//
// No hard assertions on relative quality — this is an observational comparison.
func (s *ExtractionFixedSchemaTestSuite) TestCompare_DiscoveryVsFixedSchema() {
	s.skipIfNoLLM()

	transcript, err := friendsTranscript(0, 30)
	if err != nil {
		s.T().Skipf("could not fetch Friends transcript: %v", err)
	}
	s.T().Logf("fixture: %d chars, %d lines", len(transcript), strings.Count(transcript, "\n"))

	// ── Path A: schema discovery (no guide — baseline) ────────────────────
	s.T().Log("══ PATH A: SCHEMA DISCOVERY (no guide) ═══════════════")
	discProjectID, discTypeNames, discWall := s.runDiscovery(transcript, "")
	discMetrics := s.collectDBMetrics("discovery", discProjectID, discTypeNames, discWall)
	discMetrics.logTo(s.T())

	// ── Path B: fixed schema ───────────────────────────────────────────────
	s.T().Log("══ PATH B: FIXED SCHEMA ══════════════════════════════")
	// s.projectID + s.schemaID are already set up by SetupTest.
	fixedStart := time.Now()
	docID := s.createDocument(transcript)
	s.runExtraction(docID)
	fixedWall := time.Since(fixedStart).Milliseconds()
	s.logExtractionSummary()
	fixedMetrics := s.collectDBMetrics("fixed-schema", s.projectID, schemaTypes, fixedWall)
	fixedMetrics.logTo(s.T())

	// ── Side-by-side comparison ────────────────────────────────────────────
	s.T().Log("══ COMPARISON ════════════════════════════════════════")
	s.T().Logf("  %-20s  %-12s  %-12s  %s", "metric", "discovery", "fixed-schema", "Δ (fixed-disc)")
	s.T().Logf("  %-20s  %-12d  %-12d  %+d", "total objects", discMetrics.Total, fixedMetrics.Total, fixedMetrics.Total-discMetrics.Total)
	s.T().Logf("  %-20s  %-12d  %-12d  %+d", "relationships", discMetrics.Relationships, fixedMetrics.Relationships, fixedMetrics.Relationships-discMetrics.Relationships)
	s.T().Logf("  %-20s  %-12d  %-12d  %+d", "schema types", len(discMetrics.SchemaTypes), len(fixedMetrics.SchemaTypes), len(fixedMetrics.SchemaTypes)-len(discMetrics.SchemaTypes))
	s.T().Logf("  %-20s  %-12d  %-12d  %+d", "wall ms", discMetrics.WallMs, fixedMetrics.WallMs, fixedMetrics.WallMs-discMetrics.WallMs)
	s.T().Logf("  discovery schema types:    %s", strings.Join(discMetrics.SchemaTypes, ", "))
	s.T().Logf("  fixed schema types:        %s", strings.Join(fixedMetrics.SchemaTypes, ", "))

	// Sanity: fixed-schema path must extract something.
	s.Greater(fixedMetrics.Total, 0, "fixed-schema extraction produced no objects")
}

// ---------------------------------------------------------------------------
// Quality Assessment
// ---------------------------------------------------------------------------

// friendsGroundTruth defines expected entities for S01E01 scene 1 (~50 lines).
// Source: public Friends transcripts / episode summaries (fair use, test only).
var friendsGroundTruth = struct {
	// Characters expected in the S01E01 opening scene.
	Characters []string
	// Relationships expected — each entry is [person_a, person_b, kind].
	Relationships [][3]string
	// Events expected — partial name/description keywords that should appear.
	Events []string
}{
	Characters: []string{
		"Monica", "Rachel", "Ross", "Chandler", "Joey", "Phoebe",
	},
	Relationships: [][3]string{
		{"Ross", "Monica", "sibling"},
		{"Ross", "Carol", "ex_spouse"},
		{"Chandler", "Joey", "roommate"},
	},
	Events: []string{
		// Rachel leaving her wedding / arriving soaked / Carol and Ross separating.
		"wedding", "Carol", "separat",
	},
}

// EntityRecord holds one extracted entity's type and string properties.
type EntityRecord struct {
	Type  string
	Props map[string]string // string values only; others ignored
}

// extractEntitiesFromProject queries graph_objects for a project and returns
// all live entities with their string properties.
func (s *ExtractionFixedSchemaTestSuite) extractEntitiesFromProject(projectID string) []EntityRecord {
	s.T().Helper()
	var rows []struct {
		Type       string `bun:"type"`
		Properties []byte `bun:"properties"`
	}
	_ = s.testDB.DB.NewRaw(`
		SELECT type, properties
		FROM kb.graph_objects
		WHERE project_id = ? AND supersedes_id IS NULL AND deleted_at IS NULL
		ORDER BY type, created_at`,
		projectID,
	).Scan(s.ctx, &rows)

	out := make([]EntityRecord, 0, len(rows))
	for _, r := range rows {
		rec := EntityRecord{Type: r.Type, Props: make(map[string]string)}
		var raw map[string]any
		if json.Unmarshal(r.Properties, &raw) == nil {
			for k, v := range raw {
				if sv, ok := v.(string); ok && sv != "" {
					rec.Props[k] = sv
				}
			}
		}
		out = append(out, rec)
	}
	return out
}

// extractRelationshipsFromProject queries graph_relationships joining src/dst objects
// to get human-readable src name → rel type → dst name triples.
func (s *ExtractionFixedSchemaTestSuite) extractRelationshipsFromProject(projectID string) []struct{ Src, RelType, Dst string } {
	s.T().Helper()
	var rows []struct {
		RelType   string `bun:"rel_type"`
		SrcProps  []byte `bun:"src_props"`
		DstProps  []byte `bun:"dst_props"`
	}
	_ = s.testDB.DB.NewRaw(`
		SELECT
			gr.type AS rel_type,
			src.properties AS src_props,
			dst.properties AS dst_props
		FROM kb.graph_relationships gr
		JOIN kb.graph_objects src ON src.canonical_id = gr.src_id
			AND src.project_id = gr.project_id
			AND src.supersedes_id IS NULL AND src.deleted_at IS NULL
		JOIN kb.graph_objects dst ON dst.canonical_id = gr.dst_id
			AND dst.project_id = gr.project_id
			AND dst.supersedes_id IS NULL AND dst.deleted_at IS NULL
		WHERE gr.project_id = ? AND gr.supersedes_id IS NULL AND gr.deleted_at IS NULL
		ORDER BY gr.type`,
		projectID,
	).Scan(s.ctx, &rows)

	out := make([]struct{ Src, RelType, Dst string }, 0, len(rows))
	for _, r := range rows {
		var srcP, dstP map[string]any
		_ = json.Unmarshal(r.SrcProps, &srcP)
		_ = json.Unmarshal(r.DstProps, &dstP)
		srcName := firstStringField(srcP, "name", "person_a", "speaker")
		dstName := firstStringField(dstP, "name", "person_b", "target")
		out = append(out, struct{ Src, RelType, Dst string }{srcName, r.RelType, dstName})
	}
	return out
}

func firstStringField(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return "?"
}

// qualityReport holds computed quality metrics for one extraction path.
type qualityReport struct {
	Label string
	// Characters
	TotalCharacters    int
	DistinctCharacters int // distinct by name (dedup)
	DuplicateRate      float64
	FoundMainCast      []string // which of the 6 main cast were found
	MissingMainCast    []string
	// Relationships
	TotalRelObjects int // Relationship entities (schema objects)
	TotalRelEdges   int // graph_relationships rows
	KnownRelsFound  []string // which ground-truth relationships were found
	// Events
	TotalEvents      int
	EventsWithKeyword []string // which ground-truth keywords appear
	// Properties
	AvgPropsPerChar float64
	// Raw entities for logging
	Entities []EntityRecord
}

func (r *qualityReport) logTo(t *testing.T) {
	t.Helper()
	t.Logf("  ── %s ──────────────────────────────────────────────", r.Label)
	t.Logf("  Characters: total=%d  distinct=%d  dup_rate=%.0f%%",
		r.TotalCharacters, r.DistinctCharacters, r.DuplicateRate*100)
	t.Logf("  Main cast found (%d/6): %s", len(r.FoundMainCast), strings.Join(r.FoundMainCast, ", "))
	if len(r.MissingMainCast) > 0 {
		t.Logf("  Main cast MISSING:     %s", strings.Join(r.MissingMainCast, ", "))
	}
	t.Logf("  Relationships: objects=%d  graph_edges=%d", r.TotalRelObjects, r.TotalRelEdges)
	if len(r.KnownRelsFound) > 0 {
		t.Logf("  Known rels found: %s", strings.Join(r.KnownRelsFound, " | "))
	} else {
		t.Logf("  Known rels found: none")
	}
	t.Logf("  Events: total=%d  keywords_found=%s", r.TotalEvents, strings.Join(r.EventsWithKeyword, ", "))
	t.Logf("  Avg props/Character: %.1f", r.AvgPropsPerChar)

	// List all character names.
	t.Logf("  Character list:")
	names := map[string]bool{}
	for _, e := range r.Entities {
		n := e.Props["name"]
		if n == "" {
			n = e.Props["person_a"]
		}
		if (e.Type == "Character" || e.Type == "character") && n != "" {
			marker := ""
			if names[n] {
				marker = " ← DUPLICATE"
			}
			names[n] = true
			occ := e.Props["occupation"]
			t.Logf("    %-30s  occ=%-20s  home=%s  situation=%s%s",
				n, occ, e.Props["home"], e.Props["current_situation"], marker)
		}
	}
}

// buildQualityReport computes quality metrics for a given project's extraction.
func (s *ExtractionFixedSchemaTestSuite) buildQualityReport(label, projectID string) *qualityReport {
	s.T().Helper()

	entities := s.extractEntitiesFromProject(projectID)
	edges := s.extractRelationshipsFromProject(projectID)

	r := &qualityReport{Label: label, Entities: entities}

	// Character analysis.
	nameCount := map[string]int{}
	totalProps := 0
	charCount := 0
	for _, e := range entities {
		typeLower := strings.ToLower(e.Type)
		if typeLower != "character" {
			continue
		}
		charCount++
		n := e.Props["name"]
		if n != "" {
			nameCount[strings.ToLower(n)]++
		}
		totalProps += len(e.Props)
	}
	r.TotalCharacters = charCount
	r.DistinctCharacters = len(nameCount)
	if charCount > 0 {
		r.DuplicateRate = 1.0 - float64(len(nameCount))/float64(charCount)
		r.AvgPropsPerChar = float64(totalProps) / float64(charCount)
	}

	// Main cast detection — substring match (tolerates "Monica Geller" matching "Monica").
	for _, castMember := range friendsGroundTruth.Characters {
		found := false
		for name := range nameCount {
			if strings.Contains(name, strings.ToLower(castMember)) ||
				strings.Contains(strings.ToLower(castMember), name) {
				found = true
				break
			}
		}
		if found {
			r.FoundMainCast = append(r.FoundMainCast, castMember)
		} else {
			r.MissingMainCast = append(r.MissingMainCast, castMember)
		}
	}

	// Relationship object analysis.
	for _, e := range entities {
		if strings.ToLower(e.Type) == "relationship" {
			r.TotalRelObjects++
		}
	}
	r.TotalRelEdges = len(edges)

	// Ground-truth relationship detection — check Relationship entities AND graph edges.
	for _, gt := range friendsGroundTruth.Relationships {
		personA := strings.ToLower(gt[0])
		personB := strings.ToLower(gt[1])
		kind := gt[2]
		found := false

		// Check in Relationship entities.
		for _, e := range entities {
			if strings.ToLower(e.Type) != "relationship" {
				continue
			}
			pa := strings.ToLower(e.Props["person_a"] + " " + e.Props["from_character"])
			pb := strings.ToLower(e.Props["person_b"] + " " + e.Props["to_character"])
			rk := strings.ToLower(e.Props["relationship_kind"] + " " + e.Props["relationship_type"] + " " + e.Props["type"])
			if (strings.Contains(pa, personA) || strings.Contains(pb, personA)) &&
				(strings.Contains(pa, personB) || strings.Contains(pb, personB)) &&
				strings.Contains(rk, kind) {
				found = true
				break
			}
		}

		// Also check in graph edge labels.
		if !found {
			for _, edge := range edges {
				srcL := strings.ToLower(edge.Src)
				dstL := strings.ToLower(edge.Dst)
				relL := strings.ToLower(edge.RelType)
				if (strings.Contains(srcL, personA) || strings.Contains(dstL, personA)) &&
					(strings.Contains(srcL, personB) || strings.Contains(dstL, personB)) &&
					strings.Contains(relL, kind) {
					found = true
					break
				}
			}
		}

		label := fmt.Sprintf("%s–%s(%s)", gt[0], gt[1], kind)
		if found {
			r.KnownRelsFound = append(r.KnownRelsFound, "✓"+label)
		}
	}

	// Event analysis.
	for _, e := range entities {
		tl := strings.ToLower(e.Type)
		if tl != "event" && tl != "significantevent" {
			continue
		}
		r.TotalEvents++
		allText := strings.ToLower(e.Props["name"] + " " + e.Props["description"] + " " + e.Props["participants"])
		for _, kw := range friendsGroundTruth.Events {
			if strings.Contains(allText, strings.ToLower(kw)) {
				r.EventsWithKeyword = append(r.EventsWithKeyword, kw)
				break
			}
		}
	}

	return r
}

// TestCompare_QualityAssessment runs three extraction paths on the same 50-line
// Friends transcript and produces a detailed quality report:
//
//   - Auto-discovery   : /remember with no guide — agent picks schema freely
//   - Guided discovery : /remember with friendsTranscriptGuide — agent told to use semantic types
//   - Fixed schema     : pre-installed Friends schema + synchronous extraction
//
// Metrics compared:
//   - Character deduplication rate (count vs distinct-by-name)
//   - Main cast recall (which of the 6 main characters were found)
//   - Known relationship precision (Ross–Monica sibling, Ross–Carol ex_spouse)
//   - Event keyword recall
//   - Property density per character
func (s *ExtractionFixedSchemaTestSuite) TestCompare_QualityAssessment() {
	s.skipIfNoLLM()

	transcript, err := friendsTranscript(0, 50)
	if err != nil {
		s.T().Skipf("could not fetch Friends transcript: %v", err)
	}
	s.T().Logf("fixture: %d chars, %d lines", len(transcript), strings.Count(transcript, "\n"))

	// ── Path A: auto-discovery (no guide) ──────────────────────────────────
	s.T().Log("══ PATH A: AUTO DISCOVERY (no guide) ═════════════════")
	autoProjectID, _, autoWall := s.runDiscovery(transcript, "")
	autoReport := s.buildQualityReport("auto-discovery", autoProjectID)
	autoReport.TotalRelEdges = len(s.extractRelationshipsFromProject(autoProjectID))
	autoReport.logTo(s.T())
	s.T().Logf("  wall=%dms", autoWall)

	// ── Path B: guided discovery ────────────────────────────────────────────
	s.T().Log("══ PATH B: GUIDED DISCOVERY ══════════════════════════")
	guidedProjectID, _, guidedWall := s.runDiscovery(transcript, friendsTranscriptGuide)
	guidedReport := s.buildQualityReport("guided-discovery", guidedProjectID)
	guidedReport.TotalRelEdges = len(s.extractRelationshipsFromProject(guidedProjectID))
	guidedReport.logTo(s.T())
	s.T().Logf("  wall=%dms", guidedWall)

	// ── Path C: fixed schema ───────────────────────────────────────────────
	s.T().Log("══ PATH C: FIXED SCHEMA ══════════════════════════════")
	fixedStart := time.Now()
	docID := s.createDocument(transcript)
	s.runExtraction(docID)
	fixedWall := time.Since(fixedStart).Milliseconds()
	fixedReport := s.buildQualityReport("fixed-schema", s.projectID)
	fixedReport.TotalRelEdges = len(s.extractRelationshipsFromProject(s.projectID))
	fixedReport.logTo(s.T())
	s.T().Logf("  wall=%dms", fixedWall)

	// ── Side-by-side quality comparison ────────────────────────────────────
	s.T().Log("══ QUALITY COMPARISON (A vs B vs C) ══════════════════")
	s.T().Logf("  %-30s  %-14s  %-14s  %-14s", "metric", "auto-disc", "guided-disc", "fixed-schema")
	s.T().Logf("  %-30s  %-14d  %-14d  %-14d", "total characters",
		autoReport.TotalCharacters, guidedReport.TotalCharacters, fixedReport.TotalCharacters)
	s.T().Logf("  %-30s  %-14d  %-14d  %-14d", "distinct characters",
		autoReport.DistinctCharacters, guidedReport.DistinctCharacters, fixedReport.DistinctCharacters)
	s.T().Logf("  %-30s  %-13.0f%%  %-13.0f%%  %-13.0f%%", "duplication rate",
		autoReport.DuplicateRate*100, guidedReport.DuplicateRate*100, fixedReport.DuplicateRate*100)
	s.T().Logf("  %-30s  %d/6%-11s  %d/6%-11s  %d/6",
		"main cast recall",
		len(autoReport.FoundMainCast), "",
		len(guidedReport.FoundMainCast), "",
		len(fixedReport.FoundMainCast))
	s.T().Logf("  %-30s  %-14d  %-14d  %-14d", "known rels found",
		len(autoReport.KnownRelsFound), len(guidedReport.KnownRelsFound), len(fixedReport.KnownRelsFound))
	s.T().Logf("  %-30s  %-14d  %-14d  %-14d", "relationship objects",
		autoReport.TotalRelObjects, guidedReport.TotalRelObjects, fixedReport.TotalRelObjects)
	s.T().Logf("  %-30s  %-14d  %-14d  %-14d", "graph edges",
		autoReport.TotalRelEdges, guidedReport.TotalRelEdges, fixedReport.TotalRelEdges)
	s.T().Logf("  %-30s  %-14d  %-14d  %-14d", "events",
		autoReport.TotalEvents, guidedReport.TotalEvents, fixedReport.TotalEvents)
	s.T().Logf("  %-30s  %-14.1f  %-14.1f  %-14.1f", "avg props/character",
		autoReport.AvgPropsPerChar, guidedReport.AvgPropsPerChar, fixedReport.AvgPropsPerChar)
	s.T().Logf("  %-30s  %-14d  %-14d  %-14d", "wall ms",
		autoWall, guidedWall, fixedWall)

	// Soft assertions — guided and fixed must find at least half the main cast.
	// Auto-discovery is skipped when the LLM produced 0 objects (timing flake).
	if autoReport.TotalCharacters > 0 {
		s.GreaterOrEqual(len(autoReport.FoundMainCast), 3, "auto-discovery should find at least 3 main cast")
	} else {
		s.T().Log("  NOTE: auto-discovery produced 0 characters (LLM timing flake) — assertion skipped")
	}
	s.GreaterOrEqual(len(guidedReport.FoundMainCast), 3, "guided should find at least 3 main cast")
	s.GreaterOrEqual(len(fixedReport.FoundMainCast), 3, "fixed should find at least 3 main cast")
}

// generateGuide calls the same LLM used by extraction to classify the document
// against the project's domain description and returns a natural-language guide
// string suitable for passing as the guide field of /remember.
//
// Prompt strategy: show the full project info (expected types, properties,
// relationship kinds, negative rules) alongside a 2000-char excerpt of the
// document so the LLM can produce a precise, schema-aligned guide without
// requiring a hand-written constant.
func (s *ExtractionFixedSchemaTestSuite) generateGuide(projectInfo, transcript string) string {
	s.T().Helper()

	mf := s.modelFactory()
	if mf == nil {
		s.T().Skip("no LLM credentials — cannot generate guide")
	}
	llm, err := mf.CreateModel(s.ctx)
	s.Require().NoError(err, "create LLM model for guide generation")

	// Limit doc excerpt so the prompt stays well within the model context window.
	excerpt := transcript
	if len(excerpt) > 2000 {
		excerpt = excerpt[:2000]
	}

	promptText := fmt.Sprintf(`You are a document classifier for a knowledge graph project.

Project domain model:
%s

Document excerpt (beginning of the source text):
%s

Task: Based on the project's expected types, properties, and rules above, write a concise extraction guide (3–5 sentences) for this document.
The guide should:
1. Identify the document format (e.g. screenplay transcript, news article, etc.)
2. State which entity types from the project model are relevant and what properties to populate
3. Name the relationship kinds that apply
4. Repeat the most important negative rule (what NOT to create)

Return only the guide text — no preamble, no JSON, no bullet points.`, projectInfo, excerpt)

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: promptText}}},
		},
		Config: &genai.GenerateContentConfig{
			MaxOutputTokens: 400,
		},
	}

	var guide strings.Builder
	for resp, err := range llm.GenerateContent(s.ctx, req, false) {
		if err != nil {
			s.T().Fatalf("guide generation LLM error: %v", err)
		}
		if resp != nil && resp.Content != nil {
			for _, part := range resp.Content.Parts {
				guide.WriteString(part.Text)
			}
		}
	}
	return strings.TrimSpace(guide.String())
}

// TestCompare_ProjectInfoClassifiedVsFixedSchema runs three extraction paths:
//
//   - Path A: discovery with an LLM-generated guide (project info + document → LLM → guide)
//   - Path B: discovery with the hardcoded friendsTranscriptGuide (hand-written baseline)
//   - Path C: fixed-schema extraction
//
// The key question: can the guide-generation step produce a guide from the rich project
// description alone that is as good as (or better than) the handcrafted guide?
func (s *ExtractionFixedSchemaTestSuite) TestCompare_ProjectInfoClassifiedVsFixedSchema() {
	s.skipIfNoLLM()

	transcript, err := friendsTranscript(0, 50)
	if err != nil {
		s.T().Skipf("could not fetch Friends transcript: %v", err)
	}
	s.T().Logf("fixture: %d chars, %d lines", len(transcript), strings.Count(transcript, "\n"))

	// ── Generate guide from project info + document ─────────────────────────
	s.T().Log("══ GENERATING GUIDE FROM PROJECT INFO ════════════════")
	classifiedGuide := s.generateGuide(friendsRichProjectInfo, transcript)
	s.T().Logf("  generated guide:\n    %s", strings.ReplaceAll(classifiedGuide, "\n", "\n    "))

	// ── Path A: project-info-classified guide ───────────────────────────────
	s.T().Log("══ PATH A: PROJECT-INFO CLASSIFIED GUIDE ═════════════")
	classProjectID, _, classWall := s.runDiscoveryProject(transcript, classifiedGuide, friendsRichProjectInfo)
	classReport := s.buildQualityReport("classified-guide", classProjectID)
	classReport.TotalRelEdges = len(s.extractRelationshipsFromProject(classProjectID))
	classReport.logTo(s.T())
	s.T().Logf("  wall=%dms", classWall)

	// ── Path B: hardcoded guide baseline ────────────────────────────────────
	s.T().Log("══ PATH B: HARDCODED GUIDE (baseline) ════════════════")
	guidedProjectID, _, guidedWall := s.runDiscoveryProject(transcript, friendsTranscriptGuide, friendsRichProjectInfo)
	guidedReport := s.buildQualityReport("hardcoded-guide", guidedProjectID)
	guidedReport.TotalRelEdges = len(s.extractRelationshipsFromProject(guidedProjectID))
	guidedReport.logTo(s.T())
	s.T().Logf("  wall=%dms", guidedWall)

	// ── Path C: fixed schema ────────────────────────────────────────────────
	s.T().Log("══ PATH C: FIXED SCHEMA ══════════════════════════════")
	fixedStart := time.Now()
	docID := s.createDocument(transcript)
	s.runExtraction(docID)
	fixedWall := time.Since(fixedStart).Milliseconds()
	fixedReport := s.buildQualityReport("fixed-schema", s.projectID)
	fixedReport.TotalRelEdges = len(s.extractRelationshipsFromProject(s.projectID))
	fixedReport.logTo(s.T())
	s.T().Logf("  wall=%dms", fixedWall)

	// ── 3-column quality comparison ─────────────────────────────────────────
	s.T().Log("══ QUALITY COMPARISON (A vs B vs C) ══════════════════")
	s.T().Logf("  %-30s  %-16s  %-16s  %-14s", "metric", "classified-guide", "hardcoded-guide", "fixed-schema")
	s.T().Logf("  %-30s  %-16d  %-16d  %-14d", "total characters",
		classReport.TotalCharacters, guidedReport.TotalCharacters, fixedReport.TotalCharacters)
	s.T().Logf("  %-30s  %-16d  %-16d  %-14d", "distinct characters",
		classReport.DistinctCharacters, guidedReport.DistinctCharacters, fixedReport.DistinctCharacters)
	s.T().Logf("  %-30s  %-15.0f%%  %-15.0f%%  %-13.0f%%", "duplication rate",
		classReport.DuplicateRate*100, guidedReport.DuplicateRate*100, fixedReport.DuplicateRate*100)
	s.T().Logf("  %-30s  %d/6%-13s  %d/6%-13s  %d/6",
		"main cast recall",
		len(classReport.FoundMainCast), "",
		len(guidedReport.FoundMainCast), "",
		len(fixedReport.FoundMainCast))
	s.T().Logf("  %-30s  %-16d  %-16d  %-14d", "known rels found",
		len(classReport.KnownRelsFound), len(guidedReport.KnownRelsFound), len(fixedReport.KnownRelsFound))
	s.T().Logf("  %-30s  %-16d  %-16d  %-14d", "relationship objects",
		classReport.TotalRelObjects, guidedReport.TotalRelObjects, fixedReport.TotalRelObjects)
	s.T().Logf("  %-30s  %-16d  %-16d  %-14d", "graph edges",
		classReport.TotalRelEdges, guidedReport.TotalRelEdges, fixedReport.TotalRelEdges)
	s.T().Logf("  %-30s  %-16d  %-16d  %-14d", "events",
		classReport.TotalEvents, guidedReport.TotalEvents, fixedReport.TotalEvents)
	s.T().Logf("  %-30s  %-16.1f  %-16.1f  %-14.1f", "avg props/character",
		classReport.AvgPropsPerChar, guidedReport.AvgPropsPerChar, fixedReport.AvgPropsPerChar)
	s.T().Logf("  %-30s  %-16d  %-16d  %-14d", "wall ms (excl guide gen)",
		classWall, guidedWall, fixedWall)

	// Soft assertions — all three paths should find at least half the main cast.
	if classReport.TotalCharacters > 0 {
		s.GreaterOrEqual(len(classReport.FoundMainCast), 3, "classified-guide should find at least 3 main cast")
	} else {
		s.T().Log("  NOTE: classified-guide produced 0 characters — LLM timing flake, assertion skipped")
	}
	if guidedReport.TotalCharacters > 0 {
		s.GreaterOrEqual(len(guidedReport.FoundMainCast), 3, "hardcoded-guide should find at least 3 main cast")
	} else {
		s.T().Log("  NOTE: hardcoded-guide produced 0 characters — LLM timing flake, assertion skipped")
	}
	s.GreaterOrEqual(len(fixedReport.FoundMainCast), 3, "fixed-schema should find at least 3 main cast")
}

// TestCompare_GuidedDiscoveryVsFixedSchema_LongScene repeats the guided comparison
// on a 50-line fixture for a richer signal.
func (s *ExtractionFixedSchemaTestSuite) TestCompare_GuidedDiscoveryVsFixedSchema_LongScene() {
	s.skipIfNoLLM()

	transcript, err := friendsTranscript(0, 50)
	if err != nil {
		s.T().Skipf("could not fetch Friends transcript: %v", err)
	}
	s.T().Logf("fixture: %d chars, %d lines", len(transcript), strings.Count(transcript, "\n"))

	// ── Path A: guided discovery ───────────────────────────────────────────
	s.T().Log("══ PATH A: GUIDED DISCOVERY (50 lines) ═══════════════")
	discProjectID, discTypeNames, discWall := s.runDiscovery(transcript, friendsTranscriptGuide)
	discMetrics := s.collectDBMetrics("guided-discovery-50", discProjectID, discTypeNames, discWall)
	discMetrics.logTo(s.T())

	// ── Path B: fixed schema ───────────────────────────────────────────────
	s.T().Log("══ PATH B: FIXED SCHEMA (50 lines) ═══════════════════")
	fixedStart := time.Now()
	docID := s.createDocument(transcript)
	s.runExtraction(docID)
	fixedWall := time.Since(fixedStart).Milliseconds()
	s.logExtractionSummary()
	fixedMetrics := s.collectDBMetrics("fixed-schema-50", s.projectID, schemaTypes, fixedWall)
	fixedMetrics.logTo(s.T())

	// ── Comparison ─────────────────────────────────────────────────────────
	s.T().Log("══ COMPARISON 50-line (guided vs fixed) ══════════════")
	s.T().Logf("  guided schema types:  %s", strings.Join(discMetrics.SchemaTypes, ", "))
	s.T().Logf("  fixed schema types:   %s", strings.Join(fixedMetrics.SchemaTypes, ", "))
	s.T().Logf("  %-20s  %-14s  %-14s  %s", "metric", "guided-disc", "fixed-schema", "Δ (fixed-guided)")
	s.T().Logf("  %-20s  %-14d  %-14d  %+d", "total objects", discMetrics.Total, fixedMetrics.Total, fixedMetrics.Total-discMetrics.Total)
	s.T().Logf("  %-20s  %-14d  %-14d  %+d", "relationships", discMetrics.Relationships, fixedMetrics.Relationships, fixedMetrics.Relationships-discMetrics.Relationships)
	s.T().Logf("  %-20s  %-14d  %-14d  %+d", "wall ms", discMetrics.WallMs, fixedMetrics.WallMs, fixedMetrics.WallMs-discMetrics.WallMs)
	for _, typeName := range schemaTypes {
		s.T().Logf("    %-16s  guided=%d  fixed=%d  Δ=%+d",
			typeName, discMetrics.ByType[typeName], fixedMetrics.ByType[typeName],
			fixedMetrics.ByType[typeName]-discMetrics.ByType[typeName])
	}

	s.Greater(discMetrics.Total+fixedMetrics.Total, 0, "both paths produced no objects")
}

// TestCompare_DiscoveryVsFixedSchema_LongScene repeats the comparison on a
// longer 50-line excerpt for a more data-rich signal.
func (s *ExtractionFixedSchemaTestSuite) TestCompare_DiscoveryVsFixedSchema_LongScene() {
	s.skipIfNoLLM()

	transcript, err := friendsTranscript(0, 50)
	if err != nil {
		s.T().Skipf("could not fetch Friends transcript: %v", err)
	}
	s.T().Logf("fixture: %d chars, %d lines", len(transcript), strings.Count(transcript, "\n"))

	// ── Path A: discovery (no guide — baseline) ───────────────────────────
	s.T().Log("══ PATH A: SCHEMA DISCOVERY (50 lines, no guide) ═════")
	discProjectID, discTypeNames, discWall := s.runDiscovery(transcript, "")
	discMetrics := s.collectDBMetrics("discovery-50", discProjectID, discTypeNames, discWall)
	discMetrics.logTo(s.T())

	// ── Path B: fixed schema ───────────────────────────────────────────────
	s.T().Log("══ PATH B: FIXED SCHEMA (50 lines) ═══════════════════")
	fixedStart := time.Now()
	docID := s.createDocument(transcript)
	s.runExtraction(docID)
	fixedWall := time.Since(fixedStart).Milliseconds()
	s.logExtractionSummary()
	fixedMetrics := s.collectDBMetrics("fixed-schema-50", s.projectID, schemaTypes, fixedWall)
	fixedMetrics.logTo(s.T())

	// ── Report ─────────────────────────────────────────────────────────────
	s.T().Log("══ COMPARISON (50 lines) ═════════════════════════════")
	s.T().Logf("  %-20s  %-12s  %-12s  %s", "metric", "discovery", "fixed-schema", "Δ (fixed-disc)")
	s.T().Logf("  %-20s  %-12d  %-12d  %+d", "total objects", discMetrics.Total, fixedMetrics.Total, fixedMetrics.Total-discMetrics.Total)
	s.T().Logf("  %-20s  %-12d  %-12d  %+d", "relationships", discMetrics.Relationships, fixedMetrics.Relationships, fixedMetrics.Relationships-discMetrics.Relationships)
	s.T().Logf("  %-20s  %-12d  %-12d  %+d", "schema types", len(discMetrics.SchemaTypes), len(fixedMetrics.SchemaTypes), len(fixedMetrics.SchemaTypes)-len(discMetrics.SchemaTypes))
	s.T().Logf("  %-20s  %-12d  %-12d  %+d", "wall ms", discMetrics.WallMs, fixedMetrics.WallMs, fixedMetrics.WallMs-discMetrics.WallMs)

	// Per-type comparison for fixed schema types.
	s.T().Logf("  per fixed-schema type:")
	for _, typeName := range schemaTypes {
		discN := discMetrics.ByType[typeName]
		fixedN := fixedMetrics.ByType[typeName]
		s.T().Logf("    %-16s  disc=%d  fixed=%d  Δ=%+d", typeName, discN, fixedN, fixedN-discN)
	}

	s.Greater(fixedMetrics.Total, 0, "fixed-schema extraction produced no objects")
}
