// Package api_test — adk_sessions_test.go
package api_test

import (
	"fmt"
	"net/http"
	"testing"
)

func TestADKSessions_ListEmpty(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	rl.Printf("listing ADK sessions (expect empty)...")
	resp := doAPILogged(t, rl, "GET", fmt.Sprintf("/api/projects/%s/adk-sessions", projectID),
		e2eTestToken(), "", nil)
	rl.AssertionStep("status == 200", 200, resp.StatusCode, nil)
	mustStatus(t, resp, http.StatusOK)
}

func TestADKSessions_GetInvalidSession(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	rl.Printf("fetching ADK session with invalid ID...")
	resp := doAPILogged(t, rl, "GET", fmt.Sprintf("/api/projects/%s/adk-sessions/invalid-id", projectID),
		e2eTestToken(), "", nil)
	rl.AssertionStep("status == 404", 404, resp.StatusCode, nil)
	mustStatus(t, resp, http.StatusNotFound)
}
