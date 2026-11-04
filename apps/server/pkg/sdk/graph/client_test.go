package graph_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/testutil"
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
	if result.Items[0].VersionID != "obj_123" {
		t.Errorf("expected VersionID obj_123 (cross-populated), got %s", result.Items[0].VersionID)
	}
	if result.Items[0].CanonicalID != "can_123" {
		t.Errorf("expected CanonicalID can_123, got %s", result.Items[0].CanonicalID)
	}
	if result.Items[0].EntityID != "can_123" {
		t.Errorf("expected EntityID can_123 (cross-populated), got %s", result.Items[0].EntityID)
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

// TestGraphListObjectsSkipTotal pins the SDK wiring for the #733 opt-out:
// SkipTotal must send include_total=false, and the server's omitted `total`
// must decode as the documented 0 without error.
func TestGraphListObjectsSkipTotal(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/graph/objects/search", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("include_total"); got != "false" {
			t.Errorf("expected include_total=false, got %q", got)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// The server omits `total` entirely when include_total=false.
		testutil.JSONResponse(t, w, map[string]interface{}{
			"items": []map[string]interface{}{},
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Graph.ListObjects(context.Background(), &graph.ListObjectsOptions{
		SkipTotal: true,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListObjects() error = %v", err)
	}

	if result.Total != 0 {
		t.Errorf("expected total 0 when the server omits it, got %d", result.Total)
	}
}

// TestGraphListObjectsDefaultOmitsIncludeTotal proves the default request does
// not send include_total, so existing callers keep the exact-total behaviour.
func TestGraphListObjectsDefaultOmitsIncludeTotal(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/graph/objects/search", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.URL.Query()["include_total"]; ok {
			t.Errorf("expected no include_total param by default, got %v", r.URL.Query()["include_total"])
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

	if _, err := client.Graph.ListObjects(context.Background(), &graph.ListObjectsOptions{Limit: 5}); err != nil {
		t.Fatalf("ListObjects() error = %v", err)
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
	if result.VersionID != "obj_123" {
		t.Errorf("expected VersionID obj_123 (cross-populated), got %s", result.VersionID)
	}
	if result.EntityID != "can_123" {
		t.Errorf("expected EntityID can_123 (cross-populated), got %s", result.EntityID)
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
	if result.Items[0].VersionID != "rel_123" {
		t.Errorf("expected VersionID rel_123 (cross-populated), got %s", result.Items[0].VersionID)
	}
	if result.Items[0].EntityID != "can_rel_123" {
		t.Errorf("expected EntityID can_rel_123 (cross-populated), got %s", result.Items[0].EntityID)
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
	if result.VersionID != "obj_new" {
		t.Errorf("expected VersionID obj_new (cross-populated), got %s", result.VersionID)
	}
	if result.EntityID != "can_new" {
		t.Errorf("expected EntityID can_new (cross-populated), got %s", result.EntityID)
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

	err := client.Graph.DeleteObject(context.Background(), "obj_123", nil)
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
	if result.VersionID != "obj_v2" {
		t.Errorf("expected VersionID obj_v2 (cross-populated), got %s", result.VersionID)
	}
	if result.CanonicalID != "obj_123" {
		t.Errorf("expected canonical_id obj_123, got %s", result.CanonicalID)
	}
	if result.EntityID != "obj_123" {
		t.Errorf("expected EntityID obj_123 (cross-populated), got %s", result.EntityID)
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
	if result2.VersionID != "obj_upsert_v2" {
		t.Errorf("expected VersionID obj_upsert_v2 (cross-populated), got %s", result2.VersionID)
	}
	if result2.EntityID != "obj_upsert_1" {
		t.Errorf("expected EntityID obj_upsert_1 (cross-populated), got %s", result2.EntityID)
	}
}

// ---------------------------------------------------------------------------
// GetByAnyID tests
// ---------------------------------------------------------------------------

func TestGraphGetByAnyID_VersionID(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	// Simulate server resolving a version-specific ID
	mock.On("GET", "/api/graph/objects/obj_v2", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"id":           "obj_v2",
			"project_id":   "proj_1",
			"canonical_id": "can_123",
			"version":      2,
			"type":         "Person",
			"properties":   map[string]interface{}{"name": "Jane"},
			"labels":       []string{},
			"created_at":   "2026-02-11T10:00:00Z",
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Graph.GetByAnyID(context.Background(), "obj_v2")
	if err != nil {
		t.Fatalf("GetByAnyID() error = %v", err)
	}

	if result.ID != "obj_v2" {
		t.Errorf("expected ID obj_v2, got %s", result.ID)
	}
	if result.CanonicalID != "can_123" {
		t.Errorf("expected canonical_id can_123, got %s", result.CanonicalID)
	}
}

func TestGraphGetByAnyID_CanonicalID(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	// Simulate server resolving a canonical (entity) ID to the latest version
	mock.On("GET", "/api/graph/objects/can_123", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"id":           "obj_v3",
			"project_id":   "proj_1",
			"canonical_id": "can_123",
			"version":      3,
			"type":         "Person",
			"properties":   map[string]interface{}{"name": "Jane Updated"},
			"labels":       []string{},
			"created_at":   "2026-02-11T10:00:00Z",
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Graph.GetByAnyID(context.Background(), "can_123")
	if err != nil {
		t.Fatalf("GetByAnyID() error = %v", err)
	}

	// Server should return the latest version for the canonical ID
	if result.ID != "obj_v3" {
		t.Errorf("expected ID obj_v3, got %s", result.ID)
	}
	if result.CanonicalID != "can_123" {
		t.Errorf("expected canonical_id can_123, got %s", result.CanonicalID)
	}
	if result.Version != 3 {
		t.Errorf("expected version 3, got %d", result.Version)
	}
}

func TestGraphGetByAnyID_NotFound(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/graph/objects/nonexistent_id", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"error": "object not found",
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	_, err := client.Graph.GetByAnyID(context.Background(), "nonexistent_id")
	if err == nil {
		t.Fatal("expected error for nonexistent ID, got nil")
	}
}

func TestGraphGetByAnyID_StaleVersionID(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	// A stale version ID should still resolve (server returns the object at that version)
	mock.On("GET", "/api/graph/objects/obj_v1_stale", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"id":            "obj_v2_current",
			"project_id":    "proj_1",
			"canonical_id":  "can_456",
			"supersedes_id": "obj_v1_stale",
			"version":       2,
			"type":          "Document",
			"properties":    map[string]interface{}{"title": "Updated Doc"},
			"labels":        []string{},
			"created_at":    "2026-02-11T10:00:00Z",
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Graph.GetByAnyID(context.Background(), "obj_v1_stale")
	if err != nil {
		t.Fatalf("GetByAnyID() error = %v", err)
	}

	// Server redirected to current version
	if result.ID != "obj_v2_current" {
		t.Errorf("expected ID obj_v2_current, got %s", result.ID)
	}
	if result.CanonicalID != "can_456" {
		t.Errorf("expected canonical_id can_456, got %s", result.CanonicalID)
	}
}

// ---------------------------------------------------------------------------
// HasRelationship tests
// ---------------------------------------------------------------------------

func TestGraphHasRelationship_Found(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/graph/relationships/search", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")

		// Verify correct query parameters
		q := r.URL.Query()
		if got := q.Get("type"); got != "WORKS_FOR" {
			t.Errorf("expected type=WORKS_FOR, got %s", got)
		}
		if got := q.Get("src_id"); got != "can_person" {
			t.Errorf("expected src_id=can_person, got %s", got)
		}
		if got := q.Get("dst_id"); got != "can_org" {
			t.Errorf("expected dst_id=can_org, got %s", got)
		}
		if got := q.Get("limit"); got != "1" {
			t.Errorf("expected limit=1, got %s", got)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"id":           "rel_1",
					"project_id":   "proj_1",
					"canonical_id": "can_rel_1",
					"version":      1,
					"type":         "WORKS_FOR",
					"src_id":       "can_person",
					"dst_id":       "can_org",
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

	found, err := client.Graph.HasRelationship(context.Background(), "WORKS_FOR", "can_person", "can_org")
	if err != nil {
		t.Fatalf("HasRelationship() error = %v", err)
	}

	if !found {
		t.Error("expected HasRelationship to return true, got false")
	}
}

func TestGraphHasRelationship_NotFound(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/graph/relationships/search", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")

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

	found, err := client.Graph.HasRelationship(context.Background(), "MANAGES", "can_person_1", "can_person_2")
	if err != nil {
		t.Fatalf("HasRelationship() error = %v", err)
	}

	if found {
		t.Error("expected HasRelationship to return false, got true")
	}
}

func TestGraphHasRelationship_WithVersionIDs(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/graph/relationships/search", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")

		// Verify version-specific IDs are passed through
		q := r.URL.Query()
		if got := q.Get("src_id"); got != "obj_v1" {
			t.Errorf("expected src_id=obj_v1, got %s", got)
		}
		if got := q.Get("dst_id"); got != "obj_v2" {
			t.Errorf("expected dst_id=obj_v2, got %s", got)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"id":           "rel_2",
					"project_id":   "proj_1",
					"canonical_id": "can_rel_2",
					"version":      1,
					"type":         "REFERENCES",
					"src_id":       "can_a",
					"dst_id":       "can_b",
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

	found, err := client.Graph.HasRelationship(context.Background(), "REFERENCES", "obj_v1", "obj_v2")
	if err != nil {
		t.Fatalf("HasRelationship() error = %v", err)
	}

	if !found {
		t.Error("expected HasRelationship to return true, got false")
	}
}

func TestGraphHasRelationship_MixedIDs(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/graph/relationships/search", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")

		// Mixed: canonical src, version-specific dst
		q := r.URL.Query()
		if got := q.Get("src_id"); got != "can_person" {
			t.Errorf("expected src_id=can_person, got %s", got)
		}
		if got := q.Get("dst_id"); got != "obj_org_v3" {
			t.Errorf("expected dst_id=obj_org_v3, got %s", got)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"id":           "rel_3",
					"project_id":   "proj_1",
					"canonical_id": "can_rel_3",
					"version":      1,
					"type":         "BELONGS_TO",
					"src_id":       "can_person",
					"dst_id":       "can_org",
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

	found, err := client.Graph.HasRelationship(context.Background(), "BELONGS_TO", "can_person", "obj_org_v3")
	if err != nil {
		t.Fatalf("HasRelationship() error = %v", err)
	}

	if !found {
		t.Error("expected HasRelationship to return true, got false")
	}
}

// ---------------------------------------------------------------------------
// UnmarshalJSON cross-population tests (task 5.5)
// ---------------------------------------------------------------------------

func TestGraphObject_UnmarshalJSON_OldFieldsCrossPopulateNew(t *testing.T) {
	// Server sends only old field names (id, canonical_id)
	data := []byte(`{
		"id": "obj_v1",
		"project_id": "proj_1",
		"canonical_id": "can_123",
		"version": 1,
		"type": "Person",
		"properties": {},
		"labels": [],
		"created_at": "2026-02-11T10:00:00Z"
	}`)

	var obj graph.GraphObject
	if err := json.Unmarshal(data, &obj); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// Old fields populated
	if obj.ID != "obj_v1" {
		t.Errorf("ID = %q, want obj_v1", obj.ID)
	}
	if obj.CanonicalID != "can_123" {
		t.Errorf("CanonicalID = %q, want can_123", obj.CanonicalID)
	}
	// New fields cross-populated
	if obj.VersionID != "obj_v1" {
		t.Errorf("VersionID = %q, want obj_v1 (cross-populated from id)", obj.VersionID)
	}
	if obj.EntityID != "can_123" {
		t.Errorf("EntityID = %q, want can_123 (cross-populated from canonical_id)", obj.EntityID)
	}
}

func TestGraphObject_UnmarshalJSON_NewFieldsCrossPopulateOld(t *testing.T) {
	// Future server sends only new field names (version_id, entity_id)
	data := []byte(`{
		"version_id": "obj_v2",
		"project_id": "proj_1",
		"entity_id": "can_456",
		"version": 2,
		"type": "Document",
		"properties": {},
		"labels": [],
		"created_at": "2026-02-11T10:00:00Z"
	}`)

	var obj graph.GraphObject
	if err := json.Unmarshal(data, &obj); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// New fields populated
	if obj.VersionID != "obj_v2" {
		t.Errorf("VersionID = %q, want obj_v2", obj.VersionID)
	}
	if obj.EntityID != "can_456" {
		t.Errorf("EntityID = %q, want can_456", obj.EntityID)
	}
	// Old fields cross-populated
	if obj.ID != "obj_v2" {
		t.Errorf("ID = %q, want obj_v2 (cross-populated from version_id)", obj.ID)
	}
	if obj.CanonicalID != "can_456" {
		t.Errorf("CanonicalID = %q, want can_456 (cross-populated from entity_id)", obj.CanonicalID)
	}
}

func TestGraphObject_UnmarshalJSON_BothFieldSets(t *testing.T) {
	// Server sends both old and new (current behavior with MarshalJSON)
	data := []byte(`{
		"id": "obj_v3",
		"version_id": "obj_v3",
		"project_id": "proj_1",
		"canonical_id": "can_789",
		"entity_id": "can_789",
		"version": 3,
		"type": "Config",
		"properties": {},
		"labels": [],
		"created_at": "2026-02-11T10:00:00Z"
	}`)

	var obj graph.GraphObject
	if err := json.Unmarshal(data, &obj); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if obj.ID != "obj_v3" || obj.VersionID != "obj_v3" {
		t.Errorf("ID=%q VersionID=%q, both should be obj_v3", obj.ID, obj.VersionID)
	}
	if obj.CanonicalID != "can_789" || obj.EntityID != "can_789" {
		t.Errorf("CanonicalID=%q EntityID=%q, both should be can_789", obj.CanonicalID, obj.EntityID)
	}
}

func TestGraphRelationship_UnmarshalJSON_CrossPopulation(t *testing.T) {
	// Server sends old field names
	data := []byte(`{
		"id": "rel_v1",
		"project_id": "proj_1",
		"canonical_id": "can_rel_1",
		"version": 1,
		"type": "WORKS_FOR",
		"src_id": "src_1",
		"dst_id": "dst_1",
		"properties": {},
		"created_at": "2026-02-11T10:00:00Z"
	}`)

	var rel graph.GraphRelationship
	if err := json.Unmarshal(data, &rel); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if rel.ID != "rel_v1" {
		t.Errorf("ID = %q, want rel_v1", rel.ID)
	}
	if rel.VersionID != "rel_v1" {
		t.Errorf("VersionID = %q, want rel_v1 (cross-populated)", rel.VersionID)
	}
	if rel.CanonicalID != "can_rel_1" {
		t.Errorf("CanonicalID = %q, want can_rel_1", rel.CanonicalID)
	}
	if rel.EntityID != "can_rel_1" {
		t.Errorf("EntityID = %q, want can_rel_1 (cross-populated)", rel.EntityID)
	}
}

func TestGraphObject_UnmarshalJSON_ViaSDKClient(t *testing.T) {
	// End-to-end: verify that objects deserialized via the SDK client have all four fields
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/graph/objects/obj_e2e", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"id":           "obj_v5",
			"project_id":   "proj_1",
			"canonical_id": "can_e2e",
			"version_id":   "obj_v5",
			"entity_id":    "can_e2e",
			"version":      5,
			"type":         "Person",
			"properties":   map[string]interface{}{},
			"labels":       []string{},
			"created_at":   "2026-02-11T10:00:00Z",
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	result, err := client.Graph.GetObject(context.Background(), "obj_e2e")
	if err != nil {
		t.Fatalf("GetObject() error = %v", err)
	}

	// All four fields populated
	if result.ID != "obj_v5" {
		t.Errorf("ID = %q, want obj_v5", result.ID)
	}
	if result.VersionID != "obj_v5" {
		t.Errorf("VersionID = %q, want obj_v5", result.VersionID)
	}
	if result.CanonicalID != "can_e2e" {
		t.Errorf("CanonicalID = %q, want can_e2e", result.CanonicalID)
	}
	if result.EntityID != "can_e2e" {
		t.Errorf("EntityID = %q, want can_e2e", result.EntityID)
	}
}

// TestGraphListObjectsKeys pins the SDK wiring for the `keys` query param:
// Keys must serialize as a comma-separated `keys` list.
func TestGraphListObjectsKeys(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/graph/objects/search", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")
		if got := r.URL.Query().Get("keys"); got != "a,b" {
			t.Errorf("expected keys=a,b, got %q", got)
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

	_, err := client.Graph.ListObjects(context.Background(), &graph.ListObjectsOptions{
		Keys: []string{"a", "b"},
	})
	if err != nil {
		t.Fatalf("ListObjects() error = %v", err)
	}
}

// TestGraphBulkUpsertObjects pins the SDK wiring for the bulk object upsert
// endpoint: it must POST to /api/graph/objects/bulk-upsert and decode `created`.
func TestGraphBulkUpsertObjects(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("POST", "/api/graph/objects/bulk-upsert", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")
		testutil.AssertHeader(t, r, "Content-Type", "application/json")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"success": 2,
			"failed":  0,
			"results": []map[string]interface{}{
				{
					"index":   0,
					"success": true,
					"created": true,
					"object": map[string]interface{}{
						"id":           "obj_new",
						"project_id":   "proj_1",
						"canonical_id": "can_new",
						"version":      1,
						"type":         "Config",
						"properties":   map[string]interface{}{},
						"labels":       []string{},
						"created_at":   "2026-02-11T10:00:00Z",
					},
				},
				{
					"index":   1,
					"success": true,
					"created": false,
					"object": map[string]interface{}{
						"id":           "obj_upd",
						"project_id":   "proj_1",
						"canonical_id": "can_upd",
						"version":      2,
						"type":         "Config",
						"properties":   map[string]interface{}{},
						"labels":       []string{},
						"created_at":   "2026-02-11T10:00:00Z",
					},
				},
			},
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	keyA := "a"
	keyB := "b"
	resp, err := client.Graph.BulkUpsertObjects(context.Background(), &graph.BulkUpsertObjectsRequest{
		Items: []graph.CreateObjectRequest{
			{Type: "Config", Key: &keyA},
			{Type: "Config", Key: &keyB},
		},
	})
	if err != nil {
		t.Fatalf("BulkUpsertObjects() error = %v", err)
	}

	if resp.Success != 2 || resp.Failed != 0 {
		t.Errorf("expected success=2 failed=0, got success=%d failed=%d", resp.Success, resp.Failed)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(resp.Results))
	}
	if !resp.Results[0].Created {
		t.Errorf("expected result[0].Created=true, got %v", resp.Results[0].Created)
	}
	if resp.Results[1].Created {
		t.Errorf("expected result[1].Created=false, got %v", resp.Results[1].Created)
	}
	if resp.Results[0].Object == nil || resp.Results[0].Object.EntityID != "can_new" {
		t.Errorf("expected result[0] object EntityID can_new, got %+v", resp.Results[0].Object)
	}
}

// TestGraphBulkUpsertRelationships pins the SDK wiring for the bulk relationship
// upsert endpoint: it must POST to /api/graph/relationships/bulk-upsert and
// decode `created`.
func TestGraphBulkUpsertRelationships(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("POST", "/api/graph/relationships/bulk-upsert", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")
		testutil.AssertHeader(t, r, "Content-Type", "application/json")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		testutil.JSONResponse(t, w, map[string]interface{}{
			"success": 1,
			"failed":  0,
			"results": []map[string]interface{}{
				{
					"index":   0,
					"success": true,
					"created": true,
					"relationship": map[string]interface{}{
						"id":           "rel_new",
						"project_id":   "proj_1",
						"canonical_id": "can_rel_new",
						"version":      1,
						"type":         "REFERENCES",
						"src_id":       "can_a",
						"dst_id":       "can_b",
						"properties":   map[string]interface{}{},
						"created_at":   "2026-02-11T10:00:00Z",
					},
				},
			},
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})

	resp, err := client.Graph.BulkUpsertRelationships(context.Background(), &graph.BulkUpsertRelationshipsRequest{
		Items: []graph.CreateRelationshipRequest{
			{Type: "REFERENCES", SrcID: "can_a", DstID: "can_b"},
		},
	})
	if err != nil {
		t.Fatalf("BulkUpsertRelationships() error = %v", err)
	}

	if resp.Success != 1 || resp.Failed != 0 {
		t.Errorf("expected success=1 failed=0, got success=%d failed=%d", resp.Success, resp.Failed)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}
	if !resp.Results[0].Created {
		t.Errorf("expected result[0].Created=true, got %v", resp.Results[0].Created)
	}
	if resp.Results[0].Relationship == nil || resp.Results[0].Relationship.EntityID != "can_rel_new" {
		t.Errorf("expected relationship EntityID can_rel_new, got %+v", resp.Results[0].Relationship)
	}
}

// TestGraphListObjectsActorProvenance pins the SDK wiring for the actor
// provenance filter (issue #1193): ActorType/ActorID/Provenance must serialize
// as actor_type/actor_id/provenance query params.
func TestGraphListObjectsActorProvenance(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	mock.On("GET", "/api/graph/objects/search", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertHeader(t, r, "X-API-Key", "test_key")
		if got := r.URL.Query().Get("actor_type"); got != "agent" {
			t.Errorf("expected actor_type=agent, got %q", got)
		}
		if got := r.URL.Query().Get("actor_id"); got != "11111111-1111-1111-1111-111111111111" {
			t.Errorf("expected actor_id=11111111-1111-1111-1111-111111111111, got %q", got)
		}
		if got := r.URL.Query().Get("provenance"); got != "created" {
			t.Errorf("expected provenance=created, got %q", got)
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

	_, err := client.Graph.ListObjects(context.Background(), &graph.ListObjectsOptions{
		ActorType:  "agent",
		ActorID:    "11111111-1111-1111-1111-111111111111",
		Provenance: "created",
	})
	if err != nil {
		t.Fatalf("ListObjects() error = %v", err)
	}
}

// TestGraphWriteStampFilters verifies the write-stamp filters (#2468) are
// encoded on the list/count endpoints and that the stamp fields decode from the
// responses — the surface the CLI depends on to answer "what did this run
// write".
func TestGraphWriteStampFilters(t *testing.T) {
	mock := testutil.NewMockServer(t)
	defer mock.Close()

	const (
		runID  = "11111111-1111-1111-1111-111111111111"
		credID = "22222222-2222-2222-2222-222222222222"
	)

	assertQuery := func(t *testing.T, r *http.Request, want map[string]string) {
		t.Helper()
		q := r.URL.Query()
		for key, value := range want {
			if got := q.Get(key); got != value {
				t.Errorf("query %s = %q, want %q", key, got, value)
			}
		}
	}

	objectItem := map[string]interface{}{
		"id":              "obj_1",
		"project_id":      "proj_1",
		"canonical_id":    "can_1",
		"entity_id":       "can_1",
		"version":         1,
		"type":            "Thing",
		"properties":      map[string]interface{}{},
		"labels":          []string{},
		"created_at":      "2026-02-11T10:00:00Z",
		"credential_kind": "api_key",
		"credential_id":   credID,
		"run_id":          runID,
		"root_run_id":     runID,
		"data_class":      "source",
		"owner_source_id": "33333333-3333-3333-3333-333333333333",
		"actor_type":      "agent",
		"actor_id":        "44444444-4444-4444-4444-444444444444",
		"input_hash":      "sha256:6f1e",
	}

	mock.On("GET", "/api/graph/objects/search", func(w http.ResponseWriter, r *http.Request) {
		assertQuery(t, r, map[string]string{
			"run_id":          runID,
			"credential_id":   credID,
			"credential_kind": "api_key",
			"data_class":      "source",
			"provenance":      "created",
		})
		testutil.JSONResponse(t, w, map[string]interface{}{
			"items": []interface{}{objectItem},
			"total": 1,
		})
	})
	mock.On("GET", "/api/graph/objects/count", func(w http.ResponseWriter, r *http.Request) {
		assertQuery(t, r, map[string]string{"run_id": runID, "credential_kind": "none", "provenance": "any"})
		testutil.JSONResponse(t, w, map[string]interface{}{"count": 0})
	})
	mock.On("GET", "/api/graph/relationships/search", func(w http.ResponseWriter, r *http.Request) {
		assertQuery(t, r, map[string]string{
			"run_id":          runID,
			"credential_id":   credID,
			"credential_kind": "mcp_link",
			"provenance":      "updated",
		})
		if got := r.URL.Query().Get("data_class"); got != "" {
			t.Errorf("relationship list must not send data_class, got %q", got)
		}
		testutil.JSONResponse(t, w, map[string]interface{}{
			"items": []interface{}{map[string]interface{}{
				"id":              "rel_1",
				"project_id":      "proj_1",
				"canonical_id":    "relcan_1",
				"entity_id":       "relcan_1",
				"version":         1,
				"type":            "LINKS",
				"src_id":          "obj_1",
				"dst_id":          "obj_2",
				"properties":      map[string]interface{}{},
				"created_at":      "2026-02-11T10:00:00Z",
				"actor_type":      "agent",
				"credential_kind": "mcp_link",
				"credential_id":   credID,
				"run_id":          runID,
				"root_run_id":     runID,
			}},
			"total": 1,
		})
	})

	client, _ := sdk.New(sdk.Config{
		ServerURL: mock.URL,
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: "test_key"},
	})
	ctx := context.Background()

	objects, err := client.Graph.ListObjects(ctx, &graph.ListObjectsOptions{
		RunID:          runID,
		CredentialID:   credID,
		CredentialKind: "api_key",
		DataClass:      "source",
		Provenance:     "created",
	})
	if err != nil {
		t.Fatalf("ListObjects() error = %v", err)
	}
	if len(objects.Items) != 1 {
		t.Fatalf("expected 1 object, got %d", len(objects.Items))
	}
	obj := objects.Items[0]
	if obj.CredentialKind != "api_key" {
		t.Errorf("expected credential_kind api_key, got %q", obj.CredentialKind)
	}
	if obj.CredentialID == nil || *obj.CredentialID != credID {
		t.Errorf("expected credential_id %s, got %v", credID, obj.CredentialID)
	}
	if obj.RunID == nil || *obj.RunID != runID {
		t.Errorf("expected run_id %s, got %v", runID, obj.RunID)
	}
	if obj.RootRunID == nil || *obj.RootRunID != runID {
		t.Errorf("expected root_run_id %s, got %v", runID, obj.RootRunID)
	}
	if obj.DataClass != "source" {
		t.Errorf("expected data_class source, got %q", obj.DataClass)
	}
	if obj.OwnerSourceID == nil || *obj.OwnerSourceID != "33333333-3333-3333-3333-333333333333" {
		t.Errorf("expected owner_source_id to decode, got %v", obj.OwnerSourceID)
	}
	if obj.ActorType == nil || *obj.ActorType != "agent" {
		t.Errorf("expected actor_type agent, got %v", obj.ActorType)
	}
	if obj.ActorID == nil || *obj.ActorID != "44444444-4444-4444-4444-444444444444" {
		t.Errorf("expected actor_id to decode, got %v", obj.ActorID)
	}
	if obj.InputHash == nil || *obj.InputHash != "sha256:6f1e" {
		t.Errorf("expected input_hash to decode, got %v", obj.InputHash)
	}

	count, err := client.Graph.CountObjects(ctx, &graph.CountObjectsOptions{RunID: runID, CredentialKind: "none", Provenance: "any"})
	if err != nil {
		t.Fatalf("CountObjects() error = %v", err)
	}
	if count != 0 {
		t.Errorf("expected count 0, got %d", count)
	}

	rels, err := client.Graph.ListRelationships(ctx, &graph.ListRelationshipsOptions{
		RunID:          runID,
		CredentialID:   credID,
		CredentialKind: "mcp_link",
		Provenance:     "updated",
	})
	if err != nil {
		t.Fatalf("ListRelationships() error = %v", err)
	}
	if len(rels.Items) != 1 {
		t.Fatalf("expected 1 relationship, got %d", len(rels.Items))
	}
	rel := rels.Items[0]
	if rel.CredentialKind != "mcp_link" {
		t.Errorf("expected relationship credential_kind mcp_link, got %q", rel.CredentialKind)
	}
	if rel.CredentialID == nil || *rel.CredentialID != credID {
		t.Errorf("expected relationship credential_id %s, got %v", credID, rel.CredentialID)
	}
	if rel.RunID == nil || *rel.RunID != runID {
		t.Errorf("expected relationship run_id %s, got %v", runID, rel.RunID)
	}
	if rel.ActorType == nil || *rel.ActorType != "agent" {
		t.Errorf("expected relationship actor_type agent, got %v", rel.ActorType)
	}
}
