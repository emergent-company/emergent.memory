package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"golang.org/x/net/html"
)

// testDSSchema is a google_drive-like provider schema for render tests.
func testDSSchema() *DSProviderSchema {
	return &DSProviderSchema{
		Type: "google_drive",
		Properties: map[string]any{
			"folders":       map[string]any{"type": "array", "x-ui-widget": "folders", "description": "The Google Drive folders to sync from."},
			"mime_types":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"recursive":     map[string]any{"type": "boolean"},
			"client_id":     map[string]any{"type": "string"},
			"client_secret": map[string]any{"type": "string"},
			"refresh_token": map[string]any{"type": "string"},
		},
	}
}

func TestDSConfigFieldsSplitsCredentials(t *testing.T) {
	config, creds := dsConfigFields(testDSSchema(), nil)
	if len(config) != 3 {
		t.Fatalf("config fields = %+v", config)
	}
	if len(creds) != 3 {
		t.Fatalf("credential fields = %+v", creds)
	}
	if config[0].Key != "folders" || config[0].Widget != "folders" {
		t.Errorf("first config field = %+v", config[0])
	}
	if config[1].Key != "mime_types" || config[2].Key != "recursive" {
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
		"folders":       []any{map[string]any{"folder_id": "f1", "name": "Reports", "drive_id": "d1"}},
		"mime_types":    []any{"application/pdf", "text/plain"},
		"recursive":     true,
		"refresh_token": "must-not-leak",
	}
	config, creds := dsConfigFields(testDSSchema(), cfg)
	if len(config) != 3 || len(creds) != 3 {
		t.Fatalf("config=%+v creds=%+v", config, creds)
	}
	if config[0].Key != "folders" || config[0].Widget != "folders" {
		t.Errorf("folders field = %+v", config[0])
	}
	if config[1].Value != "application/pdf\ntext/plain" {
		t.Errorf("mime_types value = %q", config[1].Value)
	}
	if !config[2].Checked {
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
	chooser := renderHTML(t, DataSourceNewPage([]DSProvider{*oauth, *credsOnly}, nil, nil, nil, dsSavedCredentialPicker{}, nil, nil, nil))
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
	confirm := renderHTML(t, DataSourceNewPage([]DSProvider{*oauthOnly}, oauthOnly, nil, nil, dsSavedCredentialPicker{}, nil, nil, nil))
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
	form := renderHTML(t, DataSourceNewPage([]DSProvider{*credsOnly}, credsOnly, config, credFields, dsSavedCredentialPicker{}, nil, nil, nil))
	for _, want := range []string{
		"Connect a data source",
		`data-testid="ds-connect-form"`,
		`name="providerType" value="github"`,
		`data-testid="ds-field-name"`,
		`data-testid="ds-field-folders"`,
		`name="mime_types"`,
		`name="recursive"`,
		`type="password"`,
		`data-testid="ds-field-client_secret"`,
		`hx-post="/data-sources/test-config"`,
		`id="ds-test-result"`,
		`name="sync_mode"`,
		"Folders",
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

	static := renderHTML(t, DataSourceResourceSelectPageStatic(src, "Google Drive", "", nil, false, nil, "", nil))
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

	html := renderHTML(t, DataSourceResourceList(src, "Google Drive", resources, false, "", nil))
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
	truncated := renderHTML(t, DataSourceResourceList(src, "Google Drive", resources, true, "", nil))
	if !strings.Contains(truncated, `data-testid="ds-resource-truncated"`) {
		t.Error("truncation note missing for a truncated listing")
	}
	if !strings.Contains(truncated, "search by name") {
		t.Error("truncation note missing its search-by-name hint")
	}

	empty := renderHTML(t, DataSourceResourceList(src, "Google Drive", nil, false, "", nil))
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
	genericList := renderHTML(t, DataSourceResourceList(&DSIntegration{ID: "s2", ProviderType: "github"}, "GitHub", []DSResource{{ID: "r1", Name: "acme/repo"}}, false, "", nil))
	if !strings.Contains(genericList, "Use this resource") {
		t.Error("generic resource action label missing")
	}

	// A failed listing renders the error state with the message and a retry, not
	// the empty state: "no resources were returned" would hide the reason.
	errored := renderHTML(t, DataSourceResourceList(src, "Google Drive", nil, false, "", errTest))
	if !strings.Contains(errored, `data-testid="ds-resource-error"`) {
		t.Error("resource error state missing")
	}
	if !strings.Contains(errored, errTest.Error()) {
		t.Errorf("resource error state dropped the message: %s", errored)
	}
	if strings.Contains(errored, `data-testid="ds-resource-empty"`) {
		t.Error("a failed listing must not render the empty state")
	}
	if !strings.Contains(errored, `hx-get="/data-sources/s1/resources/list"`) {
		t.Errorf("error state must offer the HTMX retry: %s", errored)
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

func TestRenderOrgOAuthClientsPage(t *testing.T) {
	providers := []DSProvider{
		{Type: "google_drive", Name: "Google Drive", VendorName: "Google", AuthMethods: []string{"oauth", "credentials"}},
		{Type: "github", Name: "GitHub", VendorName: "GitHub", AuthMethods: []string{"credentials"}},
	}
	clients := map[string]DSOAuthClientConfig{
		"google_drive": {ProviderType: "google_drive", ClientID: "cid-123", Configured: true},
	}
	html := renderHTML(t, OrgOAuthClientsPage("org-1", "Acme", dsOAuthAppProviders(providers), clients, nil, "", nil))
	for _, want := range []string{
		"Data source OAuth clients",
		"Acme",
		"Google OAuth app",
		"Google Drive",
		`data-testid="ds-oauth-apps-table"`,
		`data-testid="ds-oauth-app-row"`,
		`data-testid="ds-oauth-app-client-id"`,
		"cid-123",
		"Configured",
		`data-testid="ds-oauth-app-manage"`,
		`href="/orgs/org-1/settings/oauth-clients/google_drive"`,
		"Manage",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("org OAuth apps table missing %q", want)
		}
	}
	// The org index is a table, not a stack of credential forms.
	if strings.Contains(html, `data-testid="ds-oauth-client-form"`) {
		t.Error("the org OAuth apps table must not render a credential form")
	}
	// A credentials-only provider has no OAuth app to manage, so no row.
	if strings.Contains(html, "GitHub") {
		t.Error("credentials-only provider must not appear on the OAuth apps table")
	}

	// An unconfigured provider still renders a row, with a Set up action.
	unconfigured := renderHTML(t, OrgOAuthClientsPage("org-1", "Acme", dsOAuthAppProviders(providers), nil, nil, "", nil))
	if !strings.Contains(unconfigured, "Not configured") {
		t.Error("unconfigured badge missing")
	}
	if !strings.Contains(unconfigured, "Set up") {
		t.Error("unconfigured provider should offer a Set up action")
	}
}

func TestRenderOrgOAuthClientDetailPage(t *testing.T) {
	provider := DSProvider{Type: "google_drive", Name: "Google Drive", VendorName: "Google", AuthMethods: []string{"oauth"}}
	cfg := DSOAuthClientConfig{
		ProviderType: "google_drive",
		ClientID:     "cid-123",
		Scopes:       []string{"scope.a", "scope.b"},
		Strategy:     "drive.file",
		Configured:   true,
	}
	html := renderHTML(t, OrgOAuthClientDetailPage("org-1", "Acme", provider, cfg, nil, "", nil))
	for _, want := range []string{
		"Google OAuth app",
		"Google Drive",
		`data-testid="ds-oauth-client-detail"`,
		`data-testid="ds-oauth-app-back"`,
		`href="/orgs/org-1/settings/oauth-clients"`,
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
			t.Errorf("org client editor missing %q", want)
		}
	}
	// The client secret is write-only: never prefilled, even when configured.
	if regexp.MustCompile(`name="client_secret"[^>]*value=`).MatchString(html) {
		t.Error("client_secret must not be prefilled")
	}

	// #424: a saved secret is visible as state, and the field reads as a
	// replacement control rather than an empty "not set" input.
	for _, want := range []string{
		`data-testid="ds-oauth-client-secret-saved"`,
		"Enter a new one to replace it",
		`placeholder="Leave blank to keep the saved secret"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("configured secret field missing %q", want)
		}
	}

	// #425: one row at the bottom — Remove on the left, Save on the right —
	// and the shared sticky footer pattern instead of an inline button.
	if !strings.Contains(html, "sticky-form-actions") {
		t.Errorf("save/remove row must use the sticky form footer:\n%s", html)
	}
	if !strings.Contains(html, `formaction="/orgs/org-1/settings/oauth-clients/google_drive/delete"`) {
		t.Errorf("remove must post to the delete endpoint from the same row:\n%s", html)
	}
	removeAt := strings.Index(html, `data-testid="ds-oauth-client-delete"`)
	saveAt := strings.Index(html, `class="memory-save-btn`)
	if removeAt < 0 || saveAt < 0 || removeAt > saveAt {
		t.Errorf("Remove must precede Save in the action row (remove=%d save=%d)", removeAt, saveAt)
	}
	// The Remove control lives inside the save form (no nested form): the
	// delete URL must appear as a formaction, never as a second <form action>.
	if strings.Contains(html, `<form method="post" action="/orgs/org-1/settings/oauth-clients/google_drive/delete"`) {
		t.Errorf("delete must not render a second form:\n%s", html)
	}
	if n := strings.Count(html, "<form"); n != 1 {
		t.Errorf("client card rendered %d forms, want 1:\n%s", n, html)
	}

	// An unconfigured provider shows no delete control and no saved-secret state.
	bare := renderHTML(t, OrgOAuthClientDetailPage("org-1", "Acme", provider, DSOAuthClientConfig{}, nil, "", nil))
	for _, unwanted := range []string{
		`data-testid="ds-oauth-client-delete"`,
		`data-testid="ds-oauth-client-secret-saved"`,
	} {
		if strings.Contains(bare, unwanted) {
			t.Errorf("unconfigured provider must not render %q", unwanted)
		}
	}
	if !strings.Contains(bare, `placeholder="Paste the client secret"`) {
		t.Errorf("unconfigured secret field should invite a value:\n%s", bare)
	}
}

func TestRenderOrgOAuthClientDetailStrategySelectAndPicker(t *testing.T) {
	provider := DSProvider{Type: "google_drive", Name: "Google Drive", AuthMethods: []string{"oauth"}, Strategies: []string{"drive.file", "drive.readonly"}}
	cfg := DSOAuthClientConfig{
		ProviderType: "google_drive",
		ClientID:     "cid-123",
		Strategy:     "drive.file",
		Config:       map[string]any{"pickerDeveloperKey": "dev-key-1", "pickerAppId": "app-42"},
		Configured:   true,
	}
	html := renderHTML(t, OrgOAuthClientDetailPage("org-1", "Acme", provider, cfg, nil, "", nil))
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
			t.Errorf("org client editor strategy/picker page missing %q", want)
		}
	}
	// The client secret stays write-only.
	if regexp.MustCompile(`name="client_secret"[^>]*value=`).MatchString(html) {
		t.Error("client_secret must not be prefilled")
	}

	// A provider without the drive.file strategy renders neither picker field.
	noPicker := renderHTML(t, OrgOAuthClientDetailPage("org-1", "Acme",
		DSProvider{Type: "github", Name: "GitHub", AuthMethods: []string{"oauth"}, Strategies: []string{"repo"}},
		DSOAuthClientConfig{}, nil, "", nil))
	if strings.Contains(noPicker, `data-testid="ds-field-picker_developer_key"`) || strings.Contains(noPicker, `data-testid="ds-field-picker_app_id"`) {
		t.Error("picker fields must not render without the drive.file strategy")
	}
	// A provider with no advertised strategies falls back to a text input.
	noStrategies := renderHTML(t, OrgOAuthClientDetailPage("org-1", "Acme",
		DSProvider{Type: "google_drive", Name: "Google Drive", AuthMethods: []string{"oauth"}},
		DSOAuthClientConfig{}, nil, "", nil))
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

	html := renderHTML(t, DataSourceDetailPage(&src, "Google Drive", config, creds, dsSavedCredentialPicker{}, &dsSyncHistoryView{Jobs: []DSSyncJob{job}, Total: 1}, nil, nil, nil, "", nil))
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
	noJobs := renderHTML(t, DataSourceDetailPage(&src, "Google Drive", config, creds, dsSavedCredentialPicker{}, nil, nil, nil, nil, "", nil))
	if !strings.Contains(noJobs, "No syncs yet") {
		t.Error("empty sync history missing")
	}

	// A nil source renders the load-error state.
	errPage := renderHTML(t, DataSourceDetailPage(nil, "", nil, nil, dsSavedCredentialPicker{}, nil, errTest, nil, nil, "", nil))
	if !strings.Contains(errPage, "Data source unavailable") {
		t.Error("load-error state missing")
	}
}

// TestRenderDataSourceDetailPageHidesOAuthCardForConfigProvider pins #2295: a
// provider that authenticates with config fields (no oauth method) must not
// render the OAuth credentials card or its "set one up" warning, while an OAuth
// provider still does.
func TestRenderDataSourceDetailPageHidesOAuthCardForConfigProvider(t *testing.T) {
	webSrc := DSIntegration{ID: "s1", Name: "Crawler", ProviderType: "web", Status: "active"}
	configOnly := &dsCredentialSummary{
		ProviderName: "web",
		SourceID:     "s1",
		AccountLabel: "Web account",
		GrantedLabel: "Not recorded at connect",
		NoOAuth:      true,
	}
	html := renderHTML(t, DataSourceDetailPage(&webSrc, "Web", nil, nil, dsSavedCredentialPicker{}, nil, nil, nil, configOnly, "", nil))
	if strings.Contains(html, `data-testid="ds-credentials-card"`) {
		t.Error("config-only provider must not render the OAuth credentials card")
	}
	if strings.Contains(html, "No OAuth app is resolved") {
		t.Error("config-only provider must not warn about a missing OAuth app")
	}

	oauthSrc := DSIntegration{ID: "s2", Name: "Drive", ProviderType: "google_drive", Status: "active"}
	oauthSummary := &dsCredentialSummary{
		ProviderName: "google_drive",
		SourceID:     "s2",
		AccountLabel: "Google account",
		GrantedLabel: "Read-only Drive access (drive.readonly)",
		GrantedTone:  "success",
		Configured:   true,
	}
	oauthHTML := renderHTML(t, DataSourceDetailPage(&oauthSrc, "Google Drive", nil, nil, dsSavedCredentialPicker{}, nil, nil, nil, oauthSummary, "", nil))
	if !strings.Contains(oauthHTML, `data-testid="ds-credentials-card"`) {
		t.Error("OAuth provider must keep rendering the credentials card")
	}
}

// TestRenderDataSourceDetailPageResourceBanner asserts a source whose config is
// missing its sync target shows the "finish connecting" banner linking to the
// resource step, and a complete source does not.
func TestRenderDataSourceDetailPageResourceBanner(t *testing.T) {
	config, creds := dsConfigFields(testDSSchema(), nil)

	incomplete := &DSIntegration{ID: "s1", Name: "Team Drive", ProviderType: "google_drive"}
	html := renderHTML(t, DataSourceDetailPage(incomplete, "Google Drive", config, creds, dsSavedCredentialPicker{}, nil, nil, dsResourcePromptFor(incomplete), nil, "", nil))
	if !strings.Contains(html, `data-testid="ds-resource-needed"`) {
		t.Error("resource-needed banner missing")
	}
	if !strings.Contains(html, `data-testid="ds-resource-needed-link"`) {
		t.Error("resource-needed action missing")
	}
	// Drive chooses folders in the in-page Google Picker modal, not the legacy
	// resource-listing page, so the action opens the modal and the page carries
	// the picker dialog.
	if !strings.Contains(html, `data-ds-open-folder-picker="true"`) {
		t.Error("resource-needed action must open the folder picker")
	}
	if strings.Contains(html, "/data-sources/s1/resources") {
		t.Error("incomplete Drive source must not link to the retired resource page")
	}
	if !strings.Contains(html, `data-testid="ds-folder-picker"`) {
		t.Error("folder-picker modal missing")
	}
	// The trigger-sync form must not be offered while the sync target is missing.
	if strings.Contains(html, `data-testid="ds-trigger-sync"`) {
		t.Error("trigger-sync form must not render for an incomplete source")
	}
	if strings.Contains(html, `action="/data-sources/s1/sync"`) {
		t.Error("sync action must not render for an incomplete source")
	}

	complete := &DSIntegration{ID: "s2", Name: "Team Drive", ProviderType: "google_drive", Config: map[string]any{"folder_id": "f1"}}
	html = renderHTML(t, DataSourceDetailPage(complete, "Google Drive", config, creds, dsSavedCredentialPicker{}, nil, nil, dsResourcePromptFor(complete), nil, "", nil))
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
		`data-testid="ds-version-no-revision"`, "no source revision",
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

// TestRenderDocumentVersionRow asserts the version row always renders a
// revision slot (a muted "no source revision" when absent) and links the
// extraction-counts line to the version's extraction history.
func TestRenderDocumentVersionRow(t *testing.T) {
	rev := "r1"
	lastExtract := "2026-09-01T12:00:00Z"
	withCounts := DocumentVersion{
		ID:               "v9",
		Version:          9,
		SourceRevisionID: &rev,
		CreatedAt:        "2026-09-01T00:00:00Z",
		ExtractionJobs:   3,
		ObjectsCreated:   5,
		LastExtractionAt: &lastExtract,
	}
	html := renderHTML(t, dsVersionRow(withCounts))
	for _, want := range []string{
		"rev r1",
		`href="/documents/v9/extractions"`,
		`data-testid="ds-version-extractions-link"`,
		"3 extraction jobs",
		"5 objects",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("version row missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, `data-testid="ds-version-no-revision"`) {
		t.Errorf("version with a revision must not render the no-revision slot:\n%s", html)
	}

	noRev := DocumentVersion{ID: "v10", Version: 10, CreatedAt: "2026-09-02T00:00:00Z"}
	htmlNoRev := renderHTML(t, dsVersionRow(noRev))
	if !strings.Contains(htmlNoRev, `data-testid="ds-version-no-revision"`) ||
		!strings.Contains(htmlNoRev, "no source revision") {
		t.Errorf("version without a revision should render the muted no-revision slot:\n%s", htmlNoRev)
	}
	if strings.Contains(htmlNoRev, "rev ") {
		t.Errorf("version without a revision must not render a revision value:\n%s", htmlNoRev)
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
	e.POST("/data-sources/:id/field-options", s.uiDataSourceFieldOptions)
	e.GET("/data-sources/oauth/start", s.uiDataSourceOAuthStart)
	e.GET("/data-sources/oauth/callback", s.uiDataSourceOAuthCallback)
	e.GET("/data-sources/:id", s.uiDataSourceDetail)
	e.GET("/data-sources/:id/reconnect", s.uiDataSourceReconnect)
	e.GET("/data-sources/:id/picker-session", s.uiDataSourcePickerSession)
	e.POST("/data-sources/:id/sync-targets", s.uiDataSourceSyncTargetsSave)
	e.GET("/data-sources/:id/resources", s.uiDataSourceResources)
	e.GET("/data-sources/:id/resources/list", s.uiDataSourceResourceList)
	e.POST("/data-sources/:id/resources", s.uiDataSourceResourceSave)
	e.POST("/data-sources/:id/update", s.uiDataSourceUpdate)
	e.POST("/data-sources/:id/test-connection", s.uiDataSourceTestConnection)
	e.POST("/data-sources/:id/sync", s.uiDataSourceTriggerSync)
	e.GET("/data-sources/:id/sync-jobs/partial", s.uiDataSourceSyncJobsPartial)
	e.POST("/data-sources/:id/sync-jobs/:jobId/cancel", s.uiDataSourceCancelSync)
	e.POST("/data-sources/:id/delete", s.uiDataSourceDelete)
	e.GET("/orgs/:id/settings/oauth-clients", s.uiOrgOAuthClients)
	e.GET("/orgs/:id/settings/oauth-clients/:providerType", s.uiOrgOAuthClientDetail)
	e.POST("/orgs/:id/settings/oauth-clients/:providerType", s.uiOrgOAuthClientUpsert)
	e.POST("/orgs/:id/settings/oauth-clients/:providerType/delete", s.uiOrgOAuthClientDelete)
	e.GET("/settings/credentials", s.uiProjectCredentials)
	e.GET("/settings/credentials/saved/fields", s.uiSavedCredentialFields)
	e.POST("/settings/credentials/saved", s.uiSavedCredentialCreate)
	e.POST("/settings/credentials/saved/:id/update", s.uiSavedCredentialUpdate)
	e.POST("/settings/credentials/saved/:id/delete", s.uiSavedCredentialDelete)
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
	form := renderHTML(t, DataSourceNewPage([]DSProvider{*p}, p, configFields, credentialFields, dsSavedCredentialPicker{}, nil, nil, nil))

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

// webSchema is the connect form's web contract as the server now serves it:
// crawl-scope config only, no credential fields.
func webSchema() *DSProviderSchema {
	return &DSProviderSchema{
		Type: "web",
		Properties: map[string]any{
			"start_urls":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"include_globs": map[string]any{"type": "array"},
			"exclude_globs": map[string]any{"type": "array"},
			"max_pages":     map[string]any{"type": "integer"},
			"max_depth":     map[string]any{"type": "integer"},
		},
		Required: []string{"start_urls"},
	}
}

// TestDSConfigFieldsWebOrderCrawlScope asserts the web crawl-scope keys surface
// first, in crawl order, so the connect form reads start URLs → filters → limits.
func TestDSConfigFieldsWebOrderCrawlScope(t *testing.T) {
	config, creds := dsConfigFields(webSchema(), nil)
	if len(config) != 5 || len(creds) != 0 {
		t.Fatalf("config=%+v creds=%+v", config, creds)
	}
	want := []string{"start_urls", "include_globs", "exclude_globs", "max_pages", "max_depth"}
	for i, key := range want {
		if config[i].Key != key {
			t.Errorf("config[%d].Key = %q, want %q (order=%+v)", i, config[i].Key, key, config)
		}
	}
	if !config[0].Required {
		t.Errorf("start_urls must be required: %+v", config[0])
	}
}

// TestRenderDataSourceNewPageConfigOnlyChooser asserts a config-only provider
// renders a "Configure source" action and no credentials/OAuth action.
func TestRenderDataSourceNewPageConfigOnlyChooser(t *testing.T) {
	web := &DSProvider{Type: "web", Name: "Web", SourceType: "web", AuthMethods: []string{"config"}}
	chooser := renderHTML(t, DataSourceNewPage([]DSProvider{*web}, nil, nil, nil, dsSavedCredentialPicker{}, nil, nil, nil))
	for _, want := range []string{
		"Web",
		`data-testid="ds-provider-choice"`,
		`data-testid="ds-config-connect"`,
		`href="/data-sources/new?providerType=web&amp;method=config"`,
		"Configure source",
	} {
		if !strings.Contains(chooser, want) {
			t.Errorf("config-only chooser missing %q", want)
		}
	}
	if strings.Contains(chooser, `data-testid="ds-credentials-connect"`) {
		t.Error("config-only provider must not render an 'Enter credentials' action")
	}
	if strings.Contains(chooser, `data-testid="ds-oauth-connect"`) {
		t.Error("config-only provider must not render an OAuth action")
	}
}

// TestRenderDataSourceNewPageDualCapabilityUnchanged is the regression guard: a
// provider advertising both oauth and credentials keeps its two connect actions
// and gains no config-only action.
func TestRenderDataSourceNewPageDualCapabilityUnchanged(t *testing.T) {
	github := &DSProvider{Type: "github", Name: "GitHub", AuthMethods: []string{"oauth", "credentials"}}
	html := renderHTML(t, DataSourceNewPage([]DSProvider{*github}, nil, nil, nil, dsSavedCredentialPicker{}, nil, nil, nil))
	for _, want := range []string{
		`data-testid="ds-oauth-connect"`,
		`data-testid="ds-credentials-connect"`,
		`href="/data-sources/new?providerType=github&amp;method=credentials"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("dual-capability chooser missing %q", want)
		}
	}
	if strings.Contains(html, `data-testid="ds-config-connect"`) {
		t.Error("dual-capability provider must not render a config-only action")
	}
}

// TestUIDataSourceNewConfigOnlyConnectForm asserts ?providerType=web renders the
// crawl-scope connect form with no credentials panel.
func TestUIDataSourceNewConfigOnlyConnectForm(t *testing.T) {
	f := &fakeMemory{
		dsProviders: []DSProvider{{Type: "web", Name: "Web", SourceType: "web", AuthMethods: []string{"config"}}},
		dsSchema:    webSchema(),
	}
	e := newDSUIServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/new?providerType=web&method=config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="ds-connect-form"`,
		`name="providerType" value="web"`,
		`data-testid="ds-field-start_urls"`,
		`data-testid="ds-field-include_globs"`,
		`data-testid="ds-field-exclude_globs"`,
		`data-testid="ds-field-max_pages"`,
		`data-testid="ds-field-max_depth"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("config-only connect form missing %q", want)
		}
	}
	if strings.Contains(body, `data-testid="ds-credentials-empty"`) {
		t.Error("config-only provider must not render the credentials panel")
	}
	if strings.Contains(body, `type="password"`) {
		t.Error("config-only provider must not render password inputs")
	}
	if strings.Contains(body, "This provider cannot be connected yet.") {
		t.Error("config-only provider must render the connect form, not the unsupported alert")
	}
}

// TestUIDataSourceNewConfigOnlyCredentialsMethodBounce asserts the error bounce
// (dsConnectFormPath hardcodes method=credentials) still lands on a usable form
// for a config-only provider, because the method hint does not narrow a provider
// that does not advertise credentials.
func TestUIDataSourceNewConfigOnlyCredentialsMethodBounce(t *testing.T) {
	f := &fakeMemory{
		dsProviders: []DSProvider{{Type: "web", Name: "Web", SourceType: "web", AuthMethods: []string{"config"}}},
		dsSchema:    webSchema(),
	}
	e := newDSUIServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/new?providerType=web&method=credentials", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="ds-connect-form"`) || !strings.Contains(body, `name="providerType" value="web"`) {
		t.Errorf("bounce to method=credentials must still render the connect form: %s", body)
	}
	if strings.Contains(body, "This provider cannot be connected yet.") {
		t.Error("bounce must not render the unsupported alert")
	}
}

// TestUIDataSourceCreateConfigOnlyProvider asserts the create submit path posts
// start_urls as a list and max_pages as a number (the schema-driven form parsing).
func TestUIDataSourceCreateConfigOnlyProvider(t *testing.T) {
	f := &fakeMemory{
		dsProviders: []DSProvider{{Type: "web", Name: "Web", SourceType: "web", AuthMethods: []string{"config"}}},
		dsSchema:    webSchema(),
	}
	e := newDSUIServer(f)

	form := url.Values{
		"providerType": {"web"},
		"name":         {"Docs"},
		"sync_mode":    {"manual"},
		"start_urls":   {"https://example.com/a\nhttps://example.com/b"},
		"max_pages":    {"42"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/data-sources/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if f.dsCreatedReq == nil {
		t.Fatal("CreateDSIntegration was not called")
	}
	if f.dsCreatedReq.ProviderType != "web" {
		t.Errorf("providerType = %q, want web", f.dsCreatedReq.ProviderType)
	}
	urls, ok := f.dsCreatedReq.Config["start_urls"].([]string)
	if !ok || len(urls) != 2 || urls[0] != "https://example.com/a" || urls[1] != "https://example.com/b" {
		t.Errorf("start_urls = %#v, want a two-item list", f.dsCreatedReq.Config["start_urls"])
	}
	if f.dsCreatedReq.Config["max_pages"] != 42 {
		t.Errorf("max_pages = %#v, want the integer 42", f.dsCreatedReq.Config["max_pages"])
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

	// reconnected → the same source, described as a repair rather than a new one.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/oauth/callback?reconnected=s1", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("reconnected status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/data-sources/s1?reconnected=1" {
		t.Errorf("reconnected location = %q, want the source with the renewed flash", loc)
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

	// A reconnect reaches the same delegated path with its own outcome key.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources?reconnected=s1", nil))
	if loc := rec.Header().Get("Location"); loc != "/data-sources/s1?reconnected=1" {
		t.Errorf("delegated reconnect location = %q", loc)
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
		dsIntegrations: []DSIntegration{{ID: "s1", Name: "Acme repo", ProviderType: "github"}},
		dsProviders:    []DSProvider{{Type: "github", Name: "GitHub", AuthMethods: []string{"oauth"}}},
		dsResources:    []DSResource{{ID: "acme/repo", Name: "acme/repo", Type: "repo"}},
	}
	e := newDSUIServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/s1/resources", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="ds-resource-step"`) || !strings.Contains(body, "Choose a resource") {
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
	if strings.Contains(body, "acme/repo") {
		t.Errorf("page route must not render resources inline: %s", body)
	}
}

func TestUIDataSourceResourceListRoute(t *testing.T) {
	f := &fakeMemory{
		dsIntegrations: []DSIntegration{{ID: "s1", Name: "Acme repo", ProviderType: "github"}},
		dsProviders:    []DSProvider{{Type: "github", Name: "GitHub", AuthMethods: []string{"oauth"}}},
		dsResources:    []DSResource{{ID: "acme/repo", Name: "acme/repo", Type: "repo"}},
	}
	e := newDSUIServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/s1/resources/list", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "acme/repo") || !strings.Contains(body, `data-testid="ds-resource-row"`) {
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
		dsIntegrations: []DSIntegration{{ID: "s1", Name: "Acme repo", ProviderType: "github"}},
		dsProviders:    []DSProvider{{Type: "github", Name: "GitHub", AuthMethods: []string{"oauth"}}},
		dsResources:    []DSResource{{ID: "acme/repo", Name: "acme/repo", Type: "repo"}},
	}
	e := newDSUIServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/s1/resources?static=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "acme/repo") {
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
		dsIntegrations: []DSIntegration{{ID: "s1", Name: "Acme repo", ProviderType: "github"}},
		dsProviders:    []DSProvider{{Type: "github", Name: "GitHub", AuthMethods: []string{"oauth"}}},
		dsResources:    []DSResource{{ID: "acme/repo", Name: "acme/repo", Type: "repo"}},
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
	// github maps the picked resource to repo and redirects to ?updated=1.
	f := &fakeMemory{dsIntegrations: []DSIntegration{{ID: "s2", ProviderType: "github"}}}
	e := newDSUIServer(f)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/data-sources/s2/resources", strings.NewReader("resource_id=acme/repo"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("github status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/data-sources/s2?updated=1" {
		t.Errorf("github location = %q", loc)
	}
	if f.dsSavedResourceCfg["repo"] != "acme/repo" {
		t.Fatalf("github save cfg = %+v", f.dsSavedResourceCfg)
	}

	// A missing resource id redirects back with an error and never reaches memory.
	f = &fakeMemory{dsIntegrations: []DSIntegration{{ID: "s1", ProviderType: "github"}}}
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
		dsIntegrations: []DSIntegration{{ID: "s1", ProviderType: "github"}},
		dsUpdateErr:    errTest,
	}
	e = newDSUIServer(f)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/data-sources/s1/resources", strings.NewReader("resource_id=acme/repo"))
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
		ID: "s1", Name: "Acme repo", ProviderType: "github",
		Config: map[string]any{"mime_types": []any{"application/pdf"}, "recursive": true},
	}}}
	e := newDSUIServer(f)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/data-sources/s1/resources", strings.NewReader("resource_id=acme/repo"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.dsSavedResourceCfg["repo"] != "acme/repo" {
		t.Fatalf("repo missing from saved cfg: %+v", f.dsSavedResourceCfg)
	}
	if _, ok := f.dsSavedResourceCfg["mime_types"]; !ok {
		t.Errorf("stored mime_types lost in merge: %+v", f.dsSavedResourceCfg)
	}
	if v, ok := f.dsSavedResourceCfg["recursive"].(bool); !ok || !v {
		t.Errorf("stored recursive lost in merge: %+v", f.dsSavedResourceCfg)
	}
}

func TestDSFoldersCanonicalAndLegacy(t *testing.T) {
	canonical := &DSIntegration{Config: map[string]any{
		"folders": []any{
			map[string]any{"folder_id": "f1", "name": "Reports", "drive_id": "d1"},
			map[string]any{"folder_id": "f2"},
			map[string]any{"name": "no id"},
			"not a map",
		},
	}}
	got := dsFolders(canonical)
	if len(got) != 2 {
		t.Fatalf("folders = %+v, want 2", got)
	}
	if got[0].FolderID != "f1" || got[0].Name != "Reports" || got[0].DriveID != "d1" {
		t.Errorf("first folder = %+v", got[0])
	}
	if got[1].FolderID != "f2" || got[1].Name != "" {
		t.Errorf("second folder = %+v", got[1])
	}

	// Legacy single-target keys stay readable until the next save canonicalises
	// them.
	legacy := &DSIntegration{Config: map[string]any{"folder_id": "f9", "shared_drive_id": "d9"}}
	got = dsFolders(legacy)
	if len(got) != 1 || got[0].FolderID != "f9" || got[0].DriveID != "d9" {
		t.Fatalf("legacy folders = %+v", got)
	}
	if dsFolders(nil) != nil {
		t.Error("nil source must have no folders")
	}
}

func TestDSSourceHasSyncTarget(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  *DSIntegration
		want bool
	}{
		{"drive canonical", &DSIntegration{ProviderType: "google_drive", Config: map[string]any{"folders": []any{map[string]any{"folder_id": "f1"}}}}, true},
		{"drive legacy", &DSIntegration{ProviderType: "google_drive", Config: map[string]any{"folder_id": "f1"}}, true},
		{"drive empty", &DSIntegration{ProviderType: "google_drive", Config: map[string]any{}}, false},
		{"github repo", &DSIntegration{ProviderType: "github", Config: map[string]any{"repo": "acme/repo"}}, true},
		{"github empty", &DSIntegration{ProviderType: "github", Config: map[string]any{}}, false},
		{"provider without target", &DSIntegration{ProviderType: "notion"}, true},
		{"nil", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dsSourceHasSyncTarget(tc.src); got != tc.want {
				t.Errorf("dsSourceHasSyncTarget = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestDSSettingsConfigFieldsDropsFolderPicker keeps the raw folder target out of
// the settings form: folders are chosen in the picker and preserved by the
// server across a settings save.
func TestDSSettingsConfigFieldsDropsFolderPicker(t *testing.T) {
	config, _ := dsConfigFields(testDSSchema(), nil)
	got := dsSettingsConfigFields(config)
	if len(got) != 2 || got[0].Key != "mime_types" || got[1].Key != "recursive" {
		t.Fatalf("settings config fields = %+v", got)
	}
}

func TestRenderDataSourceDetailPageFolderTargets(t *testing.T) {
	src := DSIntegration{
		ID: "s1", Name: "Team Drive", ProviderType: "google_drive", Status: "active", SyncMode: "scheduled",
		Config: map[string]any{
			"folders": []any{
				map[string]any{"folder_id": "f1", "name": "Reports", "drive_id": "d1"},
				map[string]any{"folder_id": "f2", "name": "Contracts"},
			},
			"mime_types": []any{"application/pdf"},
		},
	}
	config, creds := dsConfigFields(testDSSchema(), src.Config)
	summary := &dsCredentialSummary{
		ProviderName:    "google_drive",
		ScopeLabel:      "Organization app",
		ClientID:        "12345678…abcd",
		AccountEmail:    "ops@example.com",
		GrantedLabel:    "Read-only Drive access (drive.readonly)",
		GrantedTone:     "success",
		Configured:      true,
		SettingsHref:    "/settings/credentials/google_drive",
		GrantedStrategy: "drive.readonly",
	}
	html := renderHTML(t, DataSourceDetailPage(&src, "Google Drive", config, creds, dsSavedCredentialPicker{}, nil, nil, nil, summary, "", nil))
	for _, want := range []string{
		`data-testid="ds-sync-scope"`,
		`data-testid="ds-folder-targets"`,
		"Reports",
		"Contracts",
		"Shared drive",
		`data-testid="ds-credentials-card"`,
		"Organization app",
		"ops@example.com",
		"drive.readonly",
		`data-testid="ds-folder-picker"`,
		`data-ds-open-folder-picker="true"`,
		`data-testid="ds-picker-account-hint"`,
		// The Picker iframe 401s ("Sign in to your Google Account") when the
		// browser has no Google session, and the cross-origin frame gives no
		// signal we can render — so the modal has to say it up front (#2108).
		"Use the account that connected this source",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("detail page missing %q", want)
		}
	}
	if strings.Contains(html, `data-testid="ds-field-folders"`) {
		t.Error("settings form must not render the raw folders field")
	}
	if strings.Contains(html, `data-testid="ds-resource-needed"`) {
		t.Error("complete source must not prompt for a sync target")
	}
}

func TestRenderDataSourceDetailPageDriveFileBlocked(t *testing.T) {
	src := DSIntegration{
		ID: "s1", Name: "Legacy Drive", ProviderType: "google_drive", Status: "active",
		OAuthStrategy: "drive.file",
		Config:        map[string]any{},
	}
	summary := &dsCredentialSummary{
		ProviderName:  "google_drive",
		SourceID:      "s1",
		ScopeLabel:    "Deployment default",
		GrantedLabel:  "Per-file access (drive.file)",
		GrantedTone:   "warning",
		PickerBlocked: true,
		PickerError:   "This source was authorized with per-file access (drive.file), so Google will not let the folder picker browse its folders.",
		Configured:    true,
		SettingsHref:  "/settings/credentials/google_drive",
	}
	html := renderHTML(t, DataSourceDetailPage(&src, "Google Drive", nil, nil, dsSavedCredentialPicker{}, nil, nil, dsResourcePromptFor(&src), summary, "", nil))
	if !strings.Contains(html, `data-testid="ds-picker-blocked"`) {
		t.Error("drive.file notice missing")
	}
	if !strings.Contains(html, `data-testid="ds-picker-reconnect"`) {
		t.Error("drive.file reconnect action missing")
	}
	// The action repairs this source; the old target was the new-source flow,
	// which produced a duplicate and lost the config (#2107).
	if !strings.Contains(html, `href="/data-sources/s1/reconnect"`) {
		t.Errorf("drive.file reconnect action does not target the source:\n%s", html)
	}
	if strings.Contains(html, "/data-sources/new?providerType=") {
		t.Error("reconnect action must not link to the new-source flow")
	}
	if strings.Contains(html, `data-testid="ds-folder-picker"`) {
		t.Error("drive.file source must not render the folder picker modal")
	}
	if strings.Contains(html, `data-ds-open-folder-picker`) {
		t.Error("drive.file source must not offer the picker action")
	}
	if strings.Contains(html, `data-testid="ds-resource-needed"`) {
		t.Error("blocked source should show the notice instead of the picker prompt")
	}
}

func TestUIDataSourceResourcesDriveRedirects(t *testing.T) {
	f := &fakeMemory{dsIntegrations: []DSIntegration{{ID: "s1", ProviderType: "google_drive"}}}
	e := newDSUIServer(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/s1/resources", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/data-sources/s1?picker=1" {
		t.Errorf("location = %q, want the detail page with the picker open", loc)
	}
	if f.dsResourcesCallCount != 0 {
		t.Errorf("ListDSResources called %d times, want 0", f.dsResourcesCallCount)
	}

	// htmx is how the deferred fragment is actually fetched, so the bounce has to
	// arrive as HX-Redirect: a 303 would be followed as a full page navigation
	// and lose the swap context.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/data-sources/s1/resources", nil)
	req.Header.Set("HX-Request", "true")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("hx status = %d, want 200", rec.Code)
	}
	if loc := rec.Header().Get("HX-Redirect"); loc != "/data-sources/s1?picker=1" {
		t.Errorf("HX-Redirect = %q, want the detail page with the picker open", loc)
	}
	if f.dsResourcesCallCount != 0 {
		t.Errorf("ListDSResources called %d times on the htmx path, want 0", f.dsResourcesCallCount)
	}
}

// TestUIDataSourceResourceListDriveRedirects: the deferred fragment must bounce
// to the same place as its page. Drive picks targets in the Google Picker, so a
// direct or stale hit on /resources/list must not list folders (and must not
// reach the backend at all) — and because htmx issues that request, the bounce
// must be HX-Redirect rather than a 303.
func TestUIDataSourceResourceListDriveRedirects(t *testing.T) {
	f := &fakeMemory{dsIntegrations: []DSIntegration{{ID: "s1", ProviderType: "google_drive"}}}
	e := newDSUIServer(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/s1/resources/list", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/data-sources/s1?picker=1" {
		t.Errorf("location = %q, want the detail page with the picker open", loc)
	}
	if f.dsResourcesCallCount != 0 {
		t.Errorf("ListDSResources called %d times, want 0", f.dsResourcesCallCount)
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/data-sources/s1/resources/list", nil)
	req.Header.Set("HX-Request", "true")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("hx status = %d, want 200", rec.Code)
	}
	if loc := rec.Header().Get("HX-Redirect"); loc != "/data-sources/s1?picker=1" {
		t.Errorf("HX-Redirect = %q, want the detail page with the picker open", loc)
	}
	if f.dsResourcesCallCount != 0 {
		t.Errorf("ListDSResources called %d times on the htmx path, want 0", f.dsResourcesCallCount)
	}
}

// TestUIDataSourceResourceSaveDriveRedirects: the server-rendered resource form
// no longer exists for Drive (its page and fragment bounce), so a stale POST must
// be answered on the page that owns the action. Previously it failed with
// "resource selection is not supported for provider google_drive" and sent that
// message to /resources, which bounced to the picker and dropped it.
func TestUIDataSourceResourceSaveDriveRedirects(t *testing.T) {
	f := &fakeMemory{dsIntegrations: []DSIntegration{{ID: "s1", ProviderType: "google_drive"}}}
	e := newDSUIServer(f)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/data-sources/s1/resources", strings.NewReader(url.Values{"resource_id": {"f1"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/data-sources/s1?picker=1&err=") {
		t.Errorf("location = %q, want the picker page carrying the error", loc)
	}
	if !strings.Contains(loc, "Drive+picker") {
		t.Errorf("location = %q, want the picker-specific message", loc)
	}
	if f.dsSavedResourceID != "" {
		t.Errorf("SaveDSResource called for a Drive source (id %q), want no write", f.dsSavedResourceID)
	}
}

// TestDSCredentialSummaryDistinguishesLookupFailure asserts a credentials-API
// failure is not reported as "not configured": the second message would send the
// user to create a duplicate OAuth client when the app may be fine.
func TestDSCredentialSummaryDistinguishesLookupFailure(t *testing.T) {
	src := &DSIntegration{ID: "s1", ProviderType: "google_drive"}

	failed := &Server{cfg: Config{DefaultAgent: "memory"}, memory: &fakeMemory{dsErr: &memoryHTTPError{Status: http.StatusInternalServerError, Message: "credentials unavailable"}}}
	failedSummary := failed.dsCredentialSummary(context.Background(), src, nil)
	if failedSummary.ScopeLabel != "Could not load credentials" {
		t.Errorf("ScopeLabel after a lookup failure = %q, want %q", failedSummary.ScopeLabel, "Could not load credentials")
	}
	// The card follows the same distinction: a lookup failure must not tell the
	// user to create a second OAuth client.
	html := renderHTML(t, DataSourceDetailPage(src, "Google Drive", nil, nil, dsSavedCredentialPicker{}, nil, nil, nil, failedSummary, "", nil))
	if !strings.Contains(html, "ds-credential-lookup-failed") {
		t.Error("lookup failure must render its own note")
	}
	if strings.Contains(html, "Set one up in credential settings") {
		t.Error("a lookup failure must not advise creating an OAuth app")
	}

	// The catalog knows this provider but no app is resolved for it, which is the
	// case the "set one up" advice is for (a nil entry means the catalog itself
	// failed to load, covered by TestDSDetailPageOmitsTheMissingAppWarningWhenTheCatalogIsUnknown).
	drive := DSProvider{Type: "google_drive", Name: "Google Drive", VendorName: "Google", AuthMethods: []string{"oauth"}}
	empty := &Server{cfg: Config{DefaultAgent: "memory"}, memory: &fakeMemory{}}
	emptySummary := empty.dsCredentialSummary(context.Background(), src, &drive)
	if emptySummary.ScopeLabel != "Not configured" {
		t.Errorf("ScopeLabel with no client = %q, want %q", emptySummary.ScopeLabel, "Not configured")
	}
	html = renderHTML(t, DataSourceDetailPage(src, "Google Drive", nil, nil, dsSavedCredentialPicker{}, nil, nil, nil, emptySummary, "", nil))
	if !strings.Contains(html, "ds-credential-missing") {
		t.Error("an unconfigured provider must advertise setting an app up")
	}
}

func TestUIDataSourcePickerSessionRoute(t *testing.T) {
	f := &fakeMemory{}
	e := newDSUIServer(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/s1/picker-session", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	var session DSPickerSession
	if err := json.Unmarshal(rec.Body.Bytes(), &session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if session.AccessToken != "picker-token" || session.DeveloperKey != "dev-key" || session.AppID != "123" {
		t.Errorf("session = %+v", session)
	}

	// A backend failure is a JSON error, never an HTML error page the modal
	// script would choke on — and it carries the server's actionable copy, so
	// "the Picker is not configured" reaches the user instead of a generic
	// failure line.
	wantMsg := "the Google Picker is not configured: set the Picker developer key and Picker app ID under Settings → Credentials"
	f = &fakeMemory{dsErr: &memoryHTTPError{Status: http.StatusUnprocessableEntity, Code: "validation_error", Message: wantMsg}}
	e = newDSUIServer(f)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/s1/picker-session", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("error status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	if payload["error"] != wantMsg {
		t.Errorf("error = %q, want the server's validation copy %q", payload["error"], wantMsg)
	}

	// A 5xx is redacted: transport detail and upstream identifiers never reach
	// the browser.
	f = &fakeMemory{dsErr: &memoryHTTPError{Status: http.StatusInternalServerError, Message: "memory 500: dial tcp 10.0.0.5:8080: connection refused"}}
	e = newDSUIServer(f)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/s1/picker-session", nil))
	if bytes.Contains(rec.Body.Bytes(), []byte("10.0.0.5")) {
		t.Errorf("error body leaks backend detail: %s", rec.Body.String())
	}
}

func TestUIDataSourceSyncTargetsSaveRoute(t *testing.T) {
	f := &fakeMemory{dsIntegrations: []DSIntegration{{ID: "s1", ProviderType: "google_drive"}}}
	e := newDSUIServer(f)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/data-sources/s1/sync-targets", strings.NewReader(`{"folderIds":["f1","f2"]}`))
	req.Header.Set("Content-Type", "application/json")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	folders, ok := f.dsIntegrations[0].Config["folders"].([]any)
	if !ok || len(folders) != 2 {
		t.Fatalf("saved folders = %#v", f.dsIntegrations[0].Config["folders"])
	}

	// An empty selection is a validation error, not a silent clear.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/data-sources/s1/sync-targets", strings.NewReader(`{"folderIds":[]}`))
	req.Header.Set("Content-Type", "application/json")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty status = %d, body=%s", rec.Code, rec.Body.String())
	}

	// A provider rejection comes back as JSON for the modal to render inline,
	// with the server's own message (here: the target bound) rather than a
	// generic failure line.
	wantMsg := "select at most 50 folders"
	f = &fakeMemory{
		dsIntegrations: []DSIntegration{{ID: "s1", ProviderType: "google_drive"}},
		dsErr:          &memoryHTTPError{Status: http.StatusUnprocessableEntity, Code: "validation_error", Message: wantMsg},
	}
	e = newDSUIServer(f)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/data-sources/s1/sync-targets", strings.NewReader(`{"folderIds":["f1"]}`))
	req.Header.Set("Content-Type", "application/json")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("rejected status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode rejected payload: %v", err)
	}
	if payload["error"] != wantMsg {
		t.Errorf("error = %q, want the provider's copy %q", payload["error"], wantMsg)
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

// TestUIOrgOAuthClientDetailPage covers the per-provider org editor route: the
// shared card renders for an org admin, while an unknown, non-OAuth or
// self-registering provider is a 404, and a non-admin is refused the same way.
func TestUIOrgOAuthClientDetailPage(t *testing.T) {
	e := newDSUIServer(orgOAuthAdminFake())

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/orgs/o1/settings/oauth-clients/google_drive", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("admin detail status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="ds-oauth-client-detail"`,
		`data-testid="ds-oauth-client-form"`,
		// Served pages canonicalise /orgs/{id} to /o/{id}.
		`action="/o/o1/settings/oauth-clients/google_drive"`,
		`value="cid-123"`,
		`data-testid="ds-oauth-app-back"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("editor body missing %q", want)
		}
	}

	for _, tc := range []struct {
		name string
		fake *fakeMemory
		path string
	}{
		{
			name: "unknown provider",
			fake: orgOAuthAdminFake(),
			path: "/orgs/o1/settings/oauth-clients/nope",
		},
		{
			name: "credentials-only provider has no app editor",
			fake: &fakeMemory{
				orgs:            []Org{{ID: "o1", Name: "Acme"}},
				orgsAndProjects: []OrgWithProjectsDto{{ID: "o1", Name: "Acme", Role: "org_admin"}},
				dsProviders:     []DSProvider{{Type: "github", Name: "GitHub", AuthMethods: []string{"credentials"}}},
			},
			path: "/orgs/o1/settings/oauth-clients/github",
		},
		{
			name: "self-registering provider has no app editor",
			fake: &fakeMemory{
				orgs:            []Org{{ID: "o1", Name: "Acme"}},
				orgsAndProjects: []OrgWithProjectsDto{{ID: "o1", Name: "Acme", Role: "org_admin"}},
				dsProviders:     []DSProvider{{Type: "trale", Name: "Trale", AuthMethods: []string{"oauth"}, SelfRegistering: true}},
			},
			path: "/orgs/o1/settings/oauth-clients/trale",
		},
		{
			name: "non-admin",
			fake: &fakeMemory{orgsAndProjects: []OrgWithProjectsDto{{ID: "o1", Name: "Acme", Role: "org_member"}}},
			path: "/orgs/o1/settings/oauth-clients/google_drive",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			newDSUIServer(tc.fake).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", rec.Code)
			}
		})
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

	// A Picker app id that is not the numeric Cloud project number is rejected:
	// Google would mount an empty picker for it, so fail while the form is open.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/orgs/o1/settings/oauth-clients/google_drive", strings.NewReader("client_id=cid&picker_app_id=my-project-id"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("picker app id validation status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "err=") || !strings.Contains(loc, "project+number") {
		t.Errorf("picker app id validation location = %q", loc)
	}
	if f.dsUpsertedOAuthReq != nil {
		t.Error("memory must not be called with an invalid picker app id")
	}

	// Success → 303 with the saved flash, request forwarded with the picker
	// config merged over the stored (unmanaged) keys.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/orgs/o1/settings/oauth-clients/google_drive", strings.NewReader("client_id=cid&client_secret=sec&scopes=scope.a%2Cscope.b&strategy=drive.file&picker_developer_key=dev-key&picker_app_id=123456789012"))
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
	if got.Config["pickerDeveloperKey"] != "dev-key" || got.Config["pickerAppId"] != "123456789012" {
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

// TestUIOrgOAuthClientWriteRejectsUnmanageableProviders covers the write-path
// guard: the upsert and delete handlers must 404 for an unknown, non-OAuth
// (credentials-only), or self-registering provider — before touching the memory
// service — mirroring the GET editor route.
func TestUIOrgOAuthClientWriteRejectsUnmanageableProviders(t *testing.T) {
	for _, tc := range []struct {
		name     string
		fake     *fakeMemory
		provider string
	}{
		{
			name:     "unknown provider",
			fake:     orgOAuthAdminFake(),
			provider: "nope",
		},
		{
			name: "credentials-only provider has no app",
			fake: &fakeMemory{
				orgs:            []Org{{ID: "o1", Name: "Acme"}},
				orgsAndProjects: []OrgWithProjectsDto{{ID: "o1", Name: "Acme", Role: "org_admin"}},
				dsProviders:     []DSProvider{{Type: "github", Name: "GitHub", AuthMethods: []string{"credentials"}}},
			},
			provider: "github",
		},
		{
			name: "self-registering provider has no app",
			fake: &fakeMemory{
				orgs:            []Org{{ID: "o1", Name: "Acme"}},
				orgsAndProjects: []OrgWithProjectsDto{{ID: "o1", Name: "Acme", Role: "org_admin"}},
				dsProviders:     []DSProvider{{Type: "trale", Name: "Trale", AuthMethods: []string{"oauth"}, SelfRegistering: true}},
			},
			provider: "trale",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.fake
			e := newDSUIServer(f)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/orgs/o1/settings/oauth-clients/"+tc.provider, strings.NewReader("client_id=cid"))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			e.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("upsert status = %d, want 404", rec.Code)
			}
			if f.dsUpsertedOAuthReq != nil {
				t.Error("upsert must not reach the memory service for an unmanageable provider")
			}

			rec = httptest.NewRecorder()
			req = httptest.NewRequest(http.MethodPost, "/orgs/o1/settings/oauth-clients/"+tc.provider+"/delete", nil)
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			e.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("delete status = %d, want 404", rec.Code)
			}
			if f.dsDeletedOAuthOrg != "" || f.dsDeletedOAuthProv != "" {
				t.Error("delete must not reach the memory service for an unmanageable provider")
			}
		})
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
		"Saved credentials",
		"OAuth apps",
		"Google OAuth app",
		"GitHub OAuth app",
		"Google Drive",
		"Project",
		"Organization",
		"pcid",
		"ocid",
		"In use",
		`data-testid="ds-saved-credentials"`,
		`data-testid="ds-oauth-apps"`,
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

// TestDSCredentialRowsOrganizationScopeOnlyWhenConfigured pins the row-visibility
// rule: every provider keeps its project row (it carries the admin Set up action),
// while an unconfigured organization row is omitted, so the table does not double
// in size with rows that can never resolve.
func TestDSCredentialRowsOrganizationScopeOnlyWhenConfigured(t *testing.T) {
	providers := []DSProvider{
		{Type: "google_drive", Name: "Google Drive", VendorName: "Google", AuthMethods: []string{"oauth"}},
		{Type: "github", Name: "GitHub", VendorName: "GitHub", AuthMethods: []string{"oauth", "credentials"}},
	}
	creds := []DSOAuthCredentialDescription{{
		ProviderType:  "google_drive",
		Organization:  DSOAuthCredentialSource{Configured: true, ClientID: "ocid"},
		ResolvedScope: dsCredentialScopeOrganization,
	}}
	rows, _ := dsCredentialRows(providers, creds, false, true, "o1")
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (two project rows + one configured org row): %+v", len(rows), rows)
	}
	var driveOrg, githubOrg *dsCredentialRow
	for i := range rows {
		switch {
		case rows[i].ProviderType == "google_drive" && rows[i].ScopeKey == "organization":
			driveOrg = &rows[i]
		case rows[i].ProviderType == "github" && rows[i].ScopeKey == "organization":
			githubOrg = &rows[i]
		}
	}
	if githubOrg != nil {
		t.Error("an unconfigured organization row must be omitted")
	}
	if driveOrg == nil {
		t.Fatal("the configured organization row is missing")
	}
	if !driveOrg.Resolved {
		t.Error("the org row should be In use when the project has no app")
	}
	if driveOrg.ManageHref != "/orgs/o1/settings/oauth-clients/google_drive" {
		t.Errorf("org manage href = %q", driveOrg.ManageHref)
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
		OrgManageHref: "/orgs/o1/settings/oauth-clients/google_drive",
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

	// Neither scope configured: the note still carries the org-editor link for an
	// org admin, which is the route the project table no longer provides (the
	// unconfigured organization row is hidden).
	neither := renderHTML(t, CredentialDetailPage(dsCredentialDetailData{
		Nav:           settingsNavCommon{IsProjectAdmin: true},
		Provider:      provider,
		OrgManageHref: "/orgs/o1/settings/oauth-clients/google_drive",
	}))
	for _, want := range []string{
		"No project or organization app is configured",
		`data-testid="ds-credential-org-link"`,
		`href="/orgs/o1/settings/oauth-clients/google_drive"`,
	} {
		if !strings.Contains(neither, want) {
			t.Errorf("neither-configured note missing %q", want)
		}
	}
	// A caller without org admin gets no org link.
	noOrgAdmin := renderHTML(t, CredentialDetailPage(dsCredentialDetailData{
		Nav:      settingsNavCommon{IsProjectAdmin: true},
		Provider: provider,
	}))
	if strings.Contains(noOrgAdmin, `data-testid="ds-credential-org-link"`) {
		t.Error("the org-editor link must not render without an org admin")
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
			Organization:  DSOAuthCredentialSource{Configured: true, ClientID: "ocid"},
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
	// The org-wide app stays manageable by the org admin, through the
	// per-provider org editor rather than an anchor on a stacked form page.
	// (Rendered links are canonicalised from /orgs/{id} to /o/{id}.)
	if !strings.Contains(body, `/o/o1/settings/oauth-clients/google_drive`) {
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

func TestDSConfigFromFieldsInteger(t *testing.T) {
	fields := []dsConfigField{{Key: "backfill_days", Type: "integer"}}
	render := func(value string) map[string]any {
		e := echo.New()
		body := url.Values{"backfill_days": {value}}.Encode()
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
		return dsConfigFromFields(e.NewContext(req, httptest.NewRecorder()), fields)
	}

	if got := render("30")["backfill_days"]; got != 30 {
		t.Errorf("numeric field = %#v, want int 30", got)
	}
	// A malformed value is forwarded verbatim so the provider's validation rejects
	// it, instead of being dropped and silently replaced by the default.
	if got := render("ninety")["backfill_days"]; got != "ninety" {
		t.Errorf("malformed field = %#v, want the raw string", got)
	}
	// Blank means "unset", so the field stays out of the config map.
	if _, ok := render("")["backfill_days"]; ok {
		t.Error("blank integer field should be omitted")
	}
}

func TestDSConfigFieldsPrefillsInteger(t *testing.T) {
	schema := &DSProviderSchema{
		Type: "trale",
		Properties: map[string]any{
			"tags":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"backfill_days": map[string]any{"type": "integer", "description": "Days back."},
		},
		Required: []string{"tags"},
	}
	// The API decodes a stored integer as float64; the edit form must not blank it,
	// or saving would reset the value to the server default.
	config, _ := dsConfigFields(schema, map[string]any{"backfill_days": float64(30)})
	for _, f := range config {
		if f.Key != "backfill_days" {
			continue
		}
		if f.Value != "30" {
			t.Errorf("backfill_days prefill = %q, want \"30\"", f.Value)
		}
		return
	}
	t.Fatal("backfill_days field missing from the config fields")
}

// TestUIDataSourceResourceList_ReconnectCopyReachesPartial: a dead Google grant
// comes back as a 422 carrying the remediation. The fragment must render that
// copy — userFacingError would flatten it to "Something went wrong." exactly when
// the user needs the action (it only passes 4xx messages through dsFormError).
//
// The fixture is a GitHub source because a Drive source never reaches this
// fragment (both /resources and /resources/list bounce it to the Google Picker);
// what is under test is the fragment's 4xx handling, and the reconnect copy is
// just the motivating message.
func TestUIDataSourceResourceList_ReconnectCopyReachesPartial(t *testing.T) {
	const reconnect = "the Google Drive connection for this source has expired or was revoked; reconnect the source to continue"
	f := &fakeMemory{
		dsIntegrations: []DSIntegration{{ID: "s1", Name: "Acme repo", ProviderType: "github", Status: "active"}},
		dsResourcesErr: &memoryHTTPError{
			Status:  http.StatusUnprocessableEntity,
			Code:    "validation_error",
			Message: reconnect,
		},
	}
	e := newDSUIServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/s1/resources/list", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "reconnect the source") {
		t.Fatalf("fragment dropped the reconnect copy:\n%s", body)
	}
	if strings.Contains(body, genericErrorMessage) {
		t.Errorf("fragment rendered the generic copy for a 4xx:\n%s", body)
	}
	if strings.Contains(body, "invalid_grant") {
		t.Errorf("fragment leaked the raw provider error:\n%s", body)
	}
}

// TestUIDataSourceResourceList_ServerErrorStaysRedacted is the other half of the
// binding: routing the fragment through dsFormError must not leak backend detail
// when the failure really is an outage.
func TestUIDataSourceResourceList_ServerErrorStaysRedacted(t *testing.T) {
	f := &fakeMemory{
		dsIntegrations: []DSIntegration{{ID: "s1", Name: "Acme repo", ProviderType: "github", Status: "active"}},
		dsResourcesErr: &memoryHTTPError{
			Status:  http.StatusInternalServerError,
			Code:    "internal_error",
			Message: "dial tcp 10.0.0.7:5432: connect: connection refused",
		},
	}
	e := newDSUIServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/s1/resources/list", nil))
	body := rec.Body.String()
	if strings.Contains(body, "10.0.0.7") || strings.Contains(body, "5432") {
		t.Errorf("fragment leaked backend coordinates:\n%s", body)
	}
	if !strings.Contains(body, memoryUnavailableMessage) {
		t.Errorf("fragment should show the redacted outage copy for a 5xx:\n%s", body)
	}
}

// A failed save on the resource page answers with ?err=… (the picked resource
// is not resolvable, the provider has no resource step, the save failed). The
// page rendered without ever reading the parameter, so the redirect looked like
// a no-op refresh and the reason was lost (#2167).
func TestUIDataSourceResourcesRouteShowsFlashError(t *testing.T) {
	f := &fakeMemory{
		dsIntegrations: []DSIntegration{{ID: "s1", Name: "Acme repo", ProviderType: "github"}},
		dsProviders:    []DSProvider{{Type: "github", Name: "GitHub", AuthMethods: []string{"oauth"}}},
	}
	e := newDSUIServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/data-sources/s1/resources?err="+url.QueryEscape("resource selection is not supported for provider github"), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "resource selection is not supported for provider github") {
		t.Errorf("flash message missing from the resource page:\n%s", body)
	}

	// The no-JavaScript fallback renders the same shell, so it must show it too.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/data-sources/s1/resources?static=1&err="+url.QueryEscape("resource selection is not supported for provider github"), nil))
	if body := rec.Body.String(); !strings.Contains(body, "resource selection is not supported for provider github") {
		t.Errorf("flash message missing from the static resource page:\n%s", body)
	}
}

// Drive answers a stale POST on the picker page. That must hold for an empty
// resource id too: /resources bounces to the picker, so answering
// "resource_id is required" there dropped the message the user needed (#2167).
func TestUIDataSourceResourceSaveDriveEmptyIDAnswersOnThePicker(t *testing.T) {
	f := &fakeMemory{dsIntegrations: []DSIntegration{{ID: "s2", ProviderType: "google_drive"}}}
	e := newDSUIServer(f)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/data-sources/s2/resources", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/data-sources/s2?picker=1&err=") {
		t.Fatalf("location = %q, want the picker with an error flash", loc)
	}
	msg, err := url.QueryUnescape(strings.TrimPrefix(loc, "/data-sources/s2?picker=1&err="))
	if err != nil {
		t.Fatalf("decode err param: %v", err)
	}
	if !strings.Contains(msg, "Drive picker") {
		t.Errorf("message = %q, want the Drive-specific remediation", msg)
	}
	if strings.Contains(msg, "resource_id is required") {
		t.Errorf("message = %q, must not be the form-validation text on a page that never shows it", msg)
	}
	if f.dsSavedResourceID != "" {
		t.Error("memory must not be called for a Drive save")
	}
}

// PickerBlocked is derived from the source's effective Drive strategy. The
// render tests inject it into a summary, so a regression that stopped setting it
// would leave the picker offered for a drive.file source with no test failing
// (#2167).
func TestDSCredentialSummaryPickerBlockedFollowsDriveFileScope(t *testing.T) {
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: &fakeMemory{}}

	cases := []struct {
		name string
		src  DSIntegration
		want bool
	}{
		{
			name: "granted drive.file",
			src:  DSIntegration{ProviderType: "google_drive", OAuthGrantedStrategy: "drive.file"},
			want: true,
		},
		{
			name: "granted drive.readonly",
			src:  DSIntegration{ProviderType: "google_drive", OAuthGrantedStrategy: "drive.readonly"},
			want: false,
		},
		{
			name: "configured drive.file without a granted scope",
			src:  DSIntegration{ProviderType: "google_drive", OAuthStrategy: "drive.file"},
			want: true,
		},
		{
			name: "granted scope wins over a configured drive.file",
			src:  DSIntegration{ProviderType: "google_drive", OAuthStrategy: "drive.file", OAuthGrantedStrategy: "drive.readonly"},
			want: false,
		},
		{
			name: "other provider never blocks",
			src:  DSIntegration{ProviderType: "github", OAuthStrategy: "drive.file"},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			summary := s.dsCredentialSummary(context.Background(), &tc.src, nil)
			if summary.PickerBlocked != tc.want {
				t.Fatalf("PickerBlocked = %v, want %v", summary.PickerBlocked, tc.want)
			}
			if tc.want && summary.PickerError == "" {
				t.Error("PickerBlocked without a PickerError leaves the user with no explanation")
			}
			if !tc.want && summary.PickerError != "" {
				t.Errorf("PickerError = %q, want empty when the picker is available", summary.PickerError)
			}
		})
	}
}

// TestRenderDataSourceDetailPageReconnectAction asserts the credentials card
// offers a reconnect for an OAuth provider — the only providers whose grant can
// be re-minted by consent — and targets that source, not the new-source flow
// (#2107).
func TestRenderDataSourceDetailPageReconnectAction(t *testing.T) {
	src := DSIntegration{ID: "s1", Name: "Team Drive", ProviderType: "google_drive", Status: "active", Config: map[string]any{}}
	base := dsCredentialSummary{
		ProviderName: "google_drive",
		SourceID:     "s1",
		Configured:   true,
		SettingsHref: "/settings/credentials/google_drive",
	}

	reconnectable := base
	reconnectable.CanReconnect = true
	html := renderHTML(t, DataSourceDetailPage(&src, "Google Drive", nil, nil, dsSavedCredentialPicker{}, nil, nil, nil, &reconnectable, "", nil))
	if !strings.Contains(html, `data-testid="ds-credential-reconnect"`) {
		t.Error("OAuth source missing the reconnect action")
	}
	if !strings.Contains(html, `href="/data-sources/s1/reconnect"`) {
		t.Errorf("reconnect action does not target the source:\n%s", html)
	}

	html = renderHTML(t, DataSourceDetailPage(&src, "Google Drive", nil, nil, dsSavedCredentialPicker{}, nil, nil, nil, &base, "", nil))
	if strings.Contains(html, `data-testid="ds-credential-reconnect"`) {
		t.Error("a provider without the OAuth method must not offer a reconnect")
	}
}

// TestUIDataSourceReconnectReusesTheSource covers the route: it reads the
// source, then asks memory to re-mint that source's grant, and lands a failure
// back on the source rather than the chooser (#2107).
func TestUIDataSourceReconnectReusesTheSource(t *testing.T) {
	f := &fakeMemory{
		dsIntegrations:       []DSIntegration{{ID: "s1", Name: "Team Drive", ProviderType: "google_drive", Status: "active"}},
		dsOAuthAuthorizeResp: &DSOAuthAuthorizeResponse{AuthorizeURL: "https://accounts.example/authorize?state=s1"},
	}
	e := newDSUIServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/s1/reconnect", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "https://accounts.example/authorize?state=s1" {
		t.Errorf("location = %q, want the authorize URL", loc)
	}
	if f.dsLastOAuthProvider != "google_drive" {
		t.Errorf("provider = %q, want google_drive", f.dsLastOAuthProvider)
	}
	if f.dsLastOAuthReconnectSourceID != "s1" {
		t.Errorf("reconnect source = %q, want s1 (a reconnect must target the source)", f.dsLastOAuthReconnectSourceID)
	}
	if f.dsLastOAuthConfig != nil {
		t.Errorf("reconnect sent config %+v, want none (a reconnect does not re-enter settings)", f.dsLastOAuthConfig)
	}

	// A backend failure goes back to that source with an error flash.
	f.dsOAuthAuthorizeErr = errTest
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/s1/reconnect", nil))
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/data-sources/s1?err=") {
		t.Errorf("failure location = %q, want the source detail with an error", loc)
	}
}

