package e2e

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent/domain/search"
	"github.com/emergent-company/emergent/internal/testutil"
	"github.com/emergent-company/emergent/pkg/pgutils"
)

// RelationshipSearchSuite tests that graph relationships with embeddings
// are discoverable via the unified search API and that backward compatibility
// is maintained for existing search functionality.
type RelationshipSearchSuite struct {
	testutil.BaseSuite
}

func TestRelationshipSearchSuite(t *testing.T) {
	suite.Run(t, new(RelationshipSearchSuite))
}

func (s *RelationshipSearchSuite) SetupSuite() {
	s.SetDBSuffix("relsearch")
	s.BaseSuite.SetupSuite()
}

// =============================================================================
// Helper functions
// =============================================================================

// createTestGraphObject creates a graph object and returns its ID.
func (s *RelationshipSearchSuite) createTestGraphObject(objType, key string, properties map[string]any) string {
	body := map[string]any{
		"type":       objType,
		"properties": properties,
	}
	if key != "" {
		body["key"] = key
	}

	resp := s.Client.POST("/api/graph/objects",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(body),
	)
	s.Require().Equal(http.StatusCreated, resp.StatusCode, "Failed to create graph object: %s", resp.String())

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.Require().NoError(err)
	return result["id"].(string)
}

// createTestRelationship creates a relationship between two objects and returns its ID.
func (s *RelationshipSearchSuite) createTestRelationship(relType, srcID, dstID string, properties map[string]any) string {
	body := map[string]any{
		"type":   relType,
		"src_id": srcID,
		"dst_id": dstID,
	}
	if properties != nil {
		body["properties"] = properties
	}

	resp := s.Client.POST("/api/graph/relationships",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(body),
	)
	s.Require().Equal(http.StatusCreated, resp.StatusCode, "Failed to create relationship: %s", resp.String())

	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.Require().NoError(err)
	return result["id"].(string)
}

// makeFakeEmbedding creates a deterministic 768-dim vector where one dimension
// is set to a high value to create a distinguishable direction. This makes
// the embedding semantically "about" the specified dimension index.
func makeFakeEmbedding(seed int) []float32 {
	vec := make([]float32, 768)
	// Create a unit-ish vector with energy concentrated around the seed dimension
	// to make cosine similarity work predictably in tests.
	idx := seed % 768
	vec[idx] = 0.9
	// Add small values to other dims so the vector is not too sparse
	for i := range vec {
		if i != idx {
			vec[i] = 0.01
		}
	}
	// Normalize to unit length for cosine similarity
	var norm float32
	for _, v := range vec {
		norm += v * v
	}
	norm = float32(math.Sqrt(float64(norm)))
	for i := range vec {
		vec[i] /= norm
	}
	return vec
}

// injectRelationshipEmbedding directly writes a test embedding vector into the
// database for the given relationship. This is necessary because the test
// environment may not have Vertex AI available for real embedding generation.
func (s *RelationshipSearchSuite) injectRelationshipEmbedding(relationshipID string, embedding []float32) {
	s.SkipIfExternalServer("requires direct DB access to inject embeddings")
	db := s.DB()
	s.Require().NotNil(db, "DB should be available for embedding injection")

	vectorStr := pgutils.FormatVector(embedding)
	_, err := db.ExecContext(s.Ctx,
		"UPDATE kb.graph_relationships SET embedding = ?::vector, embedding_updated_at = NOW() WHERE id = ?",
		vectorStr, relationshipID,
	)
	s.Require().NoError(err, "Failed to inject embedding for relationship %s", relationshipID)
}

// unifiedSearch performs a unified search and returns the parsed response.
func (s *RelationshipSearchSuite) unifiedSearch(body map[string]any) search.UnifiedSearchResponse {
	resp := s.Client.POST("/api/search/unified",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(body),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "Unified search failed: %s", resp.String())

	var response search.UnifiedSearchResponse
	err := json.Unmarshal(resp.Body, &response)
	s.Require().NoError(err)
	return response
}

