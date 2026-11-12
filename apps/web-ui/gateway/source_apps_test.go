package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// sourceAppsTestServer wires the source-App settings page, the trigger routes,
// and the data-source detail page onto a server whose public memory URL is set,
// so the rendered webhook URL is assertable (newDSUIServer leaves it empty).
func sourceAppsTestServer(f *fakeMemory) *echo.Echo {
	s := &Server{
		cfg:    Config{DefaultAgent: "memory", MemoryPublicURL: "https://memory.example.com"},
		memory: f,
	}
	e := echo.New()
	e.GET(dsSourceAppsPath, s.uiSourceApps)
	e.POST(dsSourceAppsPath, s.uiSourceAppCreate)
	e.GET(dsSourceAppsPath+"/new", s.uiSourceAppNew)
	e.GET(dsSourceAppsPath+"/:appId", s.uiSourceAppDetail)
	e.POST(dsSourceAppsPath+"/:appId", s.uiSourceAppUpdate)
	e.POST(dsSourceAppsPath+"/:appId/delete", s.uiSourceAppDelete)
	e.GET("/data-sources/new", s.uiDataSourceNew)
	e.GET("/data-sources/:id", s.uiDataSourceDetail)
	e.POST("/data-sources/:id/triggers", s.uiDataSourceTriggerCreate)
	e.POST("/data-sources/:id/triggers/:triggerId/toggle", s.uiDataSourceTriggerToggle)
	e.POST("/data-sources/:id/triggers/:triggerId/delete", s.uiDataSourceTriggerDelete)
	return e
}

// sourceAppsAdminFake is the fixture for a caller who administers the active
// project's org: an active project in o1, an org_admin role on o1, and the org
// record the page looks up for its subtitle.
func sourceAppsAdminFake() *fakeMemory {
	return &fakeMemory{
		project:         &Project{ID: "p1", Name: "Project", OrgID: "o1"},
		orgs:            []Org{{ID: "o1", Name: "Acme"}},
		orgsAndProjects: []OrgWithProjectsDto{{ID: "o1", Name: "Acme", Role: "org_admin"}},
	}
}

func TestSourceAppsPageRendersRegisteredApps(t *testing.T) {
	f := sourceAppsAdminFake()
	f.dsSourceApps = []SourceApp{{
		ID: "a1", OrgID: "o1", Provider: "github", AppID: 4242, Slug: "acme-bot",
		WebhookKey: "wk-1", CreatedAt: "2026-09-01T00:00:00Z",
	}}
	e := sourceAppsTestServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, dsSourceAppsPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="ds-source-apps-table"`,
		`data-testid="ds-source-app-row"`,
		`data-testid="ds-source-app-slug"`,
		"acme-bot",
		`data-testid="ds-source-app-id"`,
		"4242",
		`data-testid="ds-source-app-webhook-url"`,
		"https://memory.example.com/api/webhooks/source-apps/wk-1",
		`data-testid="ds-source-app-manage"`,
		`href="` + dsSourceAppsPath + `/a1"`,
		`data-testid="ds-source-app-add"`,
		`href="` + dsSourceAppsPath + `/new"`,
		"Acme",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
	if strings.Contains(body, `data-testid="ds-source-apps-empty"`) {
		t.Error("empty state rendered alongside a registered App")
	}
	if strings.Contains(body, `data-testid="ds-source-apps-forbidden"`) {
		t.Error("forbidden state rendered for an org admin")
	}
	// The list is a table plus a separate add page: no inline register form, and
	// no write-input for a secret anywhere on the table.
	for _, unwanted := range []string{
		`data-testid="ds-source-app-form"`,
		`name="webhookSecret"`,
		`name="privateKey"`,
	} {
		if strings.Contains(body, unwanted) {
			t.Errorf("list page rendered inline form markup %q", unwanted)
		}
	}
}

func TestSourceAppsPageEmptyState(t *testing.T) {
	e := sourceAppsTestServer(sourceAppsAdminFake())

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, dsSourceAppsPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="ds-source-apps-empty"`) {
		t.Error("empty registry did not render the empty state")
	}
	// An empty registry points at the standalone register page; it does not
	// carry the form itself.
	if !strings.Contains(body, `href="`+dsSourceAppsPath+`/new"`) {
		t.Error("empty registry lost its link to the register page")
	}
	for _, unwanted := range []string{
		`data-testid="ds-source-app-form"`,
		`data-testid="ds-source-apps-table"`,
	} {
		if strings.Contains(body, unwanted) {
			t.Errorf("empty registry rendered %q", unwanted)
		}
	}
}

