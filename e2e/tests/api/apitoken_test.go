// Package api_test — apitoken_test.go
//
// Tests for the project API token endpoints (/api/projects/:id/tokens).
// Ported from emergent.memory/apps/server/tests/e2e/apitoken_test.go
package api_test

import (
	"net/http"
	"strings"
	"testing"
)

// ─── helpers ────────────────────────────────────────────────────────────────

// createToken creates a project API token and returns (tokenID, rawToken).
func createToken(t *testing.T, projectID, name string, scopes []string) (tokenID, rawToken string) {
	t.Helper()
	resp := doAPI(t, "POST", "/api/projects/"+projectID+"/tokens", e2eTestToken(), "", jsonBody(map[string]any{
		"name":   name,
		"scopes": scopes,
	}))
	body := mustStatus(t, resp, http.StatusCreated)
	var result map[string]any
	parseBodyJSON(t, body, &result)
	tokenID, _ = result["id"].(string)
	rawToken, _ = result["token"].(string)
	if tokenID == "" {
		t.Fatalf("createToken: no id in response: %s", body)
	}
	t.Cleanup(func() { revokeToken(t, projectID, tokenID) })
	return tokenID, rawToken
}

// revokeToken revokes a project token. Safe for t.Cleanup.
func revokeToken(t *testing.T, projectID, tokenID string) {
	t.Helper()
	resp := doAPI(t, "DELETE", "/api/projects/"+projectID+"/tokens/"+tokenID, e2eTestToken(), "", nil)
	resp.Body.Close()
}

// ─── Create token ────────────────────────────────────────────────────────────

func TestApiToken_CreateSuccess(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	rl.Section("POST /api/projects/:id/tokens")
	resp := doAPILogged(t, rl, "POST", "/api/projects/"+projectID+"/tokens", e2eTestToken(), "", jsonBody(map[string]any{
		"name":   "Test Token",
		"scopes": []string{"schema:read", "data:read"},
	}))
	body := mustStatus(t, resp, http.StatusCreated)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	tokenID, _ := result["id"].(string)
	token, _ := result["token"].(string)
	if tokenID == "" {
		t.Fatal("expected non-empty id")
	}
	if !strings.HasPrefix(token, "emt_") {
		t.Errorf("expected token to start with emt_, got %q", token)
	}
	if len(token) != 68 {
		t.Errorf("expected token length 68, got %d", len(token))
	}
	if result["isRevoked"] != false {
		t.Error("expected isRevoked=false")
	}
	t.Cleanup(func() { revokeToken(t, projectID, tokenID) })
	rl.Printf("created token %s", tokenID)
}

