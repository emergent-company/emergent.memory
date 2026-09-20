// Package api_test — agents_questions_test.go
package api_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestAgentsQuestions_RespondToQuestion_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	questionID := uuid.New().String()

	resp := doAPILogged(t, rl, "POST", fmt.Sprintf("/api/projects/%s/agent-questions/%s/respond", projectID, questionID),
		"", "", jsonBody(map[string]any{"response": "test"}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestAgentsQuestions_RespondToQuestion_NotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	questionID := uuid.New().String()

	resp := doAPILogged(t, rl, "POST", fmt.Sprintf("/api/projects/%s/agent-questions/%s/respond", projectID, questionID),
		e2eTestToken(), "", jsonBody(map[string]any{"response": "test"}))
	mustStatus(t, resp, http.StatusNotFound)
}

func TestAgentsQuestions_ListQuestionsByRun_RunNotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	runID := uuid.New().String()

	resp := doAPILogged(t, rl, "GET", fmt.Sprintf("/api/projects/%s/agent-runs/%s/questions", projectID, runID),
		e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusNotFound)
}

func TestAgentsQuestions_ListQuestionsByProject_InvalidStatusFilter(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "GET", fmt.Sprintf("/api/projects/%s/agent-questions?status=invalid", projectID),
		e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestAgentsQuestions_ListQuestionsByProject_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	resp := doAPILogged(t, rl, "GET", fmt.Sprintf("/api/projects/%s/agent-questions", projectID),
		"", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}
