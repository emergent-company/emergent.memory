package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// apiTokenFixture is one memory DTO sample shared by the client tests.
func apiTokenFixture() string {
	return `{"id":"t1","name":"CI deploy","tokenPrefix":"emt_abc123def","scopes":["data:read","data:write"],"createdAt":"2026-09-01T10:00:00Z","lastUsedAt":"2026-09-02T11:00:00Z","isRevoked":false}`
}

// sessionCtx returns a context carrying session credentials for the
// session-scoped client tests.
func apiTokenSessionCtx() context.Context {
	return withSessionContext(context.Background(), &sessionContext{
		Token:     "sess-token",
		ProjectID: "sess-proj",
		OrgID:     "sess-org",
	})
}

// --- project-scoped client methods ---

func TestListAPITokens(t *testing.T) {
	var gotPath, gotMethod, gotAuth, gotProj string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		gotAuth = r.Header.Get("Authorization")
		gotProj = r.Header.Get("X-Project-ID")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"tokens":[`+apiTokenFixture()+`],"total":1}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "static-token", "static-proj")
	tokens, err := m.ListAPITokens(apiTokenSessionCtx())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/projects/sess-proj/tokens" || gotMethod != http.MethodGet {
		t.Errorf("request = %s %s, want GET /api/projects/sess-proj/tokens", gotMethod, gotPath)
	}
	if gotAuth != "Bearer sess-token" {
		t.Errorf("auth = %q, want session bearer", gotAuth)
	}
	if gotProj != "sess-proj" {
		t.Errorf("X-Project-ID = %q, want sess-proj", gotProj)
	}
	if len(tokens) != 1 {
		t.Fatalf("got %d tokens, want 1", len(tokens))
	}
	tok := tokens[0]
	if tok.ID != "t1" || tok.Name != "CI deploy" || tok.TokenPrefix != "emt_abc123def" || tok.IsRevoked {
		t.Errorf("token = %+v", tok)
	}
	if len(tok.Scopes) != 2 || tok.Scopes[0] != "data:read" || tok.Scopes[1] != "data:write" {
		t.Errorf("scopes = %v", tok.Scopes)
	}
	if tok.CreatedAt != "2026-09-01T10:00:00Z" {
		t.Errorf("createdAt = %q", tok.CreatedAt)
	}
	if tok.LastUsedAt == nil {
		t.Fatal("lastUsedAt should decode")
	}
	if want := time.Date(2026, 9, 2, 11, 0, 0, 0, time.UTC); !tok.LastUsedAt.Equal(want) {
		t.Errorf("lastUsedAt = %v, want %v", tok.LastUsedAt, want)
	}
	if tok.Token != "" {
		t.Errorf("list tokens must never carry plaintext, got %q", tok.Token)
	}
}

func TestListAPITokensEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"tokens":[],"total":0}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	tokens, err := m.ListAPITokens(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 0 {
		t.Fatalf("want 0 tokens, got %+v", tokens)
	}
}

func TestCreateAPIToken(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody CreateAPITokenRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"t9","name":"ci","tokenPrefix":"emt_ci_secret","scopes":["data:read"],"createdAt":"2026-09-01T10:00:00Z","isRevoked":false,"token":"emt_ci_secret_full_plaintext"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj")
	resp, err := m.CreateAPIToken(apiTokenSessionCtx(), "ci", []string{"data:read"})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/projects/sess-proj/tokens" || gotMethod != http.MethodPost {
		t.Errorf("request = %s %s, want POST /api/projects/sess-proj/tokens", gotMethod, gotPath)
	}
	if gotBody.Name != "ci" || len(gotBody.Scopes) != 1 || gotBody.Scopes[0] != "data:read" {
		t.Errorf("body = %+v", gotBody)
	}
	if resp.ID != "t9" || resp.Name != "ci" || resp.Token != "emt_ci_secret_full_plaintext" {
		t.Errorf("response = %+v", resp)
	}
}

func TestCreateAPITokenValidation400(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"code":"validation_error","message":"scopes must contain at least one scope"}}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	_, err := m.CreateAPIToken(context.Background(), "ci", nil)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "memory 400 validation_error") {
		t.Errorf("error = %q, want memory 400 validation_error", err)
	}
}