// TestUIDataSourceDetailReconnectActionFollowsTheCatalog asserts the detail page
// offers the reconnect action exactly when the provider advertises OAuth — the
// decision comes from the catalog, not a hard-coded provider list (#2107).
func TestUIDataSourceDetailReconnectActionFollowsTheCatalog(t *testing.T) {
	f := &fakeMemory{
		dsIntegrations: []DSIntegration{{ID: "s1", Name: "Team Drive", ProviderType: "google_drive", Status: "active"}},
		dsProviders:    []DSProvider{{Type: "google_drive", Name: "Google Drive", AuthMethods: []string{"oauth"}}},
	}
	e := newDSUIServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/s1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `data-testid="ds-credential-reconnect"`) {
		t.Error("an OAuth source detail page must offer a reconnect")
	}

	// The same source, with a catalog that no longer advertises OAuth: nothing
	// can re-mint the grant, so the action must disappear rather than offer a
	// flow that would fail.
	f.dsProviders = []DSProvider{{Type: "google_drive", Name: "Google Drive", AuthMethods: []string{"credentials"}}}
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/s1", nil))
	if strings.Contains(rec.Body.String(), `data-testid="ds-credential-reconnect"`) {
		t.Error("a provider without the OAuth method must not offer a reconnect")
	}
}

