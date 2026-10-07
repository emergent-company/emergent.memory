package main

import (
	"cmp"
	"errors"
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

// uiDataSources renders the Data Sources list.
func (s *Server) uiDataSources(c echo.Context) error {
	ctx := c.Request().Context()
	items, err := s.memory.ListDSIntegrations(ctx)
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

// uiDataSourceNew renders the connect form for the google_drive provider,
// driven by the provider's config schema.
func (s *Server) uiDataSourceNew(c echo.Context) error {
	ctx := c.Request().Context()
	schema, err := s.memory.GetDSProviderSchema(ctx, dsProviderTypeGoogleDrive)
	configFields, credentialFields := dsConfigFields(schema)
	flashErr := flashError(c)
	return s.page(c, pageTitle("New Data Source"), DataSourceNewPage(configFields, credentialFields, err, flashErr))
}

// uiDataSourceTestConfig tests an unsaved config from the connect form and
// swaps the result into the form's result region.
func (s *Server) uiDataSourceTestConfig(c echo.Context) error {
	req := &DSTestConfigRequest{ProviderType: dsProviderTypeGoogleDrive, Config: dsConfigFromForm(c)}
	resp, err := s.memory.TestDSConfig(c.Request().Context(), req)
	renderPartial(c.Response().Writer, c.Request(), DSTestResultPartial(resp, err))
	return nil
}

// uiDataSourceCreate handles the connect form (PRG): it creates the source and
// redirects to its detail page, or back to the form with ?err=.
func (s *Server) uiDataSourceCreate(c echo.Context) error {
	cfg := dsConfigFromForm(c)
	if msg := validateDSConfig(cfg); msg != "" {
		return redirectWithError(c, "/data-sources/new", errors.New(msg))
	}
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return redirectWithError(c, "/data-sources/new", errors.New("name is required"))
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
		return redirectWithError(c, "/data-sources/new", err)
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
		return s.page(c, pageTitle("Data Source"), DataSourceDetailPage(nil, "", nil, nil, nil, err, "", nil))
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
		return s.page(c, pageTitle("Document"), DocumentVersionsPage(nil, nil, err))
	}
	versions, err := s.memory.ListDocumentVersions(ctx, id)
	return s.page(c, pageTitle(documentName(*doc), "Versions"), DocumentVersionsPage(doc, versions, err))
}
