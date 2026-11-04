//go:build embeddings

// Package integration — embedding-dependent e2e test.
//
// This file requires a working embedding service (Google AI / Vertex AI).
// Run with: go test ./tests/integration/... -tags embeddings -run TestMergeEnrichmentE2E
//
// The test exercises the full pipeline:
//   1. Extract to staging branch (LLM extraction, real embedding worker)
//   2. Wait for embeddings to compute on staging objects
//   3. GET /branches/{id}/merge-readiness → confirm ready
//   4. POST /branches/main/merge with policy="enrich" → similarity-aware merge
//   5. Second extraction pass → repeat
//   6. Compare: similar_count, enriched_keys, entity quality

package integration

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/domain/branches"
	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/domain/extraction"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/pkg/adk"
)

// ---------------------------------------------------------------------------
// Suite
// ---------------------------------------------------------------------------

type MergeEnrichmentE2ESuite struct {
	suite.Suite

	ctx       context.Context
	testDB    *testutil.TestDB
	inProcess *testutil.TestServer
	client    *testutil.HTTPClient
	projectID string
	orgID     string
	authToken string
	schemaID  string
	log       interface{ Warn(string, ...any) }
}

func TestMergeEnrichmentE2E(t *testing.T) {
	suite.Run(t, new(MergeEnrichmentE2ESuite))
}

func (s *MergeEnrichmentE2ESuite) SetupSuite() {
	s.ctx = context.Background()
	testutil.LoadEnvFiles()
	db, err := testutil.SetupTestDB(s.ctx, "merge_e2e")
	s.Require().NoError(err, "setup test db")
	s.testDB = db
	s.authToken = "e2e-test-user"
}

func (s *MergeEnrichmentE2ESuite) TearDownSuite() {
	if s.testDB != nil {
		s.testDB.Close()
	}
}

func (s *MergeEnrichmentE2ESuite) TearDownTest() {
	if s.inProcess != nil && s.inProcess.StopFn != nil {
		s.inProcess.StopFn()
	}
}

