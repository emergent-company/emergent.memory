// Package api_test — userprofile_test.go
//
// Tests for the user profile API endpoints (/api/user/profile).
// Ported from emergent.memory/apps/server/tests/e2e/userprofile_test.go
package api_test

import (
	"net/http"
	"strings"
	"testing"
)

// =============================================================================
// GET /api/user/profile
// =============================================================================

func TestUserProfile_GetProfile_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/user/profile", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestUserProfile_GetProfile_Success(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)
	skipIfStandaloneMode(t)

	resp := doAPILogged(t, rl, "GET", "/api/user/profile", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var profile map[string]any
	parseBodyJSON(t, body, &profile)

	for _, field := range []string{"id", "subjectId", "firstName", "lastName", "email"} {
		if _, ok := profile[field]; !ok {
			t.Errorf("profile missing field %q", field)
		}
	}
	rl.Printf("user profile fetched successfully")
}

func TestUserProfile_GetProfile_ResponseStructure(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/user/profile", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var profile map[string]any
	parseBodyJSON(t, body, &profile)

	for _, field := range []string{"id", "subjectId", "email"} {
		if _, ok := profile[field]; !ok {
			t.Errorf("profile missing required field %q", field)
		}
	}

	id, ok := profile["id"].(string)
	if !ok || len(id) != 36 {
		t.Errorf("id should be a UUID string of length 36, got %q", id)
	}
	rl.Printf("profile response structure verified")
}

// =============================================================================
// PUT /api/user/profile
// =============================================================================

func TestUserProfile_UpdateProfile_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "PUT", "/api/user/profile", "", "", jsonBody(map[string]any{"firstName": "Updated"}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestUserProfile_UpdateProfile_UpdateFirstName(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "PUT", "/api/user/profile", e2eTestToken(), "",
		jsonBody(map[string]any{"firstName": "UpdatedFirst"}))
	body := mustStatus(t, resp, http.StatusOK)

	var profile map[string]any
	parseBodyJSON(t, body, &profile)
	if profile["firstName"] != "UpdatedFirst" {
		t.Errorf("expected firstName=UpdatedFirst, got %v", profile["firstName"])
	}

	// Verify the change persists
	getResp := doAPILogged(t, rl, "GET", "/api/user/profile", e2eTestToken(), "", nil)
	getBody := mustStatus(t, getResp, http.StatusOK)
	var getProfile map[string]any
	parseBodyJSON(t, getBody, &getProfile)
	if getProfile["firstName"] != "UpdatedFirst" {
		t.Errorf("persisted firstName should be UpdatedFirst, got %v", getProfile["firstName"])
	}
	rl.Printf("firstName updated and persisted")
}

func TestUserProfile_UpdateProfile_UpdateLastName(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "PUT", "/api/user/profile", e2eTestToken(), "",
		jsonBody(map[string]any{"lastName": "UpdatedLast"}))
	body := mustStatus(t, resp, http.StatusOK)

	var profile map[string]any
	parseBodyJSON(t, body, &profile)
	if profile["lastName"] != "UpdatedLast" {
		t.Errorf("expected lastName=UpdatedLast, got %v", profile["lastName"])
	}
}

func TestUserProfile_UpdateProfile_UpdateDisplayName(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "PUT", "/api/user/profile", e2eTestToken(), "",
		jsonBody(map[string]any{"displayName": "Test Display Name"}))
	body := mustStatus(t, resp, http.StatusOK)

	var profile map[string]any
	parseBodyJSON(t, body, &profile)
	if profile["displayName"] != "Test Display Name" {
		t.Errorf("expected displayName=Test Display Name, got %v", profile["displayName"])
	}
}

func TestUserProfile_UpdateProfile_UpdatePhoneE164(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "PUT", "/api/user/profile", e2eTestToken(), "",
		jsonBody(map[string]any{"phoneE164": "+15551234567"}))
	body := mustStatus(t, resp, http.StatusOK)

	var profile map[string]any
	parseBodyJSON(t, body, &profile)
	if profile["phoneE164"] != "+15551234567" {
		t.Errorf("expected phoneE164=+15551234567, got %v", profile["phoneE164"])
	}
}