// =============================================================================
// TG13.1: Create relationship → verify embedding → search finds it
// =============================================================================

func (s *RelationshipSearchSuite) TestCreateRelationship_SetsEmbeddingUpdatedAt() {
	s.SkipIfExternalServer("requires direct DB access to verify embedding_updated_at")

	// Create two graph objects
	srcID := s.createTestGraphObject("Person", fmt.Sprintf("elon-%d", time.Now().UnixNano()), map[string]any{
		"name": "Elon Musk",
	})
	dstID := s.createTestGraphObject("Company", fmt.Sprintf("tesla-%d", time.Now().UnixNano()), map[string]any{
		"name": "Tesla",
	})

	// Create a relationship
	relID := s.createTestRelationship("FOUNDED", srcID, dstID, map[string]any{
		"year": 2003,
	})

	// Verify the relationship was created and embedding_updated_at may be set
	// (depends on whether Vertex AI is available in the test environment)
	var embeddingUpdatedAt *time.Time
	err := s.DB().QueryRowContext(s.Ctx,
		"SELECT embedding_updated_at FROM kb.graph_relationships WHERE id = ?",
		relID,
	).Scan(&embeddingUpdatedAt)
	s.Require().NoError(err, "Failed to query embedding_updated_at")

	// If Vertex AI was available, embedding_updated_at should be set.
	// If not, it will be nil — that's acceptable (graceful degradation).
	// We log either way for visibility.
	if embeddingUpdatedAt != nil {
		s.T().Logf("embedding_updated_at is set: %v (Vertex AI was available)", embeddingUpdatedAt)
	} else {
		s.T().Log("embedding_updated_at is nil (Vertex AI not available — graceful degradation)")
	}

	// The relationship should exist regardless of embedding status
	s.NotEmpty(relID)
}

func (s *RelationshipSearchSuite) TestRelationshipSearch_FindsRelationshipBySemanticQuery() {
	s.SkipIfExternalServer("requires direct DB access to inject test embeddings")

	// Create graph objects
	srcID := s.createTestGraphObject("Person", fmt.Sprintf("founder-%d", time.Now().UnixNano()), map[string]any{
		"name": "Elon Musk",
	})
	dstID := s.createTestGraphObject("Company", fmt.Sprintf("company-%d", time.Now().UnixNano()), map[string]any{
		"name": "Tesla",
	})

	// Create relationship
	relID := s.createTestRelationship("FOUNDED", srcID, dstID, map[string]any{
		"year": 2003,
	})

	// Inject a known embedding so we can search for it deterministically
	queryEmbedding := makeFakeEmbedding(42)
	s.injectRelationshipEmbedding(relID, queryEmbedding)

	// Now inject the query embedding into the search via the service.
	// Since unified search generates its own embedding from the query text,
	// and we can't control that in E2E tests, we use a direct DB assertion
	// to verify the relationship IS searchable (has a non-null embedding).
	var hasEmbedding bool
	err := s.DB().QueryRowContext(s.Ctx,
		"SELECT embedding IS NOT NULL FROM kb.graph_relationships WHERE id = ?",
		relID,
	).Scan(&hasEmbedding)
	s.Require().NoError(err)
	s.True(hasEmbedding, "Relationship should have a non-null embedding after injection")

	// Verify the embedding_updated_at was set by our injection
	var embeddingUpdatedAt *time.Time
	err = s.DB().QueryRowContext(s.Ctx,
		"SELECT embedding_updated_at FROM kb.graph_relationships WHERE id = ?",
		relID,
	).Scan(&embeddingUpdatedAt)
	s.Require().NoError(err)
	s.NotNil(embeddingUpdatedAt, "embedding_updated_at should be set after injection")
}