// TestUIDataSourceDetailRenewedFlash asserts the landing after a reconnect says
// what happened instead of claiming a new source was connected (#2186).
func TestUIDataSourceDetailRenewedFlash(t *testing.T) {
	f := &fakeMemory{
		dsIntegrations: []DSIntegration{{ID: "s1", Name: "Team Drive", ProviderType: "google_drive", Status: "active"}},
		dsProviders:    []DSProvider{{Type: "google_drive", Name: "Google Drive", AuthMethods: []string{"oauth"}}},
	}
	e := newDSUIServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/s1?reconnected=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Connection renewed.") {
		t.Error("detail page missing the renewed flash")
	}
	if strings.Contains(body, "Data source connected.") {
		t.Error("a reconnect must not be reported as a new connection")
	}
}

// TestUIDataSourceReconnectMissingClientLandsOnTheSource asserts a reconnect that
// cannot start (no OAuth client) reports the failure on the source it was aimed
// at, not by dropping the user on the new-source page (#2186).
func TestUIDataSourceReconnectMissingClientLandsOnTheSource(t *testing.T) {
	f := &fakeMemory{
		dsIntegrations: []DSIntegration{{ID: "s1", Name: "Team Drive", ProviderType: "github", Status: "active"}},
		dsOAuthAuthorizeErr: &memoryHTTPError{
			Status:  http.StatusUnprocessableEntity,
			Code:    "validation_error",
			Message: "no OAuth client configured for provider github (configure one under Organization settings → Data source OAuth clients)",
		},
	}
	e := newDSUIServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/s1/reconnect", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/data-sources/s1?err=") {
		t.Fatalf("location = %q, want the source detail with an error", loc)
	}
	if !strings.Contains(loc, "OAuth+client") {
		t.Errorf("location = %q, want the actionable OAuth client message", loc)
	}

	// A 5xx is an upstream fault, not a user mistake: its body is redacted like
	// every other data-source form error instead of being echoed back.
	f.dsOAuthAuthorizeErr = &memoryHTTPError{
		Status:  http.StatusBadGateway,
		Code:    "internal",
		Message: "dial tcp 172.17.0.1:8080: upstream detail",
	}
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/s1/reconnect", nil))
	loc = rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/data-sources/s1?err=") {
		t.Fatalf("5xx location = %q, want the source detail", loc)
	}
	if strings.Contains(loc, "172.17.0.1") || strings.Contains(loc, "upstream+detail") {
		t.Errorf("5xx location leaks upstream detail: %q", loc)
	}
}

