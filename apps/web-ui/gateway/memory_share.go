package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/getsentry/sentry-go"
)

// --- public agent-share wire types ------------------------------------------
//
// These mirror the server's share DTOs (apps/server/domain/agents/share_*.go).
// Field names match the wire JSON exactly; the gateway maps them into the
// client-facing shapes in share.go.

// SharePublicConfig is the sanitized public config served to anonymous end
// users. It never exposes prompt/system config or project/org names.
type SharePublicConfig struct {
	LinkID                   string  `json:"linkId"`
	AgentName                string  `json:"agentName"`
	AgentDescription         string  `json:"agentDescription,omitempty"`
	Model                    string  `json:"model,omitempty"`
	RequireEmail             bool    `json:"requireEmail"`
	ShowSessionList          bool    `json:"showSessionList"`
	MaxMessageChars          int     `json:"maxMessageChars"`
	AllowEndUserApprovals    bool    `json:"allowEndUserApprovals"`
	SandboxEnabled           bool    `json:"sandboxEnabled"`
	RetentionDays            int     `json:"retentionDays"`
	BudgetMaxMessages        int     `json:"budgetMaxMessages"`
	BudgetMaxTokens          int64   `json:"budgetMaxTokens"`
	BudgetMaxCostUSD         float64 `json:"budgetMaxCostUSD"`
	MaxActiveSessionsPerUser int     `json:"maxActiveSessionsPerUser"`
	MaxConcurrentRuns        int     `json:"maxConcurrentRuns"`
	WelcomeMessage           string  `json:"welcomeMessage,omitempty"`
	Icon                     string  `json:"icon,omitempty"`
	Color                    string  `json:"color,omitempty"`
}

