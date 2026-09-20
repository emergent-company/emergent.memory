// Package api_test — events_test.go
//
// Tests for the events API endpoints (/events/...).
// Ported from emergent.memory/apps/server/tests/e2e/events_test.go
package api_test

import (
	"net/http"
	"testing"
)

// =============================================================================
// GET /events/connections/count
// =============================================================================

func TestEvents_GetConnectionsCount_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/events/connections/count", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestEvents_GetConnectionsCount_ReturnsZeroInitially(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/events/connections/count", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)

	count, _ := result["count"].(float64)
	if count != 0 {
		t.Errorf("expected count=0 when no connections, got %v", count)
	}
	rl.Printf("connections count = 0 (no active connections)")
}

func TestEvents_GetConnectionsCount_ReturnsValidJSON(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/events/connections/count", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)

	if _, hasCount := result["count"]; !hasCount {
		t.Error("response should have 'count' field")
	}
}

func TestEvents_GetConnectionsCount_AcceptsMultipleTokens(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	tokens := []string{
		e2eTestToken(),
		"read-only",
	}

	for _, token := range tokens {
		resp := doAPILogged(t, rl, "GET", "/api/events/connections/count", token, "", nil)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("token %q: expected 200, got %d", token, resp.StatusCode)
		}
		resp.Body.Close()
	}
	rl.Printf("multiple token types accepted for /events/connections/count")
}

// =============================================================================
// GET /events/stream
// =============================================================================

func TestEvents_Stream_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/events/stream", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestEvents_Stream_RequiresProjectID(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	// No projectId query param — should fail
	resp := doAPILogged(t, rl, "GET", "/api/events/stream", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusBadRequest)

	assertContains(t, body, "projectId")
	rl.Printf("stream correctly requires projectId param")
}
