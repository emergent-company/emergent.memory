package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	ui "github.com/emergent-company/go-daisy/components/ui"
)

// --- Agent MCP endpoint + labeled keys (agent-scoped-mcp-endpoint) ---
//
// One agent owns at most one active MCP endpoint; many labeled keys hang off
// that endpoint. These methods proxy memory's agent-endpoint API under
// /api/projects/:projectId. Project scoping rides on the session X-Project-ID
// header via sessionHeaders AND the project id embedded in the path via
// projectIDFor — the same shape the project MCP share routes use.
//
// The raw key secret is returned by the backend ONLY on key create and rotate.
// It must never be logged and is never persisted by the gateway: list
// responses carry metadata only (AgentMCPKey has no token field at all, and the
// one-time secret lives in AgentMCPKeySecret for the lifetime of one response).
//
// Decoding is tolerant: memory wraps some admin payloads in the {success,data}
// envelope while the agent-endpoint routes return plain JSON, so agentMCPRaw
// strips the envelope when present and passes plain JSON through untouched.

// AgentMCPEndpoint is the non-secret agent MCP endpoint: the single active
// endpoint an agent owns, plus the URL an outside MCP client connects to.
type AgentMCPEndpoint struct {
	ID        string  `json:"id"`
	ProjectID string  `json:"projectId"`
	AgentID   string  `json:"agentId"`
	Status    string  `json:"status"`
	CreatedAt string  `json:"createdAt"`
	UpdatedAt string  `json:"updatedAt"`
	RevokedAt *string `json:"revokedAt"`
	MCPURL    string  `json:"mcpUrl"`
}

// AgentMCPKey is one labeled credential on an endpoint. The struct has no token
// field by construction, so a list can never carry the secret.
type AgentMCPKey struct {
	ID         string  `json:"id"`
	EndpointID string  `json:"endpointId"`
	AgentID    string  `json:"agentId"`
	Label      string  `json:"label"`
	Status     string  `json:"status"`
	CreatedAt  string  `json:"createdAt"`
	UpdatedAt  string  `json:"updatedAt"`
	LastUsedAt *string `json:"lastUsedAt"`
	ExpiresAt  *string `json:"expiresAt"`
}

// AgentMCPKeySecret is the create/rotate payload: the key metadata plus the raw
// token value, which is present only on those two responses.
type AgentMCPKeySecret struct {
	AgentMCPKey
	Token  string `json:"token"`
	MCPURL string `json:"mcpUrl"`
}

// AgentMCPSession is one external MCP client session owned by a key on an
// endpoint. The wire shape is snake_case (frozen contract), unlike the key and
// endpoint DTOs.
type AgentMCPSession struct {
	SessionID    string  `json:"session_id"`
	KeyID        string  `json:"key_id"`
	KeyLabel     string  `json:"key_label"`
	Status       string  `json:"status"`
	TurnCount    int     `json:"turn_count"`
	TotalSteps   int     `json:"total_steps"`
	CreatedAt    string  `json:"created_at"`
	LastActiveAt string  `json:"last_active_at"`
	ExpiresAt    *string `json:"expires_at"`
}

// agentMCPPath builds /api/projects/<project>/<parts...>, escaping every
// segment. The project is the session's active project (or the static
// server-side project on the API-key path).
func (m *MemoryClient) agentMCPPath(ctx context.Context, parts ...string) string {
	p := "/api/projects/" + url.PathEscape(m.projectIDFor(ctx))
	for _, part := range parts {
		p += "/" + url.PathEscape(part)
	}
	return p
}

// agentMCPRaw performs one agent-endpoint request and returns the decoded body
// with any {success,data} envelope already stripped. A missing/empty data key
// leaves the raw body as-is so plain-JSON responses decode too.
func (m *MemoryClient) agentMCPRaw(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := m.do(ctx, method, path, body, &raw); err != nil {
		return nil, err
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &env) == nil && len(env.Data) > 0 && string(env.Data) != "null" {
		return env.Data, nil
	}
	return raw, nil
}