// TestDSArrayFieldPlaceholderMentionsTheFieldKind pins the fix for the shared
// array-field hint. Every array field used to advertise "One MIME type per
// line", which is only true for the drive provider's mime_types — the web
// provider's start_urls and its include/exclude globs showed the same text.
func TestDSArrayFieldPlaceholderMentionsTheFieldKind(t *testing.T) {
	cases := map[string]string{
		"mime_types":    "MIME type",
		"start_urls":    "URL",
		"include_globs": "glob",
		"exclude_globs": "glob",
		"paths":         "path",
	}
	for key, want := range cases {
		if got := dsArrayFieldPlaceholder(key); !strings.Contains(got, want) {
			t.Errorf("dsArrayFieldPlaceholder(%q) = %q, want it to mention %q", key, got, want)
		}
	}
	if got := dsArrayFieldPlaceholder("some_future_array"); got != "One value per line" {
		t.Errorf("dsArrayFieldPlaceholder(unknown) = %q, want the neutral fallback", got)
	}
	// The regression this fixes: a non-MIME array field must never borrow the
	// MIME hint.
	for _, key := range []string{"start_urls", "include_globs", "paths", "some_future_array"} {
		if got := dsArrayFieldPlaceholder(key); strings.Contains(got, "MIME") {
			t.Errorf("dsArrayFieldPlaceholder(%q) = %q, want no MIME hint", key, got)
		}
	}
}

