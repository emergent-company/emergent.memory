package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"

	ui "github.com/emergent-company/go-daisy/components/ui"
	"github.com/labstack/echo/v4"
)

// --- data-source UI ---

// dsConfigField is one config-schema field rendered by the connect / edit
// forms. It is derived from the provider's declared schema so the form follows
// the server's contract rather than a hard-coded field list.
type dsConfigField struct {
	Key         string
	Label       string
	Description string
	Type        string // "string", "array", "boolean", "integer"
	Required    bool
	Widget      string // "resource" | "ref" | "" (plain text) from x-ui-widget
	// LoadLabel is the provider's own copy for the widget's load action
	// ("Load tags"), from x-ui-load-label. Empty means the gateway's default for
	// the widget, so a provider that does not describe the action keeps working.
	LoadLabel string
	Value     string // prefilled text value (string/array); blank for the connect form
	Checked   bool   // prefilled checkbox state
	// SourceID is the saved source this field belongs to, so its suggestions can
	// be listed through the source's stored credentials instead of asking the
	// browser to re-submit them (the edit form never carries credential inputs).
	// Empty on the connect form, where the credentials are still unsaved.
	SourceID string
}

// dsConfigFieldOrder fixes the render order of the known config keys; any
// schema property outside this list is appended afterwards, sorted, so a
// forward-compatible schema addition still surfaces in the form.
var dsConfigFieldOrder = []string{
	// The web provider's crawl-scope keys come first so its connect form reads
	// in crawl-scope order (start URLs, then filters, then limits).
	"start_urls", "include_globs", "exclude_globs", "max_pages", "max_depth",
	"folders", "mime_types", "recursive",
	"repo", "ref", "paths", "extensions",
	"client_id", "client_secret", "refresh_token",
}

// dsArrayFieldPlaceholder returns the entry hint shown in a multi-value field's
// textarea. Providers differ — the web provider takes start URLs and path globs,
// the drive provider takes MIME types — so the hint is derived from the field
// key and an unknown array property falls back to neutral wording. It used to
// be the hardcoded "One MIME type per line" on every array field, which was
// simply wrong outside the drive form.
func dsArrayFieldPlaceholder(key string) string {
	switch {
	case strings.Contains(key, "mime"):
		return "One MIME type per line"
	case strings.Contains(key, "url"):
		return "One URL per line"
	case strings.Contains(key, "glob"):
		return "One path glob per line (e.g. /docs/**)"
	case strings.Contains(key, "path"):
		return "One path per line"
	}
	return "One value per line"
}

// dsConfigFields splits a provider schema into config fields and credential
// fields (the write-only OAuth2 material), in a stable order. cfg carries a
// source's non-secret config values (nil for the connect form); non-credential
// fields are prefilled from it so an edit round-trips the persisted settings
// without exposing credential material.
func dsConfigFields(schema *DSProviderSchema, cfg map[string]any) (configFields, credentialFields []dsConfigField) {
	if schema == nil || len(schema.Properties) == 0 {
		return nil, nil
	}
	required := make(map[string]bool, len(schema.Required))
	for _, r := range schema.Required {
		required[r] = true
	}
	seen := make(map[string]bool, len(schema.Properties))
	appendField := func(key string) {
		prop, ok := schema.Properties[key].(map[string]any)
		if !ok {
			return
		}
		f := dsConfigField{Key: key, Label: dsFieldLabel(key), Type: "string", Required: required[key]}
		if t, ok := prop["type"].(string); ok && t != "" {
			f.Type = t
		}
		if label, ok := prop["label"].(string); ok && label != "" {
			f.Label = label
		}
		if d, ok := prop["description"].(string); ok {
			f.Description = d
		}
		if w, ok := prop["x-ui-widget"].(string); ok {
			f.Widget = w
		}
		if l, ok := prop["x-ui-load-label"].(string); ok {
			f.LoadLabel = strings.TrimSpace(l)
		}
		// The server merges credential fields into the schema marked
		// writeOnly; the shared key list is the fallback for older schemas.
		writeOnly, _ := prop["writeOnly"].(bool)
		if writeOnly || dsIsCredentialKey(key) {
			credentialFields = append(credentialFields, f)
		} else {
			if cfg != nil {
				switch f.Type {
				case "boolean":
					if b, ok := cfg[key].(bool); ok {
						f.Checked = b
					}
				case "array":
					f.Value = dsConfigArrayText(cfg, key)
				case "integer":
					f.Value = dsConfigIntText(cfg, key)
				default:
					f.Value = dsConfigString(cfg, key)
				}
			}
			configFields = append(configFields, f)
		}
	}
	for _, key := range dsConfigFieldOrder {
		if _, ok := schema.Properties[key]; !ok {
			continue
		}
		seen[key] = true
		appendField(key)
	}
	var rest []string
	for key := range schema.Properties {
		if !seen[key] {
			rest = append(rest, key)
		}
	}
	slices.Sort(rest)
	for _, key := range rest {
		appendField(key)
	}
	return configFields, credentialFields
}

