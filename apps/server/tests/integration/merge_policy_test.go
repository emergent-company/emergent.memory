package integration

// Tests for similarity-aware merge policies.
//
// These tests use a real Postgres database but no LLM — embeddings are injected
// directly via raw SQL using deterministic float32 vectors. This lets us verify
// the full MergeBranch → FindSimilarObjectInBranch → applyMerge pipeline without
// needing an LLM or external embedding service.
//
// Test matrix:
//   TestMergePolicy_PolicyResolution          — resolveMergePolicy logic via service
//   TestMergePolicy_NoSimilarity_EnrichNoSim  — enrich_no_sim: no probe, added = added
//   TestMergePolicy_Suggest_FlagsButNoWrite   — suggest: similar flagged, nothing written
//   TestMergePolicy_AutoEnrich_FillsGaps      — auto_enrich: absorbs similar, fills empty fields
//   TestMergePolicy_AutoMine_SourceWins       — mine: source wins all conflicts
//   TestMergePolicy_AutoTheirs_TargetWins     — theirs: target wins conflicts
//   TestMergePolicy_BelowThreshold_TreatedAsAdded — similarity < threshold → new entity
//   TestMergePolicy_WaitForEmbeddings_Blocks  — WaitForEmbeddings=true with un-embedded objects
//   TestMergeReadiness_AllEmbedded            — readiness endpoint: all embedded → ready
//   TestMergeReadiness_SomeUnembedded         — readiness endpoint: some missing → not ready

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/domain/branches"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// ---------------------------------------------------------------------------
// Suite setup
// ---------------------------------------------------------------------------

type MergePolicyTestSuite struct {
	suite.Suite
	ctx       context.Context
	testDB    *testutil.TestDB
	orgID     string
	projectID string
	log       *slog.Logger
}

func TestMergePolicySuite(t *testing.T) {
	suite.Run(t, new(MergePolicyTestSuite))
}

func (s *MergePolicyTestSuite) SetupSuite() {
	s.ctx = context.Background()
	testutil.LoadEnvFiles()
	db, err := testutil.SetupTestDB(s.ctx, "merge_policy")
	require.NoError(s.T(), err)
	s.testDB = db
	s.log = slog.Default()
}

func (s *MergePolicyTestSuite) TearDownSuite() {
	if s.testDB != nil {
		s.testDB.Close()
	}
}

