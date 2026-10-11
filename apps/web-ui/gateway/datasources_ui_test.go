package main

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// testDSSchema is a google_drive-like provider schema for render tests.
func testDSSchema() *DSProviderSchema {
	return &DSProviderSchema{
		Type: "google_drive",
		Properties: map[string]any{
			"folder_id":       map[string]any{"type": "string", "description": "The Google Drive folder id to sync from."},
			"shared_drive_id": map[string]any{"type": "string"},
			"mime_types":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"recursive":       map[string]any{"type": "boolean"},
			"client_id":       map[string]any{"type": "string"},
			"client_secret":   map[string]any{"type": "string"},
			"refresh_token":   map[string]any{"type": "string"},
		},
		Required: []string{"folder_id"},
	}
}

func TestDSConfigFieldsSplitsCredentials(t *testing.T) {
	config, creds := dsConfigFields(testDSSchema())
	if len(config) != 4 {
		t.Fatalf("config fields = %+v", config)
	}
	if len(creds) != 3 {
		t.Fatalf("credential fields = %+v", creds)
	}
	if config[0].Key != "folder_id" || !config[0].Required {
		t.Errorf("first config field = %+v", config[0])
	}
	if config[1].Key != "shared_drive_id" || config[2].Key != "mime_types" || config[3].Key != "recursive" {
		t.Errorf("config order = %+v", config)
	}
	if creds[0].Key != "client_id" || creds[1].Key != "client_secret" || creds[2].Key != "refresh_token" {
		t.Errorf("credential order = %+v", creds)
	}
}

