// Package api_test — invite_lifecycle_test.go
//
// End-to-end tests for the invitation lifecycle: create validation, list,
// revoke, accept, and decline. The accept and decline happy paths require a
// second standalone identity (STANDALONE_API_KEY_2 / STANDALONE_USER_EMAIL_2),
// because the server verifies the invitee email belongs to the authenticated
// user before accepting or declining.
package api_test

import (
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

// inviteeEmail returns the secondary standalone user's email (the invite target).
func inviteeEmail() string {
	if v := os.Getenv("STANDALONE_USER_EMAIL_2"); v != "" {
		return v
	}
	return "invitee@localhost"
}

// inviteeToken returns the secondary standalone user's API key.
func inviteeToken() string {
	if v := os.Getenv("STANDALONE_API_KEY_2"); v != "" {
		return v
	}
	return "e2e-test-user-2"
}

// requireSecondIdentity skips the test when the secondary standalone identity is
// not configured.
func requireSecondIdentity(t *testing.T) {
	t.Helper()
	if os.Getenv("STANDALONE_API_KEY_2") == "" {
		t.Skip("secondary standalone identity not configured (set STANDALONE_API_KEY_2 and STANDALONE_USER_EMAIL_2)")
	}
}

// acceptInvite posts an accept request as the given user and returns the response.
func acceptInvite(t *testing.T, rl *runLog, userToken, inviteToken string) *http.Response {
	t.Helper()
	return doAPILogged(t, rl, "POST", "/api/invites/accept", userToken, "", jsonBody(map[string]any{"token": inviteToken}))
}

// declineInvite posts a decline request as the given user and returns the response.
func declineInvite(t *testing.T, rl *runLog, userToken, inviteID string) *http.Response {
	t.Helper()
	return doAPILogged(t, rl, "POST", "/api/invites/"+inviteID+"/decline", userToken, "", nil)
}

// projectInvites returns the list-by-project response as []map[string]any.
func projectInvites(t *testing.T, rl *runLog, projectID string) []map[string]any {
	t.Helper()
	resp := doAPILogged(t, rl, "GET", "/api/projects/"+projectID+"/invites", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)
	var out []map[string]any
	parseBodyJSON(t, body, &out)
	return out
}

// findInviteByID returns the invite with the given id, or nil.
func findInviteByID(invites []map[string]any, id string) map[string]any {
	for _, inv := range invites {
		if inv["id"] == id {
			return inv
		}
	}
	return nil
}

// ─── Create validation ──────────────────────────────────────────────────────

func TestInvite_CreateInvalidRole(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	_, orgID := setupProjectLogged(t, rl)
	body := jsonBody(map[string]any{"email": "x@example.com", "orgId": orgID, "role": "superadmin"})
	resp := doAPILogged(t, rl, "POST", "/api/invites", e2eTestToken(), "", body)
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestInvite_CreateInvalidEmail(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	_, orgID := setupProjectLogged(t, rl)
	body := jsonBody(map[string]any{"email": "not-an-email", "orgId": orgID, "role": "org_admin"})
	resp := doAPILogged(t, rl, "POST", "/api/invites", e2eTestToken(), "", body)
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestInvite_CreateDuplicate(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	_, orgID := setupProjectLogged(t, rl)
	email := fmt.Sprintf("dup-%d@example.com", time.Now().UnixNano())
	createInvite(t, email, orgID, "", "org_admin")

	body := jsonBody(map[string]any{"email": email, "orgId": orgID, "role": "org_admin"})
	resp := doAPILogged(t, rl, "POST", "/api/invites", e2eTestToken(), "", body)
	mustStatus(t, resp, http.StatusBadRequest)
}

// ─── List by project ────────────────────────────────────────────────────────

func TestInvite_ListByProject(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, orgID := setupProjectLogged(t, rl)
	email := fmt.Sprintf("list-%d@example.com", time.Now().UnixNano())
	inv := createInvite(t, email, orgID, projectID, "project_user")

	invites := projectInvites(t, rl, projectID)
	found := findInviteByID(invites, inv["id"].(string))
	if found == nil {
		t.Fatalf("invite %v not found in project list", inv["id"])
	}
	if found["status"] != "pending" {
		t.Errorf("expected status pending, got %v", found["status"])
	}
}

// ─── Revoke ─────────────────────────────────────────────────────────────────

func TestInvite_Revoke(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, orgID := setupProjectLogged(t, rl)
	email := fmt.Sprintf("revoke-%d@example.com", time.Now().UnixNano())
	inv := createInvite(t, email, orgID, projectID, "project_user")
	id := inv["id"].(string)

	resp := doAPILogged(t, rl, "DELETE", "/api/invites/"+id, e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusNoContent)

	found := findInviteByID(projectInvites(t, rl, projectID), id)
	if found == nil {
		t.Fatalf("invite %v disappeared after revoke", id)
	}
	if found["status"] != "revoked" {
		t.Errorf("expected status revoked, got %v", found["status"])
	}

	// Revoking a non-pending invite returns 404.
	resp2 := doAPILogged(t, rl, "DELETE", "/api/invites/"+id, e2eTestToken(), "", nil)
	mustStatus(t, resp2, http.StatusNotFound)
}

// ─── Accept ─────────────────────────────────────────────────────────────────

func TestInvite_AcceptSuccess(t *testing.T) {
	requireSecondIdentity(t)
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, orgID := setupProjectLogged(t, rl)
	inv := createInvite(t, inviteeEmail(), orgID, projectID, "project_user")
	id := inv["id"].(string)
	token := inv["token"].(string)

	resp := acceptInvite(t, rl, inviteeToken(), token)
	mustStatus(t, resp, http.StatusOK)

	found := findInviteByID(projectInvites(t, rl, projectID), id)
	if found == nil {
		t.Fatalf("invite %v disappeared after accept", id)
	}
	if found["status"] != "accepted" {
		t.Errorf("expected status accepted, got %v", found["status"])
	}
}

func TestInvite_AcceptWrongToken(t *testing.T) {
	requireSecondIdentity(t)
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	setupProjectLogged(t, rl)
	resp := acceptInvite(t, rl, e2eTestToken(), "deadbeef-dead-beef-dead-beefdeadbeef")
	mustStatus(t, resp, http.StatusNotFound)
}

func TestInvite_AcceptWrongEmail(t *testing.T) {
	requireSecondIdentity(t)
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, orgID := setupProjectLogged(t, rl)
	inv := createInvite(t, inviteeEmail(), orgID, projectID, "project_user")

	// The primary user tries to accept an invite addressed to the invitee.
	resp := acceptInvite(t, rl, e2eTestToken(), inv["token"].(string))
	mustStatus(t, resp, http.StatusForbidden)
}

func TestInvite_AcceptTwice(t *testing.T) {
	requireSecondIdentity(t)
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, orgID := setupProjectLogged(t, rl)
	inv := createInvite(t, inviteeEmail(), orgID, projectID, "project_user")
	token := inv["token"].(string)

	mustStatus(t, acceptInvite(t, rl, inviteeToken(), token), http.StatusOK)
	mustStatus(t, acceptInvite(t, rl, inviteeToken(), token), http.StatusNotFound)
}

// ─── Decline ────────────────────────────────────────────────────────────────

func TestInvite_DeclineSuccess(t *testing.T) {
	requireSecondIdentity(t)
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, orgID := setupProjectLogged(t, rl)
	inv := createInvite(t, inviteeEmail(), orgID, projectID, "project_user")
	id := inv["id"].(string)

	mustStatus(t, declineInvite(t, rl, inviteeToken(), id), http.StatusOK)

	found := findInviteByID(projectInvites(t, rl, projectID), id)
	if found == nil {
		t.Fatalf("invite %v disappeared after decline", id)
	}
	if found["status"] != "declined" {
		t.Errorf("expected status declined, got %v", found["status"])
	}

	// A declined invite can no longer be accepted.
	mustStatus(t, acceptInvite(t, rl, inviteeToken(), inv["token"].(string)), http.StatusNotFound)
}

func TestInvite_DeclineWrongUser(t *testing.T) {
	requireSecondIdentity(t)
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, orgID := setupProjectLogged(t, rl)
	inv := createInvite(t, inviteeEmail(), orgID, projectID, "project_user")

	// The primary user tries to decline an invite addressed to the invitee.
	resp := declineInvite(t, rl, e2eTestToken(), inv["id"].(string))
	mustStatus(t, resp, http.StatusForbidden)
}
