// Package api_test — graph_similar_test.go
//
// Tests for GET /api/graph/objects/{id}/similar.
// Ported from emergent.memory/apps/server/tests/e2e/graph_similar_test.go
//
// Tests that require direct DB access (to inject embeddings) are skipped.
package api_test

import (
	"fmt"
	"net/http"
	"testing"
)

// TestGraphSimilar_NoEmbedding verifies that an object without an embedding
// returns 200 with an empty result list (not a 500 error).
func TestGraphSimilar_NoEmbedding(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	id := createGraphObject(t, projectID, "Feature", "no-embedding-object", nil)

	resp := doAPILogged(t, rl, "GET", fmt.Sprintf("/api/graph/objects/%s/similar", id), e2eTestToken(), projectID, nil)
	body := mustStatus(t, resp, http.StatusOK)

	var results []any
	parseBodyJSON(t, body, &results)
	if len(results) != 0 {
		t.Errorf("expected empty results for object without embedding, got %d", len(results))
	}
	rl.Printf("similar endpoint returned 200 with empty results for object without embedding")
}

// TestGraphSimilar_WithEmbedding is skipped because it requires direct DB access
// to inject embeddings (the test server uses a no-op embedder).
func TestGraphSimilar_WithEmbedding(t *testing.T) {
	t.Skip("requires direct DB access to inject embeddings — not available in external server mode")
}

// TestGraphSimilar_TypeFilter is skipped because it requires direct DB access
// to inject embeddings.
func TestGraphSimilar_TypeFilter(t *testing.T) {
	t.Skip("requires direct DB access to inject embeddings — not available in external server mode")
}

// TestGraphSimilar_InvalidObjectID verifies a 400 for a malformed ID.
func TestGraphSimilar_InvalidObjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/graph/objects/not-a-uuid/similar", e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusBadRequest)
}

// TestGraphSimilar_RequiresAuth verifies the endpoint is protected.
func TestGraphSimilar_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	id := createGraphObject(t, projectID, "Belief", "auth-test", nil)

	resp := doAPILogged(t, rl, "GET", fmt.Sprintf("/api/graph/objects/%s/similar", id), "", projectID, nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

// TestGraphSimilar_RequiresProjectID verifies the endpoint requires X-Project-ID.
func TestGraphSimilar_RequiresProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	id := createGraphObject(t, projectID, "Belief", "project-test", nil)

	resp := doAPILogged(t, rl, "GET", fmt.Sprintf("/api/graph/objects/%s/similar", id), e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusBadRequest)
}