func TestSourceAppsPageForbiddenForNonAdmin(t *testing.T) {
	f := sourceAppsAdminFake()
	f.orgsAndProjects = []OrgWithProjectsDto{{ID: "o1", Name: "Acme", Role: "org_member"}}
	// A list the page must not show: the API would answer this caller with 403.
	f.dsSourceApps = []SourceApp{{ID: "a1", OrgID: "o1", AppID: 1, Slug: "secret-bot"}}
	e := sourceAppsTestServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, dsSourceAppsPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="ds-source-apps-forbidden"`) {
		t.Error("non-admin did not get the forbidden state")
	}
	if !strings.Contains(body, "Organization administrators only") {
		t.Error("forbidden state lacks its explanation")
	}
	for _, forbidden := range []string{
		`data-testid="ds-source-app-form"`,
		`data-testid="ds-source-app-add"`,
		`data-testid="ds-source-app-row"`,
		"secret-bot",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("non-admin page rendered %q", forbidden)
		}
	}
}

func TestSourceAppsPageRedirectsWithoutProject(t *testing.T) {
	e := sourceAppsTestServer(&fakeMemory{})

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, dsSourceAppsPath, nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/orgs" {
		t.Errorf("location = %q, want /orgs", loc)
	}
}

func TestSourceAppCreateForwardsRequest(t *testing.T) {
	f := sourceAppsAdminFake()
	e := sourceAppsTestServer(f)

	form := url.Values{
		"appId":         {" 4242 "},
		"slug":          {"acme-bot"},
		"privateKey":    {"-----BEGIN RSA PRIVATE KEY-----\nkey\n-----END RSA PRIVATE KEY-----"},
		"webhookSecret": {"s3cret"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, dsSourceAppsPath, strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != dsSourceAppsPath+"?saved=1" {
		t.Errorf("location = %q", loc)
	}
	if f.dsCreatedSourceAppOrg != "o1" {
		t.Errorf("org = %q, want o1", f.dsCreatedSourceAppOrg)
	}
	got := f.dsCreatedSourceAppReq
	if got == nil {
		t.Fatal("no create request forwarded")
	}
	if got.Provider != "github" || got.AppID != 4242 || got.Slug != "acme-bot" {
		t.Errorf("request = %+v", got)
	}
	if got.PrivateKey != form.Get("privateKey") || got.WebhookSecret != "s3cret" {
		t.Errorf("secrets not forwarded: %+v", got)
	}
}

func TestSourceAppCreateRejectsNonNumericAppID(t *testing.T) {
	f := sourceAppsAdminFake()
	e := sourceAppsTestServer(f)

	form := url.Values{
		"appId":         {"acme-bot"}, // the slug, not the numeric App id
		"slug":          {"acme-bot"},
		"privateKey":    {"key"},
		"webhookSecret": {"s3cret"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, dsSourceAppsPath, strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if f.dsCreatedSourceAppReq != nil {
		t.Error("invalid App id was forwarded to the API")
	}
	assertRedirectError(t, rec.Header().Get("Location"), "numeric App id")
}

func TestSourceAppCreateRequiresSecrets(t *testing.T) {
	f := sourceAppsAdminFake()
	e := sourceAppsTestServer(f)

	form := url.Values{"appId": {"1"}, "slug": {"acme-bot"}, "privateKey": {""}, "webhookSecret": {"s3cret"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, dsSourceAppsPath, strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	e.ServeHTTP(rec, req)

	if f.dsCreatedSourceAppReq != nil {
		t.Error("request without a private key was forwarded")
	}
	assertRedirectError(t, rec.Header().Get("Location"), "private key is required")
}

func TestSourceAppCreateRefusedForNonAdmin(t *testing.T) {
	f := sourceAppsAdminFake()
	f.orgsAndProjects = []OrgWithProjectsDto{{ID: "o1", Name: "Acme", Role: "org_member"}}
	e := sourceAppsTestServer(f)

	form := url.Values{"appId": {"1"}, "slug": {"acme-bot"}, "privateKey": {"key"}, "webhookSecret": {"s3cret"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, dsSourceAppsPath, strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	e.ServeHTTP(rec, req)

	if f.dsCreatedSourceAppReq != nil {
		t.Error("non-admin write was forwarded to the API")
	}
	assertRedirectError(t, rec.Header().Get("Location"), "organization administrator")
}

func TestSourceAppNewPageRendersRegisterForm(t *testing.T) {
	e := sourceAppsTestServer(sourceAppsAdminFake())

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, dsSourceAppsPath+"/new", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="ds-source-app-form-page"`,
		`data-testid="ds-source-app-form"`,
		`action="` + dsSourceAppsPath + `"`,
		`name="appId"`,
		`name="slug"`,
		`name="privateKey"`,
		`name="webhookSecret"`,
		`data-testid="ds-source-app-save"`,
		"Register source app",
		"Back",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("register page missing %q", want)
		}
	}
	// Both secrets are mandatory on the add page, and there is nothing to
	// remove or copy yet.
	if strings.Count(body, "required") < 2 {
		t.Error("register form did not mark its secrets required")
	}
	for _, unwanted := range []string{
		`data-testid="ds-source-app-delete"`,
		`data-testid="ds-source-app-webhook"`,
		`readonly`,
	} {
		if strings.Contains(body, unwanted) {
			t.Errorf("register page rendered %q", unwanted)
		}
	}
}