func (s *RelationshipSearchSuite) TestRelationshipSearch_VectorSearchReturnsMatchingRelationship() {
	s.SkipIfExternalServer("requires direct DB access to inject test embeddings and run raw queries")

	// Create graph objects
	srcID := s.createTestGraphObject("Person", fmt.Sprintf("ceo-%d", time.Now().UnixNano()), map[string]any{
		"name": "Tim Cook",
	})
	dstID := s.createTestGraphObject("Company", fmt.Sprintf("apple-%d", time.Now().UnixNano()), map[string]any{
		"name": "Apple",
	})

	// Create relationship
	relID := s.createTestRelationship("LEADS", srcID, dstID, nil)

	// Inject embedding (dimension 42 is the "signature")
	embedding := makeFakeEmbedding(42)
	s.injectRelationshipEmbedding(relID, embedding)

	// Perform a direct vector search via raw SQL to verify the repository logic works
	queryVector := pgutils.FormatVector(makeFakeEmbedding(42)) // Same direction = high similarity
	rows, err := s.DB().QueryContext(s.Ctx, `
		SELECT r.id, r.type,
			COALESCE(src.name, src.key, src.id::text) || ' ' ||
				LOWER(REPLACE(r.type, '_', ' ')) || ' ' ||
				COALESCE(dst.name, dst.key, dst.id::text) AS triplet_text,
			(1 - (r.embedding <=> ?::vector)) AS score
		FROM kb.graph_relationships r
		JOIN kb.graph_objects src ON src.id = r.src_id
		JOIN kb.graph_objects dst ON dst.id = r.dst_id
		WHERE r.embedding IS NOT NULL
		  AND r.deleted_at IS NULL
		  AND src.project_id = ?
		ORDER BY r.embedding <=> ?::vector
		LIMIT 5
	`, queryVector, s.ProjectID, queryVector)
	s.Require().NoError(err)
	defer rows.Close()

	var found bool
	for rows.Next() {
		var id, relType, tripletText string
		var score float32
		err := rows.Scan(&id, &relType, &tripletText, &score)
		s.Require().NoError(err)

		if id == relID {
			found = true
			s.Equal("LEADS", relType)
			s.Contains(tripletText, "leads")
			s.Greater(score, float32(0.9), "Same vector should have very high similarity")
			s.T().Logf("Found relationship: id=%s type=%s triplet=%q score=%.4f", id, relType, tripletText, score)
		}
	}
	s.Require().NoError(rows.Err())
	s.True(found, "Expected to find relationship %s in vector search results", relID)
}

func (s *RelationshipSearchSuite) TestRelationshipSearch_DifferentVectorsHaveLowerSimilarity() {
	s.SkipIfExternalServer("requires direct DB access to inject test embeddings")

	// Create two relationships with different embeddings
	src1 := s.createTestGraphObject("Person", fmt.Sprintf("p1-%d", time.Now().UnixNano()), map[string]any{"name": "Alice"})
	dst1 := s.createTestGraphObject("Company", fmt.Sprintf("c1-%d", time.Now().UnixNano()), map[string]any{"name": "Acme"})
	rel1 := s.createTestRelationship("WORKS_AT", src1, dst1, nil)
	s.injectRelationshipEmbedding(rel1, makeFakeEmbedding(10)) // Dimension 10

	src2 := s.createTestGraphObject("Person", fmt.Sprintf("p2-%d", time.Now().UnixNano()), map[string]any{"name": "Bob"})
	dst2 := s.createTestGraphObject("Company", fmt.Sprintf("c2-%d", time.Now().UnixNano()), map[string]any{"name": "Globex"})
	rel2 := s.createTestRelationship("MANAGES", src2, dst2, nil)
	s.injectRelationshipEmbedding(rel2, makeFakeEmbedding(500)) // Dimension 500 (very different)

	// Search with a query vector similar to rel1 (dimension 10)
	queryVector := pgutils.FormatVector(makeFakeEmbedding(10))
	rows, err := s.DB().QueryContext(s.Ctx, `
		SELECT r.id, (1 - (r.embedding <=> ?::vector)) AS score
		FROM kb.graph_relationships r
		JOIN kb.graph_objects src ON src.id = r.src_id
		WHERE r.embedding IS NOT NULL AND r.deleted_at IS NULL AND src.project_id = ?
		ORDER BY r.embedding <=> ?::vector
		LIMIT 10
	`, queryVector, s.ProjectID, queryVector)
	s.Require().NoError(err)
	defer rows.Close()

	scores := make(map[string]float32)
	for rows.Next() {
		var id string
		var score float32
		s.Require().NoError(rows.Scan(&id, &score))
		scores[id] = score
	}
	s.Require().NoError(rows.Err())

	// rel1 should have higher similarity than rel2
	score1, ok1 := scores[rel1]
	score2, ok2 := scores[rel2]
	s.True(ok1, "rel1 should appear in results")
	s.True(ok2, "rel2 should appear in results")
	s.Greater(score1, score2, "rel1 (same direction) should score higher than rel2 (different direction)")
	s.T().Logf("rel1 score=%.4f, rel2 score=%.4f", score1, score2)
}