// TestDSCredentialSummarySelfRegistering asserts a self-registering provider
// (Trale) is never reported as "no OAuth app": the client is registered at
// connect, so the card names the automatic client, labels the account after the
// provider's vendor, reports the granted scopes, and offers no settings link
// (there is no editor to open — the app editor filters this provider out).
func TestDSCredentialSummarySelfRegistering(t *testing.T) {
	trale := DSProvider{Type: "trale", Name: "Trale", VendorName: "Trale", AuthMethods: []string{"oauth"}, SelfRegistering: true}
	src := &DSIntegration{
		ID:           "t1",
		ProviderType: "trale",
		Status:       "active",
		Config: map[string]any{
			dsOAuthGrantedScopesKey: []any{"openid", "email", "profile"},
		},
	}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: &fakeMemory{}}
	summary := s.dsCredentialSummary(context.Background(), src, &trale)
	// The handler sets this from the catalog; the card offers the repair action
	// for every provider that advertises OAuth.
	summary.CanReconnect = true

	if !summary.SelfRegistering {
		t.Error("SelfRegistering = false, want true for a self-registering provider")
	}
	if summary.ScopeLabel != "Registered at connect" {
		t.Errorf("ScopeLabel = %q, want %q", summary.ScopeLabel, "Registered at connect")
	}
	if !summary.Configured {
		t.Error("Configured = false, want true: there is nothing for the user to set up")
	}
	if summary.SettingsHref != "" {
		t.Errorf("SettingsHref = %q, want empty (the provider has no app editor)", summary.SettingsHref)
	}
	if summary.AccountLabel != "Trale account" {
		t.Errorf("AccountLabel = %q, want %q", summary.AccountLabel, "Trale account")
	}
	if summary.GrantedLabel != "openid email profile" || summary.GrantedTone != "success" {
		t.Errorf("granted = %q/%q, want the recorded scopes on a success tone", summary.GrantedLabel, summary.GrantedTone)
	}

	html := renderHTML(t, DataSourceDetailPage(src, "Trale", nil, nil, dsSavedCredentialPicker{}, nil, nil, nil, summary, "", nil))
	for _, want := range []string{
		"Registered at connect",
		"Public client registered automatically",
		"Trale account",
		"openid email profile",
		`data-testid="ds-credential-reconnect"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("detail page missing %q", want)
		}
	}
	for _, unwanted := range []string{
		"ds-credential-missing",
		"Set one up in credential settings",
		"Open credential settings",
		"Google account",
	} {
		if strings.Contains(html, unwanted) {
			t.Errorf("detail page contains %q, which is wrong for a self-registering provider", unwanted)
		}
	}
}

// TestDSCredentialSummaryGrantedScope covers the granted-scope badge. Gmail has
// no scope strategy (its connect always authorizes gmail.readonly), so a healthy
// source used to render the amber "not reported" warning (#2218).
func TestDSCredentialSummaryGrantedScope(t *testing.T) {
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: &fakeMemory{}}

	cases := []struct {
		name      string
		src       DSIntegration
		wantLabel string
		wantTone  string
		wantKey   string
	}{
		{
			name:      "gmail reports its fixed scope",
			src:       DSIntegration{ProviderType: "gmail"},
			wantLabel: "Read-only Gmail access (gmail.readonly)",
			wantTone:  "success",
			wantKey:   "gmail.readonly",
		},
		{
			name:      "gmail keeps its fixed scope when a strategy was recorded",
			src:       DSIntegration{ProviderType: "gmail", OAuthStrategy: "gmail.readonly"},
			wantLabel: "Read-only Gmail access (gmail.readonly)",
			wantTone:  "success",
			wantKey:   "gmail.readonly",
		},
		{
			name:      "drive.readonly stays a success",
			src:       DSIntegration{ProviderType: "google_drive", OAuthGrantedStrategy: "drive.readonly"},
			wantLabel: "Read-only Drive access (drive.readonly)",
			wantTone:  "success",
			wantKey:   "drive.readonly",
		},
		{
			name:      "an unmapped provider with no recorded grant stays neutral",
			src:       DSIntegration{ProviderType: "github"},
			wantLabel: "Not recorded at connect",
			wantTone:  "neutral",
		},
		{
			name: "a static-app provider reports the scopes it recorded",
			src: DSIntegration{ProviderType: "github", Config: map[string]any{
				dsOAuthGrantedScopesKey: []any{"repo", "read:org"},
			}},
			wantLabel: "repo read:org",
			wantTone:  "success",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			summary := s.dsCredentialSummary(context.Background(), &tc.src, nil)
			if summary.GrantedLabel != tc.wantLabel {
				t.Errorf("GrantedLabel = %q, want %q", summary.GrantedLabel, tc.wantLabel)
			}
			if summary.GrantedTone != tc.wantTone {
				t.Errorf("GrantedTone = %q, want %q", summary.GrantedTone, tc.wantTone)
			}
			if summary.GrantedStrategy != tc.wantKey {
				t.Errorf("GrantedStrategy = %q, want %q", summary.GrantedStrategy, tc.wantKey)
			}
		})
	}
}

// TestDSCredentialSummarySelfRegisteringWithoutScopes asserts an older source
// with no recorded grant is reported neutrally — not as a provider that failed
// to report anything, and never with a warning tone.
func TestDSCredentialSummarySelfRegisteringWithoutScopes(t *testing.T) {
	trale := DSProvider{Type: "trale", Name: "Trale", VendorName: "Trale", AuthMethods: []string{"oauth"}, SelfRegistering: true}
	src := &DSIntegration{ID: "t1", ProviderType: "trale", Config: map[string]any{}}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: &fakeMemory{}}

	summary := s.dsCredentialSummary(context.Background(), src, &trale)
	if summary.GrantedLabel != "Not recorded at connect" {
		t.Errorf("GrantedLabel = %q, want %q", summary.GrantedLabel, "Not recorded at connect")
	}
	if summary.GrantedTone != "neutral" {
		t.Errorf("GrantedTone = %q, want neutral", summary.GrantedTone)
	}
	if strings.Contains(renderHTML(t, DataSourceDetailPage(src, "Trale", nil, nil, dsSavedCredentialPicker{}, nil, nil, nil, summary, "", nil)), "ds-credential-missing") {
		t.Error("a source without grant metadata must not advertise setting an app up")
	}
}

// TestDSConfigFieldPickerSavedSource asserts a saved source's suggestion listing
// goes through its stored credentials instead of the unsaved-config endpoints,
// that the ref widget keeps the form-scoped endpoint, and that the load copy
// follows the schema before any widget default.
func TestDSConfigFieldPickerSavedSource(t *testing.T) {
	tags := dsConfigField{Key: "tags", Type: "array", Widget: "resource", SourceID: "t1", LoadLabel: "Load tags"}
	if got := tags.dsLoadPath(); got != "/data-sources/t1/field-options" {
		t.Errorf("saved resource load path = %q, want the field-options route", got)
	}
	if got := tags.dsLoadLabel(); got != "Load tags" {
		t.Errorf("load label = %q, want the schema's copy", got)
	}
	if got := tags.dsLoadTestID(); got != "ds-load-tags" {
		t.Errorf("load testid = %q, want ds-load-tags", got)
	}

	// The connect form has no source yet: credentials are still unsaved.
	unsaved := dsConfigField{Key: "tags", Type: "array", Widget: "resource"}
	if got := unsaved.dsLoadPath(); got != "/data-sources/repos" {
		t.Errorf("unsaved resource load path = %q, want /data-sources/repos", got)
	}
	if got := unsaved.dsLoadLabel(); got != "Load suggestions" {
		t.Errorf("unsaved load label = %q, want the generic copy", got)
	}

	// A ref depends on the repository typed into the form, so it stays on the
	// unsaved-config endpoint even for a saved source.
	ref := dsConfigField{Key: "ref", Type: "string", Widget: "ref", SourceID: "s1"}
	if got := ref.dsLoadPath(); got != "/data-sources/refs" {
		t.Errorf("ref load path = %q, want /data-sources/refs", got)
	}
	if got := ref.dsLoadLabel(); got != "Load branches and tags" {
		t.Errorf("ref load label = %q, want the ref copy", got)
	}

	// GitHub's repo field keeps its historical copy and test id.
	repo := dsConfigField{Key: "repo", Type: "string", Widget: "resource", SourceID: "s1"}
	if got := repo.dsLoadLabel(); got != "Load repositories" {
		t.Errorf("repo load label = %q, want the repo copy", got)
	}
	if got := repo.dsLoadTestID(); got != "ds-load-repos" {
		t.Errorf("repo load testid = %q, want ds-load-repos", got)
	}
}

// TestDSConfigFieldArrayPickerRendersTextarea asserts a multi-value picker field
// is a textarea that a clicked suggestion can append to, not a single-line input
// whose datalist selection would replace the values already chosen.
func TestDSConfigFieldArrayPickerRendersTextarea(t *testing.T) {
	html := renderHTML(t, dsConfigFieldInputs([]dsConfigField{{
		Key:       "tags",
		Label:     "Tags",
		Type:      "array",
		Widget:    "resource",
		Required:  true,
		LoadLabel: "Load tags",
		SourceID:  "t1",
		Value:     "standup",
	}}))
	for _, want := range []string{
		`<textarea id="tags" name="tags"`,
		`data-ds-picker-input="true"`,
		`data-ds-picker-scope="true"`,
		`data-testid="ds-load-tags"`,
		"Load tags",
		`hx-post="/data-sources/t1/field-options"`,
		"standup",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("array picker missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "<datalist") {
		t.Error("array picker must not render a datalist: a click would replace every chosen value")
	}
}

// TestUIDataSourceFieldOptionsRoute asserts the saved-source listing route reads
// through ListDSResources (stored credentials) and renders clickable chips.
func TestUIDataSourceFieldOptionsRoute(t *testing.T) {
	f := &fakeMemory{dsResources: []DSResource{{ID: "standup", Name: "Standup", Type: "tag"}}}
	e := newDSUIServer(f)
	rec := httptest.NewRecorder()
	body := url.Values{"fieldKey": {"tags"}}.Encode()
	req := httptest.NewRequest(http.MethodPost, "/data-sources/t1/field-options", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.dsResourcesCallCount != 1 {
		t.Errorf("ListDSResources calls = %d, want exactly one", f.dsResourcesCallCount)
	}
	for _, want := range []string{`data-ds-suggest="standup"`, `data-testid="ds-suggestion"`, `data-testid="ds-options-chips"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("field-options response missing %q:\n%s", want, rec.Body.String())
		}
	}
}

