// Package api_test — relationship_search_test.go
//
// Tests for relationship search via the unified search API.
// Ported from emergent.memory/apps/server/tests/e2e/relationship_search_test.go
//
// Tests that require direct DB access (embedding injection, raw SQL) are skipped.
package api_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// unifiedSearch performs a unified search and returns the parsed response body.
func unifiedSearch(t *testing.T, projectID, query string, opts map[string]any) string {
	t.Helper()
	body := map[string]any{
		"query": query,
	}
	for k, v := range opts {
		body[k] = v
	}
	resp := doAPI(t, "POST", "/api/search/unified", e2eTestToken(), projectID, jsonBody(body))
	return mustStatus(t, resp, http.StatusOK)
}

// =============================================================================
// TG13.1: Create relationship → verify embedding → search finds it
// =============================================================================

func TestRelationshipSearch_CreateRelationship_SetsEmbeddingUpdatedAt(t *testing.T) {
	t.Skip("requires direct DB access to verify embedding_updated_at — not available in external server mode")
}

func TestRelationshipSearch_FindsRelationshipBySemanticQuery(t *testing.T) {
	t.Skip("requires direct DB access to inject test embeddings — not available in external server mode")
}

func TestRelationshipSearch_VectorSearchReturnsMatchingRelationship(t *testing.T) {
	t.Skip("requires direct DB access to inject test embeddings and run raw queries — not available in external server mode")
}

func TestRelationshipSearch_DifferentVectorsHaveLowerSimilarity(t *testing.T) {
	t.Skip("requires direct DB access to inject test embeddings — not available in external server mode")
}

// =============================================================================
// TG13.2: Search returns mixed objects and relationships
// =============================================================================

func TestRelationshipSearch_UnifiedSearch_ReturnsRelationshipResults(t *testing.T) {
	t.Skip("requires direct DB access to inject test embeddings — not available in external server mode")
}

func TestRelationshipSearch_UnifiedSearch_MixedResultTypes(t *testing.T) {
	t.Skip("requires direct DB access to inject test embeddings — not available in external server mode")
}

func TestRelationshipSearch_UnifiedSearch_RelationshipMetadataInExecutionTime(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	body := unifiedSearch(t, projectID, "test relationship timing", nil)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	metadata, _ := response["metadata"].(map[string]any)
	if metadata == nil {
		t.Fatal("expected metadata in response")
	}
	execTime, _ := metadata["executionTime"].(map[string]any)
	if execTime == nil {
		t.Fatal("expected executionTime in metadata")
	}
	if execTime["totalMs"] == nil {
		t.Error("expected totalMs in executionTime")
	}
	rl.Printf("executionTime.totalMs=%v", execTime["totalMs"])
}

func TestRelationshipSearch_UnifiedSearch_DebugIncludesRelationshipInfo(t *testing.T) {
	t.Skip("requires direct DB access to inject test embeddings — not available in external server mode")
}

// =============================================================================
// TG13.6: Backward compatibility
// =============================================================================

func TestRelationshipSearch_BackwardCompat_SearchWorksWithoutRelationships(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	body := unifiedSearch(t, projectID, "nonexistent content xyz123", nil)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	results, _ := response["results"].([]any)
	if results == nil {
		results = []any{}
	}

	metadata, _ := response["metadata"].(map[string]any)
	if metadata == nil {
		t.Fatal("expected metadata in response")
	}
	if relCount, _ := metadata["relationshipResultCount"].(float64); int(relCount) != 0 {
		t.Errorf("expected relationshipResultCount=0, got %v", metadata["relationshipResultCount"])
	}
	if total, _ := metadata["totalResults"].(float64); int(total) != 0 {
		t.Errorf("expected totalResults=0, got %v", metadata["totalResults"])
	}
	rl.Printf("search without relationships returned empty results correctly")
}

func TestRelationshipSearch_BackwardCompat_GraphFilterIncludesRelationships(t *testing.T) {
	t.Skip("requires direct DB access to inject test embeddings — not available in external server mode")
}

