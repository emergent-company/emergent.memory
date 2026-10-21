package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
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
	config, creds := dsConfigFields(testDSSchema(), nil)
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

// TestDSConfigFieldsPrefillsNonCredential asserts an edit round-trip: a source's
// non-secret config values bind onto the config fields (string → Value, array →
// newline-joined Value, boolean → Checked) while credential fields stay blank.
func TestDSConfigFieldsPrefillsNonCredential(t *testing.T) {
	cfg := map[string]any{
		"folder_id":     "f1",
		"mime_types":    []any{"application/pdf", "text/plain"},
		"recursive":     true,
		"refresh_token": "must-not-leak",
	}
	config, creds := dsConfigFields(testDSSchema(), cfg)
	if len(config) != 4 || len(creds) != 3 {
		t.Fatalf("config=%+v creds=%+v", config, creds)
	}
	if config[0].Value != "f1" {
		t.Errorf("folder_id value = %q", config[0].Value)
	}
	if config[2].Value != "application/pdf\ntext/plain" {
		t.Errorf("mime_types value = %q", config[2].Value)
	}
	if !config[3].Checked {
		t.Error("recursive should be checked")
	}
	for _, c := range creds {
		if c.Value != "" || c.Checked {
			t.Errorf("credential field %q must stay blank: %+v", c.Key, c)
		}
	}
}

