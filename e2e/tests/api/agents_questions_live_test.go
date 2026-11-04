// Package api_test — agents_questions_live_test.go
// Tests agent questions against live test data on an external server.
// Requires TEST_SERVER_URL to be set and pre-created DB test data.
package api_test

import (
	"fmt"
	"net/http"
	"testing"
)

func TestAgentsQuestionsLive_ListAndRespond(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	t.Skip("Live test requires external server with pre-seeded data — set TEST_SERVER_URL and seed DB manually")

	// Pre-created test data IDs
	projectID := "44f0c1d9-7c3b-41d1-8393-32cf05ab1a77"
	runID := "00000000-0000-0000-0000-000000000001"
	questionID := "00000000-0000-0000-0000-000000000002"
	token := e2eTestToken()

	// Step 1: List pending questions for project
	listResp := doAPILogged(t, rl, "GET", fmt.Sprintf("/api/projects/%s/agent-questions?status=pending", projectID), token, "", nil)
	body := mustStatus(t, listResp, http.StatusOK)
	var listResult map[string]any
	parseBodyJSON(t, body, &listResult)

	// Step 2: List by run
	runQResp := doAPILogged(t, rl, "GET", fmt.Sprintf("/api/projects/%s/agent-runs/%s/questions", projectID, runID), token, "", nil)
	mustStatus(t, runQResp, http.StatusOK)

	// Step 3: Respond to question
	respondResp := doAPILogged(t, rl, "POST", fmt.Sprintf("/api/projects/%s/agent-questions/%s/respond", projectID, questionID),
		token, "", jsonBody(map[string]any{"response": "blue"}))
	sc := respondResp.StatusCode
	if sc != http.StatusOK && sc != http.StatusAccepted {
		t.Errorf("expected 200 or 202, got %d", sc)
	}
}
