package main

import (
	"context"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"

	ui "github.com/emergent-company/go-daisy/components/ui"
	"github.com/labstack/echo/v4"
)

// --- MCP servers web UI (project management surface at /settings/mcp-servers) ---
//
// Mirrors the api_tokens trio layout: a standalone crumb-framed page in the
// sidebar Settings group with a list of registered servers, a create page at
// /settings/mcp-servers/new, and an edit page at
// /settings/mcp-servers/:id/edit. Sync / Inspect / per-tool toggles run as
// client-side JSON fetches (design D5) so row expand state survives; only
// create / update / delete / server-level enabled toggles round-trip through
// memory as writes.
//
// The registry lists every registered server (builtin + external). Builtin
// servers are read-only in the UI: no sync/inspect/edit/delete actions, no
// enabled or per-tool toggles (their tools are managed through memory's
// builtin-tools surface).

// mcpServersPageData is the payload for the list page (MCPServersPage): the
// registry's servers (each already carrying its cached Tools + ToolCount from
// memory's list DTO), a whole-page LoadErr, and PRG flash feedback.
type mcpServersPageData struct {
	Servers  []MCPServer
	LoadErr  error
	FlashMsg string
	FlashErr error
}

// mcpServerFormData is the payload shared by the create and edit pages
// (MCPServerNewPage / MCPServerEditPage) and by the inline error re-renders of
// their POST handlers. Transport/connection fields are stored as ordered
// values (not the MCPServer maps) so the form can round-trip drafts after a
// validation failure without losing the user's typed rows.
type mcpServerFormData struct {
	ID      string // "" in create mode; the server id when editing
	Name    string
	Type    string // "stdio" | "sse" | "http" (create default "stdio")
	URL     string
	Command string
	Args    []string
	Headers []mcpKVField // key/value rows for sse/http headers
	Env     []mcpKVField // key/value rows for stdio environment
	Enabled bool

	// LoadErr / NotFound only apply to the edit page's GET (the server could
	// not be resolved); the POST re-renders carry FieldErrs / GeneralErr.
	LoadErr    error
	NotFound   bool
	FieldErrs  mcpServerFieldErrs
	GeneralErr error
	FlashErr   error
}

// mcpServerFieldErrs carries one inline error message per form field group,
// rendered under the matching fieldset by the shared form template.
type mcpServerFieldErrs struct {
	Name    string
	Type    string
	URL     string
	Command string
	Headers string
	Env     string
}

// mcpKVField is one ordered header/env key/value row in the transport form.
// Secret marks a row whose value memory stores write-only: edits pre-fill it
// blank and a blank submission keeps the stored value.
type mcpKVField struct {
	Key    string
	Value  string
	Secret bool
}

// mcpServerSettingsBase is the base path for the MCP Servers management area.
const mcpServerSettingsBase = "/settings/mcp-servers"

// mcpTransportOption is one selectable transport in the create/edit form.
type mcpTransportOption struct {
	Value string
	Title string
	Desc  string
}

// mcpTransportOptions orders the transport radio choices (D3). stdio is the
// create default because every form needs a deterministic checked radio and
// the stdio panel (command/args/env) is the zero-config choice.
var mcpTransportOptions = []mcpTransportOption{
	{"stdio", "stdio", "Runs a command on the memory host."},
	{"sse", "sse", "Streaming events over a remote URL."},
	{"http", "http", "HTTP calls against a remote URL."},
}

// mcpServerIsBuiltin reports whether a registry row is memory's builtin
// server (read-only in the UI: no config editing, deletion, sync, inspect, or
// tool toggles).
func mcpServerIsBuiltin(s MCPServer) bool {
	return s.Type == "builtin"
}

// mcpServerValidTransport reports whether t is a transport a user may choose
// in the create/edit form (stdio/sse/http — never builtin).
func mcpServerValidTransport(t string) bool {
	switch t {
	case "stdio", "sse", "http":
		return true
	default:
		return false
	}
}