// dsConfigArrayText renders an array config value as newline-joined text for a
// textarea prefill.
func dsConfigArrayText(cfg map[string]any, key string) string {
	switch v := cfg[key].(type) {
	case []string:
		return strings.Join(v, "\n")
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

// dsIsCredentialKey reports whether a config key carries write-only credential
// material (never rendered back to the browser).
func dsIsCredentialKey(key string) bool {
	return slices.Contains(dsCredentialKeys, key)
}

// dsFieldLabel humanizes a config key into a form label.
func dsFieldLabel(key string) string {
	switch key {
	case "folders":
		return "Folders"
	case "folder_id":
		return "Folder ID"
	case "shared_drive_id":
		return "Shared drive ID"
	case "mime_types":
		return "MIME types"
	case "recursive":
		return "Recursive"
	case "client_id":
		return "OAuth2 client ID"
	case "client_secret":
		return "OAuth2 client secret"
	case "refresh_token":
		return "OAuth2 refresh token"
	case "access_token":
		return "Personal access token"
	case "repo":
		return "Repository"
	case "ref":
		return "Ref"
	case "paths":
		return "Paths"
	case "extensions":
		return "Extensions"
	default:
		words := strings.Split(key, "_")
		for i, w := range words {
			if w == "" {
				continue
			}
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
		return strings.Join(words, " ")
	}
}

// dsConfigFromFields builds a config map from the submitted form using the
// provider schema's fields. Credential fields are included (the server strips
// and encrypts them); blank strings are omitted so an update keeps stored
// credentials, arrays accept newlines or commas, and booleans are present only
// when checked.
func dsConfigFromFields(c echo.Context, fields []dsConfigField) map[string]any {
	cfg := map[string]any{}
	for _, f := range fields {
		switch f.Type {
		case "boolean":
			if c.FormValue(f.Key) != "" {
				cfg[f.Key] = true
			}
		case "array":
			if values := dsConfigList(c.FormValue(f.Key)); len(values) > 0 {
				cfg[f.Key] = values
			}
		case "integer":
			// A numeric field must reach the server as a number. A value that does
			// not parse is forwarded as the raw string so the provider's own
			// validation rejects it, instead of being dropped and silently replaced
			// by the default.
			if v := strings.TrimSpace(c.FormValue(f.Key)); v != "" {
				if n, err := strconv.Atoi(v); err == nil {
					cfg[f.Key] = n
				} else {
					cfg[f.Key] = v
				}
			}
		default:
			if v := strings.TrimSpace(c.FormValue(f.Key)); v != "" {
				cfg[f.Key] = v
			}
		}
	}
	return cfg
}

// dsConfigList splits a textarea/input into list items on newlines or commas.
func dsConfigList(raw string) []string {
	var items []string
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' }) {
		if s := strings.TrimSpace(part); s != "" {
			items = append(items, s)
		}
	}
	return items
}

// dsProviderFields fetches a provider's schema and splits it into the config and
// write-only credential form fields. The schema is the form's single source of
// truth, so create/test/list all read the same field set.
func (s *Server) dsProviderFields(ctx context.Context, providerType string) (configFields, credentialFields []dsConfigField, err error) {
	schema, err := s.memory.GetDSProviderSchema(ctx, providerType)
	if err != nil {
		return nil, nil, userFacingError(err)
	}
	configFields, credentialFields = dsConfigFields(schema, nil)
	return configFields, credentialFields, nil
}

// dsFormConfig extracts the submitted config for a provider: every field the
// schema declares, config and credential alike.
func (s *Server) dsFormConfig(ctx context.Context, c echo.Context, providerType string) (map[string]any, error) {
	configFields, credentialFields, err := s.dsProviderFields(ctx, providerType)
	if err != nil {
		return nil, err
	}
	fields := make([]dsConfigField, 0, len(configFields)+len(credentialFields))
	fields = append(fields, configFields...)
	fields = append(fields, credentialFields...)
	return dsConfigFromFields(c, fields), nil
}

// --- picker field helpers ---

// dsIsPicker reports whether the field offers provider-listing suggestions.
func (f dsConfigField) dsIsPicker() bool {
	return f.Widget == "resource" || f.Widget == "ref"
}

// dsIsFolderPicker reports whether the field is Drive's folder-target list,
// which renders in its own scope card (with the Google Picker) rather than as a
// raw form input.
func (f dsConfigField) dsIsFolderPicker() bool { return f.Widget == "folders" }

// dsSettingsConfigFields drops the fields rendered outside the settings form so
// the form never offers a raw folder id/list input. The server preserves the
// stored value across a settings save.
func dsSettingsConfigFields(fields []dsConfigField) []dsConfigField {
	out := make([]dsConfigField, 0, len(fields))
	for _, f := range fields {
		if f.dsIsFolderPicker() {
			continue
		}
		out = append(out, f)
	}
	return out
}

// dsSyncOptionsNote is the settings card's explanatory line. It names only the
// concerns the provider actually has: the folder note is Drive-specific, and the
// credential note only applies when the provider declares write-only credential
// fields (an OAuth-only, self-registering provider declares none).
func dsSyncOptionsNote(configFields, credentialFields []dsConfigField) string {
	var parts []string
	for _, f := range configFields {
		if f.dsIsFolderPicker() {
			parts = append(parts, "Filter types and recursion apply to every selected folder.")
			break
		}
	}
	if len(credentialFields) > 0 {
		parts = append(parts, "Credential fields are write-only and never shown.")
	}
	return strings.Join(parts, " ")
}

// dsOptionsListID is the datalist id the field's suggestions are written to.
func (f dsConfigField) dsOptionsListID() string { return "ds-options-" + f.Key }

// dsPickerID is the swap target for the field's load button.
func (f dsConfigField) dsPickerID() string { return "ds-picker-" + f.Key }

// dsLoadPath is the gateway endpoint that lists the field's suggestions. A saved
// source lists through its own stored credentials (the edit form carries none),
// except for the ref widget, whose listing depends on the repository currently
// typed into the form and must therefore use the unsaved-config endpoint.
func (f dsConfigField) dsLoadPath() string {
	if f.Widget == "resource" && f.SourceID != "" {
		return "/data-sources/" + url.PathEscape(f.SourceID) + "/field-options"
	}
	if f.Widget == "ref" {
		return "/data-sources/refs"
	}
	return "/data-sources/repos"
}

// dsLoadLabel is the load button's copy: the provider's own word when the schema
// declares one, else the widget default.
func (f dsConfigField) dsLoadLabel() string {
	if f.LoadLabel != "" {
		return f.LoadLabel
	}
	if f.Widget == "ref" {
		return "Load branches and tags"
	}
	if f.Key == "repo" {
		return "Load repositories"
	}
	return "Load suggestions"
}

// dsLoadTestID is the load button's stable test id. The repo/ref widgets keep
// their historical ids; every other field gets one derived from its key, so a
// provider-named widget has a stable handle.
func (f dsConfigField) dsLoadTestID() string {
	switch {
	case f.Widget == "ref":
		return "ds-load-refs"
	case f.Key == "repo":
		return "ds-load-repos"
	default:
		return "ds-load-" + f.Key
	}
}

// dsOptionLabel renders a suggestion's option text: refs carry their kind so a
// branch and a tag with the same name stay distinguishable.
func dsOptionLabel(r DSResource) string {
	switch r.Type {
	case "branch", "tag":
		return r.Name + " (" + r.Type + ")"
	default:
		return r.Name
	}
}

// dsOptionsStatus is the line under a suggestion picker.
func dsOptionsStatus(count int, truncated bool) string {
	switch {
	case count == 0:
		return "No suggestions found. You can still type a value."
	case truncated:
		return "Showing the first " + strconv.Itoa(count) + " suggestions; type any value."
	default:
		return "Showing " + strconv.Itoa(count) + " suggestions."
	}
}

// dsIntervalFromForm parses a positive sync interval; ok is false when unset or
// invalid, so the caller can omit the field (leaving any stored value intact).
func dsIntervalFromForm(c echo.Context) (*int, bool) {
	raw := strings.TrimSpace(c.FormValue("sync_interval_minutes"))
	if raw == "" {
		return nil, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return nil, false
	}
	return &n, true
}

// dsIntervalForMode returns the submitted interval only for a scheduled source.
// A manual source owns no interval, so forwarding one would be dropped by the
// server and read as a silent no-op in the form.
func dsIntervalForMode(c echo.Context, mode string) *int {
	if mode != "scheduled" {
		return nil
	}
	if interval, ok := dsIntervalFromForm(c); ok {
		return interval
	}
	return nil
}

// dsProviderNameMap indexes provider display names by provider type.
func dsProviderNameMap(providers []DSProvider) map[string]string {
	names := make(map[string]string, len(providers))
	for _, p := range providers {
		names[p.Type] = p.Name
	}
	return names
}

// dsProviderLabel resolves a provider type to its display name, falling back to
// the raw type.
func dsProviderLabel(names map[string]string, providerType string) string {
	if n := names[providerType]; n != "" {
		return n
	}
	return providerType
}

// dsSourceName is the display name of a source, falling back to a shortened id.
func dsSourceName(src DSIntegration) string {
	if src.Name != "" {
		return src.Name
	}
	return "Data source " + shortID(src.ID)
}

// dsStatusLabel renders the source lifecycle status as grounded copy.
func dsStatusLabel(status string) string {
	switch strings.ToLower(status) {
	case "active":
		return "Active"
	case "needs_reconnect":
		return "Needs reconnect"
	case "error":
		return "Error"
	case "paused":
		return "Paused"
	case "":
		return ""
	default:
		return status
	}
}

// dsStatusIntent maps a source status to a badge colour.
func dsStatusIntent(status string) ui.BadgeIntent {
	switch strings.ToLower(status) {
	case "active":
		return ui.BadgeSuccess
	case "needs_reconnect":
		return ui.BadgeError
	case "error":
		return ui.BadgeError
	case "paused":
		return ui.BadgeWarning
	default:
		return ui.BadgeNeutral
	}
}

// dsSyncStatusIntent maps a sync-job status to a badge colour.
func dsSyncStatusIntent(status string) ui.BadgeIntent {
	switch strings.ToLower(status) {
	case "completed":
		return ui.BadgeSuccess
	case "failed":
		return ui.BadgeError
	case "processing":
		return ui.BadgeInfo
	case "pending":
		return ui.BadgeWarning
	default: // cancelled and unknown
		return ui.BadgeNeutral
	}
}

// dsJobActive reports whether a sync job is in an active (cancellable) state.
func dsJobActive(job DSSyncJob) bool {
	switch strings.ToLower(job.Status) {
	case "pending", "processing":
		return true
	default:
		return false
	}
}

// dsJobPercent is the job's completion percentage (0 when the total is unknown).
func dsJobPercent(job DSSyncJob) int {
	if job.TotalItems <= 0 {
		return 0
	}
	return min(max(job.ProcessedItems*100/job.TotalItems, 0), 100)
}

// dsLastSyncLabel renders the last-synced line for a source list row.
func dsLastSyncLabel(src DSIntegration) string {
	if src.LastSyncedAt == nil || *src.LastSyncedAt == "" {
		return "Never synced"
	}
	return "Last synced " + relTime(*src.LastSyncedAt)
}

// relTimeOrDash renders a relative time, or an em-dash when the timestamp is
// empty (e.g. a version the server did not stamp).
func relTimeOrDash(iso string) string {
	if iso == "" {
		return "—"
	}
	return relTime(iso)
}

// dsTestMessage renders a test result's message, falling back to a generic
// success/failure line when the server sent none.
func dsTestMessage(resp *DSTestConnectionResponse) string {
	if resp != nil && resp.Message != "" {
		return resp.Message
	}
	if resp != nil && resp.Success {
		return "Connection successful."
	}
	return "Connection failed."
}

// --- UI handlers ---

// uiDataSources renders the Data Sources list. The memory OAuth callback lands
// on /data-sources?connected=..., ?reconnected=... or ?oauth_error=... (see the
// server's OAuthLandingURL), so those requests are delegated to the callback
// handler.
func (s *Server) uiDataSources(c echo.Context) error {
	if c.QueryParam("connected") != "" || c.QueryParam("reconnected") != "" || c.QueryParam("oauth_error") != "" {
		return s.uiDataSourceOAuthCallback(c)
	}
	ctx := c.Request().Context()
	items, err := s.memory.ListDSIntegrations(ctx)
	if err != nil {
		err = userFacingError(err)
	}
	names := dsProviderNameMap(nil)
	if providers, perr := s.memory.ListDSProviders(ctx); perr == nil {
		names = dsProviderNameMap(providers)
	} else {
		captureError(perr)
	}
	flashMsg := flashFromQuery(c, []flashParam{
		{key: "created", msg: "Data source connected."},
		{key: "deleted", msg: "Data source deleted."},
	})
	flashErr := flashError(c)
	return s.page(c, pageTitle("Data Sources"), DataSourcesPage(items, names, err, flashMsg, flashErr))
}

// uiDataSourceNew renders the provider chooser, or the connect surface for a
// chosen provider. The catalog drives the page: ?providerType selects a provider
// (absent → chooser), and ?method narrows a dual-capability provider (GitHub
// advertises both oauth and credentials) to one connect path.
func (s *Server) uiDataSourceNew(c echo.Context) error {
	ctx := c.Request().Context()
	providerType := strings.TrimSpace(c.QueryParam("providerType"))
	method := strings.TrimSpace(c.QueryParam("method"))
	providers, err := s.memory.ListDSProviders(ctx)
	if err != nil {
		err = userFacingError(err)
	}
	selected := dsSelectProvider(providers, providerType, method)

	var configFields, credentialFields []dsConfigField
	var schemaErr error
	picker := dsSavedCredentialPicker{}
	if selected != nil {
		schema, serr := s.memory.GetDSProviderSchema(ctx, selected.Type)
		if serr != nil {
			schemaErr = userFacingError(serr)
		}
		configFields, credentialFields = dsConfigFields(schema, nil)
		// ?credentialId (server-provided link) and ?savedCredential (the
		// post-create redirect from the shared modal) both preselect a
		// credential on the connect form.
		selectedCred := cmp.Or(strings.TrimSpace(c.QueryParam("credentialId")), strings.TrimSpace(c.QueryParam("savedCredential")))
		picker = s.dsSavedCredentialPickerFor(ctx, providers, selected.Type, selectedCred, dsConnectFormPath(selected.Type))
	}
	if err == nil {
		err = schemaErr
	}
	flashErr := flashError(c)
	return s.page(c, pageTitle("New Data Source"), DataSourceNewPage(providers, selected, configFields, credentialFields, picker, err, flashErr, nil))
}

// dsSelectProvider picks the selected provider from the catalog by type, applying
// the optional ?method= hint: when the named method is one the provider
// advertises, the returned copy is narrowed to it so the page renders exactly
// that connect path. A dual-capability provider without a method hint keeps its
// full capability set (its OAuth surface is shown). A config-only provider
// (e.g. web) narrows to "config" so the connect form renders; the catalog slice
// entry is copied, never mutated.
func dsSelectProvider(providers []DSProvider, providerType, method string) *DSProvider {
	if providerType == "" {
		return nil
	}
	for i := range providers {
		if providers[i].Type != providerType {
			continue
		}
		narrowed := providers[i]
		switch {
		case method == "oauth" && narrowed.DSProviderSupportsOAuth():
			narrowed.AuthMethods = []string{"oauth"}
		case method == "credentials" && narrowed.DSProviderSupportsCredentials():
			narrowed.AuthMethods = []string{"credentials"}
		case method == "config" && narrowed.DSProviderSupportsConfigOnly():
			narrowed.AuthMethods = []string{"config"}
		}
		return &narrowed
	}
	return nil
}

// dsOAuthClientError is the inline, actionable error surfaced on the
// source-creation page when an OAuth connect fails because no OAuth client is
// configured for the provider. SettingsURL carries the OAuth client settings
// link for the scope the caller can administer (project or org), empty when no
// scope is known.
type dsOAuthClientError struct {
	ProviderName string
	ProviderType string
	SettingsURL  string
}

// isMissingDSOAuthClient reports whether err is the server's "no OAuth client
// configured" validation for a data-source OAuth start. The gateway has no
// machine-readable code for this specific condition, so it matches the
// validation error's message prefix (kept stable with the server-side copy in
// service_oauth_client.go).
func isMissingDSOAuthClient(err error) bool {
	var he *memoryHTTPError
	if !errors.As(err, &he) {
		return false
	}
	return he.Code == "validation_error" && strings.HasPrefix(he.Message, "no OAuth client configured for provider ")
}

// uiDataSourceOAuthStart begins the provider OAuth flow: it asks memory for the
// provider's authorization URL and redirects the browser to it. A missing
// OAuth client renders the source-creation page with an inline, actionable
// error (rather than a toast) so the user sees the remediation path; every
// other failure redirects back with the sanitized backend message, never the
// internal "memory <status> <code>:" prefix.
func (s *Server) uiDataSourceOAuthStart(c echo.Context) error {
	ctx := c.Request().Context()
	providerType := strings.TrimSpace(c.QueryParam("providerType"))
	var config map[string]any
	if c.Request().Method == http.MethodPost {
		providerType = strings.TrimSpace(c.FormValue("providerType"))
		if providerType == "gmail" {
			config = dsGmailConfigFromForm(c)
			if _, err := s.memory.PreviewDSFilter(c.Request().Context(), providerType, config); err != nil {
				return redirectWithError(c, "/data-sources/new?providerType=gmail", dsFormError(err))
			}
		} else {
			cfg, err := s.dsFormConfig(c.Request().Context(), c, providerType)
			if err != nil {
				return redirectWithError(c, "/data-sources/new", err)
			}
			config = cfg
		}
	}
	if providerType == "" {
		return redirectWithError(c, "/data-sources/new", errors.New("providerType is required"))
	}
	oauthAppID := strings.TrimSpace(c.QueryParam("oauthAppId"))
	if oauthAppID == "" {
		project, _ := s.memory.GetCurrentProject(ctx)
		if project != nil {
			if apps, aerr := s.memory.ListProjectDSOAuthApps(ctx, project.ID); aerr == nil {
				providers, _ := s.memory.ListDSProviders(ctx)
				vendor := dsProviderVendor(providers, providerType)
				matches := make([]DSOAuthApp, 0)
				for _, app := range apps {
					if app.Vendor == vendor {
						matches = append(matches, app)
					}
				}
				switch len(matches) {
				case 1:
					oauthAppID = matches[0].ID
				default:
					if len(matches) > 1 {
						return s.page(c, pageTitle("Choose an OAuth app"), OAuthAppChoicePage(providerType, matches))
					}
				}
			}
		}
	}
	resp, err := s.memory.StartDSOAuth(ctx, providerType, config, nil, oauthAppID, "")
	if err != nil {
		if isMissingDSOAuthClient(err) {
			return s.renderDSOAuthClientMissing(c, providerType)
		}
		msg := memoryHTTPErrorMessage(err)
		if msg == "" {
			msg = genericErrorMessage
		}
		return redirectWithError(c, "/data-sources/new", errors.New(msg))
	}
	// The authorize URL is cross-origin (the provider). A boosted/htmx caller
	// cannot follow a cross-origin redirect (htmx fetches with mode
	// "same-origin") — see uiMCPOAuthStart — so answer htmx with HX-Redirect
	// (top-level navigation) and plain browsers with a 303.
	redirectAfterMutation(c, resp.AuthorizeURL)
	return nil
}

// uiDataSourceReconnect re-runs OAuth consent for an existing source. The
// provider's consent screen is the only way to repair a revoked or expired
// grant, and the callback replaces that source's stored credential in place, so
// the source keeps its name, sync scope and history instead of being duplicated
// (#2107).
func (s *Server) uiDataSourceReconnect(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	src, err := s.memory.GetDSIntegration(ctx, id)
	if err != nil {
		return redirectWithError(c, s.dsScoped("/data-sources", c), userFacingError(err))
	}
	resp, err := s.memory.StartDSOAuth(ctx, src.ProviderType, nil, nil, "", src.ID)
	if err != nil {
		// A reconnect is an action on an existing source, so every failure lands
		// there. The creation page renderDSOAuthClientMissing builds is for the
		// new-source flow and would drop the user out of the source they were
		// repairing (#2186). dsFormError is the same gate every other data-source
		// form uses: the backend's own message for a 4xx, a redacted one for a 5xx
		// or a transport failure.
		return redirectWithError(c, s.dsScoped("/data-sources/"+url.PathEscape(src.ID), c), dsFormError(err))
	}
	redirectAfterMutation(c, resp.AuthorizeURL)
	return nil
}

// dsProviderCanOAuth reports whether the catalog lists the provider with the
// OAuth connect method — the only providers whose credentials can be re-minted
// by a reconnect, so the detail page offers the action only for those.
func dsProviderCanOAuth(providers []DSProvider, providerType string) bool {
	for _, p := range providers {
		if p.Type == providerType {
			return p.DSProviderSupportsOAuth()
		}
	}
	return false
}

// renderDSOAuthClientMissing renders the source-creation page with an inline
// error naming the provider (from the catalog) and linking to the OAuth client
// settings the caller can administer: the project-scoped page when they
// administer the active project, else the org-wide page. The provider list and
// display name come from the catalog, never a hard-coded provider.
func (s *Server) renderDSOAuthClientMissing(c echo.Context, providerType string) error {
	ctx := c.Request().Context()
	providers, err := s.memory.ListDSProviders(ctx)
	if err != nil {
		err = userFacingError(err)
	}
	oauthErr := &dsOAuthClientError{
		ProviderName: dsProviderLabel(dsProviderNameMap(providers), providerType),
		ProviderType: providerType,
	}
	if sc, ok := sessionContextFrom(ctx); ok {
		switch {
		case sc.ProjectID != "" && s.strictProjectAdminForCaller(c, sc.ProjectID):
			oauthErr.SettingsURL = dsCredentialAppHref(providerType)
		case sc.OrgID != "":
			oauthErr.SettingsURL = dsOrgOAuthClientHref(sc.OrgID, providerType)
		}
	}
	return s.page(c, pageTitle("New Data Source"), DataSourceNewPage(providers, nil, nil, nil, dsSavedCredentialPicker{}, err, nil, oauthErr))
}

// dsOAuthErrorMessage renders a callback outcome as an actionable message.
// connect_failed covers every server-side failure after the provider redirect
// (a wrong client secret, an expired or replayed state, a failed exchange), so
// it names the one thing the user can check; any other value is the provider's
// own error code, passed through as-is.
func dsOAuthErrorMessage(code string) string {
	if code == "connect_failed" {
		return "Authorization could not be completed. Check this project's OAuth client configuration and try again."
	}
	return "Authorization failed: " + code
}

// dsScoped returns path prefixed with the caller's active project scope when a
// web session with a project is active, or path unchanged otherwise. It backs
// the OAuth connect flow, whose post-callback redirects arrive unscoped (the
// API server has no scope prefix) but must still land inside the active
// project. scopeLocation never scopes these targets: strictScopedPath exempts
// the /data-sources prefix from the hard cut, so needsRedirectScope never fires
// and the session fallback skips them — which is why the target is named here.
// When the request is already scoped the result starts with /o/ and
// scopeLocation leaves it untouched (no double prefix).
func (s *Server) dsScoped(path string, c echo.Context) string {
	if sc, ok := sessionContextFrom(c.Request().Context()); ok && !sc.Device && sc.ProjectID != "" {
		if base := projectScopeURL(sc.OrgID, sc.ProjectID); base != "" {
			return base + path
		}
	}
	return path
}

// uiDataSourceOAuthCallback is the memory OAuth callback's landing handler: it
// surfaces an authorization failure, redirects to the freshly connected source,
// or falls back to the list. Registered both on /data-sources/oauth/callback and
// as the delegate for /data-sources?connected=... (the server's OAuthLandingURL).
func (s *Server) uiDataSourceOAuthCallback(c echo.Context) error {
	if oauthErr := c.QueryParam("oauth_error"); oauthErr != "" {
		return redirectWithError(c, s.dsScoped("/data-sources", c), errors.New(dsOAuthErrorMessage(oauthErr)))
	}
	if connected := c.QueryParam("connected"); connected != "" {
		redirectAfterMutation(c, s.dsScoped("/data-sources/"+url.PathEscape(connected)+"?created=1", c))
		return nil
	}
	// A reconnect repairs an existing source, so it lands on that source saying
	// what happened instead of claiming a new source was connected (#2186).
	if reconnected := c.QueryParam("reconnected"); reconnected != "" {
		redirectAfterMutation(c, s.dsScoped("/data-sources/"+url.PathEscape(reconnected)+"?reconnected=1", c))
		return nil
	}
	return c.Redirect(http.StatusSeeOther, s.dsScoped("/data-sources", c))
}

// uiDataSourceResources renders the resource-selection step for a source. The
// listing itself (which can take many seconds on a large Google Drive) is
// deferred: the page renders immediately with a spinner, and the browser fetches
// /data-sources/:id/resources/list after first paint. Only ?static=1 (the
// no-JavaScript fallback) lists synchronously and server-renders the list
// inline.
func (s *Server) uiDataSourceResources(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	src, err := s.memory.GetDSIntegration(ctx, id)
	if err != nil {
		return s.page(c, pageTitle("Data Source"), DataSourceResourceSelectPage(nil, "", "", dsFormError(err), "", nil))
	}
	// Google Drive selects its targets in the browser's Google Picker modal on
	// the detail page, so the old full-page listing is retired for it. Keep the
	// URL working (bookmarks, older links) by bouncing to the detail page with
	// the picker open.
	if src.ProviderType == dsProviderTypeGoogleDrive {
		redirectAfterMutation(c, dsPickerURL(id))
		return nil
	}
	providerName := dsProviderLabel(dsProviderNameMap(nil), src.ProviderType)
	if providers, perr := s.memory.ListDSProviders(ctx); perr == nil {
		providerName = dsProviderLabel(dsProviderNameMap(providers), src.ProviderType)
	} else {
		captureError(perr)
	}
	// Every save on this page answers here with ?err=… (a provider that cannot
	// resolve the picked resource, an unsupported provider, a failed save), so
	// dropping the parameter made a failed save look like a no-op refresh. The
	// shell already renders a flash; the page handler just never fed it (#2167).
	flashErr := flashError(c)
	query := c.QueryParam("q")
	if c.QueryParam("static") == "1" {
		resources, truncated, rerr := s.memory.ListDSResources(ctx, id, query)
		if rerr != nil {
			rerr = dsFormError(rerr)
		}
		return s.page(c, pageTitle(dsSourceName(*src), "Resources"), DataSourceResourceSelectPageStatic(src, providerName, query, resources, truncated, rerr, "", flashErr))
	}
	return s.page(c, pageTitle(dsSourceName(*src), "Resources"), DataSourceResourceSelectPage(src, providerName, query, nil, "", flashErr))
}

// uiDataSourceResourceList renders the deferred resource-list fragment for a
// source: it recomputes the source and lists the provider's resources, then
// returns the bare list partial (no page layout) for the HTMX swap. Errors map
// through dsFormError, not userFacingError: the backend's 4xx message is the
// actionable copy ("reconnect the source…"), and userFacingError would flatten
// it to "Something went wrong." exactly when the user needs it.
func (s *Server) uiDataSourceResourceList(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	query := c.QueryParam("q")
	src, err := s.memory.GetDSIntegration(ctx, id)
	if err != nil {
		// A fragment swapped into an existing page: the page already owns the h1,
		// so the error renders as a notice under it.
		renderPartial(c.Response().Writer, c.Request(), pageErrorNotice("Data source unavailable", dsFormError(err)))
		return nil
	}
	// The fragment mirrors the page's Drive gate: the folder-listing step is
	// retired for Drive (targets are picked in the Google Picker on the detail
	// page), so a direct or stale hit on /resources/list must bounce to the same
	// place rather than list folders the page never offers. Sharing dsPickerURL
	// keeps the two surfaces from drifting apart.
	if src.ProviderType == dsProviderTypeGoogleDrive {
		redirectAfterMutation(c, dsPickerURL(id))
		return nil
	}
	providerName := dsProviderLabel(dsProviderNameMap(nil), src.ProviderType)
	if providers, perr := s.memory.ListDSProviders(ctx); perr == nil {
		providerName = dsProviderLabel(dsProviderNameMap(providers), src.ProviderType)
	} else {
		captureError(perr)
	}
	resources, truncated, rerr := s.memory.ListDSResources(ctx, id, query)
	if rerr != nil {
		rerr = dsFormError(rerr)
	}
	renderPartial(c.Response().Writer, c.Request(), DataSourceResourceList(src, providerName, resources, truncated, query, rerr))
	return nil
}

// dsDriveFileScope reports whether a source's Drive scope strategy is the
// explicit drive.file strategy — the one scope that cannot list the drive
// without the Google Picker. The strategy is resolved server-side and carried on
// the single-source read, so the gateway never re-derives it; an empty value
// means the default, which can list.
func dsDriveFileScope(src *DSIntegration) bool {
	if src == nil || src.ProviderType != dsProviderTypeGoogleDrive {
		return false
	}
	// The granted strategy wins over the configured one: the token was issued for
	// a fixed scope set at connect, so a source authorized under drive.file still
	// cannot list the drive even after the org's configured strategy changes.
	// Fall back to the configured strategy only for older sources connected
	// before the granted scope was persisted (OAuthGrantedStrategy is empty).
	if src.OAuthGrantedStrategy != "" {
		return src.OAuthGrantedStrategy == dsDriveFileStrategy
	}
	return src.OAuthStrategy == dsDriveFileStrategy
}

// dsPickerURL is the detail page with the Google Picker open — where every
// folder-listing surface for a Drive source sends the user.
func dsPickerURL(id string) string {
	return "/data-sources/" + url.PathEscape(id) + "?picker=1"
}

// dsResourceListURL builds the deferred resource-list partial URL for a source,
// carrying the current search query so a filtered view stays filtered on first
// load and on the client-side retry.
func dsResourceListURL(id, query string) string {
	u := "/data-sources/" + url.PathEscape(id) + "/resources/list"
	if q := strings.TrimSpace(query); q != "" {
		u += "?q=" + url.QueryEscape(q)
	}
	return u
}

// dsResourceStaticURL builds the no-JavaScript fallback URL for the resource
// step: the page with ?static=1 so the list is server-rendered inline.
func dsResourceStaticURL(id, query string) string {
	u := "/data-sources/" + url.PathEscape(id) + "/resources?static=1"
	if q := strings.TrimSpace(query); q != "" {
		u += "&q=" + url.QueryEscape(q)
	}
	return u
}

// uiDataSourceResourceSave saves the chosen resource onto a source's config
// (PRG). It maps the picked resource id to the provider's config key and asks
// memory to persist it. The mapping is deliberately small and provider-specific;
// a provider with no mapping fails closed rather than writing a stray key.
func (s *Server) uiDataSourceResourceSave(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	back := "/data-sources/" + url.PathEscape(id) + "/resources"
	resourceID := strings.TrimSpace(c.FormValue("resource_id"))
	src, err := s.memory.GetDSIntegration(ctx, id)
	if err != nil {
		return redirectWithError(c, back, err)
	}
	// Drive's sync target is a folder list chosen in the Google Picker, not a
	// single id from a server-rendered listing: the /resources page and this form
	// no longer exist for Drive (both bounce to ?picker=1), so a stale POST is
	// answered on the page that owns the action instead of on a page that would
	// bounce and drop the message. That includes the empty-id case — answering
	// "resource_id is required" on a page the user never sees is the #2157
	// leftover this ordering removes (#2167).
	if src.ProviderType == dsProviderTypeGoogleDrive {
		return redirectWithError(c, dsPickerURL(id), errors.New("choose folders in the Drive picker to set this source's sync target"))
	}
	if resourceID == "" {
		return redirectWithError(c, back, errors.New("resource_id is required"))
	}
	key, ok := dsResourceConfigKey(src.ProviderType)
	if !ok {
		return redirectWithError(c, back, fmt.Errorf("resource selection is not supported for provider %s", src.ProviderType))
	}
	// Merge the picked resource over the source's stored config so an update
	// preserves any other persisted non-secret keys (mime_types, recursive, ...)
	// instead of replacing the whole config with the single resource key.
	cfg := make(map[string]any, len(src.Config)+1)
	for k, v := range src.Config {
		cfg[k] = v
	}
	cfg[key] = resourceID
	if _, err := s.memory.SaveDSResource(ctx, id, cfg); err != nil {
		return redirectWithError(c, back, err)
	}
	redirectAfterMutation(c, "/data-sources/"+url.PathEscape(id)+"?updated=1")
	return nil
}

// dsResourceConfigKey maps a provider type to the config key its chosen sync
// resource is stored under. A provider absent from the switch has no generic
// resource-list step (the caller fails closed). Google Drive is deliberately
// absent: its target is a list chosen in the Google Picker, not a single id
// picked from a server-rendered listing.
func dsResourceConfigKey(providerType string) (string, bool) {
	switch providerType {
	case "github":
		return "repo", true
	default:
		return "", false
	}
}

// dsFolders decodes a source's canonical folder targets, falling back to the
// legacy single-target keys for sources connected before the list contract.
// The fallback keeps old rows renderable and syncable while they converge on
// the canonical shape at the next save.
func dsFolders(src *DSIntegration) []DSFolderTarget {
	if src == nil {
		return nil
	}
	var out []DSFolderTarget
	if items, ok := src.Config["folders"].([]any); ok {
		for _, item := range items {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			id := dsConfigString(m, "folder_id")
			if id == "" {
				continue
			}
			out = append(out, DSFolderTarget{
				FolderID: id,
				Name:     dsConfigString(m, "name"),
				DriveID:  dsConfigString(m, "drive_id"),
			})
		}
	}
	if len(out) == 0 {
		if id := dsConfigString(src.Config, "folder_id"); id != "" {
			out = append(out, DSFolderTarget{
				FolderID: id,
				DriveID:  dsConfigString(src.Config, "shared_drive_id"),
			})
		}
	}
	return out
}

// dsFolderLabel is a folder target's display name, falling back to a short
// folder id when the target was stored before names were persisted.
func dsFolderLabel(f DSFolderTarget) string {
	if name := strings.TrimSpace(f.Name); name != "" {
		return name
	}
	return "Folder " + dsShortID(f.FolderID)
}

// dsShortID shortens an opaque id for display without hiding which id it is.
func dsShortID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) <= 12 {
		return id
	}
	return id[:8] + "…" + id[len(id)-4:]
}

