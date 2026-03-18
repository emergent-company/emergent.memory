package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/pkg/pgutils"
)

// GraphSimilarSuite tests GET /api/graph/objects/{id}/similar.
//
// Issue #97: The endpoint was returning 500 database_error for all requests
// because bun/pgx has no pgvector codec registered, so scanning embedding_v2
// into []float32 failed. Fix: cast embedding_v2::text in the SQL query and
// parse with pgutils.ParseVector.
type GraphSimilarSuite struct {
	testutil.BaseSuite
}

func TestGraphSimilarSuite(t *testing.T) {
	suite.Run(t, new(GraphSimilarSuite))
}

func (s *GraphSimilarSuite) SetupSuite() {
	s.SetDBSuffix("graphsimilar")
	s.BaseSuite.SetupSuite()
}

// =============================================================================
// Helpers
// =============================================================================

func (s *GraphSimilarSuite) createObject(objType string, properties map[string]any) string {
	body := map[string]any{
		"type":       objType,
		"properties": properties,
	}
	rec := s.Client.POST("/api/graph/objects",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(body),
	)
	s.Require().Equal(http.StatusCreated, rec.StatusCode, "create object: %s", rec.String())

	var result map[string]any
	s.Require().NoError(json.Unmarshal(rec.Body, &result))
	return result["id"].(string)
}

// injectObjectEmbedding writes an embedding vector directly into the DB for a
// graph object. Required in tests because the test server uses a no-op embedder.
func (s *GraphSimilarSuite) injectObjectEmbedding(objectID string, embedding []float32) {
	s.SkipIfExternalServer("requires direct DB access to inject embeddings")
	db := s.DB()
	s.Require().NotNil(db)

	vectorStr := pgutils.FormatVector(embedding)
	_, err := db.ExecContext(s.Ctx,
		"UPDATE kb.graph_objects SET embedding_v2 = ?::vector, embedding_updated_at = NOW() WHERE id = ?",
		vectorStr, objectID,
	)
	s.Require().NoError(err, "inject embedding for object %s", objectID)
}

func (s *GraphSimilarSuite) getSimilar(objectID string, queryParams ...string) *testutil.HTTPResponse {
	path := fmt.Sprintf("/api/graph/objects/%s/similar", objectID)
	if len(queryParams) > 0 {
		path += "?" + queryParams[0]
	}
	return s.Client.GET(path,
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
}

// =============================================================================
// Tests
// =============================================================================

// TestSimilar_NoEmbedding verifies that an object without an embedding returns
// 200 with an empty result list (not a 500 error).
func (s *GraphSimilarSuite) TestSimilar_NoEmbedding() {
	id := s.createObject("Feature", map[string]any{"name": "no-embedding-object"})

	rec := s.getSimilar(id)

	s.Equal(http.StatusOK, rec.StatusCode, "response: %s", rec.String())

	var results []graph.SimilarObjectResult
	s.Require().NoError(json.Unmarshal(rec.Body, &results))
	s.Empty(results)
}

// TestSimilar_WithEmbedding is the regression test for issue #97.
// Before the fix, this returned 500 because bun couldn't scan the pgvector
// column into []float32. After the fix, it returns 200 with similar objects.
func (s *GraphSimilarSuite) TestSimilar_WithEmbedding() {
	s.SkipIfExternalServer("requires direct DB access to inject embeddings")

	// Create two objects with very similar embeddings (same direction, seed=1)
	// and one object with a very different embedding (seed=400).
	sourceID := s.createObject("Belief", map[string]any{"name": "source"})
	nearID := s.createObject("Belief", map[string]any{"name": "near"})
	farID := s.createObject("Feature", map[string]any{"name": "far"})

	nearEmbedding := makeFakeEmbedding(1)
	s.injectObjectEmbedding(sourceID, nearEmbedding)
	s.injectObjectEmbedding(nearID, nearEmbedding)            // identical → distance ≈ 0
	s.injectObjectEmbedding(farID, makeFakeEmbedding(400))    // orthogonal → distance ≈ 1

	rec := s.getSimilar(sourceID, "limit=10")

	s.Equal(http.StatusOK, rec.StatusCode, "response: %s", rec.String())

	var results []graph.SimilarObjectResult
	s.Require().NoError(json.Unmarshal(rec.Body, &results))

	// At least the near object should be returned
	s.NotEmpty(results, "expected similar objects to be returned")

	// Source object itself must not appear in results
	for _, r := range results {
		s.NotEqual(sourceID, r.ID.String(), "source object must not appear in its own similar results")
	}

	// The near object (identical embedding) should appear before the far object
	var nearIdx, farIdx = -1, -1
	for i, r := range results {
		switch r.ID.String() {
		case nearID:
			nearIdx = i
		case farID:
			farIdx = i
		}
	}
	s.GreaterOrEqual(nearIdx, 0, "near object should be in results")
	if farIdx >= 0 {
		s.Less(nearIdx, farIdx, "near object should rank before far object")
	}
}

// TestSimilar_TypeFilter verifies that the ?type= filter restricts results.
func (s *GraphSimilarSuite) TestSimilar_TypeFilter() {
	s.SkipIfExternalServer("requires direct DB access to inject embeddings")

	sourceID := s.createObject("Belief", map[string]any{"name": "source"})
	beliefID := s.createObject("Belief", map[string]any{"name": "other-belief"})
	featureID := s.createObject("Feature", map[string]any{"name": "other-feature"})

	sharedEmbedding := makeFakeEmbedding(5)
	s.injectObjectEmbedding(sourceID, sharedEmbedding)
	s.injectObjectEmbedding(beliefID, sharedEmbedding)
	s.injectObjectEmbedding(featureID, sharedEmbedding)

	rec := s.getSimilar(sourceID, "type=Belief&limit=10")

	s.Equal(http.StatusOK, rec.StatusCode, "response: %s", rec.String())

	var results []graph.SimilarObjectResult
	s.Require().NoError(json.Unmarshal(rec.Body, &results))

	for _, r := range results {
		s.Equal("Belief", r.Type, "type filter should exclude Feature objects")
		s.NotEqual(sourceID, r.ID.String())
	}

	// Feature object must not appear
	for _, r := range results {
		s.NotEqual(featureID, r.ID.String(), "feature should be excluded by type=Belief filter")
	}
}

// TestSimilar_InvalidObjectID verifies a 400 for a malformed ID.
func (s *GraphSimilarSuite) TestSimilar_InvalidObjectID() {
	rec := s.Client.GET("/api/graph/objects/not-a-uuid/similar",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusBadRequest, rec.StatusCode)
}

// TestSimilar_RequiresAuth verifies the endpoint is protected.
func (s *GraphSimilarSuite) TestSimilar_RequiresAuth() {
	id := s.createObject("Belief", map[string]any{"name": "auth-test"})
	rec := s.Client.GET(fmt.Sprintf("/api/graph/objects/%s/similar", id),
		testutil.WithProjectID(s.ProjectID),
	)
	s.Equal(http.StatusUnauthorized, rec.StatusCode)
}

// TestSimilar_RequiresProjectID verifies the endpoint requires X-Project-ID.
func (s *GraphSimilarSuite) TestSimilar_RequiresProjectID() {
	id := s.createObject("Belief", map[string]any{"name": "project-test"})
	rec := s.Client.GET(fmt.Sprintf("/api/graph/objects/%s/similar", id),
		testutil.WithAuth("e2e-test-user"),
	)
	s.Equal(http.StatusBadRequest, rec.StatusCode)
}
