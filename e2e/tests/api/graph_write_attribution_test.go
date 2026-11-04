// Package api_test — graph_write_attribution_test.go
//
// Over-HTTP proof that the write stamp is recorded and queryable (slice 1 of
// #2376; issue #2468). The domain-level stamp tests assert the server's response
// structs; this asserts what a client actually receives on the wire.
//
// The harness's own token authenticates as a *session*, and a session write
// carries no credential id (migration 00257's credential_id_pairs_with_kind), so
// this test writes with a project token as well: that is the only way to cover
// the api_key + credential_id path a real integration uses.
package api_test

import (
	"net/http"
	"strings"
	"testing"
)

const noSuchRunID = "00000000-0000-0000-0000-000000000000"

// TestGraphWriteAttribution_HTTP exercises the stamp read surface end to end:
// writes through a project token and through the session token disclose their
// attribution, and the listing endpoints filter on it.
func TestGraphWriteAttribution_HTTP(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)

	// ── 1. Two writers: a project token (api_key) and the session token ──────
	rl.Section("Create a project token")
	tokenID, rawToken := createToken(t, projectID, "e2e-stamp-api-token", []string{"data:read", "data:write"})
	if tokenID == "" || rawToken == "" {
		t.Fatalf("createToken returned an incomplete token: id=%q token=%q", tokenID, rawToken)
	}
	rl.Printf("project token credential id: %s", tokenID)

	rl.Section("Write one object with the project token")
	tokenResp := doAPILogged(t, rl, "POST", "/api/graph/objects", rawToken, projectID, jsonBody(map[string]any{
		"type":       "StampE2EObject",
		"properties": map[string]any{"title": "written by a project token"},
	}))
	tokenBody := mustStatus(t, tokenResp, http.StatusCreated)

	var tokenObj map[string]any
	parseBodyJSON(t, tokenBody, &tokenObj)
	tokenObjectID, _ := tokenObj["id"].(string)
	if tokenObjectID == "" {
		t.Fatalf("created object has no id: %s", tokenBody)
	}
	t.Cleanup(func() { deleteGraphObject(t, projectID, tokenObjectID) })

	if got := tokenObj["credential_kind"]; got != "api_key" {
		t.Errorf("expected credential_kind=api_key on a project-token write, got %v", got)
	}
	if got := tokenObj["credential_id"]; got != tokenID {
		t.Errorf("expected credential_id=%s on a project-token write, got %v", tokenID, got)
	}
	if got := tokenObj["data_class"]; got != "synthesized" {
		t.Errorf("expected data_class=synthesized, got %v", got)
	}
	rl.Printf("object %s stamped api_key/%s synthesized", tokenObjectID, tokenID)

	rl.Section("Write one object with the session token")
	sessionResp := doAPILogged(t, rl, "POST", "/api/graph/objects", e2eTestToken(), projectID, jsonBody(map[string]any{
		"type":       "StampE2EObject",
		"properties": map[string]any{"title": "written by a human session"},
	}))
	sessionBody := mustStatus(t, sessionResp, http.StatusCreated)

	var sessionObj map[string]any
	parseBodyJSON(t, sessionBody, &sessionObj)
	sessionObjectID, _ := sessionObj["id"].(string)
	if sessionObjectID == "" {
		t.Fatalf("session-written object has no id: %s", sessionBody)
	}
	t.Cleanup(func() { deleteGraphObject(t, projectID, sessionObjectID) })

	if got := sessionObj["credential_kind"]; got != "session" {
		t.Errorf("expected credential_kind=session on a session write, got %v", got)
	}
	if got, present := sessionObj["credential_id"]; present && got != nil && got != "" {
		t.Errorf("expected no credential_id on a session write, got %v", got)
	}

	// ── 2. Filter by credential kind and data class ──────────────────────────
	listResp := doAPILogged(t, rl, "GET",
		"/api/graph/objects/search?type=StampE2EObject&credential_kind=api_key&data_class=synthesized",
		e2eTestToken(), projectID, nil)
	listBody := mustStatus(t, listResp, http.StatusOK)

	var listed map[string]any
	parseBodyJSON(t, listBody, &listed)
	items, _ := listed["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected exactly 1 object for credential_kind=api_key&data_class=synthesized, got %d: %s", len(items), listBody)
	}
	item, _ := items[0].(map[string]any)
	if item["id"] != tokenObjectID {
		t.Errorf("expected listed id %s, got %v", tokenObjectID, item["id"])
	}
	if item["credential_kind"] != "api_key" {
		t.Errorf("expected the listed row to disclose credential_kind=api_key, got %v", item["credential_kind"])
	}
	if item["credential_id"] != tokenID {
		t.Errorf("expected the listed row to disclose credential_id=%s, got %v", tokenID, item["credential_id"])
	}

	// ── 3. The session writer is separable from the token writer ─────────────
	sessionListResp := doAPILogged(t, rl, "GET",
		"/api/graph/objects/search?type=StampE2EObject&credential_kind=session",
		e2eTestToken(), projectID, nil)
	sessionListBody := mustStatus(t, sessionListResp, http.StatusOK)
	var sessionListed map[string]any
	parseBodyJSON(t, sessionListBody, &sessionListed)
	sessionItems, _ := sessionListed["items"].([]any)
	if len(sessionItems) != 1 {
		t.Fatalf("expected exactly 1 session-written object, got %d: %s", len(sessionItems), sessionListBody)
	}
	if got, _ := sessionItems[0].(map[string]any)["id"]; got != sessionObjectID {
		t.Errorf("expected the session filter to return %s, got %v", sessionObjectID, got)
	}

	// ── 4. Filter by the credential id the write disclosed ───────────────────
	byCredResp := doAPILogged(t, rl, "GET",
		"/api/graph/objects/search?type=StampE2EObject&credential_id="+tokenID,
		e2eTestToken(), projectID, nil)
	byCredBody := mustStatus(t, byCredResp, http.StatusOK)
	var byCred map[string]any
	parseBodyJSON(t, byCredBody, &byCred)
	credItems, _ := byCred["items"].([]any)
	if len(credItems) != 1 {
		t.Errorf("expected exactly 1 object for credential_id=%s, got %d: %s", tokenID, len(credItems), byCredBody)
	}

	// ── 5. A well-formed filter that matches nothing is an empty page ────────
	emptyResp := doAPILogged(t, rl, "GET",
		"/api/graph/objects/search?run_id="+noSuchRunID,
		e2eTestToken(), projectID, nil)
	emptyBody := mustStatus(t, emptyResp, http.StatusOK)
	var empty map[string]any
	parseBodyJSON(t, emptyBody, &empty)
	if emptyItems, _ := empty["items"].([]any); len(emptyItems) != 0 {
		t.Errorf("expected an empty page for an unmatched run_id, got %d items", len(emptyItems))
	}
	if total, ok := empty["total"].(float64); !ok || total != 0 {
		t.Errorf("expected total=0 for an unmatched run_id, got %v", empty["total"])
	}

	// ── 6. The count endpoint honours the same filters ───────────────────────
	countResp := doAPILogged(t, rl, "GET",
		"/api/graph/objects/count?type=StampE2EObject&credential_kind=api_key",
		e2eTestToken(), projectID, nil)
	countBody := mustStatus(t, countResp, http.StatusOK)
	var count map[string]any
	parseBodyJSON(t, countBody, &count)
	if got, _ := count["count"].(float64); got != 1 {
		t.Errorf("expected count=1 for credential_kind=api_key, got %v (%s)", count["count"], countBody)
	}

	// ── 7. Unknown enum values are client errors, not empty pages ────────────
	badKindResp := doAPILogged(t, rl, "GET",
		"/api/graph/objects/search?credential_kind=oauth",
		e2eTestToken(), projectID, nil)
	badKindBody := mustStatus(t, badKindResp, http.StatusBadRequest)
	if !strings.Contains(badKindBody, "credential_kind") {
		t.Errorf("expected the 400 to name credential_kind, got %s", badKindBody)
	}

	badClassResp := doAPILogged(t, rl, "GET",
		"/api/graph/objects/search?data_class=derived",
		e2eTestToken(), projectID, nil)
	mustStatus(t, badClassResp, http.StatusBadRequest)

	badRunResp := doAPILogged(t, rl, "GET",
		"/api/graph/objects/search?run_id=not-a-uuid",
		e2eTestToken(), projectID, nil)
	mustStatus(t, badRunResp, http.StatusBadRequest)

	// ── 8. Relationships record the stamp and filter on it ───────────────────
	relSrc := createGraphObject(t, projectID, "StampE2EObject", "rel-src", nil)
	relDst := createGraphObject(t, projectID, "StampE2EObject", "rel-dst", nil)

	relResp := doAPILogged(t, rl, "POST", "/api/graph/relationships", rawToken, projectID, jsonBody(map[string]any{
		"type":   "STAMP_E2E_LINKS",
		"src_id": relSrc,
		"dst_id": relDst,
	}))
	relBody := mustStatus(t, relResp, http.StatusCreated)

	var createdRel map[string]any
	parseBodyJSON(t, relBody, &createdRel)
	if got := createdRel["credential_kind"]; got != "api_key" {
		t.Errorf("expected credential_kind=api_key on a project-token relationship write, got %v", got)
	}
	if got := createdRel["credential_id"]; got != tokenID {
		t.Errorf("expected credential_id=%s on a project-token relationship write, got %v", tokenID, got)
	}

	relListResp := doAPILogged(t, rl, "GET",
		"/api/graph/relationships/search?credential_id="+tokenID,
		e2eTestToken(), projectID, nil)
	relListBody := mustStatus(t, relListResp, http.StatusOK)
	var relList map[string]any
	parseBodyJSON(t, relListBody, &relList)
	relItems, _ := relList["items"].([]any)
	if len(relItems) != 1 {
		t.Errorf("expected exactly 1 relationship for credential_id=%s, got %d: %s", tokenID, len(relItems), relListBody)
	}

	relKindResp := doAPILogged(t, rl, "GET",
		"/api/graph/relationships/search?credential_kind=session",
		e2eTestToken(), projectID, nil)
	relKindBody := mustStatus(t, relKindResp, http.StatusOK)
	var relKind map[string]any
	parseBodyJSON(t, relKindBody, &relKind)
	if kindItems, _ := relKind["items"].([]any); len(kindItems) != 0 {
		t.Errorf("expected no session-written relationships, got %d", len(kindItems))
	}

	relBadResp := doAPILogged(t, rl, "GET",
		"/api/graph/relationships/search?data_class=source",
		e2eTestToken(), projectID, nil)
	relBadBody := mustStatus(t, relBadResp, http.StatusBadRequest)
	if !strings.Contains(relBadBody, "data_class") {
		t.Errorf("expected the 400 to name data_class, got %s", relBadBody)
	}

	rl.Printf("stamp disclosed and filtered over HTTP; invalid enums and malformed ids rejected with 400")
}