// dsCredentialValue renders an optional credential-summary value, naming the
// unknown state instead of leaving an empty cell.
func dsCredentialValue(v string) string {
	if s := strings.TrimSpace(v); s != "" {
		return s
	}
	return "Not recorded"
}

// dsSourceHasSyncTarget reports whether a source has the sync target its
// provider requires. Drive accepts a folder list (or the legacy single folder);
// other providers use their single config key.
func dsSourceHasSyncTarget(src *DSIntegration) bool {
	if src == nil {
		return false
	}
	if src.ProviderType == dsProviderTypeGoogleDrive {
		return len(dsFolders(src)) > 0
	}
	key, ok := dsResourceConfigKey(src.ProviderType)
	if !ok {
		return true
	}
	return strings.TrimSpace(dsConfigString(src.Config, key)) != ""
}

// dsResourcePrompt is the detail-page banner shown when a source's config is
// missing its sync target (folders/repo). OpenPicker marks the action as the
// in-page folder-picker modal instead of a link to the resource step.
type dsResourcePrompt struct {
	Title       string
	Description string
	Href        string
	OpenPicker  bool
}

// dsResourcePromptFor builds a resource prompt for a source whose config is
// missing its sync target, or nil when the source is complete or the provider
// has no resource step.
func dsResourcePromptFor(src *DSIntegration) *dsResourcePrompt {
	if src == nil || dsSourceHasSyncTarget(src) {
		return nil
	}
	_, genericResourceStep := dsResourceConfigKey(src.ProviderType)
	if src.ProviderType != dsProviderTypeGoogleDrive && !genericResourceStep {
		return nil
	}
	prompt := &dsResourcePrompt{
		Title:       dsResourceStepTitle(*src),
		Description: dsResourceStepDescription(*src),
		Href:        "/data-sources/" + url.PathEscape(src.ID) + "/resources",
	}
	if src.ProviderType == dsProviderTypeGoogleDrive {
		prompt.OpenPicker = true
	}
	return prompt
}

// dsCredentialSummary is the read-only "which credentials does this source
// use" card on the detail page. It answers the three questions a user has when
// a sync fails or when a source was connected by someone else: which OAuth app
// (and at which scope), which Google account, and which scope the token was
// actually granted.
type dsCredentialSummary struct {
	ProviderName    string
	ScopeKey        string // project | organization | deployment | ""
	ScopeLabel      string
	ClientID        string // short, display form
	ClientIDFull    string
	AccountEmail    string
	AccountLabel    string // "Trale account" / "Google account" / "Account"
	GrantedStrategy string
	GrantedLabel    string
	GrantedTone     string // "success" | "warning" | "neutral"
	Configured      bool
	LookupFailed    bool
	SettingsHref    string
	PickerBlocked   bool
	PickerError     string
	// SelfRegistering reports that the provider registers its own OAuth client
	// at connect (RFC 7591). There is no admin app to resolve and no editor to
	// link, so the card reports the automatic client instead of "not configured".
	SelfRegistering bool
	// ProviderUnknown reports that the catalog did not resolve this provider's
	// entry (a failed or empty catalog read). Without it the card would claim a
	// static app is missing for a provider that needs none, which is the exact
	// symptom of #2213 — so the "set one up" warning is suppressed instead.
	ProviderUnknown bool
	// NoOAuth reports that a resolved provider does not advertise the OAuth auth
	// method. Its config/credentials auth has no OAuth app to resolve, so the
	// OAuth credentials card (and its "set one up" warning) does not apply
	// (#2295). The zero value keeps the card for OAuth/unknown providers.
	NoOAuth bool
	// SourceID is the source this summary belongs to, so an action can target
	// that source instead of starting a new connect flow (#2107).
	SourceID string
	// CanReconnect reports that the provider advertises the OAuth connect method,
	// i.e. its grant can be re-minted by consent. A reconnect repairs the existing
	// source in place; it never creates a second one.
	CanReconnect bool
}

// dsFixedScope is the OAuth scope a provider always requests, with the label the
// credentials card shows for it.
type dsFixedScope struct {
	Strategy string
	Label    string
}

// dsFixedScopeByProvider covers the providers whose connect flow offers no scope
// choice: their sources carry no granted strategy (there is nothing to select),
// so without this table a healthy source rendered the amber "not reported"
// badge. A provider belongs here only when its connect always authorizes exactly
// one scope.
var dsFixedScopeByProvider = map[string]dsFixedScope{
	"gmail": {Strategy: "gmail.readonly", Label: "Read-only Gmail access (gmail.readonly)"},
}

// dsProviderEntry returns the catalog entry for a provider type, or nil when the
// catalog did not load or does not list it.
func dsProviderEntry(providers []DSProvider, providerType string) *DSProvider {
	for i := range providers {
		if providers[i].Type == providerType {
			return &providers[i]
		}
	}
	return nil
}

// dsAccountRowLabel is the credentials card's label for the authenticated
// account, named after the vendor that owns it rather than hard-coded to Google.
func dsAccountRowLabel(p *DSProvider) string {
	if p != nil {
		for _, name := range []string{p.VendorName, p.Name} {
			if name = strings.TrimSpace(name); name != "" {
				return name + " account"
			}
		}
	}
	return "Account"
}

// addCredentialAppDetails fills the OAuth-app columns of the credentials card
// from the credentials API: which app is resolved, at which scope, and its
// (short) client id. A lookup failure is recorded instead of failing the page.
func (s *Server) addCredentialAppDetails(ctx context.Context, summary *dsCredentialSummary) {
	descriptions, err := s.memory.ListDSCredentials(ctx)
	if err != nil {
		captureError(err)
		summary.LookupFailed = true
	}
	for _, d := range descriptions {
		if d.ProviderType != summary.ProviderName {
			continue
		}
		summary.ScopeKey = d.ResolvedScope
		var source DSOAuthCredentialSource
		switch d.ResolvedScope {
		case "project":
			source = d.Project
			summary.ScopeLabel = "Project override"
		case "organization":
			source = d.Organization
			summary.ScopeLabel = "Organization app"
		case "deployment":
			source = d.Deployment
			summary.ScopeLabel = "Deployment default"
		}
		summary.Configured = source.Configured && source.ClientID != ""
		summary.ClientIDFull = source.ClientID
		summary.ClientID = dsShortID(source.ClientID)
		break
	}
	if summary.ScopeLabel == "" {
		// A failed lookup and a genuinely unconfigured provider are different
		// problems: "not configured" would send the user to create a duplicate
		// client when the real fault is the credentials API.
		if summary.LookupFailed {
			summary.ScopeLabel = "Could not load credentials"
		} else {
			summary.ScopeLabel = "Not configured"
		}
	}
}

