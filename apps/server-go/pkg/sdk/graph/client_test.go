package graph_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/graph"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/testutil"
)

func TestGraphListObjects(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/graph/objects/search", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"id":           "obj_123",
					"project_id":   "proj_1",
					"canonical_id": "can_123",
					"version":      1,
					"type":         "Person",
					"properties": map[string]interface{}{
						"name": "John Doe",
					},
					"labels":     []string{},
					"created_at": "2026-02-11T10:00:00Z",
				},
			},
			"total": 1,
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Graph.ListObjects(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListObjects() error = %v", err)
	}

	if len(result.Items) != 1 {
		t.Errorf("expected 1 object, got %d", len(result.Items))
	}

	if result.Items[0].ID != "obj_123" {
		t.Errorf("expected object ID obj_123, got %s", result.Items[0].ID)
	}

	if result.Total != 1 {
		t.Errorf("expected total 1, got %d", result.Total)
	}
}

func TestGraphListObjectsWithOptions(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/graph/objects/search", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")

		// Verify query parameters
		if got := r.URL.Query().Get("type"); got != "Person" {
			t.Errorf("expected type=Person, got %s", got)
		}
		if got := r.URL.Query().Get("limit"); got != "10" {
			t.Errorf("expected limit=10, got %s", got)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"items": []map[string]interface{}{},
			"total": 0,
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Graph.ListObjects(context.Background(), &graph.ListObjectsOptions{
		Type:  "Person",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListObjects() error = %v", err)
	}

	if result.Total != 0 {
		t.Errorf("expected total 0, got %d", result.Total)
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
			"id":           "obj_123",
			"project_id":   "proj_1",
			"canonical_id": "can_123",
			"version":      1,
			"type":         "Organization",
			"properties": map[string]interface{}{
				"name": "Acme Corp",
			},
			"labels":     []string{},
			"created_at": "2026-02-11T10:00:00Z",
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

	mock.On("GET", "/api/graph/relationships/search", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"id":           "rel_123",
					"project_id":   "proj_1",
					"canonical_id": "can_rel_123",
					"version":      1,
					"src_id":       "obj_123",
					"dst_id":       "obj_456",
					"type":         "WORKS_FOR",
					"properties":   map[string]interface{}{},
					"created_at":   "2026-02-11T10:00:00Z",
				},
			},
			"total": 1,
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Graph.ListRelationships(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListRelationships() error = %v", err)
	}

	if len(result.Items) != 1 {
		t.Errorf("expected 1 relationship, got %d", len(result.Items))
	}

	if result.Items[0].Type != "WORKS_FOR" {
		t.Errorf("expected type WORKS_FOR, got %s", result.Items[0].Type)
	}

	if result.Total != 1 {
		t.Errorf("expected total 1, got %d", result.Total)
	}
}

func TestGraphCreateObject(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("POST", "/api/graph/objects", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")
		testutil.AssertHeader(t, r, "Content-Type", "application/json")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"id":           "obj_new",
			"project_id":   "proj_1",
			"canonical_id": "can_new",
			"version":      1,
			"type":         "Person",
			"properties": map[string]interface{}{
				"name": "Jane Doe",
			},
			"labels":     []string{"engineer"},
			"created_at": "2026-02-11T10:00:00Z",
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Graph.CreateObject(context.Background(), &graph.CreateObjectRequest{
		Type:       "Person",
		Properties: map[string]any{"name": "Jane Doe"},
		Labels:     []string{"engineer"},
	})
	if err != nil {
		t.Fatalf("CreateObject() error = %v", err)
	}

	if result.ID != "obj_new" {
		t.Errorf("expected ID obj_new, got %s", result.ID)
	}
	if result.Type != "Person" {
		t.Errorf("expected type Person, got %s", result.Type)
	}
}

func TestGraphDeleteObject(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("DELETE", "/api/graph/objects/obj_123", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")
		w.WriteHeader(http.StatusNoContent)
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	err := client.Graph.DeleteObject(context.Background(), "obj_123")
	if err != nil {
		t.Fatalf("DeleteObject() error = %v", err)
	}
}

func TestGraphUpdateObject(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("PATCH", "/api/graph/objects/obj_123", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")
		testutil.AssertHeader(t, r, "Content-Type", "application/json")

		// Decode and verify request body
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		props, ok := body["properties"].(map[string]interface{})
		if !ok {
			t.Fatal("expected properties in request body")
		}
		if props["name"] != "Updated Name" {
			t.Errorf("expected property name=Updated Name, got %v", props["name"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"id":           "obj_v2",
			"project_id":   "proj_1",
			"canonical_id": "obj_123",
			"version":      2,
			"type":         "Person",
			"properties": map[string]interface{}{
				"name":  "Updated Name",
				"email": "existing@test.com",
			},
			"labels":     []string{"engineer"},
			"created_at": "2026-02-11T10:00:00Z",
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Graph.UpdateObject(context.Background(), "obj_123", &graph.UpdateObjectRequest{
		Properties: map[string]any{"name": "Updated Name"},
	})
	if err != nil {
		t.Fatalf("UpdateObject() error = %v", err)
	}

	if result.ID != "obj_v2" {
		t.Errorf("expected ID obj_v2, got %s", result.ID)
	}
	if result.CanonicalID != "obj_123" {
		t.Errorf("expected canonical_id obj_123, got %s", result.CanonicalID)
	}
	if result.Version != 2 {
		t.Errorf("expected version 2, got %d", result.Version)
	}
}

func TestGraphUpdateObjectReplaceLabels(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("PATCH", "/api/graph/objects/obj_123", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")

		// Verify that replaceLabels=false is present in the body (not omitted)
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		replaceLabels, exists := body["replaceLabels"]
		if !exists {
			t.Fatal("expected replaceLabels to be present in request body (omitempty bug)")
		}
		if replaceLabels != false {
			t.Errorf("expected replaceLabels=false, got %v", replaceLabels)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"id":           "obj_v2",
			"project_id":   "proj_1",
			"canonical_id": "obj_123",
			"version":      2,
			"type":         "Person",
			"properties":   map[string]interface{}{},
			"labels":       []string{"old", "new"},
			"created_at":   "2026-02-11T10:00:00Z",
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	replaceLabels := false
	result, err := client.Graph.UpdateObject(context.Background(), "obj_123", &graph.UpdateObjectRequest{
		Labels:        []string{"new"},
		ReplaceLabels: &replaceLabels,
	})
	if err != nil {
		t.Fatalf("UpdateObject() error = %v", err)
	}

	if len(result.Labels) != 2 {
		t.Errorf("expected 2 labels (merged), got %d", len(result.Labels))
	}
}

func TestGraphUpsertObject(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	callCount := 0
	mock.On("PUT", "/api/graph/objects/upsert", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")
		testutil.AssertHeader(t, r, "Content-Type", "application/json")
		callCount++

		w.Header().Set("Content-Type", "application/json")
		if callCount == 1 {
			// First call: create
			w.WriteHeader(http.StatusCreated)
			testutil.JSONResponse(t, w, map[string]interface{}{
				"id":           "obj_upsert_1",
				"project_id":   "proj_1",
				"canonical_id": "obj_upsert_1",
				"version":      1,
				"type":         "Config",
				"key":          "app_settings",
				"properties": map[string]interface{}{
					"theme": "dark",
				},
				"labels":     []string{},
				"created_at": "2026-02-11T10:00:00Z",
			})
		} else {
			// Second call: update
			w.WriteHeader(http.StatusOK)
			testutil.JSONResponse(t, w, map[string]interface{}{
				"id":           "obj_upsert_v2",
				"project_id":   "proj_1",
				"canonical_id": "obj_upsert_1",
				"version":      2,
				"type":         "Config",
				"key":          "app_settings",
				"properties": map[string]interface{}{
					"theme": "light",
				},
				"labels":     []string{},
				"created_at": "2026-02-11T10:00:00Z",
			})
		}
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	appSettingsKey := "app_settings"

	// First call: create path
	result1, err := client.Graph.UpsertObject(context.Background(), &graph.CreateObjectRequest{
		Type:       "Config",
		Key:        &appSettingsKey,
		Properties: map[string]any{"theme": "dark"},
	})
	if err != nil {
		t.Fatalf("UpsertObject() create error = %v", err)
	}
	if result1.Version != 1 {
		t.Errorf("expected version 1 on create, got %d", result1.Version)
	}

	// Second call: update path
	result2, err := client.Graph.UpsertObject(context.Background(), &graph.CreateObjectRequest{
		Type:       "Config",
		Key:        &appSettingsKey,
		Properties: map[string]any{"theme": "light"},
	})
	if err != nil {
		t.Fatalf("UpsertObject() update error = %v", err)
	}
	if result2.Version != 2 {
		t.Errorf("expected version 2 on update, got %d", result2.Version)
	}
	if result2.CanonicalID != "obj_upsert_1" {
		t.Errorf("expected canonical_id to remain obj_upsert_1, got %s", result2.CanonicalID)
	}
}