// mcpServerBadgeIntent maps a transport/type to a badge colour for the list.
func mcpServerBadgeIntent(t string) ui.BadgeIntent {
	switch t {
	case "builtin":
		return ui.BadgeGhost
	case "http":
		return ui.BadgeAccent
	case "sse":
		return ui.BadgeNeutral
	case "stdio":
		return ui.BadgeInfo
	default:
		return ui.BadgeNeutral
	}
}

// mcpKVFields converts a string map (headers/env decoded from a server) into
// deterministic, ordered key/value rows for form pre-fill.
func mcpKVFields(m map[string]string) []mcpKVField {
	if len(m) == 0 {
		return nil
	}
	keys := slices.Sorted(maps.Keys(m))
	out := make([]mcpKVField, 0, len(keys))
	for _, k := range keys {
		out = append(out, mcpKVField{Key: k, Value: m[k]})
	}
	return out
}

// mcpKVFieldMap converts ordered key/value rows back into a map, dropping
// blank keys (validation has already rejected value-without-key rows). Secret
// rows are included too — the request carries their (possibly blank) values
// inline and memory strips them from the stored/returned config.
func mcpKVFieldMap(rows []mcpKVField) map[string]string {
	var out map[string]string
	for _, r := range rows {
		k := strings.TrimSpace(r.Key)
		if k == "" {
			continue
		}
		if out == nil {
			out = map[string]string{}
		}
		out[k] = strings.TrimSpace(r.Value)
	}
	return out
}

