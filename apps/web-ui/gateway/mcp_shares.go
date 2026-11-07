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
// list may arrive as a bare array or under a {"shares":[...]} or
// {"instances":[...]} key. shareRaw/decodeMCPShareList accept all of these so
// the UI keeps working across backend versions; an unrecognized object wrapper
// is surfaced as an error rather than a false empty list.

// MCPShareInstance is one named MCP share instance: an API-key grant exposing a
// chosen subset of memory tools to outside MCP clients. Instances scope TOOLS
// ONLY — there is no agent allowlist; agent sharing uses the agent-scoped MCP
// endpoint.
type MCPShareInstance struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Tools       []string `json:"tools"`
	// TokenID is the credential that authenticates the share. It is the value
	// graph write stamps record as credential_id, so it is what the "writes via
	// this share" link filters on. Omitted by older backends.
	TokenID string `json:"tokenId,omitempty"`
	// RecipientEmails are the addresses the share key was invited to. Empty for
	// a share that was never emailed.
	RecipientEmails []string `json:"recipientEmails,omitempty"`
	// ReadOnly reports whether the share grants only read scopes.
	ReadOnly   bool    `json:"readOnly"`
	Status     string  `json:"status"`
	IsLegacy   bool    `json:"isLegacy"`
	CreatedAt  string  `json:"createdAt"`
	LastUsedAt *string `json:"lastUsedAt"`
	ToolCount  int     `json:"toolCount"`
}

// MCPShareInput is the create/update request body. Tools must hold at least one
// name. Emails is only meaningful on create and rotate: the listed recipients
// receive the one-time key, the endpoint, and client setup snippets.
type MCPShareInput struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Tools       []string `json:"tools"`
	Emails      []string `json:"emails,omitempty"`
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
// responses only). EmailsSent lists the recipients the key was queued to and
// EmailError reports a delivery problem; both are empty when no invite was
// requested.
type MCPShareCreated struct {
	MCPShareInstance
	MCPShareSecret
	EmailsSent []string `json:"emailsSent,omitempty"`
	EmailError string   `json:"emailError,omitempty"`
}

// MCPShareInvite is the ad-hoc invite result: the recipients the share's
// EXISTING key and setup instructions were queued to, plus that key's prefix so
// the caller can confirm the key was re-sent rather than replaced.
type MCPShareInvite struct {
	TokenPrefix string   `json:"tokenPrefix"`
	EmailsSent  []string `json:"emailsSent,omitempty"`
	EmailError  string   `json:"emailError,omitempty"`
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

// MCPShareTemplate is one named preset resolved against the catalog: the tools
// a share should get when the user picks it. Default marks the preset the
// create form preselects.
type MCPShareTemplate struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Description string   `json:"description,omitempty"`
	Default     bool     `json:"default,omitempty"`
	Tools       []string `json:"tools"`
	ToolCount   int      `json:"toolCount"`
}

// MCPShareCatalog is the tool catalog plus the presets resolved against it.
// Templates is empty for a backend that predates share templates, which renders
// the picker without the preset row.
type MCPShareCatalog struct {
	Tools     []MCPShareTool     `json:"tools"`
	Templates []MCPShareTemplate `json:"templates,omitempty"`
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

// decodeMCPShareList accepts a bare array or an object wrapping the list under
// either the "shares" (gateway/agent-share convention) or "instances" key
// (memory's ShareInstanceListResponse). Any other object shape is an error so
// the page renders its error state instead of a false empty list.
func decodeMCPShareList(raw json.RawMessage) ([]MCPShareInstance, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err == nil {
		return decodeMCPShareItems(items)
	}
	var wrap map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, fmt.Errorf("decode mcp share list: %w", err)
	}
	for _, key := range []string{"instances", "shares"} {
		payload, ok := wrap[key]
		if !ok {
			continue
		}
		var wrapped []json.RawMessage
		if err := json.Unmarshal(payload, &wrapped); err != nil {
			return nil, fmt.Errorf("decode mcp share list %q: %w", key, err)
		}
		return decodeMCPShareItems(wrapped)
	}
	return nil, fmt.Errorf("decode mcp share list: unrecognized payload shape (want array, \"shares\" or \"instances\")")
}