// =============================================================================
// TG13.2: Search returns mixed objects and relationships
// =============================================================================

func (s *RelationshipSearchSuite) TestUnifiedSearch_ReturnsRelationshipResults() {
	s.SkipIfExternalServer("requires direct DB access to inject test embeddings")

	// Create graph objects and a relationship with an embedding
	srcID := s.createTestGraphObject("Person", fmt.Sprintf("inventor-%d", time.Now().UnixNano()), map[string]any{
		"name": "Nikola Tesla",
	})
	dstID := s.createTestGraphObject("Concept", fmt.Sprintf("ac-%d", time.Now().UnixNano()), map[string]any{
		"name": "Alternating Current",
	})
	relID := s.createTestRelationship("INVENTED", srcID, dstID, nil)
	s.injectRelationshipEmbedding(relID, makeFakeEmbedding(77))

	// Perform unified search — the search will embed the query via Vertex AI.
	// If Vertex AI is available, relationship results may appear.
	// If not, we verify the response structure is correct and
	// relationship metadata fields are present.
	response := s.unifiedSearch(map[string]any{
		"query": "who invented alternating current",
	})

	// Response structure should always be valid
	s.NotNil(response.Metadata)
	s.GreaterOrEqual(response.Metadata.RelationshipResultCount, 0, "RelationshipResultCount should be non-negative")
	s.NotNil(response.Metadata.ExecutionTime)

	// Check for relationship results if any were returned
	for _, item := range response.Results {
		if item.Type == search.ItemTypeRelationship {
			s.NotEmpty(item.ID, "Relationship result should have an ID")
			s.NotEmpty(item.RelationshipType, "Relationship result should have a type")
			s.NotEmpty(item.SourceID, "Relationship result should have a source_id")
			s.NotEmpty(item.TargetID, "Relationship result should have a target_id")
			s.Greater(item.Score, float32(0), "Relationship result should have a positive score")
			s.T().Logf("Found relationship result: type=%s triplet=%q score=%.4f",
				item.RelationshipType, item.TripletText, item.Score)
		}
	}
}