func TestRenderDataSourcesPage(t *testing.T) {
	lastSynced := "2026-09-01T10:00:00Z"
	items := []DSIntegration{
		{ID: "s1", Name: "Team Drive", ProviderType: "google_drive", Status: "active", LastSyncedAt: &lastSynced},
		{ID: "s2", Name: "Archive", ProviderType: "google_drive", Status: "paused"},
	}
	html := renderHTML(t, DataSourcesPage(items, map[string]string{"google_drive": "Google Drive"}, nil, "", nil))
	for _, want := range []string{
		"Data Sources", "Team Drive", "Archive", "Google Drive", "Active", "Paused",
		`href="/data-sources/s1"`, `data-testid="ds-source-row"`, `href="/data-sources/new"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("data sources page missing %q", want)
		}
	}

	empty := renderHTML(t, DataSourcesPage(nil, nil, nil, "", nil))
	if !strings.Contains(empty, "No data sources connected") {
		t.Error("empty state missing")
	}

	errHTML := renderHTML(t, DataSourcesPage(nil, nil, errTest, "", nil))
	if !strings.Contains(errHTML, "Failed to load data sources") {
		t.Error("error state missing")
	}

	flash := renderHTML(t, DataSourcesPage(nil, nil, nil, "Data source connected.", nil))
	if !strings.Contains(flash, "Data source connected.") {
		t.Error("flash missing")
	}
}

func TestRenderDataSourceNewPage(t *testing.T) {
	oauth := &DSProvider{Type: "google_drive", Name: "Google Drive", AuthMethods: []string{"oauth", "credentials"}}
	credsOnly := &DSProvider{Type: "github", Name: "GitHub", AuthMethods: []string{"credentials"}}

	// Chooser: one card per provider, actions driven by advertised methods.
	chooser := renderHTML(t, DataSourceNewPage([]DSProvider{*oauth, *credsOnly}, nil, nil, nil, nil, nil, nil))
	for _, want := range []string{
		"Connect a data source",
		`data-testid="ds-provider-chooser"`,
		`data-testid="ds-provider-choice"`,
		"Google Drive", "GitHub",
		`data-testid="ds-oauth-connect"`,
		"/data-sources/oauth/start?providerType=google_drive",
		"Connect with",
		`data-testid="ds-credentials-connect"`,
		"/data-sources/new?providerType=github",
	} {
		if !strings.Contains(chooser, want) {
			t.Errorf("chooser missing %q", want)
		}
	}
	if strings.Contains(chooser, `data-testid="ds-connect-form"`) {
		t.Error("chooser must not render the credentials form")
	}

	// An OAuth provider renders the confirmation panel (no credentials form).
	oauthOnly := &DSProvider{Type: "google_drive", Name: "Google Drive", AuthMethods: []string{"oauth"}}
	confirm := renderHTML(t, DataSourceNewPage([]DSProvider{*oauthOnly}, oauthOnly, nil, nil, nil, nil, nil))
	for _, want := range []string{
		"Authorize with Google Drive",
		`data-testid="ds-oauth-connect"`,
		"/data-sources/oauth/start?providerType=google_drive",
	} {
		if !strings.Contains(confirm, want) {
			t.Errorf("oauth confirmation missing %q", want)
		}
	}
	if strings.Contains(confirm, `data-testid="ds-connect-form"`) {
		t.Error("oauth confirmation must not render the credentials form")
	}

	// A credentials provider renders the write-only form from the schema.
	config, credFields := dsConfigFields(testDSSchema())
	form := renderHTML(t, DataSourceNewPage([]DSProvider{*credsOnly}, credsOnly, config, credFields, nil, nil, nil))
	for _, want := range []string{
		"Connect a data source",
		`data-testid="ds-connect-form"`,
		`name="providerType" value="github"`,
		`data-testid="ds-field-name"`,
		`data-testid="ds-field-folder_id"`,
		`name="mime_types"`,
		`name="recursive"`,
		`type="password"`,
		`data-testid="ds-field-client_secret"`,
		`hx-post="/data-sources/test-config"`,
		`id="ds-test-result"`,
		`name="sync_mode"`,
		"Folder ID",
	} {
		if !strings.Contains(form, want) {
			t.Errorf("connect form missing %q", want)
		}
	}
	// Credentials must never be prefilled.
	if strings.Contains(form, `name="refresh_token" value`) {
		t.Error("refresh_token must not be prefilled")
	}
	if strings.Contains(form, `name="client_secret" value`) {
		t.Error("client_secret must not be prefilled")
	}
}

func TestRenderDataSourceResourceSelectPage(t *testing.T) {
	src := &DSIntegration{ID: "s1", Name: "Team Drive", ProviderType: "google_drive"}
	resources := []DSResource{{ID: "f1", Name: "Reports", Type: "folder"}, {ID: "f2", Name: "Contracts"}}

	html := renderHTML(t, DataSourceResourceSelectPage(src, "Google Drive", resources, nil, "", nil))
	for _, want := range []string{
		`data-testid="ds-resource-step"`,
		"Choose a Drive folder",
		`data-testid="ds-resource-row"`,
		"Reports", "Contracts",
		`name="resource_id"`,
		`value="f1"`,
		`data-testid="ds-resource-select"`,
		"Use this folder",
		`href="/data-sources/s1"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("resource step missing %q", want)
		}
	}

	empty := renderHTML(t, DataSourceResourceSelectPage(src, "Google Drive", nil, nil, "", nil))
	if !strings.Contains(empty, `data-testid="ds-resource-empty"`) {
		t.Error("resource empty state missing")
	}
	if !strings.Contains(empty, "will be available") {
		t.Error("resource empty copy missing")
	}

	// A non-Drive provider gets the generic copy and a generic action label.
	generic := renderHTML(t, DataSourceResourceSelectPage(&DSIntegration{ID: "s2", ProviderType: "github"}, "GitHub", nil, nil, "", nil))
	if !strings.Contains(generic, "Choose a resource (repository or folder)") {
		t.Error("generic provider copy missing")
	}
	genericList := renderHTML(t, DataSourceResourceSelectPage(&DSIntegration{ID: "s2", ProviderType: "github"}, "GitHub", []DSResource{{ID: "r1", Name: "acme/repo"}}, nil, "", nil))
	if !strings.Contains(genericList, "Use this resource") {
		t.Error("generic resource action label missing")
	}

	// A failed listing renders the empty/error state, not a broken list.
	errored := renderHTML(t, DataSourceResourceSelectPage(src, "Google Drive", nil, errTest, "", nil))
	if !strings.Contains(errored, `data-testid="ds-resource-empty"`) {
		t.Error("resource error state missing")
	}
}