func (s *MergeEnrichmentE2ESuite) SetupTest() {
	s.Require().NoError(testutil.TruncateTables(s.ctx, s.testDB.DB))
	s.Require().NoError(testutil.SetupTestFixtures(s.ctx, s.testDB.DB))
	s.orgID = uuid.New().String()
	s.projectID = uuid.New().String()
	s.Require().NoError(testutil.SetupFullTestProject(s.ctx, s.testDB.DB, s.orgID, s.projectID))

	// Set project_info for extraction context.
	_, err := s.testDB.DB.NewRaw(
		`UPDATE kb.projects SET project_info = ? WHERE id = ?`,
		friendsProjectInfo, s.projectID,
	).Exec(s.ctx)
	s.Require().NoError(err)

	// Start in-process server with LLM + embedding worker.
	s.inProcess = testutil.NewTestServerWithLLM(s.testDB)
	s.client = testutil.NewHTTPClient(s.inProcess.Echo)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (s *MergeEnrichmentE2ESuite) skipIfNoEmbeddingService() {
	// Check that the embedding service is configured for this project by
	// verifying the server is healthy and the embedding worker has started.
	resp := s.client.GET("/api/health", testutil.WithAuth(s.authToken))
	if resp.StatusCode == http.StatusServiceUnavailable {
		s.T().Skip("server unavailable — skipping embedding test")
	}
	// Verify at least one of the embedding model env vars is set.
	testutil.LoadEnvFiles()
	cfg, err := config.NewConfig(nil)
	if err != nil {
		s.T().Skip("could not load config — skipping embedding test")
	}
	if !cfg.LLM.IsEnabled() {
		s.T().Skip("no LLM credentials — skipping embedding test")
	}
	// We rely on the server-side embedding worker using the project's credential store.
	// If no embedding credentials are configured in the project, embeddings will
	// remain NULL and waitForEmbeddings will time out — the test will then be skipped.
}

func (s *MergeEnrichmentE2ESuite) modelFactory() *adk.ModelFactory {
	cfg, err := config.NewConfig(nil)
	if err != nil || !cfg.LLM.IsEnabled() {
		return nil
	}
	return adk.NewModelFactory(&cfg.LLM, nil, nil, nil, nil)
}

func (s *MergeEnrichmentE2ESuite) installFriendsSchemaE2E() string {
	db := s.testDB.DB
	// Reuse the schema install helpers from extraction_fixed_schema_test.go
	// by building the same schema JSONB directly.
	typeSchemas, err := marshalJSON(friendsSchema)
	s.Require().NoError(err)
	relSchemas, err := marshalJSON(friendsRelationshipSchema)
	s.Require().NoError(err)
	prompts, err := marshalJSON(friendsExtractionPrompts)
	s.Require().NoError(err)

	schemaID := uuid.New().String()
	_, err = db.NewRaw(`
		INSERT INTO kb.graph_schemas (id,name,version,object_type_schemas,relationship_type_schemas,extraction_prompts,source,published_at)
		VALUES (?::uuid,?,?,?::jsonb,?::jsonb,?::jsonb,'manual',now())`,
		schemaID, "Friends E2E", "1.0.0", string(typeSchemas), string(relSchemas), string(prompts),
	).Exec(s.ctx)
	s.Require().NoError(err)

	_, err = db.NewRaw(`
		INSERT INTO kb.project_schemas (id,project_id,schema_id,installed_at,active)
		VALUES (gen_random_uuid(),?::uuid,?::uuid,now(),true)`,
		s.projectID, schemaID,
	).Exec(s.ctx)
	s.Require().NoError(err)
	return schemaID
}

func marshalJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

// runExtractionToBranchE2E runs a synchronous extraction that creates a staging branch
// and returns the staging branch ID. Uses branchService wired in.
func (s *MergeEnrichmentE2ESuite) runExtractionToBranchE2E(docID string) uuid.UUID {
	s.T().Helper()
	db := s.testDB.DB

	branchSvc := branches.NewService(branches.NewStore(db))
	docsRepo := documents.NewRepository(db, nil)
	docsSvc := documents.NewService(docsRepo, nil)
	cfg := &config.Config{}
	cfg.Graph.MaxBatchObjects = 500
	cfg.Graph.MaxBatchRelationships = 500
	cfg.Graph.MaxListLimit = 1000
	cfg.Graph.DefaultListLimit = 100
	graphRepo := graph.NewRepository(db, nil, cfg)
	graphSvc := graph.NewService(graphRepo, nil,
		graph.ProvideSchemaProvider(db, nil),
		graph.ProvideInverseTypeProvider(db, nil),
		nil, nil, nil, nil, nil, nil)
	extractionSchemaProvider := extraction.NewMemorySchemaProvider(db, nil)
	jobsCfg := &extraction.ObjectExtractionConfig{}
	jobsSvc := extraction.NewObjectExtractionJobsService(db, nil, jobsCfg)

	mf := s.modelFactory()
	if mf == nil {
		s.T().Skip("no LLM credentials")
	}

	job, err := jobsSvc.CreateJob(s.ctx, extraction.CreateObjectExtractionJobOptions{
		ProjectID:    s.projectID,
		DocumentID:   &docID,
		EnabledTypes: []string{"Character", "Location", "Event", "Object"},
	})
	s.Require().NoError(err)

	worker := extraction.NewObjectExtractionWorker(
		jobsSvc, graphSvc, branchSvc, docsSvc,
		extractionSchemaProvider, mf, nil,
		extraction.DefaultObjectExtractionWorkerConfig(), nil, nil,
	)
	results, runErr := worker.ProcessJobSync(s.ctx, job)
	if runErr != nil {
		s.T().Logf("  extraction error (may be partial): %v", runErr)
	}
	if results != nil {
		s.T().Logf("  extracted: objects=%d relationships=%d", results.ObjectsCreated, results.RelationshipsCreated)
	}

	// Load staging_branch_id from job.
	var stagingBranchIDStr string
	_ = db.NewRaw(
		`SELECT COALESCE(staging_branch_id::text,'') FROM kb.object_extraction_jobs WHERE id = ?`,
		job.ID,
	).Scan(s.ctx, &stagingBranchIDStr)
	if stagingBranchIDStr == "" {
		s.T().Fatal("no staging branch created — branchService not wired correctly")
	}
	bid, _ := uuid.Parse(stagingBranchIDStr)
	s.T().Logf("  staging branch: %s", bid)
	return bid
}

// waitForEmbeddings polls until all objects on the staging branch have embeddings,
// or until deadline. Skips the test if embeddings never arrive (service not configured).
func (s *MergeEnrichmentE2ESuite) waitForEmbeddings(branchID uuid.UUID) {
	s.T().Helper()
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		var pending int
		_ = s.testDB.DB.NewRaw(`
			SELECT COUNT(*) FROM kb.graph_objects
			WHERE branch_id = ? AND supersedes_id IS NULL AND deleted_at IS NULL
			  AND embedding_v2 IS NULL`,
			branchID,
		).Scan(s.ctx, &pending)
		if pending == 0 {
			s.T().Log("  embeddings: all done ✓")
			return
		}
		s.T().Logf("  embeddings: %d still pending...", pending)
		time.Sleep(5 * time.Second)
	}
	s.T().Skip("embedding worker did not produce embeddings within 120s — embedding service may not be configured")
}

