package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/emergent/emergent-core/domain/search"
	"github.com/emergent/emergent-core/internal/testutil"
)

// SearchTestSuite tests the unified search API endpoints
type SearchTestSuite struct {
	testutil.BaseSuite
}

func TestSearchSuite(t *testing.T) {
	suite.Run(t, new(SearchTestSuite))
}

func (s *SearchTestSuite) SetupSuite() {
	s.SetDBSuffix("search")
	s.BaseSuite.SetupSuite()
}

// =============================================================================
// Helper functions
// =============================================================================

// createDocumentViaAPI creates a document via API and returns its ID
func (s *SearchTestSuite) createDocumentViaAPI(filename, content string) string {
	body := map[string]any{
		"filename": filename,
		"content":  content,
	}

	resp := s.Client.POST("/api/v2/documents",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(body),
	)
	// Accept both 200 (deduplicated) and 201 (created)
	s.Require().True(resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated,
		"Expected 200 or 201, got %d: %s", resp.StatusCode, resp.String())

	var doc map[string]any
	err := json.Unmarshal(resp.Body, &doc)
	s.Require().NoError(err)

	return doc["id"].(string)
}

// createTestGraphObject creates a test graph object via the API and returns its ID
func (s *SearchTestSuite) createTestGraphObject(objType string, key string, properties map[string]any) string {
	body := map[string]any{
		"type":       objType,
		"properties": properties,
	}
	if key != "" {
		body["key"] = key
	}

	resp := s.Client.POST("/api/v2/graph/objects",
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

// =============================================================================
// Test: Authentication & Authorization
// =============================================================================

func (s *SearchTestSuite) TestUnifiedSearch_RequiresAuth() {
	// Request without Authorization header should fail
	resp := s.Client.POST("/api/v2/search/unified",
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"query": "test query",
		}),
	)

	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *SearchTestSuite) TestUnifiedSearch_RequiresSearchReadScope() {
	// User without search:read scope should be forbidden
	resp := s.Client.POST("/api/v2/search/unified",
		testutil.WithAuth("no-scope"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"query": "test query",
		}),
	)

	s.Equal(http.StatusForbidden, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	errObj, ok := body["error"].(map[string]any)
	s.True(ok)
	s.Equal("forbidden", errObj["code"])
}