func TestRenderOrgOAuthClientsPage(t *testing.T) {
	providers := []DSProvider{
		{Type: "google_drive", Name: "Google Drive", AuthMethods: []string{"oauth", "credentials"}},
		{Type: "github", Name: "GitHub", AuthMethods: []string{"credentials"}},
	}
	clients := map[string]DSOAuthClientConfig{
		"google_drive": {ProviderType: "google_drive", ClientID: "cid-123", Scopes: []string{"scope.a", "scope.b"}, Strategy: "drive.file", Configured: true},
	}
	html := renderHTML(t, OrgOAuthClientsPage("org-1", "Acme", providers, clients, nil, "", nil))
	for _, want := range []string{
		"Data source OAuth clients",
		"Acme",
		"Google Drive",
		`data-testid="ds-oauth-client-form"`,
		`action="/orgs/org-1/settings/oauth-clients/google_drive"`,
		`data-testid="ds-field-client_id"`,
		`name="client_id"`,
		`value="cid-123"`,
		`data-testid="ds-field-client_secret"`,
		`type="password"`,
		`data-testid="ds-field-scopes"`,
		"scope.a",
		"scope.b",
		`data-testid="ds-field-strategy"`,
		`value="drive.file"`,
		`data-testid="ds-oauth-client-delete"`,
		"Configured",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("org clients page missing %q", want)
		}
	}
	// The client secret is write-only: never prefilled, even when configured.
	if regexp.MustCompile(`name="client_secret"[^>]*value=`).MatchString(html) {
		t.Error("client_secret must not be prefilled")
	}
	// A credentials-only provider is not rendered on the OAuth clients page.
	if strings.Contains(html, "GitHub") {
		t.Error("credentials-only provider must not appear on the OAuth clients page")
	}

	// An unconfigured provider shows no delete form.
	unconfigured := renderHTML(t, OrgOAuthClientsPage("org-1", "Acme", providers, nil, nil, "", nil))
	if !strings.Contains(unconfigured, "Not configured") {
		t.Error("unconfigured badge missing")
	}
	if strings.Contains(unconfigured, `data-testid="ds-oauth-client-delete"`) {
		t.Error("unconfigured provider must not render a delete form")
	}
}

