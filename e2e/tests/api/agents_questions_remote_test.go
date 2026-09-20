// Package api_test — agents_questions_remote_test.go
// Tests agent questions API endpoints against a remote/external server.
// Requires TEST_SERVER_URL to be set.
package api_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestAgentsQuestionsRemote_APIEndpoints(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	t.Skip("Remote test requires external server — set TEST_SERVER_URL")

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()

	// List pending questions
	resp := doAPILogged(t, rl, "GET", fmt.Sprintf("/api/projects/%s/agent-questions?status=pending", projectID), token, "", nil)
	mustStatus(t, resp, http.StatusOK)

	// List answered questions
	resp = doAPILogged(t, rl, "GET", fmt.Sprintf("/api/projects/%s/agent-questions?status=answered", projectID), token, "", nil)
	mustStatus(t, resp, http.StatusOK)

	// List all questions (no filter)
	resp = doAPILogged(t, rl, "GET", fmt.Sprintf("/api/projects/%s/agent-questions", projectID), token, "", nil)
	mustStatus(t, resp, http.StatusOK)

	// Respond to nonexistent question — should return 404
	fakeQuestionID := uuid.New().String()
	resp = doAPILogged(t, rl, "POST", fmt.Sprintf("/api/projects/%s/agent-questions/%s/respond", projectID, fakeQuestionID),
		token, "", jsonBody(map[string]any{"response": "test"}))
	sc := resp.StatusCode
	if sc != http.StatusNotFound && sc != http.StatusBadRequest {
		t.Errorf("expected 404 or 400, got %d", sc)
	}

	// List by run — nonexistent run returns 404
	fakeRunID := uuid.New().String()
	resp = doAPILogged(t, rl, "GET", fmt.Sprintf("/api/projects/%s/agent-runs/%s/questions", projectID, fakeRunID), token, "", nil)
	mustStatus(t, resp, http.StatusNotFound)
}