// ShareSession is the end-user-facing representation of a share session.
type ShareSession struct {
	ID             string     `json:"id"`
	Title          *string    `json:"title,omitempty"`
	IsArchived     bool       `json:"isArchived"`
	LastActivityAt *time.Time `json:"lastActivityAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
}

// ShareMessage is one message in a share session transcript. The server sends
// plain text (role + content); the gateway adds an `html` field for assistant
// messages (sanitized markdown) so the client can render it directly.
type ShareMessage struct {
	Role    string `json:"role"`           // "user" | "assistant"
	Content string `json:"content"`        // plain text
	HTML    string `json:"html,omitempty"` // sanitized markdown (assistant only)
}

// ShareSessionDetail is the transcript-bearing response for
// GET /api/share/agent/sessions/:id: the list DTO plus its messages.
type ShareSessionDetail struct {
	ID         string         `json:"id"`
	Title      *string        `json:"title,omitempty"`
	IsArchived bool           `json:"isArchived"`
	Messages   []ShareMessage `json:"messages"`
	CreatedAt  time.Time      `json:"createdAt"`
}

// ShareQuestion is the subset of the server's agent-question DTO the share
// surface relays (pending tool-approval questions).
type ShareQuestion struct {
	ID       string         `json:"id"`
	RunID    string         `json:"runId"`
	Question string         `json:"question"`
	Proposal map[string]any `json:"proposal,omitempty"`
	Status   string         `json:"status"`
}

// ShareCreateSessionInput is the body for POST /api/share/agent/sessions. The
// end-user ref is NOT sent in the body — it rides the signed X-End-User-Ref
// header.
type ShareCreateSessionInput struct {
	Email string `json:"email,omitempty"`
	Title string `json:"title,omitempty"`
}

// ShareStreamInput is the body for POST /api/share/agent/stream. The end-user
// ref rides the signed header, not the body.
type ShareStreamInput struct {
	SessionID string `json:"sessionId"`
	Message   string `json:"message"`
}

// ShareApproveInput is the body for POST .../approvals/:questionId. The
// end-user ref rides the signed header, not the body.
type ShareApproveInput struct {
	Response string `json:"response"`
	Message  string `json:"message,omitempty"`
}

// --- owner share-link wire types --------------------------------------------

// ShareLink is the owner-facing representation of a share link.
type ShareLink struct {
	ID                string           `json:"id"`
	ProjectID         string           `json:"projectId"`
	AgentDefinitionID string           `json:"agentDefinitionId"`
	Label             string           `json:"label"`
	Config            *ShareLinkConfig `json:"config"`
	APITokenPrefix    string           `json:"apiTokenPrefix"`
	Token             string           `json:"token,omitempty"` // only at create/rotate
	CreatedAt         time.Time        `json:"createdAt"`
	UpdatedAt         time.Time        `json:"updatedAt"`
	LastUsedAt        *time.Time       `json:"lastUsedAt,omitempty"`
	RevokedAt         *time.Time       `json:"revokedAt,omitempty"`
	ExpiresAt         *time.Time       `json:"expiresAt,omitempty"`
}

// ShareLinkConfig is the effective per-link configuration (snake_case wire).
type ShareLinkConfig struct {
	LinkExpiryDays           int      `json:"link_expiry_days"`
	BudgetWindowSeconds      int      `json:"budget_window_seconds"`
	BudgetMaxMessages        int      `json:"budget_max_messages"`
	BudgetMaxTokens          int64    `json:"budget_max_tokens"`
	BudgetMaxCostUSD         float64  `json:"budget_max_cost_usd"`
	MaxActiveSessionsPerUser int      `json:"max_active_sessions_per_user"`
	MaxConcurrentRuns        int      `json:"max_concurrent_runs"`
	MaxApprovalsPerSession   int      `json:"max_approvals_per_session"`
	ApprovalTimeoutSeconds   int      `json:"approval_timeout_seconds"`
	MaxMessageChars          int      `json:"max_message_chars"`
	RetentionDays            int      `json:"retention_days"`
	RequireEmail             bool     `json:"require_email"`
	ShowSessionList          bool     `json:"show_session_list"`
	SandboxEnabled           bool     `json:"sandbox_enabled"`
	AllowEndUserApprovals    bool     `json:"allow_end_user_approvals"`
	ToolAllowlist            []string `json:"tool_allowlist,omitempty"`
}

// ShareLinkConfigInput is the request shape for create/PATCH config overrides.
// Pointer fields distinguish "absent" (keep default) from "explicit false".
type ShareLinkConfigInput struct {
	LinkExpiryDays           *int     `json:"link_expiry_days,omitempty"`
	BudgetMaxMessages        *int     `json:"budget_max_messages,omitempty"`
	BudgetMaxTokens          *int64   `json:"budget_max_tokens,omitempty"`
	MaxActiveSessionsPerUser *int     `json:"max_active_sessions_per_user,omitempty"`
	RequireEmail             *bool    `json:"require_email,omitempty"`
	ShowSessionList          *bool    `json:"show_session_list,omitempty"`
	SandboxEnabled           *bool    `json:"sandbox_enabled,omitempty"`
	ToolAllowlist            []string `json:"tool_allowlist,omitempty"`
	WelcomeMessage           *string  `json:"welcome_message,omitempty"`
}

// ShareLinkCreateInput is the body for POST .../agent-definitions/:id/share-links.
type ShareLinkCreateInput struct {
	Label  string                `json:"label"`
	Config *ShareLinkConfigInput `json:"config,omitempty"`
}

// --- public share request plumbing ------------------------------------------

// errShareRefSecretUnset is returned when a share call would need to send an
// end-user ref but SHARE_REF_SECRET is unset — the gateway fails closed rather
// than sending an unsigned ref.
var errShareRefSecretUnset = errors.New("share: SHARE_REF_SECRET not configured")

// shareRefHeaders builds the signed X-End-User-Ref header pair. It fails closed
// (rather than sending an unsigned ref) when the ref secret is unset.
func (m *MemoryClient) shareRefHeaders(endUserRef string) (map[string]string, error) {
	if m.shareRefSecret == "" {
		return nil, errShareRefSecretUnset
	}
	return map[string]string{
		"X-End-User-Ref":     endUserRef,
		"X-End-User-Ref-Sig": shareRefSig(m.shareRefSecret, endUserRef),
	}, nil
}

// doShare performs a request against the public share surface using an explicit
// bearer token (the decrypted share key). It deliberately sends NO
// X-Project-ID / X-Org-ID header and no session credentials: the server derives
// project/org from the bound link and ignores client headers. Requests are
// never retried (share operations are non-idempotent).
func (m *MemoryClient) doShare(ctx context.Context, method, path, token string, hdrs map[string]string, body any, out any) error {
	var rdBuf []byte
	hasBody := body != nil
	if hasBody {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdBuf = b
	}
	var rd io.Reader
	if hasBody {
		rd = bytes.NewReader(rdBuf)
	}
	req, err := http.NewRequestWithContext(ctx, method, m.baseURL+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if hasBody {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdrs {
		req.Header.Set(k, v)
	}
	span := sentry.StartSpan(ctx, "http.client", sentry.WithDescription(method+" "+path))
	resp, err := m.http.Do(req)
	span.Finish()
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxMemoryResponseBytes+1))
	if err != nil {
		return err
	}
	if len(raw) > maxMemoryResponseBytes {
		return fmt.Errorf("memory response exceeds %d bytes", maxMemoryResponseBytes)
	}
	if resp.StatusCode >= 400 {
		perr := parseMemoryError(resp.StatusCode, raw)
		if resp.StatusCode != http.StatusNotFound {
			log.Printf("memory API error: %s %s -> %d: %s", method, path, resp.StatusCode, truncateString(string(raw), 2048))
			captureMemoryError(method, path, resp.StatusCode, perr)
		}
		return perr
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// --- public share methods ---------------------------------------------------

func (m *MemoryClient) SharePublicConfig(ctx context.Context, token string) (*SharePublicConfig, error) {
	var out SharePublicConfig
	if err := m.doShare(ctx, http.MethodGet, "/api/share/agent", token, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (m *MemoryClient) ShareListSessions(ctx context.Context, token, endUserRef, filter string) ([]ShareSession, error) {
	hdrs, err := m.shareRefHeaders(endUserRef)
	if err != nil {
		return nil, err
	}
	path := "/api/share/agent/sessions"
	if filter != "" {
		path += "?filter=" + url.QueryEscape(filter)
	}
	var out []ShareSession
	if err := m.doShare(ctx, http.MethodGet, path, token, hdrs, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (m *MemoryClient) ShareGetSession(ctx context.Context, token, endUserRef, id string) (*ShareSessionDetail, error) {
	hdrs, err := m.shareRefHeaders(endUserRef)
	if err != nil {
		return nil, err
	}
	var out ShareSessionDetail
	if err := m.doShare(ctx, http.MethodGet, "/api/share/agent/sessions/"+url.PathEscape(id), token, hdrs, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (m *MemoryClient) ShareCreateSession(ctx context.Context, token, endUserRef string, in ShareCreateSessionInput) (*ShareSession, error) {
	hdrs, err := m.shareRefHeaders(endUserRef)
	if err != nil {
		return nil, err
	}
	var out ShareSession
	if err := m.doShare(ctx, http.MethodPost, "/api/share/agent/sessions", token, hdrs, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (m *MemoryClient) ShareArchiveSession(ctx context.Context, token, endUserRef, id string) error {
	hdrs, err := m.shareRefHeaders(endUserRef)
	if err != nil {
		return err
	}
	return m.doShare(ctx, http.MethodPost, "/api/share/agent/sessions/"+url.PathEscape(id)+"/archive", token, hdrs, nil, nil)
}

func (m *MemoryClient) ShareListQuestions(ctx context.Context, token, endUserRef, sessionID string) ([]ShareQuestion, error) {
	hdrs, err := m.shareRefHeaders(endUserRef)
	if err != nil {
		return nil, err
	}
	path := "/api/share/agent/questions"
	if sessionID != "" {
		path += "?sessionId=" + url.QueryEscape(sessionID)
	}
	var out []ShareQuestion
	if err := m.doShare(ctx, http.MethodGet, path, token, hdrs, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (m *MemoryClient) ShareApprove(ctx context.Context, token, endUserRef, sessionID, questionID string, in ShareApproveInput) error {
	hdrs, err := m.shareRefHeaders(endUserRef)
	if err != nil {
		return err
	}
	path := "/api/share/agent/sessions/" + url.PathEscape(sessionID) + "/approvals/" + url.PathEscape(questionID)
	return m.doShare(ctx, http.MethodPost, path, token, hdrs, in, nil)
}

// ShareChatStream opens the share SSE stream with an explicit bearer token and
// returns the raw SSE body. The caller owns the returned body and must close it.
func (m *MemoryClient) ShareChatStream(ctx context.Context, token, endUserRef string, in ShareStreamInput) (io.ReadCloser, error) {
	b, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	hdrs, err := m.shareRefHeaders(endUserRef)
	if err != nil {
		return nil, err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/api/share/agent/stream", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Authorization", "Bearer "+token)
	hreq.Header.Set("Content-Type", "application/json")
	for k, v := range hdrs {
		hreq.Header.Set(k, v)
	}
	resp, err := m.streamHTTP.Do(hreq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(resp.Body)
		perr := parseMemoryError(resp.StatusCode, raw)
		captureMemoryError(http.MethodPost, "/api/share/agent/stream", resp.StatusCode, perr)
		return nil, perr
	}
	return resp.Body, nil
}

// --- owner share-link methods (session-scoped) ------------------------------

func (m *MemoryClient) ListShareLinks(ctx context.Context, agentDefinitionID string) ([]ShareLink, error) {
	path := "/api/projects/" + url.PathEscape(m.projectIDFor(ctx)) + "/agent-definitions/" + url.PathEscape(agentDefinitionID) + "/share-links"
	var out []ShareLink
	if err := m.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (m *MemoryClient) CreateShareLink(ctx context.Context, agentDefinitionID string, in ShareLinkCreateInput) (*ShareLink, error) {
	path := "/api/projects/" + url.PathEscape(m.projectIDFor(ctx)) + "/agent-definitions/" + url.PathEscape(agentDefinitionID) + "/share-links"
	var out ShareLink
	if err := m.do(ctx, http.MethodPost, path, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (m *MemoryClient) RevokeShareLink(ctx context.Context, linkID string) error {
	path := "/api/projects/" + url.PathEscape(m.projectIDFor(ctx)) + "/share-links/" + url.PathEscape(linkID)
	return m.do(ctx, http.MethodDelete, path, nil, nil)
}

func (m *MemoryClient) RotateShareLink(ctx context.Context, linkID string) (*ShareLink, error) {
	path := "/api/projects/" + url.PathEscape(m.projectIDFor(ctx)) + "/share-links/" + url.PathEscape(linkID) + "/rotate"
	var out ShareLink
	if err := m.do(ctx, http.MethodPost, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RevealShareLink recovers a link's share key (owner-only). The key is returned
// by memory only when the link is still recoverable; otherwise the upstream
// error carries a "not_recoverable" code the caller maps to a rotate hint.
func (m *MemoryClient) RevealShareLink(ctx context.Context, linkID string) (string, error) {
	path := "/api/projects/" + url.PathEscape(m.projectIDFor(ctx)) + "/share-links/" + url.PathEscape(linkID) + "/reveal"
	var out struct {
		Key string `json:"key"`
	}
	if err := m.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return "", err
	}
	return out.Key, nil
}