func TestRenderOrgOAuthClientsPageStrategySelectAndPicker(t *testing.T) {
	providers := []DSProvider{
		{Type: "google_drive", Name: "Google Drive", AuthMethods: []string{"oauth"}, Strategies: []string{"drive.file", "drive.readonly"}},
	}
	clients := map[string]DSOAuthClientConfig{
		"google_drive": {
			ProviderType: "google_drive",
			ClientID:     "cid-123",
			Strategy:     "drive.file",
			Config:       map[string]any{"pickerDeveloperKey": "dev-key-1", "pickerAppId": "app-42"},
			Configured:   true,
		},
	}
	html := renderHTML(t, OrgOAuthClientsPage("org-1", "Acme", providers, clients, nil, "", nil))
	for _, want := range []string{
		`<select`,
		`name="strategy"`,
		`data-testid="ds-field-strategy"`,
		`value=""`,
		"Provider default",
		`<option value="drive.readonly"`,
		`<option value="drive.file" selected`,
		`data-testid="ds-field-picker_developer_key"`,
		`name="picker_developer_key"`,
		`value="dev-key-1"`,
		`data-testid="ds-field-picker_app_id"`,
		`name="picker_app_id"`,
		`value="app-42"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("org clients strategy/picker page missing %q", want)
		}
	}
	// The client secret stays write-only.
	if regexp.MustCompile(`name="client_secret"[^>]*value=`).MatchString(html) {
		t.Error("client_secret must not be prefilled")
	}

	// A provider without the drive.file strategy renders neither picker field.
	noPicker := renderHTML(t, OrgOAuthClientsPage("org-1", "Acme",
		[]DSProvider{{Type: "github", Name: "GitHub", AuthMethods: []string{"oauth"}, Strategies: []string{"repo"}}}, nil, nil, "", nil))
	if strings.Contains(noPicker, `data-testid="ds-field-picker_developer_key"`) || strings.Contains(noPicker, `data-testid="ds-field-picker_app_id"`) {
		t.Error("picker fields must not render without the drive.file strategy")
	}
	// A provider with no advertised strategies falls back to a text input.
	noStrategies := renderHTML(t, OrgOAuthClientsPage("org-1", "Acme",
		[]DSProvider{{Type: "google_drive", Name: "Google Drive", AuthMethods: []string{"oauth"}}}, nil, nil, "", nil))
	if strings.Contains(noStrategies, `<select`) || !strings.Contains(noStrategies, `type="text"`) {
		t.Error("a provider without strategies should keep a text strategy input")
	}
}

func TestDSStrategyOptions(t *testing.T) {
	p := DSProvider{Strategies: []string{"drive.file", "drive.readonly"}}
	// The advertised list is returned as-is when the current value is in it.
	opts := dsStrategyOptions(p, "drive.file")
	if len(opts) != 2 || opts[0] != "drive.file" {
		t.Fatalf("opts = %v", opts)
	}
	// A stored value the provider no longer advertises stays selectable.
	opts = dsStrategyOptions(p, "legacy")
	if !slices.Contains(opts, "legacy") || len(opts) != 3 {
		t.Fatalf("opts = %v", opts)
	}
	// The input slice is never mutated.
	if len(p.Strategies) != 2 {
		t.Fatalf("provider strategies mutated: %v", p.Strategies)
	}
}

func TestRenderDataSourceDetailPage(t *testing.T) {
	src := DSIntegration{ID: "s1", Name: "Team Drive", ProviderType: "google_drive", Status: "active", SyncMode: "scheduled"}
	interval := 60
	src.SyncIntervalMinutes = &interval
	phase := "enumerating"
	job := DSSyncJob{ID: "j1", IntegrationID: "s1", Status: "processing", TotalItems: 10, ProcessedItems: 4, SuccessfulItems: 3, FailedItems: 1, CurrentPhase: &phase, TriggerType: "manual", CreatedAt: "2026-09-01T10:00:00Z"}
	config, creds := dsConfigFields(testDSSchema())

	html := renderHTML(t, DataSourceDetailPage(&src, "Google Drive", config, creds, []DSSyncJob{job}, nil, "", nil))
	for _, want := range []string{
		"Team Drive", "Google Drive",
		`hx-post="/data-sources/s1/test-connection"`,
		`data-testid="ds-trigger-sync"`,
		`action="/data-sources/s1/sync"`,
		"Delete this data source?",
		`data-testid="ds-sync-job-row"`,
		"Phase: enumerating",
		"4 / 10 processed",
		`data-testid="ds-cancel-sync"`,
		`value="40"`,
		`data-testid="ds-edit-form"`,
		`name="sync_interval_minutes"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("detail page missing %q", want)
		}
	}

	// No jobs renders the explanatory note, not a broken section.
	noJobs := renderHTML(t, DataSourceDetailPage(&src, "Google Drive", config, creds, nil, nil, "", nil))
	if !strings.Contains(noJobs, "No syncs yet") {
		t.Error("empty sync history missing")
	}

	// A nil source renders the load-error state.
	errPage := renderHTML(t, DataSourceDetailPage(nil, "", nil, nil, nil, errTest, "", nil))
	if !strings.Contains(errPage, "Data source unavailable") {
		t.Error("load-error state missing")
	}
}

func TestRenderDocumentVersionsPage(t *testing.T) {
	rev := "r1"
	deletedAt := "2026-09-03T00:00:00Z"
	doc := &Document{ID: "d1", Filename: "Spec.md", Chunks: 3}
	versions := []DocumentVersion{
		{ID: "v1", Version: 1, SourceStatus: "active", SourceRevisionID: &rev, CreatedAt: "2026-09-01T00:00:00Z"},
		{ID: "v2", Version: 2, SourceStatus: "deleted_from_source", SourceDeletedAt: &deletedAt, CreatedAt: "2026-09-02T00:00:00Z"},
	}
	html := renderHTML(t, DocumentVersionsPage(doc, versions, nil))
	for _, want := range []string{
		"Version history", "v1", "v2", "Active", "Deleted from source",
		`data-testid="document-version-row"`, `data-testid="document-versions"`, "rev r1",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("versions page missing %q", want)
		}
	}

	single := renderHTML(t, DocumentVersionsPage(doc, versions[:1], nil))
	if !strings.Contains(single, "v1") || strings.Contains(single, "v2") {
		t.Error("single-version render should show only v1")
	}

	empty := renderHTML(t, DocumentVersionsPage(doc, nil, nil))
	if !strings.Contains(empty, "no recorded versions") {
		t.Error("empty versions state missing")
	}
}

