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

// --- Per-agent MCP shares (single agent exposed as one MCP tool) ---
//
// These methods proxy the memory per-agent MCP share API under
// /api/projects/:projectId. Project scoping rides on the session X-Project-ID
// header via sessionHeaders AND the project id embedded in the path via
// projectIDFor — the same shape the project MCP share routes use.
//
// The raw API key is returned by the backend ONLY on create and rotate. It must
// never be logged and is never persisted by the gateway: list responses carry
// metadata only (AgentMCPShare has no token field at all).
//
// The response decoding is tolerant: memory wraps admin payloads in the
// {success,data} envelope while some project routes return plain JSON, and a
// list may arrive as a bare array or under a {"shares":[...]} / {"items":[...]}
// key. agentMCPShareRaw strips the envelope so all of these decode.

// AgentMCPShare is one per-agent MCP share: a single-tool MCP server exposing
// the agent's call_agent tool to an outside client, guarded by its own key.
type AgentMCPShare struct {
	ID          string  `json:"id"`
	AgentID     string  `json:"agentId,omitempty"`
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Status      string  `json:"status"`
	CreatedAt   string  `json:"createdAt"`
	LastUsedAt  *string `json:"lastUsedAt"`
}

// AgentMCPShareInput is the create request body. Both fields are optional; the
// backend applies a default name when empty.
type AgentMCPShareInput struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// AgentMCPShareSecret is the create/rotate payload: the raw key (returned only
// on create and rotate), the per-agent MCP endpoint, and optional ready-to-paste
// client snippets supplied by the backend.
type AgentMCPShareSecret struct {
	Token    string            `json:"token"`
	MCPURL   string            `json:"mcpUrl"`
	Snippets map[string]string `json:"snippets,omitempty"`
}

// AgentMCPShareCreated is the share plus its one-time secret (create/rotate
// responses only).
type AgentMCPShareCreated struct {
	Share  AgentMCPShare
	Secret AgentMCPShareSecret
}

// agentMCPSharePath builds /api/projects/<project>/<parts...>, escaping every
// segment. The project is the session's active project (or the static
// server-side project on the API-key path).
func (m *MemoryClient) agentMCPSharePath(ctx context.Context, parts ...string) string {
	p := "/api/projects/" + url.PathEscape(m.projectIDFor(ctx))
	for _, part := range parts {
		p += "/" + url.PathEscape(part)
	}
	return p
}

