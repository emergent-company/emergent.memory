package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Saved, project-scoped credentials: the chooser copy, the connect-form picker,
// the credentials-page table and shared modal, and the PRG handlers.

func savedCredentialProviders() []DSProvider {
	return []DSProvider{
		{Type: "google_drive", Name: "Google Drive", VendorName: "Google", AuthMethods: []string{"oauth"}},
		{Type: "github", Name: "GitHub", VendorName: "GitHub", AuthMethods: []string{"oauth", "credentials"}},
	}
}

func savedCredentialItems() []DSSavedCredential {
	return []DSSavedCredential{
		{ID: "cred-1", ProviderType: "github", Name: "Work token", UsedByCount: 2, UpdatedAt: "2026-09-01T00:00:00Z"},
		{ID: "cred-2", ProviderType: "github", Name: "Unused token", UsedByCount: 0, UpdatedAt: "2026-09-02T00:00:00Z"},
	}
}

func githubCredentialPicker(selected string) dsSavedCredentialPicker {
	_, credentialFields := dsConfigFields(githubSchema(), nil)
	return dsSavedCredentialPicker{
		Supported:       true,
		ShowModal:       true,
		Items:           savedCredentialItems(),
		SelectedID:      selected,
		ReturnURL:       dsConnectFormPath("github"),
		FixedProvider:   "github",
		DefaultProvider: "github",
		Providers:       dsCredentialProviders(savedCredentialProviders()),
		Fields:          dsRequiredCredentialFields(credentialFields),
	}
}

func TestRenderProviderChooserSeparatesConnectionMethods(t *testing.T) {
	github := DSProvider{Type: "github", Name: "GitHub", AuthMethods: []string{"oauth", "credentials"}}
	html := renderHTML(t, DataSourceNewPage([]DSProvider{github}, nil, nil, nil, dsSavedCredentialPicker{}, nil, nil, nil))

	for _, want := range []string{
		"Connection method",
		"OAuth connection",
		"Personal access token",
		"Use a personal access token",
		"Connect with GitHub",
		`data-testid="ds-method-oauth"`,
		`data-testid="ds-method-credentials"`,
		`data-testid="ds-oauth-connect"`,
		`data-testid="ds-credentials-connect"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("chooser missing %q", want)
		}
	}
	// The two paths read as separate, labelled rows in this order.
	oauth := strings.Index(html, "OAuth connection")
	pat := strings.Index(html, "Personal access token")
	if oauth < 0 || pat < 0 || oauth > pat {
		t.Errorf("expected the OAuth row before the personal-access-token row (oauth=%d pat=%d)", oauth, pat)
	}
	if strings.Contains(html, "Enter credentials") {
		t.Error("chooser still carries the old 'Enter credentials' copy")
	}
}

func TestRenderConnectPageSavedCredentialPicker(t *testing.T) {
	p := DSProvider{Type: "github", Name: "GitHub", AuthMethods: []string{"credentials"}}
	configFields, credentialFields := dsConfigFields(githubSchema(), nil)
	picker := githubCredentialPicker("cred-1")
	html := renderHTML(t, DataSourceNewPage([]DSProvider{p}, &p, configFields, credentialFields, picker, nil, nil, nil))

	for _, want := range []string{
		`name="credential_id"`,
		`data-testid="ds-field-credential"`,
		`data-testid="ds-credential-create"`,
		`value="cred-1" selected`,
		"Work token — 2 sources",
		"New credential",
		`id="saved-credential-modal"`,
		`data-testid="ds-saved-credential-provider-fixed"`,
		`name="access_token"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("connect page missing %q", want)
		}
	}
	// The raw token input is gone from the connect flow.
	if strings.Contains(html, `data-testid="ds-field-access_token"`) {
		t.Error("connect form must not render the raw access_token input")
	}
	// A selected credential keeps Connect enabled.
	if strings.Contains(html, `data-testid="ds-connect-submit" disabled`) {
		t.Error("Connect must be enabled when a credential is selected")
	}
}

func TestRenderConnectPageEmptyCredentialList(t *testing.T) {
	p := DSProvider{Type: "github", Name: "GitHub", AuthMethods: []string{"credentials"}}
	configFields, credentialFields := dsConfigFields(githubSchema(), nil)
	picker := githubCredentialPicker("")
	picker.Items = nil
	html := renderHTML(t, DataSourceNewPage([]DSProvider{p}, &p, configFields, credentialFields, picker, nil, nil, nil))

	if !strings.Contains(html, "No saved credentials for GitHub yet.") {
		t.Error("empty state copy missing")
	}
	if !strings.Contains(html, `data-testid="ds-credential-empty"`) {
		t.Error("empty state wrapper missing")
	}
	if !strings.Contains(html, `data-testid="ds-credential-create"`) {
		t.Error("empty state must offer New credential")
	}
	if !strings.Contains(html, `data-testid="ds-connect-submit" disabled`) {
		t.Error("Connect should be disabled while no credential is selected")
	}
}