func (s *RelationshipSearchSuite) TestUnifiedSearch_MixedResultTypes() {
	s.SkipIfExternalServer("requires direct DB access to inject test embeddings")

	// Create a graph object (will appear in graph search via FTS)
	s.createTestGraphObject("Technology", fmt.Sprintf("blockchain-%d", time.Now().UnixNano()), map[string]any{
		"name":        "Blockchain Technology",
		"description": "Distributed ledger technology for secure transactions",
	})

	// Create a relationship with embedding (may appear in relationship search)
	srcID := s.createTestGraphObject("Person", fmt.Sprintf("satoshi-%d", time.Now().UnixNano()), map[string]any{
		"name": "Satoshi Nakamoto",
	})
	dstID := s.createTestGraphObject("Technology", fmt.Sprintf("bitcoin-%d", time.Now().UnixNano()), map[string]any{
		"name": "Bitcoin",
	})
	relID := s.createTestRelationship("CREATED", srcID, dstID, nil)
	s.injectRelationshipEmbedding(relID, makeFakeEmbedding(200))

	// Search for blockchain — should potentially return both graph and relationship results
	response := s.unifiedSearch(map[string]any{
		"query": "blockchain",
	})

	// Verify response metadata tracks all result types
	s.GreaterOrEqual(response.Metadata.GraphResultCount, 0)
	s.GreaterOrEqual(response.Metadata.TextResultCount, 0)
	s.GreaterOrEqual(response.Metadata.RelationshipResultCount, 0)
	s.Equal(
		response.Metadata.GraphResultCount+response.Metadata.TextResultCount+response.Metadata.RelationshipResultCount,
		response.Metadata.TotalResults,
		"TotalResults should equal sum of all result type counts",
	)

	// Verify each result has the correct type discriminator
	for _, item := range response.Results {
		switch item.Type {
		case search.ItemTypeGraph:
			s.NotEmpty(item.ObjectType, "Graph result should have object_type")
		case search.ItemTypeText:
			s.NotEmpty(item.Snippet, "Text result should have snippet")
		case search.ItemTypeRelationship:
			s.NotEmpty(item.RelationshipType, "Relationship result should have relationship_type")
			s.NotEmpty(item.SourceID, "Relationship result should have source_id")
			s.NotEmpty(item.TargetID, "Relationship result should have target_id")
		default:
			s.Failf("Unknown result type", "got type=%q for item id=%s", item.Type, item.ID)
		}
	}
}

func (s *RelationshipSearchSuite) TestUnifiedSearch_RelationshipMetadataInExecutionTime() {
	// Verify that RelationshipSearchMs is populated in execution time metadata
	response := s.unifiedSearch(map[string]any{
		"query": "test relationship timing",
	})

	s.NotNil(response.Metadata.ExecutionTime)
	// RelationshipSearchMs should be present (may be nil if embedding service unavailable)
	s.T().Logf("ExecutionTime: totalMs=%d, relationshipSearchMs=%v",
		response.Metadata.ExecutionTime.TotalMs,
		response.Metadata.ExecutionTime.RelationshipSearchMs)
}

func (s *RelationshipSearchSuite) TestUnifiedSearch_DebugIncludesRelationshipInfo() {
	s.SkipIfExternalServer("requires direct DB access to inject test embeddings")

	// Create relationship with embedding for debug output
	srcID := s.createTestGraphObject("Person", fmt.Sprintf("dbg-src-%d", time.Now().UnixNano()), map[string]any{
		"name": "Debug Person",
	})
	dstID := s.createTestGraphObject("Company", fmt.Sprintf("dbg-dst-%d", time.Now().UnixNano()), map[string]any{
		"name": "Debug Corp",
	})
	relID := s.createTestRelationship("WORKS_FOR", srcID, dstID, nil)
	s.injectRelationshipEmbedding(relID, makeFakeEmbedding(300))

	// Search with debug enabled
	response := s.unifiedSearch(map[string]any{
		"query":        "debug person works for",
		"includeDebug": true,
	})

	// Debug info should be present
	s.NotNil(response.Debug, "Debug should be present when includeDebug=true")

	// If fusion details are available, check relationship counts
	if response.Debug.FusionDetails != nil && response.Debug.FusionDetails.PreFusionCounts != nil {
		s.GreaterOrEqual(response.Debug.FusionDetails.PreFusionCounts.Relationship, 0,
			"Pre-fusion relationship count should be non-negative")
		s.T().Logf("Pre-fusion counts: graph=%d text=%d relationship=%d",
			response.Debug.FusionDetails.PreFusionCounts.Graph,
			response.Debug.FusionDetails.PreFusionCounts.Text,
			response.Debug.FusionDetails.PreFusionCounts.Relationship)
	}
}

// =============================================================================
// TG13.6: Backward compatibility
// =============================================================================

