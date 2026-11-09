package main

import (
	"context"
	"encoding/json"
	"fmt"
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
//
// The builtin server's capability-group taxonomy is NOT loaded here: the tool
// rows (and the groups that organise them) are fetched when a row is first
// expanded, so the page does not pay for a catalogue nobody has opened (#210).
type mcpServersPageData struct {
	Servers  []MCPServer
	LoadErr  error
	FlashMsg string
	FlashErr error
}

// mcpServerToolsFragmentPath is where an expanded row fetches its cached-tool
// rows from (uiMCPServerToolsFragment). Kept next to the page data so the templ
// and the route cannot drift.
func mcpServerToolsFragmentPath(serverID string) string {
	return mcpServerSettingsBase + "/" + url.PathEscape(serverID) + "/tools"
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

	// RequiredConfigKeys / MissingConfigKeys carry the server's declared
	// required configuration and the subset currently blank, for the
	// required-row markers and the configure banner. ConfigureMode is set when
	// the edit page is opened with ?configure=1 (post-install redirect).
	RequiredConfigKeys []string
	MissingConfigKeys  []string
	ConfigureMode      bool
	// RequiredConfig carries the full declared entries (name + kind + registry
	// description) so the configure banner can explain each missing key rather
	// than only name it.
	RequiredConfig []MCPRequiredConfigKey
	// RegistryRepositoryURL is the registry entry's source repo (empty for
	// manual servers); the configure view links to it for credential setup.
	RegistryRepositoryURL string

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
// blank and a blank submission keeps the stored value. Required marks a row
// corresponding to the server's declared required configuration; Missing marks
// a required row whose value is blank (drive the "Required" marker and the
// "Enter value" placeholder). Hint is the registry's guidance for a required
// key (shown under the row); Placeholder is the registry's value template,
// shown as the value input's placeholder — never prefilled.
//
// Auth marks a required Authorization header row: instead of a free-form value
// the form renders a scheme picker (default Bearer) plus a token field, and
// assembles "<scheme> <token>" on submit. AuthScheme is the selected scheme
// ("" = raw, no prefix).
type mcpKVField struct {
	Key         string
	Value       string
	Secret      bool
	Required    bool
	Missing     bool
	Hint        string
	Placeholder string
	Auth        bool
	AuthScheme  string
}

// mcpImportPageData backs the Claude-config import page. Config is the pasted
// document; Target is "auto" (remote → shared host, stdio → connector),
// "server", or "device"; DeviceInstanceID binds device entries. Result is
// populated after a POST so the created/skipped/failed report is visible
// without a round trip.
type mcpImportPageData struct {
	Config           string
	Target           string
	DeviceInstanceID string
	Overwrite        bool
	DryRun           bool
	Devices          []RelaySession
	DevicesErr       error
	FieldErrConfig   string
	FieldErrTarget   string
	GeneralErr       error
	FlashErr         error
	Result           *MCPImportResult
}

// mcpImportTargets are the accepted delivery-target choices.
func mcpImportTargets() []string { return []string{"auto", "server", "device"} }

// validateImportDraft checks the import form before calling memory. The
// per-entry transport rules are memory's job; this only rejects an empty or
// non-JSON document and an unknown target.
func (d *mcpImportPageData) validate() {
	d.FieldErrConfig = ""
	d.FieldErrTarget = ""
	if strings.TrimSpace(d.Config) == "" {
		d.FieldErrConfig = "Paste a Claude MCP config document."
	} else if !json.Valid([]byte(d.Config)) {
		d.FieldErrConfig = "That is not valid JSON."
	}
	if d.Target == "" {
		d.Target = "auto"
	}
	if !slices.Contains(mcpImportTargets(), d.Target) {
		d.FieldErrTarget = "Choose auto, server, or device."
	}
}

// Summary renders the one-line import outcome.
func (r *MCPImportResult) Summary() string {
	if r == nil {
		return ""
	}
	prefix := ""
	if r.DryRun {
		prefix = "Dry run: "
	}
	return fmt.Sprintf("%s%d created, %d updated, %d skipped, %d failed",
		prefix, len(r.Created), len(r.Updated), len(r.Skipped), len(r.Failed))
}

func (d *mcpImportPageData) hasFieldErrs() bool {
	return d.FieldErrConfig != "" || d.FieldErrTarget != ""
}

// mcpServerSettingsBase is the base path for the MCP Servers management area.
const mcpServerSettingsBase = "/settings/mcp-servers"

// mcpServerAddDialogID is the header add-chooser dialog's element id. Kept next
// to the base path so the header trigger (data-dialog-open) and the dialog
// shell cannot drift.
const mcpServerAddDialogID = "mcp-server-add-dialog"

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

// mcpServerIsDevice reports whether a registry row targets a connected device
// (pushed over the relay to a connector) rather than being connected outbound
// by the shared host. Device rows drop the host-only outbound affordances
// (sync, inspect, OAuth connect) since the host never dials them.
func mcpServerIsDevice(s MCPServer) bool {
	return s.Target == "device"
}

// mcpServerOAuthShown reports whether a server row renders the OAuth
// connect/disconnect controls: a remote (http/sse) server whose list DTO
// carried an oauth object. Absent for builtin, stdio, and device-targeted
// servers.
func mcpServerOAuthShown(s MCPServer) bool {
	if mcpServerIsBuiltin(s) || mcpServerIsDevice(s) {
		return false
	}
	if s.Type != "http" && s.Type != "sse" {
		return false
	}
	return s.OAuth != nil
}

// mcpServerAuthBadge returns the list summary's at-a-glance operational-state
// badge for a server whose "enabled" state alone can be misleading. A remote
// server that participates in OAuth connect but has no stored token and no
// cached tools cannot operate yet, so it renders "Needs auth" instead of
// looking healthy; a stored token renders "Connected" (the server was
// authorized at least once). A non-remote external server with no cached tools
// has never completed a successful sync/probe and renders "Never tested".
//
// This derives entirely from existing list-DTO state: OAuth summary
// (configured/connected) and the cached tool rows. It deliberately does not
// invent a persisted last-tested/last-connected field — the backend has none
// (see PR notes), so "Connected" means a token set is stored and "Never
// tested" means no tools have ever been cached.
//
// Builtin rows, device-targeted rows, and rows that already carry a
// "Needs configuration" warning are skipped (one warning per row is enough).
func mcpServerAuthBadge(s MCPServer) (label string, variant ui.BadgeIntent, icon string, ok bool) {
	if mcpServerIsBuiltin(s) || mcpServerIsDevice(s) || s.NeedsConfiguration {
		return "", "", "", false
	}
	if s.OAuth != nil {
		if s.OAuth.Connected {
			return "Connected", ui.BadgeSuccess, "lucide--shield-check", true
		}
		if len(s.Tools) == 0 && s.ToolCount == 0 {
			return "Needs auth", ui.BadgeWarning, "lucide--key-round", true
		}
		return "", "", "", false
	}
	if len(s.Tools) == 0 && s.ToolCount == 0 {
		return "Never tested", ui.BadgeNeutral, "lucide--circle-help", true
	}
	return "", "", "", false
}

// mcpServerDisplayName is the list summary's label for a server. The builtin
// server's stored name is the literal "builtin"; the UI shows it as "Memory
// tools" so it reads as memory's own tool source rather than a transport.
func mcpServerDisplayName(s MCPServer) string {
	if mcpServerIsBuiltin(s) {
		return "Memory tools"
	}
	return s.Name
}

// mcpServerSummaryIcon maps a server to the list summary's leading icon: the
// package mark for the builtin tool source, the server mark otherwise.
func mcpServerSummaryIcon(s MCPServer) string {
	if mcpServerIsBuiltin(s) {
		return "lucide--package"
	}
	return "lucide--server"
}

// mcpServerSummaryIconClass is the leading icon's tone in the list summary.
func mcpServerSummaryIconClass(s MCPServer) string {
	if mcpServerIsBuiltin(s) {
		return "size-4 shrink-0 text-primary/70"
	}
	return "size-4 shrink-0 text-muted-faint"
}

// mcpBuiltinToolGroupView is one rendered builtin capability group: the
// server-owned group plus the cached tool rows it covers, in the group's own
// membership order.
type mcpBuiltinToolGroupView struct {
	Group BuiltinToolGroup
	Rows  []MCPTool
}

// mcpBuiltinToolGroups joins memory's builtin capability taxonomy to the
// server's cached tool rows: each group carries the rows whose ToolName it
// lists (in the group's order, dropping names with no cached row), and any
// cached tool no group covers is returned as other so it stays reachable. The
// taxonomy is server-owned — this only joins membership to descriptions.
func mcpBuiltinToolGroups(tools []MCPTool, groups []BuiltinToolGroup) (grouped []mcpBuiltinToolGroupView, other []MCPTool) {
	byName := make(map[string]MCPTool, len(tools))
	for _, t := range tools {
		byName[t.ToolName] = t
	}
	covered := make(map[string]bool, len(tools))
	for _, g := range groups {
		var rows []MCPTool
		for _, name := range g.Tools {
			t, ok := byName[name]
			if !ok || covered[name] {
				continue
			}
			covered[name] = true
			rows = append(rows, t)
		}
		if len(rows) == 0 {
			continue
		}
		grouped = append(grouped, mcpBuiltinToolGroupView{Group: g, Rows: rows})
	}
	for _, t := range tools {
		if !covered[t.ToolName] {
			other = append(other, t)
		}
	}
	return grouped, other
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
// rely on the "leave blank to keep" contract on submit. required carries the
// server's declared required config for this kind (see mcpKVField): matching
// rows are marked Required, and their registry description/value template is
// attached as Hint/Placeholder. missingKeys marks rows whose required value is
// still blank.
func mcpKVFieldsWithSecrets(m map[string]string, secretKeys []string, required []MCPRequiredConfigKey, missingKeys []string) []mcpKVField {
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
	byName := make(map[string]MCPRequiredConfigKey, len(required))
	for _, req := range required {
		if name := strings.TrimSpace(req.Name); name != "" {
			byName[name] = req
		}
	}
	for i := range out {
		out[i].Missing = slices.Contains(missingKeys, out[i].Key)
		req, ok := byName[out[i].Key]
		if !ok {
			continue
		}
		out[i].Required = true
		out[i].Hint = req.Description
		out[i].Placeholder = req.ValueHint
		if mcpIsAuthHeader(req) {
			out[i].Auth = true
			out[i].AuthScheme = mcpAuthSchemeFromHint(req.ValueHint)
			out[i].Secret = true
		}
	}
	return out
}

// mcpIsAuthHeader reports whether a declared required key is an Authorization
// header, which the form renders as a scheme + token input rather than a
// free-form header value.
func mcpIsAuthHeader(req MCPRequiredConfigKey) bool {
	return req.Kind == "header" && strings.EqualFold(strings.TrimSpace(req.Name), "Authorization")
}

// mcpAuthSchemeFromHint derives the auth scheme to preselect from the registry
// value template (e.g. "Bearer {TOKEN}" → "Bearer"). Templates that are just a
// placeholder (no scheme word, e.g. "{TOKEN}") default to Bearer, the convention
// for the Authorization header.
func mcpAuthSchemeFromHint(hint string) string {
	hint = strings.TrimSpace(hint)
	if hint == "" {
		return "Bearer"
	}
	scheme, _, ok := strings.Cut(hint, " ")
	scheme = strings.TrimSpace(scheme)
	if !ok || scheme == "" || strings.ContainsAny(scheme, "{}") {
		return "Bearer"
	}
	return scheme
}

// mcpAuthSchemeOption is one selectable Authorization scheme in the token row.
type mcpAuthSchemeOption struct {
	Value string
	Label string
}

// mcpAuthSchemeOptions returns the scheme choices for an Authorization row,
// with the currently-selected scheme first/selected. A custom scheme already on
// the row is preserved so editing never silently rewrites it.
func mcpAuthSchemeOptions(current string) []mcpAuthSchemeOption {
	base := []mcpAuthSchemeOption{
		{Value: "Bearer", Label: "Bearer"},
		{Value: "Token", Label: "Token"},
		{Value: "Basic", Label: "Basic"},
		{Value: "", Label: "Raw (no prefix)"},
	}
	current = strings.TrimSpace(current)
	for _, opt := range base {
		if opt.Value == current {
			return base
		}
	}
	if current == "" {
		return base
	}
	return append([]mcpAuthSchemeOption{{Value: current, Label: current}}, base...)
}

// mcpAuthTokenPlaceholder is the token field's placeholder: the registry value
// template when declared, otherwise a generic prompt.
func mcpAuthTokenPlaceholder(r mcpKVField) string {
	if p := strings.TrimSpace(r.Placeholder); p != "" {
		return p
	}
	return "Enter token"
}

// mcpAuthHeaderValue assembles the Authorization header value from a selected
// scheme and a user-entered token. An empty token yields an empty value (memory
// keeps the stored secret on update). A scheme of "" (raw) returns the token
// unchanged; otherwise the token is prefixed with "<scheme> " unless the user
// already pasted a value carrying that prefix.
func mcpAuthHeaderValue(scheme, token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	scheme = strings.TrimSpace(scheme)
	if scheme == "" {
		return token
	}
	if len(token) >= len(scheme)+1 && strings.EqualFold(token[:len(scheme)+1], scheme+" ") {
		return token
	}
	return scheme + " " + token
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
	auths := c.Request().Form[prefix+".auth"]
	schemes := c.Request().Form[prefix+".scheme"]
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
		auth := i < len(auths) && strings.TrimSpace(auths[i]) != ""
		scheme := ""
		if i < len(schemes) {
			scheme = strings.TrimSpace(schemes[i])
		}
		if auth {
			// The form posts the bare token plus a scheme; assemble the header
			// value memory stores.
			v = mcpAuthHeaderValue(scheme, v)
			secret = true
		}
		out = append(out, mcpKVField{Key: k, Value: v, Secret: secret, Auth: auth, AuthScheme: scheme})
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
	return mcpServerFormDataFromServerConfigure(s, false)
}

// mcpServerFormDataFromServerConfigure is mcpServerFormDataFromServer with the
// ?configure=1 flag applied (the post-install redirect path). Required rows are
// marked from the server's declared required config (headers for sse/http,
// env for stdio); Missing rows from s.MissingConfigKeys.
func mcpServerFormDataFromServerConfigure(s MCPServer, configure bool) mcpServerFormData {
	headerRequired := mcpRequiredConfigForKind(s.RequiredConfig, "header")
	envRequired := mcpRequiredConfigForKind(s.RequiredConfig, "env")
	d := mcpServerFormData{
		ID:                    s.ID,
		Name:                  s.Name,
		Type:                  s.Type,
		URL:                   s.URL,
		Command:               s.Command,
		Args:                  s.Args,
		Headers:               mcpKVFieldsWithSecrets(s.Headers, s.SecretHeadersKeys, headerRequired, s.MissingConfigKeys),
		Env:                   mcpKVFieldsWithSecrets(s.Env, s.SecretEnvKeys, envRequired, s.MissingConfigKeys),
		Enabled:               s.Enabled,
		RequiredConfigKeys:    mcpRequiredConfigKeysForTransport(s.RequiredConfig, s.Type),
		MissingConfigKeys:     s.MissingConfigKeys,
		ConfigureMode:         configure,
		RequiredConfig:        s.RequiredConfig,
		RegistryRepositoryURL: s.RegistryRepositoryURL,
	}
	if !mcpServerValidTransport(d.Type) {
		d.Type = "stdio"
	}
	return d
}

// mcpMissingConfigDetail pairs a still-missing required key with the registry's
// description so the configure banner can explain each missing key.
type mcpMissingConfigDetail struct {
	Name        string
	Description string
}

// mcpMissingConfigDetails resolves each missing key name to its declared
// description (empty when the key or its guidance is unknown).
func mcpMissingConfigDetails(missing []string, required []MCPRequiredConfigKey) []mcpMissingConfigDetail {
	desc := make(map[string]string, len(required))
	for _, k := range required {
		if name := strings.TrimSpace(k.Name); name != "" {
			desc[name] = k.Description
		}
	}
	out := make([]mcpMissingConfigDetail, 0, len(missing))
	for _, name := range missing {
		out = append(out, mcpMissingConfigDetail{Name: name, Description: desc[name]})
	}
	return out
}

// mcpCredentialDocs derives a "where to get this credential" label and URL from
// a registry repository URL. Known hosts get a direct link to their token
// settings; anything else falls back to the repository. Returns empty strings
// when there is no repository URL (manual servers).
func mcpCredentialDocs(repoURL string) (label, href string) {
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return "", ""
	}
	if u, err := url.Parse(repoURL); err == nil && strings.EqualFold(u.Host, "github.com") {
		return "Create a GitHub token", "https://github.com/settings/tokens"
	}
	return "View provider docs", repoURL
}

// mcpRequiredConfigForKind returns the required config entries of the given
// kind ("header" or "env"), preserving their registry descriptions and value
// templates for the form rows.
func mcpRequiredConfigForKind(required []MCPRequiredConfigKey, kind string) []MCPRequiredConfigKey {
	var out []MCPRequiredConfigKey
	for _, k := range required {
		if k.Kind == kind && strings.TrimSpace(k.Name) != "" {
			out = append(out, k)
		}
	}
	return out
}

// mcpRequiredConfigKeysForKind returns the names of required config keys of the
// given kind ("header" or "env").
func mcpRequiredConfigKeysForKind(required []MCPRequiredConfigKey, kind string) []string {
	var out []string
	for _, k := range required {
		if k.Kind != kind {
			continue
		}
		if name := strings.TrimSpace(k.Name); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// mcpRequiredConfigKeysForTransport returns the names of required config keys
// for the given transport (sse/http → "header"; anything else → "env").
func mcpRequiredConfigKeysForTransport(required []MCPRequiredConfigKey, transport string) []string {
	kind := "env"
	if transport == "sse" || transport == "http" {
		kind = "header"
	}
	return mcpRequiredConfigKeysForKind(required, kind)
}

// mcpMissingKeysList joins missing required-config key names into a
// ", "-separated string for the list alert and the edit-page configure banner.
func mcpMissingKeysList(keys []string) string {
	return strings.Join(keys, ", ")
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
	if err := mcpServerToolsContainer(s, tools, nil).Render(context.Background(), &b); err != nil {
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
