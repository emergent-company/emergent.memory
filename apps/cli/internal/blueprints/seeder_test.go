package blueprints_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/blueprints"
	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

// strPtr returns a pointer to s for building *string fields in test fixtures.
func strPtr(s string) *string { return &s }

// noopAuth satisfies the auth.Provider interface without any credentials.
// It is used to construct graph.Client instances in unit tests against a
// local httptest.Server.
type noopAuth struct{}

func (noopAuth) Authenticate(r *http.Request) error { return nil }
func (noopAuth) Refresh(ctx context.Context) error  { return nil }

// newTestGraphClient creates a graph.Client wired to the given test server URL.
func newTestGraphClient(serverURL string) *sdkgraph.Client {
	return sdkgraph.NewClient(http.DefaultClient, serverURL, noopAuth{}, "", "")
}

// ──────────────────────────────────────────────────────────────────────────────
// Seeder — dry-run
// ──────────────────────────────────────────────────────────────────────────────

func TestSeeder_DryRun_Objects(t *testing.T) {
	objects := []blueprints.SeedObjectRecord{
		{Type: "Document", Key: "doc-1"},
		{Type: "Document"},
	}

	var buf bytes.Buffer
	s := blueprints.NewSeeder(nil /* nil is safe in dry-run */, true, false, &buf)

	result, err := s.Run(context.Background(), objects, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "[dry-run]") {
		t.Errorf("expected [dry-run] in output, got:\n%s", out)
	}
	// Both objects should be counted as "created" in dry-run (no existing check).
	if result.ObjectsCreated != 2 {
		t.Errorf("expected ObjectsCreated=2, got %d", result.ObjectsCreated)
	}
	if result.ObjectsFailed != 0 {
		t.Errorf("expected ObjectsFailed=0, got %d", result.ObjectsFailed)
	}
}

