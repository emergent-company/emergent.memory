package datasources_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/auth"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/datasources"
)

func newOAuthClient(t *testing.T, baseURL string) *datasources.Client {
	t.Helper()
	return datasources.NewClient(http.DefaultClient, baseURL, auth.NewAPIKeyProvider("test_key"), "org-123", "proj-123")
}

func TestStartOAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/data-source-integrations/providers/google_drive/oauth/authorize" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "test_key" {
			t.Errorf("expected X-API-Key test_key, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"authorizeUrl": "https://accounts.google.com/o/oauth2/auth"})
	}))
	defer srv.Close()

	client := newOAuthClient(t, srv.URL)

	resp, err := client.StartOAuth(context.Background(), "google_drive", &datasources.OAuthAuthorizeRequest{
		Scopes: []string{"https://www.googleapis.com/auth/drive.readonly"},
	})
	if err != nil {
		t.Fatalf("StartOAuth() error = %v", err)
	}
	if resp.AuthorizeURL != "https://accounts.google.com/o/oauth2/auth" {
		t.Errorf("unexpected authorizeUrl: %s", resp.AuthorizeURL)
	}
}

func TestListOrgOAuthClients(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/orgs/org-123/data-source-integrations/oauth-clients" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"providerType": "google_drive",
				"clientId":     "client-id-1",
				"scopes":       []string{"scope-1"},
				"strategy":     "default",
				"configured":   true,
				"createdAt":    "2025-01-01T00:00:00Z",
				"updatedAt":    "2025-01-02T00:00:00Z",
			},
		})
	}))
	defer srv.Close()

	client := newOAuthClient(t, srv.URL)

	configs, err := client.ListOrgOAuthClients(context.Background(), "org-123")
	if err != nil {
		t.Fatalf("ListOrgOAuthClients() error = %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(configs))
	}

	marshaled, err := json.Marshal(configs)
	if err != nil {
		t.Fatalf("marshal error = %v", err)
	}
	if strings.Contains(string(marshaled), "clientSecret") {
		t.Errorf("marshaled output should not contain clientSecret: %s", string(marshaled))
	}
}

func TestUpsertOrgOAuthClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if r.URL.Path != "/api/orgs/org-123/data-source-integrations/oauth-clients/google_drive" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var decoded map[string]interface{}
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if decoded["clientId"] != "client-id-1" {
			t.Errorf("expected clientId client-id-1, got %v", decoded["clientId"])
		}
		if decoded["clientSecret"] != "secret-1" {
			t.Errorf("expected clientSecret secret-1, got %v", decoded["clientSecret"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"providerType": "google_drive",
			"clientId":     "client-id-1",
			"scopes":       []string{"scope-1"},
			"strategy":     "default",
			"configured":   true,
			"createdAt":    "2025-01-01T00:00:00Z",
			"updatedAt":    "2025-01-02T00:00:00Z",
		})
	}))
	defer srv.Close()

	client := newOAuthClient(t, srv.URL)

	resp, err := client.UpsertOrgOAuthClient(context.Background(), "org-123", "google_drive", &datasources.SetOAuthClientRequest{
		ClientID:     "client-id-1",
		ClientSecret: "secret-1",
		Scopes:       []string{"scope-1"},
		Strategy:     "default",
	})
	if err != nil {
		t.Fatalf("UpsertOrgOAuthClient() error = %v", err)
	}
	if resp.ClientID != "client-id-1" {
		t.Errorf("expected clientId client-id-1, got %s", resp.ClientID)
	}
}

func TestDeleteOrgOAuthClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/api/orgs/org-123/data-source-integrations/oauth-clients/google_drive" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := newOAuthClient(t, srv.URL)

	if err := client.DeleteOrgOAuthClient(context.Background(), "org-123", "google_drive"); err != nil {
		t.Fatalf("DeleteOrgOAuthClient() error = %v", err)
	}
}

func TestStartOAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{"message": "forbidden"},
		})
	}))
	defer srv.Close()

	client := newOAuthClient(t, srv.URL)

	_, err := client.StartOAuth(context.Background(), "google_drive", nil)
	if err == nil {
		t.Fatal("expected error for 403 response, got nil")
	}
}

func TestOAuthCallbackURL(t *testing.T) {
	client := newOAuthClient(t, "http://unused")
	got := client.OAuthCallbackURL("https://api.example/", "google_drive")
	want := "https://api.example/api/data-source-integrations/oauth/google_drive/callback"
	if got != want {
		t.Errorf("OAuthCallbackURL() = %q, want %q", got, want)
	}
}