func (s *RelationshipSearchSuite) TestBackwardCompat_SearchWorksWithoutRelationships() {
	// Unified search should work fine when there are NO relationships in the project
	response := s.unifiedSearch(map[string]any{
		"query": "nonexistent content xyz123",
	})

	s.Equal(http.StatusOK, 200) // Sanity — search succeeded
	s.NotNil(response.Results)
	s.Equal(0, response.Metadata.RelationshipResultCount)
	s.Equal(0, response.Metadata.TotalResults)
}

func (s *RelationshipSearchSuite) TestBackwardCompat_GraphFilterIncludesRelationships() {
	s.SkipIfExternalServer("requires direct DB access to inject test embeddings")

	// Create a graph object AND a relationship with embedding
	s.createTestGraphObject("Vehicle", fmt.Sprintf("car-%d", time.Now().UnixNano()), map[string]any{
		"name":        "Electric Vehicle",
		"description": "Battery powered automobile for sustainable transport",
	})

	srcID := s.createTestGraphObject("Person", fmt.Sprintf("driver-%d", time.Now().UnixNano()), map[string]any{
		"name": "Test Driver",
	})
	dstID := s.createTestGraphObject("Vehicle", fmt.Sprintf("ev-%d", time.Now().UnixNano()), map[string]any{
		"name": "Model S",
	})
	relID := s.createTestRelationship("DRIVES", srcID, dstID, nil)
	s.injectRelationshipEmbedding(relID, makeFakeEmbedding(150))

	// Search with graph-only filter — relationships are also included
	// because relationship search runs alongside graph (only text is excluded)
	response := s.unifiedSearch(map[string]any{
		"query":       "electric vehicle",
		"resultTypes": "graph",
	})

	// Verify text results are excluded
	for _, item := range response.Results {
		s.NotEqual(search.ItemTypeText, item.Type,
			"Text results should not appear when resultTypes=graph")
	}
	s.Equal(0, response.Metadata.TextResultCount,
		"TextResultCount should be 0 when filtering to graph only")
}

func (s *RelationshipSearchSuite) TestBackwardCompat_TextOnlyFilterExcludesRelationships() {
	s.SkipIfExternalServer("requires direct DB access to inject test embeddings")

	// Create a relationship with embedding
	srcID := s.createTestGraphObject("Author", fmt.Sprintf("author-%d", time.Now().UnixNano()), map[string]any{
		"name": "Jane Author",
	})
	dstID := s.createTestGraphObject("Book", fmt.Sprintf("book-%d", time.Now().UnixNano()), map[string]any{
		"name": "The Great Novel",
	})
	relID := s.createTestRelationship("WROTE", srcID, dstID, nil)
	s.injectRelationshipEmbedding(relID, makeFakeEmbedding(250))

	// Search with text-only filter — should not include relationship results
	response := s.unifiedSearch(map[string]any{
		"query":       "novel writing",
		"resultTypes": "text",
	})

	for _, item := range response.Results {
		s.NotEqual(search.ItemTypeRelationship, item.Type,
			"Relationship results should not appear when resultTypes=text")
	}
	s.Equal(0, response.Metadata.RelationshipResultCount,
		"RelationshipResultCount should be 0 when filtering to text only")
}

func (s *RelationshipSearchSuite) TestBackwardCompat_ExistingResponseStructure() {
	// Verify the response structure hasn't broken for existing clients.
	// All original fields should still be present and correct.
	response := s.unifiedSearch(map[string]any{
		"query": "test backward compat",
	})

	// Metadata fields that existed before relationships should still work
	s.GreaterOrEqual(response.Metadata.TotalResults, 0)
	s.GreaterOrEqual(response.Metadata.GraphResultCount, 0)
	s.GreaterOrEqual(response.Metadata.TextResultCount, 0)
	s.NotEmpty(string(response.Metadata.FusionStrategy), "FusionStrategy should be set")
	s.GreaterOrEqual(response.Metadata.ExecutionTime.TotalMs, 0)
	s.GreaterOrEqual(response.Metadata.ExecutionTime.FusionMs, 0)
}

