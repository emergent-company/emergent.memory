package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// newDSAPIServer builds an echo with the data-source + version JSON routes
// behind the client-key gate, mirroring newDocAPIServer.
func newDSAPIServer(f *fakeMemory, apiKey string) (*Server, *echo.Echo) {
	s := &Server{cfg: Config{DefaultAgent: "memory", ClientAPIKey: apiKey}, memory: f}
	e := echo.New()
	api := e.Group("/api", s.requireClientKey)
	api.GET("/data-source-integrations/providers", s.listDSProviders)
	api.GET("/data-source-integrations/providers/:providerType/schema", s.getDSProviderSchema)
	api.POST("/data-source-integrations/test-config", s.testDSConfig)
	api.GET("/data-source-integrations", s.listDSIntegrations)
	api.POST("/data-source-integrations", s.createDSIntegration)
	api.GET("/data-source-integrations/:id", s.getDSIntegration)
	api.PATCH("/data-source-integrations/:id", s.updateDSIntegration)
	api.DELETE("/data-source-integrations/:id", s.deleteDSIntegration)
	api.POST("/data-source-integrations/:id/test-connection", s.testDSConnection)
	api.POST("/data-source-integrations/:id/sync", s.triggerDSSync)
	api.GET("/data-source-integrations/:id/sync-jobs", s.listDSSyncJobs)
	api.GET("/data-source-integrations/:id/sync-jobs/latest", s.getDSLatestSyncJob)
	api.GET("/data-source-integrations/:id/sync-jobs/:jobId", s.getDSSyncJob)
	api.POST("/data-source-integrations/:id/sync-jobs/:jobId/cancel", s.cancelDSSyncJob)
	api.POST("/data-source-integrations/providers/:providerType/oauth/authorize", s.startDSOAuth)
	api.GET("/orgs/:orgId/data-source-integrations/oauth-clients", s.listDSOAuthClients)
	api.GET("/orgs/:orgId/data-source-integrations/oauth-clients/:providerType", s.getDSOAuthClient)
	api.PUT("/orgs/:orgId/data-source-integrations/oauth-clients/:providerType", s.upsertDSOAuthClient)
	api.DELETE("/orgs/:orgId/data-source-integrations/oauth-clients/:providerType", s.deleteDSOAuthClient)
	api.GET("/documents/:id/versions", s.listDocumentVersions)
	return s, e
}