// agentMCPShareRaw performs one per-agent share request and returns the decoded
// body with any {success,data} envelope already stripped. A missing/empty data
// key leaves the raw body as-is so plain-JSON responses decode too.
func (m *MemoryClient) agentMCPShareRaw(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
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

// decodeAgentMCPShareList accepts a bare array or a {"shares":[...]} /
// {"items":[...]} wrapper.
func decodeAgentMCPShareList(raw json.RawMessage) ([]AgentMCPShare, error) {
	var list []AgentMCPShare
	if err := json.Unmarshal(raw, &list); err == nil {
		return list, nil
	}
	var wrap struct {
		Shares []AgentMCPShare `json:"shares"`
		Items  []AgentMCPShare `json:"items"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, err
	}
	if wrap.Shares != nil {
		return wrap.Shares, nil
	}
	return wrap.Items, nil
}

// decodeAgentMCPShareCreated accepts the create response shape — the share
// nested under a "share" key plus the secret fields at the top level — as well
// as the flattened variant and the rotate response ({token, mcpUrl} only).
func decodeAgentMCPShareCreated(raw json.RawMessage) (*AgentMCPShareCreated, error) {
	var wire struct {
		Share *AgentMCPShare `json:"share"`
		AgentMCPShareSecret
		AgentMCPShare
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, err
	}
	out := &AgentMCPShareCreated{Secret: wire.AgentMCPShareSecret}
	if wire.Share != nil {
		out.Share = *wire.Share
	} else {
		out.Share = wire.AgentMCPShare
	}
	return out, nil
}

// ListAgentMCPShares lists one agent's shares (metadata only — never the key).
func (m *MemoryClient) ListAgentMCPShares(ctx context.Context, agentID string) ([]AgentMCPShare, error) {
	raw, err := m.agentMCPShareRaw(ctx, http.MethodGet, m.agentMCPSharePath(ctx, "agents", agentID, "mcp-shares"), nil)
	if err != nil {
		return nil, err
	}
	return decodeAgentMCPShareList(raw)
}

// ListProjectAgentMCPShares lists every per-agent share in the project
// (metadata only — never the key).
func (m *MemoryClient) ListProjectAgentMCPShares(ctx context.Context) ([]AgentMCPShare, error) {
	raw, err := m.agentMCPShareRaw(ctx, http.MethodGet, m.agentMCPSharePath(ctx, "agent-mcp-shares"), nil)
	if err != nil {
		return nil, err
	}
	return decodeAgentMCPShareList(raw)
}

// CreateAgentMCPShare creates a share for one agent and returns it with its
// one-time key.
func (m *MemoryClient) CreateAgentMCPShare(ctx context.Context, agentID string, in *AgentMCPShareInput) (*AgentMCPShareCreated, error) {
	raw, err := m.agentMCPShareRaw(ctx, http.MethodPost, m.agentMCPSharePath(ctx, "agents", agentID, "mcp-share"), in)
	if err != nil {
		return nil, err
	}
	return decodeAgentMCPShareCreated(raw)
}

// RevokeAgentMCPShare permanently revokes a share, invalidating its key.
func (m *MemoryClient) RevokeAgentMCPShare(ctx context.Context, id string) error {
	return m.do(ctx, http.MethodDelete, m.agentMCPSharePath(ctx, "agent-mcp-shares", id), nil, nil)
}

// RotateAgentMCPShare issues a new key for a share and returns the new one-time
// secret; the previous key stops working immediately.
func (m *MemoryClient) RotateAgentMCPShare(ctx context.Context, id string) (*AgentMCPShareCreated, error) {
	raw, err := m.agentMCPShareRaw(ctx, http.MethodPost, m.agentMCPSharePath(ctx, "agent-mcp-shares", id, "rotate"), nil)
	if err != nil {
		return nil, err
	}
	return decodeAgentMCPShareCreated(raw)
}

// --- display helpers (shared by the agent MCP share templates) ---

// agentMCPSharesSortedNewestFirst returns the shares most recently created
// first. RFC3339 timestamps sort correctly as strings; ties and unparseable
// values keep the backend's order.
func agentMCPSharesSortedNewestFirst(shares []AgentMCPShare) []AgentMCPShare {
	out := append([]AgentMCPShare(nil), shares...)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt > out[j].CreatedAt
	})
	return out
}

// agentMCPShareFindByID returns a pointer to the share with the given id, or
// nil.
func agentMCPShareFindByID(shares []AgentMCPShare, id string) *AgentMCPShare {
	for i := range shares {
		if shares[i].ID == id {
			return &shares[i]
		}
	}
	return nil
}

// agentMCPShareStatusIntent maps a share status to a badge intent.
func agentMCPShareStatusIntent(status string) ui.BadgeIntent {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "active", "enabled":
		return ui.BadgeSuccess
	case "revoked", "disabled", "error":
		return ui.BadgeError
	default:
		return ui.BadgeNeutral
	}
}

// agentMCPShareStatusLabel capitalizes a share status for display; empty
// renders "active" (the backend's implicit default).
func agentMCPShareStatusLabel(status string) string {
	if strings.TrimSpace(status) == "" {
		return "active"
	}
	return status
}

// agentMCPShareIsRevoked reports whether a share can no longer be used (and so
// must not offer rotation).
func agentMCPShareIsRevoked(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "revoked", "disabled":
		return true
	default:
		return false
	}
}

// agentMCPShareLastUsedLabel renders the last-used timestamp or a never-used
// note.
func agentMCPShareLastUsedLabel(inst AgentMCPShare) string {
	if inst.LastUsedAt == nil || strings.TrimSpace(*inst.LastUsedAt) == "" {
		return "never"
	}
	return relTime(*inst.LastUsedAt)
}

// --- one-time key reveal snippets ---

// agentMCPShareSnippet is one ready-to-paste client configuration block.
type agentMCPShareSnippet struct {
	Key   string
	Label string
	Code  string
}

// agentMCPShareSnippetClients is the canonical client order for the reveal.
var agentMCPShareSnippetClients = []struct{ Key, Label string }{
	{"claudeDesktop", "Claude Desktop"},
	{"claudeCode", "Claude Code"},
	{"cursor", "Cursor"},
	{"cloudCode", "Cloud Code"},
}

// agentMCPShareSnippets builds the reveal's snippet list. Backend-provided
// snippets win when present (looked up by canonical key or a small alias set);
// otherwise the gateway generates a correct block from the per-agent endpoint
// URL and key.
func agentMCPShareSnippets(secret AgentMCPShareSecret) []agentMCPShareSnippet {
	out := make([]agentMCPShareSnippet, 0, len(agentMCPShareSnippetClients))
	for _, c := range agentMCPShareSnippetClients {
		code := ""
		if secret.Snippets != nil {
			code = secret.Snippets[c.Key]
			if code == "" {
				code = agentMCPShareLookupSnippet(secret.Snippets, c.Key)
			}
		}
		if code == "" {
			code = agentMCPShareGeneratedSnippet(c.Key, secret.MCPURL, secret.Token)
		}
		out = append(out, agentMCPShareSnippet{Key: c.Key, Label: c.Label, Code: code})
	}
	return out
}

// agentMCPShareLookupSnippet finds a snippet under a normalized key (case/sep
// insensitive) so "claude_desktop" and "Claude-Desktop" both match.
func agentMCPShareLookupSnippet(snippets map[string]string, key string) string {
	want := agentMCPShareNormalizeSnippetKey(key)
	for k, v := range snippets {
		if agentMCPShareNormalizeSnippetKey(k) == want {
			return v
		}
	}
	return ""
}

// agentMCPShareNormalizeSnippetKey lowercases and strips separators so alias
// spellings of a client key compare equal.
func agentMCPShareNormalizeSnippetKey(k string) string {
	k = strings.ToLower(k)
	return strings.NewReplacer("_", "", "-", "", " ", "").Replace(k)
}

// agentMCPShareGeneratedSnippet renders a client config that contains the
// per-agent endpoint URL and the key. The blocks are intentionally minimal and
// current.
func agentMCPShareGeneratedSnippet(key, mcpURL, token string) string {
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