func TestRenderDataSourcesPage(t *testing.T) {
	lastSynced := "2026-09-01T10:00:00Z"
	items := []DSIntegration{
		{ID: "s1", Name: "Team Drive", ProviderType: "google_drive", Status: "active", SyncMode: "scheduled", LastSyncedAt: &lastSynced},
		{ID: "s2", Name: "Archive", ProviderType: "google_drive", Status: "paused", SyncMode: "manual"},
	}
	html := renderHTML(t, DataSourcesPage(items, map[string]string{"google_drive": "Google Drive"}, nil, "", nil))
	for _, want := range []string{
		"Data Sources", "Team Drive", "Archive", "Google Drive", "Active", "Paused",
		"Scheduled", "Manual",
		`href="/data-sources/s1"`, `data-testid="ds-source-row"`, `data-testid="ds-source-table"`,
		`href="/data-sources/new"`,
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

// TestRenderDataSourcesPageHasNoCredentialShortcut asserts credential
// configuration does not leak onto the connections table: the page is a plain
// data-source table with an add CTA, and the old OAuth-clients header shortcut
// is gone.
func TestRenderDataSourcesPageHasNoCredentialShortcut(t *testing.T) {
	html := renderHTML(t, DataSourcesPage([]DSIntegration{{ID: "s1", Name: "Team Drive", ProviderType: "google_drive"}}, map[string]string{"google_drive": "Google Drive"}, nil, "", nil))
	for _, unwanted := range []string{`data-testid="ds-oauth-clients-link"`, `href="/settings/oauth-clients"`, `href="/settings/credentials"`} {
		if strings.Contains(html, unwanted) {
			t.Errorf("data sources page must not show credential configuration (%q)", unwanted)
		}
	}
}

func TestRenderDataSourceNewPage(t *testing.T) {
	oauth := &DSProvider{Type: "google_drive", Name: "Google Drive", AuthMethods: []string{"oauth", "credentials"}}
	credsOnly := &DSProvider{Type: "github", Name: "GitHub", AuthMethods: []string{"credentials"}}

	// Chooser: one card per provider, actions driven by advertised methods.
	// The OAuth action opts out of hx-boost: /data-sources/oauth/start redirects
	// to the provider's cross-origin authorization server, which a boosted
	// fetch (mode: same-origin) cannot follow.
	chooser := renderHTML(t, DataSourceNewPage([]DSProvider{*oauth, *credsOnly}, nil, nil, nil, nil, nil, nil))
	for _, want := range []string{
		"Connect a data source",
		`data-testid="ds-provider-chooser"`,
		`data-testid="ds-provider-choice"`,
		"Google Drive", "GitHub",
		`data-testid="ds-oauth-connect"`,
		"/data-sources/oauth/start?providerType=google_drive",
		`hx-boost="false"`,
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
		`hx-boost="false"`,
	} {
		if !strings.Contains(confirm, want) {
			t.Errorf("oauth confirmation missing %q", want)
		}
	}
	if strings.Contains(confirm, `data-testid="ds-connect-form"`) {
		t.Error("oauth confirmation must not render the credentials form")
	}

	// A credentials provider renders the write-only form from the schema.
	config, credFields := dsConfigFields(testDSSchema(), nil)
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

	html := renderHTML(t, DataSourceResourceSelectPage(src, "Google Drive", "", nil, "", nil))
	for _, want := range []string{
		`data-testid="ds-resource-step"`,
		"Choose a Drive folder",
		`href="/data-sources/s1"`,
		`data-testid="ds-resource-search"`,
		`data-testid="ds-resource-search-submit"`,
		// Native GET navigation, not a boosted form submit (see the templ),
		// and no action attribute so the scoped path is preserved.
		`<form method="get" class="mt-4 flex items-center gap-2" hx-boost="false">`,
		// The list is deferred, not rendered inline: a loading placeholder that
		// fetches the list partial after first paint.
		`data-testid="ds-resource-loading"`,
		`hx-get="/data-sources/s1/resources/list"`,
		`hx-trigger="load"`,
		`hx-target="this"`,
		`hx-swap="outerHTML"`,
		"Loading folders from Google Drive",
		`href="/data-sources/s1/resources?static=1"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("resource step shell missing %q", want)
		}
	}
	if strings.Contains(html, `data-testid="ds-resource-row"`) {
		t.Error("page shell must not render resource rows inline")
	}

	// The typed filter is echoed back into the search field so the reload keeps
	// it visible, and threaded into the deferred list URL.
	filtered := renderHTML(t, DataSourceResourceSelectPage(src, "Google Drive", "Reports", nil, "", nil))
	if !strings.Contains(filtered, `name="q" value="Reports"`) {
		t.Error("search field does not echo the applied filter")
	}
	if !strings.Contains(filtered, `hx-get="/data-sources/s1/resources/list?q=Reports"`) {
		t.Error("deferred list URL does not carry the filter")
	}

	// A non-Drive provider gets the generic copy.
	generic := renderHTML(t, DataSourceResourceSelectPage(&DSIntegration{ID: "s2", ProviderType: "github"}, "GitHub", "", nil, "", nil))
	if !strings.Contains(generic, "Choose a resource (repository or folder)") {
		t.Error("generic provider copy missing")
	}

	// A nil source renders the load-error state.
	errPage := renderHTML(t, DataSourceResourceSelectPage(nil, "", "", errTest, "", nil))
	if !strings.Contains(errPage, "Data source unavailable") {
		t.Error("load-error state missing")
	}
}

// TestRenderDataSourceResourceSelectPageStaticSearchKeepsMode asserts the
// no-JS shell's GET search form carries a hidden `static` field so a filtered
// submit stays on the server-rendered path, while the deferred (JS) shell does
// not — the flag is threaded through dsResourceSelectShell.
func TestRenderDataSourceResourceSelectPageStaticSearchKeepsMode(t *testing.T) {
	src := &DSIntegration{ID: "s1", Name: "Team Drive", ProviderType: "google_drive"}

	static := renderHTML(t, DataSourceResourceSelectPageStatic(src, "Google Drive", "", nil, false, nil, false, "", nil))
	if !strings.Contains(static, `name="static" value="1"`) {
		t.Error("static shell search form must carry the hidden static field")
	}

	deferred := renderHTML(t, DataSourceResourceSelectPage(src, "Google Drive", "", nil, "", nil))
	if strings.Contains(deferred, `name="static" value="1"`) {
		t.Error("deferred shell search form must not carry the hidden static field")
	}
}

func TestRenderDataSourceResourceList(t *testing.T) {
	src := &DSIntegration{ID: "s1", Name: "Team Drive", ProviderType: "google_drive"}
	resources := []DSResource{{ID: "f1", Name: "Reports", Type: "folder"}, {ID: "f2", Name: "Contracts"}}

	html := renderHTML(t, DataSourceResourceList(src, "Google Drive", resources, false, "", nil, false))
	for _, want := range []string{
		`data-testid="ds-resource-row"`,
		"Reports", "Contracts",
		`name="resource_id"`,
		`value="f1"`,
		`data-testid="ds-resource-select"`,
		"Use this folder",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("resource list missing %q", want)
		}
	}
	if strings.Contains(html, `data-testid="ds-resource-truncated"`) {
		t.Error("truncation note rendered for a complete (non-truncated) listing")
	}

	// A truncated listing renders the note; the flag — not the item count —
	// controls it.
	truncated := renderHTML(t, DataSourceResourceList(src, "Google Drive", resources, true, "", nil, false))
	if !strings.Contains(truncated, `data-testid="ds-resource-truncated"`) {
		t.Error("truncation note missing for a truncated listing")
	}
	if !strings.Contains(truncated, "search by name") {
		t.Error("truncation note missing its search-by-name hint")
	}

	empty := renderHTML(t, DataSourceResourceList(src, "Google Drive", nil, false, "", nil, false))
	if !strings.Contains(empty, `data-testid="ds-resource-empty"`) {
		t.Error("resource empty state missing")
	}
	if !strings.Contains(empty, "No resources were returned for Google Drive") {
		t.Error("resource empty copy missing")
	}
	if !strings.Contains(empty, `data-testid="ds-resource-retry"`) {
		t.Error("resource retry link missing")
	}
	// The retry re-issues the list fetch client-side, not a full page reload.
	if !strings.Contains(empty, `hx-get="/data-sources/s1/resources/list"`) || !strings.Contains(empty, `hx-target="closest #ds-resource-list-root"`) {
		t.Errorf("retry must be an HTMX list re-fetch: %s", empty)
	}
	// It must also carry a real href: on the ?static=1 no-JS path hx-get never
	// runs, and a bare button would be dead.
	if !strings.Contains(empty, `href="/data-sources/s1/resources?static=1"`) {
		t.Errorf("retry must keep an href for the no-JS path: %s", empty)
	}

	// A non-Drive provider gets a generic resource action label.
	genericList := renderHTML(t, DataSourceResourceList(&DSIntegration{ID: "s2", ProviderType: "github"}, "GitHub", []DSResource{{ID: "r1", Name: "acme/repo"}}, false, "", nil, false))
	if !strings.Contains(genericList, "Use this resource") {
		t.Error("generic resource action label missing")
	}

	// A failed listing renders the empty/error state, not a broken list.
	errored := renderHTML(t, DataSourceResourceList(src, "Google Drive", nil, false, "", errTest, false))
	if !strings.Contains(errored, `data-testid="ds-resource-empty"`) {
		t.Error("resource error state missing")
	}
}

func TestRenderDataSourceResourceSelectPageDriveFileEmpty(t *testing.T) {
	src := &DSIntegration{ID: "s1", Name: "Team Drive", ProviderType: "google_drive"}
	html := renderHTML(t, DataSourceResourceList(src, "Google Drive", nil, false, "", nil, true))
	for _, want := range []string{
		`data-testid="ds-resource-drivefile"`,
		"drive.file scope",
		"Google Picker support is not available yet",
		`href="/settings/credentials"`,
		`data-testid="ds-resource-drivefile-credentials"`,
		"drive.readonly",
		"Settings → Credentials (project)",
		"Organization Settings",
		"reconnect this source",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("drive.file empty state missing %q", want)
		}
	}
	if strings.Contains(html, `data-testid="ds-resource-empty"`) {
		t.Error("drive.file empty state must not render the bare ds-resource-empty state")
	}
}

func TestRenderDataSourceResourceSelectPageNonDriveFileEmpty(t *testing.T) {
	src := &DSIntegration{ID: "s1", Name: "Team Drive", ProviderType: "google_drive"}
	html := renderHTML(t, DataSourceResourceList(src, "Google Drive", nil, false, "", nil, false))
	if !strings.Contains(html, `data-testid="ds-resource-empty"`) {
		t.Error("non-drive.file empty listing must keep the plain empty state")
	}
	if strings.Contains(html, `data-testid="ds-resource-drivefile"`) {
		t.Error("non-drive.file empty listing must not render the drive.file notice")
	}
}

func TestDSDriveFileScope(t *testing.T) {
	// The strategy is resolved server-side and carried on the single-source
	// read, so this helper only interprets the field.
	driveFile := &DSIntegration{ID: "s1", ProjectID: "p1", ProviderType: "google_drive", OAuthStrategy: "drive.file"}
	if !dsDriveFileScope(driveFile) {
		t.Error("drive.file strategy should be reported as drive.file scope")
	}

	// Empty strategy → not drive.file (the provider default can list).
	inheritedDefault := &DSIntegration{ID: "s1", ProjectID: "p1", ProviderType: "google_drive"}
	if dsDriveFileScope(inheritedDefault) {
		t.Error("empty strategy should not be reported as drive.file scope")
	}

	// drive.readonly → not drive.file.
	readonly := &DSIntegration{ID: "s1", ProjectID: "p1", ProviderType: "google_drive", OAuthStrategy: "drive.readonly"}
	if dsDriveFileScope(readonly) {
		t.Error("drive.readonly should not be reported as drive.file scope")
	}

	// Non-drive provider → not drive.file.
	if dsDriveFileScope(&DSIntegration{ID: "s2", ProjectID: "p1", ProviderType: "github", OAuthStrategy: "drive.file"}) {
		t.Error("github source should not be reported as drive.file scope")
	}

	// A nil source is not drive.file.
	if dsDriveFileScope(nil) {
		t.Error("nil source should not be reported as drive.file scope")
	}
}

func TestDSDriveFileScopePrefersGranted(t *testing.T) {
	// The granted strategy wins over the configured one: a source authorized
	// under drive.file still cannot list the drive even after the org strategy
	// changed to drive.readonly.
	grantedFile := &DSIntegration{ID: "s1", ProjectID: "p1", ProviderType: "google_drive", OAuthStrategy: "drive.readonly", OAuthGrantedStrategy: "drive.file"}
	if !dsDriveFileScope(grantedFile) {
		t.Error("granted drive.file should win over configured drive.readonly")
	}

	// The reverse: granted drive.readonly is not drive.file even when the
	// configured strategy drifted to drive.file.
	grantedReadonly := &DSIntegration{ID: "s1", ProjectID: "p1", ProviderType: "google_drive", OAuthStrategy: "drive.file", OAuthGrantedStrategy: "drive.readonly"}
	if dsDriveFileScope(grantedReadonly) {
		t.Error("granted drive.readonly should not be reported as drive.file even when configured drive.file")
	}

	// Both empty → fall back to the configured strategy (older sources).
	fallback := &DSIntegration{ID: "s1", ProjectID: "p1", ProviderType: "google_drive", OAuthStrategy: "drive.file"}
	if !dsDriveFileScope(fallback) {
		t.Error("empty granted strategy should fall back to the configured drive.file strategy")
	}
}

func TestRenderDataSourceResourceListPrefersGrantedScope(t *testing.T) {
	// Granted drive.file with configured drive.readonly still renders the
	// drive.file notice (the granted scope is what the token can actually do).
	grantedFile := &DSIntegration{ID: "s1", Name: "Team Drive", ProviderType: "google_drive", OAuthStrategy: "drive.readonly", OAuthGrantedStrategy: "drive.file"}
	html := renderHTML(t, DataSourceResourceList(grantedFile, "Google Drive", nil, false, "", nil, dsDriveFileScope(grantedFile)))
	if !strings.Contains(html, `data-testid="ds-resource-drivefile"`) {
		t.Error("granted drive.file must render the drive.file notice even when configured drive.readonly")
	}
	if strings.Contains(html, `data-testid="ds-resource-empty"`) {
		t.Error("granted drive.file must not render the bare empty state")
	}

	// Granted drive.readonly with configured drive.file does not render the notice.
	grantedReadonly := &DSIntegration{ID: "s2", Name: "Team Drive", ProviderType: "google_drive", OAuthStrategy: "drive.file", OAuthGrantedStrategy: "drive.readonly"}
	html = renderHTML(t, DataSourceResourceList(grantedReadonly, "Google Drive", nil, false, "", nil, dsDriveFileScope(grantedReadonly)))
	if strings.Contains(html, `data-testid="ds-resource-drivefile"`) {
		t.Error("granted drive.readonly must not render the drive.file notice even when configured drive.file")
	}
	if !strings.Contains(html, `data-testid="ds-resource-empty"`) {
		t.Error("granted drive.readonly empty listing must keep the plain empty state")
	}

	// Both empty falls back to the configured drive.file strategy.
	fallback := &DSIntegration{ID: "s3", Name: "Team Drive", ProviderType: "google_drive", OAuthStrategy: "drive.file"}
	html = renderHTML(t, DataSourceResourceList(fallback, "Google Drive", nil, false, "", nil, dsDriveFileScope(fallback)))
	if !strings.Contains(html, `data-testid="ds-resource-drivefile"`) {
		t.Error("empty granted strategy must fall back to the configured drive.file strategy")
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
	config, creds := dsConfigFields(testDSSchema(), nil)

	html := renderHTML(t, DataSourceDetailPage(&src, "Google Drive", config, creds, []DSSyncJob{job}, nil, nil, "", nil))
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
	noJobs := renderHTML(t, DataSourceDetailPage(&src, "Google Drive", config, creds, nil, nil, nil, "", nil))
	if !strings.Contains(noJobs, "No syncs yet") {
		t.Error("empty sync history missing")
	}

	// A nil source renders the load-error state.
	errPage := renderHTML(t, DataSourceDetailPage(nil, "", nil, nil, nil, errTest, nil, "", nil))
	if !strings.Contains(errPage, "Data source unavailable") {
		t.Error("load-error state missing")
	}
}

// TestRenderDataSourceDetailPageResourceBanner asserts a source whose config is
// missing its sync target shows the "finish connecting" banner linking to the
// resource step, and a complete source does not.
func TestRenderDataSourceDetailPageResourceBanner(t *testing.T) {
	config, creds := dsConfigFields(testDSSchema(), nil)

	incomplete := &DSIntegration{ID: "s1", Name: "Team Drive", ProviderType: "google_drive"}
	html := renderHTML(t, DataSourceDetailPage(incomplete, "Google Drive", config, creds, nil, nil, dsResourcePromptFor(incomplete), "", nil))
	if !strings.Contains(html, `data-testid="ds-resource-needed"`) {
		t.Error("resource-needed banner missing")
	}
	if !strings.Contains(html, `data-testid="ds-resource-needed-link"`) {
		t.Error("resource-needed link missing")
	}
	if !strings.Contains(html, "/data-sources/s1/resources") {
		t.Error("resource-needed link href missing")
	}
	// The trigger-sync form must not be offered while the sync target is missing.
	if strings.Contains(html, `data-testid="ds-trigger-sync"`) {
		t.Error("trigger-sync form must not render for an incomplete source")
	}
	if strings.Contains(html, `action="/data-sources/s1/sync"`) {
		t.Error("sync action must not render for an incomplete source")
	}

	complete := &DSIntegration{ID: "s2", Name: "Team Drive", ProviderType: "google_drive", Config: map[string]any{"folder_id": "f1"}}
	html = renderHTML(t, DataSourceDetailPage(complete, "Google Drive", config, creds, nil, nil, dsResourcePromptFor(complete), "", nil))
	if strings.Contains(html, `data-testid="ds-resource-needed"`) {
		t.Error("resource-needed banner must not render for a complete source")
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
	e.POST("/data-sources/repos", s.uiDataSourceRepos)
	e.POST("/data-sources/refs", s.uiDataSourceRefs)
	e.GET("/data-sources/oauth/start", s.uiDataSourceOAuthStart)
	e.GET("/data-sources/oauth/callback", s.uiDataSourceOAuthCallback)
	e.GET("/data-sources/:id", s.uiDataSourceDetail)
	e.GET("/data-sources/:id/resources", s.uiDataSourceResources)
	e.GET("/data-sources/:id/resources/list", s.uiDataSourceResourceList)
	e.POST("/data-sources/:id/resources", s.uiDataSourceResourceSave)
	e.POST("/data-sources/:id/update", s.uiDataSourceUpdate)
	e.POST("/data-sources/:id/test-connection", s.uiDataSourceTestConnection)
	e.POST("/data-sources/:id/sync", s.uiDataSourceTriggerSync)
	e.POST("/data-sources/:id/sync-jobs/:jobId/cancel", s.uiDataSourceCancelSync)
	e.POST("/data-sources/:id/delete", s.uiDataSourceDelete)
	e.GET("/orgs/:id/settings/oauth-clients", s.uiOrgOAuthClients)
	e.POST("/orgs/:id/settings/oauth-clients/:providerType", s.uiOrgOAuthClientUpsert)
	e.POST("/orgs/:id/settings/oauth-clients/:providerType/delete", s.uiOrgOAuthClientDelete)
	e.GET("/settings/credentials", s.uiProjectCredentials)
	e.GET("/settings/credentials/:providerType", s.uiProjectCredential)
	e.POST("/settings/credentials/:providerType", s.uiProjectOAuthClientUpsert)
	e.POST("/settings/credentials/:providerType/delete", s.uiProjectOAuthClientDelete)
	e.GET("/settings/oauth-clients", s.uiProjectOAuthClientsRedirect)
	e.GET("/documents/:id/versions", s.uiDocumentVersions)
	return e
}

func TestUIDataSourceRoutes(t *testing.T) {
	f := &fakeMemory{
		dsIntegrations: []DSIntegration{{ID: "s1", Name: "Team Drive", ProviderType: "google_drive", Status: "active", Config: map[string]any{"folder_id": "f1"}}},
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

func TestUIDataSourceTriggerSyncIncomplete(t *testing.T) {
	f := &fakeMemory{
		dsIntegrations: []DSIntegration{{ID: "s1", Name: "Team Drive", ProviderType: "google_drive"}},
	}
	e := newDSUIServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/data-sources/s1/sync", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("sync (incomplete): status=%d body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "/data-sources/s1?err=") {
		t.Errorf("sync (incomplete) location = %q, want detail page with error flash", loc)
	}
	if f.dsLastTriggerID != "" {
		t.Errorf("TriggerDSSync called for an incomplete source, id = %q", f.dsLastTriggerID)
	}
}

func TestUIDataSourceTestConfigPartial(t *testing.T) {
	f := &fakeMemory{dsTestConfigResp: &DSTestConnectionResponse{Success: true, Message: "connection successful"}}
	e := newDSUIServer(f)

	form := strings.NewReader("providerType=google_drive&folder_id=f1")
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

	form := strings.NewReader("providerType=google_drive&folder_id=f1")
	req := httptest.NewRequest(http.MethodPost, "/data-sources/test-config", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), "invalid credentials") {
		t.Errorf("failure partial = %s", rec.Body.String())
	}
}

// githubSchema is the connect form's GitHub contract as the server now serves it:
// config fields plus the write-only access token, with the two picker hints.
func githubSchema() *DSProviderSchema {
	return &DSProviderSchema{
		Type: "github",
		Properties: map[string]any{
			"repo":         map[string]any{"type": "string", "x-ui-widget": "resource"},
			"ref":          map[string]any{"type": "string", "x-ui-widget": "ref"},
			"paths":        map[string]any{"type": "array"},
			"extensions":   map[string]any{"type": "array"},
			"access_token": map[string]any{"type": "string", "writeOnly": true, "label": "Personal access token"},
		},
		Required: []string{"repo", "access_token"},
	}
}

func TestRenderGitHubConnectFormPickers(t *testing.T) {
	p := &DSProvider{Type: "github", Name: "GitHub", AuthMethods: []string{"credentials"}}
	configFields, credentialFields := dsConfigFields(githubSchema(), nil)
	form := renderHTML(t, DataSourceNewPage([]DSProvider{*p}, p, configFields, credentialFields, nil, nil, nil))

	for _, want := range []string{
		`name="providerType" value="github"`,
		`data-testid="ds-field-repo"`,
		`list="ds-options-repo"`,
		`data-testid="ds-load-repos"`,
		`hx-post="/data-sources/repos"`,
		`data-testid="ds-field-ref"`,
		`list="ds-options-ref"`,
		`data-testid="ds-load-refs"`,
		`hx-post="/data-sources/refs"`,
		`data-testid="ds-field-access_token"`,
		`type="password"`,
		"Personal access token",
		"Sync scope",
	} {
		if !strings.Contains(form, want) {
			t.Errorf("GitHub connect form missing %q", want)
		}
	}
	if strings.Contains(form, `name="access_token" value`) {
		t.Error("access_token must never be prefilled")
	}
}

func TestUIDataSourceCreateUsesSelectedProvider(t *testing.T) {
	f := &fakeMemory{dsSchema: githubSchema()}
	e := newDSUIServer(f)

	form := url.Values{
		"providerType": {"github"},
		"name":         {"Docs"},
		"sync_mode":    {"manual"},
		"repo":         {"acme/private-docs"},
		"ref":          {"main"},
		"access_token": {"ghp_test"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/data-sources/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/data-sources/ds-new?created=1" {
		t.Errorf("create location = %q", loc)
	}
	if f.dsCreatedReq == nil {
		t.Fatal("CreateDSIntegration was not called")
	}
	if f.dsCreatedReq.ProviderType != "github" {
		t.Errorf("providerType = %q, want github (no Drive hardcode)", f.dsCreatedReq.ProviderType)
	}
	if f.dsCreatedReq.Config["repo"] != "acme/private-docs" || f.dsCreatedReq.Config["ref"] != "main" {
		t.Errorf("config = %v, want the submitted repository and ref", f.dsCreatedReq.Config)
	}
	if f.dsCreatedReq.Config["access_token"] != "ghp_test" {
		t.Errorf("config = %v, want the submitted token forwarded for encryption", f.dsCreatedReq.Config)
	}
	if _, ok := f.dsCreatedReq.Config["folder_id"]; ok {
		t.Errorf("config = %v, must not carry a Drive folder_id", f.dsCreatedReq.Config)
	}
}

func TestUIDataSourceCreateErrorRedirectsToSelectedProvider(t *testing.T) {
	f := &fakeMemory{
		dsSchema:    githubSchema(),
		dsCreateErr: &memoryHTTPError{Status: http.StatusUnprocessableEntity, Code: "validation_error", Message: "config.repo is required"},
	}
	e := newDSUIServer(f)

	form := url.Values{"providerType": {"github"}, "name": {"Docs"}, "access_token": {"ghp_test"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/data-sources/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create: status=%d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/data-sources/new?providerType=github&method=credentials") {
		t.Errorf("error redirect = %q, want the same provider's connect form", loc)
	}
	if !strings.Contains(loc, "err=config.repo+is+required") {
		t.Errorf("error redirect = %q, want the server's message without the memory prefix", loc)
	}
	if strings.Contains(loc, "memory+422") || strings.Contains(loc, "validation_error") {
		t.Errorf("error redirect = %q, must not leak the transport prefix", loc)
	}
}

func TestUIDataSourceTestConfigUsesSelectedProvider(t *testing.T) {
	f := &fakeMemory{dsSchema: githubSchema()}
	e := newDSUIServer(f)

	form := strings.NewReader("providerType=github&repo=acme/private-docs&access_token=ghp_test")
	req := httptest.NewRequest(http.MethodPost, "/data-sources/test-config", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if f.dsLastTestConfig == nil || f.dsLastTestConfig.ProviderType != "github" {
		t.Fatalf("test config = %+v, want the form's provider (github)", f.dsLastTestConfig)
	}
	if f.dsLastTestConfig.Config["repo"] != "acme/private-docs" || f.dsLastTestConfig.Config["access_token"] != "ghp_test" {
		t.Errorf("test config config = %v, want repo and token", f.dsLastTestConfig.Config)
	}
}

func TestUIDataSourceReposPartial(t *testing.T) {
	f := &fakeMemory{
		dsSchema: githubSchema(),
		dsProviderResources: []DSResource{
			{ID: "acme/private-docs", Name: "acme/private-docs", Type: "repo"},
			{ID: "acme/public-docs", Name: "acme/public-docs", Type: "repo"},
		},
	}
	e := newDSUIServer(f)

	rec := postDSUIForm(e, "/data-sources/repos", "providerType=github&access_token=ghp_test")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<datalist id="ds-options-repo">`,
		`<option value="acme/private-docs">`,
		`<option value="acme/public-docs">`,
		`data-testid="ds-options-status"`,
		"Showing 2 suggestions.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("repo options missing %q in %s", want, body)
		}
	}
	if f.dsListedProviderType != "github" || f.dsListedConfig["access_token"] != "ghp_test" {
		t.Errorf("listing call = %q %v, want github with the unsaved token", f.dsListedProviderType, f.dsListedConfig)
	}
}

// TestUIDataSourceReposPartialUsesPostedFieldKey pins that the suggestion list
// follows the field the load button belongs to, so a provider may name its
// widget field anything; the endpoint key is only the fallback, and an unsafe
// value cannot escape into the datalist id.
func TestUIDataSourceReposPartialUsesPostedFieldKey(t *testing.T) {
	f := &fakeMemory{
		dsSchema:            githubSchema(),
		dsProviderResources: []DSResource{{ID: "acme/docs", Name: "acme/docs", Type: "repo"}},
	}
	e := newDSUIServer(f)

	rec := postDSUIForm(e, "/data-sources/repos", "providerType=github&access_token=ghp_test&fieldKey=repository")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, `<datalist id="ds-options-repository">`) {
		t.Errorf("options partial = %s, want the posted field key in the datalist id", body)
	}

	rec = postDSUIForm(e, "/data-sources/repos", `providerType=github&access_token=ghp_test&fieldKey=%22%3E%3Cscript%3E`)
	if body := rec.Body.String(); !strings.Contains(body, `<datalist id="ds-options-repo">`) {
		t.Errorf("options partial = %s, want the endpoint fallback for an unsafe field key", body)
	}
}