// dsCredentialSummary builds the credentials card for a source. The app details
// are member-readable server data (never secrets); the account email is the
// best-effort value captured at connect. A lookup failure degrades to the
// account/scope fields the source read already carries rather than failing the
// detail page.
func (s *Server) dsCredentialSummary(ctx context.Context, src *DSIntegration, provider *DSProvider) *dsCredentialSummary {
	summary := &dsCredentialSummary{
		ProviderName: src.ProviderType,
		SourceID:     src.ID,
		AccountEmail: dsConfigString(src.Config, "oauth_account_email"),
		AccountLabel: dsAccountRowLabel(provider),
		SettingsHref: dsCredentialAppHref(src.ProviderType),
		// Nothing recorded is neutral, not a fault: an older source simply has no
		// grant metadata, and Test connection is what checks the grant. The badge
		// is overwritten below when the source's strategy, the provider's single
		// fixed scope, or a recorded scope set says more.
		GrantedTone:     "neutral",
		GrantedLabel:    "Not recorded at connect",
		GrantedStrategy: src.OAuthGrantedStrategy,
		ProviderUnknown: provider == nil,
		// Unknown catalog entry keeps the existing behaviour (the warning is
		// already suppressed); only a resolved provider without oauth hides it.
		NoOAuth: provider != nil && !slices.Contains(provider.AuthMethods, "oauth"),
	}
	// A self-registering provider (Trale) registers its OAuth client through
	// RFC 7591 at connect time: there is no admin app to resolve, nothing to set
	// up, and no editor to link. Reporting "not configured" here would send the
	// user after a control that does not exist.
	if provider != nil && provider.DSProviderSelfRegistering() {
		summary.SelfRegistering = true
		summary.ScopeLabel = "Registered at connect"
		summary.Configured = true
		summary.SettingsHref = ""
	}
	// A self-registering provider has no app to resolve, so skipping the lookup
	// keeps a credentials-API failure from surfacing as a fault on a source that
	// does not depend on it.
	if !summary.SelfRegistering {
		s.addCredentialAppDetails(ctx, summary)
	}
	effective := src.OAuthGrantedStrategy
	if effective == "" {
		effective = src.OAuthStrategy
	}
	strategyMatched := true
	switch effective {
	case "drive.readonly":
		summary.GrantedLabel = "Read-only Drive access (drive.readonly)"
		summary.GrantedTone = "success"
	case "drive.file":
		summary.GrantedLabel = "Per-file access (drive.file)"
		summary.GrantedTone = "warning"
	default:
		if fixed, ok := dsFixedScopeByProvider[src.ProviderType]; ok {
			summary.GrantedLabel = fixed.Label
			summary.GrantedTone = "success"
			summary.GrantedStrategy = fixed.Strategy
		} else {
			strategyMatched = false
		}
	}
	if !strategyMatched {
		// A provider without selectable scope strategies reports the scope set it
		// actually granted (captured at connect) instead of a mapped strategy; the
		// neutral default stands when nothing was recorded.
		if scopes := dsConfigStrings(src.Config, dsOAuthGrantedScopesKey); len(scopes) > 0 {
			summary.GrantedLabel = strings.Join(scopes, " ")
			summary.GrantedTone = "success"
		}
	}
	if dsDriveFileScope(src) {
		summary.PickerBlocked = true
		summary.PickerError = "This source was authorized with per-file access (drive.file), so Google will not let the folder picker browse its folders. Reconnect the source and choose the read-only Drive scope to pick folders."
	}
	return summary
}

// uiDataSourcePickerSession proxies the short-lived Google Picker bootstrap for
// a source. It is called by the modal's script on open, so the token never sits
// in the page HTML and each open mints a fresh one. The response is no-store.
func (s *Server) uiDataSourcePickerSession(c echo.Context) error {
	session, err := s.memory.GetDSPickerSession(c.Request().Context(), c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": dsFormError(err).Error()})
	}
	c.Response().Header().Set("Cache-Control", "no-store")
	c.Response().Header().Set("Pragma", "no-cache")
	return c.JSON(http.StatusOK, session)
}

// dsSyncTargetsRequest is the picker-save payload. Only ids travel from the
// browser; the server resolves authoritative names and shared-drive ids.
type dsSyncTargetsRequest struct {
	FolderIDs []string `json:"folderIds"`
}

// uiDataSourceSyncTargetsSave persists the folders picked in the modal and
// answers 204 so the modal script can reload the page. Validation failures come
// back as JSON so the modal can show them inline without losing the selection.
func (s *Server) uiDataSourceSyncTargetsSave(c echo.Context) error {
	req := &dsSyncTargetsRequest{}
	if err := c.Bind(req); err != nil || len(req.FolderIDs) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Choose at least one folder."})
	}
	if _, err := s.memory.SetDSSyncTargets(c.Request().Context(), c.Param("id"), req.FolderIDs); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": dsFormError(err).Error()})
	}
	c.Response().Header().Set("Cache-Control", "no-store")
	return c.NoContent(http.StatusNoContent)
}

// uiOrgOAuthClients renders the org's OAuth client settings page (org-admin
// only; hidden and 404 for anyone else).
func (s *Server) uiOrgOAuthClients(c echo.Context) error {
	ctx := c.Request().Context()
	orgID := c.Param("id")
	if !s.orgAdminForCaller(c, orgID) {
		return c.NoContent(http.StatusNotFound)
	}
	apps, err := s.memory.ListOrgDSOAuthApps(ctx, orgID)
	if err != nil {
		err = userFacingError(err)
	}
	orgName := ""
	if org, oerr := s.memory.GetOrg(ctx, orgID); oerr == nil && org != nil {
		orgName = org.Name
	}
	flashMsg := flashFromQuery(c, []flashParam{
		{key: "saved", msg: "OAuth app saved."},
		{key: "deleted", msg: "OAuth app removed."},
	})
	rows := dsOAuthAppRows(apps, false, true, orgID, "")
	return s.page(c, pageTitle("OAuth apps"), OrgOAuthAppsPage(orgID, orgName, rows, err, flashMsg, flashError(c)))
}

// uiOrgOAuthClientDetail renders the org per-provider OAuth app editor: the same
// shared OAuth-client card the project editor uses, on its own page inside the
// org settings rail. Org-admin only; an unknown or non-OAuth provider is a 404,
// matching the project editor.
func (s *Server) uiOrgOAuthClientDetail(c echo.Context) error {
	ctx := c.Request().Context()
	orgID := c.Param("id")
	if !s.orgAdminForCaller(c, orgID) {
		return c.NoContent(http.StatusNotFound)
	}
	providerType := strings.TrimSpace(c.Param("providerType"))
	providers, err := s.memory.ListDSProviders(ctx)
	if err != nil {
		return redirectWithError(c, "/orgs/"+url.PathEscape(orgID)+"/settings/oauth-clients", userFacingError(err))
	}
	provider := dsOAuthAppProviderFor(providers, providerType)
	if provider == nil {
		return c.NoContent(http.StatusNotFound)
	}
	if redirect := s.legacyAppRedirect(c, ctx, true, "", orgID, provider.Vendor, provider.Type, "/orgs/"+url.PathEscape(orgID)+"/settings/oauth-clients"); redirect != "" {
		return c.Redirect(http.StatusSeeOther, redirect)
	}
	cfg, cerr := s.memory.GetDSOAuthClient(ctx, orgID, providerType)
	if cerr != nil {
		cerr = userFacingError(cerr)
	}
	var config DSOAuthClientConfig
	if cfg != nil {
		config = *cfg
	}
	orgName := ""
	if org, oerr := s.memory.GetOrg(ctx, orgID); oerr == nil && org != nil {
		orgName = org.Name
	}
	flashMsg := flashFromQuery(c, []flashParam{
		{key: "saved", msg: "OAuth client saved."},
		{key: "deleted", msg: "OAuth client removed."},
	})
	return s.page(c, pageTitle(dsCredentialAppLabel(*provider)), OrgOAuthClientDetailPage(orgID, orgName, *provider, config, cerr, flashMsg, flashError(c)))
}

// uiOrgOAuthClientUpsert saves an org's OAuth client for a provider (org-admin
// only). A blank secret preserves the stored one; scopes accept one per line or
// comma-separated. Non-secret Picker settings are merged into Config, preserving
// stored keys the form does not manage.
func (s *Server) uiOrgOAuthClientUpsert(c echo.Context) error {
	orgID := c.Param("id")
	back := "/orgs/" + url.PathEscape(orgID) + "/settings/oauth-clients"
	if !s.orgAdminForCaller(c, orgID) {
		return c.NoContent(http.StatusNotFound)
	}
	providerType := strings.TrimSpace(c.Param("providerType"))
	providers, err := s.memory.ListDSProviders(c.Request().Context())
	if err != nil {
		return redirectWithError(c, back, userFacingError(err))
	}
	// The write path must reject anything the editor never offers: an unknown,
	// non-OAuth, or self-registering provider would leave a client record that
	// cannot be managed and can never be resolved.
	if dsOAuthAppProviderFor(providers, providerType) == nil {
		return c.NoContent(http.StatusNotFound)
	}
	clientID := strings.TrimSpace(c.FormValue("client_id"))
	if clientID == "" {
		return redirectWithError(c, back, errors.New("clientId is required"))
	}
	cfg, err := s.dsOAuthClientConfigFromForm(c, func(pt string) (*DSOAuthClientConfig, error) {
		return s.memory.GetDSOAuthClient(c.Request().Context(), orgID, pt)
	}, providerType)
	if err != nil {
		return redirectWithError(c, back, err)
	}
	req := &DSSetOAuthClientRequest{
		ClientID:     clientID,
		ClientSecret: strings.TrimSpace(c.FormValue("client_secret")),
		Scopes:       dsScopesFromForm(c.FormValue("scopes")),
		Strategy:     strings.TrimSpace(c.FormValue("strategy")),
		Config:       cfg,
	}
	if _, err := s.memory.UpsertDSOAuthClient(c.Request().Context(), orgID, providerType, req); err != nil {
		return redirectWithError(c, back, err)
	}

	redirectAfterMutation(c, back+"?saved=1")
	return nil
}

// dsOAuthClientConfigFromForm builds the non-secret config map for a scoped
// OAuth client from the form, preserving any stored keys the form does not
// manage (the form only manages the two Picker fields). stored resolves the
// currently-saved client for the active scope (org or project) and provider.
// The two managed keys are set when the form supplies them and cleared when
// blank; a non-numeric Picker app id is an error because Google's Picker only
// accepts the Cloud project number. Secret material never belongs in Config.
func (s *Server) dsOAuthClientConfigFromForm(c echo.Context, stored func(providerType string) (*DSOAuthClientConfig, error), providerType string) (map[string]any, error) {
	cfg := map[string]any{}
	if got, err := stored(providerType); err != nil {
		captureError(err)
	} else if got != nil {
		for k, v := range got.Config {
			cfg[k] = v
		}
	}
	if v := strings.TrimSpace(c.FormValue("picker_developer_key")); v != "" {
		cfg["pickerDeveloperKey"] = v
	} else {
		delete(cfg, "pickerDeveloperKey")
	}
	// Google's PickerBuilder.setAppId takes the Cloud project *number*; a project
	// id (or any other string) mounts an empty picker instead of failing loudly,
	// so reject it at save time while the form is in front of the admin.
	appID := strings.TrimSpace(c.FormValue("picker_app_id"))
	if appID != "" {
		if _, err := strconv.ParseUint(appID, 10, 64); err != nil {
			return nil, errors.New("picker app ID must be the numeric Cloud project number (for example 123456789012), not the project id")
		}
		cfg["pickerAppId"] = appID
	} else {
		delete(cfg, "pickerAppId")
	}
	if len(cfg) == 0 {
		return nil, nil
	}
	return cfg, nil
}

// uiOrgOAuthClientDelete removes an org's OAuth client for a provider
// (org-admin only).
func (s *Server) uiOrgOAuthClientDelete(c echo.Context) error {
	orgID := c.Param("id")
	back := "/orgs/" + url.PathEscape(orgID) + "/settings/oauth-clients"
	if !s.orgAdminForCaller(c, orgID) {
		return c.NoContent(http.StatusNotFound)
	}
	providerType := strings.TrimSpace(c.Param("providerType"))
	providers, err := s.memory.ListDSProviders(c.Request().Context())
	if err != nil {
		return redirectWithError(c, back, userFacingError(err))
	}
	// Symmetric with the upsert: an unknown, non-OAuth, or self-registering
	// provider has no manageable client, so a delete against it is a 404.
	if dsOAuthAppProviderFor(providers, providerType) == nil {
		return c.NoContent(http.StatusNotFound)
	}
	if err := s.memory.DeleteDSOAuthClient(c.Request().Context(), orgID, providerType); err != nil {
		return redirectWithError(c, back, err)
	}
	redirectAfterMutation(c, back+"?deleted=1")
	return nil
}

// --- saved credentials (project settings) ---
//
// Saved credentials are project-scoped, reusable, write-only secrets (a GitHub
// personal access token, for example). A data source references one by id
// instead of carrying secret material in its config; the credentials API returns
// metadata only, never the secret fields.

// dsSavedCredentialPickerFor builds the picker state for one provider: its saved
// credentials, the credential the form should preselect, and the initial
// required fields for the add/edit modal. A failed credentials read leaves the
// picker empty with LoadErr set, so the form still renders and offers "New
// credential" instead of failing the page.
func (s *Server) dsSavedCredentialPickerFor(ctx context.Context, providers []DSProvider, providerType, selectedID, returnURL string) dsSavedCredentialPicker {
	picker := dsSavedCredentialPicker{
		Providers:       dsCredentialProviders(providers),
		ReturnURL:       returnURL,
		SelectedID:      selectedID,
		DefaultProvider: providerType,
	}
	picker.Supported = dsProviderSupportsCredentials(providers, providerType)
	if !picker.Supported {
		return picker
	}
	picker.FixedProvider = providerType
	picker.ShowModal = true
	items, err := s.memory.ListSavedDSCredentials(ctx, providerType)
	if err != nil {
		captureError(err)
		picker.LoadErr = dsFormError(err)
	} else {
		picker.Items = items
	}
	if _, credentialFields, ferr := s.dsProviderFields(ctx, providerType); ferr != nil {
		captureError(ferr)
	} else {
		picker.Fields = dsRequiredCredentialFields(credentialFields)
	}
	return picker
}

// uiSavedCredentialFields renders the modal's credential inputs for a provider
// (HTMX swap target). Only required fields are returned for a create; ?optional=1
// adds the rest for an edit, where leaving a field empty keeps its stored value.
func (s *Server) uiSavedCredentialFields(c echo.Context) error {
	ctx := c.Request().Context()
	providerType := strings.TrimSpace(c.QueryParam("providerType"))
	optional := c.QueryParam("optional") == "1"
	if providerType == "" {
		renderPartial(c.Response().Writer, c.Request(), dsSavedCredentialFields("", nil, !optional, errors.New("providerType is required")))
		return nil
	}
	_, credentialFields, err := s.dsProviderFields(ctx, providerType)
	if err != nil {
		renderPartial(c.Response().Writer, c.Request(), dsSavedCredentialFields(providerType, nil, !optional, err))
		return nil
	}
	fields := credentialFields
	if !optional {
		fields = dsRequiredCredentialFields(fields)
	}
	renderPartial(c.Response().Writer, c.Request(), dsSavedCredentialFields(providerType, fields, !optional, nil))
	return nil
}

// uiSavedCredentialCreate stores a new saved credential (PRG) and returns to the
// page that opened the modal with ?savedCredential=<id>, so the connect form (or
// the settings card) can preselect it.
func (s *Server) uiSavedCredentialCreate(c echo.Context) error {
	ctx := c.Request().Context()
	returnTo := dsSafeReturnPath(c.FormValue("return_to"))
	providerType := strings.TrimSpace(c.FormValue("providerType"))
	if providerType == "" {
		return redirectWithError(c, returnTo, errors.New("providerType is required"))
	}
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return redirectWithError(c, returnTo, errors.New("name is required"))
	}
	_, credentialFields, err := s.dsProviderFields(ctx, providerType)
	if err != nil {
		return redirectWithError(c, returnTo, err)
	}
	creds := dsConfigFromFields(c, credentialFields)
	if len(creds) == 0 {
		return redirectWithError(c, returnTo, errors.New("enter the provider's credential fields"))
	}
	item, err := s.memory.CreateSavedDSCredential(ctx, &DSSavedCredentialRequest{
		ProviderType: providerType,
		Name:         name,
		Credentials:  creds,
	})
	if err != nil {
		return redirectWithError(c, returnTo, dsFormError(err))
	}
	redirectAfterMutation(c, dsAppendQuery(returnTo, "savedCredential", item.ID))
	return nil
}