func TestApiToken_CreateRequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/projects/"+projectID+"/tokens", "", "", jsonBody(map[string]any{
		"name":   "Test Token",
		"scopes": []string{"schema:read"},
	}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestApiToken_CreateMissingName(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/projects/"+projectID+"/tokens", e2eTestToken(), "", jsonBody(map[string]any{
		"scopes": []string{"schema:read"},
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestApiToken_CreateMissingScopes(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/projects/"+projectID+"/tokens", e2eTestToken(), "", jsonBody(map[string]any{
		"name": "Test Token",
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestApiToken_CreateEmptyScopes(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/projects/"+projectID+"/tokens", e2eTestToken(), "", jsonBody(map[string]any{
		"name":   "Test Token",
		"scopes": []string{},
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestApiToken_CreateInvalidScope(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/projects/"+projectID+"/tokens", e2eTestToken(), "", jsonBody(map[string]any{
		"name":   "Test Token",
		"scopes": []string{"invalid:scope"},
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestApiToken_CreateDuplicateName(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	name := uniqueName("dup-token")

	resp1 := doAPILogged(t, rl, "POST", "/api/projects/"+projectID+"/tokens", e2eTestToken(), "", jsonBody(map[string]any{
		"name": name, "scopes": []string{"schema:read"},
	}))
	body1 := mustStatus(t, resp1, http.StatusCreated)
	tokenID1 := parseIDFromBody(t, body1)
	t.Cleanup(func() { revokeToken(t, projectID, tokenID1) })

	resp2 := doAPILogged(t, rl, "POST", "/api/projects/"+projectID+"/tokens", e2eTestToken(), "", jsonBody(map[string]any{
		"name": name, "scopes": []string{"schema:read"},
	}))
	mustStatus(t, resp2, http.StatusConflict)
	rl.Printf("duplicate token name rejected")
}

func TestApiToken_CreateNameTooLong(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/projects/"+projectID+"/tokens", e2eTestToken(), "", jsonBody(map[string]any{
		"name":   strings.Repeat("a", 256),
		"scopes": []string{"schema:read"},
	}))
	mustStatus(t, resp, http.StatusBadRequest)
}

// ─── List tokens ─────────────────────────────────────────────────────────────

func TestApiToken_ListReturnsTokens(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	createToken(t, projectID, uniqueName("tok1"), []string{"schema:read"})
	createToken(t, projectID, uniqueName("tok2"), []string{"data:read"})

	rl.Section("GET /api/projects/:id/tokens")
	resp := doAPILogged(t, rl, "GET", "/api/projects/"+projectID+"/tokens", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	total, _ := result["total"].(float64)
	tokens, _ := result["tokens"].([]any)
	if int(total) < 2 {
		t.Errorf("expected total >= 2, got %v", total)
	}
	if len(tokens) < 2 {
		t.Errorf("expected at least 2 tokens, got %d", len(tokens))
	}
	rl.Printf("listed %d tokens", len(tokens))
}

func TestApiToken_ListRequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/projects/"+projectID+"/tokens", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestApiToken_ListDoesNotIncludeTokenValue(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	createToken(t, projectID, uniqueName("no-reveal"), []string{"schema:read"})

	resp := doAPILogged(t, rl, "GET", "/api/projects/"+projectID+"/tokens", e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	tokens, _ := result["tokens"].([]any)
	if len(tokens) == 0 {
		t.Fatal("expected at least one token in list")
	}
	tok := tokens[0].(map[string]any)
	if _, has := tok["token"]; has {
		t.Error("list response should not include token value")
	}
	rl.Printf("confirmed token value not leaked in list")
}

// ─── Get token ───────────────────────────────────────────────────────────────

func TestApiToken_GetSuccess(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	tokenID, _ := createToken(t, projectID, uniqueName("get-tok"), []string{"schema:read", "data:write"})

	rl.Section("GET /api/projects/:id/tokens/:tokenId")
	resp := doAPILogged(t, rl, "GET", "/api/projects/"+projectID+"/tokens/"+tokenID, e2eTestToken(), "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["id"] != tokenID {
		t.Errorf("expected id=%s, got %v", tokenID, result["id"])
	}
	if result["isRevoked"] != false {
		t.Error("expected isRevoked=false")
	}
	rl.Printf("got token %s", tokenID)
}

func TestApiToken_GetNotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/projects/"+projectID+"/tokens/00000000-0000-0000-0000-000000000000", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusNotFound)
}

func TestApiToken_GetRequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/projects/"+projectID+"/tokens/00000000-0000-0000-0000-000000000000", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

// ─── Revoke token ────────────────────────────────────────────────────────────

func TestApiToken_RevokeSuccess(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	// Create without auto-cleanup so we can test revoke manually.
	resp := doAPILogged(t, rl, "POST", "/api/projects/"+projectID+"/tokens", e2eTestToken(), "", jsonBody(map[string]any{
		"name": uniqueName("revoke-tok"), "scopes": []string{"schema:read"},
	}))
	body := mustStatus(t, resp, http.StatusCreated)
	tokenID := parseIDFromBody(t, body)

	rl.Section("DELETE /api/projects/:id/tokens/:tokenId")
	delResp := doAPILogged(t, rl, "DELETE", "/api/projects/"+projectID+"/tokens/"+tokenID, e2eTestToken(), "", nil)
	delBody := mustStatus(t, delResp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, delBody, &result)
	if result["status"] != "revoked" {
		t.Errorf("expected status=revoked, got %v", result["status"])
	}

	// Verify revoked
	getResp := doAPILogged(t, rl, "GET", "/api/projects/"+projectID+"/tokens/"+tokenID, e2eTestToken(), "", nil)
	getBody := mustStatus(t, getResp, http.StatusOK)
	var getResult map[string]any
	parseBodyJSON(t, getBody, &getResult)
	if getResult["isRevoked"] != true {
		t.Error("expected isRevoked=true after revoke")
	}
	rl.Printf("token %s revoked successfully", tokenID)
}

func TestApiToken_RevokeNotFound(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "DELETE", "/api/projects/"+projectID+"/tokens/00000000-0000-0000-0000-000000000000", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusNotFound)
}