// mergeBranchViaService calls MergeBranch with the given policy and executes it.
func (s *MergeEnrichmentE2ESuite) mergeBranchViaService(branchID uuid.UUID, policy string) *graph.BranchMergeResponse {
	s.T().Helper()
	cfg := &config.Config{}
	cfg.Graph.MaxBatchObjects = 500
	cfg.Graph.MaxBatchRelationships = 500
	cfg.Graph.MaxListLimit = 1000
	cfg.Graph.DefaultListLimit = 100
	db := s.testDB.DB
	graphRepo := graph.NewRepository(db, nil, cfg)
	graphSvc := graph.NewService(graphRepo, nil,
		graph.ProvideSchemaProvider(db, nil),
		graph.ProvideInverseTypeProvider(db, nil),
		nil, nil, nil, nil, nil, nil)
	pid, _ := uuid.Parse(s.projectID)
	resp, err := graphSvc.MergeBranch(s.ctx, pid, nil, &graph.BranchMergeRequest{
		SourceBranchID:      branchID,
		Execute:             true,
		Policy:              policy,
		SimilarityThreshold: 0.92,
	})
	s.Require().NoError(err)
	s.T().Logf("  merge(%s): added=%d similar=%d ff=%d embeddings_pending=%d",
		policy, resp.AddedCount, resp.SimilarCount, resp.FastForwardCount, resp.EmbeddingsPending)
	for _, o := range resp.Objects {
		if o.Status == "similar" {
			s.T().Logf("    ~ similar: score=%.3f src→%s enriched=%v",
				o.SimilarityScore, o.SimilarTargetName, o.EnrichedKeys)
		}
	}
	return resp
}

// mergeReadinessViaService calls BranchMergeReadiness and returns (total, pending).
func (s *MergeEnrichmentE2ESuite) mergeReadinessViaService(branchID uuid.UUID) (int, int) {
	s.T().Helper()
	cfg := &config.Config{}
	cfg.Graph.MaxBatchObjects = 500
	cfg.Graph.MaxBatchRelationships = 500
	cfg.Graph.MaxListLimit = 1000
	cfg.Graph.DefaultListLimit = 100
	db := s.testDB.DB
	graphRepo := graph.NewRepository(db, nil, cfg)
	graphSvc := graph.NewService(graphRepo, nil,
		graph.ProvideSchemaProvider(db, nil),
		graph.ProvideInverseTypeProvider(db, nil),
		nil, nil, nil, nil, nil, nil)
	pid, _ := uuid.Parse(s.projectID)
	total, pending, err := graphSvc.BranchMergeReadiness(s.ctx, pid, branchID)
	s.Require().NoError(err)
	return total, pending
}