func TestSourceAppNewPageNeedsOrgAdmin(t *testing.T) {
	f := sourceAppsAdminFake()
	f.orgsAndProjects = []OrgWithProjectsDto{{ID: "o1", Name: "Acme", Role: "org_member"}}
	e := sourceAppsTestServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, dsSourceAppsPath+"/new", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `data-testid="ds-source-app-form"`) {
		t.Error("non-admin got the register form")
	}
}

func TestSourceAppNewPageRedirectsWithoutProject(t *testing.T) {
	e := sourceAppsTestServer(&fakeMemory{})

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, dsSourceAppsPath+"/new", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/orgs" {
		t.Errorf("location = %q, want /orgs", loc)
	}
}

func TestSourceAppDetailPagePrefillsApp(t *testing.T) {
	f := sourceAppsAdminFake()
	f.dsSourceApps = []SourceApp{{
		ID: "a1", OrgID: "o1", Provider: "github", AppID: 4242, Slug: "acme-bot",
		WebhookKey: "wk-1", CreatedAt: "2026-09-01T00:00:00Z",
	}}
	e := sourceAppsTestServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, dsSourceAppsPath+"/a1?saved=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Edit source app",
		"Source App saved",
		`action="` + dsSourceAppsPath + `/a1"`,
		`value="acme-bot"`,
		`value="4242"`,
		`readonly`,
		"Saved — leave empty to keep",
		`data-testid="ds-source-app-webhook"`,
		"https://memory.example.com/api/webhooks/source-apps/wk-1",
		`action="` + dsSourceAppsPath + `/a1/delete"`,
		`data-testid="ds-source-app-delete"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("edit page missing %q", want)
		}
	}
	// The identity fields are fixed, and neither the App id nor a secret can be
	// submitted from the edit page: the PATCH takes a slug and optional secrets.
	for _, unwanted := range []string{
		`name="appId"`,
		`name="provider"`,
	} {
		if strings.Contains(body, unwanted) {
			t.Errorf("edit page rendered %q", unwanted)
		}
	}
	// The delete form is a sibling of the save form, never nested inside it: a
	// nested form is invalid HTML and the inner submit silently posts nothing.
	saveStart := strings.Index(body, `data-testid="ds-source-app-form"`)
	saveEnd := strings.Index(body[saveStart:], "</form>")
	if saveStart < 0 || saveEnd < 0 {
		t.Fatalf("save form bounds not found")
	}
	if strings.Contains(body[saveStart:saveStart+saveEnd], `ds-source-app-delete`) {
		t.Error("delete control rendered inside the save form")
	}
}

func TestSourceAppDetailUnknownIDRedirectsWithError(t *testing.T) {
	f := sourceAppsAdminFake()
	f.dsSourceApps = []SourceApp{{ID: "a1", OrgID: "o1", AppID: 1, Slug: "acme-bot"}}
	e := sourceAppsTestServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, dsSourceAppsPath+"/nope", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	assertRedirectError(t, rec.Header().Get("Location"), "no such source App")
}

func TestSourceAppDetailRendersSaveError(t *testing.T) {
	f := sourceAppsAdminFake()
	f.dsSourceApps = []SourceApp{{ID: "a1", OrgID: "o1", AppID: 4242, Slug: "acme-bot"}}
	e := sourceAppsTestServer(f)

	// The update redirect carries the failure in ?err=; the edit page must show
	// it or a rejected key rotation looks like a no-op.
	loc := dsSourceAppsPath + "/a1?err=" + url.QueryEscape("privateKey is not PEM encoded")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, loc, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Source App not saved",
		"privateKey is not PEM encoded",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("edit page missing %q", want)
		}
	}
	// The failure must not be mistaken for a success toast.
	if strings.Contains(body, "Source App saved") {
		t.Error("error render also showed the success toast")
	}
}

func TestSourceAppDetailNeedsOrgAdmin(t *testing.T) {
	f := sourceAppsAdminFake()
	f.orgsAndProjects = []OrgWithProjectsDto{{ID: "o1", Name: "Acme", Role: "org_member"}}
	f.dsSourceApps = []SourceApp{{ID: "a1", OrgID: "o1", AppID: 1, Slug: "secret-bot"}}
	e := sourceAppsTestServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, dsSourceAppsPath+"/a1", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret-bot") {
		t.Error("non-admin got the App edit page")
	}
}

func TestSourceAppUpdateForwardsSlugAndBlankSecrets(t *testing.T) {
	f := sourceAppsAdminFake()
	f.dsSourceApps = []SourceApp{{ID: "a1", OrgID: "o1", AppID: 4242, Slug: "acme-bot"}}
	e := sourceAppsTestServer(f)

	form := url.Values{"slug": {" acme-renamed "}, "privateKey": {""}, "webhookSecret": {""}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, dsSourceAppsPath+"/a1", strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != dsSourceAppsPath+"/a1?saved=1" {
		t.Errorf("location = %q", loc)
	}
	if f.dsUpdatedSourceAppOrg != "o1" || f.dsUpdatedSourceAppID != "a1" {
		t.Errorf("target = %q/%q, want o1/a1", f.dsUpdatedSourceAppOrg, f.dsUpdatedSourceAppID)
	}
	got := f.dsUpdatedSourceAppReq
	if got == nil {
		t.Fatal("no update request forwarded")
	}
	if got.Slug != "acme-renamed" {
		t.Errorf("slug = %q, want the trimmed value", got.Slug)
	}
	// Blank fields must stay blank: they are omitted from the PATCH so the
	// server keeps the sealed copy.
	if got.PrivateKey != "" || got.WebhookSecret != "" {
		t.Errorf("blank secrets were forwarded: %+v", got)
	}
}

func TestSourceAppUpdateForwardsRotatedSecret(t *testing.T) {
	f := sourceAppsAdminFake()
	f.dsSourceApps = []SourceApp{{ID: "a1", OrgID: "o1", AppID: 4242, Slug: "acme-bot"}}
	e := sourceAppsTestServer(f)

	// The gateway only forwards the key (the server validates it), so a plain
	// marker string is enough: it keeps the scan clean of PEM literals.
	form := url.Values{"slug": {"acme-bot"}, "privateKey": {"rotated-private-key-material"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, dsSourceAppsPath+"/a1", strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	e.ServeHTTP(rec, req)

	got := f.dsUpdatedSourceAppReq
	if got == nil {
		t.Fatal("no update request forwarded")
	}
	if got.PrivateKey != form.Get("privateKey") {
		t.Errorf("rotated key not forwarded: %+v", got)
	}
	if got.WebhookSecret != "" {
		t.Errorf("untouched webhook secret was overwritten: %+v", got)
	}
}

func TestSourceAppUpdateRequiresSlug(t *testing.T) {
	f := sourceAppsAdminFake()
	e := sourceAppsTestServer(f)

	form := url.Values{"slug": {"   "}, "privateKey": {"key"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, dsSourceAppsPath+"/a1", strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	e.ServeHTTP(rec, req)

	if f.dsUpdatedSourceAppReq != nil {
		t.Error("slug-less update was forwarded")
	}
	assertRedirectError(t, rec.Header().Get("Location"), "app slug is required")
}

func TestSourceAppUpdateRefusedForNonAdmin(t *testing.T) {
	f := sourceAppsAdminFake()
	f.orgsAndProjects = []OrgWithProjectsDto{{ID: "o1", Name: "Acme", Role: "org_member"}}
	e := sourceAppsTestServer(f)

	form := url.Values{"slug": {"acme-bot"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, dsSourceAppsPath+"/a1", strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	e.ServeHTTP(rec, req)

	if f.dsUpdatedSourceAppReq != nil {
		t.Error("non-admin update was forwarded to the API")
	}
	assertRedirectError(t, rec.Header().Get("Location"), "organization administrator")
}

func TestSourceAppDeleteForwardsID(t *testing.T) {
	f := sourceAppsAdminFake()
	e := sourceAppsTestServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, dsSourceAppsPath+"/a1/delete", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != dsSourceAppsPath+"?deleted=1" {
		t.Errorf("location = %q", loc)
	}
	if f.dsDeletedSourceAppID != "a1" {
		t.Errorf("deleted id = %q, want a1", f.dsDeletedSourceAppID)
	}
}

func TestSourceAppDeleteSurfacesConflictMessage(t *testing.T) {
	f := sourceAppsAdminFake()
	const msg = "source App is bound to project sources: acme/docs, acme/web"
	f.dsSourceAppErr = &memoryHTTPError{Status: http.StatusConflict, Code: "conflict", Message: msg}
	e := sourceAppsTestServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, dsSourceAppsPath+"/a1/delete", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	// The server's remediation text (which sources block the delete) is the
	// useful feedback; it must reach the flash verbatim.
	assertRedirectError(t, rec.Header().Get("Location"), msg)
}

func TestDataSourceTriggerCreateForwardsPullRequest(t *testing.T) {
	f := sourceAppsAdminFake()
	f.dsIntegrations = []DSIntegration{{ID: "s1", Name: "GH", ProviderType: dsProviderTypeGitHubApp}}
	e := sourceAppsTestServer(f)

	form := url.Values{"agentId": {"agent-1"}, "queue": {" reviews "}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/data-sources/s1/triggers", strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/data-sources/s1?triggerCreated=1#triggers" {
		t.Errorf("location = %q", loc)
	}
	got := f.dsCreatedTriggerReq
	if got == nil {
		t.Fatal("no trigger create forwarded")
	}
	if f.dsCreatedTriggerSource != "s1" {
		t.Errorf("source = %q, want s1", f.dsCreatedTriggerSource)
	}
	if got.EventType != dsTriggerEventPullRequest || got.AgentID != "agent-1" || got.Queue != "reviews" {
		t.Errorf("request = %+v", got)
	}
	if got.Enabled == nil || !*got.Enabled {
		t.Errorf("new trigger not enabled: %+v", got)
	}
}

func TestDataSourceTriggerCreateRequiresAgent(t *testing.T) {
	f := sourceAppsAdminFake()
	f.dsIntegrations = []DSIntegration{{ID: "s1", Name: "GH", ProviderType: dsProviderTypeGitHubApp}}
	e := sourceAppsTestServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/data-sources/s1/triggers", nil))

	if f.dsCreatedTriggerReq != nil {
		t.Error("trigger without an agent was forwarded")
	}
	assertRedirectError(t, rec.Header().Get("Location"), "pick an agent")
}

func TestDataSourceTriggerCreateRefusesNonGitHubAppSource(t *testing.T) {
	f := sourceAppsAdminFake()
	f.dsIntegrations = []DSIntegration{{ID: "s1", Name: "Drive", ProviderType: "google_drive"}}
	e := sourceAppsTestServer(f)

	form := url.Values{"agentId": {"agent-1"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/data-sources/s1/triggers", strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	e.ServeHTTP(rec, req)

	if f.dsCreatedTriggerReq != nil {
		t.Error("trigger on a non-github_app source was forwarded")
	}
	assertRedirectError(t, rec.Header().Get("Location"), "does not support triggers")
}

func TestDataSourceTriggerToggleSendsOnlyEnabled(t *testing.T) {
	f := sourceAppsAdminFake()
	f.dsSourceTriggers = map[string][]DSSourceTrigger{"s1": {{ID: "t1", EventType: "pull_request", AgentID: "agent-1", Enabled: true}}}
	e := sourceAppsTestServer(f)

	form := url.Values{"enabled": {"false"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/data-sources/s1/triggers/t1/toggle", strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/data-sources/s1?triggerToggled=1#triggers" {
		t.Errorf("location = %q", loc)
	}
	if f.dsUpdatedTriggerID != "t1" {
		t.Errorf("updated id = %q", f.dsUpdatedTriggerID)
	}
	got := f.dsUpdatedTriggerReq
	if got == nil || got.Enabled == nil || *got.Enabled {
		t.Fatalf("enabled request = %+v", got)
	}
	if got.EventType != "" || got.AgentID != "" || got.Queue != "" {
		t.Errorf("toggle sent fields it should leave alone: %+v", got)
	}
}

func TestDataSourceTriggerDeleteForwardsIDs(t *testing.T) {
	f := sourceAppsAdminFake()
	e := sourceAppsTestServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/data-sources/s1/triggers/t1/delete", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/data-sources/s1?triggerDeleted=1#triggers" {
		t.Errorf("location = %q", loc)
	}
	if f.dsDeletedTriggerID != "t1" {
		t.Errorf("deleted id = %q", f.dsDeletedTriggerID)
	}
}

func TestDataSourceDetailRendersTriggersForGitHubApp(t *testing.T) {
	f := sourceAppsAdminFake()
	f.dsIntegrations = []DSIntegration{{ID: "s1", Name: "GH", ProviderType: dsProviderTypeGitHubApp}}
	f.dsSourceTriggers = map[string][]DSSourceTrigger{"s1": {{
		ID: "t1", SourceID: "s1", EventType: "pull_request", AgentID: "agent-1", Enabled: true,
	}}}
	f.agents = []AgentDefinitionSummary{{ID: "agent-1", Name: "Reviewer"}}
	e := sourceAppsTestServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/s1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="ds-triggers"`,
		`data-testid="ds-trigger-row"`,
		"pull_request",
		"Reviewer",
		"agent-1",
		"Queue: default",
		"Disable",
		`data-testid="ds-trigger-form"`,
		`data-testid="ds-trigger-agent-select"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("detail page missing %q", want)
		}
	}
	if strings.Contains(body, `data-testid="ds-trigger-agents-empty"`) {
		t.Error("agent picker reported an empty project though agents exist")
	}
	// A trigger-only source never syncs documents: no sync action, no history
	// card inviting the action that can only fail, and no connection test (the
	// App credentials live in the org registry, not this source).
	for _, dead := range []string{
		`data-testid="ds-trigger-sync"`,
		`data-testid="ds-sync-history"`,
		`data-testid="ds-sync-empty"`,
		`data-testid="ds-field-sync-mode"`,
		`data-testid="ds-test-connection"`,
		`id="ds-test-result"`,
	} {
		if strings.Contains(body, dead) {
			t.Errorf("github_app detail rendered %q", dead)
		}
	}
	if !strings.Contains(body, `data-testid="ds-connection-note"`) {
		t.Error("github_app detail lost the trigger-only connection explanation")
	}
}

func TestDataSourceDetailHidesTriggerCreateWithoutAgents(t *testing.T) {
	f := sourceAppsAdminFake()
	f.dsIntegrations = []DSIntegration{{ID: "s1", Name: "GH", ProviderType: dsProviderTypeGitHubApp}}
	e := sourceAppsTestServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/s1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="ds-trigger-agents-empty"`) {
		t.Error("no-agents state is not explained")
	}
	if !strings.Contains(body, `href="/agents"`) {
		t.Error("no-agents state does not link to the agents page")
	}
	for _, dead := range []string{
		`data-testid="ds-trigger-form"`,
		`data-testid="ds-trigger-agent-select"`,
		`data-testid="ds-trigger-create"`,
	} {
		if strings.Contains(body, dead) {
			t.Errorf("no-agents state rendered %q", dead)
		}
	}
}

