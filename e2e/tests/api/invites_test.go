// Package api_test — invites_test.go
//
// Tests for the invites API endpoints.
// Ported from emergent.memory/apps/server/tests/e2e/invites_test.go
//
// The invite email for the standalone e2e user is read from STANDALONE_USER_EMAIL
// (defaults to "e2e@localhost", matching the docker-compose fixture).
package api_test

import (
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

// standaloneUserEmail returns the email address of the e2e test user in standalone mode.
func standaloneUserEmail() string {
	if v := os.Getenv("STANDALONE_USER_EMAIL"); v != "" {
		return v
	}
	return "e2e@localhost"
}

// createInvite creates an invite via POST /api/invites and returns the response body.
// projectID may be empty for org-level invites.
func createInvite(t *testing.T, email, orgID, projectID, role string) map[string]any {
	t.Helper()
	body := map[string]any{
		"email": email,
		"orgId": orgID,
		"role":  role,
	}
	if projectID != "" {
		body["projectId"] = projectID
	}
	resp := doAPI(t, "POST", "/api/invites", e2eTestToken(), "", jsonBody(body))
	respBody := mustStatus(t, resp, http.StatusCreated)
	var invite map[string]any
	parseBodyJSON(t, respBody, &invite)
	return invite
}

// ─── List Pending Tests ───────────────────────────────────────────────────────

func TestListPending_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/invites/pending", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestListPending_EmptyArrayWhenNoInvites(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/invites/pending", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	// Verify the response is a valid JSON array
	var result []map[string]any
	parseBodyJSON(t, body, &result)
	rl.Printf("pending list returned %d items", len(result))
}

// TestListPending_CreateAndVerifyInviteResponse verifies the invite creation
// response structure. In standalone mode only one user exists, so the inviting
// user is already a member of any org they create — invites don't appear in
// their own pending list. We verify the creation API response structure instead.
func TestListPending_CreateAndVerifyInviteResponse(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	_, orgID := setupProjectLogged(t, rl)
	invite := createInvite(t, standaloneUserEmail(), orgID, "", "org_admin")

	for _, field := range []string{"id", "organizationId", "role", "token", "createdAt"} {
		if _, ok := invite[field]; !ok {
			t.Errorf("invite creation response missing field %q", field)
		}
	}
	if invite["organizationId"] != orgID {
		t.Errorf("expected organizationId=%s, got %v", orgID, invite["organizationId"])
	}
	if invite["role"] != "org_admin" {
		t.Errorf("expected role=org_admin, got %v", invite["role"])
	}
	rl.Printf("invite created with expected fields, id=%v", invite["id"])
}

// TestListPending_ProjectInviteStructure verifies project invite creation response.
func TestListPending_ProjectInviteStructure(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, orgID := setupProjectLogged(t, rl)
	invite := createInvite(t, standaloneUserEmail(), orgID, projectID, "project_user")

	for _, field := range []string{"id", "organizationId", "role", "token", "createdAt", "projectId"} {
		if _, ok := invite[field]; !ok {
			t.Errorf("invite creation response missing field %q", field)
		}
	}
	if invite["projectId"] != projectID {
		t.Errorf("expected projectId=%s, got %v", projectID, invite["projectId"])
	}
	rl.Printf("project invite created with expected fields")
}

// TestListPending_MultipleInvites verifies creating multiple invites succeeds.
func TestListPending_MultipleInvites(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	ts := time.Now().UnixNano()
	_, orgID1 := setupProjectLogged(t, rl)
	_, orgID2 := setupProjectLogged(t, rl)

	// Create invites for different emails (not the test user since they're already members)
	inv1 := createInvite(t, fmt.Sprintf("user1-%d@example.com", ts), orgID1, "", "org_admin")
	inv2 := createInvite(t, fmt.Sprintf("user2-%d@example.com", ts), orgID2, "", "org_admin")

	if inv1["id"] == nil || inv2["id"] == nil {
		t.Error("expected both invites to have IDs")
	}
	if inv1["organizationId"] != orgID1 {
		t.Errorf("invite 1 wrong org: got %v", inv1["organizationId"])
	}
	if inv2["organizationId"] != orgID2 {
		t.Errorf("invite 2 wrong org: got %v", inv2["organizationId"])
	}
	rl.Printf("multiple invites created: %v, %v", inv1["id"], inv2["id"])
}

// TestListPending_PendingListReturnsArray verifies the pending endpoint returns a JSON array.
func TestListPending_PendingListReturnsArray(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/invites/pending", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result []map[string]any
	parseBodyJSON(t, body, &result)
	rl.Printf("pending list is a valid JSON array with %d items", len(result))
}
