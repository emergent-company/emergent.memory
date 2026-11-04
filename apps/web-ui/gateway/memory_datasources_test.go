package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- MemoryClient data-source + version methods ---

func TestListDSIntegrations(t *testing.T) {
	var gotPath, gotProject string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotProject = r.Header.Get("X-Project-ID")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"id":"s1","name":"Team Drive","providerType":"google_drive","sourceType":"drive","syncMode":"manual","status":"active","errorCount":0,"createdAt":"2026-09-01T00:00:00Z","updatedAt":"2026-09-01T00:00:00Z"}]`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	items, err := m.ListDSIntegrations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/data-source-integrations" {
		t.Errorf("path = %q", gotPath)
	}
	if gotProject != "proj-1" {
		t.Errorf("X-Project-ID = %q, want proj-1", gotProject)
	}
	if len(items) != 1 || items[0].Name != "Team Drive" || items[0].ProviderType != "google_drive" {
		t.Fatalf("items = %+v", items)
	}
}

func TestListDSResources(t *testing.T) {
	t.Run("query and truncated header", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.RequestURI()
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Resources-Truncated", "true")
			_, _ = io.WriteString(w, `[{"id":"f1","name":"Reports","type":"folder"}]`)
		}))
		defer srv.Close()

		m := NewMemoryClient(srv.URL, "proj-1")
		items, truncated, err := m.ListDSResources(context.Background(), "s1", "rep")
		if err != nil {
			t.Fatal(err)
		}
		if gotPath != "/api/data-source-integrations/s1/resources?q=rep" {
			t.Errorf("path = %q", gotPath)
		}
		if !truncated {
			t.Errorf("truncated = false, want true (header present)")
		}
		if len(items) != 1 || items[0].Name != "Reports" {
			t.Fatalf("items = %+v", items)
		}
	})

	t.Run("absent header means complete", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `[{"id":"f1","name":"Reports","type":"folder"}]`)
		}))
		defer srv.Close()

		m := NewMemoryClient(srv.URL, "proj-1")
		_, truncated, err := m.ListDSResources(context.Background(), "s1", "")
		if err != nil {
			t.Fatal(err)
		}
		if truncated {
			t.Errorf("truncated = true, want false (header absent)")
		}
	})
}

func TestGetDSIntegrationNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":"not_found","message":"source not found"}}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	_, err := m.GetDSIntegration(context.Background(), "missing")
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !isMemoryStatus(err, http.StatusNotFound) {
		t.Fatalf("want 404, got %v (status %d)", err, memoryStatus(err))
	}
}

func TestCreateDSIntegration(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody DSCreateIntegrationRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"s1","name":"Team Drive","providerType":"google_drive","status":"active","createdAt":"2026-09-01T00:00:00Z","updatedAt":"2026-09-01T00:00:00Z"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	item, err := m.CreateDSIntegration(context.Background(), &DSCreateIntegrationRequest{
		Name:         "Team Drive",
		ProviderType: "google_drive",
		SourceType:   "drive",
		Config:       map[string]any{"folder_id": "f1", "refresh_token": "tok"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/data-source-integrations" {
		t.Errorf("method=%q path=%q", gotMethod, gotPath)
	}
	if gotBody.Name != "Team Drive" || gotBody.Config["folder_id"] != "f1" {
		t.Errorf("body = %+v", gotBody)
	}
	if item.ID != "s1" {
		t.Errorf("item = %+v", item)
	}
}

func TestTestDSConfig(t *testing.T) {
	var gotPath string
	var gotBody DSTestConfigRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"message":"connection successful"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	resp, err := m.TestDSConfig(context.Background(), &DSTestConfigRequest{
		ProviderType: "google_drive",
		Config:       map[string]any{"folder_id": "f1", "refresh_token": "tok"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/data-source-integrations/test-config" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody.ProviderType != "google_drive" {
		t.Errorf("body = %+v", gotBody)
	}
	if !resp.Success {
		t.Errorf("resp = %+v", resp)
	}
}

func TestListDSSyncJobs(t *testing.T) {
	var gotPath, gotStatus, gotLimit, gotOffset string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotStatus = r.URL.Query().Get("status")
		gotLimit = r.URL.Query().Get("limit")
		gotOffset = r.URL.Query().Get("offset")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"items":[{"id":"j1","integrationId":"s1","status":"processing","totalItems":10,"processedItems":4,"successfulItems":3,"failedItems":1,"skippedItems":0,"triggerType":"manual","retryCount":0,"maxRetries":0,"createdAt":"2026-09-01T00:00:00Z"}],"total":1}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	resp, err := m.ListDSSyncJobs(context.Background(), "s1", "processing", 5, 10)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/data-source-integrations/s1/sync-jobs" || gotStatus != "processing" {
		t.Errorf("path=%q status=%q", gotPath, gotStatus)
	}
	if gotLimit != "5" || gotOffset != "10" {
		t.Errorf("limit=%q offset=%q, want paging forwarded", gotLimit, gotOffset)
	}
	if resp.Total != 1 || len(resp.Items) != 1 || resp.Items[0].ProcessedItems != 4 {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestGetLatestDSSyncJobNull(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `null`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	job, err := m.GetLatestDSSyncJob(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if job != nil {
		t.Fatalf("job = %+v, want nil", job)
	}
}

func TestListDocumentVersions(t *testing.T) {
	var gotPath, gotProject string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotProject = r.Header.Get("X-Project-ID")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"versions":[
			{"id":"v1","canonicalId":"c1","version":1,"sourceStatus":"active","sourceRevisionId":"r1","createdAt":"2026-09-01T00:00:00Z"},
			{"id":"v2","canonicalId":"c1","version":2,"sourceStatus":"deleted_from_source","createdAt":"2026-09-02T00:00:00Z"}
		]}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj-1")
	versions, err := m.ListDocumentVersions(context.Background(), "d1")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/documents/d1/versions" {
		t.Errorf("path = %q", gotPath)
	}
	if gotProject != "proj-1" {
		t.Errorf("X-Project-ID = %q", gotProject)
	}
	if len(versions) != 2 || versions[1].Version != 2 || versions[1].SourceStatus != "deleted_from_source" {
		t.Fatalf("versions = %+v", versions)
	}
	if versions[0].SourceRevisionID == nil || *versions[0].SourceRevisionID != "r1" {
		t.Errorf("v1 revision = %+v", versions[0].SourceRevisionID)
	}
}

// --- OAuth connect + org OAuth-client config (§4.2) ---

func TestStartDSOAuth(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"authorizeUrl":"https://accounts.example/a?state=s"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	resp, err := m.StartDSOAuth(context.Background(), "google_drive", map[string]any{"name": "Team Drive"}, []string{"scope-a"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodGet || gotPath != "/api/data-source-integrations/providers/google_drive/oauth/authorize" {
		t.Errorf("method=%q path=%q", gotMethod, gotPath)
	}
	if resp.AuthorizeURL != "https://accounts.example/a?state=s" {
		t.Errorf("authorizeUrl = %q", resp.AuthorizeURL)
	}
	cfg, _ := gotBody["config"].(map[string]any)
	if cfg["name"] != "Team Drive" {
		t.Errorf("body config = %+v", gotBody)
	}
	if _, present := gotBody["reconnectSourceId"]; present {
		t.Errorf("body carries reconnectSourceId %v for a connect flow", gotBody["reconnectSourceId"])
	}
}

// TestStartDSOAuth_ReconnectCarriesTheSource asserts a reconnect asks the server
// to re-mint the grant of a specific source (#2107) rather than opening another
// connect flow.
func TestStartDSOAuth_ReconnectCarriesTheSource(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"authorizeUrl":"https://accounts.example/a?state=s"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	if _, err := m.StartDSOAuth(context.Background(), "google_drive", nil, nil, "src-1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := gotBody["reconnectSourceId"].(string); got != "src-1" {
		t.Errorf("body reconnectSourceId = %q, want src-1 (body %+v)", got, gotBody)
	}
}

func TestListDSOAuthClients(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"providerType":"google_drive","clientId":"cid","configured":true,"strategy":"drive.file","createdAt":"2026-09-01T00:00:00Z","updatedAt":"2026-09-01T00:00:00Z"}]`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	items, err := m.ListDSOAuthClients(context.Background(), "org-1")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/orgs/org-1/data-source-integrations/oauth-clients" {
		t.Errorf("path = %q", gotPath)
	}
	if len(items) != 1 || items[0].ClientID != "cid" || !items[0].Configured {
		t.Fatalf("items = %+v", items)
	}
	// The DTO must never carry the client secret.
	b, _ := json.Marshal(items)
	if strings.Contains(string(b), "clientSecret") {
		t.Errorf("org OAuth client JSON leaks a secret field: %s", b)
	}
}

func TestUpsertDSOAuthClient(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody DSSetOAuthClientRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"providerType":"google_drive","clientId":"cid","configured":true,"createdAt":"2026-09-01T00:00:00Z","updatedAt":"2026-09-01T00:00:00Z"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	resp, err := m.UpsertDSOAuthClient(context.Background(), "org-1", "google_drive", &DSSetOAuthClientRequest{ClientID: "cid", ClientSecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/orgs/org-1/data-source-integrations/oauth-clients/google_drive" {
		t.Errorf("method=%q path=%q", gotMethod, gotPath)
	}
	if gotBody.ClientID != "cid" || gotBody.ClientSecret != "secret" {
		t.Errorf("body = %+v", gotBody)
	}
	if !resp.Configured || resp.ProviderType != "google_drive" {
		t.Errorf("resp = %+v", resp)
	}
}

func TestDeleteDSOAuthClient(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "proj")
	if err := m.DeleteDSOAuthClient(context.Background(), "org-1", "google_drive"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/orgs/org-1/data-source-integrations/oauth-clients/google_drive" {
		t.Errorf("method=%q path=%q", gotMethod, gotPath)
	}
}