func TestUIDataSourceRefsPartialReportsTruncation(t *testing.T) {
	f := &fakeMemory{
		dsSchema: githubSchema(),
		dsProviderRefs: []DSResource{
			{ID: "main", Name: "main", Type: "branch"},
			{ID: "v1.0.0", Name: "v1.0.0", Type: "tag"},
		},
		dsProviderRefsTrunc: true,
	}
	e := newDSUIServer(f)

	rec := postDSUIForm(e, "/data-sources/refs", "providerType=github&repo=acme/private-docs&access_token=ghp_test")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<datalist id="ds-options-ref">`,
		"main (branch)",
		"v1.0.0 (tag)",
		"Showing the first 2 suggestions",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("ref options missing %q in %s", want, body)
		}
	}
}

func TestUIDataSourceOptionsFailureIsInline(t *testing.T) {
	f := &fakeMemory{
		dsSchema:               githubSchema(),
		dsProviderResourcesErr: &memoryHTTPError{Status: http.StatusUnprocessableEntity, Code: "validation_error", Message: "Bad credentials"},
	}
	e := newDSUIServer(f)

	rec := postDSUIForm(e, "/data-sources/repos", "providerType=github&access_token=bad")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with an inline error", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, `data-testid="ds-options-error"`) || !strings.Contains(body, "Bad credentials") {
		t.Errorf("inline error missing in %s", body)
	}
}

func TestUIDataSourceUpdateUsesProviderSchema(t *testing.T) {
	f := &fakeMemory{
		dsSchema:       githubSchema(),
		dsIntegrations: []DSIntegration{{ID: "s1", Name: "Docs", ProviderType: "github", Status: "active"}},
	}
	e := newDSUIServer(f)

	form := url.Values{
		"providerType": {"github"},
		"name":         {"Docs"},
		"repo":         {"acme/private-docs"},
		"ref":          {"release/1.x"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/data-sources/s1/update", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("update: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if f.dsUpdatedReq == nil || f.dsUpdatedReq.Config == nil {
		t.Fatal("update was not forwarded with a config")
	}
	if f.dsUpdatedReq.Config["repo"] != "acme/private-docs" || f.dsUpdatedReq.Config["ref"] != "release/1.x" {
		t.Errorf("update config = %v, want the GitHub scope round-tripped", f.dsUpdatedReq.Config)
	}
	if _, ok := f.dsUpdatedReq.Config["access_token"]; ok {
		t.Error("a blank credential input must not send access_token (it would replace the stored credential)")
	}
}

// postDSUIForm posts an urlencoded form to the DS UI server and returns the
// recorder.
func postDSUIForm(e *echo.Echo, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	return rec
}

// --- OAuth connect (§5.1/5.2) ---

func TestUIDataSourceOAuthStart(t *testing.T) {
	f := &fakeMemory{dsOAuthAuthorizeResp: &DSOAuthAuthorizeResponse{AuthorizeURL: "https://accounts.example/authorize?state=s1"}}
	e := newDSUIServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/oauth/start?providerType=google_drive", nil))
	if rec.Code != http.StatusSeeOther {
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

// TestUIDataSourceOAuthStartHTMXRedirectsViaHXRedirect covers the boosted
// caller: the chooser's OAuth action sits inside the boosted <main>, so htmx
// hits this route with HX-Request. htmx fetches with mode "same-origin" and
// cannot follow the cross-origin 303 the plain path emits, so the route answers
// HX-Redirect (a top-level navigation to the absolute authorize URL) instead.
func TestUIDataSourceOAuthStartHTMXRedirectsViaHXRedirect(t *testing.T) {
	f := &fakeMemory{dsOAuthAuthorizeResp: &DSOAuthAuthorizeResponse{AuthorizeURL: "https://accounts.example/authorize?state=s1"}}
	e := newDSUIServer(f)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/data-sources/oauth/start?providerType=google_drive", nil)
	req.Header.Set("HX-Request", "true")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("htmx status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("HX-Redirect"); got != "https://accounts.example/authorize?state=s1" {
		t.Errorf("HX-Redirect = %q, want the authorize URL", got)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("Location = %q, want empty for an htmx caller", loc)
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
		"No OAuth app is configured for GitHub",
		"project or organization admin",
		"Settings → Credentials",
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

	// connect_failed → actionable message naming the OAuth client config.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/oauth/callback?oauth_error=connect_failed", nil))
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "OAuth+client") {
		t.Errorf("connect_failed location = %q, want actionable OAuth client message", loc)
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

// TestUIDataSourceOAuthCallbackScoped verifies the OAuth landing redirects onto
// the caller's active project scope when a session carries one, so the redirect
// target never trips scopeRewrite's hard cut. The unscoped fallback is covered
// by TestUIDataSourceOAuthCallback (no session attached).
func TestUIDataSourceOAuthCallbackScoped(t *testing.T) {
	e := newDSUIServer(&fakeMemory{})

	// connected → the scoped detail URL with the created flash.
	req := httptest.NewRequest(http.MethodGet, "/data-sources/oauth/callback?connected=s1", nil)
	req = req.WithContext(withSessionContext(req.Context(), &sessionContext{OrgID: "o1", ProjectID: "p1"}))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("connected status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/o/o1/p/p1/data-sources/s1?created=1" {
		t.Errorf("connected location = %q, want scoped", loc)
	}

	// oauth_error → the scoped list with the error flash.
	req = httptest.NewRequest(http.MethodGet, "/data-sources/oauth/callback?oauth_error=denied", nil)
	req = req.WithContext(withSessionContext(req.Context(), &sessionContext{OrgID: "o1", ProjectID: "p1"}))
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/o/o1/p/p1/data-sources?err=") {
		t.Errorf("oauth_error location = %q, want scoped list", loc)
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
	// The page must defer the list, not block on it: no resources API call and
	// a loading placeholder that fetches the list partial.
	if f.dsResourcesCallCount != 0 {
		t.Errorf("ListDSResources called %d times on the page route, want 0 (deferred)", f.dsResourcesCallCount)
	}
	if !strings.Contains(body, `data-testid="ds-resource-loading"`) {
		t.Errorf("deferred loading indicator missing: %s", body)
	}
	if !strings.Contains(body, `hx-get="/data-sources/s1/resources/list"`) {
		t.Errorf("deferred list URL missing: %s", body)
	}
	if strings.Contains(body, "Reports") {
		t.Errorf("page route must not render resources inline: %s", body)
	}
}

func TestUIDataSourceResourceListRoute(t *testing.T) {
	f := &fakeMemory{
		dsIntegrations: []DSIntegration{{ID: "s1", Name: "Team Drive", ProviderType: "google_drive"}},
		dsProviders:    []DSProvider{{Type: "google_drive", Name: "Google Drive", AuthMethods: []string{"oauth"}}},
		dsResources:    []DSResource{{ID: "f1", Name: "Reports", Type: "folder"}},
	}
	e := newDSUIServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/s1/resources/list", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Reports") || !strings.Contains(body, `data-testid="ds-resource-row"`) {
		t.Errorf("list partial rows missing: %s", body)
	}
	if f.dsResourcesCallCount != 1 {
		t.Errorf("ListDSResources called %d times on the list route, want 1", f.dsResourcesCallCount)
	}
	// The partial is a bare fragment, not the page layout.
	if strings.Contains(body, `data-testid="ds-resource-step"`) {
		t.Errorf("list partial must not render the page shell: %s", body)
	}
}

func TestUIDataSourceResourcesStatic(t *testing.T) {
	f := &fakeMemory{
		dsIntegrations: []DSIntegration{{ID: "s1", Name: "Team Drive", ProviderType: "google_drive"}},
		dsProviders:    []DSProvider{{Type: "google_drive", Name: "Google Drive", AuthMethods: []string{"oauth"}}},
		dsResources:    []DSResource{{ID: "f1", Name: "Reports", Type: "folder"}},
	}
	e := newDSUIServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/s1/resources?static=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Reports") {
		t.Errorf("static inline list missing: %s", body)
	}
	if strings.Contains(body, `data-testid="ds-resource-loading"`) {
		t.Errorf("static render must not show the deferred loading indicator: %s", body)
	}
	if f.dsResourcesCallCount != 1 {
		t.Errorf("ListDSResources called %d times on the static route, want 1", f.dsResourcesCallCount)
	}
}

// The no-JS links must survive a search filter: the static page URL carries
// ?static=1 plus the trimmed, escaped query, and the list fragment carries the
// same filter for the HTMX path.
func TestDSResourceURLsWithQuery(t *testing.T) {
	for _, tc := range []struct {
		name  string
		id    string
		query string
		want  string
	}{{
		name:  "static without query",
		id:    "s1",
		query: "",
		want:  "/data-sources/s1/resources?static=1",
	}, {
		name:  "static with query",
		id:    "s1",
		query: "team drive/a&b",
		want:  "/data-sources/s1/resources?static=1&q=team+drive%2Fa%26b",
	}, {
		name:  "static trims blank query",
		id:    "s1",
		query: "   ",
		want:  "/data-sources/s1/resources?static=1",
	}, {
		name:  "list with query",
		id:    "s1",
		query: "Reports",
		want:  "/data-sources/s1/resources/list?q=Reports",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			if strings.Contains(tc.want, "?static=1") {
				got = dsResourceStaticURL(tc.id, tc.query)
			} else {
				got = dsResourceListURL(tc.id, tc.query)
			}
			if got != tc.want {
				t.Errorf("url = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestUIDataSourceResourcesQuery verifies the ?q= search parameter is threaded
// from the request into the deferred list URL (page) and the backend listing
// call (list partial), and echoed back into the rendered search input.
func TestUIDataSourceResourcesQuery(t *testing.T) {
	f := &fakeMemory{
		dsIntegrations: []DSIntegration{{ID: "s1", Name: "Team Drive", ProviderType: "google_drive"}},
		dsProviders:    []DSProvider{{Type: "google_drive", Name: "Google Drive", AuthMethods: []string{"oauth"}}},
		dsResources:    []DSResource{{ID: "f1", Name: "Reports", Type: "folder"}},
	}
	e := newDSUIServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/s1/resources?q=rep", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `value="rep"`) {
		t.Errorf("search input not pre-filled: %s", body)
	}
	if !strings.Contains(body, `hx-get="/data-sources/s1/resources/list?q=rep"`) {
		t.Errorf("deferred list URL must carry the filter: %s", body)
	}
	if f.dsResourcesCallCount != 0 {
		t.Errorf("ListDSResources called %d times on the page route, want 0", f.dsResourcesCallCount)
	}

	// The list route applies the filter to the backend call.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/s1/resources/list?q=rep", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.dsResourcesQuery != "rep" {
		t.Errorf("ListDSResources query = %q, want %q", f.dsResourcesQuery, "rep")
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

// TestUIDataSourceResourceSaveMergesStoredConfig asserts picking a resource
// merges it over the source's existing non-secret config instead of replacing
// it, so other persisted keys (mime_types, recursive) survive the update.
func TestUIDataSourceResourceSaveMergesStoredConfig(t *testing.T) {
	f := &fakeMemory{dsIntegrations: []DSIntegration{{
		ID: "s1", Name: "Team Drive", ProviderType: "google_drive",
		Config: map[string]any{"mime_types": []any{"application/pdf"}, "recursive": true},
	}}}
	e := newDSUIServer(f)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/data-sources/s1/resources", strings.NewReader("resource_id=f1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.dsSavedResourceCfg["folder_id"] != "f1" {
		t.Fatalf("folder_id missing from saved cfg: %+v", f.dsSavedResourceCfg)
	}
	if _, ok := f.dsSavedResourceCfg["mime_types"]; !ok {
		t.Errorf("stored mime_types lost in merge: %+v", f.dsSavedResourceCfg)
	}
	if v, ok := f.dsSavedResourceCfg["recursive"].(bool); !ok || !v {
		t.Errorf("stored recursive lost in merge: %+v", f.dsSavedResourceCfg)
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

// --- project credentials (member-readable table, admin-only editor) ---
func projectCredentialsAdminFake() *fakeMemory {
	return &fakeMemory{
		project: &Project{ID: "p1", Name: "Home", OrgID: "o1"},
		orgsAndProjects: []OrgWithProjectsDto{{
			ID: "o1", Name: "Acme", Role: "org_member",
			Projects: []ProjectAccessDto{{ID: "p1", Name: "Home", OrgID: "o1", Role: "project_admin"}},
		}},
		dsProviders: []DSProvider{
			{Type: "google_drive", Name: "Google Drive", Vendor: "google", VendorName: "Google", AuthMethods: []string{"oauth"}},
			{Type: "github", Name: "GitHub", Vendor: "github", VendorName: "GitHub", AuthMethods: []string{"oauth", "credentials"}},
		},
		dsCredentials: []DSOAuthCredentialDescription{
			{
				ProviderType:  "google_drive",
				Project:       DSOAuthCredentialSource{Configured: true, ClientID: "pcid", UpdatedAt: "2026-09-01T10:00:00Z"},
				Organization:  DSOAuthCredentialSource{Configured: true, ClientID: "ocid"},
				ResolvedScope: dsCredentialScopeProject,
			},
			{ProviderType: "github", ResolvedScope: "none"},
		},
		dsProjectOAuthClients: []DSOAuthClientConfig{
			{ProviderType: "google_drive", ClientID: "pcid", RedirectURI: "https://api.example/api/data-source-integrations/oauth/google_drive/callback", JavascriptOrigin: "https://admin.example", Configured: true},
		},
	}
}

func TestRenderCredentialsPage(t *testing.T) {
	providers := []DSProvider{
		{Type: "google_drive", Name: "Google Drive", Vendor: "google", VendorName: "Google", AuthMethods: []string{"oauth"}},
		{Type: "github", Name: "GitHub", Vendor: "github", VendorName: "GitHub", AuthMethods: []string{"oauth", "credentials"}},
	}
	creds := []DSOAuthCredentialDescription{
		{
			ProviderType:  "google_drive",
			Project:       DSOAuthCredentialSource{Configured: true, ClientID: "pcid", UpdatedAt: "2026-09-01T10:00:00Z"},
			Organization:  DSOAuthCredentialSource{Configured: true, ClientID: "ocid"},
			ResolvedScope: dsCredentialScopeProject,
		},
	}
	rows, note := dsCredentialRows(providers, creds, true, false, "o1")
	html := renderHTML(t, CredentialsPage(dsCredentialsPageData{
		Nav:         settingsNavCommon{IsProjectAdmin: true},
		ProjectName: "Home",
		OrgName:     "Acme",
		Rows:        rows,
	}))
	if note != "" {
		t.Errorf("deployment note = %q, want empty", note)
	}
	for _, want := range []string{
		"Credentials",
		"Google OAuth app",
		"GitHub OAuth app",
		"Google Drive",
		"Project",
		"Organization",
		"pcid",
		"ocid",
		"In use",
		`data-testid="ds-credentials-table"`,
		`data-testid="ds-credential-row"`,
		`href="/settings/credentials/google_drive"`,
		"Managed by org admins",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("credentials page missing %q", want)
		}
	}
	// The project app wins, so only the project-scope row is "In use".
	if strings.Count(html, "In use") != 1 {
		t.Errorf("expected exactly one In use row, got %d", strings.Count(html, "In use"))
	}

	// A non-admin sees the same table read-only.
	memberRows, _ := dsCredentialRows(providers, creds, false, false, "o1")
	member := renderHTML(t, CredentialsPage(dsCredentialsPageData{
		Nav:         settingsNavCommon{},
		ProjectName: "Home",
		OrgName:     "Acme",
		Rows:        memberRows,
	}))
	if !strings.Contains(member, "Google OAuth app") || !strings.Contains(member, "Managed by project admins") {
		t.Error("member credentials table should be visible and read-only")
	}
	if strings.Contains(member, `href="/settings/credentials/google_drive"`) {
		t.Error("member credentials table must not link to the admin editor")
	}
}

func TestRenderCredentialsPageDeploymentNote(t *testing.T) {
	providers := []DSProvider{{Type: "google_drive", Name: "Google Drive", VendorName: "Google", AuthMethods: []string{"oauth"}}}
	creds := []DSOAuthCredentialDescription{{
		ProviderType:  "google_drive",
		Deployment:    DSOAuthCredentialSource{Configured: true, ClientID: "dcid"},
		ResolvedScope: dsCredentialScopeDeployment,
	}}
	rows, note := dsCredentialRows(providers, creds, true, false, "o1")
	if !strings.Contains(note, "Google Drive") || !strings.Contains(note, "deployment") {
		t.Errorf("deployment note = %q", note)
	}
	html := renderHTML(t, CredentialsPage(dsCredentialsPageData{Nav: settingsNavCommon{IsProjectAdmin: true}, Rows: rows, DeploymentNote: note}))
	if !strings.Contains(html, "built-in OAuth app") {
		t.Error("deployment note missing from the page")
	}
}

func TestRenderCredentialDetailPage(t *testing.T) {
	provider := DSProvider{Type: "google_drive", Name: "Google Drive", Vendor: "google", VendorName: "Google", AuthMethods: []string{"oauth"}}
	cfg := DSOAuthClientConfig{
		ProviderType:     "google_drive",
		ClientID:         "pcid",
		RedirectURI:      "https://api.example/api/data-source-integrations/oauth/google_drive/callback",
		JavascriptOrigin: "https://admin.example",
		Configured:       true,
	}
	html := renderHTML(t, CredentialDetailPage(dsCredentialDetailData{
		Nav:           settingsNavCommon{IsProjectAdmin: true},
		Provider:      provider,
		Config:        cfg,
		OrgConfigured: true,
		OrgManageHref: "/orgs/o1/settings/oauth-clients#google_drive",
	}))
	for _, want := range []string{
		"Google OAuth app",
		"Google Drive",
		"pcid",
		`action="/settings/credentials/google_drive"`,
		`data-testid="ds-oauth-console-guide"`,
		`data-testid="ds-oauth-redirect-uri"`,
		"https://api.example/api/data-source-integrations/oauth/google_drive/callback",
		`data-testid="ds-oauth-js-origin"`,
		"https://admin.example",
		"Google Cloud Console",
		"overrides the organization-wide app",
		`data-testid="ds-credential-org-link"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("credential detail page missing %q", want)
		}
	}

	// No project app: the inheritance note explains the org fallback.
	bare := renderHTML(t, CredentialDetailPage(dsCredentialDetailData{
		Nav:           settingsNavCommon{IsProjectAdmin: true},
		Provider:      provider,
		OrgConfigured: true,
	}))
	if !strings.Contains(bare, "uses the organization-wide app") {
		t.Error("org fallback note missing")
	}
	if strings.Contains(bare, `data-testid="ds-oauth-console-guide"`) {
		t.Error("console guide must not render without resolved callback values")
	}
}

