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

func TestCreateDSIntegrationHandlerValidation(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"missing folder_id", `{"name":"x","providerType":"google_drive","config":{"refresh_token":"tok"}}`},
		{"missing refresh_token", `{"name":"x","providerType":"google_drive","config":{"folder_id":"f1"}}`},
		{"wrong provider", `{"name":"x","providerType":"dropbox","config":{"folder_id":"f1","refresh_token":"tok"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeMemory{}
			_, e := newDSAPIServer(f, "")
			rec := dsJSONReq(e, http.MethodPost, "/api/data-source-integrations", tc.body, nil)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			if f.dsCreatedReq != nil {
				t.Fatalf("memory must not be called on validation failure: %+v", f.dsCreatedReq)
			}
		})
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

func TestTestDSConfigHandlerValidation(t *testing.T) {
	f := &fakeMemory{}
	_, e := newDSAPIServer(f, "")
	rec := dsJSONReq(e, http.MethodPost, "/api/data-source-integrations/test-config", `{"providerType":"google_drive","config":{}}`, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if f.dsLastTestConfig != nil {
		t.Fatalf("memory must not be called on validation failure")
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