func TestRelationshipSearch_BackwardCompat_TextOnlyFilterExcludesRelationships(t *testing.T) {
	t.Skip("requires direct DB access to inject test embeddings — not available in external server mode")
}

func TestRelationshipSearch_BackwardCompat_ExistingResponseStructure(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	body := unifiedSearch(t, projectID, "test backward compat", nil)

	var response map[string]any
	parseBodyJSON(t, body, &response)

	metadata, _ := response["metadata"].(map[string]any)
	if metadata == nil {
		t.Fatal("expected metadata in response")
	}

	// Metadata fields that existed before relationships should still work
	if metadata["totalResults"] == nil {
		t.Error("expected totalResults in metadata")
	}
	if metadata["graphResultCount"] == nil {
		t.Error("expected graphResultCount in metadata")
	}
	if metadata["textResultCount"] == nil {
		t.Error("expected textResultCount in metadata")
	}
	if metadata["fusionStrategy"] == nil || metadata["fusionStrategy"] == "" {
		t.Error("expected non-empty fusionStrategy in metadata")
	}
	execTime, _ := metadata["executionTime"].(map[string]any)
	if execTime == nil {
		t.Fatal("expected executionTime in metadata")
	}
	if execTime["totalMs"] == nil {
		t.Error("expected totalMs in executionTime")
	}
	if execTime["fusionMs"] == nil {
		t.Error("expected fusionMs in executionTime")
	}
	rl.Printf("backward compat response structure verified, fusionStrategy=%v", metadata["fusionStrategy"])
}

func TestRelationshipSearch_BackwardCompat_RelationshipCreationDoesNotBreakExistingAPIs(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	srcID := createGraphObject(t, projectID, "TestEntity", fmt.Sprintf("compat-src-%d", time.Now().UnixNano()), nil)
	dstID := createGraphObject(t, projectID, "TestEntity", fmt.Sprintf("compat-dst-%d", time.Now().UnixNano()), nil)

	resp := doAPILogged(t, rl, "POST", "/api/graph/relationships", e2eTestToken(), projectID, jsonBody(map[string]any{
		"type":   "RELATES_TO",
		"src_id": srcID,
		"dst_id": dstID,
		"properties": map[string]any{
			"reason": "compatibility test",
		},
	}))
	body := mustStatus(t, resp, http.StatusCreated)

	var result map[string]any
	parseBodyJSON(t, body, &result)

	if result["id"] == nil || result["id"] == "" {
		t.Error("expected non-empty id in response")
	}
	if !strings.EqualFold(result["type"].(string), "RELATES_TO") {
		t.Errorf("expected type=RELATES_TO, got %v", result["type"])
	}
	if result["src_id"] != srcID {
		t.Errorf("expected src_id=%s, got %v", srcID, result["src_id"])
	}
	if result["dst_id"] != dstID {
		t.Errorf("expected dst_id=%s, got %v", dstID, result["dst_id"])
	}
	if result["properties"] == nil {
		t.Error("expected non-nil properties in response")
	}
	rl.Printf("relationship creation still works correctly, id=%v", result["id"])
}

// =============================================================================
// TG13.1 (additional): Graceful degradation
// =============================================================================

func TestRelationshipSearch_GracefulDegradation_RelationshipWithoutEmbeddingNotInVectorSearch(t *testing.T) {
	t.Skip("requires direct DB access to verify embedding state — not available in external server mode")
}

func TestRelationshipSearch_GracefulDegradation_NullEmbeddingDoesNotBreakSearch(t *testing.T) {
	t.Skip("requires direct DB access — not available in external server mode")
}

// =============================================================================
// TG13.2 (additional): Unified search fusion strategies with relationships
// =============================================================================

func TestRelationshipSearch_UnifiedSearch_RRFFusionIncludesRelationships(t *testing.T) {
	t.Skip("requires direct DB access to inject test embeddings — not available in external server mode")
}

func TestRelationshipSearch_UnifiedSearch_WeightedFusionIncludesRelationships(t *testing.T) {
	t.Skip("requires direct DB access to inject test embeddings — not available in external server mode")
}