func dsJSONReq(e *echo.Echo, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestDSRequireAPIKey(t *testing.T) {
	_, e := newDSAPIServer(&fakeMemory{}, "secret")

	rec := dsJSONReq(e, http.MethodGet, "/api/data-source-integrations", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 without key, got %d", rec.Code)
	}

	rec = dsJSONReq(e, http.MethodGet, "/api/data-source-integrations", "", map[string]string{"X-API-Key": "secret"})
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 with key, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestListDSIntegrationsHandler(t *testing.T) {
	f := &fakeMemory{dsIntegrations: []DSIntegration{{ID: "s1", Name: "Team Drive", ProviderType: "google_drive", Status: "active"}}}
	_, e := newDSAPIServer(f, "")
	rec := dsJSONReq(e, http.MethodGet, "/api/data-source-integrations", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got []DSIntegration
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "Team Drive" {
		t.Fatalf("got = %+v", got)
	}
}

func TestListDSIntegrationsHandlerEmptyArray(t *testing.T) {
	_, e := newDSAPIServer(&fakeMemory{}, "")
	rec := dsJSONReq(e, http.MethodGet, "/api/data-source-integrations", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "[]") {
		t.Errorf("want empty array, got %s", rec.Body.String())
	}
}

func TestCreateDSIntegrationHandler(t *testing.T) {
	f := &fakeMemory{}
	_, e := newDSAPIServer(f, "")
	body := `{"name":"Team Drive","providerType":"google_drive","sourceType":"drive","config":{"folder_id":"f1","refresh_token":"tok"}}`
	rec := dsJSONReq(e, http.MethodPost, "/api/data-source-integrations", body, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.dsCreatedReq == nil || f.dsCreatedReq.Config["folder_id"] != "f1" {
		t.Fatalf("created req = %+v", f.dsCreatedReq)
	}
	if !strings.Contains(rec.Body.String(), "ds-new") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

// TestCreateDSIntegrationForwardsProviderAndConfig pins that the gateway does
// not re-apply a Drive-only contract: provider validation belongs to memory, so
// a GitHub config reaches it intact.
func TestCreateDSIntegrationForwardsProviderAndConfig(t *testing.T) {
	f := &fakeMemory{}
	_, e := newDSAPIServer(f, "")
	body := `{"name":"Docs","providerType":"github","sourceType":"repo","config":{"repo":"acme/docs","access_token":"at"}}`
	rec := dsJSONReq(e, http.MethodPost, "/api/data-source-integrations", body, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.dsCreatedReq == nil || f.dsCreatedReq.ProviderType != "github" {
		t.Fatalf("created req = %+v, want the github provider forwarded", f.dsCreatedReq)
	}
	if f.dsCreatedReq.Config["repo"] != "acme/docs" || f.dsCreatedReq.Config["access_token"] != "at" {
		t.Errorf("config = %v, want the submitted config forwarded for server validation", f.dsCreatedReq.Config)
	}
}

// TestUpdateDSIntegrationForwardsConfig pins the same delegation on update: a
// non-Drive config must not be rejected for lacking folder_id.
func TestUpdateDSIntegrationForwardsConfig(t *testing.T) {
	f := &fakeMemory{dsIntegrations: []DSIntegration{{ID: "s1", ProviderType: "github", Status: "active"}}}
	_, e := newDSAPIServer(f, "")
	rec := dsJSONReq(e, http.MethodPatch, "/api/data-source-integrations/s1", `{"config":{"repo":"acme/docs","ref":"main"}}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.dsUpdatedReq == nil || f.dsUpdatedReq.Config["repo"] != "acme/docs" {
		t.Fatalf("updated req = %+v, want the github config forwarded", f.dsUpdatedReq)
	}
}

func TestCreateDSIntegrationHandlerMemoryError(t *testing.T) {
	f := &fakeMemory{dsCreateErr: errors.New("boom")}
	_, e := newDSAPIServer(f, "")
	body := `{"name":"x","providerType":"google_drive","config":{"folder_id":"f1","refresh_token":"tok"}}`
	rec := dsJSONReq(e, http.MethodPost, "/api/data-source-integrations", body, nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetDSIntegrationHandlerNotFound(t *testing.T) {
	_, e := newDSAPIServer(&fakeMemory{}, "")
	rec := dsJSONReq(e, http.MethodGet, "/api/data-source-integrations/missing", "", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

// TestTestDSConfigForwardsSelectedProvider pins that testing an unsaved config
// uses the provider the caller selected; memory owns the validation verdict.
func TestTestDSConfigForwardsSelectedProvider(t *testing.T) {
	f := &fakeMemory{}
	_, e := newDSAPIServer(f, "")
	rec := dsJSONReq(e, http.MethodPost, "/api/data-source-integrations/test-config", `{"providerType":"github","config":{"repo":"acme/docs","access_token":"at"}}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.dsLastTestConfig == nil || f.dsLastTestConfig.ProviderType != "github" {
		t.Fatalf("test config = %+v, want the github provider forwarded", f.dsLastTestConfig)
	}
}

func TestTriggerDSSyncHandler(t *testing.T) {
	f := &fakeMemory{}
	_, e := newDSAPIServer(f, "")
	rec := dsJSONReq(e, http.MethodPost, "/api/data-source-integrations/s1/sync", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.dsLastTriggerID != "s1" {
		t.Fatalf("trigger id = %q", f.dsLastTriggerID)
	}
	if !strings.Contains(rec.Body.String(), "job-1") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestListDSSyncJobsHandler(t *testing.T) {
	f := &fakeMemory{dsSyncJobs: map[string][]DSSyncJob{"s1": {{ID: "j1", Status: "processing", TotalItems: 10, ProcessedItems: 4}}}}
	_, e := newDSAPIServer(f, "")
	rec := dsJSONReq(e, http.MethodGet, "/api/data-source-integrations/s1/sync-jobs", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got DSListSyncJobsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Total != 1 || got.Items[0].ProcessedItems != 4 {
		t.Fatalf("got = %+v", got)
	}
}

func TestListDocumentVersionsHandler(t *testing.T) {
	f := &fakeMemory{documentVersions: []DocumentVersion{{ID: "v1", Version: 1, SourceStatus: "active"}}}
	_, e := newDSAPIServer(f, "")
	rec := dsJSONReq(e, http.MethodGet, "/api/documents/d1/versions", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"versions"`) || !strings.Contains(rec.Body.String(), `"v1"`) {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestListDocumentVersionsHandlerError(t *testing.T) {
	f := &fakeMemory{documentVersionsErr: errors.New("boom")}
	_, e := newDSAPIServer(f, "")
	rec := dsJSONReq(e, http.MethodGet, "/api/documents/d1/versions", "", nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", rec.Code, rec.Body.String())
	}
}

// --- OAuth connect + org OAuth-client config handlers (§4.2) ---

func TestStartDSOAuthHandler(t *testing.T) {
	f := &fakeMemory{}
	_, e := newDSAPIServer(f, "")
	rec := dsJSONReq(e, http.MethodPost, "/api/data-source-integrations/providers/google_drive/oauth/authorize", `{"config":{"name":"Team Drive"}}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.dsLastOAuthProvider != "google_drive" {
		t.Errorf("provider = %q", f.dsLastOAuthProvider)
	}
	if f.dsLastOAuthConfig["name"] != "Team Drive" {
		t.Errorf("config = %+v", f.dsLastOAuthConfig)
	}
	if !strings.Contains(rec.Body.String(), "authorizeUrl") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestStartDSOAuthHandlerMemoryError(t *testing.T) {
	f := &fakeMemory{dsOAuthAuthorizeErr: errors.New("no client configured")}
	_, e := newDSAPIServer(f, "")
	rec := dsJSONReq(e, http.MethodPost, "/api/data-source-integrations/providers/google_drive/oauth/authorize", "", nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpsertDSOAuthClientHandler(t *testing.T) {
	f := &fakeMemory{}
	_, e := newDSAPIServer(f, "")
	rec := dsJSONReq(e, http.MethodPut, "/api/orgs/org-1/data-source-integrations/oauth-clients/google_drive", `{"clientId":"cid","clientSecret":"secret","strategy":"drive.file"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.dsUpsertedOAuthOrg != "org-1" || f.dsUpsertedOAuthProv != "google_drive" {
		t.Errorf("upsert target = %q/%q", f.dsUpsertedOAuthOrg, f.dsUpsertedOAuthProv)
	}
	if f.dsUpsertedOAuthReq == nil || f.dsUpsertedOAuthReq.ClientID != "cid" {
		t.Fatalf("upsert req = %+v", f.dsUpsertedOAuthReq)
	}
	// The response never echoes the secret.
	if strings.Contains(rec.Body.String(), "secret") {
		t.Errorf("response leaks secret: %s", rec.Body.String())
	}
}

func TestUpsertDSOAuthClientHandlerValidation(t *testing.T) {
	f := &fakeMemory{}
	_, e := newDSAPIServer(f, "")
	rec := dsJSONReq(e, http.MethodPut, "/api/orgs/org-1/data-source-integrations/oauth-clients/google_drive", `{"clientSecret":"secret"}`, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if f.dsUpsertedOAuthReq != nil {
		t.Fatal("memory must not be called on validation failure")
	}
}

func TestListDSOAuthClientsHandlerEmptyArray(t *testing.T) {
	_, e := newDSAPIServer(&fakeMemory{}, "")
	rec := dsJSONReq(e, http.MethodGet, "/api/orgs/org-1/data-source-integrations/oauth-clients", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "[]") {
		t.Errorf("want empty array, got %s", rec.Body.String())
	}
}

func TestDeleteDSOAuthClientHandler(t *testing.T) {
	f := &fakeMemory{}
	_, e := newDSAPIServer(f, "")
	rec := dsJSONReq(e, http.MethodDelete, "/api/orgs/org-1/data-source-integrations/oauth-clients/google_drive", "", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.dsDeletedOAuthOrg != "org-1" || f.dsDeletedOAuthProv != "google_drive" {
		t.Errorf("delete target = %q/%q", f.dsDeletedOAuthOrg, f.dsDeletedOAuthProv)
	}
}