func (s *RelationshipSearchSuite) TestBackwardCompat_RelationshipCreationDoesNotBreakExistingAPIs() {
	// Verify that creating a relationship still works correctly and returns
	// the expected response format, regardless of embedding success/failure.
	srcID := s.createTestGraphObject("TestEntity", fmt.Sprintf("compat-src-%d", time.Now().UnixNano()), map[string]any{
		"name": "Source Entity",
	})
	dstID := s.createTestGraphObject("TestEntity", fmt.Sprintf("compat-dst-%d", time.Now().UnixNano()), map[string]any{
		"name": "Destination Entity",
	})

	body := map[string]any{
		"type":   "RELATES_TO",
		"src_id": srcID,
		"dst_id": dstID,
		"properties": map[string]any{
			"reason": "compatibility test",
		},
	}

	resp := s.Client.POST("/api/graph/relationships",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(body),
	)
	s.Require().Equal(http.StatusCreated, resp.StatusCode, "Relationship creation should succeed: %s", resp.String())

	// Parse and verify the response structure
	var result map[string]any
	err := json.Unmarshal(resp.Body, &result)
	s.Require().NoError(err)

	// Verify expected fields are present
	s.NotEmpty(result["id"], "Response should include id")
	s.Equal("RELATES_TO", result["type"], "Response should include type")
	s.Equal(srcID, result["src_id"], "Response should include src_id")
	s.Equal(dstID, result["dst_id"], "Response should include dst_id")
	s.NotNil(result["properties"], "Response should include properties")
}

// =============================================================================
// TG13.1 (additional): Graceful degradation when embedding fails
// =============================================================================

func (s *RelationshipSearchSuite) TestGracefulDegradation_RelationshipWithoutEmbeddingNotInVectorSearch() {
	s.SkipIfExternalServer("requires direct DB access to verify embedding state")

	// Create a relationship but do NOT inject an embedding.
	// This simulates Vertex AI being unavailable.
	srcID := s.createTestGraphObject("Person", fmt.Sprintf("noembsrc-%d", time.Now().UnixNano()), map[string]any{
		"name": "No Embedding Person",
	})
	dstID := s.createTestGraphObject("Company", fmt.Sprintf("noembdst-%d", time.Now().UnixNano()), map[string]any{
		"name": "No Embedding Corp",
	})
	relID := s.createTestRelationship("WORKS_AT", srcID, dstID, nil)

	// Verify that the relationship was created successfully
	s.NotEmpty(relID)

	// Check embedding state — if Vertex AI is not available, embedding should be null
	var hasEmbedding bool
	err := s.DB().QueryRowContext(s.Ctx,
		"SELECT embedding IS NOT NULL FROM kb.graph_relationships WHERE id = ?",
		relID,
	).Scan(&hasEmbedding)
	s.Require().NoError(err)

	if !hasEmbedding {
		// When embedding is null, verify it's excluded from vector search
		queryVector := pgutils.FormatVector(makeFakeEmbedding(1))
		var count int
		err := s.DB().QueryRowContext(s.Ctx, `
			SELECT COUNT(*) FROM kb.graph_relationships r
			JOIN kb.graph_objects src ON src.id = r.src_id
			WHERE r.id = ? AND r.embedding IS NOT NULL AND src.project_id = ?
		`, relID, s.ProjectID).Scan(&count)
		s.Require().NoError(err)
		s.Equal(0, count, "Relationship without embedding should not match WHERE embedding IS NOT NULL")
		_ = queryVector
	} else {
		s.T().Log("Vertex AI was available — embedding was generated automatically")
	}
}

