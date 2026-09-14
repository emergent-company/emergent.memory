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

// --- MCP share instances (project-scoped MCP exposure) ---
//
// These methods proxy memory's project MCP share API under
// /api/projects/:projectId/mcp. Project scoping rides on the session
// X-Project-ID header via sessionHeaders AND the project id embedded in the
// path via projectIDFor — the same shape the agent-definition routes use.
//
// The response decoding is tolerant: admin routes wrap payloads in the
// {success,data} envelope while some project routes return plain JSON, and a
// list may arrive as a bare array or under a {"shares":[...]} key. shareDecode
// accepts all of these so the UI keeps working across backend versions.

// MCPShareInstance is one named MCP share instance: an API-key grant exposing a
// chosen subset of memory tools and agents to outside MCP clients. Tools and
// Agents are nullable — a nil Agents list means "all scope-permitted agents".
type MCPShareInstance struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Tools       []string `json:"tools"`
	Agents      []string `json:"agents"`
	Status      string   `json:"status"`
	IsLegacy    bool     `json:"isLegacy"`
	CreatedAt   string   `json:"createdAt"`
	LastUsedAt  *string  `json:"lastUsedAt"`
	ToolCount   int      `json:"toolCount"`
	AgentCount  int      `json:"agentCount"`
}

// MCPShareInput is the create/update request body. Tools must hold at least one
// name; Agents may be empty (all agents).
type MCPShareInput struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Tools       []string `json:"tools"`
	Agents      []string `json:"agents"`
}

// MCPShareSecret is the create/rotate payload. Token is the raw API key and is
// returned by memory ONLY on create and rotate — it must never be logged or
// persisted by the gateway. MCPURL is the share endpoint clients connect to;
// Snippets holds ready-to-paste client configs when the backend provides them.
type MCPShareSecret struct {
	Token    string            `json:"token"`
	MCPURL   string            `json:"mcpUrl"`
	Snippets map[string]string `json:"snippets,omitempty"`
}

// MCPShareCreated is the instance plus its one-time secret (create/rotate
// responses only).
type MCPShareCreated struct {
	MCPShareInstance
	MCPShareSecret
}

// MCPShareTool is one entry in the memory tool catalog: the tools a share may
// expose. Category groups the picker; RequiredScope is the memory scope that
// protects the tool.
type MCPShareTool struct {
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	RequiredScope string `json:"requiredScope,omitempty"`
	Category      string `json:"category,omitempty"`
}

// mcpSharePath builds /api/projects/<project>/mcp[/<part>...].
func (m *MemoryClient) mcpSharePath(ctx context.Context, parts ...string) string {
	p := "/api/projects/" + url.PathEscape(m.projectIDFor(ctx)) + "/mcp"
	for _, part := range parts {
		p += "/" + url.PathEscape(part)
	}
	return p
}