// TestUIDataSourceFieldOptionsRouteFailureIsInline asserts a failed listing is
// swapped in as an inline message that leaves the field editable: the partial
// answers 200 so htmx replaces the region instead of dropping the response.
func TestUIDataSourceFieldOptionsRouteFailureIsInline(t *testing.T) {
	f := &fakeMemory{dsResourcesErr: &memoryHTTPError{Status: http.StatusBadGateway, Message: "trale is unreachable"}}
	e := newDSUIServer(f)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/data-sources/t1/field-options", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 so the partial swaps inline; body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="ds-options-error"`) {
		t.Errorf("failure response missing the inline error region:\n%s", body)
	}
	if !strings.Contains(body, "temporarily unavailable") {
		t.Errorf("failure response hides the reason:\n%s", body)
	}
	if strings.Contains(body, "ds-suggestion") {
		t.Errorf("failure response renders suggestions:\n%s", body)
	}
}

// TestDSSyncOptionsNote asserts the settings card explains only the concerns the
// provider actually has: the folder sentence is Drive-specific and the
// credential sentence needs write-only credential fields.
func TestDSSyncOptionsNote(t *testing.T) {
	folders := []dsConfigField{{Key: "folders", Widget: "folders"}}
	creds := []dsConfigField{{Key: "refresh_token", Type: "string"}}
	tags := []dsConfigField{{Key: "tags", Type: "array", Widget: "resource"}}

	if got := dsSyncOptionsNote(folders, creds); !strings.Contains(got, "every selected folder") || !strings.Contains(got, "write-only") {
		t.Errorf("drive note = %q, want both sentences", got)
	}
	if got := dsSyncOptionsNote(tags, nil); got != "" {
		t.Errorf("self-registering note = %q, want empty", got)
	}
	if got := dsSyncOptionsNote(nil, creds); got != "Credential fields are write-only and never shown." {
		t.Errorf("credential-only note = %q", got)
	}
}

