package graph_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/testutil"
)

func TestGraphListObjects(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/graph/objects", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"id":   "obj_123",
					"type": "Person",
					"properties": map[string]interface{}{
						"name": "John Doe",
					},
					"created_at": "2026-02-11T10:00:00Z",
					"updated_at": "2026-02-11T10:00:00Z",
				},
			},
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Graph.ListObjects(context.Background())
	if err != nil {
		t.Fatalf("ListObjects() error = %v", err)
	}

	if len(result) != 1 {
		t.Errorf("expected 1 object, got %d", len(result))
	}

	if result[0].ID != "obj_123" {
		t.Errorf("expected object ID obj_123, got %s", result[0].ID)
	}
}

func TestGraphGetObject(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/graph/objects/obj_123", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"data": map[string]interface{}{
				"id":   "obj_123",
				"type": "Organization",
				"properties": map[string]interface{}{
					"name": "Acme Corp",
				},
				"created_at": "2026-02-11T10:00:00Z",
				"updated_at": "2026-02-11T10:00:00Z",
			},
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Graph.GetObject(context.Background(), "obj_123")
	if err != nil {
		t.Fatalf("GetObject() error = %v", err)
	}

	if result.Type != "Organization" {
		t.Errorf("expected type Organization, got %s", result.Type)
	}
}

func TestGraphListRelationships(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/graph/relationships", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"id":         "rel_123",
					"source_id":  "obj_123",
					"target_id":  "obj_456",
					"type":       "WORKS_FOR",
					"created_at": "2026-02-11T10:00:00Z",
				},
			},
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Graph.ListRelationships(context.Background())
	if err != nil {
		t.Fatalf("ListRelationships() error = %v", err)
	}

	if len(result) != 1 {
		t.Errorf("expected 1 relationship, got %d", len(result))
	}

	if result[0].Type != "WORKS_FOR" {
		t.Errorf("expected type WORKS_FOR, got %s", result[0].Type)
	}
}
