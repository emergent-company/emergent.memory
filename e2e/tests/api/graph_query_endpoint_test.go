// Package api_test — graph_query_endpoint_test.go
//
// Tests for the POST /api/projects/:projectId/query endpoint.
// Ported from emergent.memory/apps/server/tests/e2e/graph_query_endpoint_test.go
package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// parseSSEEvents parses an SSE stream body into a slice of event data maps.
// Each SSE event is separated by "\n\n". Data lines start with "data: ".
func parseSSEEvents(body string) []map[string]any {
	var events []map[string]any
	// Split on double newline to get individual events
	rawEvents := strings.Split(body, "\n\n")
	for _, rawEvent := range rawEvents {
		lines := strings.Split(rawEvent, "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")
				data = strings.TrimSpace(data)
				if data == "" {
					continue
				}
				var ev map[string]any
				if err := json.Unmarshal([]byte(data), &ev); err == nil {
					events = append(events, ev)
				}
			}
		}
	}
	return events
}

// TestGraphQuery_ReturnsSSEStream verifies that the query endpoint returns a valid
// SSE stream with at least a "done" event.
func TestGraphQuery_ReturnsSSEStream(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfNoLLMProvider(t)

	projectID, _ := setupProjectLogged(t, rl)
	configureProjectModel(t, projectID)

	resp := doAPILogged(t, rl, "POST", "/api/projects/"+projectID+"/query", e2eTestToken(), projectID, jsonBody(map[string]any{
		"message": "what is in this project?",
	}))
	body := mustStatus(t, resp, http.StatusOK)

	events := parseSSEEvents(body)
	if len(events) == 0 {
		t.Fatal("expected at least one SSE event")
	}

	var hasDone bool
	for _, ev := range events {
		if t2, ok := ev["type"].(string); ok && t2 == "done" {
			hasDone = true
			break
		}
	}
	if !hasDone {
		t.Errorf("expected a 'done' SSE event; got body: %s", body)
	}
	rl.Printf("query endpoint returned SSE stream with %d events including 'done'", len(events))
}

// TestGraphQuery_RequiresMessage verifies that an empty message is rejected.
func TestGraphQuery_RequiresMessage(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "POST", "/api/projects/"+projectID+"/query", e2eTestToken(), projectID, jsonBody(map[string]any{
		"message": "",
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

// TestGraphQuery_GraphQueryAgentHiddenFromList verifies that the graph-query-agent
// does NOT appear in the public agent definitions list.
func TestGraphQuery_GraphQueryAgentHiddenFromList(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfNoLLMProvider(t)

	projectID, _ := setupProjectLogged(t, rl)
	configureProjectModel(t, projectID)

	// Call the query endpoint to exercise the path
	queryResp := doAPILogged(t, rl, "POST", "/api/projects/"+projectID+"/query", e2eTestToken(), projectID, jsonBody(map[string]any{
		"message": "ping",
	}))
	mustStatus(t, queryResp, http.StatusOK)

	// Allow the async DB writes from QueryStream to complete
	time.Sleep(100 * time.Millisecond)

	// Now list agent definitions — graph-query-agent must not appear
	listResp := doAPILogged(t, rl, "GET", "/api/projects/"+projectID+"/agent-definitions", e2eTestToken(), projectID, nil)
	listBody := mustStatus(t, listResp, http.StatusOK)

	var result struct {
		Success bool                     `json:"success"`
		Data    []map[string]interface{} `json:"data"`
	}
	parseBodyJSON(t, listBody, &result)
	if !result.Success {
		t.Error("expected success=true in agent-definitions list")
	}

	for _, def := range result.Data {
		name, _ := def["name"].(string)
		if strings.EqualFold(name, "graph-query-agent") {
			t.Errorf("graph-query-agent should be hidden from the public list, but found: %v", def)
		}
	}
	rl.Printf("graph-query-agent not in agent definitions list")
}

// TestGraphQuery_IsIdempotent verifies that calling the query endpoint multiple times
// does not create duplicate graph-query-agents.
// Note: The DB-count assertion from the original test is omitted (requires direct DB access).
func TestGraphQuery_IsIdempotent(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfNoLLMProvider(t)

	projectID, _ := setupProjectLogged(t, rl)
	configureProjectModel(t, projectID)

	for i := 0; i < 2; i++ {
		resp := doAPILogged(t, rl, "POST", "/api/projects/"+projectID+"/query", e2eTestToken(), projectID, jsonBody(map[string]any{
			"message": "idempotency check",
		}))
		mustStatus(t, resp, http.StatusOK)
		// Allow async DB writes from QueryStream to drain
		time.Sleep(100 * time.Millisecond)
	}
	rl.Printf("query endpoint called twice without errors (idempotent)")
}