// TestRenderSettingsRailCredentials asserts the Credentials entry is visible to
// every project member (the table is member-readable), not just admins.
func TestRenderSettingsRailCredentials(t *testing.T) {
	html := renderHTML(t, settingsSubNav("credentials", true))
	if !strings.Contains(html, `href="/settings/credentials"`) {
		t.Error("settings rail missing the Credentials entry")
	}
	if !strings.Contains(html, "Credentials") {
		t.Error("settings rail missing the Credentials label")
	}
	if !strings.Contains(html, `aria-current="page"`) {
		t.Error("Credentials rail entry should be active")
	}
}

func TestUIProjectCredentialsPageAdminAndMember(t *testing.T) {
	// Project admin sees the table with editor links.
	e := newDSUIServer(projectCredentialsAdminFake())
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/credentials", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("admin status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "Google OAuth app") || !strings.Contains(body, "pcid") {
		t.Errorf("admin body = %s", body)
	}

	// A project member gets the read-only table, NOT a 404.
	e = newDSUIServer(&fakeMemory{
		project: &Project{ID: "p1", Name: "Home", OrgID: "o1"},
		orgsAndProjects: []OrgWithProjectsDto{{
			ID: "o1", Name: "Acme", Role: "org_member",
			Projects: []ProjectAccessDto{{ID: "p1", Name: "Home", OrgID: "o1", Role: "project_user"}},
		}},
		dsProviders: []DSProvider{{Type: "google_drive", Name: "Google Drive", VendorName: "Google", AuthMethods: []string{"oauth"}}},
		dsCredentials: []DSOAuthCredentialDescription{{
			ProviderType: "google_drive",
			Project:      DSOAuthCredentialSource{Configured: true, ClientID: "pcid"},
		}},
	})
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/credentials", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("member status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Managed by project admins") {
		t.Error("member table should be read-only")
	}
	if strings.Contains(body, `href="/settings/credentials/google_drive"`) {
		t.Error("member table must not link to the admin editor")
	}

	// The per-provider editor stays project-admin only.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/credentials/google_drive", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("member editor status = %d, want 404", rec.Code)
	}

	// Admin editor resolves the provider from the catalog.
	e = newDSUIServer(projectCredentialsAdminFake())
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/credentials/google_drive", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("admin editor status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "Google OAuth app") {
		t.Errorf("editor body missing the vendor label: %s", body)
	}

	// Unknown provider is a 404, not an empty editor.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/credentials/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown provider status = %d, want 404", rec.Code)
	}

	// The retired URL redirects to the credentials table.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/oauth-clients", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("legacy redirect status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/settings/credentials" {
		t.Errorf("legacy redirect location = %q", loc)
	}
}