// TestDSDetailPageRendersTheProviderSpecificSyncNote is the render-level guard
// for the settings copy. dsEditCard is handed the folder-filtered fields
// (folders are edited by the picker modal, not a text field), so a note computed
// from what the card receives would never see a folder widget and the Drive
// sentence would silently disappear.
func TestDSDetailPageRendersTheProviderSpecificSyncNote(t *testing.T) {
	drive := DSIntegration{ID: "s1", Name: "Team Drive", ProviderType: "google_drive", Status: "active"}
	config, creds := dsConfigFields(testDSSchema(), nil)
	driveHTML := renderHTML(t, DataSourceDetailPage(&drive, "Google Drive", config, creds, dsSavedCredentialPicker{}, nil, nil, nil, nil, "", nil))
	if !strings.Contains(driveHTML, "every selected folder") {
		t.Error("Drive detail page lost the folder note")
	}
	if !strings.Contains(driveHTML, "write-only") {
		t.Error("Drive detail page lost the credential note")
	}

	// A provider with neither a folder widget nor credential fields (Trale, the
	// web crawler) must get no note at all.
	bare := DSIntegration{ID: "t1", Name: "Standups", ProviderType: "trale", Status: "active"}
	bareFields, bareCreds := dsConfigFields(webSchema(), nil)
	if note := dsSyncOptionsNote(bareFields, bareCreds); note != "" {
		t.Fatalf("note = %q, want empty (no folders, no credential fields)", note)
	}
	summary := &dsCredentialSummary{ProviderName: "trale", SourceID: "t1", AccountLabel: "Trale account", SelfRegistering: true, Configured: true, CanReconnect: true, GrantedTone: "neutral", GrantedLabel: "Not recorded at connect"}
	bareHTML := renderHTML(t, DataSourceDetailPage(&bare, "Trale", bareFields, bareCreds, dsSavedCredentialPicker{}, nil, nil, nil, summary, "", nil))
	for _, unwanted := range []string{"every selected folder", "write-only"} {
		if strings.Contains(bareHTML, unwanted) {
			t.Errorf("detail page of a folder-less provider contains Drive copy %q", unwanted)
		}
	}
}

