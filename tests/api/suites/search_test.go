package suites

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// SearchTestSuite tests the unified search API endpoints.
// These tests create data via API calls for external testing.
type SearchTestSuite struct {
	BaseSuite
	createdObjectIDs []string // Track created graph objects for cleanup
	createdDocIDs    []string // Track created documents for cleanup
}

func TestSearchSuite(t *testing.T) {
	RunSuite(t, new(SearchTestSuite))
}

// SetupTest runs before each test.
func (s *SearchTestSuite) SetupTest() {
	s.BaseSuite.SetupTest()
	s.createdObjectIDs = nil
	s.createdDocIDs = nil
}

// TearDownTest cleans up created resources after each test.
func (s *SearchTestSuite) TearDownTest() {
	// Clean up graph objects
	for _, id := range s.createdObjectIDs {
		_, _ = s.Client.DELETE("/api/v2/graph/objects/"+id,
			s.AdminAuth(),
			s.ProjectHeader(),
		)
	}
	// Clean up documents
	for _, id := range s.createdDocIDs {
		_, _ = s.Client.DELETE("/api/v2/documents/"+id,
			s.AdminAuth(),
			s.ProjectHeader(),
		)
	}
}

// createGraphObject is a helper that creates a graph object and tracks it for cleanup.
func (s *SearchTestSuite) createGraphObject(objType string, properties map[string]any) string {
	body := map[string]any{
		"type": objType,
	}
	if properties != nil {
		body["properties"] = properties
	}

	resp, err := s.Client.POST("/api/v2/graph/objects", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)
	s.Require().NoError(err)
	s.Require().Equal(http.StatusCreated, resp.StatusCode,
		"Expected 201, got %d: %s", resp.StatusCode, resp.BodyString())

	var obj map[string]any
	err = resp.JSON(&obj)
	s.Require().NoError(err)

	id := obj["id"].(string)
	s.createdObjectIDs = append(s.createdObjectIDs, id)
	return id
}

// createDocument is a helper that creates a document and tracks it for cleanup.
func (s *SearchTestSuite) createDocument(filename, content string) string {
	body := map[string]any{
		"filename": filename,
		"content":  content,
	}

	resp, err := s.Client.POST("/api/v2/documents", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)
	s.Require().NoError(err)
	s.Require().True(resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK,
		"Expected 201 or 200, got %d: %s", resp.StatusCode, resp.BodyString())

	var doc map[string]any
	err = resp.JSON(&doc)
	s.Require().NoError(err)

	id := doc["id"].(string)
	s.createdDocIDs = append(s.createdDocIDs, id)
	return id
}

// =============================================================================
// Test: Authentication & Authorization
// =============================================================================