// TestUIProjectCredentialsOrgAdminCannotManageProjectApps asserts the project
// credential surfaces follow the memory service's exact project_admin rule: an
// org_admin who is only project_user can read the table and manage the org-wide
// app, but gets no project editor link and a 404 from the project editor.
func TestUIProjectCredentialsOrgAdminCannotManageProjectApps(t *testing.T) {
	f := &fakeMemory{
		project: &Project{ID: "p1", Name: "Home", OrgID: "o1"},
		orgsAndProjects: []OrgWithProjectsDto{{
			ID: "o1", Name: "Acme", Role: "org_admin",
			Projects: []ProjectAccessDto{{ID: "p1", Name: "Home", OrgID: "o1", Role: "project_user"}},
		}},
		dsProviders: []DSProvider{{Type: "google_drive", Name: "Google Drive", VendorName: "Google", AuthMethods: []string{"oauth"}}},
		dsCredentials: []DSOAuthCredentialDescription{{
			ProviderType:  "google_drive",
			Project:       DSOAuthCredentialSource{Configured: true, ClientID: "pcid"},
			ResolvedScope: dsCredentialScopeProject,
		}},
	}
	e := newDSUIServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/credentials", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("org admin credentials status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, `href="/settings/credentials/google_drive"`) {
		t.Error("org_admin/project_user must not get the project editor link")
	}
	if !strings.Contains(body, "Managed by project admins") {
		t.Error("org_admin/project_user should see the project row as read-only")
	}
	// The org-wide app stays manageable by the org admin.
	if !strings.Contains(body, `/o/o1/settings/oauth-clients#google_drive`) {
		t.Error("org_admin should still get the organization editor link")
	}

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/credentials/google_drive", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("org_admin/project_user editor status = %d, want 404", rec.Code)
	}

	for _, tc := range []struct{ path, body string }{
		{"/settings/credentials/google_drive", "client_id=pcid&client_secret=sec"},
		{"/settings/credentials/google_drive/delete", ""},
	} {
		rec = httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("POST %s status = %d, want 404", tc.path, rec.Code)
		}
	}
	if f.dsUpsertedProjectOAuthID != "" || f.dsDeletedProjectOAuthID != "" {
		t.Error("memory must not be called for a non-project-admin")
	}

	// The missing-client alert falls back to the org settings page, matching
	// the editor link the caller can actually open.
	missing := &fakeMemory{
		dsProviders: []DSProvider{{Type: "github", Name: "GitHub", AuthMethods: []string{"oauth"}}},
		dsOAuthAuthorizeErr: &memoryHTTPError{
			Status:  http.StatusUnprocessableEntity,
			Code:    "validation_error",
			Message: "no OAuth client configured for provider github",
		},
		orgsAndProjects: []OrgWithProjectsDto{{
			ID: "o1", Name: "Acme", Role: "org_admin",
			Projects: []ProjectAccessDto{{ID: "p1", Name: "Home", OrgID: "o1", Role: "project_user"}},
		}},
	}
	e = newDSUIServer(missing)
	req := httptest.NewRequest(http.MethodGet, "/data-sources/oauth/start?providerType=github", nil)
	req = req.WithContext(withSessionContext(req.Context(), &sessionContext{OrgID: "o1", ProjectID: "p1"}))
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("missing-client status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "/o/o1/settings/oauth-clients") || strings.Contains(body, `href="/settings/credentials/github"`) {
		t.Errorf("missing-client link should target the org page for an org_admin/project_user: %s", body)
	}
}