// shareRaw performs one share-API request and returns the decoded body with any
// {success,data} envelope already stripped. A missing/empty data key leaves the
// raw body as-is so plain-JSON responses decode too.
func (m *MemoryClient) shareRaw(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
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

// decodeMCPShareList accepts a bare array or a {"shares":[...]} wrapper.
func decodeMCPShareList(raw json.RawMessage) ([]MCPShareInstance, error) {
	var list []MCPShareInstance
	if err := json.Unmarshal(raw, &list); err == nil {
		return list, nil
	}
	var wrap struct {
		Shares []MCPShareInstance `json:"shares"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, err
	}
	return wrap.Shares, nil
}

// decodeMCPShareTools accepts a bare array or a {"tools":[...]} wrapper.
func decodeMCPShareTools(raw json.RawMessage) ([]MCPShareTool, error) {
	var list []MCPShareTool
	if err := json.Unmarshal(raw, &list); err == nil {
		return list, nil
	}
	var wrap struct {
		Tools []MCPShareTool `json:"tools"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, err
	}
	return wrap.Tools, nil
}

// decodeMCPShareCreated accepts the instance fields and the secret fields
// flattened at the top level, or the instance nested under an "instance" key.
func decodeMCPShareCreated(raw json.RawMessage) (*MCPShareCreated, error) {
	var wire struct {
		MCPShareInstance
		MCPShareSecret
		Instance *MCPShareInstance `json:"instance"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, err
	}
	out := &MCPShareCreated{MCPShareSecret: wire.MCPShareSecret}
	if wire.Instance != nil {
		out.MCPShareInstance = *wire.Instance
	} else {
		out.MCPShareInstance = wire.MCPShareInstance
	}
	return out, nil
}

// ListMCPShareInstances lists the project's share instances.
func (m *MemoryClient) ListMCPShareInstances(ctx context.Context) ([]MCPShareInstance, error) {
	raw, err := m.shareRaw(ctx, http.MethodGet, m.mcpSharePath(ctx, "shares"), nil)
	if err != nil {
		return nil, err
	}
	return decodeMCPShareList(raw)
}

// CreateMCPShareInstance creates a share and returns it with its one-time key.
func (m *MemoryClient) CreateMCPShareInstance(ctx context.Context, in *MCPShareInput) (*MCPShareCreated, error) {
	raw, err := m.shareRaw(ctx, http.MethodPost, m.mcpSharePath(ctx, "shares"), in)
	if err != nil {
		return nil, err
	}
	return decodeMCPShareCreated(raw)
}

// GetMCPShareInstance fetches one share (never carries the raw key).
func (m *MemoryClient) GetMCPShareInstance(ctx context.Context, id string) (*MCPShareInstance, error) {
	raw, err := m.shareRaw(ctx, http.MethodGet, m.mcpSharePath(ctx, "shares", id), nil)
	if err != nil {
		return nil, err
	}
	var inst MCPShareInstance
	if err := json.Unmarshal(raw, &inst); err != nil {
		return nil, err
	}
	return &inst, nil
}

// UpdateMCPShareInstance persists a share's name/description/allowlists. The
// key is unchanged by an update.
func (m *MemoryClient) UpdateMCPShareInstance(ctx context.Context, id string, in *MCPShareInput) (*MCPShareInstance, error) {
	raw, err := m.shareRaw(ctx, http.MethodPatch, m.mcpSharePath(ctx, "shares", id), in)
	if err != nil {
		return nil, err
	}
	var inst MCPShareInstance
	if err := json.Unmarshal(raw, &inst); err != nil {
		return nil, err
	}
	return &inst, nil
}

// RevokeMCPShareInstance permanently revokes a share, invalidating its key.
func (m *MemoryClient) RevokeMCPShareInstance(ctx context.Context, id string) error {
	return m.do(ctx, http.MethodDelete, m.mcpSharePath(ctx, "shares", id), nil, nil)
}

// RotateMCPShareInstance issues a new key for a share and returns the new
// one-time secret (the previous key stops working).
func (m *MemoryClient) RotateMCPShareInstance(ctx context.Context, id string) (*MCPShareCreated, error) {
	raw, err := m.shareRaw(ctx, http.MethodPost, m.mcpSharePath(ctx, "shares", id, "rotate"), nil)
	if err != nil {
		return nil, err
	}
	return decodeMCPShareCreated(raw)
}

// ListMCPShareTools fetches the memory tool catalog a share may expose.
func (m *MemoryClient) ListMCPShareTools(ctx context.Context) ([]MCPShareTool, error) {
	raw, err := m.shareRaw(ctx, http.MethodGet, m.mcpSharePath(ctx, "tools"), nil)
	if err != nil {
		return nil, err
	}
	return decodeMCPShareTools(raw)
}

// --- display helpers (shared by the share templates) ---

// mcpSharesSortedNewestFirst returns the shares most recently created first.
// RFC3339 timestamps sort correctly as strings; ties and unparseable values
// keep the backend's order.
func mcpSharesSortedNewestFirst(shares []MCPShareInstance) []MCPShareInstance {
	out := append([]MCPShareInstance(nil), shares...)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt > out[j].CreatedAt
	})
	return out
}

// mcpShareToolGroup is one category bucket in the tool picker.
type mcpShareToolGroup struct {
	Category string
	Tools    []MCPShareTool
}

// mcpShareToolGroups groups the catalog by category (empty → "Other"), sorting
// categories alphabetically with "Other" last and tools by name.
func mcpShareToolGroups(catalog []MCPShareTool) []mcpShareToolGroup {
	byCat := map[string][]MCPShareTool{}
	for _, t := range catalog {
		cat := strings.TrimSpace(t.Category)
		if cat == "" {
			cat = "Other"
		}
		byCat[cat] = append(byCat[cat], t)
	}
	groups := make([]mcpShareToolGroup, 0, len(byCat))
	for cat, tools := range byCat {
		sort.SliceStable(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
		groups = append(groups, mcpShareToolGroup{Category: cat, Tools: tools})
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if (groups[i].Category == "Other") != (groups[j].Category == "Other") {
			return groups[j].Category == "Other"
		}
		return groups[i].Category < groups[j].Category
	})
	return groups
}

// mcpShareStatusIntent maps a share status to a badge intent.
func mcpShareStatusIntent(status string) ui.BadgeIntent {
	switch strings.ToLower(status) {
	case "active", "enabled":
		return ui.BadgeSuccess
	case "revoked", "disabled", "error":
		return ui.BadgeError
	default:
		return ui.BadgeNeutral
	}
}

// mcpShareStatusLabel capitalizes a share status for display; empty renders
// "active" (the backend's implicit default).
func mcpShareStatusLabel(status string) string {
	if strings.TrimSpace(status) == "" {
		return "active"
	}
	return status
}

// mcpShareAgentsLabel describes the agent allowlist: an empty selection means
// the share can reach every scope-permitted agent.
func mcpShareAgentsLabel(count int) string {
	if count <= 0 {
		return "all agents"
	}
	return countLabel(count, "agent", "agents")
}

// mcpShareLastUsedLabel renders the last-used timestamp (or a never-used note).
func mcpShareLastUsedLabel(inst MCPShareInstance) string {
	if inst.LastUsedAt == nil || strings.TrimSpace(*inst.LastUsedAt) == "" {
		return "never used"
	}
	return relTime(*inst.LastUsedAt)
}

// mcpShareContains reports whether list holds value.
func mcpShareContains(list []string, value string) bool {
	for _, s := range list {
		if s == value {
			return true
		}
	}
	return false
}

// mcpShareToolSearchHay is the lowercase haystack the picker's search box
// filters a tool option against.
func mcpShareToolSearchHay(t MCPShareTool) string {
	return strings.ToLower(strings.Join([]string{t.Name, t.Description, t.Category, t.RequiredScope}, " "))
}

// --- one-time key reveal snippets ---

// mcpShareSnippet is one ready-to-paste client configuration block.
type mcpShareSnippet struct {
	Key   string
	Label string
	Code  string
}

// mcpShareSnippetClients is the canonical client order for the reveal.
var mcpShareSnippetClients = []struct{ Key, Label string }{
	{"claudeDesktop", "Claude Desktop"},
	{"claudeCode", "Claude Code"},
	{"cursor", "Cursor"},
	{"cloudCode", "Cloud Code"},
}

// mcpShareSnippets builds the reveal's snippet list. Backend-provided snippets
// win when present (looked up by canonical key or a small alias set); otherwise
// the gateway generates a correct block from the endpoint URL and key.
func mcpShareSnippets(secret MCPShareSecret) []mcpShareSnippet {
	out := make([]mcpShareSnippet, 0, len(mcpShareSnippetClients))
	for _, c := range mcpShareSnippetClients {
		code := ""
		if secret.Snippets != nil {
			code = secret.Snippets[c.Key]
			if code == "" {
				code = mcpShareLookupSnippet(secret.Snippets, c.Key)
			}
		}
		if code == "" {
			code = mcpShareGeneratedSnippet(c.Key, secret.MCPURL, secret.Token)
		}
		out = append(out, mcpShareSnippet{Key: c.Key, Label: c.Label, Code: code})
	}
	return out
}

// mcpShareLookupSnippet finds a snippet under a normalized key (case/sep
// insensitive) so "claude_desktop" and "Claude-Desktop" both match.
func mcpShareLookupSnippet(snippets map[string]string, key string) string {
	want := normalizeSnippetKey(key)
	for k, v := range snippets {
		if normalizeSnippetKey(k) == want {
			return v
		}
	}
	return ""
}

func normalizeSnippetKey(k string) string {
	k = strings.ToLower(k)
	k = strings.NewReplacer("_", "", "-", "", " ", "").Replace(k)
	return k
}

// mcpShareGeneratedSnippet renders a client config that contains the endpoint
// URL and the key. The blocks are intentionally minimal and current.
func mcpShareGeneratedSnippet(key, mcpURL, token string) string {
	if strings.TrimSpace(mcpURL) == "" {
		mcpURL = "(project MCP endpoint)"
	}
	switch key {
	case "claudeDesktop":
		return fmt.Sprintf(`{
  "mcpServers": {
    "memory": {
      "command": "npx",
      "args": ["-y", "mcp-remote", %q, "--header", "Authorization: Bearer %s"]
    }
  }
}`, mcpURL, token)
	case "claudeCode":
		return fmt.Sprintf("claude mcp add --transport http memory %s --header \"Authorization: Bearer %s\"", mcpURL, token)
	case "cursor":
		return fmt.Sprintf(`{
  "mcpServers": {
    "memory": {
      "url": %q,
      "headers": { "Authorization": "Bearer %s" }
    }
  }
}`, mcpURL, token)
	case "cloudCode":
		return fmt.Sprintf(`{
  "mcpServers": {
    "memory": {
      "httpUrl": %q,
      "headers": { "Authorization": "Bearer %s" }
    }
  }
}`, mcpURL, token)
	default:
		return mcpURL + "\nAuthorization: Bearer " + token
	}
}