// uiSavedCredentialUpdate renames a saved credential and replaces the secret
// fields that were filled in (PRG). Empty secret inputs keep their stored value,
// so a rename never forces the token to be retyped.
func (s *Server) uiSavedCredentialUpdate(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	returnTo := dsSafeReturnPath(c.FormValue("return_to"))
	if id == "" {
		return redirectWithError(c, returnTo, errors.New("credential id is required"))
	}
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return redirectWithError(c, returnTo, errors.New("name is required"))
	}
	providerType := strings.TrimSpace(c.FormValue("providerType"))
	if providerType == "" {
		return redirectWithError(c, returnTo, errors.New("providerType is required"))
	}
	_, credentialFields, err := s.dsProviderFields(ctx, providerType)
	if err != nil {
		return redirectWithError(c, returnTo, err)
	}
	req := &DSSavedCredentialUpdateRequest{Name: name}
	if creds := dsConfigFromFields(c, credentialFields); len(creds) > 0 {
		req.Credentials = creds
	}
	if _, err := s.memory.UpdateSavedDSCredential(ctx, id, req); err != nil {
		return redirectWithError(c, returnTo, dsFormError(err))
	}
	redirectAfterMutation(c, dsAppendQuery(returnTo, "savedCredentialUpdated", "1"))
	return nil
}

// uiSavedCredentialDelete removes a saved credential (PRG). The server answers
// 409 while a source still uses it, and that message is surfaced on the page so
// the user knows to switch the source first.
func (s *Server) uiSavedCredentialDelete(c echo.Context) error {
	ctx := c.Request().Context()
	returnTo := dsSafeReturnPath(c.FormValue("return_to"))
	if err := s.memory.DeleteSavedDSCredential(ctx, c.Param("id")); err != nil {
		return redirectWithError(c, returnTo, dsFormError(err))
	}
	redirectAfterMutation(c, dsAppendQuery(returnTo, "savedCredentialDeleted", "1"))
	return nil
}

// --- project credentials (project settings) ---
//
// Credentials are OAuth apps, resolved project → organization → deployment.
// The table is member-readable (every project member sees which apps exist,
// which scope wins, and the non-secret client id). The per-provider editor and
// the write paths stay project-admin only. The org-wide app remains managed from
// the org settings page (/orgs/:id/settings/oauth-clients).

// dsCredentialScope* mirror the server's resolved credential scopes.
const (
	dsCredentialScopeProject      = "project"
	dsCredentialScopeOrganization = "organization"
	dsCredentialScopeDeployment   = "deployment"
)

// dsCredentialRow is one credential instance: a provider at one scope.
type dsCredentialRow struct {
	ProviderType string
	ScopeKey     string
	App          string
	DataSource   string
	ScopeIcon    string
	ScopeLabel   string
	Configured   bool
	ClientID     string
	UpdatedAt    string
	Resolved     bool
	ManageHref   string
	ManageLabel  string
	ManagedNote  string
}

// dsCredentialsPageData is the project credentials page payload.
type dsCredentialsPageData struct {
	Nav              settingsNavCommon
	ProjectName      string
	OrgName          string
	Rows             []dsCredentialRow
	SavedCredentials []dsSavedCredentialRow
	// SavedCredentialsErr keeps a failed saved-credential read from taking the
	// whole page down: the OAuth apps table still renders, with the failure
	// called out above it.
	SavedCredentialsErr   error
	SavedCredentialPicker dsSavedCredentialPicker
	DeploymentNote        string
	LoadErr               error
	FlashMsg              string
	FlashErr              error
}

// dsSavedCredentialRow is one saved credential plus its provider's display name.
type dsSavedCredentialRow struct {
	DSSavedCredential
	ProviderName string
}

// dsSavedCredentialPicker is the shared saved-credential state for the connect
// form, the source settings card, and the reusable add/edit modal. Supported
// means the provider accepts a saved credential (so the raw credential inputs
// are replaced); Items is the list for the picker; Fields is the initial set of
// required credential inputs rendered inside the modal.
type dsSavedCredentialPicker struct {
	Supported       bool
	ShowModal       bool
	Items           []DSSavedCredential
	SelectedID      string
	ReturnURL       string
	FixedProvider   string
	DefaultProvider string
	Providers       []DSProvider
	Fields          []dsConfigField
	LoadErr         error
}

// dsCredentialProviders filters the catalog to the providers that accept a saved
// credential — the only ones the modal offers.
func dsCredentialProviders(providers []DSProvider) []DSProvider {
	out := make([]DSProvider, 0, len(providers))
	for _, p := range providers {
		if p.DSProviderSupportsCredentials() {
			out = append(out, p)
		}
	}
	return out
}

// dsProviderSupportsCredentials reports whether the catalog lists the provider
// with the credentials connect method.
func dsProviderSupportsCredentials(providers []DSProvider, providerType string) bool {
	for _, p := range providers {
		if p.Type == providerType {
			return p.DSProviderSupportsCredentials()
		}
	}
	return false
}

// dsRequiredCredentialFields keeps the schema's required credential fields: the
// saved-credential modal asks only for what the provider needs to connect.
func dsRequiredCredentialFields(fields []dsConfigField) []dsConfigField {
	out := make([]dsConfigField, 0, len(fields))
	for _, f := range fields {
		if f.Required {
			out = append(out, f)
		}
	}
	return out
}

// dsSavedCredentialLabel is a saved credential's option label: its name plus how
// many sources already use it, so reusing one is an informed choice.
func dsSavedCredentialLabel(c DSSavedCredential) string {
	name := strings.TrimSpace(c.Name)
	if name == "" {
		name = "Credential " + dsShortID(c.ID)
	}
	return name + " — " + dsUsedByLabel(c.UsedByCount)
}

// dsUsedByLabel renders a saved credential's usage count as plain copy.
func dsUsedByLabel(n int) string {
	switch n {
	case 0:
		return "Not used"
	case 1:
		return "1 source"
	default:
		return strconv.Itoa(n) + " sources"
	}
}

// dsPickerProviderName resolves a provider's display name from the catalog.
func dsPickerProviderName(providers []DSProvider, providerType string) string {
	if providerType == "" {
		return ""
	}
	return dsProviderLabel(dsProviderNameMap(providers), providerType)
}

// savedCredentialFormAction is the modal form's POST target: create for a nil
// edit, the update endpoint for an existing credential.
func savedCredentialFormAction(edit *DSSavedCredential) string {
	if edit != nil {
		return "/settings/credentials/saved/" + url.PathEscape(edit.ID) + "/update"
	}
	return "/settings/credentials/saved"
}

// dsSafeReturnPath confines a modal's return path to this origin: a same-origin
// path beginning with a single slash. Anything absolute, scheme-relative, or
// scheme-qualified falls back to the credentials page, so the form can never be
// used as an open redirect.
func dsSafeReturnPath(raw string) string {
	const fallback = "/settings/credentials"
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return fallback
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" {
		return fallback
	}
	return raw
}

// dsAppendQuery adds key=value to path, preserving an existing query string.
func dsAppendQuery(path, key, value string) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + key + "=" + url.QueryEscape(value)
}

// dsCredentialDetailData is the per-provider project editor payload.
type dsCredentialDetailData struct {
	Nav           settingsNavCommon
	Provider      DSProvider
	Config        DSOAuthClientConfig
	OrgConfigured bool
	OrgManageHref string
	LoadErr       error
	FlashMsg      string
	FlashErr      error
}

// dsCredentialAppLabel is the vendor-level credential label ("Google OAuth app")
// shown in the table and editor card. The data source keeps its own name so a
// Google app is never mislabelled as a Drive-only credential.
func dsCredentialAppLabel(p DSProvider) string {
	vendor := strings.TrimSpace(p.VendorName)
	if vendor == "" {
		vendor = strings.TrimSpace(p.Name)
	}
	if vendor == "" {
		vendor = p.Type
	}
	return vendor + " OAuth app"
}

// dsOAuthAppProviders filters the catalogue down to the providers that have an
// admin-manageable OAuth app: those advertising the oauth auth method, minus the
// self-registering ones (Trale), which register their own client via RFC 7591 at
// connect time and have no client id/secret to manage. The org OAuth-apps table
// and its per-provider editor both render from this list, so a credentials-only
// provider (GitHub's personal-access-token method) never gets an app row.
func dsOAuthAppProviders(providers []DSProvider) []DSProvider {
	out := make([]DSProvider, 0, len(providers))
	for _, p := range providers {
		if p.DSProviderSelfRegistering() || !p.DSProviderSupportsOAuth() {
			continue
		}
		out = append(out, p)
	}
	return out
}

// dsOAuthAppProviderFor resolves a provider type to the one catalogue entry that
// is admin-manageable — the same predicate the org editor and table render from
// (advertises oauth and is not self-registering). A nil return means the type is
// unknown, non-OAuth, or self-registering, and the caller must 404 rather than
// create or delete a client record the UI can never offer.
func dsOAuthAppProviderFor(providers []DSProvider, providerType string) *DSProvider {
	for i := range providers {
		if providers[i].Type == providerType && providers[i].DSProviderSupportsOAuth() && !providers[i].DSProviderSelfRegistering() {
			return &providers[i]
		}
	}
	return nil
}

// dsOrgOAuthClientHref is the org per-provider OAuth app editor path.
func dsOrgOAuthClientHref(orgID, providerType string) string {
	return "/orgs/" + url.PathEscape(orgID) + "/settings/oauth-clients/" + url.PathEscape(providerType)
}

// dsCredentialRows builds the table rows: the project row for every OAuth
// provider, plus the organization row for a provider only when the organization
// app is configured, in catalog order. A provider with no project app still gets
// its project row so the admin "Set up" action stays reachable; an unconfigured
// organization row carries no information (resolution never lands there) and is
// omitted, which keeps the table one row per provider until an org app exists.
// It also collects the providers that currently resolve to the deployment
// default, so the page can explain that no user-managed app is in play. Actions
// depend on the caller's roles: project admins manage project rows, org admins
// manage organization rows, and everyone else sees a read-only note.
func dsCredentialRows(providers []DSProvider, creds []DSOAuthCredentialDescription, isProjectAdmin, isOrgAdmin bool, orgID string) ([]dsCredentialRow, string) {
	byType := make(map[string]DSOAuthCredentialDescription, len(creds))
	for _, c := range creds {
		byType[c.ProviderType] = c
	}
	var rows []dsCredentialRow
	var deployment []string
	for _, p := range providers {
		if !p.DSProviderSupportsOAuth() {
			continue
		}
		// Self-registering providers (Trale) discover + register their own client
		// via RFC 7591 at connect time; there is no client id/secret to manage, so
		// they must not appear as a "Set up OAuth app" row (or in the
		// deployment-default note) here or on the org OAuth-clients page.
		if p.DSProviderSelfRegistering() {
			continue
		}
		desc, ok := byType[p.Type]
		if !ok {
			desc = DSOAuthCredentialDescription{ProviderType: p.Type}
		}
		app := dsCredentialAppLabel(p)

		projectRow := dsCredentialRow{
			ProviderType: p.Type,
			ScopeKey:     "project",
			App:          app,
			DataSource:   p.Name,
			ScopeIcon:    "lucide--folder",
			ScopeLabel:   "Project",
			Configured:   desc.Project.Configured,
			ClientID:     desc.Project.ClientID,
			UpdatedAt:    desc.Project.UpdatedAt,
			Resolved:     desc.ResolvedScope == dsCredentialScopeProject,
		}
		if isProjectAdmin {
			projectRow.ManageHref = dsCredentialAppHref(p.Type)
			projectRow.ManageLabel = "Manage"
			if !projectRow.Configured {
				projectRow.ManageLabel = "Set up"
			}
		} else {
			projectRow.ManagedNote = "Managed by project admins"
		}
		rows = append(rows, projectRow)

		if desc.Organization.Configured {
			orgRow := dsCredentialRow{
				ProviderType: p.Type,
				ScopeKey:     "organization",
				App:          app,
				DataSource:   p.Name,
				ScopeIcon:    "lucide--building-2",
				ScopeLabel:   "Organization",
				Configured:   desc.Organization.Configured,
				ClientID:     desc.Organization.ClientID,
				UpdatedAt:    desc.Organization.UpdatedAt,
				Resolved:     desc.ResolvedScope == dsCredentialScopeOrganization,
			}
			if isOrgAdmin && orgID != "" {
				orgRow.ManageHref = dsOrgOAuthClientHref(orgID, p.Type)
				orgRow.ManageLabel = "Manage"
			} else {
				orgRow.ManagedNote = "Managed by org admins"
			}
			rows = append(rows, orgRow)
		}

		if desc.ResolvedScope == dsCredentialScopeDeployment {
			deployment = append(deployment, p.Name)
		}
	}

	note := ""
	if len(deployment) > 0 {
		note = strings.Join(deployment, ", ") + " currently use the deployment's built-in OAuth app. Add a project or organization app to override it."
	}
	return rows, note
}

// uiProjectCredentials renders the project's credentials table. Every project
// member can view it; the data comes from the member-safe credentials endpoint,
// which never returns secrets.
func (s *Server) uiProjectCredentials(c echo.Context) error {
	ctx := c.Request().Context()
	project, err := s.memory.GetCurrentProject(ctx)
	if err != nil {
		return s.page(c, pageTitle("Credentials"), CredentialsPage(dsCredentialsPageData{Nav: settingsNavCommon{}, LoadErr: userFacingError(err)}))
	}
	if project == nil {
		// No active project: land on the picker rather than rendering an empty
		// page (the same degrade as uiProjectSettings).
		return c.Redirect(http.StatusSeeOther, "/orgs")
	}
	nav := s.settingsNavPayload(c, ctx)
	providers, perr := s.memory.ListDSProviders(ctx)
	if perr != nil {
		perr = userFacingError(perr)
	}
	_, cerr := s.memory.ListDSCredentials(ctx)
	if cerr != nil {
		cerr = userFacingError(cerr)
	}
	loadErr := perr
	if loadErr == nil {
		loadErr = cerr
	}
	orgName := ""
	if project.OrgID != "" {
		if org, oerr := s.memory.GetOrg(ctx, project.OrgID); oerr == nil && org != nil {
			orgName = org.Name
		} else if oerr != nil {
			captureError(oerr)
		}
	}
	isProjectAdmin := s.strictProjectAdminForCaller(c, project.ID)
	isOrgAdmin := s.orgAdminForCaller(c, project.OrgID)
	apps, aerr := s.memory.ListProjectDSOAuthApps(ctx, project.ID)
	if aerr != nil {
		captureError(aerr)
	}
	rows := dsOAuthAppRows(apps, isProjectAdmin, isOrgAdmin, project.OrgID, "/settings/credentials/apps")
	deploymentNote := ""
	if len(apps) == 0 {
		deploymentNote = "No OAuth apps yet. Data sources use the deployment's built-in app when one is configured."
	}
	credProviders := dsCredentialProviders(providers)
	savedPicker := dsSavedCredentialPicker{
		Providers: credProviders,
		ShowModal: len(credProviders) > 0,
		ReturnURL: "/settings/credentials",
	}
	saved, serr := s.memory.ListSavedDSCredentials(ctx, "")
	savedErr := error(nil)
	if serr != nil {
		captureError(serr)
		savedErr = userFacingError(serr)
	}
	savedRows := make([]dsSavedCredentialRow, 0, len(saved))
	for _, item := range saved {
		savedRows = append(savedRows, dsSavedCredentialRow{
			DSSavedCredential: item,
			ProviderName:      dsPickerProviderName(providers, item.ProviderType),
		})
	}
	if len(credProviders) > 0 {
		savedPicker.DefaultProvider = credProviders[0].Type
		if _, credentialFields, ferr := s.dsProviderFields(ctx, savedPicker.DefaultProvider); ferr != nil {
			captureError(ferr)
		} else {
			savedPicker.Fields = dsRequiredCredentialFields(credentialFields)
		}
	}
	flashMsg := flashFromQuery(c, []flashParam{
		{key: "savedCredential", msg: "Credential saved."},
		{key: "savedCredentialUpdated", msg: "Credential updated."},
		{key: "savedCredentialDeleted", msg: "Credential deleted."},
		{key: "saved", msg: "OAuth app saved."},
		{key: "deleted", msg: "Project OAuth app removed."},
	})
	return s.page(c, pageTitle("Credentials"), CredentialsPage(dsCredentialsPageData{
		Nav:                   nav,
		ProjectName:           project.Name,
		OrgName:               orgName,
		Rows:                  rows,
		SavedCredentials:      savedRows,
		SavedCredentialsErr:   savedErr,
		SavedCredentialPicker: savedPicker,
		DeploymentNote:        deploymentNote,
		LoadErr:               loadErr,
		FlashMsg:              flashMsg,
		FlashErr:              flashError(c),
	}))
}