func TestRenderCredentialsPageSavedCredentials(t *testing.T) {
	_, credentialFields := dsConfigFields(githubSchema(), nil)
	data := dsCredentialsPageData{
		Nav:         settingsNavCommon{IsProjectAdmin: true},
		ProjectName: "Home",
		Rows:        []dsCredentialRow{{ProviderType: "github", App: "GitHub OAuth app", DataSource: "GitHub", ScopeLabel: "Project"}},
		SavedCredentials: []dsSavedCredentialRow{
			{DSSavedCredential: DSSavedCredential{ID: "cred-1", ProviderType: "github", Name: "Work token", UsedByCount: 2, UpdatedAt: "2026-09-01T00:00:00Z"}, ProviderName: "GitHub"},
		},
		SavedCredentialPicker: dsSavedCredentialPicker{
			ShowModal:       true,
			Providers:       dsCredentialProviders(savedCredentialProviders()),
			DefaultProvider: "github",
			ReturnURL:       "/settings/credentials",
			Fields:          dsRequiredCredentialFields(credentialFields),
		},
	}
	html := renderHTML(t, CredentialsPage(data))

	for _, want := range []string{
		`data-testid="ds-saved-credentials"`,
		`data-testid="ds-saved-credentials-table"`,
		`data-testid="ds-saved-credential-row"`,
		"Work token",
		"2 sources",
		`data-testid="ds-saved-credential-add"`,
		`data-testid="ds-saved-credential-edit"`,
		`data-testid="ds-saved-credential-delete"`,
		`data-testid="ds-saved-credential-provider"`,
		`data-testid="ds-saved-credential-name"`,
		`data-testid="ds-saved-credential-submit"`,
		`action="/settings/credentials/saved"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("credentials page missing %q", want)
		}
	}
	// The saved credentials section sits above the OAuth apps table.
	if saved, oauth := strings.Index(html, `data-testid="ds-saved-credentials-table"`), strings.Index(html, `data-testid="ds-credentials-table"`); saved < 0 || oauth < 0 || saved > oauth {
		t.Errorf("saved credentials should render above the OAuth table (saved=%d oauth=%d)", saved, oauth)
	}
}

func TestRenderCredentialsPageSavedCredentialEmpty(t *testing.T) {
	html := renderHTML(t, CredentialsPage(dsCredentialsPageData{Nav: settingsNavCommon{IsProjectAdmin: true}}))
	if !strings.Contains(html, "No saved credentials yet") {
		t.Error("empty saved-credentials state missing")
	}
	if !strings.Contains(html, `data-testid="ds-saved-credential-add"`) {
		t.Error("empty state must still offer Add credential")
	}
}

func TestUISavedCredentialFieldsPartial(t *testing.T) {
	f := &fakeMemory{dsSchema: githubSchema()}
	e := newDSUIServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/credentials/saved/fields?providerType=github", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("fields status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`name="providerType" value="github"`,
		`name="access_token"`,
		`type="password"`,
		`data-testid="ds-saved-credential-field-access_token"`,
		"Personal access token",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("fields partial missing %q", want)
		}
	}
	// Only required credential fields are asked for on create.
	if strings.Contains(body, `name="ref"`) {
		t.Error("fields partial must not render config fields")
	}
}

func TestUISavedCredentialCreate(t *testing.T) {
	f := &fakeMemory{dsSchema: githubSchema()}
	e := newDSUIServer(f)

	form := url.Values{
		"providerType": {"github"},
		"name":         {"Work token"},
		"access_token": {"ghp_saved"},
		"return_to":    {"/data-sources/new?providerType=github&method=credentials"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/settings/credentials/saved", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create status = %d, body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/data-sources/new?providerType=github&method=credentials&savedCredential=cred-new") {
		t.Errorf("create location = %q", loc)
	}
	if f.dsCreatedSavedReq == nil {
		t.Fatal("CreateSavedDSCredential was not called")
	}
	if f.dsCreatedSavedReq.ProviderType != "github" || f.dsCreatedSavedReq.Name != "Work token" {
		t.Errorf("create request = %+v", f.dsCreatedSavedReq)
	}
	if f.dsCreatedSavedReq.Credentials["access_token"] != "ghp_saved" {
		t.Errorf("credentials = %v, want the submitted token", f.dsCreatedSavedReq.Credentials)
	}
}

func TestUISavedCredentialCreateMissingFields(t *testing.T) {
	f := &fakeMemory{dsSchema: githubSchema()}
	e := newDSUIServer(f)

	form := url.Values{"providerType": {"github"}, "name": {"No secret"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/settings/credentials/saved", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "err=") {
		t.Errorf("missing credential fields should redirect with an error, got %q", loc)
	}
	if f.dsCreatedSavedReq != nil {
		t.Error("an empty credential must not be created")
	}
}

func TestUISavedCredentialCreateRejectsOffsiteReturnPath(t *testing.T) {
	f := &fakeMemory{dsSchema: githubSchema()}
	e := newDSUIServer(f)

	form := url.Values{
		"providerType": {"github"},
		"name":         {"Work token"},
		"access_token": {"ghp_saved"},
		"return_to":    {"https://evil.example/steal"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/settings/credentials/saved", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/settings/credentials?") {
		t.Errorf("offsite return path must fall back to the credentials page, got %q", loc)
	}
}

func TestUISavedCredentialUpdate(t *testing.T) {
	f := &fakeMemory{
		dsSchema:           githubSchema(),
		dsSavedCredentials: []DSSavedCredential{{ID: "cred-1", ProviderType: "github", Name: "Work token"}},
	}
	e := newDSUIServer(f)

	form := url.Values{
		"providerType": {"github"},
		"name":         {"Renamed token"},
		"return_to":    {"/settings/credentials"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/settings/credentials/saved/cred-1/update", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("update status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/settings/credentials?savedCredentialUpdated=1" {
		t.Errorf("update location = %q", loc)
	}
	if f.dsUpdatedSavedID != "cred-1" {
		t.Errorf("updated id = %q", f.dsUpdatedSavedID)
	}
	if f.dsUpdatedSavedReq == nil || f.dsUpdatedSavedReq.Name != "Renamed token" {
		t.Errorf("update request = %+v", f.dsUpdatedSavedReq)
	}
	// Blank secret inputs keep the stored secret.
	if len(f.dsUpdatedSavedReq.Credentials) != 0 {
		t.Errorf("credentials = %v, want none submitted", f.dsUpdatedSavedReq.Credentials)
	}
}

func TestUISavedCredentialDelete(t *testing.T) {
	e := newDSUIServer(&fakeMemory{})
	form := url.Values{"return_to": {"/settings/credentials"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/settings/credentials/saved/cred-1/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/settings/credentials?savedCredentialDeleted=1" {
		t.Errorf("delete location = %q", loc)
	}
}

func TestUISavedCredentialDeleteInUse(t *testing.T) {
	f := &fakeMemory{dsSavedCredentialErr: &memoryHTTPError{
		Status:  http.StatusConflict,
		Code:    "conflict",
		Message: "credential is used by 2 data sources",
	}}
	e := newDSUIServer(f)
	form := url.Values{"return_to": {"/settings/credentials"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/settings/credentials/saved/cred-1/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete status = %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/settings/credentials?") || !strings.Contains(loc, "credential+is+used+by+2+data+sources") {
		t.Errorf("in-use delete should surface the server message, got %q", loc)
	}
}

func TestUIProjectCredentialsShowsSavedCredentials(t *testing.T) {
	f := &fakeMemory{
		project:     &Project{ID: "p1", Name: "Home", OrgID: "o1"},
		dsProviders: savedCredentialProviders(),
		dsSchema:    githubSchema(),
		dsSavedCredentials: []DSSavedCredential{
			{ID: "cred-1", ProviderType: "github", Name: "Work token", UsedByCount: 1},
		},
	}
	e := newDSUIServer(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/credentials", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`data-testid="ds-saved-credentials-table"`, "Work token", "GitHub", `data-testid="ds-saved-credential-provider"`} {
		if !strings.Contains(body, want) {
			t.Errorf("credentials page missing %q", want)
		}
	}
}

func TestUIDataSourceNewPreselectsCredential(t *testing.T) {
	f := &fakeMemory{
		dsProviders: savedCredentialProviders(),
		dsSchema:    githubSchema(),
		dsSavedCredentials: []DSSavedCredential{
			{ID: "cred-1", ProviderType: "github", Name: "Work token", UsedByCount: 0},
		},
	}
	e := newDSUIServer(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/new?providerType=github&method=credentials&credentialId=cred-1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, `value="cred-1" selected`) {
		t.Error("?credentialId should preselect the saved credential")
	}

	// The post-create redirect (?savedCredential) preselects it too.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/new?providerType=github&method=credentials&savedCredential=cred-1", nil))
	if body := rec.Body.String(); !strings.Contains(body, `value="cred-1" selected`) {
		t.Error("?savedCredential should preselect the saved credential")
	}
}

func TestUIDataSourceCreateForwardsCredentialID(t *testing.T) {
	f := &fakeMemory{dsSchema: githubSchema()}
	e := newDSUIServer(f)
	form := url.Values{
		"providerType":  {"github"},
		"name":          {"Docs"},
		"repo":          {"acme/private-docs"},
		"credential_id": {"cred-1"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/data-sources/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if f.dsCreatedReq == nil || f.dsCreatedReq.CredentialID != "cred-1" {
		t.Fatalf("create request = %+v, want credentialId cred-1", f.dsCreatedReq)
	}
	if _, ok := f.dsCreatedReq.Config["access_token"]; ok {
		t.Error("a saved-credential create must not carry an inline token")
	}
}

func TestUIDataSourceUpdateForwardsCredentialID(t *testing.T) {
	f := &fakeMemory{
		dsSchema:       githubSchema(),
		dsIntegrations: []DSIntegration{{ID: "s9", Name: "Docs", ProviderType: "github", SyncMode: "manual"}},
	}
	e := newDSUIServer(f)
	form := url.Values{
		"providerType":  {"github"},
		"name":          {"Docs"},
		"repo":          {"acme/private-docs"},
		"credential_id": {"cred-2"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/data-sources/s9/update", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if f.dsUpdatedReq == nil || f.dsUpdatedReq.CredentialID != "cred-2" {
		t.Fatalf("update request = %+v, want credentialId cred-2", f.dsUpdatedReq)
	}
}

func TestUIDataSourceTestConfigForwardsCredentialID(t *testing.T) {
	f := &fakeMemory{dsSchema: githubSchema()}
	e := newDSUIServer(f)
	form := url.Values{
		"providerType":  {"github"},
		"repo":          {"acme/private-docs"},
		"credential_id": {"cred-1"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/data-sources/test-config", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if f.dsLastTestConfig == nil || f.dsLastTestConfig.CredentialID != "cred-1" {
		t.Fatalf("test-config request = %+v, want credentialId cred-1", f.dsLastTestConfig)
	}
}

func TestUIDataSourceReposForwardsCredentialID(t *testing.T) {
	f := &fakeMemory{dsSchema: githubSchema()}
	e := newDSUIServer(f)
	form := url.Values{
		"providerType":  {"github"},
		"fieldKey":      {"repo"},
		"credential_id": {"cred-1"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/data-sources/repos", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if f.dsListedCredentialID != "cred-1" {
		t.Errorf("listing credential id = %q, want cred-1", f.dsListedCredentialID)
	}
}

func TestUIDataSourceDetailShowsCredentialPicker(t *testing.T) {
	f := &fakeMemory{
		dsProviders: savedCredentialProviders(),
		dsSchema:    githubSchema(),
		dsIntegrations: []DSIntegration{{
			ID: "s9", Name: "Docs", ProviderType: "github", Status: "active",
			CredentialID: "cred-1", Config: map[string]any{"repo": "acme/private-docs"},
		}},
		dsSavedCredentials: []DSSavedCredential{{ID: "cred-1", ProviderType: "github", Name: "Work token", UsedByCount: 1}},
	}
	e := newDSUIServer(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/s9", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`name="credential_id"`,
		`value="cred-1" selected`,
		`data-testid="ds-credential-create"`,
		`data-testid="ds-saved-credential-modal"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("detail page missing %q", want)
		}
	}
}

func TestUIProjectCredentialsSurvivesSavedCredentialReadFailure(t *testing.T) {
	f := &fakeMemory{
		project:              &Project{ID: "p1", Name: "Home", OrgID: "o1"},
		dsProviders:          savedCredentialProviders(),
		dsSchema:             githubSchema(),
		dsSavedCredentialErr: &memoryHTTPError{Status: http.StatusBadGateway, Message: "upstream unavailable"},
		dsProjectOAuthApps: []DSOAuthApp{
			{ID: "app-1", Scope: "project", Vendor: "google", VendorName: "Google", Name: "Google OAuth app", ClientID: "cid", Configured: true},
		},
	}
	e := newDSUIServer(f)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/credentials", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Saved credentials could not be loaded") {
		t.Error("a failed saved-credential read should be called out inline")
	}
	// The OAuth apps table still renders.
	if !strings.Contains(body, `data-testid="ds-credentials-table"`) {
		t.Error("the OAuth apps table should still render")
	}
}