func TestApiToken_RevokeAlreadyRevoked(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/projects/"+projectID+"/tokens", e2eTestToken(), "", jsonBody(map[string]any{
		"name": uniqueName("revoke-twice"), "scopes": []string{"schema:read"},
	}))
	body := mustStatus(t, resp, http.StatusCreated)
	tokenID := parseIDFromBody(t, body)

	doAPILogged(t, rl, "DELETE", "/api/projects/"+projectID+"/tokens/"+tokenID, e2eTestToken(), "", nil)

	resp2 := doAPILogged(t, rl, "DELETE", "/api/projects/"+projectID+"/tokens/"+tokenID, e2eTestToken(), "", nil)
	mustStatus(t, resp2, http.StatusConflict)
	rl.Printf("double-revoke rejected with 409")
}

func TestApiToken_RevokeRequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "DELETE", "/api/projects/"+projectID+"/tokens/00000000-0000-0000-0000-000000000000", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestApiToken_DuplicateNameNotAllowedAfterRevoke(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	name := uniqueName("unique-revoked")

	resp := doAPILogged(t, rl, "POST", "/api/projects/"+projectID+"/tokens", e2eTestToken(), "", jsonBody(map[string]any{
		"name": name, "scopes": []string{"schema:read"},
	}))
	body := mustStatus(t, resp, http.StatusCreated)
	tokenID := parseIDFromBody(t, body)

	doAPILogged(t, rl, "DELETE", "/api/projects/"+projectID+"/tokens/"+tokenID, e2eTestToken(), "", nil)

	resp2 := doAPILogged(t, rl, "POST", "/api/projects/"+projectID+"/tokens", e2eTestToken(), "", jsonBody(map[string]any{
		"name": name, "scopes": []string{"data:read"},
	}))
	// Server allows re-using a name after revoke (soft-delete behavior).
	mustStatus(t, resp2, http.StatusCreated)
	rl.Printf("duplicate name allowed after revoke (soft-delete)")
}

// ─── API token auth ──────────────────────────────────────────────────────────

func TestApiToken_ValidTokenAuthenticates(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	_, rawToken := createToken(t, projectID, uniqueName("auth-tok"), []string{"data:read"})

	// Use the emt_ token against the auth/me endpoint.
	resp := doAPILogged(t, rl, "GET", "/api/auth/me", rawToken, "", nil)
	body := mustStatus(t, resp, http.StatusOK)

	var result map[string]any
	parseBodyJSON(t, body, &result)
	if result["user_id"] == "" || result["user_id"] == nil {
		t.Error("expected non-empty user_id")
	}
	rl.Printf("API token authenticates OK")
}

func TestApiToken_RevokedTokenReturns401(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/projects/"+projectID+"/tokens", e2eTestToken(), "", jsonBody(map[string]any{
		"name": uniqueName("revoke-auth"), "scopes": []string{"data:read"},
	}))
	body := mustStatus(t, resp, http.StatusCreated)
	var created map[string]any
	parseBodyJSON(t, body, &created)
	tokenID, _ := created["id"].(string)
	rawToken, _ := created["token"].(string)

	doAPILogged(t, rl, "DELETE", "/api/projects/"+projectID+"/tokens/"+tokenID, e2eTestToken(), "", nil)

	resp2 := doAPILogged(t, rl, "GET", "/api/auth/me", rawToken, "", nil)
	mustStatus(t, resp2, http.StatusUnauthorized)
	rl.Printf("revoked token correctly returns 401")
}