// legacyAppRedirect maps a deprecated per-provider editor URL to the named-app
// surface: the sole app's editor when there is one, otherwise the app list.
// Empty means no eligible app for this editor; the caller then renders the
// per-provider form (whose inheritance note explains an org app, if one owns
// the vendor).
//
// At project scope only project-owned apps are eligible. The project detail
// route reads through the project store, so redirecting to an organization app
// there lands on not-found; the project editor instead renders the inheritance
// note that explains the organization app is in use.
func (s *Server) legacyAppRedirect(c echo.Context, ctx context.Context, isOrg bool, projectID string, orgID, vendor, providerType, listHref string) string {
	vendor = strings.TrimSpace(vendor)
	if vendor == "" {
		vendor = providerType
	}
	apps, err := s.listOAuthApps(ctx, isOrg, projectID, orgID)
	if err != nil {
		return ""
	}
	matches := make([]DSOAuthApp, 0)
	for _, app := range apps {
		if app.Vendor != vendor {
			continue
		}
		if !isOrg && app.Scope == dsCredentialScopeOrganization {
			continue
		}
		matches = append(matches, app)
	}
	switch len(matches) {
	case 0:
		return ""
	case 1:
		base := "/settings/credentials/apps"
		if isOrg {
			base = "/orgs/" + url.PathEscape(orgID) + "/settings/oauth-apps"
		}
		return base + "/" + url.PathEscape(matches[0].ID)
	default:
		return listHref
	}
}

func (s *Server) listOAuthApps(ctx context.Context, isOrg bool, projectID string, orgID string) ([]DSOAuthApp, error) {
	if isOrg {
		return s.memory.ListOrgDSOAuthApps(ctx, orgID)
	}
	return s.memory.ListProjectDSOAuthApps(ctx, projectID)
}

// uiProjectCredential renders the per-provider project OAuth app editor
// (project-admin only; hidden and 404 for everyone else). The org-wide app's
// configured flag comes from the member-safe endpoint so a project admin who is
// not an org admin still sees the inheritance state without the org secret or
// client id leaking beyond what the table already shows.
func (s *Server) uiProjectCredential(c echo.Context) error {
	ctx := c.Request().Context()
	project, err := s.memory.GetCurrentProject(ctx)
	if err != nil || project == nil {
		return redirectWithError(c, "/settings/credentials", errors.New("no active project"))
	}
	if !s.strictProjectAdminForCaller(c, project.ID) {
		return c.NoContent(http.StatusNotFound)
	}
	providerType := strings.TrimSpace(c.Param("providerType"))
	providers, perr := s.memory.ListDSProviders(ctx)
	if perr != nil {
		return redirectWithError(c, "/settings/credentials", perr)
	}
	var provider *DSProvider
	for i := range providers {
		// Self-registering providers (Trale) have no per-provider app editor.
		if providers[i].Type == providerType && providers[i].DSProviderSupportsOAuth() && !providers[i].DSProviderSelfRegistering() {
			provider = &providers[i]
			break
		}
	}
	if provider == nil {
		return c.NoContent(http.StatusNotFound)
	}
	if redirect := s.legacyAppRedirect(c, ctx, false, project.ID, "", provider.Vendor, provider.Type, "/settings/credentials"); redirect != "" {
		return c.Redirect(http.StatusSeeOther, redirect)
	}
	cfg, cerr := s.memory.GetProjectDSOAuthClient(ctx, project.ID, providerType)
	if cerr != nil {
		cerr = userFacingError(cerr)
	}
	orgConfigured := false
	if creds, lerr := s.memory.ListDSCredentials(ctx); lerr == nil {
		for _, d := range creds {
			if d.ProviderType == providerType {
				orgConfigured = d.Organization.Configured
				break
			}
		}
	} else {
		captureError(lerr)
	}
	orgManageHref := ""
	if project.OrgID != "" && s.orgAdminForCaller(c, project.OrgID) {
		orgManageHref = dsOrgOAuthClientHref(project.OrgID, providerType)
	}
	var config DSOAuthClientConfig
	if cfg != nil {
		config = *cfg
	}
	flashMsg := flashFromQuery(c, []flashParam{
		{key: "saved", msg: "OAuth app saved."},
		{key: "deleted", msg: "Project OAuth app removed."},
	})
	return s.page(c, pageTitle(dsCredentialAppLabel(*provider)), CredentialDetailPage(dsCredentialDetailData{
		Nav:           s.settingsNavPayload(c, ctx),
		Provider:      *provider,
		Config:        config,
		OrgConfigured: orgConfigured,
		OrgManageHref: orgManageHref,
		LoadErr:       cerr,
		FlashMsg:      flashMsg,
		FlashErr:      flashError(c),
	}))
}

// uiProjectOAuthClientsRedirect retires the old project OAuth-client URL.
func (s *Server) uiProjectOAuthClientsRedirect(c echo.Context) error {
	return c.Redirect(http.StatusSeeOther, "/settings/credentials")
}

// uiProjectOAuthClientUpsert saves the project's OAuth client for a provider
// (project-admin only). A blank secret preserves the stored one; scopes accept
// one per line or comma-separated. Non-secret Picker settings are merged into
// Config, preserving stored keys the form does not manage.
func (s *Server) uiProjectOAuthClientUpsert(c echo.Context) error {
	ctx := c.Request().Context()
	back := dsCredentialAppHref(c.Param("providerType"))
	project, err := s.memory.GetCurrentProject(ctx)
	if err != nil || project == nil {
		return redirectWithError(c, back, errors.New("no active project"))
	}
	if !s.strictProjectAdminForCaller(c, project.ID) {
		return c.NoContent(http.StatusNotFound)
	}
	clientID := strings.TrimSpace(c.FormValue("client_id"))
	if clientID == "" {
		return redirectWithError(c, back, errors.New("clientId is required"))
	}
	cfg, err := s.dsOAuthClientConfigFromForm(c, func(pt string) (*DSOAuthClientConfig, error) {
		return s.memory.GetProjectDSOAuthClient(ctx, project.ID, pt)
	}, c.Param("providerType"))
	if err != nil {
		return redirectWithError(c, back, err)
	}
	req := &DSSetOAuthClientRequest{
		ClientID:     clientID,
		ClientSecret: strings.TrimSpace(c.FormValue("client_secret")),
		Scopes:       dsScopesFromForm(c.FormValue("scopes")),
		Strategy:     strings.TrimSpace(c.FormValue("strategy")),
		Config:       cfg,
	}
	if _, err := s.memory.UpsertProjectDSOAuthClient(ctx, project.ID, c.Param("providerType"), req); err != nil {
		return redirectWithError(c, back, err)
	}
	redirectAfterMutation(c, back+"?saved=1")
	return nil
}

// uiProjectOAuthClientDelete removes the project's OAuth client for a provider
// (project-admin only), restoring the org-wide client as the resolved one.
func (s *Server) uiProjectOAuthClientDelete(c echo.Context) error {
	ctx := c.Request().Context()
	back := dsCredentialAppHref(c.Param("providerType"))
	project, err := s.memory.GetCurrentProject(ctx)
	if err != nil || project == nil {
		return redirectWithError(c, back, errors.New("no active project"))
	}
	if !s.strictProjectAdminForCaller(c, project.ID) {
		return c.NoContent(http.StatusNotFound)
	}
	if err := s.memory.DeleteProjectDSOAuthClient(ctx, project.ID, c.Param("providerType")); err != nil {
		return redirectWithError(c, back, err)
	}
	redirectAfterMutation(c, back+"?deleted=1")
	return nil
}

// dsScopesFromForm splits a scopes textarea into a list, accepting one scope per
// line or comma-separated on a single line.
func dsScopesFromForm(raw string) []string {
	var scopes []string
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' }) {
		if sc := strings.TrimSpace(part); sc != "" {
			scopes = append(scopes, sc)
		}
	}
	return scopes
}

// dsScopesText renders a scopes list as one-per-line textarea content.
func dsScopesText(scopes []string) string {
	return strings.Join(scopes, "\n")
}

// dsOAuthClientsSubtitle builds the org clients page subtitle from the org name.
func dsOAuthClientsSubtitle(orgName string) string {
	if orgName != "" {
		return "OAuth client credentials for " + orgName + ", used when connecting providers with OAuth. A project client overrides these for that project only."
	}
	return "OAuth client credentials, used when connecting providers with OAuth. A project client overrides these for that project only."
}

// dsOrgOAuthClientSubtitle describes the org per-provider editor without
// assuming the org name resolved (GetOrg is best-effort on that page).
func dsOrgOAuthClientSubtitle(orgName string) string {
	if orgName != "" {
		return "Used by every project in " + orgName + " that does not set up its own app."
	}
	return "Used by every project that does not set up its own app."
}

// dsResourceStepTitle is the provider-driven heading for the resource step.
func dsResourceStepTitle(src DSIntegration) string {
	if src.ProviderType == dsProviderTypeGoogleDrive {
		return "Choose a Drive folder"
	}
	return "Choose a resource (repository or folder)"
}

// dsResourceStepDescription is the provider-driven prompt for the resource step.
func dsResourceStepDescription(src DSIntegration) string {
	if src.ProviderType == dsProviderTypeGoogleDrive {
		return "Select the Drive folder to sync into this project."
	}
	return "Select the repository or folder to sync into this project."
}

// dsResourceUseLabel is the submit label for a resource row, provider-driven.
func dsResourceUseLabel(src DSIntegration) string {
	if src.ProviderType == dsProviderTypeGoogleDrive {
		return "Use this folder"
	}
	return "Use this resource"
}

// dsStrategyOptions lists a provider's advertised org-selectable OAuth
// strategies, keeping a currently-stored value selectable even when the provider
// no longer advertises it.
func dsStrategyOptions(p DSProvider, current string) []string {
	if curr := strings.TrimSpace(current); curr != "" && !slices.Contains(p.Strategies, curr) {
		return append(slices.Clone(p.Strategies), curr)
	}
	return p.Strategies
}

// dsOAuthGrantedScopesKey is the non-secret config key the server writes at
// connect with the scope set the token response actually granted (see
// oauthGrantedScopesKey in the sources domain). It is the only grant detail a
// provider without selectable scope strategies can report.
const dsOAuthGrantedScopesKey = "oauth_granted_scopes"

// dsConfigStrings reads a stored list value from a non-secret config map (JSON
// arrays decode to []any), dropping blank entries.
func dsConfigStrings(cfg map[string]any, key string) []string {
	switch v := cfg[key].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// secretInputPlaceholder is the write-only secret field's hint: when a secret is
// already stored the field is a replacement control, so the placeholder says a
// blank value keeps the saved secret instead of implying nothing is set (#424).
func secretInputPlaceholder(saved bool) string {
	if saved {
		return "Leave blank to keep the saved secret"
	}
	return "Paste the client secret"
}

// dsConfigString reads a string value from a non-secret config map, returning ""
// when absent or not a string.
func dsConfigString(cfg map[string]any, key string) string {
	if v, ok := cfg[key].(string); ok {
		return v
	}
	return ""
}

// dsConfigIntText renders a stored numeric value for an integer field. The API
// decodes JSON numbers as float64, so a string-only read would leave the field
// blank on the edit form and a save would silently reset it to the default.
func dsConfigIntText(cfg map[string]any, key string) string {
	switch v := cfg[key].(type) {
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case json.Number:
		return v.String()
	case string:
		return v
	}
	return ""
}

// uiDataSourceTestConfig tests the unsaved config from the connect form, using
// the form's selected provider, and swaps the result into the form's result
// region.
func (s *Server) uiDataSourceTestConfig(c echo.Context) error {
	ctx := c.Request().Context()
	providerType := strings.TrimSpace(c.FormValue("providerType"))
	if providerType == "" {
		renderPartial(c.Response().Writer, c.Request(), DSTestResultPartial(nil, errors.New("providerType is required")))
		return nil
	}
	cfg, err := s.dsFormConfig(ctx, c, providerType)
	if err != nil {
		renderPartial(c.Response().Writer, c.Request(), DSTestResultPartial(nil, err))
		return nil
	}
	resp, terr := s.memory.TestDSConfig(ctx, &DSTestConfigRequest{
		ProviderType: providerType,
		Config:       cfg,
		CredentialID: strings.TrimSpace(c.FormValue("credential_id")),
	})
	renderPartial(c.Response().Writer, c.Request(), DSTestResultPartial(resp, dsFormError(terr)))
	return nil
}

// dsConnectFormPath is a provider's credentials connect form. The chooser at
// /data-sources/new precedes it, so a form error bounces back here (with ?err=)
// rather than to the chooser the user just came from.
func dsConnectFormPath(providerType string) string {
	return "/data-sources/new?providerType=" + url.QueryEscape(providerType) + "&method=credentials"
}

// uiDataSourceCreate handles the connect form (PRG): it creates the source from
// the selected provider's schema-driven config and redirects to its detail page,
// or back to the same provider's form with ?err=. Validation is the server's
// (provider ValidateConfig); the gateway does not apply another provider's rules.
func (s *Server) uiDataSourceCreate(c echo.Context) error {
	ctx := c.Request().Context()
	providerType := strings.TrimSpace(c.FormValue("providerType"))
	if providerType == "" {
		return redirectWithError(c, "/data-sources/new", errors.New("providerType is required"))
	}
	back := dsConnectFormPath(providerType)
	cfg, err := s.dsFormConfig(ctx, c, providerType)
	if err != nil {
		return redirectWithError(c, back, err)
	}
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return redirectWithError(c, back, errors.New("name is required"))
	}
	mode := cmp.Or(c.FormValue("sync_mode"), "manual")
	req := &DSCreateIntegrationRequest{
		Name:                name,
		ProviderType:        providerType,
		Config:              cfg,
		SyncMode:            &mode,
		SyncIntervalMinutes: dsIntervalForMode(c, mode),
		CredentialID:        strings.TrimSpace(c.FormValue("credential_id")),
	}
	item, err := s.memory.CreateDSIntegration(ctx, req)
	if err != nil {
		return redirectWithError(c, back, dsFormError(err))
	}
	redirectAfterMutation(c, s.dsScoped("/data-sources/"+url.PathEscape(item.ID)+"?created=1", c))
	return nil
}