func TestUserProfile_UpdateProfile_UpdateMultipleFields(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "PUT", "/api/user/profile", e2eTestToken(), "",
		jsonBody(map[string]any{
			"firstName":   "NewFirst",
			"lastName":    "NewLast",
			"displayName": "New Display",
		}))
	body := mustStatus(t, resp, http.StatusOK)

	var profile map[string]any
	parseBodyJSON(t, body, &profile)
	if profile["firstName"] != "NewFirst" {
		t.Errorf("expected firstName=NewFirst, got %v", profile["firstName"])
	}
	if profile["lastName"] != "NewLast" {
		t.Errorf("expected lastName=NewLast, got %v", profile["lastName"])
	}
	if profile["displayName"] != "New Display" {
		t.Errorf("expected displayName=New Display, got %v", profile["displayName"])
	}
}

func TestUserProfile_UpdateProfile_RequiresAtLeastOneField(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "PUT", "/api/user/profile", e2eTestToken(), "", jsonBody(map[string]any{}))
	body := mustStatus(t, resp, http.StatusBadRequest)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, _ := result["error"].(map[string]any)
	msg, _ := errObj["message"].(string)
	assertContains(t, msg, "at least one field")
}

func TestUserProfile_UpdateProfile_FirstNameTooLong(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	longName := strings.Repeat("a", 101)
	resp := doAPILogged(t, rl, "PUT", "/api/user/profile", e2eTestToken(), "",
		jsonBody(map[string]any{"firstName": longName}))
	body := mustStatus(t, resp, http.StatusBadRequest)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, _ := result["error"].(map[string]any)
	msg, _ := errObj["message"].(string)
	assertContains(t, msg, "firstName")
}

func TestUserProfile_UpdateProfile_LastNameTooLong(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	longName := strings.Repeat("b", 101)
	resp := doAPILogged(t, rl, "PUT", "/api/user/profile", e2eTestToken(), "",
		jsonBody(map[string]any{"lastName": longName}))
	body := mustStatus(t, resp, http.StatusBadRequest)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, _ := result["error"].(map[string]any)
	msg, _ := errObj["message"].(string)
	assertContains(t, msg, "lastName")
}

func TestUserProfile_UpdateProfile_DisplayNameTooLong(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	longName := strings.Repeat("c", 201)
	resp := doAPILogged(t, rl, "PUT", "/api/user/profile", e2eTestToken(), "",
		jsonBody(map[string]any{"displayName": longName}))
	body := mustStatus(t, resp, http.StatusBadRequest)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, _ := result["error"].(map[string]any)
	msg, _ := errObj["message"].(string)
	assertContains(t, msg, "displayName")
}

func TestUserProfile_UpdateProfile_PhoneTooLong(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	longPhone := strings.Repeat("1", 21)
	resp := doAPILogged(t, rl, "PUT", "/api/user/profile", e2eTestToken(), "",
		jsonBody(map[string]any{"phoneE164": longPhone}))
	body := mustStatus(t, resp, http.StatusBadRequest)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	errObj, _ := result["error"].(map[string]any)
	msg, _ := errObj["message"].(string)
	assertContains(t, msg, "phoneE164")
}

func TestUserProfile_UpdateProfile_InvalidJSON(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	// Send raw invalid JSON
	resp := doAPILogged(t, rl, "PUT", "/api/user/profile", e2eTestToken(), "", []byte(`{invalid json`))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestUserProfile_UpdateProfile_EmptyFirstName(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	// Setting firstName to empty string should be allowed
	resp := doAPILogged(t, rl, "PUT", "/api/user/profile", e2eTestToken(), "",
		jsonBody(map[string]any{"firstName": ""}))
	mustStatus(t, resp, http.StatusOK)
}

func TestUserProfile_UpdateProfile_NullFieldsIgnored(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	// First: set a displayName
	doAPILogged(t, rl, "PUT", "/api/user/profile", e2eTestToken(), "",
		jsonBody(map[string]any{"displayName": "Keep This"})).Body.Close()

	// Update firstName only — displayName should remain
	resp := doAPILogged(t, rl, "PUT", "/api/user/profile", e2eTestToken(), "",
		jsonBody(map[string]any{"firstName": "NewFirst"}))
	body := mustStatus(t, resp, http.StatusOK)

	var profile map[string]any
	parseBodyJSON(t, body, &profile)
	if profile["firstName"] != "NewFirst" {
		t.Errorf("expected firstName=NewFirst, got %v", profile["firstName"])
	}
	if profile["displayName"] != "Keep This" {
		t.Errorf("expected displayName=Keep This to be preserved, got %v", profile["displayName"])
	}
	rl.Printf("partial update preserved unmodified fields")
}