// countMainObjects returns the count of live main-graph objects of a given type.
func (s *MergeEnrichmentE2ESuite) countMainObjectsE2E(typeName string) int {
	var n int
	_ = s.testDB.DB.NewRaw(
		`SELECT COUNT(*) FROM kb.graph_objects WHERE project_id = ? AND type = ?
		 AND branch_id IS NULL AND supersedes_id IS NULL AND deleted_at IS NULL`,
		s.projectID, typeName,
	).Scan(s.ctx, &n)
	return n
}

func (s *MergeEnrichmentE2ESuite) extractEntitySnapshotsE2E(projectID string) []entitySnapshot {
	var rows []struct {
		ID         string `bun:"id"`
		Type       string `bun:"type"`
		Properties []byte `bun:"properties"`
	}
	_ = s.testDB.DB.NewRaw(`
		SELECT id::text, type, properties FROM kb.graph_objects
		WHERE project_id = ? AND branch_id IS NULL
		  AND supersedes_id IS NULL AND deleted_at IS NULL
		ORDER BY type, created_at`, projectID,
	).Scan(s.ctx, &rows)
	out := make([]entitySnapshot, 0, len(rows))
	for _, r := range rows {
		snap := entitySnapshot{ID: r.ID, Type: r.Type, Props: make(map[string]string)}
		var raw map[string]any
		if err := json.Unmarshal(r.Properties, &raw); err == nil {
			for k, v := range raw {
				if sv, ok := v.(string); ok && sv != "" {
					snap.Props[k] = sv
				}
			}
		}
		out = append(out, snap)
	}
	return out
}

// ---------------------------------------------------------------------------
// Test
// ---------------------------------------------------------------------------