func TestDataSourceConnectHidesSyncModeForGitHubApp(t *testing.T) {
	f := sourceAppsAdminFake()
	f.dsProviders = []DSProvider{{Type: dsProviderTypeGitHubApp, Name: "GitHub App", AuthMethods: []string{"config"}}}
	f.dsSchema = &DSProviderSchema{Type: dsProviderTypeGitHubApp, Properties: map[string]any{
		"app_id":          map[string]any{"type": "string"},
		"installation_id": map[string]any{"type": "string"},
		"repos":           map[string]any{"type": "array"},
	}}
	e := sourceAppsTestServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/new?providerType="+dsProviderTypeGitHubApp, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="ds-connect-form"`) {
		t.Fatalf("github_app connect form did not render: %s", body)
	}
	// The app_id picker is still the org-App select special case, and its help
	// text points at the registry page instead of the API contract.
	if !strings.Contains(body, `data-testid="ds-field-app_id-empty"`) {
		t.Errorf("app_id field did not render its empty registry state: %s", body)
	}
	if strings.Contains(body, "/api/orgs/") {
		t.Errorf("connect form leaked the API contract text: %s", body)
	}
	if !strings.Contains(body, "Manage Apps under Settings") {
		t.Errorf("connect form missing the app_id remediation copy: %s", body)
	}
	for _, dead := range []string{`data-testid="ds-field-sync-mode"`, `data-testid="ds-field-sync-interval"`} {
		if strings.Contains(body, dead) {
			t.Errorf("github_app connect form rendered %q", dead)
		}
	}
}