func TestSeeder_DryRun_Relationships(t *testing.T) {
	rels := []blueprints.SeedRelationshipRecord{
		{Type: "Mentions", SrcKey: "doc-1", DstKey: "doc-2"},
	}
	// Provide keyMap entries through a prior object phase by seeding objects first.
	objects := []blueprints.SeedObjectRecord{
		{Type: "Document", Key: "doc-1"},
		{Type: "Document", Key: "doc-2"},
	}

	var buf bytes.Buffer
	s := blueprints.NewSeeder(nil, true, false, &buf)

	result, err := s.Run(context.Background(), objects, rels)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "[dry-run]") {
		t.Errorf("expected [dry-run] in output, got:\n%s", out)
	}
	if result.RelsCreated != 1 {
		t.Errorf("expected RelsCreated=1, got %d", result.RelsCreated)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Seeder — create path (server returns success)
// ──────────────────────────────────────────────────────────────────────────────

func TestSeeder_Create_NewObject(t *testing.T) {
	const entityID = "eid-001"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/objects/search"):
			// Key lookup — return empty (no existing object).
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.SearchObjectsResponse{Items: nil, Total: 0})

		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/objects/bulk"):
			// Bulk create — return one success.
			obj := &sdkgraph.GraphObject{EntityID: entityID, Type: "Document"}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.BulkCreateObjectsResponse{
				Success: 1,
				Results: []sdkgraph.BulkCreateObjectResult{
					{Index: 0, Success: true, Object: obj},
				},
			})

		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := newTestGraphClient(srv.URL)
	var buf bytes.Buffer
	s := blueprints.NewSeeder(client, false, false, &buf)

	objects := []blueprints.SeedObjectRecord{{Type: "Document", Key: "doc-1"}}
	result, err := s.Run(context.Background(), objects, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ObjectsCreated != 1 {
		t.Errorf("expected ObjectsCreated=1, got %d", result.ObjectsCreated)
	}
	if result.ObjectsFailed != 0 {
		t.Errorf("expected ObjectsFailed=0, got %d", result.ObjectsFailed)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Seeder — skip path (existing key, no --upgrade)
// ──────────────────────────────────────────────────────────────────────────────

func TestSeeder_Skip_ExistingKey_NoUpgrade(t *testing.T) {
	const entityID = "eid-existing"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/objects/search") {
			key := "existing-key"
			obj := &sdkgraph.GraphObject{EntityID: entityID, Type: "Document", Key: &key}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.SearchObjectsResponse{
				Items: []*sdkgraph.GraphObject{obj}, Total: 1,
			})
			return
		}
		// Any other call (bulk create) would be an error in this scenario.
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := newTestGraphClient(srv.URL)
	var buf bytes.Buffer
	s := blueprints.NewSeeder(client, false, false /* no upgrade */, &buf)

	objects := []blueprints.SeedObjectRecord{{Type: "Document", Key: "existing-key"}}
	result, err := s.Run(context.Background(), objects, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ObjectsSkipped != 1 {
		t.Errorf("expected ObjectsSkipped=1, got %d", result.ObjectsSkipped)
	}
	if result.ObjectsCreated != 0 {
		t.Errorf("expected ObjectsCreated=0, got %d", result.ObjectsCreated)
	}

	out := buf.String()
	if !strings.Contains(out, "skip") {
		t.Errorf("expected 'skip' in output, got:\n%s", out)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Seeder — update path (existing key + --upgrade)
// ──────────────────────────────────────────────────────────────────────────────

func TestSeeder_Update_ExistingKey_WithUpgrade(t *testing.T) {
	const entityID = "eid-existing"

	var bulkUpsertCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/objects/search"):
			key := "existing-key"
			obj := &sdkgraph.GraphObject{EntityID: entityID, Type: "Document", Key: &key}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.SearchObjectsResponse{
				Items: []*sdkgraph.GraphObject{obj}, Total: 1,
			})

		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/objects/bulk-upsert"):
			bulkUpsertCalled = true
			obj := &sdkgraph.GraphObject{EntityID: entityID, Type: "Document"}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.BulkUpsertObjectsResponse{
				Success: 1,
				Results: []sdkgraph.BulkUpsertObjectResult{
					{Index: 0, Success: true, Created: false, Object: obj},
				},
			})

		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := newTestGraphClient(srv.URL)
	var buf bytes.Buffer
	s := blueprints.NewSeeder(client, false, true /* upgrade */, &buf)

	objects := []blueprints.SeedObjectRecord{{Type: "Document", Key: "existing-key"}}
	result, err := s.Run(context.Background(), objects, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !bulkUpsertCalled {
		t.Error("expected BulkUpsertObjects to be called")
	}
	if result.ObjectsUpdated != 1 {
		t.Errorf("expected ObjectsUpdated=1, got %d", result.ObjectsUpdated)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Seeder — unresolvable key error
// ──────────────────────────────────────────────────────────────────────────────

func TestSeeder_UnresolvableKey_RelationshipFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No objects — empty search results.
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/objects/search") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.SearchObjectsResponse{Items: nil, Total: 0})
			return
		}
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/objects/bulk") {
			// Create returns empty result set (simulate no objects to create).
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.BulkCreateObjectsResponse{})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := newTestGraphClient(srv.URL)
	var buf bytes.Buffer
	s := blueprints.NewSeeder(client, false, false, &buf)

	// Relationship references a key that doesn't exist in seed objects.
	rels := []blueprints.SeedRelationshipRecord{
		{Type: "Mentions", SrcKey: "missing-src", DstKey: "missing-dst"},
	}
	result, err := s.Run(context.Background(), nil, rels)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.RelsFailed != 1 {
		t.Errorf("expected RelsFailed=1 for unresolvable key, got %d", result.RelsFailed)
	}

	out := buf.String()
	if !strings.Contains(out, "error") {
		t.Errorf("expected error message in output, got:\n%s", out)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Seeder — relationship dedup (server returns success on duplicate)
// ──────────────────────────────────────────────────────────────────────────────

func TestSeeder_RelationshipDedup_ServerSucceeds(t *testing.T) {
	// Server always returns success for relationships (dedup is server-side).
	relCallCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/relationships/bulk-upsert") {
			relCallCount++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.BulkUpsertRelationshipsResponse{
				Success: 1,
				Results: []sdkgraph.BulkUpsertRelationshipResult{
					{Index: 0, Success: true, Created: true},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := newTestGraphClient(srv.URL)
	var buf bytes.Buffer
	s := blueprints.NewSeeder(client, false, false, &buf)

	// Use raw srcId/dstId so no key lookup is needed.
	rels := []blueprints.SeedRelationshipRecord{
		{Type: "Mentions", SrcID: "eid-a", DstID: "eid-b"},
	}
	result, err := s.Run(context.Background(), nil, rels)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.RelsCreated != 1 {
		t.Errorf("expected RelsCreated=1, got %d", result.RelsCreated)
	}
	if relCallCount != 1 {
		t.Errorf("expected 1 bulk relationship call, got %d", relCallCount)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Seeder — assignee forwarding (board-enabled work lane)
// ──────────────────────────────────────────────────────────────────────────────

func TestSeeder_ForwardsAssigneeOnCreate(t *testing.T) {
	const entityID = "eid-001"

	var createItems []sdkgraph.CreateObjectRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/objects/search"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.SearchObjectsResponse{Items: nil, Total: 0})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/objects/bulk"):
			var req sdkgraph.BulkCreateObjectsRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode bulk create: %v", err)
			}
			createItems = req.Items
			obj := &sdkgraph.GraphObject{EntityID: entityID, Type: "Task"}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.BulkCreateObjectsResponse{
				Success: 1,
				Results: []sdkgraph.BulkCreateObjectResult{{Index: 0, Success: true, Object: obj}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := newTestGraphClient(srv.URL)
	var buf bytes.Buffer
	s := blueprints.NewSeeder(client, false, false, &buf)

	objects := []blueprints.SeedObjectRecord{{Type: "Task", Key: "k1", Assignee: "agent-x"}}
	if _, err := s.Run(context.Background(), objects, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(createItems) != 1 {
		t.Fatalf("expected 1 create item, got %d", len(createItems))
	}
	if createItems[0].Assignee == nil || *createItems[0].Assignee != "agent-x" {
		t.Fatalf("expected assignee agent-x forwarded, got %v", createItems[0].Assignee)
	}
}

func TestSeeder_ForwardsAssigneeOnUpsert(t *testing.T) {
	const entityID = "eid-existing"

	var upsertBody sdkgraph.BulkUpsertObjectsRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/objects/search"):
			key := "existing-key"
			obj := &sdkgraph.GraphObject{EntityID: entityID, Type: "Task", Key: &key}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.SearchObjectsResponse{
				Items: []*sdkgraph.GraphObject{obj}, Total: 1,
			})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/objects/bulk-upsert"):
			if err := json.NewDecoder(r.Body).Decode(&upsertBody); err != nil {
				t.Fatalf("decode bulk-upsert: %v", err)
			}
			obj := &sdkgraph.GraphObject{EntityID: entityID, Type: "Task"}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.BulkUpsertObjectsResponse{
				Success: 1,
				Results: []sdkgraph.BulkUpsertObjectResult{
					{Index: 0, Success: true, Created: false, Object: obj},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := newTestGraphClient(srv.URL)
	var buf bytes.Buffer
	s := blueprints.NewSeeder(client, false, true /* upgrade */, &buf)

	objects := []blueprints.SeedObjectRecord{{Type: "Task", Key: "existing-key", Assignee: "reviewer"}}
	if _, err := s.Run(context.Background(), objects, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(upsertBody.Items) != 1 {
		t.Fatalf("expected 1 upsert item, got %d", len(upsertBody.Items))
	}
	if upsertBody.Items[0].Assignee == nil || *upsertBody.Items[0].Assignee != "reviewer" {
		t.Fatalf("expected assignee reviewer forwarded on upsert, got %v", upsertBody.Items[0].Assignee)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Seeder — batching (multi-key lookup + bulk upsert)
// ──────────────────────────────────────────────────────────────────────────────

func TestSeeder_MultiKeyLookup_SingleSearchCall(t *testing.T) {
	searchCallCount := 0
	var keysParam string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/objects/search") {
			searchCallCount++
			keysParam = r.URL.Query().Get("keys")
			items := []*sdkgraph.GraphObject{
				{EntityID: "eid-a", Type: "Document", Key: strPtr("k1")},
				{EntityID: "eid-b", Type: "Document", Key: strPtr("k2")},
				{EntityID: "eid-c", Type: "Document", Key: strPtr("k3")},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.SearchObjectsResponse{Items: items, Total: 3})
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := newTestGraphClient(srv.URL)
	var buf bytes.Buffer
	s := blueprints.NewSeeder(client, false, false /* no upgrade */, &buf)

	objects := []blueprints.SeedObjectRecord{
		{Type: "Document", Key: "k1"},
		{Type: "Document", Key: "k2"},
		{Type: "Document", Key: "k3"},
	}
	result, err := s.Run(context.Background(), objects, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if searchCallCount != 1 {
		t.Errorf("expected exactly 1 search call, got %d", searchCallCount)
	}
	if keysParam != "k1,k2,k3" {
		t.Errorf("expected keys=k1,k2,k3, got %q", keysParam)
	}
	if result.ObjectsSkipped != 3 {
		t.Errorf("expected ObjectsSkipped=3, got %d", result.ObjectsSkipped)
	}
}

func TestSeeder_BulkUpsertBatching_SingleCall(t *testing.T) {
	upsertCallCount := 0
	var upsertItems []sdkgraph.CreateObjectRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/objects/search"):
			items := []*sdkgraph.GraphObject{
				{EntityID: "eid-a", Type: "Document", Key: strPtr("k1")},
				{EntityID: "eid-b", Type: "Document", Key: strPtr("k2")},
				{EntityID: "eid-c", Type: "Document", Key: strPtr("k3")},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.SearchObjectsResponse{Items: items, Total: 3})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/objects/bulk-upsert"):
			upsertCallCount++
			var req sdkgraph.BulkUpsertObjectsRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode bulk-upsert: %v", err)
			}
			upsertItems = req.Items
			results := make([]sdkgraph.BulkUpsertObjectResult, len(req.Items))
			for i := range req.Items {
				results[i] = sdkgraph.BulkUpsertObjectResult{
					Index:   i,
					Success: true,
					Created: false,
					Object:  &sdkgraph.GraphObject{EntityID: fmt.Sprintf("eid-%d", i)},
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.BulkUpsertObjectsResponse{
				Success: len(req.Items),
				Results: results,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := newTestGraphClient(srv.URL)
	var buf bytes.Buffer
	s := blueprints.NewSeeder(client, false, true /* upgrade */, &buf)

	objects := []blueprints.SeedObjectRecord{
		{Type: "Document", Key: "k1"},
		{Type: "Document", Key: "k2"},
		{Type: "Document", Key: "k3"},
	}
	result, err := s.Run(context.Background(), objects, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if upsertCallCount != 1 {
		t.Errorf("expected exactly 1 bulk-upsert call, got %d", upsertCallCount)
	}
	if len(upsertItems) != 3 {
		t.Errorf("expected 3 items in bulk-upsert, got %d", len(upsertItems))
	}
	if result.ObjectsUpdated != 3 {
		t.Errorf("expected ObjectsUpdated=3, got %d", result.ObjectsUpdated)
	}
}

func TestSeeder_BulkUpsert_PerItemIsolation(t *testing.T) {
	const errMsg = "boom"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/objects/search"):
			items := []*sdkgraph.GraphObject{
				{EntityID: "eid-a", Type: "Document", Key: strPtr("k1")},
				{EntityID: "eid-b", Type: "Document", Key: strPtr("k2")},
				{EntityID: "eid-c", Type: "Document", Key: strPtr("k3")},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.SearchObjectsResponse{Items: items, Total: 3})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/objects/bulk-upsert"):
			failed := "boom"
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.BulkUpsertObjectsResponse{
				Success: 2,
				Failed:  1,
				Results: []sdkgraph.BulkUpsertObjectResult{
					{Index: 0, Success: true, Created: false, Object: &sdkgraph.GraphObject{EntityID: "eid-a"}},
					{Index: 1, Success: false, Created: false, Error: &failed},
					{Index: 2, Success: true, Created: false, Object: &sdkgraph.GraphObject{EntityID: "eid-c"}},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := newTestGraphClient(srv.URL)
	var buf bytes.Buffer
	s := blueprints.NewSeeder(client, false, true /* upgrade */, &buf)

	objects := []blueprints.SeedObjectRecord{
		{Type: "Document", Key: "k1"},
		{Type: "Document", Key: "k2"},
		{Type: "Document", Key: "k3"},
	}
	result, err := s.Run(context.Background(), objects, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ObjectsUpdated != 2 {
		t.Errorf("expected ObjectsUpdated=2, got %d", result.ObjectsUpdated)
	}
	if result.ObjectsFailed != 1 {
		t.Errorf("expected ObjectsFailed=1, got %d", result.ObjectsFailed)
	}

	out := buf.String()
	if !strings.Contains(out, errMsg) {
		t.Errorf("expected error line to surface %q, got:\n%s", errMsg, out)
	}
	if !strings.Contains(out, "upsert object") {
		t.Errorf("expected per-item upsert error line, got:\n%s", out)
	}
}

func TestSeeder_RelationshipUpsertEndpoint(t *testing.T) {
	var relPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/relationships/bulk-upsert") {
			relPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.BulkUpsertRelationshipsResponse{
				Success: 1,
				Results: []sdkgraph.BulkUpsertRelationshipResult{
					{Index: 0, Success: true, Created: true},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := newTestGraphClient(srv.URL)
	var buf bytes.Buffer
	s := blueprints.NewSeeder(client, false, false, &buf)

	rels := []blueprints.SeedRelationshipRecord{
		{Type: "Mentions", SrcID: "eid-a", DstID: "eid-b"},
	}
	result, err := s.Run(context.Background(), nil, rels)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if relPath != "/api/graph/relationships/bulk-upsert" {
		t.Errorf("expected relationship upsert path /api/graph/relationships/bulk-upsert, got %q", relPath)
	}
	if result.RelsCreated != 1 {
		t.Errorf("expected RelsCreated=1, got %d", result.RelsCreated)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Seeder — non-round-trip keys (comma / whitespace) use exact single-key lookup
// ──────────────────────────────────────────────────────────────────────────────

func TestSeeder_NonRoundTripKeys_SkipWhenExisting(t *testing.T) {
	var exactLookups []string
	var bulkCreateCalled, bulkUpsertCalled bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/objects/search"):
			// The seeder must use the exact `key` param (not `keys`), and must
			// never send a comma-joined `keys` param for these keys.
			if got := r.URL.Query().Get("keys"); got != "" {
				t.Errorf("expected no `keys` param for non-round-trip keys, got %q", got)
				http.Error(w, "unexpected keys param", http.StatusBadRequest)
				return
			}
			key := r.URL.Query().Get("key")
			if key == "" {
				t.Errorf("expected exact `key` param, got none")
				http.Error(w, "missing key", http.StatusBadRequest)
				return
			}
			exactLookups = append(exactLookups, key)
			obj := &sdkgraph.GraphObject{EntityID: "eid-" + key, Type: "Document", Key: strPtr(key)}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.SearchObjectsResponse{
				Items: []*sdkgraph.GraphObject{obj}, Total: 1,
			})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/objects/bulk"):
			bulkCreateCalled = true
			http.Error(w, "unexpected bulk create", http.StatusInternalServerError)
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/objects/bulk-upsert"):
			bulkUpsertCalled = true
			http.Error(w, "unexpected bulk upsert", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := newTestGraphClient(srv.URL)
	var buf bytes.Buffer
	s := blueprints.NewSeeder(client, false, false /* no upgrade */, &buf)

	objects := []blueprints.SeedObjectRecord{
		{Type: "Document", Key: "part,one"},
		{Type: "Document", Key: " a "},
	}
	result, err := s.Run(context.Background(), objects, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ObjectsSkipped != 2 {
		t.Errorf("expected ObjectsSkipped=2, got %d", result.ObjectsSkipped)
	}
	if result.ObjectsCreated != 0 {
		t.Errorf("expected ObjectsCreated=0, got %d", result.ObjectsCreated)
	}
	if bulkCreateCalled {
		t.Error("bulk create should not be called for existing non-round-trip keys")
	}
	if bulkUpsertCalled {
		t.Error("bulk upsert should not be called without --upgrade")
	}

	// Each key must be looked up literally, never split/trimmed.
	if len(exactLookups) != 2 {
		t.Fatalf("expected 2 exact single-key lookups, got %d (%v)", len(exactLookups), exactLookups)
	}
	seen := map[string]bool{}
	for _, k := range exactLookups {
		seen[k] = true
	}
	if !seen["part,one"] {
		t.Errorf("expected literal lookup of %q, got %v", "part,one", exactLookups)
	}
	if !seen[" a "] {
		t.Errorf("expected literal lookup of %q, got %v", " a ", exactLookups)
	}
}

func TestSeeder_NonRoundTripKeys_UpsertWhenExisting(t *testing.T) {
	var exactLookups []string
	var upsertKeys []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/objects/search"):
			if got := r.URL.Query().Get("keys"); got != "" {
				t.Errorf("expected no `keys` param for non-round-trip keys, got %q", got)
				http.Error(w, "unexpected keys param", http.StatusBadRequest)
				return
			}
			key := r.URL.Query().Get("key")
			if key == "" {
				t.Errorf("expected exact `key` param, got none")
				http.Error(w, "missing key", http.StatusBadRequest)
				return
			}
			exactLookups = append(exactLookups, key)
			obj := &sdkgraph.GraphObject{EntityID: "eid-" + key, Type: "Document", Key: strPtr(key)}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.SearchObjectsResponse{
				Items: []*sdkgraph.GraphObject{obj}, Total: 1,
			})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/objects/bulk-upsert"):
			var req sdkgraph.BulkUpsertObjectsRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode bulk-upsert: %v", err)
			}
			results := make([]sdkgraph.BulkUpsertObjectResult, len(req.Items))
			for i, item := range req.Items {
				upsertKeys = append(upsertKeys, *item.Key)
				results[i] = sdkgraph.BulkUpsertObjectResult{
					Index: i, Success: true, Created: false,
					Object: &sdkgraph.GraphObject{EntityID: fmt.Sprintf("eid-%d", i)},
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.BulkUpsertObjectsResponse{
				Success: len(req.Items), Results: results,
			})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/objects/bulk"):
			t.Errorf("unexpected bulk create")
			http.Error(w, "unexpected", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := newTestGraphClient(srv.URL)
	var buf bytes.Buffer
	s := blueprints.NewSeeder(client, false, true /* upgrade */, &buf)

	objects := []blueprints.SeedObjectRecord{
		{Type: "Document", Key: "part,one"},
		{Type: "Document", Key: " a "},
	}
	result, err := s.Run(context.Background(), objects, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ObjectsUpdated != 2 {
		t.Errorf("expected ObjectsUpdated=2, got %d", result.ObjectsUpdated)
	}
	if result.ObjectsCreated != 0 {
		t.Errorf("expected ObjectsCreated=0, got %d", result.ObjectsCreated)
	}
	if len(exactLookups) != 2 {
		t.Fatalf("expected 2 exact single-key lookups, got %d (%v)", len(exactLookups), exactLookups)
	}
	// The upsert request must carry the literal keys, unsplit/untrimmed.
	if len(upsertKeys) != 2 {
		t.Fatalf("expected 2 upsert items, got %d (%v)", len(upsertKeys), upsertKeys)
	}
	seen := map[string]bool{}
	for _, k := range upsertKeys {
		seen[k] = true
	}
	if !seen["part,one"] || !seen[" a "] {
		t.Errorf("expected literal upsert keys [\"part,one\", \" a \"], got %v", upsertKeys)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Seeder — batched key lookup follows NextCursor across pages
// ──────────────────────────────────────────────────────────────────────────────

func TestSeeder_BatchedLookup_FollowsNextCursor(t *testing.T) {
	searchCallCount := 0
	var secondPageCursor string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/objects/search") {
			searchCallCount++
			w.Header().Set("Content-Type", "application/json")
			cursor := r.URL.Query().Get("cursor")
			if cursor == "" {
				// First page: only k1/k2, with a next cursor.
				items := []*sdkgraph.GraphObject{
					{EntityID: "eid-a", Type: "Document", Key: strPtr("k1")},
					{EntityID: "eid-b", Type: "Document", Key: strPtr("k2")},
				}
				next := "page2"
				_ = json.NewEncoder(w).Encode(sdkgraph.SearchObjectsResponse{Items: items, Total: 4, NextCursor: &next})
			} else {
				// Second page: k3/k4, no further cursor.
				secondPageCursor = cursor
				items := []*sdkgraph.GraphObject{
					{EntityID: "eid-c", Type: "Document", Key: strPtr("k3")},
					{EntityID: "eid-d", Type: "Document", Key: strPtr("k4")},
				}
				_ = json.NewEncoder(w).Encode(sdkgraph.SearchObjectsResponse{Items: items, Total: 4})
			}
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := newTestGraphClient(srv.URL)
	var buf bytes.Buffer
	s := blueprints.NewSeeder(client, false, false /* no upgrade */, &buf)

	objects := []blueprints.SeedObjectRecord{
		{Type: "Document", Key: "k1"},
		{Type: "Document", Key: "k2"},
		{Type: "Document", Key: "k3"},
		{Type: "Document", Key: "k4"},
	}
	result, err := s.Run(context.Background(), objects, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if searchCallCount != 2 {
		t.Errorf("expected 2 search calls (page 1 + follow cursor), got %d", searchCallCount)
	}
	if secondPageCursor != "page2" {
		t.Errorf("expected second page to pass cursor=page2, got %q", secondPageCursor)
	}
	if result.ObjectsSkipped != 4 {
		t.Errorf("expected ObjectsSkipped=4 (all keys resolved across pages), got %d", result.ObjectsSkipped)
	}
	if result.ObjectsCreated != 0 {
		t.Errorf("expected ObjectsCreated=0, got %d", result.ObjectsCreated)
	}
}