// mcpShareCountsPresent records which count fields the backend actually sent.
// MCPShareInstance carries a plain int count, so after decoding an omitted count
// is indistinguishable from an explicit zero; the pointer preserves that
// presence bit so a backend-supplied 0 is never overwritten by the tool
// allowlist length. A JSON null counts as absent.
type mcpShareCountsPresent struct {
	ToolCount *int `json:"toolCount"`
}

// decodeMCPShareItems decodes every list element, preserving count-field
// presence per element.
func decodeMCPShareItems(items []json.RawMessage) ([]MCPShareInstance, error) {
	out := make([]MCPShareInstance, 0, len(items))
	for i, item := range items {
		inst, err := decodeMCPShareItem(item)
		if err != nil {
			return nil, fmt.Errorf("decode mcp share list item %d: %w", i, err)
		}
		out = append(out, inst)
	}
	return out, nil
}

// decodeMCPShareItem fills the tool count from the allowlist array so list rows
// show a real number when the backend omits an explicit count. Only an omitted
// (or null) count is derived: an explicit backend count — including an explicit
// 0 — always wins.
func decodeMCPShareItem(raw json.RawMessage) (MCPShareInstance, error) {
	var inst MCPShareInstance
	if err := json.Unmarshal(raw, &inst); err != nil {
		return MCPShareInstance{}, err
	}
	var present mcpShareCountsPresent
	if err := json.Unmarshal(raw, &present); err != nil {
		return MCPShareInstance{}, err
	}
	if present.ToolCount == nil && len(inst.Tools) > 0 {
		inst.ToolCount = len(inst.Tools)
	}
	return inst, nil
}

// decodeMCPShareCatalog accepts the catalog endpoint's {"tools":[…],
// "templates":[…]} shape, or a bare tools array from a backend that predates
// templates (templates then stay empty and the picker renders without presets).
func decodeMCPShareCatalog(raw json.RawMessage) (*MCPShareCatalog, error) {
	var list []MCPShareTool
	if err := json.Unmarshal(raw, &list); err == nil {
		return &MCPShareCatalog{Tools: list}, nil
	}
	var wrap struct {
		Tools     []MCPShareTool     `json:"tools"`
		Templates []MCPShareTemplate `json:"templates"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, err
	}
	return &MCPShareCatalog{Tools: wrap.Tools, Templates: wrap.Templates}, nil
}

// mcpShareApplyDefaultTemplate returns the tools of the preset flagged Default
// when the draft has no selection yet. Used only by the create-page GET: a
// submitted draft must reach validation exactly as the user left it, and an
// edit must never rewrite an existing grant.
func mcpShareApplyDefaultTemplate(catalog *MCPShareCatalog, current []string) []string {
	if catalog == nil || len(current) > 0 {
		return current
	}
	for _, tmpl := range catalog.Templates {
		if tmpl.Default && len(tmpl.Tools) > 0 {
			return append([]string(nil), tmpl.Tools...)
		}
	}
	return current
}

// mcpShareToolSet turns a tool list into a set.
func mcpShareToolSet(tools []string) map[string]bool {
	set := make(map[string]bool, len(tools))
	for _, name := range tools {
		set[name] = true
	}
	return set
}

// mcpShareSameToolSet reports whether two tool lists describe the same set.
// Order and duplicates are irrelevant, and two empty lists are not "equal" (an
// empty selection matches no preset).
func mcpShareSameToolSet(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	as, bs := mcpShareToolSet(a), mcpShareToolSet(b)
	if len(as) != len(bs) {
		return false
	}
	for name := range as {
		if !bs[name] {
			return false
		}
	}
	return true
}

// mcpShareSelectedLabel seeds the picker's live counter server-side, so a
// preselected preset never renders "0 selected" until the first interaction.
// It counts selected tools the catalog actually offers, matching the checkboxes
// the browser can see.
func mcpShareSelectedLabel(catalog []MCPShareTool, tools []string) string {
	set := mcpShareToolSet(tools)
	count := 0
	for _, tool := range catalog {
		if set[tool.Name] {
			count++
		}
	}
	return fmt.Sprintf("%d selected", count)
}

// mcpShareTemplateActiveClass marks the preset whose tools exactly match the
// current selection, so a server-preselected default renders as chosen.
func mcpShareTemplateActiveClass(active bool) string {
	if active {
		return "gap-1.5 btn-active"
	}
	return "gap-1.5"
}

// mcpShareAriaPressed is the pressed state of a preset toggle button.
func mcpShareAriaPressed(active bool) string {
	if active {
		return "true"
	}
	return "false"
}

// decodeMCPShareCreated accepts the instance fields and the secret fields
// flattened at the top level, or the instance nested under an "instance" key.
func decodeMCPShareCreated(raw json.RawMessage) (*MCPShareCreated, error) {
	var wire struct {
		MCPShareInstance
		MCPShareSecret
		Instance   *MCPShareInstance `json:"instance"`
		EmailsSent []string          `json:"emailsSent,omitempty"`
		EmailError string            `json:"emailError,omitempty"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, err
	}
	out := &MCPShareCreated{
		MCPShareSecret: wire.MCPShareSecret,
		EmailsSent:     wire.EmailsSent,
		EmailError:     wire.EmailError,
	}
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

// UpdateMCPShareInstance persists a share's name/description/tool allowlist.
// The key is unchanged by an update.
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
// one-time secret (the previous key stops working). Recipients in emails get
// the new key by email.
func (m *MemoryClient) RotateMCPShareInstance(ctx context.Context, id string, emails []string) (*MCPShareCreated, error) {
	var body any
	if len(emails) > 0 {
		body = map[string][]string{"emails": emails}
	}
	raw, err := m.shareRaw(ctx, http.MethodPost, m.mcpSharePath(ctx, "shares", id, "rotate"), body)
	if err != nil {
		return nil, err
	}
	return decodeMCPShareCreated(raw)
}

// InviteMCPShareInstance emails an existing share's CURRENT key and client
// setup instructions to the given recipients. Unlike rotate it mints nothing,
// so everyone already using the share keeps working; the backend reports
// not_recoverable when the key predates token encryption.
func (m *MemoryClient) InviteMCPShareInstance(ctx context.Context, id string, emails []string) (*MCPShareInvite, error) {
	raw, err := m.shareRaw(ctx, http.MethodPost, m.mcpSharePath(ctx, "shares", id, "invite"), map[string][]string{"emails": emails})
	if err != nil {
		return nil, err
	}
	var out MCPShareInvite
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode mcp share invite: %w", err)
	}
	return &out, nil
}

