package blueprints_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/emergent.memory/tools/cli/internal/blueprints"
)

// ──────────────────────────────────────────────────────────────────────────────
// Dumper tests
// ──────────────────────────────────────────────────────────────────────────────

func TestDumper_OutputFilesCreated(t *testing.T) {
	key := "doc-1"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/objects/search"):
			obj := &sdkgraph.GraphObject{
				EntityID: "eid-001", Type: "Document", Key: &key,
				Properties: map[string]any{"title": "Hello"},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.SearchObjectsResponse{
				Items: []*sdkgraph.GraphObject{obj}, Total: 1,
			})

		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/relationships/search"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.SearchRelationshipsResponse{Items: nil, Total: 0})

		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := newTestGraphClient(srv.URL)
	outputDir := t.TempDir()

	d := blueprints.NewDumper(client, nil, nil)
	result, err := d.Run(context.Background(), outputDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ObjectsDumped != 1 {
		t.Errorf("expected ObjectsDumped=1, got %d", result.ObjectsDumped)
	}

	// Check that seed/objects/Document.jsonl was created.
	objFile := filepath.Join(outputDir, "seed", "objects", "Document.jsonl")
	if _, err := os.Stat(objFile); err != nil {
		t.Errorf("expected %s to exist: %v", objFile, err)
	}
}

func TestDumper_TypeFilterApplied(t *testing.T) {
	key := "doc-1"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/objects/search"):
			// Verify the types filter was sent.
			if !strings.Contains(r.URL.RawQuery, "types=Document") {
				t.Errorf("expected types=Document query param, got: %s", r.URL.RawQuery)
			}
			obj := &sdkgraph.GraphObject{EntityID: "eid-001", Type: "Document", Key: &key}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.SearchObjectsResponse{
				Items: []*sdkgraph.GraphObject{obj}, Total: 1,
			})

		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/relationships/search"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.SearchRelationshipsResponse{Items: nil, Total: 0})

		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := newTestGraphClient(srv.URL)
	outputDir := t.TempDir()

	d := blueprints.NewDumper(client, []string{"Document"}, nil)
	_, err := d.Run(context.Background(), outputDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDumper_KeyReferencesUsedWhenAvailable(t *testing.T) {
	srcKey, dstKey := "doc-1", "doc-2"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/objects/search"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.SearchObjectsResponse{
				Items: []*sdkgraph.GraphObject{
					{EntityID: "eid-001", Type: "Document", Key: &srcKey},
					{EntityID: "eid-002", Type: "Document", Key: &dstKey},
				},
				Total: 2,
			})

		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/relationships/search"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.SearchRelationshipsResponse{
				Items: []*sdkgraph.GraphRelationship{
					{EntityID: "rid-001", Type: "Mentions", SrcID: "eid-001", DstID: "eid-002"},
				},
				Total: 1,
			})

		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := newTestGraphClient(srv.URL)
	outputDir := t.TempDir()

	d := blueprints.NewDumper(client, nil, nil)
	result, err := d.Run(context.Background(), outputDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.RelationshipsDumped != 1 {
		t.Errorf("expected RelationshipsDumped=1, got %d", result.RelationshipsDumped)
	}

	// Read the relationship file and verify key references.
	relFile := filepath.Join(outputDir, "seed", "relationships", "Mentions.jsonl")
	data, err := os.ReadFile(relFile)
	if err != nil {
		t.Fatalf("read relationship file: %v", err)
	}
	var rec blueprints.SeedRelationshipRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("unmarshal relationship record: %v", err)
	}
	if rec.SrcKey != "doc-1" || rec.DstKey != "doc-2" {
		t.Errorf("expected SrcKey=doc-1 DstKey=doc-2, got SrcKey=%s DstKey=%s", rec.SrcKey, rec.DstKey)
	}
	if rec.SrcID != "" || rec.DstID != "" {
		t.Errorf("expected empty SrcID/DstID when keys are available, got SrcID=%s DstID=%s", rec.SrcID, rec.DstID)
	}
}

func TestDumper_IDFallbackWhenKeyMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/objects/search"):
			// Objects without keys.
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.SearchObjectsResponse{
				Items: []*sdkgraph.GraphObject{
					{EntityID: "eid-001", Type: "Node"},
					{EntityID: "eid-002", Type: "Node"},
				},
				Total: 2,
			})

		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/relationships/search"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.SearchRelationshipsResponse{
				Items: []*sdkgraph.GraphRelationship{
					{EntityID: "rid-001", Type: "Links", SrcID: "eid-001", DstID: "eid-002"},
				},
				Total: 1,
			})

		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := newTestGraphClient(srv.URL)
	outputDir := t.TempDir()

	d := blueprints.NewDumper(client, nil, nil)
	_, err := d.Run(context.Background(), outputDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	relFile := filepath.Join(outputDir, "seed", "relationships", "Links.jsonl")
	data, err := os.ReadFile(relFile)
	if err != nil {
		t.Fatalf("read relationship file: %v", err)
	}
	var rec blueprints.SeedRelationshipRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("unmarshal relationship record: %v", err)
	}
	if rec.SrcID != "eid-001" || rec.DstID != "eid-002" {
		t.Errorf("expected SrcID=eid-001 DstID=eid-002, got SrcID=%s DstID=%s", rec.SrcID, rec.DstID)
	}
	if rec.SrcKey != "" || rec.DstKey != "" {
		t.Errorf("expected empty SrcKey/DstKey for keyless objects, got SrcKey=%s DstKey=%s", rec.SrcKey, rec.DstKey)
	}
}

func TestDumper_SplitTriggeredAt50MBBoundary(t *testing.T) {
	// Generate objects that collectively exceed 50 MB.
	// Each record will be ~1000 bytes; we need >50*1024 = 51200 records.
	// To keep the test fast, we'll use a very small split size by verifying
	// the split logic directly: send enough data to exceed the threshold.
	// We simulate this by sending records that add up to more than dumpSplitSize.
	// Since dumpSplitSize is 50 MB and we don't want a huge test, we instead
	// verify that a second file is created when the threshold is exceeded.
	// We'll send one very large property to trigger the split quickly.

	const bigSize = 51 * 1024 * 1024 // 51 MB worth of property data

	bigVal := strings.Repeat("x", bigSize)
	callCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/objects/search"):
			callCount++
			var items []*sdkgraph.GraphObject
			if callCount == 1 {
				// Return two objects: first will fill >50 MB, second triggers split.
				items = []*sdkgraph.GraphObject{
					{EntityID: "eid-001", Type: "BigDoc", Properties: map[string]any{"data": bigVal}},
					{EntityID: "eid-002", Type: "BigDoc", Properties: map[string]any{"data": "small"}},
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.SearchObjectsResponse{Items: items, Total: len(items)})

		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/relationships/search"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.SearchRelationshipsResponse{Items: nil, Total: 0})

		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := newTestGraphClient(srv.URL)
	outputDir := t.TempDir()

	d := blueprints.NewDumper(client, nil, nil)
	result, err := d.Run(context.Background(), outputDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ObjectsDumped != 2 {
		t.Errorf("expected ObjectsDumped=2, got %d", result.ObjectsDumped)
	}

	// After splitting: BigDoc.001.jsonl and BigDoc.002.jsonl should exist.
	split1 := filepath.Join(outputDir, "seed", "objects", "BigDoc.001.jsonl")
	split2 := filepath.Join(outputDir, "seed", "objects", "BigDoc.002.jsonl")

	if _, err := os.Stat(split1); err != nil {
		t.Errorf("expected %s to exist after split: %v", split1, err)
	}
	if _, err := os.Stat(split2); err != nil {
		t.Errorf("expected %s to exist after split: %v", split2, err)
	}

	// The unsplit file should not exist.
	unsplit := filepath.Join(outputDir, "seed", "objects", "BigDoc.jsonl")
	if _, err := os.Stat(unsplit); err == nil {
		t.Errorf("expected unsplit BigDoc.jsonl to not exist after split")
	}

	_ = fmt.Sprintf // suppress unused import
}