// mcpKVFieldSecretKeys returns the distinct keys of rows marked Secret, in
// row order. Blank keys are skipped (validation rejects them).
func mcpKVFieldSecretKeys(rows []mcpKVField) []string {
	var out []string
	seen := make(map[string]bool)
	for _, r := range rows {
		k := strings.TrimSpace(r.Key)
		if k == "" || !r.Secret || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return out
}

// mcpKVFieldsWithSecrets builds editor rows from a server's non-secret
// map entries (values shown) plus one blank-valued Secret row per write-only
// key. Memory never returns secret values, so those rows pre-fill empty and
// rely on the "leave blank to keep" contract on submit.
func mcpKVFieldsWithSecrets(m map[string]string, secretKeys []string) []mcpKVField {
	out := mcpKVFields(m)
	keys := make([]string, 0, len(secretKeys))
	for _, k := range secretKeys {
		if k = strings.TrimSpace(k); k != "" {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	for _, k := range keys {
		out = append(out, mcpKVField{Key: k, Secret: true})
	}
	return out
}

// mcpEnsureSecretKeyPlaceholders fills a server's Env/Headers maps with a
// blank value for every listed secret key. A full-server PATCH (the toggle
// flow) must name each secret key in the transport map so memory keeps the
// stored value instead of dropping it.
func mcpEnsureSecretKeyPlaceholders(s *MCPServer) {
	if len(s.SecretEnvKeys) > 0 {
		s.Env = mcpMapWithSecretPlaceholders(s.Env, s.SecretEnvKeys)
	}
	if len(s.SecretHeadersKeys) > 0 {
		s.Headers = mcpMapWithSecretPlaceholders(s.Headers, s.SecretHeadersKeys)
	}
}

// mcpMapWithSecretPlaceholders returns m with a blank entry for each key from
// keys that m does not already carry.
func mcpMapWithSecretPlaceholders(m map[string]string, keys []string) map[string]string {
	if len(keys) == 0 {
		return m
	}
	if m == nil {
		m = make(map[string]string, len(keys))
	}
	for _, k := range keys {
		if k = strings.TrimSpace(k); k == "" {
			continue
		}
		if _, ok := m[k]; !ok {
			m[k] = ""
		}
	}
	return m
}

// mcpServerFormDataFromRequest parses a submitted create/edit form into a
// draft. transport defaults to "stdio" (create's default); args arrive as one
// per line in a textarea. Blank kv rows are dropped here and re-checked during
// validation so value-without-key submissions still surface an inline error.
func mcpServerFormDataFromRequest(c echo.Context) mcpServerFormData {
	d := mcpServerFormData{
		Name:    strings.TrimSpace(c.FormValue("name")),
		Type:    strings.TrimSpace(c.FormValue("type")),
		URL:     strings.TrimSpace(c.FormValue("url")),
		Command: strings.TrimSpace(c.FormValue("command")),
		Enabled: c.FormValue("enabled") != "",
	}
	if !mcpServerValidTransport(d.Type) {
		d.Type = "stdio"
	}
	for _, line := range strings.Split(c.FormValue("args"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			d.Args = append(d.Args, line)
		}
	}
	d.Headers = mcpKVFieldsFromForm(c, "headers")
	d.Env = mcpKVFieldsFromForm(c, "env")
	return d
}

// mcpKVFieldsFromForm reads zipped name="<prefix>.key"/"<prefix>.value" rows
// from the submitted form, skipping rows where both halves are blank. The
// <prefix>.secret select is always submitted (Plain -> "", Secret -> "1"), so
// its values stay index-aligned with the key/value rows even when only some
// rows are secret. A truthy value ("1" or "true", case-insensitive) marks the
// row secret.
func mcpKVFieldsFromForm(c echo.Context, prefix string) []mcpKVField {
	keys := c.Request().Form[prefix+".key"]
	values := c.Request().Form[prefix+".value"]
	secrets := c.Request().Form[prefix+".secret"]
	n := max(len(keys), len(values))
	var out []mcpKVField
	for i := range n {
		k := ""
		if i < len(keys) {
			k = strings.TrimSpace(keys[i])
		}
		v := ""
		if i < len(values) {
			v = strings.TrimSpace(values[i])
		}
		if k == "" && v == "" {
			continue
		}
		secret := i < len(secrets) && mcpKVSecretValue(secrets[i])
		out = append(out, mcpKVField{Key: k, Value: v, Secret: secret})
	}
	return out
}

// mcpKVSecretValue reports whether a submitted "<prefix>.secret" value marks
// its row secret. The select posts "" for Plain and "1" for Secret; "true"
// (any case) is accepted too so non-select/older submissions keep working.
func mcpKVSecretValue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true":
		return true
	default:
		return false
	}
}

// validate validates the draft form state and fills FieldErrs. Memory-side
// errors (duplicate name) are handled by the POST handlers after the call.
func (d *mcpServerFormData) validate() mcpServerFieldErrs {
	var fe mcpServerFieldErrs
	if d.Name == "" {
		fe.Name = "Server name is required."
	} else if len(d.Name) > 255 {
		fe.Name = "Server name must be at most 255 characters."
	}
	switch d.Type {
	case "stdio":
		if d.Command == "" {
			fe.Command = "Command is required for stdio servers."
		}
	case "sse", "http":
		if d.URL == "" {
			fe.URL = "A URL is required for " + d.Type + " servers."
		} else if u, err := url.Parse(d.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			fe.URL = "Enter a full URL starting with http:// or https://."
		}
	}
	for _, r := range d.Headers {
		if strings.TrimSpace(r.Key) == "" {
			fe.Headers = "Header rows need a name — blank header names are rejected."
			break
		}
	}
	for _, r := range d.Env {
		if strings.TrimSpace(r.Key) == "" {
			fe.Env = "Environment rows need a name — blank environment names are rejected."
			break
		}
	}
	return fe
}

// toServer maps a validated draft onto the MCPServer wire shape that memory
// creates/updates. Only the transport's fields are populated so cross-field
// leftovers never reach memory. Secret rows still send their value inline
// (memory strips it) and are additionally named in SecretEnvKeys /
// SecretHeadersKeys so memory stores them write-only; a blank secret value on
// update keeps the stored one.
func (d *mcpServerFormData) toServer() *MCPServer {
	s := &MCPServer{
		Name:    d.Name,
		Type:    d.Type,
		Enabled: d.Enabled,
	}
	switch d.Type {
	case "stdio":
		s.Command = d.Command
		s.Args = d.Args
		s.Env = mcpKVFieldMap(d.Env)
		s.SecretEnvKeys = mcpKVFieldSecretKeys(d.Env)
	case "sse", "http":
		s.URL = d.URL
		s.Headers = mcpKVFieldMap(d.Headers)
		s.SecretHeadersKeys = mcpKVFieldSecretKeys(d.Headers)
	}
	return s
}

// mcpServerFormDataFromServer converts a resolved server into form state for
// the edit page (transport pre-checked, headers/env as ordered rows, args as
// lines).
func mcpServerFormDataFromServer(s MCPServer) mcpServerFormData {
	d := mcpServerFormData{
		ID:      s.ID,
		Name:    s.Name,
		Type:    s.Type,
		URL:     s.URL,
		Command: s.Command,
		Args:    s.Args,
		Headers: mcpKVFieldsWithSecrets(s.Headers, s.SecretHeadersKeys),
		Env:     mcpKVFieldsWithSecrets(s.Env, s.SecretEnvKeys),
		Enabled: s.Enabled,
	}
	if !mcpServerValidTransport(d.Type) {
		d.Type = "stdio"
	}
	return d
}

// mcpFindServerByID returns a pointer to the registry entry with the given id,
// or nil.
func mcpFindServerByID(servers []MCPServer, id string) *MCPServer {
	for i := range servers {
		if servers[i].ID == id {
			return &servers[i]
		}
	}
	return nil
}

// mcpServerIsNameConflict reports whether a memory write error is the
// duplicate-name rejection (memory: 'server with name "x" already exists in
// this project' — surfaced inline on the name field, no partial save).
func mcpServerIsNameConflict(err error) bool {
	return isMemoryStatus(err, http.StatusConflict) || (err != nil && strings.Contains(err.Error(), "already exists"))
}

// mcpToolsFragment renders the data-mcp-tools rows container for one server
// into an HTML string. The sync JSON endpoint returns it so the page can swap
// the refreshed cached-tool list back into the row (outerHTML replace) without
// a second server trip and without duplicating the row markup in JS.
func mcpToolsFragment(s MCPServer, tools []MCPTool) (string, error) {
	var b strings.Builder
	if err := mcpServerToolsContainer(s, tools).Render(context.Background(), &b); err != nil {
		return "", err
	}
	return b.String(), nil
}

// mcpToolsFragmentOrEmpty is mcpToolsFragment's best-effort wrapper for the
// sync JSON payload: a render failure yields "" (the memory tool state is what
// matters; the page falls back to the old rows).
func mcpToolsFragmentOrEmpty(s MCPServer, tools []MCPTool) string {
	html, err := mcpToolsFragment(s, tools)
	if err != nil {
		return ""
	}
	return html
}

// mcpFormPageHeading derives the edit page header copy (the create page's
// header is inlined in the template).
func mcpFormPageHeading(editing bool, name string) (title, subtitle string) {
	if editing {
		return "Edit server", "Connection and registration details for " + name + ". Changes apply immediately."
	}
	return "New MCP server", "Name the server and pick a transport — stdio runs a command on the memory host, sse/http dial a remote endpoint."
}

// mcpPrunedToolNames returns tool names present in before but missing from
// after (the sync prune diff surfaced in the success toast).
func mcpPrunedToolNames(before, after []MCPTool) []string {
	afterSet := make(map[string]bool, len(after))
	for _, t := range after {
		afterSet[t.ToolName] = true
	}
	var out []string
	for _, t := range before {
		if !afterSet[t.ToolName] {
			out = append(out, t.ToolName)
		}
	}
	return out
}