// ListMCPShareCatalog fetches the memory tool catalog a share may expose plus
// the presets resolved against it.
func (m *MemoryClient) ListMCPShareCatalog(ctx context.Context) (*MCPShareCatalog, error) {
	raw, err := m.shareRaw(ctx, http.MethodGet, m.mcpSharePath(ctx, "tools"), nil)
	if err != nil {
		return nil, err
	}
	return decodeMCPShareCatalog(raw)
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

// mcpShareTemplateToolsAttr renders a preset's tool names as the JSON array the
// picker reads from data-share-template-tools. The browser decodes the escaped
// attribute value back into this exact string.
func mcpShareTemplateToolsAttr(t MCPShareTemplate) string {
	if len(t.Tools) == 0 {
		return "[]"
	}
	encoded, err := json.Marshal(t.Tools)
	if err != nil {
		return "[]"
	}
	return string(encoded)
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

// mcpShareRecipientsLabel renders a share's invite recipients for the row's meta
// line. It names the addresses so an admin can see who holds the share, showing
// up to two and summarising the tail as "+N" to keep the row on one line; the
// full list is in the title (mcpShareRecipientsTitle).
func mcpShareRecipientsLabel(emails []string) string {
	switch len(emails) {
	case 0:
		return ""
	case 1:
		return "invited " + emails[0]
	case 2:
		return "invited " + emails[0] + ", " + emails[1]
	default:
		return fmt.Sprintf("invited %s, %s +%d", emails[0], emails[1], len(emails)-2)
	}
}

// mcpShareRecipientsTitle is the full recipient list, used as the chip's hover
// text so the addresses the row had to summarise are still reachable.
func mcpShareRecipientsTitle(emails []string) string {
	return strings.Join(emails, ", ")
}

// mcpShareActivityHref links to the object browser scoped to the objects this
// share's key created or last updated. The credential id is the value graph
// write stamps record for the share's token; `via` carries the share name only
// so the browser can label the scope (it never filters on it).
func mcpShareActivityHref(inst MCPShareInstance) string {
	if inst.TokenID == "" {
		return ""
	}
	q := url.Values{}
	q.Set("credential_id", inst.TokenID)
	q.Set("via", inst.Name)
	return "/objects?" + q.Encode()
}