func TestUIProjectOAuthClientUpsertAndDelete(t *testing.T) {
	f := projectCredentialsAdminFake()
	e := newDSUIServer(f)

	// Missing client id → 303 back with an error, memory untouched.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/settings/credentials/google_drive", strings.NewReader("client_secret=sec"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("validation status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/settings/credentials/google_drive?err=") {
		t.Errorf("validation location = %q", loc)
	}
	if f.dsUpsertedProjectOAuthID != "" {
		t.Error("memory must not be called on validation failure")
	}

	// Success → 303 with the saved flash, targeting the active project.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/settings/credentials/google_drive", strings.NewReader("client_id=pcid&client_secret=sec&scopes=scope.a%2Cscope.b&strategy=drive.file"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("success status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/settings/credentials/google_drive?saved=1" {
		t.Errorf("success location = %q", loc)
	}
	if got := f.dsUpsertedProjectOAuthReq; got == nil || got.ClientID != "pcid" || got.ClientSecret != "sec" || got.Strategy != "drive.file" {
		t.Fatalf("upsert req = %+v", got)
	}
	if f.dsUpsertedProjectOAuthID != "p1" || f.dsUpsertedProjectOAuthProv != "google_drive" {
		t.Errorf("upsert target = %q/%q", f.dsUpsertedProjectOAuthID, f.dsUpsertedProjectOAuthProv)
	}

	// Delete → 303 with the deleted flash.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/settings/credentials/google_drive/delete", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/settings/credentials/google_drive?deleted=1" {
		t.Errorf("delete location = %q", loc)
	}
	if f.dsDeletedProjectOAuthID != "p1" || f.dsDeletedProjectOAuthProv != "google_drive" {
		t.Errorf("delete target = %q/%q", f.dsDeletedProjectOAuthID, f.dsDeletedProjectOAuthProv)
	}
}