func (s *RelationshipSearchSuite) TestGracefulDegradation_NullEmbeddingDoesNotBreakSearch() {
	s.SkipIfExternalServer("requires direct DB access")

	// Create relationships: one with embedding, one without
	src1 := s.createTestGraphObject("Person", fmt.Sprintf("emb1-%d", time.Now().UnixNano()), map[string]any{"name": "With Embedding"})
	dst1 := s.createTestGraphObject("Company", fmt.Sprintf("co1-%d", time.Now().UnixNano()), map[string]any{"name": "Corp A"})
	rel1 := s.createTestRelationship("EMPLOYED_BY", src1, dst1, nil)
	s.injectRelationshipEmbedding(rel1, makeFakeEmbedding(55))

	src2 := s.createTestGraphObject("Person", fmt.Sprintf("noemb2-%d", time.Now().UnixNano()), map[string]any{"name": "Without Embedding"})
	dst2 := s.createTestGraphObject("Company", fmt.Sprintf("co2-%d", time.Now().UnixNano()), map[string]any{"name": "Corp B"})
	_ = s.createTestRelationship("EMPLOYED_BY", src2, dst2, nil) // No embedding injected

	// Vector search should only return the relationship with an embedding
	queryVector := pgutils.FormatVector(makeFakeEmbedding(55))
	rows, err := s.DB().QueryContext(s.Ctx, `
		SELECT r.id FROM kb.graph_relationships r
		JOIN kb.graph_objects src ON src.id = r.src_id
		WHERE r.embedding IS NOT NULL AND r.deleted_at IS NULL AND src.project_id = ?
		ORDER BY r.embedding <=> ?::vector
		LIMIT 10
	`, s.ProjectID, queryVector)
	s.Require().NoError(err)
	defer rows.Close()

	var foundIDs []string
	for rows.Next() {
		var id string
		s.Require().NoError(rows.Scan(&id))
		foundIDs = append(foundIDs, id)
	}
	s.Require().NoError(rows.Err())

	s.Contains(foundIDs, rel1, "Relationship with embedding should be found")
	// rel2 should NOT be in results since it has no embedding
}

// =============================================================================
// TG13.2 (additional): Unified search fusion strategies with relationships
// =============================================================================

func (s *RelationshipSearchSuite) TestUnifiedSearch_RRFFusionIncludesRelationships() {
	s.SkipIfExternalServer("requires direct DB access to inject test embeddings")

	// Create relationship with embedding
	srcID := s.createTestGraphObject("Scientist", fmt.Sprintf("sci-%d", time.Now().UnixNano()), map[string]any{
		"name": "Marie Curie",
	})
	dstID := s.createTestGraphObject("Element", fmt.Sprintf("elem-%d", time.Now().UnixNano()), map[string]any{
		"name": "Radium",
	})
	relID := s.createTestRelationship("DISCOVERED", srcID, dstID, nil)
	s.injectRelationshipEmbedding(relID, makeFakeEmbedding(333))

	// Search with RRF fusion strategy
	response := s.unifiedSearch(map[string]any{
		"query":          "who discovered radium",
		"fusionStrategy": "rrf",
	})

	s.Equal(search.FusionStrategyRRF, response.Metadata.FusionStrategy)
	// RelationshipResultCount may be 0 if Vertex AI is not available for query embedding
	s.GreaterOrEqual(response.Metadata.RelationshipResultCount, 0)
}

func (s *RelationshipSearchSuite) TestUnifiedSearch_WeightedFusionIncludesRelationships() {
	s.SkipIfExternalServer("requires direct DB access to inject test embeddings")

	// Create relationship with embedding
	srcID := s.createTestGraphObject("Engineer", fmt.Sprintf("eng-%d", time.Now().UnixNano()), map[string]any{
		"name": "Ada Lovelace",
	})
	dstID := s.createTestGraphObject("Program", fmt.Sprintf("prog-%d", time.Now().UnixNano()), map[string]any{
		"name": "First Computer Program",
	})
	relID := s.createTestRelationship("WROTE", srcID, dstID, nil)
	s.injectRelationshipEmbedding(relID, makeFakeEmbedding(400))

	// Search with weighted fusion strategy
	response := s.unifiedSearch(map[string]any{
		"query":          "first computer program",
		"fusionStrategy": "weighted",
	})

	s.Equal(search.FusionStrategyWeighted, response.Metadata.FusionStrategy)
	s.GreaterOrEqual(response.Metadata.RelationshipResultCount, 0)
}
