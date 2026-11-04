// Package api_test — users_test.go
//
// Tests for the users search API (/api/users/search).
// Ported from emergent.memory/apps/server/tests/e2e/users_test.go
package api_test

import (
	"net/http"
	"testing"
)

func TestSearchUsers_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	rl.Section("GET /api/users/search without auth")
	resp := doAPILogged(t, rl, "GET", "/api/users/search?email=test", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestSearchUsers_RequiresEmailParam(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	rl.Section("GET /api/users/search without email param")
	resp := doAPILogged(t, rl, "GET", "/api/users/search", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusBadRequest)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, ok := result["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error object in response")
	}
	msg, _ := errObj["message"].(string)
	if msg == "" {
		t.Error("expected non-empty error message")
	}
	rl.Printf("got expected error: %s", msg)
}

func TestSearchUsers_MinLength(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	rl.Section("GET /api/users/search with single-char email")
	resp := doAPILogged(t, rl, "GET", "/api/users/search?email=a", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusBadRequest)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, ok := result["error"].(map[string]any)
	if !ok {
		t.Fatal("expected error object in response")
	}
	msg, _ := errObj["message"].(string)
	if msg == "" {
		t.Error("expected non-empty error message about minimum length")
	}
	rl.Printf("got expected min-length error: %s", msg)
}

func TestSearchUsers_ReturnsUsersArray(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	// In external server mode there are no pre-seeded fixture users.
	// We verify the endpoint responds 200 with a users array (possibly empty).
	rl.Section("GET /api/users/search — returns users array")
	resp := doAPILogged(t, rl, "GET", "/api/users/search?email=test", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	if _, ok := result["users"]; !ok {
		t.Error("expected 'users' key in response")
	}
	users, ok := result["users"].([]any)
	if !ok {
		t.Fatal("expected users to be an array")
	}
	rl.Printf("search returned %d users", len(users))
}

func TestSearchUsers_ExcludesCurrentUser(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	// Get current user ID from /api/auth/me
	rl.Section("GET /api/auth/me to get current user")
	meResp := doAPILogged(t, rl, "GET", "/api/auth/me", e2eTestToken(), "", nil)
	meBody := mustStatus(t, meResp, http.StatusOK)
	var me map[string]any
	parseBodyJSON(t, meBody, &me)
	currentUserID, _ := me["user_id"].(string)
	if currentUserID == "" {
		t.Fatal("could not get current user_id from /api/auth/me")
	}

	// Search and verify current user is excluded
	rl.Section("GET /api/users/search — current user excluded")
	resp := doAPILogged(t, rl, "GET", "/api/users/search?email=test", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	users, _ := result["users"].([]any)
	for _, u := range users {
		user, _ := u.(map[string]any)
		if user["id"] == currentUserID {
			t.Errorf("current user %s should be excluded from search results", currentUserID)
		}
	}
	rl.Printf("verified current user %s not in %d results", currentUserID, len(users))
}

func TestSearchUsers_NoMatches(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	rl.Section("GET /api/users/search — no matches")
	resp := doAPILogged(t, rl, "GET", "/api/users/search?email=nonexistent@nowhere.invalid", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	users, ok := result["users"].([]any)
	if !ok {
		t.Fatal("expected users array in response")
	}
	if len(users) != 0 {
		t.Errorf("expected empty users array for no matches, got %d", len(users))
	}
	rl.Printf("no-match search returned empty users array")
}

func TestSearchUsers_CaseInsensitive(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	// Both searches should return the same count
	rl.Section("GET /api/users/search — case insensitive")
	lower := doAPILogged(t, rl, "GET", "/api/users/search?email=test", e2eTestToken(), "", nil)
	lowerBody := mustStatus(t, lower, http.StatusOK)

	upper := doAPILogged(t, rl, "GET", "/api/users/search?email=TEST", e2eTestToken(), "", nil)
	upperBody := mustStatus(t, upper, http.StatusOK)

	var lowerResult, upperResult map[string]any
	parseBodyJSON(t, lowerBody, &lowerResult)
	parseBodyJSON(t, upperBody, &upperResult)

	lowerUsers, _ := lowerResult["users"].([]any)
	upperUsers, _ := upperResult["users"].([]any)
	if len(lowerUsers) != len(upperUsers) {
		t.Errorf("case-insensitive mismatch: lowercase returned %d, uppercase returned %d",
			len(lowerUsers), len(upperUsers))
	}
	rl.Printf("case-insensitive search consistent: %d results each", len(lowerUsers))
}

func TestSearchUsers_ResponseStructure(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	rl.Section("GET /api/users/search — response structure")
	resp := doAPILogged(t, rl, "GET", "/api/users/search?email=nonexistent@nowhere.invalid", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	if _, ok := result["users"]; !ok {
		t.Error("response missing 'users' field")
	}
	rl.Printf("response structure OK")
}