// TestUIDataSourceOAuthStartMissingClientLinksProjectSettings asserts the
// missing-client alert targets the PROJECT settings page when the caller
// administers the active project, and the org page only as a fallback.
func TestUIDataSourceOAuthStartMissingClientLinksProjectSettings(t *testing.T) {
	f := &fakeMemory{
		dsProviders: []DSProvider{{Type: "github", Name: "GitHub", AuthMethods: []string{"oauth"}}},
		dsOAuthAuthorizeErr: &memoryHTTPError{
			Status:  http.StatusUnprocessableEntity,
			Code:    "validation_error",
			Message: "no OAuth client configured for provider github (configure one under Organization settings → Data source OAuth clients)",
		},
		orgsAndProjects: []OrgWithProjectsDto{{
			ID: "o1", Name: "Acme", Role: "org_member",
			Projects: []ProjectAccessDto{{ID: "p1", Name: "Home", OrgID: "o1", Role: "project_admin"}},
		}},
	}
	e := newDSUIServer(f)
	req := httptest.NewRequest(http.MethodGet, "/data-sources/oauth/start?providerType=github", nil)
	req = req.WithContext(withSessionContext(req.Context(), &sessionContext{OrgID: "o1", ProjectID: "p1"}))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="ds-oauth-client-settings-link"`) {
		t.Fatalf("settings link missing: %s", body)
	}
	if !strings.Contains(body, `href="/settings/credentials/github"`) {
		t.Errorf("project-scoped credentials link missing: %s", body)
	}
	if strings.Contains(body, "/orgs/o1/settings/oauth-clients") {
		t.Errorf("org settings link must not be used when the caller administers the project: %s", body)
	}
}
