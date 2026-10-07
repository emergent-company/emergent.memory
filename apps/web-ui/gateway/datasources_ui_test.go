package main

import (
	"net/http"
	"net/http/httptest"
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
	config, creds := dsConfigFields(testDSSchema())
	html := renderHTML(t, DataSourceNewPage(config, creds, nil, nil))
	for _, want := range []string{
		"Connect a data source",
		`data-testid="ds-connect-form"`,
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
		if !strings.Contains(html, want) {
			t.Errorf("connect form missing %q", want)
		}
	}
	// Credentials must never be prefilled.
	if strings.Contains(html, `name="refresh_token" value`) {
		t.Error("refresh_token must not be prefilled")
	}
	if strings.Contains(html, `name="client_secret" value`) {
		t.Error("client_secret must not be prefilled")
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
	e.GET("/data-sources/:id", s.uiDataSourceDetail)
	e.POST("/data-sources/:id/update", s.uiDataSourceUpdate)
	e.POST("/data-sources/:id/test-connection", s.uiDataSourceTestConnection)
	e.POST("/data-sources/:id/sync", s.uiDataSourceTriggerSync)
	e.POST("/data-sources/:id/sync-jobs/:jobId/cancel", s.uiDataSourceCancelSync)
	e.POST("/data-sources/:id/delete", s.uiDataSourceDelete)
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
