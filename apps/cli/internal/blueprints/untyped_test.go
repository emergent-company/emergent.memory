package blueprints_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/blueprints"
	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

// TestDumper_UntypedObjectsUseVisibleFileName covers issue #323: objects whose
// type is empty must be dumped to "_untyped.jsonl", not a file literally named
// ".jsonl".
func TestDumper_UntypedObjectsUseVisibleFileName(t *testing.T) {
	key := "repo-1"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/objects/search"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.SearchObjectsResponse{
				Items: []*sdkgraph.GraphObject{
					{EntityID: "eid-untyped", Type: "", Key: &key},
				},
				Total: 1,
			})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/relationships/search"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.SearchRelationshipsResponse{Items: nil, Total: 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	outputDir := t.TempDir()
	d := blueprints.NewDumper(newTestGraphClient(srv.URL), nil, nil)
	result, err := d.Run(context.Background(), outputDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ObjectsDumped != 1 {
		t.Fatalf("expected ObjectsDumped=1, got %d", result.ObjectsDumped)
	}

	untypedPath := filepath.Join(outputDir, "seed", "objects", "_untyped.jsonl")
	if _, err := os.Stat(untypedPath); err != nil {
		t.Fatalf("expected %s to exist: %v", untypedPath, err)
	}
	hiddenPath := filepath.Join(outputDir, "seed", "objects", ".jsonl")
	if _, err := os.Stat(hiddenPath); !os.IsNotExist(err) {
		t.Fatalf("expected no hidden %q file, stat err = %v", hiddenPath, err)
	}

	data, err := os.ReadFile(untypedPath)
	if err != nil {
		t.Fatalf("read untyped file: %v", err)
	}
	var rec blueprints.SeedObjectRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("unmarshal object record: %v", err)
	}
	if rec.Type != "" {
		t.Errorf("expected dump to preserve empty type, got %q", rec.Type)
	}
	if rec.Key != key {
		t.Errorf("expected key %q, got %q", key, rec.Key)
	}
}

// TestDumper_UntypedRoundTrip proves the _untyped.jsonl file is picked up by
// the loader on install and restored with a visible placeholder type.
func TestDumper_UntypedRoundTrip(t *testing.T) {
	key := "workspace-1"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/objects/search"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.SearchObjectsResponse{
				Items: []*sdkgraph.GraphObject{
					{EntityID: "eid-untyped", Type: "", Key: &key},
				},
				Total: 1,
			})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/relationships/search"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sdkgraph.SearchRelationshipsResponse{Items: nil, Total: 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	outputDir := t.TempDir()
	d := blueprints.NewDumper(newTestGraphClient(srv.URL), nil, nil)
	if _, err := d.Run(context.Background(), outputDir); err != nil {
		t.Fatalf("dump: %v", err)
	}

	_, _, _, _, objects, _, results, err := blueprints.LoadDir(outputDir, nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, r := range results {
		if r.Action == blueprints.BlueprintsActionError {
			t.Fatalf("unexpected load error for %s: %v", r.Name, r.Error)
		}
	}
	if len(objects) != 1 {
		t.Fatalf("expected 1 loaded object, got %d", len(objects))
	}
	if objects[0].Type != blueprints.UntypedTypeName {
		t.Errorf("expected placeholder type %q, got %q", blueprints.UntypedTypeName, objects[0].Type)
	}
	if objects[0].Key != key {
		t.Errorf("expected key %q, got %q", key, objects[0].Key)
	}
}

// TestLoadDir_UntypedSplitFilesRecovered verifies split untyped files keep
// loading in order and are all normalized to the placeholder type.
func TestLoadDir_UntypedSplitFilesRecovered(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "seed/objects/_untyped.001.jsonl", `{"type":"","key":"first"}
`)
	writeFile(t, dir, "seed/objects/_untyped.002.jsonl", `{"type":"","key":"second"}
`)

	_, _, _, _, objects, _, results, err := blueprints.LoadDir(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, r := range results {
		if r.Action == blueprints.BlueprintsActionError {
			t.Fatalf("unexpected load error for %s: %v", r.Name, r.Error)
		}
	}
	if len(objects) != 2 {
		t.Fatalf("expected 2 objects, got %d", len(objects))
	}
	if objects[0].Key != "first" || objects[1].Key != "second" {
		t.Errorf("unexpected order: %q, %q", objects[0].Key, objects[1].Key)
	}
	for _, obj := range objects {
		if obj.Type != blueprints.UntypedTypeName {
			t.Errorf("expected placeholder type %q, got %q", blueprints.UntypedTypeName, obj.Type)
		}
	}
}

// TestLoadDir_EmptyTypeOutsideUntypedFileStillRejected keeps the strict
// validation for hand-authored seed files: only the dedicated _untyped.jsonl
// dump file gets the placeholder repair.
func TestLoadDir_EmptyTypeOutsideUntypedFileStillRejected(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "seed/objects/Repository.jsonl", `{"type":"","key":"r1"}
`)

	_, _, _, _, objects, _, results, err := blueprints.LoadDir(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(objects) != 0 {
		t.Fatalf("expected 0 valid objects, got %d", len(objects))
	}
	if len(results) != 1 || results[0].Action != blueprints.BlueprintsActionError {
		t.Fatalf("expected 1 validation error, got %+v", results)
	}
}
