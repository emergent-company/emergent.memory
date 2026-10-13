package main

import (
	"cmp"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	ui "github.com/emergent-company/go-daisy/components/ui"
	"github.com/emergent-company/go-daisy/render"
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
	Type        string // "string", "array", "boolean"
	Required    bool
}

// dsConfigFieldOrder fixes the render order of the known google_drive config
// keys; any schema property outside this list is appended afterwards, sorted, so
// a forward-compatible schema addition still surfaces in the form.
var dsConfigFieldOrder = []string{
	"folder_id", "shared_drive_id", "mime_types", "recursive",
	"client_id", "client_secret", "refresh_token",
}

// dsConfigFields splits a provider schema into config fields and credential
// fields (the write-only OAuth2 material), in a stable order.
func dsConfigFields(schema *DSProviderSchema) (configFields, credentialFields []dsConfigField) {
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
		if d, ok := prop["description"].(string); ok {
			f.Description = d
		}
		if dsIsCredentialKey(key) {
			credentialFields = append(credentialFields, f)
		} else {
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

// dsIsCredentialKey reports whether a config key carries write-only credential
// material (never rendered back to the browser).
func dsIsCredentialKey(key string) bool {
	return slices.Contains(dsCredentialKeys, key)
}

// dsFieldLabel humanizes a config key into a form label.
func dsFieldLabel(key string) string {
	switch key {
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

// dsConfigFromForm builds a config map from the connect/edit form values.
// Optional string fields are omitted when blank, mime_types is split on
// newlines/commas, and recursive is included only when checked.
func dsConfigFromForm(c echo.Context) map[string]any {
	cfg := map[string]any{}
	for _, key := range []string{"folder_id", "shared_drive_id", "client_id", "client_secret", "refresh_token"} {
		if v := strings.TrimSpace(c.FormValue(key)); v != "" {
			cfg[key] = v
		}
	}
	if raw := strings.TrimSpace(c.FormValue("mime_types")); raw != "" {
		var types []string
		for _, line := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' }) {
			if s := strings.TrimSpace(line); s != "" {
				types = append(types, s)
			}
		}
		if len(types) > 0 {
			cfg["mime_types"] = types
		}
	}
	if c.FormValue("recursive") != "" {
		cfg["recursive"] = true
	}
	return cfg
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
// on /data-sources?connected=... or ?oauth_error=... (see the server's
// OAuthLandingURL), so those requests are delegated to the callback handler.
func (s *Server) uiDataSources(c echo.Context) error {
	if c.QueryParam("connected") != "" || c.QueryParam("oauth_error") != "" {
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
// (absent → chooser), and ?method narrows a dual-capability provider (Google
// Drive advertises both oauth and credentials) to one connect path.
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
	if selected != nil {
		schema, serr := s.memory.GetDSProviderSchema(ctx, selected.Type)
		if serr != nil {
			schemaErr = userFacingError(serr)
		}
		configFields, credentialFields = dsConfigFields(schema)
	}
	if err == nil {
		err = schemaErr
	}
	flashErr := flashError(c)
	return s.page(c, pageTitle("New Data Source"), DataSourceNewPage(providers, selected, configFields, credentialFields, err, flashErr, nil))
}

// dsSelectProvider picks the selected provider from the catalog by type, applying
// the optional ?method= hint: when the named method is one the provider
// advertises, the returned copy is narrowed to it so the page renders exactly
// that connect path. A dual-capability provider without a method hint keeps its
// full capability set (its OAuth surface is shown). The catalog slice entry is
// copied, never mutated.
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
	providerType := strings.TrimSpace(c.QueryParam("providerType"))
	if providerType == "" {
		return redirectWithError(c, "/data-sources/new", errors.New("providerType is required"))
	}
	resp, err := s.memory.StartDSOAuth(c.Request().Context(), providerType, nil, nil)
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
	return c.Redirect(http.StatusFound, resp.AuthorizeURL)
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
	}
	if sc, ok := sessionContextFrom(ctx); ok {
		switch {
		case sc.ProjectID != "" && s.projectAdminForCaller(c, sc.ProjectID):
			oauthErr.SettingsURL = "/settings/oauth-clients"
		case sc.OrgID != "":
			oauthErr.SettingsURL = "/orgs/" + url.PathEscape(sc.OrgID) + "/settings/oauth-clients"
		}
	}
	return s.page(c, pageTitle("New Data Source"), DataSourceNewPage(providers, nil, nil, nil, err, nil, oauthErr))
}

// uiDataSourceOAuthCallback is the memory OAuth callback's landing handler: it
// surfaces an authorization failure, redirects to the freshly connected source,
// or falls back to the list. Registered both on /data-sources/oauth/callback and
// as the delegate for /data-sources?connected=... (the server's OAuthLandingURL).
func (s *Server) uiDataSourceOAuthCallback(c echo.Context) error {
	if oauthErr := c.QueryParam("oauth_error"); oauthErr != "" {
		return redirectWithError(c, "/data-sources", errors.New("Authorization failed: "+oauthErr))
	}
	if connected := c.QueryParam("connected"); connected != "" {
		render.RedirectAfterMutation(c.Response().Writer, c.Request(), "/data-sources/"+url.PathEscape(connected)+"?created=1")
		return nil
	}
	return c.Redirect(http.StatusSeeOther, "/data-sources")
}

// uiDataSourceResources renders the resource-selection step for a source. It
// lists the provider's real resources and renders each as a select-and-save
// form. Providers without a listing (e.g. github today) get a graceful
// placeholder.
func (s *Server) uiDataSourceResources(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	src, err := s.memory.GetDSIntegration(ctx, id)
	if err != nil {
		return s.page(c, pageTitle("Data Source"), DataSourceResourceSelectPage(nil, "", nil, userFacingError(err), "", nil))
	}
	providerName := dsProviderLabel(dsProviderNameMap(nil), src.ProviderType)
	if providers, perr := s.memory.ListDSProviders(ctx); perr == nil {
		providerName = dsProviderLabel(dsProviderNameMap(providers), src.ProviderType)
	} else {
		captureError(perr)
	}
	resources, rerr := s.memory.ListDSResources(ctx, id)
	if rerr != nil {
		rerr = userFacingError(rerr)
	}
	return s.page(c, pageTitle(dsSourceName(*src), "Resources"), DataSourceResourceSelectPage(src, providerName, resources, rerr, "", nil))
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
	if resourceID == "" {
		return redirectWithError(c, back, errors.New("resource_id is required"))
	}
	src, err := s.memory.GetDSIntegration(ctx, id)
	if err != nil {
		return redirectWithError(c, back, err)
	}
	key, ok := dsResourceConfigKey(src.ProviderType)
	if !ok {
		return redirectWithError(c, back, fmt.Errorf("resource selection is not supported for provider %s", src.ProviderType))
	}
	if _, err := s.memory.SaveDSResource(ctx, id, map[string]any{key: resourceID}); err != nil {
		return redirectWithError(c, back, err)
	}
	render.RedirectAfterMutation(c.Response().Writer, c.Request(), "/data-sources/"+url.PathEscape(id)+"?updated=1")
	return nil
}

// dsResourceConfigKey maps a provider type to the config key its chosen sync
// resource is stored under. A provider absent from the switch has no resource
// step (the caller fails closed).
func dsResourceConfigKey(providerType string) (string, bool) {
	switch providerType {
	case dsProviderTypeGoogleDrive:
		return "folder_id", true
	case "github":
		return "repo", true
	default:
		return "", false
	}
}

// uiOrgOAuthClients renders the org's OAuth client settings page (org-admin
// only; hidden and 404 for anyone else).
func (s *Server) uiOrgOAuthClients(c echo.Context) error {
	ctx := c.Request().Context()
	orgID := c.Param("id")
	if !s.orgAdminForCaller(c, orgID) {
		return c.NoContent(http.StatusNotFound)
	}
	providers, err := s.memory.ListDSProviders(ctx)
	if err != nil {
		err = userFacingError(err)
	}
	clients, cerr := s.memory.ListDSOAuthClients(ctx, orgID)
	if cerr != nil {
		cerr = userFacingError(cerr)
	}
	byType := make(map[string]DSOAuthClientConfig, len(clients))
	for _, cl := range clients {
		byType[cl.ProviderType] = cl
	}
	orgName := ""
	if org, oerr := s.memory.GetOrg(ctx, orgID); oerr == nil && org != nil {
		orgName = org.Name
	}
	if err == nil {
		err = cerr
	}
	flashMsg := flashFromQuery(c, []flashParam{
		{key: "saved", msg: "OAuth client saved."},
		{key: "deleted", msg: "OAuth client removed."},
	})
	return s.page(c, pageTitle("OAuth clients"), OrgOAuthClientsPage(orgID, orgName, providers, byType, err, flashMsg, flashError(c)))
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
	clientID := strings.TrimSpace(c.FormValue("client_id"))
	if clientID == "" {
		return redirectWithError(c, back, errors.New("clientId is required"))
	}
	req := &DSSetOAuthClientRequest{
		ClientID:     clientID,
		ClientSecret: strings.TrimSpace(c.FormValue("client_secret")),
		Scopes:       dsScopesFromForm(c.FormValue("scopes")),
		Strategy:     strings.TrimSpace(c.FormValue("strategy")),
		Config: s.dsOAuthClientConfigFromForm(c, func(pt string) (*DSOAuthClientConfig, error) {
			return s.memory.GetDSOAuthClient(c.Request().Context(), orgID, pt)
		}, c.Param("providerType")),
	}
	if _, err := s.memory.UpsertDSOAuthClient(c.Request().Context(), orgID, c.Param("providerType"), req); err != nil {
		return redirectWithError(c, back, err)
	}
	render.RedirectAfterMutation(c.Response().Writer, c.Request(), back+"?saved=1")
	return nil
}

// dsOAuthClientConfigFromForm builds the non-secret config map for a scoped
// OAuth client from the form, preserving any stored keys the form does not
// manage (the form only manages the two Picker fields). stored resolves the
// currently-saved client for the active scope (org or project) and provider.
// The two managed keys are set when the form supplies them and cleared when
// blank. Secret material never belongs in Config.
func (s *Server) dsOAuthClientConfigFromForm(c echo.Context, stored func(providerType string) (*DSOAuthClientConfig, error), providerType string) map[string]any {
	cfg := map[string]any{}
	if got, err := stored(providerType); err != nil {
		captureError(err)
	} else if got != nil {
		for k, v := range got.Config {
			cfg[k] = v
		}
	}
	for _, f := range []struct{ form, key string }{
		{"picker_developer_key", "pickerDeveloperKey"},
		{"picker_app_id", "pickerAppId"},
	} {
		if v := strings.TrimSpace(c.FormValue(f.form)); v != "" {
			cfg[f.key] = v
		} else {
			delete(cfg, f.key)
		}
	}
	if len(cfg) == 0 {
		return nil
	}
	return cfg
}

// uiOrgOAuthClientDelete removes an org's OAuth client for a provider
// (org-admin only).
func (s *Server) uiOrgOAuthClientDelete(c echo.Context) error {
	orgID := c.Param("id")
	back := "/orgs/" + url.PathEscape(orgID) + "/settings/oauth-clients"
	if !s.orgAdminForCaller(c, orgID) {
		return c.NoContent(http.StatusNotFound)
	}
	if err := s.memory.DeleteDSOAuthClient(c.Request().Context(), orgID, c.Param("providerType")); err != nil {
		return redirectWithError(c, back, err)
	}
	render.RedirectAfterMutation(c.Response().Writer, c.Request(), back+"?deleted=1")
	return nil
}

// --- project-scoped OAuth clients (project-admin only) ---
//
// The project settings page mirrors the org page: a project may bring its own
// OAuth client, which overrides the org-wide one for that project only (server
// resolution: project → org → deployment env → error). The project page is
// context-addressed (`/settings/oauth-clients`, scoped by page()).

// uiProjectOAuthClients renders the active project's OAuth client settings page
// (project-admin only; hidden and 404 for anyone else).
func (s *Server) uiProjectOAuthClients(c echo.Context) error {
	ctx := c.Request().Context()
	project, err := s.memory.GetCurrentProject(ctx)
	if err != nil {
		return s.page(c, pageTitle("OAuth clients"), ProjectOAuthClientsPage("", nil, nil, userFacingError(err), "", nil))
	}
	if project == nil {
		// No active project: land on the picker rather than rendering an empty
		// page (the same degrade as uiProjectSettings).
		return c.Redirect(http.StatusSeeOther, "/orgs")
	}
	if !s.projectAdminForCaller(c, project.ID) {
		return c.NoContent(http.StatusNotFound)
	}
	providers, perr := s.memory.ListDSProviders(ctx)
	if perr != nil {
		perr = userFacingError(perr)
	}
	clients, cerr := s.memory.ListProjectDSOAuthClients(ctx, project.ID)
	if cerr != nil {
		cerr = userFacingError(cerr)
	}
	byType := make(map[string]DSOAuthClientConfig, len(clients))
	for _, cl := range clients {
		byType[cl.ProviderType] = cl
	}
	if perr == nil {
		perr = cerr
	}
	flashMsg := flashFromQuery(c, []flashParam{
		{key: "saved", msg: "OAuth client saved."},
		{key: "deleted", msg: "OAuth client removed."},
	})
	return s.page(c, pageTitle("OAuth clients"), ProjectOAuthClientsPage(project.Name, providers, byType, perr, flashMsg, flashError(c)))
}

// uiProjectOAuthClientUpsert saves the project's OAuth client for a provider
// (project-admin only). A blank secret preserves the stored one; scopes accept
// one per line or comma-separated. Non-secret Picker settings are merged into
// Config, preserving stored keys the form does not manage.
func (s *Server) uiProjectOAuthClientUpsert(c echo.Context) error {
	ctx := c.Request().Context()
	back := "/settings/oauth-clients"
	project, err := s.memory.GetCurrentProject(ctx)
	if err != nil || project == nil {
		return redirectWithError(c, back, errors.New("no active project"))
	}
	if !s.projectAdminForCaller(c, project.ID) {
		return c.NoContent(http.StatusNotFound)
	}
	clientID := strings.TrimSpace(c.FormValue("client_id"))
	if clientID == "" {
		return redirectWithError(c, back, errors.New("clientId is required"))
	}
	req := &DSSetOAuthClientRequest{
		ClientID:     clientID,
		ClientSecret: strings.TrimSpace(c.FormValue("client_secret")),
		Scopes:       dsScopesFromForm(c.FormValue("scopes")),
		Strategy:     strings.TrimSpace(c.FormValue("strategy")),
		Config: s.dsOAuthClientConfigFromForm(c, func(pt string) (*DSOAuthClientConfig, error) {
			return s.memory.GetProjectDSOAuthClient(ctx, project.ID, pt)
		}, c.Param("providerType")),
	}
	if _, err := s.memory.UpsertProjectDSOAuthClient(ctx, project.ID, c.Param("providerType"), req); err != nil {
		return redirectWithError(c, back, err)
	}
	render.RedirectAfterMutation(c.Response().Writer, c.Request(), back+"?saved=1")
	return nil
}

// uiProjectOAuthClientDelete removes the project's OAuth client for a provider
// (project-admin only), restoring the org-wide client as the resolved one.
func (s *Server) uiProjectOAuthClientDelete(c echo.Context) error {
	ctx := c.Request().Context()
	back := "/settings/oauth-clients"
	project, err := s.memory.GetCurrentProject(ctx)
	if err != nil || project == nil {
		return redirectWithError(c, back, errors.New("no active project"))
	}
	if !s.projectAdminForCaller(c, project.ID) {
		return c.NoContent(http.StatusNotFound)
	}
	if err := s.memory.DeleteProjectDSOAuthClient(ctx, project.ID, c.Param("providerType")); err != nil {
		return redirectWithError(c, back, err)
	}
	render.RedirectAfterMutation(c.Response().Writer, c.Request(), back+"?deleted=1")
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

// dsProjectOAuthClientsSubtitle builds the project clients page subtitle from
// the project name.
func dsProjectOAuthClientsSubtitle(projectName string) string {
	if projectName != "" {
		return "OAuth client credentials for " + projectName + ", used when connecting providers with OAuth. These override the organization's client for this project only."
	}
	return "OAuth client credentials, used when connecting providers with OAuth. These override the organization's client for this project only."
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

// dsConfigString reads a string value from a non-secret config map, returning ""
// when absent or not a string.
func dsConfigString(cfg map[string]any, key string) string {
	if v, ok := cfg[key].(string); ok {
		return v
	}
	return ""
}

// uiDataSourceTestConfig tests an unsaved config from the connect form and
// swaps the result into the form's result region.
func (s *Server) uiDataSourceTestConfig(c echo.Context) error {
	req := &DSTestConfigRequest{ProviderType: dsProviderTypeGoogleDrive, Config: dsConfigFromForm(c)}
	resp, err := s.memory.TestDSConfig(c.Request().Context(), req)
	renderPartial(c.Response().Writer, c.Request(), DSTestResultPartial(resp, err))
	return nil
}

// dsConnectFormPath is the google_drive credentials connect form. The chooser at
// /data-sources/new now precedes it, so a form validation error bounces back
// here (with ?err=) rather than to the chooser the user just came from.
const dsConnectFormPath = "/data-sources/new?providerType=" + dsProviderTypeGoogleDrive + "&method=credentials"

// uiDataSourceCreate handles the connect form (PRG): it creates the source and
// redirects to its detail page, or back to the form with ?err=.
func (s *Server) uiDataSourceCreate(c echo.Context) error {
	cfg := dsConfigFromForm(c)
	if msg := validateDSConfig(cfg); msg != "" {
		return redirectWithError(c, dsConnectFormPath, errors.New(msg))
	}
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return redirectWithError(c, dsConnectFormPath, errors.New("name is required"))
	}
	mode := cmp.Or(c.FormValue("sync_mode"), "manual")
	req := &DSCreateIntegrationRequest{
		Name:         name,
		ProviderType: dsProviderTypeGoogleDrive,
		SourceType:   "drive",
		Config:       cfg,
		SyncMode:     &mode,
	}
	if interval, ok := dsIntervalFromForm(c); ok {
		req.SyncIntervalMinutes = interval
	}
	item, err := s.memory.CreateDSIntegration(c.Request().Context(), req)
	if err != nil {
		return redirectWithError(c, dsConnectFormPath, err)
	}
	render.RedirectAfterMutation(c.Response().Writer, c.Request(), "/data-sources/"+url.PathEscape(item.ID)+"?created=1")
	return nil
}

// uiDataSourceDetail renders a source's detail/edit page: connection test,
// settings, and sync-job history.
func (s *Server) uiDataSourceDetail(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	src, err := s.memory.GetDSIntegration(ctx, id)
	if err != nil {
		return s.page(c, pageTitle("Data Source"), DataSourceDetailPage(nil, "", nil, nil, nil, userFacingError(err), "", nil))
	}
	providerName := dsProviderLabel(dsProviderNameMap(nil), src.ProviderType)
	if providers, perr := s.memory.ListDSProviders(ctx); perr == nil {
		providerName = dsProviderLabel(dsProviderNameMap(providers), src.ProviderType)
	} else {
		captureError(perr)
	}
	var jobs []DSSyncJob
	if resp, jerr := s.memory.ListDSSyncJobs(ctx, id, ""); jerr == nil && resp != nil {
		jobs = resp.Items
	} else {
		captureError(jerr)
	}
	var configFields, credentialFields []dsConfigField
	if schema, serr := s.memory.GetDSProviderSchema(ctx, src.ProviderType); serr == nil {
		configFields, credentialFields = dsConfigFields(schema)
	}
	flashMsg := flashFromQuery(c, []flashParam{
		{key: "created", msg: "Data source connected."},
		{key: "updated", msg: "Data source updated."},
		{key: "synced", msg: "Sync triggered."},
		{key: "cancelled", msg: "Sync cancelled."},
	})
	flashErr := flashError(c)
	return s.page(c, pageTitle(dsSourceName(*src)),
		DataSourceDetailPage(src, providerName, configFields, credentialFields, jobs, nil, flashMsg, flashErr))
}

// uiDataSourceUpdate handles the settings form (PRG).
func (s *Server) uiDataSourceUpdate(c echo.Context) error {
	id := c.Param("id")
	detailPath := "/data-sources/" + url.PathEscape(id)
	req := &DSUpdateIntegrationRequest{}
	if name := strings.TrimSpace(c.FormValue("name")); name != "" {
		req.Name = &name
	}
	if mode := c.FormValue("sync_mode"); mode != "" {
		req.SyncMode = &mode
	}
	if interval, ok := dsIntervalFromForm(c); ok {
		req.SyncIntervalMinutes = interval
	}
	cfg := dsConfigFromForm(c)
	if len(cfg) > 0 {
		if folderID, _ := cfg["folder_id"].(string); strings.TrimSpace(folderID) == "" {
			return redirectWithError(c, detailPath, errors.New("folder_id is required to change config"))
		}
		if dsHasCredentialKeys(cfg) {
			if token, _ := cfg["refresh_token"].(string); strings.TrimSpace(token) == "" {
				return redirectWithError(c, detailPath, errors.New("refresh_token is required when updating credentials"))
			}
		}
		req.Config = cfg
	}
	if _, err := s.memory.UpdateDSIntegration(c.Request().Context(), id, req); err != nil {
		return redirectWithError(c, detailPath, err)
	}
	render.RedirectAfterMutation(c.Response().Writer, c.Request(), detailPath+"?updated=1")
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
	if _, err := s.memory.TriggerDSSync(c.Request().Context(), id); err != nil {
		return redirectWithError(c, detailPath, err)
	}
	render.RedirectAfterMutation(c.Response().Writer, c.Request(), detailPath+"?synced=1")
	return nil
}

// uiDataSourceCancelSync handles cancelling a running sync job (PRG).
func (s *Server) uiDataSourceCancelSync(c echo.Context) error {
	id := c.Param("id")
	detailPath := "/data-sources/" + url.PathEscape(id)
	if _, err := s.memory.CancelDSSyncJob(c.Request().Context(), id, c.Param("jobId")); err != nil {
		return redirectWithError(c, detailPath, err)
	}
	render.RedirectAfterMutation(c.Response().Writer, c.Request(), detailPath+"?cancelled=1")
	return nil
}

// uiDataSourceDelete handles the delete form (PRG): it removes the source and
// redirects to the list with a flash.
func (s *Server) uiDataSourceDelete(c echo.Context) error {
	id := c.Param("id")
	if err := s.memory.DeleteDSIntegration(c.Request().Context(), id); err != nil {
		return redirectWithError(c, "/data-sources", err)
	}
	render.RedirectAfterMutation(c.Response().Writer, c.Request(), "/data-sources?deleted=1")
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
	}
	return s.page(c, pageTitle(documentName(*doc), "Versions"), DocumentVersionsPage(doc, versions, err))
}
