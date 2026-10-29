package datasources_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/datasources"
)

func TestSavedCredentialEndpoints(t *testing.T) {
	var sawListQuery, sawCreate, sawUpdate, sawDelete bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/data-source-integrations/credentials/saved":
			sawListQuery = r.URL.Query().Get("providerType") == "github"
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{{
					"id":           "cred-1",
					"providerType": "github",
					"name":         "ops PAT",
					"usedByCount":  2,
					"createdAt":    "2025-01-01T00:00:00Z",
					"updatedAt":    "2025-01-02T00:00:00Z",
				}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/data-source-integrations/credentials/saved":
			sawCreate = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "cred-1", "providerType": "github", "name": "ops PAT",
				"createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-01-01T00:00:00Z",
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/data-source-integrations/credentials/saved/cred-1":
			sawUpdate = true
			body, _ := io.ReadAll(r.Body)
			var payload map[string]any
			_ = json.Unmarshal(body, &payload)
			if payload["name"] != "renamed" {
				t.Errorf("update name = %v, want renamed", payload["name"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "cred-1", "providerType": "github", "name": "renamed",
				"createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-01-02T00:00:00Z",
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/data-source-integrations/credentials/saved/cred-1":
			sawDelete = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := newOAuthClient(t, srv.URL)
	ctx := context.Background()

	items, err := client.ListSavedCredentials(ctx, "github")
	if err != nil {
		t.Fatalf("ListSavedCredentials() error = %v", err)
	}
	if !sawListQuery || len(items) != 1 || items[0].UsedByCount != 2 {
		t.Fatalf("ListSavedCredentials() = %+v (query seen %v)", items, sawListQuery)
	}

	created, err := client.CreateSavedCredential(ctx, &datasources.CreateSavedCredentialRequest{
		ProviderType: "github",
		Name:         "ops PAT",
		Credentials:  map[string]interface{}{"access_token": "token"},
	})
	if err != nil {
		t.Fatalf("CreateSavedCredential() error = %v", err)
	}
	if !sawCreate || created.ID != "cred-1" {
		t.Fatalf("CreateSavedCredential() = %+v (create seen %v)", created, sawCreate)
	}

	name := "renamed"
	updated, err := client.UpdateSavedCredential(ctx, "cred-1", &datasources.UpdateSavedCredentialRequest{Name: &name})
	if err != nil {
		t.Fatalf("UpdateSavedCredential() error = %v", err)
	}
	if !sawUpdate || updated.Name != "renamed" {
		t.Fatalf("UpdateSavedCredential() = %+v (update seen %v)", updated, sawUpdate)
	}

	if err := client.DeleteSavedCredential(ctx, "cred-1"); err != nil {
		t.Fatalf("DeleteSavedCredential() error = %v", err)
	}
	if !sawDelete {
		t.Error("delete endpoint was not called")
	}
}