func newDSUIServer(f *fakeMemory) *echo.Echo {
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: f}
	e := echo.New()
	e.GET("/data-sources", s.uiDataSources)
	e.GET("/data-sources/new", s.uiDataSourceNew)
	e.POST("/data-sources/new", s.uiDataSourceCreate)
	e.POST("/data-sources/test-config", s.uiDataSourceTestConfig)
	e.GET("/data-sources/oauth/start", s.uiDataSourceOAuthStart)
	e.GET("/data-sources/oauth/callback", s.uiDataSourceOAuthCallback)
	e.GET("/data-sources/:id", s.uiDataSourceDetail)
	e.GET("/data-sources/:id/resources", s.uiDataSourceResources)
	e.POST("/data-sources/:id/resources", s.uiDataSourceResourceSave)
	e.POST("/data-sources/:id/update", s.uiDataSourceUpdate)
	e.POST("/data-sources/:id/test-connection", s.uiDataSourceTestConnection)
	e.POST("/data-sources/:id/sync", s.uiDataSourceTriggerSync)
	e.POST("/data-sources/:id/sync-jobs/:jobId/cancel", s.uiDataSourceCancelSync)
	e.POST("/data-sources/:id/delete", s.uiDataSourceDelete)
	e.GET("/orgs/:id/settings/oauth-clients", s.uiOrgOAuthClients)
	e.POST("/orgs/:id/settings/oauth-clients/:providerType", s.uiOrgOAuthClientUpsert)
	e.POST("/orgs/:id/settings/oauth-clients/:providerType/delete", s.uiOrgOAuthClientDelete)
	e.GET("/documents/:id/versions", s.uiDocumentVersions)
	return e
}

func TestUIDataSourceRoutes(t *testing.T) {
	f := &fakeMemory{
		dsIntegrations: []DSIntegration{{ID: "s1", Name: "Team Drive", ProviderType: "google_drive", Status: "active"}},
		dsSyncJobs:     map[string][]DSSyncJob{"s1": {{ID: "j1", Status: "processing", TotalItems: 5, ProcessedItems: 2}}},
		documents:      []Document{{ID: "d1", Filename: "Spec.md"}},
		documentVersions: []DocumentVersion{
			{ID: "v1", Version: 1, SourceStatus: "active", CreatedAt: "2026-09-01T00:00:00Z"},
		},
	}
	e := newDSUIServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Team Drive") {
		t.Fatalf("list: status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/new", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("new: status=%d", rec.Code)
	}

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/s1", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Team Drive") {
		t.Fatalf("detail: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Trigger sync → 303 back to the detail with the flash.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/data-sources/s1/sync", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("sync: status=%d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/data-sources/s1?synced=1" {
		t.Errorf("sync location = %q", loc)
	}
	if f.dsLastTriggerID != "s1" {
		t.Errorf("trigger id = %q", f.dsLastTriggerID)
	}

	// Delete → 303 to the list with the flash and the fake removes the source.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/data-sources/s1/delete", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete: status=%d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/data-sources?deleted=1" {
		t.Errorf("delete location = %q", loc)
	}
	if f.dsDeletedID != "s1" {
		t.Errorf("deleted id = %q", f.dsDeletedID)
	}

	// Version history route renders.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/documents/d1/versions", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "v1") {
		t.Fatalf("versions: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUIDataSourceTestConfigPartial(t *testing.T) {
	f := &fakeMemory{dsTestConfigResp: &DSTestConnectionResponse{Success: true, Message: "connection successful"}}
	e := newDSUIServer(f)

	form := strings.NewReader("folder_id=f1&refresh_token=tok")
	req := httptest.NewRequest(http.MethodPost, "/data-sources/test-config", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.dsLastTestConfig == nil || f.dsLastTestConfig.Config["folder_id"] != "f1" {
		t.Fatalf("test config req = %+v", f.dsLastTestConfig)
	}
	if !strings.Contains(rec.Body.String(), "connection successful") {
		t.Errorf("partial body = %s", rec.Body.String())
	}
}

func TestUIDataSourceTestConfigFailurePartial(t *testing.T) {
	f := &fakeMemory{dsTestConfigResp: &DSTestConnectionResponse{Success: false, Message: "invalid credentials"}}
	e := newDSUIServer(f)

	form := strings.NewReader("folder_id=f1&refresh_token=tok")
	req := httptest.NewRequest(http.MethodPost, "/data-sources/test-config", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), "invalid credentials") {
		t.Errorf("failure partial = %s", rec.Body.String())
	}
}

// --- OAuth connect (§5.1/5.2) ---

func TestUIDataSourceOAuthStart(t *testing.T) {
	f := &fakeMemory{dsOAuthAuthorizeResp: &DSOAuthAuthorizeResponse{AuthorizeURL: "https://accounts.example/authorize?state=s1"}}
	e := newDSUIServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/oauth/start?providerType=google_drive", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "https://accounts.example/authorize?state=s1" {
		t.Errorf("location = %q", loc)
	}
	if f.dsLastOAuthProvider != "google_drive" {
		t.Errorf("provider = %q", f.dsLastOAuthProvider)
	}

	// Missing providerType redirects back to the chooser with an error flash.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/oauth/start", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("missing provider status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/data-sources/new?err=") {
		t.Errorf("missing provider location = %q", loc)
	}

	// A backend failure redirects back to the chooser.
	f.dsOAuthAuthorizeErr = errTest
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/oauth/start?providerType=google_drive", nil))
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/data-sources/new?err=") {
		t.Errorf("failure location = %q", loc)
	}
}