// GetAgentMCPEndpoint returns the agent's active endpoint, or (nil, nil) when
// the agent has none (the backend answers 404) — the caller renders its
// create-the-endpoint state rather than an error.
func (m *MemoryClient) GetAgentMCPEndpoint(ctx context.Context, agentID string) (*AgentMCPEndpoint, error) {
	raw, err := m.agentMCPRaw(ctx, http.MethodGet, m.agentMCPPath(ctx, "agents", agentID, "mcp-endpoint"), nil)
	if err != nil {
		if isMemoryNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var ep AgentMCPEndpoint
	if err := json.Unmarshal(raw, &ep); err != nil {
		return nil, fmt.Errorf("decode agent mcp endpoint: %w", err)
	}
	if ep.ID == "" {
		return nil, nil
	}
	return &ep, nil
}

// CreateAgentMCPEndpoint creates the agent's MCP endpoint. The backend rejects
// a second active endpoint with 409.
func (m *MemoryClient) CreateAgentMCPEndpoint(ctx context.Context, agentID string) (*AgentMCPEndpoint, error) {
	raw, err := m.agentMCPRaw(ctx, http.MethodPost, m.agentMCPPath(ctx, "agents", agentID, "mcp-endpoint"), nil)
	if err != nil {
		return nil, err
	}
	var ep AgentMCPEndpoint
	if err := json.Unmarshal(raw, &ep); err != nil {
		return nil, fmt.Errorf("decode agent mcp endpoint: %w", err)
	}
	return &ep, nil
}

// RevokeAgentMCPEndpoint revokes an endpoint, invalidating every key on it. The
// backend revoke is idempotent.
func (m *MemoryClient) RevokeAgentMCPEndpoint(ctx context.Context, endpointID string) error {
	return m.do(ctx, http.MethodDelete, m.agentMCPPath(ctx, "agent-mcp-endpoints", endpointID), nil, nil)
}

// ListAgentMCPKeys lists an endpoint's keys (metadata only — never the secret),
// newest first.
func (m *MemoryClient) ListAgentMCPKeys(ctx context.Context, endpointID string) ([]AgentMCPKey, error) {
	raw, err := m.agentMCPRaw(ctx, http.MethodGet, m.agentMCPPath(ctx, "agent-mcp-endpoints", endpointID, "keys"), nil)
	if err != nil {
		return nil, err
	}
	return decodeAgentMCPKeyList(raw)
}

// decodeAgentMCPKeyList accepts the backend's {"keys":[...],"total":n} wrapper
// and a bare array.
func decodeAgentMCPKeyList(raw json.RawMessage) ([]AgentMCPKey, error) {
	var wrap struct {
		Keys []AgentMCPKey `json:"keys"`
	}
	if err := json.Unmarshal(raw, &wrap); err == nil && wrap.Keys != nil {
		return wrap.Keys, nil
	}
	var list []AgentMCPKey
	if err := json.Unmarshal(raw, &list); err == nil {
		return list, nil
	}
	return nil, fmt.Errorf("decode agent mcp key list: unrecognized payload shape")
}

// CreateAgentMCPKey mints a labeled key on the endpoint and returns it with the
// raw secret exactly once. A duplicate label is rejected by the backend (409).
func (m *MemoryClient) CreateAgentMCPKey(ctx context.Context, endpointID, label string) (*AgentMCPKeySecret, error) {
	body := map[string]any{"label": label}
	raw, err := m.agentMCPRaw(ctx, http.MethodPost, m.agentMCPPath(ctx, "agent-mcp-endpoints", endpointID, "keys"), body)
	if err != nil {
		return nil, err
	}
	return decodeAgentMCPKeySecret(raw)
}

// RevokeAgentMCPKey revokes one key without touching the endpoint or its other
// keys. The backend revoke is idempotent.
func (m *MemoryClient) RevokeAgentMCPKey(ctx context.Context, keyID string) error {
	return m.do(ctx, http.MethodDelete, m.agentMCPPath(ctx, "agent-mcp-keys", keyID), nil, nil)
}

// RotateAgentMCPKey issues a new token for an existing key and returns the new
// secret exactly once. The key identity is preserved, so its sessions survive.
func (m *MemoryClient) RotateAgentMCPKey(ctx context.Context, keyID string) (*AgentMCPKeySecret, error) {
	raw, err := m.agentMCPRaw(ctx, http.MethodPost, m.agentMCPPath(ctx, "agent-mcp-keys", keyID, "rotate"), nil)
	if err != nil {
		return nil, err
	}
	return decodeAgentMCPKeySecret(raw)
}

// decodeAgentMCPKeySecret accepts the flattened create/rotate response and a
// response nesting the key under a "key" wrapper.
func decodeAgentMCPKeySecret(raw json.RawMessage) (*AgentMCPKeySecret, error) {
	var wire struct {
		AgentMCPKey
		Key    *AgentMCPKey `json:"key"`
		Token  string       `json:"token"`
		MCPURL string       `json:"mcpUrl"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("decode agent mcp key secret: %w", err)
	}
	out := &AgentMCPKeySecret{Token: wire.Token, MCPURL: wire.MCPURL}
	if wire.Key != nil {
		out.AgentMCPKey = *wire.Key
	} else {
		out.AgentMCPKey = wire.AgentMCPKey
	}
	return out, nil
}

// ListAgentMCPSessions lists the endpoint's external client sessions, ordered
// by last activity (the backend's order). status optionally filters the list;
// an empty status lists every session.
func (m *MemoryClient) ListAgentMCPSessions(ctx context.Context, endpointID, status string) ([]AgentMCPSession, error) {
	path := m.agentMCPPath(ctx, "agent-mcp-endpoints", endpointID, "sessions")
	if status != "" {
		path += "?status=" + url.QueryEscape(status)
	}
	raw, err := m.agentMCPRaw(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	return decodeAgentMCPSessionList(raw)
}

// decodeAgentMCPSessionList accepts the frozen {"sessions":[...]} wrapper and a
// bare array.
func decodeAgentMCPSessionList(raw json.RawMessage) ([]AgentMCPSession, error) {
	var wrap struct {
		Sessions []AgentMCPSession `json:"sessions"`
	}
	if err := json.Unmarshal(raw, &wrap); err == nil && wrap.Sessions != nil {
		return wrap.Sessions, nil
	}
	var list []AgentMCPSession
	if err := json.Unmarshal(raw, &list); err == nil {
		return list, nil
	}
	return nil, fmt.Errorf("decode agent mcp session list: unrecognized payload shape")
}

// --- display helpers (shared by the agent MCP templates) ---

// agentMCPEndpointStatusLabel renders the endpoint status, defaulting an empty
// status to "active" (the backend's implicit default).
func agentMCPEndpointStatusLabel(status string) string {
	if strings.TrimSpace(status) == "" {
		return "active"
	}
	return status
}

// agentMCPStatusIntent maps an endpoint/key/session status to a badge intent.
func agentMCPStatusIntent(status string) ui.BadgeIntent {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "active", "enabled", "running":
		return ui.BadgeSuccess
	case "revoked", "disabled", "expired", "error":
		return ui.BadgeError
	case "interrupted", "cancelled", "canceled", "paused":
		return ui.BadgeWarning
	default:
		return ui.BadgeNeutral
	}
}

// agentMCPKeyIsUsable reports whether a key can still authorize the endpoint
// (and so may be rotated/revoked rather than shown read-only).
func agentMCPKeyIsUsable(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "revoked", "disabled", "expired":
		return false
	default:
		return true
	}
}

// agentMCPKeyLastUsedLabel renders the key's last-used time or a never-used
// note (the value comes from the bound token).
func agentMCPKeyLastUsedLabel(k AgentMCPKey) string {
	if k.LastUsedAt == nil || strings.TrimSpace(*k.LastUsedAt) == "" {
		return "never"
	}
	return relTime(*k.LastUsedAt)
}

// agentMCPKeysSortedNewestFirst returns the keys most recently created first.
// RFC3339 timestamps sort correctly as strings; ties and unparseable values
// keep the backend's order.
func agentMCPKeysSortedNewestFirst(keys []AgentMCPKey) []AgentMCPKey {
	out := append([]AgentMCPKey(nil), keys...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}

// agentMCPSessionLastActiveLabel renders when the session was last used.
func agentMCPSessionLastActiveLabel(s AgentMCPSession) string {
	if strings.TrimSpace(s.LastActiveAt) == "" {
		return "never"
	}
	return relTime(s.LastActiveAt)
}

// agentMCPSessionKeyLabel names the key a session belongs to, falling back to
// the short key id when the backend sent no label.
func agentMCPSessionKeyLabel(s AgentMCPSession) string {
	if label := strings.TrimSpace(s.KeyLabel); label != "" {
		return label
	}
	if id := strings.TrimSpace(s.KeyID); id != "" {
		if len(id) > 8 {
			return id[:8]
		}
		return id
	}
	return "unknown key"
}

// --- one-time key reveal snippets ---

// agentMCPKeySnippet is one ready-to-paste client configuration block.
type agentMCPKeySnippet struct {
	Key   string
	Label string
	Code  string
}

// agentMCPKeySnippets builds the reveal's client snippet list from the endpoint
// URL and the one-time secret. The blocks are intentionally minimal and current.
func agentMCPKeySnippets(secret AgentMCPKeySecret) []agentMCPKeySnippet {
	out := make([]agentMCPKeySnippet, 0, len(mcpShareSnippetClients))
	for _, c := range mcpShareSnippetClients {
		out = append(out, agentMCPKeySnippet{
			Key:   c.Key,
			Label: c.Label,
			Code:  agentMCPGeneratedSnippet(c.Key, secret.MCPURL, secret.Token),
		})
	}
	return out
}

// agentMCPGeneratedSnippet renders a client config containing the agent's MCP
// endpoint URL and the key.
func agentMCPGeneratedSnippet(key, mcpURL, token string) string {
	if strings.TrimSpace(mcpURL) == "" {
		mcpURL = "(agent MCP endpoint)"
	}
	switch key {
	case "claudeDesktop":
		return fmt.Sprintf(`{
  "mcpServers": {
    "agent": {
      "command": "npx",
      "args": ["-y", "mcp-remote", %q, "--header", "Authorization: Bearer %s"]
    }
  }
}`, mcpURL, token)
	case "claudeCode":
		return fmt.Sprintf("claude mcp add --transport http agent %s --header \"Authorization: Bearer %s\"", mcpURL, token)
	case "cursor":
		return fmt.Sprintf(`{
  "mcpServers": {
    "agent": {
      "url": %q,
      "headers": { "Authorization": "Bearer %s" }
    }
  }
}`, mcpURL, token)
	case "cloudCode":
		return fmt.Sprintf(`{
  "mcpServers": {
    "agent": {
      "httpUrl": %q,
      "headers": { "Authorization": "Bearer %s" }
    }
  }
}`, mcpURL, token)
	default:
		return mcpURL + "\nAuthorization: Bearer " + token
	}
}