func TestCreateAPITokenDuplicateName409(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"error":{"code":"token_name_exists","message":"an API token named \"ci\" already exists"}}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	_, err := m.CreateAPIToken(context.Background(), "ci", []string{"data:read"})
	if err == nil || !strings.Contains(err.Error(), "memory 409 token_name_exists") {
		t.Fatalf("error = %v, want memory 409 token_name_exists", err)
	}
}

func TestCreateAPITokenForbidden403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":{"code":"viewer-write-scope-denied","message":"project viewers may not create tokens with write scopes"}}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	_, err := m.CreateAPIToken(context.Background(), "ci", []string{"data:write"})
	if err == nil || !strings.Contains(err.Error(), "memory 403 viewer-write-scope-denied") {
		t.Fatalf("error = %v, want memory 403 viewer-write-scope-denied", err)
	}
}

func TestGetAPIToken(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"t1","name":"ci","tokenPrefix":"emt_abc","scopes":["data:read"],"createdAt":"2026-09-01T10:00:00Z","isRevoked":false,"token":"emt_get_plaintext"}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	tok, err := m.GetAPIToken(apiTokenSessionCtx(), "t1")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/projects/sess-proj/tokens/t1" {
		t.Errorf("path = %q", gotPath)
	}
	if tok.ID != "t1" || tok.Token != "emt_get_plaintext" {
		t.Errorf("token = %+v", tok)
	}
}

func TestUpdateAPITokenScopes(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"t1","name":"ci","tokenPrefix":"emt_abc","scopes":["data:read","data:write"],"createdAt":"2026-09-01T10:00:00Z","isRevoked":false}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	tok, err := m.UpdateAPITokenScopes(apiTokenSessionCtx(), "t1", []string{"data:read", "data:write"})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/projects/sess-proj/tokens/t1" || gotMethod != http.MethodPatch {
		t.Errorf("request = %s %s, want PATCH /api/projects/sess-proj/tokens/t1", gotMethod, gotPath)
	}
	// the update body carries only scopes — never name.
	if len(gotBody) != 1 {
		t.Errorf("body = %v, want only scopes", gotBody)
	}
	scopes, ok := gotBody["scopes"].([]any)
	if !ok || len(scopes) != 2 {
		t.Fatalf("body.scopes = %v", gotBody["scopes"])
	}
	if len(tok.Scopes) != 2 || tok.Scopes[1] != "data:write" {
		t.Errorf("token = %+v", tok)
	}
}

func TestRevokeAPIToken(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	if err := m.RevokeAPIToken(apiTokenSessionCtx(), "t1"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/projects/sess-proj/tokens/t1" || gotMethod != http.MethodDelete {
		t.Errorf("request = %s %s, want DELETE /api/projects/sess-proj/tokens/t1", gotMethod, gotPath)
	}
}

func TestRegenerateAPIToken(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"t2","name":"ci","tokenPrefix":"emt_new","scopes":["data:read"],"createdAt":"2026-09-03T10:00:00Z","isRevoked":false,"token":"emt_new_plaintext"}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	resp, err := m.RegenerateAPIToken(apiTokenSessionCtx(), "t1")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/projects/sess-proj/tokens/t1/regenerate" || gotMethod != http.MethodPost {
		t.Errorf("request = %s %s, want POST /api/projects/sess-proj/tokens/t1/regenerate", gotMethod, gotPath)
	}
	if resp.ID != "t2" || resp.Token != "emt_new_plaintext" {
		t.Errorf("response = %+v", resp)
	}
}

func TestRegenerateAPITokenAlreadyRevoked409(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"error":{"code":"token_already_revoked","message":"cannot regenerate a revoked token"}}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	_, err := m.RegenerateAPIToken(context.Background(), "t1")
	if err == nil || !strings.Contains(err.Error(), "memory 409 token_already_revoked") {
		t.Fatalf("error = %v, want memory 409 token_already_revoked", err)
	}
}

func TestAPITokensUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	m := NewMemoryClient(url, "tok", "proj")
	if _, err := m.ListAPITokens(context.Background()); err == nil {
		t.Error("ListAPITokens: want transport error, got nil")
	}
	if _, err := m.CreateAPIToken(context.Background(), "ci", []string{"data:read"}); err == nil {
		t.Error("CreateAPIToken: want transport error, got nil")
	}
	if _, err := m.GetAPIToken(context.Background(), "t1"); err == nil {
		t.Error("GetAPIToken: want transport error, got nil")
	}
	if _, err := m.UpdateAPITokenScopes(context.Background(), "t1", []string{"data:read"}); err == nil {
		t.Error("UpdateAPITokenScopes: want transport error, got nil")
	}
	if err := m.RevokeAPIToken(context.Background(), "t1"); err == nil {
		t.Error("RevokeAPIToken: want transport error, got nil")
	}
	if _, err := m.RegenerateAPIToken(context.Background(), "t1"); err == nil {
		t.Error("RegenerateAPIToken: want transport error, got nil")
	}
}

// --- account-scoped client methods ---
//
// Account tokens are user-bound: the calls go to /api/tokens with no project
// path and no X-Project-ID header. These tests run with a plain (session-less)
// context so the static credentials are used and the header is provably absent.

func TestListAccountAPITokens(t *testing.T) {
	var gotPath, gotAuth, gotProj string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotProj = r.Header.Get("X-Project-ID")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"tokens":[`+apiTokenFixture()+`],"total":1}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "static-token", "static-proj")
	tokens, err := m.ListAccountAPITokens(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/tokens" {
		t.Errorf("path = %q, want /api/tokens", gotPath)
	}
	if gotAuth != "Bearer static-token" {
		t.Errorf("auth = %q", gotAuth)
	}
	if gotProj != "" {
		t.Errorf("X-Project-ID = %q, want empty (account tokens are not project-scoped)", gotProj)
	}
	if len(tokens) != 1 || tokens[0].Name != "CI deploy" {
		t.Fatalf("tokens = %+v", tokens)
	}
}

func TestListAccountAPITokensEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"tokens":[],"total":0}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	tokens, err := m.ListAccountAPITokens(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 0 {
		t.Fatalf("want 0 tokens, got %+v", tokens)
	}
}

func TestCreateAccountAPIToken(t *testing.T) {
	var gotPath, gotMethod, gotProj string
	var gotBody CreateAPITokenRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		gotProj = r.Header.Get("X-Project-ID")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"a1","name":"acct","tokenPrefix":"emt_acct","scopes":["search"],"createdAt":"2026-09-01T10:00:00Z","isRevoked":false,"token":"emt_acct_plaintext"}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	resp, err := m.CreateAccountAPIToken(context.Background(), "acct", []string{"search"})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/tokens" || gotMethod != http.MethodPost {
		t.Errorf("request = %s %s, want POST /api/tokens", gotMethod, gotPath)
	}
	if gotProj != "" {
		t.Errorf("X-Project-ID = %q, want empty", gotProj)
	}
	if gotBody.Name != "acct" || len(gotBody.Scopes) != 1 || gotBody.Scopes[0] != "search" {
		t.Errorf("body = %+v", gotBody)
	}
	if resp.ID != "a1" || resp.Token != "emt_acct_plaintext" {
		t.Errorf("response = %+v", resp)
	}
}

// TestCreateAccountAPITokenSessionBearer asserts account calls still use the
// session bearer when one is attached (the account is derived from the token).
func TestCreateAccountAPITokenSessionBearer(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"a1","name":"acct","tokenPrefix":"emt_acct","scopes":["search"],"createdAt":"2026-09-01T10:00:00Z","token":"emt_x"}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "static-token", "static-proj")
	if _, err := m.CreateAccountAPIToken(apiTokenSessionCtx(), "acct", []string{"search"}); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer sess-token" {
		t.Errorf("auth = %q, want the session bearer", gotAuth)
	}
}