func TestUIDataSourceOAuthStartMissingClientRendersInline(t *testing.T) {
	f := &fakeMemory{
		dsProviders: []DSProvider{{Type: "github", Name: "GitHub", AuthMethods: []string{"oauth"}}},
		dsOAuthAuthorizeErr: &memoryHTTPError{
			Status:  http.StatusUnprocessableEntity,
			Code:    "validation_error",
			Message: "no OAuth client configured for provider github (configure one under Organization settings → Data source OAuth clients)",
		},
	}
	e := newDSUIServer(f)

	req := httptest.NewRequest(http.MethodGet, "/data-sources/oauth/start?providerType=github", nil)
	req = req.WithContext(withSessionContext(req.Context(), &sessionContext{OrgID: "org-1"}))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (inline render, not redirect); body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("Location = %q, want no redirect", loc)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="ds-oauth-client-missing"`,
		"No OAuth client is configured for GitHub",
		"Organization settings",
		`data-testid="ds-oauth-client-settings-link"`,
		"org-1/settings/oauth-clients",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing-client page missing %q", want)
		}
	}
	// The internal backend prefix must never reach the page.
	if strings.Contains(body, "memory 422") || strings.Contains(body, "validation_error") {
		t.Errorf("raw backend prefix leaked into page: %s", body)
	}
}

func TestUIDataSourceOAuthStartMissingClientNoOrg(t *testing.T) {
	f := &fakeMemory{
		dsProviders: []DSProvider{{Type: "github", Name: "GitHub", AuthMethods: []string{"oauth"}}},
		dsOAuthAuthorizeErr: &memoryHTTPError{
			Status:  http.StatusUnprocessableEntity,
			Code:    "validation_error",
			Message: "no OAuth client configured for provider github (configure one under Organization settings → Data source OAuth clients)",
		},
	}
	e := newDSUIServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/oauth/start?providerType=github", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `data-testid="ds-oauth-client-missing"`) {
		t.Error("missing-client alert missing when org is unknown")
	}
	if strings.Contains(rec.Body.String(), `data-testid="ds-oauth-client-settings-link"`) {
		t.Error("settings link must be omitted when the active org is unknown")
	}
}

func TestUIDataSourceOAuthStartFailureStripsBackendPrefix(t *testing.T) {
	f := &fakeMemory{
		dsOAuthAuthorizeErr: &memoryHTTPError{
			Status:  http.StatusUnprocessableEntity,
			Code:    "validation_error",
			Message: "some other validation message",
		},
	}
	e := newDSUIServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/oauth/start?providerType=google_drive", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 redirect", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/data-sources/new?err=") {
		t.Fatalf("location = %q, want redirect back to the chooser", loc)
	}
	// The surfaced message is the backend Message, never the internal prefix.
	if strings.Contains(loc, "memory 422") || strings.Contains(loc, "validation_error") {
		t.Errorf("raw backend prefix leaked into redirect: %q", loc)
	}
	if !strings.Contains(loc, "some+other+validation+message") {
		t.Errorf("location = %q, want the sanitized backend message", loc)
	}
}

func TestUIDataSourceOAuthCallback(t *testing.T) {
	e := newDSUIServer(&fakeMemory{})

	// connected → redirect to the new source's detail with the created flash.
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/oauth/callback?connected=s1", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("connected status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/data-sources/s1?created=1" {
		t.Errorf("connected location = %q", loc)
	}

	// oauth_error → redirect to the list with a real error message.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/oauth/callback?oauth_error=denied", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("oauth_error status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/data-sources?err=") {
		t.Errorf("oauth_error location = %q", loc)
	}

	// Neither param → plain redirect to the list.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/oauth/callback", nil))
	if loc := rec.Header().Get("Location"); loc != "/data-sources" {
		t.Errorf("bare callback location = %q", loc)
	}

	// The memory callback lands on /data-sources?connected=... directly.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources?connected=s1", nil))
	if loc := rec.Header().Get("Location"); loc != "/data-sources/s1?created=1" {
		t.Errorf("delegated callback location = %q", loc)
	}
}

func TestUIDataSourceResourcesRoute(t *testing.T) {
	f := &fakeMemory{
		dsIntegrations: []DSIntegration{{ID: "s1", Name: "Team Drive", ProviderType: "google_drive"}},
		dsProviders:    []DSProvider{{Type: "google_drive", Name: "Google Drive", AuthMethods: []string{"oauth"}}},
		dsResources:    []DSResource{{ID: "f1", Name: "Reports", Type: "folder"}},
	}
	e := newDSUIServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/s1/resources", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="ds-resource-step"`) || !strings.Contains(body, "Choose a Drive folder") {
		t.Errorf("resource step body = %s", body)
	}
	if !strings.Contains(body, "Reports") {
		t.Errorf("resource row missing: %s", body)
	}
}

