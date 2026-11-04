// Package api_test — security_scopes_test.go
//
// Tests for per-endpoint scope enforcement.
// Ported from emergent.memory/apps/server/tests/e2e/security_scopes_test.go
//
// In standalone mode the server only has one registered API key (e2e-test-user).
// Scope enforcement is tested using project-scoped emt_* tokens with limited scopes.
package api_test

import (
	"net/http"
	"strings"
	"testing"
)

// ─── Auth required (no token → 401) ─────────────────────────────────────────

func TestSecurityScopes_Documents_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/documents", "", projectID, nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestSecurityScopes_Chat_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/chat/conversations", "", projectID, nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestSecurityScopes_Graph_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/graph/objects/search", "", projectID, nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestSecurityScopes_Chunks_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/chunks", "", projectID, nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestSecurityScopes_Search_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "POST", "/api/search/unified", "", projectID, jsonBody(map[string]any{"query": "test"}))
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestSecurityScopes_Projects_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/projects", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestSecurityScopes_Orgs_RequiresAuth(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/orgs", "", "", nil)
	mustStatus(t, resp, http.StatusUnauthorized)
}

// ─── Full-auth token succeeds ────────────────────────────────────────────────

func TestSecurityScopes_Documents_AllowsFullToken(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/documents", e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusOK)
}

func TestSecurityScopes_Chat_AllowsFullToken(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/chat/conversations", e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusOK)
}

func TestSecurityScopes_Graph_AllowsFullToken(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/graph/objects/search", e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusOK)
}

func TestSecurityScopes_Chunks_AllowsFullToken(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	resp := doAPILogged(t, rl, "GET", "/api/chunks", e2eTestToken(), projectID, nil)
	mustStatus(t, resp, http.StatusOK)
}

func TestSecurityScopes_Orgs_AllowsFullToken(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	resp := doAPILogged(t, rl, "GET", "/api/orgs", e2eTestToken(), "", nil)
	mustStatus(t, resp, http.StatusOK)
}

// ─── Scope enforcement via emt_* tokens ─────────────────────────────────────

// TestSecurityScopes_Documents_DeniesTokenWithoutDocumentsRead verifies that a
// project-scoped token missing documents:read is rejected with 403.
func TestSecurityScopes_Documents_DeniesTokenWithoutDocumentsRead(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	_, limitedToken := createToken(t, projectID, uniqueName("schema-only"), []string{"schema:read"})

	resp := doAPILogged(t, rl, "GET", "/api/documents", limitedToken, projectID, nil)
	mustStatus(t, resp, http.StatusForbidden)
	rl.AssertionStep("schema:read forbidden from documents:read", http.StatusForbidden, resp.StatusCode, nil)
}

// TestSecurityScopes_Documents_AllowsTokenWithDocumentsRead verifies that a
// token with documents:read can list documents. documents:read replaces the
// retired data:read umbrella.
func TestSecurityScopes_Documents_AllowsTokenWithDocumentsRead(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	_, readToken := createToken(t, projectID, uniqueName("documents-read"), []string{"documents:read"})

	resp := doAPILogged(t, rl, "GET", "/api/documents", readToken, projectID, nil)
	mustStatus(t, resp, http.StatusOK)
	rl.AssertionStep("documents:read allowed on documents", http.StatusOK, resp.StatusCode, nil)
}

// TestSecurityScopes_Documents_DeniesTokenWithoutDocumentsWrite verifies that a
// read-only token is rejected with 403 on POST /api/documents.
func TestSecurityScopes_Documents_DeniesTokenWithoutDocumentsWrite(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	_, readToken := createToken(t, projectID, uniqueName("documents-read-only"), []string{"documents:read"})

	resp := doAPILogged(t, rl, "POST", "/api/documents", readToken, projectID, jsonBody(map[string]any{
		"filename": "test.txt",
		"content":  "test content",
	}))
	mustStatus(t, resp, http.StatusForbidden)
	rl.AssertionStep("documents:read-only forbidden from documents write", http.StatusForbidden, resp.StatusCode, nil)
}

// TestSecurityScopes_RetiredDataScopes_RejectedAtMint pins the retirement of the
// coarse data:read / data:write umbrellas on the user-facing token API: creating
// a token that requests either is a 400, and an update may not reintroduce them.
// Existing tokens that already hold one keep working (ScopeImplies unchanged) —
// that is covered by the read/write behaviour above.
func TestSecurityScopes_RetiredDataScopes_RejectedAtMint(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	for _, retired := range []string{"data:read", "data:write"} {
		resp := doAPILogged(t, rl, "POST", "/api/projects/"+projectID+"/tokens", e2eTestToken(), "", jsonBody(map[string]any{
			"name":   uniqueName("retired-" + strings.ReplaceAll(retired, ":", "-")),
			"scopes": []string{retired},
		}))
		mustStatus(t, resp, http.StatusBadRequest)
		rl.AssertionStep("create token with "+retired+" rejected", http.StatusBadRequest, resp.StatusCode, nil)
	}

	// A token can still be created with the fine-grained replacement...
	tokenID, _ := createToken(t, projectID, uniqueName("fine-grained"), []string{"documents:read"})

	// ...and an update that reintroduces a retired umbrella is rejected.
	resp := doAPILogged(t, rl, "PATCH", "/api/projects/"+projectID+"/tokens/"+tokenID, e2eTestToken(), "", jsonBody(map[string]any{
		"scopes": []string{"documents:read", "data:write"},
	}))
	mustStatus(t, resp, http.StatusBadRequest)
	rl.AssertionStep("update token with data:write rejected", http.StatusBadRequest, resp.StatusCode, nil)
}

// TestSecurityScopes_Chunks_DeniesTokenWithoutChunksRead verifies chunks endpoint
// enforces scope.
func TestSecurityScopes_Chunks_DeniesTokenWithoutChunksRead(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	_, schemaToken := createToken(t, projectID, uniqueName("schema-only-chunks"), []string{"schema:read"})

	resp := doAPILogged(t, rl, "GET", "/api/chunks", schemaToken, projectID, nil)
	mustStatus(t, resp, http.StatusForbidden)
	rl.AssertionStep("schema:read forbidden from chunks", http.StatusForbidden, resp.StatusCode, nil)
}

// TestSecurityScopes_Chat_DeniesTokenWithoutChatScope verifies chat endpoint
// enforces scope.
func TestSecurityScopes_Chat_DeniesTokenWithoutChatScope(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	_, documentsToken := createToken(t, projectID, uniqueName("documents-only"), []string{"documents:read"})

	resp := doAPILogged(t, rl, "GET", "/api/chat/conversations", documentsToken, projectID, nil)
	mustStatus(t, resp, http.StatusForbidden)
	rl.AssertionStep("documents:read forbidden from chat", http.StatusForbidden, resp.StatusCode, nil)
}