func TestDataSourceConnectKeepsSyncModeForOtherProviders(t *testing.T) {
	f := sourceAppsAdminFake()
	f.dsProviders = []DSProvider{{Type: dsProviderTypeGoogleDrive, Name: "Google Drive", AuthMethods: []string{"credentials"}}}
	e := sourceAppsTestServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/new?providerType="+dsProviderTypeGoogleDrive, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `data-testid="ds-field-sync-mode"`) {
		t.Error("non-trigger provider lost its sync-mode select")
	}
}

func TestDataSourceDetailOmitsTriggersForOtherProviders(t *testing.T) {
	f := sourceAppsAdminFake()
	f.dsIntegrations = []DSIntegration{{
		ID: "s1", Name: "Drive", ProviderType: "google_drive",
		Config: map[string]any{"folder_id": "f1"},
	}}
	e := sourceAppsTestServer(f)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data-sources/s1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, `data-testid="ds-triggers"`) {
		t.Error("non-github_app source rendered the trigger panel")
	}
	// The sync affordances the trigger-only provider hides stay for a syncing
	// provider: the canSync/history conditions must not switch them off
	// globally.
	for _, want := range []string{
		`data-testid="ds-trigger-sync"`,
		`data-testid="ds-sync-history"`,
		`data-testid="ds-field-sync-mode"`,
		`data-testid="ds-test-connection"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("google_drive detail missing %q", want)
		}
	}
	if strings.Contains(body, `data-testid="ds-connection-note"`) {
		t.Error("google_drive detail rendered the trigger-only connection note")
	}
}

func TestSourceAppWebhookURL(t *testing.T) {
	cases := []struct {
		name string
		base string
		key  string
		want string
	}{
		{"trailing slash trimmed", "https://memory.example.com/", "wk-1", "https://memory.example.com/api/webhooks/source-apps/wk-1"},
		{"no trailing slash", "https://memory.example.com", "wk-1", "https://memory.example.com/api/webhooks/source-apps/wk-1"},
		{"empty key", "https://memory.example.com", "", ""},
		{"key is escaped", "https://memory.example.com", "a/b", "https://memory.example.com/api/webhooks/source-apps/a%2Fb"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := dsSourceAppWebhookURL(tc.base, tc.key); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDsSourceAppLabel(t *testing.T) {
	cases := []struct {
		app  SourceApp
		want string
	}{
		{SourceApp{Slug: "acme-bot", AppID: 42}, "acme-bot (42)"},
		{SourceApp{AppID: 42}, "42"},
		{SourceApp{Slug: "acme-bot"}, "acme-bot"},
		{SourceApp{Slug: "  acme-bot  ", AppID: 7}, "acme-bot (7)"},
	}
	for _, tc := range cases {
		if got := dsSourceAppLabel(tc.app); got != tc.want {
			t.Errorf("dsSourceAppLabel(%+v) = %q, want %q", tc.app, got, tc.want)
		}
	}
}

func TestDsSourceTriggerHelpers(t *testing.T) {
	if got := dsTriggerToggleLabel(true); got != "Disable" {
		t.Errorf("toggle label enabled = %q", got)
	}
	if got := dsTriggerToggleLabel(false); got != "Enable" {
		t.Errorf("toggle label disabled = %q", got)
	}
	if got := dsTriggerNextEnabled(true); got != "false" {
		t.Errorf("next enabled for enabled = %q", got)
	}
	if got := dsTriggerNextEnabled(false); got != "true" {
		t.Errorf("next enabled for disabled = %q", got)
	}
	if got := dsTriggerQueueLabel(""); got != "default" {
		t.Errorf("empty queue label = %q", got)
	}
	if got := dsTriggerQueueLabel(" reviews "); got != "reviews" {
		t.Errorf("queue label = %q", got)
	}
	if got := dsTriggerAgentName([]AgentDefinitionSummary{{ID: "a1", Name: "Reviewer"}}, "a1"); got != "Reviewer" {
		t.Errorf("agent name = %q", got)
	}
	// A deleted agent keeps its id so the stale row stays removable.
	if got := dsTriggerAgentName(nil, "gone"); got != "gone" {
		t.Errorf("missing agent name = %q", got)
	}
}

func TestDsAttachSourceAppSelect(t *testing.T) {
	fields := []dsConfigField{{Key: "app_id", Label: "App ID", Type: "string"}, {Key: "installation_id", Type: "string"}}

	t.Run("options from the org registry", func(t *testing.T) {
		f := &fakeMemory{
			project:      &Project{ID: "p1", OrgID: "o1"},
			dsSourceApps: []SourceApp{{ID: "a1", OrgID: "o1", AppID: 42, Slug: "acme-bot"}},
		}
		s := &Server{memory: f}
		got := s.dsAttachSourceAppSelect(context.Background(), dsProviderTypeGitHubApp, append([]dsConfigField(nil), fields...))

		if got[0].Select == nil {
			t.Fatal("app_id has no select")
		}
		if got[1].Select != nil {
			t.Error("installation_id gained a select")
		}
		sel := got[0].Select
		if sel.LoadErr != nil {
			t.Fatalf("load error = %v", sel.LoadErr)
		}
		if len(sel.Options) != 1 || sel.Options[0].Value != "a1" || sel.Options[0].Label != "acme-bot (42)" {
			t.Errorf("options = %+v", sel.Options)
		}
		if sel.EmptyHref != dsSourceAppsPath {
			t.Errorf("empty href = %q", sel.EmptyHref)
		}
		// The schema's description is the API contract (it names the endpoint),
		// so the gateway replaces both label and help text for this form.
		if got[0].Label != "App ID" {
			t.Errorf("label = %q, want App ID", got[0].Label)
		}
		if !strings.Contains(got[0].Description, "Manage Apps under Settings") {
			t.Errorf("description = %q", got[0].Description)
		}
		if strings.Contains(got[0].Description, "/api/") {
			t.Errorf("description leaked the API contract: %q", got[0].Description)
		}
	})

	t.Run("empty registry explains the remediation", func(t *testing.T) {
		f := &fakeMemory{project: &Project{ID: "p1", OrgID: "o1"}}
		s := &Server{memory: f}
		got := s.dsAttachSourceAppSelect(context.Background(), dsProviderTypeGitHubApp, append([]dsConfigField(nil), fields...))

		sel := got[0].Select
		if sel == nil {
			t.Fatal("app_id has no select")
		}
		if len(sel.Options) != 0 || sel.LoadErr != nil {
			t.Errorf("select = %+v", sel)
		}
		if sel.EmptyMessage == "" || sel.EmptyLabel == "" || sel.EmptyHref != dsSourceAppsPath {
			t.Errorf("empty state incomplete: %+v", sel)
		}
	})

	t.Run("failed lookup is carried on the field", func(t *testing.T) {
		f := &fakeMemory{project: &Project{ID: "p1", OrgID: "o1"}, dsSourceAppErr: errors.New("boom")}
		s := &Server{memory: f}
		got := s.dsAttachSourceAppSelect(context.Background(), dsProviderTypeGitHubApp, append([]dsConfigField(nil), fields...))

		sel := got[0].Select
		if sel == nil || sel.LoadErr == nil {
			t.Errorf("select did not carry the load error: %+v", sel)
		}
	})

	t.Run("other providers are untouched", func(t *testing.T) {
		s := &Server{memory: &fakeMemory{project: &Project{ID: "p1", OrgID: "o1"}}}
		got := s.dsAttachSourceAppSelect(context.Background(), "google_drive", append([]dsConfigField(nil), fields...))
		for _, f := range got {
			if f.Select != nil {
				t.Errorf("provider-neutral field %q gained a select", f.Key)
			}
		}
	})
}

func TestDsConfigFieldInputRendersAppSelect(t *testing.T) {
	t.Run("options", func(t *testing.T) {
		f := dsConfigField{Key: "app_id", Label: "App ID", Type: "string", Select: &dsFieldSelect{
			Placeholder: "Select a registered App",
			Options:     []dsFieldOption{{Value: "a1", Label: "acme-bot (42)"}},
		}}
		var buf bytes.Buffer
		if err := dsConfigFieldInput(f).Render(context.Background(), &buf); err != nil {
			t.Fatal(err)
		}
		body := buf.String()
		for _, want := range []string{
			`data-testid="ds-field-app_id"`,
			`name="app_id"`,
			"Select a registered App",
			`value="a1"`,
			"acme-bot (42)",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("select missing %q: %s", want, body)
			}
		}
	})

	t.Run("empty registry links to the Apps page", func(t *testing.T) {
		f := dsConfigField{Key: "app_id", Label: "App ID", Type: "string", Select: &dsFieldSelect{
			EmptyHref:    dsSourceAppsPath,
			EmptyLabel:   "Open source Apps",
			EmptyMessage: "No GitHub Apps are registered for this organization yet.",
		}}
		var buf bytes.Buffer
		if err := dsConfigFieldInput(f).Render(context.Background(), &buf); err != nil {
			t.Fatal(err)
		}
		body := buf.String()
		if !strings.Contains(body, `data-testid="ds-field-app_id-empty"`) {
			t.Errorf("empty state not rendered: %s", body)
		}
		if !strings.Contains(body, "Open source Apps") || !strings.Contains(body, dsSourceAppsPath) {
			t.Errorf("remediation link missing: %s", body)
		}
		if strings.Contains(body, `name="app_id"`) {
			t.Error("empty registry rendered a dead input")
		}
	})

	t.Run("load error is shown", func(t *testing.T) {
		f := dsConfigField{Key: "app_id", Label: "App ID", Type: "string", Select: &dsFieldSelect{
			EmptyHref:  dsSourceAppsPath,
			EmptyLabel: "Open source Apps",
			LoadErr:    errors.New("nope"),
		}}
		var buf bytes.Buffer
		if err := dsConfigFieldInput(f).Render(context.Background(), &buf); err != nil {
			t.Fatal(err)
		}
		body := buf.String()
		if !strings.Contains(body, `data-testid="ds-field-app_id-error"`) || !strings.Contains(body, "nope") {
			t.Errorf("load error not rendered: %s", body)
		}
		// The connect form has no bound App to preserve: no empty hidden input
		// (submitting one would just trade the alert for "app_id is required").
		if strings.Contains(body, `name="app_id"`) {
			t.Errorf("connect form rendered an app_id input on load error: %s", body)
		}
		if !strings.Contains(body, "Open source Apps") || !strings.Contains(body, dsSourceAppsPath) {
			t.Errorf("load-error state lost its remediation link: %s", body)
		}
	})

	t.Run("load error keeps a bound App in the payload", func(t *testing.T) {
		f := dsConfigField{Key: "app_id", Label: "App ID", Type: "string", Value: "app-uuid-1", Select: &dsFieldSelect{
			EmptyHref:  dsSourceAppsPath,
			EmptyLabel: "Open source Apps",
			LoadErr:    errors.New("nope"),
		}}
		var buf bytes.Buffer
		if err := dsConfigFieldInput(f).Render(context.Background(), &buf); err != nil {
			t.Fatal(err)
		}
		body := buf.String()
		if !strings.Contains(body, `data-testid="ds-field-app_id-current"`) {
			t.Fatalf("edit form lost its current app_id: %s", body)
		}
		if !strings.Contains(body, `name="app_id"`) || !strings.Contains(body, `value="app-uuid-1"`) {
			t.Errorf("hidden app_id does not carry the bound value: %s", body)
		}
		if !strings.Contains(body, `data-testid="ds-field-app_id-error"`) {
			t.Error("load error alert was dropped alongside the hidden input")
		}
	})
}

// assertRedirectError checks a PRG Location carries an ?err= flash containing
// want (the message is what the page shows on the redirect target).
func assertRedirectError(t *testing.T, loc, want string) {
	t.Helper()
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("unparseable location %q: %v", loc, err)
	}
	msg := u.Query().Get("err")
	if msg == "" {
		t.Fatalf("location %q has no err flash", loc)
	}
	if !strings.Contains(msg, want) {
		t.Errorf("err flash = %q, want it to contain %q", msg, want)
	}
}