func TestUIDataSourceResourceSave(t *testing.T) {
	// google_drive maps the picked resource to folder_id and redirects to ?updated=1.
	f := &fakeMemory{dsIntegrations: []DSIntegration{{ID: "s1", Name: "Team Drive", ProviderType: "google_drive"}}}
	e := newDSUIServer(f)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/data-sources/s1/resources", strings.NewReader("resource_id=f1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/data-sources/s1?updated=1" {
		t.Errorf("location = %q", loc)
	}
	if f.dsSavedResourceID != "s1" || f.dsSavedResourceCfg["folder_id"] != "f1" {
		t.Fatalf("save = %q %+v", f.dsSavedResourceID, f.dsSavedResourceCfg)
	}

	// github maps the picked resource to repo.
	f = &fakeMemory{dsIntegrations: []DSIntegration{{ID: "s2", ProviderType: "github"}}}
	e = newDSUIServer(f)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/data-sources/s2/resources", strings.NewReader("resource_id=acme/repo"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if f.dsSavedResourceCfg["repo"] != "acme/repo" {
		t.Fatalf("github save cfg = %+v", f.dsSavedResourceCfg)
	}

	// A missing resource id redirects back with an error and never reaches memory.
	f = &fakeMemory{dsIntegrations: []DSIntegration{{ID: "s1", ProviderType: "google_drive"}}}
	e = newDSUIServer(f)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/data-sources/s1/resources", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("validation status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/data-sources/s1/resources?err=") {
		t.Errorf("validation location = %q", loc)
	}
	if f.dsSavedResourceID != "" {
		t.Error("memory must not be called without a resource id")
	}

	// A backend failure redirects back with an error.
	f = &fakeMemory{
		dsIntegrations: []DSIntegration{{ID: "s1", ProviderType: "google_drive"}},
		dsUpdateErr:    errTest,
	}
	e = newDSUIServer(f)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/data-sources/s1/resources", strings.NewReader("resource_id=f1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/data-sources/s1/resources?err=") {
		t.Errorf("failure location = %q", loc)
	}
}