func TestCreateAccountAPITokenDuplicate409(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"error":{"code":"token_name_exists","message":"duplicate"}}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	_, err := m.CreateAccountAPIToken(context.Background(), "acct", []string{"search"})
	if err == nil || !strings.Contains(err.Error(), "memory 409 token_name_exists") {
		t.Fatalf("error = %v", err)
	}
}

func TestGetAccountAPIToken(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"a1","name":"acct","tokenPrefix":"emt_a","scopes":["search"],"createdAt":"2026-09-01T10:00:00Z","isRevoked":false,"token":"emt_getacct_plaintext"}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	tok, err := m.GetAccountAPIToken(context.Background(), "a1")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/tokens/a1" {
		t.Errorf("path = %q, want /api/tokens/a1", gotPath)
	}
	if tok.Token != "emt_getacct_plaintext" {
		t.Errorf("token = %+v", tok)
	}
}

func TestUpdateAccountAPITokenScopes(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"a1","name":"acct","tokenPrefix":"emt_a","scopes":["search","graph:read"],"createdAt":"2026-09-01T10:00:00Z"}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	tok, err := m.UpdateAccountAPITokenScopes(context.Background(), "a1", []string{"search", "graph:read"})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/tokens/a1" || gotMethod != http.MethodPatch {
		t.Errorf("request = %s %s, want PATCH /api/tokens/a1", gotMethod, gotPath)
	}
	if len(gotBody) != 1 {
		t.Errorf("body = %v, want only scopes", gotBody)
	}
	if len(tok.Scopes) != 2 {
		t.Errorf("token = %+v", tok)
	}
}

func TestRevokeAccountAPIToken(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	if err := m.RevokeAccountAPIToken(context.Background(), "a1"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/tokens/a1" || gotMethod != http.MethodDelete {
		t.Errorf("request = %s %s, want DELETE /api/tokens/a1", gotMethod, gotPath)
	}
}

func TestRegenerateAccountAPIToken(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"a2","name":"acct","tokenPrefix":"emt_b","scopes":["search"],"createdAt":"2026-09-03T10:00:00Z","token":"emt_new"}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	resp, err := m.RegenerateAccountAPIToken(context.Background(), "a1")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/tokens/a1/regenerate" || gotMethod != http.MethodPost {
		t.Errorf("request = %s %s, want POST /api/tokens/a1/regenerate", gotMethod, gotPath)
	}
	if resp.ID != "a2" || resp.Token != "emt_new" {
		t.Errorf("response = %+v", resp)
	}
}

// TestAccountAPITokensForbidden403 covers the account 403 gate (admin:all
// scope held by a non-org-admin).
func TestAccountAPITokensForbidden403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":{"code":"admin-all-scope-denied","message":"only org admins may hold admin:all"}}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	_, err := m.CreateAccountAPIToken(context.Background(), "acct", []string{"admin:all"})
	if err == nil || !strings.Contains(err.Error(), "memory 403 admin-all-scope-denied") {
		t.Fatalf("error = %v", err)
	}
}

func TestAccountAPITokensUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	m := NewMemoryClient(url, "tok", "proj")
	if _, err := m.ListAccountAPITokens(context.Background()); err == nil {
		t.Error("ListAccountAPITokens: want transport error, got nil")
	}
	if _, err := m.CreateAccountAPIToken(context.Background(), "acct", []string{"search"}); err == nil {
		t.Error("CreateAccountAPIToken: want transport error, got nil")
	}
	if _, err := m.GetAccountAPIToken(context.Background(), "a1"); err == nil {
		t.Error("GetAccountAPIToken: want transport error, got nil")
	}
	if _, err := m.UpdateAccountAPITokenScopes(context.Background(), "a1", []string{"search"}); err == nil {
		t.Error("UpdateAccountAPITokenScopes: want transport error, got nil")
	}
	if err := m.RevokeAccountAPIToken(context.Background(), "a1"); err == nil {
		t.Error("RevokeAccountAPIToken: want transport error, got nil")
	}
	if _, err := m.RegenerateAccountAPIToken(context.Background(), "a1"); err == nil {
		t.Error("RegenerateAccountAPIToken: want transport error, got nil")
	}
}
