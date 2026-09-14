package main

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// --- API tokens (Memory REST: project-scoped + account-scoped) ---
//
// Memory exposes two API-token surfaces with identical verbs and DTOs:
//   - project-scoped  GET/POST            /api/projects/{projectId}/tokens
//                    GET/PATCH/DELETE     /api/projects/{projectId}/tokens/{tokenId}
//                    POST                 /api/projects/{projectId}/tokens/{tokenId}/regenerate
//   - account-level   GET/POST            /api/tokens
//                    GET/PATCH/DELETE     /api/tokens/{tokenId}
//                    POST                 /api/tokens/{tokenId}/regenerate
//
// Tokens are emt_*-prefixed, scoped, and revocable. The plaintext is returned
// at create/regenerate; list responses carry metadata only.

// APIToken mirrors memory's API-token DTO: full metadata plus, on
// create/regenerate/encrypted-get responses, the one-time plaintext Token
// (emt_*). List responses never carry Token. Times are RFC3339 on the wire
// (CreatedAt is a string; nullable timestamps are pointers).
type APIToken struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	TokenPrefix string     `json:"tokenPrefix"`
	Scopes      []string   `json:"scopes"`
	CreatedAt   string     `json:"createdAt"`
	LastUsedAt  *time.Time `json:"lastUsedAt,omitempty"`
	IsRevoked   bool       `json:"isRevoked"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	Token       string     `json:"token,omitempty"`
}

// APITokenList is the wrapped list response shape {tokens, total}.
type APITokenList struct {
	Tokens []APIToken `json:"tokens"`
	Total  int        `json:"total"`
}

// CreateAPITokenRequest is the create body {name, scopes}.
type CreateAPITokenRequest struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

// APITokenCreateResponse is the create/regenerate response: the persisted
// token metadata plus the plaintext secret (shown exactly once in the UI).
type APITokenCreateResponse struct {
	APIToken
}

// updateAPITokenScopesBody is the PATCH /{tokenId} body: only the scopes —
// the name is immutable and never re-sent.
type updateAPITokenScopesBody struct {
	Scopes []string `json:"scopes"`
}

// apiTokenScopes lists every scope Memory accepts for API tokens (memory's
// ValidApiTokenScopes, 23 total). admin:all is server-gated to org
// admins/superadmins, so the UI omits it from pickers and surfaces the 403 if
// Memory rejects it.
var apiTokenScopes = []string{
	// coarse-grained
	"schema:read", "schema:write", "data:read", "data:write",
	"agents:read", "agents:write", "projects:read", "projects:write", "chat:use",
	// fine-grained
	"graph:read", "graph:write", "schema:migrate",
	"branches:read", "branches:write", "search",
	"journal:read", "journal:write",
	"skills:read", "skills:write",
	"documents:read", "documents:write",
	// admin
	"admin", "admin:all",
}

// validAPITokenScope reports whether scope is in the supported reference set.
func validAPITokenScope(scope string) bool {
	for _, s := range apiTokenScopes {
		if s == scope {
			return true
		}
	}
	return false
}

// projectAPITokensPath builds the project-scoped tokens base path with the
// request's active project embedded (session project, else the static one).
func (m *MemoryClient) projectAPITokensPath(ctx context.Context) string {
	return "/api/projects/" + url.PathEscape(m.projectIDFor(ctx)) + "/tokens"
}

// --- project-scoped methods ---

// ListAPITokens lists the active project's API tokens (metadata only).
func (m *MemoryClient) ListAPITokens(ctx context.Context) ([]APIToken, error) {
	var out APITokenList
	if err := m.doH(ctx, http.MethodGet, m.projectAPITokensPath(ctx), nil, sessionHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return out.Tokens, nil
}

// CreateAPIToken creates a project-scoped token and returns it with the
// one-time plaintext secret.
func (m *MemoryClient) CreateAPIToken(ctx context.Context, name string, scopes []string) (*APITokenCreateResponse, error) {
	var out APITokenCreateResponse
	body := CreateAPITokenRequest{Name: name, Scopes: scopes}
	if err := m.doH(ctx, http.MethodPost, m.projectAPITokensPath(ctx), body, sessionHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAPIToken fetches one project token's metadata by id. When Memory
// encryption is configured the response may also carry the decrypted plaintext
// in Token.
func (m *MemoryClient) GetAPIToken(ctx context.Context, tokenID string) (*APIToken, error) {
	var out APIToken
	if err := m.doH(ctx, http.MethodGet, m.projectAPITokensPath(ctx)+"/"+url.PathEscape(tokenID), nil, sessionHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateAPITokenScopes replaces a non-revoked project token's scopes.
func (m *MemoryClient) UpdateAPITokenScopes(ctx context.Context, tokenID string, scopes []string) (*APIToken, error) {
	var out APIToken
	body := updateAPITokenScopesBody{Scopes: scopes}
	if err := m.doH(ctx, http.MethodPatch, m.projectAPITokensPath(ctx)+"/"+url.PathEscape(tokenID), body, sessionHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RevokeAPIToken revokes a project token immediately. The response body is
// discarded; only transport/HTTP errors matter.
func (m *MemoryClient) RevokeAPIToken(ctx context.Context, tokenID string) error {
	return m.doH(ctx, http.MethodDelete, m.projectAPITokensPath(ctx)+"/"+url.PathEscape(tokenID), nil, sessionHeaders(ctx), nil)
}

// RegenerateAPIToken atomically revokes a project token and issues a
// replacement (same name and scopes), returning it with the one-time secret.
func (m *MemoryClient) RegenerateAPIToken(ctx context.Context, tokenID string) (*APITokenCreateResponse, error) {
	var out APITokenCreateResponse
	if err := m.doH(ctx, http.MethodPost, m.projectAPITokensPath(ctx)+"/"+url.PathEscape(tokenID)+"/regenerate", nil, sessionHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// --- account-scoped methods ---

// ListAccountAPITokens lists the signed-in user's account-level API tokens
// (metadata only). Account tokens are user-bound, not project-bound — no
// project header/path is involved.
func (m *MemoryClient) ListAccountAPITokens(ctx context.Context) ([]APIToken, error) {
	var out APITokenList
	if err := m.do(ctx, http.MethodGet, "/api/tokens", nil, &out); err != nil {
		return nil, err
	}
	return out.Tokens, nil
}

// CreateAccountAPIToken creates an account-level token and returns it with the
// one-time plaintext secret.
func (m *MemoryClient) CreateAccountAPIToken(ctx context.Context, name string, scopes []string) (*APITokenCreateResponse, error) {
	var out APITokenCreateResponse
	body := CreateAPITokenRequest{Name: name, Scopes: scopes}
	if err := m.do(ctx, http.MethodPost, "/api/tokens", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAccountAPIToken fetches one account token's metadata by id; with Memory
// encryption configured the response may also carry the decrypted plaintext in
// Token.
func (m *MemoryClient) GetAccountAPIToken(ctx context.Context, tokenID string) (*APIToken, error) {
	var out APIToken
	if err := m.do(ctx, http.MethodGet, "/api/tokens/"+url.PathEscape(tokenID), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateAccountAPITokenScopes replaces a non-revoked account token's scopes.
func (m *MemoryClient) UpdateAccountAPITokenScopes(ctx context.Context, tokenID string, scopes []string) (*APIToken, error) {
	var out APIToken
	body := updateAPITokenScopesBody{Scopes: scopes}
	if err := m.do(ctx, http.MethodPatch, "/api/tokens/"+url.PathEscape(tokenID), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RevokeAccountAPIToken revokes an account token immediately. The response
// body is discarded; only transport/HTTP errors matter.
func (m *MemoryClient) RevokeAccountAPIToken(ctx context.Context, tokenID string) error {
	return m.do(ctx, http.MethodDelete, "/api/tokens/"+url.PathEscape(tokenID), nil, nil)
}

// RegenerateAccountAPIToken atomically revokes an account token and issues a
// replacement (same name and scopes), returning it with the one-time secret.
func (m *MemoryClient) RegenerateAccountAPIToken(ctx context.Context, tokenID string) (*APITokenCreateResponse, error) {
	var out APITokenCreateResponse
	if err := m.do(ctx, http.MethodPost, "/api/tokens/"+url.PathEscape(tokenID)+"/regenerate", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