// --- org OAuth clients (§5.4) ---
func orgOAuthAdminFake() *fakeMemory {
	return &fakeMemory{
		orgs:            []Org{{ID: "o1", Name: "Acme"}},
		orgsAndProjects: []OrgWithProjectsDto{{ID: "o1", Name: "Acme", Role: "org_admin"}},
		dsProviders:     []DSProvider{{Type: "google_drive", Name: "Google Drive", AuthMethods: []string{"oauth", "credentials"}}},
		dsOrgOAuthClients: []DSOAuthClientConfig{
			{ProviderType: "google_drive", ClientID: "cid-123", Strategy: "drive.file", Config: map[string]any{"unmanaged": "keep-me"}, Configured: true},
		},
	}
}

func TestUIOrgOAuthClientsPageAdminAndNonAdmin(t *testing.T) {
	// Admin sees the page.
	e := newDSUIServer(orgOAuthAdminFake())
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/orgs/o1/settings/oauth-clients", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("admin status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "Google Drive") || !strings.Contains(body, "cid-123") {
		t.Errorf("admin body = %s", body)
	}

	// A non-admin gets a 404 (hide, fail closed).
	e = newDSUIServer(&fakeMemory{orgsAndProjects: []OrgWithProjectsDto{{ID: "o1", Name: "Acme", Role: "org_member"}}})
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/orgs/o1/settings/oauth-clients", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("non-admin status = %d, want 404", rec.Code)
	}
}

func TestUIOrgOAuthClientUpsertValidationAndSuccess(t *testing.T) {
	f := orgOAuthAdminFake()
	e := newDSUIServer(f)

	// Missing client id → 303 back with an error, memory untouched.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/orgs/o1/settings/oauth-clients/google_drive", strings.NewReader("client_secret=sec"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("validation status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/orgs/o1/settings/oauth-clients?err=") {
		t.Errorf("validation location = %q", loc)
	}
	if f.dsUpsertedOAuthReq != nil {
		t.Error("memory must not be called on validation failure")
	}

	// Success → 303 with the saved flash, request forwarded with the picker
	// config merged over the stored (unmanaged) keys.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/orgs/o1/settings/oauth-clients/google_drive", strings.NewReader("client_id=cid&client_secret=sec&scopes=scope.a%2Cscope.b&strategy=drive.file&picker_developer_key=dev-key&picker_app_id=app-7"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("success status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/orgs/o1/settings/oauth-clients?saved=1" {
		t.Errorf("success location = %q", loc)
	}
	got := f.dsUpsertedOAuthReq
	if got == nil || got.ClientID != "cid" || got.ClientSecret != "sec" || got.Strategy != "drive.file" {
		t.Fatalf("upsert req = %+v", got)
	}
	if len(got.Scopes) != 2 || got.Scopes[0] != "scope.a" || got.Scopes[1] != "scope.b" {
		t.Errorf("scopes = %v", got.Scopes)
	}
	if got.Config["pickerDeveloperKey"] != "dev-key" || got.Config["pickerAppId"] != "app-7" {
		t.Errorf("picker config = %+v", got.Config)
	}
	if got.Config["unmanaged"] != "keep-me" {
		t.Errorf("stored unmanaged config key not preserved: %+v", got.Config)
	}
	if f.dsUpsertedOAuthOrg != "o1" || f.dsUpsertedOAuthProv != "google_drive" {
		t.Errorf("upsert target = %q/%q", f.dsUpsertedOAuthOrg, f.dsUpsertedOAuthProv)
	}

	// Blank picker fields are dropped while unmanaged stored keys survive.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/orgs/o1/settings/oauth-clients/google_drive", strings.NewReader("client_id=cid&picker_developer_key="))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if got := f.dsUpsertedOAuthReq; got == nil || got.Config["unmanaged"] != "keep-me" {
		t.Fatalf("config = %+v", got)
	} else if _, ok := got.Config["pickerDeveloperKey"]; ok {
		t.Errorf("blank picker key must be dropped: %+v", got.Config)
	}
}

func TestUIOrgOAuthClientDelete(t *testing.T) {
	f := orgOAuthAdminFake()
	e := newDSUIServer(f)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/orgs/o1/settings/oauth-clients/google_drive/delete", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/orgs/o1/settings/oauth-clients?deleted=1" {
		t.Errorf("delete location = %q", loc)
	}
	if f.dsDeletedOAuthOrg != "o1" || f.dsDeletedOAuthProv != "google_drive" {
		t.Errorf("delete target = %q/%q", f.dsDeletedOAuthOrg, f.dsDeletedOAuthProv)
	}
}