// TestDSDetailPageSyncOptionsOwnsTheSyncControls pins the #426 reorder: the
// sync mode and interval controls belong to the "Sync options" section, not to
// the card header above it.
func TestDSDetailPageSyncOptionsOwnsTheSyncControls(t *testing.T) {
	src := &DSIntegration{ID: "s1", Name: "Team Drive", ProviderType: "google_drive", Status: "active"}
	config, creds := dsConfigFields(testDSSchema(), nil)
	html := renderHTML(t, DataSourceDetailPage(src, "Google Drive", config, creds, dsSavedCredentialPicker{}, nil, nil, nil, nil, "", nil))

	const heading = `<h3 class="text-sm font-semibold">Sync options</h3>`
	start := strings.Index(html, heading)
	if start < 0 {
		t.Fatalf("detail page has no Sync options section:\n%s", html)
	}
	// The section runs to the next sibling heading (or the end of the form, for
	// a provider with no credential block).
	end := len(html)
	if next := strings.Index(html[start+len(heading):], `<h3 class="text-sm font-semibold">`); next >= 0 {
		end = start + len(heading) + next
	}
	section := html[start:end]

	for _, want := range []string{`data-testid="ds-field-sync-mode"`, `data-testid="ds-field-sync-interval"`, "Sync mode", "Sync interval (minutes)"} {
		if !strings.Contains(section, want) {
			t.Errorf("Sync options section missing %q:\n%s", want, section)
		}
	}
	// The name field stays in the first block, above the section.
	if nameAt := strings.Index(html, `data-testid="ds-field-name"`); nameAt < 0 || nameAt > start {
		t.Errorf("name field should render above the Sync options section")
	}
	// ...and neither sync control may leak into that first block.
	if header := html[:start]; strings.Contains(header, "ds-field-sync-mode") || strings.Contains(header, "ds-field-sync-interval") {
		t.Errorf("sync controls still render above the Sync options section:\n%s", header)
	}
}

// TestDSDetailPageOmitsTheMissingAppWarningWhenTheCatalogIsUnknown asserts a
// failed catalog read degrades quietly: with no provider entry the card cannot
// tell whether an app is needed, and claiming "set one up" was the #2213
// symptom for a provider that registers its own client.
func TestDSDetailPageOmitsTheMissingAppWarningWhenTheCatalogIsUnknown(t *testing.T) {
	src := &DSIntegration{ID: "t1", Name: "Standups", ProviderType: "trale", Status: "active"}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: &fakeMemory{}}
	summary := s.dsCredentialSummary(context.Background(), src, nil)
	if !summary.ProviderUnknown {
		t.Error("ProviderUnknown = false for a nil catalog entry")
	}
	html := renderHTML(t, DataSourceDetailPage(src, "Trale", nil, nil, dsSavedCredentialPicker{}, nil, nil, nil, summary, "", nil))
	if strings.Contains(html, "ds-credential-missing") {
		t.Error("unknown provider still renders the \"set one up\" warning")
	}
}

// TestDSCredentialSummarySelfRegisteringSkipsCredentialsLookup asserts the card
// for a self-registering provider does not depend on the credentials API: a
// lookup failure there is not this source's fault, and reporting it would point
// the user at an app that does not exist.
func TestDSCredentialSummarySelfRegisteringSkipsCredentialsLookup(t *testing.T) {
	trale := DSProvider{Type: "trale", Name: "Trale", VendorName: "Trale", AuthMethods: []string{"oauth"}, SelfRegistering: true}
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: &fakeMemory{dsErr: &memoryHTTPError{Status: http.StatusBadGateway, Message: "boom"}}}

	summary := s.dsCredentialSummary(context.Background(), &DSIntegration{ID: "t1", ProviderType: "trale"}, &trale)
	if summary.LookupFailed {
		t.Error("LookupFailed = true for a self-registering provider, want false (it has no app to look up)")
	}
	if summary.ScopeLabel != "Registered at connect" {
		t.Errorf("ScopeLabel = %q, want the registered-client label", summary.ScopeLabel)
	}

	// A provider that does depend on a resolved app still reports the failure.
	drive := DSProvider{Type: "google_drive", Name: "Google Drive", VendorName: "Google", AuthMethods: []string{"oauth"}}
	driveSummary := s.dsCredentialSummary(context.Background(), &DSIntegration{ID: "d1", ProviderType: "google_drive"}, &drive)
	if !driveSummary.LookupFailed {
		t.Error("LookupFailed = false for a static-app provider, want true")
	}
}

// TestRenderConfigBooleanDescriptionAsInfoTip pins #2103 and #2393: a boolean
// field's schema description rides on the label as an info tooltip instead of
// a paragraph under the switch, and the switch references a real (visually
// hidden) copy with aria-describedby so the text is in the accessibility tree.
func TestRenderConfigBooleanDescriptionAsInfoTip(t *testing.T) {
	const description = "Recurse into subfolders when true. Applies to every selected folder."
	html := renderHTML(t, dsConfigFieldInput(dsConfigField{
		Key:         "recursive",
		Label:       "Recursive",
		Type:        "boolean",
		Description: description,
		Checked:     true,
	}))
	doc := parseDOM(t, html)

	// The visual tooltip survives for sighted pointer users.
	tip := findElementByAttr(doc, "data-tip", description)
	if tip == nil {
		t.Fatal("the switch's description should still render as a tooltip")
	}
	if !hasClasses(tip, "tooltip") {
		t.Error("the description should ride on the tooltip element")
	}
	if !strings.Contains(html, "lucide--info") {
		t.Error("the tooltip should carry the info icon")
	}

	// The switch must reference a real description, not only data-tip.
	input := findElementByAttr(doc, "name", "recursive")
	if input == nil {
		t.Fatal("switch input not found")
	}
	if got := attrValue(input, "aria-describedby"); got != "ds-tip-recursive" {
		t.Errorf("switch aria-describedby = %q, want ds-tip-recursive", got)
	}
	desc := findElementByAttr(doc, "id", "ds-tip-recursive")
	if desc == nil {
		t.Fatal("no element with the id the switch references")
	}
	if !hasClasses(desc, "sr-only") {
		t.Error("the referenced description must be visually hidden (sr-only)")
	}
	if got := elementText(desc); got != description {
		t.Errorf("referenced description = %q, want %q", got, description)
	}
	if got := attrValue(desc, "aria-hidden"); got != "" {
		t.Errorf("the referenced description must stay in the accessibility tree, got aria-hidden=%q", got)
	}

	if strings.Contains(html, "<p") {
		t.Error("the description must not render as a paragraph under the switch")
	}
	// The tooltip sits outside the <label>: a click on it must not toggle the
	// switch, and its hidden copy must not join the switch's accessible name.
	labelEnd := strings.Index(html, "</label>")
	tipAt := strings.Index(html, `data-tip="`)
	if labelEnd == -1 || tipAt == -1 || tipAt < labelEnd {
		t.Error("the info tip must follow the label, outside it")
	}
}

// TestRenderConfigBooleanSwitchKeepsItsWiring pins the wiring a boolean config
// field's switch must keep now that it renders through form.ToggleInput. That
// component deliberately forces no id, so the field id travels through Attrs:
// a regression would silently drop the id while the label kept working.
func TestRenderConfigBooleanSwitchKeepsItsWiring(t *testing.T) {
	html := renderHTML(t, dsConfigFieldInput(dsConfigField{
		Key:     "recursive",
		Label:   "Recursive",
		Type:    "boolean",
		Checked: true,
	}))
	doc := parseDOM(t, html)

	input := findElementByAttr(doc, "name", "recursive")
	if input == nil {
		t.Fatal("switch input not found")
	}
	if got := attrValue(input, "type"); got != "checkbox" {
		t.Errorf("switch type = %q, want checkbox", got)
	}
	if got := attrValue(input, "id"); got != "recursive" {
		t.Errorf("switch id = %q, want recursive (ToggleInput forces no id; it must come from Attrs)", got)
	}
	if got := attrValue(input, "data-testid"); got != "ds-field-recursive" {
		t.Errorf("switch data-testid = %q, want ds-field-recursive", got)
	}
	if !hasClasses(input, "toggle") {
		t.Error("switch should carry the canonical toggle class")
	}
	checked := false
	for _, a := range input.Attr {
		if a.Key == "checked" {
			checked = true
		}
	}
	if !checked {
		t.Error("switch should render checked for a true field value")
	}
}

// elementText returns the concatenated text nodes under n.
func elementText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			b.WriteString(node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return strings.TrimSpace(b.String())
}