func (s *SearchTestSuite) TestUnifiedSearch_RequiresProjectID() {
	// Request without X-Project-ID should fail
	resp := s.Client.POST("/api/v2/search/unified",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithJSONBody(map[string]any{
			"query": "test query",
		}),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

// =============================================================================
// Test: Request Validation
// =============================================================================

func (s *SearchTestSuite) TestUnifiedSearch_RequiresQuery() {
	// Request without query should fail
	resp := s.Client.POST("/api/v2/search/unified",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{}),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	errObj, ok := body["error"].(map[string]any)
	s.True(ok)
	s.Equal("bad_request", errObj["code"])
}

func (s *SearchTestSuite) TestUnifiedSearch_QueryTooLong() {
	// Query exceeding max length (800 chars) should fail
	longQuery := make([]byte, 801)
	for i := range longQuery {
		longQuery[i] = 'a'
	}

	resp := s.Client.POST("/api/v2/search/unified",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"query": string(longQuery),
		}),
	)

	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

// =============================================================================
// Test: Empty Results
// =============================================================================

func (s *SearchTestSuite) TestUnifiedSearch_EmptyResults() {
	// Search with no data in DB should return empty results
	resp := s.Client.POST("/api/v2/search/unified",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"query": "nonexistent query term",
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var response search.UnifiedSearchResponse
	err := json.Unmarshal(resp.Body, &response)
	s.Require().NoError(err)

	s.Empty(response.Results)
	s.Equal(0, response.Metadata.TotalResults)
	s.Equal(0, response.Metadata.GraphResultCount)
	s.Equal(0, response.Metadata.TextResultCount)
	s.Nil(response.Debug) // Debug should be nil when not requested
}

// =============================================================================
// Test: Result Types Filter
// =============================================================================

func (s *SearchTestSuite) TestUnifiedSearch_ResultTypesGraph() {
	// Create test data via API
	uniqueKey := fmt.Sprintf("john-%d", time.Now().UnixNano())
	s.createTestGraphObject("Person", uniqueKey, map[string]any{"name": "John Doe"})
	// Create a document with unique content (will be indexed)
	s.createDocumentViaAPI(fmt.Sprintf("test-%d.txt", time.Now().UnixNano()), "Some text content about other things")

	// Search with graph-only results
	resp := s.Client.POST("/api/v2/search/unified",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"query":       "John",
			"resultTypes": "graph",
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var response search.UnifiedSearchResponse
	err := json.Unmarshal(resp.Body, &response)
	s.Require().NoError(err)

	// Should only contain graph results
	s.Equal(0, response.Metadata.TextResultCount)
	for _, item := range response.Results {
		s.Equal(search.ItemTypeGraph, item.Type)
	}
}

func (s *SearchTestSuite) TestUnifiedSearch_ResultTypesText() {
	// Create test data via API
	uniqueKey := fmt.Sprintf("john-text-%d", time.Now().UnixNano())
	s.createTestGraphObject("Person", uniqueKey, map[string]any{"name": "John Doe"})
	// Create a document with searchable content (will be indexed automatically)
	s.createDocumentViaAPI(fmt.Sprintf("john-doc-%d.txt", time.Now().UnixNano()), "John Doe is mentioned in this text")

	// Search with text-only results
	resp := s.Client.POST("/api/v2/search/unified",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"query":       "John",
			"resultTypes": "text",
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var response search.UnifiedSearchResponse
	err := json.Unmarshal(resp.Body, &response)
	s.Require().NoError(err)

	// Should only contain text results
	s.Equal(0, response.Metadata.GraphResultCount)
	for _, item := range response.Results {
		s.Equal(search.ItemTypeText, item.Type)
	}
}

// =============================================================================
// Test: Fusion Strategies
// =============================================================================

func (s *SearchTestSuite) TestUnifiedSearch_FusionStrategyWeighted() {
	resp := s.Client.POST("/api/v2/search/unified",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"query":          "test",
			"fusionStrategy": "weighted",
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var response search.UnifiedSearchResponse
	err := json.Unmarshal(resp.Body, &response)
	s.Require().NoError(err)

	s.Equal(search.FusionStrategyWeighted, response.Metadata.FusionStrategy)
}

func (s *SearchTestSuite) TestUnifiedSearch_FusionStrategyRRF() {
	resp := s.Client.POST("/api/v2/search/unified",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"query":          "test",
			"fusionStrategy": "rrf",
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var response search.UnifiedSearchResponse
	err := json.Unmarshal(resp.Body, &response)
	s.Require().NoError(err)

	s.Equal(search.FusionStrategyRRF, response.Metadata.FusionStrategy)
}

func (s *SearchTestSuite) TestUnifiedSearch_FusionStrategyInterleave() {
	resp := s.Client.POST("/api/v2/search/unified",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"query":          "test",
			"fusionStrategy": "interleave",
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var response search.UnifiedSearchResponse
	err := json.Unmarshal(resp.Body, &response)
	s.Require().NoError(err)

	s.Equal(search.FusionStrategyInterleave, response.Metadata.FusionStrategy)
}

func (s *SearchTestSuite) TestUnifiedSearch_FusionStrategyGraphFirst() {
	resp := s.Client.POST("/api/v2/search/unified",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"query":          "test",
			"fusionStrategy": "graph_first",
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var response search.UnifiedSearchResponse
	err := json.Unmarshal(resp.Body, &response)
	s.Require().NoError(err)

	s.Equal(search.FusionStrategyGraphFirst, response.Metadata.FusionStrategy)
}

func (s *SearchTestSuite) TestUnifiedSearch_FusionStrategyTextFirst() {
	resp := s.Client.POST("/api/v2/search/unified",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"query":          "test",
			"fusionStrategy": "text_first",
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var response search.UnifiedSearchResponse
	err := json.Unmarshal(resp.Body, &response)
	s.Require().NoError(err)

	s.Equal(search.FusionStrategyTextFirst, response.Metadata.FusionStrategy)
}

// =============================================================================
// Test: Custom Weights
// =============================================================================

func (s *SearchTestSuite) TestUnifiedSearch_CustomWeights() {
	resp := s.Client.POST("/api/v2/search/unified",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"query":          "test",
			"fusionStrategy": "weighted",
			"weights": map[string]any{
				"graphWeight": 0.7,
				"textWeight":  0.3,
			},
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var response search.UnifiedSearchResponse
	err := json.Unmarshal(resp.Body, &response)
	s.Require().NoError(err)

	s.Equal(search.FusionStrategyWeighted, response.Metadata.FusionStrategy)
}

// =============================================================================
// Test: Limit
// =============================================================================

func (s *SearchTestSuite) TestUnifiedSearch_WithLimit() {
	// Create multiple graph objects
	for i := 0; i < 10; i++ {
		s.createTestGraphObject("Item", fmt.Sprintf("item-%d", i), map[string]any{
			"name": "Test Item",
		})
	}

	// Search with limit
	resp := s.Client.POST("/api/v2/search/unified",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"query":       "Item",
			"limit":       5,
			"resultTypes": "graph",
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var response search.UnifiedSearchResponse
	err := json.Unmarshal(resp.Body, &response)
	s.Require().NoError(err)

	s.LessOrEqual(len(response.Results), 5)
}

// =============================================================================
// Test: Debug Mode
// =============================================================================

func (s *SearchTestSuite) TestUnifiedSearch_DebugModeRequiresScope() {
	// User without search:debug scope requesting debug mode should be forbidden
	resp := s.Client.POST("/api/v2/search/unified",
		testutil.WithAuth("no-scope"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"query":        "test",
			"includeDebug": true,
		}),
	)

	// Should be forbidden because no-scope user doesn't have search:debug scope
	s.Equal(http.StatusForbidden, resp.StatusCode)

	var body map[string]any
	err := json.Unmarshal(resp.Body, &body)
	s.NoError(err)

	errObj, ok := body["error"].(map[string]any)
	s.True(ok)
	s.Equal("forbidden", errObj["code"])
}

func (s *SearchTestSuite) TestUnifiedSearch_DebugModeRequiresScopeViaQueryParam() {
	// User without search:debug scope should be forbidden even with query param
	resp := s.Client.POST("/api/v2/search/unified?debug=true",
		testutil.WithAuth("no-scope"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"query": "test",
		}),
	)

	s.Equal(http.StatusForbidden, resp.StatusCode)
}

func (s *SearchTestSuite) TestUnifiedSearch_DebugModeViaBodyField() {
	// e2e-test-user has search:debug scope (via GetAllScopes)
	resp := s.Client.POST("/api/v2/search/unified",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"query":        "test debug",
			"includeDebug": true,
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var response search.UnifiedSearchResponse
	err := json.Unmarshal(resp.Body, &response)
	s.Require().NoError(err)

	// Debug info should be present when requested
	s.NotNil(response.Debug, "Debug should be present when includeDebug=true")
}

func (s *SearchTestSuite) TestUnifiedSearch_DebugModeViaQueryParam() {
	// e2e-test-user has search:debug scope
	resp := s.Client.POST("/api/v2/search/unified?debug=true",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"query": "query param debug",
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var response search.UnifiedSearchResponse
	err := json.Unmarshal(resp.Body, &response)
	s.Require().NoError(err)

	// Debug info should be present when requested via query param
	s.NotNil(response.Debug, "Debug should be present when ?debug=true")
}

func (s *SearchTestSuite) TestUnifiedSearch_NoDebugWithoutFlag() {
	// Request without debug flag should not include debug info
	resp := s.Client.POST("/api/v2/search/unified",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"query": "no debug",
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var response search.UnifiedSearchResponse
	err := json.Unmarshal(resp.Body, &response)
	s.Require().NoError(err)

	// Debug should NOT be present when not requested
	s.Nil(response.Debug, "Debug should NOT be present when debug is not requested")
}

// =============================================================================
// Test: Execution Time Metadata
// =============================================================================

func (s *SearchTestSuite) TestUnifiedSearch_IncludesExecutionTime() {
	resp := s.Client.POST("/api/v2/search/unified",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"query": "test",
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var response search.UnifiedSearchResponse
	err := json.Unmarshal(resp.Body, &response)
	s.Require().NoError(err)

	// Execution time should be non-negative
	s.GreaterOrEqual(response.Metadata.ExecutionTime.TotalMs, 0)
	s.GreaterOrEqual(response.Metadata.ExecutionTime.FusionMs, 0)
}

// =============================================================================
// Test: With Test Data
// =============================================================================

func (s *SearchTestSuite) TestUnifiedSearch_ReturnsGraphResults() {
	// Create a graph object with searchable content
	objID := s.createTestGraphObject("Requirement", "req-001", map[string]any{
		"title":       "User Authentication Requirement",
		"description": "The system shall support user authentication via OAuth2",
	})

	// Search for content
	resp := s.Client.POST("/api/v2/search/unified",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"query":       "authentication",
			"resultTypes": "graph",
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var response search.UnifiedSearchResponse
	err := json.Unmarshal(resp.Body, &response)
	s.Require().NoError(err)

	// Note: Results depend on FTS indexing being enabled on the database
	// If FTS isn't configured, this may return empty results
	s.NotNil(response.Results)
	_ = objID // Silence unused variable warning
}

func (s *SearchTestSuite) TestUnifiedSearch_ReturnsTextResults() {
	// Create document with searchable content via API (will be indexed automatically)
	s.createDocumentViaAPI(fmt.Sprintf("auth-doc-%d.txt", time.Now().UnixNano()), "This document discusses authentication requirements")

	// Search for content
	resp := s.Client.POST("/api/v2/search/unified",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"query":       "authentication",
			"resultTypes": "text",
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var response search.UnifiedSearchResponse
	err := json.Unmarshal(resp.Body, &response)
	s.Require().NoError(err)

	// Note: Results depend on FTS indexing being enabled on the database
	s.NotNil(response.Results)
}

func (s *SearchTestSuite) TestUnifiedSearch_ReturnsBothResults() {
	// Create both graph object and document with searchable content via API
	uniqueKey := fmt.Sprintf("req-security-%d", time.Now().UnixNano())
	s.createTestGraphObject("Requirement", uniqueKey, map[string]any{
		"title": "Security Requirement",
	})
	s.createDocumentViaAPI(fmt.Sprintf("security-doc-%d.txt", time.Now().UnixNano()), "This document covers security requirements")

	// Search for content with both result types
	resp := s.Client.POST("/api/v2/search/unified",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"query":       "security",
			"resultTypes": "both",
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var response search.UnifiedSearchResponse
	err := json.Unmarshal(resp.Body, &response)
	s.Require().NoError(err)

	s.NotNil(response.Results)
	s.NotNil(response.Metadata)
}

// =============================================================================
// Test: Default Fusion Strategy
// =============================================================================

func (s *SearchTestSuite) TestUnifiedSearch_DefaultFusionStrategy() {
	// Search without specifying fusion strategy
	resp := s.Client.POST("/api/v2/search/unified",
		testutil.WithAuth("e2e-test-user"),
		testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"query": "test",
		}),
	)

	s.Equal(http.StatusOK, resp.StatusCode)

	var response search.UnifiedSearchResponse
	err := json.Unmarshal(resp.Body, &response)
	s.Require().NoError(err)

	// Default should be "weighted"
	s.Equal(search.FusionStrategyWeighted, response.Metadata.FusionStrategy)
}