// uiDataSourceRepos lists the repositories visible to the token currently in the
// connect form and renders them as suggestions for the repo field. The token is
// used for this call only; the memory endpoint never persists it.
func (s *Server) uiDataSourceRepos(c echo.Context) error {
	return s.uiDataSourceOptions(c, "repo", func(ctx context.Context, providerType string, cfg map[string]any, credentialID string) ([]DSResource, bool, error) {
		return s.memory.ListDSProviderResources(ctx, providerType, cfg, credentialID)
	})
}

// uiDataSourceRefs lists the branches and tags of the repository currently in
// the connect form, as suggestions for the ref field.
func (s *Server) uiDataSourceRefs(c echo.Context) error {
	return s.uiDataSourceOptions(c, "ref", func(ctx context.Context, providerType string, cfg map[string]any, credentialID string) ([]DSResource, bool, error) {
		return s.memory.ListDSProviderRefs(ctx, providerType, cfg, credentialID)
	})
}

// uiDataSourceFieldOptions lists a saved source's selectable config values
// through the source's STORED credentials and renders them as clickable
// suggestions. The edit form never carries credential inputs, so the
// unsaved-config endpoints (/data-sources/repos|refs) cannot list for it.
func (s *Server) uiDataSourceFieldOptions(c echo.Context) error {
	ctx := c.Request().Context()
	items, truncated, err := s.memory.ListDSResources(ctx, c.Param("id"), "")
	if err != nil {
		err = dsFormError(err)
	}
	renderPartial(c.Response().Writer, c.Request(), DSResourceSuggestionsPartial(items, truncated, err))
	return nil
}

// uiDataSourceOptions renders a field's suggestion list for the connect form.
// The field key comes from the button that triggered the request (hx-vals), so
// a provider may name its widget field anything; defaultFieldKey is the
// endpoint's fallback for a request without one. Listing failures are inline
// and never block submission: the input keeps its typed value and stays
// editable.
func (s *Server) uiDataSourceOptions(c echo.Context, defaultFieldKey string, list func(context.Context, string, map[string]any, string) ([]DSResource, bool, error)) error {
	ctx := c.Request().Context()
	fieldKey := dsPickerFieldKey(c.FormValue("fieldKey"), defaultFieldKey)
	providerType := strings.TrimSpace(c.FormValue("providerType"))
	credentialID := strings.TrimSpace(c.FormValue("credential_id"))
	var (
		items     []DSResource
		truncated bool
		err       error
	)
	if providerType == "" {
		err = errors.New("providerType is required")
	} else {
		var cfg map[string]any
		cfg, err = s.dsFormConfig(ctx, c, providerType)
		if err == nil {
			items, truncated, err = list(ctx, providerType, cfg, credentialID)
			if err != nil {
				err = dsFormError(err)
			}
		}
	}
	renderPartial(c.Response().Writer, c.Request(), DSOptionsPartial(fieldKey, items, truncated, err))
	return nil
}

// dsPickerFieldKey returns the field key posted by a picker's load button when
// it is a plain identifier, so the datalist id matches the input's list
// attribute for a provider-named field. Anything else falls back to the
// endpoint's key.
func dsPickerFieldKey(posted, fallback string) string {
	posted = strings.TrimSpace(posted)
	if posted == "" || len(posted) > 64 {
		return fallback
	}
	for _, r := range posted {
		if r != '_' && r != '-' && r != '.' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return fallback
		}
	}
	return posted
}

// dsFormError is the message a connect/edit form shows for a failed backend
// call: the server's own 4xx message when there is one (already user-facing),
// otherwise the redacted backend error. Locally authored errors pass through
// unchanged. It never exposes the transport detail or the "memory <status>:"
// prefix.
func dsFormError(err error) error {
	if err == nil {
		return nil
	}
	if status := memoryStatus(err); status > 0 && status < http.StatusInternalServerError {
		if msg := memoryHTTPErrorMessage(err); msg != "" {
			return errors.New(msg)
		}
	}
	return redactBackendError(err)
}

// uiDataSourceDetail renders a source's detail/edit page: connection test,
// settings, and sync-job history.
func (s *Server) uiDataSourceDetail(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	src, err := s.memory.GetDSIntegration(ctx, id)
	if err != nil {
		return s.page(c, pageTitle("Data Source"), DataSourceDetailPage(nil, "", nil, nil, dsSavedCredentialPicker{}, nil, userFacingError(err), nil, nil, "", nil))
	}
	providerName := dsProviderLabel(dsProviderNameMap(nil), src.ProviderType)
	var providers []DSProvider
	if list, perr := s.memory.ListDSProviders(ctx); perr == nil {
		providers = list
		providerName = dsProviderLabel(dsProviderNameMap(providers), src.ProviderType)
	} else {
		captureError(perr)
	}
	var jobs *dsSyncHistoryView
	if resp, jerr := s.memory.ListDSSyncJobs(ctx, id, "", dsSyncJobsPageSize, 0); jerr == nil {
		jobs = dsSyncHistoryViewFor(resp, 0)
	} else {
		captureError(jerr)
	}
	var configFields, credentialFields []dsConfigField
	if schema, serr := s.memory.GetDSProviderSchema(ctx, src.ProviderType); serr == nil {
		configFields, credentialFields = dsConfigFields(schema, src.Config)
	}
	// A saved source's suggestions list through its stored credentials; the
	// connect form (no source yet) keeps the unsaved-config endpoints.
	for i := range configFields {
		configFields[i].SourceID = src.ID
	}
	resourcePrompt := dsResourcePromptFor(src)
	credentialSummary := s.dsCredentialSummary(ctx, src, dsProviderEntry(providers, src.ProviderType))
	credentialSummary.CanReconnect = dsProviderCanOAuth(providers, src.ProviderType)
	// The settings card offers the saved-credential picker (and the shared
	// add-credential modal) for providers connected with a credential. The
	// post-create redirect (?savedCredential) preselects the new one.
	selectedCred := cmp.Or(strings.TrimSpace(c.QueryParam("savedCredential")), src.CredentialID)
	picker := s.dsSavedCredentialPickerFor(ctx, providers, src.ProviderType, selectedCred, "/data-sources/"+url.PathEscape(src.ID))
	flashMsg := flashFromQuery(c, []flashParam{
		{key: "created", msg: "Data source connected."},
		{key: "reconnected", msg: "Connection renewed."},
		{key: "updated", msg: "Data source updated."},
		{key: "synced", msg: "Sync triggered."},
		{key: "cancelled", msg: "Sync cancelled."},
	})
	flashErr := flashError(c)
	return s.page(c, pageTitle(dsSourceName(*src)),
		DataSourceDetailPage(src, providerName, configFields, credentialFields, picker, jobs, nil, resourcePrompt, credentialSummary, flashMsg, flashErr))
}

// uiDataSourceUpdate handles the settings form (PRG). The config is rebuilt from
// the provider schema's fields; credential fields are only sent when filled, so
// a blank credential input keeps the stored credential.
func (s *Server) uiDataSourceUpdate(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	detailPath := "/data-sources/" + url.PathEscape(id)
	src, err := s.memory.GetDSIntegration(ctx, id)
	if err != nil {
		return redirectWithError(c, detailPath, err)
	}
	req := &DSUpdateIntegrationRequest{}
	if name := strings.TrimSpace(c.FormValue("name")); name != "" {
		req.Name = &name
	}
	if mode := c.FormValue("sync_mode"); mode != "" {
		req.SyncMode = &mode
		req.SyncIntervalMinutes = dsIntervalForMode(c, mode)
	} else {
		// Older forms did not carry sync_mode; the stored policy decides whether a
		// submitted interval means anything.
		req.SyncIntervalMinutes = dsIntervalForMode(c, src.SyncMode)
	}
	providerType := strings.TrimSpace(c.FormValue("providerType"))
	if providerType == "" {
		// Older forms did not carry providerType; resolve it from the source.
		providerType = src.ProviderType
	}
	if providerType == "" {
		return redirectWithError(c, detailPath, errors.New("providerType is required"))
	}
	if providerType == "gmail" {
		cfg := dsGmailConfigFromForm(c)
		scope := &DSScopeRequest{
			Filter:      mapStringAny(cfg["filter"]),
			ObjectTypes: mapStringSlice(cfg["object_types"]),
		}
		if v, ok := cfg["mcp_write_tools_enabled"].(bool); ok {
			scope.MCPWriteToolsEnabled = &v
		}
		preview, perr := s.memory.PreviewDSScope(ctx, id, scope)
		if perr != nil {
			return redirectWithError(c, detailPath, dsFormError(perr))
		}
		if preview.Changed {
			if _, uerr := s.memory.UpdateDSScope(ctx, id, scope, preview.PreviewHash, preview.BaseScopeHash); uerr != nil {
				return redirectWithError(c, detailPath, dsFormError(uerr))
			}
		}
		// Scope is persisted by the dedicated endpoint when it changed; the
		// generic update only carries name and cadence and never config.
	} else {
		cfg, cfgErr := s.dsFormConfig(ctx, c, providerType)
		if cfgErr != nil {
			return redirectWithError(c, detailPath, cfgErr)
		}
		if len(cfg) > 0 {
			req.Config = cfg
		}
	}
	req.CredentialID = strings.TrimSpace(c.FormValue("credential_id"))
	if _, err := s.memory.UpdateDSIntegration(ctx, id, req); err != nil {
		return redirectWithError(c, detailPath, dsFormError(err))
	}
	redirectAfterMutation(c, detailPath+"?updated=1")
	return nil
}

// uiDataSourceTestConnection tests an existing source's stored credentials and
// swaps the result into the detail page's result region.
func (s *Server) uiDataSourceTestConnection(c echo.Context) error {
	resp, err := s.memory.TestDSConnection(c.Request().Context(), c.Param("id"))
	renderPartial(c.Response().Writer, c.Request(), DSTestResultPartial(resp, err))
	return nil
}

// uiDataSourceTriggerSync handles the manual sync trigger (PRG).
func (s *Server) uiDataSourceTriggerSync(c echo.Context) error {
	id := c.Param("id")
	detailPath := "/data-sources/" + url.PathEscape(id)
	src, err := s.memory.GetDSIntegration(c.Request().Context(), id)
	if err != nil {
		return redirectWithError(c, detailPath, err)
	}
	// A source still missing its sync target (folder/repo) can only fail at
	// enumeration; bounce back to the detail page's resource banner instead of
	// admitting a doomed job.
	if dsResourcePromptFor(src) != nil {
		return redirectWithError(c, detailPath, errors.New("choose a sync target before syncing this data source"))
	}
	if _, err := s.memory.TriggerDSSync(c.Request().Context(), id); err != nil {
		return redirectWithError(c, detailPath, err)
	}
	// The panel and sidebar badge cache the report for 30 s; drop it so a
	// just-triggered sync does not keep showing the failure it is re-running.
	s.invalidateProcessingProblems(c.Request().Context())
	redirectAfterMutation(c, detailPath+"?synced=1")
	return nil
}

// uiDataSourceCancelSync handles cancelling a running sync job (PRG).
func (s *Server) uiDataSourceCancelSync(c echo.Context) error {
	id := c.Param("id")
	detailPath := "/data-sources/" + url.PathEscape(id)
	if _, err := s.memory.CancelDSSyncJob(c.Request().Context(), id, c.Param("jobId")); err != nil {
		return redirectWithError(c, detailPath, err)
	}
	redirectAfterMutation(c, detailPath+"?cancelled=1")
	return nil
}

// uiDataSourceDelete handles the delete form (PRG): it removes the source and
// redirects to the list with a flash.
func (s *Server) uiDataSourceDelete(c echo.Context) error {
	id := c.Param("id")
	if err := s.memory.DeleteDSIntegration(c.Request().Context(), id); err != nil {
		return redirectWithError(c, "/data-sources", err)
	}
	redirectAfterMutation(c, "/data-sources?deleted=1")
	return nil
}

// uiDocumentVersions renders a document's version history sub-page.
func (s *Server) uiDocumentVersions(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	doc, err := s.memory.GetDocument(ctx, id)
	if err != nil {
		return s.page(c, pageTitle("Document"), DocumentVersionsPage(nil, nil, userFacingError(err)))
	}
	versions, err := s.memory.ListDocumentVersions(ctx, id)
	if err != nil {
		err = userFacingError(err)
	} else {
		// Newest first: the extraction-review UI surfaces the latest version's
		// extraction provenance at the top of the history.
		slices.Reverse(versions)
	}
	return s.page(c, pageTitle(documentName(*doc), "Versions"), DocumentVersionsPage(doc, versions, err))
}

// dsGmailConfigFromForm builds the nested Gmail config object from the connect
// and edit forms. The filter is required: the backend rejects an empty scope.
func dsGmailConfigFromForm(c echo.Context) map[string]any {
	_ = c.Request().ParseForm()
	cfg := map[string]any{}
	filter := map[string]any{}
	if v := dsFormMultiValues(c, "filter.categories"); len(v) > 0 {
		filter["categories"] = v
	}
	for _, key := range []string{"filter.labels", "filter.excludeLabels", "filter.senders", "filter.excludeSenders", "filter.domains", "filter.excludeDomains", "filter.subjectContains"} {
		if v := dsFormTerms(c, key); len(v) > 0 {
			filter[strings.TrimPrefix(key, "filter.")] = v
		}
	}
	for _, key := range []string{"hasAttachment", "unreadOnly", "starredOnly"} {
		if c.FormValue("filter."+key) == "true" {
			filter[key] = true
		}
	}
	for _, key := range []string{"after", "before", "query"} {
		if v := strings.TrimSpace(c.FormValue("filter." + key)); v != "" {
			filter[key] = v
		}
	}
	if len(filter) > 0 {
		cfg["filter"] = filter
	}
	if types := dsFormMultiValues(c, "object_types"); len(types) > 0 {
		cfg["object_types"] = types
	}
	cfg["mcp_write_tools_enabled"] = c.FormValue("mcp_write_tools_enabled") == "true"
	if alias := strings.TrimSpace(c.FormValue("mcp_alias")); alias != "" {
		cfg["mcp_alias"] = alias
	}
	return cfg
}

func dsFormMultiValues(c echo.Context, key string) []string {
	raw := c.Request().Form[key]
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s := strings.TrimSpace(v); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func dsFormTerms(c echo.Context, key string) []string {
	raw := strings.TrimSpace(c.FormValue(key))
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == ',' })
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// dsGmailFilterSummary renders the persisted filter as human lines for the
// source detail card.
func dsGmailFilterSummary(cfg map[string]any) []string {
	filter, _ := cfg["filter"].(map[string]any)
	if len(filter) == 0 {
		return []string{"No filter configured — sync will fail until a scope is set."}
	}
	var out []string
	add := func(label string, v any) {
		switch val := v.(type) {
		case string:
			if strings.TrimSpace(val) != "" {
				out = append(out, label+": "+val)
			}
		case []any:
			parts := make([]string, 0, len(val))
			for _, item := range val {
				if s, ok := item.(string); ok && s != "" {
					parts = append(parts, s)
				}
			}
			if len(parts) > 0 {
				out = append(out, label+": "+strings.Join(parts, ", "))
			}
		case bool:
			if val {
				out = append(out, label)
			}
		}
	}
	for _, key := range []string{"categories", "labels", "excludeLabels", "senders", "excludeSenders", "domains", "excludeDomains", "subjectContains", "after", "before", "query"} {
		add(key, filter[key])
	}
	if v, _ := filter["hasAttachment"].(bool); v {
		out = append(out, "hasAttachment")
	}
	if v, _ := filter["unreadOnly"].(bool); v {
		out = append(out, "unreadOnly")
	}
	if v, _ := filter["starredOnly"].(bool); v {
		out = append(out, "starredOnly")
	}
	return out
}