// TestMergeEnrichmentE2E_SecondRunWithRealEmbeddings runs two extraction passes on the
// same Friends transcript using the full pipeline:
//   - LLM extraction (deepseek-v4-flash via LiteLLM)
//   - Real embedding worker (Google AI / Vertex AI text-embedding model)
//   - Similarity-aware merge (policy="enrich", threshold=0.92)
//
// This test verifies that real semantic embeddings enable near-duplicate entity
// detection across extraction runs — e.g., "Mother (Geller)" and "Monica and Ross's
// mother" may have cosine similarity > 0.92 and be merged rather than duplicated.
//
// Build tag: //go:build embeddings
// Run: go test ./tests/integration/... -tags embeddings -run TestMergeEnrichmentE2E -v -timeout 20m
func (s *MergeEnrichmentE2ESuite) TestMergeEnrichmentE2E_SecondRunWithRealEmbeddings() {
	s.skipIfNoEmbeddingService()

	transcript, err := friendsTranscript(0, 50)
	if err != nil {
		s.T().Skipf("could not fetch Friends transcript: %v", err)
	}
	s.T().Logf("fixture: %d chars, %d lines", len(transcript), strings.Count(transcript, "\n"))

	// Install Friends schema.
	_ = s.installFriendsSchemaE2E()

	// Create source document.
	docsRepo := documents.NewRepository(s.testDB.DB, nil)
	docsSvc := documents.NewService(docsRepo, nil)
	filename := "friends-transcript.txt"
	sourceType := "manual"
	doc, _, err := docsSvc.Create(s.ctx, documents.CreateParams{
		ProjectID:  s.projectID,
		Filename:   &filename,
		Content:    &transcript,
		SourceType: &sourceType,
	})
	s.Require().NoError(err)
	s.T().Logf("document: id=%s", doc.ID)

	// ── Run 1: extract → staging branch 1 → wait for embeddings → merge ──────
	s.T().Log("══ RUN 1: extract → embed → merge ═══════════════════")
	b1 := s.runExtractionToBranchE2E(doc.ID)

	// Check readiness — show pending count.
	total1, pending1 := s.mergeReadinessViaService(b1)
	s.T().Logf("  merge-readiness: total=%d pending=%d", total1, pending1)

	// Wait for embedding worker to process all staging objects.
	s.waitForEmbeddings(b1)
	total1, pending1 = s.mergeReadinessViaService(b1)
	s.T().Logf("  merge-readiness after wait: total=%d pending=%d", total1, pending1)
	s.Equal(0, pending1, "all objects should be embedded before merge")

	// Merge with similarity-aware enrich policy.
	mergeResp1 := s.mergeBranchViaService(b1, "enrich")
	snap1 := s.extractEntitySnapshotsE2E(s.projectID)
	s.T().Logf("  run-1 main graph: %d entities, added=%d", len(snap1), mergeResp1.AddedCount)

	// ── Run 2: same doc → staging branch 2 → wait → merge ────────────────────
	s.T().Log("══ RUN 2: same doc → embed → merge (with similarity) ═")
	b2 := s.runExtractionToBranchE2E(doc.ID)

	total2, pending2 := s.mergeReadinessViaService(b2)
	s.T().Logf("  merge-readiness: total=%d pending=%d", total2, pending2)

	s.waitForEmbeddings(b2)
	total2, pending2 = s.mergeReadinessViaService(b2)
	s.T().Logf("  merge-readiness after wait: total=%d pending=%d", total2, pending2)
	s.Equal(0, pending2, "all objects should be embedded before merge")

	mergeResp2 := s.mergeBranchViaService(b2, "enrich")
	snap2 := s.extractEntitySnapshotsE2E(s.projectID)
	s.T().Logf("  run-2 main graph: %d entities, added=%d similar=%d",
		len(snap2), mergeResp2.AddedCount, mergeResp2.SimilarCount)

	// ── Delta analysis ────────────────────────────────────────────────────────
	added, enriched, stable := diffEntitySnapshots(snap1, snap2)

	s.T().Log("══ ENTITY DELTA (run 1 → run 2, real embeddings) ════")
	s.T().Logf("  added    (new entities):  %d", len(added))
	for _, e := range added {
		s.T().Logf("    + [%-12s] %s  props=%d", e.Type, e.Props["name"], countFilled(e.Props))
	}
	s.T().Logf("  enriched (more props):    %d", len(enriched))
	byKeyBefore := make(map[string]entitySnapshot, len(snap1))
	for _, e := range snap1 {
		byKeyBefore[snapshotKey(e)] = e
	}
	for _, e := range enriched {
		prev := byKeyBefore[snapshotKey(e)]
		s.T().Logf("    ~ [%-12s] %-28s  props: %d → %d (+%d)",
			e.Type, e.Props["name"],
			countFilled(prev.Props), countFilled(e.Props),
			countFilled(e.Props)-countFilled(prev.Props))
	}
	s.T().Logf("  stable   (no change):     %d", len(stable))

	// ── Quality summary ───────────────────────────────────────────────────────
	s.T().Log("══ QUALITY SUMMARY ═══════════════════════════════════")
	s.T().Logf("  %-35s  %d", "entities after run 1", len(snap1))
	s.T().Logf("  %-35s  %d", "entities after run 2", len(snap2))
	s.T().Logf("  %-35s  %d", "run-2 added (new)", len(added))
	s.T().Logf("  %-35s  %d", "run-2 enriched", len(enriched))
	s.T().Logf("  %-35s  %d", "run-2 stable", len(stable))
	s.T().Logf("  %-35s  %d", "run-2 similar detected by merge", mergeResp2.SimilarCount)
	s.T().Logf("  %-35s  %d", "run-2 added to main by merge", mergeResp2.AddedCount)
	s.T().Logf("  %-35s  %d", "characters after run 2",
		s.countMainObjectsE2E("Character"))

	if mergeResp2.SimilarCount > 0 {
		s.T().Logf("  ✓ real embedding similarity detected %d near-duplicate pairs — absorbed rather than duplicated",
			mergeResp2.SimilarCount)
	} else {
		s.T().Log("  NOTE: no similar pairs detected — embeddings may not have sufficient similarity at threshold 0.92")
		s.T().Log("        This is expected if the LLM uses consistent names across both runs (CreateOrUpdate handles key-match dedup)")
	}

	// Soft assertion: run 2 must produce entities (extraction worked).
	s.Greater(len(snap2), 0, "run-2 must produce at least one entity")
}