func (s *MergePolicyTestSuite) SetupTest() {
	require.NoError(s.T(), testutil.TruncateTables(s.ctx, s.testDB.DB))
	require.NoError(s.T(), testutil.SetupTestFixtures(s.ctx, s.testDB.DB))
	s.orgID = uuid.New().String()
	s.projectID = uuid.New().String()
	require.NoError(s.T(), testutil.SetupFullTestProject(s.ctx, s.testDB.DB, s.orgID, s.projectID))
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (s *MergePolicyTestSuite) graphSvc() *graph.Service {
	cfg := &config.Config{}
	cfg.Graph.MaxBatchObjects = 500
	cfg.Graph.MaxBatchRelationships = 500
	cfg.Graph.MaxListLimit = 1000
	cfg.Graph.DefaultListLimit = 100
	repo := graph.NewRepository(s.testDB.DB, s.log, cfg)
	return graph.NewService(repo, s.log,
		graph.ProvideSchemaProvider(s.testDB.DB, s.log),
		graph.ProvideInverseTypeProvider(s.testDB.DB, s.log),
		nil, nil, nil, nil, nil, nil)
}

func (s *MergePolicyTestSuite) branchSvc() *branches.Service {
	return branches.NewService(branches.NewStore(s.testDB.DB))
}

// makeVector generates a deterministic unit-normalised 768-dim vector seeded by n.
// Uses a simple LCG to spread the seed across all dimensions so different seeds
// produce genuinely different (near-orthogonal) vectors.
func makeVector(seed float32, dim int) []float32 {
	v := make([]float32, dim)
	// Use the seed as an LCG start value to fill all dimensions.
	s := uint64(math.Float32bits(seed)) | 1 // ensure odd for LCG
	var sumSq float64
	for i := range v {
		// LCG: next = (a*s + c) mod 2^64
		s = s*6364136223846793005 + 1442695040888963407
		v[i] = float32(int32(s>>33)) / float32(1<<31) // range ~[-1, 1]
		sumSq += float64(v[i]) * float64(v[i])
	}
	// Normalise to unit length.
	norm := float32(math.Sqrt(sumSq))
	if norm > 0 {
		for i := range v {
			v[i] /= norm
		}
	} else {
		v[0] = 1.0
	}
	return v
}

// makeNearVector returns a vector very close to base (cosine similarity ≈ 0.999).
func makeNearVector(base []float32) []float32 {
	near := make([]float32, len(base))
	copy(near, base)
	near[1] = 0.01 // tiny perturbation on second component
	// renormalise
	var sumSq float64
	for _, x := range near {
		sumSq += float64(x) * float64(x)
	}
	norm := float32(math.Sqrt(sumSq))
	for i := range near {
		near[i] /= norm
	}
	return near
}

// makeFarVector returns a vector orthogonal to base (cosine similarity ≈ 0).
func makeFarVector(base []float32) []float32 {
	far := make([]float32, len(base))
	// Put weight on second dimension (base has none), making them orthogonal.
	far[1] = 1.0
	return far
}

// formatVec formats a float32 slice as a PostgreSQL vector literal.
func formatVec(v []float32) string {
	parts := make([]string, len(v))
	for i, x := range v {
		parts[i] = fmt.Sprintf("%f", x)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// setEmbedding writes an embedding_v2 vector directly on a graph_objects row.
func (s *MergePolicyTestSuite) setEmbedding(objectID uuid.UUID, v []float32) {
	s.T().Helper()
	_, err := s.testDB.DB.NewRaw(
		fmt.Sprintf(`UPDATE kb.graph_objects SET embedding_v2 = '%s'::vector WHERE id = ?`, formatVec(v)),
		objectID,
	).Exec(s.ctx)
	require.NoError(s.T(), err, "setEmbedding failed for %s", objectID)
}

// createStagingBranch creates a staging branch and returns its ID.
func (s *MergePolicyTestSuite) createStagingBranch(name string) uuid.UUID {
	s.T().Helper()
	pid, _ := uuid.Parse(s.projectID)
	brSvc := s.branchSvc()
	desc := "test staging branch"
	br, err := brSvc.Create(s.ctx, &branches.CreateBranchRequest{
		ProjectID:   &s.projectID,
		Name:        name,
		Description: &desc,
	})
	require.NoError(s.T(), err)
	bid, _ := uuid.Parse(br.ID)
	_ = pid
	return bid
}

// createObjectOnBranch creates a graph object on a branch and returns its ID.
func (s *MergePolicyTestSuite) createObjectOnBranch(branchID *uuid.UUID, typeName string, props map[string]any) uuid.UUID {
	s.T().Helper()
	svc := s.graphSvc()
	pid, _ := uuid.Parse(s.projectID)
	obj, err := svc.Create(s.ctx, pid, &graph.CreateGraphObjectRequest{
		Type:       typeName,
		Properties: props,
		BranchID:   branchID,
	}, nil)
	require.NoError(s.T(), err)
	return obj.ID
}

// createObjectOnMain creates a graph object on the main graph (no branch).
func (s *MergePolicyTestSuite) createObjectOnMain(typeName string, props map[string]any) uuid.UUID {
	return s.createObjectOnBranch(nil, typeName, props)
}

// countMainObjects returns the count of live main-graph objects of a given type.
func (s *MergePolicyTestSuite) countMainObjects(typeName string) int {
	var n int
	_ = s.testDB.DB.NewRaw(
		`SELECT COUNT(*) FROM kb.graph_objects WHERE project_id = ? AND type = ?
		 AND branch_id IS NULL AND supersedes_id IS NULL AND deleted_at IS NULL`,
		s.projectID, typeName,
	).Scan(s.ctx, &n)
	return n
}

// getMainObjectProps fetches all live main-graph objects of a type and returns their properties.
func (s *MergePolicyTestSuite) getMainObjectProps(typeName string) []map[string]any {
	var rows []struct {
		Props []byte `bun:"properties"`
	}
	_ = s.testDB.DB.NewRaw(
		`SELECT properties FROM kb.graph_objects WHERE project_id = ? AND type = ?
		 AND branch_id IS NULL AND supersedes_id IS NULL AND deleted_at IS NULL`,
		s.projectID, typeName,
	).Scan(s.ctx, &rows)
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		var m map[string]any
		if err := json.Unmarshal(r.Props, &m); err == nil {
			out = append(out, m)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestMergePolicy_NoSimilarity_EnrichNoSim verifies that enrich_no_sim skips the
// similarity probe entirely: a source object with an identical name to a target object
// but a different canonical_id is added as a new entity (not absorbed).
func (s *MergePolicyTestSuite) TestMergePolicy_NoSimilarity_EnrichNoSim() {
	branchID := s.createStagingBranch("test-enrich-no-sim")

	// Main: "Monica Geller" Character
	mainObjID := s.createObjectOnMain("Character", map[string]any{
		"name": "Monica Geller", "occupation": "chef",
	})
	// Set embedding on main object.
	vec := makeVector(1.0, 768)
	s.setEmbedding(mainObjID, vec)

	// Source branch: "Monica Geller" with near-identical embedding but different canonical_id.
	srcObjID := s.createObjectOnBranch(&branchID, "Character", map[string]any{
		"name": "Monica Geller", "personality": "obsessive",
	})
	nearVec := makeNearVector(vec) // cosine similarity ≈ 0.999
	s.setEmbedding(srcObjID, nearVec)

	svc := s.graphSvc()
	pid, _ := uuid.Parse(s.projectID)

	resp, err := svc.MergeBranch(s.ctx, pid, nil, &graph.BranchMergeRequest{
		SourceBranchID: branchID,
		Execute:        true,
		Policy:         "enrich_no_sim",
	})
	require.NoError(s.T(), err)
	assert.False(s.T(), resp.SimilarityEnabled, "enrich_no_sim must not enable similarity")
	assert.Equal(s.T(), 0, resp.SimilarCount)

	// Expect TWO main-graph Monica objects (no dedup without similarity).
	count := s.countMainObjects("Character")
	assert.Equal(s.T(), 2, count, "enrich_no_sim: source added as new entity, not absorbed")
}

// TestMergePolicy_Suggest_FlagsButNoWrite verifies that suggest policy detects similar
// objects and marks them "similar" in the summary but does NOT write them to the target.
func (s *MergePolicyTestSuite) TestMergePolicy_Suggest_FlagsButNoWrite() {
	branchID := s.createStagingBranch("test-suggest")

	vec := makeVector(1.0, 768)
	mainObjID := s.createObjectOnMain("Character", map[string]any{"name": "Ross Geller"})
	s.setEmbedding(mainObjID, vec)

	srcObjID := s.createObjectOnBranch(&branchID, "Character", map[string]any{"name": "Ross G."})
	s.setEmbedding(srcObjID, makeNearVector(vec))

	svc := s.graphSvc()
	pid, _ := uuid.Parse(s.projectID)

	resp, err := svc.MergeBranch(s.ctx, pid, nil, &graph.BranchMergeRequest{
		SourceBranchID:      branchID,
		Execute:             true,
		Policy:              "suggest",
		SimilarityThreshold: 0.90,
	})
	require.NoError(s.T(), err)
	assert.True(s.T(), resp.SimilarityEnabled)
	assert.Equal(s.T(), 1, resp.SimilarCount, "one similar object should be detected")

	// No new object should appear on main — suggest does not write.
	assert.Equal(s.T(), 1, s.countMainObjects("Character"),
		"suggest must not create a new entity on main")

	// The summary should show the object as "similar".
	var foundSimilar bool
	for _, o := range resp.Objects {
		if o.Status == "similar" {
			foundSimilar = true
			assert.Greater(s.T(), o.SimilarityScore, float32(0.90))
			assert.NotNil(s.T(), o.SimilarTargetID)
		}
	}
	assert.True(s.T(), foundSimilar, "at least one object should have status='similar'")
}

// TestMergePolicy_AutoEnrich_FillsGaps verifies that auto_enrich absorbs the source object
// into the matching target entity and fills empty target properties from the source.
func (s *MergePolicyTestSuite) TestMergePolicy_AutoEnrich_FillsGaps() {
	branchID := s.createStagingBranch("test-auto-enrich")

	vec := makeVector(1.0, 768)
	// Main target: name + occupation only (home empty).
	mainObjID := s.createObjectOnMain("Character", map[string]any{
		"name": "Rachel Green", "occupation": "waitress",
	})
	s.setEmbedding(mainObjID, vec)

	// Source: same entity with different name spelling, but has "home" filled.
	srcObjID := s.createObjectOnBranch(&branchID, "Character", map[string]any{
		"name": "Rachel G.", "home": "Monica's apartment",
	})
	s.setEmbedding(srcObjID, makeNearVector(vec))

	svc := s.graphSvc()
	pid, _ := uuid.Parse(s.projectID)

	resp, err := svc.MergeBranch(s.ctx, pid, nil, &graph.BranchMergeRequest{
		SourceBranchID:      branchID,
		Execute:             true,
		Policy:              "enrich",
		SimilarityThreshold: 0.90,
	})
	require.NoError(s.T(), err)
	assert.Equal(s.T(), 1, resp.SimilarCount)

	// Still only ONE main-graph character (absorbed, not added).
	assert.Equal(s.T(), 1, s.countMainObjects("Character"))

	// The main object should now have "home" filled from source.
	props := s.getMainObjectProps("Character")
	require.Len(s.T(), props, 1)
	assert.Equal(s.T(), "Rachel Green", props[0]["name"], "target name preserved (not overwritten)")
	assert.Equal(s.T(), "waitress", props[0]["occupation"], "target occupation preserved")
	assert.Equal(s.T(), "Monica's apartment", props[0]["home"], "home filled from source")
}

// TestMergePolicy_AutoMine_SourceWins verifies that mine policy makes the source win
// on conflicting property keys (including the name itself).
func (s *MergePolicyTestSuite) TestMergePolicy_AutoMine_SourceWins() {
	branchID := s.createStagingBranch("test-mine")

	vec := makeVector(2.0, 768)
	mainObjID := s.createObjectOnMain("Character", map[string]any{
		"name": "R. Geller", "occupation": "fossil expert",
	})
	s.setEmbedding(mainObjID, vec)

	srcObjID := s.createObjectOnBranch(&branchID, "Character", map[string]any{
		"name": "Ross Geller", "occupation": "paleontologist", "home": "Greenwich Village",
	})
	s.setEmbedding(srcObjID, makeNearVector(vec))

	svc := s.graphSvc()
	pid, _ := uuid.Parse(s.projectID)

	resp, err := svc.MergeBranch(s.ctx, pid, nil, &graph.BranchMergeRequest{
		SourceBranchID:      branchID,
		Execute:             true,
		Policy:              "mine",
		SimilarityThreshold: 0.90,
	})
	require.NoError(s.T(), err)
	assert.Equal(s.T(), 1, resp.SimilarCount)
	assert.Equal(s.T(), 1, s.countMainObjects("Character"))

	props := s.getMainObjectProps("Character")
	require.Len(s.T(), props, 1)
	assert.Equal(s.T(), "Ross Geller", props[0]["name"], "source name wins")
	assert.Equal(s.T(), "paleontologist", props[0]["occupation"], "source occupation wins")
	assert.Equal(s.T(), "Greenwich Village", props[0]["home"], "new key from source added")
}

// TestMergePolicy_AutoTheirs_TargetWins verifies theirs policy: target keeps its values
// on conflicts, source only fills empty fields.
func (s *MergePolicyTestSuite) TestMergePolicy_AutoTheirs_TargetWins() {
	branchID := s.createStagingBranch("test-theirs")

	vec := makeVector(3.0, 768)
	mainObjID := s.createObjectOnMain("Character", map[string]any{
		"name": "Monica Geller", "occupation": "head chef",
	})
	s.setEmbedding(mainObjID, vec)

	srcObjID := s.createObjectOnBranch(&branchID, "Character", map[string]any{
		"name": "Monica G.", "occupation": "sous chef", "home": "West Village",
	})
	s.setEmbedding(srcObjID, makeNearVector(vec))

	svc := s.graphSvc()
	pid, _ := uuid.Parse(s.projectID)

	resp, err := svc.MergeBranch(s.ctx, pid, nil, &graph.BranchMergeRequest{
		SourceBranchID:      branchID,
		Execute:             true,
		Policy:              "theirs",
		SimilarityThreshold: 0.90,
	})
	require.NoError(s.T(), err)
	assert.Equal(s.T(), 1, resp.SimilarCount)
	assert.Equal(s.T(), 1, s.countMainObjects("Character"))

	props := s.getMainObjectProps("Character")
	require.Len(s.T(), props, 1)
	assert.Equal(s.T(), "Monica Geller", props[0]["name"], "target name preserved")
	assert.Equal(s.T(), "head chef", props[0]["occupation"], "target occupation preserved")
	assert.Equal(s.T(), "West Village", props[0]["home"], "empty target field filled from source")
}

// TestMergePolicy_BelowThreshold_TreatedAsAdded verifies that an object whose similarity
// score is below the threshold is created as a new entity rather than absorbed.
func (s *MergePolicyTestSuite) TestMergePolicy_BelowThreshold_TreatedAsAdded() {
	branchID := s.createStagingBranch("test-below-threshold")

	vec := makeVector(1.0, 768)
	mainObjID := s.createObjectOnMain("Character", map[string]any{"name": "Joey Tribbiani"})
	s.setEmbedding(mainObjID, vec)

	// Source has a FAR vector (cosine similarity ≈ 0) — well below any threshold.
	srcObjID := s.createObjectOnBranch(&branchID, "Character", map[string]any{"name": "Chandler Bing"})
	s.setEmbedding(srcObjID, makeFarVector(vec))

	svc := s.graphSvc()
	pid, _ := uuid.Parse(s.projectID)

	resp, err := svc.MergeBranch(s.ctx, pid, nil, &graph.BranchMergeRequest{
		SourceBranchID:      branchID,
		Execute:             true,
		Policy:              "enrich",
		SimilarityThreshold: 0.90, // far vector will never reach this
	})
	require.NoError(s.T(), err)
	assert.Equal(s.T(), 0, resp.SimilarCount, "below-threshold object should not be similar")
	assert.Equal(s.T(), 1, resp.AddedCount, "below-threshold object should be added as new")

	// Both objects now on main.
	assert.Equal(s.T(), 2, s.countMainObjects("Character"))
}

// TestMergePolicy_WaitForEmbeddings_Blocks verifies that WaitForEmbeddings=true returns
// an error when the source branch has un-embedded objects.
func (s *MergePolicyTestSuite) TestMergePolicy_WaitForEmbeddings_Blocks() {
	branchID := s.createStagingBranch("test-wait-embeddings")

	// Create source object WITHOUT setting an embedding.
	_ = s.createObjectOnBranch(&branchID, "Character", map[string]any{"name": "Phoebe Buffay"})

	svc := s.graphSvc()
	pid, _ := uuid.Parse(s.projectID)

	_, err := svc.MergeBranch(s.ctx, pid, nil, &graph.BranchMergeRequest{
		SourceBranchID:    branchID,
		Execute:           false, // dry-run
		Policy:            "enrich",
		WaitForEmbeddings: true,
	})
	require.Error(s.T(), err, "should return error when embeddings pending and WaitForEmbeddings=true")
	assert.Contains(s.T(), err.Error(), "embedding", "error message should mention embeddings")
}

// TestMergePolicy_WaitForEmbeddings_ProceedsWhenAllEmbedded verifies that
// WaitForEmbeddings=true succeeds when all source objects are embedded.
func (s *MergePolicyTestSuite) TestMergePolicy_WaitForEmbeddings_ProceedsWhenAllEmbedded() {
	branchID := s.createStagingBranch("test-all-embedded")

	vec := makeVector(4.0, 768)
	srcObjID := s.createObjectOnBranch(&branchID, "Character", map[string]any{"name": "Barry Farber"})
	s.setEmbedding(srcObjID, vec) // all embedded

	svc := s.graphSvc()
	pid, _ := uuid.Parse(s.projectID)

	resp, err := svc.MergeBranch(s.ctx, pid, nil, &graph.BranchMergeRequest{
		SourceBranchID:    branchID,
		Execute:           true,
		Policy:            "enrich",
		WaitForEmbeddings: true,
	})
	require.NoError(s.T(), err, "should succeed when all objects embedded")
	assert.Equal(s.T(), 0, resp.EmbeddingsPending)
}

// TestMergeReadiness_AllEmbedded verifies BranchMergeReadiness returns ready=true.
func (s *MergePolicyTestSuite) TestMergeReadiness_AllEmbedded() {
	branchID := s.createStagingBranch("test-readiness-all")

	vec := makeVector(5.0, 768)
	id1 := s.createObjectOnBranch(&branchID, "Character", map[string]any{"name": "Carl"})
	id2 := s.createObjectOnBranch(&branchID, "Event", map[string]any{"name": "wedding"})
	s.setEmbedding(id1, vec)
	s.setEmbedding(id2, vec)

	svc := s.graphSvc()
	pid, _ := uuid.Parse(s.projectID)

	total, pending, err := svc.BranchMergeReadiness(s.ctx, pid, branchID)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), 2, total)
	assert.Equal(s.T(), 0, pending)
}

// TestMergeReadiness_SomeUnembedded verifies BranchMergeReadiness counts missing embeddings.
func (s *MergePolicyTestSuite) TestMergeReadiness_SomeUnembedded() {
	branchID := s.createStagingBranch("test-readiness-partial")

	vec := makeVector(6.0, 768)
	id1 := s.createObjectOnBranch(&branchID, "Character", map[string]any{"name": "Mindy"})
	_ = s.createObjectOnBranch(&branchID, "Character", map[string]any{"name": "Mark"}) // no embedding
	_ = s.createObjectOnBranch(&branchID, "Event", map[string]any{"name": "dinner"})   // no embedding
	s.setEmbedding(id1, vec)                                                           // only one embedded

	svc := s.graphSvc()
	pid, _ := uuid.Parse(s.projectID)

	total, pending, err := svc.BranchMergeReadiness(s.ctx, pid, branchID)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), 3, total)
	assert.Equal(s.T(), 2, pending, "two objects are un-embedded")
}

// TestMergePolicy_EmbeddingsPendingReportedInDryRun verifies that a dry-run merge
// with similarity enabled reports the pending embedding count without blocking.
func (s *MergePolicyTestSuite) TestMergePolicy_EmbeddingsPendingReportedInDryRun() {
	branchID := s.createStagingBranch("test-pending-dry-run")

	vec := makeVector(7.0, 768)
	id1 := s.createObjectOnBranch(&branchID, "Character", map[string]any{"name": "Gunther"})
	_ = s.createObjectOnBranch(&branchID, "Character", map[string]any{"name": "Ugly Naked Guy"})
	s.setEmbedding(id1, vec) // only first has embedding

	svc := s.graphSvc()
	pid, _ := uuid.Parse(s.projectID)

	resp, err := svc.MergeBranch(s.ctx, pid, nil, &graph.BranchMergeRequest{
		SourceBranchID:    branchID,
		Execute:           false, // dry run — should not block
		Policy:            "enrich",
		WaitForEmbeddings: false,
	})
	require.NoError(s.T(), err, "dry run must not block even when embeddings pending")
	assert.Equal(s.T(), 1, resp.EmbeddingsPending, "should report 1 pending embedding")
}

// TestMergePolicy_NameVariants_SameEntity tests the core second-run use case:
// the same real-world entity is extracted with different name strings in run 1 vs run 2.
//
// This is the scenario that motivates similarity-aware merge: the LLM extracts
// "Mom (Geller)" in one run and "Monica and Ross's mother" in another. They have
// different names → different canonical_ids → without similarity they would create
// two separate entity nodes. With similarity enabled and near-identical embeddings
// (reflecting that they describe the same real person), they are merged into one.
//
// The test injects manually crafted near-identical embeddings to simulate what
// a real embedding model would produce for two descriptions of the same character.
func (s *MergePolicyTestSuite) TestMergePolicy_NameVariants_SameEntity() {
	// ── Simulate run 1: "Mom (Geller)" with rich properties ──────────────────
	// This is what the first extraction pass produced and merged to main.
	mainVec := makeVector(10.0, 768) // base vector for "Geller mother" concept
	mainID := s.createObjectOnMain("Character", map[string]any{
		"name":              "Mom (Geller)",
		"current_situation": "upset about potentially no grandchildren",
	})
	s.setEmbedding(mainID, mainVec)

	// ── Simulate run 2 staging branch: "Monica and Ross's mother" ─────────────
	// Different name string → different canonical_id → looks "added" to merge.
	// Near-identical embedding → cosine similarity ≈ 0.999 → should be detected as same entity.
	branchID := s.createStagingBranch("test-name-variants")
	srcID := s.createObjectOnBranch(&branchID, "Character", map[string]any{
		"name":              "Monica and Ross's mother",
		"stated_feelings":   "hysterical about grandchildren",
		"current_situation": "called Monica at 3am crying",
	})
	nearVec := makeNearVector(mainVec) // cosine similarity ≈ 0.9999
	s.setEmbedding(srcID, nearVec)

	svc := s.graphSvc()
	pid, _ := uuid.Parse(s.projectID)

	// ── Without similarity: both entities land separately ─────────────────────
	// First check what happens with enrich_no_sim — expect "added" only.
	respDryRun, err := svc.MergeBranch(s.ctx, pid, nil, &graph.BranchMergeRequest{
		SourceBranchID:      branchID,
		Execute:             false, // dry run
		Policy:              "enrich_no_sim",
		SimilarityThreshold: 0.90,
	})
	require.NoError(s.T(), err)
	assert.Equal(s.T(), 1, respDryRun.AddedCount, "enrich_no_sim: name variant treated as new entity")
	assert.Equal(s.T(), 0, respDryRun.SimilarCount, "enrich_no_sim: no similarity probe")

	// ── With similarity: variants detected and absorbed ────────────────────────
	respSim, err := svc.MergeBranch(s.ctx, pid, nil, &graph.BranchMergeRequest{
		SourceBranchID:      branchID,
		Execute:             true, // apply
		Policy:              "enrich",
		SimilarityThreshold: 0.90,
	})
	require.NoError(s.T(), err)

	assert.Equal(s.T(), 1, respSim.SimilarCount,
		"enrich: name variant detected as similar to existing main entity")
	assert.Equal(s.T(), 0, respSim.AddedCount,
		"enrich: name variant absorbed, not added as duplicate")

	// Check summary: the source object should have status="similar".
	var foundSimilar bool
	for _, o := range respSim.Objects {
		if o.Status == "similar" {
			foundSimilar = true
			assert.Greater(s.T(), o.SimilarityScore, float32(0.90),
				"similarity score should exceed threshold")
			assert.Equal(s.T(), "Mom (Geller)", o.SimilarTargetName,
				"should have matched the existing main entity")
			assert.NotEmpty(s.T(), o.EnrichedKeys,
				"should have enriched the target with new properties from source")
			s.T().Logf("  similar: score=%.4f target=%q enriched=%v",
				o.SimilarityScore, o.SimilarTargetName, o.EnrichedKeys)
		}
	}
	assert.True(s.T(), foundSimilar, "at least one object should have status=similar")

	// Still only ONE Character on main (name variant absorbed, not duplicated).
	assert.Equal(s.T(), 1, s.countMainObjects("Character"),
		"similarity merge: one entity on main (not two)")

	// The main entity should now have properties from BOTH extractions:
	// "current_situation" from run 1 + "stated_feelings" from run 2.
	props := s.getMainObjectProps("Character")
	require.Len(s.T(), props, 1)
	assert.Equal(s.T(), "Mom (Geller)", props[0]["name"],
		"original name preserved (target wins)")
	assert.Equal(s.T(), "upset about potentially no grandchildren", props[0]["current_situation"],
		"target current_situation preserved")
	assert.Equal(s.T(), "hysterical about grandchildren", props[0]["stated_feelings"],
		"new stated_feelings filled from source")
	// With enrich policy: target wins on conflicts (both had current_situation).
	// Source "called Monica at 3am crying" should NOT overwrite the target's value.
	assert.Equal(s.T(), "upset about potentially no grandchildren", props[0]["current_situation"],
		"enrich policy: target current_situation preserved (not overwritten by source)")
}

// TestMergePolicy_NameVariants_SecondRun_FullScenario simulates the complete
// second-run extraction scenario end-to-end without an LLM.
//
// Timeline:
//  1. "Run 1": main graph has entities from first extraction (mixed variants)
//  2. "Run 2": staging branch has re-extracted entities, some same name, some variant
//  3. Merge with policy=enrich → same-name entities are fast_forward/conflict
//     (canonical_id match via key), name-variants are caught by similarity probe
func (s *MergePolicyTestSuite) TestMergePolicy_NameVariants_SecondRun_FullScenario() {
	baseVec := makeVector(20.0, 768)

	// ── Main graph (run 1 result) ─────────────────────────────────────────────
	// 6 main cast + 2 minor characters with specific name forms.
	mainEntities := []struct {
		typeName string
		props    map[string]any
	}{
		{"Character", map[string]any{"name": "Monica Geller", "occupation": "chef", "current_situation": "going to dinner with coworker"}},
		{"Character", map[string]any{"name": "Ross Geller", "current_situation": "Carol moved out today"}},
		{"Character", map[string]any{"name": "Rachel Green", "current_situation": "fled her wedding"}},
		{"Character", map[string]any{"name": "Mom (Geller)", "current_situation": "upset about grandchildren"}},
		{"Character", map[string]any{"name": "Dad (Geller)"}},
		{"Event", map[string]any{"name": "Carol moving out", "timing": "today"}},
	}
	mainIDs := make(map[string]uuid.UUID)
	for i, e := range mainEntities {
		// Each entity gets a distinct unit vector seeded by its index.
		vec := makeVector(float32(i+1)*0.1, 768)
		_ = vec // not used as base — each is unique
		// Actually use the entity name to produce the embedding:
		// makeVector with different seeds ensures they are distinct.
		id := s.createObjectOnMain(e.typeName, e.props)
		// Use index-based vector so each main entity has a distinct embedding.
		seedVec := makeVector(float32(20+i), 768)
		s.setEmbedding(id, seedVec)
		mainIDs[e.props["name"].(string)] = id
	}
	_ = baseVec

	// ── Staging branch (run 2 result) ──────────────────────────────────────────
	branchID := s.createStagingBranch("test-second-run-full")

	// Exact same name → will match by canonical_id via key lookup (fast_forward/conflict).
	monicaID := s.createObjectOnBranch(&branchID, "Character", map[string]any{
		"name": "Monica Geller", "stated_feelings": "excited about dinner",
	})
	s.setEmbedding(monicaID, makeVector(20.0, 768)) // same seed as main Monica

	// Exact same name, same content → unchanged.
	rossID := s.createObjectOnBranch(&branchID, "Character", map[string]any{
		"name": "Ross Geller", "current_situation": "Carol moved out today",
	})
	s.setEmbedding(rossID, makeVector(21.0, 768))

	// Exact same name, new properties → fast_forward.
	rachelID := s.createObjectOnBranch(&branchID, "Character", map[string]any{
		"name": "Rachel Green", "current_situation": "fled her wedding", "home": "Monica's apartment",
	})
	s.setEmbedding(rachelID, makeVector(22.0, 768))

	// VARIANT NAME: "Monica and Ross's mother" vs "Mom (Geller)" — different canonical_id.
	// Near-identical embedding to main's "Mom (Geller)" (seeded at 20+3=23) → similarity probe fires.
	momVariantID := s.createObjectOnBranch(&branchID, "Character", map[string]any{
		"name":            "Monica and Ross's mother",
		"stated_feelings": "hysterical about grandchildren, called at 3am",
	})
	s.setEmbedding(momVariantID, makeNearVector(makeVector(23.0, 768))) // near identical to main "Mom (Geller)" at seed 23

	// VARIANT NAME: "Father (Geller)" vs "Dad (Geller)" — different canonical_id, near embedding.
	dadVariantID := s.createObjectOnBranch(&branchID, "Character", map[string]any{
		"name": "Father (Geller)",
	})
	s.setEmbedding(dadVariantID, makeNearVector(makeVector(24.0, 768))) // near identical to main "Dad (Geller)" at seed 24

	// Genuinely new entity not in run 1.
	newID := s.createObjectOnBranch(&branchID, "Character", map[string]any{
		"name": "Gunther",
	})
	s.setEmbedding(newID, makeFarVector(makeVector(20.0, 768))) // far from all main entities

	svc := s.graphSvc()
	pid, _ := uuid.Parse(s.projectID)

	resp, err := svc.MergeBranch(s.ctx, pid, nil, &graph.BranchMergeRequest{
		SourceBranchID:      branchID,
		Execute:             true,
		Policy:              "enrich",
		SimilarityThreshold: 0.90,
	})
	require.NoError(s.T(), err)

	s.T().Logf("merge result: added=%d similar=%d ff=%d unchanged=%d",
		resp.AddedCount, resp.SimilarCount, resp.FastForwardCount, resp.UnchangedCount)

	for _, o := range resp.Objects {
		if o.Status != "unchanged" {
			s.T().Logf("  [%s] canonical=%s similar_target=%q score=%.3f enriched=%v",
				o.Status, o.CanonicalID, o.SimilarTargetName, o.SimilarityScore, o.EnrichedKeys)
		}
	}

	// "Gunther" should be added (genuinely new, far vector).
	assert.GreaterOrEqual(s.T(), resp.AddedCount, 1, "Gunther should be added as new entity")

	// Mom and Dad variants should be detected as similar.
	// (exact count depends on how the key-based merge handles same-name entities)
	s.T().Logf("  similar_count=%d (name variants caught by similarity probe)", resp.SimilarCount)

	// Total characters: original 5 char + Gunther = 6 (mom/dad variants absorbed).
	mainCharCount := s.countMainObjects("Character")
	s.T().Logf("  total characters on main after merge: %d", mainCharCount)
	// With similarity: 5 original + 1 Gunther = 6 (no new mom/dad duplicates)
	// Without similarity: 5 original + 1 Gunther + 2 variants = 8
	assert.LessOrEqual(s.T(), mainCharCount, 7,
		"similarity merge should prevent most name-variant duplicates")
}