func dsGmailObjectTypes(cfg map[string]any) string {
	v, _ := cfg["object_types"].([]any)
	if len(v) == 0 {
		return "EmailMessage, EmailThread"
	}
	parts := make([]string, 0, len(v))
	for _, item := range v {
		if s, ok := item.(string); ok {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

func dsGmailWriteEnabled(cfg map[string]any) bool {
	v, _ := cfg["mcp_write_tools_enabled"].(bool)
	return v
}

func dsGmailFormList(cfg map[string]any, filterKey string) string {
	filter, _ := cfg["filter"].(map[string]any)
	v, _ := filter[filterKey].([]any)
	parts := make([]string, 0, len(v))
	for _, item := range v {
		if s, ok := item.(string); ok {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n")
}

func dsGmailFormString(cfg map[string]any, filterKey string) string {
	filter, _ := cfg["filter"].(map[string]any)
	s, _ := filter[filterKey].(string)
	return s
}

func dsGmailFormBool(cfg map[string]any, filterKey string) bool {
	filter, _ := cfg["filter"].(map[string]any)
	b, _ := filter[filterKey].(bool)
	return b
}

func dsGmailHasCategory(cfg map[string]any, category string) bool {
	filter, _ := cfg["filter"].(map[string]any)
	v, _ := filter["categories"].([]any)
	for _, item := range v {
		if s, ok := item.(string); ok && strings.EqualFold(s, category) {
			return true
		}
	}
	return false
}

func dsGmailHasObjectType(cfg map[string]any, typ string) bool {
	v, _ := cfg["object_types"].([]any)
	if len(v) == 0 {
		return true
	}
	for _, item := range v {
		if s, ok := item.(string); ok && s == typ {
			return true
		}
	}
	return false
}

func mapStringAny(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func mapStringSlice(v any) []string {
	switch val := v.(type) {
	case []string:
		return val
	case []any:
		out := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// --- named OAuth app settings (project + organization) ---

type dsOAuthVendor struct {
	Vendor     string
	VendorName string
	Providers  []DSProvider
}

type dsOAuthAppFormData struct {
	Nav        settingsNavCommon
	OrgID      string
	OrgName    string
	Action     string
	BackHref   string
	DeleteHref string
	App        *DSOAuthApp
	Vendors    []dsOAuthVendor
	ScopeLabel string
	LoadErr    error
	FlashErr   error
}

func dsOAuthVendors(providers []DSProvider) []dsOAuthVendor {
	byVendor := map[string]*dsOAuthVendor{}
	for _, p := range providers {
		if p.DSProviderSelfRegistering() || !p.DSProviderSupportsOAuth() {
			continue
		}
		vendor := strings.TrimSpace(p.Vendor)
		if vendor == "" {
			vendor = p.Type
		}
		name := strings.TrimSpace(p.VendorName)
		if name == "" {
			name = vendor
		}
		v, ok := byVendor[vendor]
		if !ok {
			v = &dsOAuthVendor{Vendor: vendor, VendorName: name}
			byVendor[vendor] = v
		}
		v.Providers = append(v.Providers, p)
	}
	out := make([]dsOAuthVendor, 0, len(byVendor))
	for _, v := range byVendor {
		slices.SortFunc(v.Providers, func(a, b DSProvider) int { return strings.Compare(a.Type, b.Type) })
		out = append(out, *v)
	}
	slices.SortFunc(out, func(a, b dsOAuthVendor) int { return strings.Compare(a.VendorName, b.VendorName) })
	return out
}

func dsOAuthAppRows(apps []DSOAuthApp, canManageProject, canManageOrg bool, orgID, projectBase string) []dsCredentialRow {
	rows := make([]dsCredentialRow, 0, len(apps))
	for _, app := range apps {
		scope := app.Scope
		if scope == "" {
			scope = dsCredentialScopeProject
		}
		row := dsCredentialRow{
			ProviderType: app.ID,
			ScopeKey:     scope,
			App:          app.Name,
			DataSource:   app.VendorName,
			Configured:   app.Configured,
			ClientID:     app.ClientID,
			UpdatedAt:    app.UpdatedAt,
			Resolved:     app.UsedBy > 0,
		}
		canManage := canManageProject
		manageBase := projectBase
		if scope == dsCredentialScopeOrganization {
			row.ScopeIcon = "lucide--building-2"
			row.ScopeLabel = "Organization"
			canManage = canManageOrg
			manageBase = "/orgs/" + url.PathEscape(orgID) + "/settings/oauth-apps"
		} else {
			row.ScopeIcon = "lucide--folder"
			row.ScopeLabel = "Project"
		}
		if canManage {
			row.ManageHref = manageBase + "/" + url.PathEscape(app.ID)
			row.ManageLabel = "Manage"
		} else {
			row.ManagedNote = "Managed by admins"
		}
		rows = append(rows, row)
	}
	return rows
}

func (s *Server) uiProjectOAuthAppNew(c echo.Context) error {
	ctx := c.Request().Context()
	project, _ := s.memory.GetCurrentProject(ctx)
	if project == nil || !s.strictProjectAdminForCaller(c, project.ID) {
		return c.NoContent(http.StatusNotFound)
	}
	providers, err := s.memory.ListDSProviders(ctx)
	if err != nil {
		return redirectWithError(c, "/settings/credentials", userFacingError(err))
	}
	orgName := ""
	orgID := ""
	if project != nil {
		orgID = project.OrgID
		if org, oerr := s.memory.GetOrg(c.Request().Context(), project.OrgID); oerr == nil && org != nil {
			orgName = org.Name
		}
	}
	return s.page(c, pageTitle("Add OAuth app"), OAuthAppFormPage(dsOAuthAppFormData{
		Nav:        s.settingsNavPayload(c, c.Request().Context()),
		OrgID:      orgID,
		OrgName:    orgName,
		Action:     "/settings/credentials/apps",
		BackHref:   "/settings/credentials",
		Vendors:    dsOAuthVendors(providers),
		ScopeLabel: "project",
	}))
}

func (s *Server) uiProjectOAuthAppDetail(c echo.Context) error {
	ctx := c.Request().Context()
	project, _ := s.memory.GetCurrentProject(ctx)
	if project == nil || !s.strictProjectAdminForCaller(c, project.ID) {
		return c.NoContent(http.StatusNotFound)
	}
	app, err := s.memory.GetProjectDSOAuthApp(ctx, project.ID, c.Param("appId"))
	if err != nil {
		return redirectWithError(c, "/settings/credentials", userFacingError(err))
	}
	providers, _ := s.memory.ListDSProviders(ctx)
	orgName, orgID := "", ""
	orgID = project.OrgID
	if org, oerr := s.memory.GetOrg(ctx, project.OrgID); oerr == nil && org != nil {
		orgName = org.Name
	}
	return s.page(c, pageTitle("OAuth app"), OAuthAppFormPage(dsOAuthAppFormData{
		Nav:        s.settingsNavPayload(c, ctx),
		OrgID:      orgID,
		OrgName:    orgName,
		Action:     "/settings/credentials/apps/" + url.PathEscape(app.ID),
		BackHref:   "/settings/credentials",
		DeleteHref: "/settings/credentials/apps/" + url.PathEscape(app.ID) + "/delete",
		App:        app,
		Vendors:    dsOAuthVendors(providers),
		ScopeLabel: "project",
	}))
}

func (s *Server) uiProjectOAuthAppCreate(c echo.Context) error {
	ctx := c.Request().Context()
	project, _ := s.memory.GetCurrentProject(ctx)
	if project == nil || !s.strictProjectAdminForCaller(c, project.ID) {
		return c.NoContent(http.StatusNotFound)
	}
	req := dsOAuthAppRequestFromForm(c)
	_, err := s.memory.CreateProjectDSOAuthApp(ctx, project.ID, req)
	if err != nil {
		return redirectWithError(c, "/settings/credentials/apps/new", userFacingError(err))
	}
	return c.Redirect(http.StatusSeeOther, "/settings/credentials?saved=1")
}

func (s *Server) uiProjectOAuthAppUpdate(c echo.Context) error {
	ctx := c.Request().Context()
	project, _ := s.memory.GetCurrentProject(ctx)
	if project == nil || !s.strictProjectAdminForCaller(c, project.ID) {
		return c.NoContent(http.StatusNotFound)
	}
	req := dsOAuthAppUpdateFromForm(c)
	_, err := s.memory.UpdateProjectDSOAuthApp(ctx, project.ID, c.Param("appId"), req)
	if err != nil {
		return redirectWithError(c, "/settings/credentials/apps/"+url.PathEscape(c.Param("appId")), userFacingError(err))
	}
	return c.Redirect(http.StatusSeeOther, "/settings/credentials?saved=1")
}

func (s *Server) uiProjectOAuthAppDelete(c echo.Context) error {
	ctx := c.Request().Context()
	project, _ := s.memory.GetCurrentProject(ctx)
	if project == nil || !s.strictProjectAdminForCaller(c, project.ID) {
		return c.NoContent(http.StatusNotFound)
	}
	if err := s.memory.DeleteProjectDSOAuthApp(ctx, project.ID, c.Param("appId")); err != nil {
		return redirectWithError(c, "/settings/credentials", userFacingError(err))
	}
	return c.Redirect(http.StatusSeeOther, "/settings/credentials?deleted=1")
}

func (s *Server) uiOrgOAuthAppNew(c echo.Context) error {
	ctx := c.Request().Context()
	orgID := c.Param("id")
	if !s.orgAdminForCaller(c, orgID) {
		return c.NoContent(http.StatusNotFound)
	}
	providers, err := s.memory.ListDSProviders(ctx)
	if err != nil {
		return redirectWithError(c, "/orgs/"+url.PathEscape(orgID)+"/settings/oauth-clients", userFacingError(err))
	}
	orgName := ""
	if org, oerr := s.memory.GetOrg(ctx, orgID); oerr == nil && org != nil {
		orgName = org.Name
	}
	return s.page(c, pageTitle("Add OAuth app"), OAuthAppFormPage(dsOAuthAppFormData{
		Nav:        s.settingsNavPayload(c, ctx),
		OrgID:      orgID,
		OrgName:    orgName,
		Action:     "/orgs/" + url.PathEscape(orgID) + "/settings/oauth-apps",
		BackHref:   "/orgs/" + url.PathEscape(orgID) + "/settings/oauth-clients",
		Vendors:    dsOAuthVendors(providers),
		ScopeLabel: "organization",
	}))
}

func (s *Server) uiOrgOAuthAppDetail(c echo.Context) error {
	ctx := c.Request().Context()
	orgID := c.Param("id")
	if !s.orgAdminForCaller(c, orgID) {
		return c.NoContent(http.StatusNotFound)
	}
	app, err := s.memory.GetOrgDSOAuthApp(ctx, orgID, c.Param("appId"))
	if err != nil {
		return redirectWithError(c, "/orgs/"+url.PathEscape(orgID)+"/settings/oauth-clients", userFacingError(err))
	}
	providers, _ := s.memory.ListDSProviders(ctx)
	orgName := ""
	if org, oerr := s.memory.GetOrg(ctx, orgID); oerr == nil && org != nil {
		orgName = org.Name
	}
	return s.page(c, pageTitle("OAuth app"), OAuthAppFormPage(dsOAuthAppFormData{
		Nav:        s.settingsNavPayload(c, ctx),
		OrgID:      orgID,
		OrgName:    orgName,
		Action:     "/orgs/" + url.PathEscape(orgID) + "/settings/oauth-apps/" + url.PathEscape(app.ID),
		BackHref:   "/orgs/" + url.PathEscape(orgID) + "/settings/oauth-clients",
		DeleteHref: "/orgs/" + url.PathEscape(orgID) + "/settings/oauth-apps/" + url.PathEscape(app.ID) + "/delete",
		App:        app,
		Vendors:    dsOAuthVendors(providers),
		ScopeLabel: "organization",
	}))
}

func (s *Server) uiOrgOAuthAppCreate(c echo.Context) error {
	orgID := c.Param("id")
	if !s.orgAdminForCaller(c, orgID) {
		return c.NoContent(http.StatusNotFound)
	}
	if _, err := s.memory.CreateOrgDSOAuthApp(c.Request().Context(), orgID, dsOAuthAppRequestFromForm(c)); err != nil {
		return redirectWithError(c, "/orgs/"+url.PathEscape(orgID)+"/settings/oauth-apps/new", userFacingError(err))
	}
	return c.Redirect(http.StatusSeeOther, "/orgs/"+url.PathEscape(orgID)+"/settings/oauth-clients?saved=1")
}

func (s *Server) uiOrgOAuthAppUpdate(c echo.Context) error {
	orgID := c.Param("id")
	if !s.orgAdminForCaller(c, orgID) {
		return c.NoContent(http.StatusNotFound)
	}
	if _, err := s.memory.UpdateOrgDSOAuthApp(c.Request().Context(), orgID, c.Param("appId"), dsOAuthAppUpdateFromForm(c)); err != nil {
		return redirectWithError(c, "/orgs/"+url.PathEscape(orgID)+"/settings/oauth-apps/"+url.PathEscape(c.Param("appId")), userFacingError(err))
	}
	return c.Redirect(http.StatusSeeOther, "/orgs/"+url.PathEscape(orgID)+"/settings/oauth-clients?saved=1")
}

func (s *Server) uiOrgOAuthAppDelete(c echo.Context) error {
	orgID := c.Param("id")
	if !s.orgAdminForCaller(c, orgID) {
		return c.NoContent(http.StatusNotFound)
	}
	if err := s.memory.DeleteOrgDSOAuthApp(c.Request().Context(), orgID, c.Param("appId")); err != nil {
		return redirectWithError(c, "/orgs/"+url.PathEscape(orgID)+"/settings/oauth-clients", userFacingError(err))
	}
	return c.Redirect(http.StatusSeeOther, "/orgs/"+url.PathEscape(orgID)+"/settings/oauth-clients?deleted=1")
}

func dsOAuthAppRequestFromForm(c echo.Context) *DSCreateOAuthAppRequest {
	vendor := strings.TrimSpace(c.FormValue("vendor"))
	req := &DSCreateOAuthAppRequest{
		Name:         strings.TrimSpace(c.FormValue("name")),
		Vendor:       vendor,
		ClientID:     strings.TrimSpace(c.FormValue("client_id")),
		ClientSecret: c.FormValue("client_secret"),
	}
	req.ProviderConfigs = dsOAuthProviderConfigsFromForm(c, vendor)
	return req
}

func dsOAuthAppUpdateFromForm(c echo.Context) *DSUpdateOAuthAppRequest {
	name := strings.TrimSpace(c.FormValue("name"))
	clientID := strings.TrimSpace(c.FormValue("client_id"))
	req := &DSUpdateOAuthAppRequest{
		Name:         &name,
		ClientID:     &clientID,
		ClientSecret: c.FormValue("client_secret"),
	}
	if v := strings.TrimSpace(c.FormValue("vendor")); v != "" {
		req.ProviderConfigs = dsOAuthProviderConfigsFromForm(c, v)
	}
	return req
}

func dsOAuthProviderConfigsFromForm(c echo.Context, vendor string) map[string]DSOAuthProviderConfig {
	configs := map[string]DSOAuthProviderConfig{}
	if vendor == "google" {
		if strategy := strings.TrimSpace(c.FormValue("google_drive_strategy")); strategy != "" {
			configs["google_drive"] = DSOAuthProviderConfig{Strategy: strategy}
		}
	}
	return configs
}

func dsProviderVendor(providers []DSProvider, providerType string) string {
	for _, p := range providers {
		if p.Type != providerType {
			continue
		}
		if v := strings.TrimSpace(p.Vendor); v != "" {
			return v
		}
		return p.Type
	}
	return ""
}