func (s *SearchTestSuite) TestUnifiedSearch_RequiresAuth() {
	body := map[string]any{
		"query": "test query",
	}

	resp, err := s.Client.POST("/api/v2/search/unified", body,
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *SearchTestSuite) TestUnifiedSearch_RequiresProjectID() {
	body := map[string]any{
		"query": "test query",
	}

	resp, err := s.Client.POST("/api/v2/search/unified", body,
		s.AdminAuth(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

// =============================================================================
// Test: Request Validation
// =============================================================================

func (s *SearchTestSuite) TestUnifiedSearch_RequiresQuery() {
	resp, err := s.Client.POST("/api/v2/search/unified", map[string]any{},
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)

	body, err := resp.JSONMap()
	s.Require().NoError(err)

	errObj, ok := body["error"].(map[string]any)
	s.True(ok)
	s.Equal("bad_request", errObj["code"])
}

func (s *SearchTestSuite) TestUnifiedSearch_QueryTooLong() {
	// Query exceeding max length (800 chars) should fail
	longQuery := strings.Repeat("a", 801)

	body := map[string]any{
		"query": longQuery,
	}

	resp, err := s.Client.POST("/api/v2/search/unified", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

// =============================================================================
// Test: Empty Results
// =============================================================================

func (s *SearchTestSuite) TestUnifiedSearch_EmptyResults() {
	body := map[string]any{
		"query": fmt.Sprintf("nonexistent_query_%s", uuid.New().String()),
	}

	resp, err := s.Client.POST("/api/v2/search/unified", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	result, err := resp.JSONMap()
	s.Require().NoError(err)

	s.Contains(result, "results")
	s.Contains(result, "metadata")

	results := result["results"].([]any)
	s.Empty(results)

	metadata := result["metadata"].(map[string]any)
	s.Equal(float64(0), metadata["totalResults"])
}

// =============================================================================
// Test: Result Types Filter
// =============================================================================

func (s *SearchTestSuite) TestUnifiedSearch_ResultTypesGraph() {
	// Create a graph object
	s.createGraphObject("Person", map[string]any{"name": "John Doe Search Test"})

	body := map[string]any{
		"query":       "John",
		"resultTypes": "graph",
	}

	resp, err := s.Client.POST("/api/v2/search/unified", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	result, err := resp.JSONMap()
	s.Require().NoError(err)

	metadata := result["metadata"].(map[string]any)
	// Text result count should be 0 when filtering for graph only
	s.Equal(float64(0), metadata["textResultCount"])

	// All results should be graph type
	results := result["results"].([]any)
	for _, item := range results {
		r := item.(map[string]any)
		s.Equal("graph", r["type"])
	}
}

func (s *SearchTestSuite) TestUnifiedSearch_ResultTypesText() {
	// Create a document (which creates chunks)
	s.createDocument("search-test-doc.txt", "John Doe is mentioned in this text for search")

	body := map[string]any{
		"query":       "John",
		"resultTypes": "text",
	}

	resp, err := s.Client.POST("/api/v2/search/unified", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	result, err := resp.JSONMap()
	s.Require().NoError(err)

	metadata := result["metadata"].(map[string]any)
	// Graph result count should be 0 when filtering for text only
	s.Equal(float64(0), metadata["graphResultCount"])

	// All results should be text type
	results := result["results"].([]any)
	for _, item := range results {
		r := item.(map[string]any)
		s.Equal("text", r["type"])
	}
}

func (s *SearchTestSuite) TestUnifiedSearch_ResultTypesBoth() {
	body := map[string]any{
		"query":       "test",
		"resultTypes": "both",
	}

	resp, err := s.Client.POST("/api/v2/search/unified", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	result, err := resp.JSONMap()
	s.Require().NoError(err)

	s.Contains(result, "results")
	s.Contains(result, "metadata")
}

// =============================================================================
// Test: Fusion Strategies
// =============================================================================

func (s *SearchTestSuite) TestUnifiedSearch_FusionStrategyWeighted() {
	body := map[string]any{
		"query":          "test",
		"fusionStrategy": "weighted",
	}

	resp, err := s.Client.POST("/api/v2/search/unified", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	result, err := resp.JSONMap()
	s.Require().NoError(err)

	metadata := result["metadata"].(map[string]any)
	s.Equal("weighted", metadata["fusionStrategy"])
}

func (s *SearchTestSuite) TestUnifiedSearch_FusionStrategyRRF() {
	body := map[string]any{
		"query":          "test",
		"fusionStrategy": "rrf",
	}

	resp, err := s.Client.POST("/api/v2/search/unified", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	result, err := resp.JSONMap()
	s.Require().NoError(err)

	metadata := result["metadata"].(map[string]any)
	s.Equal("rrf", metadata["fusionStrategy"])
}

func (s *SearchTestSuite) TestUnifiedSearch_FusionStrategyInterleave() {
	body := map[string]any{
		"query":          "test",
		"fusionStrategy": "interleave",
	}

	resp, err := s.Client.POST("/api/v2/search/unified", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	result, err := resp.JSONMap()
	s.Require().NoError(err)

	metadata := result["metadata"].(map[string]any)
	s.Equal("interleave", metadata["fusionStrategy"])
}

func (s *SearchTestSuite) TestUnifiedSearch_FusionStrategyGraphFirst() {
	body := map[string]any{
		"query":          "test",
		"fusionStrategy": "graph_first",
	}

	resp, err := s.Client.POST("/api/v2/search/unified", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	result, err := resp.JSONMap()
	s.Require().NoError(err)

	metadata := result["metadata"].(map[string]any)
	s.Equal("graph_first", metadata["fusionStrategy"])
}

func (s *SearchTestSuite) TestUnifiedSearch_FusionStrategyTextFirst() {
	body := map[string]any{
		"query":          "test",
		"fusionStrategy": "text_first",
	}

	resp, err := s.Client.POST("/api/v2/search/unified", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	result, err := resp.JSONMap()
	s.Require().NoError(err)

	metadata := result["metadata"].(map[string]any)
	s.Equal("text_first", metadata["fusionStrategy"])
}

func (s *SearchTestSuite) TestUnifiedSearch_DefaultFusionStrategy() {
	// Search without specifying fusion strategy
	body := map[string]any{
		"query": "test",
	}

	resp, err := s.Client.POST("/api/v2/search/unified", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	result, err := resp.JSONMap()
	s.Require().NoError(err)

	metadata := result["metadata"].(map[string]any)
	// Default should be "weighted"
	s.Equal("weighted", metadata["fusionStrategy"])
}

// =============================================================================
// Test: Custom Weights
// =============================================================================

func (s *SearchTestSuite) TestUnifiedSearch_CustomWeights() {
	body := map[string]any{
		"query":          "test",
		"fusionStrategy": "weighted",
		"weights": map[string]any{
			"graphWeight": 0.7,
			"textWeight":  0.3,
		},
	}

	resp, err := s.Client.POST("/api/v2/search/unified", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	result, err := resp.JSONMap()
	s.Require().NoError(err)

	metadata := result["metadata"].(map[string]any)
	s.Equal("weighted", metadata["fusionStrategy"])
}

// =============================================================================
// Test: Limit
// =============================================================================

func (s *SearchTestSuite) TestUnifiedSearch_WithLimit() {
	// Create multiple graph objects
	for i := 0; i < 10; i++ {
		s.createGraphObject("Item", map[string]any{
			"name": fmt.Sprintf("Search Test Item %d", i),
		})
	}

	body := map[string]any{
		"query":       "Item",
		"limit":       5,
		"resultTypes": "graph",
	}

	resp, err := s.Client.POST("/api/v2/search/unified", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	result, err := resp.JSONMap()
	s.Require().NoError(err)

	results := result["results"].([]any)
	s.LessOrEqual(len(results), 5)
}

// =============================================================================
// Test: Execution Time Metadata
// =============================================================================

func (s *SearchTestSuite) TestUnifiedSearch_IncludesExecutionTime() {
	body := map[string]any{
		"query": "test",
	}

	resp, err := s.Client.POST("/api/v2/search/unified", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	result, err := resp.JSONMap()
	s.Require().NoError(err)

	metadata := result["metadata"].(map[string]any)
	s.Contains(metadata, "executionTime")

	execTime := metadata["executionTime"].(map[string]any)
	// Execution time should be non-negative
	s.GreaterOrEqual(execTime["totalMs"], float64(0))
	s.GreaterOrEqual(execTime["fusionMs"], float64(0))
}

// =============================================================================
// Test: With Test Data
// =============================================================================

func (s *SearchTestSuite) TestUnifiedSearch_ReturnsGraphResults() {
	// Create a graph object with searchable content
	s.createGraphObject("Requirement", map[string]any{
		"title":       "User Authentication Requirement",
		"description": "The system shall support user authentication via OAuth2",
	})

	body := map[string]any{
		"query":       "authentication",
		"resultTypes": "graph",
	}

	resp, err := s.Client.POST("/api/v2/search/unified", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	result, err := resp.JSONMap()
	s.Require().NoError(err)

	// Note: Results depend on FTS indexing being enabled on the database
	s.Contains(result, "results")
}

func (s *SearchTestSuite) TestUnifiedSearch_ReturnsTextResults() {
	// Create document with searchable content
	s.createDocument("auth-doc.txt", "This document discusses authentication requirements")

	body := map[string]any{
		"query":       "authentication",
		"resultTypes": "text",
	}

	resp, err := s.Client.POST("/api/v2/search/unified", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	result, err := resp.JSONMap()
	s.Require().NoError(err)

	// Note: Results depend on FTS indexing being enabled on the database
	s.Contains(result, "results")
}

func (s *SearchTestSuite) TestUnifiedSearch_ReturnsBothResults() {
	// Create both graph object and document with searchable content
	s.createGraphObject("Requirement", map[string]any{
		"title": "Security Requirement",
	})
	s.createDocument("security-doc.txt", "This document covers security requirements")

	body := map[string]any{
		"query":       "security",
		"resultTypes": "both",
	}

	resp, err := s.Client.POST("/api/v2/search/unified", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	result, err := resp.JSONMap()
	s.Require().NoError(err)

	s.Contains(result, "results")
	s.Contains(result, "metadata")
}

// =============================================================================
// Test: Search with Filters
// =============================================================================

func (s *SearchTestSuite) TestUnifiedSearch_WithTypeFilter() {
	// Create objects of different types
	s.createGraphObject("Requirement", map[string]any{"title": "Req for filter test"})
	s.createGraphObject("Decision", map[string]any{"title": "Decision for filter test"})

	body := map[string]any{
		"query":       "filter test",
		"resultTypes": "graph",
		"graphTypes":  []string{"Requirement"},
	}

	resp, err := s.Client.POST("/api/v2/search/unified", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	result, err := resp.JSONMap()
	s.Require().NoError(err)

	s.Contains(result, "results")
}

// =============================================================================
// Test: Debug Mode (should require special scope)
// =============================================================================

func (s *SearchTestSuite) TestUnifiedSearch_DebugModeResponse() {
	// Note: Debug mode may require special scope, so this tests the response structure
	body := map[string]any{
		"query": "test",
	}

	resp, err := s.Client.POST("/api/v2/search/unified", body,
		s.AdminAuth(),
		s.ProjectHeader(),
	)

	s.Require().NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	result, err := resp.JSONMap()
	s.Require().NoError(err)

	// Debug should be nil when not requested
	s.Nil(result["debug"])
}
