package blueprints_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/emergent.memory/tools/cli/internal/blueprints"
)

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
			json.NewEncoder(w).Encode(sdkgraph.SearchObjectsResponse{Items: nil, Total: 0})

		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/objects/bulk"):
			// Bulk create — return one success.
			obj := &sdkgraph.GraphObject{EntityID: entityID, Type: "Document"}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(sdkgraph.BulkCreateObjectsResponse{
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
			obj := &sdkgraph.GraphObject{EntityID: entityID, Type: "Document"}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(sdkgraph.SearchObjectsResponse{
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

	var upsertCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/objects/search"):
			obj := &sdkgraph.GraphObject{EntityID: entityID, Type: "Document"}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(sdkgraph.SearchObjectsResponse{
				Items: []*sdkgraph.GraphObject{obj}, Total: 1,
			})

		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/objects/upsert"):
			upsertCalled = true
			obj := &sdkgraph.GraphObject{EntityID: entityID, Type: "Document"}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(obj)

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

	if !upsertCalled {
		t.Error("expected UpsertObject to be called")
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
			json.NewEncoder(w).Encode(sdkgraph.SearchObjectsResponse{Items: nil, Total: 0})
			return
		}
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/objects/bulk") {
			// Create returns empty result set (simulate no objects to create).
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(sdkgraph.BulkCreateObjectsResponse{})
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
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/relationships/bulk") {
			relCallCount++
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(sdkgraph.BulkCreateRelationshipsResponse{
				Success: 1,
				Results: []sdkgraph.BulkCreateRelationshipResult{
					{Index: 0, Success: true},
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
